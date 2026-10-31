package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// startLoopEngine starts a null engine on the test bank with a snare, a hihat,
// saw voices and three patterns, a beat, a melody and a bass line, so a loop the
// engine renders of them has a rip a perfect listener would make
func startLoopEngine(t *testing.T) (*AudioEngine, [MusicSlots]PatternID) {
	t.Helper()
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	cfg := DefaultAudioConfig()
	cfg.Enabled = true
	cfg.ForceBackend = BackendNameNull
	cfg.BaseSounds = append(testBank(),
		&SoundDef{Name: "snare", Duration: 0.18, Norm: 0.8, Chain: []Proc{{Kind: "hp", Freq: 150}}, Layer: []Layer{
			{Gain: 0.7, Source: Source{Kind: "sweep", Wave: "sine", Freq: 200, FreqEnd: 170, Curve: "exp"}, Chain: []Proc{{Kind: "decay", Tau: 0.05}}},
			{Source: Source{Kind: "noise"}, Chain: []Proc{{Kind: "bp", Freq: 2500, Q: 0.6}, {Kind: "decay", Tau: 0.06}}}}},
		&SoundDef{Name: "hihat", Duration: 0.07, Norm: 0.7, Layer: []Layer{
			{Source: Source{Kind: "noise"}, Chain: []Proc{{Kind: "hp", Freq: 7000}, {Kind: "decay", Tau: 0.012}}}}})
	cfg.BaseVoices = []*VoiceDef{
		{Name: "lead", Wave: "saw", Cutoff: 2500, Q: 0.5, Attack: 0.005, Decay: 0.1, Sustain: 0.7, Release: 0.08},
		{Name: "bass", Wave: "saw", Cutoff: 600, Q: 0.7, Attack: 0.005, Decay: 0.1, Sustain: 0.8, Release: 0.06},
	}
	cfg.BasePatterns = []*Pattern{
		{Name: "beat", Steps: 16, Tracks: []Track{
			{Instr: InstrKick, Events: []Step{{Pos: 0, Vel: 1}, {Pos: 4, Vel: 1}, {Pos: 8, Vel: 1}, {Pos: 12, Vel: 1}}},
			{Instr: InstrSnare, Events: []Step{{Pos: 4, Vel: 0.9}, {Pos: 12, Vel: 0.9}}},
			{Instr: InstrHihat, Events: []Step{{Pos: 2, Vel: 0.6}, {Pos: 6, Vel: 0.6}, {Pos: 10, Vel: 0.6}, {Pos: 14, Vel: 0.6}}}}},
		{Name: "tune", Steps: 16, Tracks: []Track{{Instr: InstrLead, Events: []Step{
			{Pos: 0, Vel: 0.9, Deg: 0, Oct: 2, Dur: 2}, {Pos: 2, Vel: 0.8, Deg: 1, Oct: 2, Dur: 2}, {Pos: 4, Vel: 0.8, Deg: 2, Oct: 2, Dur: 2}, {Pos: 6, Vel: 0.8, Deg: 3, Oct: 2, Dur: 2},
			{Pos: 8, Vel: 0.9, Deg: 4, Oct: 2, Dur: 2}, {Pos: 10, Vel: 0.8, Deg: 5, Oct: 2, Dur: 2}, {Pos: 12, Vel: 0.8, Deg: 6, Oct: 2, Dur: 2}, {Pos: 14, Vel: 0.9, Deg: 0, Oct: 3, Dur: 2}}}}},
		{Name: "low", Steps: 16, Tracks: []Track{{Instr: InstrBass, Events: []Step{
			{Pos: 0, Vel: 0.9, Deg: 0, Oct: 0, Dur: 4}, {Pos: 8, Vel: 0.9, Deg: 4, Oct: 0, Dur: 4}}}}},
	}
	ae, err := NewAudioEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(ae.Stop)
	return ae, [MusicSlots]PatternID{PatternIDByName("beat"), PatternIDByName("tune"), PatternSilence, PatternIDByName("low")}
}

// TestRipHearsTheLoop is the rule a rip answers to: played the engine's own music,
// it finds the tempo, the bars, the key, the drums on their steps and most of the
// melody and bass notes on theirs, graded against the rip a perfect listener would
// make of the same patterns, which grades itself perfect.
func TestRipHearsTheLoop(t *testing.T) {
	const bpm, bars = 140, 8
	ae, slots := startLoopEngine(t)
	buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := ExpectedRip(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil)
	if self := CompareRips(want, want); self.Total != 1 {
		t.Fatalf("the truth grades itself %+v", self)
	}
	r, err := RipSamples(buf, AudioSampleRate, DefaultRipParams())
	if err != nil {
		t.Fatal(err)
	}
	if r.Root%12 != DefaultRootNote%12 || r.Scale != ScalePhrygian {
		t.Errorf("key root %d scale %d, want E phrygian", r.Root, r.Scale)
	}
	score := CompareRips(r, want)
	t.Logf("bpm %d bars %d downbeat %.2fs; score %+v", r.BPM, len(r.Bars), r.Downbeat, score)
	if score.Tempo < 1 || score.Bars < 0.85 || score.Drums < 0.9 || score.Melody < 0.8 || score.Bass < 0.8 {
		t.Errorf("score %+v", score)
	}
	if n := len(r.Patterns("loop", 4)); n < 3 {
		t.Errorf("%d patterns written, want a beat, a melody and a bass at least", n)
	}
}

// TestRipParamsAreData is the rule the parameters live by: a [rip] document
// round-trips, a key outside its range or unknown is refused, and the gene vector
// is the parameters again.
func TestRipParamsAreData(t *testing.T) {
	p := DefaultRipParams()
	data, err := MarshalRipParams(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := LoadRipParams(data)
	if err != nil || back != p {
		t.Fatalf("round trip: %v\n%s", err, data)
	}
	if _, err := LoadRipParams([]byte("[rip]\nkick_share = 5\n")); err == nil {
		t.Error("a share of five was accepted")
	}
	if _, err := LoadRipParams([]byte("[rip]\nkick_shares = 0.1\n")); err == nil {
		t.Error("an unknown key was accepted")
	}
	partial, err := LoadRipParams([]byte("[rip]\nharmonics = 4\n"))
	if err != nil || partial.Harmonics != 4 || partial.Jump != p.Jump {
		t.Errorf("a partial document: %v, %+v", err, partial)
	}
	if again := RipParamsFromGenes(p.Genes()); again != p {
		t.Errorf("genes do not round-trip: %+v", again)
	}
	if len(RipGeneRanges()) != len(p.Genes()) {
		t.Error("ranges and genes differ in length")
	}
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
