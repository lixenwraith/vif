package systems

import (
	"cmp"
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// DrainSystem manages the drain entity lifecycle
// The drain entity spawns when energy > 0 and despawns when energy <= 0
// Priority: 25 (after CleanerSystem:22, before DecaySystem:30)
type DrainSystem struct {
	ctx                *engine.GameContext
	nuggetSystem       *NuggetSystem
	materializeActive  bool
	materializeTargetX int
	materializeTargetY int
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
	return constants.PriorityDrain
}

// Update runs the drain system logic
// Movement is purely clock-based (DrainMoveIntervalMs), independent of input events or frame rate
func (s *DrainSystem) Update(world *engine.World, dt time.Duration) {
	energy := s.ctx.State.GetEnergy()

	// TODO: need better lifecycle management, prep for ember
	drainActive := world.Drains.Count() > 0
	materializersExist := world.Materializers.Count() > 0
	// drainActive := world.Drains.Count() > 50 // Stress test spatial grid
	// Lifecycle logic: spawn when energy > 0, despawn when energy <= 0
	if energy > 0 && !drainActive && !s.materializeActive && !materializersExist {
		s.startMaterialize(world)
	} else if energy <= 0 && drainActive {
		s.despawnDrain(world)
	}

	// Update materialize animation if active
	if materializersExist {
		s.updateMaterializers(world, dt)
	}

	// Clock-based updates: movement and energy drain occur on fixed intervals
	if drainActive {
		s.updateDrainMovement(world)
		s.updateEnergyDrain(world)
		s.handleCollisions(world)
	}
}

// despawnDrain removes the drain entity
func (s *DrainSystem) despawnDrain(world *engine.World) {
	drains := world.Drains.All()
	for _, e := range drains {
		world.DestroyEntity(e)
	}
}

// startMaterialize initiates the materialize animation by spawning 4 converging spawner entities
func (s *DrainSystem) startMaterialize(world *engine.World) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	// Lock target position
	s.materializeTargetX = cursorPos.X
	s.materializeTargetY = cursorPos.Y
	s.materializeActive = true

	// Clamp to bounds
	if s.materializeTargetX < 0 {
		s.materializeTargetX = 0
	}
	if s.materializeTargetX >= config.GameWidth {
		s.materializeTargetX = config.GameWidth - 1
	}
	if s.materializeTargetY < 0 {
		s.materializeTargetY = 0
	}
	if s.materializeTargetY >= config.GameHeight {
		s.materializeTargetY = config.GameHeight - 1
	}

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	targetX := float64(s.materializeTargetX)
	targetY := float64(s.materializeTargetY)
	duration := constants.MaterializeAnimationDuration.Seconds()

	type spawnerDef struct {
		startX, startY float64
		dir            components.MaterializeDirection
	}

	spawners := []spawnerDef{
		{targetX, -1, components.MaterializeFromTop},
		{targetX, gameHeight, components.MaterializeFromBottom},
		{-1, targetY, components.MaterializeFromLeft},
		{gameWidth, targetY, components.MaterializeFromRight},
	}

	for _, def := range spawners {
		velX := (targetX - def.startX) / duration
		velY := (targetY - def.startY) / duration

		comp := components.MaterializeComponent{
			PreciseX:  def.startX,
			PreciseY:  def.startY,
			VelocityX: velX,
			VelocityY: velY,
			TargetX:   s.materializeTargetX,
			TargetY:   s.materializeTargetY,
			GridX:     int(def.startX),
			GridY:     int(def.startY),
			Trail:     []core.Point{{X: int(def.startX), Y: int(def.startY)}},
			Direction: def.dir,
			Char:      constants.MaterializeChar,
			Arrived:   false,
		}

		entity := world.CreateEntity()
		world.Materializers.Add(entity, comp)
	}
}

// updateMaterializers updates the materialize spawner entities and triggers drain spawn when all converge
func (s *DrainSystem) updateMaterializers(world *engine.World, dt time.Duration) {
	dtSeconds := dt.Seconds()
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	entities := world.Materializers.All()
	allArrived := true

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		if mat.Arrived {
			continue
		}

		mat.PreciseX += mat.VelocityX * dtSeconds
		mat.PreciseY += mat.VelocityY * dtSeconds

		arrived := false
		switch mat.Direction {
		case components.MaterializeFromTop:
			arrived = mat.PreciseY >= float64(mat.TargetY)
		case components.MaterializeFromBottom:
			arrived = mat.PreciseY <= float64(mat.TargetY)
		case components.MaterializeFromLeft:
			arrived = mat.PreciseX >= float64(mat.TargetX)
		case components.MaterializeFromRight:
			arrived = mat.PreciseX <= float64(mat.TargetX)
		}

		if arrived {
			mat.PreciseX = float64(mat.TargetX)
			mat.PreciseY = float64(mat.TargetY)
			mat.Arrived = true
		} else {
			allArrived = false
		}

		newGridX := int(mat.PreciseX)
		newGridY := int(mat.PreciseY)

		if newGridX != mat.GridX || newGridY != mat.GridY {
			mat.GridX = newGridX
			mat.GridY = newGridY

			newPoint := core.Point{X: mat.GridX, Y: mat.GridY}
			oldLen := len(mat.Trail)
			newLen := oldLen + 1
			if newLen > constants.MaterializeTrailLength {
				newLen = constants.MaterializeTrailLength
			}

			newTrail := make([]core.Point, newLen)
			newTrail[0] = newPoint
			copyLen := newLen - 1
			if copyLen > 0 {
				copy(newTrail[1:], mat.Trail[:copyLen])
			}
			mat.Trail = newTrail
		}

		world.Materializers.Add(entity, mat)
	}

	if allArrived && len(entities) > 0 {
		for _, entity := range entities {
			world.DestroyEntity(entity)
		}
		s.materializeActive = false
		s.materializeDrain(world)
	}
}

// materializeDrain creates the drain entity at the locked materialize target position
func (s *DrainSystem) materializeDrain(world *engine.World) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	spawnX := s.materializeTargetX
	spawnY := s.materializeTargetY

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

	entity := world.CreateEntity()

	pos := components.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	drain := components.DrainComponent{
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    spawnX == cursorPos.X && spawnY == cursorPos.Y,
	}

	// Handle collisions at spawn position
	entitiesAtSpawn := world.Positions.GetAllAt(spawnX, spawnY)
	var toProcess []engine.Entity
	for _, e := range entitiesAtSpawn {
		if e != s.ctx.CursorEntity {
			toProcess = append(toProcess, e)
		}
	}
	for _, e := range toProcess {
		s.handleCollisionAtPosition(world, e)
	}

	world.Positions.Add(entity, pos)
	world.Drains.Add(entity, drain)
}

// updateDrainMovement handles purely clock-based drain movement toward cursor
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Optimization buffer reusable for this scope
	var collisionBuf [engine.MaxEntitiesPerCell]engine.Entity

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

		// Use Zero-Alloc check
		count := world.Positions.GetAllAtInto(newX, newY, collisionBuf[:])
		collidingEntities := collisionBuf[:count]

		blocked := false

		// Process collisions safely
		for _, collidingEntity := range collidingEntities {
			if collidingEntity != 0 && collidingEntity != drainEntity && collidingEntity != s.ctx.CursorEntity {
				s.handleCollisionAtPosition(world, collidingEntity)
				blocked = true
			}
		}

		if blocked {
			continue
		}

		// Movement succeeded - update components
		drainPos.X = newX
		drainPos.Y = newY

		// Recalculate IsOnCursor after position change using fresh cursor data
		drain.IsOnCursor = drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Update position
		world.Positions.Add(drainEntity, drainPos)

		// Save updated drain component
		drain.LastMoveTime = now
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

		// Check for collision at new position
		targets := world.Positions.GetAllAt(drainPos.X, drainPos.Y)

		// Collect collision candidates for Collect-Then-Destroy
		var toProcess []engine.Entity
		for _, target := range targets {
			if target != 0 && target != entity {
				toProcess = append(toProcess, target)
			}
		}

		// Process collisions safely outside the spatial grid iteration loop
		for _, target := range toProcess {
			s.handleCollisionAtPosition(world, target)
		}
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