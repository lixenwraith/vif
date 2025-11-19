package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
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

func main() {
	// Parse command-line flags
	debug := flag.Bool("debug", false, "Enable debug logging to file")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

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

	spawnSystem := systems.NewSpawnSystem(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY, ctx)
	ctx.World.AddSystem(spawnSystem)

	decaySystem := systems.NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx)
	ctx.World.AddSystem(decaySystem)

	goldSequenceSystem := systems.NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)
	ctx.World.AddSystem(goldSequenceSystem)

	// Initialize cleaner system with default configuration
	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerSystem := systems.NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)
	ctx.World.AddSystem(cleanerSystem)

	// Wire up system references
	scoreSystem.SetGoldSequenceSystem(goldSequenceSystem)
	scoreSystem.SetSpawnSystem(spawnSystem)
	decaySystem.SetSpawnSystem(spawnSystem)
	// Removed goldSequenceSystem.SetCleanerTrigger - now managed by ClockScheduler

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

	// Create and start clock scheduler (50ms tick for game logic)
	// Clock scheduler now handles Gold/Decay phase transitions
	// Clock scheduler now handles Cleaner triggers
	clockScheduler := engine.NewClockScheduler(ctx)
	clockScheduler.SetSystems(goldSequenceSystem, decaySystem, cleanerSystem)
	clockScheduler.Start()
	defer clockScheduler.Stop()

	// Main game loop
	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS (frame rate for rendering)
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
			// Check if boost should expire (atomic CAS pattern)
			ctx.UpdateBoostTimerAtomic()

			// Update all ECS systems
			dt := 16 * time.Millisecond
			ctx.World.Update(dt)

			// Wait for all updates to complete before rendering (frame barrier)
			// This ensures no entity modifications occur during rendering
			ctx.World.WaitForUpdates()

			// Update ping grid timer atomically (CAS pattern)
			if ctx.UpdatePingGridTimerAtomic(dt.Seconds()) {
				// Timer expired, deactivate ping
				ctx.SetPingActive(false)
			}

			// Update decay system dimensions
			decaySystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.Width, ctx.GetScoreIncrement())

			// Update gold sequence system dimensions and cursor position
			goldSequenceSystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)

			// Update cleaner system dimensions
			cleanerSystem.UpdateDimensions(ctx.GameWidth, ctx.GameHeight)

			// Render frame (all updates guaranteed complete)
			renderer.RenderFrame(ctx, decaySystem.IsAnimating(), decaySystem.CurrentRow(), decaySystem.GetTimeUntilDecay())
		}
	}
}
