//go:build !vif_headless && !vif_noaudio && !wasm

package main

// The editor's keys tab: the tonal voices on a tracker keyboard, drawn as a piano
// with the key's scale marked. Notes go through Execute("note ...") like
// everything else, echoed quietly. A take records what was played over the
// pattern's length, quantized to the sixteenth, and goes into it through take.

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
	keyLengths  = []int{1, 2, 4, 8, 16} // note lengths in steps, [ and ] walk them
	patternBars = []int{1, 2, 4}        // the lengths \ walks a pattern through
)

func blackKey(semi int) bool {
	switch semi % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// keysState is the keys tab: what a key plays and the take being recorded
type keysState struct {
	oct   int // octave of the keyboard's first C
	vel   float64
	dur   int // steps a note sounds, and a take records
	instr audio.InstrumentType
	lit   map[int]int // MIDI note -> ticks it stays lit
	rec   bool
	take  []takeNote
}

// takeNote is one note of a take, at its step in the take
type takeNote struct {
	pos, midi, dur int
	vel            float64
	instr          audio.InstrumentType
}

func newKeysState() keysState {
	return keysState{oct: 3, vel: 0.8, dur: 2, instr: audio.InstrPiano, lit: map[int]int{}}
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

// keysKey plays the keyboard; the editor took its own keys first
func (a *tuiApp) keysKey(ev terminal.Event) {
	k := &a.keys
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
		a.cycleBars()
	default:
		semi, ok := keySemis[r]
		if !ok {
			return
		}
		if !a.s.eng.IsMusicPlaying() && !a.s.eng.IsMusicMuted() {
			a.exec("music start") // the voices sound through the sequencer
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

// cycleBars walks the pattern through 1, 2 and 4 bars, refusing a length that
// would cut a note off; s in the grid sets any length
func (a *tuiApp) cycleBars() {
	d := a.edPat()
	next := patternBars[(slices.Index(patternBars, d.Steps/audio.StepsPerBar)+1)%len(patternBars)] * audio.StepsPerBar
	for _, tr := range d.Track {
		for _, e := range tr.Event {
			if e.Pos >= next {
				fmt.Fprintf(a.log, "%s has notes past step %d — s in the grid sets any length\n", d.Name, next)
				return
			}
		}
	}
	a.exec(fmt.Sprintf("set %s.steps %d", d.Name, next))
}

// toggleRecord starts a take over the pattern's length, playing the pattern so
// the take lands where it is heard, or writes the take recorded
func (a *tuiApp) toggleRecord(d *audio.PatternDef) {
	k := &a.keys
	if k.rec {
		a.finishRecording()
		return
	}
	a.editPlay()
	k.rec, k.take = true, k.take[:0]
	fmt.Fprintf(a.log, "recording into %s over its %s — notes land on the sixteenth; ^R writes them in\n", d.Name, lengthWord(d.Steps))
}

// captureNote places a played note at the pattern's step the transport is on
func (a *tuiApp) captureNote(midi int) {
	_, step, running := a.s.eng.Transport()
	d := a.edPat()
	if !running || d == nil {
		fmt.Fprintln(a.log, "the transport is stopped — nothing to record against")
		return
	}
	k := &a.keys
	k.take = append(k.take, takeNote{pos: step % d.Steps, midi: midi, vel: k.vel, dur: k.dur, instr: k.instr})
}

// finishRecording writes the take into the pattern, a track per voice played,
// through take, and applies it so the loop plays it from its next pass
func (a *tuiApp) finishRecording() {
	k := &a.keys
	k.rec = false
	d := a.edPat()
	switch {
	case d == nil:
		fmt.Fprintln(a.log, "the pattern is gone — take dropped")
	case len(k.take) == 0:
		fmt.Fprintln(a.log, "take empty — nothing written")
	default:
		var voices []audio.InstrumentType
		for _, n := range k.take {
			if !slices.Contains(voices, n.instr) {
				voices = append(voices, n.instr)
			}
		}
		for _, v := range voices {
			line := fmt.Sprintf("take %s %s", d.Name, v)
			for _, n := range k.take {
				if n.instr == v {
					line += fmt.Sprintf(" %d:%d:%d:%s", n.pos, n.midi, n.dur, fmtF(n.vel))
				}
			}
			if a.execQ(line) != nil {
				return
			}
		}
		a.exec("apply " + d.Name)
	}
	k.take = k.take[:0]
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
	return "z…p play  ↑↓ voice  ←→ octave  - = velocity  [ ] length  ^R record  \\ bars  ⌫ undo  enter play  tab grid  esc close  ? keys"
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

// keysViewH is the height the keys tab wants at width w: the frame, two rows of
// settings and a gap, the keyboard with its names, a gap and the take
func (a *tuiApp) keysViewH(w int, d *audio.PatternDef) int {
	_, kh := keysLayout(w - 2)
	_, _, rows := takeLayout(w-2, barsOf(d.Steps))
	return 2 + 3 + kh + 1 + 1 + rows
}

// renderKeys draws the keys tab: the voice and velocity, the key, the range and
// what ^R records into, the keyboard, and the take over the pattern's length
func (a *tuiApp) renderKeys(c tui.Region, d *audio.PatternDef) {
	k := &a.keys
	th := &a.theme
	x := instrFamilyRow(c, instrFamily(k.instr), k.instr, th)
	meter := velMeter(k.vel, 10)
	if mx := c.W - tui.RuneLen(meter); mx > x {
		c.Text(mx, 0, meter, th.HintFg, th.Bg, terminal.AttrNone)
	}
	lo := audio.MIDINote(audio.NoteC, k.oct)
	info := fmt.Sprintf("%s · %s–%s · note %s", a.s.keyName(), noteName(lo), noteName(lo+len(keyCaps)-1), stepsWord(k.dur))
	c.Text(0, 1, tui.Truncate(info, c.W), th.Fg, th.Bg, terminal.AttrNone)
	take, fg := "^R records into "+d.Name, th.HintFg
	if k.rec {
		take, fg = fmt.Sprintf("● recording into %s · %d note(s)", d.Name, len(k.take)), th.Error
	}
	if tx := c.W - tui.RuneLen(take); tx > tui.RuneLen(info)+1 {
		c.Text(tx, 1, take, fg, th.Bg, terminal.AttrBold)
	}

	kw, kh := keysLayout(c.W)
	x0 := max((c.W-whiteKeys*kw)/2, 0)
	a.drawKeyboard(c.Sub(x0, 3, whiteKeys*kw, kh+1), lo, kw, kh)
	tcw, perRow, rows := takeLayout(c.W, barsOf(d.Steps))
	tw := tui.RuneLen(takeLabel) + perRow*(1+audio.StepsPerBar*tcw)
	ty := min(5+kh, c.H-rows)
	a.drawTake(c.Sub(max((c.W-tw)/2, 0), ty, tw, rows), d.Steps, tcw, perRow)
}

// barsOf is the bars steps run over, the last one partly
func barsOf(steps int) int { return (steps + audio.StepsPerBar - 1) / audio.StepsPerBar }

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

// drawTake shows the take bar by bar over the pattern's steps, cw cells a step and
// perRow bars a row, a note as its degree (* for a chord) and, while recording,
// the step the transport is at lit
func (a *tuiApp) drawTake(r tui.Region, steps, cw, perRow int) {
	k := &a.keys
	th := &a.theme
	at := map[int][]takeNote{}
	for _, n := range k.take {
		at[n.pos] = append(at[n.pos], n)
	}
	_, step, running := a.s.eng.Transport()
	play := -1
	if running && k.rec {
		play = step % steps
	}
	r.Text(0, 0, takeLabel, th.HintFg, th.Bg, terminal.AttrNone)
	for b := range barsOf(steps) {
		y := b / perRow
		x := tui.RuneLen(takeLabel) + b%perRow*(1+audio.StepsPerBar*cw)
		r.Cell(x, y, '│', th.Border, th.Bg, terminal.AttrNone)
		x++
		for s := range min(audio.StepsPerBar, steps-b*audio.StepsPerBar) {
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
