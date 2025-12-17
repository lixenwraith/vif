package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/registry"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/manifest"
	"github.com/lixenwraith/vi-fighter/modes"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/systems"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CLI flags
var colorModeFlag = flag.String("color", "auto", "Color mode: auto, truecolor, 256")

func main() {
	// Panic Recovery: Ensure terminal is reset even if the game crashes
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r)
		}
	}()

	// Parse command-line flags (keeping flag parsing infrastructure)
	flag.Parse()

	// Resolve color mode from flag
	var colorMode terminal.ColorMode
	switch *colorModeFlag {
	case "256":
		colorMode = terminal.ColorMode256
	case "truecolor", "true", "24bit":
		colorMode = terminal.ColorModeTrueColor
	default:
		colorMode = terminal.DetectColorMode()
	}

	// Initialize terminal
	term := terminal.New(colorMode)
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize terminal: %v\n", err)
		os.Exit(1)
	}
	// Normal exit terminal cleanup
	defer term.Fini()

	// === PHASE 1: Registry Setup ===
	manifest.RegisterSystems()
	manifest.RegisterRenderers()

	// === PHASE 2: World & Component Registration ===
	world := engine.NewWorld()
	manifest.RegisterComponents(world)

	// === PHASE 3: GameContext Creation ===
	ctx := engine.NewGameContext(term, world)

	// === PHASE 4: Core Resources ===
	// Initialize singleton TimeResource (updated in-place by ClockScheduler)
	timeRes := &engine.TimeResource{
		GameTime:    ctx.PausableClock.Now(),
		RealTime:    ctx.GetRealTime(),
		DeltaTime:   constants.GameUpdateInterval,
		FrameNumber: 0,
	}
	engine.AddResource(ctx.World.Resources, timeRes)

	// Initialize singleton InputResource (updated in-place during input handling)
	inputRes := &engine.InputResource{
		GameMode:       int(ctx.GetMode()),
		CommandText:    "",
		SearchText:     "",
		PendingCommand: "",
		IsPaused:       ctx.IsPaused.Load(),
	}
	engine.AddResource(ctx.World.Resources, inputRes)

	// Initialize Render Configuration
	renderConfig := &engine.RenderConfig{
		ColorMode:       uint8(colorMode), // 0=256, 1=TrueColor (matches constants in terminal package usually, but safe cast here)
		GrayoutDuration: 1 * time.Second,
		GrayoutMask:     render.MaskEntity,
		DimFactor:       0.5,
		DimMask:         render.MaskAll ^ render.MaskUI,
	}
	engine.AddResource(ctx.World.Resources, renderConfig)

	// Z-Index Resolver (after components registered)
	zIndexResolver := engine.NewZIndexResolver(world)
	engine.AddResource(ctx.World.Resources, zIndexResolver)

	// === PHASE 5: Audio Engine ===
	// Initialize audio engine
	if audioEngine, err := audio.NewAudioEngine(); err == nil {
		if err := audioEngine.Start(); err == nil {
			ctx.SetAudioEngine(audioEngine)
			defer audioEngine.Stop()
		}
	} // Silent fail if audio could not be initialized

	// === PHASE 6: Systems Instantiation ===
	// Add active systems to ECS world
	for _, name := range manifest.ActiveSystems() {
		factory, ok := registry.GetSystem(name)
		if !ok {
			panic(fmt.Sprintf("system not registered: %s", name))
		}
		sys := factory(world).(engine.System)
		world.AddSystem(sys)
	}

	// === PHASE 7: Render Orchestrator & Renderers ===
	// Create render orchestrator
	orchestrator := render.NewRenderOrchestrator(
		term,
		ctx.Width,
		ctx.Height,
	)

	// Add active renderers to render orchestrator
	for _, name := range manifest.ActiveRenderers() {
		entry, ok := registry.GetRenderer(name)
		if !ok {
			panic(fmt.Sprintf("renderer not registered: %s", name))
		}
		renderer := entry.Factory(ctx).(render.SystemRenderer)
		orchestrator.Register(renderer, entry.Priority)
	}

	// // Event-driven system, not added to World.systems list since no Update(dt) logic
	// audioSystem := systems.NewAudioSystem(ctx.World)
	// // No factor for command system
	//
	// commandSystem := systems.NewCommandSystem(ctx)
	//

	// === PHASE 8: Input & Clock Scheduler ===
	// Create input handler
	inputMachine := input.NewMachine()
	router := modes.NewRouter(ctx, inputMachine)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, constants.GameUpdateInterval, frameReady)

	// === Phase 9: FSM Setup ===
	// Initialize Event Registry first (for payload reflection)
	events.InitRegistry()

	// Load FSM Config
	// For now we use the default JSON embedded in manifest
	if err := clockScheduler.LoadFSM(manifest.DefaultGameplayFSMConfig, manifest.RegisterFSMComponents); err != nil {
		panic(fmt.Sprintf("failed to load FSM: %v", err))
	}

	// === Phase 10: Event Handlers ===
	// Auto-register event handlers from World systems
	for _, sys := range world.Systems() {
		if handler, ok := sys.(events.Handler[*engine.World]); ok {
			clockScheduler.RegisterEventHandler(handler)
		}
	}
	clockScheduler.RegisterEventHandler(clockScheduler)

	// Meta/Audio systems (not in World.Systems - event-only, no Update logic)
	metaSystem := systems.NewMetaSystem(ctx)
	clockScheduler.RegisterEventHandler(metaSystem.(events.Handler[*engine.World]))

	audioSystem := systems.NewAudioSystem(world)
	clockScheduler.RegisterEventHandler(audioSystem.(events.Handler[*engine.World]))

	// === PHASE 10: Main Loop ===
	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Cache FPS metric pointer for render loop
	statusReg := engine.MustGetResource[*status.Registry](ctx.World.Resources)
	statFPS := statusReg.Ints.Get("engine.fps")

	// FPS tracking
	var frameCount int64
	var lastFPSUpdate time.Time = time.Now()

	// Set frame rate
	frameTicker := time.NewTicker(constants.FrameUpdateInterval)
	defer frameTicker.Stop()

	eventChan := make(chan terminal.Event, 256)
	// Input polling uses raw goroutine as it interacts directly with terminal
	core.Go(func() {
		for {
			ev := term.PollEvent()
			// Clean exit on terminal closure or error
			if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
				return
			}
			eventChan <- ev
		}
	})

	// Track last update state for rendering
	var updatePending bool

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

			// Update Input Resource from Context AFTER handling input
			// This ensures renderers see the mode change in the same frame it happened
			uiSnapshot := ctx.GetUISnapshot()

			// Lock world to prevent race with Systems reading InputResource in Scheduler
			ctx.World.RunSafe(func() {
				inputRes.Update(
					int(ctx.GetMode()),
					uiSnapshot.CommandText,
					uiSnapshot.SearchText,
					inputMachine.GetPendingCommand(),
					ctx.IsPaused.Load(),
				)
			})

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
			now := time.Now()
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
				timeRes.FrameNumber = ctx.GetFrameNumber()

				// During pause: skip game updates but still render
				if ctx.IsPaused.Load() {
					// Update time for paused rendering
					timeRes.RealTime = ctx.GetRealTime()
				}

				// Copy TimeResource state by value for thread-safe reading
				snapTimeRes = *timeRes

				// Capture cursor position
				if pos, ok := ctx.World.Positions.Get(ctx.CursorEntity); ok {
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