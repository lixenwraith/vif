package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/modes"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/systems"
)

func main() {
	rand.Seed(time.Now().UnixNano())

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

	// Create and add systems to the ECS world
	scoreSystem := systems.NewScoreSystem(ctx)
	ctx.World.AddSystem(scoreSystem)

	spawnSystem := systems.NewSpawnSystem(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY, ctx)
	ctx.World.AddSystem(spawnSystem)

	decaySystem := systems.NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.GetScoreIncrement(), ctx)
	ctx.World.AddSystem(decaySystem)

	goldSequenceSystem := systems.NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)
	ctx.World.AddSystem(goldSequenceSystem)

	// Wire up the gold sequence system reference in score system
	scoreSystem.SetGoldSequenceSystem(goldSequenceSystem)

	// Create renderer
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
	inputHandler := modes.NewInputHandler(ctx, scoreSystem)

	// Main game loop
	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	eventChan := make(chan tcell.Event, 100)
	go func() {
		for {
			eventChan <- screen.PollEvent()
		}
	}()

	for {
		select {
		case ev := <-eventChan:
			if !inputHandler.HandleEvent(ev) {
				return // Exit game
			}

			// Update spawn system cursor position
			spawnSystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)

			// Update renderer dimensions if screen resized
			renderer.UpdateDimensions(
				ctx.Width,
				ctx.Height,
				ctx.GameX,
				ctx.GameY,
				ctx.GameWidth,
				ctx.GameHeight,
				ctx.LineNumWidth,
			)

		case <-ticker.C:
			// Check if boost should expire
			if ctx.GetBoostEnabled() && ctx.TimeProvider.Now().After(ctx.GetBoostEndTime()) {
				ctx.SetBoostEnabled(false)
			}

			// Update all ECS systems
			dt := 16 * time.Millisecond
			ctx.World.Update(dt)

			// Update ping grid timer
			pingTimer := ctx.GetPingGridTimer()
			if pingTimer > 0 {
				newTimer := pingTimer - dt.Seconds()
				if newTimer <= 0 {
					ctx.SetPingGridTimer(0)
					ctx.SetPingActive(false)
				} else {
					ctx.SetPingGridTimer(newTimer)
				}
			}

			// Update decay system dimensions
			decaySystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.GetScoreIncrement())

			// Update gold sequence system dimensions
			goldSequenceSystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight)

			// Render frame
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow(), decaySystem.GetTimeUntilDecay())
		}
	}
}
