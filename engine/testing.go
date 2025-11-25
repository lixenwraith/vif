package engine

import "github.com/lixenwraith/vi-fighter/components"

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

	// Create cursor entity (singleton, protected) - required after Phase 2 migration
	ctx.CursorEntity = With(
		WithPosition(
			ctx.World.NewEntity(),
			ctx.World.Positions,
			components.PositionComponent{
				X: gameWidth / 2,
				Y: gameHeight / 2,
			},
		),
		ctx.World.Cursors,
		components.CursorComponent{},
	).Build()

	// Make cursor indestructible
	ctx.World.Protections.Add(ctx.CursorEntity, components.ProtectionComponent{
		Mask:      components.ProtectAll,
		ExpiresAt: 0, // Permanent
	})

	// Initialize cursor cache (synced with ECS)
	if pos, ok := ctx.World.Positions.Get(ctx.CursorEntity); ok {
		ctx.CursorX = pos.X
		ctx.CursorY = pos.Y
	}

	// Initialize atomic values
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)
	ctx.IsPaused.Store(false)

	return ctx
}
