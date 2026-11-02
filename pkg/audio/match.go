package audio

import (
	"bufio"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Agreement is how two transcripts of one piece agree: each line's notes of one
// aligned to the other's in order, the same pitch under one transposition, so a
// tempo that drifts or a phrase played freer does not break the match, the
// melody register also as a whole in case the two part its lines differently,
// and the time map the matched notes fit, a's seconds as a line of b's.
type Agreement struct {
	Shift           int // semitones added to b's pitches to match a's
	Lines           [3]LineAgreement
	Register        LineAgreement // the melody register, both lines as one
	Stretch, Offset float64       // a's time of a matched note ≈ Stretch*b's + Offset
}

// LineAgreement is one line's count: the notes each side has, how many align,
// and the notes only one side has
type LineAgreement struct {
	A, B, Matched int
	OnlyA, OnlyB  []RipNote
}

// lineOf is a note's line in ripLines' order: melody, under, bass
func lineOf(n RipNote) int {
	switch {
	case n.Bass:
		return 2
	case n.Second:
		return 1
	}
	return 0
}

// MatchNotes aligns two transcripts, trying every transposition within an
// octave and keeping the one that matches most notes, nearer to none when two
// tie. Within a line the alignment is the longest common subsequence of the
// pitches in time order.
func MatchNotes(a, b []RipNote) Agreement {
	a, b = slices.Clone(a), slices.Clone(b)
	byStart := func(x, y RipNote) int { return cmpFloat(x.Start, y.Start) }
	slices.SortFunc(a, byStart)
	slices.SortFunc(b, byStart)
	best := Agreement{}
	total := -1
	for d := 0; d <= 12; d++ {
		for _, shift := range []int{d, -d} {
			ag := align(a, b, shift)
			n := ag.Lines[0].Matched + ag.Lines[1].Matched + ag.Lines[2].Matched
			if n > total {
				best, total = ag, n
			}
			if d == 0 {
				break
			}
		}
	}
	return best
}

func align(a, b []RipNote, shift int) Agreement {
	ag := Agreement{Shift: shift, Stretch: 1}
	var xs, ys []float64
	part := func(keep func(RipNote) bool) LineAgreement {
		var la, lb []RipNote
		for _, n := range a {
			if keep(n) {
				la = append(la, n)
			}
		}
		for _, n := range b {
			if keep(n) {
				lb = append(lb, n)
			}
		}
		pairs := subsequence(la, lb, shift)
		l := LineAgreement{A: len(la), B: len(lb), Matched: len(pairs)}
		inA, inB := make([]bool, len(la)), make([]bool, len(lb))
		for _, p := range pairs {
			inA[p[0]], inB[p[1]] = true, true
			xs, ys = append(xs, lb[p[1]].Start), append(ys, la[p[0]].Start)
		}
		for i, n := range la {
			if !inA[i] {
				l.OnlyA = append(l.OnlyA, n)
			}
		}
		for i, n := range lb {
			if !inB[i] {
				l.OnlyB = append(l.OnlyB, n)
			}
		}
		return l
	}
	for line := range ag.Lines {
		ag.Lines[line] = part(func(n RipNote) bool { return lineOf(n) == line })
	}
	ag.Stretch, ag.Offset = fitLine(xs, ys)
	ag.Register = part(func(n RipNote) bool { return !n.Bass })
	return ag
}

// subsequence is the longest common subsequence of two lines' pitches, b's
// shifted, as index pairs in order
func subsequence(a, b []RipNote, shift int) [][2]int {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil
	}
	l := make([]int32, (n+1)*(m+1))
	at := func(i, j int) *int32 { return &l[i*(m+1)+j] }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i].MIDI == b[j].MIDI+shift {
				*at(i, j) = *at(i+1, j+1) + 1
			} else {
				*at(i, j) = max(*at(i+1, j), *at(i, j+1))
			}
		}
	}
	var pairs [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i].MIDI == b[j].MIDI+shift:
			pairs = append(pairs, [2]int{i, j})
			i, j = i+1, j+1
		case *at(i+1, j) >= *at(i, j+1):
			i++
		default:
			j++
		}
	}
	return pairs
}

// fitLine is the least-squares line y ≈ stretch*x + offset, one and zero
// without two points
func fitLine(xs, ys []float64) (stretch, offset float64) {
	n := float64(len(xs))
	if n < 2 {
		return 1, 0
	}
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx, sy, sxx, sxy = sx+xs[i], sy+ys[i], sxx+xs[i]*xs[i], sxy+xs[i]*ys[i]
	}
	den := n*sxx - sx*sx
	if den <= 0 {
		return 1, 0
	}
	stretch = (n*sxy - sx*sy) / den
	return stretch, (sy - stretch*sx) / n
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// transcriptLine reads one note of a transcript as the notes verb prints it, or
// in short: the line, the start in seconds, then anywhere after, the pitch as a
// note name and a length in seconds or in steps
var transcriptLine = regexp.MustCompile(`^\s*(melody|under|bass)\s+([0-9.]+)s\b(.*)$`)
var transcriptPitch = regexp.MustCompile(`\b([A-G][#b]?)(-?[0-9])\b`)
var transcriptLength = regexp.MustCompile(`\b([0-9.]+)\s*(s\b|step)`)
var transcriptTempo = regexp.MustCompile(`\bat ([0-9]+) bpm\b`)

// ParseTranscript reads notes from text in the form the notes verb prints,
// which a listener can correct by hand: a header "... at N bpm ..." sets the
// tempo lengths in steps are read by, and each note is "<line> <start>s ...
// <pitch> ... <length> step(s)" or "<line> <start>s <pitch> <length>s", the
// rest of a line ignored. A note's line is melody, under or bass.
func ParseTranscript(text string) ([]RipNote, error) {
	var notes []RipNote
	step := 0.0
	sc := bufio.NewScanner(strings.NewReader(text))
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := sc.Text()
		if m := transcriptTempo.FindStringSubmatch(line); m != nil && step == 0 {
			bpm, _ := strconv.Atoi(m[1])
			if bpm > 0 {
				step = 60 / float64(bpm) / StepsPerBeat
			}
		}
		m := transcriptLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		start, _ := strconv.ParseFloat(m[2], 64)
		pm := transcriptPitch.FindStringSubmatch(m[3])
		if pm == nil {
			return nil, fmt.Errorf("line %d: no pitch", lineNo)
		}
		midi, ok := parseNoteName(pm[1], pm[2])
		if !ok {
			return nil, fmt.Errorf("line %d: pitch %q", lineNo, pm[0])
		}
		dur := 0.1
		if lm := transcriptLength.FindStringSubmatch(m[3][len(pm[0])+strings.Index(m[3], pm[0]):]); lm != nil {
			v, _ := strconv.ParseFloat(lm[1], 64)
			if lm[2] == "step" {
				if step == 0 {
					return nil, fmt.Errorf("line %d: a length in steps needs a tempo (\"at N bpm\") first", lineNo)
				}
				v *= step
			}
			dur = max(v, 0.01)
		}
		notes = append(notes, RipNote{Start: start, End: start + dur, MIDI: midi, Vel: 0.8, Bass: m[1] == "bass", Second: m[1] == "under"})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, fmt.Errorf("no notes: want lines like \"melody 1.25s G4 0.3s\"")
	}
	return notes, nil
}

// parseNoteName reads a pitch class with a sharp or flat and an octave, C4 = 60
func parseNoteName(class, octave string) (int, bool) {
	classes := map[byte]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}
	pc, ok := classes[class[0]]
	if !ok {
		return 0, false
	}
	if len(class) > 1 {
		if class[1] == '#' {
			pc++
		} else {
			pc--
		}
	}
	o, err := strconv.Atoi(octave)
	if err != nil {
		return 0, false
	}
	return 12*(o+1) + pc, true
}
