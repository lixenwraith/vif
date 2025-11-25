package systems

import (
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

// spawnDrain creates the drain entity centered on the cursor using generic stores
func (s *DrainSystem) spawnDrain(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Check if drain is already active (double-check for safety)
	if s.ctx.State.GetDrainActive() {
		return
	}

	// Read cursor position directly from ECS (Source of Truth)
	// instead of stale GameState atomics
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return // Should never happen if CursorEntity is initialized correctly
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
	now := timeRes.GameTime
	drain := components.DrainComponent{
		X:             spawnX,
		Y:             spawnY,
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    true, // Drain spawns centered on cursor
	}

	// Check if position is occupied and handle collision
	collidingEntity := world.Positions.GetEntityAt(spawnX, spawnY)
	// TODO: (2-second) delayed spawn on score > 0, with spawn animation
	// Do not trigger collision logic against the cursor itself (Drain spawns on top of it)
	if collidingEntity != 0 && collidingEntity != s.ctx.CursorEntity {
		s.handleCollisionAtPosition(world, spawnX, spawnY, collidingEntity)
	}

	// Add components (position first for spatial index)
	world.Positions.Add(entity, pos)
	world.Drains.Add(entity, drain)

	// Update GameState atomics
	s.ctx.State.SetDrainActive(true)
	s.ctx.State.SetDrainEntity(uint64(entity))
	s.ctx.State.SetDrainX(spawnX)
	s.ctx.State.SetDrainY(spawnY)
}

// despawnDrain removes the drain entity using generic stores
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
	if !world.Drains.Has(entity) {
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

// updateDrainMovement handles purely clock-based drain movement toward cursor using generic stores
// Movement occurs ONLY on DrainMoveIntervalMs intervals, independent of input events
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Get drain entity ID
	entityID := s.ctx.State.GetDrainEntity()
	if entityID == 0 {
		return
	}

	entity := engine.Entity(entityID)

	// Get drain component
	drain, ok := world.Drains.Get(entity)
	if !ok {
		return
	}

	// Purely clock-based movement: only move when interval has elapsed
	now := timeRes.GameTime
	timeSinceLastMove := now.Sub(drain.LastMoveTime)
	if timeSinceLastMove < constants.DrainMoveInterval {
		// Not enough time has passed, skip movement this frame
		return
	}

	// Read cursor position directly from ECS
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}
	cursorX := cursorPos.X
	cursorY := cursorPos.Y

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
	if newX >= config.GameWidth {
		newX = config.GameWidth - 1
	}
	if newY < 0 {
		newY = 0
	}
	if newY >= config.GameHeight {
		newY = config.GameHeight - 1
	}

	// Get current position from PositionComponent
	pos, ok := world.Positions.Get(entity)
	if !ok {
		return // No position component, can't move
	}

	// Check for collision at new position
	collidingEntity := world.Positions.GetEntityAt(newX, newY)

	// FIX: Allow moving onto the Cursor Entity
	// We check if the colliding entity exists, is not self, AND is not the cursor.
	if collidingEntity != 0 && collidingEntity != entity && collidingEntity != s.ctx.CursorEntity {
		s.handleCollisionAtPosition(world, newX, newY, collidingEntity)
		// Don't update position if there was a blocking collision
		return
	}

	// Movement succeeded - update components
	drain.X = newX
	drain.Y = newY
	drain.LastMoveTime = now

	// Recalculate IsOnCursor after position change using fresh cursor data
	drain.IsOnCursor = drain.X == cursorX && drain.Y == cursorY

	// Update position (this handles spatial index)
	pos.X = newX
	pos.Y = newY
	world.Positions.Add(entity, pos)

	// Save updated drain component
	world.Drains.Add(entity, drain)

	// Update GameState atomics for renderer
	s.ctx.State.SetDrainX(newX)
	s.ctx.State.SetDrainY(newY)
}

// updateScoreDrain handles score draining when drain is on cursor using generic stores
func (s *DrainSystem) updateScoreDrain(world *engine.World) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)

	// Get drain entity ID
	entityID := s.ctx.State.GetDrainEntity()
	if entityID == 0 {
		return
	}

	entity := engine.Entity(entityID)

	// Get drain component
	drain, ok := world.Drains.Get(entity)
	if !ok {
		return
	}

	// FIX: Read cursor position directly from ECS
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}
	cursorX := cursorPos.X
	cursorY := cursorPos.Y

	// Recalculate IsOnCursor every frame by comparing drain position with current cursor position
	isOnCursor := (drain.X == cursorX && drain.Y == cursorY)

	// Always update IsOnCursor to ensure it stays in sync (recalculated every frame)
	if drain.IsOnCursor != isOnCursor {
		drain.IsOnCursor = isOnCursor
		world.Drains.Add(entity, drain)
	}

	// Drain score if on cursor and DrainScoreDrainInterval has passed
	if isOnCursor {
		now := timeRes.GameTime
		if now.Sub(drain.LastDrainTime) >= constants.DrainScoreDrainInterval {
			// Drain score by the configured amount
			s.ctx.State.AddScore(-constants.DrainScoreDrainAmount)

			// Update last drain time
			drain.LastDrainTime = now
			world.Drains.Add(entity, drain)
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

// handleCollisions detects and processes collisions with entities at the drain's current position using generic stores
func (s *DrainSystem) handleCollisions(world *engine.World) {

	// Get drain position from GameState
	drainX := s.ctx.State.GetDrainX()
	drainY := s.ctx.State.GetDrainY()

	// Get entity at drain position
	entity := world.Positions.GetEntityAt(drainX, drainY)
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

// handleCollisionAtPosition processes collision with a specific entity at a given position using generic stores
// This is extracted to allow collision handling before spatial index updates
func (s *DrainSystem) handleCollisionAtPosition(world *engine.World, x, y int, entity engine.Entity) {

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
		s.handleGoldSequenceCollision(world, seq.ID)
		return
	}

	// Handle Blue, Green, and Red sequences
	if seq.Type == components.SequenceBlue ||
		seq.Type == components.SequenceGreen ||
		seq.Type == components.SequenceRed {

		// Destroy the entity using generic world
		world.DestroyEntity(entity)
	}
}

// handleGoldSequenceCollision removes all gold sequence entities and triggers phase transition using generic stores
func (s *DrainSystem) handleGoldSequenceCollision(world *engine.World, sequenceID int) {

	// Get current gold state to verify this is the active gold sequence
	goldSnapshot := s.ctx.State.ReadGoldState()
	if !goldSnapshot.Active || goldSnapshot.SequenceID != sequenceID {
		return // Not the active gold sequence
	}

	// Find and destroy all gold sequence entities with this ID
	entities := world.Sequences.All()

	for _, entity := range entities {
		seq, ok := world.Sequences.Get(entity)
		if !ok {
			continue
		}

		// Only destroy gold sequence entities with matching ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			world.DestroyEntity(entity)
		}
	}

	// Trigger phase transition to PhaseGoldComplete
	s.ctx.State.DeactivateGoldSequence()
}

// handleNuggetCollision destroys the nugget entity and clears active nugget state using generic stores
func (s *DrainSystem) handleNuggetCollision(world *engine.World, entity engine.Entity) {

	// Clear active nugget using race-safe method
	if s.nuggetSystem != nil {
		s.nuggetSystem.ClearActiveNuggetIfMatches(uint64(entity))
	}

	// Destroy the nugget entity
	world.DestroyEntity(entity)
}

// handleFallingDecayCollision destroys the falling decay entity using generic stores
// Collision with falling decay entities during decay animation
func (s *DrainSystem) handleFallingDecayCollision(world *engine.World, entity engine.Entity) {

	// Simply destroy the falling decay entity
	// This maintains decay animation continuity - other falling entities continue
	world.DestroyEntity(entity)
}