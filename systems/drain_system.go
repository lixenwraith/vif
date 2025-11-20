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
	return 22 // After GoldSequence:20, before Decay:25
}

// Update runs the drain system logic
func (s *DrainSystem) Update(world *engine.World, dt time.Duration) {
	score := s.ctx.State.GetScore()
	drainActive := s.ctx.State.GetDrainActive()

	// Lifecycle logic: spawn when score > 0, despawn when score <= 0
	if score > 0 && !drainActive {
		s.spawnDrain(world)
	} else if score <= 0 && drainActive {
		s.despawnDrain(world)
	}

	// Movement logic: move drain toward cursor every 250ms
	if drainActive {
		s.updateDrainMovement(world)
		s.updateScoreDrain(world)
		s.handleCollisions(world)
	}
}

// spawnDrain creates the drain entity at a random position
func (s *DrainSystem) spawnDrain(world *engine.World) {
	// Check if drain is already active (double-check for safety)
	if s.ctx.State.GetDrainActive() {
		return
	}

	// Get cursor position for spawn exclusion zone
	cursor := s.ctx.State.ReadCursorPosition()

	// Find a spawn position that is not at cursor and not occupied
	// For simplicity, spawn at top-left corner (0, 0) if available
	// Later parts will improve this logic
	spawnX := 0
	spawnY := 0

	// Basic validation: ensure position is within bounds
	if spawnX < 0 || spawnX >= s.ctx.GameWidth || spawnY < 0 || spawnY >= s.ctx.GameHeight {
		// Invalid position, use safe default
		spawnX = 0
		spawnY = 0
	}

	// Avoid spawning directly on cursor
	if spawnX == cursor.X && spawnY == cursor.Y {
		// Try alternative position
		if spawnX+1 < s.ctx.GameWidth {
			spawnX++
		} else if spawnY+1 < s.ctx.GameHeight {
			spawnY++
		}
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
		IsOnCursor:    false,
	})

	// Update spatial index
	world.UpdateSpatialIndex(entity, spawnX, spawnY)

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
	world.SafeDestroyEntity(entity)

	// Clear GameState atomics
	s.ctx.State.SetDrainActive(false)
	s.ctx.State.SetDrainEntity(0)
	s.ctx.State.SetDrainX(0)
	s.ctx.State.SetDrainY(0)
}

// updateDrainMovement handles drain movement toward cursor every 250ms
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

	// Check if 250ms has passed since last move
	now := s.ctx.TimeProvider.Now()
	if now.Sub(drain.LastMoveTime) < constants.DrainMoveInterval {
		return
	}

	// Get cursor position
	cursor := s.ctx.State.ReadCursorPosition()

	// Calculate movement direction using Manhattan distance (8-directional)
	dx := sign(cursor.X - drain.X)
	dy := sign(cursor.Y - drain.Y)

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

	// Update drain component
	drain.X = newX
	drain.Y = newY
	drain.LastMoveTime = now
	world.AddComponent(entity, drain)

	// Update position component
	posType := reflect.TypeOf(components.PositionComponent{})
	if posComp, ok := world.GetComponent(entity, posType); ok {
		pos := posComp.(components.PositionComponent)

		// Remove from old spatial index position
		world.RemoveFromSpatialIndex(pos.X, pos.Y)

		// Update position
		pos.X = newX
		pos.Y = newY
		world.AddComponent(entity, pos)

		// Update spatial index with new position
		world.UpdateSpatialIndex(entity, newX, newY)
	}

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

	// Get cursor position
	cursor := s.ctx.State.ReadCursorPosition()

	// Check if drain is on cursor
	isOnCursor := (drain.X == cursor.X && drain.Y == cursor.Y)

	// Update IsOnCursor state if changed
	if drain.IsOnCursor != isOnCursor {
		drain.IsOnCursor = isOnCursor
		world.AddComponent(entity, drain)
	}

	// Drain score if on cursor and 250ms has passed
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

// handleCollisions detects and processes collisions with entities at the drain's position
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

	// Part 6: Check for nugget collision first
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(entity, nuggetType) {
		s.handleNuggetCollision(world, entity)
		return
	}

	// Check if entity has SequenceComponent
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		return // Not a sequence entity
	}

	seq := seqComp.(components.SequenceComponent)

	// Part 6: Handle gold sequence collision
	if seq.Type == components.SequenceGold {
		s.handleGoldSequenceCollision(world, seq.ID)
		return
	}

	// Part 5: Handle Blue, Green, and Red sequences
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
			world.SafeDestroyEntity(entity)
			return
		}

		// Update color counter (decrement by 1)
		s.ctx.State.AddColorCount(typeInt, int(seq.Level), -1)

		// Destroy the entity
		world.SafeDestroyEntity(entity)
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
			world.SafeDestroyEntity(entity)
		}
	}

	// Check current phase - if cleaners are pending, don't deactivate gold
	phaseSnapshot := s.ctx.State.ReadPhaseState()
	if phaseSnapshot.Phase == engine.PhaseCleanerPending || phaseSnapshot.Phase == engine.PhaseCleanerActive {
		// Cleaners are pending/active - gold entities removed, but stay in cleaner phase
		return
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
	world.SafeDestroyEntity(entity)
}
