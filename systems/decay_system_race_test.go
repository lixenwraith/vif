package systems

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// setupTestContext creates a test game context with simulation screen
func setupTestContext(t *testing.T) *engine.GameContext {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Use mock time provider for controlled testing
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	return ctx
}

// TestConcurrentStartDecayTimer tests concurrent StartDecayTimer() calls from multiple goroutines
// This test verifies that multiple goroutines can call StartDecayTimer() simultaneously
// without causing data races on timerStarted, nextDecayTime, and lastUpdate fields
func TestConcurrentStartDecayTimer(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	// Number of concurrent goroutines
	numGoroutines := 10
	var wg sync.WaitGroup

	// Launch multiple goroutines that all call StartDecayTimer()
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decaySystem.StartDecayTimer()
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify timer was started
	decaySystem.mu.RLock()
	timerStarted := decaySystem.timerStarted
	decaySystem.mu.RUnlock()

	if !timerStarted {
		t.Error("Timer should be started after concurrent calls")
	}
}

// TestConcurrentUpdateAndStartDecayTimer tests concurrent Update() and StartDecayTimer() operations
// This test verifies that Update() can safely read timer state while other goroutines
// are calling StartDecayTimer()
func TestConcurrentUpdateAndStartDecayTimer(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)
	world := ctx.World

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	// Number of iterations for stress testing
	iterations := 100
	var wg sync.WaitGroup

	// Goroutine 1: Repeatedly calls Update()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			decaySystem.Update(world, 16*time.Millisecond)
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: Repeatedly calls StartDecayTimer()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			decaySystem.StartDecayTimer()
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 3: Repeatedly calls GetTimeUntilDecay()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = decaySystem.GetTimeUntilDecay()
			time.Sleep(time.Microsecond)
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()
}

// TestConcurrentTimerAccess tests concurrent access to timer-related fields
// This test ensures that reading and writing timer values is properly synchronized
func TestConcurrentTimerAccess(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	// Start the timer initially
	decaySystem.StartDecayTimer()

	var wg sync.WaitGroup
	numGoroutines := 5
	iterations := 50

	// Launch goroutines that read timer state
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Read operations
				_ = decaySystem.GetTimeUntilDecay()

				decaySystem.mu.RLock()
				_ = decaySystem.timerStarted
				_ = decaySystem.nextDecayTime
				decaySystem.mu.RUnlock()

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Launch goroutines that write timer state
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				decaySystem.StartDecayTimer()
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Wait for all goroutines
	wg.Wait()
}

// TestConcurrentUpdateDimensions tests concurrent UpdateDimensions() calls
// This verifies that dimension updates don't race with calculateInterval()
func TestConcurrentUpdateDimensions(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	var wg sync.WaitGroup
	iterations := 100

	// Goroutine 1: Repeatedly calls UpdateDimensions()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			decaySystem.UpdateDimensions(80+i%10, 24+i%5, 100+i%20, 50+i%30)
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: Repeatedly calls StartDecayTimer() which reads dimensions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			decaySystem.StartDecayTimer()
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 3: Repeatedly calls GetTimeUntilDecay()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = decaySystem.GetTimeUntilDecay()
			time.Sleep(time.Microsecond)
		}
	}()

	// Wait for all goroutines
	wg.Wait()
}

// TestTimerConsistency verifies that timer values remain consistent under concurrent access
// This test checks that the timer state is always valid after concurrent operations
func TestTimerConsistency(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	var wg sync.WaitGroup
	iterations := 50

	// Launch concurrent timer starts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				decaySystem.StartDecayTimer()

				// Read timer state immediately after starting
				decaySystem.mu.RLock()
				timerStarted := decaySystem.timerStarted
				nextDecayTime := decaySystem.nextDecayTime
				lastUpdate := decaySystem.lastUpdate
				decaySystem.mu.RUnlock()

				// Verify consistency
				if timerStarted && nextDecayTime.Before(lastUpdate) {
					t.Errorf("Inconsistent state: nextDecayTime (%v) is before lastUpdate (%v)",
						nextDecayTime, lastUpdate)
				}

				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Wait for completion
	wg.Wait()

	// Final consistency check
	decaySystem.mu.RLock()
	timerStarted := decaySystem.timerStarted
	nextDecayTime := decaySystem.nextDecayTime
	lastUpdate := decaySystem.lastUpdate
	decaySystem.mu.RUnlock()

	if !timerStarted {
		t.Error("Timer should be started after all operations")
	}

	if nextDecayTime.Before(lastUpdate) {
		t.Errorf("Final state inconsistent: nextDecayTime (%v) is before lastUpdate (%v)",
			nextDecayTime, lastUpdate)
	}
}

// TestConcurrentFullWorkload tests the complete workflow with all operations running concurrently
// This is a comprehensive stress test that simulates real-world concurrent usage
func TestConcurrentFullWorkload(t *testing.T) {
	// Create test context
	ctx := setupTestContext(t)
	world := ctx.World

	// Create DecaySystem
	decaySystem := NewDecaySystem(80, 24, 100, 50, ctx)

	var wg sync.WaitGroup
	duration := 100 * time.Millisecond
	stopChan := make(chan struct{})

	// Goroutine 1: Continuous Update() calls
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				decaySystem.Update(world, 16*time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Periodic StartDecayTimer() calls
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				decaySystem.StartDecayTimer()
			}
		}
	}()

	// Goroutine 3: Continuous GetTimeUntilDecay() calls
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				_ = decaySystem.GetTimeUntilDecay()
			}
		}
	}()

	// Goroutine 4: Occasional UpdateDimensions() calls
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				count++
				decaySystem.UpdateDimensions(80+count%10, 24+count%5, 100+count%20, 50+count%30)
			}
		}
	}()

	// Let the test run for the specified duration
	time.Sleep(duration)
	close(stopChan)

	// Wait for all goroutines to finish
	wg.Wait()

	// Final validation
	decaySystem.mu.RLock()
	timerStarted := decaySystem.timerStarted
	decaySystem.mu.RUnlock()

	if !timerStarted {
		t.Error("Timer should be started after workload")
	}
}
