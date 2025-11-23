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
	"github.com/lixenwraith/vi-fighter/audio"
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
// This includes purely visual UI elements that should animate while paused.
// Note: Game state feedback (ScoreBlink, CursorError) is handled in ScoreSystem via Game Time.
func updateUIElements(ctx *engine.GameContext) {
	// Currently empty as specific game state feedback (blinks/errors)
	// must freeze during pause and are handled by the ScoreSystem.
	// Future UI-only animations (like a "PAUSED" text blinker) would go here.
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

	// Initialize audio engine (graceful failure)
	audioCfg := audio.LoadAudioConfig()
	if audioEngine, err := audio.NewAudioEngine(audioCfg); err == nil {
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

	cleanerSystem := systems.NewCleanerSystem(ctx)
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
	clockScheduler, gameUpdateDone := engine.NewClockScheduler(ctx, constants.GameUpdateInterval, frameReady)

	// Signal initial frame ready
	frameReady <- struct{}{}

	clockScheduler.SetSystems(goldSystem, decaySystem, cleanerSystem)
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
