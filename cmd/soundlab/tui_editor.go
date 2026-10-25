//go:build !vif_headless && !vif_noaudio && !wasm

package main

// The pattern editor: one pattern, new or edited, on two tabs. Keys plays the
// tonal voices and records takes into it; grid lays its tracks on a step grid, or
// one tonal track as a piano roll. Every edit issues a command line through
// Execute (set/add/del/take), so a script can make it and dirty tracking is free.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/pkg/audio"
)

const gridDefaultVel = 0.8

// chances are the probabilities c walks, 1 being always
var chances = []float64{1, 0.75, 0.5, 0.25}

// editState is the pattern editor: the pattern, the tab shown, and the grid's
// cursor over it
type editState struct {
	name      string
	tab       focusArea // focusKeys or focusGrid, the one shown while the mixer has the keys
	trk, step int
	roll      bool // the cursor's tonal track as a piano roll
	pitch     int  // the roll's cursor: octave*len(scale) + degree
	top       int  // the first track row shown, or the roll's highest pitch
	left      int  // the first step shown when the pattern is wider than the view
	xArmed    bool // x pressed once on this track
	delArmed  bool // ^D pressed once
	rh        int  // rows a track takes, more on a tall screen
	// what the next note placed takes, carried from the last one touched
	deg, oct, dur int
}

// openEditor edits the document's pattern n on a tab, the keys or the grid,
// closing the pattern in hand first
func (a *tuiApp) openEditor(n string, tab focusArea) {
	d, ok := a.s.pats.get(n)
	if !ok {
		return
	}
	if d.Steps <= 0 || d.Steps > audio.MaxPatternLen {
		fmt.Fprintf(a.log, "%q: steps %d — set %s.steps first\n", n, d.Steps, n)
		return
	}
	if a.ed != nil {
		a.closeEditor()
	}
	a.ed = &editState{name: n, tab: tab, oct: 2, dur: 2}
	a.view, a.focus, a.kind = viewEdit, tab, kindPattern
	a.cursorTo(n)
}

// closeEditor goes back to the browser, on the pattern, writing a take being
// recorded into it first
func (a *tuiApp) closeEditor() {
	if a.keys.rec {
		a.finishRecording()
	}
	a.view, a.focus, a.ed, a.fresh = viewBrowse, focusMain, nil, ""
}

// editDelete deletes the pattern on a second ^D and takes it out of its slot
func (a *tuiApp) editDelete() {
	ed := a.ed
	if !ed.delArmed {
		ed.delArmed = true
		fmt.Fprintf(a.log, "delete %q? ^D again deletes it, out of its slot too\n", ed.name)
		return
	}
	if id := audio.PatternIDByName(ed.name); id != audio.PatternSilence {
		if slot := a.slotOf(id); slot >= 0 {
			a.exec(fmt.Sprintf("slot %d -", slot))
			a.stopIfClear(slot)
		}
	}
	a.keys.rec, a.keys.take = false, a.keys.take[:0] // a take goes with its pattern
	a.exec("del " + ed.name)
	a.closeEditor()
}

// homeSlot is the slot the editor plays a pattern in: the one it plays in, its
// role's, the first silent one, else slot 2, the free layer
func (a *tuiApp) homeSlot(n string) int {
	if id := audio.PatternIDByName(n); id != audio.PatternSilence {
		if slot := a.slotOf(id); slot >= 0 {
			return slot
		}
	}
	if slot := a.roleSlot(n); slot >= 0 {
		return slot
	}
	for i := range audio.MusicSlots {
		if a.s.eng.SlotPattern(i) == audio.PatternSilence {
			return i
		}
	}
	return 2
}

// editorKey takes the editor's own keys, then hands the rest to the tab or the
// mixer that has them
func (a *tuiApp) editorKey(ev terminal.Event) {
	d := a.edPat()
	if d == nil { // deleted out from under the editor via :del
		a.closeEditor()
		return
	}
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	if ev.Key != terminal.KeyCtrlD {
		a.ed.delArmed = false
	}
	switch {
	case ev.Key == terminal.KeyEscape:
		a.closeEditor()
	case ev.Key == terminal.KeyEnter:
		a.editPlay()
	case ev.Key == terminal.KeyCtrlD:
		a.editDelete()
	case ev.Key == terminal.KeyCtrlR:
		a.focus, a.ed.tab = focusKeys, focusKeys
		a.toggleRecord(d)
	case a.focus == focusMixer:
		a.mixerKey(ev, r)
	case a.focus == focusKeys:
		a.keysKey(ev)
	default:
		a.gridKey(ev, d, r)
	}
}

// edPat re-reads the document on every access: edits go through Execute, which
// replaces what a held pointer would show. nil when the pattern was deleted.
func (a *tuiApp) edPat() *audio.PatternDef {
	if a.ed == nil {
		return nil
	}
	d, _ := a.s.pats.get(a.ed.name)
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

// scaleLen is the degrees an octave of the mirrored key holds
func (a *tuiApp) scaleLen() int { return len(audio.ScaleIntervals(a.s.scale)) }

// pitchOf is an event's place in the roll, its octave and degree as one number
func (a *tuiApp) pitchOf(e audio.StepDef) int { return e.Oct*a.scaleLen() + e.Deg }

// splitPitch is a roll pitch's octave and degree
func (a *tuiApp) splitPitch(p int) (oct, deg int) {
	n := a.scaleLen()
	oct = p / n
	if deg = p - oct*n; deg < 0 {
		deg += n
		oct--
	}
	return oct, deg
}

// targets are the events the cursor edits: every one starting at its step on its
// track, or in the roll the one at its pitch
func (a *tuiApp) targets(d *audio.PatternDef) []int {
	b := a.ed
	if b.trk < 0 || b.trk >= len(d.Track) {
		return nil
	}
	var out []int
	for i, e := range d.Track[b.trk].Event {
		if e.Pos == b.step && (!b.roll || a.pitchOf(e) == b.pitch) {
			out = append(out, i)
		}
	}
	return out
}

func (a *tuiApp) tonalTrack(d *audio.PatternDef) bool {
	if a.ed.trk < 0 || a.ed.trk >= len(d.Track) {
		return false
	}
	i, ok := audio.InstrumentByName(d.Track[a.ed.trk].Instr)
	return ok && !i.IsDrum()
}

func (a *tuiApp) gridKey(ev terminal.Event, d *audio.PatternDef, r rune) {
	b := a.ed
	if r != 'x' {
		b.xArmed = false
	}
	switch {
	case ev.Key == terminal.KeyLeft || r == 'h':
		b.step--
	case ev.Key == terminal.KeyRight || r == 'l':
		b.step++
	case r == 'H':
		b.step -= audio.StepsPerBeat
	case r == 'L':
		b.step += audio.StepsPerBeat
	case ev.Key == terminal.KeyUp || r == 'k':
		if b.roll {
			b.pitch++ // up is higher in the roll
		} else {
			b.trk--
		}
	case ev.Key == terminal.KeyDown || r == 'j':
		if b.roll {
			b.pitch--
		} else {
			b.trk++
		}
	case ev.Key == terminal.KeyPageUp:
		if b.roll {
			b.pitch += a.scaleLen()
		} else {
			b.step -= audio.StepsPerBar
		}
	case ev.Key == terminal.KeyPageDown:
		if b.roll {
			b.pitch -= a.scaleLen()
		} else {
			b.step += audio.StepsPerBar
		}
	case r == 'g' || ev.Key == terminal.KeyHome:
		b.step = 0
	case r == 'G' || ev.Key == terminal.KeyEnd:
		b.step = d.Steps - 1
	case ev.Key == terminal.KeySpace || r == ' ':
		a.gridToggle(d)
	case r == '+' || r == '=':
		a.gridEach(d, func(e audio.StepDef) (string, string) { return "vel", fmtF(min(max(e.Vel+0.1, 0.05), 1)) })
	case r == '-':
		a.gridEach(d, func(e audio.StepDef) (string, string) { return "vel", fmtF(min(max(e.Vel-0.1, 0.05), 1)) })
	case r >= '1' && r <= '9':
		v := float64(r-'0') / 10
		a.gridEach(d, func(audio.StepDef) (string, string) { return "vel", fmtF(v) })
	case r == '0':
		a.gridEach(d, func(audio.StepDef) (string, string) { return "vel", "1" })
	case r == 'c':
		a.gridEach(d, func(e audio.StepDef) (string, string) {
			c := slices.Index(chances, cmpF(e.Prob, 1))
			return "prob", fmtF(chances[(c+1)%len(chances)])
		})
	case r == '[' || r == ']':
		dd := 1
		if r == '[' {
			dd = -1
		}
		a.gridTonal(d, func(e audio.StepDef) (string, string) {
			b.dur = min(max(max(e.Dur, 1)+dd, 1), audio.MaxPatternLen)
			return "dur", strconv.Itoa(b.dur)
		})
	case r == ',':
		a.gridShift(d, -1)
	case r == '.':
		a.gridShift(d, 1)
	case r == '{':
		a.gridShift(d, -a.scaleLen())
	case r == '}':
		a.gridShift(d, a.scaleLen())
	case r == 't':
		a.gridCycleInstr(d, false)
	case r == 'T':
		a.gridCycleInstr(d, true)
	case r == 'f':
		if len(d.Track) > 0 {
			a.exec(fmt.Sprintf("set %s.track.%d.follow_chord %t", b.name, b.trk, !d.Track[b.trk].FollowChord))
		}
	case r == 'a':
		instr := "kick"
		if len(d.Track) > 0 {
			instr = d.Track[b.trk].Instr // a new lane layers the voice in hand
		}
		a.exec(fmt.Sprintf("add %s.track", b.name))
		if d2 := a.edPat(); d2 != nil {
			ti := len(d2.Track) - 1
			// A zero TrackDef has no instrument and cannot validate; seed it.
			a.exec(fmt.Sprintf("set %s.track.%d.instr %s", b.name, ti, instr))
			b.trk = ti
		}
	case r == 'x':
		if len(d.Track) == 0 {
			break
		}
		if !b.xArmed {
			b.xArmed = true
			fmt.Fprintf(a.log, "delete track %d (%s, %d events)? x again to confirm\n", b.trk+1, d.Track[b.trk].Instr, len(d.Track[b.trk].Event))
			break
		}
		b.xArmed = false
		a.exec(fmt.Sprintf("del %s.track.%d", b.name, b.trk))
	case r == 's':
		a.promptSteps(d)
	case r == 'r':
		if !b.roll && !a.tonalTrack(d) {
			fmt.Fprintln(a.log, "the piano roll shows a tonal track — t/T moves this one to a voice")
			break
		}
		b.roll = !b.roll
		if b.roll {
			b.pitch = a.rollStart(d)
			lo, hi := a.pitchSpan(d)
			b.top = hi + (a.gridRows(d)-(hi-lo+1))/2 // the notes in the middle
		}
	}
	a.editClamp()
}

// editClamp keeps the cursor on the pattern after a key moved it or an edit
// changed the pattern, and leaves the roll when its track stops being tonal
func (a *tuiApp) editClamp() {
	b := a.ed
	if b == nil {
		return
	}
	d := a.edPat()
	if d == nil {
		a.closeEditor()
		return
	}
	b.trk = min(max(b.trk, 0), max(len(d.Track)-1, 0))
	b.step = min(max(b.step, 0), max(d.Steps-1, 0))
	if b.roll && !a.tonalTrack(d) {
		b.roll = false
	}
}

// rollStart is the pitch the roll opens on: the cursor step's note, else the
// track's lowest, else the second octave's root, where the shipped lines sit
func (a *tuiApp) rollStart(d *audio.PatternDef) int {
	b := a.ed
	evs := d.Track[b.trk].Event
	for _, e := range evs {
		if e.Pos == b.step {
			return a.pitchOf(e)
		}
	}
	if len(evs) > 0 {
		lo := a.pitchOf(evs[0])
		for _, e := range evs {
			lo = min(lo, a.pitchOf(e))
		}
		return lo
	}
	return 2 * a.scaleLen()
}

// gridToggle adds a hit or note at the cursor, or removes what is there. A note
// takes the roll's pitch, or in the grid goes on from the line's last note before
// it, its pitch, length and velocity.
func (a *tuiApp) gridToggle(d *audio.PatternDef) {
	b := a.ed
	if len(d.Track) == 0 {
		fmt.Fprintln(a.log, "no tracks — a adds one")
		return
	}
	if idx := a.targets(d); len(idx) > 0 {
		for i := len(idx) - 1; i >= 0; i-- { // highest first: a delete shifts the rest
			a.exec(fmt.Sprintf("del %s.track.%d.event.%d", b.name, b.trk, idx[i]))
		}
		return
	}
	tonal, vel := a.tonalTrack(d), gridDefaultVel
	switch prev := lastBefore(d.Track[b.trk].Event, b.step); {
	case tonal && b.roll:
		b.oct, b.deg = a.splitPitch(b.pitch)
	case tonal && prev != nil:
		b.deg, b.oct, b.dur, vel = prev.Deg, prev.Oct, max(prev.Dur, 1), prev.Vel
	}
	base := fmt.Sprintf("%s.track.%d.event", b.name, b.trk)
	i := len(d.Track[b.trk].Event) // the index add appends at
	if a.exec("add "+base) != nil {
		return
	}
	a.exec(fmt.Sprintf("set %s.%d.pos %d", base, i, b.step))
	a.exec(fmt.Sprintf("set %s.%d.vel %s", base, i, fmtF(vel)))
	if !tonal {
		return
	}
	for _, f := range []struct {
		key string
		v   int
	}{{"deg", b.deg}, {"oct", b.oct}, {"dur", b.dur}} {
		if f.v != 0 {
			a.exec(fmt.Sprintf("set %s.%d.%s %d", base, i, f.key, f.v))
		}
	}
}

// lastBefore is the note sounding last before step, nil when none does
func lastBefore(evs []audio.StepDef, step int) *audio.StepDef {
	var last *audio.StepDef
	for i := range evs {
		if e := &evs[i]; e.Pos < step && (last == nil || e.Pos > last.Pos) {
			last = e
		}
	}
	return last
}

// gridEach sets one field of every target to what edit returns for it
func (a *tuiApp) gridEach(d *audio.PatternDef, edit func(audio.StepDef) (key, val string)) {
	b := a.ed
	for _, i := range a.targets(d) {
		key, val := edit(d.Track[b.trk].Event[i])
		a.exec(fmt.Sprintf("set %s.track.%d.event.%d.%s %s", b.name, b.trk, i, key, val))
	}
}

// gridTonal is gridEach on a tonal track; a drum has no pitch or length
func (a *tuiApp) gridTonal(d *audio.PatternDef, edit func(audio.StepDef) (key, val string)) {
	if !a.tonalTrack(d) {
		fmt.Fprintln(a.log, "a drum has no pitch or length — t/T moves the track to a voice")
		return
	}
	a.gridEach(d, edit)
}

// gridShift moves the targets by dp degrees, carrying across octaves, and the
// roll's cursor with them
func (a *tuiApp) gridShift(d *audio.PatternDef, dp int) {
	b := a.ed
	if !a.tonalTrack(d) {
		fmt.Fprintln(a.log, "a drum has no pitch — t/T moves the track to a voice")
		return
	}
	for _, i := range a.targets(d) {
		oct, deg := a.splitPitch(a.pitchOf(d.Track[b.trk].Event[i]) + dp)
		base := fmt.Sprintf("%s.track.%d.event.%d", b.name, b.trk, i)
		a.exec(fmt.Sprintf("set %s.deg %d", base, deg))
		a.exec(fmt.Sprintf("set %s.oct %d", base, oct))
		b.oct, b.deg = oct, deg
	}
	if b.roll {
		b.pitch += dp
	}
}

// gridCycleInstr moves the track to the next voice of its family, drums among
// drums and tonal voices among themselves, or with other to the other family
func (a *tuiApp) gridCycleInstr(d *audio.PatternDef, other bool) {
	b := a.ed
	if len(d.Track) == 0 {
		return
	}
	cur, _ := audio.InstrumentByName(d.Track[b.trk].Instr)
	next := cur
	if other {
		next = audio.InstrBass
		if !cur.IsDrum() {
			next = audio.InstrKick
		}
	} else {
		fam := instrFamily(cur)
		for i, n := range fam {
			if n == cur {
				next = fam[(i+1)%len(fam)]
				break
			}
		}
	}
	a.exec(fmt.Sprintf("set %s.track.%d.instr %s", b.name, b.trk, next))
}

// promptSteps asks for the pattern's length; a shorter one drops the events past it
func (a *tuiApp) promptSteps(d *audio.PatternDef) {
	name := a.ed.name
	a.openPrompt(fmt.Sprintf("steps of %s (1-%d): ", name, audio.MaxPatternLen), strconv.Itoa(d.Steps), func(v string) {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > audio.MaxPatternLen {
			fmt.Fprintf(a.log, "steps %q: want 1 to %d\n", v, audio.MaxPatternLen)
			return
		}
		cur := a.edPat()
		if cur == nil {
			return
		}
		for t := range cur.Track {
			for i := len(cur.Track[t].Event) - 1; i >= 0; i-- {
				if cur.Track[t].Event[i].Pos >= n {
					a.exec(fmt.Sprintf("del %s.track.%d.event.%d", name, t, i))
				}
			}
		}
		a.exec(fmt.Sprintf("set %s.steps %d", name, n))
		a.editClamp()
	})
}

// editPlay applies the pattern and plays it in its home slot, starting the
// transport: the edit-listen loop the editor exists for
func (a *tuiApp) editPlay() {
	n := a.ed.name
	if a.exec("apply "+n) != nil {
		return // an older registered copy must not play as this one; the log says why
	}
	slot := a.homeSlot(n)
	if id := audio.PatternIDByName(n); a.s.eng.SlotPattern(slot) == id {
		if !a.s.eng.IsMusicPlaying() && !a.s.eng.IsMusicMuted() {
			a.exec("music start")
		}
		return
	}
	a.placeInSlot(slot, n)
}

func (a *tuiApp) gridHint() string {
	if a.ed != nil && a.ed.roll {
		return "←→ step  ↑↓ pitch  space note  1-0 vel  [ ] length  , . degree  c chance  r grid  enter play  tab mixer  ⇧tab keys  esc close  ? keys"
	}
	return "hjkl move  space hit  1-0 vel  [ ] length  , . degree  t/T voice  a/x track  r roll  s steps  enter play  tab mixer  ⇧tab keys  esc close  ? keys"
}

// gridFit is how a pattern's steps lie across a width: cw cells a step and a
// gap before each beat, or, when even one cell a step is too wide, a window of
// whole bars from first
type gridFit struct{ cw, first, count int }

// gridSpan is the columns n steps take at cw cells a step
func gridSpan(n, cw int) int { return n*cw + max((n-1)/audio.StepsPerBeat, 0) }

func fitGrid(steps, w, cursor, left int) gridFit {
	for cw := 8; cw >= 1; cw-- {
		if gridSpan(steps, cw) <= w {
			return gridFit{cw: cw, count: steps}
		}
	}
	bars := max((w+1)/(gridSpan(audio.StepsPerBar, 1)+1), 1)
	count := bars * audio.StepsPerBar
	first := left - left%audio.StepsPerBar
	if cursor < first || cursor >= first+count {
		first = cursor - cursor%audio.StepsPerBar - (bars-1)*audio.StepsPerBar/2
	}
	first = min(max(first-first%audio.StepsPerBar, 0), max(steps-count, 0))
	return gridFit{cw: 1, first: first, count: min(count, steps-first)}
}

// x is the column of a step, -1 when it lies outside the window
func (g gridFit) x(step int) int {
	if step < g.first || step >= g.first+g.count {
		return -1
	}
	s := step - g.first
	return s*g.cw + s/audio.StepsPerBeat
}

// gridLabW is the grid's column of track names, or of the roll's pitches
const gridLabW = 8

// gridRows is the rows the grid or the roll wants: every track, or the notes'
// span of pitches with an octave's headroom, at least two octaves
func (a *tuiApp) gridRows(d *audio.PatternDef) int {
	if !a.ed.roll {
		return min(max(len(d.Track), 3), 32)
	}
	n := a.scaleLen()
	lo, hi := a.pitchSpan(d)
	return min(max(hi-lo+1+n, 2*n+1), 3*n+1)
}

// pitchSpan is the lowest and highest pitch of the roll's track, the cursor's
// pitch alone when it is empty
func (a *tuiApp) pitchSpan(d *audio.PatternDef) (lo, hi int) {
	lo, hi = a.ed.pitch, a.ed.pitch
	for _, e := range d.Track[a.ed.trk].Event {
		lo, hi = min(lo, a.pitchOf(e)), max(hi, a.pitchOf(e))
	}
	return lo, hi
}

// gridViewH is the height the grid tab wants: the frame, the family and ruler,
// the grid or roll, and two rows of detail
func (a *tuiApp) gridViewH(d *audio.PatternDef) int { return 2 + 2 + a.gridRows(d)*a.ed.rh + 2 }

// renderEditor frames the pattern under its tabs and draws the tab shown, the
// mixer beneath where it fits; both tabs are a Tab away
func (a *tuiApp) renderEditor(r tui.Region) {
	b, d := a.ed, a.edPat()
	if b == nil || d == nil { // deleted out from under the editor via :del
		a.closeEditor()
		a.renderBrowse(r)
		return
	}
	a.panes = append(a.panes, focusKeys, focusGrid)
	if a.focus == focusKeys || a.focus == focusGrid {
		b.tab = a.focus
	}
	if b.tab == focusKeys {
		a.renderKeys(a.editFrame(a.stackBelow(r, a.keysViewH(r.W, d)), d), d)
		return
	}
	// A track's rows grow with its cells, to half a cell's width, on a screen tall
	// enough that the view still takes no more than half the body
	g := fitGrid(d.Steps, r.W-2-gridLabW, b.step, b.left)
	b.rh = 1
	if !b.roll && len(d.Track) > 0 {
		b.rh = min(3, max(g.cw/2, 1), max((r.H/2-6)/len(d.Track), 1))
	}
	a.renderGrid(a.editFrame(a.stackBelow(r, a.gridViewH(d)), d), d, g)
}

// editFrame frames the editor in the focus color while a tab has the keys: the
// tabs on its top border, the one shown lit, then whether the pattern is new or
// edited, and what it is
func (a *tuiApp) editFrame(r tui.Region, d *audio.PatternDef) tui.Region {
	th := &a.theme
	border := th.Border
	if a.focus == focusKeys || a.focus == focusGrid {
		border = th.Selected
	}
	c := r.Pane(tui.PaneOpts{Border: tui.LineRounded, BorderFg: border, Bg: th.Bg})
	x := 2
	for _, t := range []struct {
		f    focusArea
		name string
	}{{focusKeys, "keys"}, {focusGrid, "grid"}} {
		fg, attr := th.HintFg, terminal.AttrNone
		if t.f == a.ed.tab {
			fg, attr = th.HeaderFg, terminal.AttrBold
		}
		r.Text(x, 0, " "+t.name+" ", fg, th.Bg, attr)
		x += tui.RuneLen(t.name) + 3
	}
	state, fg := "edit", th.HintFg
	if a.fresh == d.Name {
		state, fg = "new", th.Warning
	}
	r.Text(x, 0, " "+state+" ", fg, th.Bg, terminal.AttrBold)
	x += len(state) + 2
	r.Text(x, 0, tui.Truncate(a.editTitle(d)+" ", max(r.W-x-2, 0)), th.HeaderFg, th.Bg, terminal.AttrBold)
	return c
}

// renderGrid draws the grid tab, kept to the grid's width and centered: the
// cursor track's voice family, the ruler, the grid or roll, and two rows of detail
func (a *tuiApp) renderGrid(frame tui.Region, d *audio.PatternDef, g gridFit) {
	b, th := a.ed, &a.theme
	b.left = g.first
	// Wide cells leave room: the view keeps to the grid's width, centered
	contentW := min(frame.W, max(gridLabW+gridSpan(g.count, g.cw), 64))
	c := frame.Sub((frame.W-contentW)/2, 0, contentW, frame.H)

	mode := "grid"
	if b.roll {
		mode = "roll"
	}
	if len(d.Track) > 0 {
		cur, _ := audio.InstrumentByName(d.Track[b.trk].Instr)
		instrFamilyRow(c, instrFamily(cur), cur, th)
	} else {
		c.Text(0, 0, "no tracks — a adds one", th.HintFg, th.Bg, terminal.AttrNone)
	}
	c.TextRight(0, mode, th.HintFg, th.Bg, terminal.AttrNone)

	play := -1
	if id := audio.PatternIDByName(b.name); id != audio.PatternSilence && a.slotOf(id) >= 0 && a.sig.running {
		play = a.sig.step % d.Steps
	}
	a.drawRuler(c.Sub(gridLabW, 1, c.W-gridLabW, 1), g, play)
	rows := min(a.gridRows(d)*b.rh, max(c.H-4, 0))
	body := c.Sub(0, 2, c.W, rows)
	if b.roll {
		a.drawRoll(body, d, g, gridLabW, play)
	} else {
		a.drawGrid(body, d, g, gridLabW, play, b.rh)
	}
	a.drawGridDetail(c.Sub(0, 2+rows, c.W, 2), d)
}

// editTitle names the pattern, where the game draws it and the slot it plays in
func (a *tuiApp) editTitle(d *audio.PatternDef) string {
	parts := []string{d.Name}
	if d.Role != "" {
		parts = append(parts, d.Role)
	}
	parts = append(parts, lengthWord(d.Steps))
	if len(d.Groups) > 0 {
		parts = append(parts, strings.Join(d.Groups, "+"))
	}
	if d.Motif != "" {
		parts = append(parts, "motif "+d.Motif)
	}
	if len(d.Tiers) > 0 {
		parts = append(parts, strings.Join(d.Tiers, " "))
	}
	if id := audio.PatternIDByName(d.Name); id != audio.PatternSilence && a.slotOf(id) >= 0 {
		parts = append(parts, fmt.Sprintf("slot %d", a.slotOf(id)))
	}
	if a.s.pats.isDirty(d.Name) {
		parts = append(parts, "unapplied")
	}
	return strings.Join(parts, " · ")
}

// lengthWord is a pattern's length in bars, or in steps when not whole bars
func lengthWord(steps int) string {
	switch bars := steps / audio.StepsPerBar; {
	case steps%audio.StepsPerBar != 0 || bars == 0:
		return stepsWord(steps)
	case bars == 1:
		return "1 bar"
	default:
		return fmt.Sprintf("%d bars", bars)
	}
}

// drawRuler numbers the bars and marks the beats over the steps shown
func (a *tuiApp) drawRuler(r tui.Region, g gridFit, play int) {
	th := &a.theme
	for s := g.first; s < g.first+g.count; s++ {
		x := g.x(s)
		ch, fg := ' ', th.Unselected
		switch {
		case s%audio.StepsPerBar == 0:
			label := strconv.Itoa(s/audio.StepsPerBar + 1)
			r.Text(x, 0, label, th.HeaderFg, th.Bg, terminal.AttrBold)
			continue
		case s%audio.StepsPerBeat == 0:
			ch = '·'
		}
		if s == play {
			ch, fg = '▾', th.Selected
		}
		r.Cell(x, 0, ch, fg, th.Bg, terminal.AttrNone)
	}
}

// cellBg is the background of a step's cells: the cursor, the playhead, or none
func (a *tuiApp) cellBg(cursor bool, s, play int) color.RGB {
	switch {
	case cursor:
		return a.theme.CursorBg
	case s == play:
		return a.theme.FocusBg
	}
	return a.theme.Bg
}

// drawGrid draws each track over rh rows: a drum's hits as velocity meters, a
// tonal note as its degree (+ for a chord) with its length drawn on, a chance
// under 1 dimmed
func (a *tuiApp) drawGrid(r tui.Region, d *audio.PatternDef, g gridFit, labW, play, rh int) {
	b := a.ed
	th := &a.theme
	vis := max(r.H/rh, 1)
	if b.trk < b.top {
		b.top = b.trk
	}
	if b.trk >= b.top+vis {
		b.top = b.trk - vis + 1
	}
	b.top = min(max(b.top, 0), max(len(d.Track)-vis, 0))
	mid := rh / 2
	for row := 0; row < vis && b.top+row < len(d.Track); row++ {
		ti := b.top + row
		y := row * rh
		tr := &d.Track[ti]
		instr, _ := audio.InstrumentByName(tr.Instr)
		lab := tui.Truncate(tr.Instr, labW-2)
		if tr.FollowChord {
			lab += "~"
		}
		labFg := th.HintFg
		if ti == b.trk {
			labFg = th.HeaderFg
		}
		r.Text(0, y+mid, lab, labFg, th.Bg, terminal.AttrNone)

		held := make([]int, d.Steps) // steps a note started earlier still sounds
		for _, e := range tr.Event {
			for s := e.Pos + 1; s < e.Pos+max(e.Dur, 1) && s < d.Steps; s++ {
				held[s]++
			}
		}
		for s := g.first; s < g.first+g.count; s++ {
			var at []audio.StepDef
			for _, e := range tr.Event {
				if e.Pos == s {
					at = append(at, e)
				}
			}
			c := a.gridCell(at, held[s] > 0, instr.IsDrum(), s, g.cw)
			bg := a.cellBg(ti == b.trk && s == b.step, s, play)
			x := labW + g.x(s)
			pad := max(g.cw-1, 1) // a hit leaves its step's last cell as a gap
			for j := range rh {
				text := []rune(tui.PadRight("", g.cw))
				switch {
				case c.vel > 0:
					for i := range pad {
						text[i] = meterGlyph(c.vel, rh, rh-1-j)
					}
				case j == mid:
					text = []rune(tui.PadRight(c.text, g.cw))
				}
				for i, ch := range text[:g.cw] {
					r.Cell(x+i, y+j, ch, c.fg, bg, terminal.AttrNone)
				}
			}
		}
	}
}

// stepCell is what a step of a track draws: a hit's velocity, else a note's
// degree, a sustain's line or a rest's dot
type stepCell struct {
	vel  float64
	text string
	fg   color.RGB
}

func (a *tuiApp) gridCell(at []audio.StepDef, held, drum bool, s, cw int) stepCell {
	th := &a.theme
	if len(at) == 0 {
		switch {
		case held && !drum:
			return stepCell{text: strings.Repeat("─", cw), fg: th.HintFg}
		case s%audio.StepsPerBeat == 0:
			return stepCell{text: "·", fg: th.HintFg}
		}
		return stepCell{text: "·", fg: th.Unselected}
	}
	vel, chance := 0.0, 1.0
	for _, e := range at {
		vel = max(vel, e.Vel)
		chance = min(chance, cmpF(e.Prob, 1))
	}
	fg := th.Fg
	if vel >= 0.75 {
		fg = th.HeaderFg
	}
	if chance < 1 {
		fg = th.HintFg
	}
	if drum {
		return stepCell{vel: max(vel, 0.01), fg: fg}
	}
	lo := at[0]
	for _, e := range at {
		if a.pitchOf(e) < a.pitchOf(lo) {
			lo = e
		}
	}
	text := strconv.Itoa(lo.Deg)
	if len(at) > 1 {
		text += "+"
	}
	if tui.RuneLen(text) > cw {
		text = "*"
		if len(at) == 1 && lo.Deg >= 0 && lo.Deg < 10 {
			text = strconv.Itoa(lo.Deg)
		}
	}
	return stepCell{text: text + strings.Repeat("─", max(cw-1-tui.RuneLen(text), 0)), fg: fg}
}

// eighths fill a cell from the bottom, a level an eighth of it
var eighths = []rune(" ▁▂▃▄▅▆▇█")

// meterGlyph is row up (0 the bottom) of a velocity meter rows tall
func meterGlyph(vel float64, rows, up int) rune {
	total := max(int(vel*float64(8*rows)+0.5), 1)
	return eighths[min(max(total-8*up, 0), 8)]
}

// drawRoll draws the cursor's track as a piano roll: a row per pitch of the
// mirrored key, high at the top, each note a bar as long as it sounds
func (a *tuiApp) drawRoll(r tui.Region, d *audio.PatternDef, g gridFit, labW, play int) {
	b := a.ed
	th := &a.theme
	tr := &d.Track[b.trk]
	if b.pitch > b.top {
		b.top = b.pitch
	}
	if b.pitch <= b.top-r.H {
		b.top = b.pitch + r.H - 1
	}
	for row := range r.H {
		p := b.top - row
		oct, deg := a.splitPitch(p)
		lab := fmt.Sprintf("%d·%d", oct, deg)
		if !tr.FollowChord {
			lab += " " + noteName(a.s.pitch(deg, oct))
		}
		labFg := th.Unselected
		switch {
		case p == b.pitch:
			labFg = th.HeaderFg
		case deg == 0:
			labFg = th.HintFg
		}
		r.Text(0, row, tui.Truncate(lab, labW-1), labFg, th.Bg, terminal.AttrNone)
		for s := g.first; s < g.first+g.count; s++ {
			ch, fg := '·', th.Unselected
			if s%audio.StepsPerBeat != 0 {
				ch = ' '
			}
			for _, e := range tr.Event {
				if a.pitchOf(e) != p || s < e.Pos || s >= e.Pos+max(e.Dur, 1) {
					continue
				}
				ch, fg = '▒', th.Fg
				if s == e.Pos {
					ch = '█'
					if e.Vel >= 0.75 {
						fg = th.HeaderFg
					}
				}
				if cmpF(e.Prob, 1) < 1 {
					fg = th.HintFg
				}
			}
			bg := a.cellBg(p == b.pitch && s == b.step, s, play)
			x := labW + g.x(s)
			for i := range g.cw {
				c := ch
				if i == g.cw-1 && g.cw > 1 && ch != '▒' {
					c = ' ' // a note's start and an empty step keep their last cell clear
				}
				r.Cell(x+i, row, c, fg, bg, terminal.AttrNone)
			}
		}
	}
}

// drawGridDetail spells out the cursor's track and what lies under the cursor
func (a *tuiApp) drawGridDetail(r tui.Region, d *audio.PatternDef) {
	b := a.ed
	th := &a.theme
	if len(d.Track) == 0 {
		return
	}
	tr := &d.Track[b.trk]
	trk := fmt.Sprintf("track %d/%d %s", b.trk+1, len(d.Track), tr.Instr)
	if tr.FollowChord {
		trk += " · follows the chord"
	}
	if tr.Humanize > 0 {
		trk += " · humanize " + fmtF(tr.Humanize)
	}
	trk += fmt.Sprintf(" · %d event(s)", len(tr.Event))
	if b.xArmed {
		trk = "x again deletes this track · " + trk
	}
	r.Text(0, 0, tui.Truncate(trk, r.W), th.HintFg, th.Bg, terminal.AttrNone)

	cell := fmt.Sprintf("step %d/%d (%s)", b.step+1, d.Steps, transportPos(int64(b.step/audio.StepsPerBar), b.step))
	idx := a.targets(d)
	switch {
	case len(idx) == 0 && b.roll:
		oct, deg := a.splitPitch(b.pitch)
		cell += fmt.Sprintf(" · deg %d oct %d", deg, oct)
		if !tr.FollowChord {
			cell += " " + noteName(a.s.pitch(deg, oct))
		}
		cell += " · empty"
	case len(idx) == 0:
		cell += " · empty"
	default:
		e := tr.Event[idx[0]]
		cell += " · vel " + fmtF(e.Vel)
		if a.tonalTrack(d) {
			cell += fmt.Sprintf(" · deg %d oct %d", e.Deg, e.Oct)
			if !tr.FollowChord {
				cell += " " + noteName(a.s.pitch(e.Deg, e.Oct))
			}
			cell += " · " + stepsWord(max(e.Dur, 1))
		}
		cell += " · chance " + fmtF(cmpF(e.Prob, 1))
		if len(idx) > 1 {
			cell += fmt.Sprintf(" · %d notes", len(idx))
		}
	}
	r.Text(0, 1, tui.Truncate(cell, r.W), th.Fg, th.Bg, terminal.AttrNone)
}

// cmpF is v, or dflt where v is the zero that means it
func cmpF(v, dflt float64) float64 {
	if v == 0 {
		return dflt
	}
	return v
}

func fmtF(v float64) string { return strconv.FormatFloat(v, 'g', 3, 64) }
