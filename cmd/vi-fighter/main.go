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
	energySystem.SetNuggetSystem(nuggetSystem)
	decaySystem.SetSpawnSystem(spawnSystem)
	decaySystem.SetNuggetSystem(nuggetSystem)
	drainSystem.SetNuggetSystem(nuggetSystem)

	// Create renderer (queries World directly for cleaner components)
	renderer := render.NewTerminalRenderer(
		screen,
		ctx.Width,
		ctx.Height,
		ctx.GameX,
		ctx.GameY,
		ctx.GameWidth,
		ctx.GameHeight,
		ctx.LineNumWidth,
	)

	// Create input handler
	inputHandler := modes.NewInputHandler(ctx, energySystem)
	inputHandler.SetNuggetSystem(nuggetSystem)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	// Clock scheduler handles systems phase transitions and triggers
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, constants.GameUpdateInterval, frameReady)

	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.SetSystems(goldSystem, decaySystem)
	clockScheduler.RegisterEventHandler(cleanerSystem)
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

			// Update renderer dimensions if screen resized
			// This needs to work during pause for proper display
			renderer.UpdateDimensions(
				ctx.Width,
				ctx.Height,
				ctx.GameX,
				ctx.GameY,
				ctx.GameWidth,
				ctx.GameHeight,
				ctx.LineNumWidth,
			)

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
				renderer.RenderFrame(ctx, decaySystem.IsAnimating(timeRes.GameTime), decaySystem.GetTimeUntilDecay(timeRes.GameTime))
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
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(timeRes.GameTime), decaySystem.GetTimeUntilDecay(timeRes.GameTime))

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