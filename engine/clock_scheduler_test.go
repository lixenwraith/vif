package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
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

func (m *MockScreen) Init() error          { return nil }
func (m *MockScreen) Fini()                {}
func (m *MockScreen) Clear()               {}
func (m *MockScreen) Show()                {}
func (m *MockScreen) Sync()                {}
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
	gs.SetPhase(PhaseGoldActive)
	if gs.GetPhase() != PhaseGoldActive {
		t.Errorf("Expected PhaseGoldActive, got %v", gs.GetPhase())
	}

	// Verify phase duration
	tp.Advance(2 * time.Second)
	duration := gs.GetPhaseDuration()
	if duration < 2*time.Second || duration > 2100*time.Millisecond {
		t.Errorf("Phase duration = %v, expected ~2s", duration)
	}

	// Transition to DecayWait
	tp.Advance(500 * time.Millisecond)
	gs.SetPhase(PhaseDecayWait)
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
	gs.SetPhase(PhaseDecayAnimation)
	if gs.GetPhase() != PhaseDecayAnimation {
		t.Errorf("Expected PhaseDecayAnimation, got %v", gs.GetPhase())
	}

	// Transition back to Normal (cycle complete)
	tp.Advance(5 * time.Second)
	gs.SetPhase(PhaseNormal)
	if gs.GetPhase() != PhaseNormal {
		t.Errorf("Expected PhaseNormal, got %v", gs.GetPhase())
	}
}

// TestPhaseSnapshot tests consistent phase state reads
func TestPhaseSnapshot(t *testing.T) {
	tp := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(80, 24, 100, tp)

	// Set to GoldActive
	gs.SetPhase(PhaseGoldActive)
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

	// Concurrent writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		phases := []GamePhase{PhaseNormal, PhaseGoldActive, PhaseDecayWait, PhaseDecayAnimation}
		for j := 0; j < 50; j++ {
			gs.SetPhase(phases[j%len(phases)])
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
	scheduler := NewClockScheduler(ctx)

	if scheduler.ctx != ctx {
		t.Error("Scheduler context not set correctly")
	}

	if scheduler.ticker == nil {
		t.Error("Scheduler ticker not initialized")
	}

	if scheduler.GetTickCount() != 0 {
		t.Errorf("Initial tick count = %d, want 0", scheduler.GetTickCount())
	}

	tickRate := scheduler.GetTickRate()
	if tickRate != 50*time.Millisecond {
		t.Errorf("Tick rate = %v, want 50ms", tickRate)
	}

	// Cleanup
	scheduler.Stop()
}

// TestClockSchedulerBasicTicking tests that the clock scheduler ticks correctly
func TestClockSchedulerBasicTicking(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	scheduler := NewClockScheduler(ctx)

	// Verify tick count starts at 0
	if count := scheduler.GetTickCount(); count != 0 {
		t.Fatalf("Expected initial tick count 0, got %d", count)
	}

	// Manually call tick (simulating what the goroutine would do)
	for i := 1; i <= 10; i++ {
		scheduler.tick()
		count := scheduler.GetTickCount()
		if count != uint64(i) {
			t.Errorf("After %d ticks, expected count %d, got %d", i, i, count)
		}
	}

	t.Logf("✓ Clock scheduler tick count increments correctly")
}

// TestClockSchedulerPhaseTransitionTiming tests that phase transitions happen on clock ticks
func TestClockSchedulerPhaseTransitionTiming(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	// ===== Test Gold Timeout Transition =====
	// Activate gold sequence
	sequenceID := ctx.State.IncrementGoldSequenceID()
	ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Tick before timeout - should not trigger
	scheduler.tick()
	if count := mockGold.GetTimeoutCount(); count != 0 {
		t.Errorf("Expected no gold timeout before timeout time, got %d", count)
	}

	// Advance time past gold timeout
	mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)

	// Tick after timeout - should trigger
	scheduler.tick()
	if count := mockGold.GetTimeoutCount(); count != 1 {
		t.Errorf("Expected 1 gold timeout after timeout time, got %d", count)
	}

	// Verify phase transition to DecayWait
	if phase := ctx.State.GetPhase(); phase != PhaseDecayWait {
		t.Errorf("Expected PhaseDecayWait after gold timeout, got %v", phase)
	}

	// ===== Test Decay Timer Expiration Transition =====
	// Get decay interval
	decaySnapshot := ctx.State.ReadDecayState()
	interval := decaySnapshot.TimeUntil

	// Tick before decay ready - should not trigger
	scheduler.tick()
	if count := mockDecay.GetTriggerCount(); count != 0 {
		t.Errorf("Expected no decay trigger before timer expires, got %d", count)
	}

	// Advance time past decay interval
	mockTime.Advance(time.Duration(interval+1) * time.Second)

	// Tick after decay ready - should trigger
	scheduler.tick()
	if count := mockDecay.GetTriggerCount(); count != 1 {
		t.Errorf("Expected 1 decay trigger after timer expires, got %d", count)
	}

	// Verify phase transition to DecayAnimation
	if phase := ctx.State.GetPhase(); phase != PhaseDecayAnimation {
		t.Errorf("Expected PhaseDecayAnimation after decay trigger, got %v", phase)
	}

	t.Logf("✓ Clock scheduler triggers phase transitions at correct times")
}

// TestClockSchedulerWithoutSystems tests that scheduler doesn't crash without systems
func TestClockSchedulerWithoutSystems(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	scheduler := NewClockScheduler(ctx)
	// Don't call SetSystems - leave systems nil

	// Activate gold and advance time
	sequenceID := ctx.State.IncrementGoldSequenceID()
	ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)
	mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)

	// Tick should not crash even with nil systems
	scheduler.tick()

	// Phase should still transition (state management is independent)
	if phase := ctx.State.GetPhase(); phase != PhaseDecayWait {
		t.Errorf("Expected PhaseDecayWait even without systems, got %v", phase)
	}

	t.Logf("✓ Clock scheduler handles nil systems gracefully")
}

// TestClockSchedulerMultipleGoldTimeouts tests multiple gold timeout cycles
func TestClockSchedulerMultipleGoldTimeouts(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	// Run 5 complete cycles
	for cycle := 0; cycle < 5; cycle++ {
		// 1. Activate gold
		sequenceID := ctx.State.IncrementGoldSequenceID()
		ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

		// 2. Advance to timeout and tick
		mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)
		scheduler.tick()

		expectedTimeouts := cycle + 1
		if count := mockGold.GetTimeoutCount(); count != expectedTimeouts {
			t.Errorf("Cycle %d: Expected %d gold timeouts, got %d", cycle+1, expectedTimeouts, count)
		}

		// 3. Advance to decay ready and tick
		decaySnapshot := ctx.State.ReadDecayState()
		mockTime.Advance(time.Duration(decaySnapshot.TimeUntil+1) * time.Second)
		scheduler.tick()

		expectedTriggers := cycle + 1
		if count := mockDecay.GetTriggerCount(); count != expectedTriggers {
			t.Errorf("Cycle %d: Expected %d decay triggers, got %d", cycle+1, expectedTriggers, count)
		}

		// 4. Stop decay animation (return to Normal)
		ctx.State.StopDecayAnimation()
	}

	if mockGold.GetTimeoutCount() != 5 {
		t.Errorf("Expected 5 total gold timeouts, got %d", mockGold.GetTimeoutCount())
	}
	if mockDecay.GetTriggerCount() != 5 {
		t.Errorf("Expected 5 total decay triggers, got %d", mockDecay.GetTriggerCount())
	}

	t.Logf("✓ Multiple gold timeout cycles work correctly: %d timeouts, %d triggers",
		mockGold.GetTimeoutCount(), mockDecay.GetTriggerCount())
}

// TestClockSchedulerConcurrentTicking tests concurrent tick calls
func TestClockSchedulerConcurrentTicking(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	const numGoroutines = 10
	const ticksPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent tick calls
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ticksPerGoroutine; j++ {
				scheduler.tick()
			}
		}()
	}

	wg.Wait()

	// Verify tick count is correct (all ticks should be counted)
	expectedTicks := uint64(numGoroutines * ticksPerGoroutine)
	actualTicks := scheduler.GetTickCount()

	if actualTicks != expectedTicks {
		t.Errorf("Expected %d total ticks, got %d", expectedTicks, actualTicks)
	}

	t.Logf("✓ Concurrent ticking handled correctly: %d ticks from %d goroutines",
		actualTicks, numGoroutines)
}

// TestClockSchedulerStopIdempotent tests that Stop() can be called multiple times
func TestClockSchedulerStopIdempotent(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	scheduler := NewClockScheduler(ctx)

	// Call Stop() multiple times - should not panic or cause issues
	scheduler.Stop()
	scheduler.Stop()
	scheduler.Stop()

	t.Logf("✓ Stop() is idempotent - can be called multiple times safely")
}

// TestClockSchedulerPhaseTransitionAtBoundary tests phase transitions at exact boundary times
func TestClockSchedulerPhaseTransitionAtBoundary(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	// Activate gold
	sequenceID := ctx.State.IncrementGoldSequenceID()
	ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Advance to just PAST the timeout time (1ms after)
	// Note: IsGoldTimedOut() uses After() which requires time to be strictly greater
	mockTime.Advance(constants.GoldSequenceDuration + time.Millisecond)

	// Tick just after boundary - should trigger
	scheduler.tick()

	if count := mockGold.GetTimeoutCount(); count != 1 {
		t.Errorf("Expected gold timeout just after boundary time, got %d timeouts", count)
	}

	// For decay timer, test EXACT boundary (IsDecayReady uses After() || Equal())
	decaySnapshot := ctx.State.ReadDecayState()
	decayInterval := decaySnapshot.NextTime.Sub(mockTime.Now())

	// Advance to EXACTLY decay time
	mockTime.Advance(decayInterval)

	// Tick at exact boundary - should trigger (IsDecayReady allows equal)
	scheduler.tick()

	if count := mockDecay.GetTriggerCount(); count != 1 {
		t.Errorf("Expected decay trigger at exact boundary time, got %d triggers", count)
	}

	t.Logf("✓ Phase transitions trigger at boundary times (gold=after, decay=at-or-after)")
}

// TestClockSchedulerNoEarlyTransition tests that transitions don't happen early
func TestClockSchedulerNoEarlyTransition(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	// Activate gold
	sequenceID := ctx.State.IncrementGoldSequenceID()
	ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Advance to 1ms BEFORE timeout
	mockTime.Advance(constants.GoldSequenceDuration - time.Millisecond)

	// Tick just before timeout - should NOT trigger
	scheduler.tick()

	if count := mockGold.GetTimeoutCount(); count != 0 {
		t.Errorf("Expected no gold timeout before timeout time, got %d", count)
	}

	// Phase should still be GoldActive
	if phase := ctx.State.GetPhase(); phase != PhaseGoldActive {
		t.Errorf("Expected PhaseGoldActive before timeout, got %v", phase)
	}

	t.Logf("✓ Phase transitions do not happen prematurely")
}

// TestClockSchedulerTickRate tests that tick rate is correct
func TestClockSchedulerTickRate(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	scheduler := NewClockScheduler(ctx)

	tickRate := scheduler.GetTickRate()
	expectedRate := 50 * time.Millisecond

	if tickRate != expectedRate {
		t.Errorf("Expected tick rate %v, got %v", expectedRate, tickRate)
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
	scheduler := NewClockScheduler(ctx)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop()

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
	scheduler := NewClockScheduler(ctx)

	scheduler.Start()
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

// TestClockSchedulerConcurrentAccess tests thread-safe tick count reads
func TestClockSchedulerConcurrentAccess(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	scheduler.Start()
	defer scheduler.Stop()

	const numReaders = 10
	const numReads = 50

	var wg sync.WaitGroup
	wg.Add(numReaders)

	// Launch concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numReads; j++ {
				_ = scheduler.GetTickCount()
				_ = scheduler.GetTickRate()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify tick count is reasonable
	tickCount := scheduler.GetTickCount()
	if tickCount == 0 {
		t.Error("Expected non-zero tick count after concurrent access")
	}
}

// TestPhaseAndClockIntegration tests phase state changes during clock ticks
func TestPhaseAndClockIntegration(t *testing.T) {
	screen := &MockScreen{}
	ctx := NewGameContext(screen)
	scheduler := NewClockScheduler(ctx)

	// Verify initial phase
	if ctx.State.GetPhase() != PhaseNormal {
		t.Errorf("Initial phase = %v, want PhaseNormal", ctx.State.GetPhase())
	}

	scheduler.Start()
	defer scheduler.Stop()

	// Let it tick for a bit
	time.Sleep(200 * time.Millisecond)

	// Change phase during ticking
	ctx.State.SetPhase(PhaseGoldActive)
	time.Sleep(100 * time.Millisecond)

	// Verify phase persisted
	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Errorf("Phase = %v, want PhaseGoldActive", ctx.State.GetPhase())
	}

	// Change to DecayWait
	ctx.State.SetPhase(PhaseDecayWait)
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
		scheduler := NewClockScheduler(ctx)
		scheduler.Start()
		time.Sleep(20 * time.Millisecond)
		scheduler.Stop()
	}

	// If we reach here without hanging, test passes
	// (Goroutine leak would cause test timeout)
}

// TestClockSchedulerIntegrationWithRealTime tests scheduler with actual time (not mocked)
func TestClockSchedulerIntegrationWithRealTime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real-time test in short mode")
	}

	// Use real monotonic time provider
	realTime := NewMonotonicTimeProvider()
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, realTime),
		TimeProvider: realTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}
	mockCleaner := &MockCleanerSystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay, mockCleaner)

	// Start scheduler in goroutine
	scheduler.Start()
	defer scheduler.Stop()

	// Activate gold with very short timeout (200ms for testing)
	sequenceID := ctx.State.IncrementGoldSequenceID()
	ctx.State.ActivateGoldSequence(sequenceID, 200*time.Millisecond)

	// Wait for timeout to trigger (should happen within ~250ms)
	time.Sleep(300 * time.Millisecond)

	// Check that timeout was triggered
	if count := mockGold.GetTimeoutCount(); count == 0 {
		t.Error("Expected gold timeout to be triggered by real-time scheduler")
	}

	// Verify phase transitioned
	if phase := ctx.State.GetPhase(); phase != PhaseDecayWait {
		t.Errorf("Expected PhaseDecayWait after timeout, got %v", phase)
	}

	t.Logf("✓ Real-time scheduler integration works correctly")
}
