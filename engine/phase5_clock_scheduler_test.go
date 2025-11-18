package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
)

// Phase 5 Clock Scheduler Integration Tests
// These tests verify the clock scheduler behavior with deterministic time,
// including tick timing, phase transition triggering, and system coordination.

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

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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

// TestClockSchedulerConcurrentTicking tests concurrent tick calls (shouldn't happen in practice)
func TestClockSchedulerConcurrentTicking(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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

// TestClockSchedulerStopIdempotence tests that Stop() can be called multiple times
func TestClockSchedulerStopIdempotence(t *testing.T) {
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

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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

// TestClockSchedulerPhaseNormalDoesNothing tests that Normal phase doesn't trigger anything
func TestClockSchedulerPhaseNormalDoesNothing(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

	// Start in Normal phase
	if phase := ctx.State.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected initial phase PhaseNormal, got %v", phase)
	}

	// Tick many times in Normal phase
	for i := 0; i < 100; i++ {
		scheduler.tick()
		mockTime.Advance(50 * time.Millisecond)
	}

	// Verify no systems were triggered
	if count := mockGold.GetTimeoutCount(); count != 0 {
		t.Errorf("Expected no gold timeouts in Normal phase, got %d", count)
	}
	if count := mockDecay.GetTriggerCount(); count != 0 {
		t.Errorf("Expected no decay triggers in Normal phase, got %d", count)
	}

	// Phase should still be Normal
	if phase := ctx.State.GetPhase(); phase != PhaseNormal {
		t.Errorf("Expected phase to remain PhaseNormal, got %v", phase)
	}

	t.Logf("✓ PhaseNormal doesn't trigger any transitions (100 ticks)")
}

// TestClockSchedulerPhaseDecayAnimationDoesNothing tests that DecayAnimation phase waits for system
func TestClockSchedulerPhaseDecayAnimationDoesNothing(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ctx := &GameContext{
		State:        NewGameState(80, 24, 100, mockTime),
		TimeProvider: mockTime,
		World:        NewWorld(),
	}

	mockGold := &MockGoldSystem{}
	mockDecay := &MockDecaySystem{}

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

	// Manually transition to DecayAnimation phase
	ctx.State.StartDecayAnimation()

	if phase := ctx.State.GetPhase(); phase != PhaseDecayAnimation {
		t.Fatalf("Expected PhaseDecayAnimation, got %v", phase)
	}

	// Tick many times - scheduler should not trigger anything
	// (animation is handled by DecaySystem.Update(), not clock)
	for i := 0; i < 50; i++ {
		scheduler.tick()
		mockTime.Advance(50 * time.Millisecond)
	}

	// Decay trigger should not be called again (already called when entering this phase)
	// Gold timeout should not be called (not in gold phase)
	// Phase should remain DecayAnimation until system calls StopDecayAnimation()

	if phase := ctx.State.GetPhase(); phase != PhaseDecayAnimation {
		t.Errorf("Expected phase to remain PhaseDecayAnimation, got %v", phase)
	}

	t.Logf("✓ PhaseDecayAnimation waits for DecaySystem to finish (50 ticks, phase unchanged)")
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

	scheduler := NewClockScheduler(ctx)
	scheduler.SetSystems(mockGold, mockDecay)

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
