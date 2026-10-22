package app

import (
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func geometryServer(t *testing.T, w, h int) *App {
	t.Helper()
	a, err := New(Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0", Width: w, Height: h,
		Resources: resource.Options{Embedded: true}, Seed: 0x6E01,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

func reportGeometry(t *testing.T, a *App, report network.JoinerReport) {
	t.Helper()
	offer, err := a.assignParticipant()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.noteJoinerReport(offer.Assigned, report); err != nil {
		t.Fatal(err)
	}
}

func mapSize(a *App) (int, int) {
	var w, h int
	a.World().RunSafe(func() {
		cfg := a.World().Resources.Config
		w, h = cfg.MapWidth, cfg.MapHeight
	})
	return w, h
}

// Without an operator size, the first admitted person supplies the real geometry.
func TestAServerWithNoSizeTakesItsMapFromItsFirstPerson(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 0, 0)

	beforeW, beforeH := mapSize(a)
	if beforeW > 80 {
		t.Fatalf("a server with no -size started on a %dx%d map, expected the default", beforeW, beforeH)
	}

	reportGeometry(t, a, network.JoinerReport{Width: 160, Height: 45})
	a.adoptLobbyGeometry()

	w, h := mapSize(a)
	if w <= beforeW || h <= beforeH {
		t.Fatalf("map stayed at %dx%d after a 160x45 guest reported", w, h)
	}
	if w >= 160 || h >= 45 {
		t.Fatalf("map %dx%d did not subtract the margins from the guest's 160x45", w, h)
	}
	if a.Context().Width != 160 || a.Context().Height != 45 {
		t.Fatalf("the run's geometry is %dx%d, want the guest's 160x45",
			a.Context().Width, a.Context().Height)
	}
}

// framePresentation is a screen of a fixed size that keeps every frame flushed to it.
type framePresentation struct {
	w, h   int
	frames [][]terminal.Cell
}

func (p *framePresentation) Name() string                  { return "frames" }
func (p *framePresentation) Dependencies() []string        { return nil }
func (p *framePresentation) Init() error                   { return nil }
func (p *framePresentation) Start() error                  { return nil }
func (p *framePresentation) Stop() error                   { return nil }
func (p *framePresentation) Surface() render.Surface       { return p }
func (p *framePresentation) Size() (int, int)              { return p.w, p.h }
func (p *framePresentation) Blocked() time.Duration        { return 0 }
func (p *framePresentation) CellAspect() vmath.Aspect      { return vmath.RefAspect }
func (p *framePresentation) Events() <-chan terminal.Event { return nil }
func (p *framePresentation) SetPointerMode(bool, bool)     {}
func (p *framePresentation) ColorMode() terminal.ColorMode { return terminal.ColorModeTrueColor }
func (p *framePresentation) Sync()                         {}
func (p *framePresentation) Flush(cells []terminal.Cell, _, _ int) {
	p.frames = append(p.frames, slices.Clone(cells))
}

// TestAJoinerDrawsNoWorldBeforeItsSessions: a joiner builds its world on the map its
// dial was offered, which a server sized by its first person changes at the gate.
// Until the gate installs the session's world, only the status line is drawn, so
// the stand-in's smaller map never shows; the session's then fills the game area.
func TestAJoinerDrawsNoWorldBeforeItsSessions(t *testing.T) {
	// Not parallel: this binds a real socket.
	host, err := newSessionApp(Config{Mode: ModeServer, HostAddress: "127.0.0.1:0",
		Resources: resource.Options{Embedded: true}, Seed: 0x6E04})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(host.Close)
	if err := host.hub.StartAll(); err != nil {
		t.Fatalf("server services: %v", err)
	}
	host.life.Start(time.Now())
	port, err := host.socketPort()
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan error, 1)
	go func() { gate <- host.startHostSession(nil) }()

	const w, h = 160, 45
	screen := &framePresentation{w: w, h: h}
	guest, err := newSessionApp(Config{Mode: ModePlay, JoinAddress: port.Addr().String(),
		Resources: resource.Options{Embedded: true}, Host: Host{Presentation: screen}})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	t.Cleanup(guest.Close)
	area := render.RectWH(parameter.LeftMargin, parameter.TopMargin,
		w-parameter.LeftMargin, h-parameter.TopMargin-parameter.BottomMargin)
	if mw, mh := mapSize(guest); mw >= area.Width() && mh >= area.Height() {
		t.Fatalf("the joiner built on a %dx%d map, not the smaller one a sizing server offers", mw, mh)
	}
	if err := guest.startJoinSession(nil); err != nil {
		t.Fatalf("join gate: %v", err)
	}
	if err := <-gate; err != nil {
		t.Fatalf("server gate: %v", err)
	}

	for i, f := range screen.frames {
		installed := i == len(screen.frames)-1
		for y := area.Y0; y < area.Y1; y++ {
			for x := area.X0; x < area.X1; x++ {
				if void := f[y*w+x].Bg == visual.RgbVoid; void == installed {
					t.Fatalf("frame %d of %d: cell (%d,%d) void=%v, want void only before the install",
						i+1, len(screen.frames), x, y, void)
				}
			}
		}
	}
}

// TestASizingHostIsNotRefusedByItsOwnLiveSession: the guest a host sizes from is on
// the link, so the session is live when the level setup is pushed. That push is the
// host authoring the bounds before the roster closes, not an operator request, and
// the refusal a live session answers operators with must not reach it.
func TestASizingHostIsNotRefusedByItsOwnLiveSession(t *testing.T) {
	// Not parallel: this binds a real socket.
	a := geometryServer(t, 0, 0)
	hostCfg := network.DebugConfig(network.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = engine.CoordinatorParticipant
	hostCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign: a.assignParticipant, Release: a.releaseParticipant, Report: a.noteJoinerReport,
	}, socketWait)
	port := network.NewSocketPort(hostCfg)
	t.Cleanup(func() { _ = port.Close() })
	if err := port.Start(); err != nil {
		t.Fatalf("host transport: %v", err)
	}
	a.AttachTransport(port)

	pending, offered := dialSession(t, port.Addr().String())
	t.Cleanup(func() { _ = pending.Close() })
	report := network.JoinerReport{Width: 160, Height: 45, Identity: identityFromAnchor(offered.Anchor)}
	if err := pending.Complete(nil, report); err != nil {
		t.Fatalf("join reply: %v", err)
	}
	waitSocket(t, port, func() bool { return port.PeerCount() > 0 }, "the guest's admission")
	if !a.World().LiveSession() {
		t.Fatal("the session is not live, so the refusal is not exercised")
	}

	a.adoptLobbyGeometry()

	wantW, wantH := 160-parameter.LeftMargin, 45-parameter.TopMargin-parameter.BottomMargin
	if w, h := mapSize(a); w != wantW || h != wantH {
		t.Fatalf("map is %dx%d with the guest on the link, want its %dx%d", w, h, wantW, wantH)
	}
	if an := a.JoinAnchor().Anchor; an.MapWidth != wantW || an.MapHeight != wantH {
		t.Fatalf("guests are offered %dx%d, want %dx%d", an.MapWidth, an.MapHeight, wantW, wantH)
	}
}

// TestTheFirstPersonWins holds the choice still. Guests arrive throughout the run
// through the mid-run gate, so sizing from the smallest would mean shrinking the
// map under participants already playing on it — which D-14 forbids for the same
// reason a terminal may not crop a shared map.
func TestTheFirstPersonWins(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 0, 0)

	reportGeometry(t, a, network.JoinerReport{Width: 160, Height: 45})
	reportGeometry(t, a, network.JoinerReport{Width: 80, Height: 24})
	a.adoptLobbyGeometry()

	if got := a.Context().Width; got != 160 {
		t.Fatalf("geometry = %d, want the first person's 160", got)
	}
}

// TestAnOperatorSizeIsNotOverridden: -size is a statement, and a guest's terminal
// does not get to contradict it.
func TestAnOperatorSizeIsNotOverridden(t *testing.T) {
	t.Parallel()
	a := geometryServer(t, 100, 30)

	beforeW, beforeH := mapSize(a)
	reportGeometry(t, a, network.JoinerReport{Width: 200, Height: 60})
	a.adoptLobbyGeometry()

	if w, h := mapSize(a); w != beforeW || h != beforeH {
		t.Fatalf("an explicit -size was overridden: %dx%d became %dx%d", beforeW, beforeH, w, h)
	}
}

// TestAnUnreportedOrUnusableGeometryChangesNothing keeps the adoption advisory: a
// guest that says nothing, or says something a viewport could not fit in, leaves
// the session sized as it was rather than breaking it.
func TestAnUnreportedOrUnusableGeometryChangesNothing(t *testing.T) {
	t.Parallel()
	for _, report := range []network.JoinerReport{
		{},
		{Width: 160},
		{Width: 2, Height: 2},
		{Width: -5, Height: -5},
	} {
		a := geometryServer(t, 0, 0)
		beforeW, beforeH := mapSize(a)
		reportGeometry(t, a, report)
		a.adoptLobbyGeometry()
		if w, h := mapSize(a); w != beforeW || h != beforeH {
			t.Fatalf("report %+v moved the map from %dx%d to %dx%d", report, beforeW, beforeH, w, h)
		}
	}
}

// TestTheCellAspectIsPartOfTheGeometry: the run's aspect reaches the simulation
// and the anchor a guest or a replay rebuilds its configuration from; a host that
// reports none plays at the reference.
func TestTheCellAspectIsPartOfTheGeometry(t *testing.T) {
	t.Parallel()
	for _, set := range []vmath.Aspect{0, 1} {
		a, err := NewHeadless(Config{
			Width: 80, Height: 24, Resources: resource.Options{Embedded: true}, Seed: 0x6E03, CellAspect: set,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Close)
		want := set
		if want == 0 {
			want = vmath.RefAspect
		}
		var got vmath.Aspect
		a.World().RunSafe(func() { got = a.World().Resources.Config.CellAspect })
		if got != want {
			t.Fatalf("aspect %v: simulation runs at %v", set, got)
		}
		an := a.JoinAnchor().Anchor
		cfg, err := ConfigFromAnchor(an)
		if err != nil || an.CellAspect != float64(want) || cfg.CellAspect != want {
			t.Fatalf("aspect %v: anchor carries %v, rebuilt as %v (%v)", set, an.CellAspect, cfg.CellAspect, err)
		}
	}
}

// TestAScenarioThatSetsItsOwnMapIsServedOnIt: a dedicated host offers the map its
// scenario's boot sets up, as td does, rather than -size's, and its first guest does
// not resize it. Read before the boot settled, the lobby gave every guest the
// operator's size and the first run played on it.
func TestAScenarioThatSetsItsOwnMapIsServedOnIt(t *testing.T) {
	t.Parallel()
	root := fixedMapScenario(t)
	for _, size := range [][2]int{{120, 40}, {0, 0}} {
		a, err := newSessionApp(Config{
			Mode: ModeServer, HostAddress: "127.0.0.1:0", Width: size[0], Height: size[1],
			Resources: resource.Options{Dir: root, Scenario: "fixed"}, Seed: 0x6E02,
		})
		if err != nil {
			t.Fatalf("server: %v", err)
		}
		t.Cleanup(a.Close)
		reportGeometry(t, a, network.JoinerReport{Width: 190, Height: 44})
		a.adoptLobbyGeometry()
		if w, h := mapSize(a); w != 90 || h != 30 {
			t.Fatalf("-size %dx%d: map %dx%d, want the scenario's 90x30", size[0], size[1], w, h)
		}
		if an := a.JoinAnchor().Anchor; an.MapWidth != 90 || an.MapHeight != 30 || an.CropOnResize {
			t.Fatalf("-size %dx%d: guests are offered %dx%d crop=%v, want 90x30 uncropped",
				size[0], size[1], an.MapWidth, an.MapHeight, an.CropOnResize)
		}
	}
}

// fixedMapScenario writes a scenario whose boot sets up a map of its own, 90x30 and
// uncropped as td's is, and returns the root holding it.
func fixedMapScenario(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := writeScenario(t, root, "fixed", "A map of its own size.")
	main := "[states]\nMainStart = { parent = \"Root\", on_enter = [{ action = \"EmitEvent\", " +
		"event = \"EventLevelSetup\", payload = { width = 90, height = 30, crop_on_resize = false } }] }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.toml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// serveWithBots runs a dedicated host as Serve does up to its lobby: one bot of its
// own seated first, then the lobby waiting in the background, its outcome sent on
// the channel returned.
func serveWithBots(t *testing.T, cfg Config) (*App, <-chan error) {
	t.Helper()
	cfg.Mode, cfg.HostAddress, cfg.Seed, cfg.Bots = ModeServer, "127.0.0.1:0", 0x6E05, []string{"roam"}
	a, err := newSessionApp(cfg)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	if err := a.hub.StartAll(); err != nil {
		t.Fatalf("server services: %v", err)
	}
	a.life.Start(time.Now())
	if err := a.seatBots(); err != nil {
		t.Fatalf("server bots: %v", err)
	}
	gate := make(chan error, 1)
	go func() { gate <- a.startHostSession(nil) }()
	return a, gate
}

// TestASeatNeverSizesTheMap: a server sizes its map from a person's terminal and never
// from a seat's, which is its holder's screen; a lobby of seats alone plays on the
// screen a bot is given, whatever they report.
func TestASeatNeverSizesTheMap(t *testing.T) {
	t.Parallel()
	seat := network.JoinerReport{Holder: engine.CoordinatorParticipant, Width: 200, Height: 60}

	alone := geometryServer(t, 0, 0)
	reportGeometry(t, alone, seat)
	alone.adoptLobbyGeometry()
	if got := alone.Context().Width; got != BotWidth {
		t.Fatalf("geometry = %d after a 200-wide seat alone, want a bot's %d", got, BotWidth)
	}

	joined := geometryServer(t, 0, 0)
	reportGeometry(t, joined, seat)
	reportGeometry(t, joined, network.JoinerReport{Width: 160, Height: 45})
	joined.adoptLobbyGeometry()
	if got := joined.Context().Width; got != 160 {
		t.Fatalf("geometry = %d, want the person's 160 over the seat's 200", got)
	}
}

// TestAServersOwnBotsStartItsGameAtOnce: a server seats its own bots before its
// lobby and they close it at once, first-join window or not, on the map -size or its
// scenario sets, or else on the screen a bot is given.
func TestAServersOwnBotsStartItsGameAtOnce(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	root := fixedMapScenario(t)
	botW, botH := BotWidth-parameter.LeftMargin, BotHeight-parameter.TopMargin-parameter.BottomMargin
	for _, tt := range []struct {
		name string
		cfg  Config
		w, h int
	}{
		{"no size", Config{Resources: resource.Options{Embedded: true}}, botW, botH},
		{"size", Config{Width: 100, Height: 30, Resources: resource.Options{Embedded: true}},
			100 - parameter.LeftMargin, 30 - parameter.TopMargin - parameter.BottomMargin},
		{"fixed map", Config{Resources: resource.Options{Dir: root, Scenario: "fixed"}}, 90, 30},
	} {
		tt.cfg.Lifetime = lifecycle.Policy{FirstJoin: time.Minute}
		host, gate := serveWithBots(t, tt.cfg)
		select {
		case err := <-gate:
			if err != nil {
				t.Fatalf("%s: server gate: %v", tt.name, err)
			}
		case <-time.After(socketWait):
			t.Fatalf("%s: the server's bot did not start its game", tt.name)
		}
		if mw, mh := mapSize(host); mw != tt.w || mh != tt.h {
			t.Fatalf("%s: the bots play on %dx%d, want %dx%d", tt.name, mw, mh, tt.w, tt.h)
		}
	}
}

// TestASeatPlaysInItsHoldersScreen: a seat takes the screen of the run holding it, a
// person's terminal or a run's -size, and a bot's when the run names none; a person's
// run publishes its terminal as it resizes, and its seats take it between steps.
func TestASeatPlaysInItsHoldersScreen(t *testing.T) {
	// Not parallel: an interactive run is built on a presentation of its own.
	person, err := New(Config{Mode: ModePlay, Resources: resource.Options{Embedded: true},
		Host: Host{Presentation: &framePresentation{w: 160, h: 45}}})
	if err != nil {
		t.Fatalf("person: %v", err)
	}
	t.Cleanup(person.Close)
	headless, err := NewHeadless(Config{Resources: resource.Options{Embedded: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(headless.Close)
	sized := mustHeadless(t, fixtureSeed, 90, 28)
	t.Cleanup(sized.Close)
	for _, tt := range []struct {
		name string
		a    *App
		w, h int
	}{
		{"a person", person, 160, 45},
		{"a server", geometryServer(t, 0, 0), BotWidth, BotHeight},
		{"a sized server", geometryServer(t, 100, 30), 100, 30},
		{"a headless run", headless, BotWidth, BotHeight},
		{"a sized headless run", sized, 90, 28},
	} {
		var w, h int
		tt.a.World().RunSafe(func() { w, h = tt.a.seatScreenLocked() })
		if w != tt.w || h != tt.h {
			t.Errorf("%s seats its bots on %dx%d, want %dx%d", tt.name, w, h, tt.w, tt.h)
		}
	}

	person.handleResize(170, 50)
	seated := mustHeadless(t, fixtureSeed, 160, 45)
	t.Cleanup(seated.Close)
	graph, err := loadBotGraph(resource.Options{Embedded: true}, "roam")
	if err != nil {
		t.Fatal(err)
	}
	d, err := bot.NewDriver(seated, seated.ctx, graph, seated.Seed(), seated.localParticipant())
	if err != nil {
		t.Fatal(err)
	}
	src := &seatSource{botSource: botSource{d}, holder: new(atomic.Bool), screen: &person.seatScreen,
		run: seated, shown: screen{160, 45}}
	if _, err := src.Step(); err != nil {
		t.Fatal(err)
	}
	var w, h int
	seated.World().RunSafe(func() { w, h = seated.ctx.Width, seated.ctx.Height })
	if w != 170 || h != 50 {
		t.Fatalf("the seat plays on %dx%d after its holder's terminal became 170x50", w, h)
	}
}

// awayScreen is a framePresentation its player can leave, as Android's view is
type awayScreen struct {
	framePresentation
	away chan bool
}

func (s *awayScreen) Away() <-chan bool { return s.away }

// TestAnUnseenGameStartsOverAtTheScreensNewSize: a player away before the clock
// starts, as behind Android's start screen, holds the game at its first tick, drawn
// undimmed. A screen that changes size before they come starts that game over as
// one opened at the new size would be, still waiting; once they have come, a resize
// crops the game in play instead.
func TestAnUnseenGameStartsOverAtTheScreensNewSize(t *testing.T) {
	// Not parallel: an interactive run is built on a presentation of its own.
	const pw, ph, lw, lh = 60, 100, 100, 60 // a phone held upright, then turned
	open := func(p Presentation) *App {
		t.Helper()
		a, err := New(Config{Mode: ModePlay, Resources: resource.Options{Embedded: true}, Seed: 0x5743,
			Host: Host{Presentation: p}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Close)
		a.scheduler.Prepare()
		return a
	}
	cursor := func(a *App) (at [2]int) {
		a.World().RunSafe(func() {
			if pos, ok := a.World().LocalCursor(); ok {
				at = [2]int{pos.X, pos.Y}
			}
		})
		return at
	}

	screen := &awayScreen{framePresentation{w: pw, h: ph}, make(chan bool, 1)}
	a := open(screen)
	screen.away <- true
	a.takeAway(screen.away)
	if !a.ctx.TimeCtl.IsPaused() || !a.unseen || a.Position().Tick != 0 {
		t.Fatalf("paused %v, unseen %v at tick %d; want an unseen game held at its first tick",
			a.ctx.TimeCtl.IsPaused(), a.unseen, a.Position().Tick)
	}
	if a.renderContext().IsPaused {
		t.Fatal("the waiting game is drawn dimmed, as a pause the player made")
	}

	a.handleResize(lw, lh)
	a.scheduler.RunTicks(0) // the reset's half the clock runs between ticks
	a.keepAway()
	turned := open(&framePresentation{w: lw, h: lh})
	turned.Settle()
	if !a.ctx.TimeCtl.IsPaused() || a.Position().Run != 1 || a.Position().Tick != 0 {
		t.Fatalf("after the turn: paused %v at %+v; want a new game held at its first tick",
			a.ctx.TimeCtl.IsPaused(), a.Position())
	}
	w, h := mapSize(a)
	if tw, th := mapSize(turned); w != tw || h != th || cursor(a) != cursor(turned) {
		t.Fatalf("the turned game is %dx%d with its cursor at %v, want one opened turned: %dx%d at %v",
			w, h, cursor(a), tw, th, cursor(turned))
	}

	a.setAway(false)
	if a.ctx.TimeCtl.IsPaused() || a.unseen {
		t.Fatalf("paused %v, unseen %v once the player came", a.ctx.TimeCtl.IsPaused(), a.unseen)
	}
	a.handleResize(pw, ph)
	a.scheduler.RunTicks(0)
	if pos := a.Position(); pos.Run != 1 {
		t.Fatalf("a game in play started over on a resize: %+v", pos)
	}
	if w2, h2 := mapSize(a); h2 <= h || w2 >= w {
		t.Fatalf("the map is %dx%d after turning back, want it cropped upright from %dx%d", w2, h2, w, h)
	}
}
