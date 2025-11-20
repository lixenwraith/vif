package systems

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_IntegrationWithScoreSystem tests drain system integration with score system
// This test verifies that:
// 1. Score can be earned through typing (ScoreSystem)
// 2. Score can be drained when drain is on cursor (DrainSystem)
// 3. Both systems work together correctly
func TestDrainSystem_IntegrationWithScoreSystem(t *testing.T) {
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(80, 24, 80, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
		Width:        80,
		Height:       24,
		CursorX:      0,
		CursorY:      0,
	}

	// Create systems
	drainSys := NewDrainSystem(ctx)

	// Initial score is 0
	if ctx.State.GetScore() != 0 {
		t.Fatalf("Expected initial score 0, got %d", ctx.State.GetScore())
	}

	// No drain should be active at score 0
	if ctx.State.GetDrainActive() {
		t.Error("Drain should not be active when score is 0")
	}

	// Add some score (simulating user earning points)
	earnedScore := 50
	ctx.State.SetScore(earnedScore)

	// Update drain system - should spawn drain
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should now be active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active when score > 0")
	}

	// Verify score is still 50
	if ctx.State.GetScore() != earnedScore {
		t.Errorf("Score should be %d, got %d", earnedScore, ctx.State.GetScore())
	}

	// Move cursor to drain position to trigger draining
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Update drain system (should update IsOnCursor state)
	drainSys.Update(world, 16*time.Millisecond)

	// Advance time by drain interval
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should have decreased by DrainScoreDrainAmount
	expectedScore := earnedScore - constants.DrainScoreDrainAmount
	actualScore := ctx.State.GetScore()
	if actualScore != expectedScore {
		t.Errorf("Expected score %d after first drain, got %d", expectedScore, actualScore)
	}

	// Add more score (user earning while drain is active)
	additionalScore := 30
	ctx.State.AddScore(additionalScore)

	currentScore := ctx.State.GetScore()
	expectedCurrentScore := expectedScore + additionalScore
	if currentScore != expectedCurrentScore {
		t.Errorf("Expected score %d after earning more, got %d",
			expectedCurrentScore, currentScore)
	}

	// Move cursor away from drain
	ctx.State.SetCursorX(drainX + 10)
	ctx.State.SetCursorY(drainY + 10)

	// Advance time again
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should NOT have changed (drain not on cursor)
	if ctx.State.GetScore() != expectedCurrentScore {
		t.Errorf("Score should not change when drain is not on cursor, got %d, expected %d",
			ctx.State.GetScore(), expectedCurrentScore)
	}

	// Move cursor back to drain
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Let drain catch up to cursor (it might have moved while cursor was away)
	// Run movement updates until drain reaches cursor
	maxMoves := 20
	for i := 0; i < maxMoves; i++ {
		mockTime.Advance(constants.DrainMoveInterval)
		drainSys.Update(world, 16*time.Millisecond)

		currentDrainX := ctx.State.GetDrainX()
		currentDrainY := ctx.State.GetDrainY()

		if currentDrainX == drainX && currentDrainY == drainY {
			break
		}
	}

	// Now drain should be on cursor again, advance time for drain
	beforeDrainScore := ctx.State.GetScore()
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should have decreased again
	afterDrainScore := ctx.State.GetScore()
	expectedDecrease := constants.DrainScoreDrainAmount
	actualDecrease := beforeDrainScore - afterDrainScore

	if actualDecrease != expectedDecrease {
		t.Errorf("Expected score to decrease by %d, but decreased by %d (before: %d, after: %d)",
			expectedDecrease, actualDecrease, beforeDrainScore, afterDrainScore)
	}
}

// TestDrainSystem_ScoreDrainSpawnDespawnCycle tests the full lifecycle with score changes
func TestDrainSystem_ScoreDrainSpawnDespawnCycle(t *testing.T) {
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(80, 24, 80, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
		Width:        80,
		Height:       24,
		CursorX:      0,
		CursorY:      0,
	}

	drainSys := NewDrainSystem(ctx)

	// Start with score 0 - no drain
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should not spawn at score 0")
	}

	// Earn some score - drain should spawn
	ctx.State.SetScore(50)
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should spawn when score > 0")
	}

	// Position cursor on drain
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Drain score to 0
	maxDrainTicks := 100 // Safety limit
	for i := 0; i < maxDrainTicks; i++ {
		currentScore := ctx.State.GetScore()
		if currentScore <= 0 {
			break
		}

		mockTime.Advance(constants.DrainScoreDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)
	}

	// Score should be at or below 0
	if ctx.State.GetScore() > 0 {
		t.Errorf("Expected score to reach 0, got %d", ctx.State.GetScore())
	}

	// Drain should despawn on next update
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should despawn when score <= 0")
	}

	// Add score again - drain should respawn
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should respawn when score > 0 again")
	}
}

// TestDrainSystem_ContinuousDrainUntilZero tests draining until score reaches zero
func TestDrainSystem_ContinuousDrainUntilZero(t *testing.T) {
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(80, 24, 80, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
		Width:        80,
		Height:       24,
		CursorX:      0,
		CursorY:      0,
	}

	drainSys := NewDrainSystem(ctx)

	// Set initial score
	initialScore := 45
	ctx.State.SetScore(initialScore)

	// Spawn drain
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active")
	}

	// Position cursor on drain
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Calculate expected number of drain ticks to reach 0
	expectedTicks := (initialScore + constants.DrainScoreDrainAmount - 1) / constants.DrainScoreDrainAmount
	actualTicks := 0

	// Drain continuously
	for actualTicks < expectedTicks+5 { // Add buffer for safety
		if ctx.State.GetScore() <= 0 {
			break
		}

		mockTime.Advance(constants.DrainScoreDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)
		actualTicks++
	}

	// Verify score reached 0 or negative
	finalScore := ctx.State.GetScore()
	if finalScore > 0 {
		t.Errorf("Expected score to be <= 0, got %d", finalScore)
	}

	// Verify number of ticks matches expectation
	if actualTicks < expectedTicks {
		t.Errorf("Expected at least %d drain ticks, got %d", expectedTicks, actualTicks)
	}

	// Next update should despawn drain
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should be despawned after score reaches 0")
	}
}

// TestDrainSystem_AlternatingScoreChanges tests score increasing and decreasing
func TestDrainSystem_AlternatingScoreChanges(t *testing.T) {
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(80, 24, 80, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
		Width:        80,
		Height:       24,
		CursorX:      0,
		CursorY:      0,
	}

	drainSys := NewDrainSystem(ctx)

	// Start with score
	ctx.State.SetScore(50)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active")
	}

	// Get drain position and move cursor there
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Drain some score
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	score1 := ctx.State.GetScore()
	if score1 != 40 { // 50 - 10
		t.Errorf("Expected score 40, got %d", score1)
	}

	// Add score (simulate user earning points)
	ctx.State.AddScore(20)
	score2 := ctx.State.GetScore()
	if score2 != 60 { // 40 + 20
		t.Errorf("Expected score 60, got %d", score2)
	}

	// Drain again
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	score3 := ctx.State.GetScore()
	if score3 != 50 { // 60 - 10
		t.Errorf("Expected score 50, got %d", score3)
	}

	// Add more score
	ctx.State.AddScore(30)
	score4 := ctx.State.GetScore()
	if score4 != 80 { // 50 + 30
		t.Errorf("Expected score 80, got %d", score4)
	}

	// Verify drain is still active
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should remain active while score > 0")
	}
}
