package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/audio"
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

	// Initialize sound manager
	soundManager := audio.NewSoundManager()
	if err := soundManager.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize audio: %v\n", err)
		// Continue without audio - non-fatal
	}
	ctx.SetSoundManager(soundManager)
	defer soundManager.Cleanup()

	// Create and add systems to the ECS world
	scoreSystem := systems.NewScoreSystem(ctx)
	ctx.World.AddSystem(scoreSystem)

	spawnSystem := systems.NewSpawnSystem(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)
	ctx.World.AddSystem(spawnSystem)

	trailSystem := systems.NewTrailSystem()
	ctx.World.AddSystem(trailSystem)

	decaySystem := systems.NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.ScoreIncrement, ctx)
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
			// Update all ECS systems
			dt := 16 * time.Millisecond
			ctx.World.Update(dt)

			// Update ping grid timer
			if ctx.PingGridTimer > 0 {
				ctx.PingGridTimer -= dt.Seconds()
				if ctx.PingGridTimer <= 0 {
					ctx.PingGridTimer = 0
					ctx.PingActive = false
				}
			}

			// Update decay system dimensions
			decaySystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.ScoreIncrement)

			// Render frame
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow(), decaySystem.GetTimeUntilDecay())
		}
	}
}
