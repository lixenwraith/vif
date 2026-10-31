package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// renderLoop plays a beat, a melody and a bass line through a sequencer for bars
// bars at bpm, on a kit of the test bank's drums and saw voices, and returns the
// music as RipSamples would hear it
func renderLoop(t *testing.T, bpm, bars int) ([]float64, map[string]PatternID) {
	t.Helper()
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	bank := append(testBank(),
		&SoundDef{Name: "snare", Duration: 0.18, Norm: 0.8, Chain: []Proc{{Kind: "hp", Freq: 150}}, Layer: []Layer{
			{Gain: 0.7, Source: Source{Kind: "sweep", Wave: "sine", Freq: 200, FreqEnd: 170, Curve: "exp"}, Chain: []Proc{{Kind: "decay", Tau: 0.05}}},
			{Source: Source{Kind: "noise"}, Chain: []Proc{{Kind: "bp", Freq: 2500, Q: 0.6}, {Kind: "decay", Tau: 0.06}}}}},
		&SoundDef{Name: "hihat", Duration: 0.07, Norm: 0.7, Layer: []Layer{
			{Source: Source{Kind: "noise"}, Chain: []Proc{{Kind: "hp", Freq: 7000}, {Kind: "decay", Tau: 0.012}}}}})
	for _, d := range bank {
		if _, err := RegisterSound(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []*VoiceDef{
		{Name: "lead", Wave: "saw", Cutoff: 2500, Q: 0.5, Attack: 0.005, Decay: 0.1, Sustain: 0.7, Release: 0.08},
		{Name: "bass", Wave: "saw", Cutoff: 600, Q: 0.7, Attack: 0.005, Decay: 0.1, Sustain: 0.8, Release: 0.06},
	} {
		if _, err := RegisterVoice(v); err != nil {
			t.Fatal(err)
		}
	}
	freezeSounds()
	cache := newSoundCache()
	cache.preloadAll(nil)
	k := buildKit(cache)
	ids := map[string]PatternID{
		"beat": RegisterPattern(&Pattern{Name: "beat", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 1}, {Pos: 4, Vel: 1}, {Pos: 8, Vel: 1}, {Pos: 12, Vel: 1}}},
			{Instr: InstrSnare, Events: []Step{{Pos: 4, Vel: 0.9}, {Pos: 12, Vel: 0.9}}},
			{Instr: InstrHihat, Events: []Step{{Pos: 2, Vel: 0.6}, {Pos: 6, Vel: 0.6}, {Pos: 10, Vel: 0.6}, {Pos: 14, Vel: 0.6}}}}}),
		"tune": RegisterPattern(&Pattern{Name: "tune", Steps: 16, Tracks: []Track{{Instr: InstrLead, Events: []Step{
			{Pos: 0, Vel: 0.9, Deg: 0, Oct: 2, Dur: 2}, {Pos: 2, Vel: 0.8, Deg: 1, Oct: 2, Dur: 2}, {Pos: 4, Vel: 0.8, Deg: 2, Oct: 2, Dur: 2}, {Pos: 6, Vel: 0.8, Deg: 3, Oct: 2, Dur: 2},
			{Pos: 8, Vel: 0.9, Deg: 4, Oct: 2, Dur: 2}, {Pos: 10, Vel: 0.8, Deg: 5, Oct: 2, Dur: 2}, {Pos: 12, Vel: 0.8, Deg: 6, Oct: 2, Dur: 2}, {Pos: 14, Vel: 0.9, Deg: 0, Oct: 3, Dur: 2}}}}}),
		"low": RegisterPattern(&Pattern{Name: "low", Steps: 16, Tracks: []Track{{Instr: InstrBass, Events: []Step{
			{Pos: 0, Vel: 0.9, Deg: 0, Oct: 0, Dur: 4}, {Pos: 8, Vel: 0.9, Deg: 4, Oct: 0, Dur: 4}}}}}),
	}
	for name, id := range ids {
		if id == PatternSilence {
			t.Fatalf("%s did not register", name)
		}
	}
	s := NewSequencer(bpm, k)
	s.SetPattern(0, ids["beat"], 0, false)
	s.SetPattern(1, ids["tune"], 0, false)
	s.SetPattern(3, ids["low"], 0, false)
	s.Start()
	buf := make([]float64, SamplesPerBar(bpm)*bars)
	s.Generate(buf)
	return buf, ids
}

// recall is the share of the authored steps of a track found in the ripped bars
// after the first, at their step and, for notes, their pitch
func recall(t *testing.T, r *Rip, authored []Step, pick func(*RipBar) []Step, pitched bool) float64 {
	t.Helper()
	found, want := 0, 0
	for b := 1; b < len(r.Bars); b++ {
		for _, a := range authored {
			want++
			for _, s := range pick(&r.Bars[b]) {
				if s.Pos != a.Pos {
					continue
				}
				if !pitched || DefaultRootNote+12*s.Oct+scaleTable[r.Scale][s.Deg] == DefaultRootNote+12*a.Oct+scaleTable[ScalePhrygian][a.Deg] {
					found++
					break
				}
			}
		}
	}
	return float64(found) / float64(max(want, 1))
}

// TestRipHearsTheLoop is the rule a rip answers to: played the engine's own music,
// it finds the tempo, the bars, the key, the drums on their steps and most of the
// melody and bass notes on theirs.
func TestRipHearsTheLoop(t *testing.T) {
	const bpm, bars = 140, 8
	buf, _ := renderLoop(t, bpm, bars)
	r, err := RipSamples(buf, AudioSampleRate, DefaultRipParams())
	if err != nil {
		t.Fatal(err)
	}
	if d := r.BPM - bpm; d < -2 || d > 2 {
		t.Errorf("tempo %d, want %d", r.BPM, bpm)
	}
	if len(r.Bars) < bars-2 {
		t.Fatalf("%d bars found of %d", len(r.Bars), bars)
	}
	if r.Root%12 != DefaultRootNote%12 {
		t.Errorf("root %d, want the pitch class of %d", r.Root, DefaultRootNote)
	}
	if r.Scale != ScalePhrygian {
		t.Errorf("scale %d, want phrygian", r.Scale)
	}
	kicks := recall(t, r, []Step{{Pos: 0}, {Pos: 4}, {Pos: 8}, {Pos: 12}}, func(b *RipBar) []Step { return b.Drums[InstrKick] }, false)
	snares := recall(t, r, []Step{{Pos: 4}, {Pos: 12}}, func(b *RipBar) []Step { return b.Drums[InstrSnare] }, false)
	hats := recall(t, r, []Step{{Pos: 2}, {Pos: 6}, {Pos: 10}, {Pos: 14}}, func(b *RipBar) []Step { return b.Drums[InstrHihat] }, false)
	if kicks < 0.75 || snares < 0.6 || hats < 0.6 {
		t.Errorf("drum recall: kicks %.2f snares %.2f hats %.2f", kicks, snares, hats)
	}
	tune := GetPattern(PatternIDByName("tune")).Tracks[0].Events
	low := GetPattern(PatternIDByName("low")).Tracks[0].Events
	melody := recall(t, r, tune, func(b *RipBar) []Step { return b.Melody }, true)
	bass := recall(t, r, low, func(b *RipBar) []Step { return b.Bass }, true)
	if melody < 0.6 || bass < 0.5 {
		t.Errorf("note recall: melody %.2f bass %.2f", melody, bass)
	}
	if n := len(r.Patterns("loop", 4)); n < 3 {
		t.Errorf("%d patterns written, want a beat, a melody and a bass at least", n)
	}
	t.Logf("bpm %d root %d scale %d bars %d downbeat %.2fs; recall kick %.2f snare %.2f hat %.2f melody %.2f bass %.2f",
		r.BPM, r.Root, r.Scale, len(r.Bars), r.Downbeat, kicks, snares, hats, melody, bass)
}

// TestRipPatternsWindowTheBars is the rule Patterns writes by: a pattern per part
// per window of up to four bars, the last window as long as what is left.
func TestRipPatternsWindowTheBars(t *testing.T) {
	r := &Rip{Root: 40, Scale: ScalePhrygian, Bars: make([]RipBar, 6)}
	for b := range r.Bars {
		r.Bars[b].Drums[InstrKick] = []Step{{Pos: 0, Vel: 1}}
		r.Bars[b].Melody = []Step{{Pos: 8, Vel: 0.5, Deg: b % 7, Oct: 2, Dur: 2}}
	}
	pats := r.Patterns("x", 4)
	if len(pats) != 4 {
		t.Fatalf("%d patterns, want two windows of a beat and a melody", len(pats))
	}
	if pats[0].Name != "x_beat_1" || pats[0].Steps != 64 || pats[2].Name != "x_beat_2" || pats[2].Steps != 32 {
		t.Errorf("windows %s/%d and %s/%d", pats[0].Name, pats[0].Steps, pats[2].Name, pats[2].Steps)
	}
	if ev := pats[3].Track[0].Event; len(ev) != 2 || ev[1].Pos != 24 || ev[1].Deg != 5 {
		t.Errorf("second melody window: %+v", ev)
	}
	for _, p := range pats {
		if _, err := p.Pattern(); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
	}
}

// TestReadWAVReadsWhatWriteWAVWrote is the round trip of the two, and the other
// widths a recording comes in.
func TestReadWAVReadsWhatWriteWAVWrote(t *testing.T) {
	src := make([]float64, 1000)
	for i := range src {
		src[i] = 0.5 * math.Sin(float64(i)/10)
	}
	var b bytes.Buffer
	if err := WriteWAV(&b, src); err != nil {
		t.Fatal(err)
	}
	got, rate, err := ReadWAV(&b)
	if err != nil || rate != AudioSampleRate || len(got) != len(src) {
		t.Fatalf("read: %v, %d Hz, %d samples", err, rate, len(got))
	}
	for i := range src {
		if math.Abs(got[i]-src[i]) > 2.0/32768 { // the writer scales by 32767 and truncates
			t.Fatalf("sample %d: %g, wrote %g", i, got[i], src[i])
		}
	}
	// a 24-bit mono file at 48 kHz with a chunk the reader does not know
	var w bytes.Buffer
	le := binary.LittleEndian
	w.WriteString("RIFF")
	binary.Write(&w, le, uint32(0))
	w.WriteString("WAVE")
	w.WriteString("LIST")
	binary.Write(&w, le, uint32(3))
	w.Write([]byte{1, 2, 3, 0}) // padded to even
	w.WriteString("fmt ")
	binary.Write(&w, le, uint32(16))
	binary.Write(&w, le, uint16(1))
	binary.Write(&w, le, uint16(1))
	binary.Write(&w, le, uint32(48000))
	binary.Write(&w, le, uint32(48000*3))
	binary.Write(&w, le, uint16(3))
	binary.Write(&w, le, uint16(24))
	w.WriteString("data")
	binary.Write(&w, le, uint32(6))
	w.Write([]byte{0, 0, 0x40, 0, 0, 0xC0}) // +0.5, -0.5
	got, rate, err = ReadWAV(&w)
	if err != nil || rate != 48000 || len(got) != 2 || got[0] != 0.5 || got[1] != -0.5 {
		t.Fatalf("24-bit: %v, %d Hz, %v", err, rate, got)
	}
}
