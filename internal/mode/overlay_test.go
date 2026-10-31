package mode

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

func TestSelectedCardScrollDeltaTraversesClippedCard(t *testing.T) {
	cards := []engine.OverlayCardRef{
		{Key: "before", Y: 0, H: 4},
		{Key: "tall", Y: 10, H: 25},
		{Key: "after", Y: 36, H: 4},
	}
	tests := []struct {
		name      string
		selected  string
		offset    int
		viewportH int
		motion    input.MotionOp
		want      int
	}{
		{"down reveals next row", "tall", 10, 10, input.MotionDown, 1},
		{"down stops at card bottom", "tall", 25, 10, input.MotionDown, 0},
		{"up reveals previous row", "tall", 25, 10, input.MotionUp, -1},
		{"up stops at card top", "tall", 10, 10, input.MotionUp, 0},
		{"horizontal motion selects", "tall", 10, 10, input.MotionRight, 0},
		{"missing selection", "missing", 10, 10, input.MotionDown, 0},
		{"empty viewport", "tall", 10, 0, input.MotionDown, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectedCardScrollDelta(cards, tt.selected, tt.offset, tt.viewportH, tt.motion)
			if got != tt.want {
				t.Fatalf("delta = %d, want %d", got, tt.want)
			}
		})
	}
}

type menuSession struct {
	engine.SessionController
	bots  []engine.BotSeat
	calls []string
	err   error
}

func (s *menuSession) HostError() error                          { return nil }
func (s *menuSession) JoinError() error                          { return nil }
func (s *menuSession) LeaveError() error                         { return nil }
func (s *menuSession) SessionSummary() string                    { return "Test session" }
func (s *menuSession) Bots() []engine.BotSeat                    { return s.bots }
func (s *menuSession) Participants() []engine.SessionParticipant { return nil }
func (s *menuSession) Scenarios() ([]string, string)             { return []string{"main", "tower"}, "main" }
func (s *menuSession) Journaling() bool                          { return false }
func (s *menuSession) JournalError() error                       { return nil }
func (s *menuSession) ChangeScenario(name string) (bool, error) {
	if !slices.Contains([]string{"main", "tower"}, name) {
		return false, fmt.Errorf("scenario %q not found", name)
	}
	s.calls = append(s.calls, "scenario "+name)
	return true, nil
}
func (s *menuSession) BeginHosting(addr, authority string) error {
	if s.err != nil {
		return s.err
	}
	s.calls = append(s.calls, "host "+addr+" "+authority)
	return nil
}
func (s *menuSession) Join(target string) error {
	s.calls = append(s.calls, "join "+target)
	return nil
}
func (s *menuSession) RequestSession(site string, players int, scenario string) error {
	s.calls = append(s.calls, fmt.Sprintf("request %s %d %s", site, players, scenario))
	return nil
}
func (s *menuSession) AddBot(spec string) error {
	s.calls = append(s.calls, "add "+spec)
	return nil
}
func (s *menuSession) RemoveBot(id uint64) error {
	s.calls = append(s.calls, fmt.Sprintf("remove %d", id))
	return nil
}

// awaySession is a session holding the participants it is given.
type awaySession struct {
	menuSession
	participants []engine.SessionParticipant
}

func (s *awaySession) Participants() []engine.SessionParticipant { return s.participants }

// TestAnAwayPlayerIsListedAsAway: `:player` and the menu's drop rows tell a player
// whose cursor waits as a Wraith from one in play.
func TestAnAwayPlayerIsListedAsAway(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &awaySession{participants: []engine.SessionParticipant{{Slot: 1}, {Slot: 2, Away: true}}}
	ExecuteCommand(ctx, "player")
	if got, want := ctx.GetStatusMessage(), "Players: 1 player, 2 player (away)"; !strings.HasPrefix(got, want) {
		t.Fatalf(":player reads %q, want it to open %q", got, want)
	}
	var labels []string
	for _, o := range participantConfigOptions(ctx) {
		labels = append(labels, o.label)
	}
	if want := []string{"Drop 1: player", "Drop 2: player, away"}; !slices.Equal(labels, want) {
		t.Fatalf("the menu's drop rows read %q, want %q", labels, want)
	}
}

// TestAHeldPressOnTheStatusBarSummonsTheMenu: a left press on the status bar
// neither moves the cursor nor fires; held for MenuHoldDuration it opens the
// configuration menu, while a release or a drift past MenuHoldSlack first is nothing.
func TestAHeldPressOnTheStatusBarSummonsTheMenu(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	mouse := func(action terminal.MouseAction, x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: action, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
	}
	statusY := ctx.GameYOffset + ctx.World.Resources.Config.ViewportHeight
	pushed := ctx.World.Resources.Event.Queue.Pushed()

	mouse(terminal.MouseActionPress, 10, statusY)
	if !r.menuHold || r.mouseLeftHeld || ctx.World.Resources.Event.Queue.Pushed() != pushed {
		t.Fatalf("a press on the status bar: hold %v, fire held %v, events %d", r.menuHold, r.mouseLeftHeld, ctx.World.Resources.Event.Queue.Pushed()-pushed)
	}
	mouse(terminal.MouseActionRelease, 10, statusY)
	mouse(terminal.MouseActionPress, 10, statusY)
	mouse(terminal.MouseActionDrag, 11, statusY)
	if !r.menuHold {
		t.Fatal("a drift within the slack ended the hold")
	}
	mouse(terminal.MouseActionDrag, 13, statusY)
	if r.menuHold {
		t.Fatal("a drift past the slack kept the hold")
	}
	mouse(terminal.MouseActionRelease, 13, statusY)
	mouse(terminal.MouseActionPress, 10, statusY-1)
	if r.menuHold || !r.mouseLeftHeld {
		t.Fatal("a press on the ruler row above the status bar is a map press")
	}
	mouse(terminal.MouseActionRelease, 10, statusY-1)

	mouse(terminal.MouseActionPress, 10, statusY)
	r.ProcessInputTick()
	if ctx.IsOverlayMode() {
		t.Fatal("the menu opened before the hold lasted")
	}
	r.menuSince = r.menuSince.Add(-parameter.MenuHoldDuration)
	r.ProcessInputTick()
	if !ctx.IsOverlayMode() || configMenu(ctx) == nil || r.menuHold {
		t.Fatalf("after the hold: overlay %v, menu %v, hold %v", ctx.IsOverlayMode(), configMenu(ctx) != nil, r.menuHold)
	}
}

// TestAPressDrivesTheMenu: a left press selects the row under it and acts on the
// row already selected, as Enter, or as Left on a setting's left half; it focuses
// a form field, and outside the window goes back as Escape does.
func TestAPressDrivesTheMenu(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	g := ctx.OverlayGeometry()
	press := func(x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
	}
	left, right := g.X+g.ContentX, g.X+g.ContentX+g.ContentW-1
	row := func(i int) int { return g.Y + g.ContentY + i }
	r.handleConfigMenu()

	press(left, row(3))
	if m := configMenu(ctx); m.Page != "" || ctx.GetOverlaySelection() != "settings" {
		t.Fatalf("a press on an unselected row: page %q, selection %q", m.Page, ctx.GetOverlaySelection())
	}
	press(left, row(3))
	if m := configMenu(ctx); m.Page != "settings" || ctx.GetOverlaySelection() != "sfx" {
		t.Fatalf("a press on the selected page: page %q, selection %q", m.Page, ctx.GetOverlaySelection())
	}
	// Back, Sound's title and three rows, then Controls' title: auto-fire is row 6
	press(left, row(5))
	if got := ctx.GetOverlaySelection(); got != "sfx" {
		t.Fatalf("a press on a section title selected %q", got)
	}
	auto := ctx.AutoFire.Load()
	press(right, row(6))
	if got := ctx.AutoFire.Load(); got != auto || ctx.GetOverlaySelection() != "auto" {
		t.Fatalf("a press on an unselected setting: auto-fire %d to %d, selection %q", auto, got, ctx.GetOverlaySelection())
	}
	press(right, row(6))
	if got := ctx.AutoFire.Load(); got != (auto+1)%3 {
		t.Fatalf("the right half of a setting stepped auto-fire %d to %d", auto, got)
	}
	press(left, row(6))
	if got := ctx.AutoFire.Load(); got != auto {
		t.Fatalf("the left half of a setting stepped auto-fire back to %d, want %d", got, auto)
	}
	press(g.X-1, row(6))
	if m := configMenu(ctx); m.Page != "" || ctx.GetOverlaySelection() != "settings" {
		t.Fatalf("a press outside the window: page %q, selection %q", m.Page, ctx.GetOverlaySelection())
	}

	showConfigMenu(ctx, "multiplayer")
	ctx.SetOverlaySelection("host")
	r.handleOverlayActivate()
	press(left, row(1))
	if r.configForm == nil || r.configForm.state.Focus != 1 {
		t.Fatal("a press on a form field did not focus it")
	}
	press(left, g.Y+g.H)
	if r.configForm != nil || ctx.GetOverlaySelection() != "host" {
		t.Fatal("a press outside the window did not cancel the form")
	}
	press(left, g.Y-1)
	press(left, g.Y-1)
	if ctx.IsOverlayActive() {
		t.Fatal("presses outside the window did not close the menu")
	}
}

// TestThePointerScrollsAnOverlay: a wheel step scrolls an overlay's view by
// OverlayWheelRows; a press on the scrollbar's track jumps the thumb there and a
// drag carries it until the release; a menu's selection stays as its view moves,
// and a press on a row picks the row the view shows there.
func TestThePointerScrollsAnOverlay(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	g := ctx.OverlayGeometry()
	mouse := func(btn terminal.MouseButton, action terminal.MouseAction, x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: btn, MouseAction: action, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
	}
	bar, top, foot := g.X+g.ScrollX, g.Y+g.ContentY, g.Y+g.ContentY+g.ContentH-1

	r.handleHelp()
	ctx.SetOverlayContent(&core.OverlayContent{Title: core.OverlayHelpTitle, Layout: core.OverlayLayoutDoc})
	ctx.SetOverlayContentH(10 * g.ContentH) // as the renderer lays it out
	mouse(terminal.MouseBtnWheelDown, terminal.MouseActionPress, 0, 0)
	if got := ctx.GetOverlayScroll(); got != parameter.OverlayWheelRows {
		t.Fatalf("a wheel step scrolled help to %d, want %d", got, parameter.OverlayWheelRows)
	}
	mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, bar, foot)
	if got, want := ctx.GetOverlayScroll(), 9*g.ContentH; got != want {
		t.Fatalf("a press at the track's foot scrolled help to %d, want its end %d", got, want)
	}
	mouse(terminal.MouseBtnLeft, terminal.MouseActionDrag, bar, top)
	if got := ctx.GetOverlayScroll(); got != 0 {
		t.Fatalf("the thumb dragged to the track's top scrolled help to %d", got)
	}
	mouse(terminal.MouseBtnLeft, terminal.MouseActionRelease, bar, top)
	mouse(terminal.MouseBtnLeft, terminal.MouseActionDrag, bar, foot)
	if got := ctx.GetOverlayScroll(); got != 0 {
		t.Fatalf("a drag after the release scrolled help to %d", got)
	}

	r.handleConfigMenu()
	showConfigMenu(ctx, "developer")
	m := configMenu(ctx)
	if len(m.Rows) <= g.ListH()+parameter.OverlayWheelRows {
		t.Fatalf("the page's %d rows fit a list of %d", len(m.Rows), g.ListH())
	}
	selected := ctx.GetOverlaySelection()
	mouse(terminal.MouseBtnWheelDown, terminal.MouseActionPress, 0, 0)
	if ctx.GetOverlayScroll() != parameter.OverlayWheelRows || ctx.GetOverlaySelection() != selected {
		t.Fatalf("a wheel step in the menu: view at %d, selection %q", ctx.GetOverlayScroll(), ctx.GetOverlaySelection())
	}
	mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, g.X+g.ContentX, top)
	if got, want := ctx.GetOverlaySelection(), m.Rows[parameter.OverlayWheelRows].Key; got != want {
		t.Fatalf("a press on the view's first row selected %q, want %q", got, want)
	}
	mouse(terminal.MouseBtnLeft, terminal.MouseActionRelease, g.X+g.ContentX, top)
	mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, bar, top+g.ListH()-1)
	if got, want := ctx.GetOverlayScroll(), len(m.Rows)-g.ListH(); got != want {
		t.Fatalf("a press at the foot of the menu's track scrolled it to %d, want %d", got, want)
	}
}

// TestAPressPicksACardAndOutsideActsAsEscape: in telemetry a press selects the card
// under it and pins the card already selected, as Enter does; a press in a row
// below the cards in view picks nothing, and one outside the window is Escape.
func TestAPressPicksACardAndOutsideActsAsEscape(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	g := ctx.OverlayGeometry()
	press := func(x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
	}
	r.enterOverlay()
	ExecuteCommand(ctx, "t")
	ctx.SetOverlayContent(&core.OverlayContent{Title: "TELEMETRY", Items: []core.OverlayItem{
		core.OverlayCard{Key: "a", Title: "A"}, core.OverlayCard{Key: "b", Title: "B"}, core.OverlayCard{Key: "c", Title: "C"},
	}})
	// As the renderer lays them out: c starts below the rows in view
	ctx.SetOverlayCards([]engine.OverlayCardRef{{Key: "a", W: 20, H: 4}, {Key: "b", X: 22, W: 20, H: 4}, {Key: "c", Y: g.ContentH, W: 20, H: 4}})
	left, row := g.X+g.ContentX, g.Y+g.ContentY

	press(left+25, row+1)
	if got := ctx.GetOverlaySelection(); got != "b" || len(ctx.OverlayPins()) != 0 {
		t.Fatalf("a press on a card: selection %q, pins %q", got, ctx.OverlayPins())
	}
	press(left+25, row+3)
	if got := ctx.OverlayPins(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("a press on the selected card pinned %q", got)
	}
	press(left+1, g.Y+g.H-2)
	if got := ctx.GetOverlaySelection(); got != "b" || !ctx.IsOverlayMode() {
		t.Fatalf("a press on the hint row: selection %q, overlay %v", got, ctx.IsOverlayMode())
	}
	press(g.X+g.W, row)
	if ctx.IsOverlayMode() {
		t.Fatal("a press outside the window did not close telemetry")
	}
}

func TestConfigFormsPreserveEditsOnFailureAndCancelWithoutSubmitting(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	session := &menuSession{err: errors.New("address in use")}
	ctx.SessionCtl = session
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := func(k terminal.Key, ch rune) {
		t.Helper()
		intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch})
		if intent == nil || !r.Handle(intent) {
			t.Fatalf("unhandled key %v %q", k, ch)
		}
	}
	key(terminal.KeyCtrlG, 0)
	ctx.SetOverlaySelection("multiplayer")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("host")
	key(terminal.KeyRight, 0)
	if r.configForm != nil {
		t.Fatal("arrow key opened an action")
	}
	key(terminal.KeyEnter, 0)
	key(terminal.KeyHome, 0)
	key(terminal.KeyDelete, 0)
	key(terminal.KeyRune, ':')
	key(terminal.KeyEnd, 0)
	key(terminal.KeyBackspace, 0)
	key(terminal.KeyRune, '3')
	key(terminal.KeyDown, 0)
	for _, ch := range "host" {
		key(terminal.KeyRune, ch)
	}
	key(terminal.KeyEnter, 0)
	view := configMenu(ctx).Form
	if view.Error != "address in use" || view.Fields[0].Value != ":4243" || view.Fields[1].Value != "host" {
		t.Fatalf("failed submission lost edits: %+v", view)
	}
	r.RefreshConfigMenu()
	if configMenu(ctx).Form != view {
		t.Fatal("refresh replaced a form in progress")
	}
	key(terminal.KeyEscape, 0)
	if r.configForm != nil || configMenu(ctx).Form != nil || ctx.GetOverlaySelection() != "host" || len(session.calls) != 0 {
		t.Fatal("cancel submitted or lost the parent selection")
	}
	key(terminal.KeyEnter, 0)
	key(terminal.KeyCtrlG, 0)
	if ctx.GetMode() != core.ModeNormal || ctx.TimeCtl.IsPaused() || r.configForm != nil {
		t.Fatal("closing a form failed to resume")
	}
}

func TestConfigSessionActionsRevalidateAndForwardFormValues(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	session := &menuSession{bots: []engine.BotSeat{{ID: 10, Slot: 2, Graph: "default"}}}
	ctx.SessionCtl = session
	r := NewRouter(ctx, input.NewMachine())
	for _, tt := range []struct {
		page, key string
		values    []string
		want      string
	}{
		{"multiplayer", "add", []string{"2", "default"}, "add 2:default"},
		{"multiplayer", "host", []string{"127.0.0.1:0", "host"}, "host 127.0.0.1:0 host"},
		{"multiplayer", "join", []string{"vif://example.test:4242/run"}, "join vif://example.test:4242/run"},
		{"multiplayer", "request", []string{"https://example.test", "3", "tower"}, "request https://example.test 3 tower"},
	} {
		r.handleConfigMenu()
		showConfigMenu(ctx, tt.page)
		ctx.SetOverlaySelection(tt.key)
		r.handleOverlayActivate()
		for i, v := range tt.values {
			r.configForm.state.SetValue(i, v)
		}
		ctx.Viewer.Store(true)
		r.Handle(&input.Intent{Type: input.IntentTextConfirm})
		if r.configForm == nil || r.configForm.err == "" || slices.Contains(session.calls, tt.want) {
			t.Fatal("stale form bypassed replay guard")
		}
		ctx.Viewer.Store(false)
		r.Handle(&input.Intent{Type: input.IntentTextConfirm})
		if r.configForm != nil || ctx.GetMode() != core.ModeNormal || session.calls[len(session.calls)-1] != tt.want {
			t.Fatalf("%s did not submit: %v", tt.key, session.calls)
		}
	}
	r.handleConfigMenu()
	showConfigMenu(ctx, "multiplayer")
	ctx.SetOverlaySelection("drop/10")
	r.moveConfigMenu(input.MotionRight, 0)
	if len(session.calls) != 4 {
		t.Fatal("arrow key removed a bot")
	}
	session.bots[0].ID = 11
	r.handleOverlayActivate()
	if len(session.calls) != 4 {
		t.Fatal("stale selection removed a replacement on the same slot")
	}
	r.RefreshConfigMenu()
	ctx.SetOverlaySelection("drop/11")
	r.handleOverlayActivate()
	if session.calls[len(session.calls)-1] != "remove 11" || ctx.IsOverlayActive() {
		t.Fatal("bot removal failed to submit and resume")
	}
}

// menuRow is the menu's row keyed key, a zero row when it shows none
func menuRow(ctx *engine.GameContext, key string) core.OverlayMenuRow {
	m := configMenu(ctx)
	if i := slices.IndexFunc(m.Rows, func(row core.OverlayMenuRow) bool { return row.Key == key }); i >= 0 {
		return m.Rows[i]
	}
	return core.OverlayMenuRow{}
}

type menuPeers struct{ engine.NetworkPort }

func (menuPeers) PeerCount() int  { return 1 }
func (menuPeers) IsRunning() bool { return true }

func TestConfigMenuHonorsCommandRestrictionsAtActivation(t *testing.T) {
	for _, viewer := range []bool{false, true} {
		w := engine.NewWorld()
		ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
		if viewer {
			ctx.Viewer.Store(true)
		} else {
			w.Resources.Network = &engine.NetworkResource{Port: menuPeers{}}
		}
		r := NewRouter(ctx, input.NewMachine())
		ExecuteCommand(ctx, "g")
		ctx.SetOverlaySelection("game")
		r.handleOverlayActivate()
		ctx.SetOverlaySelection("speed")
		w.Resources.Event.Queue.Consume()
		if menuRow(ctx, "speed").Disabled == "" {
			t.Fatalf("viewer=%v: speed appears editable", viewer)
		}
		r.handleOverlayActivate()
		if len(w.Resources.Event.Queue.Consume()) != 0 {
			t.Fatal("disabled menu entry emitted an event")
		}
		ctx.Viewer.Store(false)
		w.Resources.Network = nil
		r.RefreshConfigMenu()
		if menuRow(ctx, "speed").Disabled != "" || ctx.GetOverlaySelection() != "speed" {
			t.Fatal("refresh failed to unlock speed or lost selection")
		}
	}
}

// TestMaxFPSStepsInTheMenuAndTakesRatesInRange: the Display row steps through
// parameter.FrameRateSteps around to auto, a value between steps moves to the
// step on that side, and :fps takes auto or a rate in range and refuses others.
func TestMaxFPSStepsInTheMenuAndTakesRatesInRange(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	r := NewRouter(ctx, input.NewMachine())
	ExecuteCommand(ctx, "g")
	ctx.SetOverlaySelection("settings")
	r.handleOverlayActivate()
	step := func(d int) int32 {
		ctx.SetOverlaySelection("fps")
		r.changeConfigMenu(d, false)
		return ctx.MaxFPS.Load()
	}
	steps := parameter.FrameRateSteps
	for i := range steps {
		want := steps[(i+1)%len(steps)]
		if got := step(1); got != int32(want) {
			t.Fatalf("step %d up: max fps %d, want %d", i, got, want)
		}
	}
	if got := step(-1); got != int32(steps[len(steps)-1]) {
		t.Fatalf("down from auto: max fps %d, want %d", got, steps[len(steps)-1])
	}
	for _, c := range []struct{ from, d, want int32 }{{45, 1, 60}, {45, -1, 30}} {
		ctx.MaxFPS.Store(c.from)
		if got := step(int(c.d)); got != c.want {
			t.Errorf("%d stepped %d: max fps %d, want %d", c.from, c.d, got, c.want)
		}
	}
	ctx.MaxFPS.Store(0)
	for _, c := range []struct {
		command string
		want    int32
	}{
		{"fps 24", 24}, {"fps", 24}, {"fps 9", 24}, {"fps 121", 24}, {"fps x", 24},
		{"fps 10", 10}, {"fps AUTO", 0}, {"fps 120", 120}, {"fps 0", 0},
	} {
		ExecuteCommand(ctx, c.command)
		if got := ctx.MaxFPS.Load(); got != c.want {
			t.Errorf("%q: max fps %d, want %d", c.command, got, c.want)
		}
	}
}

// TestTheHelpKeyOpensHelpOverAnyModeAndClosesIt: F1 asks for help from NORMAL and
// from a text mode, takes the place of the menu and drops its form, and closes help
// where help is shown; F10 opens the menu over help as Ctrl-G does.
func TestTheHelpKeyOpensHelpOverAnyModeAndClosesIt(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := func(k terminal.Key, ch rune) {
		t.Helper()
		intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch})
		if intent == nil || !r.Handle(intent) {
			t.Fatalf("unhandled key %v %q", k, ch)
		}
	}
	asked := func() bool {
		return slices.ContainsFunc(w.Resources.Event.Queue.Consume(), func(ev event.GameEvent) bool {
			return ev.Type == event.EventMetaHelpRequest
		})
	}
	// What MetaSystem shows for the ask
	shown := func() {
		ctx.SetOverlayContent(&core.OverlayContent{Title: core.OverlayHelpTitle, Layout: core.OverlayLayoutDoc})
	}

	key(terminal.KeyF1, 0)
	if !ctx.IsOverlayMode() || !asked() {
		t.Fatal("F1 in NORMAL did not ask for help")
	}
	shown()
	key(terminal.KeyF1, 0)
	if !ctx.IsNormalMode() || ctx.IsOverlayActive() {
		t.Fatal("F1 did not close the help shown")
	}
	key(terminal.KeyRune, 'i')
	key(terminal.KeyF1, 0)
	if !ctx.IsOverlayMode() || !asked() {
		t.Fatal("F1 in INSERT did not ask for help")
	}
	shown()
	key(terminal.KeyF10, 0)
	if configMenu(ctx) == nil {
		t.Fatal("F10 did not open the menu over help")
	}
	ctx.SetOverlaySelection("multiplayer")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("host")
	key(terminal.KeyEnter, 0)
	if r.configForm == nil {
		t.Fatal("Enter did not open the host form")
	}
	key(terminal.KeyF1, 0)
	if r.configForm != nil || configMenu(ctx) != nil || !ctx.IsOverlayMode() || !asked() {
		t.Fatal("F1 did not take the place of the menu and its form")
	}
}

func TestConfigMenuKeyCanBeRemapped(t *testing.T) {
	custom, err := input.LoadKeyConfig([]byte("[normal_keys]\nctrl_g = \"none\"\nf2 = \"config_menu\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	machine := input.NewMachine()
	machine.SetKeyTable(input.MergeKeyTable(input.DefaultKeyTable(), custom))
	if got := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyCtrlG}); got != nil && got.Type == input.IntentConfigMenu {
		t.Fatal("unbound Ctrl-G still opens the menu")
	}
	if got := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyF2}); got == nil || got.Type != input.IntentConfigMenu {
		t.Fatalf("remapped menu key = %+v", got)
	}
}

// menuKeys drives a router by keys, as a terminal sends them
func menuKeys(t *testing.T, machine *input.Machine, r *Router) func(terminal.Key, rune) {
	return func(k terminal.Key, ch rune) {
		t.Helper()
		if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); intent != nil {
			r.Handle(intent)
		}
	}
}

// TestTheMenuSearchFindsRowsOnEveryPageAndActsInPlace: `/` lists the rows of every
// page matching all the words typed, a section's title included, each naming its
// page; Down selects while typing, Enter keeps the search and a setting changes in
// place; Escape clears the search, then closes the menu.
func TestTheMenuSearchFindsRowsOnEveryPageAndActsInPlace(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := menuKeys(t, machine, r)
	key(terminal.KeyCtrlG, 0)
	key(terminal.KeyRune, '/')
	for _, ch := range "pause" {
		key(terminal.KeyRune, ch)
	}
	var keys []string
	for _, row := range configMenu(ctx).Rows {
		keys = append(keys, row.Key)
	}
	if want := []string{"game:pause", "settings:pause_menu", "settings:pause_command"}; !slices.Equal(keys, want) {
		t.Fatalf("a search for pause listed %q, want %q", keys, want)
	}
	if got := menuRow(ctx, "settings:pause_menu").Label; got != "Settings / Pause in menu" {
		t.Fatalf("a found row is labelled %q", got)
	}
	key(terminal.KeyDown, 0)
	key(terminal.KeyEnter, 0)
	key(terminal.KeyRune, 'l')
	if ctx.PauseMenu.Load() || ctx.TimeCtl.IsPaused() || ctx.OverlayFilter() != "pause" {
		t.Fatalf("l on a found setting: pause in menu %v, paused %v, search %q", ctx.PauseMenu.Load(), ctx.TimeCtl.IsPaused(), ctx.OverlayFilter())
	}
	key(terminal.KeyEscape, 0)
	if m := configMenu(ctx); m == nil || m.Page != "" || ctx.OverlayFilter() != "" || m.Rows[0].Key != "game" {
		t.Fatal("Escape did not clear the search back to the pages")
	}
	key(terminal.KeyEscape, 0)
	if ctx.IsOverlayActive() {
		t.Fatal("Escape after the search did not close the menu")
	}
}

// TestAOneFieldFormIsATextBoxOnItsRow: Other scenario is typed in on its row; a name
// that does not resolve keeps the text with the error beside it, Up leaves the box
// for the row above, and a name that does closes the menu.
func TestAOneFieldFormIsATextBoxOnItsRow(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	session := &menuSession{}
	ctx.SessionCtl = session
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := menuKeys(t, machine, r)
	key(terminal.KeyCtrlG, 0)
	ctx.SetOverlaySelection("scenarios")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("scenario")
	key(terminal.KeyEnter, 0)
	for _, ch := range "nope" {
		key(terminal.KeyRune, ch)
	}
	key(terminal.KeyEnter, 0)
	row := menuRow(ctx, "scenario")
	if configMenu(ctx).Form != nil || !row.Typing || row.Value != "nope" || !strings.HasPrefix(row.Description, "Error: ") {
		t.Fatalf("an unknown scenario in the box: %+v", row)
	}
	key(terminal.KeyUp, 0)
	if r.configForm != nil || menuRow(ctx, "scenario").Typing || ctx.GetOverlaySelection() != "start" {
		t.Fatalf("Up left the box to %q, typing %v", ctx.GetOverlaySelection(), menuRow(ctx, "scenario").Typing)
	}
	key(terminal.KeyDown, 0)
	key(terminal.KeyEnter, 0)
	for _, ch := range "tower" {
		key(terminal.KeyRune, ch)
	}
	key(terminal.KeyEnter, 0)
	if ctx.IsOverlayActive() || !slices.Equal(session.calls, []string{"scenario tower"}) {
		t.Fatalf("a known scenario in the box: overlay %v, calls %q", ctx.IsOverlayActive(), session.calls)
	}
}

// TestThePauseFlagsGateTheMenuAndCommandLinePauses: Pause in menu and Pause in
// command line decide whether opening each asks to pause a solo game, the menu's
// change asking at once; p asks for the pause only another ends, and a game waiting
// for its first press refuses :pause and greys the Pause row.
func TestThePauseFlagsGateTheMenuAndCommandLinePauses(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := menuKeys(t, machine, r)
	// asked is what the pause or hold requests of kind asked for since the last call
	asked := func(kind event.EventType) (out []bool) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.GamePausePayload); ok && ev.Type == kind {
				out = append(out, p.Paused)
			}
		}
		return out
	}
	ExecuteCommand(ctx, "pause menu off")
	key(terminal.KeyCtrlG, 0)
	if got := asked(event.EventGamePauseRequest); slices.Contains(got, true) {
		t.Fatalf("with pause in menu off the menu asked for pauses %v", got)
	}
	ExecuteCommand(ctx, "pause menu on")
	if got := asked(event.EventGamePauseRequest); !slices.Equal(got, []bool{true}) {
		t.Fatalf("turning pause in menu on with the menu open asked for pauses %v", got)
	}
	key(terminal.KeyCtrlG, 0)
	ExecuteCommand(ctx, "pause command off")
	asked(event.EventGamePauseRequest)
	key(terminal.KeyRune, ':')
	if got := asked(event.EventGamePauseRequest); !ctx.IsCommandMode() || slices.Contains(got, true) {
		t.Fatalf("with pause in command line off: command mode %v, pauses asked %v", ctx.IsCommandMode(), got)
	}
	key(terminal.KeyEscape, 0)
	asked(event.EventGameHoldRequest)

	key(terminal.KeyRune, 'p')
	if got := asked(event.EventGameHoldRequest); !slices.Equal(got, []bool{true}) {
		t.Fatalf("p asked for holds %v, want a pause", got)
	}
	ctx.TimeCtl.HoldAs(engine.HoldStart)
	ExecuteCommand(ctx, "pause")
	if got := asked(event.EventGameHoldRequest); len(got) != 0 {
		t.Fatalf(":pause while the game waits to start asked for holds %v", got)
	}
	ExecuteCommand(ctx, "g")
	ctx.SetOverlaySelection("game")
	r.handleOverlayActivate()
	if row := menuRow(ctx, "pause"); row.Label != "Pause" || !strings.Contains(row.Disabled, "first press") {
		t.Fatalf("the Pause row while the game waits to start: %+v", row)
	}
}
