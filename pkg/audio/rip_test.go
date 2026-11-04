package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"slices"
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
	// on a pinned grid the decoders of tempo and bar line are skipped and the
	// rest reads the same
	pinned, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if ps := CompareRips(pinned, want); pinned.BPM != bpm || pinned.Downbeat != 0 || len(pinned.Bars) != bars || ps.Total < score.Total-0.01 {
		t.Errorf("pinned: bpm %d downbeat %.2f bars %d score %+v", pinned.BPM, pinned.Downbeat, len(pinned.Bars), ps)
	}
	// the tempo alone pinned: the beats tracked at it and the bar line decoded,
	// so the loop reads as it does free; at another tempo it reads on that grid
	tempoed, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm, Downbeat: -1}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: 100, Downbeat: -1}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if ts := CompareRips(tempoed, want); tempoed.BPM != bpm || ts.Total < score.Total-0.05 || other.BPM != 100 || len(other.Bars) < bars*100/bpm-1 || len(other.Bars) > bars*100/bpm+1 {
		t.Errorf("tempo pinned: bpm %d score %+v; at 100: bpm %d bars %d", tempoed.BPM, ts, other.BPM, len(other.Bars))
	}
	if score.Tempo < 1 || score.Bars < 0.85 || score.Drums < 0.9 || score.Melody < 0.8 || score.Bass < 0.8 {
		t.Errorf("score %+v", score)
	}
	if n := len(r.Patterns("loop", 4)); n < 3 {
		t.Errorf("%d patterns written, want a beat, a melody and a bass at least", n)
	}
	if len(r.Notes) < 16 || r.Notes[0].Start > r.Notes[len(r.Notes)-1].Start {
		t.Errorf("%d notes in the transcript, in order %v", len(r.Notes), len(r.Notes) > 1)
	}
	// a timeline that empties every slot after the first bar falls near silent
	// there, and plays on when the third bar fills them again
	timeline := [][MusicSlots]PatternID{slots, {}, slots}
	tl, err := ae.RenderTimeline(timeline, bpm, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	n := SamplesPerBar(bpm)
	full, empty, again := energy(tl[:n]), energy(tl[n:2*n]), energy(tl[2*n:])
	if empty > full/10 || again < full/2 {
		t.Errorf("timeline bars carry %g, %g and %g", full, empty, again)
	}
}

func energy(x []float64) float64 {
	var e float64
	for _, v := range x {
		e += v * v
	}
	return e / float64(max(len(x), 1))
}

// TestRipNoteBrightIsTheFundamentalsShare: the loop's melody played on a sine
// reads near one and brighter than on a saw, whose partials share the sum. Its
// root and fifth sound two octaves over the bass's, the bass's fourth partial
// covering their fundamentals, and are unmeasured rather than read through it;
// every other melody-register note is measured, and no bass note.
func TestRipNoteBrightIsTheFundamentalsShare(t *testing.T) {
	const bpm, bars = 140, 4
	ae, slots := startLoopEngine(t)
	covered := map[int]bool{DefaultRootNote + 24: true, StepPitch(DefaultRootNote, ScalePhrygian, 4, 0, 0) + 24: true}
	bright := func(wave string) float64 {
		if _, err := ae.DefineVoice(&VoiceDef{Name: "lead", Wave: wave, Cutoff: 2500, Q: 0.5, Attack: 0.005, Decay: 0.1, Sustain: 0.7, Release: 0.08}); err != nil {
			t.Fatal(err)
		}
		buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
		if err != nil {
			t.Fatal(err)
		}
		var sum float64
		var measured int
		for _, n := range r.Notes {
			unmeasured := n.Bright == RipUnmeasured
			if unmeasured != (n.Bass || covered[n.MIDI]) {
				t.Errorf("%s: MIDI %d at %.2fs, bass %v, reads %.2f", wave, n.MIDI, n.Start, n.Bass, n.Bright)
			}
			if !unmeasured {
				sum, measured = sum+n.Bright, measured+1
			}
		}
		if measured == 0 {
			t.Fatalf("%s: no note measured", wave)
		}
		return sum / float64(measured)
	}
	saw, sine := bright("saw"), bright("sine")
	t.Logf("mean bright: saw %.2f, sine %.2f", saw, sine)
	if sine < 0.9 || saw > sine-0.2 {
		t.Errorf("saw %.2f, sine %.2f", saw, sine)
	}
}

// TestNoteRenderAndDistances: a rendered note sounds and dies, mel frames of the
// same note are no distance apart and of another voice some, and bars are as far
// apart as the steps they do not share.
func TestNoteRenderAndDistances(t *testing.T) {
	saw := &VoiceDef{Name: "a", Wave: "saw", Cutoff: 2000, Q: 0.5, Attack: 0.005, Decay: 0.1, Sustain: 0.7, Release: 0.1}
	sine := &VoiceDef{Name: "b", Wave: "sine", Attack: 0.005, Decay: 0.1, Sustain: 0.7, Release: 0.1}
	note := RenderNote(saw, 60, 0.8, 0.3, 0.5)
	if len(note) < int(0.3*AudioSampleRate) || len(note) > int(0.8*AudioSampleRate) || energy(note[:1000]) == 0 {
		t.Fatalf("note of %d samples, head energy %g", len(note), energy(note[:1000]))
	}
	if tail := note[len(note)-100:]; energy(tail) > energy(note[:4410])/100 {
		t.Errorf("the note did not die away: %g vs %g", energy(tail), energy(note[:4410]))
	}
	a := MelFrames(note, AudioSampleRate)
	b := MelFrames(RenderNote(sine, 60, 0.8, 0.3, 0.5), AudioSampleRate)
	if MelDistance(a, a) != 0 || MelDistance(a, b) <= 0 {
		t.Errorf("distances %g and %g", MelDistance(a, a), MelDistance(a, b))
	}
	x := RipBar{Melody: []Step{{Pos: 0, Deg: 0, Oct: 2}, {Pos: 4, Deg: 2, Oct: 2}}}
	x.Drums[InstrKick] = []Step{{Pos: 0}, {Pos: 8}}
	y := x
	y.Melody = []Step{{Pos: 0, Deg: 0, Oct: 2}, {Pos: 4, Deg: 3, Oct: 2}}
	if d := BarDistance(&x, &x); d != 0 {
		t.Errorf("a bar is %g from itself", d)
	}
	if d := BarDistance(&x, &y); math.Abs(d-0.4) > 1e-9 {
		t.Errorf("one note of four changed is %g apart, want 0.4", d)
	}
	if d := BarDistance(&x, &RipBar{}); d != 1 {
		t.Errorf("a bar is %g from an empty one", d)
	}
}

// TestRipHearsASecondVoice: a sustained line under the melody, in its register,
// is heard by the second pass and lost without it, and the first pass's own
// notes are not traded for it.
func TestRipHearsASecondVoice(t *testing.T) {
	const bpm, bars = 140, 8
	ae, slots := startLoopEngine(t)
	pad, err := ae.DefinePattern(&Pattern{Name: "pad", Steps: 16, Role: RoleFill, Tracks: []Track{{Instr: InstrLead, Events: []Step{
		{Pos: 0, Vel: 0.7, Deg: 2, Oct: 1, Dur: 8}, {Pos: 8, Vel: 0.7, Deg: 4, Oct: 1, Dur: 8}}}}})
	if err != nil {
		t.Fatal(err)
	}
	slots[2] = pad
	buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := ExpectedRip(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil)
	// on the render's own grid: the pad's change on the third beat moves the
	// bar-line decoder, which is not the rule under test
	one := DefaultRipParams()
	one.SecondVoice = 0
	solo, err := Ripper{Params: one, Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	both, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	a, b := CompareRips(solo, want), CompareRips(both, want)
	// the pad's notes found: its two a bar, by step and absolute pitch, since
	// the pad moves the key the rip reads its degrees in
	padNotes := func(r *Rip) int {
		n := 0
		for _, bar := range r.Bars {
			for _, st := range append(slices.Clone(bar.Melody), bar.Under...) {
				midi := StepPitch(r.Root, r.Scale, st.Deg, st.Oct, st.Acc)
				if st.Pos == 0 && midi == DefaultRootNote+15 || st.Pos == 8 && midi == DefaultRootNote+19 {
					n++
				}
			}
		}
		return n
	}
	t.Logf("one voice %+v, %d pad notes; two voices %+v, %d pad notes of %d", a, padNotes(solo), b, padNotes(both), 2*bars)
	// the second line's fragments may cost the melody a little, not more
	if padNotes(both) <= padNotes(solo) || b.Melody < a.Melody-0.05 {
		t.Errorf("the second pass found %d pad notes against %d, melody %.2f against %.2f", padNotes(both), padNotes(solo), b.Melody, a.Melody)
	}
	// the second line is a track of its own, through the lead until a voice is fitted
	under := 0
	for _, bar := range both.Bars {
		under += len(bar.Under)
	}
	pats := both.Patterns("x", 8)
	i := slices.IndexFunc(pats, func(p *PatternDef) bool { return p.Name == "x_melody_1" })
	if under == 0 || i < 0 || len(pats[i].Track) != 2 || pats[i].Track[1].Instr != "lead" {
		t.Fatalf("%d under notes; melody pattern tracks %v", under, pats)
	}
	both.UnderVoice = &VoiceDef{Name: "u", Wave: "sine"}
	if vs := both.Voices("x"); len(vs) != 1 || vs[0].Name != "x_under_voice" || both.Patterns("x", 8)[i].Track[1].Instr != "x_under_voice" {
		t.Errorf("the under voice names %v", vs)
	}
}

// twoLines renders two lines in the melody register on one voice, the lead's
// events in the melody slot and the counter's in the free slot, and rips the
// render with the lead rule given; it returns the rip and the two pitch sets,
// which are disjoint, so a step tells its line by pitch.
func twoLines(t *testing.T, leadEvs, counterEvs []Step, lead LeadRule) (*Rip, map[int]bool, map[int]bool) {
	t.Helper()
	const bpm, bars = 120, 8
	ae, slots := startLoopEngine(t)
	l, err := ae.DefinePattern(&Pattern{Name: "lead", Steps: 32, Role: RoleMelody, Tracks: []Track{{Instr: InstrLead, Events: leadEvs}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ae.DefinePattern(&Pattern{Name: "counter", Steps: 32, Role: RoleFill, Tracks: []Track{{Instr: InstrLead, Events: counterEvs}}})
	if err != nil {
		t.Fatal(err)
	}
	slots[1], slots[2] = l, c
	buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}, Lead: lead}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	pitches := func(evs []Step) map[int]bool {
		m := map[int]bool{}
		for _, e := range evs {
			m[StepPitch(DefaultRootNote, ScalePhrygian, e.Deg, e.Oct, 0)] = true
		}
		return m
	}
	return r, pitches(leadEvs), pitches(counterEvs)
}

// linesHeard counts the lead's and the counter's pitches in the melody line
// and in the under line of a rip
func linesHeard(r *Rip, leadSet, counterSet map[int]bool) (leadIn, counterIn [2]int) {
	for _, bar := range r.Bars {
		for i, line := range [2][]Step{bar.Melody, bar.Under} {
			for _, st := range line {
				switch m := StepPitch(r.Root, r.Scale, st.Deg, st.Oct, st.Acc); {
				case leadSet[m]:
					leadIn[i]++
				case counterSet[m]:
					counterIn[i]++
				}
			}
		}
	}
	return
}

// TestRipKeepsTheLeadThroughACrossing: two lines in the melody register on one
// voice, a louder lead in quarter notes and a quieter counter line in eighths,
// a fourth under the lead one bar and a fifth over it the next; told the lead
// is the louder, the rip keeps the lead's notes in the melody line and the
// counter's in the under line through the crossing, where a line told by pitch
// order or by the louder frame changes hands.
func TestRipKeepsTheLeadThroughACrossing(t *testing.T) {
	const bars = 8
	var leadEvs, counterEvs []Step
	for i, d := range [...]int{4, 5, 6, 5, 4, 5, 6, 5} {
		leadEvs = append(leadEvs, Step{Pos: 4 * i, Vel: 1, Deg: d, Oct: 2, Dur: 4})
	}
	for i, d := range [...]int{1, 2, 3, 2, 1, 2, 3, 2, 1, 2, 3, 2, 1, 2, 3, 2} {
		counterEvs = append(counterEvs, Step{Pos: 2 * i, Vel: 0.65, Deg: d, Oct: 2 + i/8, Dur: 2})
	}
	r, leadSet, counterSet := twoLines(t, leadEvs, counterEvs, LeadLoud)
	leadIn, counterIn := linesHeard(r, leadSet, counterSet)
	t.Logf("melody line: %d lead notes, %d counter notes; under line: %d lead, %d counter; %d lead and %d counter notes played", leadIn[0], counterIn[0], leadIn[1], counterIn[1], 4*bars, 8*bars)
	if leadIn[0] < 3*bars || counterIn[0] > bars || counterIn[1] < 3*bars {
		t.Errorf("the lead did not hold its line: melody %d lead %d counter, under %d lead %d counter", leadIn[0], counterIn[0], leadIn[1], counterIn[1])
	}
}

// TestRipTellsTheLeadByItsCharacter: a softer lead in eighths under a louder
// counter line in half notes, a fourth under the lead one bar and a fifth over
// it the next, on one voice. The decoder follows the louder line, which is the
// counter here, so the notes are parted into lines by what each keeps, its
// pitch, its lengths and its strength, and the busier line is the melody.
func TestRipTellsTheLeadByItsCharacter(t *testing.T) {
	const bars = 8
	var leadEvs, counterEvs []Step
	for i, d := range [...]int{4, 5, 6, 5, 4, 5, 6, 5, 4, 5, 6, 5, 4, 5, 6, 5} {
		leadEvs = append(leadEvs, Step{Pos: 2 * i, Vel: 0.6, Deg: d, Oct: 2, Dur: 2})
	}
	for i, d := range [...]int{1, 2, 1, 2} {
		counterEvs = append(counterEvs, Step{Pos: 8 * i, Vel: 1, Deg: d, Oct: 2 + i/2, Dur: 8})
	}
	r, leadSet, counterSet := twoLines(t, leadEvs, counterEvs, LeadBusy)
	leadIn, counterIn := linesHeard(r, leadSet, counterSet)
	t.Logf("melody line: %d lead notes, %d counter notes; under line: %d lead, %d counter; %d lead and %d counter notes played", leadIn[0], counterIn[0], leadIn[1], counterIn[1], 8*bars, 2*bars)
	if leadIn[0] < 6*bars || counterIn[0] > bars || counterIn[1] < bars {
		t.Errorf("the softer lead did not hold its line: melody %d lead %d counter, under %d lead %d counter", leadIn[0], counterIn[0], leadIn[1], counterIn[1])
	}
}

// TestRipKeepsABassLineThatClimbs: a bass line on F3 and G3, above the E3 the
// bass register's rows end at, stays the bass line, heard by the bass decoder
// where it reaches over the melody's rows; and a melody that dips to G3 over a
// low bass keeps its note, since the note continues the melody, not the bass.
func TestRipKeepsABassLineThatClimbs(t *testing.T) {
	const bpm, bars = 120, 4
	f3, g3 := StepPitch(DefaultRootNote, ScalePhrygian, 1, 1, 0), StepPitch(DefaultRootNote, ScalePhrygian, 2, 1, 0)
	count := func(r *Rip) (inBass, inMelody int) {
		for _, bar := range r.Bars {
			for _, st := range bar.Bass {
				if m := StepPitch(r.Root, r.Scale, st.Deg, st.Oct, st.Acc); m == f3 || m == g3 {
					inBass++
				}
			}
			for _, st := range append(slices.Clone(bar.Melody), bar.Under...) {
				if m := StepPitch(r.Root, r.Scale, st.Deg, st.Oct, st.Acc); m == f3 || m == g3 {
					inMelody++
				}
			}
		}
		return
	}
	ae, slots := startLoopEngine(t)
	high, err := ae.DefinePattern(&Pattern{Name: "high", Steps: 16, Role: RoleBass, Tracks: []Track{{Instr: InstrBass, Events: []Step{
		{Pos: 0, Vel: 0.9, Deg: 1, Oct: 1, Dur: 4}, {Pos: 8, Vel: 0.9, Deg: 2, Oct: 1, Dur: 4}}}}})
	if err != nil {
		t.Fatal(err)
	}
	// a melody that dips: the tune's run with its third note a G3, over the low bass
	dip, err := ae.DefinePattern(&Pattern{Name: "dip", Steps: 16, Role: RoleMelody, Tracks: []Track{{Instr: InstrLead, Events: []Step{
		{Pos: 0, Vel: 0.9, Deg: 4, Oct: 1, Dur: 2}, {Pos: 2, Vel: 0.8, Deg: 3, Oct: 1, Dur: 2}, {Pos: 4, Vel: 0.9, Deg: 2, Oct: 1, Dur: 2}, {Pos: 6, Vel: 0.8, Deg: 3, Oct: 1, Dur: 2},
		{Pos: 8, Vel: 0.9, Deg: 4, Oct: 1, Dur: 2}, {Pos: 10, Vel: 0.8, Deg: 5, Oct: 1, Dur: 2}, {Pos: 12, Vel: 0.8, Deg: 6, Oct: 1, Dur: 2}, {Pos: 14, Vel: 0.9, Deg: 0, Oct: 2, Dur: 2}}}}})
	if err != nil {
		t.Fatal(err)
	}
	climb := slots
	climb[3] = high
	buf, err := ae.RenderMusic(climb, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if inBass, inMelody := count(r); inBass < bars || inMelody > 1 {
		t.Errorf("the climbing bass line: %d notes in the bass line, %d in the melody register, of %d played", inBass, inMelody, 2*bars)
	}
	dipped := slots
	dipped[1] = dip
	buf, err = ae.RenderMusic(dipped, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	if inBass, inMelody := count(r); inMelody < bars/2 || inBass > 1 {
		t.Errorf("the dipping melody: %d G3 notes in the melody register, %d in the bass line, of %d played", inMelody, inBass, bars)
	}
}

// TestAReleaseEndsANoteStillSounding: a front end that hears each pitch let go
// ends a note there though a pedal keeps it sounding, the note as hard as the front
// end heard it struck; while it rings on, only a strike over StrikeAgain starts another.
func TestAReleaseEndsANoteStillSounding(t *testing.T) {
	const frames = 200
	f := &RipFrames{Hop: RipFrameSec, Flux: make([]float64, frames), Salience: make([]float32, frames*RipPitches),
		Strike: make([]float32, frames*RipPitches), Release: make([]float32, frames*RipPitches), Velocity: make([]float32, frames*RipPitches)}
	at := func(t, midi int) int { return t*RipPitches + midi - RipMinMIDI }
	for t := 10; t < frames; t++ {
		f.Salience[at(t, 72)] = 0.9
	}
	f.Strike[at(10, 72)], f.Velocity[at(10, 72)], f.Release[at(120, 72)] = 0.99, 0.4, 0.95
	f.Strike[at(150, 72)], f.Strike[at(170, 72)] = 0.9, 0.99
	got := f.notes(RipMelodyLow, RipMaxMIDI, DefaultRipParams())
	if len(got) != 2 || got[0].start != 10 || got[0].end != 120 || got[0].vel != float64(float32(0.4)) || got[1].start != 170 {
		t.Errorf("notes %+v, want one from 10 to 120 at velocity 0.4 and one from 170", got)
	}
}

// TestStrikesStartNotesWhileOthersRing: under a pedal, three pitches struck in turn
// ring on together and one struck again while it rings is two notes. A strike whose
// pitch never sounds, a weaker one an octave over a note struck, one again within
// StrikeGap, and one of a ringing pitch under StrikeAgain or under a neighbour's are
// none; a bass strike two octaves under hides no melody note.
func TestStrikesStartNotesWhileOthersRing(t *testing.T) {
	const frames = 300
	f := &RipFrames{Hop: RipFrameSec, Flux: make([]float64, frames), Salience: make([]float32, frames*RipPitches), Strike: make([]float32, frames*RipPitches)}
	for _, n := range []struct {
		midi, at, end int
		strike        float32
	}{{72, 0, 250, 0.99}, {76, 40, 250, 0.99}, {79, 80, 250, 0.99}, {72, 150, 250, 0.99}, {84, 0, 100, 0.95}, {67, 200, 200, 0.99}, {76, 45, 45, 0.99},
		{74, 260, 266, 0.99}, {74, 268, 290, 0.99}, {79, 120, 120, 0.9}, {45, 220, 220, 0.99}, {69, 220, 240, 0.97},
		{79, 180, 180, 0.97}, {80, 180, 200, 0.99}} {
		f.Strike[n.at*RipPitches+n.midi-RipMinMIDI] = n.strike
		for t := n.at; t < n.end; t++ {
			f.Salience[t*RipPitches+n.midi-RipMinMIDI] = 0.9
		}
	}
	var got [][3]int
	for _, n := range f.notes(RipMelodyLow, RipMaxMIDI, DefaultRipParams()) {
		got = append(got, [3]int{n.midi, n.start, n.end})
	}
	want := [][3]int{{72, 0, 150}, {76, 40, 250}, {79, 80, 250}, {72, 150, 250}, {80, 180, 200}, {69, 220, 240}, {74, 260, 266}}
	if !slices.Equal(got, want) {
		t.Errorf("notes (midi, start, end) %v, want %v", got, want)
	}
}

// TestMatchNotesAlignsTwoTranscripts: a transcript against itself transposed,
// stretched, one note dropped and one added matches every note but those,
// finds the transposition and the time map, and parses back from the text
// form a listener corrects.
func TestMatchNotesAlignsTwoTranscripts(t *testing.T) {
	var a []RipNote
	for i, m := range [...]int{67, 69, 69, 74, 75, 70, 72, 75, 74, 81, 74, 67} {
		a = append(a, RipNote{Start: 0.3 * float64(i), End: 0.3*float64(i) + 0.2, MIDI: m, Vel: 0.7, Second: i%4 == 3})
	}
	for i, m := range [...]int{43, 41, 43, 46} {
		a = append(a, RipNote{Start: 1.2 * float64(i), End: 1.2*float64(i) + 1, MIDI: m, Vel: 0.9, Bass: true})
	}
	var b []RipNote
	for i, n := range a {
		if i == 5 { // one note the second witness lacks
			continue
		}
		n.Start, n.End, n.MIDI = 1.15*n.Start+0.4, 1.15*n.End+0.4, n.MIDI+3
		b = append(b, n)
	}
	b = append(b, RipNote{Start: 2.05, End: 2.2, MIDI: 72, Vel: 0.5}) // and one it alone hears
	ag := MatchNotes(a, b)
	m, u, bass := ag.Lines[0], ag.Lines[1], ag.Lines[2]
	if ag.Shift != -3 || m.Matched != 8 || len(m.OnlyA) != 1 || len(m.OnlyB) != 1 || u.Matched != 3 || bass.Matched != 4 || ag.Register.Matched != 11 {
		t.Errorf("shift %d, melody %d matched of %d and %d (only a %d, only b %d), under %d, bass %d, register %d", ag.Shift, m.Matched, m.A, m.B, len(m.OnlyA), len(m.OnlyB), u.Matched, bass.Matched, ag.Register.Matched)
	}
	if math.Abs(ag.Stretch-1/1.15) > 0.02 || math.Abs(ag.Offset+0.4/1.15) > 0.05 {
		t.Errorf("time map a = %.3f b %+.3f, want %.3f and %+.3f", ag.Stretch, ag.Offset, 1/1.15, -0.4/1.15)
	}
	text := "x: 16 note(s) at 120 bpm in G2 minor\nmelody   0.300s bar 1 step  3        A4   degree 1 octave 2, 1.6 step(s) at 0.70, support 1.00\nunder 0.9s F5 0.25s\nbass 0s G2 8 steps\n"
	notes, err := ParseTranscript(text, 0, 0)
	if err != nil || len(notes) != 3 || notes[0].MIDI != 69 || math.Abs(notes[0].End-0.5) > 0.01 || !notes[1].Second || notes[1].MIDI != 77 || !notes[2].Bass || math.Abs(notes[2].End-1) > 0.01 {
		t.Errorf("parsed %+v, %v", notes, err)
	}
	if _, err := ParseTranscript("melody 0.1s 3 steps\n", 0, 0); err == nil {
		t.Error("a line without a pitch parsed")
	}
	// a rhythm tapped out: eighths at 120 bpm from one second, a group twice
	tab, err := ParseTranscript("unit eighth\nstart 1s\nmelody (1-2)x2 3.4\nunder 11\n", 120, 0)
	if err != nil || len(tab) != 8 || tab[0].MIDI != -1 || math.Abs(tab[0].Start-1) > 1e-9 || math.Abs(tab[0].End-1.5) > 1e-9 || math.Abs(tab[1].Start-1.5) > 1e-9 || math.Abs(tab[4].Start-2.5) > 1e-9 || math.Abs(tab[5].Start-3) > 1e-9 || !tab[6].Second || math.Abs(tab[7].Start-1.25) > 1e-9 {
		t.Errorf("tab parsed %+v, %v", tab, err)
	}
	// heard against it by onset: every note found but one, and one cut in two
	var heard []RipNote
	for i, n := range tab[:5] {
		if i == 3 {
			continue
		}
		heard = append(heard, RipNote{Start: n.Start + 0.02, End: n.End, MIDI: 60 + i})
		if i == 0 {
			heard = append(heard, RipNote{Start: n.Start + 0.3, End: n.End, MIDI: 60})
		}
	}
	rhythm := MatchNotes(heard, tab[:5])
	if l := rhythm.Lines[0]; !l.Rhythm || l.Matched != 4 || len(l.OnlyB) != 1 || l.Split != 1 || len(l.OnlyA) != 1 {
		t.Errorf("rhythm: matched %d of %d given, only given %d, only heard %d, split %d", l.Matched, l.B, len(l.OnlyB), len(l.OnlyA), l.Split)
	}
	if _, err := ParseTranscript("melody (1-2\n", 0, 0); err == nil {
		t.Error("a rhythm without a unit parsed")
	}
}

// TestMatchHitsPlacesARipsDrumsOnTheGivenOnes: a rip's drum steps, placed on its
// beats at 117 bpm, match a beat given at 120 bpm two seconds later under the
// map the search finds, drum by drum within half a step: a kick the rip missed
// is only given, and a snare it added only the rip's.
func TestMatchHitsPlacesARipsDrumsOnTheGivenOnes(t *testing.T) {
	const bars, beat = 8, 60.0 / 117
	r := &Rip{BPM: 117, Bars: make([]RipBar, bars)}
	for i := range BeatsPerBar*bars + 1 {
		r.Beats = append(r.Beats, 0.3+float64(i)*beat)
	}
	var given []RipHit
	for b := range bars {
		for s := 0; s < StepsPerBar; s += 2 {
			drums := []InstrumentType{InstrHihat}
			if s%8 == 0 {
				drums = append(drums, InstrKick)
			} else if s%4 == 0 {
				drums = append(drums, InstrSnare)
			}
			for _, d := range drums {
				given = append(given, RipHit{Start: 2 + float64(b*StepsPerBar+s)*0.125, Drum: d})
				if d != InstrKick || b != 3 || s != 8 {
					r.Bars[b].Drums[d] = append(r.Bars[b].Drums[d], Step{Pos: s})
				}
			}
		}
	}
	r.Bars[5].Drums[InstrSnare] = append(r.Bars[5].Drums[InstrSnare], Step{Pos: 6})
	ag := MatchHits(r.Hits(), given, beat/StepsPerBeat/2)
	want := [InstrClap][3]int{InstrKick: {15, 0, 1}, InstrSnare: {16, 1, 0}, InstrHihat: {64, 0, 0}}
	for d, l := range ag.Drums {
		if got := [3]int{l.Matched, len(l.OnlyA), len(l.OnlyB)}; got != want[d] {
			t.Errorf("drum %d: matched, only the rip's, only given %v, want %v", d, got, want[d])
		}
	}
	if math.Abs(ag.Stretch-beat/0.5) > 0.005 || math.Abs(ag.Offset-(0.3-2*beat/0.5)) > 0.05 {
		t.Errorf("map %.3f × given %+.3f, want %.3f × given %+.3f", ag.Stretch, ag.Offset, beat/0.5, 0.3-2*beat/0.5)
	}
}

// TestNoteLevelsReadARecordingsLoudness: of two notes a recording plays 20 dB
// apart, under a map that places the given seconds on it, the softer reads
// 20 dB lower, and a note it does not play, two octaves up, reads far under both.
func TestNoteLevelsReadARecordingsLoudness(t *testing.T) {
	const rate = 22050
	x := make([]float64, 3*rate)
	for i := range rate / 2 {
		ph := 2 * math.Pi * NoteFreq(69) * float64(i) / rate
		x[rate/2+i], x[2*rate+i] = 0.5*math.Sin(ph), 0.05*math.Sin(ph)
	}
	notes := []RipNote{{Start: 0, MIDI: 69}, {Start: 1.5, MIDI: 69}, {Start: 1.5, MIDI: 93}}
	lv := NoteLevels(x, rate, notes, func(s float64) float64 { return s + 0.5 }, 0)
	if d := lv[0] - lv[1]; math.Abs(d-20) > 1 {
		t.Errorf("the softer note reads %.1f dB under the louder, want 20", d)
	}
	if lv[2] > lv[1]-40 {
		t.Errorf("A6 over a recording of A4 reads %.1f dB against A4's %.1f", lv[2], lv[1])
	}
}

// TestMIDIRoundTrip: a transcript written as a MIDI file reads back note for
// note and hit for hit within half a tick, each line from its named track and
// each drum from the drum channel, a note that ends as the next of its pitch
// starts kept apart from it, and every note unmeasured.
func TestMIDIRoundTrip(t *testing.T) {
	in := []RipNote{
		{Start: 0, End: 1.2, MIDI: 40, Vel: 1, Bass: true},
		{Start: 0.1, End: 0.4, MIDI: 72, Vel: 0.8, Bright: 0.9},
		{Start: 0.4, End: 0.73, MIDI: 72, Vel: 0.5},
		{Start: 0.5, End: 1.1, MIDI: 67, Vel: 0.6, Second: true},
		{Start: 0.77, End: 1.234, MIDI: 76, Vel: 0.3},
	}
	hits := []RipHit{{Start: 0, Drum: InstrKick, Vel: 1}, {Start: 0.22, Drum: InstrHihat, Vel: 0.5}, {Start: 0.44, Drum: InstrSnare, Vel: 0.8}}
	var file bytes.Buffer
	if err := WriteMIDI(&file, in, hits, 137); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMIDI(&file)
	if err != nil {
		t.Fatal(err)
	}
	out, outHits := m.Notes, m.Hits
	if len(out) != len(in) || len(outHits) != len(hits) {
		t.Fatalf("read %d note(s) of %d, %d hit(s) of %d", len(out), len(in), len(outHits), len(hits))
	}
	half := 0.5 / (137.0 / 60 * midiDivision)
	for i, n := range out {
		w := in[i]
		if n.MIDI != w.MIDI || n.Bass != w.Bass || n.Second != w.Second || n.Bright != RipUnmeasured ||
			math.Abs(n.Start-w.Start) > half || math.Abs(n.End-w.End) > half || math.Abs(n.Vel-w.Vel) > 0.5/127 {
			t.Errorf("note %d: read %+v, wrote %+v", i, n, w)
		}
	}
	for i, h := range outHits {
		if w := hits[i]; h.Drum != w.Drum || math.Abs(h.Start-w.Start) > half || math.Abs(h.Vel-w.Vel) > 0.5/127 {
			t.Errorf("hit %d: read %+v, wrote %+v", i, h, w)
		}
	}
}

// smf is a type-1 file's header for tracks tracks, 480 ticks a quarter
func smf(tracks byte) []byte {
	return []byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 1, 0, tracks, 0x01, 0xE0}
}

// track is a track chunk of the events given
func track(events ...byte) []byte {
	return append(binary.BigEndian.AppendUint32([]byte("MTrk"), uint32(len(events))), events...)
}

// TestReadMIDITempoMap: a type-1 file whose first track halves the tempo two
// beats in reads in seconds under that map; of its unnamed tracks, the one
// centred under E3 is the bass and the other the melody, and the drum channel's
// key 36 is a kick. Its notes use running status and a note-on at velocity 0.
func TestReadMIDITempoMap(t *testing.T) {
	file := smf(4)
	file = append(file, track(
		0x00, 0xFF, 0x51, 3, 0x07, 0xA1, 0x20, // 120 bpm
		0x87, 0x40, 0xFF, 0x51, 3, 0x0F, 0x42, 0x40, // 960 ticks on, 60 bpm
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0xFF, 0x03, 4, 'L', 'e', 'a', 'd',
		0x00, 0x90, 72, 100, // C5 at 0s
		0x83, 0x60, 72, 0, // its end at 480, 0.5s
		0x00, 76, 90, // E5 at 0.5s
		0x87, 0x40, 0x80, 76, 0, // its end at 1440, 2s
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x87, 0x40, 0x91, 36, 80, // C2 at 960, 1s
		0x87, 0x40, 0x81, 36, 0, // its end at 1920, 3s
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0x99, 36, 100,
		0x83, 0x60, 0x89, 36, 0,
		0x00, 0xFF, 0x2F, 0)...)
	m, err := ReadMIDI(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	notes, hits := m.Notes, m.Hits
	if len(hits) != 1 || hits[0].Drum != InstrKick || hits[0].Start != 0 {
		t.Errorf("drums read %+v, want a kick at 0s", hits)
	}
	want := []RipNote{
		{Start: 0, End: 0.5, MIDI: 72},
		{Start: 0.5, End: 2, MIDI: 76},
		{Start: 1, End: 3, MIDI: 36, Bass: true},
	}
	if len(notes) != len(want) {
		t.Fatalf("read %+v", notes)
	}
	for i, n := range notes {
		w := want[i]
		if n.MIDI != w.MIDI || n.Bass != w.Bass || n.Second || math.Abs(n.Start-w.Start) > 1e-9 || math.Abs(n.End-w.End) > 1e-9 {
			t.Errorf("note %d: %+v, want %+v", i, n, w)
		}
	}
}

// TestMIDIScoreRipsAsTheLoop: the events a rendered loop plays, written as a
// MIDI file and read back as a rip, give the loop's own patterns, in its key,
// at its tempo, its first bar from the file's start.
func TestMIDIScoreRipsAsTheLoop(t *testing.T) {
	const bpm, bars = 140, 8
	_, slots := startLoopEngine(t)
	step := 60.0 / bpm / StepsPerBeat
	var notes []RipNote
	var hits []RipHit
	for _, e := range ExpectedEvents(slots, bars, DefaultRootNote, ScalePhrygian, nil) {
		at := float64(e.Bar*StepsPerBar+e.Step) * step
		if e.Instr.IsDrum() {
			hits = append(hits, RipHit{Start: at, Drum: e.Instr, Vel: e.Vel})
			continue
		}
		notes = append(notes, RipNote{Start: at, End: at + float64(e.Dur)*step, MIDI: e.MIDI, Vel: e.Vel, Bass: e.MIDI < RipBassTop})
	}
	var file bytes.Buffer
	if err := WriteMIDI(&file, notes, hits, bpm); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMIDI(&file)
	if err != nil {
		t.Fatal(err)
	}
	r, dropped := m.Rip()
	if r.BPM != bpm || r.Downbeat != 0 || len(r.Bars) != bars || r.Root != DefaultRootNote || r.Scale != ScalePhrygian || dropped != 0 {
		t.Fatalf("%d bpm from %.2fs, %d bars, root %d scale %d, %d dropped", r.BPM, r.Downbeat, len(r.Bars), r.Root, r.Scale, dropped)
	}
	// a velocity travels as one of 127 steps
	rounded := func(pats []*PatternDef) []*PatternDef {
		for _, p := range pats {
			for _, tr := range p.Track {
				for i := range tr.Event {
					tr.Event[i].Vel = math.Round(tr.Event[i].Vel*127) / 127
				}
			}
		}
		return pats
	}
	want := ExpectedRip(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil)
	if got, want := rounded(r.Patterns("loop", 4)), rounded(want.Patterns("loop", 4)); !reflect.DeepEqual(got, want) {
		t.Errorf("patterns\n%+v\nwant\n%+v", got, want)
	}
}

// TestMIDIScoreKeepsItsFirstTempo: a score that starts at 60 bpm and speeds to
// 120 two quarters in rips at its first tempo, an octave up into the
// sequencer's range, so a beat is an eighth; its beats and its notes follow its
// ticks under the tempo map, the change counted.
func TestMIDIScoreKeepsItsFirstTempo(t *testing.T) {
	file := append(smf(2), track(
		0x00, 0xFF, 0x51, 3, 0x0F, 0x42, 0x40, // 60 bpm
		0x87, 0x40, 0xFF, 0x51, 3, 0x07, 0xA1, 0x20, // 960 ticks on, 120 bpm
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0x90, 72, 100, // C5 for a quarter, 1s
		0x83, 0x60, 0x80, 72, 0,
		0x87, 0x40, 0x90, 74, 100, // D5 at 1440, 2.5s, for a quarter, 0.5s
		0x83, 0x60, 0x80, 74, 0,
		0x00, 0xFF, 0x2F, 0)...)
	m, err := ReadMIDI(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := m.Rip()
	if m.BPM != 60 || m.Changes != 1 || r.BPM != 120 || len(r.Bars) != 2 {
		t.Fatalf("file at %.1f bpm with %d change(s); rip at %d bpm, %d bar(s)", m.BPM, m.Changes, r.BPM, len(r.Bars))
	}
	for i, want := range []float64{0, 0.5, 1, 1.5, 2, 2.25, 2.5, 2.75, 3} {
		if math.Abs(r.Beats[i]-want) > 1e-6 {
			t.Errorf("beat %d at %.3fs, want %.3fs", i, r.Beats[i], want)
		}
	}
	// a crotchet is two beats before the change and after it
	if mel := r.Bars[1].Melody; len(mel) != 1 || mel[0].Pos != 8 || mel[0].Dur != 8 || r.Bars[0].Melody[0].Dur != 8 {
		t.Errorf("melody %+v then %+v", r.Bars[0].Melody, mel)
	}
}

// TestMIDIScoreBarsFromItsTimeSignature: a file that names no time signature is
// in 4/4 from its start; one whose first bar is shorter than its signature's,
// a pickup, rips with its first bar line where the full bars begin and a bar
// before it holding the pickup.
func TestMIDIScoreBarsFromItsTimeSignature(t *testing.T) {
	notes := []byte{
		0x00, 0x90, 72, 100, // C5 at 0 for a quarter
		0x83, 0x60, 0x80, 72, 0,
		0x00, 0x90, 74, 100, // D5 at 480 for a whole bar
		0x8F, 0x00, 0x80, 74, 0,
		0x00, 0xFF, 0x2F, 0}
	plain, err := ReadMIDI(bytes.NewReader(append(smf(1), track(notes...)...)))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := plain.Rip()
	if plain.Meter != [2]int{4, 4} || r.Downbeat != 0 || len(r.Bars) != 2 || r.Bars[0].Melody[1].Pos != 4 {
		t.Errorf("no signature: %v, first bar from %.2fs, %d bar(s), melody %+v", plain.Meter, r.Downbeat, len(r.Bars), r.Bars[0].Melody)
	}
	meters := []byte{
		0x00, 0xFF, 0x58, 4, 1, 2, 24, 8, // 1/4 at 0
		0x83, 0x60, 0xFF, 0x58, 4, 3, 2, 24, 8, // 3/4 at 480
		0x00, 0xFF, 0x2F, 0}
	picked, err := ReadMIDI(bytes.NewReader(append(append(smf(2), track(meters...)...), track(notes...)...)))
	if err != nil {
		t.Fatal(err)
	}
	r, _ = picked.Rip()
	if picked.Meter != [2]int{3, 4} || math.Abs(r.Downbeat+1.5) > 1e-6 || len(r.Bars) != 2 {
		t.Fatalf("a pickup: %v, first bar from %.2fs, %d bar(s)", picked.Meter, r.Downbeat, len(r.Bars))
	}
	if mel := r.Bars[0].Melody; len(mel) != 1 || mel[0].Pos != 12 || len(r.Bars[1].Melody) != 1 || r.Bars[1].Melody[0].Pos != 0 {
		t.Errorf("pickup bar %+v, first full bar %+v", mel, r.Bars[1].Melody)
	}
}

// TestMIDIScoreDropsNotesOutOfRange: a note under E1 or over C7 is no note a
// rip holds, so a score's is dropped and counted, the rest kept.
func TestMIDIScoreDropsNotesOutOfRange(t *testing.T) {
	m := &MIDIScore{BPM: 120, division: 480, end: 1920, clock: []midiTempo{{perTick: 0.5 / 480}}, Notes: []RipNote{
		{Start: 0, End: 0.5, MIDI: RipMinMIDI - 1}, {Start: 0.5, End: 1, MIDI: 60, Vel: 1}, {Start: 1, End: 1.5, MIDI: RipMaxMIDI}}}
	r, dropped := m.Rip()
	if dropped != 2 || len(r.Notes) != 1 || r.Notes[0].MIDI != 60 || len(r.Bars[0].Melody) != 1 {
		t.Errorf("%d dropped, transcript %+v, melody %+v", dropped, r.Notes, r.Bars[0].Melody)
	}
}

// TestRenderLinesPlaysOnlyTheInstrumentsNamed: the loop rendered with only the
// bass is quieter than with everything and not silent, and with no instrument
// named it is silence.
func TestRenderLinesPlaysOnlyTheInstrumentsNamed(t *testing.T) {
	ae, slots := startLoopEngine(t)
	tl := [][MusicSlots]PatternID{slots, slots}
	render := func(only []InstrumentType) float64 {
		buf, err := ae.RenderLines(tl, 120, DefaultRootNote, ScalePhrygian, nil, 1, only)
		if err != nil {
			t.Fatal(err)
		}
		var e float64
		for _, v := range buf {
			e += v * v
		}
		return math.Sqrt(e / float64(len(buf)))
	}
	all, bass, none := render(nil), render([]InstrumentType{InstrBass}), render([]InstrumentType{})
	if none != 0 || bass == 0 || bass >= all {
		t.Errorf("rms all %.4f, bass alone %.4f, nothing %.4f", all, bass, none)
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

// TestRipSongIsADictionaryAndATimeline: bars within the threshold of one another
// share a pattern, each group's medoid standing for it, the song plays them in
// the rip's order at its tempo and key, and the document carries it with the
// patterns, validated, resolvable to a timeline and back to the same song.
func TestRipSongIsADictionaryAndATimeline(t *testing.T) {
	r := &Rip{BPM: 120, Root: 40, Scale: ScaleMinor, Progression: []int{0, 5}, Bars: make([]RipBar, 6)}
	for b := range r.Bars {
		r.Bars[b].Drums[InstrKick] = []Step{{Pos: 0, Vel: 1}, {Pos: 8, Vel: 1}}
		if b%3 == 2 { // every third bar a snare joins: one hit in three differs
			r.Bars[b].Drums[InstrSnare] = []Step{{Pos: 4, Vel: 0.9}}
		}
		if b < 4 {
			r.Bars[b].Melody = []Step{{Pos: 0, Vel: 0.8, Deg: b % 2, Oct: 2, Dur: 4}}
		}
	}
	// four items a tenth apart: the pairs join at 0.1 and the pairs at 0.2 by
	// their mean, so a threshold between keeps two groups and one above makes one
	assign, medoids := Cluster(4, func(i, j int) float64 { return math.Abs(float64(i-j)) / 10 }, 0.15)
	if !slices.Equal(assign, []int{0, 0, 1, 1}) || len(medoids) != 2 {
		t.Errorf("items a tenth apart clustered %v with medoids %v under 0.15", assign, medoids)
	}
	if assign, medoids = Cluster(4, func(i, j int) float64 { return math.Abs(float64(i-j)) / 10 }, 0.25); !slices.Equal(assign, []int{0, 0, 0, 0}) || (medoids[0] != 1 && medoids[0] != 2) {
		t.Errorf("items a tenth apart clustered %v with medoids %v under 0.25", assign, medoids)
	}
	beat := r.Cluster(RipBeat, 0.05)
	if !slices.Equal(beat, []int{0, 0, 2, 0, 0, 2}) {
		t.Errorf("beat groups %v, want the snare bars apart", beat)
	}
	if loose := r.Cluster(RipBeat, 0.5); !slices.Equal(loose, []int{0, 0, 0, 0, 0, 0}) {
		t.Errorf("within a half every bar is one group: %v", loose)
	}
	melody := r.Cluster(RipMelody, 0.05)
	if !slices.Equal(melody, []int{0, 1, 0, 1, -1, -1}) {
		t.Errorf("melody groups %v, want two alternating and the silent bars out", melody)
	}
	pats, song := r.Song("x", [RipParts][]int{beat, melody, r.Cluster(RipBass, 0.05)})
	if len(pats) != 4 || pats[0].Name != "x_beat_a" || pats[1].Name != "x_beat_b" || pats[2].Name != "x_melody_a" || pats[3].Name != "x_melody_b" {
		names := make([]string, len(pats))
		for i, p := range pats {
			names[i] = p.Name
		}
		t.Fatalf("dictionary %v", names)
	}
	if song.BPM != 120 || song.Scale != "minor" || len(song.Bars) != 6 || song.Bars[2][0] != "x_beat_b" || song.Bars[4][1] != "" || song.Bars[3][1] != "x_melody_b" {
		t.Errorf("song %+v", song)
	}
	if err := ValidateSong(song); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalPatternFile(&PatternSpecFile{Pattern: pats, Song: []*SongDef{song}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := LoadPatternFile(data)
	if err != nil || len(f.Pattern) != 4 || len(f.Song) != 1 || !reflect.DeepEqual(f.Song[0], song) {
		t.Fatalf("the document came back as %v, %+v", err, f)
	}
	ids := map[string]PatternID{"x_beat_a": 3, "x_beat_b": 4, "x_melody_a": 5, "x_melody_b": 6}
	tl, err := song.Timeline(func(n string) PatternID { return ids[n] })
	if err != nil || len(tl) != 6 || tl[2][0] != 4 || tl[1][1] != 6 || tl[5][1] != PatternSilence {
		t.Errorf("timeline %v: %v", tl, err)
	}
	if _, err := song.Timeline(func(string) PatternID { return PatternSilence }); err == nil {
		t.Error("an unregistered name resolved")
	}
	if letters(0) != "a" || letters(25) != "z" || letters(26) != "aa" || letters(27) != "ab" {
		t.Errorf("letters %s %s %s %s", letters(0), letters(25), letters(26), letters(27))
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

// TestStepAccidentalKeepsAPitchOffTheKey: a step's accidental is the semitones
// its pitch sits off the nearest degree, so every pitch reads back as itself
// through a key, a document carries it, and a rip tells it from the degree alone.
func TestStepAccidentalKeepsAPitchOffTheKey(t *testing.T) {
	for midi := RipMinMIDI; midi < RipMaxMIDI; midi++ {
		deg, oct, acc := PitchStep(40, ScalePhrygian, midi)
		if back := StepPitch(40, ScalePhrygian, deg, oct, acc); back != midi || acc < -1 || acc > 1 {
			t.Fatalf("midi %d reads as degree %d octave %d accidental %d, back %d", midi, deg, oct, acc, back)
		}
	}
	pats, err := LoadPatternsTOML([]byte("[[pattern]]\nname = \"sharp\"\nsteps = 16\n[[pattern.track]]\ninstr = \"lead\"\nevent = [{pos = 0, vel = 0.9, deg = 1, oct = 1, acc = 1}]\n"))
	if err != nil || len(pats) != 1 || len(pats[0].Tracks) != 1 || pats[0].Tracks[0].Events[0].Acc != 1 {
		t.Fatalf("a document's acc: %v %+v", err, pats)
	}
	plain := &RipBar{Melody: []Step{{Pos: 0, Vel: 0.9, Deg: 1, Oct: 1}}}
	sharp := &RipBar{Melody: []Step{{Pos: 0, Vel: 0.9, Deg: 1, Oct: 1, Acc: 1}}}
	a := &Rip{BPM: 120, Root: 40, Scale: ScalePhrygian, Bars: []RipBar{*plain}}
	b := &Rip{BPM: 120, Root: 40, Scale: ScalePhrygian, Bars: []RipBar{*sharp}}
	if d := BarDistance(plain, sharp); d <= 0 || CompareRips(a, b).Melody >= 1 || CompareRips(b, b).Melody != 1 {
		t.Errorf("a sharp is another note: distance %.2f, melody %.2f", d, CompareRips(a, b).Melody)
	}
}

// TestRipReadsADetunedRecording: a recording tuned forty cents flat of A440 is
// heard on its own tuning, so its notes read as they do in tune rather than
// wobbling between the two semitones each sits between.
func TestRipReadsADetunedRecording(t *testing.T) {
	const bpm, bars = 140, 8
	ae, slots := startLoopEngine(t)
	buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := ExpectedRip(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil)
	inTune, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(buf, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	// the same render read as if sampled slower: every pitch forty cents flat,
	// the tempo slower by the same ratio
	flat := math.Pow(2, -40.0/1200)
	rate := int(math.Round(AudioSampleRate * flat))
	detuned, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: int(math.Round(bpm * flat))}}.Samples(buf, rate)
	if err != nil {
		t.Fatal(err)
	}
	a, b := CompareRips(inTune, want), CompareRips(detuned, want)
	t.Logf("in tune %+.1f cents %+v; detuned %+.1f cents %+v", inTune.Tuning, a, detuned.Tuning, b)
	if inTune.Tuning < -8 || inTune.Tuning > 8 || detuned.Tuning < -48 || detuned.Tuning > -32 {
		t.Errorf("tuning heard %+.1f and %+.1f cents, want about 0 and -40", inTune.Tuning, detuned.Tuning)
	}
	if b.Melody < a.Melody-0.1 || b.Bass < a.Bass-0.1 {
		t.Errorf("detuned melody %.2f bass %.2f against %.2f and %.2f in tune", b.Melody, b.Bass, a.Melody, a.Bass)
	}
	// the engine's own tuning: the loop rendered forty cents flat, as a corpus
	// loop standing in for such a recording is, reads so and keeps its notes
	ae.SetTuning(-40)
	low, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	ae.SetTuning(0)
	if err != nil {
		t.Fatal(err)
	}
	lowRip, err := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}.Samples(low, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	c := CompareRips(lowRip, want)
	t.Logf("rendered flat %+.1f cents %+v", lowRip.Tuning, c)
	if lowRip.Tuning < -48 || lowRip.Tuning > -32 || c.Melody < a.Melody-0.1 || c.Bass < a.Bass-0.1 || ae.Tuning() != 0 {
		t.Errorf("rendered flat: tuning %+.1f, melody %.2f bass %.2f, engine back at %+.0f", lowRip.Tuning, c.Melody, c.Bass, ae.Tuning())
	}
}

// TestRipOpensAPickupBar: a recording that begins inside a bar, notes leading
// into the first full one, gets a bar before it holding them, opening before
// the recording starts; the comparison skips it against a truth without one,
// and the notes' lag against the beats is taken out before they are quantized.
func TestRipOpensAPickupBar(t *testing.T) {
	const bpm, bars = 140, 8
	ae, slots := startLoopEngine(t)
	buf, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := ExpectedRip(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil)
	cut := 10 * SamplesPerBar(bpm) / StepsPerBar // the recording starts on the tenth step
	r, err := RipSamples(buf[cut:], AudioSampleRate, DefaultRipParams())
	if err != nil {
		t.Fatal(err)
	}
	score := CompareRips(r, want)
	t.Logf("downbeat %.2fs bars %d score %+v, pickup melody %v", r.Downbeat, len(r.Bars), score, r.Bars[0].Melody)
	if r.Downbeat >= 0 || len(r.Bars) != bars || len(r.Bars[0].Melody) < 2 {
		t.Fatalf("downbeat %.2fs, %d bars, pickup melody %v", r.Downbeat, len(r.Bars), r.Bars[0].Melody)
	}
	for _, st := range r.Bars[0].Melody {
		if st.Pos < 8 {
			t.Errorf("a note at step %d of the pickup bar, before the recording starts", st.Pos)
		}
	}
	if score.Melody < 0.7 || score.Bass < 0.7 {
		t.Errorf("score %+v", score)
	}
	// a truth with the same pickup bar grades itself perfect
	with := &Rip{BPM: bpm, Root: want.Root, Scale: want.Scale, Downbeat: -float64(BeatsPerBar*60) / bpm, Bars: append([]RipBar{{}}, want.Bars...)}
	if s := CompareRips(with, want); s.Total != 1 {
		t.Errorf("a pickup bar against none grades %+v", s)
	}
	g := ripGrid{beats: []int{0, 16, 32, 48, 64}, bars: 1}
	if lag := g.lag([]ripNote{{start: 1}, {start: 17}, {start: 33}, {start: 49}}); lag != 0.25 {
		t.Errorf("lag %.2f steps, want 0.25", lag)
	}
	if _, ok := g.phase(70); ok {
		t.Error("a frame past the last beat has no phase")
	}
}

// TestFidelityHearsTheDifference: a render heard beside itself is wholly alike;
// the same bars with every melody note a semitone sharp are less alike in pitch
// while their onsets still are; a sound on a rip's beats reads as one on a grid;
// a sound needs one or the other; a Ripper of zero parameters rips with the
// defaults.
func TestFidelityHearsTheDifference(t *testing.T) {
	const bpm, bars = 140, 4
	ae, slots := startLoopEngine(t)
	same, err := ae.RenderMusic(slots, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	sharp, err := ae.DefinePattern(&Pattern{Name: "tune_sharp", Steps: 16, Tracks: []Track{{Instr: InstrLead, Events: []Step{
		{Pos: 0, Vel: 0.9, Deg: 0, Oct: 2, Acc: 1, Dur: 2}, {Pos: 2, Vel: 0.8, Deg: 1, Oct: 2, Acc: 1, Dur: 2}, {Pos: 4, Vel: 0.8, Deg: 2, Oct: 2, Acc: 1, Dur: 2}, {Pos: 6, Vel: 0.8, Deg: 3, Oct: 2, Acc: 1, Dur: 2},
		{Pos: 8, Vel: 0.9, Deg: 4, Oct: 2, Acc: 1, Dur: 2}, {Pos: 10, Vel: 0.8, Deg: 5, Oct: 2, Acc: 1, Dur: 2}, {Pos: 12, Vel: 0.8, Deg: 6, Oct: 2, Acc: 1, Dur: 2}, {Pos: 14, Vel: 0.9, Deg: 0, Oct: 3, Acc: 1, Dur: 2}}}}})
	if err != nil {
		t.Fatal(err)
	}
	moved := slots
	moved[1] = sharp
	other, err := ae.RenderMusic(moved, bpm, bars, DefaultRootNote, ScalePhrygian, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	rp := Ripper{Params: DefaultRipParams(), Grid: &RipGrid{BPM: bpm}}
	a, err := rp.Sound(same, AudioSampleRate, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rp.Sound(other, AudioSampleRate, nil)
	if err != nil {
		t.Fatal(err)
	}
	self, diff := Fidelity(a, a), Fidelity(b, a)
	t.Logf("itself %+v; a semitone sharp %+v", self, diff)
	if self.Total < 0.99 || diff.Pitch > self.Pitch-0.1 || diff.Onset < 0.8 {
		t.Errorf("itself %+v, a semitone sharp %+v", self, diff)
	}
	r, err := rp.Samples(same, AudioSampleRate)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Ripper{Params: DefaultRipParams()}.Sound(same, AudioSampleRate, r.Beats)
	if err != nil {
		t.Fatal(err)
	}
	if f := Fidelity(c, a); f.Total < 0.99 {
		t.Errorf("on the rip's beats %+v", f)
	}
	if _, err := (Ripper{}).Sound(same, AudioSampleRate, nil); err == nil {
		t.Error("a sound with neither beats nor a grid was heard")
	}
	if z, err := (Ripper{Grid: &RipGrid{BPM: bpm}}).Samples(same, AudioSampleRate); err != nil || len(z.Bars) != bars || len(z.Notes) == 0 {
		t.Errorf("zero parameters: %v, %d bars", err, len(z.Bars))
	}
}
