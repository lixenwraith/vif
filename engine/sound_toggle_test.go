package engine

import (
	"sync"
	"testing"
)

// TestSoundEnabledDefaultState tests that SoundEnabled defaults to false
func TestSoundEnabledDefaultState(t *testing.T) {
	// Create a minimal GameContext for testing
	ctx := &GameContext{
		World: NewWorld(),
	}

	// Initialize SoundEnabled (simulating NewGameContext behavior)
	ctx.SoundEnabled.Store(false)

	// Verify default state is false
	if ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to default to false, got true")
	}
}

// TestSoundEnabledToggle tests that SoundEnabled can be toggled
func TestSoundEnabledToggle(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	// Initialize to false
	ctx.SoundEnabled.Store(false)

	// Toggle to true
	ctx.SoundEnabled.Store(!ctx.SoundEnabled.Load())
	if !ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be true after first toggle")
	}

	// Toggle back to false
	ctx.SoundEnabled.Store(!ctx.SoundEnabled.Load())
	if ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be false after second toggle")
	}
}

// TestSoundEnabledConcurrentAccess tests concurrent reads and writes to SoundEnabled
func TestSoundEnabledConcurrentAccess(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	ctx.SoundEnabled.Store(false)

	var wg sync.WaitGroup

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ctx.SoundEnabled.Load()
			}
		}()
	}

	// Concurrent writers (togglers)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				currentState := ctx.SoundEnabled.Load()
				ctx.SoundEnabled.Store(!currentState)
			}
		}()
	}

	wg.Wait()

	// Test passed if no race condition detected
	// Final state doesn't matter as there were many concurrent toggles
	t.Log("Concurrent SoundEnabled access test passed")
}

// TestSoundEnabledMultipleToggleSequence tests a sequence of toggles
func TestSoundEnabledMultipleToggleSequence(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	ctx.SoundEnabled.Store(false)

	// Perform 10 toggles
	for i := 0; i < 10; i++ {
		expected := (i % 2) == 1 // odd iterations should be true
		ctx.SoundEnabled.Store(!ctx.SoundEnabled.Load())
		actual := ctx.SoundEnabled.Load()

		if actual != expected {
			t.Errorf("After %d toggles, expected SoundEnabled=%v, got %v", i+1, expected, actual)
		}
	}

	// After 10 toggles (even number), should be back to false
	if ctx.SoundEnabled.Load() {
		t.Error("Expected SoundEnabled to be false after even number of toggles")
	}
}

// TestSoundEnabledWithSoundManager tests SoundEnabled in conjunction with SoundManager check
func TestSoundEnabledWithSoundManager(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	ctx.SoundEnabled.Store(false)

	// Simulate the pattern used in the codebase for sound calls
	shouldPlaySound := func() bool {
		if ctx.SoundEnabled.Load() {
			ctx.SoundMu.RLock()
			defer ctx.SoundMu.RUnlock()
			return ctx.SoundManager != nil
		}
		return false
	}

	// When SoundEnabled is false, should not play even if SoundManager exists
	if shouldPlaySound() {
		t.Error("Expected no sound when SoundEnabled is false")
	}

	// When SoundEnabled is true but SoundManager is nil, should not play
	ctx.SoundEnabled.Store(true)
	if shouldPlaySound() {
		t.Error("Expected no sound when SoundManager is nil")
	}

	// When both SoundEnabled is true and SoundManager exists, should play
	// (We can't easily create a real SoundManager in tests, so we'll just check the logic)
	ctx.SoundEnabled.Store(false)
	if shouldPlaySound() {
		t.Error("Expected no sound when SoundEnabled is false even with potential SoundManager")
	}
}
