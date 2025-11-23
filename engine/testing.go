package engine

// NewTestGameContext creates a minimal GameContext for testing with proper initialization
// This is a test helper that initializes the event queue and other required fields
func NewTestGameContext(gameWidth, gameHeight, screenWidth int) *GameContext {
	timeProvider := NewMonotonicTimeProvider()
	world := NewWorld()

	ctx := &GameContext{
		World:        world,
		TimeProvider: timeProvider,
		State:        NewGameState(gameWidth, gameHeight, screenWidth, timeProvider),
		GameWidth:    gameWidth,
		GameHeight:   gameHeight,
		eventQueue:   NewEventQueue(), // Critical: Initialize event queue
		Mode:         ModeNormal,
	}

	// Initialize atomic values
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)
	ctx.IsPaused.Store(false)

	return ctx
}
