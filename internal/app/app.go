package app

import (
	"fmt"
	"os"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/mode"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/service"
	"github.com/lixenwraith/vi-fighter/internal/system"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// App owns the wired runtime: services, world, renderer, input, and scheduler
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

func (a *App) init() error {
	vlog.Info("app", "msg", "init begin")

	// Embedders never call vlog.Configure, so the scope is applied here.
	// The CLI reaches this too and applies it twice; both resolve the spec
	// against ScopeAll, so the second application is a no-op.
	if a.cfg.LogScope != "" {
		if s, err := vlog.ParseScopes(a.cfg.LogScope, vlog.ScopeAll); err == nil {
			vlog.SetScopes(s)
		}
	}

	// Event registry backs FSM trigger resolution and :emit; precedes FSM load
	event.InitRegistry()

	// 1. Service registration (Strongly typed, replacing manifest.BuildServices and serviceArgs)
	colorMode := terminal.DetectColorMode()
	if a.cfg.ColorModeSet {
		colorMode = a.cfg.ColorMode
	}
	termSvc := service.NewTerminalService(colorMode)
	_ = a.hub.Register(termSvc)

	netSvc := service.NewNetworkService(nil) // disabled by default (RoleNone)
	_ = a.hub.Register(netSvc)

	_ = a.hub.Register(service.NewAudioService(a.cfg.AudioMuted, a.cfg.AudioBackend))

	contentSrc, err := ResolveContent(a.cfg)
	if err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	_ = a.hub.Register(service.NewContentService(contentSrc))

	// 2. World creation
	// Services take no world argument, so placement relative to InitAll is free
	a.world = engine.NewWorld()

	// 3. Service init in dependency order; rolls back internally on failure
	if err := a.hub.InitAll(); err != nil {
		return err
	}

	// 4. Service resources bridged into the ECS
	a.hub.BindResources(a.world.Resources)

	// 5. Terminal; the orchestrator needs the interface directly
	a.termSvc = termSvc
	a.term = a.termSvc.Terminal()
	core.SetCrashTerminal(a.term)
	width, height := a.term.Size()

	// 6. GameContext initializes the remaining world resources
	a.ctx = engine.NewGameContext(a.world, width, height)
	a.world.Resources.Config.ColorMode = a.term.ColorMode()
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

	// TODO: wire event handling in network system

	// Post-Context wiring: Connect network service to the initialized event queue
	// netSvc.SetEventQueue(a.world.Resources.Event.Queue)

	// 7. Systems; AddSystem sorts by Priority(), manifest order breaks ties
	for _, sys := range manifest.BuildSystems(a.world) {
		a.world.AddSystem(sys)
	}

	// 8. Renderers; Register sorts by priority, manifest order breaks ties
	a.orchestrator = render.NewRenderOrchestrator(a.term, a.ctx.Width, a.ctx.Height)
	for _, reg := range manifest.BuildRenderers(a.ctx) {
		a.orchestrator.Register(reg.Renderer, reg.Priority)
	}

	// 9. Input
	a.inputMachine = input.NewMachine()
	if err := a.loadKeymap(); err != nil {
		return err
	}
	a.router = mode.NewRouter(a.ctx, a.inputMachine)
	a.router.SetMouseModeApplier(a.applyMouseMode)

	// 10. Clock scheduler and frame synchronization
	a.frameReady = make(chan struct{}, 1)
	var resetChan chan<- struct{}
	a.scheduler, a.gameUpdateDone, resetChan = engine.NewClockScheduler(
		a.world,
		a.ctx.PausableClock,
		&a.ctx.IsPaused,
		parameter.GameUpdateInterval,
		a.frameReady,
	)
	a.ctx.ResetChan = resetChan

	// 11. FSM
	if err := a.loadFSM(); err != nil {
		return err
	}

	// 12. Event handlers
	// MetaSystem is event-only and deliberately absent from the manifest
	metaSystem := system.NewMetaSystem(a.ctx)
	a.scheduler.RegisterEventHandler(metaSystem.(event.Handler))
	for _, sys := range a.world.Systems() {
		if h, ok := sys.(event.Handler); ok {
			a.scheduler.RegisterEventHandler(h)
		}
	}

	vlog.Info("app", "msg", "init complete",
		"width", a.ctx.Width,
		"height", a.ctx.Height,
		"systems", len(a.world.Systems()))
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
	a.inputMachine.SetKeyTable(input.MergeKeyTable(input.DefaultKeyTable(), override))
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
