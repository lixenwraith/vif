package app

import (
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/help"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/mode"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/internal/system"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// headlessColorMode is the fixed mode a headless run publishes; there is no
// terminal to detect against and no renderer to consume it
const headlessColorMode = terminal.ColorMode256

// App owns the wired runtime: services, world, renderer, input, and scheduler
// Headless runs leave termSvc, term and orchestrator nil
type App struct {
	cfg Config

	hub     *service.Hub
	termSvc *service.TerminalService
	term    terminal.Terminal

	world *engine.World
	ctx   *engine.GameContext

	orchestrator *render.RenderOrchestrator
	inputMachine *input.Machine
	router       *mode.Router

	scheduler      *engine.ClockScheduler
	frameReady     chan struct{}
	gameUpdateDone <-chan struct{}
}

// New wires the runtime, releasing anything already started on failure
// every step panicked; the map editor and wasm entry need errors
func New(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	a := &App{cfg: cfg, hub: service.NewHub()}
	if err := a.init(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// init wires the runtime in dependency order; a headless run skips presentation
func (a *App) init() error {
	vlog.Info("app", "msg", "init begin", "headless", a.cfg.Headless)

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
	if !a.cfg.Headless {
		a.initPresentation()
	}
	if err := a.initInput(); err != nil {
		return err
	}
	if err := a.initScheduler(); err != nil {
		return err
	}

	vlog.Info("app", "msg", "init complete",
		"width", a.ctx.Width,
		"height", a.ctx.Height,
		"systems", len(a.world.Systems()))
	return nil
}

// initServices registers and initializes the I/O boundary. A headless run
// registers content only, so no terminal, audio or network goroutine exists.
func (a *App) initServices() error {
	// Event registry backs FSM trigger resolution and :emit; precedes FSM load
	event.InitRegistry()

	if !a.cfg.Headless {
		colorMode := terminal.DetectColorMode()
		if a.cfg.ColorModeSet {
			colorMode = a.cfg.ColorMode
		}
		a.termSvc = service.NewTerminalService(colorMode)
		_ = a.hub.Register(a.termSvc)

		_ = a.hub.Register(service.NewNetworkService(nil)) // disabled by default (RoleNone)
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

	// The terminal owns dimensions and color interactively; headless takes
	// dimensions from config and publishes a fixed mode
	width, height := a.cfg.Width, a.cfg.Height
	colorMode := headlessColorMode
	if !a.cfg.Headless {
		a.term = a.termSvc.Terminal()
		core.SetCrashTerminal(a.term)
		width, height = a.term.Size()
		colorMode = a.term.ColorMode()
	}

	// GameContext initializes the remaining world resources.
	// Headless runs the manual clock: game time is a pure function of ticks.
	if a.cfg.Headless {
		a.ctx = engine.NewGameContextWithClock(a.world, width, height, engine.NewManualClock())
	} else {
		a.ctx = engine.NewGameContext(a.world, width, height)
	}
	a.world.Resources.Config.ColorMode = colorMode

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

	// TODO: wire event handling in network system, post-context, against the
	// event queue GameContext creates

	// Initial rate; ParseScale rejects "" so a bare run stays at real time
	if s, ok := engine.ParseScale(a.cfg.TimeScaleSpec); ok {
		a.ctx.TimeCtl.SetScale(s)
	}

	// Systems; AddSystem sorts by Priority(), manifest order breaks ties
	for _, sys := range manifest.BuildSystems(a.world) {
		a.world.AddSystem(sys)
	}
	// This game's streams are drawn; advance so the next game differs
	a.world.Resources.Rand.NextSession()
}

// initPresentation builds the render pipeline; never reached headless
func (a *App) initPresentation() {
	// Register sorts by priority, manifest order breaks ties
	a.orchestrator = render.NewRenderOrchestrator(a.term, a.ctx.Width, a.ctx.Height)
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
	if !a.cfg.Headless {
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
	a.world.AddSystem(system.NewMetaSystem(a.ctx))
	for _, sys := range a.world.Systems() {
		if h, ok := sys.(event.Handler); ok {
			a.scheduler.RegisterEventHandler(h)
		}
	}
	return nil
}

// Close stops the scheduler before the services it depends on
// Safe on a partially constructed App
func (a *App) Close() {
	vlog.Info("app", "msg", "shutdown begin")
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	a.hub.StopAll()
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
	help.SetKeyTable(kt) // Help documents the bindings actually in force
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
