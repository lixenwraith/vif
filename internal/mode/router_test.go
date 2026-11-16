package mode

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestASecondPressOnACellFiresSpecial: two left presses on one cell within the
// window fire main twice and special once; a press on another cell, or one past
// the window, fires main alone, and the third press of a run starts over.
func TestASecondPressOnACellFiresSpecial(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	fired := func() (main, special int) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Type {
			case event.EventWeaponFireRequest:
				main++
			case event.EventFireSpecialRequest:
				special++
			}
		}
		return main, special
	}
	press := func(x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
		r.Handle(machine.Process(terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionRelease, MouseX: x, MouseY: y}))
	}
	press(10, 10)
	press(10, 10)
	if main, special := fired(); main != 2 || special != 1 {
		t.Fatalf("a pair on one cell fired main %d special %d, want 2 and 1", main, special)
	}
	press(10, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("the third press fired main %d special %d, want 1 and 0", main, special)
	}
	press(11, 11)
	if main, special := fired(); main != 1 || special != 1 {
		t.Fatalf("a press a cell off, a finger's jitter, fired main %d special %d, want 1 and 1", main, special)
	}
	press(14, 11)
	press(12, 10)
	if main, special := fired(); main != 2 || special != 0 {
		t.Fatalf("presses past the slack fired main %d special %d, want 2 and 0", main, special)
	}
	r.leftPressAt = r.leftPressAt.Add(-parameter.MouseDoubleClickWindow - time.Millisecond)
	press(12, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("a press past the window fired main %d special %d, want 1 and 0", main, special)
	}
}

// TestTheMenuKeepsThePointerWithTheMouseDisabled: with mouse input disabled a
// press on the map is nothing, a press held on the status bar still opens the
// menu, which turns pointer reporting on for its rows, and a press there selects.
func TestTheMenuKeepsThePointerWithTheMouseDisabled(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	var reporting bool
	r.SetMouseModeApplier(func(enabled, _ bool) { reporting = enabled })
	ctx.MouseDisabled.Store(true)
	ctx.AutoFire.Store(engine.AutoFireOff) // the tick's own fire requests would count as the press's
	r.ProcessInputTick()
	if reporting {
		t.Fatal("the mouse disabled, yet reported")
	}
	down := func(x, y int) {
		t.Helper()
		if intent := machine.Process(terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}); intent != nil {
			r.Handle(intent)
		}
	}
	down(10, 10)
	if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 || r.mouseLeftHeld {
		t.Fatalf("a press on the map was taken with the mouse disabled: held %v, events %d %v", r.mouseLeftHeld, len(evs), evs)
	}
	down(10, ctx.Height-1)
	if !r.menuHold {
		t.Fatal("a press on the status bar did not start the hold")
	}
	r.menuSince = r.menuSince.Add(-parameter.MenuHoldDuration)
	r.ProcessInputTick()
	r.ProcessInputTick() // the tick after the menu opened applies its reporting
	if configMenu(ctx) == nil || !reporting {
		t.Fatalf("after the hold: menu %v, reporting %v", configMenu(ctx) != nil, reporting)
	}
	g := ctx.OverlayGeometry()
	down(g.X+g.ContentX, g.Y+g.ContentY+2)
	if ctx.GetOverlaySelection() != "multiplayer" {
		t.Fatalf("a press on the third row selected %q", ctx.GetOverlaySelection())
	}
}

// TestAHoldTakesNoPlayButOpensTheMenu: under a hold a key that plays and a press on
// the map do nothing, while the command line opens and the status bar's press opens
// the menu, whose Pause row reads Resume and ends the hold.
func TestAHoldTakesNoPlayButOpensTheMenu(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	ctx.AutoFire.Store(engine.AutoFireOff) // the tick's own fire requests would count as play
	ctx.TimeCtl.SetHeld(true)
	key := func(k terminal.Key, ch rune) {
		if in := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); in != nil {
			r.Handle(in)
		}
	}
	mouse := func(action terminal.MouseAction, x, y int) {
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: action, MouseX: x, MouseY: y}
		if in := machine.Process(ev); in != nil {
			r.Handle(in)
		}
	}
	key(terminal.KeyRune, 'l')
	key(terminal.KeyRune, 'i')
	mouse(terminal.MouseActionPress, 10, 10)
	mouse(terminal.MouseActionRelease, 10, 10)
	if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 || ctx.GetMode() != core.ModeNormal {
		t.Fatalf("play under a hold left mode %v and events %v", ctx.GetMode(), evs)
	}
	key(terminal.KeyRune, ':')
	if !ctx.IsCommandMode() {
		t.Fatal("the command line did not open under a hold")
	}
	key(terminal.KeyEscape, 0)

	mouse(terminal.MouseActionPress, 10, ctx.GameYOffset+ctx.World.Resources.Config.ViewportHeight)
	r.menuSince = r.menuSince.Add(-parameter.MenuHoldDuration)
	r.ProcessInputTick()
	if configMenu(ctx) == nil {
		t.Fatal("the status bar's press opened no menu under a hold")
	}
	ctx.SetOverlaySelection("game")
	key(terminal.KeyEnter, 0)
	m := configMenu(ctx)
	i := slices.IndexFunc(m.Rows, func(row core.OverlayMenuRow) bool { return row.Key == "game/pause" })
	if i < 0 || m.Rows[i].Label != "Resume" || m.Rows[i].Disabled != "" {
		t.Fatalf("the Game page under a hold shows %+v", m.Rows)
	}
	w.Resources.Event.Queue.Consume()
	ctx.SetOverlaySelection("game/pause")
	key(terminal.KeyEnter, 0)
	var asked []bool
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.GamePausePayload); ok && ev.Type == event.EventGameHoldRequest {
			asked = append(asked, p.Paused)
		}
	}
	if !slices.Equal(asked, []bool{false}) {
		t.Fatalf("the Pause row asked for holds %v, want its end alone", asked)
	}
}

// TestAHeldKeyCrossesOneReleaseHoweverItEnds: a captured key held where keys release
// crosses its own release; play stopping, or the capture giving the key back, ends a
// hold as lost, and a keyboard reporting presses alone taps. Keys coming to be
// captured, and each answer while they are, cross the slot's report.
func TestAHeldKeyCrossesOneReleaseHoweverItEnds(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	var asks []bool
	r.SetKeyReportApplier(func(console bool) { asks = append(asks, console) })
	crossed := func() (out []string) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.InputPayload:
				if ev.Type == event.EventInputPressed {
					out = append(out, "press "+p.Key)
				} else if ev.Type == event.EventInputReleased {
					out = append(out, fmt.Sprintf("release %s lost=%v", p.Key, p.Lost))
				}
			case *event.InputReportPayload:
				out = append(out, fmt.Sprintf("report releases=%v", p.Releases))
			}
		}
		return out
	}
	expect := func(step string, want ...string) {
		t.Helper()
		if got := crossed(); !slices.Equal(got, want) {
			t.Fatalf("%s crossed %q, want %q", step, got, want)
		}
	}
	key := func(k rune, action terminal.KeyAction) {
		if in := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: k, Action: action}); in != nil {
			r.Handle(in)
		}
	}

	ctx.KeyCapture.Set("12", 0, false, true)
	r.ProcessInputTick()
	r.KeyReported(true)
	expect("the answer to the console ask", "report releases=true")
	key('1', terminal.KeyPress)
	key('1', terminal.KeyRepeat)
	key('1', terminal.KeyRelease)
	expect("a press held and let go", "press 1", "release 1 lost=false")

	key('2', terminal.KeyPress)
	r.Handle(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1})
	r.ProcessInputTick()
	key('2', terminal.KeyRelease)
	expect("a hold as the command line opens", "press 2", "release 2 lost=true")

	r.transitionMode(core.ModeNormal)
	key('1', terminal.KeyPress)
	ctx.KeyCapture.Set("2", 0, false, true)
	r.ProcessInputTick()
	expect("a hold the capture gives back", "press 1", "release 1 lost=true")

	r.KeyReported(false)
	key('2', terminal.KeyPress)
	expect("a press with presses alone reported", "report releases=false", "press 2", "release 2 lost=true")

	ctx.KeyCapture.Set("", 0, false, false)
	r.ProcessInputTick()
	if !slices.Equal(asks, []bool{true, false}) {
		t.Fatalf("asked for the console keyboard %v, want it while the capture asked and not after", asks)
	}
}

// TestEveryPointerPressesAButton: the right button and a finger press a button's key
// and hold it as the left button does, and a wheel notch presses it released at
// once; away from a button the wheel moves and the right button fires as before.
func TestEveryPointerPressesAButton(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	r := NewRouter(ctx, input.NewMachine())
	w.Components.Button.SetComponent(w.CreateEntity(core.DomainShared), component.ButtonComponent{Slot: 1, Key: '1'})
	ctx.KeyCapture.Set("1", 0, false, false)
	config := w.Resources.Config
	bx, by, bw, bh := component.ButtonRect(1, 1, config.MapWidth, config.MapHeight)
	on := func(kind input.IntentType) *input.Intent {
		return &input.Intent{Type: kind, X: bx + bw/2, Y: by + bh/2, MapCell: true}
	}
	crossed := func() (out []string) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.InputPayload:
				if ev.Type == event.EventInputPressed {
					out = append(out, fmt.Sprintf("press source=%d", p.Source))
				} else if ev.Type == event.EventInputReleased {
					out = append(out, fmt.Sprintf("release source=%d lost=%v", p.Source, p.Lost))
				}
			case *event.FireSpecialRequestPayload:
				out = append(out, "special")
			}
		}
		return out
	}

	r.Handle(on(input.IntentMouseRightDown))
	r.Handle(&input.Intent{Type: input.IntentMouseRightUp})
	touch := on(input.IntentMouseLeftDown)
	touch.Touch = true
	r.Handle(touch)
	r.Handle(&input.Intent{Type: input.IntentMouseLeftUp})
	wheel := on(input.IntentMouseWheelMove)
	wheel.ScrollDir = input.ScrollDown
	r.Handle(wheel)
	r.Handle(&input.Intent{Type: input.IntentMouseWheelMove, X: 1, Y: 1, MapCell: true})
	r.Handle(&input.Intent{Type: input.IntentMouseRightDown, X: 1, Y: 1, MapCell: true})

	want := []string{
		"press source=2", "release source=2 lost=false",
		"press source=5", "release source=5 lost=false",
		"press source=4", "release source=4 lost=true",
		"special",
	}
	if got := crossed(); !slices.Equal(got, want) {
		t.Fatalf("crossed %q, want %q", got, want)
	}
}

// lineWorld is a bare world holding text's lines as the player's glyphs from row 0,
// the local cursor at column x of the first, and a router fed by keys. It applies
// what each key asks for before the next, as the systems would, and returns the
// edits: a delete as its span "x-x", "line" or "block", a put as "put", a retype as
// "retype" and its text.
func lineWorld(t *testing.T, text string, x int) (*engine.GameContext, func(keys string) []string) {
	t.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	lay := func(x, y int, g component.GlyphComponent) {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		w.Components.Glyph.SetComponent(e, g)
	}
	for y, line := range strings.Split(text, "\n") {
		for i, ch := range line {
			if ch != ' ' {
				lay(i, y, component.GlyphComponent{Rune: ch})
			}
		}
	}
	w.Resources.Player.Entity = w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(w.Resources.Player.Entity, component.PositionComponent{X: x, Y: 0})
	machine := input.NewMachine()
	machine.SetKeyTable(input.MergeKeyTable(input.DefaultKeyTable(), vimKeys(t)))
	r := NewRouter(ctx, machine)
	apply := func(edits []string) []string {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.CursorMoveRequestPayload:
				w.Positions.SetPosition(p.Entity, component.PositionComponent{X: p.X, Y: p.Y})
			case *event.DeleteRequestPayload:
				var gone []core.Entity
				w.Components.Glyph.Each(func(e core.Entity, _ *component.GlyphComponent) bool {
					if pos, _ := w.Positions.GetPosition(e); p.Covers(pos.X, pos.Y) {
						gone = append(gone, e)
					}
					return true
				})
				w.DestroyEntitiesBatch(gone)
				edits = append(edits, [...]string{fmt.Sprintf("%d-%d", p.StartX, p.EndX), "line", "block"}[p.RangeType])
			case *event.GlyphReplaceRequestPayload:
				for i, ch := range []rune(p.Text) {
					if e, ok := w.Positions.EditableGlyphAt(p.X+i, p.Y); ok && ch != ' ' {
						g, _ := w.Components.Glyph.GetPtr(e)
						g.Rune = ch
					}
				}
				edits = append(edits, "retype "+p.Text)
			case *event.GlyphPutPayload:
				for _, c := range p.Cells {
					if w.Positions.TakesGlyph(c.X, c.Y) {
						lay(c.X, c.Y, component.GlyphComponent{Rune: c.Rune, Type: c.Type, Level: c.Level})
					}
				}
				edits = append(edits, "put")
			case *event.CharacterTypedPayload:
				// A match clears its glyph and moves on, as typing does
				if e, ok := w.Positions.TopGlyphAt(p.X, p.Y); ok {
					if g, _ := w.Components.Glyph.GetComponent(e); g.Rune == p.Char {
						w.DestroyEntity(e)
						w.Positions.SetPosition(p.Entity, component.PositionComponent{X: p.X + 1, Y: p.Y})
					}
				}
			}
		}
		return edits
	}
	return ctx, func(keys string) (edits []string) {
		for _, ch := range keys {
			ev := terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: ch}
			switch ch {
			case '\x1b':
				ev = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEscape}
			case '\x12':
				ev = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlR}
			}
			for intent := machine.Process(ev); intent != nil; intent = machine.Next() {
				r.Handle(intent)
				edits = apply(edits)
			}
		}
		return edits
	}
}

// vimKeys binds the vim actions no default key has to vim's own keys, as the tutor
// does, a, p, P, o, O, u, Ctrl-R and m, and the backtick to mark_jump, as a keymap may
func vimKeys(t *testing.T) *input.KeyTable {
	t.Helper()
	kt := &input.KeyTable{NormalRunes: map[rune]input.KeyEntry{}, SpecialKeys: map[terminal.Key]input.KeyEntry{}}
	for key, action := range map[rune]string{'a': "mode_append", 'p': "put_after", 'P': "put_before",
		'o': "mode_open_below", 'O': "mode_open_above", 'u': "undo_change", 'm': "mark_set", '`': "mark_jump"} {
		kt.NormalRunes[key], _ = input.ActionEntry(action)
	}
	kt.OperatorMotions = map[rune]input.KeyEntry{'`': kt.NormalRunes['`']}
	kt.SpecialKeys[terminal.KeyCtrlR], _ = input.ActionEntry("redo_change")
	return kt
}

// screen reads the world's glyphs back as text, a row a line, down to the last row
// holding one
func screen(ctx *engine.GameContext) string {
	var rows []string
	ctx.World.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
		pos, _ := ctx.World.Positions.GetPosition(e)
		for len(rows) <= pos.Y {
			rows = append(rows, "")
		}
		row := []rune(rows[pos.Y] + strings.Repeat(" ", max(0, pos.X+1-len(rows[pos.Y]))))
		row[pos.X] = g.Rune
		rows[pos.Y] = string(row)
		return true
	})
	return strings.Join(rows, "\n")
}

// TestAnActionNamingInsertEntersItOnceItActs: c deletes as d does, over its line when
// doubled as dd is, and enters Insert once its range holds, cw ending at its word's
// last glyph; C, s and S do after their delete; A, I and vim's a and o after their
// move, A to the last glyph however often it is pressed.
func TestAnActionNamingInsertEntersItOnceItActs(t *testing.T) {
	ctx, keys := lineWorld(t, "  one two\n    three", 2)
	cursor := func() [2]int { pos, _ := ctx.World.LocalCursor(); return [2]int{pos.X, pos.Y} }
	for _, c := range []struct {
		keys   string
		edits  []string
		insert bool
		at     [2]int
	}{
		{"cw", []string{"2-4"}, true, [2]int{2, 0}},
		{"\x1bcfz", nil, false, [2]int{2, 0}},
		{"A", nil, true, [2]int{8, 0}},
		{"\x1bA", nil, true, [2]int{8, 0}},
		{"\x1bI", nil, true, [2]int{6, 0}},
		{"\x1ba", nil, true, [2]int{7, 0}},
		{"\x1bs", []string{"7-7"}, true, [2]int{7, 0}},
		{"\x1bC", []string{"7-8"}, true, [2]int{7, 0}},
		{"\x1bo", nil, true, [2]int{4, 1}},
		{"\x1bO", nil, true, [2]int{6, 0}},
		{"\x1bS", []string{"line"}, true, [2]int{6, 0}},
		{"\x1bjcc", []string{"line"}, true, [2]int{6, 1}},
		{"\x1bdd", []string{"line"}, false, [2]int{6, 1}},
	} {
		got := keys(c.keys)
		if !slices.Equal(got, c.edits) || ctx.IsInsertMode() != c.insert || cursor() != c.at {
			t.Fatalf("%q: edits %q, insert %v, cursor %v; want %q, %v, %v",
				c.keys, got, ctx.IsInsertMode(), cursor(), c.edits, c.insert, c.at)
		}
	}
}

// TestEveryDeleteFillsTheRegisterAndAPutLaysItOutward: y fills the register and
// deletes nothing, and each delete fills it too. A put lays a span right of the
// cursor, or for P left of it, and rows below or above, a count that many, passing
// over a held cell; the cursor goes to its far end, so a repeat goes on from there.
func TestEveryDeleteFillsTheRegisterAndAPutLaysItOutward(t *testing.T) {
	for _, c := range []struct {
		text       string
		x          int
		keys, want string
		at         [2]int
	}{
		{"ab cd", 0, "yw$p", "ab cdab", [2]int{6, 0}},
		{"ab cd", 0, "yw$p.", "ab cdabab", [2]int{8, 0}},
		{"ab   cd", 5, "xhP", "ab c  d", [2]int{3, 0}},
		{"ab cd", 0, "ywwp", "ab cdb", [2]int{5, 0}},
		{"ab", 0, "yy2p", "ab\nab\nab", [2]int{0, 2}},
		{"\n\nab", 0, "jjddkP", "ab", [2]int{0, 0}},
	} {
		ctx, keys := lineWorld(t, c.text, c.x)
		keys(c.keys)
		if pos, _ := ctx.World.LocalCursor(); screen(ctx) != c.want || [2]int{pos.X, pos.Y} != c.at {
			t.Errorf("%q on %q left %q with the cursor at %d,%d; want %q at %v", c.keys, c.text, screen(ctx), pos.X, pos.Y, c.want, c.at)
		}
	}
}

// TestAChangeIsTakenBackAndMadeAgainWhileItsCellsStand: undo_change takes back the
// last delete, retype or put and redo_change makes it again, a new change ending
// what could be redone. A change whose cells changed under it is dropped for the
// one before, and a glyph clear or a new game ends the history.
func TestAChangeIsTakenBackAndMadeAgainWhileItsCellsStand(t *testing.T) {
	for _, c := range []struct{ text, keys, want string }{
		{"one two", "dwu", "one two"},
		{"one two", "dwu\x12", "    two"},
		{"one two", "rx3~u", "xne two"},
		{"one two", "rx3~uu\x12\x12", "XNE two"},
		{"ab", "yypu", "ab"},
		{"ab", "yypu\x12", "ab\nab"},
		{"ab cd", "ywwpu", "ab cd"},
		{"one two", "xuwx\x12", "one  wo"},
		{"abc", "xlrziz\x1bu", "a c"},
	} {
		ctx, keys := lineWorld(t, c.text, 0)
		if keys(c.keys); screen(ctx) != c.want {
			t.Errorf("%q on %q left %q, want %q", c.keys, c.text, screen(ctx), c.want)
		}
	}
	ctx, keys := lineWorld(t, "abc", 0)
	keys("x")
	ctx.World.Resources.Status.Ints.Get("glyph.cleared").Add(1)
	keys("ulx")
	ctx.World.Resources.Event.Queue.NextRun()
	if keys("u"); screen(ctx) != "  c" {
		t.Fatalf("undos past a glyph clear and a new game left %q, want both deletes to stand", screen(ctx))
	}
}

// TestAMarkNamesACellItsJumpsReturnTo: mark_set names the cursor's cell with a
// letter; mark_jump_line returns to its row at the row's first glyph, taking whole
// rows for an operator, and mark_jump to the cell, an operator stopping short of it.
// An unset mark moves nothing, and a jump is no find for ; to repeat.
func TestAMarkNamesACellItsJumpsReturnTo(t *testing.T) {
	ctx, keys := lineWorld(t, "  one two\nthree\nfour", 0)
	for _, c := range []struct {
		keys  string
		edits []string
		at    [2]int
	}{
		{"fomajj'a", nil, [2]int{2, 0}},
		{"'b", nil, [2]int{2, 0}},
		{";", nil, [2]int{8, 0}},
		{"mbG`b", nil, [2]int{8, 0}},
		{"0d`b", []string{"0-7"}, [2]int{0, 0}},
		{"jjd'a", []string{"line"}, [2]int{0, 2}},
	} {
		got := keys(c.keys)
		if pos, _ := ctx.World.LocalCursor(); !slices.Equal(got, c.edits) || [2]int{pos.X, pos.Y} != c.at {
			t.Fatalf("%q: edits %q, cursor %d,%d; want %q at %v", c.keys, got, pos.X, pos.Y, c.edits, c.at)
		}
	}
}

// TestVisualsOperatorsTakeEveryGlyphItsBoundsCover: in Visual with the shield up d
// takes every glyph the ping bounds cover and stays in Visual, y fills the register
// with them, r and ~ retype each, * searches for the run they select and c leaves
// for Insert; with no shield the bounds are the cursor's cell.
func TestVisualsOperatorsTakeEveryGlyphItsBoundsCover(t *testing.T) {
	const field = "abcdefghi\nabcdefghi\nabcdefghi\nabcdefghi\nabcdefghi"
	ctx, keys := lineWorld(t, field, 4)
	player := ctx.World.Resources.Player.Entity
	ctx.World.Components.Ping.SetComponent(player, component.PingComponent{})
	ctx.World.Components.Shield.SetComponent(player, component.ShieldComponent{Active: true, Radius: 4})
	keys("jjv")
	b := ctx.World.GetPingAbsoluteBounds()
	if !b.Active || b.MaxX-b.MinX != 4 || b.MaxY-b.MinY != 2 {
		t.Fatalf("the bounds are %+v, want a box five by three about the cursor", b)
	}
	// boxed is the field with each glyph in the bounds made what to makes of it
	boxed := func(to func(rune) rune) string {
		rows := strings.Split(field, "\n")
		for y := b.MinY; y <= b.MaxY; y++ {
			row := []rune(rows[y])
			for x := b.MinX; x <= b.MaxX; x++ {
				row[x] = to(row[x])
			}
			rows[y] = string(row)
		}
		return strings.Join(rows, "\n")
	}
	for _, c := range []struct {
		keys, want string
		visual     bool
	}{
		{"d", boxed(func(rune) rune { return ' ' }), true},
		{"u", field, true},
		{"rz", boxed(func(rune) rune { return 'z' }), true},
		{"~", boxed(func(rune) rune { return 'Z' }), true},
		{"uuy", field, true},
		{"v7jp", field + strings.Repeat("\n", 5) + strings.Repeat("     cdefg\n", 2) + "     cdefg", false},
	} {
		keys(c.keys)
		if got := screen(ctx); got != c.want || ctx.IsVisualMode() != c.visual {
			t.Fatalf("%q left Visual %v and\n%s\nwant %v and\n%s", c.keys, ctx.IsVisualMode(), got, c.visual, c.want)
		}
	}
	keys("gg0jj4lv*")
	if pos, _ := ctx.World.LocalCursor(); pos.X != 2 || pos.Y != 3 {
		t.Fatalf("* in Visual left the cursor at %d,%d, want the next cdefg, at 2,3", pos.X, pos.Y)
	}
	if edits := keys("c"); !slices.Equal(edits, []string{"block"}) || !ctx.IsInsertMode() {
		t.Fatalf("c in Visual made %q and left Insert %v, want the bounds deleted and Insert", edits, ctx.IsInsertMode())
	}

	ctx, keys = lineWorld(t, "abc", 1)
	if keys("vd"); screen(ctx) != "a c" || !ctx.IsVisualMode() {
		t.Fatalf("d in Visual with no shield left %q, Visual %v; want the cursor's cell gone, in Visual", screen(ctx), ctx.IsVisualMode())
	}
}

// TestXTakesTheCellsLeftAndTildeFlipsCaseOnward: X deletes count cells left of the
// cursor and steps there; ~ flips the case of count glyphs from the cursor, a rune
// with no case as it is, and moves past them.
func TestXTakesTheCellsLeftAndTildeFlipsCaseOnward(t *testing.T) {
	ctx, keys := lineWorld(t, "ab-cd eF", 4)
	for _, c := range []struct {
		keys, want string
		x          int
	}{
		{"2X", "ab  d eF", 2},
		{"9X", "    d eF", 0},
		{"4l4~", "    D Ef", 8},
	} {
		keys(c.keys)
		if pos, _ := ctx.World.LocalCursor(); screen(ctx) != c.want || pos.X != c.x {
			t.Fatalf("%q left %q with the cursor at %d, want %q at %d", c.keys, screen(ctx), pos.X, c.want, c.x)
		}
	}
}

// TestDotTypesItsInsertTextOverWhatStandsThere: '.' makes its change again, types
// what Insert typed after it over the glyphs standing there now, and returns to
// Normal.
func TestDotTypesItsInsertTextOverWhatStandsThere(t *testing.T) {
	ctx, keys := lineWorld(t, "abab ab", 3)
	if keys("Iab\x1b"); screen(ctx) != "  ab ab" {
		t.Fatalf("I and its text left %q, want the first ab typed away", screen(ctx))
	}
	if keys("."); screen(ctx) != "     ab" || !ctx.IsNormalMode() {
		t.Fatalf("'.' left %q in Normal %v, want the next ab typed away, in Normal", screen(ctx), ctx.IsNormalMode())
	}
}
