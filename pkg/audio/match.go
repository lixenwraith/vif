package audio

import (
	"bufio"
	"fmt"
	"math"
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
	// paired are the seconds of b and of a of each note the register matched, in
	// order, the map At follows
	paired [][2]float64
}

// At places a second of b on a: between notes the register matched, on the
// line through the two either side, so a tempo b's player bends is followed;
// past them, and with fewer than two, on Stretch from the nearest
func (ag Agreement) At(s float64) float64 {
	p := ag.paired
	if len(p) < 2 {
		return ag.Stretch*s + ag.Offset
	}
	i, _ := slices.BinarySearchFunc(p, s, func(q [2]float64, s float64) int { return cmpFloat(q[0], s) })
	switch {
	case i == 0:
		return p[0][1] + (s-p[0][0])*ag.Stretch
	case i == len(p):
		return p[i-1][1] + (s-p[i-1][0])*ag.Stretch
	case p[i][0] == p[i-1][0]:
		return p[i-1][1]
	}
	return p[i-1][1] + (s-p[i-1][0])*(p[i][1]-p[i-1][1])/(p[i][0]-p[i-1][0])
}

// LineAgreement is one line's count: the notes each side has, how many align,
// the notes only one side has, and for a line given as a rhythm alone, how many
// of its notes a has cut in two or more (Split) and how many of a's notes span
// two or more of them (Merged)
type LineAgreement struct {
	A, B, Matched int
	OnlyA, OnlyB  []RipNote
	Split, Merged int
	Cut, Spanning []RipNote // the notes of b cut in two or more by a's, and the notes of a spanning two or more of b's
	Rhythm        bool      // b's notes had no pitch: matched by onset, not by pitch in order
}

// ripLineNames are a transcript's lines by name, in lineOf's order
var ripLineNames = [3]string{"melody", "under", "bass"}

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
	// a rhythm given has a unit and a start the listener guessed: its notes are
	// placed on the map, a stretch and an offset within a tenth and a third of a
	// second, that matches most of them to a's onsets
	if slices.ContainsFunc(b, func(n RipNote) bool { return n.MIDI < 0 }) {
		b = placed(a, b)
	}
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
		var pairs [][2]int
		rhythm := slices.ContainsFunc(lb, func(n RipNote) bool { return n.MIDI < 0 })
		if rhythm {
			pairs = onsets(la, lb)
		} else {
			pairs = subsequence(la, lb, shift)
		}
		l := LineAgreement{A: len(la), B: len(lb), Matched: len(pairs), Rhythm: rhythm}
		if rhythm {
			l.Cut, l.Spanning = spans(la, lb)
			l.Split, l.Merged = len(l.Cut), len(l.Spanning)
		}
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
	n := len(xs)
	ag.Register = part(func(n RipNote) bool { return !n.Bass })
	for k := n; k < len(xs); k++ {
		ag.paired = append(ag.paired, [2]float64{xs[k], ys[k]})
	}
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

// placed stretches and shifts a rhythm's notes onto a's time, keeping the map
// that matches most of them, line by line, to a's onsets
func placed(a, b []RipNote) []RipNote {
	best, bestStretch, bestOffset, bestAway := -1, 1.0, 0.0, math.Inf(1)
	mapped := func(stretch, offset float64) []RipNote {
		out := slices.Clone(b)
		for i := range out {
			out[i].Start, out[i].End = out[i].Start*stretch+offset, out[i].End*stretch+offset
		}
		return out
	}
	for stretch := 0.9; stretch <= 1.1001; stretch += 0.005 {
		for offset := -0.3; offset <= 0.3001; offset += 0.02 {
			m := mapped(stretch, offset)
			n := 0
			for line := range 3 {
				var la, lb []RipNote
				for _, x := range a {
					if lineOf(x) == line {
						la = append(la, x)
					}
				}
				for _, x := range m {
					if lineOf(x) == line && x.MIDI < 0 {
						lb = append(lb, x)
					}
				}
				n += len(onsets(la, lb))
			}
			// among maps that match as many, the one nearest the listener's own
			away := math.Abs(stretch-1)*10 + math.Abs(offset)
			if n > best || n == best && away < bestAway {
				best, bestStretch, bestOffset, bestAway = n, stretch, offset, away
			}
		}
	}
	return mapped(bestStretch, bestOffset)
}

// onsets matches a's notes to b's by when they start, b's notes having no
// pitch: in time order, each note of b takes the nearest note of a that starts
// within half of b's shortest note, once
func onsets(a, b []RipNote) [][2]int {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	tol := math.Inf(1)
	for _, n := range b {
		tol = math.Min(tol, (n.End-n.Start)/2)
	}
	tol = math.Max(tol, 0.03)
	var pairs [][2]int
	i := 0
	for j, n := range b {
		for i < len(a) && a[i].Start < n.Start-tol {
			i++
		}
		best := -1
		for k := i; k < len(a) && a[k].Start <= n.Start+tol; k++ {
			if best < 0 || math.Abs(a[k].Start-n.Start) < math.Abs(a[best].Start-n.Start) {
				best = k
			}
		}
		if best >= 0 {
			pairs = append(pairs, [2]int{best, j})
			i = best + 1
		}
	}
	return pairs
}

// spans lists b's notes that two or more of a's start within (cut) and a's
// notes that two or more of b's start within (spanning)
func spans(a, b []RipNote) (cut, spanning []RipNote) {
	within := func(outer, inner []RipNote) []RipNote {
		var out []RipNote
		for _, o := range outer {
			k := 0
			for _, x := range inner {
				if x.Start >= o.Start-0.03 && x.Start < o.End-0.03 {
					k++
				}
			}
			if k >= 2 {
				out = append(out, o)
			}
		}
		return out
	}
	return within(b, a), within(a, b)
}

// NoteLevels is the level in dB a recording plays each note at: the strongest
// bin of its first three harmonics from 60 ms before its onset to 120 ms after,
// the slack for a map that places it a little early or late, its seconds placed
// on the recording by at (a truth's Agreement.At), its pitch at the recording's
// tuning in cents
func NoteLevels(x []float64, rate int, notes []RipNote, at func(float64) float64, cents float64) []float64 {
	n := 2
	for n < rate/6 { // about 170 ms, a bin under a semitone from E2 up
		n *= 2
	}
	s, hop, before, after := newSTFT(n), max(rate/100, 1), int(0.06*float64(rate)), int(0.12*float64(rate))
	out := make([]float64, len(notes))
	for i, note := range notes {
		f0 := NoteFreq(note.MIDI) * math.Pow(2, cents/1200)
		start := int(at(note.Start) * float64(rate))
		var peak float64
		for c := start + n/2 - before; c <= start+n/2+after; c += hop {
			mag := s.at(x, c)
			for h := 1.0; h <= 3; h++ {
				k := int(math.Round(h * f0 * float64(n) / float64(rate)))
				for j := max(k-2, 0); j <= k+2 && j < len(mag); j++ {
					peak = max(peak, mag[j])
				}
			}
		}
		out[i] = 20 * math.Log10(peak+1e-12)
	}
	return out
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

// HitsAgreement is how two drum parts of one piece agree, drum by drum, under
// the map that matches most: a's time of a hit ≈ Stretch*b's + Offset
type HitsAgreement struct {
	Drums           [InstrClap]HitLine
	Stretch, Offset float64
}

// HitLine is one drum's count: the hits each side has, how many align, and the
// hits only one side has, b's at their place on a's time
type HitLine struct {
	A, B, Matched int
	OnlyA, OnlyB  []RipHit
}

// MatchHits aligns b's hits to a's drum by drum under the stretch within a
// tenth of one, and the offset putting one of b's first hits on one of a's, that
// match most, nearer to none when two tie: each hit of b takes the nearest
// unmatched hit of a's drum within tol of it, in time order
func MatchHits(a, b []RipHit, tol float64) HitsAgreement {
	var la, lb [InstrClap][]RipHit
	for _, h := range a {
		if h.Drum >= 0 && h.Drum < InstrClap {
			la[h.Drum] = append(la[h.Drum], h)
		}
	}
	for _, h := range b {
		if h.Drum >= 0 && h.Drum < InstrClap {
			lb[h.Drum] = append(lb[h.Drum], h)
		}
	}
	byStart := func(x, y RipHit) int { return cmpFloat(x.Start, y.Start) }
	for d := range la {
		slices.SortStableFunc(la[d], byStart)
		slices.SortStableFunc(lb[d], byStart)
	}
	best, bestStretch, bestOffset, bestAway := -1, 1.0, 0.0, math.Inf(1)
	for stretch := 0.9; stretch <= 1.1001; stretch += 0.005 {
		seen := map[int64]bool{}
		for _, h := range b[:min(len(b), 4)] {
			for _, g := range a {
				offset := g.Start - stretch*h.Start
				if g.Drum != h.Drum || seen[int64(math.Round(offset*200))] {
					continue
				}
				seen[int64(math.Round(offset*200))] = true
				n := 0
				for d := range la {
					n += pairHits(la[d], lb[d], stretch, offset, tol, nil)
				}
				away := math.Abs(stretch-1)*10 + math.Abs(offset)
				if n > best || n == best && away < bestAway {
					best, bestStretch, bestOffset, bestAway = n, stretch, offset, away
				}
			}
		}
	}
	ag := HitsAgreement{Stretch: bestStretch, Offset: bestOffset}
	for d := range la {
		pairHits(la[d], lb[d], bestStretch, bestOffset, tol, &ag.Drums[d])
	}
	return ag
}

// pairHits counts the hits of b, mapped onto a's time, that take one of a's,
// and fills line when it is given
func pairHits(a, b []RipHit, stretch, offset, tol float64, line *HitLine) int {
	used := make([]bool, len(a))
	n, i := 0, 0
	for _, h := range b {
		h.Start = h.Start*stretch + offset
		for i < len(a) && a[i].Start < h.Start-tol {
			i++
		}
		k := -1
		for j := i; j < len(a) && a[j].Start <= h.Start+tol; j++ {
			if !used[j] && (k < 0 || math.Abs(a[j].Start-h.Start) < math.Abs(a[k].Start-h.Start)) {
				k = j
			}
		}
		if k >= 0 {
			used[k], n = true, n+1
		} else if line != nil {
			line.OnlyB = append(line.OnlyB, h)
		}
	}
	if line != nil {
		line.A, line.B, line.Matched = len(a), len(b), n
		for j, h := range a {
			if !used[j] {
				line.OnlyA = append(line.OnlyA, h)
			}
		}
	}
	return n
}

// transcriptLine reads one note of a transcript as the notes verb prints it, or
// in short: the line, the start in seconds, then anywhere after, the pitch as a
// note name and a length in seconds or in steps
var transcriptLine = regexp.MustCompile(`^\s*(melody|under|bass)\s+([0-9.]+)s\b(.*)$`)
var transcriptPitch = regexp.MustCompile(`\b([A-G][#b]?)(-?[0-9])\b`)
var transcriptLength = regexp.MustCompile(`\b([0-9.]+)\s*(s\b|step)`)
var transcriptTempo = regexp.MustCompile(`\bat ([0-9]+) bpm\b`)

var transcriptUnit = regexp.MustCompile(`^\s*unit\s+([0-9.]+)\s*(ms|s)?\s*$|^\s*unit\s+(sixteenth|eighth|quarter|half)\s*$`)
var transcriptStart = regexp.MustCompile(`^\s*start\s+([0-9.]+)\s*s?\s*$`)
var transcriptTab = regexp.MustCompile(`^\s*(melody|under|bass)\s+(\S.*)$`)

// ParseTranscript reads notes from text in the form the notes verb prints,
// which a listener can correct by hand: a header "... at N bpm ..." sets the
// tempo lengths in steps are read by (bpm when there is none), and each note
// is "<line> <start>s ... <pitch> ... <length> step(s)" or "<line> <start>s
// <pitch> <length>s", the rest of a line ignored. A line is melody, under or
// bass. A line may also be a rhythm with no pitches, as a listener taps it
// out: "unit eighth" (or quarter, sixteenth, half, or a length such as 203ms)
// and "start 0.34s" (origin when none) set the grid, then "<line> <tab>" where
// each character is a note a unit long, "-" lengthens the note before it by a
// unit, "." is a unit of rest and "(...)x3" repeats; such notes have no pitch
// (MIDI -1) and are matched by when they start.
func ParseTranscript(text string, bpm int, origin float64) ([]RipNote, error) {
	var notes []RipNote
	step, unit, start := 0.0, 0.0, origin
	if bpm > 0 {
		step = 60 / float64(bpm) / StepsPerBeat
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := sc.Text()
		if m := transcriptTempo.FindStringSubmatch(line); m != nil {
			if v, _ := strconv.Atoi(m[1]); v > 0 {
				step = 60 / float64(v) / StepsPerBeat
			}
		}
		if m := transcriptUnit.FindStringSubmatch(line); m != nil {
			switch {
			case m[3] != "":
				if step == 0 {
					return nil, fmt.Errorf("line %d: a unit named by a note needs a tempo (\"at N bpm\") first", lineNo)
				}
				unit = step * StepsPerBeat * map[string]float64{"sixteenth": 0.25, "eighth": 0.5, "quarter": 1, "half": 2}[m[3]]
			default:
				v, _ := strconv.ParseFloat(m[1], 64)
				if m[2] == "ms" {
					v /= 1000
				}
				unit = v
			}
			if unit <= 0 {
				return nil, fmt.Errorf("line %d: unit %q", lineNo, strings.TrimSpace(line))
			}
			continue
		}
		if m := transcriptStart.FindStringSubmatch(line); m != nil {
			start, _ = strconv.ParseFloat(m[1], 64)
			continue
		}
		if m := transcriptTab.FindStringSubmatch(line); m != nil && !transcriptLine.MatchString(line) {
			if unit <= 0 {
				return nil, fmt.Errorf("line %d: a rhythm needs \"unit ...\" before it", lineNo)
			}
			tab, err := parseTab(m[2], unit, start, m[1])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			notes = append(notes, tab...)
			continue
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
		notes = append(notes, RipNote{Start: start, End: start + dur, MIDI: midi, Vel: 0.8, Bass: m[1] == "bass", Second: m[1] == "under", Bright: RipUnmeasured})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, fmt.Errorf("no notes: want lines like \"melody 1.25s G4 0.3s\"")
	}
	return notes, nil
}

// parseTab reads a rhythm tapped out as text into notes without a pitch, from
// start, unit seconds a character: "-" lengthens the note before it, "." is a
// rest, "(...)xN" repeats, spaces are nothing, and any other character is a note
func parseTab(tab string, unit, start float64, line string) ([]RipNote, error) {
	var notes []RipNote
	at := start
	var expand func(s string) (string, error)
	expand = func(s string) (string, error) { // groups unrolled, innermost first
		for {
			close := strings.IndexByte(s, ')')
			if close < 0 {
				if strings.IndexByte(s, '(') >= 0 {
					return "", fmt.Errorf("a \"(\" without its \")\"")
				}
				return s, nil
			}
			open := strings.LastIndexByte(s[:close], '(')
			if open < 0 {
				return "", fmt.Errorf("a \")\" without its \"(\"")
			}
			times, rest := 1, s[close+1:]
			if strings.HasPrefix(rest, "x") {
				i := 1
				for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
					i++
				}
				times, _ = strconv.Atoi(rest[1:i])
				if times < 1 || times > 64 {
					return "", fmt.Errorf("repeat %q: want x1 to x64", rest[:i])
				}
				rest = rest[i:]
			}
			s = s[:open] + strings.Repeat(s[open+1:close], times) + rest
		}
	}
	flat, err := expand(tab)
	if err != nil {
		return nil, err
	}
	for _, c := range flat {
		switch {
		case c == ' ' || c == '\t':
		case c == '-':
			if len(notes) == 0 || notes[len(notes)-1].End < at-1e-9 {
				return nil, fmt.Errorf("a \"-\" with no note before it")
			}
			notes[len(notes)-1].End += unit
			at += unit
		case c == '.' || c == '_':
			at += unit
		default:
			notes = append(notes, RipNote{Start: at, End: at + unit, MIDI: -1, Vel: 0.8, Bass: line == "bass", Second: line == "under", Bright: RipUnmeasured})
			at += unit
		}
	}
	if len(notes) == 0 {
		return nil, fmt.Errorf("no notes in the rhythm")
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
