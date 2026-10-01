package mode

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
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
func (s *menuSession) SessionSummary() string                    { return "Test session" }
func (s *menuSession) Bots() []engine.BotSeat                    { return s.bots }
func (s *menuSession) Participants() []engine.SessionParticipant { return nil }
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
