//go:build !vif_headless && !vif_noaudio && !wasm

package main

// The keys view: the tonal voices on a tracker keyboard, drawn as a piano with
// the key's scale marked. Notes go through Execute("note ...") like everything
// else — quiet echo, but full script parity. A take records what was played,
// quantized to the sixteenth, and commits it as a pattern.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/pkg/audio"
)

// keyCaps are the keycaps drawn on the keyboard, by semitone above its first C:
// the bottom row an octave, the top row the next and a third
var keyCaps = [...]rune{'z', 's', 'x', 'd', 'c', 'v', 'g', 'b', 'h', 'n', 'j', 'm',
	'q', '2', 'w', '3', 'e', 'r', '5', 't', '6', 'y', '7', 'u', 'i', '9', 'o', '0', 'p'}

// keySemis plays each keycap: the drawn ones, and the bottom row running on into
// the second octave as trackers lay it out
var keySemis = func() map[rune]int {
	m := make(map[rune]int, len(keyCaps)+5)
	for i, c := range keyCaps {
		m[c] = i
	}
	for i, c := range ",l.;/" {
		m[c] = 12 + i
	}
	return m
}()

var (
	keyLengths = []int{1, 2, 4, 8, 16} // note lengths in steps, - and = walk them
	takeBars   = []int{1, 2, 4}        // take lengths, \ walks them
)

func blackKey(semi int) bool {
	switch semi % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// keysState is the keys view: what a key plays and the take being recorded
type keysState struct {
	oct     int // octave of the keyboard's first C
	vel     float64
	dur     int // steps a note sounds, and a take records
	instr   audio.InstrumentType
	lit     map[int]int // MIDI note -> ticks it stays lit
	started bool        // the view started the transport; leaving stops it again
	rec     bool
	bars    int // take length
	take    []takeNote
	recN    int
}

// takeNote is one note of a take, at its step in the take
type takeNote struct {
	pos, midi, dur int
	vel            float64
	instr          audio.InstrumentType
}

func newKeysState() keysState {
	return keysState{oct: 3, vel: 0.8, dur: 2, instr: audio.InstrPiano, lit: map[int]int{}, bars: 1}
}

// decay dims the lit keys by a tick, reporting whether any was lit
func (k *keysState) decay() bool {
	for n, t := range k.lit {
		if t <= 1 {
			delete(k.lit, n)
		} else {
			k.lit[n] = t - 1
		}
	}
	return len(k.lit) > 0
}

func (a *tuiApp) enterKeys() {
	if !a.s.eng.IsMusicPlaying() {
		if a.s.eng.IsMusicMuted() {
			fmt.Fprintln(a.log, "music is muted — :mute none, then p (the voices sound through the sequencer)")
			return
		}
		a.exec("music start")
		a.keys.started = true
	}
	a.view, a.focus = viewKeys, focusMain
}

func (a *tuiApp) leaveKeys() {
	if a.keys.rec {
		a.finishRecording()
	}
	a.view, a.focus = viewBrowse, focusMain
	// Stop only a transport the view started: patterns the user had running play on
	if a.keys.started {
		a.exec("music stop")
	}
	a.keys.started = false
}

func (a *tuiApp) keysKey(ev terminal.Event) {
	k := &a.keys
	switch {
	case ev.Key == terminal.KeyEscape:
		a.leaveKeys()
		return
	case ev.Key == terminal.KeyCtrlR:
		if k.rec {
			a.finishRecording()
			return
		}
		k.rec, k.take = true, k.take[:0]
		fmt.Fprintf(a.log, "recording a %d-bar take — notes land on the sixteenth; ^R commits\n", k.bars)
		return
	case a.focus == focusMixer:
		a.mixerKey(ev, ev.Rune)
		return
	}
	switch ev.Key {
	case terminal.KeyUp:
		k.instr = nextVoice(k.instr, -1)
	case terminal.KeyDown:
		k.instr = nextVoice(k.instr, 1)
	case terminal.KeyLeft:
		k.oct = max(k.oct-1, 0)
	case terminal.KeyRight:
		k.oct = min(k.oct+1, 7)
	case terminal.KeyBackspace, terminal.KeyCtrlH:
		if n := len(k.take); n > 0 {
			k.take = k.take[:n-1]
		}
	case terminal.KeyRune:
		a.keysRune(ev.Rune)
	}
}

// keysRune plays a note, or sets what the next one takes with the keys the beat
// view edits a note's with: - = velocity, [ ] length, { } octave
func (a *tuiApp) keysRune(r rune) {
	k := &a.keys
	switch r {
	case '{':
		k.oct = max(k.oct-1, 0)
	case '}':
		k.oct = min(k.oct+1, 7) // the keyboard's top E stays under MIDI 127
	case '-':
		k.vel = max(k.vel-0.05, 0.05)
	case '=', '+':
		k.vel = min(k.vel+0.05, 1.0)
	case '[':
		k.dur = keyLengths[max(slices.Index(keyLengths, k.dur)-1, 0)]
	case ']':
		k.dur = keyLengths[min(slices.Index(keyLengths, k.dur)+1, len(keyLengths)-1)]
	case '\\':
		k.bars = takeBars[(slices.Index(takeBars, k.bars)+1)%len(takeBars)]
	default:
		semi, ok := keySemis[r]
		if !ok {
			return
		}
		midi := audio.MIDINote(audio.NoteC, k.oct) + semi
		a.execQ(fmt.Sprintf("note %d %.2f %d %s", midi, k.vel, k.dur, k.instr))
		// lit for as long as it sounds, at the tick's resolution
		k.lit[midi] = max(k.dur*60000/(a.s.bpm*audio.StepsPerBeat)/int(tickEvery.Milliseconds())+1, 2)
		if k.rec {
			a.captureNote(midi)
		}
	}
}

// nextVoice steps through the tonal voices, the drums skipped
func nextVoice(i audio.InstrumentType, d int) audio.InstrumentType {
	n := int(audio.InstrumentCount - audio.InstrBass)
	return audio.InstrBass + audio.InstrumentType((int(i-audio.InstrBass)+d+n)%n)
}

// captureNote places a played note at its step of the take, which repeats
// against the transport's four-bar cycle
func (a *tuiApp) captureNote(midi int) {
	_, step, running := a.s.eng.Transport()
	if !running {
		fmt.Fprintln(a.log, "the transport is stopped — nothing to record against")
		return
	}
	k := &a.keys
	k.take = append(k.take, takeNote{pos: step % (k.bars * audio.StepsPerBar), midi: midi, vel: k.vel, dur: k.dur, instr: k.instr})
}

// finishRecording writes the take as a document pattern, a track per voice
// played, each note on the degree of the mirrored key nearest it; a later
// note of one pitch on one step replaces the earlier
func (a *tuiApp) finishRecording() {
	k := &a.keys
	k.rec = false
	if len(k.take) == 0 {
		fmt.Fprintln(a.log, "take empty — nothing written")
		return
	}
	var name string
	for {
		k.recN++
		name = fmt.Sprintf("rec_%d", k.recN)
		if !a.s.pats.has(name) && !a.s.sounds.has(name) {
			break
		}
	}
	type at struct {
		instr         audio.InstrumentType
		pos, deg, oct int
	}
	notes, snapped := map[at]audio.StepDef{}, 0
	var voices []audio.InstrumentType
	for _, n := range k.take {
		deg, oct, exact := a.s.degreeOf(n.midi)
		if !exact {
			snapped++
		}
		notes[at{n.instr, n.pos, deg, oct}] = audio.StepDef{Pos: n.pos, Vel: n.vel, Deg: deg, Oct: oct, Dur: n.dur}
		if !slices.Contains(voices, n.instr) {
			voices = append(voices, n.instr)
		}
	}
	d := &audio.PatternDef{Name: name, Steps: k.bars * audio.StepsPerBar}
	for _, v := range voices {
		tr := audio.TrackDef{Instr: v.String(), Humanize: 0.15}
		for key, ev := range notes {
			if key.instr == v {
				tr.Event = append(tr.Event, ev)
			}
		}
		slices.SortFunc(tr.Event, func(x, y audio.StepDef) int {
			return cmpInts(x.Pos, y.Pos, x.Oct, y.Oct, x.Deg, y.Deg)
		})
		d.Track = append(d.Track, tr)
	}
	a.s.pats.put(d, true)
	a.kind = kindPattern
	a.cursorTo(name)
	fmt.Fprintf(a.log, "recorded %d note(s) into %s, %d bar(s) — space plays it, b edits it\n", len(notes), name, k.bars)
	if snapped > 0 {
		fmt.Fprintf(a.log, "%d note(s) outside %s moved to the nearest degree\n", snapped, a.s.keyName())
	}
}

// cmpInts orders by pairs of ints in turn, (x0, y0) first
func cmpInts(pairs ...int) int {
	for i := 0; i+1 < len(pairs); i += 2 {
		if c := pairs[i] - pairs[i+1]; c != 0 {
			return c
		}
	}
	return 0
}

func (a *tuiApp) keysHint() string {
	return "z…p play  ↑↓ voice  ←→ octave  - = velocity  [ ] length  ^R record  \\ take  ⌫ undo  tab mixer  esc close  ? keys"
}

// whiteKeys counts the keyboard's white keys
var whiteKeys = func() int {
	n := 0
	for s := range keyCaps {
		if !blackKey(s) {
			n++
		}
	}
	return n
}()

// keysLayout sizes the keyboard to the width w inside the frame: white keys
// three to seven cells wide, each two rows taller than it is wide
func keysLayout(w int) (kw, kh int) {
	kw = min(max(w/whiteKeys, 3), 7)
	return kw, kw + 2
}

// takeLayout lays a take of bars out across w: the widest steps that keep it on
// one row, else a step a cell and as many bars a row as fit
func takeLayout(w, bars int) (cw, perRow, rows int) {
	row := func(cw, n int) int { return tui.RuneLen(takeLabel) + n*(1+audio.StepsPerBar*cw) }
	for cw = 3; cw >= 1; cw-- {
		if row(cw, bars) <= w {
			return cw, bars, 1
		}
	}
	perRow = 1
	for perRow < bars && row(1, perRow*2) <= w {
		perRow *= 2
	}
	return 1, perRow, (bars + perRow - 1) / perRow
}

const takeLabel = "take "

// keysViewH is the height the keys view wants at width w: the frame, two rows of
// settings and a gap, the keyboard with its names, a gap and the take
func (a *tuiApp) keysViewH(w int) int {
	_, kh := keysLayout(w - 2)
	_, _, rows := takeLayout(w-2, a.keys.bars)
	return 2 + 3 + kh + 1 + 1 + rows
}

func (a *tuiApp) renderKeysView(r tui.Region) {
	k := &a.keys
	r = a.stackBelow(r, a.keysViewH(r.W))
	title := "keys · " + k.instr.String()
	if k.rec {
		title = "● recording · " + k.instr.String()
	}
	c := a.viewPane(r, title)
	th := &a.theme

	x := instrFamilyRow(c, instrFamily(k.instr), k.instr, th)
	meter := velMeter(k.vel, 10)
	if mx := c.W - tui.RuneLen(meter); mx > x {
		c.Text(mx, 0, meter, th.HintFg, th.Bg, terminal.AttrNone)
	}
	lo := audio.MIDINote(audio.NoteC, k.oct)
	info := fmt.Sprintf("%s · %s–%s · note %s", a.s.keyName(), noteName(lo), noteName(lo+len(keyCaps)-1), stepsWord(k.dur))
	c.Text(0, 1, tui.Truncate(info, c.W), th.Fg, th.Bg, terminal.AttrNone)
	take := fmt.Sprintf("take %d bar", k.bars)
	if k.bars > 1 {
		take += "s"
	}
	if k.rec {
		take = fmt.Sprintf("● %s · %d note(s)", take, len(k.take))
	}
	if tx := c.W - tui.RuneLen(take); tx > tui.RuneLen(info)+1 {
		fg := th.HintFg
		if k.rec {
			fg = th.Error
		}
		c.Text(tx, 1, take, fg, th.Bg, terminal.AttrBold)
	}

	kw, kh := keysLayout(c.W)
	x0 := max((c.W-whiteKeys*kw)/2, 0)
	a.drawKeyboard(c.Sub(x0, 3, whiteKeys*kw, kh+1), lo, kw, kh)
	tcw, perRow, rows := takeLayout(c.W, k.bars)
	tw := tui.RuneLen(takeLabel) + perRow*(1+audio.StepsPerBar*tcw)
	ty := min(5+kh, c.H-rows)
	a.drawTake(c.Sub(max((c.W-tw)/2, 0), ty, tw, rows), tcw, perRow)
}

// drawKeyboard draws the keys from MIDI note lo, white keys kw wide and kh tall
// with the black keys over their upper rows, then the Cs' names under them
func (a *tuiApp) drawKeyboard(r tui.Region, lo, kw, kh int) {
	th := &a.theme
	bh := kh - 2         // black keys' height
	bw := (kw*2 + 1) / 3 // black keys' width, two thirds of a white key's
	mark := func(semi int) (rune, bool) {
		midi := lo + semi
		_, _, in := a.s.degreeOf(midi)
		root := (midi-a.s.root)%12 == 0
		switch {
		case root:
			return '●', true
		case in:
			return '•', false
		}
		return ' ', false
	}
	// White keys first, each a crack and its face, its cap at the foot
	w := 0
	for semi := range keyCaps {
		if blackKey(semi) {
			continue
		}
		bg := th.Ivory
		if a.keys.lit[lo+semi] > 0 {
			bg = th.Selected
		}
		x := w * kw
		for y := range kh {
			r.Cell(x, y, '│', th.KeyLine, bg, terminal.AttrNone)
			for dx := 1; dx < kw; dx++ {
				r.Cell(x+dx, y, ' ', th.Ebony, bg, terminal.AttrNone)
			}
		}
		cx := x + kw/2
		m, root := mark(semi)
		mfg := th.KeyLine
		if root {
			mfg = th.Warning
		}
		r.Cell(cx, kh-2, m, mfg, bg, terminal.AttrNone)
		r.Cell(cx, kh-1, keyCaps[semi], th.Ebony, bg, terminal.AttrBold)
		if semi%12 == 0 {
			r.Text(x, kh, noteName(lo+semi), th.HintFg, th.Bg, terminal.AttrNone)
		}
		w++
	}
	// Black keys over the crack between their neighbors
	w = 0
	for semi := range keyCaps {
		if !blackKey(semi) {
			w++
			continue
		}
		bg := th.Ebony
		if a.keys.lit[lo+semi] > 0 {
			bg = th.Selected
		}
		x := w*kw - bw/2
		for y := range bh {
			for dx := range bw {
				r.Cell(x+dx, y, ' ', th.Fg, bg, terminal.AttrNone)
			}
		}
		cx := x + (bw-1)/2
		m, root := mark(semi)
		mfg := th.Unselected
		if root {
			mfg = th.Warning
		}
		r.Cell(cx, bh-2, m, mfg, bg, terminal.AttrNone)
		r.Cell(cx, bh-1, keyCaps[semi], th.Fg, bg, terminal.AttrBold)
	}
}

// drawTake shows the take bar by bar, cw cells a step and perRow bars a row, a
// note as its degree (* for a chord) and, while recording, the step the
// transport is at lit
func (a *tuiApp) drawTake(r tui.Region, cw, perRow int) {
	k := &a.keys
	th := &a.theme
	at := map[int][]takeNote{}
	for _, n := range k.take {
		at[n.pos] = append(at[n.pos], n)
	}
	_, step, running := a.s.eng.Transport()
	play := -1
	if running && k.rec {
		play = step % (k.bars * audio.StepsPerBar)
	}
	r.Text(0, 0, takeLabel, th.HintFg, th.Bg, terminal.AttrNone)
	for b := range k.bars {
		y := b / perRow
		x := tui.RuneLen(takeLabel) + b%perRow*(1+audio.StepsPerBar*cw)
		r.Cell(x, y, '│', th.Border, th.Bg, terminal.AttrNone)
		x++
		for s := range audio.StepsPerBar {
			pos := b*audio.StepsPerBar + s
			ch, fg := '·', th.Unselected
			if s%audio.StepsPerBeat == 0 {
				fg = th.HintFg
			}
			if ns := at[pos]; len(ns) > 0 {
				deg, _, _ := a.s.degreeOf(ns[len(ns)-1].midi)
				ch, fg = rune('0'+deg%10), th.Fg
				if len(ns) > 1 {
					ch = '*'
				}
			}
			bg := th.Bg
			if pos == play {
				bg = th.CursorBg
			}
			for i := range cw {
				c := ch
				if i > 0 {
					c = ' '
				}
				r.Cell(x+s*cw+i, y, c, fg, bg, terminal.AttrNone)
			}
		}
	}
}

// velMeter draws a velocity as a bar of n cells and its value
func velMeter(v float64, n int) string {
	full := int(v*float64(n) + 0.5)
	return "vel " + strings.Repeat("▮", full) + strings.Repeat("▯", n-full) + fmt.Sprintf(" %.2f", v)
}

func stepsWord(n int) string {
	if n == 1 {
		return "1 step"
	}
	return fmt.Sprintf("%d steps", n)
}

// instrFamily is the drums or the tonal voices, whichever i belongs to
func instrFamily(i audio.InstrumentType) []audio.InstrumentType {
	lo, hi := audio.InstrBass, audio.InstrumentCount
	if i.IsDrum() {
		lo, hi = audio.InstrKick, audio.InstrBass
	}
	f := make([]audio.InstrumentType, 0, hi-lo)
	for j := lo; j < hi; j++ {
		f = append(f, j)
	}
	return f
}

// instrFamilyRow writes a family across the region's first row with cur lit and
// returns the column after it
func instrFamilyRow(r tui.Region, fam []audio.InstrumentType, cur audio.InstrumentType, th *palette) int {
	x := 0
	for _, i := range fam {
		name := " " + i.String() + " "
		fg, bg, attr := th.HintFg, th.Bg, terminal.AttrNone
		if i == cur {
			fg, bg, attr = th.Bg, th.Selected, terminal.AttrBold
		}
		r.Text(x, 0, name, fg, bg, attr)
		x += tui.RuneLen(name) + 1
	}
	return x
}
