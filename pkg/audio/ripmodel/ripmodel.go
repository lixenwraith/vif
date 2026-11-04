// Package ripmodel is the rip's learned front end: a transformer over log-mel
// frames whose heads say, per frame, whether anything and which drum struck,
// which pitches sound, are struck or let go and how hard, and the note's wave and
// genes. stapes' rip-train trains it on loops the engine renders; Spec is the shape
// both sides build, and a weight file loads into a FrontEnd the Ripper hears with.
package ripmodel

import (
	"fmt"
	"io"
	"math"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/transformer"
)

// The heads, in the order the output vector lays them
const (
	HeadOnset    = "onset"    // anything struck or started at this frame
	HeadDrums    = "drums"    // kick, snare, hihat
	HeadPitch    = "pitch"    // each of the rip's pitches sounding
	HeadVoicing  = "voicing"  // any note sounding
	HeadWave     = "wave"     // the sounding note's wave, one score per VoiceWaves entry
	HeadVoice    = "voice"    // its voice genes
	HeadStrike   = "strike"   // each of the rip's pitches struck at this frame
	HeadRelease  = "release"  // each of the rip's pitches let go at this frame
	HeadVelocity = "velocity" // how hard each pitch struck at this frame was struck
)

// Spec is the model the trainer builds and a weight file must match in its
// inputs and heads; its width, depth and window may differ.
func Spec() transformer.Config {
	return transformer.Config{
		Inputs: audio.RipMelBands + 1, Dim: 256, Heads: 8, Layers: 4, FFN: 1024, Window: 64,
		Outputs: []transformer.Head{
			{Name: HeadOnset, Size: 1}, {Name: HeadDrums, Size: int(audio.InstrClap)}, {Name: HeadPitch, Size: audio.RipPitches},
			{Name: HeadVoicing, Size: 1}, {Name: HeadWave, Size: len(audio.VoiceWaves)}, {Name: HeadVoice, Size: audio.VoiceGeneCount},
			{Name: HeadStrike, Size: audio.RipPitches}, {Name: HeadRelease, Size: audio.RipPitches}, {Name: HeadVelocity, Size: audio.RipPitches},
		},
	}
}

// FrontEnd hears with a model
type FrontEnd struct {
	Model *transformer.Model
}

// Load reads a weight file and checks it hears and says what the rip expects.
func Load(r io.Reader) (*FrontEnd, error) {
	m, err := transformer.Load(r)
	if err != nil {
		return nil, err
	}
	return New(m)
}

// optionalHeads is how many of Spec's last heads a model may lack: one trained
// before the strike, release or velocity head loads without them, its notes read
// from what it has
const optionalHeads = 3

// New wraps a model, refusing one whose inputs or heads are not a prefix of Spec's.
func New(m *transformer.Model) (*FrontEnd, error) {
	want, got := Spec(), m.Config()
	if got.Inputs != want.Inputs {
		return nil, fmt.Errorf("ripmodel: model reads %d inputs, want %d, the bands in dB over the frame's level and the level", got.Inputs, want.Inputs)
	}
	if n := len(got.Outputs); n > len(want.Outputs) || n < len(want.Outputs)-optionalHeads {
		return nil, fmt.Errorf("ripmodel: model has %d heads, want %d", n, len(want.Outputs))
	}
	for i, h := range want.Outputs[:len(got.Outputs)] {
		if got.Outputs[i] != h {
			return nil, fmt.Errorf("ripmodel: head %d is %q of %d, want %q of %d", i, got.Outputs[i].Name, got.Outputs[i].Size, h.Name, h.Size)
		}
	}
	return &FrontEnd{Model: m}, nil
}

// Hear runs the recording's log-mel frames through the model and reads its heads
// into frames: the onset and drum scores as strengths, the pitch scores as
// salience, the chroma folded from them, the wave scores and voice genes as they
// are, and the strike, release and velocity scores when the model has them.
func (fe *FrontEnd) Hear(x []float64, rate int, _ audio.RipParams) (*audio.RipFrames, error) {
	out := fe.Model.Forward(features(audio.MelFrames(x, rate)))
	n := len(out)
	f := &audio.RipFrames{Hop: audio.RipFrameSec, Flux: make([]float64, n),
		Salience:   make([]float32, 0, n*audio.RipPitches),
		Chroma:     make([][12]float64, n),
		BassChroma: make([][12]float64, n),
		Wave:       make([]float32, 0, n*len(audio.VoiceWaves)),
		Voice:      make([]float32, 0, n*audio.VoiceGeneCount)}
	for d := range f.Onset {
		f.Onset[d] = make([]float64, n)
	}
	m := fe.Model
	heads := len(m.Config().Outputs)
	per := []struct {
		head string
		into *[]float32
	}{{HeadStrike, &f.Strike}, {HeadRelease, &f.Release}, {HeadVelocity, &f.Velocity}}[:heads-(len(Spec().Outputs)-optionalHeads)]
	for _, h := range per {
		*h.into = make([]float32, 0, n*audio.RipPitches)
	}
	for t, y := range out {
		f.Flux[t] = sigmoid(m.Slice(y, HeadOnset)[0])
		for d, v := range m.Slice(y, HeadDrums) {
			f.Onset[d][t] = sigmoid(v)
		}
		for i, v := range m.Slice(y, HeadPitch) {
			s := sigmoid(v)
			f.Salience = append(f.Salience, float32(s))
			midi := audio.RipMinMIDI + i
			f.Chroma[t][midi%12] += s
			if midi < audio.RipBassTop {
				f.BassChroma[t][midi%12] += s
			}
		}
		for _, v := range m.Slice(y, HeadWave) {
			f.Wave = append(f.Wave, v)
		}
		for _, v := range m.Slice(y, HeadVoice) {
			f.Voice = append(f.Voice, float32(sigmoid(v)))
		}
		for _, h := range per {
			for _, v := range m.Slice(y, h.head) {
				*h.into = append(*h.into, float32(sigmoid(v)))
			}
		}
	}
	return f, nil
}

// melFloor is the energy 80 dB under a full-scale sine's, where the logarithm floors
const melFloor = 1e-8

// features is the model's input from MelFrames: each band's energy in dB over
// melFloor less the frame's mean, and the mean itself, both over 80 dB, so a
// frame's loudness is one input and the bands are level-free: a soft phrase
// reaches the model as clearly as a loud one
func features(mel [][]float32) [][]float32 {
	out := make([][]float32, len(mel))
	for t, row := range mel {
		db := make([]float64, len(row))
		var level float64
		for b, v := range row {
			db[b] = 10 * math.Log10(math.Expm1(float64(v))/100+melFloor)
			level += db[b]
		}
		level /= float64(len(row))
		f := make([]float32, len(row)+1)
		for b, d := range db {
			f[b] = float32((d - level) / 80)
		}
		f[len(row)] = float32((level + 80) / 80)
		out[t] = f
	}
	return out
}

func sigmoid(v float32) float64 { return 1 / (1 + math.Exp(-float64(v))) }
