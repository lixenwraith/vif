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
	spawnSystem := systems.NewSpawnSystem(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)
	ctx.World.AddSystem(spawnSystem)

	trailSystem := systems.NewTrailSystem()
	ctx.World.AddSystem(trailSystem)

	decaySystem := systems.NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.ScoreIncrement)
	ctx.World.AddSystem(decaySystem)

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
	inputHandler := modes.NewInputHandler(ctx)

	// Start decay ticker
	ctx.DecayTicker = time.AfterFunc(60*time.Second, func() {
		ctx.DecayAnimating = true
		ctx.DecayRow = 0
		ctx.DecayStartTime = time.Now()
	})

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
			// Update all ECS systems
			dt := 16 * time.Millisecond
			ctx.World.Update(dt)

			// Update decay system dimensions
			decaySystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.ScoreIncrement)

			// Render frame
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow())
		}
	}
}
