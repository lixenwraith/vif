package main

// Beat editor: a step grid over a document PatternDef, rendered in the strip
// the piano also uses — one strip mode at a time. Every mutation issues a
// command line through Execute (set/add/del), so the grid is a macro keyboard
// over the same table: script parity holds, dirty tracking is free, and the
// inspector shows each change live.

import (
	"fmt"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/audio"
)

const beatDefaultVel = 0.8

type beatState struct {
	name       string
	trk, step  int
	instrCycle []string
}

func (a *tuiApp) enterBeat() {
	n := a.selName()
	if n == "" || !addressable(n) {
		return
	}
	d, ok := a.s.pats.get(n)
	if !ok {
		return
	}
	if d.Steps <= 0 || d.Steps > audio.MaxPatternLen {
		fmt.Fprintf(a.log, "%q: steps %d — set %s.steps first\n", n, d.Steps, n)
		return
	}
	cyc := make([]string, 0, int(audio.InstrumentCount))
	for i := audio.InstrumentType(0); i < audio.InstrumentCount; i++ {
		cyc = append(cyc, i.String())
	}
	a.beat = &beatState{name: n, instrCycle: cyc}
	a.mode = modeBeat
}

// beatPat re-reads the document every access: grid edits go through Execute,
// which mutates the doc, and holding a stale pointer across that would show
// pre-edit state. nil when not in beat mode or the pattern was deleted.
func (a *tuiApp) beatPat() *audio.PatternDef {
	if a.beat == nil {
		return nil
	}
	d, _ := a.s.pats.get(a.beat.name)
	return d
}

// eventAt finds the event index at (trk, step); -1 when the cell is empty.
func eventAt(d *audio.PatternDef, trk, step int) int {
	if trk < 0 || trk >= len(d.Track) {
		return -1
	}
	for i := range d.Track[trk].Event {
		if d.Track[trk].Event[i].Pos == step {
			return i
		}
	}
	return -1
}

func (a *tuiApp) beatKey(ev terminal.Event) {
	b := a.beat
	d := a.beatPat()
	if d == nil { // deleted out from under the editor via :del
		a.mode = modeNormal
		a.beat = nil
		return
	}
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	switch {
	case ev.Key == terminal.KeyEscape:
		a.mode = modeNormal
		a.beat = nil
		return
	case ev.Key == terminal.KeyCtrlS:
		a.saveModified()
		return
	case ev.Key == terminal.KeyEnter:
		// Commit-and-listen: apply, park in slot 0, roll transport. This is
		// the edit-listen loop the grid exists for.
		a.exec("apply " + b.name)
		a.exec("slot 0 " + b.name)
		if !a.s.eng.IsMusicPlaying() && !a.s.eng.IsMusicMuted() {
			a.exec("music start")
		}
		return
	case ev.Key == terminal.KeyLeft || r == 'h':
		b.step--
	case ev.Key == terminal.KeyRight || r == 'l':
		b.step++
	case ev.Key == terminal.KeyUp || r == 'k':
		b.trk--
	case ev.Key == terminal.KeyDown || r == 'j':
		b.trk++
	case r == 'g':
		b.step = 0
	case r == 'G':
		b.step = d.Steps - 1
	case ev.Key == terminal.KeySpace || r == ' ':
		a.beatToggle(d)
	case r == '+' || r == '=':
		a.beatVelNudge(d, +0.1)
	case r == '-':
		a.beatVelNudge(d, -0.1)
	case r >= '1' && r <= '9':
		a.beatVelSet(d, float64(r-'0')*0.1)
	case r == '0':
		a.beatVelSet(d, 1.0)
	case r == 't':
		a.beatCycleInstr(d)
	case r == 'a':
		a.exec(fmt.Sprintf("add %s.track", b.name))
		if d2 := a.beatPat(); d2 != nil {
			ti := len(d2.Track) - 1
			// A zero TrackDef has no instrument and cannot validate; seed it.
			a.exec(fmt.Sprintf("set %s.track.%d.instr kick", b.name, ti))
			b.trk = ti
		}
	case r == 'x':
		if len(d.Track) > 0 {
			a.exec(fmt.Sprintf("del %s.track.%d", b.name, b.trk))
		}
	}
	// Re-read: the keystroke may have changed track count or steps.
	if d = a.beatPat(); d == nil {
		a.mode = modeNormal
		a.beat = nil
		return
	}
	if b.trk > len(d.Track)-1 {
		b.trk = len(d.Track) - 1
	}
	if b.trk < 0 {
		b.trk = 0
	}
	if b.step > d.Steps-1 {
		b.step = d.Steps - 1
	}
	if b.step < 0 {
		b.step = 0
	}
}

func (a *tuiApp) beatToggle(d *audio.PatternDef) {
	b := a.beat
	if len(d.Track) == 0 {
		return
	}
	if i := eventAt(d, b.trk, b.step); i >= 0 {
		a.exec(fmt.Sprintf("del %s.track.%d.event.%d", b.name, b.trk, i))
		return
	}
	base := fmt.Sprintf("%s.track.%d.event", b.name, b.trk)
	a.exec("add " + base)
	if d2 := a.beatPat(); d2 != nil {
		i := len(d2.Track[b.trk].Event) - 1
		a.exec(fmt.Sprintf("set %s.%d.pos %d", base, i, b.step))
		a.exec(fmt.Sprintf("set %s.%d.vel %g", base, i, beatDefaultVel))
	}
}

func (a *tuiApp) beatVelNudge(d *audio.PatternDef, delta float64) {
	b := a.beat
	i := eventAt(d, b.trk, b.step)
	if i < 0 {
		return
	}
	v := d.Track[b.trk].Event[i].Vel + delta
	a.beatVelSetIdx(i, min(max(v, 0.05), 1.0))
}

func (a *tuiApp) beatVelSet(d *audio.PatternDef, v float64) {
	if i := eventAt(d, a.beat.trk, a.beat.step); i >= 0 {
		a.beatVelSetIdx(i, v)
	}
}

func (a *tuiApp) beatVelSetIdx(i int, v float64) {
	b := a.beat
	a.exec(fmt.Sprintf("set %s.track.%d.event.%d.vel %.2f", b.name, b.trk, i, v))
}

func (a *tuiApp) beatCycleInstr(d *audio.PatternDef) {
	b := a.beat
	if len(d.Track) == 0 {
		return
	}
	cur := d.Track[b.trk].Instr
	next := b.instrCycle[0]
	for i, n := range b.instrCycle {
		if n == cur {
			next = b.instrCycle[(i+1)%len(b.instrCycle)]
			break
		}
	}
	a.exec(fmt.Sprintf("set %s.track.%d.instr %s", b.name, b.trk, next))
}

// renderBeat draws the grid: one row per track (instrument label + cells),
// velocity as glyph weight. The playhead column highlights only when this
// pattern is what slot 0 is sounding — a grid for pattern A must not animate
// to pattern B's clock.
func (a *tuiApp) renderBeat(r tui.Region) {
	b := a.beat
	d := a.beatPat()
	if d == nil {
		return
	}
	r.Fill(a.theme.Bg)

	playCol := -1
	if _, step, running := a.s.eng.Transport(); running && d.Steps > 0 &&
		a.s.eng.SlotPattern(0) == audio.PatternIDByName(b.name) {
		playCol = step % d.Steps
	}

	const labW = 7
	vis := r.H // tracks that fit; scroll window follows the cursor
	top := 0
	if b.trk >= vis {
		top = b.trk - vis + 1
	}
	for row := range vis {
		ti := top + row
		if ti >= len(d.Track) {
			break
		}
		tr := &d.Track[ti]
		labFg := a.theme.HintFg
		if ti == b.trk {
			labFg = a.theme.HeaderFg
		}
		r.Text(0, row, fmt.Sprintf("%-6s", tui.Truncate(tr.Instr, 6)), labFg, a.theme.Bg, terminal.AttrNone)

		for st := range d.Steps {
			x := labW + st + st/4 // beat gap every 4 steps
			if x >= r.W {
				break
			}
			ch := '·'
			fg := a.theme.Unselected
			if i := eventAt(d, ti, st); i >= 0 {
				v := tr.Event[i].Vel
				switch {
				case v > 0.8:
					ch = '█'
				case v > 0.55:
					ch = '▓'
				case v > 0.3:
					ch = '▒'
				default:
					ch = '░'
				}
				fg = a.theme.Fg
			}
			bg := a.theme.Bg
			if st == playCol {
				bg = a.theme.FocusBg
			}
			if ti == b.trk && st == b.step {
				bg = a.theme.CursorBg
			}
			r.Cell(x, row, ch, fg, bg, terminal.AttrNone)
		}
	}
}
