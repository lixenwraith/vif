package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/modes"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/systems"
)

const (
	logDir      = "logs"
	logFileName = "vi-fighter.log"
	maxLogSize  = 10 * 1024 * 1024 // 10MB
)

// Fixed timesteps for game updates
const (
	frameUpdateDT = 16 * time.Millisecond // ~60 FPS (frame rate for rendering)
	gameUpdateDT  = 50 * time.Millisecond // game logic tick
)

// setupLogging configures log output based on debug flag
// If debug is true, logs go to file; otherwise, logging is disabled
// Returns the log file handle (or nil) that should be closed when done
func setupLogging(debug bool) *os.File {
	if !debug {
		// Disable all logging by redirecting to io.Discard
		log.SetOutput(io.Discard)
		return nil
	}

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// Can't log yet, so write to stderr
		fmt.Fprintf(os.Stderr, "Warning: failed to create logs directory: %v\n", err)
		log.SetOutput(io.Discard)
		return nil
	}

	logPath := filepath.Join(logDir, logFileName)

	// Check if log file needs rotation (>10MB)
	if info, err := os.Stat(logPath); err == nil {
		if info.Size() > maxLogSize {
			// Rotate the log file by renaming it with timestamp
			timestamp := time.Now().Format("2006-01-02-15-04-05")
			rotatedName := filepath.Join(logDir, fmt.Sprintf("vi-fighter-%s.log", timestamp))
			if err := os.Rename(logPath, rotatedName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to rotate log file: %v\n", err)
			}
		}
	}

	// Open log file in append mode
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to open log file: %v\n", err)
		log.SetOutput(io.Discard)
		return nil
	}

	// Redirect all log output to the file
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Log startup
	log.Printf("=== Vi-Fighter started ===")

	return logFile
}

// updateUIElements handles UI updates that need real-time clock (work during pause)
// This includes cursor blinking, error state timeouts, and other visual feedback
func updateUIElements(ctx *engine.GameContext) {
	// Use real time for UI elements (unaffected by pause)
	realNow := ctx.GetRealTime()

	// Update cursor blink
	if realNow.Sub(ctx.CursorBlinkTime) > 500*time.Millisecond {
		ctx.CursorVisible = !ctx.CursorVisible
		ctx.CursorBlinkTime = realNow
	}

	// Clear expired cursor error state using real time
	if ctx.State.GetCursorError() {
		errorTime := ctx.State.GetCursorErrorTime()
		if !errorTime.IsZero() && realNow.Sub(errorTime) > 200*time.Millisecond {
			ctx.State.SetCursorError(false)
		}
	}

	// Update any other UI-specific timers that need real time
	// (e.g., notification fadeouts, temporary UI highlights)
}

func main() {
	// Parse command-line flags
	debug := flag.Bool("debug", false, "Enable debug logging to file")
	flag.Parse()

	// Setup logging before any other operations
	// This ensures no log output goes to stdout/stderr during gameplay
	logFile := setupLogging(*debug)
	if logFile != nil {
		defer logFile.Close()
	}

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

	// Initialize cleaner system with default configuration
	cleanerConfig := constants.DefaultCleanerConfig()
	// Initialize cleaner gradient based on configuration
	render.BuildCleanerGradient(cleanerConfig.TrailLength, tcell.NewRGBColor(255, 255, 0))

	cleanerSystem := systems.NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)
	ctx.World.AddSystem(cleanerSystem)

	// Wire up system references
	scoreSystem.SetGoldSystem(goldSystem)
	scoreSystem.SetSpawnSystem(spawnSystem)
	scoreSystem.SetNuggetSystem(nuggetSystem)
	decaySystem.SetSpawnSystem(spawnSystem)
	decaySystem.SetNuggetSystem(nuggetSystem)
	drainSystem.SetNuggetSystem(nuggetSystem)

	// Create renderer (with CleanerSystem for thread-safe cleaner rendering)
	renderer := render.NewTerminalRenderer(
		screen,
		ctx.Width,
		ctx.Height,
		ctx.GameX,
		ctx.GameY,
		ctx.GameWidth,
		ctx.GameHeight,
		ctx.LineNumWidth,
		cleanerSystem,
	)

	// Create input handler
	inputHandler := modes.NewInputHandler(ctx, scoreSystem)
	inputHandler.SetNuggetSystem(nuggetSystem)

	// Create frame synchronization channel
	frameReady := make(chan struct{}, 1)

	// Create clock scheduler with frame synchronization
	// Clock scheduler handles systems phase transitions and triggers
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, gameUpdateDT, frameReady)

	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.SetSystems(goldSystem, decaySystem, cleanerSystem)
	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Main game loop
	frameTicker := time.NewTicker(frameUpdateDT)
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
			// Always update UI elements (use real time, works during pause)
			updateUIElements(ctx)

			// During pause: skip game updates but still render
			if ctx.IsPaused.Load() {
				// This shows the pause overlay and maintains visual feedback
				renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow(), decaySystem.GetTimeUntilDecay())
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
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow(), decaySystem.GetTimeUntilDecay())

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