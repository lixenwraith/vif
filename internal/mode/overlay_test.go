package mode

import (
	"testing"

	"github.com/lixenwraith/terminal"
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
