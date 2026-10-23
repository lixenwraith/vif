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
func (s *menuSession) Scenarios() ([]string, string)             { return nil, "" }
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

	press(left, row(4))
	if m := configMenu(ctx); m.Page != "" || ctx.GetOverlaySelection() != "controls" {
		t.Fatalf("a press on an unselected row: page %q, selection %q", m.Page, ctx.GetOverlaySelection())
	}
	press(left, row(4))
	if m := configMenu(ctx); m.Page != "controls" || ctx.GetOverlaySelection() != "auto" {
		t.Fatalf("a press on the selected category: page %q, selection %q", m.Page, ctx.GetOverlaySelection())
	}
	auto := ctx.AutoFire.Load()
	press(right, row(1))
	if got := ctx.AutoFire.Load(); got != (auto+1)%3 {
		t.Fatalf("the right half of a setting stepped auto-fire %d to %d", auto, got)
	}
	press(left, row(1))
	if got := ctx.AutoFire.Load(); got != auto {
		t.Fatalf("the left half of a setting stepped auto-fire back to %d, want %d", got, auto)
	}
	press(g.X-1, row(1))
	if m := configMenu(ctx); m.Page != "" || ctx.GetOverlaySelection() != "controls" {
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
		{"bots", "add", []string{"2", "default"}, "add 2:default"},
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
	showConfigMenu(ctx, "bots")
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
		ctx.SetOverlaySelection("simulation")
		r.handleOverlayActivate()
		w.Resources.Event.Queue.Consume()
		if m := configMenu(ctx); m.Rows[1].Disabled == "" {
			t.Fatalf("viewer=%v: speed appears editable", viewer)
		}
		r.handleOverlayActivate()
		if len(w.Resources.Event.Queue.Consume()) != 0 {
			t.Fatal("disabled menu entry emitted an event")
		}
		ctx.Viewer.Store(false)
		w.Resources.Network = nil
		r.RefreshConfigMenu()
		if configMenu(ctx).Rows[1].Disabled != "" || ctx.GetOverlaySelection() != "speed" {
			t.Fatal("refresh failed to unlock speed or lost selection")
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
