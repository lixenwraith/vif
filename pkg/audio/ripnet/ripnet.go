// Package ripnet is the rip's learned front end: a transformer over log-mel
// frames whose heads say, per frame, whether anything and which drum struck,
// which pitches sound, and which wave and voice genes the note is played with.
// tool/rip-train trains it on loops the engine renders; Spec is the shape both
// sides build, and a weight file loads into a FrontEnd the Ripper hears with.
package ripnet

import (
	"fmt"
	"io"
	"math"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/transformer"
)

// The heads, in the order the output vector lays them
const (
	HeadOnset   = "onset"   // anything struck or started at this frame
	HeadDrums   = "drums"   // kick, snare, hihat
	HeadPitch   = "pitch"   // each of the rip's pitches sounding
	HeadVoicing = "voicing" // any note sounding
	HeadWave    = "wave"    // the sounding note's wave, one score per VoiceWaves entry
	HeadVoice   = "voice"   // its voice genes
)

// Spec is the model the trainer builds and a weight file must match in its
// inputs and heads; its width, depth and window may differ.
func Spec() transformer.Config {
	return transformer.Config{
		Inputs: audio.RipMelBands, Dim: 128, Heads: 4, Layers: 4, FFN: 512, Window: 64,
		Outputs: []transformer.Head{
			{Name: HeadOnset, Size: 1}, {Name: HeadDrums, Size: int(audio.InstrClap)}, {Name: HeadPitch, Size: audio.RipPitches},
			{Name: HeadVoicing, Size: 1}, {Name: HeadWave, Size: len(audio.VoiceWaves)}, {Name: HeadVoice, Size: audio.VoiceGeneCount},
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

// New wraps a model, refusing one whose inputs or heads are not Spec's.
func New(m *transformer.Model) (*FrontEnd, error) {
	want, got := Spec(), m.Config()
	if got.Inputs != want.Inputs {
		return nil, fmt.Errorf("ripnet: model hears %d bands, want %d", got.Inputs, want.Inputs)
	}
	if len(got.Outputs) != len(want.Outputs) {
		return nil, fmt.Errorf("ripnet: model has %d heads, want %d", len(got.Outputs), len(want.Outputs))
	}
	for i, h := range want.Outputs {
		if got.Outputs[i] != h {
			return nil, fmt.Errorf("ripnet: head %d is %q of %d, want %q of %d", i, got.Outputs[i].Name, got.Outputs[i].Size, h.Name, h.Size)
		}
	}
	return &FrontEnd{Model: m}, nil
}

// Hear runs the recording's log-mel frames through the model and reads its heads
// into frames: the onset and drum scores as strengths, the pitch scores as
// salience, the chroma folded from them, and the wave scores and voice genes as
// they are.
func (fe *FrontEnd) Hear(x []float64, rate int, _ audio.RipParams) (*audio.RipFrames, error) {
	mel := audio.MelFrames(x, rate)
	out := fe.Model.Forward(mel)
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
	}
	return f, nil
}

func sigmoid(v float32) float64 { return 1 / (1 + math.Exp(-float64(v))) }
