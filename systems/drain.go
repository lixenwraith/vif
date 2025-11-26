package systems

import (
	"cmp"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// DrainSystem manages the drain entity lifecycle
// The drain entity spawns when energy > 0 and despawns when energy <= 0
// Priority: 25 (after CleanerSystem:22, before DecaySystem:30)
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
	energy := s.ctx.State.GetEnergy()

	// TODO: need better lifecycle management, prep for ember
	drainActive := world.Drains.Count() > 0
	// Lifecycle logic: spawn when energy > 0, despawn when energy <= 0
	if energy > 0 && !drainActive {
		s.spawnDrain(world)
	} else if energy <= 0 && drainActive {
		s.despawnDrain(world)
	}

	// Clock-based updates: movement and energy drain occur on fixed intervals
	if drainActive {
		s.updateDrainMovement(world)
		s.updateEnergyDrain(world)
		s.handleCollisions(world)
	}
}

// spawnDrain creates the drain entity centered on the cursor using generic stores
func (s *DrainSystem) spawnDrain(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read cursor position
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	spawnX := cursorPos.X
	spawnY := cursorPos.Y

	// Basic validation: ensure position is within bounds
	if spawnX < 0 {
		spawnX = 0
	}
	if spawnX >= config.GameWidth {
		spawnX = config.GameWidth - 1
	}
	if spawnY < 0 {
		spawnY = 0
	}
	if spawnY >= config.GameHeight {
		spawnY = config.GameHeight - 1
	}

	// Create drain entity
	entity := world.CreateEntity()

	pos := components.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	// Add drain component with initial state
	drain := components.DrainComponent{
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    true, // Drain spawns centered on cursor
	}

	// Check if position is occupied and handle collision
	collidingEntity := world.Positions.GetEntityAt(spawnX, spawnY)
	// TODO: (2-second) delayed spawn, with spawn animation: materialize system
	// TODO: this check seems unnecessary, cursor is protected entity, check iteration on multiple overlaps (cursor+sequence+decay) that it destroys them all correctly
	// Do not trigger collision logic against the cursor itself (Drain currently spawns on top of it with no delay)
	if collidingEntity != 0 && collidingEntity != s.ctx.CursorEntity {
		s.handleCollisionAtPosition(world, collidingEntity)
	}

	// Add components (position first for spatial index)
	world.Positions.Add(entity, pos)
	world.Drains.Add(entity, drain)
}

// despawnDrain removes the drain entity using generic stores
func (s *DrainSystem) despawnDrain(world *engine.World) {
	drains := world.Drains.All()
	for _, e := range drains {
		world.DestroyEntity(e)
	}
}

// updateDrainMovement handles purely clock-based drain movement toward cursor using generic stores
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Get and iterate on all drains
	drainEntities := world.Drains.All()
	for _, drainEntity := range drainEntities {
		drain, ok := world.Drains.Get(drainEntity)
		if !ok {
			continue
		}

		// Purely clock-based movement: only move when interval has elapsed
		timeSinceLastMove := now.Sub(drain.LastMoveTime)
		if timeSinceLastMove < constants.DrainMoveInterval {
			// Not enough time has passed, skip movement this frame and check the others
			continue
		}

		// Read cursor position
		cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
		if !ok {
			panic(fmt.Errorf("cursor destroyed"))
		}

		// Get current position
		drainPos, ok := world.Positions.Get(drainEntity)
		if !ok {
			panic(fmt.Errorf("drain destroyed"))
		}

		// Calculate movement direction using Manhattan distance (8-directional)
		// If already on cursor, dx and dy will be 0 (no movement but LastMoveTime still updates)
		dx := cmp.Compare(cursorPos.X, drainPos.X)
		dy := cmp.Compare(cursorPos.Y, drainPos.Y)

		// Calculate new position
		newX := drainPos.X + dx
		newY := drainPos.Y + dy

		// Boundary checks
		if newX < 0 {
			newX = 0
		}
		if newX >= config.GameWidth {
			newX = config.GameWidth - 1
		}
		if newY < 0 {
			newY = 0
		}
		if newY >= config.GameHeight {
			newY = config.GameHeight - 1
		}

		// Check for collision at new position
		collidingEntity := world.Positions.GetEntityAt(newX, newY)

		// FIX: Allow moving onto the Cursor Entity
		// We check if the colliding entity exists, is not self, AND is not the cursor.
		if collidingEntity != 0 && collidingEntity != drainEntity && collidingEntity != s.ctx.CursorEntity {
			s.handleCollisionAtPosition(world, collidingEntity)
			// Don't update position if there was a blocking collision
			return
		}

		// Movement succeeded - update components
		drainPos.X = newX
		drainPos.Y = newY
		drain.LastMoveTime = now

		// Recalculate IsOnCursor after position change using fresh cursor data
		drain.IsOnCursor = drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Update position (this handles spatial index)
		drainPos.X = newX
		drainPos.Y = newY
		world.Positions.Add(drainEntity, drainPos)

		// Save updated drain component
		world.Drains.Add(drainEntity, drain)
	}
}

// updateEnergyDrain handles energy draining when drain is on cursor using generic stores
func (s *DrainSystem) updateEnergyDrain(world *engine.World) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Get and iterate on all drains
	drainEntities := world.Drains.All()
	for _, drainEntity := range drainEntities {
		drain, ok := world.Drains.Get(drainEntity)
		if !ok {
			continue
		}

		// Read cursor position
		cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
		if !ok {
			panic(fmt.Errorf("cursor destroyed"))
		}

		// Get current position
		drainPos, ok := world.Positions.Get(drainEntity)
		if !ok {
			panic(fmt.Errorf("drain destroyed"))
		}

		// Recalculate IsOnCursor every frame by comparing drain position with current cursor position
		isOnCursor := drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Always update IsOnCursor to ensure it stays in sync (recalculated every frame)
		if drain.IsOnCursor != isOnCursor {
			drain.IsOnCursor = isOnCursor
			world.Drains.Add(drainEntity, drain)
		}

		// Drain energy if on cursor and DrainEnergyDrainInterval has passed
		if isOnCursor {
			now := timeRes.GameTime
			if now.Sub(drain.LastDrainTime) >= constants.DrainEnergyDrainInterval {
				// Drain energy by the configured amount
				s.ctx.State.AddEnergy(-constants.DrainEnergyDrainAmount)

				// Update last drain time
				drain.LastDrainTime = now
				world.Drains.Add(drainEntity, drain)
			}
		}
	}
}

// handleCollisions detects and processes collisions with entities at the drain's current position using generic stores
func (s *DrainSystem) handleCollisions(world *engine.World) {
	// Get and iterate on all drains
	entities := world.Drains.All()
	for _, entity := range entities {
		// Get current position
		drainPos, ok := world.Positions.Get(entity)
		if !ok {
			panic(fmt.Errorf("drain destroyed"))
		}

		// Check for entity at drain position
		target := world.Positions.GetEntityAt(drainPos.X, drainPos.Y)
		if target == 0 || target == entity {
			continue
		}

		// Delegate to position-based collision handler
		s.handleCollisionAtPosition(world, target)
	}
}

// handleCollisionAtPosition processes collision with a specific entity at a given position using generic stores
// This is extracted to allow collision handling before spatial index updates
func (s *DrainSystem) handleCollisionAtPosition(world *engine.World, entity engine.Entity) {
	// Check protection before any collision handling
	if prot, ok := world.Protections.Get(entity); ok {
		if prot.Mask.Has(components.ProtectFromDrain) || prot.Mask == components.ProtectAll {
			return
		}
	}

	// Check for nugget collision first
	if world.Nuggets.Has(entity) {
		s.handleNuggetCollision(world, entity)
		return
	}

	// Check for falling decay collision
	if world.FallingDecays.Has(entity) {
		s.handleFallingDecayCollision(world, entity)
		return
	}

	// Check if entity has SequenceComponent
	seq, ok := world.Sequences.Get(entity)
	if !ok {
		return // Not a sequence entity
	}

	// Handle gold sequence collision
	if seq.Type == components.SequenceGold {
		s.handleGoldSequenceCollision(world, entity, seq.ID)
		return
	}

	// Handle Blue, Green, and Red sequences
	if seq.Type == components.SequenceBlue ||
		seq.Type == components.SequenceGreen ||
		seq.Type == components.SequenceRed {

		// Destroy the spawn character entity
		world.DestroyEntity(entity)
	}
}

// handleGoldSequenceCollision removes all gold sequence entities and triggers phase transition using generic stores
func (s *DrainSystem) handleGoldSequenceCollision(world *engine.World, entity engine.Entity, sequenceID int) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Get current gold state to verify this is the active gold sequence
	goldSnapshot := s.ctx.State.ReadGoldState(now)
	if !goldSnapshot.Active || goldSnapshot.SequenceID != sequenceID {
		world.DestroyEntity(entity)
		return // Not the active gold sequence
	}

	// Find and destroy all gold sequence entities with this ID
	goldSequenceEntities := world.Sequences.All()
	for _, goldSequenceEntity := range goldSequenceEntities {
		seq, ok := world.Sequences.Get(goldSequenceEntity)
		if !ok {
			continue // Already destroyed or typed
		}

		// Only destroy gold sequence entities with matching ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			world.DestroyEntity(goldSequenceEntity)
		}
	}

	// Trigger phase transition to PhaseGoldComplete
	s.ctx.State.DeactivateGoldSequence(now)
}

// handleNuggetCollision destroys the nugget entity and clears active nugget state using generic stores
func (s *DrainSystem) handleNuggetCollision(world *engine.World, entity engine.Entity) {

	// Clear active nugget
	if s.nuggetSystem != nil {
		s.nuggetSystem.ClearActiveNuggetIfMatches(entity)
	}

	// Destroy the nugget entity
	world.DestroyEntity(entity)
}

// handleFallingDecayCollision destroys the entity colliding with falling decay
func (s *DrainSystem) handleFallingDecayCollision(world *engine.World, entity engine.Entity) {
	world.DestroyEntity(entity)
}