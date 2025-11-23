package systems

import (
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// DrainSystem manages the drain entity lifecycle
// The drain entity spawns when score > 0 and despawns when score <= 0
// Priority: 22 (after GoldSequence:20, before Decay:25)
type DrainSystem struct {
	ctx          *engine.GameContext
	nuggetSystem *NuggetSystem
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(ctx *engine.GameContext) *DrainSystem {
	return &DrainSystem{
		ctx: ctx,
	}
}

// SetNuggetSystem sets the nugget system reference for collision handling
func (s *DrainSystem) SetNuggetSystem(nuggetSystem *NuggetSystem) {
	s.nuggetSystem = nuggetSystem
}

// Priority returns the system's priority
func (s *DrainSystem) Priority() int {
	return 25
}

// Update runs the drain system logic
// Movement is purely clock-based (DrainMoveIntervalMs), independent of input events or frame rate
func (s *DrainSystem) Update(world *engine.World, dt time.Duration) {
	score := s.ctx.State.GetScore()
	drainActive := s.ctx.State.GetDrainActive()

	// Lifecycle logic: spawn when score > 0, despawn when score <= 0
	if score > 0 && !drainActive {
		s.spawnDrain(world)
	} else if score <= 0 && drainActive {
		s.despawnDrain(world)
	}

	// Clock-based updates: movement and score drain occur on fixed intervals
	if drainActive {
		s.updateDrainMovement(world)
		s.updateScoreDrain(world)
		s.handleCollisions(world)
	}
}

// spawnDrain creates the drain entity centered on the cursor
func (s *DrainSystem) spawnDrain(world *engine.World) {
	// Check if drain is already active (double-check for safety)
	if s.ctx.State.GetDrainActive() {
		return
	}

	// Get cursor position for spawn location
	cursor := s.ctx.State.ReadCursorPosition()

	// Spawn drain centered on cursor
	spawnX := cursor.X
	spawnY := cursor.Y

	// Basic validation: ensure position is within bounds
	if spawnX < 0 {
		spawnX = 0
	}
	if spawnX >= s.ctx.GameWidth {
		spawnX = s.ctx.GameWidth - 1
	}
	if spawnY < 0 {
		spawnY = 0
	}
	if spawnY >= s.ctx.GameHeight {
		spawnY = s.ctx.GameHeight - 1
	}

	// Create drain entity
	entity := world.CreateEntity()

	// Add position component
	world.AddComponent(entity, components.PositionComponent{
		X: spawnX,
		Y: spawnY,
	})

	// Add drain component with initial state
	now := s.ctx.TimeProvider.Now()
	world.AddComponent(entity, components.DrainComponent{
		X:             spawnX,
		Y:             spawnY,
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    true, // Drain spawns centered on cursor
	})

	// Use spatial transaction to safely add drain to spatial index
	// This handles collision detection and cleanup atomically
	tx := world.BeginSpatialTransaction()
	result := tx.Spawn(entity, spawnX, spawnY)

	// If there's a collision, handle it before committing
	if result.HasCollision {
		s.handleCollisionAtPosition(world, spawnX, spawnY, result.CollidingEntity)
	}

	// Commit the spawn operation
	tx.Commit()

	// Update GameState atomics
	s.ctx.State.SetDrainActive(true)
	s.ctx.State.SetDrainEntity(uint64(entity))
	s.ctx.State.SetDrainX(spawnX)
	s.ctx.State.SetDrainY(spawnY)
}

// despawnDrain removes the drain entity
func (s *DrainSystem) despawnDrain(world *engine.World) {
	// Get drain entity ID from GameState
	entityID := s.ctx.State.GetDrainEntity()
	if entityID == 0 {
		// No drain entity to despawn
		s.ctx.State.SetDrainActive(false)
		return
	}

	entity := engine.Entity(entityID)

	// Verify entity exists and has DrainComponent
	drainType := reflect.TypeOf(components.DrainComponent{})
	if _, ok := world.GetComponent(entity, drainType); !ok {
		// Entity doesn't have drain component, clear state and return
		s.ctx.State.SetDrainActive(false)
		s.ctx.State.SetDrainEntity(0)
		return
	}

	// Destroy the drain entity
	world.DestroyEntity(entity)

	// Clear GameState atomics
	s.ctx.State.SetDrainActive(false)
	s.ctx.State.SetDrainEntity(0)
	s.ctx.State.SetDrainX(0)
	s.ctx.State.SetDrainY(0)
}

// updateDrainMovement handles purely clock-based drain movement toward cursor
// Movement occurs ONLY on DrainMoveIntervalMs intervals, independent of input events
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	// Get drain entity ID
	entityID := s.ctx.State.GetDrainEntity()
	if entityID == 0 {
		return
	}

	entity := engine.Entity(entityID)

	// Get drain component
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(entity, drainType)
	if !ok {
		return
	}

	drain := drainComp.(components.DrainComponent)

	// Purely clock-based movement: only move when interval has elapsed
	now := s.ctx.TimeProvider.Now()
	timeSinceLastMove := now.Sub(drain.LastMoveTime)
	if timeSinceLastMove < constants.DrainMoveInterval {
		// Not enough time has passed, skip movement this frame
		return
	}

	// Get current cursor position (fresh data from GameState)
	cursor := s.ctx.State.ReadCursorPosition()
	cursorX := cursor.X
	cursorY := cursor.Y

	// Calculate movement direction using Manhattan distance (8-directional)
	// If already on cursor, dx and dy will be 0 (no movement but LastMoveTime still updates)
	dx := sign(cursorX - drain.X)
	dy := sign(cursorY - drain.Y)

	// Calculate new position
	newX := drain.X + dx
	newY := drain.Y + dy

	// Boundary checks
	if newX < 0 {
		newX = 0
	}
	if newX >= s.ctx.GameWidth {
		newX = s.ctx.GameWidth - 1
	}
	if newY < 0 {
		newY = 0
	}
	if newY >= s.ctx.GameHeight {
		newY = s.ctx.GameHeight - 1
	}

	// Get current position from PositionComponent
	posType := reflect.TypeOf(components.PositionComponent{})
	posComp, ok := world.GetComponent(entity, posType)
	if !ok {
		return // No position component, can't move
	}
	pos := posComp.(components.PositionComponent)

	// Use MoveEntitySafe to atomically handle collision detection and spatial index update
	result := world.MoveEntitySafe(entity, pos.X, pos.Y, newX, newY)

	// If there's a collision, handle it
	if result.HasCollision {
		s.handleCollisionAtPosition(world, newX, newY, result.CollidingEntity)
		// Don't update position if there was a collision
		return
	}

	// Movement succeeded - update drain component position
	drain.X = newX
	drain.Y = newY
	drain.LastMoveTime = now

	// Recalculate IsOnCursor after position change using fresh cursor data
	drain.IsOnCursor = (drain.X == cursorX && drain.Y == cursorY)

	// Save updated drain component
	world.AddComponent(entity, drain)

	// Update position component
	pos.X = newX
	pos.Y = newY
	world.AddComponent(entity, pos)

	// Update GameState atomics for renderer
	s.ctx.State.SetDrainX(newX)
	s.ctx.State.SetDrainY(newY)
}

// updateScoreDrain handles score draining when drain is on cursor
func (s *DrainSystem) updateScoreDrain(world *engine.World) {
	// Get drain entity ID
	entityID := s.ctx.State.GetDrainEntity()
	if entityID == 0 {
		return
	}

	entity := engine.Entity(entityID)

	// Get drain component
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(entity, drainType)
	if !ok {
		return
	}

	drain := drainComp.(components.DrainComponent)

	// Get current cursor position (fresh data from GameState)
	cursor := s.ctx.State.ReadCursorPosition()
	cursorX := cursor.X
	cursorY := cursor.Y

	// Recalculate IsOnCursor every frame by comparing drain position with current cursor position
	isOnCursor := (drain.X == cursorX && drain.Y == cursorY)

	// Always update IsOnCursor to ensure it stays in sync (recalculated every frame)
	if drain.IsOnCursor != isOnCursor {
		drain.IsOnCursor = isOnCursor
		world.AddComponent(entity, drain)
	}

	// Drain score if on cursor and DrainScoreDrainInterval has passed
	if isOnCursor {
		now := s.ctx.TimeProvider.Now()
		if now.Sub(drain.LastDrainTime) >= constants.DrainScoreDrainInterval {
			// Drain score by the configured amount
			s.ctx.State.AddScore(-constants.DrainScoreDrainAmount)

			// Update last drain time
			drain.LastDrainTime = now
			world.AddComponent(entity, drain)
		}
	}
}

// sign returns -1, 0, or 1 depending on the sign of the input
func sign(x int) int {
	if x < 0 {
		return -1
	} else if x > 0 {
		return 1
	}
	return 0
}

// handleCollisions detects and processes collisions with entities at the drain's current position
func (s *DrainSystem) handleCollisions(world *engine.World) {
	// Get drain position from GameState
	drainX := s.ctx.State.GetDrainX()
	drainY := s.ctx.State.GetDrainY()

	// Get entity at drain position
	entity := world.GetEntityAtPosition(drainX, drainY)
	if entity == 0 {
		return // No entity at this position
	}

	// Check if entity is the drain itself
	drainEntityID := s.ctx.State.GetDrainEntity()
	if uint64(entity) == drainEntityID {
		return // Don't collide with self
	}

	// Delegate to position-based collision handler
	s.handleCollisionAtPosition(world, drainX, drainY, entity)
}

// handleCollisionAtPosition processes collision with a specific entity at a given position
// This is extracted to allow collision handling before spatial index updates
func (s *DrainSystem) handleCollisionAtPosition(world *engine.World, x, y int, entity engine.Entity) {
	// Check for nugget collision first
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(entity, nuggetType) {
		s.handleNuggetCollision(world, entity)
		return
	}

	// Check for falling decay collision
	fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
	if world.HasComponent(entity, fallingDecayType) {
		s.handleFallingDecayCollision(world, entity)
		return
	}

	// Check if entity has SequenceComponent
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		return // Not a sequence entity
	}

	seq := seqComp.(components.SequenceComponent)

	// Handle gold sequence collision
	if seq.Type == components.SequenceGold {
		s.handleGoldSequenceCollision(world, seq.ID)
		return
	}

	// Handle Blue, Green, and Red sequences
	if seq.Type == components.SequenceBlue ||
		seq.Type == components.SequenceGreen ||
		seq.Type == components.SequenceRed {

		// Convert SequenceType to GameState mapping
		// GameState expects: 0=Blue, 1=Green
		// components.SequenceType: Blue=2, Green=0
		var typeInt int
		if seq.Type == components.SequenceBlue {
			typeInt = 0
		} else if seq.Type == components.SequenceGreen {
			typeInt = 1
		} else {
			// Red sequences don't have color counters in GameState
			// Just destroy without updating counters
			world.DestroyEntity(entity)
			return
		}

		// Update color counter (decrement by 1)
		s.ctx.State.AddColorCount(typeInt, int(seq.Level), -1)

		// Destroy the entity
		world.DestroyEntity(entity)
	}
}

// handleGoldSequenceCollision removes all gold sequence entities and triggers phase transition
func (s *DrainSystem) handleGoldSequenceCollision(world *engine.World, sequenceID int) {
	// Get current gold state to verify this is the active gold sequence
	goldSnapshot := s.ctx.State.ReadGoldState()
	if !goldSnapshot.Active || goldSnapshot.SequenceID != sequenceID {
		return // Not the active gold sequence
	}

	// Find and destroy all gold sequence entities with this ID
	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		// Only destroy gold sequence entities with matching ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			world.DestroyEntity(entity)
		}
	}

	// Trigger phase transition to PhaseGoldComplete
	s.ctx.State.DeactivateGoldSequence()
}

// handleNuggetCollision destroys the nugget entity and clears active nugget state
func (s *DrainSystem) handleNuggetCollision(world *engine.World, entity engine.Entity) {
	// Clear active nugget using race-safe method
	if s.nuggetSystem != nil {
		s.nuggetSystem.ClearActiveNuggetIfMatches(uint64(entity))
	}

	// Destroy the nugget entity
	world.DestroyEntity(entity)
}

// handleFallingDecayCollision destroys the falling decay entity
// Collision with falling decay entities during decay animation
func (s *DrainSystem) handleFallingDecayCollision(world *engine.World, entity engine.Entity) {
	// Simply destroy the falling decay entity
	// This maintains decay animation continuity - other falling entities continue
	world.DestroyEntity(entity)
}