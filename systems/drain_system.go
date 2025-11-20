package systems

import (
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// DrainSystem manages the drain entity lifecycle
// The drain entity spawns when score > 0 and despawns when score <= 0
// Priority: 22 (after GoldSequence:20, before Decay:25)
type DrainSystem struct {
	ctx *engine.GameContext
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(ctx *engine.GameContext) *DrainSystem {
	return &DrainSystem{
		ctx: ctx,
	}
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
