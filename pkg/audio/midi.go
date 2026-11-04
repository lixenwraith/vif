package audio

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// midiDivision is the ticks per quarter note the writers write, a tick under
// two milliseconds at any tempo the sequencer plays
const midiDivision = 480

const (
	midiDrums        = 9      // the channel General MIDI keeps for percussion, which has no pitch
	midiDefaultTempo = 500000 // microseconds a quarter note before a file's first tempo, 120 bpm
	midiThreeBeats   = 3 * StepsPerBeat
)

// midiInstruments is each shipped instrument as General MIDI writes and reads it:
// a drum's keys on the drum channel, the first the one written; a voice's
// program, written, and the families of eight programs read back as it, so a
// program no row writes plays on the shipped voice of its family. A ride,
// tambourine or shaker reads as the hihat, the line stapes plays it on.
var midiInstruments = [InstrumentCount]struct {
	keys     []byte
	program  byte
	families []byte
}{
	InstrKick:  {keys: []byte{36, 35}},
	InstrSnare: {keys: []byte{38, 40, 37}},                             // head, rim, cross-stick
	InstrHihat: {keys: []byte{42, 44, 46, 22, 26, 51, 59, 53, 54, 82}}, // closed, pedal, open, edges, ride, tambourine, shaker
	InstrClap:  {keys: []byte{39}},
	InstrBass:  {program: 32, families: []byte{4}},                   // acoustic bass: the basses
	InstrPiano: {program: 0, families: []byte{0, 1, 14}},             // grand piano: pianos, chromatic and pitched percussion
	InstrPad:   {program: 89, families: []byte{2, 5, 6, 11, 12, 15}}, // warm pad: organs, strings, ensembles, pads, effects
	InstrLead:  {program: 81, families: []byte{7, 8, 9, 10}},         // sawtooth lead: brass, reeds, pipes, synth leads
	InstrPluck: {program: 24, families: []byte{3, 13}},               // nylon guitar: guitars, plucked ethnic
}

// midiVoice is the shipped voice a program plays on, its family's row
func midiVoice(program byte) InstrumentType {
	for i := InstrBass; i < InstrumentCount; i++ {
		if slices.Contains(midiInstruments[i].families, program/8) {
			return i
		}
	}
	return InstrPiano
}

// midiDrum is the kit piece under pieces a drum key plays, -1 for none
func midiDrum(key byte, pieces InstrumentType) InstrumentType {
	for d := InstrKick; d < pieces; d++ {
		if slices.Contains(midiInstruments[d].keys, key) {
			return d
		}
	}
	return -1
}

// midiProgram is the program a tonal instrument is written with: a shipped
// voice's own, and for one a document added the shipped voice's nearest it, of
// its wave first, then by its genes
func midiProgram(instr InstrumentType) byte {
	if instr < InstrumentCount {
		return midiInstruments[instr].program
	}
	voiceMu.RLock()
	v := &VoiceDef{}
	if int(instr) < len(voiceDefs) && voiceDefs[instr] != nil {
		v = voiceDefs[instr]
	}
	shipped := slices.Clone(voiceDefs[InstrBass:InstrumentCount])
	voiceMu.RUnlock()
	best, far := InstrLead, math.Inf(1)
	genes := v.Genes()
	for i, sv := range shipped {
		if sv == nil {
			continue
		}
		var d float64
		if cmp.Or(sv.Wave, "sine") != cmp.Or(v.Wave, "sine") {
			d = float64(VoiceGeneCount) // a wave apart outweighs every gene
		}
		for k, g := range sv.Genes() {
			d += (g - genes[k]) * (g - genes[k])
		}
		if d < far {
			best, far = InstrBass+InstrumentType(i), d
		}
	}
	return midiInstruments[best].program
}

// midiNote is a note of a track in ticks, before the tempo map, with the program
// its channel plays at its start
type midiNote struct {
	on, off            int64
	ch, key, vel, prog byte
}

// midiPart is one track's notes on one channel: what ReadMIDI maps to a line
type midiPart struct {
	name  string
	track int
	notes []midiNote
}

// midiTempo is a tempo change: from tick on, sec seconds into the file, each
// tick lasts perTick seconds
type midiTempo struct {
	tick         int64
	sec, perTick float64
}

// midiMeter is a time signature from tick on: num beats a bar of 1/den notes
type midiMeter struct {
	tick     int64
	num, den int
}

// midiKey is a key signature from tick on: its sharps, flats negative, and
// whether it signs the minor
type midiKey struct {
	tick  int64
	sf    int
	minor bool
}

// midiChange is a program change on a channel from tick on
type midiChange struct {
	tick        int64
	ch, program byte
}

// midiTrack is what a track chunk holds: its notes by channel, and the tempo
// changes, signatures and program changes in it
type midiTrack struct {
	parts    []midiPart
	tempos   [][2]int64 // tick, microseconds a quarter note
	meters   []midiMeter
	keys     []midiKey
	programs []midiChange
}

// MIDIScore is a Standard MIDI File as ReadMIDI reads it: the transcript in
// seconds under its tempo map, and what of its grid a rip needs.
type MIDIScore struct {
	Notes   []RipNote
	Hits    []RipHit
	BPM     float64 // quarter notes a minute at the start, 120 when the file names none
	Changes int     // tempo changes after the start
	Meter   [2]int  // the time signature the music starts in, 4/4 when the file names none
	Left    int     // parts past the three lines, left out
	// Voices are the shipped voices the melody, under and bass parts' programs
	// play on (midiVoice), General MIDI's piano where a file sets none
	Voices [3]InstrumentType
	// Struck are the drum channel's other strikes (a tom, a crash, a clap),
	// Drum -1: onsets of no line
	Struck []RipHit

	clock     []midiTempo
	division  int
	line, end int64 // the tick that time signature starts on, and the last note's end
	parts     []midiPart
	meters    []midiMeter
	key       *midiKey // the first key signature, nil when the file signs none
}

// seconds is where a tick falls under the tempo map, a tick before the file at
// the tempo it starts with
func (m *MIDIScore) seconds(tick float64) float64 {
	i := sort.Search(len(m.clock), func(i int) bool { return float64(m.clock[i].tick) > max(tick, 0) }) - 1
	return m.clock[i].sec + (tick-float64(m.clock[i].tick))*m.clock[i].perTick
}

// ticks is seconds' tick, seconds inverted
func (m *MIDIScore) ticks(sec float64) float64 {
	i := sort.Search(len(m.clock), func(i int) bool { return m.clock[i].sec > max(sec, 0) }) - 1
	return float64(m.clock[i].tick) + (sec-m.clock[i].sec)/m.clock[i].perTick
}

// ErrMIDIUnlined is ReadMIDI's refusal of a file with no line to read: no
// notes, and no kick, snare or hihat
var ErrMIDIUnlined = errors.New("midi: no notes, and no kick, snare or hihat")

// ReadMIDI reads a Standard MIDI File of type 0 or 1 as a transcript in seconds
// under its tempo map, the drum channel's kicks, snares and hats as hits. A
// track named melody, under or bass is that line; of the other parts (a track's
// notes on one channel), the lowest is the bass, whole, when its median is under
// E3, and the rest the melody, then the under; parts past those are left out.
func ReadMIDI(r io.Reader) (*MIDIScore, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	be := binary.BigEndian
	if len(data) < 14 || string(data[:4]) != "MThd" {
		return nil, errors.New("midi: not a Standard MIDI File")
	}
	size := int(be.Uint32(data[4:]))
	if size < 6 || size > len(data)-8 {
		return nil, errors.New("midi: short header")
	}
	format, tracks, division := be.Uint16(data[8:]), int(be.Uint16(data[10:])), int(be.Uint16(data[12:]))
	switch {
	case format > 1:
		return nil, fmt.Errorf("midi: type %d, want 0 or 1", format)
	case division&0x8000 != 0 || division == 0:
		return nil, errors.New("midi: SMPTE time division unsupported, want ticks per quarter note")
	}
	var all midiTrack
	for off, read := 8+size, 0; off+8 <= len(data) && read < tracks; {
		id, length := string(data[off:off+4]), int(be.Uint32(data[off+4:]))
		body := data[off+8:]
		length = min(length, len(body))
		off += 8 + length
		if id != "MTrk" {
			continue
		}
		t, err := readMIDITrack(body[:length], read)
		if err != nil {
			return nil, fmt.Errorf("midi: track %d: %w", read+1, err)
		}
		all.parts, all.tempos, all.meters = append(all.parts, t.parts...), append(all.tempos, t.tempos...), append(all.meters, t.meters...)
		all.keys, all.programs = append(all.keys, t.keys...), append(all.programs, t.programs...)
		read++
	}
	slices.SortStableFunc(all.tempos, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	m := &MIDIScore{division: division, clock: []midiTempo{{perTick: midiDefaultTempo / 1e6 / float64(division)}}, parts: all.parts, meters: all.meters}
	for _, c := range all.tempos {
		last := m.clock[len(m.clock)-1]
		perTick := float64(c[1]) / 1e6 / float64(division)
		if c[0] > 0 && perTick != last.perTick {
			m.Changes++
		}
		m.clock = append(m.clock, midiTempo{tick: c[0], sec: last.sec + float64(c[0]-last.tick)*last.perTick, perTick: perTick})
	}
	start := m.clock[sort.Search(len(m.clock), func(i int) bool { return m.clock[i].tick > 0 })-1]
	m.BPM = 60 / start.perTick / float64(division)
	m.line, m.Meter = firstBar(m.meters, division)
	if len(all.keys) > 0 {
		k := slices.MinFunc(all.keys, func(a, b midiKey) int { return cmp.Compare(a.tick, b.tick) })
		m.key = &k
	}
	// a channel plays the program last set on it in any track, General MIDI's
	// first before any is
	var programs [16][]midiChange
	slices.SortStableFunc(all.programs, func(a, b midiChange) int { return cmp.Compare(a.tick, b.tick) })
	for _, c := range all.programs {
		programs[c.ch] = append(programs[c.ch], c)
	}
	for _, p := range m.parts {
		for i, n := range p.notes {
			cs := programs[n.ch]
			if k := sort.Search(len(cs), func(k int) bool { return cs[k].tick > n.on }); k > 0 {
				p.notes[i].prog = cs[k-1].program
			}
		}
	}
	seconds := func(tick int64) float64 { return m.seconds(float64(tick)) }
	var tonal []midiPart
	for _, p := range m.parts {
		for _, n := range p.notes {
			m.end = max(m.end, n.off, n.on+1)
		}
		if p.notes[0].ch != midiDrums {
			tonal = append(tonal, p)
			continue
		}
		for _, n := range p.notes {
			h := RipHit{Start: seconds(n.on), Drum: midiDrum(n.key, InstrClap), Vel: float64(n.vel) / 127}
			if h.Drum >= 0 {
				m.Hits = append(m.Hits, h)
			} else {
				m.Struck = append(m.Struck, h)
			}
		}
	}
	m.Voices = [3]InstrumentType{midiVoice(0), midiVoice(0), midiVoice(0)}
	for i, line := range midiLinesOf(tonal) {
		if line < 0 {
			m.Left++
			continue
		}
		m.Voices[line] = midiVoice(tonal[i].notes[0].prog)
		for _, n := range tonal[i].notes {
			m.Notes = append(m.Notes, RipNote{Start: seconds(n.on), End: seconds(n.off), MIDI: int(n.key), Vel: float64(n.vel) / 127, Bass: line == 2, Second: line == 1, Bright: RipUnmeasured})
		}
	}
	if len(m.Notes) == 0 && len(m.Hits) == 0 {
		return nil, ErrMIDIUnlined
	}
	slices.SortStableFunc(m.Notes, func(a, b RipNote) int { return cmpFloat(a.Start, b.Start) })
	slices.SortStableFunc(m.Hits, func(a, b RipHit) int { return cmpFloat(a.Start, b.Start) })
	slices.SortStableFunc(m.Struck, func(a, b RipHit) int { return cmpFloat(a.Start, b.Start) })
	return m, nil
}

// firstBar is where the first time signature that holds for more than a bar
// starts, and that signature: a bar of its own before it, as a pickup is
// written, is not the music's first. A file with none is in 4/4 from its start.
func firstBar(meters []midiMeter, division int) (int64, [2]int) {
	slices.SortStableFunc(meters, func(a, b midiMeter) int { return cmp.Compare(a.tick, b.tick) })
	if len(meters) == 0 || meters[0].tick > 0 {
		meters = append([]midiMeter{{num: 4, den: 4}}, meters...)
	}
	for i, mt := range meters {
		bar := int64(4 * division * mt.num / mt.den)
		if i+1 == len(meters) || mt.tick+bar < meters[i+1].tick {
			return mt.tick, [2]int{mt.num, mt.den}
		}
	}
	panic("unreachable: the last signature holds to the end")
}

// Rip is the score as a rip of it: its notes quantized on its own ticks; its first
// tempo, by octaves into the sequencer's range; its beats from where its time
// signature starts, four a bar, a pickup bar for notes before it; and the key
// keyOf its pitch classes by their lengths. A note outside the rip's range is
// dropped, and counted.
func (m *MIDIScore) Rip() (*Rip, int) {
	const fine = 64 // the grid's units a tick, so a folded beat is whole
	at := func(sec float64) int { return int(math.Round(m.ticks(sec) * fine)) }
	beat, bpm := float64(m.division*fine), m.BPM
	for bpm < MinBPM {
		beat, bpm = beat/2, bpm*2
	}
	for bpm > MaxBPM {
		beat, bpm = beat*2, bpm/2
	}
	r := &Rip{BPM: int(math.Round(bpm))}
	var c, b [12]float64
	var notes []ripNote
	dropped := 0
	for _, n := range m.Notes {
		if n.MIDI < RipMinMIDI || n.MIDI >= RipMaxMIDI {
			dropped++
			continue
		}
		r.Notes = append(r.Notes, n)
		notes = append(notes, ripNote{start: at(n.Start), end: at(n.End), midi: n.MIDI, vel: n.Vel})
		c[n.MIDI%12] += n.End - n.Start
		if n.MIDI < RipBassTop {
			b[n.MIDI%12] += n.End - n.Start
		}
	}
	r.Root, r.Scale = keyOf(c, b)
	var hits [InstrClap][]ripHit
	for _, h := range m.Hits {
		hits[h.Drum] = append(hits[h.Drum], ripHit{frame: at(h.Start), strength: h.Vel})
	}
	bars := max(int(math.Ceil(float64(m.end-m.line)*fine/(BeatsPerBar*beat))), 1)
	g := ripGrid{beats: make([]int, BeatsPerBar*bars+1), bars: bars}
	for i := range g.beats {
		g.beats[i] = int(m.line)*fine + int(math.Round(float64(i)*beat))
	}
	g = g.pickup(beat, notes, hits)
	r.Bars = make([]RipBar, g.bars)
	r.Beats = make([]float64, BeatsPerBar*g.bars+1)
	for i := range r.Beats {
		r.Beats[i] = m.seconds(float64(g.beats[g.first+i]) / fine)
	}
	r.Downbeat = r.Beats[0]
	for d, hs := range hits {
		for _, h := range hs {
			g.hit(r, InstrumentType(d), h.frame, h.strength)
		}
	}
	for i, n := range notes {
		part := func(b *RipBar) *[]Step { return &b.Melody }
		switch {
		case r.Notes[i].Bass:
			part = func(b *RipBar) *[]Step { return &b.Bass }
		case r.Notes[i].Second:
			part = func(b *RipBar) *[]Step { return &b.Under }
		}
		g.place(r, n, part)
	}
	r.Progression = make([]int, g.bars)
	for bar := range r.Progression {
		lo, hi := r.Beats[BeatsPerBar*bar], r.Beats[BeatsPerBar*(bar+1)]
		var chroma [12]float64
		for _, n := range r.Notes {
			chroma[n.MIDI%12] += max(min(n.End, hi)-max(n.Start, lo), 0)
		}
		r.Progression[bar] = chordOf(chroma, r.Root, r.Scale)
	}
	return r, dropped
}

// Song reads the score as patterns and a song under name: a pattern a part a
// bar, in the bars its meter's steps are written in (barSteps: 3/4 twelve, 3/8
// six), the drum channel the beat, the parts a bass program plays the bass and
// the rest the melody, a track each, named as compress names them; the song plays
// them in order, in the key signed or its notes'. It says what it could not keep.
func (m *MIDIScore) Song(name string) ([]*PatternDef, *SongDef, []string) {
	var said []string
	// a step is the file's sixteenth, by the octaves that bring its first tempo
	// into the sequencer's range as the rip folds its beat
	bpm, perStep := m.BPM, float64(m.division)/StepsPerBeat
	for bpm < MinBPM {
		bpm, perStep = bpm*2, perStep/2
	}
	for bpm > MaxBPM {
		bpm, perStep = bpm/2, perStep*2
	}
	if f := float64(m.division) / StepsPerBeat / perStep; f > 1 {
		said = append(said, fmt.Sprintf("its %.0f bpm played at %.0f, a sixteenth %.0f steps", m.BPM, bpm, f))
	} else if f < 1 {
		said = append(said, fmt.Sprintf("its %.0f bpm played at %.0f, a sixteenth half a step or less", m.BPM, bpm))
	}
	at := func(tick int64) int { return int(math.Round(float64(tick) / perStep)) }

	// a line is a track's notes on one channel in one program, a track of the
	// shipped voice the program plays on; a drum's, every drum part's of it
	type line struct {
		part        RipPart
		instr, desc string
		notes       []midiNote
	}
	var lines []line
	var drums [InstrClap + 1][]midiNote
	unkit := map[byte]int{}                  // the hits of a drum key no piece plays
	nearest := map[InstrumentType][]string{} // the programs no row writes, by the voice they play on
	for _, p := range m.parts {
		if p.notes[0].ch == midiDrums {
			for _, n := range p.notes {
				if d := midiDrum(n.key, InstrClap+1); d >= 0 {
					drums[d] = append(drums[d], n)
				} else {
					unkit[n.key]++
				}
			}
			continue
		}
		of := map[byte]int{}
		for _, n := range p.notes {
			i, ok := of[n.prog]
			if !ok {
				v := midiVoice(n.prog)
				if word := strconv.Itoa(int(n.prog)); n.prog != midiInstruments[v].program && !slices.Contains(nearest[v], word) {
					nearest[v] = append(nearest[v], word)
				}
				part, desc := RipMelody, strings.TrimSpace(p.name)
				if v == InstrBass {
					part = RipBass
				}
				if desc == v.String() {
					desc = ""
				}
				i, of[n.prog] = len(lines), len(lines)
				lines = append(lines, line{part: part, instr: v.String(), desc: desc})
			}
			lines[i].notes = append(lines[i].notes, n)
		}
	}
	for d, ns := range drums {
		if len(ns) > 0 {
			lines = append(lines, line{part: RipBeat, instr: InstrumentType(d).String(), notes: ns})
		}
	}
	var tracks [RipParts]int
	left := 0
	lines = slices.DeleteFunc(lines, func(l line) bool {
		tracks[l.part]++
		if tracks[l.part] > MaxPatternTracks {
			left++
		}
		return tracks[l.part] > MaxPatternTracks
	})

	// the bars: each signature's from where it starts, cut where the next starts
	// within one, every bar in the bars its steps are written in
	type bar struct{ start, steps int }
	var bars []bar
	end := 0
	for _, l := range lines {
		for _, n := range l.notes {
			end = max(end, at(n.on)+1)
		}
	}
	meters := m.meters
	if len(meters) == 0 || meters[0].tick > 0 {
		meters = append([]midiMeter{{num: 4, den: 4}}, meters...)
	}
	odd := false
	for i, start, told := 0, 0, -1; start < end; {
		for i+1 < len(meters) && at(meters[i+1].tick) <= start {
			i++
		}
		mt := meters[i]
		exact := float64(mt.num) * 4 * float64(m.division) / float64(mt.den) / perStep
		steps := int(math.Round(exact))
		kept := math.Abs(exact-float64(steps)) < 1e-6 && steps >= 1 && steps <= MaxPatternLen
		if !kept {
			steps = StepsPerBar
		}
		if told != i {
			told = i
			switch b := barSteps(steps); {
			case !kept:
				said = append(said, fmt.Sprintf("%d/%d from bar %d is no whole number of steps a pattern holds: read in bars of %d", mt.num, mt.den, len(bars), steps))
			case b != steps:
				said = append(said, fmt.Sprintf("%d/%d from bar %d in patterns of %d steps, %d a bar", mt.num, mt.den, len(bars), b, steps/b))
			case steps != StepsPerBar:
				said = append(said, fmt.Sprintf("%d/%d from bar %d in patterns of %d steps", mt.num, mt.den, len(bars), steps))
			}
		}
		if i+1 < len(meters) {
			if next := at(meters[i+1].tick); next < start+steps {
				said = append(said, fmt.Sprintf("bar %d cut to %d step(s), where %d/%d starts within it", len(bars), next-start, meters[i+1].num, meters[i+1].den))
				steps = next - start
			}
		}
		b := barSteps(steps)
		for k := 0; k < steps; k += b {
			bars = append(bars, bar{start + k, b})
			odd = odd || b != StepsPerBar
		}
		start += steps
	}
	barAt := func(step int) int {
		return max(sort.Search(len(bars), func(i int) bool { return bars[i].start > step })-1, 0)
	}
	var changes []string
	for i := 1; i < len(m.clock); i++ {
		if c := m.clock[i]; c.tick > 0 && c.perTick != m.clock[i-1].perTick {
			changes = append(changes, fmt.Sprintf("to %.0f at bar %d", 60/c.perTick/float64(m.division), barAt(at(c.tick))))
		}
	}

	var c, b [12]float64
	for _, l := range lines {
		for _, n := range l.notes {
			if l.part != RipBeat {
				c[n.key%12] += float64(n.off - n.on)
				if int(n.key) < RipBassTop {
					b[n.key%12] += float64(n.off - n.on)
				}
			}
		}
	}
	root, scale := keyOf(c, b)
	if m.key != nil {
		root, scale = m.key.of(c)
	}

	// each line's notes on its bars' steps, one a step and pitch, the louder kept
	events := make([][][]StepDef, len(bars))
	for i := range events {
		events[i] = make([][]StepDef, len(lines))
	}
	merged, unheld, long := 0, 0, 0
	for li, l := range lines {
		for _, n := range l.notes {
			step := at(n.on)
			bi := barAt(step)
			// three places hold a note-on's velocity exactly, and read as authored
			s := StepDef{Pos: step - bars[bi].start, Vel: math.Round(float64(n.vel)/127*1000) / 1000}
			if l.part != RipBeat {
				dur := int(math.Round(float64(n.off-n.on) / perStep))
				if dur > MaxPatternLen {
					long++
				}
				s.Dur = min(max(dur, 1), MaxPatternLen)
				if s.Deg, s.Oct, s.Acc = PitchStep(root, scale, int(n.key)); StepPitchBounds(s.Deg, s.Oct, s.Acc) != nil {
					unheld++
					continue
				}
			}
			evs := &events[bi][li]
			if j := slices.IndexFunc(*evs, func(e StepDef) bool { return e.Pos == s.Pos && e.Deg == s.Deg && e.Oct == s.Oct && e.Acc == s.Acc }); j >= 0 {
				merged++
				if (*evs)[j].Vel < s.Vel {
					(*evs)[j] = s
				}
				continue
			}
			if len(*evs) == MaxPatternEvents {
				unheld++
				continue
			}
			*evs = append(*evs, s)
		}
	}

	song := &SongDef{Name: name, BPM: int(math.Round(bpm)), Root: root, Scale: ScaleName(scale), Bars: make([][]string, len(bars))}
	var pats []*PatternDef
	var named [RipParts]map[string]string // a pattern's name by what it holds
	for bi, br := range bars {
		song.Bars[bi] = make([]string, MusicSlots)
		for part := range RipParts {
			p := &PatternDef{Steps: br.steps, Role: Role(RipPartSlots[part] + 1).String(), Groups: []string{name}}
			sounds := false
			for li, l := range lines {
				if l.part == part {
					evs := events[bi][li]
					slices.SortStableFunc(evs, func(a, b StepDef) int { return a.Pos - b.Pos })
					p.Track = append(p.Track, TrackDef{Instr: l.instr, Desc: l.desc, Event: evs})
					sounds = sounds || len(evs) > 0
				}
			}
			if !sounds {
				continue
			}
			if named[part] == nil {
				named[part] = map[string]string{}
			}
			key := fmt.Sprint(p.Steps, p.Track)
			if p.Name = named[part][key]; p.Name == "" {
				p.Name = fmt.Sprintf("%s_%s_%s", name, RipPartNames[part], letters(len(named[part])))
				named[part][key] = p.Name
				pats = append(pats, p)
			}
			song.Bars[bi][RipPartSlots[part]] = p.Name
		}
	}

	if len(changes) > 0 {
		more := ""
		if len(changes) > 3 {
			changes, more = changes[:3], fmt.Sprintf(" and %d more", len(changes)-3)
		}
		said = append(said, fmt.Sprintf("%d tempo change(s) not kept, the song plays at %d bpm throughout: %s%s", m.Changes, song.BPM, strings.Join(changes, ", "), more))
	}
	if odd {
		said = append(said, fmt.Sprintf("the sequencer counts a bar %d steps until songs carry a meter: a pattern of other steps plays a song bar out of time", StepsPerBar))
	}
	for v := InstrBass; v < InstrumentCount; v++ {
		if len(nearest[v]) > 0 {
			said = append(said, fmt.Sprintf("program(s) %s on the %s, the shipped voice of their family", strings.Join(nearest[v], ", "), v))
		}
	}
	if len(unkit) > 0 {
		keys := slices.Sorted(maps.Keys(unkit))
		words, hits := make([]string, len(keys)), 0
		for i, k := range keys {
			words[i], hits = fmt.Sprintf("%d ×%d", k, unkit[k]), hits+unkit[k]
		}
		said = append(said, fmt.Sprintf("%d hit(s) on keys the kit has no piece for left out: %s", hits, strings.Join(words, ", ")))
	}
	for _, n := range []struct {
		count int
		what  string
	}{
		{left, fmt.Sprintf("part(s) past a pattern's %d tracks left out", MaxPatternTracks)},
		{merged, "note(s) on a step and pitch another holds kept once, the louder"},
		{long, fmt.Sprintf("note(s) longer than %d steps cut to %[1]d", MaxPatternLen)},
		{unheld, "note(s) a pattern's step cannot hold left out"},
	} {
		if n.count > 0 {
			said = append(said, fmt.Sprintf("%d %s", n.count, n.what))
		}
	}
	return pats, song, said
}

// midiLinesOf gives each part its line in lineOf's order, -1 for one left out
func midiLinesOf(parts []midiPart) []int {
	lines := make([]int, len(parts))
	var taken [3]bool
	var rest []int
	for i, p := range parts {
		lines[i] = slices.Index(ripLineNames[:], strings.ToLower(strings.TrimSpace(p.name)))
		if lines[i] >= 0 {
			taken[lines[i]] = true
		} else {
			rest = append(rest, i)
		}
	}
	median := func(i int) int {
		keys := make([]int, len(parts[i].notes))
		for j, n := range parts[i].notes {
			keys[j] = int(n.key)
		}
		slices.Sort(keys)
		return keys[len(keys)/2]
	}
	if len(rest) > 0 && !taken[2] {
		low := slices.MinFunc(rest, func(a, b int) int { return cmp.Compare(median(a), median(b)) })
		if median(low) < RipBassTop {
			lines[low], taken[2] = 2, true
			rest = slices.DeleteFunc(rest, func(i int) bool { return i == low })
		}
	}
	for _, i := range rest {
		if l := slices.Index(taken[:2], false); l >= 0 {
			lines[i], taken[l] = l, true
		}
	}
	return lines
}

// readMIDITrack reads the track chunk numbered track: its notes by channel, each
// part named by the track, and the tempo changes, signatures and program changes
// in it. A note-off ends the earliest note of its channel and pitch still
// sounding; a note still sounding when the track ends ends with it.
func readMIDITrack(b []byte, track int) (midiTrack, error) {
	var t midiTrack
	var name string
	var notes []midiNote
	open := map[[2]byte][]int{} // channel and pitch: the notes sounding, oldest first
	var tick int64
	var running byte
	for i := 0; i < len(b); {
		delta, n := readVLQ(b[i:])
		if n == 0 {
			return t, errors.New("a delta time cut short")
		}
		i += n
		tick += int64(delta)
		if i >= len(b) {
			return t, errors.New("an event cut short")
		}
		status := b[i]
		if status < 0x80 {
			if running == 0 {
				return t, fmt.Errorf("a data byte %#x with no status before it", status)
			}
			status = running
		} else {
			i++
		}
		switch {
		case status == 0xFF || status == 0xF0 || status == 0xF7:
			kind := byte(0)
			if status == 0xFF {
				if i >= len(b) {
					return t, errors.New("a meta event cut short")
				}
				kind, i = b[i], i+1
			}
			size, n := readVLQ(b[i:])
			if n == 0 || size > len(b)-i-n {
				return t, errors.New("a meta or system exclusive event cut short")
			}
			body := b[i+n : i+n+size]
			i += n + size
			running = 0
			switch {
			case status != 0xFF:
			case kind == 0x03 && name == "":
				name = string(body)
			case kind == 0x51 && size == 3:
				t.tempos = append(t.tempos, [2]int64{tick, int64(body[0])<<16 | int64(body[1])<<8 | int64(body[2])})
			case kind == 0x58 && size >= 2 && body[0] > 0 && body[1] < 8:
				t.meters = append(t.meters, midiMeter{tick: tick, num: int(body[0]), den: 1 << body[1]})
			case kind == 0x59 && size == 2 && int8(body[0]) >= -7 && int8(body[0]) <= 7:
				t.keys = append(t.keys, midiKey{tick: tick, sf: int(int8(body[0])), minor: body[1] == 1})
			case kind == 0x2F:
				i = len(b)
			}
		case status >= 0xF0:
			return t, fmt.Errorf("status %#x in a file", status)
		default:
			running = status
			kind, width := status&0xF0, 2
			if kind == 0xC0 || kind == 0xD0 {
				width = 1
			}
			if i+width > len(b) {
				return t, errors.New("a channel message cut short")
			}
			ch, key, vel := status&0x0F, b[i]&0x7F, b[i+width-1]&0x7F
			i += width
			at := [2]byte{ch, key}
			switch {
			case kind == 0x90 && vel > 0:
				open[at] = append(open[at], len(notes))
				notes = append(notes, midiNote{on: tick, off: -1, ch: ch, key: key, vel: vel})
			case kind == 0x80 || kind == 0x90:
				if q := open[at]; len(q) > 0 {
					notes[q[0]].off, open[at] = tick, q[1:]
				}
			case kind == 0xC0:
				t.programs = append(t.programs, midiChange{tick: tick, ch: ch, program: key})
			}
		}
	}
	of := map[byte]int{}
	for _, n := range notes {
		if n.off < 0 {
			n.off = tick
		}
		p, ok := of[n.ch]
		if !ok {
			p, of[n.ch] = len(t.parts), len(t.parts)
			t.parts = append(t.parts, midiPart{name: name, track: track})
		}
		t.parts[p].notes = append(t.parts[p].notes, n)
	}
	return t, nil
}

// readVLQ reads a variable-length quantity: its value and its bytes, none when
// it is cut short or runs past the four bytes a file allows
func readVLQ(b []byte) (int, int) {
	v := 0
	for i := 0; i < len(b) && i < 4; i++ {
		v = v<<7 | int(b[i]&0x7F)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// appendVLQ appends v as a variable-length quantity
func appendVLQ(b []byte, v int64) []byte {
	var tmp [5]byte
	i := len(tmp) - 1
	tmp[i] = byte(v & 0x7F)
	for v >>= 7; v > 0; v >>= 7 {
		i--
		tmp[i] = byte(v&0x7F) | 0x80
	}
	return append(b, tmp[i:]...)
}

// midiEvent is an event a writer puts in a track: its tick, its place among the
// events of one tick (meta, program, note-off, note-on) and its bytes
type midiEvent struct {
	tick int64
	rank int
	data []byte
}

func midiMeta(tick int64, kind byte, body ...byte) midiEvent {
	return midiEvent{tick: tick, data: append(appendVLQ([]byte{0xFF, kind}, int64(len(body))), body...)}
}

func midiTempoAt(bpm float64) midiEvent {
	us := int(math.Round(60e6 / bpm))
	return midiMeta(0, 0x51, byte(us>>16), byte(us>>8), byte(us))
}

func midiNoteOn(tick int64, ch, key, vel byte) midiEvent {
	return midiEvent{tick: tick, rank: 3, data: []byte{0x90 | ch, key, vel}}
}

func midiNoteOff(tick int64, ch, key byte) midiEvent {
	return midiEvent{tick: tick, rank: 2, data: []byte{0x80 | ch, key, 0}}
}

func midiProgramAt(ch, program byte) midiEvent {
	return midiEvent{rank: 1, data: []byte{0xC0 | ch, program}}
}

// midiVel is a velocity of 0 to 1 as a note-on's, never the 0 that ends a note
func midiVel(v float64) byte { return byte(min(max(math.Round(v*127), 1), 127)) }

// writeSMF writes tracks as a Standard MIDI File of type 1, midiDivision ticks a
// quarter, each track's events in time and the track ended at its last
func writeSMF(w io.Writer, tracks [][]midiEvent) error {
	be := binary.BigEndian
	out := be.AppendUint16(append([]byte("MThd"), 0, 0, 0, 6, 0, 1), uint16(len(tracks)))
	out = be.AppendUint16(out, midiDivision)
	for _, events := range tracks {
		slices.SortStableFunc(events, func(a, b midiEvent) int { return cmp.Or(cmp.Compare(a.tick, b.tick), cmp.Compare(a.rank, b.rank)) })
		var trk []byte
		var at int64
		for _, e := range events {
			trk = append(appendVLQ(trk, e.tick-at), e.data...)
			at = e.tick
		}
		trk = append(trk, 0, 0xFF, 0x2F, 0)
		out = be.AppendUint32(append(out, "MTrk"...), uint32(len(trk)))
		out = append(out, trk...)
	}
	_, err := w.Write(out)
	return err
}

// WriteMIDI writes a transcript as a Standard MIDI File of type 1 at bpm: its
// lines as tracks named melody, under and bass on channels 1 to 3 and the lead's,
// lead's and bass's programs, the tempo on the first, its hits as a track named
// drums on the drum channel, a sixteenth each. A note lasts a tick at least;
// ReadMIDI reads it back within half a tick.
func WriteMIDI(w io.Writer, notes []RipNote, hits []RipHit, bpm int) error {
	if bpm <= 0 {
		return fmt.Errorf("midi: tempo %d bpm", bpm)
	}
	var tracks [4][]midiEvent // melody, under, bass, drums
	for l, voice := range [3]InstrumentType{InstrLead, InstrLead, InstrBass} {
		tracks[l] = []midiEvent{midiMeta(0, 0x03, []byte(ripLineNames[l])...), midiProgramAt(byte(l), midiInstruments[voice].program)}
	}
	tracks[0] = append(tracks[0], midiTempoAt(float64(bpm)))
	tracks[3] = []midiEvent{midiMeta(0, 0x03, []byte("drums")...)}
	perSec := float64(bpm) / 60 * midiDivision
	for _, n := range notes {
		if n.MIDI < 0 || n.MIDI > 127 || n.Start < 0 {
			return fmt.Errorf("midi: a note at %.3fs of pitch %d: want a pitch 0 to 127 at or after 0s", n.Start, n.MIDI)
		}
		on := int64(math.Round(n.Start * perSec))
		off := max(int64(math.Round(n.End*perSec)), on+1)
		l := lineOf(n)
		tracks[l] = append(tracks[l], midiNoteOn(on, byte(l), byte(n.MIDI), midiVel(n.Vel)), midiNoteOff(off, byte(l), byte(n.MIDI)))
	}
	for _, h := range hits {
		if h.Drum < 0 || h.Drum >= InstrClap || h.Start < 0 {
			return fmt.Errorf("midi: a hit at %.3fs of drum %d: want a kick, snare or hihat at or after 0s", h.Start, h.Drum)
		}
		on, key := int64(math.Round(h.Start*perSec)), midiInstruments[h.Drum].keys[0]
		tracks[3] = append(tracks[3], midiNoteOn(on, midiDrums, key, midiVel(h.Vel)), midiNoteOff(on+midiDivision/StepsPerBeat, midiDrums, key))
	}
	return writeSMF(w, tracks[:3+min(len(hits), 1)])
}

// barSteps is the bar a pattern of steps is written in: sixteen steps, else
// three beats, else the pattern whole
func barSteps(steps int) int {
	switch {
	case steps%StepsPerBar == 0:
		return StepsPerBar
	case steps%midiThreeBeats == 0:
		return midiThreeBeats
	}
	return steps
}

// timeSignature is a bar of steps as a time signature's bytes: in quarters
// where they divide it, else in eighths, else in sixteenths
func timeSignature(steps int) []byte {
	num, den, log := steps, 16, byte(4)
	for num%2 == 0 && den > 4 {
		num, den, log = num/2, den/2, log-1
	}
	return []byte{byte(num), log, byte(96 / den), 8}
}

// keySignature is a key as a key signature's bytes: a major scale as its own,
// every other as the minor on its root
func keySignature(root int, scale ScaleID) []byte {
	tonic, minor := root%12, byte(0)
	if scale != ScaleMajor {
		tonic, minor = (tonic+3)%12, 1 // the relative major's sharps
	}
	sf := tonic * 7 % 12 // a fifth a sharp
	if sf > 6 {
		sf -= 12
	}
	return []byte{byte(int8(sf)), minor}
}

// of is the key's root, in the octave the engine's harmony root lives in, and
// of the scales on it the one whose tones hold the most of a chroma c and whose
// others the least, the signature's own on a tie
func (k midiKey) of(c [12]float64) (int, ScaleID) {
	tonic, scale := (k.sf*7%12+12)%12, ScaleMajor
	if k.minor {
		tonic, scale = (tonic+9)%12, ScaleMinor
	}
	best := math.Inf(-1)
	for _, s := range []ScaleID{scale, ScaleMajor, ScaleMinor, ScaleDorian, ScalePhrygian, ScaleHarmonicMinor, ScaleMinorPent} {
		var in [12]bool
		for _, iv := range scaleTable[s] {
			in[(tonic+iv)%12] = true
		}
		var score float64
		for pc, v := range c {
			if in[pc] {
				score += v
			} else {
				score -= v
			}
		}
		if score > best {
			best, scale = score, s
		}
	}
	return tonic + 3*12, scale
}

// WritePatternMIDI writes a pattern as WriteSongMIDI writes a song that plays
// it once through, at bpm in the key and chords given
func WritePatternMIDI(w io.Writer, p *PatternDef, bpm, root int, scale ScaleID, chords []int) error {
	slot := 1
	if r := slices.Index(roleNames[:], p.Role); r > int(RoleNone) {
		slot = Role(r).slot()
	}
	d := &SongDef{Name: p.Name, BPM: bpm, Root: root, Scale: ScaleName(scale), Chords: chords}
	for range max(p.Steps/barSteps(p.Steps), 1) {
		bar := make([]string, MusicSlots)
		bar[slot] = p.Name
		d.Bars = append(d.Bars, bar)
	}
	return WriteSongMIDI(w, d, func(string) *PatternDef { return p })
}

// WriteSongMIDI writes a song as a Standard MIDI File of type 1: a track of its
// tempo, key and meters, a bar as long as the longest bar its patterns are
// written in (barSteps), then a track a pattern track by slot, instrument and
// order, a slot's pattern playing at the song's step: a voice on its program
// (midiProgram), a drum on the drum channel, each note as long as its step.
func WriteSongMIDI(w io.Writer, d *SongDef, pattern func(string) *PatternDef) error {
	if err := ValidateSong(d); err != nil {
		return err
	}
	pats := map[string]*Pattern{}
	for _, bar := range d.Bars {
		for _, n := range bar {
			if n == "" || pats[n] != nil {
				continue
			}
			def := pattern(n)
			if def == nil {
				return fmt.Errorf("song %q: %q is no pattern", d.Name, n)
			}
			p, err := def.Pattern()
			if err != nil {
				return err
			}
			pats[n] = p
		}
	}
	const perStep = midiDivision / StepsPerBeat
	conductor := []midiEvent{midiMeta(0, 0x03, []byte(d.Name)...), midiTempoAt(float64(d.BPM)), midiMeta(0, 0x59, keySignature(d.Root, d.ScaleID())...)}
	type lane struct {
		slot  int
		instr InstrumentType
		nth   int // of the tracks of its instrument in its pattern
	}
	var lanes []lane
	events := map[lane][]midiEvent{}
	held := map[lane]map[byte]int{} // a lane's last note-on of each key
	channels := map[byte]byte{}     // a program's channel, the drums' kept apart
	h := newHarmony()
	h.set(d.Root, d.ScaleID(), d.Chords)
	at, meter := 0, 0
	for b, bar := range d.Bars {
		if b > 0 {
			h.advanceBar()
		}
		steps := 0
		for _, n := range bar {
			if p := pats[n]; p != nil {
				steps = max(steps, barSteps(p.Steps))
			}
		}
		steps = cmp.Or(steps, StepsPerBar)
		if steps != meter {
			conductor = append(conductor, midiMeta(int64(at*perStep), 0x58, timeSignature(steps)...))
			meter = steps
		}
		for slot, n := range bar {
			p := pats[n]
			if p == nil {
				continue
			}
			nth := map[InstrumentType]int{}
			for _, tr := range p.Tracks {
				ln := lane{slot, tr.Instr, nth[tr.Instr]}
				nth[tr.Instr]++
				ch, program := byte(midiDrums), byte(0)
				if !tr.Instr.IsDrum() {
					program = midiProgram(tr.Instr)
					c, ok := channels[program]
					if !ok {
						c = byte(len(channels))
						if c >= midiDrums {
							c++
						}
						channels[program] = c
					}
					ch = c
				}
				evs, ok := events[ln]
				if !ok {
					lanes = append(lanes, ln)
					evs = []midiEvent{midiMeta(0, 0x03, []byte(cmp.Or(tr.Desc, InstrumentName(tr.Instr)))...)}
					if ch != midiDrums {
						evs = append(evs, midiProgramAt(ch, program))
					}
				}
				if held[ln] == nil {
					held[ln] = map[byte]int{}
				}
				for j := range steps {
					local, tick := (at+j)%p.Steps, int64((at+j)*perStep)
					for _, e := range tr.Events {
						if e.Pos != local || e.Vel <= 0 {
							continue
						}
						var key byte
						off := tick + int64(max(e.Dur, 1)*perStep)
						if ch == midiDrums {
							key, off = midiInstruments[tr.Instr].keys[0], tick+perStep
						} else {
							key = byte(h.resolve(e.Deg, e.Oct, e.Acc, tr.FollowChord))
						}
						// a key sounding again ends its note before, as a reader pairs a
						// note-off with the earliest of its key; on one tick, once, the louder
						if i, ok := held[ln][key]; ok && evs[i+1].tick > tick {
							if evs[i].tick == tick {
								evs[i].data[2], evs[i+1].tick = max(evs[i].data[2], midiVel(e.Vel)), max(evs[i+1].tick, off)
								continue
							}
							evs[i+1].tick = tick
						}
						held[ln][key] = len(evs)
						evs = append(evs, midiNoteOn(tick, ch, key, midiVel(e.Vel)), midiNoteOff(off, ch, key))
					}
				}
				events[ln] = evs
			}
		}
		at += steps
	}
	slices.SortStableFunc(lanes, func(a, b lane) int { return cmp.Compare(a.slot, b.slot) })
	tracks := [][]midiEvent{conductor}
	for _, ln := range lanes {
		tracks = append(tracks, events[ln])
	}
	return writeSMF(w, tracks)
}
