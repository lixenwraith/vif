package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/manifest"
	"github.com/lixenwraith/vi-fighter/mode"
	"github.com/lixenwraith/vi-fighter/registry"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/service"
	"github.com/lixenwraith/vi-fighter/system"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CLI flags
var (
	flagColor256    = flag.Bool("cx", false, "Force 256-color mode")
	flagColorTrue   = flag.Bool("ct", false, "Force truecolor mode")
	flagAudioMute   = flag.Bool("am", false, "Start with audio muted")
	flagAudioUnmute = flag.Bool("au", false, "Start with audio unmuted")
	flagContentPath = flag.String("f", "", "Content file path or glob pattern")
)

func main() {
	// 0. Config
	// Parse CLI flags
	flag.Parse()

	// Resolve service args from flags
	serviceArgs := buildServiceArgs()

	// 1. Service Hub
	hub := service.NewHub()

	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r) // Crash
		}
		// Normal exit
		hub.StopAll()
	}()

	// 2. Registry Setup
	manifest.RegisterServices()
	manifest.RegisterSystems()
	manifest.RegisterRenderers()

	// 3. Service Registration
	for _, name := range manifest.ActiveServices() {
		factory, ok := registry.GetService(name)
		if !ok {
			panic(fmt.Sprintf("service not registered: %s", name))
		}
		svc := factory().(service.Service)
		args := serviceArgs[name]
		if err := hub.RegisterWithArgs(svc, args...); err != nil {
			panic(fmt.Sprintf("service registration failed: %s: %v", name, err))
		}
	}

	// 4. World Creation, Resource & Component Initialization
	world := engine.NewWorld()

	// TODO: check moving it up since no world dependency
	// 5.: Service Initialization
	if err := hub.InitAll(); err != nil {
		panic(fmt.Sprintf("service init failed: %v", err))
	}

	// 6. Resource ServiceBridge - Services contribute to ECS
	hub.PublishResources(world.Resource.ServiceBridge)

	// 7. Terminal extraction (orchestrator needs interface directly)
	termSvc := service.MustGet[*terminal.TerminalService](hub, "terminal")
	term := termSvc.Terminal()
	width, height := term.Size()

	// 8. GameContext Creation
	// NOTE: World resources are initialized in GameContext
	ctx := engine.NewGameContext(world, width, height)

	// // 7. Audio Engine
	// // Initialize audio from services
	// if audioSvc, ok := hub.GetComponent("audio"); ok {
	// 	if as, ok := audioSvc.(*audio.AudioService); ok {
	// 		if player := as.Player(); player != nil {
	// 			ctx.SetAudioEngine(player.(engine.AudioPlayer))
	// 		}
	// 	}
	// } // Silent fail if audio could not be initialized

	// 9. Systems Instantiation
	// SetComponent active systems to ECS world
	for _, name := range manifest.ActiveSystems() {
		factory, ok := registry.GetSystem(name)
		if !ok {
			panic(fmt.Sprintf("system not registered: %s", name))
		}
		sys := factory(world).(engine.System)
		world.AddSystem(sys)
	}

	// 10. Render Orchestrator
	// Resolve color mode for RenderConfig
	colorMode := term.ColorMode()
	ctx.World.Resource.Render = &engine.RenderConfig{ColorMode: colorMode}

	orchestrator := render.NewRenderOrchestrator(term, ctx.Width, ctx.Height)

	for _, name := range manifest.ActiveRenderers() {
		entry, ok := registry.GetRenderer(name)
		if !ok {
			panic(fmt.Sprintf("renderer not registered: %s", name))
		}
		renderer := entry.Factory(ctx).(render.SystemRenderer)
		orchestrator.Register(renderer, entry.Priority)
	}

	// 11. Input & Clock Scheduler
	// Create input handler
	inputMachine := input.NewMachine()
	router := mode.NewRouter(ctx, inputMachine)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	clockScheduler, gameUpdateDone, resetChan := engine.NewClockScheduler(
		world,
		ctx.PausableClock,
		&ctx.IsPaused,
		constant.GameUpdateInterval,
		frameReady,
	)

	// Wire reset channels to GameContext for MetaSystem access
	ctx.ResetChan = resetChan

	// 12. Engine (FSM) Setup
	// Initialize Event Registry for payload reflection
	event.InitRegistry()

	// Load FSM Config: external config with embedded fallback
	if err := clockScheduler.LoadFSMAuto(manifest.DefaultGameplayFSMConfig, manifest.RegisterFSMComponents); err != nil {
		panic(fmt.Sprintf("failed to load FSM: %v", err))
	}

	// 13. Event Handlers
	// Auto-register event handlers from World systems
	for _, sys := range world.Systems() {
		if handler, ok := sys.(event.Handler); ok {
			clockScheduler.RegisterEventHandler(handler)
		}
	}

	// Meta/Audio systems (not in World.Systems - event-only, no Update logic)
	metaSystem := system.NewMetaSystem(ctx)
	clockScheduler.RegisterEventHandler(metaSystem.(event.Handler))

	audioSystem := system.NewAudioSystem(world)
	clockScheduler.RegisterEventHandler(audioSystem.(event.Handler))

	// 14. Start Services
	if err := hub.StartAll(); err != nil {
		panic(fmt.Sprintf("service start failed: %v", err))
	}

	// 15. Start Game

	// Signal initial frame ready
	frameReady <- struct{}{}

	// Start game ticks
	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Cache FPS metric pointer for render loop
	statFPS := ctx.World.Resource.Status.Ints.Get("engine.fps")

	// FPS tracking
	var frameCount int64
	lastFPSUpdate := ctx.World.Resource.Time.RealTime

	// SetComponent frame rate
	frameTicker := time.NewTicker(constant.FrameUpdateInterval)
	defer frameTicker.Stop()

	eventChan := termSvc.Events()
	// Track last update state for rendering
	var updatePending bool

	// 16. Main Loop
	for {
		select {
		case ev := <-eventChan:
			// Input handling always works (even during pause)
			// Dumb pipe: Key Event → Machine → Intent → Router
			intent := inputMachine.Process(ev)

			if intent != nil {
				if !router.Handle(intent) {
					return // Exit game
				}
			}

			// Dispatch input events immediately, bypassing game tick wait
			clockScheduler.DispatchEventsImmediately()

			// Update orchestrator dimensions if screen resized
			if ev.Type == terminal.EventResize {
				ctx.Width = ev.Width
				ctx.Height = ev.Height
				ctx.HandleResize()
				orchestrator.Resize(ctx.Width, ctx.Height)
			}

		case <-frameTicker.C:
			// Increment frame number at the start of the frame cycle
			ctx.IncrementFrameNumber()

			// FPS calculation (once per second)
			frameCount++
			now := ctx.World.Resource.Time.RealTime
			if now.Sub(lastFPSUpdate) >= time.Second {
				statFPS.Store(frameCount)
				frameCount = 0
				lastFPSUpdate = now
			}

			// Snapshots for rendering (captured safely under lock)
			var (
				snapTimeRes engine.TimeResource
				snapCursorX int
				snapCursorY int
			)

			// Lock world to safely access/mutate shared TimeResource
			// Copy the values to stack variables for minimal lock duration and ensuring RenderContext is built with consistent state
			ctx.World.RunSafe(func() {
				// TimeResource updated by ClockScheduler; refresh frame number for render
				ctx.World.Resource.Time.FrameNumber = ctx.GetFrameNumber()

				// During pause: skip game updates but still render
				if ctx.IsPaused.Load() {
					// Update time for paused rendering
					ctx.World.Resource.Time.RealTime = ctx.PausableClock.RealTime()
				}

				// Snapshot TimeResource state by value for thread-safe reading
				snapTimeRes = *ctx.World.Resource.Time

				// Capture cursor position
				if pos, ok := ctx.World.Position.Get(ctx.CursorEntity); ok {
					snapCursorX = pos.X
					snapCursorY = pos.Y
				}
			})

			// Build RenderContext using the thread-safe snapshots
			// NewRenderContextFromGame expects a pointer, so we pass the address of our snapshot copy
			renderCtx := render.NewRenderContextFromGame(ctx, &snapTimeRes, snapCursorX, snapCursorY)

			if ctx.IsPaused.Load() {
				// Show pause overlay and maintains visual feedback
				orchestrator.RenderFrame(renderCtx, ctx.World)
				continue
			}

			// Check if game update completed
			select {
			case <-gameUpdateDone:
				// Update completed since last frame
				updatePending = false
			default:
				// No update or still in progress
				updatePending = true
			}

			// Render frame (all updates guaranteed complete), locks internally for component access
			orchestrator.RenderFrame(renderCtx, ctx.World)

			// Signal ready for next update (non-blocking)
			if !updatePending && !ctx.IsPaused.Load() {
				select {
				case frameReady <- struct{}{}:
				default:
					// Channel full, skip signal
				}
			}
		}
	}
}

// buildServiceArgs maps service names to their initialization args from flags
func buildServiceArgs() map[string][]any {
	args := make(map[string][]any)

	// Terminal: color mode
	if *flagColor256 {
		args["terminal"] = []any{terminal.ColorMode256}
	} else if *flagColorTrue {
		args["terminal"] = []any{terminal.ColorModeTrueColor}
	}
	// No flag = auto-detect (empty args)

	// Audio: mute state (default muted)
	if *flagAudioUnmute {
		args["audio"] = []any{false} // unmuted
	} else if *flagAudioMute {
		args["audio"] = []any{true} // muted (explicit)
	} else {
		args["audio"] = []any{true} // muted (default)
	}

	// Content: file pattern
	if *flagContentPath != "" {
		args["content"] = []any{*flagContentPath}
	}
	// No flag = default discovery (empty args)

	return args
}