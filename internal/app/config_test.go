package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
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
	ctx.SetOverlaySelection("settings")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("settings/#Controls/auto")
	key(terminal.KeyEnter, 0)
	if ctx.AutoFire.Load() != engine.AutoFireOff || !ctx.TimeCtl.IsPaused() {
		t.Fatal("auto-fire change did not apply while paused")
	}
	key(terminal.KeyEscape, 0)
	if ctx.GetOverlaySelection() != "settings" {
		t.Fatal("back lost the page selection")
	}
	key(terminal.KeyEscape, 0)
	if ctx.GetMode() != core.ModeNormal || ctx.IsOverlayActive() || ctx.TimeCtl.IsPaused() || ctx.AutoFire.Load() != engine.AutoFireOff {
		t.Fatal("closing the menu failed to resume with changed settings")
	}
	key(terminal.KeyRune, ':')
	key(terminal.KeyRune, 'g')
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("game")
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("game/speed")
	key(terminal.KeyRight, 0)
	a.World().RunSafe(a.router.RefreshConfigMenu)
	m := ctx.GetOverlayContent().Menu
	if i := slices.IndexFunc(m.Rows, func(r core.OverlayMenuRow) bool { return r.Key == "game/speed" }); i < 0 || m.Rows[i].Value != "2x" || !ctx.TimeCtl.IsPaused() {
		t.Fatalf("queued speed change was not reflected: %+v", m)
	}
	key(terminal.KeyEscape, 0)
	ctx.SetOverlaySelection("developer")
	ctx.World.Resources.Status.SetSnapshotInterval(77)
	key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("developer/#Logging/stat")
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
// at once and closes the menu, and a tick asked of it takes none. Closing the menu or
// the command line ends only the pause each made; p runs the game on, and so does a
// step after :pause.
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
	ctx.SetOverlaySelection("game/pause")
	key(terminal.KeyEnter, 0)
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
	key(terminal.KeyRune, 'p')
	a.Tick(1)
	if ctx.TimeCtl.IsPaused() || a.Position().Tick != at+1 {
		t.Fatalf("p left paused %v at tick %d, want running at %d", ctx.TimeCtl.IsPaused(), a.Position().Tick, at+1)
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
		name     string
		cfg      Config
		scripted bool // the run is a script's or a bot's (wiring.scriptedSession)
		want     string
	}{
		{name: "host", cfg: Config{Membership: Membership{HostAddress: ":7777"}}},
		{name: "join", cfg: Config{Membership: Membership{JoinAddress: "127.0.0.1:7777"}}},
		{name: "later host cap", cfg: Config{Membership: Membership{Participants: 4}}},
		{
			name: "guest cannot set host cap",
			cfg:  Config{Membership: Membership{JoinAddress: "127.0.0.1:7777", Participants: 4}},
			want: "not a joining guest",
		},
		{
			name: "headless without script runner",
			cfg:  Config{Mode: ModeHeadless, Membership: Membership{HostAddress: ":7777"}, Width: 80, Height: 24},
			want: "RunScript",
		},
		{
			name:     "headless script host",
			cfg:      Config{Mode: ModeHeadless, Membership: Membership{HostAddress: ":7777"}, Width: 80, Height: 24},
			scripted: true,
		},
		{
			name: "exclusive",
			cfg:  Config{Membership: Membership{HostAddress: ":7777", JoinAddress: "127.0.0.1:7777"}},
			want: "mutually exclusive",
		},
		{
			name: "replay cannot network",
			cfg:  Config{Mode: ModeReplay, Membership: Membership{HostAddress: ":7777"}, Width: 80, Height: 24},
			want: "playback cannot join",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runConfig{Config: tt.cfg}
			run.wiring.scriptedSession = tt.scripted
			err := run.Validate()
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
// line Run was given with what the restart named, so a pinned seed survives, and
// its console lists on from this one's. A :join
// leaves the -players and -authority of a session it no longer hosts, and falls back
// to that run on its own, in no session; a run following nobody starts over on its
// own, keeping the ceiling its session was opened with. A door that reopens does so
// under the policy its session held, not the command line's.
func TestARestartKeepsTheCommandLineAndTakesWhatItNames(t *testing.T) {
	t.Parallel()
	line := runConfig{Config: Config{Seed: 7, Resources: resource.Options{Scenario: "main", Embedded: true},
		Membership: Membership{HostAddress: "127.0.0.1:7000", JoinAddress: "10.0.0.1:7777", SessionName: "old",
			Participants: 4, Migrate: true}}, wiring: wiring{membership: membership.Wiring{SessionPlayers: 3}}}
	dialled := &membership.JoinDial{}
	console := []engine.ConsoleLine{{Text: ":join 10.0.0.2:7777/abc", Kind: parameter.StatusInput, At: 1, Count: 1}}
	next, solo := line.restarted(&restartRequest{Notice: "n", Scenario: "duel", Host: "127.0.0.1:7001",
		Join: "10.0.0.2:7777/abc", Dialled: dialled, Bots: []string{"roam"}, Journal: true, Console: console})
	if next.Seed != 7 || next.Resources.Scenario != "duel" || next.Resources.Embedded || next.Notice != "n" ||
		!slices.Equal(next.Membership.Bots, []string{"roam"}) || !next.Journal || !next.wiring.journalCarried {
		t.Fatalf("next run %+v; want the command line with what the restart named", next)
	}
	if !slices.Equal(next.wiring.console, console) || solo == nil || !slices.Equal(solo.wiring.console, console) {
		t.Fatal("the next run, or the one a failed join falls back to, does not list the console's lines on")
	}
	if next.Membership.HostAddress != "" || next.wiring.resumeHost != "127.0.0.1:7001" {
		t.Fatalf("hosting opens %q before the clock and %q after it; want only after",
			next.Membership.HostAddress, next.wiring.resumeHost)
	}
	if next.Membership.JoinAddress != "10.0.0.2:7777" || next.Membership.SessionName != "abc" ||
		next.wiring.dialled != dialled {
		t.Fatalf("a :join builds %q/%q on %p; want the target on its own dial",
			next.Membership.JoinAddress, next.Membership.SessionName, next.wiring.dialled)
	}
	if next.Membership.Participants != 0 || next.Membership.Migrate {
		t.Fatalf("a :join keeps -players %d and a migrating authority %t; want neither, its session's coming with its offer",
			next.Membership.Participants, next.Membership.Migrate)
	}
	if solo == nil || solo.Seed != 7 || solo.Membership.JoinAddress != "" || solo.Membership.SessionName != "" ||
		solo.wiring.membership.SessionPlayers != 0 || solo.wiring.dialled != nil {
		t.Fatalf("a :join falls back to %+v; want the same run on its own", solo)
	}
	moved, _ := line.restarted(&restartRequest{Rejoin: true, Join: "10.0.0.3:7777"})
	if moved.Membership.JoinAddress != "10.0.0.3:7777" || moved.Membership.SessionName != "old" {
		t.Fatalf("a rejoin dials %q/%q; want the session's moved door",
			moved.Membership.JoinAddress, moved.Membership.SessionName)
	}
	if alone, solo := line.restarted(&restartRequest{}); alone.Membership.JoinAddress != "" || solo != nil ||
		alone.wiring.membership.SessionPlayers != 3 || !alone.Membership.Migrate {
		t.Fatalf("a run following nobody dials %q under a ceiling of %d, migrating %t; want nothing dialled, the session's 3 and the command line's policy kept",
			alone.Membership.JoinAddress, alone.wiring.membership.SessionPlayers, alone.Membership.Migrate)
	}
	if pinned, _ := line.restarted(&restartRequest{Scenario: "duel", Host: "127.0.0.1:7001"}); pinned.Membership.Migrate {
		t.Fatal("a door reopened for a session that ends with its host migrates, as the command line asked of another")
	}
	line.Membership.Migrate = false
	if moving, _ := line.restarted(&restartRequest{Scenario: "duel", Host: "127.0.0.1:7001", Migrate: true}); !moving.Membership.Migrate {
		t.Fatal("a door reopened for a migrating session ends with its host, as the command line's default would")
	}
}

func TestHeadlessSessionRequiresTheScriptGate(t *testing.T) {
	t.Parallel()
	_, err := NewHeadless(Config{Membership: Membership{HostAddress: ":7777"}})
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
// mouse, the HUD and the camera's edge before the first tick; an unchanged run
// writes no file, each value changed is written to the settings file and nothing
// else, and a purge returns to the run's start.
func TestPlaySettingsStartTheRunAndAreWrittenBackAsLeft(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	dir := t.TempDir()
	file := filepath.Join(dir, paths.SettingsFile)
	a, err := NewHeadless(Config{Seed: 1, Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Play: paths.Play{AutoFire: "off", HUD: true, SfxVolume: 0.5, CameraEdge: 20}, Settings: paths.FileStore{Path: file}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	ctx := a.ctx
	if ctx.AutoFire.Load() != engine.AutoFireOff || !ctx.MouseDisabled.Load() || ctx.MouseFreeMode.Load() || !ctx.OverlayHUD.Load() ||
		a.world.CameraEdge() != 20 {
		t.Fatalf("the run did not start as [play] says: auto %d mouse off %v free %v hud %v camera edge %d",
			ctx.AutoFire.Load(), ctx.MouseDisabled.Load(), ctx.MouseFreeMode.Load(), ctx.OverlayHUD.Load(), a.world.CameraEdge())
	}
	a.persistPlay()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("nothing changed, yet the settings file was written")
	}
	if ctx.Takeover.Load() {
		t.Fatal("a run whose [play] says nothing of it lets a session move to it")
	}
	ctx.AutoFire.Store(engine.AutoFireMain)
	ctx.MouseDisabled.Store(false)
	ctx.MaxFPS.Store(24)
	ctx.PauseCommand.Store(true)
	ctx.Takeover.Store(true)
	a.world.SetCameraEdge(25)
	a.persistPlay()
	s, path, _, err := paths.LoadSettings(dir)
	if err != nil || path != file || s.Play.AutoFire != "main" || !s.Play.Mouse || s.Play.MaxFPS != 24 || s.Play.HUD != paths.DefaultPlay().HUD ||
		!s.Play.PauseCommand || !s.Play.Takeover || s.Play.CameraEdge != 25 {
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
	if s, _, _, err := paths.LoadSettings(dir); err != nil || s.Presentation.Cell != 9.5 || s.Play != paths.DefaultPlay() {
		t.Fatalf("written back: cell %g, play %+v, %v", s.Presentation.Cell, s.Play, err)
	}
}
