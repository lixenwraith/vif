package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
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

// TestASoloPauseStandsUntilTheNextPause: the Game page's Pause row holds a solo run
// at once, and a tick asked of it takes none. Closing the menu or the command line
// ends only the pause each made; :pause runs the game on, and so does a step.
func TestASoloPauseStandsUntilTheNextPause(t *testing.T) {
	t.Parallel()
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
	command := func(text string) {
		t.Helper()
		key(terminal.KeyRune, ':')
		for _, ch := range text {
			key(terminal.KeyRune, ch)
		}
		key(terminal.KeyEnter, 0)
	}
	ctx := a.Context()
	a.Tick(1)
	key(terminal.KeyCtrlG, 0)
	ctx.SetOverlaySelection("game")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("pause")
	key(terminal.KeyEnter, 0)
	key(terminal.KeyCtrlG, 0)
	at := a.Position().Tick
	a.Tick(3)
	if !ctx.TimeCtl.Held() || ctx.GetMode() != core.ModeNormal || a.Position().Tick != at {
		t.Fatalf("after the Pause row and the menu closing: held %v, mode %v, tick %d from %d",
			ctx.TimeCtl.Held(), ctx.GetMode(), a.Position().Tick, at)
	}
	key(terminal.KeyRune, ':')
	key(terminal.KeyEscape, 0)
	if !ctx.TimeCtl.Held() {
		t.Fatal("closing the command line ended the hold")
	}
	command("pause")
	a.Tick(1)
	if ctx.TimeCtl.IsPaused() || a.Position().Tick != at+1 {
		t.Fatalf(":pause again left paused %v at tick %d, want running at %d", ctx.TimeCtl.IsPaused(), a.Position().Tick, at+1)
	}
	command("pause")
	command("step")
	if ctx.TimeCtl.Held() {
		t.Fatal("a step left the hold standing")
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

// TestARestartKeepsTheCommandLineAndTakesWhatItNames: the next run is the command
// line Run was given with what the restart named, so a pinned seed survives. A :join
// leaves the -players and -authority of a session it no longer hosts, and falls back
// to that run on its own, in no session; a run following nobody starts over on its
// own, keeping the ceiling its session was opened with.
func TestARestartKeepsTheCommandLineAndTakesWhatItNames(t *testing.T) {
	t.Parallel()
	line := Config{Seed: 7, HostAddress: "127.0.0.1:7000", JoinAddress: "10.0.0.1:7777", SessionName: "old",
		Participants: 4, FixedAuthority: true, sessionPlayers: 3,
		Resources: resource.Options{Scenario: "main", Embedded: true}}
	dialled := &joinDial{}
	next, solo := line.restarted(&restartRequest{Notice: "n", Scenario: "duel", Host: "127.0.0.1:7001",
		Join: "10.0.0.2:7777/abc", dialled: dialled, Bots: []string{"roam"}, Journal: true})
	if next.Seed != 7 || next.Resources.Scenario != "duel" || next.Resources.Embedded || next.Notice != "n" ||
		!slices.Equal(next.Bots, []string{"roam"}) || !next.Journal || !next.journalCarried {
		t.Fatalf("next run %+v; want the command line with what the restart named", next)
	}
	if next.HostAddress != "" || next.resumeHost != "127.0.0.1:7001" {
		t.Fatalf("hosting opens %q before the clock and %q after it; want only after", next.HostAddress, next.resumeHost)
	}
	if next.JoinAddress != "10.0.0.2:7777" || next.SessionName != "abc" || next.dialled != dialled {
		t.Fatalf("a :join builds %q/%q on %p; want the target on its own dial", next.JoinAddress, next.SessionName, next.dialled)
	}
	if next.Participants != 0 || next.FixedAuthority {
		t.Fatalf("a :join keeps -players %d and a pinned authority %t; want neither, its session's coming with its offer",
			next.Participants, next.FixedAuthority)
	}
	if solo == nil || solo.Seed != 7 || solo.JoinAddress != "" || solo.SessionName != "" || solo.sessionPlayers != 0 || solo.dialled != nil {
		t.Fatalf("a :join falls back to %+v; want the same run on its own", solo)
	}
	if moved, _ := line.restarted(&restartRequest{Rejoin: true, Join: "10.0.0.3:7777"}); moved.JoinAddress != "10.0.0.3:7777" || moved.SessionName != "old" {
		t.Fatalf("a rejoin dials %q/%q; want the session's moved door", moved.JoinAddress, moved.SessionName)
	}
	if alone, solo := line.restarted(&restartRequest{}); alone.JoinAddress != "" || solo != nil || alone.sessionPlayers != 3 {
		t.Fatalf("a run following nobody dials %q under a ceiling of %d; want nothing dialled, the session's 3 kept",
			alone.JoinAddress, alone.sessionPlayers)
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

// TestPlaySettingsStartTheRunAndAreWrittenBackAsLeft: [play] sets auto-fire, the
// mouse and the HUD before the first tick; an unchanged run writes no file, each
// value changed is written to the settings file and nothing else, and a purge
// returns to the run's start.
func TestPlaySettingsStartTheRunAndAreWrittenBackAsLeft(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	dir := t.TempDir()
	file := filepath.Join(dir, paths.SettingsFile)
	a, err := NewHeadless(Config{Seed: 1, Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Play: paths.Play{AutoFire: "off", HUD: true, SfxVolume: 0.5}, Settings: paths.FileStore{Path: file}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	ctx := a.ctx
	if ctx.AutoFire.Load() != engine.AutoFireOff || !ctx.MouseDisabled.Load() || ctx.MouseFreeMode.Load() || !ctx.OverlayHUD.Load() {
		t.Fatalf("the run did not start as [play] says: auto %d mouse off %v free %v hud %v",
			ctx.AutoFire.Load(), ctx.MouseDisabled.Load(), ctx.MouseFreeMode.Load(), ctx.OverlayHUD.Load())
	}
	a.persistPlay()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("nothing changed, yet the settings file was written")
	}
	ctx.AutoFire.Store(engine.AutoFireMain)
	ctx.MouseDisabled.Store(false)
	a.persistPlay()
	s, path, err := paths.LoadSettings(dir)
	if err != nil || path != file || s.Play.AutoFire != "main" || !s.Play.Mouse || s.Play.HUD != paths.DefaultPlay().HUD {
		t.Fatalf("written back: %+v from %q, %v", s.Play, path, err)
	}
	ctx.ResetSessionState()
	if ctx.AutoFire.Load() != engine.AutoFireOff || ctx.MouseFreeMode.Load() {
		t.Fatal("a purge did not return to the values the run started with")
	}
}

// zoomed is a presentation the player zooms, and nothing else
type zoomed struct {
	Presentation
	cell float64
}

func (z *zoomed) Cell() float64 { return z.cell }

// TestAZoomIsWrittenBackAsLeft: the cell a presentation opened at is never written;
// one the player zoomed to is, into [presentation], and the next run reads it.
func TestAZoomIsWrittenBackAsLeft(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	dir := t.TempDir()
	file := filepath.Join(dir, paths.SettingsFile)
	a, err := NewHeadless(Config{Seed: 1, Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Play: paths.DefaultPlay(), Settings: paths.FileStore{Path: file}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	z := &zoomed{cell: 12}
	a.pres, a.cell = z, z.cell
	a.persistCell()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the zoom did not move, yet the settings file was written")
	}
	z.cell = 9.5
	a.persistCell()
	if s, _, err := paths.LoadSettings(dir); err != nil || s.Presentation.Cell != 9.5 || s.Play != paths.DefaultPlay() {
		t.Fatalf("written back: cell %g, play %+v, %v", s.Presentation.Cell, s.Play, err)
	}
}
