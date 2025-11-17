package systems

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestBoostRapidToggle tests rapidly activating and deactivating boost
// This simulates the race condition from hitting blue characters quickly
func TestBoostRapidToggle(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	var wg sync.WaitGroup

	// Goroutine 1: Rapidly activate boost by typing blue characters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			// Create blue character
			entity := ctx.World.CreateEntity()
			pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
			char := components.CharacterComponent{Rune: rune('a' + (i % 26)), Style: tcell.StyleDefault}
			seq := components.SequenceComponent{
				ID:    i,
				Index: 0,
				Type:  components.SequenceBlue,
				Level: components.LevelNormal,
			}

			ctx.World.AddComponent(entity, pos)
			ctx.World.AddComponent(entity, char)
			ctx.World.AddComponent(entity, seq)
			ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

			// Type the character to trigger boost extension
			scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+(i%26)))
			time.Sleep(1 * time.Millisecond) // Small delay
		}
	}()

	// Goroutine 2: Simulate rendering loop reading boost state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			// Simulate what the renderer does
			boostEnabled := ctx.GetBoostEnabled()
			if boostEnabled {
				endTime := ctx.GetBoostEndTime()
				remaining := endTime.Sub(time.Now()).Seconds()
				_ = remaining // Use the value
			}
			time.Sleep(100 * time.Microsecond) // Fast render loop
		}
	}()

	// Goroutine 3: Simulate main loop checking boost expiration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			// Check if boost should expire
			if ctx.GetBoostEnabled() && time.Now().After(ctx.GetBoostEndTime()) {
				ctx.SetBoostEnabled(false)
			}
			time.Sleep(1 * time.Millisecond) // Main loop tick
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestBoostConcurrentRead tests concurrent reads of boost state
func TestBoostConcurrentRead(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Activate boost
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
	ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	var wg sync.WaitGroup

	// Spawn 10 goroutines to concurrently read boost state
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				enabled := ctx.GetBoostEnabled()
				endTime := ctx.GetBoostEndTime()
				scoreInc := ctx.GetScoreIncrement()
				score := ctx.GetScore()

				// Use the values to avoid optimization
				_ = enabled
				_ = endTime
				_ = scoreInc
				_ = score
			}
		}(i)
	}

	wg.Wait()

	// Test passes if no race is detected
}

// TestBoostExpirationRace tests the race between boost expiration and extension
func TestBoostExpirationRace(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set boost to expire soon
	ctx.SetBoostEnabled(true)
	ctx.SetBoostEndTime(time.Now().Add(10 * time.Millisecond))

	var wg sync.WaitGroup

	// Goroutine 1: Try to extend boost
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // Let it almost expire

		// Create blue character
		entity := ctx.World.CreateEntity()
		pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
		char := components.CharacterComponent{Rune: 'b', Style: tcell.StyleDefault}
		seq := components.SequenceComponent{
			ID:    1,
			Index: 0,
			Type:  components.SequenceBlue,
			Level: components.LevelNormal,
		}

		ctx.World.AddComponent(entity, pos)
		ctx.World.AddComponent(entity, char)
		ctx.World.AddComponent(entity, seq)
		ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

		// Type to extend boost
		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')
	}()

	// Goroutine 2: Check for expiration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if ctx.GetBoostEnabled() && time.Now().After(ctx.GetBoostEndTime()) {
				ctx.SetBoostEnabled(false)
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 3: Read boost state (renderer)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			enabled := ctx.GetBoostEnabled()
			if enabled {
				endTime := ctx.GetBoostEndTime()
				_ = endTime.Sub(time.Now()).Seconds()
			}
			time.Sleep(500 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestBoostWithScoreUpdates tests boost concurrent with score modifications
func TestBoostWithScoreUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Activate boost
	ctx.SetBoostEnabled(true)
	ctx.SetBoostEndTime(time.Now().Add(100 * time.Millisecond))

	var wg sync.WaitGroup

	// Goroutine 1: Type green characters to increase score/heat
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			entity := ctx.World.CreateEntity()
			pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
			char := components.CharacterComponent{Rune: rune('a' + (i % 26)), Style: tcell.StyleDefault}
			seq := components.SequenceComponent{
				ID:    i,
				Index: 0,
				Type:  components.SequenceGreen,
				Level: components.LevelNormal,
			}

			ctx.World.AddComponent(entity, pos)
			ctx.World.AddComponent(entity, char)
			ctx.World.AddComponent(entity, seq)
			ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

			scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+(i%26)))
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Read score and boost state (renderer)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			score := ctx.GetScore()
			scoreInc := ctx.GetScoreIncrement()
			boostEnabled := ctx.GetBoostEnabled()

			// Use values
			_ = score
			_ = scoreInc
			_ = boostEnabled

			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Goroutine 3: Check boost expiration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			if ctx.GetBoostEnabled() && time.Now().After(ctx.GetBoostEndTime()) {
				ctx.SetBoostEnabled(false)
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestSimulateFullGameLoop simulates a realistic game loop with boost
func TestSimulateFullGameLoop(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	var wg sync.WaitGroup

	// Simulate game loop (ticker)
	stopCh := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check boost expiration (as in main.go)
				if ctx.GetBoostEnabled() && time.Now().After(ctx.GetBoostEndTime()) {
					ctx.SetBoostEnabled(false)
				}

				// Update ping timer
				pingTimer := ctx.GetPingGridTimer()
				if pingTimer > 0 {
					newTimer := pingTimer - 0.016 // 16ms in seconds
					if newTimer <= 0 {
						ctx.SetPingGridTimer(0)
						ctx.SetPingActive(false)
					} else {
						ctx.SetPingGridTimer(newTimer)
					}
				}

				// Simulate rendering
				_ = ctx.GetScore()
				_ = ctx.GetScoreIncrement()
				_ = ctx.GetBoostEnabled()
				if ctx.GetBoostEnabled() {
					_ = ctx.GetBoostEndTime().Sub(time.Now()).Seconds()
				}

			case <-stopCh:
				return
			}
		}
	}()

	// Simulate user typing blue and green characters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			// Alternate between blue and green characters
			charType := components.SequenceGreen
			if i%5 == 0 {
				charType = components.SequenceBlue
			}

			entity := ctx.World.CreateEntity()
			pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
			char := components.CharacterComponent{Rune: rune('a' + (i % 26)), Style: tcell.StyleDefault}
			seq := components.SequenceComponent{
				ID:    i,
				Index: 0,
				Type:  charType,
				Level: components.LevelNormal,
			}

			ctx.World.AddComponent(entity, pos)
			ctx.World.AddComponent(entity, char)
			ctx.World.AddComponent(entity, seq)
			ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

			scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+(i%26)))
			time.Sleep(10 * time.Millisecond) // Simulate typing speed
		}

		// Let the game loop run a bit more after typing stops
		time.Sleep(100 * time.Millisecond)
		close(stopCh)
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestAllAtomicStateAccess tests all atomic state fields concurrently
func TestAllAtomicStateAccess(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup

	// Goroutine 1: Write to all atomic fields
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ctx.SetScore(i)
			ctx.SetScoreIncrement(i % 50)
			ctx.SetBoostEnabled(i%2 == 0)
			ctx.SetBoostEndTime(time.Now().Add(time.Duration(i) * time.Millisecond))
			ctx.SetCursorError(i%3 == 0)
			ctx.SetCursorErrorTime(time.Now())
			ctx.SetScoreBlinkActive(i%4 == 0)
			ctx.SetScoreBlinkColor(tcell.Color(i % 256))
			ctx.SetScoreBlinkTime(time.Now())
			ctx.SetPingActive(i%5 == 0)
			ctx.SetPingGridTimer(float64(i) / 10.0)
			time.Sleep(100 * time.Microsecond)
		}
	}()

	// Goroutine 2: Read from all atomic fields
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = ctx.GetScore()
			_ = ctx.GetScoreIncrement()
			_ = ctx.GetBoostEnabled()
			_ = ctx.GetBoostEndTime()
			_ = ctx.GetCursorError()
			_ = ctx.GetCursorErrorTime()
			_ = ctx.GetScoreBlinkActive()
			_ = ctx.GetScoreBlinkColor()
			_ = ctx.GetScoreBlinkTime()
			_ = ctx.GetPingActive()
			_ = ctx.GetPingGridTimer()
			time.Sleep(50 * time.Microsecond)
		}
	}()

	// Goroutine 3: Mixed read/write operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			if ctx.GetScore() > 50 {
				ctx.SetScore(0)
			}
			if ctx.GetBoostEnabled() {
				_ = ctx.GetBoostEndTime()
			}
			if ctx.GetPingActive() {
				timer := ctx.GetPingGridTimer()
				if timer > 0 {
					ctx.SetPingGridTimer(timer - 0.1)
				}
			}
			time.Sleep(75 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}
