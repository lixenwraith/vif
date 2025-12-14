package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/modes"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/render/renderers"
	"github.com/lixenwraith/vi-fighter/systems"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CLI flags
var colorModeFlag = flag.String("color", "auto", "Color mode: auto, truecolor, 256")

func main() {
	// Panic Recovery: Ensure terminal is reset even if the game crashes
	defer func() {
		if r := recover(); r != nil {
			// Restore terminal to sane state immediately
			terminal.EmergencyReset(os.Stdout)

			// Print error and stack trace to stderr so it's visible after reset
			fmt.Fprintf(os.Stderr, "\n\x1b[31mVI-FIGHTER CRASHED: %v\x1b[0m\n", r)
			fmt.Fprintf(os.Stderr, "Stack Trace:\n%s\n", debug.Stack())
			os.Exit(1)
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

	// Create game context with ECS world
	ctx := engine.NewGameContext(term)

	// Initialize Render Configuration
	renderConfig := &engine.RenderConfig{
		ColorMode:       uint8(colorMode), // 0=256, 1=TrueColor (matches constants in terminal package usually, but safe cast here)
		GrayoutDuration: 1 * time.Second,
		GrayoutMask:     render.MaskEntity,
		DimFactor:       0.5,
		DimMask:         render.MaskAll ^ render.MaskUI,
	}
	engine.AddResource(ctx.World.Resources, renderConfig)

	// Dependency Injection: Set safe crash handler for engine goroutines
	// This keeps engine package independent of terminal package
	ctx.SetCrashHandler(func(r any) {
		terminal.EmergencyReset(os.Stdout)
		// Use \r\n for raw mode compatibility to avoid zig-zag output
		fmt.Fprintf(os.Stderr, "\r\n\x1b[31mGAME CRASHED: %v\x1b[0m\r\n", r)
		fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())
		os.Exit(1)
	})

	// Initialize audio engine
	if audioEngine, err := audio.NewAudioEngine(); err == nil {
		if err := audioEngine.Start(); err == nil {
			ctx.AudioEngine = audioEngine
			defer audioEngine.Stop()
		} else {
			fmt.Printf("Audio start failed: %v (continuing without audio)\n", err)
		}
	} else {
		fmt.Printf("Audio initialization failed: %v (continuing without audio)\n", err)
	}

	// Create and add systems to the ECS world
	pingSystem := systems.NewPingSystem(ctx)
	ctx.World.AddSystem(pingSystem)

	energySystem := systems.NewEnergySystem(ctx)
	ctx.World.AddSystem(energySystem)

	shieldSystem := systems.NewShieldSystem(ctx)
	ctx.World.AddSystem(shieldSystem)

	heatSystem := systems.NewHeatSystem(ctx)
	ctx.World.AddSystem(heatSystem)

	spawnSystem := systems.NewSpawnSystem(ctx)
	ctx.World.AddSystem(spawnSystem)

	boostSystem := systems.NewBoostSystem(ctx)
	ctx.World.AddSystem(boostSystem)

	nuggetSystem := systems.NewNuggetSystem(ctx)
	ctx.World.AddSystem(nuggetSystem)

	decaySystem := systems.NewDecaySystem(ctx)
	ctx.World.AddSystem(decaySystem)

	goldSystem := systems.NewGoldSystem(ctx)
	ctx.World.AddSystem(goldSystem)

	drainSystem := systems.NewDrainSystem(ctx)
	ctx.World.AddSystem(drainSystem)

	cleanerSystem := systems.NewCleanerSystem(ctx)
	ctx.World.AddSystem(cleanerSystem)

	flashSystem := systems.NewFlashSystem(ctx)
	ctx.World.AddSystem(flashSystem)

	splashSystem := systems.NewSplashSystem(ctx)
	ctx.World.AddSystem(splashSystem)

	timeKeeperSystem := systems.NewTimeKeeperSystem(ctx)
	ctx.World.AddSystem(timeKeeperSystem)

	cullSystem := systems.NewCullSystem()
	ctx.World.AddSystem(cullSystem)

	// Mostly event-driven, added to ECS for consistency, not adding to World.systems list since it doesn't have any Update(dt) logic
	commandSystem := systems.NewCommandSystem(ctx)

	// Create render orchestrator
	orchestrator := render.NewRenderOrchestrator(
		term,
		ctx.Width,
		ctx.Height,
	)

	// Create and register renderers in priority order

	// Standardized initialization loop
	type rendererDef struct {
		factory  func(*engine.GameContext) render.SystemRenderer
		priority render.RenderPriority
	}

	rendererList := []rendererDef{
		// Grid (100)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewPingRenderer(c) }, render.PriorityGrid},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewSplashRenderer(c) }, render.PrioritySplash},
		// Entities (200)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewCharactersRenderer(c) }, render.PriorityEntities},
		// Effects (300)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewShieldRenderer(c) }, render.PriorityEffects},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewEffectsRenderer(c) }, render.PriorityEffects},
		// Drain (350)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewDrainRenderer(c) }, render.PriorityDrain},
		// Post-Processing (390-395)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewGrayoutRenderer(c) }, render.PriorityUI - 10},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewDimRenderer(c) }, render.PriorityUI - 5},
		// UI (400)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewHeatMeterRenderer(c) }, render.PriorityUI},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewLineNumbersRenderer(c) }, render.PriorityUI},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewColumnIndicatorsRenderer(c) }, render.PriorityUI},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewStatusBarRenderer(c) }, render.PriorityUI},
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewCursorRenderer(c) }, render.PriorityUI},
		// Overlay (500)
		{func(c *engine.GameContext) render.SystemRenderer { return renderers.NewOverlayRenderer(c) }, render.PriorityOverlay},
	}

	for _, def := range rendererList {
		orchestrator.Register(def.factory(ctx), def.priority)
	}

	// Create input handler
	inputMachine := input.NewMachine()
	router := modes.NewRouter(ctx, inputMachine)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	// Clock scheduler handles systems phase transitions and triggers
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, constants.GameUpdateInterval, frameReady)

	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.SetSystems(goldSystem, decaySystem)
	clockScheduler.RegisterEventHandler(pingSystem)
	clockScheduler.RegisterEventHandler(shieldSystem)
	clockScheduler.RegisterEventHandler(heatSystem)
	clockScheduler.RegisterEventHandler(energySystem)
	clockScheduler.RegisterEventHandler(boostSystem)
	clockScheduler.RegisterEventHandler(spawnSystem)
	clockScheduler.RegisterEventHandler(nuggetSystem)
	clockScheduler.RegisterEventHandler(goldSystem)
	clockScheduler.RegisterEventHandler(cleanerSystem)
	clockScheduler.RegisterEventHandler(splashSystem)
	clockScheduler.RegisterEventHandler(timeKeeperSystem)
	clockScheduler.RegisterEventHandler(commandSystem)
	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Main game loop
	frameTicker := time.NewTicker(constants.FrameUpdateInterval)
	defer frameTicker.Stop()

	eventChan := make(chan terminal.Event, 256)
	// Input polling uses raw goroutine as it interacts directly with terminal
	go func() {
		// Panic recovery for input polling goroutine to ensure terminal cleanup
		defer func() {
			if r := recover(); r != nil {
				terminal.EmergencyReset(os.Stdout)
				fmt.Fprintf(os.Stderr, "\r\n\x1b[31mEVENT POLLER CRASHED: %v\x1b[0m\r\n", r)
				fmt.Fprintf(os.Stderr, "Stack Trace:\r\n%s\r\n", debug.Stack())
				os.Exit(1)
			}
		}()

		for {
			ev := term.PollEvent()
			// Clean exit on terminal closure or error
			if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
				return
			}
			eventChan <- ev
		}
	}()

	// Track last update state for rendering
	var updatePending bool

	for {
		select {
		case ev := <-eventChan:
			// Input handling always works (even during pause)
			// Dumb pipe: Event → Machine → Intent → Router
			intent := inputMachine.Process(ev)

			if intent != nil {
				if !router.Handle(intent) {
					return // Exit game
				}
			}

			// Update Input Resource from Context AFTER handling input
			// This ensures renderers see the mode change in the same frame it happened
			uiSnapshot := ctx.GetUISnapshot()
			inputRes := &engine.InputResource{
				GameMode:       int(ctx.GetMode()),
				CommandText:    uiSnapshot.CommandText,
				SearchText:     uiSnapshot.SearchText,
				PendingCommand: inputMachine.GetPendingCommand(),
				IsPaused:       ctx.IsPaused.Load(),
			}
			engine.AddResource(ctx.World.Resources, inputRes)

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

			// Update time resource based on context pausable clock
			timeRes := &engine.TimeResource{
				GameTime:    ctx.PausableClock.Now(),
				RealTime:    ctx.GetRealTime(),
				DeltaTime:   constants.FrameUpdateInterval,
				FrameNumber: ctx.GetFrameNumber(),
			}
			engine.AddResource(ctx.World.Resources, timeRes)

			// During pause: skip game updates but still render
			if ctx.IsPaused.Load() {
				// Show pause overlay and maintains visual feedback
				cursorPos, _ := ctx.World.Positions.Get(ctx.CursorEntity)
				renderCtx := render.NewRenderContextFromGame(ctx, timeRes, cursorPos.X, cursorPos.Y)
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

			// Render frame (all updates guaranteed complete)

			cursorPos, _ := ctx.World.Positions.Get(ctx.CursorEntity)
			renderCtx := render.NewRenderContextFromGame(ctx, timeRes, cursorPos.X, cursorPos.Y)
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