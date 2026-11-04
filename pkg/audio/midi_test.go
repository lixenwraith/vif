package audio

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

// TestReadMIDIReadsTheSameTranscript: what a song's file adds, programs that
// change, a key and time signatures, a clap and a tom, a drum part on a track of
// its own, moves none of the notes, velocities or hits ReadMIDI reads, which
// truth scores and corpus labels from.
func TestReadMIDIReadsTheSameTranscript(t *testing.T) {
	file := smf(4)
	file = append(file, track(
		0x00, 0xFF, 0x51, 3, 0x07, 0xA1, 0x20, // 120 bpm
		0x00, 0xFF, 0x58, 4, 3, 2, 24, 8, // 3/4
		0x00, 0xFF, 0x59, 2, 1, 1, // E minor
		0x8B, 0x20, 0xFF, 0x58, 4, 6, 3, 12, 8, // 6/8 at 1440
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0xFF, 0x03, 5, 'P', 'i', 'a', 'n', 'o',
		0x00, 0xC0, 0, // grand piano on channel 1
		0x00, 0xC1, 33, // a bass on channel 2
		0x00, 0x90, 76, 100, // E5 at 0
		0x00, 0x91, 40, 90, // E2 at 0
		0x83, 0x60, 0x80, 76, 0, // E5's end at 480
		0x00, 0xC0, 40, // a violin on channel 1
		0x00, 0x90, 79, 70, // G5 at 480
		0x81, 0x70, 0x80, 79, 0, // its end at 720
		0x81, 0x70, 0x81, 40, 0, // E2's end at 960
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0x99, 36, 120, // a kick at 0
		0x83, 0x60, 0x89, 36, 0,
		0x00, 0x99, 39, 100, // a clap and a tom at 480
		0x00, 45, 80,
		0x81, 0x70, 0x89, 39, 0,
		0x00, 45, 0,
		0x00, 0x99, 42, 60, // a hat at 720
		0x78, 0x89, 42, 0,
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x87, 0x40, 0x99, 38, 110, // a snare at 960, on a track of its own
		0x78, 0x89, 38, 0,
		0x00, 0xFF, 0x2F, 0)...)
	m, err := ReadMIDI(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	notes := []RipNote{
		{Start: 0, End: 0.5, MIDI: 76, Vel: 100.0 / 127},
		{Start: 0, End: 1, MIDI: 40, Vel: 90.0 / 127, Bass: true},
		{Start: 0.5, End: 0.75, MIDI: 79, Vel: 70.0 / 127},
	}
	hits := []RipHit{{Start: 0, Drum: InstrKick, Vel: 120.0 / 127}, {Start: 0.75, Drum: InstrHihat, Vel: 60.0 / 127}, {Start: 1, Drum: InstrSnare, Vel: 110.0 / 127}}
	if len(m.Notes) != len(notes) || len(m.Hits) != len(hits) {
		t.Fatalf("read %+v and %+v", m.Notes, m.Hits)
	}
	for i, n := range m.Notes {
		w := notes[i]
		if n.MIDI != w.MIDI || n.Bass != w.Bass || n.Second || n.Bright != RipUnmeasured ||
			math.Abs(n.Start-w.Start) > 1e-9 || math.Abs(n.End-w.End) > 1e-9 || math.Abs(n.Vel-w.Vel) > 1e-9 {
			t.Errorf("note %d: %+v, want %+v", i, n, w)
		}
	}
	for i, h := range m.Hits {
		if w := hits[i]; h.Drum != w.Drum || math.Abs(h.Start-w.Start) > 1e-9 || math.Abs(h.Vel-w.Vel) > 1e-9 {
			t.Errorf("hit %d: %+v, want %+v", i, h, w)
		}
	}
	if m.BPM != 120 || m.Changes != 0 || m.Left != 0 {
		t.Errorf("%.0f bpm, %d change(s), %d part(s) left out", m.BPM, m.Changes, m.Left)
	}
}

// sounding is what a song plays, a line a note or hit: its instrument and desc,
// the step from the song's start, the pitch under the song's key, the velocity
// as a note-on holds it and the length in steps, sorted
func sounding(t *testing.T, d *SongDef, pattern func(string) *PatternDef) []string {
	t.Helper()
	var out []string
	at := 0
	for _, bar := range d.Bars {
		steps := StepsPerBar
		for _, n := range bar {
			if n != "" {
				steps = barSteps(pattern(n).Steps)
			}
		}
		for _, n := range bar {
			if n == "" {
				continue
			}
			p := pattern(n)
			for _, tr := range p.Track {
				instr, _ := InstrumentByName(tr.Instr)
				for j := range steps {
					for _, e := range tr.Event {
						if e.Pos != (at+j)%p.Steps {
							continue
						}
						pitch, dur := 0, 0
						if !instr.IsDrum() {
							pitch, dur = StepPitch(d.Root, d.ScaleID(), e.Deg, e.Oct, e.Acc), max(e.Dur, 1)
						}
						out = append(out, fmt.Sprintf("%s %q at %d: pitch %d vel %d for %d", tr.Instr, tr.Desc, at+j, pitch, int(math.Round(e.Vel*127)), dur))
					}
				}
			}
		}
		at += steps
	}
	slices.Sort(out)
	return out
}

// lookup is a pattern by name among pats
func lookup(pats ...*PatternDef) func(string) *PatternDef {
	return func(n string) *PatternDef {
		i := slices.IndexFunc(pats, func(p *PatternDef) bool { return p.Name == n })
		if i < 0 {
			return nil
		}
		return pats[i]
	}
}

// TestPatternRoundTripsThroughMIDI: a pattern written as MIDI and loaded back
// holds the same notes, velocities and lengths, a note past its bar's end and a
// pitch off the key among them, and the file says nothing it could not keep
func TestPatternRoundTripsThroughMIDI(t *testing.T) {
	p := &PatternDef{Name: "tune", Steps: 64, Role: "melody", Track: []TrackDef{
		{Instr: "piano", Desc: "right hand", Event: []StepDef{{Pos: 0, Vel: 0.8, Dur: 4}, {Pos: 4, Vel: 0.5, Deg: 2, Acc: 1, Dur: 2}, {Pos: 14, Vel: 1, Deg: 4, Dur: 20}, {Pos: 40, Vel: 0.3, Deg: -1, Oct: 1}, {Pos: 63, Vel: 0.6, Deg: 6, Oct: -1, Dur: 3}}},
		{Instr: "lead", Event: []StepDef{{Pos: 2, Vel: 0.7, Deg: 7, Dur: 6}, {Pos: 18, Vel: 0.4, Deg: 3, Acc: -1}, {Pos: 33, Vel: 0.9, Deg: 9, Dur: 30}}},
		{Instr: "kick", Event: []StepDef{{Pos: 0, Vel: 1}, {Pos: 16, Vel: 0.9}, {Pos: 32, Vel: 0.8}, {Pos: 48, Vel: 0.7}}},
		{Instr: "snare", Event: []StepDef{{Pos: 8, Vel: 0.75}, {Pos: 24, Vel: 0.6}, {Pos: 56, Vel: 0.85}}},
		{Instr: "hihat", Event: []StepDef{{Pos: 2, Vel: 0.3}, {Pos: 6, Vel: 0.35}, {Pos: 50, Vel: 0.4}}},
		{Instr: "clap", Event: []StepDef{{Pos: 24, Vel: 0.5}}},
	}}
	var b bytes.Buffer
	if err := WritePatternMIDI(&b, p, 120, 45, ScaleMinor, nil); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMIDI(&b)
	if err != nil {
		t.Fatal(err)
	}
	pats, song, said := m.Song("back")
	if len(said) > 0 || song.BPM != 120 || len(song.Bars) != 4 {
		t.Errorf("%d bar(s) at %d bpm, said %q", len(song.Bars), song.BPM, said)
	}
	once := &SongDef{Name: "tune", BPM: 120, Root: 45, Scale: "minor", Bars: [][]string{{"", "tune"}, {"", "tune"}, {"", "tune"}, {"", "tune"}}}
	want, got := sounding(t, once, lookup(p)), sounding(t, song, lookup(pats...))
	if !slices.Equal(got, want) {
		t.Errorf("loaded back\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestSongRoundTripsThroughMIDI: a song written as MIDI and loaded back plays the
// same notes, velocities and lengths, a bar each, and a bar it repeats is the
// pattern it played before
func TestSongRoundTripsThroughMIDI(t *testing.T) {
	pats := []*PatternDef{
		{Name: "beat", Steps: 16, Role: "rhythm", Track: []TrackDef{
			{Instr: "kick", Event: []StepDef{{Pos: 0, Vel: 1}, {Pos: 8, Vel: 0.8}}},
			{Instr: "hihat", Event: []StepDef{{Pos: 4, Vel: 0.4}, {Pos: 12, Vel: 0.45}}}}},
		{Name: "fill", Steps: 16, Role: "rhythm", Track: []TrackDef{
			{Instr: "snare", Event: []StepDef{{Pos: 0, Vel: 0.7}, {Pos: 2, Vel: 0.6}, {Pos: 4, Vel: 0.9}}},
			{Instr: "clap", Event: []StepDef{{Pos: 12, Vel: 1}}}}},
		{Name: "hook", Steps: 16, Role: "melody", Track: []TrackDef{
			{Instr: "piano", Event: []StepDef{{Pos: 0, Vel: 0.9, Deg: 4, Dur: 3}, {Pos: 6, Vel: 0.5, Deg: 2, Acc: -1, Dur: 10}}},
			{Instr: "pluck", Desc: "guitar", Event: []StepDef{{Pos: 2, Vel: 0.6, Deg: 0, Oct: 1}, {Pos: 10, Vel: 0.65, Deg: 1, Oct: 1, Dur: 2}}}}},
		{Name: "answer", Steps: 16, Role: "melody", Track: []TrackDef{
			{Instr: "pad", Event: []StepDef{{Pos: 0, Vel: 0.4, Deg: 0, Dur: 16}, {Pos: 0, Vel: 0.4, Deg: 2, Dur: 16}}}}},
		{Name: "low", Steps: 16, Role: "bass", Track: []TrackDef{
			{Instr: "bass", Event: []StepDef{{Pos: 0, Vel: 1, Oct: -1, Dur: 6}, {Pos: 8, Vel: 0.85, Deg: 4, Oct: -2, Dur: 6}}}}},
	}
	d := &SongDef{Name: "ditty", BPM: 96, Root: 43, Scale: "dorian", Bars: [][]string{
		{"beat", "hook", "", "low"}, {"beat", "answer", "", "low"}, {"beat", "hook", "", "low"}, {"fill", "hook", "", ""}}}
	var b bytes.Buffer
	if err := WriteSongMIDI(&b, d, lookup(pats...)); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMIDI(&b)
	if err != nil {
		t.Fatal(err)
	}
	back, song, said := m.Song("back")
	if len(said) > 0 || song.BPM != d.BPM || len(back) != len(pats) {
		t.Errorf("%d pattern(s) at %d bpm, said %q", len(back), song.BPM, said)
	}
	if bars := song.Bars; bars[0][1] != bars[2][1] || bars[0][1] != bars[3][1] || bars[0][0] != bars[2][0] || bars[1][0] != bars[0][0] || bars[3][3] != "" {
		t.Errorf("bars %q, want the hook's and the beat's patterns again where the song repeats them", bars)
	}
	want, got := sounding(t, d, lookup(pats...)), sounding(t, song, lookup(back...))
	if !slices.Equal(got, want) {
		t.Errorf("loaded back\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestMIDIReadsMetersInTheirSteps: 3/4 and 6/8 bars load as patterns of 12 steps
// and 3/8 of 6; a meter no pattern holds in whole steps loads in 16 and is said,
// as is a tempo change, with the bar it falls in
func TestMIDIReadsMetersInTheirSteps(t *testing.T) {
	file := smf(2)
	file = append(file, track(
		0x00, 0xFF, 0x51, 3, 0x07, 0xA1, 0x20, // 120 bpm
		0x00, 0xFF, 0x58, 4, 3, 2, 24, 8, // 3/4, two bars
		0x96, 0x40, 0xFF, 0x58, 4, 6, 3, 12, 8, // 6/8 at 2880
		0x8B, 0x20, 0xFF, 0x58, 4, 3, 3, 12, 8, // 3/8 at 4320
		0x00, 0xFF, 0x51, 3, 0x0F, 0x42, 0x40, // 60 bpm
		0x85, 0x50, 0xFF, 0x58, 4, 5, 5, 3, 8, // 5/32 at 5040
		0x00, 0xFF, 0x2F, 0)...)
	file = append(file, track(
		0x00, 0x90, 60, 100, 0x78, 0x80, 60, 0, // a sixteenth at each bar line
		0x8A, 0x28, 0x90, 60, 100, 0x78, 0x80, 60, 0,
		0x8A, 0x28, 0x90, 60, 100, 0x78, 0x80, 60, 0,
		0x8A, 0x28, 0x90, 60, 100, 0x78, 0x80, 60, 0,
		0x84, 0x58, 0x90, 60, 100, 0x78, 0x80, 60, 0,
		0x00, 0xFF, 0x2F, 0)...)
	m, err := ReadMIDI(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	pats, song, said := m.Song("odd")
	var steps []int
	for _, bar := range song.Bars {
		steps = append(steps, lookup(pats...)(bar[RipPartSlots[RipMelody]]).Steps)
	}
	if !slices.Equal(steps, []int{12, 12, 12, 6, 16}) || len(pats) != 3 {
		t.Errorf("bars of %v steps in %d pattern(s), want 12 12 12 6 16 in 3", steps, len(pats))
	}
	for _, w := range []string{
		"3/4 from bar 0 in patterns of 12 steps",
		"6/8 from bar 2 in patterns of 12 steps",
		"3/8 from bar 3 in patterns of 6 steps",
		"5/32 from bar 4 is no whole number of steps a pattern holds: read in bars of 16",
		"1 tempo change(s) not kept, the song plays at 120 bpm throughout: to 60 at bar 3",
	} {
		if !slices.Contains(said, w) {
			t.Errorf("not said: %q, in %q", w, said)
		}
	}
}

// TestMIDIWritesAPatternInTheMeterItsStepsMake: a pattern is written in bars of
// 16 steps, else of 12 (3/4), else whole, and loads back in bars of as many
func TestMIDIWritesAPatternInTheMeterItsStepsMake(t *testing.T) {
	for _, c := range []struct {
		steps, bars int
		meter       [2]int
	}{{12, 1, [2]int{3, 4}}, {24, 2, [2]int{3, 4}}, {6, 1, [2]int{3, 8}}, {10, 1, [2]int{5, 8}}, {64, 4, [2]int{4, 4}}} {
		p := &PatternDef{Name: "riff", Steps: c.steps, Role: "bass", Track: []TrackDef{{Instr: "bass", Event: []StepDef{
			{Pos: 0, Vel: 1, Oct: -1, Dur: 2}, {Pos: c.steps - 1, Vel: 0.5, Deg: 4, Oct: -1}}}}}
		var b bytes.Buffer
		if err := WritePatternMIDI(&b, p, 100, 40, ScaleMajor, nil); err != nil {
			t.Fatal(err)
		}
		m, err := ReadMIDI(&b)
		if err != nil {
			t.Fatal(err)
		}
		pats, song, _ := m.Song("back")
		if m.Meter != c.meter || len(song.Bars) != c.bars || pats[0].Steps != c.steps/c.bars {
			t.Errorf("%d steps: %d/%d in %d bar(s) of %d, want %d/%d in %d of %d", c.steps, m.Meter[0], m.Meter[1], len(song.Bars), pats[0].Steps, c.meter[0], c.meter[1], c.bars, c.steps/c.bars)
		}
	}
}

// TestMIDIProgramsReadAsTheirVoice: every program reads as one shipped voice, of
// its family's row; a voice's own program and a kit piece's keys read back as it
func TestMIDIProgramsReadAsTheirVoice(t *testing.T) {
	for program := range 128 {
		rows := 0
		for v := InstrBass; v < InstrumentCount; v++ {
			if slices.Contains(midiInstruments[v].families, byte(program/8)) {
				rows++
			}
		}
		if rows != 1 {
			t.Errorf("program %d in %d rows, want 1", program, rows)
		}
	}
	for v := InstrBass; v < InstrumentCount; v++ {
		if got := midiVoice(midiProgram(v)); got != v {
			t.Errorf("%s writes program %d, read back as %s", v, midiProgram(v), got)
		}
	}
	for d := InstrKick; d <= InstrClap; d++ {
		for _, k := range midiInstruments[d].keys {
			if got := midiDrum(k, InstrClap+1); got != d {
				t.Errorf("%s's key %d read back as %d", d, k, got)
			}
		}
	}
}

// TestMIDIWritesAnAddedVoiceOnItsNearest: a voice no row writes is written on the
// program of the shipped voice nearest it, of its wave first, then by its genes
func TestMIDIWritesAnAddedVoiceOnItsNearest(t *testing.T) {
	t.Cleanup(ResetRegistries)
	ResetRegistries()
	for _, v := range []*VoiceDef{
		{Name: "piano", Wave: "modal", Partials: 16, Decay: 1.5, Release: 0.5, Level: 0.8},
		{Name: "bass", Wave: "modal", Partials: 4, Decay: 0.3, Sustain: 0.6, Release: 0.1, Level: 1.2},
		{Name: "pad", Wave: "saw", Attack: 0.8, Sustain: 1, Release: 2, Level: 0.5},
		{Name: "lead", Wave: "saw", Attack: 0.005, Sustain: 0.7, Release: 0.1, Level: 0.6},
		{Name: "pluck", Wave: "string", Decay: 0.8, Release: 0.3, Level: 0.7},
	} {
		if _, err := RegisterVoice(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		v    *VoiceDef
		near InstrumentType
	}{
		{&VoiceDef{Name: "keys", Wave: "modal", Partials: 14, Decay: 1.2, Release: 0.6, Level: 0.8}, InstrPiano},
		{&VoiceDef{Name: "sub", Wave: "modal", Partials: 3, Decay: 0.4, Sustain: 0.5, Release: 0.1, Level: 1.1}, InstrBass},
		{&VoiceDef{Name: "buzz", Wave: "saw", Attack: 0.01, Sustain: 0.8, Release: 0.15, Level: 0.6}, InstrLead},
		{&VoiceDef{Name: "harp", Wave: "string", Attack: 0.8, Sustain: 1, Release: 2, Level: 0.5}, InstrPluck}, // a pad's genes
	} {
		id, err := RegisterVoice(c.v)
		if err != nil {
			t.Fatal(err)
		}
		if got := midiProgram(id); got != midiInstruments[c.near].program {
			t.Errorf("%s written on program %d, want the %s's %d", c.v.Name, got, c.near, midiInstruments[c.near].program)
		}
	}
}
