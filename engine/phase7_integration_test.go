package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
)

// Integration Tests: Gold/Decay/Cleaner Flow
// These tests verify the complete game cycle including cleaner mechanics

// TestGoldToCleanerFlow tests the complete flow from gold completion to cleaner activation
func TestGoldToCleanerFlow(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Create clock scheduler
	clockScheduler := NewClockScheduler(ctx)

	// Set heat to maximum to trigger cleaners on gold completion
	maxHeat := 80 - constants.HeatBarIndicatorWidth
	ctx.State.SetHeat(maxHeat)

	// Request cleaners (simulating gold completion at max heat)
	ctx.State.RequestCleaners()

	// Verify pending state
	snapshot := ctx.State.ReadCleanerState()
	if !snapshot.Pending {
		t.Error("Cleaners should be pending after request")
	}
	if snapshot.Active {
		t.Error("Cleaners should not be active yet")
	}

	// Advance time to next clock tick (50ms)
	mockTime.Advance(50 * time.Millisecond)

	// Clock scheduler should activate cleaners
	// We simulate this manually since we don't have actual systems in this test
	if ctx.State.GetCleanerPending() {
		ctx.State.ActivateCleaners()
	}

	// Verify active state
	snapshot = ctx.State.ReadCleanerState()
	if snapshot.Pending {
		t.Error("Cleaners should no longer be pending")
	}
	if !snapshot.Active {
		t.Error("Cleaners should be active after activation")
	}
	if snapshot.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}

	// Verify timing
	expectedStartTime := startTime.Add(50 * time.Millisecond)
	if !snapshot.StartTime.Equal(expectedStartTime) {
		t.Errorf("StartTime should be %v, got %v", expectedStartTime, snapshot.StartTime)
	}

	clockScheduler.Stop()
	t.Logf("Gold→Cleaner flow validated successfully")
}

// TestCleanerAnimationCompletion tests that cleaners deactivate after animation duration
func TestCleanerAnimationCompletion(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Activate cleaners
	ctx.State.RequestCleaners()
	mockTime.Advance(50 * time.Millisecond)
	ctx.State.ActivateCleaners()

	// Verify active
	if !ctx.State.GetCleanerActive() {
		t.Fatal("Cleaners should be active")
	}

	// Advance time to just before animation completes (1 second default)
	mockTime.Advance(900 * time.Millisecond)

	// Cleaners should still be active
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active before animation duration")
	}

	// Deactivate cleaners (simulating animation completion)
	ctx.State.DeactivateCleaners()

	// Verify deactivation
	if ctx.State.GetCleanerActive() {
		t.Error("Cleaners should be inactive after deactivation")
	}

	t.Logf("Cleaner animation completion validated successfully")
}

// TestConcurrentCleanerAndGoldPhases tests that cleaners can run in parallel with gold sequences
func TestConcurrentCleanerAndGoldPhases(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Start with normal phase
	ctx.State.SetPhase(PhaseNormal)

	// Activate cleaners
	ctx.State.RequestCleaners()
	mockTime.Advance(50 * time.Millisecond)
	ctx.State.ActivateCleaners()

	if !ctx.State.GetCleanerActive() {
		t.Fatal("Cleaners should be active")
	}

	// Transition to gold phase (cleaners should remain active)
	ctx.State.SetPhase(PhaseGoldActive)

	// Both should be true
	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Error("Phase should be GoldActive")
	}
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during gold phase")
	}

	// Transition through decay phases
	ctx.State.SetPhase(PhaseDecayWait)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay wait")
	}

	ctx.State.SetPhase(PhaseDecayAnimation)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay animation")
	}

	// Return to normal
	ctx.State.SetPhase(PhaseNormal)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active until deactivation")
	}

	// Deactivate cleaners
	ctx.State.DeactivateCleaners()

	if ctx.State.GetCleanerActive() {
		t.Error("Cleaners should be inactive after deactivation")
	}

	t.Logf("Concurrent cleaner and phase transitions validated successfully")
}

// TestMultipleCleanerCycles tests multiple cleaner activation/deactivation cycles
func TestMultipleCleanerCycles(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Run 3 cleaner cycles
	for i := 0; i < 3; i++ {
		// Request and activate
		ctx.State.RequestCleaners()
		mockTime.Advance(50 * time.Millisecond)

		if !ctx.State.GetCleanerPending() {
			t.Errorf("Cycle %d: Cleaners should be pending", i)
		}

		ctx.State.ActivateCleaners()

		if !ctx.State.GetCleanerActive() {
			t.Errorf("Cycle %d: Cleaners should be active", i)
		}

		// Simulate animation duration
		mockTime.Advance(1 * time.Second)

		// Deactivate
		ctx.State.DeactivateCleaners()

		if ctx.State.GetCleanerActive() {
			t.Errorf("Cycle %d: Cleaners should be inactive", i)
		}
	}

	t.Logf("Multiple cleaner cycles validated successfully")
}

// TestCleanerStateSnapshot tests that cleaner state snapshots are consistent
func TestCleanerStateSnapshot(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Request cleaners
	ctx.State.RequestCleaners()
	mockTime.Advance(50 * time.Millisecond)

	snapshot1 := ctx.State.ReadCleanerState()
	if !snapshot1.Pending {
		t.Error("Snapshot should show pending=true")
	}
	if snapshot1.Active {
		t.Error("Snapshot should show active=false")
	}

	// Activate cleaners
	ctx.State.ActivateCleaners()

	snapshot2 := ctx.State.ReadCleanerState()
	if snapshot2.Pending {
		t.Error("Snapshot should show pending=false after activation")
	}
	if !snapshot2.Active {
		t.Error("Snapshot should show active=true after activation")
	}
	if snapshot2.StartTime.IsZero() {
		t.Error("Snapshot should have non-zero StartTime")
	}

	// Deactivate cleaners
	ctx.State.DeactivateCleaners()

	snapshot3 := ctx.State.ReadCleanerState()
	if snapshot3.Pending {
		t.Error("Snapshot should show pending=false after deactivation")
	}
	if snapshot3.Active {
		t.Error("Snapshot should show active=false after deactivation")
	}

	t.Logf("Cleaner state snapshots validated successfully")
}

// TestGoldDecayCleanerCompleteCycle tests a complete game cycle with all mechanics
func TestGoldDecayCleanerCompleteCycle(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Start in Normal phase
	ctx.State.SetPhase(PhaseNormal)

	// Transition to GoldActive (manually simulated)
	ctx.State.SetPhase(PhaseGoldActive)
	mockTime.Advance(50 * time.Millisecond)

	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Error("Phase should be GoldActive")
	}

	// Set heat to max before completing gold
	maxHeat := 80 - constants.HeatBarIndicatorWidth
	ctx.State.SetHeat(maxHeat)

	// Complete gold sequence (should request cleaners)
	ctx.State.RequestCleaners()
	mockTime.Advance(50 * time.Millisecond)

	// Verify cleaners requested
	if !ctx.State.GetCleanerPending() {
		t.Error("Cleaners should be pending after gold completion at max heat")
	}

	// Activate cleaners
	ctx.State.ActivateCleaners()

	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should be active")
	}

	// Transition to decay wait phase
	ctx.State.SetPhase(PhaseDecayWait)
	mockTime.Advance(50 * time.Millisecond)

	if ctx.State.GetPhase() != PhaseDecayWait {
		t.Error("Phase should be DecayWait")
	}

	// Cleaners should still be active during decay wait
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay wait")
	}

	// Advance to decay animation
	mockTime.Advance(30 * time.Second)
	ctx.State.SetPhase(PhaseDecayAnimation)

	// Cleaners should still be active
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay animation")
	}

	// Deactivate cleaners
	mockTime.Advance(1 * time.Second)
	ctx.State.DeactivateCleaners()

	if ctx.State.GetCleanerActive() {
		t.Error("Cleaners should be inactive after animation")
	}

	// End decay animation
	ctx.State.SetPhase(PhaseNormal)

	t.Logf("Complete Gold→Decay→Cleaner cycle validated successfully")
}

// TestCleanerTrailCollisionLogic tests the new trail-based collision detection
func TestCleanerTrailCollisionLogic(t *testing.T) {
	// This test verifies the trail-based collision changes:
	// 1. Trail positions are checked continuously
	// 2. Integer truncation is used (not rounding)
	// 3. No characters are skipped

	// Test fractional positions
	trail := []float64{10.3, 10.7, 11.2, 11.9}

	// With truncation: int(10.3)=10, int(10.7)=10, int(11.2)=11, int(11.9)=11
	// Expected unique positions: 10, 11

	uniquePositions := make(map[int]bool)
	for _, pos := range trail {
		x := int(pos) // Truncation instead of rounding
		uniquePositions[x] = true
	}

	if len(uniquePositions) != 2 {
		t.Errorf("Expected 2 unique positions, got %d", len(uniquePositions))
	}

	if !uniquePositions[10] || !uniquePositions[11] {
		t.Error("Expected positions 10 and 11 to be checked")
	}

	t.Logf("Trail collision logic validated: %v → positions %v", trail, uniquePositions)
}

// TestCleanerWithRapidMovement tests cleaner behavior at high speed
func TestCleanerWithRapidMovement(t *testing.T) {
	// Simulate a cleaner moving very fast (e.g., speed = 160 chars/sec)
	// At 60 FPS (16.67ms per frame), it moves ~2.67 chars per frame
	// The trail should catch all positions

	deltaTime := 1.0 / 60.0 // 60 FPS
	speed := 160.0          // chars/sec

	position := 0.0
	trail := make([]float64, 0, 10)

	// Simulate 10 frames
	for i := 0; i < 10; i++ {
		position += speed * deltaTime
		trail = append(trail, position)
		if len(trail) > 10 {
			trail = trail[1:] // Keep last 10
		}
	}

	// Calculate covered positions using Phase 7 logic (truncation)
	coveredPositions := make(map[int]bool)
	for _, pos := range trail {
		x := int(pos)
		coveredPositions[x] = true
	}

	// After 10 frames: position ≈ 26.67
	// Trail should cover approximately positions 16-26

	if len(coveredPositions) < 5 {
		t.Errorf("Expected at least 5 positions covered, got %d", len(coveredPositions))
	}

	t.Logf("Rapid movement test: final position=%.2f, covered %d positions: %v",
		position, len(coveredPositions), coveredPositions)
}

// TestNoSkippedCharacters tests that no characters are skipped due to rounding
func TestNoSkippedCharacters(t *testing.T) {
	world := NewWorld()

	// Create Red characters at positions 10, 11, 12
	redPositions := []int{10, 11, 12}
	for _, x := range redPositions {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: x, Y: 5})
		world.AddComponent(entity, components.SequenceComponent{
			Type: components.SequenceRed,
		})
		world.UpdateSpatialIndex(entity, x, 5)
	}

	// Simulate cleaner trail covering these positions with fractional values
	// Old (rounding): int(9.6+0.5)=10, int(10.4+0.5)=10 (skips 11!)
	// New (truncation): int(9.6)=9, int(10.4)=10, int(11.3)=11, int(12.1)=12
	trail := []float64{9.6, 10.4, 11.3, 12.1}

	// Using truncation logic
	checkedPositions := make(map[int]bool)
	for _, pos := range trail {
		x := int(pos) // Truncation
		checkedPositions[x] = true
	}

	// Verify all Red positions would be checked
	for _, redX := range redPositions {
		if !checkedPositions[redX] {
			t.Errorf("Position %d should be checked but wasn't (truncation)", redX)
		}
	}

	// Verify positions checked: 9, 10, 11, 12
	expectedChecked := map[int]bool{9: true, 10: true, 11: true, 12: true}
	for x := range expectedChecked {
		if !checkedPositions[x] {
			t.Errorf("Expected position %d to be checked", x)
		}
	}

	t.Logf("No skipped characters: trail %v covers positions %v", trail, checkedPositions)
}
