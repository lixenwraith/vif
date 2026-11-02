package audio

import (
	"math"
	"slices"
)

// LeadRule names which of the melody register's two lines a rip calls the
// melody, once the notes are parted into lines: the busier line by default,
// the one with the most notes for its sounding time, since the ear follows the
// line that moves while a line of held notes accompanies it; or the higher,
// lower, louder, softer, longer or shorter in its notes when the listener
// knows the piece, as when a melody of held notes sings over an accompaniment
// of repeated eighths.
type LeadRule int

const (
	LeadBusy LeadRule = iota // the line with more notes a second of its sounding, the one that moves
	LeadHigh                 // the line whose notes sit higher on average
	LeadLow
	LeadLoud // the line whose notes are stronger on average
	LeadSoft
	LeadLong // the line whose notes last longer on average
	LeadShort
)

var leadWords = [...]string{"busy", "high", "low", "loud", "soft", "long", "short"}

func (l LeadRule) String() string {
	if l < 0 || int(l) >= len(leadWords) {
		return "busy"
	}
	return leadWords[l]
}

// ParseLeadRule reads a rule's word
func ParseLeadRule(s string) (LeadRule, bool) {
	i := slices.Index(leadWords[:], s)
	return LeadRule(max(i, 0)), i >= 0
}

// The costs a stream pays, each about one for a clear difference: an octave
// between a note and the last of its stream, a note four times as long or as
// short, a change of strength of two thirds, a note that begins while the
// stream's last still sounds for more than half of the shorter, a change of
// stream between two notes in a row that do not sound together, and a
// stream's first note.
const (
	streamJump    = 1.0
	streamLength  = 1.0
	streamLevel   = 1.5
	streamOverlap = 10.0
	streamSwitch  = 0.25
	streamStart   = 0.5
	streamMemory  = 48 // how many notes back the other stream's last note is kept
)

// streams parts a register's notes, the two decoded lines' together, into the
// two lines by what a line keeps: its pitch from note to note, the length of
// its notes, their strength, and no two notes at once. A decoder follows the
// louder line through a crossing or a rest, so the lead, when it is the softer
// line, changes hands with it; its notes keep their own character, which this
// pass follows instead, by a path over the notes in time order where each joins
// the stream of the note before it or the other stream. Notes that never sound
// together are one line. The lead is then the stream the rule names, its notes
// the first line and the rest the second.
func streams(notes []ripNote, hop float64, lead LeadRule) []ripNote {
	slices.SortFunc(notes, func(a, b ripNote) int { return a.start - b.start })
	n := len(notes)
	if n == 0 {
		return notes
	}
	sec := func(frames int) float64 { return float64(frames) * hop }
	together := func(i, j int) bool {
		a, b := notes[i], notes[j]
		over := min(a.end, b.end) - max(a.start, b.start)
		return 2*over > min(a.end-a.start, b.end-b.start)
	}
	one := true
	for k := 1; k < n && one; k++ {
		for i := k - 1; i >= 0 && notes[i].end > notes[k].start; i-- {
			if together(i, k) {
				one = false
				break
			}
		}
	}
	if one {
		for k := range notes {
			notes[k].second = false
		}
		return notes
	}
	cost := func(i, j int) float64 { // note j joining the stream whose last note is i
		a, b := notes[i], notes[j]
		gap := max(sec(b.start-a.end), 0)
		c := streamJump * math.Min(math.Abs(float64(b.midi-a.midi)), 12) / 12 * math.Exp(-gap)
		c += streamLength * math.Min(math.Abs(math.Log(float64(max(b.end-b.start, 1))/float64(max(a.end-a.start, 1))))/math.Log(4), 1)
		c += streamLevel * math.Abs(b.strength-a.strength)
		if together(i, j) {
			c += streamOverlap
		}
		return c
	}
	// state after note k: the index of the other stream's last note, -1 for
	// none; note k is the last of its own. best holds the cheapest cost of each
	// state and from where it came, as the other stream's last note before k and
	// whether k joined the stream of k-1.
	type arrival struct {
		cost float64
		from int
		same bool
	}
	states := make([]map[int]arrival, n)
	states[0] = map[int]arrival{-1: {cost: streamStart, from: -1, same: true}}
	for k := 1; k < n; k++ {
		states[k] = map[int]arrival{}
		put := func(other int, a arrival) {
			if cur, ok := states[k][other]; !ok || a.cost < cur.cost {
				states[k][other] = a
			}
		}
		for other, prev := range states[k-1] {
			// k joins the stream of k-1
			put(other, arrival{cost: prev.cost + cost(k-1, k), from: other, same: true})
			// k joins the other stream, started here or continued from other;
			// the change costs nothing where k sounds together with k-1
			c := prev.cost
			if !together(k-1, k) {
				c += streamSwitch
			}
			if other < 0 || k-1-other > streamMemory { // no note, or one long silent and forgotten
				c += streamStart
			} else {
				c += cost(other, k)
			}
			put(k-1, arrival{cost: c, from: other, same: false})
		}
	}
	// the cheapest end, walked back into stream numbers
	best, at := math.Inf(1), -1
	for other, a := range states[n-1] {
		if a.cost < best {
			best, at = a.cost, other
		}
	}
	same := make([]bool, n)
	for k := n - 1; k >= 1; k-- {
		a := states[k][at]
		same[k] = a.same
		at = a.from
	}
	stream := make([]int, n)
	for k := 1; k < n; k++ {
		stream[k] = stream[k-1]
		if !same[k] {
			stream[k] = 1 - stream[k-1]
		}
	}
	// which stream is the lead
	var count, strength, pitch, sounding [2]float64
	for k, s := range stream {
		count[s]++
		strength[s] += notes[k].strength
		pitch[s] += float64(notes[k].midi)
		sounding[s] += sec(notes[k].end - notes[k].start)
	}
	if count[1] == 0 {
		return notes
	}
	score := func(s int) float64 {
		switch lead {
		case LeadHigh:
			return pitch[s] / count[s]
		case LeadLow:
			return -pitch[s] / count[s]
		case LeadLoud:
			return strength[s] / count[s]
		case LeadSoft:
			return -strength[s] / count[s]
		case LeadLong:
			return sounding[s] / count[s]
		case LeadShort:
			return -sounding[s] / count[s]
		}
		return count[s] / math.Max(sounding[s], hop)
	}
	leadStream := 0
	if score(1) > score(0) {
		leadStream = 1
	}
	for k := range notes {
		notes[k].second = stream[k] != leadStream
	}
	return notes
}
