package engine

import (
	"sync"
	"testing"

	"github.com/lixenwraith/vi-fighter/audio"
)

// MockSoundManager is a mock implementation for testing
type MockSoundManager struct {
	mu            sync.Mutex
	playCount     int
	stopCount     int
	errorCount    int
	maxHeatCount  int
	decayCount    int
}

func (m *MockSoundManager) PlayTrail() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playCount++
}

func (m *MockSoundManager) StopTrail() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCount++
}

func (m *MockSoundManager) PlayError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorCount++
}

func (m *MockSoundManager) PlayMaxHeat() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxHeatCount++
}

func (m *MockSoundManager) StopMaxHeat() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxHeatCount--
}

func (m *MockSoundManager) PlayDecay() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decayCount++
}

func (m *MockSoundManager) GetCounts() (play, stop, errorC, maxHeat, decay int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playCount, m.stopCount, m.errorCount, m.maxHeatCount, m.decayCount
}

// TestConcurrentSoundManagerAccess tests concurrent access to SoundManager through GameContext
func TestConcurrentSoundManagerAccess(t *testing.T) {
	// Note: We can't easily create a full GameContext without a screen,
	// so we'll test the mutex protection pattern directly

	ctx := &GameContext{
		World: NewWorld(),
	}

	// Create and set a real sound manager (or mock)
	// For this test, we'll use a mock that doesn't require audio initialization
	mock := &MockSoundManager{}

	// Cast to the interface type that GameContext expects
	// Since we can't create audio.SoundManager without initialization,
	// we'll test the locking pattern itself

	var wg sync.WaitGroup
	iterations := 100

	// Simulate concurrent reads from multiple goroutines (like systems accessing audio)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ctx.SoundMu.RLock()
				sm := ctx.SoundManager
				if sm != nil {
					// Simulate using the sound manager
					_ = sm
				}
				ctx.SoundMu.RUnlock()
			}
		}()
	}

	// Simulate concurrent write (like initialization)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx.SoundMu.Lock()
		ctx.SoundManager = (*audio.SoundManager)(nil)
		ctx.SoundMu.Unlock()
	}()

	wg.Wait()

	// Test passed if no race condition detected
	t.Log("Concurrent SoundManager access test passed")
}

// TestSetGetSoundManager tests the thread-safe accessors
func TestSetGetSoundManager(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	var wg sync.WaitGroup

	// Concurrent sets
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm := audio.NewSoundManager()
			ctx.SetSoundManager(sm)
		}()
	}

	// Concurrent gets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sm := ctx.GetSoundManager()
				_ = sm
			}
		}()
	}

	wg.Wait()

	// Verify SoundManager is set
	sm := ctx.GetSoundManager()
	if sm == nil {
		t.Error("Expected SoundManager to be set")
	}
}

// TestSoundManagerNilSafety tests that nil SoundManager doesn't cause panics
func TestSoundManagerNilSafety(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	// SoundManager is nil by default
	ctx.soundMu.RLock()
	sm := ctx.SoundManager
	ctx.soundMu.RUnlock()

	if sm != nil {
		t.Error("Expected SoundManager to be nil initially")
	}

	// Accessing nil SoundManager should be checked by callers
	// This pattern is used throughout the codebase:
	// if ctx.SoundManager != nil { ... }
}

// TestMultipleSystemsAccessingAudio simulates multiple systems accessing audio concurrently
func TestMultipleSystemsAccessingAudio(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	sm := audio.NewSoundManager()
	ctx.SetSoundManager(sm)

	var wg sync.WaitGroup

	// Simulate ScoreSystem accessing audio
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ctx.SoundMu.RLock()
				if ctx.SoundManager != nil {
					// Would call PlayError() here
				}
				ctx.SoundMu.RUnlock()
			}
		}()
	}

	// Simulate DecaySystem accessing audio
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ctx.SoundMu.RLock()
				if ctx.SoundManager != nil {
					// Would call PlayDecay() here
				}
				ctx.SoundMu.RUnlock()
			}
		}()
	}

	// Simulate TrailSystem accessing audio (via ScoreSystem)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				ctx.SoundMu.RLock()
				if ctx.SoundManager != nil {
					// Would call PlayTrail() or StopTrail() here
				}
				ctx.SoundMu.RUnlock()
			}
		}()
	}

	wg.Wait()

	// Test passed if no race condition detected
	t.Log("Multiple systems accessing audio concurrently - test passed")
}

// TestConcurrentAudioInitialization tests setting SoundManager during concurrent access
func TestConcurrentAudioInitialization(t *testing.T) {
	ctx := &GameContext{
		World: NewWorld(),
	}

	var wg sync.WaitGroup

	// Goroutines trying to read SoundManager
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				sm := ctx.GetSoundManager()
				_ = sm
			}
		}()
	}

	// Goroutine initializing SoundManager
	wg.Add(1)
	go func() {
		defer wg.Done()
		sm := audio.NewSoundManager()
		ctx.SetSoundManager(sm)
	}()

	wg.Wait()

	// Verify final state
	finalSM := ctx.GetSoundManager()
	if finalSM == nil {
		t.Error("Expected SoundManager to be initialized")
	}
}
