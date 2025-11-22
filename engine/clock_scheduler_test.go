package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// MockScreen is a minimal mock for tcell.Screen used in tests
type MockScreen struct {
	tcell.Screen
	width, height int
}

func (m *MockScreen) Size() (int, int) {
	if m.width == 0 && m.height == 0 {
		return 80, 24 // Default size
	}
	return m.width, m.height
}

func (m *MockScreen) Init() error                                                      { return nil }
func (m *MockScreen) Fini()                                                            {}
func (m *MockScreen) Clear()                                                           {}
func (m *MockScreen) Show()                                                            {}
func (m *MockScreen) Sync()                                                            {}
func (m *MockScreen) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {}

// MockGoldSystem implements GoldSequenceSystemInterface for testing
type MockGoldSystem struct {
	timeoutCount atomic.Int32
}

func (m *MockGoldSystem) TimeoutGoldSequence(world *World) {
	m.timeoutCount.Add(1)
}

func (m *MockGoldSystem) GetTimeoutCount() int {
	return int(m.timeoutCount.Load())
}

// MockDecaySystem implements DecaySystemInterface for testing
type MockDecaySystem struct {
	triggerCount atomic.Int32
}

func (m *MockDecaySystem) TriggerDecayAnimation(world *World) {
	m.triggerCount.Add(1)
}

func (m *MockDecaySystem) GetTriggerCount() int {
	return int(m.triggerCount.Load())
}

// MockCleanerSystem implements CleanerSystemInterface for testing
type MockCleanerSystem struct {
	activateCount atomic.Int32
	isComplete    atomic.Bool
}

func (m *MockCleanerSystem) ActivateCleaners(world *World) {
	m.activateCount.Add(1)
	m.isComplete.Store(false) // Animation starts, not complete
}

func (m *MockCleanerSystem) IsAnimationComplete() bool {
	return m.isComplete.Load()
}

func (m *MockCleanerSystem) GetActivateCount() int {
	return int(m.activateCount.Load())
}

func (m *MockCleanerSystem) SetComplete(complete bool) {
	m.isComplete.Store(complete)
}

// ============================================================================
// Phase State Tests
// ============================================================================

// TestGamePhaseString tests the String() method for GamePhase
func TestGamePhaseString(t *testing.T) {
	tests := []struct {
		phase    GamePhase
		expected string
	}{
		{PhaseNormal, "Normal"},
		{PhaseGoldActive, "GoldActive"},
		{PhaseDecayWait, "DecayWait"},
		{PhaseDecayAnimation, "DecayAnimation"},
		{GamePhase(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.phase.String()
			if result != tt.expected {
				t.Errorf("GamePhase(%d).String() = %q, want %q", tt.phase, result, tt.expected)
			}
		})
	}
}

// TestPhaseStateInitialization verifies initial phase state
func TestPhaseStateInitialization(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Verify initial phase
	phase := gs.GetPhase()
	if phase != PhaseNormal {
		t.Errorf("Initial phase = %v, want %v", phase, PhaseNormal)
	}

	// Verify phase start time
	startTime := gs.GetPhaseStartTime()
	if startTime.IsZero() {
		t.Error("Phase start time should not be zero")
	}

	// Verify phase duration is near zero
	duration := gs.GetPhaseDuration()
	if duration > 10*time.Millisecond {
		t.Errorf("Initial phase duration = %v, expected near zero", duration)
	}
}

// TestPhaseTransitions tests phase state changes
func TestPhaseTransitions(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Start in Normal phase
	if gs.GetPhase() != PhaseNormal {
		t.Errorf("Expected initial phase PhaseNormal, got %v", gs.GetPhase())
	}

	// Transition to GoldActive
	tp.Advance(1 * time.Second)
	if !gs.TransitionPhase(PhaseGoldActive) {
		t.Fatal("Failed to transition to PhaseGoldActive")
	}
	if gs.GetPhase() != PhaseGoldActive {
		t.Errorf("Expected PhaseGoldActive, got %v", gs.GetPhase())
	}

	// Verify phase duration
	tp.Advance(2 * time.Second)
	duration := gs.GetPhaseDuration()
	if duration < 2*time.Second || duration > 2100*time.Millisecond {
		t.Errorf("Phase duration = %v, expected ~2s", duration)
	}

	// Transition through GoldComplete to DecayWait (valid sequence)
	tp.Advance(500 * time.Millisecond)
	if !gs.TransitionPhase(PhaseGoldComplete) {
		t.Fatal("Failed to transition to PhaseGoldComplete")
	}
	if !gs.TransitionPhase(PhaseDecayWait) {
		t.Fatal("Failed to transition to PhaseDecayWait")
	}
	if gs.GetPhase() != PhaseDecayWait {
		t.Errorf("Expected PhaseDecayWait, got %v", gs.GetPhase())
	}

	// Verify duration reset on phase change
	duration = gs.GetPhaseDuration()
	if duration > 10*time.Millisecond {
		t.Errorf("Phase duration after transition = %v, expected near zero", duration)
	}

	// Transition to DecayAnimation
	tp.Advance(30 * time.Second)
	if !gs.TransitionPhase(PhaseDecayAnimation) {
		t.Fatal("Failed to transition to PhaseDecayAnimation")
	}
	if gs.GetPhase() != PhaseDecayAnimation {
		t.Errorf("Expected PhaseDecayAnimation, got %v", gs.GetPhase())
	}

	// Transition back to Normal (cycle complete)
	tp.Advance(5 * time.Second)
	if !gs.TransitionPhase(PhaseNormal) {
		t.Fatal("Failed to transition to PhaseNormal")
	}
	if gs.GetPhase() != PhaseNormal {
		t.Errorf("Expected PhaseNormal, got %v", gs.GetPhase())
	}
}

// TestPhaseSnapshot tests consistent phase state reads
func TestPhaseSnapshot(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Transition to GoldActive
	if !gs.TransitionPhase(PhaseGoldActive) {
		t.Fatal("Failed to transition to PhaseGoldActive")
	}
	tp.Advance(3 * time.Second)

	// Read snapshot
	snapshot := gs.ReadPhaseState()

	if snapshot.Phase != PhaseGoldActive {
		t.Errorf("Snapshot phase = %v, want PhaseGoldActive", snapshot.Phase)
	}

	if snapshot.Duration < 3*time.Second {
		t.Errorf("Snapshot duration = %v, expected >= 3s", snapshot.Duration)
	}

	if snapshot.StartTime.IsZero() {
		t.Error("Snapshot start time should not be zero")
	}
}

// TestConcurrentPhaseReads tests thread-safe phase state access
func TestConcurrentPhaseReads(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	const numReaders = 20
	const numReads = 100

	var wg sync.WaitGroup
	wg.Add(numReaders)

	// Launch concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numReads; j++ {
				_ = gs.GetPhase()
				_ = gs.GetPhaseStartTime()
				_ = gs.GetPhaseDuration()
				_ = gs.ReadPhaseState()
			}
		}()
	}

	// Concurrent writer using valid phase transition cycle
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Valid cycle: Normal → GoldActive → GoldComplete → DecayWait → DecayAnimation → Normal
		for j := 0; j < 10; j++ {
			gs.TransitionPhase(PhaseGoldActive)
			time.Sleep(1 * time.Millisecond)
			gs.TransitionPhase(PhaseGoldComplete)
			time.Sleep(1 * time.Millisecond)
			gs.TransitionPhase(PhaseDecayWait)
			time.Sleep(1 * time.Millisecond)
			gs.TransitionPhase(PhaseDecayAnimation)
			time.Sleep(1 * time.Millisecond)
			gs.TransitionPhase(PhaseNormal)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()

	// If we reach here without deadlock or panic, test passes
}

// ============================================================================
// Clock Scheduler Tests - Deterministic
// ============================================================================

// TestClockSchedulerCreation tests scheduler initialization
func TestClockSchedulerCreation(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)

	if scheduler.ctx != ctx {
		t.Error("Scheduler context not set correctly")
	}

	if scheduler.GetTickCount() != 0 {
		t.Errorf("Initial tick count = %d, want 0", scheduler.GetTickCount())
	}

	// Cleanup
	scheduler.Stop()
}

// TestClockSchedulerStopIdempotent tests that Stop() can be called multiple times
func TestClockSchedulerStopIdempotent(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	pausableClock := NewPausableClock()
	ctx := &GameContext{
		State:         NewGameState(80, 24, 100, mockTime),
		TimeProvider:  mockTime,
		PausableClock: pausableClock,
		World:         NewWorld(),
	}

	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)

	// Call Stop() multiple times - should not panic or cause issues
	scheduler.Stop()
	scheduler.Stop()
	scheduler.Stop()

	t.Logf("✓ Stop() is idempotent - can be called multiple times safely")
}

// TestClockSchedulerTickInterval tests that tick rate is correct
func TestClockSchedulerTickInterval(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	pausableClock := NewPausableClock()
	ctx := &GameContext{
		State:         NewGameState(80, 24, 100, mockTime),
		TimeProvider:  mockTime,
		PausableClock: pausableClock,
		World:         NewWorld(),
	}

	const arbitraryInterval = 100 * time.Millisecond
	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, arbitraryInterval, frameReady)

	tickRate := scheduler.GetTickInterval()

	if tickRate != arbitraryInterval {
		t.Errorf("Expected tick rate %v, got %v", arbitraryInterval, tickRate)
	}

	t.Logf("✓ Clock scheduler tick rate is %v (as expected)", tickRate)
}

// ============================================================================
// Clock Scheduler Tests - Real-Time Integration
// ============================================================================

// TestClockSchedulerTicking tests that scheduler ticks with real time
func TestClockSchedulerTicking(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Signal frame ready to allow ticking
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(40 * time.Millisecond)
			select {
			case frameReady <- struct{}{}:
			default:
			}
		}
	}()

	// Wait for multiple ticks (50ms × 10 = 500ms)
	time.Sleep(550 * time.Millisecond)

	tickCount := scheduler.GetTickCount()
	// Should have ticked at least 8 times (allowing for timing variance)
	if tickCount < 8 {
		t.Errorf("Tick count = %d after 550ms, expected at least 8", tickCount)
	}

	// Should not have ticked more than 12 times (allowing for timing variance)
	if tickCount > 12 {
		t.Errorf("Tick count = %d after 550ms, expected at most 12", tickCount)
	}
}

// TestClockSchedulerStopIdempotentRealTime tests that Stop() works with real-time scheduler
func TestClockSchedulerStopIdempotentRealTime(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)

	scheduler.Start()

	// Signal frame ready to allow ticking
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(40 * time.Millisecond)
			select {
			case frameReady <- struct{}{}:
			default:
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop multiple times - should not panic
	scheduler.Stop()
	scheduler.Stop()
	scheduler.Stop()

	initialCount := scheduler.GetTickCount()

	// Wait a bit more
	time.Sleep(100 * time.Millisecond)

	// Tick count should not increase after stop
	finalCount := scheduler.GetTickCount()
	if finalCount != initialCount {
		t.Errorf("Tick count increased after stop: %d -> %d", initialCount, finalCount)
	}
}

// TestPhaseAndClockIntegration tests phase state changes during clock ticks
func TestPhaseAndClockIntegration(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	frameReady := make(chan struct{}, 1)
	scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)

	// Verify initial phase
	if ctx.State.GetPhase() != PhaseNormal {
		t.Errorf("Initial phase = %v, want PhaseNormal", ctx.State.GetPhase())
	}

	scheduler.Start()
	defer scheduler.Stop()

	// Signal frame ready to allow ticking
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(40 * time.Millisecond)
			select {
			case frameReady <- struct{}{}:
			default:
			}
		}
	}()

	// Let it tick for a bit
	time.Sleep(200 * time.Millisecond)

	// Change phase during ticking using valid transitions
	if !ctx.State.TransitionPhase(PhaseGoldActive) {
		t.Fatal("Failed to transition to PhaseGoldActive")
	}
	time.Sleep(100 * time.Millisecond)

	// Verify phase persisted
	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Errorf("Phase = %v, want PhaseGoldActive", ctx.State.GetPhase())
	}

	// Transition through GoldComplete to DecayWait (valid sequence)
	if !ctx.State.TransitionPhase(PhaseGoldComplete) {
		t.Fatal("Failed to transition to PhaseGoldComplete")
	}
	if !ctx.State.TransitionPhase(PhaseDecayWait) {
		t.Fatal("Failed to transition to PhaseDecayWait")
	}
	time.Sleep(100 * time.Millisecond)

	if ctx.State.GetPhase() != PhaseDecayWait {
		t.Errorf("Phase = %v, want PhaseDecayWait", ctx.State.GetPhase())
	}

	// Verify ticks continued during phase changes
	tickCount := scheduler.GetTickCount()
	if tickCount < 6 { // 400ms / 50ms = 8 ticks expected, allow variance
		t.Errorf("Tick count = %d after 400ms, expected at least 6", tickCount)
	}
}

// TestClockSchedulerMemoryLeak tests for goroutine leaks
func TestClockSchedulerMemoryLeak(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)

	// Create and destroy multiple schedulers
	for i := 0; i < 10; i++ {
		frameReady := make(chan struct{}, 1)
		scheduler, _ := NewClockScheduler(ctx, 50*time.Millisecond, frameReady)
		scheduler.Start()
		time.Sleep(20 * time.Millisecond)
		scheduler.Stop()
	}

	// If we reach here without hanging, test passes
	// (Goroutine leak would cause test timeout)
}