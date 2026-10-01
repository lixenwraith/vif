package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
)

func TestConfigMenuChangesStayAppliedAfterClose(t *testing.T) {
	a, err := NewHeadless(scriptConfig(fixtureSeed))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	key := func(k terminal.Key, ch rune) {
		t.Helper()
		intent := a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch})
		if intent == nil || !a.Inject(intent) {
			t.Fatalf("key %v %q was not routed", k, ch)
		}
	}
	key(terminal.KeyCtrlG, 0)
	ctx := a.Context()
	if ctx.GetMode() != core.ModeOverlay || !ctx.TimeCtl.IsPaused() {
		t.Fatal("menu did not pause solo play in overlay mode")
	}
	ctx.SetOverlaySelection("controls")
	key(terminal.KeyEnter, 0)
	key(terminal.KeyEnter, 0)
	if ctx.AutoFire.Load() != engine.AutoFireOff || !ctx.TimeCtl.IsPaused() {
		t.Fatal("auto-fire change did not apply while paused")
	}
	key(terminal.KeyEscape, 0)
	if ctx.GetOverlaySelection() != "controls" {
		t.Fatal("back lost the category selection")
	}
	key(terminal.KeyEscape, 0)
	if ctx.GetMode() != core.ModeNormal || ctx.IsOverlayActive() || ctx.TimeCtl.IsPaused() || ctx.AutoFire.Load() != engine.AutoFireOff {
		t.Fatal("closing the menu failed to resume with changed settings")
	}
	key(terminal.KeyRune, ':')
	key(terminal.KeyRune, 'g')
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("simulation")
	key(terminal.KeyEnter, 0)
	key(terminal.KeyRight, 0)
	a.World().RunSafe(a.router.RefreshConfigMenu)
	m := ctx.GetOverlayContent().Menu
	if m.Rows[1].Value != "2x" || !ctx.TimeCtl.IsPaused() {
		t.Fatalf("queued speed change was not reflected: %+v", m)
	}
	key(terminal.KeyEscape, 0)
	ctx.SetOverlaySelection("diagnostics")
	ctx.World.Resources.Status.SetSnapshotInterval(77)
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("stat")
	key(terminal.KeyRight, 0)
	if got := ctx.World.Resources.Status.SnapshotInterval(); got != 200 {
		t.Fatalf("numeric preset should increase a custom value, got %d", got)
	}
	key(terminal.KeyCtrlG, 0)
	if ctx.GetMode() != core.ModeNormal || ctx.TimeCtl.IsPaused() {
		t.Fatal("Ctrl-G did not close from a settings page")
	}
}

func TestNetworkSessionConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "host", cfg: Config{HostAddress: ":7777"}},
		{name: "join", cfg: Config{JoinAddress: "127.0.0.1:7777"}},
		{name: "later host cap", cfg: Config{Participants: 4}},
		{
			name: "guest cannot set host cap",
			cfg:  Config{JoinAddress: "127.0.0.1:7777", Participants: 4},
			want: "not a joining guest",
		},
		{
			name: "headless without script runner",
			cfg:  Config{Mode: ModeHeadless, HostAddress: ":7777", Width: 80, Height: 24},
			want: "RunScript",
		},
		{
			name: "headless script host",
			cfg: Config{
				Mode: ModeHeadless, HostAddress: ":7777", Width: 80, Height: 24,
				scriptedSession: true,
			},
		},
		{
			name: "exclusive",
			cfg:  Config{HostAddress: ":7777", JoinAddress: "127.0.0.1:7777"},
			want: "mutually exclusive",
		},
		{
			name: "replay cannot network",
			cfg:  Config{Mode: ModeReplay, HostAddress: ":7777", Width: 80, Height: 24},
			want: "playback cannot join",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHeadlessSessionRequiresTheScriptGate(t *testing.T) {
	t.Parallel()
	_, err := NewHeadless(Config{HostAddress: ":7777"})
	if err == nil || !strings.Contains(err.Error(), "RunScript") {
		t.Fatalf("NewHeadless() error = %v, want RunScript gate", err)
	}
}

func TestInstalledKeymapMigratesRetiredAppendBinding(t *testing.T) {
	t.Parallel()
	cfg := scriptConfig(fixtureSeed)
	cfg.Resources.Dir = t.TempDir()
	path := filepath.Join(cfg.Resources.Dir, paths.InputDirName, paths.KeymapConfigFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[normal]\na = \"append\"\nh = \"motion_right\"\nv = \"none\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("installed keymap: %v", err)
	}
	defer a.Close()
	tickUntilCursor(t, a)
	before, _ := a.World().LocalCursor()
	a.Inject(a.inputMachine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'a'}))
	after, _ := a.World().LocalCursor()
	if a.Context().AutoFire.Load() != engine.AutoFireOff || a.Context().IsInsertMode() || after != before {
		t.Fatal("retired append binding did not cycle auto-fire without moving or entering Insert")
	}
	kt := a.Context().KeyTable
	if kt.NormalRunes['h'].Motion != input.MotionRight || kt.NormalRunes['l'].Motion != input.MotionRight {
		t.Fatal("custom binding or default fallback was lost")
	}
	if _, bound := kt.NormalRunes['v']; bound {
		t.Fatal("explicit unbinding was lost")
	}
	if err := os.WriteFile(path, []byte("[normal]\na = \"unknown_action\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.loadKeymap(); err == nil || !strings.Contains(err.Error(), "unknown_action") {
		t.Fatalf("unknown action was not rejected: %v", err)
	}
}
