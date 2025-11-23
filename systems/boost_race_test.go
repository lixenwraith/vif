package systems

// Race condition tests for boost/heat system (toggle, expiration, timer updates).
// See also:
//   - race_condition_comprehensive_test.go: Spawn system and content management race conditions
//   - cleaner_race_test.go: Cleaner system race conditions

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
	
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()

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
			boostEnabled := ctx.State.GetBoostEnabled()
			if boostEnabled {
				endTime := ctx.State.GetBoostEndTime()
				remaining := endTime.Sub(time.Now()).Seconds()
				_ = remaining // Use the value
			}
			time.Sleep(100 * time.Microsecond) // Fast render loop
		}
	}()

	// Goroutine 3: Simulate main loop checking boost expiration (using atomic CAS)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			// Check if boost should expire atomically
			ctx.State.UpdateBoostTimerAtomic()
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

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	var wg sync.WaitGroup

	// Spawn 10 goroutines to concurrently read boost state
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				enabled := ctx.State.GetBoostEnabled()
				endTime := ctx.State.GetBoostEndTime()
				scoreInc := ctx.State.GetHeat()
				score := ctx.State.GetScore()

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
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostEndTime(time.Now().Add(10 * time.Millisecond))

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

		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()

		// Type to extend boost
		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')
	}()

	// Goroutine 2: Check for expiration (using atomic CAS)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			ctx.State.UpdateBoostTimerAtomic()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 3: Read boost state (renderer)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			enabled := ctx.State.GetBoostEnabled()
			if enabled {
				endTime := ctx.State.GetBoostEndTime()
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
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostEndTime(time.Now().Add(100 * time.Millisecond))

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
	
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()

			scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+(i%26)))
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Read score and boost state (renderer)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			score := ctx.State.GetScore()
			scoreInc := ctx.State.GetHeat()
			boostEnabled := ctx.State.GetBoostEnabled()

			// Use values
			_ = score
			_ = scoreInc
			_ = boostEnabled

			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Goroutine 3: Check boost expiration (using atomic CAS)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			ctx.State.UpdateBoostTimerAtomic()
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
				// Check boost expiration (atomic CAS pattern as in main.go)
				ctx.State.UpdateBoostTimerAtomic()

				// Update ping timer atomically (CAS pattern as in main.go)
				if ctx.UpdatePingGridTimerAtomic(0.016) {
					// Timer expired, deactivate ping
					ctx.SetPingActive(false)
				}

				// Simulate rendering
				_ = ctx.State.GetScore()
				_ = ctx.State.GetHeat()
				_ = ctx.State.GetBoostEnabled()
				if ctx.State.GetBoostEnabled() {
					_ = ctx.State.GetBoostEndTime().Sub(time.Now()).Seconds()
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
	
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()

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
			ctx.State.SetScore(i)
			ctx.State.SetHeat(i % 50)
			ctx.State.SetBoostEnabled(i%2 == 0)
			ctx.State.SetBoostEndTime(time.Now().Add(time.Duration(i) * time.Millisecond))
			ctx.State.SetCursorError(i%3 == 0)
			ctx.State.SetCursorErrorTime(time.Now())
			ctx.State.SetScoreBlinkActive(i%4 == 0)
			// Set score blink type and level
			seqType := components.SequenceType(i % 4) // 0=Blue, 1=Green, 2=Red, 3=Gold
			level := components.SequenceLevel(i % 3)  // 0=Bright, 1=Normal, 2=Dark
			ctx.State.SetScoreBlinkType(uint32(seqType))
			ctx.State.SetScoreBlinkLevel(uint32(level))
			ctx.State.SetScoreBlinkTime(time.Now())
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
			_ = ctx.State.GetScore()
			_ = ctx.State.GetHeat()
			_ = ctx.State.GetBoostEnabled()
			_ = ctx.State.GetBoostEndTime()
			_ = ctx.State.GetCursorError()
			_ = ctx.State.GetCursorErrorTime()
			_ = ctx.State.GetScoreBlinkActive()
			_ = ctx.State.GetScoreBlinkType()
			_ = ctx.State.GetScoreBlinkTime()
			_ = ctx.GetPingActive()
			_ = ctx.GetPingGridTimer()
			time.Sleep(50 * time.Microsecond)
		}
	}()

	// Goroutine 3: Mixed read/write operations (using atomic methods)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			if ctx.State.GetScore() > 50 {
				ctx.State.SetScore(0)
			}
			if ctx.State.GetBoostEnabled() {
				_ = ctx.State.GetBoostEndTime()
			}
			if ctx.GetPingActive() {
				// Use atomic CAS for ping timer update
				ctx.UpdatePingGridTimerAtomic(0.1)
			}
			time.Sleep(75 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestPingTimerAtomicCAS tests the atomic CAS pattern for ping timer updates
func TestPingTimerAtomicCAS(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)

	// Set initial ping timer
	ctx.SetPingGridTimer(1.0) // 1 second
	ctx.SetPingActive(true)

	var wg sync.WaitGroup

	// Goroutine 1: Simulate main loop decrementing ping timer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if ctx.UpdatePingGridTimerAtomic(0.016) {
				// Timer expired
				ctx.SetPingActive(false)
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 2: Simulate another goroutine reading ping timer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			timer := ctx.GetPingGridTimer()
			active := ctx.GetPingActive()
			_ = timer
			_ = active
			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Goroutine 3: Occasionally set ping timer (simulate ping activation)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			ctx.SetPingGridTimer(0.5)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestBoostTimerAtomicCAS tests the atomic CAS pattern for boost timer expiration
func TestBoostTimerAtomicCAS(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup

	// Goroutine 1: Repeatedly activate boost with short duration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			ctx.State.SetBoostEnabled(true)
			ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(5 * time.Millisecond))
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Goroutine 2: Check for expiration using atomic CAS
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			ctx.State.UpdateBoostTimerAtomic()
			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Goroutine 3: Read boost state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			enabled := ctx.State.GetBoostEnabled()
			if enabled {
				_ = ctx.State.GetBoostEndTime()
			}
			time.Sleep(300 * time.Microsecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestConcurrentPingTimerUpdates tests concurrent updates to ping timer
func TestConcurrentPingTimerUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)

	// Set initial timer
	ctx.SetPingGridTimer(10.0)
	ctx.SetPingActive(true)

	var wg sync.WaitGroup

	// Multiple goroutines decrementing the timer
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if ctx.UpdatePingGridTimerAtomic(0.1) {
					// Timer expired
					ctx.SetPingActive(false)
				}
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	// Goroutine to occasionally reset the timer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			ctx.SetPingGridTimer(5.0)
			ctx.SetPingActive(true)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}

// TestConcurrentBoostUpdates tests concurrent boost activations and expirations
func TestConcurrentBoostUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup

	// Goroutine 1: Rapidly toggle boost on/off
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ctx.State.SetBoostEnabled(true)
			ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(10 * time.Millisecond))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 2: Check for expiration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			ctx.State.UpdateBoostTimerAtomic()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Goroutine 3: Manually disable boost
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if ctx.State.GetBoostEnabled() {
				ctx.State.SetBoostEnabled(false)
			}
			time.Sleep(8 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Test passes if no race is detected
}
