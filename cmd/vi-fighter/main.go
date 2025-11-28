package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/modes"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/render/renderers"
	"github.com/lixenwraith/vi-fighter/systems"
)

func main() {
	// Parse command-line flags (keeping flag parsing infrastructure)
	flag.Parse()

	// Initialize screen
	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create screen: %v\n", err)
		os.Exit(1)
	}

	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize screen: %v\n", err)
		os.Exit(1)
	}
	defer screen.Fini()

	// Create game context with ECS world
	ctx := engine.NewGameContext(screen)

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
	energySystem := systems.NewEnergySystem(ctx)
	ctx.World.AddSystem(energySystem)

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

	// Wire up system references
	energySystem.SetGoldSystem(goldSystem)
	energySystem.SetSpawnSystem(spawnSystem)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create render orchestrator
	orchestrator := render.NewRenderOrchestrator(
		screen,
		ctx.Width,
		ctx.Height,
	)

	// Create and register renderers in priority order
	var decayTimeRemaining float64

	// Grid (100)
	pingGridRenderer := renderers.NewPingGridRenderer(ctx)
	orchestrator.Register(pingGridRenderer, render.PriorityGrid)

	// Entities (200)
	charactersRenderer := renderers.NewCharactersRenderer(ctx)
	orchestrator.Register(charactersRenderer, render.PriorityEntities)

	// Effects (300)
	shieldRenderer := renderers.NewShieldRenderer()
	orchestrator.Register(shieldRenderer, render.PriorityEffects)

	effectsRenderer := renderers.NewEffectsRenderer(ctx)
	orchestrator.Register(effectsRenderer, render.PriorityEffects)

	// Drain (350)
	drainRenderer := renderers.NewDrainRenderer()
	orchestrator.Register(drainRenderer, render.PriorityDrain)

	// UI (400)
	heatMeterRenderer := renderers.NewHeatMeterRenderer(ctx.State)
	orchestrator.Register(heatMeterRenderer, render.PriorityUI)

	lineNumbersRenderer := renderers.NewLineNumbersRenderer(ctx.LineNumWidth, ctx)
	orchestrator.Register(lineNumbersRenderer, render.PriorityUI)

	columnIndicatorsRenderer := renderers.NewColumnIndicatorsRenderer(ctx)
	orchestrator.Register(columnIndicatorsRenderer, render.PriorityUI)

	statusBarRenderer := renderers.NewStatusBarRenderer(ctx, &decayTimeRemaining)
	orchestrator.Register(statusBarRenderer, render.PriorityUI)

	cursorRenderer := renderers.NewCursorRenderer(ctx)
	orchestrator.Register(cursorRenderer, render.PriorityUI)

	// Overlay (500)
	overlayRenderer := renderers.NewOverlayRenderer(ctx)
	orchestrator.Register(overlayRenderer, render.PriorityOverlay)

	// Create input handler
	inputHandler := modes.NewInputHandler(ctx)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	// Clock scheduler handles systems phase transitions and triggers
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, constants.GameUpdateInterval, frameReady)

	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.SetSystems(goldSystem, decaySystem)
	clockScheduler.RegisterEventHandler(cleanerSystem)
	clockScheduler.RegisterEventHandler(energySystem)
	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Main game loop
	frameTicker := time.NewTicker(constants.FrameUpdateInterval)
	defer frameTicker.Stop()

	eventChan := make(chan tcell.Event, 100)
	go func() {
		for {
			eventChan <- screen.PollEvent()
		}
	}()

	// Track last update state for rendering
	var updatePending bool

	for {
		select {
		case ev := <-eventChan:
			// Update Input Resource from Context
			// This is a temporary bridge until InputHandler writes directly to Resources
			inputRes := &engine.InputResource{
				GameMode:    int(ctx.Mode),
				CommandText: ctx.CommandText,
				SearchText:  ctx.SearchText,
				IsPaused:    ctx.IsPaused.Load(),
			}
			engine.AddResource(ctx.World.Resources, inputRes)

			// Input handling always works (even during pause)
			// InputHandler will handle pause internally when entering or exiting COMMAND mode
			if !inputHandler.HandleEvent(ev) {
				return // Exit game
			}

			// Dispatch input events immediately, bypassing 50ms tick wait
			clockScheduler.DispatchEventsImmediately()

			// Update orchestrator dimensions if screen resized
			// This needs to work during pause for proper display
			if _, isResize := ev.(*tcell.EventResize); isResize {
				orchestrator.Resize(ctx.Width, ctx.Height)
			}

		case <-frameTicker.C:
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
				// This shows the pause overlay and maintains visual feedback
				// Update decay time for status bar renderer
				decayTimeRemaining = decaySystem.GetTimeUntilDecay(timeRes.GameTime)

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
			// Update decay time for status bar renderer
			decayTimeRemaining = decaySystem.GetTimeUntilDecay(timeRes.GameTime)

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