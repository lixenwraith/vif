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
