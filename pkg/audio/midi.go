package audio

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
)

// midiDivision is the ticks per quarter note WriteMIDI writes, a tick under
// two milliseconds at any tempo the sequencer plays
const midiDivision = 480

const (
	midiDrums        = 9      // the channel General MIDI keeps for percussion, which has no pitch
	midiDefaultTempo = 500000 // microseconds a quarter note before a file's first tempo, 120 bpm
)

// midiDrumKeys are the General MIDI keys each drum the rip hears is read from,
// the first the one written
var midiDrumKeys = [InstrClap][]byte{InstrKick: {36, 35}, InstrSnare: {38, 40}, InstrHihat: {42, 44, 46}}

// midiNote is a note of a track in ticks, before the tempo map
type midiNote struct {
	on, off      int64
	ch, key, vel byte
}

// midiPart is one track's notes on one channel: what ReadMIDI maps to a line
type midiPart struct {
	name  string
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

// MIDIScore is a Standard MIDI File as ReadMIDI reads it: the transcript in
// seconds under its tempo map, and what of its grid a rip needs.
type MIDIScore struct {
	Notes   []RipNote
	Hits    []RipHit
	BPM     float64 // quarter notes a minute at the start, 120 when the file names none
	Changes int     // tempo changes after the start
	Meter   [2]int  // the time signature the music starts in, 4/4 when the file names none
	Left    int     // parts past the three lines, left out

	clock     []midiTempo
	division  int
	line, end int64 // the tick that time signature starts on, and the last note's end
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

// ReadMIDI reads a Standard MIDI File of type 0 or 1 as a transcript in seconds
// under its tempo map, the drum channel's keys midiDrumKeys names as hits. A
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
	var parts []midiPart
	var changes [][2]int64 // tick, microseconds a quarter note
	var meters []midiMeter
	for off, read := 8+size, 0; off+8 <= len(data) && read < tracks; {
		id, length := string(data[off:off+4]), int(be.Uint32(data[off+4:]))
		body := data[off+8:]
		length = min(length, len(body))
		off += 8 + length
		if id != "MTrk" {
			continue
		}
		ps, cs, ms, err := readMIDITrack(body[:length])
		if err != nil {
			return nil, fmt.Errorf("midi: track %d: %w", read+1, err)
		}
		parts, changes, meters = append(parts, ps...), append(changes, cs...), append(meters, ms...)
		read++
	}
	slices.SortStableFunc(changes, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	m := &MIDIScore{division: division, clock: []midiTempo{{perTick: midiDefaultTempo / 1e6 / float64(division)}}}
	for _, c := range changes {
		last := m.clock[len(m.clock)-1]
		perTick := float64(c[1]) / 1e6 / float64(division)
		if c[0] > 0 && perTick != last.perTick {
			m.Changes++
		}
		m.clock = append(m.clock, midiTempo{tick: c[0], sec: last.sec + float64(c[0]-last.tick)*last.perTick, perTick: perTick})
	}
	start := m.clock[sort.Search(len(m.clock), func(i int) bool { return m.clock[i].tick > 0 })-1]
	m.BPM = 60 / start.perTick / float64(division)
	m.line, m.Meter = firstBar(meters, division)
	seconds := func(tick int64) float64 { return m.seconds(float64(tick)) }
	var tonal []midiPart
	for _, p := range parts {
		for _, n := range p.notes {
			m.end = max(m.end, n.off, n.on+1)
		}
		if p.notes[0].ch != midiDrums {
			tonal = append(tonal, p)
			continue
		}
		for _, n := range p.notes {
			if d := slices.IndexFunc(midiDrumKeys[:], func(keys []byte) bool { return slices.Contains(keys, n.key) }); d >= 0 {
				m.Hits = append(m.Hits, RipHit{Start: seconds(n.on), Drum: InstrumentType(d), Vel: float64(n.vel) / 127})
			}
		}
	}
	for i, line := range midiLinesOf(tonal) {
		if line < 0 {
			m.Left++
			continue
		}
		for _, n := range tonal[i].notes {
			m.Notes = append(m.Notes, RipNote{Start: seconds(n.on), End: seconds(n.off), MIDI: int(n.key), Vel: float64(n.vel) / 127, Bass: line == 2, Second: line == 1, Bright: RipUnmeasured})
		}
	}
	if len(m.Notes) == 0 && len(m.Hits) == 0 {
		return nil, errors.New("midi: no notes, and no drums of the keys General MIDI names")
	}
	slices.SortStableFunc(m.Notes, func(a, b RipNote) int { return cmpFloat(a.Start, b.Start) })
	slices.SortStableFunc(m.Hits, func(a, b RipHit) int { return cmpFloat(a.Start, b.Start) })
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

// Rip is the score as a rip of it: its notes the transcript, quantized as a
// rip's are on the score's own time, its ticks; the tempo it starts with, an
// octave faster or slower until the sequencer plays it; the beats its own from
// where its time signature starts, four a bar, a pickup bar for notes before it;
// and the key keyOf its notes' pitch classes by their lengths. A note outside
// the rip's range is dropped, and counted.
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

// readMIDITrack reads one track chunk: its notes by channel, each named by the
// track, and the tempo changes and time signatures in it. A note-off ends the
// earliest note of its channel and pitch still sounding; a note still sounding
// when the track ends ends with it.
func readMIDITrack(b []byte) ([]midiPart, [][2]int64, []midiMeter, error) {
	var name string
	var notes []midiNote
	var changes [][2]int64
	var meters []midiMeter
	open := map[[2]byte][]int{} // channel and pitch: the notes sounding, oldest first
	var tick int64
	var running byte
	for i := 0; i < len(b); {
		delta, n := readVLQ(b[i:])
		if n == 0 {
			return nil, nil, nil, errors.New("a delta time cut short")
		}
		i += n
		tick += int64(delta)
		if i >= len(b) {
			return nil, nil, nil, errors.New("an event cut short")
		}
		status := b[i]
		if status < 0x80 {
			if running == 0 {
				return nil, nil, nil, fmt.Errorf("a data byte %#x with no status before it", status)
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
					return nil, nil, nil, errors.New("a meta event cut short")
				}
				kind, i = b[i], i+1
			}
			size, n := readVLQ(b[i:])
			if n == 0 || size > len(b)-i-n {
				return nil, nil, nil, errors.New("a meta or system exclusive event cut short")
			}
			body := b[i+n : i+n+size]
			i += n + size
			running = 0
			switch {
			case status != 0xFF:
			case kind == 0x03 && name == "":
				name = string(body)
			case kind == 0x51 && size == 3:
				changes = append(changes, [2]int64{tick, int64(body[0])<<16 | int64(body[1])<<8 | int64(body[2])})
			case kind == 0x58 && size >= 2 && body[0] > 0 && body[1] < 8:
				meters = append(meters, midiMeter{tick: tick, num: int(body[0]), den: 1 << body[1]})
			case kind == 0x2F:
				i = len(b)
			}
		case status >= 0xF0:
			return nil, nil, nil, fmt.Errorf("status %#x in a file", status)
		default:
			running = status
			kind, width := status&0xF0, 2
			if kind == 0xC0 || kind == 0xD0 {
				width = 1
			}
			if i+width > len(b) {
				return nil, nil, nil, errors.New("a channel message cut short")
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
			}
		}
	}
	var parts []midiPart
	of := map[byte]int{}
	for _, n := range notes {
		if n.off < 0 {
			n.off = tick
		}
		p, ok := of[n.ch]
		if !ok {
			p, of[n.ch] = len(parts), len(parts)
			parts = append(parts, midiPart{name: name})
		}
		parts[p].notes = append(parts[p].notes, n)
	}
	return parts, changes, meters, nil
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

// WriteMIDI writes a transcript as a Standard MIDI File of type 1 at bpm: its
// lines as three tracks named melody, under and bass on channels 1 to 3, the
// tempo on the first, and its hits, when it has any, as a track named drums on
// the drum channel, a sixteenth each; every note is at least a tick long.
// ReadMIDI reads it back within half a tick.
func WriteMIDI(w io.Writer, notes []RipNote, hits []RipHit, bpm int) error {
	if bpm <= 0 {
		return fmt.Errorf("midi: tempo %d bpm", bpm)
	}
	type event struct {
		tick     int64
		off      bool
		key, vel byte
	}
	var lines [4][]event // melody, under, bass, drums
	perSec := float64(bpm) / 60 * midiDivision
	for _, n := range notes {
		if n.MIDI < 0 || n.MIDI > 127 || n.Start < 0 {
			return fmt.Errorf("midi: a note at %.3fs of pitch %d: want a pitch 0 to 127 at or after 0s", n.Start, n.MIDI)
		}
		on := int64(math.Round(n.Start * perSec))
		off := max(int64(math.Round(n.End*perSec)), on+1)
		vel := byte(min(max(math.Round(n.Vel*127), 1), 127))
		l := lineOf(n)
		lines[l] = append(lines[l], event{tick: on, key: byte(n.MIDI), vel: vel}, event{tick: off, off: true, key: byte(n.MIDI)})
	}
	for _, h := range hits {
		if h.Drum < 0 || h.Drum >= InstrClap || h.Start < 0 {
			return fmt.Errorf("midi: a hit at %.3fs of drum %d: want a kick, snare or hihat at or after 0s", h.Start, h.Drum)
		}
		on, key := int64(math.Round(h.Start*perSec)), midiDrumKeys[h.Drum][0]
		vel := byte(min(max(math.Round(h.Vel*127), 1), 127))
		lines[3] = append(lines[3], event{tick: on, key: key, vel: vel}, event{tick: on + midiDivision/StepsPerBeat, off: true, key: key})
	}
	tracks := lines[:3+min(len(hits), 1)]
	be := binary.BigEndian
	out := append([]byte("MThd"), 0, 0, 0, 6, 0, 1, 0, byte(len(tracks)))
	out = be.AppendUint16(out, midiDivision)
	for l, events := range tracks {
		// a note-off first where one note ends as the next of its pitch starts
		slices.SortStableFunc(events, func(a, b event) int {
			if c := cmp.Compare(a.tick, b.tick); c != 0 || a.off == b.off {
				return c
			}
			if a.off {
				return -1
			}
			return 1
		})
		name, ch := "drums", byte(midiDrums)
		if l < len(ripLineNames) {
			name, ch = ripLineNames[l], byte(l)
		}
		trk := append([]byte{0, 0xFF, 0x03, byte(len(name))}, name...)
		if l == 0 {
			us := int(math.Round(60e6 / float64(bpm)))
			trk = append(trk, 0, 0xFF, 0x51, 3, byte(us>>16), byte(us>>8), byte(us))
		}
		var at int64
		for _, e := range events {
			trk = appendVLQ(trk, e.tick-at)
			at = e.tick
			status := byte(0x90)
			if e.off {
				status = 0x80
			}
			trk = append(trk, status|ch, e.key, e.vel)
		}
		trk = append(trk, 0, 0xFF, 0x2F, 0)
		out = be.AppendUint32(append(out, "MTrk"...), uint32(len(trk)))
		out = append(out, trk...)
	}
	_, err := w.Write(out)
	return err
}
