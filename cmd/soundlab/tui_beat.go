//go:build !vif_headless && !vif_noaudio && !wasm

package main

// The beat view: a pattern's tracks on a step grid, or one tonal track as a
// piano roll, over a document PatternDef. Every edit issues a command line
// through Execute (set/add/del), so the view is a macro keyboard over the same
// table: script parity holds, dirty tracking is free, and the inspector agrees.

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

const beatDefaultVel = 0.8

// chances are the probabilities p walks, 1 being always
var chances = []float64{1, 0.75, 0.5, 0.25}

// beatState is the beat view's cursor over its pattern
type beatState struct {
	name      string
	trk, step int
	roll      bool // the cursor's tonal track as a piano roll
	pitch     int  // the roll's cursor: octave*len(scale) + degree
	top       int  // the first track row shown, or the roll's highest pitch
	left      int  // the first step shown when the pattern is wider than the view
	xArmed    bool // x pressed once on this track
	rh        int  // rows a track takes, more on a tall screen
	// what the next note placed takes, carried from the last one touched
	deg, oct, dur int
}

func (a *tuiApp) enterBeat() {
	n, ok := a.addressableSel()
	if !ok {
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
	a.beat = &beatState{name: n, oct: 2, dur: 2}
	a.view = viewBeat
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
	b := a.beat
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
	if a.beat.trk < 0 || a.beat.trk >= len(d.Track) {
		return false
	}
	i, ok := audio.InstrumentByName(d.Track[a.beat.trk].Instr)
	return ok && !i.IsDrum()
}

func (a *tuiApp) beatKey(ev terminal.Event) {
	b := a.beat
	d := a.beatPat()
	if d == nil { // deleted out from under the editor via :del
		a.view, a.beat = viewBrowse, nil
		return
	}
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	if r != 'x' {
		b.xArmed = false
	}
	switch {
	case ev.Key == terminal.KeyEscape:
		a.view, a.beat = viewBrowse, nil
		return
	case ev.Key == terminal.KeyCtrlS:
		a.saveModified()
		return
	case ev.Key == terminal.KeyEnter:
		a.beatPlay(d)
		return
	case r == '?':
		a.help = true
		return
	case r == ':':
		a.openCommand()
		return
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
		a.beatToggle(d)
	case r == '+' || r == '=':
		a.beatEach(d, func(e audio.StepDef) (string, string) { return "vel", fmtF(min(max(e.Vel+0.1, 0.05), 1)) })
	case r == '-':
		a.beatEach(d, func(e audio.StepDef) (string, string) { return "vel", fmtF(min(max(e.Vel-0.1, 0.05), 1)) })
	case r >= '1' && r <= '9':
		v := float64(r-'0') / 10
		a.beatEach(d, func(audio.StepDef) (string, string) { return "vel", fmtF(v) })
	case r == '0':
		a.beatEach(d, func(audio.StepDef) (string, string) { return "vel", "1" })
	case r == 'p':
		a.beatEach(d, func(e audio.StepDef) (string, string) {
			c := slices.Index(chances, cmpF(e.Prob, 1))
			return "prob", fmtF(chances[(c+1)%len(chances)])
		})
	case r == '[' || r == ']':
		dd := 1
		if r == '[' {
			dd = -1
		}
		a.beatTonal(d, func(e audio.StepDef) (string, string) {
			b.dur = min(max(max(e.Dur, 1)+dd, 1), audio.MaxPatternLen)
			return "dur", strconv.Itoa(b.dur)
		})
	case r == ',' || r == '<':
		a.beatShift(d, -1)
	case r == '.' || r == '>':
		a.beatShift(d, 1)
	case r == '{':
		a.beatShift(d, -a.scaleLen())
	case r == '}':
		a.beatShift(d, a.scaleLen())
	case r == 't':
		a.beatCycleInstr(d, false)
	case r == 'T':
		a.beatCycleInstr(d, true)
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
		if d2 := a.beatPat(); d2 != nil {
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
			b.top = hi + (a.beatRows(d)-(hi-lo+1))/2 // the notes in the middle
		}
	}
	a.beatClamp()
}

// beatClamp keeps the cursor on the pattern after a key moved it or an edit
// changed the pattern, and leaves the roll when its track stops being tonal
func (a *tuiApp) beatClamp() {
	b := a.beat
	if b == nil {
		return
	}
	d := a.beatPat()
	if d == nil {
		a.view, a.beat = viewBrowse, nil
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
	b := a.beat
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

// beatToggle adds a hit or note at the cursor, or removes what is there. A note
// takes the roll's pitch, or in the grid goes on from the line's last note before
// it, its pitch, length and velocity.
func (a *tuiApp) beatToggle(d *audio.PatternDef) {
	b := a.beat
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
	tonal, vel := a.tonalTrack(d), beatDefaultVel
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

// beatEach sets one field of every target to what edit returns for it
func (a *tuiApp) beatEach(d *audio.PatternDef, edit func(audio.StepDef) (key, val string)) {
	b := a.beat
	for _, i := range a.targets(d) {
		key, val := edit(d.Track[b.trk].Event[i])
		a.exec(fmt.Sprintf("set %s.track.%d.event.%d.%s %s", b.name, b.trk, i, key, val))
	}
}

// beatTonal is beatEach on a tonal track; a drum has no pitch or length
func (a *tuiApp) beatTonal(d *audio.PatternDef, edit func(audio.StepDef) (key, val string)) {
	if !a.tonalTrack(d) {
		fmt.Fprintln(a.log, "a drum has no pitch or length — t/T moves the track to a voice")
		return
	}
	a.beatEach(d, edit)
}

// beatShift moves the targets by dp degrees, carrying across octaves, and the
// roll's cursor with them
func (a *tuiApp) beatShift(d *audio.PatternDef, dp int) {
	b := a.beat
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

// beatCycleInstr moves the track to the next voice of its family, drums among
// drums and tonal voices among themselves, or with other to the other family
func (a *tuiApp) beatCycleInstr(d *audio.PatternDef, other bool) {
	b := a.beat
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
	name := a.beat.name
	a.openPrompt(fmt.Sprintf("steps of %s (1-%d): ", name, audio.MaxPatternLen), strconv.Itoa(d.Steps), func(v string) {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > audio.MaxPatternLen {
			fmt.Fprintf(a.log, "steps %q: want 1 to %d\n", v, audio.MaxPatternLen)
			return
		}
		cur := a.beatPat()
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
		a.beatClamp()
	})
}

// beatPlay applies the pattern and plays it in its role's slot, slot 0 for none,
// starting the transport: the edit-listen loop the view exists for
func (a *tuiApp) beatPlay(d *audio.PatternDef) {
	b := a.beat
	if a.exec("apply "+b.name) != nil {
		return // an older registered copy must not play as this one; the log says why
	}
	slot := max(a.roleSlot(b.name), 0)
	if id := audio.PatternIDByName(b.name); a.s.eng.SlotPattern(slot) == id {
		if !a.s.eng.IsMusicPlaying() && !a.s.eng.IsMusicMuted() {
			a.exec("music start")
		}
		return
	}
	a.placeInSlot(slot, b.name)
}

func (a *tuiApp) beatHint() string {
	if a.beat != nil && a.beat.roll {
		return "←→ step  ↑↓ pitch  space note  1-0 vel  [ ] length  , . degree  p chance  r grid  enter play  esc close  ? keys"
	}
	return "hjkl move  space hit  1-0 vel  [ ] length  , . degree  t/T voice  a/x track  r roll  s steps  enter play  esc close  ? keys"
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

// beatLabW is the beat view's column of track names, or of the roll's pitches
const beatLabW = 8

// beatRows is the rows the grid or the roll wants: every track, or the notes'
// span of pitches with an octave's headroom, at least two octaves
func (a *tuiApp) beatRows(d *audio.PatternDef) int {
	if !a.beat.roll {
		return min(max(len(d.Track), 3), 32)
	}
	n := a.scaleLen()
	lo, hi := a.pitchSpan(d)
	return min(max(hi-lo+1+n, 2*n+1), 3*n+1)
}

// pitchSpan is the lowest and highest pitch of the roll's track, the cursor's
// pitch alone when it is empty
func (a *tuiApp) pitchSpan(d *audio.PatternDef) (lo, hi int) {
	lo, hi = a.beat.pitch, a.beat.pitch
	for _, e := range d.Track[a.beat.trk].Event {
		lo, hi = min(lo, a.pitchOf(e)), max(hi, a.pitchOf(e))
	}
	return lo, hi
}

// beatViewH is the height the beat view wants: the frame, the family and ruler,
// the grid or roll, and two rows of detail
func (a *tuiApp) beatViewH(d *audio.PatternDef) int { return 2 + 2 + a.beatRows(d)*a.beat.rh + 2 }

func (a *tuiApp) renderBeatView(r tui.Region) {
	b := a.beat
	d := a.beatPat()
	if b == nil || d == nil {
		a.view, a.beat = viewBrowse, nil
		a.renderBrowse(r)
		return
	}
	th := &a.theme
	// A track's rows grow with its cells, to half a cell's width, on a screen tall
	// enough that the view still takes no more than half the body
	g := fitGrid(d.Steps, r.W-2-beatLabW, b.step, b.left)
	b.rh = 1
	if !b.roll && len(d.Track) > 0 {
		b.rh = min(3, max(g.cw/2, 1), max((r.H/2-6)/len(d.Track), 1))
	}
	r = a.stackBelow(r, a.beatViewH(d))
	frame := a.viewPane(r, a.beatTitle(d))
	b.left = g.first
	// Wide cells leave room: the view keeps to the grid's width, centered
	contentW := min(frame.W, max(beatLabW+gridSpan(g.count, g.cw), 64))
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
	a.drawRuler(c.Sub(beatLabW, 1, c.W-beatLabW, 1), g, play)
	rows := min(a.beatRows(d)*b.rh, max(c.H-4, 0))
	body := c.Sub(0, 2, c.W, rows)
	if b.roll {
		a.drawRoll(body, d, g, beatLabW, play)
	} else {
		a.drawGrid(body, d, g, beatLabW, play, b.rh)
	}
	a.drawBeatDetail(c.Sub(0, 2+rows, c.W, 2), d)
}

// beatTitle names the pattern and where the game draws it
func (a *tuiApp) beatTitle(d *audio.PatternDef) string {
	parts := []string{"beat · " + d.Name}
	if d.Role != "" {
		parts = append(parts, d.Role)
	}
	bars := d.Steps / audio.StepsPerBar
	if d.Steps%audio.StepsPerBar == 0 && bars > 0 {
		parts = append(parts, fmt.Sprintf("%d bar%s", bars, map[bool]string{true: "s"}[bars > 1]))
	} else {
		parts = append(parts, fmt.Sprintf("%d steps", d.Steps))
	}
	if len(d.Groups) > 0 {
		parts = append(parts, strings.Join(d.Groups, "+"))
	}
	if d.Motif != "" {
		parts = append(parts, "motif "+d.Motif)
	}
	if len(d.Tiers) > 0 {
		parts = append(parts, strings.Join(d.Tiers, " "))
	}
	if a.s.pats.isDirty(d.Name) {
		parts = append(parts, "unapplied")
	}
	return strings.Join(parts, " · ")
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
	b := a.beat
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
	b := a.beat
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

// drawBeatDetail spells out the cursor's track and what lies under the cursor
func (a *tuiApp) drawBeatDetail(r tui.Region, d *audio.PatternDef) {
	b := a.beat
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
