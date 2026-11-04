//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/pkg/audio"
)

// The sampler plays a corpus loop's tonal lines on NSynth's recorded notes: 4 s
// of 16 kHz mono each, held for 3 s and released over the last.
const (
	nsynthRate    = 16000
	nsynthHold    = 3.0   // seconds a recorded note is held before its release
	sampledReach  = 3     // semitones a recorded pitch is shifted at most
	sampledDraws  = 8     // instruments drawn for a slot before its engine voice plays it
	sampledFade   = 0.010 // seconds a cut note crossfades into its sample's release
	sampledTop    = 7500  // Hz, the shift's low-pass, under the recordings' 8 kHz
	sampledHalf   = 29    // the low-pass's half width in source samples: 60 dB by 8 kHz
	sampledPhases = 512   // the low-pass's table steps a source sample
	sampledWindow = 0.050 // seconds a note's level is measured over, its loudest held window
	sampledAlone  = 0.15  // RMS sampled lines are mixed against in a silent render, an engine loop's median
)

// sampledMix is the level a slot's sampled line is mixed at, dB of its RMS
// against the rest of the render: the melody and bass beside it, the fill under
var sampledMix = [audio.MusicSlots][2]float64{{}, {-6, 3}, {-12, -3}, {-6, 3}}

// sampleNote is one recorded note on disk, its velocity layer
type sampleNote struct {
	file string
	vel  int
}

// sampleInstr is one recorded instrument, its notes by pitch, the layers in order
type sampleInstr struct {
	name    string
	pitches []int
	notes   map[int][]sampleNote
}

// sampleSet indexes a directory of NSynth notes, named
// family_source_NNN-pitch-velocity.wav, by instrument, the bass family's apart;
// share is the chance a loop's tonal slot plays on them
type sampleSet struct {
	dir         string
	bass, other []*sampleInstr
	share       float64
}

// sampledLine is a tonal slot a loop plays on recorded notes: the instrument,
// each note's end as it sounds, in seconds, in the order ExpectedEvents gives
// the slot's notes, and the line's render until it is mixed
type sampledLine struct {
	instr string
	ends  []float64
	audio []float64
}

func loadSampleSet(dir string) (*sampleSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	set := &sampleSet{dir: dir, share: 0.5}
	byName := map[string]*sampleInstr{}
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".wav")
		if !ok || e.IsDir() {
			continue
		}
		// from the right: a family's name can hold an underscore (synth_lead)
		rest, v, ok1 := cutLast(base, "-")
		name, p, ok2 := cutLast(rest, "-")
		family, _, ok3 := cutLast(name, "_")
		family, _, ok4 := cutLast(family, "_")
		pitch, err1 := strconv.Atoi(p)
		vel, err2 := strconv.Atoi(v)
		if !ok1 || !ok2 || !ok3 || !ok4 || err1 != nil || err2 != nil {
			continue
		}
		in := byName[name]
		if in == nil {
			in = &sampleInstr{name: name, notes: map[int][]sampleNote{}}
			byName[name] = in
			if family == "bass" {
				set.bass = append(set.bass, in)
			} else {
				set.other = append(set.other, in)
			}
		}
		if in.notes[pitch] == nil {
			in.pitches = append(in.pitches, pitch)
		}
		in.notes[pitch] = append(in.notes[pitch], sampleNote{filepath.Join(dir, e.Name()), vel})
	}
	if len(byName) == 0 {
		return nil, fmt.Errorf("sampled %s: no family_source_NNN-pitch-velocity.wav notes", dir)
	}
	for _, in := range byName {
		slices.Sort(in.pitches)
		for _, layers := range in.notes {
			slices.SortFunc(layers, func(a, b sampleNote) int { return cmp.Compare(a.vel, b.vel) })
		}
	}
	return set, nil
}

func cutLast(s, sep string) (before, after string, ok bool) {
	i := strings.LastIndex(s, sep)
	if i <= 0 || i == len(s)-len(sep) {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// nearest is the recorded pitch closest to midi, the higher of two so a note is
// shifted down, which keeps it under the band's top
func (in *sampleInstr) nearest(midi int) int {
	i, _ := slices.BinarySearch(in.pitches, midi)
	if i == len(in.pitches) || i > 0 && midi-in.pitches[i-1] < in.pitches[i]-midi {
		i--
	}
	return in.pitches[i]
}

// covers says whether every note is within reach of a recorded pitch
func (in *sampleInstr) covers(notes []audio.RipEvent) bool {
	for _, e := range notes {
		if d := in.nearest(e.MIDI) - e.MIDI; d < -sampledReach || d > sampledReach {
			return false
		}
	}
	return true
}

// layer is the recorded note at pitch whose velocity is nearest vel's
func (in *sampleInstr) layer(pitch int, vel float64) sampleNote {
	layers := in.notes[pitch]
	return slices.MinFunc(layers, func(a, b sampleNote) int {
		return cmp.Compare(math.Abs(float64(a.vel)-vel*127), math.Abs(float64(b.vel)-vel*127))
	})
}

// draw picks which of a loop's tonal slots play on recorded notes and on what,
// each with its share's chance, and renders their lines: the slots the engine
// renders come back with those silenced. A slot whose pattern holds a drum
// track, or which no instrument drawn covers, keeps its engine voice.
func (set *sampleSet) draw(slots [audio.MusicSlots]audio.PatternID, bars, bpm, root int, scale audio.ScaleID, cents float64, rng *rand.Rand) ([audio.MusicSlots]audio.PatternID, [audio.MusicSlots]*sampledLine, error) {
	var lines [audio.MusicSlots]*sampledLine
	pedal := rng.Float64() < 1.0/3
	cache := map[sampleKey][]float64{}
	played := slots
	for k := 1; k < audio.MusicSlots; k++ {
		if rng.Float64() >= set.share {
			continue
		}
		p := audio.GetPattern(slots[k])
		if p == nil || slices.ContainsFunc(p.Tracks, func(t audio.Track) bool { return t.Instr.IsDrum() }) {
			continue
		}
		notes := slotEvents(slots, k, bars, root, scale)
		pool := set.other
		if k == audio.MusicSlots-1 {
			pool = set.bass
		}
		if len(notes) == 0 || len(pool) == 0 {
			continue
		}
		var in *sampleInstr
		for range sampledDraws {
			if c := pool[rng.IntN(len(pool))]; c.covers(notes) {
				in = c
				break
			}
		}
		if in == nil {
			continue
		}
		line, err := set.line(in, notes, bars, bpm, cents, pedal && k != audio.MusicSlots-1, cache)
		if err != nil {
			return slots, lines, err
		}
		lines[k], played[k] = line, audio.PatternSilence
	}
	return played, lines, nil
}

// slotEvents is ExpectedEvents of slot k alone
func slotEvents(slots [audio.MusicSlots]audio.PatternID, k, bars, root int, scale audio.ScaleID) []audio.RipEvent {
	var one [audio.MusicSlots]audio.PatternID
	one[k] = slots[k]
	return audio.ExpectedEvents(one, bars, root, scale, nil)
}

// sampleKey is a recorded note shifted by semitones, as one loop plays it
type sampleKey struct {
	file  string
	shift int
}

// line renders a slot's notes on an instrument's recordings, each at the layer
// nearest its velocity, shifted from the nearest recorded pitch, its own level
// taken out and its velocity put in. A note held past its sample's hold ends
// there; a shorter one is cut at its note-off into the sample's release, or
// under the pedal rings on uncut, labelled as played.
func (set *sampleSet) line(in *sampleInstr, notes []audio.RipEvent, bars, bpm int, cents float64, pedal bool, cache map[sampleKey][]float64) (*sampledLine, error) {
	sr := float64(audio.AudioSampleRate)
	stepSec := float64(audio.SamplesPerStep(bpm)) / sr
	l := &sampledLine{instr: in.name, audio: make([]float64, bars*audio.SamplesPerBar(bpm))}
	fade := int(sampledFade * sr)
	for _, e := range notes {
		pitch := in.nearest(e.MIDI)
		key := sampleKey{in.layer(pitch, e.Vel).file, e.MIDI - pitch}
		ratio := math.Exp2(float64(key.shift)/12 + cents/1200)
		x, ok := cache[key]
		if !ok {
			var err error
			if x, err = recorded(key.file, ratio); err != nil {
				return nil, err
			}
			cache[key] = x
		}
		start := float64(e.Bar*audio.StepsPerBar+e.Step) * stepSec
		hold := min(int(nsynthHold/ratio*sr), len(x))
		cut := int(float64(e.Dur) * stepSec * sr)
		end := start + float64(min(cut, hold))/sr
		at := int(start * sr)
		put := func(i int, v float64) {
			if i += at; i < len(l.audio) {
				l.audio[i] += v * e.Vel
			}
		}
		if pedal || cut+fade >= hold {
			for i, v := range x {
				put(i, v)
			}
		} else {
			for i := range cut {
				put(i, x[i])
			}
			for i := range len(x) - hold {
				v := x[hold+i]
				if i < fade {
					g := float64(i) / float64(fade)
					v = x[cut+i]*(1-g) + v*g
				}
				put(cut+i, v)
			}
		}
		l.ends = append(l.ends, end)
	}
	return l, nil
}

// recorded is a recorded note at the engine's rate sped up by ratio, divided
// by its loudest held window's RMS
func recorded(file string, ratio float64) ([]float64, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	x, rate, err := audio.ReadWAV(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if rate != nsynthRate {
		return nil, fmt.Errorf("%s: %d Hz, want NSynth's %d", file, rate, nsynthRate)
	}
	n, held := int(sampledWindow*nsynthRate), min(len(x), int(nsynthHold*nsynthRate))
	var level float64
	for i := 0; i+n <= held; i += n / 2 {
		var e float64
		for _, v := range x[i : i+n] {
			e += v * v
		}
		level = max(level, math.Sqrt(e/float64(n)))
	}
	if level == 0 {
		return nil, fmt.Errorf("%s: silent", file)
	}
	y := shiftNote(x, ratio)
	for i := range y {
		y[i] /= level
	}
	return y, nil
}

// shiftNote reads x at ratio times its rate into the engine's rate through a
// Kaiser-windowed sinc, its cutoff under sampledTop in the output and under
// the source's Nyquist, where audio.Resample's linear upsampling would image
func shiftNote(x []float64, ratio float64) []float64 {
	step := ratio * nsynthRate / audio.AudioSampleRate     // source samples an output sample
	fc := float64(sampledTop) / nsynthRate / max(ratio, 1) // cycles a source sample
	const beta = 5.65                                      // a Kaiser window of 60 dB
	table := make([]float64, 2*sampledHalf*sampledPhases+2)
	i0 := func(v float64) float64 { // the modified Bessel function of order zero
		sum, term := 1.0, 1.0
		for k := 1; k < 32; k++ {
			term *= v * v / (4 * float64(k*k))
			sum += term
		}
		return sum
	}
	for j := range table {
		u := float64(j)/sampledPhases - sampledHalf
		w := 0.0
		if r := u / sampledHalf; r*r <= 1 {
			w = i0(beta*math.Sqrt(1-r*r)) / i0(beta)
		}
		s := 2 * fc
		if u != 0 {
			s = math.Sin(2*math.Pi*fc*u) / (math.Pi * u)
		}
		table[j] = s * w
	}
	y := make([]float64, int(float64(len(x))/step))
	for i := range y {
		t := float64(i) * step
		c := int(t)
		var acc float64
		for j := max(c-sampledHalf+1, 0); j <= min(c+sampledHalf, len(x)-1); j++ {
			pos := (float64(j) - t + sampledHalf) * sampledPhases
			k := int(pos)
			acc += x[j] * (table[k] + (pos-float64(k))*(table[k+1]-table[k]))
		}
		y[i] = acc
	}
	return y
}

// mixSampled adds the sampled lines to the engine's render, each at a level
// drawn against the render's RMS, or against a fixed one when the render is
// silent, the sum turned down if it passes full scale
func mixSampled(buf []float64, lines [audio.MusicSlots]*sampledLine, rng *rand.Rand) []float64 {
	rest := rms(buf)
	if rest < 1e-6 {
		rest = sampledAlone
	}
	for k, l := range lines {
		if l == nil {
			continue
		}
		db := sampledMix[k][0] + rng.Float64()*(sampledMix[k][1]-sampledMix[k][0])
		got := rms(l.audio)
		if got == 0 {
			continue
		}
		g := rest * math.Pow(10, db/20) / got
		for i, v := range l.audio[:min(len(l.audio), len(buf))] {
			buf[i] += v * g
		}
		l.audio = nil // in the mix now; a bench holds its loops
	}
	var peak float64
	for _, v := range buf {
		peak = max(peak, math.Abs(v))
	}
	if peak > 1 {
		for i := range buf {
			buf[i] /= peak
		}
	}
	return buf
}
