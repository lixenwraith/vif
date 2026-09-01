package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/mode"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/internal/system"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// fallbackColorMode is published when no terminal exists to detect against
const fallbackColorMode = terminal.ColorMode256

// App owns the wired runtime: services, world, renderer, input, and scheduler
// Headless runs leave termSvc, term and orchestrator nil
type App struct {
	cfg Config

	hub        *service.Hub
	termSvc    *service.TerminalService
	networkSvc *service.NetworkService
	term       terminal.Terminal

	world *engine.World
	ctx   *engine.GameContext

	orchestrator *render.RenderOrchestrator
	inputMachine *input.Machine
	router       *mode.Router
	recorder     *journal.Recorder

	scheduler      *engine.ClockScheduler
	frameReady     chan struct{}
	gameUpdateDone <-chan struct{}

	pendingJoin  *network.PendingJoin
	sessionMu    sync.Mutex
	sessionOffer network.SessionOffer
	// sessionRoster is the lobby the coordinator has admitted so far. It grows one
	// entry per accepted connection and closes into the offer the start gate sends.
	sessionRoster []network.SessionParticipant

	// midRunPort is the socket a solo run opened for itself with :host. A run
	// started with -host takes its endpoint from NetworkService instead, which the
	// hub owns and closes; this one is owned here because nothing else knows it
	// exists.
	midRunPort *network.SocketPort

	// snapshotTelemetry is reserved during construction so a capture or an install
	// can publish its cost into a registry that is frozen by then.
	snapshotTelemetry snapshotTelemetry
}

// New wires the runtime, releasing anything already started on failure
// every step panicked; the map editor and wasm entry need errors
func New(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	a := &App{cfg: cfg, hub: service.NewHub()}
	if a.cfg.HostAddress != "" && a.cfg.networkConfig == nil {
		a.cfg.networkConfig = a.hostNetworkConfig()
	}
	if a.cfg.JoinAddress != "" && a.cfg.networkConfig == nil {
		return nil, errors.New("join sessions must be started with app.Run")
	}
	if err := a.init(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// init wires the runtime in dependency order; a headless run skips presentation
func (a *App) init() error {
	vlog.Info("app", "msg", "init begin", "mode", a.cfg.Mode.String())

	// Root RNG seed; resolved first, since services and systems both derive
	// from it. A drawn seed is logged so any run replays with -seed.
	if a.cfg.Seed == 0 {
		a.cfg.Seed = uint64(time.Now().UnixNano()) // [wall] once per process
	}
	vlog.Info("app", "msg", "seed", "seed", a.cfg.Seed)

	// Embedders never call vlog.Configure, so the scope is applied here.
	// The CLI reaches this too and applies it twice; both resolve the spec
	// against ScopeAll, so the second application is a no-op.
	if a.cfg.LogScope != "" {
		if s, err := vlog.ParseScopes(a.cfg.LogScope, vlog.ScopeAll); err == nil {
			vlog.SetScopes(s)
		}
	}

	if err := a.initServices(); err != nil {
		return err
	}
	a.initWorld()
	if err := a.initJournal(); err != nil {
		return err
	}
	if a.cfg.Mode.Presents() {
		a.initPresentation()
	}
	if err := a.initInput(); err != nil {
		return err
	}
	if err := a.initScheduler(); err != nil {
		return err
	}
	a.ctx.SessionCtl = a

	vlog.Info("app", "msg", "init complete",
		"width", a.ctx.Width,
		"height", a.ctx.Height,
		"systems", len(a.world.Systems()))
	return nil
}

// initServices registers and initializes the I/O boundary. A driven run registers
// only what its mode presents, so no goroutine exists that the caller does not own.
func (a *App) initServices() error {
	// Event registry backs FSM trigger resolution and :emit; precedes FSM load
	event.EnsureRegistry()

	if a.cfg.Mode.Presents() {
		colorMode := terminal.DetectColorMode()
		if a.cfg.ColorModeSet {
			colorMode = a.cfg.ColorMode
		}
		a.termSvc = service.NewTerminalService(colorMode)
		_ = a.hub.Register(a.termSvc)
	}
	if a.cfg.Mode == ModePlay || a.cfg.HostAddress != "" || a.cfg.JoinAddress != "" {
		a.networkSvc = service.NewNetworkService(a.cfg.networkConfig)
		_ = a.hub.Register(a.networkSvc)
	}
	if a.cfg.Mode.Audio() {
		_ = a.hub.Register(service.NewAudioService(a.cfg.AudioMuted, a.cfg.AudioBackend))
	}

	contentSrc, err := ResolveContent(a.cfg)
	if err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	_ = a.hub.Register(service.NewContentService(contentSrc, a.cfg.Seed))

	// Init in dependency order; rolls back internally on failure
	return a.hub.InitAll()
}

// initWorld builds the ECS world, the game context and the system set
func (a *App) initWorld() {
	// Services take no world argument, so placement relative to InitAll is free
	a.world = engine.NewWorld()
	a.world.Resources.Rand = engine.NewRandResource(a.cfg.Seed)

	// Service resources bridged into the ECS
	a.hub.BindResources(a.world.Resources)
	if r := a.world.Resources.Network; r != nil {
		r.OnDeparture = a.releaseParticipant32
		r.SharedDigest = a.sharedDigestLocked
		// A session endpoint exists, so this run is shared for its whole life
		// whether or not a peer is attached at a given tick. Latching it here rather
		// than reading the port keeps the anchor, the D-14 verdict and the playout
		// barrier answering one question, which is what a reproduction adopts.
		a.world.MarkSessionShared()
	}

	// The terminal supplies color whenever one exists, but dimensions only when the
	// mode says so; a replay's come from the journal, via config
	width, height := a.cfg.Width, a.cfg.Height
	colorMode := fallbackColorMode
	if a.cfg.Mode.Presents() {
		a.term = a.termSvc.Terminal()
		core.SetCrashTerminal(a.term)
		colorMode = a.term.ColorMode()
	}
	if a.cfg.Mode.OwnsGeometry() {
		width, height = a.term.Size()
	}

	// GameContext initializes the remaining world resources.
	// A driven run uses the manual clock: game time is a pure function of ticks.
	if a.cfg.Mode.Driven() {
		a.ctx = engine.NewGameContextWithClock(a.world, width, height, engine.NewManualClock())
	} else {
		a.ctx = engine.NewGameContext(a.world, width, height)
	}
	a.world.Resources.Config.ColorMode = colorMode

	a.applyMapLatch()

	if n := a.cfg.StatTicks; n != 0 {
		if n < 0 {
			n = 0 // explicit disable
		}
		a.world.Resources.Status.SetSnapshotInterval(uint64(n))
	}

	// Recorder is laid out at Freeze; enabling it here only sizes the ring.
	// Opt-in: an explicit depth always wins, the default applies only when a
	// log session is configured. A bare run installs no recorder, so no
	// trigger can write a file the operator did not ask for.
	recDepth := a.cfg.RecTicks
	if recDepth == 0 && vlog.Enabled() {
		recDepth = parameter.RecorderDepthTicks
	}
	a.world.Resources.Status.EnableRecorder(recDepth)

	// Corpus telemetry needs the registry NewGameContext creates
	service.MustGet[*service.ContentService](a.hub, "content").
		PublishStatus(a.world.Resources.Status)
	a.snapshotTelemetry = newSnapshotTelemetry(a.world.Resources.Status)

	// Initial rate; ParseScale rejects "" so a bare run stays at real time
	if s, ok := engine.ParseScale(a.cfg.TimeScaleSpec); ok {
		a.ctx.TimeCtl.SetScale(s)
	}
	// The host can wait in the lobby much longer than a joiner. Freeze both clocks
	// before the FSM creates any game-time deadline, then release them only after
	// the common start gate. Otherwise the lobby wait ages the host's gold timer and
	// every other absolute deadline before tick one.
	if a.cfg.HostAddress != "" || a.cfg.JoinAddress != "" {
		a.ctx.TimeCtl.SetPaused(true)
	}

	// Systems; AddSystem sorts by Priority(), manifest order breaks ties
	for _, sys := range manifest.BuildSystems(a.world) {
		a.world.AddSystem(sys, manifest.ProfileFor(sys.Name()))
	}
	// This game's streams are drawn; advance so the next game differs
	a.world.Resources.Rand.NextSession()
}

// applyMapLatch installs this run's D-14 position before any system is built and
// before the FSM boot script spawns cursor slot zero at the centre of the map.
// Adopting bounds later would leave that shared cursor on this terminal's centre
// rather than the session's, which is a shared position no crossing ever corrects.
//
// LockMap latches the world as shared, which closes the crop path and engages the
// playout barrier for the whole run; a reproduction of a session — a join, a
// catch-up or a replay — additionally carries the bounds it must start on. A
// hosting run keeps its own terminal's bounds and only stops deriving them.
func (a *App) applyMapLatch() {
	if a.cfg.LockMap {
		a.world.MarkSessionShared()
	}
	cfg := a.world.Resources.Config
	if a.cfg.MapWidth <= 0 || a.cfg.MapHeight <= 0 {
		return
	}
	if a.cfg.MapWidth == cfg.MapWidth && a.cfg.MapHeight == cfg.MapHeight &&
		a.cfg.CropOnResize == cfg.CropOnResize {
		return
	}
	a.world.SetupLevel(a.cfg.MapWidth, a.cfg.MapHeight, false, a.cfg.CropOnResize)
}

// initPresentation builds the render pipeline. The buffer is terminal-sized while
// renderers draw in simulation coordinates, so a replay on a smaller terminal clips;
// the windowed composite replaces that.
func (a *App) initPresentation() {
	w, h := a.term.Size()
	a.orchestrator = render.NewRenderOrchestrator(a.term, w, h)
	for _, reg := range manifest.BuildRenderers(a.ctx) {
		a.orchestrator.Register(reg.Renderer, reg.Priority)
	}
}

// initInput builds the intent pipeline. Kept headless because intents are the
// injection path; only the terminal mouse sink is skipped.
func (a *App) initInput() error {
	a.inputMachine = input.NewMachine()
	if err := a.loadKeymap(); err != nil {
		return err
	}
	a.router = mode.NewRouter(a.ctx, a.inputMachine)
	if a.cfg.Mode.OwnsInput() {
		a.router.SetMouseModeApplier(a.applyMouseMode)
	}
	return nil
}

// initScheduler wires the clock scheduler, loads the FSM, and registers the
// systems that handle events
func (a *App) initScheduler() error {
	a.frameReady = make(chan struct{}, 1)
	var resetChan chan<- struct{}
	a.scheduler, a.gameUpdateDone, resetChan = engine.NewClockScheduler(
		a.world,
		a.ctx.TimeCtl,
		parameter.GameUpdateInterval,
		a.frameReady,
	)
	a.ctx.ResetChan = resetChan

	if err := a.loadFSM(); err != nil {
		return err
	}

	// MetaSystem is context-scoped, so it joins the set here rather than via the manifest
	meta := system.NewMetaSystem(a.ctx)
	a.world.AddSystem(meta, manifest.ProfileFor(meta.Name()))

	// The declared dependency graph is resolved once the set is complete; an
	// unknown name or a cycle is a startup error, not a runtime degradation
	order, err := a.world.SystemInitOrder()
	if err != nil {
		return fmt.Errorf("system dependencies: %w", err)
	}
	vlog.Debug("app", "msg", "system init order", "systems", strings.Join(order, ","))

	for _, sys := range a.world.Systems() {
		if h, ok := sys.(event.Handler); ok {
			a.scheduler.RegisterEventHandler(h)
		}
	}
	return nil
}

// embeddedLabel is the identity recorded for a built-in asset
const embeddedLabel = "embedded"

// resolveConfigID names the FSM entry a run loaded, or the embedded default.
// Shared by the anchor writer and VerifyAnchor so the two cannot disagree.
func resolveConfigID(cfg Config) string {
	path, err := ResolveGameConfig(cfg)
	if err != nil || path == "" {
		return embeddedLabel
	}
	return path
}

// initJournal opens the replay journal and installs it on the event queue.
// Opt-in: it records every non-system event for the life of the run.
func (a *App) initJournal() error {
	if !a.cfg.Journal {
		return nil
	}

	r, err := journal.Start(a.world.Resources.Event.Queue, a.buildAnchor(), a.cfg.JournalSink)
	if err != nil {
		return err
	}
	a.recorder = r
	vlog.Info("app", "msg", "journal open", "path", r.Path())
	return nil
}

// buildAnchor describes what a replay must reproduce. Reads the telemetry
// ContentService published during initWorld, so anchor and verification share a source.
func (a *App) buildAnchor() event.JournalAnchor {
	reg := a.world.Resources.Status
	cfg := a.world.Resources.Config
	return event.JournalAnchor{
		Speed:         a.ctx.TimeCtl.Scale().String(),
		ConfigID:      resolveConfigID(a.cfg),
		ContentID:     reg.Strings.Get("content.source").Load(),
		ContentPin:    service.MustGet[*service.ContentService](a.hub, "content").Pin(),
		ContentFiles:  uint64(reg.Ints.Get("content.files").Load()),
		ContentBlocks: uint64(reg.Ints.Get("content.blocks").Load()),
		ContentLines:  uint64(reg.Ints.Get("content.lines").Load()),
		Seed:          a.world.Resources.Rand.Root(),
		Session:       a.world.Resources.Rand.Session(),
		TickInterval:  int64(parameter.GameUpdateInterval),
		Width:         a.ctx.Width,
		Height:        a.ctx.Height,
		MapWidth:      cfg.MapWidth,
		MapHeight:     cfg.MapHeight,
		CropOnResize:  cfg.CropOnResize,
		SessionShared: a.world.SessionShared(),
	}
}

// Close stops the scheduler before the services it depends on
// Safe on a partially constructed App
func (a *App) Close() {
	vlog.Info("app", "msg", "shutdown begin")
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.pendingJoin != nil {
		_ = a.pendingJoin.Close()
	}
	a.closeMidRunPort()
	a.hub.StopAll()

	if a.recorder != nil {
		stats, err := a.recorder.Close()
		vlog.Info("app", "msg", "journal close",
			"path", stats.Path, "records", stats.Emitted, "encode_failed", stats.EncodeFailed)
		if err != nil {
			vlog.Error("app", "msg", "journal close", "error", err.Error())
		}
	}

	vlog.Info("app", "msg", "shutdown complete")
}

// loadKeymap merges an external key table over the defaults
// A missing discovered file is silent; a missing explicit path is an error
func (a *App) loadKeymap() error {
	path := ResolveKeymap(a.cfg)
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if a.cfg.KeymapPath == "" {
			return nil // discovered path vanished between stat and read
		}
		return fmt.Errorf("keymap load: %w", err)
	}

	override, err := input.LoadKeyConfig(data)
	if err != nil {
		return fmt.Errorf("keymap config %s: %w", path, err)
	}
	kt := input.MergeKeyTable(input.DefaultKeyTable(), override)
	a.inputMachine.SetKeyTable(kt)
	a.ctx.KeyTable = kt
	return nil
}

// loadFSM resolves and loads the FSM config, falling back to the embedded default
func (a *App) loadFSM() error {
	path, err := ResolveGameConfig(a.cfg)
	if err != nil {
		return fmt.Errorf("game config: %w", err)
	}
	if path == "" {
		if err := a.scheduler.LoadFSMFromFS(asset.DefaultFSMConfig, asset.DefaultFSMEntry, manifest.RegisterFSMComponents); err != nil {
			return fmt.Errorf("load embedded FSM: %w", err)
		}
		return nil
	}
	if err := a.scheduler.LoadFSMFromPath(path, manifest.RegisterFSMComponents); err != nil {
		return fmt.Errorf("load FSM %s: %w", path, err)
	}
	return nil
}
