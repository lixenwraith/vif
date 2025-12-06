package systems

import (
	"cmp"
	"fmt"
	"math/rand"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// pendingDrainSpawn represents a queued drain spawn awaiting materialization
type pendingDrainSpawn struct {
	targetX       int    // Spawn position X
	targetY       int    // Spawn position Y
	scheduledTick uint64 // Game tick when materialization should start
}

// DrainSystem manages the drain entity lifecycle
// Drain count = floor(heat / 10), max 10
// Drains spawn based on Heat only
// Priority: 25 (after CleanerSystem:22, before DecaySystem:30)
type DrainSystem struct {
	ctx *engine.GameContext

	// Spawn queue for staggered materialization
	pendingSpawns []pendingDrainSpawn

	// Monotonic counter for LIFO spawn ordering
	nextSpawnOrder int64

	// Spawn failure backoff (game ticks)
	spawnCooldownUntil uint64
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(ctx *engine.GameContext) *DrainSystem {
	return &DrainSystem{
		ctx:           ctx,
		pendingSpawns: make([]pendingDrainSpawn, 0, constants.DrainMaxCount),
	}
}

// Priority returns the system's priority
func (s *DrainSystem) Priority() int {
	return constants.PriorityDrain
}

// Update runs the drain system logic
func (s *DrainSystem) Update(world *engine.World, dt time.Duration) {
	currentTick := s.ctx.State.GetGameTicks()

	// Process pending spawn queue first
	s.processPendingSpawns(world)

	// Update materialize animation if active
	if world.Materializers.Count() > 0 {
		s.updateMaterializers(world, dt)
	}

	// Multi-drain lifecycle based on heat
	currentCount := world.Drains.Count()
	pendingCount := len(s.pendingSpawns) + s.countActiveMaterializations(world)

	targetCount := s.calcTargetDrainCount()
	effectiveCount := currentCount + pendingCount

	if effectiveCount < targetCount {
		// Check spawn cooldown
		if currentTick >= s.spawnCooldownUntil {
			needed := targetCount - effectiveCount
			queued := s.queueDrainSpawns(world, needed)

			// Apply backoff if we couldn't queue all needed spawns
			if queued < needed {
				// Exponential backoff: 8 ticks base, doubles on consecutive failures
				// Capped at ~1 second (assuming 60 ticks/sec)
				backoff := uint64(8)
				if s.spawnCooldownUntil > 0 {
					// Already had a recent failure, increase backoff
					prevBackoff := s.spawnCooldownUntil - (currentTick - 1)
					if prevBackoff > 0 && prevBackoff < 60 {
						backoff = prevBackoff * 2
					}
				}
				s.spawnCooldownUntil = currentTick + backoff
			}
		}
	} else if currentCount > targetCount {
		// Too many drains (heat dropped)
		s.despawnExcessDrains(world, currentCount-targetCount)
		// Clear cooldown on despawn (positions freed up)
		s.spawnCooldownUntil = 0
	}

	// Clock-based updates for active drains
	if world.Drains.Count() > 0 {
		s.updateDrainMovement(world)
		s.handleDrainInteractions(world)
	}
}

// hasPendingSpawns returns true if spawn queue is non-empty
func (s *DrainSystem) hasPendingSpawns() bool {
	return len(s.pendingSpawns) > 0
}

// processPendingSpawns starts materialization for spawns whose scheduled tick has arrived
func (s *DrainSystem) processPendingSpawns(world *engine.World) {
	if len(s.pendingSpawns) == 0 {
		return
	}

	currentTick := s.ctx.State.GetGameTicks()
	var remaining []pendingDrainSpawn

	for _, spawn := range s.pendingSpawns {
		if currentTick >= spawn.scheduledTick {
			s.startMaterializeAt(world, spawn.targetX, spawn.targetY)
		} else {
			remaining = append(remaining, spawn)
		}
	}

	s.pendingSpawns = remaining
}

// queueDrainSpawn adds a drain spawn to the pending queue with stagger timing
func (s *DrainSystem) queueDrainSpawn(targetX, targetY int, staggerIndex int) {
	currentTick := s.ctx.State.GetGameTicks()
	scheduledTick := currentTick + uint64(staggerIndex)*uint64(constants.DrainSpawnStaggerTicks)

	s.pendingSpawns = append(s.pendingSpawns, pendingDrainSpawn{
		targetX:       targetX,
		targetY:       targetY,
		scheduledTick: scheduledTick,
	})
}

// calcTargetDrainCount returns the desired number of drains based on current heat
// Formula: floor(heat / 10), capped at DrainMaxCount
func (s *DrainSystem) calcTargetDrainCount() int {
	heat := s.ctx.State.GetHeat()
	count := heat / 10 // Integer division = floor
	if count > constants.DrainMaxCount {
		count = constants.DrainMaxCount
	}
	return count
}

// getActiveDrainsBySpawnOrder returns drains sorted by SpawnOrder descending (newest first)
func (s *DrainSystem) getActiveDrainsBySpawnOrder(world *engine.World) []engine.Entity {
	entities := world.Drains.All()
	if len(entities) <= 1 {
		return entities
	}

	// Sort by SpawnOrder descending (LIFO - highest order first)
	type drainWithOrder struct {
		entity engine.Entity
		order  int64
	}

	ordered := make([]drainWithOrder, 0, len(entities))
	for _, e := range entities {
		if drain, ok := world.Drains.Get(e); ok {
			ordered = append(ordered, drainWithOrder{entity: e, order: drain.SpawnOrder})
		}
	}

	// Simple insertion sort (small N, max 10)
	for i := 1; i < len(ordered); i++ {
		j := i
		for j > 0 && ordered[j].order > ordered[j-1].order {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
			j--
		}
	}

	result := make([]engine.Entity, len(ordered))
	for i, d := range ordered {
		result[i] = d.entity
	}
	return result
}

// randomSpawnOffset returns a valid position with boundary-stretched offset
// When cursor is near edge, extends spawn range on opposite side to maintain area
// Retries up to maxRetries times to find unoccupied cell not in pending queue
func (s *DrainSystem) randomSpawnOffset(world *engine.World, baseX, baseY int, config *engine.ConfigResource, queuedPositions map[uint64]bool) (int, int, bool) {
	maxRetries := constants.DrainSpawnMaxRetries
	radius := constants.DrainSpawnOffsetMax
	width := config.GameWidth
	height := config.GameHeight

	// Calculate spawn range with boundary stretching
	// X axis: maintain 2*radius+1 cell range by extending opposite side
	minX := baseX - radius
	maxX := baseX + radius

	if minX < 0 {
		// Extend right to compensate
		maxX += -minX
		minX = 0
	}
	if maxX >= width {
		// Extend left to compensate
		overflow := maxX - (width - 1)
		minX -= overflow
		maxX = width - 1
	}
	// Final clamp in case screen is smaller than 2*radius
	if minX < 0 {
		minX = 0
	}

	// Y axis: same logic
	minY := baseY - radius
	maxY := baseY + radius

	if minY < 0 {
		maxY += -minY
		minY = 0
	}
	if maxY >= height {
		overflow := maxY - (height - 1)
		minY -= overflow
		maxY = height - 1
	}
	if minY < 0 {
		minY = 0
	}

	rangeX := maxX - minX + 1
	rangeY := maxY - minY + 1

	for attempt := 0; attempt < maxRetries; attempt++ {
		x := minX + rand.Intn(rangeX)
		y := minY + rand.Intn(rangeY)

		// Check if position already queued for spawn
		key := uint64(x)<<32 | uint64(y)
		if queuedPositions[key] {
			continue
		}

		// Check if cell is occupied by existing drain (authoritative, grid-independent)
		if !s.hasDrainAt(world, x, y) {
			return x, y, true
		}
	}

	return 0, 0, false
}

// countActiveMaterializations returns number of drain spawns currently materializing
// Rounds up to account for partial groups from premature destruction
func (s *DrainSystem) countActiveMaterializations(world *engine.World) int {
	count := world.Materializers.Count()
	if count == 0 {
		return 0
	}
	return (count + 3) / 4
}

// buildQueuedPositionSet creates position exclusion map from all spawn sources
func (s *DrainSystem) buildQueuedPositionSet(world *engine.World) map[uint64]bool {
	queuedPositions := make(map[uint64]bool, len(s.pendingSpawns)+world.Drains.Count()+world.Materializers.Count()/4)

	// Pending spawns
	for _, ps := range s.pendingSpawns {
		key := uint64(ps.targetX)<<32 | uint64(ps.targetY)
		queuedPositions[key] = true
	}

	// Active materializer targets
	matEntities := world.Materializers.All()
	for _, e := range matEntities {
		if mat, ok := world.Materializers.Get(e); ok {
			key := uint64(mat.TargetX)<<32 | uint64(mat.TargetY)
			queuedPositions[key] = true
		}
	}

	// Existing drain positions (authoritative iteration, not spatial query)
	drainEntities := world.Drains.All()
	for _, e := range drainEntities {
		if pos, ok := world.Positions.Get(e); ok {
			key := uint64(pos.X)<<32 | uint64(pos.Y)
			queuedPositions[key] = true
		}
	}

	return queuedPositions
}

// hasDrainAt checks if any drain exists at position using authoritative Drains store
// O(n) where n = drain count (max 10), immune to spatial grid saturation
func (s *DrainSystem) hasDrainAt(world *engine.World, x, y int) bool {
	drainEntities := world.Drains.All()
	for _, e := range drainEntities {
		if pos, ok := world.Positions.Get(e); ok {
			if pos.X == x && pos.Y == y {
				return true
			}
		}
	}
	return false
}

// queueDrainSpawns queues multiple drain spawns with stagger timing
// Returns number of spawns successfully queued
func (s *DrainSystem) queueDrainSpawns(world *engine.World, count int) int {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return 0
	}

	queuedPositions := s.buildQueuedPositionSet(world)

	queued := 0
	for i := 0; i < count; i++ {
		targetX, targetY, valid := s.randomSpawnOffset(world, cursorPos.X, cursorPos.Y, config, queuedPositions)
		if !valid {
			continue
		}

		key := uint64(targetX)<<32 | uint64(targetY)
		queuedPositions[key] = true

		s.queueDrainSpawn(targetX, targetY, queued)
		queued++
	}

	return queued
}

// despawnExcessDrains removes N drains using LIFO ordering (newest first)
func (s *DrainSystem) despawnExcessDrains(world *engine.World, count int) {
	if count <= 0 {
		return
	}

	ordered := s.getActiveDrainsBySpawnOrder(world)
	toRemove := count
	if toRemove > len(ordered) {
		toRemove = len(ordered)
	}

	for i := 0; i < toRemove; i++ {
		s.despawnDrainWithFlash(world, ordered[i])
	}
}

// despawnAllDrains removes all drain entities with flash effect
func (s *DrainSystem) despawnAllDrains(world *engine.World) {
	drains := world.Drains.All()
	for _, e := range drains {
		s.despawnDrainWithFlash(world, e)
	}
}

// despawnDrainWithFlash removes a single drain entity and triggers destruction flash
func (s *DrainSystem) despawnDrainWithFlash(world *engine.World, entity engine.Entity) {
	// Get position for flash effect before destruction
	if pos, ok := world.Positions.Get(entity); ok {
		s.triggerDespawnFlash(world, pos.X, pos.Y)
	}
	world.DestroyEntity(entity)
}

// triggerDespawnFlash triggers a destruction flash effect at the given position
func (s *DrainSystem) triggerDespawnFlash(world *engine.World, x, y int) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	SpawnDestructionFlash(world, x, y, constants.DrainChar, timeRes.GameTime)
}

// startMaterializeAt initiates the materialize animation for a specific position
func (s *DrainSystem) startMaterializeAt(world *engine.World, targetX, targetY int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	if targetX < 0 {
		targetX = 0
	}
	if targetX >= config.GameWidth {
		targetX = config.GameWidth - 1
	}
	if targetY < 0 {
		targetY = 0
	}
	if targetY >= config.GameHeight {
		targetY = config.GameHeight - 1
	}

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	tX := float64(targetX)
	tY := float64(targetY)
	duration := constants.MaterializeAnimationDuration.Seconds()

	type spawnerDef struct {
		startX, startY float64
		dir            components.MaterializeDirection
	}

	// Create 4 spawner entities converging from edges (top, bottom, left, right)
	spawners := []spawnerDef{
		{tX, -1, components.MaterializeFromTop},
		{tX, gameHeight, components.MaterializeFromBottom},
		{-1, tY, components.MaterializeFromLeft},
		{gameWidth, tY, components.MaterializeFromRight},
	}

	// Spawn 4 materializer entities that animate toward target position
	for _, def := range spawners {
		velX := (tX - def.startX) / duration
		velY := (tY - def.startY) / duration

		startGridX := int(def.startX)
		startGridY := int(def.startY)

		// Initialize trail ring buffer with starting position
		var trailRing [constants.MaterializeTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := components.MaterializeComponent{
			PreciseX:  def.startX,
			PreciseY:  def.startY,
			VelocityX: velX,
			VelocityY: velY,
			TargetX:   targetX,
			TargetY:   targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Direction: def.dir,
			Char:      constants.MaterializeChar,
			Arrived:   false,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → MaterializeComponent (float overlay)
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: startGridX, Y: startGridY})
		world.Materializers.Add(entity, comp)
		// Protect from resize culling (off-screen start positions) and drains
		world.Protections.Add(entity, components.ProtectionComponent{
			Mask: components.ProtectFromDrain | components.ProtectFromCull,
		})
	}
}

// updateMaterializers updates materialize spawner entities and triggers drain spawn
func (s *DrainSystem) updateMaterializers(world *engine.World, dt time.Duration) {
	dtSeconds := dt.Seconds()
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	entities := world.Materializers.All()

	type targetState struct {
		entities   []engine.Entity
		allArrived bool
	}
	// Group materializers by target position (4 entities per target)
	targets := make(map[uint64]*targetState)

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore
		oldPos, hasPos := world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Group by target position
		key := uint64(mat.TargetX)<<32 | uint64(mat.TargetY)
		if targets[key] == nil {
			targets[key] = &targetState{
				entities:   make([]engine.Entity, 0, 4),
				allArrived: true,
			}
		}

		state := targets[key]
		state.entities = append(state.entities, entity)

		if mat.Arrived {
			continue
		}

		// --- Physics Update: Integrate velocity into float position (overlay state) ---
		mat.PreciseX += mat.VelocityX * dtSeconds
		mat.PreciseY += mat.VelocityY * dtSeconds

		// Check arrival based on direction
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
			state.allArrived = false
		}

		// --- Trail Update & Grid Sync: Update trail ring buffer and sync PositionStore if cell changed ---
		newGridX := int(mat.PreciseX)
		newGridY := int(mat.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			mat.TrailHead = (mat.TrailHead + 1) % constants.MaterializeTrailLength
			mat.TrailRing[mat.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if mat.TrailLen < constants.MaterializeTrailLength {
				mat.TrailLen++
			}

			// Sync grid position to PositionStore
			world.Positions.Add(entity, components.PositionComponent{X: newGridX, Y: newGridY})
		}

		world.Materializers.Add(entity, mat)
	}

	// --- Target Completion Handling: Spawn drain when all 4 materializers reach target ---
	for key, state := range targets {
		if state.allArrived && len(state.entities) > 0 {
			// Destroy all 4 materializers
			for _, entity := range state.entities {
				world.DestroyEntity(entity)
			}
			// Spawn drain at target position
			targetX := int(key >> 32)
			targetY := int(key & 0xFFFFFFFF)
			s.materializeDrainAt(world, targetX, targetY)
		}
	}
}

// materializeDrainAt creates a drain entity at the specified position
func (s *DrainSystem) materializeDrainAt(world *engine.World, spawnX, spawnY int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Clamp to bounds
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

	// Check for existing drain using authoritative store (immune to grid saturation)
	if s.hasDrainAt(world, spawnX, spawnY) {
		// Collision with moved drain - re-queue at alternate position
		s.requeueSpawnWithOffset(world, spawnX, spawnY)
		return
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

	// Increment and assign spawn order for LIFO tracking
	s.nextSpawnOrder++

	drain := components.DrainComponent{
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    spawnX == cursorPos.X && spawnY == cursorPos.Y,
		SpawnOrder:    s.nextSpawnOrder,
	}

	// Handle collisions at spawn position
	// GetAllAt returns a copy, so iterating while destroying is safe
	entitiesAtSpawn := world.Positions.GetAllAt(spawnX, spawnY)
	for _, e := range entitiesAtSpawn {
		if e != s.ctx.CursorEntity {
			s.handleCollisionAtPosition(world, e)
		}
	}

	world.Positions.Add(entity, pos)
	world.Drains.Add(entity, drain)
}

// requeueSpawnWithOffset attempts to find alternate position and re-queue spawn
// Called when target position blocked by drain that moved into it
func (s *DrainSystem) requeueSpawnWithOffset(world *engine.World, blockedX, blockedY int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}

	queuedPositions := s.buildQueuedPositionSet(world)
	// Block original position to force different selection
	queuedPositions[uint64(blockedX)<<32|uint64(blockedY)] = true

	newX, newY, valid := s.randomSpawnOffset(world, cursorPos.X, cursorPos.Y, config, queuedPositions)
	if valid {
		s.queueDrainSpawn(newX, newY, 0) // Immediate re-spawn
	}
	// If no valid position, spawn dropped (map saturated with drains)
}

// isShieldActive checks if shield is functionally active
func (s *DrainSystem) isShieldActive() bool {
	return s.ctx.State.GetShieldActive()
}

// handlePassiveShieldDrain applies 1 energy/second cost while shield is active
func (s *DrainSystem) handlePassiveShieldDrain(world *engine.World, now time.Time) {
	shield, ok := world.Shields.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}

	// Shield must have sources and energy > 0 for passive drain
	if shield.Sources == 0 || s.ctx.State.GetEnergy() <= 0 {
		return
	}

	// Check passive drain interval
	if now.Sub(shield.LastDrainTime) >= constants.ShieldPassiveDrainInterval {
		s.ctx.State.AddEnergy(-constants.ShieldPassiveDrainAmount)
		shield.LastDrainTime = now
		world.Shields.Add(s.ctx.CursorEntity, shield)
	}
}

// isInsideShieldEllipse checks if position is within the shield ellipse
func (s *DrainSystem) isInsideShieldEllipse(world *engine.World, x, y int) bool {
	shield, ok := world.Shields.Get(s.ctx.CursorEntity)
	if !ok {
		return false
	}

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return false
	}

	dx := float64(x - cursorPos.X)
	dy := float64(y - cursorPos.Y)

	// Ellipse equation: (dx/rx)^2 + (dy/ry)^2 <= 1
	normalizedDistSq := (dx*dx)/(shield.RadiusX*shield.RadiusX) + (dy*dy)/(shield.RadiusY*shield.RadiusY)
	return normalizedDistSq <= 1.0
}

// handleDrainInteractions processes all drain interactions per tick
func (s *DrainSystem) handleDrainInteractions(world *engine.World) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}

	shieldActive := s.isShieldActive()

	// Phase 1: Detect drain-drain collisions (same cell)
	s.handleDrainDrainCollisions(world)

	// Phase 2: Handle shield zone and cursor interactions
	drainEntities := world.Drains.All()
	for _, drainEntity := range drainEntities {
		drain, ok := world.Drains.Get(drainEntity)
		if !ok {
			continue
		}

		drainPos, ok := world.Positions.Get(drainEntity)
		if !ok {
			continue
		}

		isOnCursor := drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Update cached state
		if drain.IsOnCursor != isOnCursor {
			drain.IsOnCursor = isOnCursor
			world.Drains.Add(drainEntity, drain)
		}

		// Shield zone energy drain (applies to drains anywhere in shield ellipse)
		if shieldActive && s.isInsideShieldEllipse(world, drainPos.X, drainPos.Y) {
			if now.Sub(drain.LastDrainTime) >= constants.DrainEnergyDrainInterval {
				s.ctx.PushEvent(engine.EventShieldDrain, &engine.ShieldDrainPayload{
					Amount: constants.DrainShieldEnergyDrainAmount,
				}, now)
				drain.LastDrainTime = now
				world.Drains.Add(drainEntity, drain)
			}
			// Drain persists when shield is active
			continue
		}

		// Cursor collision (shield not active or drain outside shield)
		if isOnCursor {
			// No shield protection: reduce heat and despawn
			s.ctx.State.AddHeat(-constants.DrainHeatReductionAmount)
			s.despawnDrainWithFlash(world, drainEntity)
		}
	}

	// Phase 3: Handle non-drain entity collisions
	s.handleEntityCollisions(world)
}

// handleDrainDrainCollisions detects and removes all drains sharing a cell
func (s *DrainSystem) handleDrainDrainCollisions(world *engine.World) {
	// Build position -> drain entities map
	drainPositions := make(map[uint64][]engine.Entity)

	drainEntities := world.Drains.All()
	for _, drainEntity := range drainEntities {
		pos, ok := world.Positions.Get(drainEntity)
		if !ok {
			continue
		}
		key := uint64(pos.X)<<32 | uint64(pos.Y)
		drainPositions[key] = append(drainPositions[key], drainEntity)
	}

	// Find and destroy all drains at cells with multiple drains
	for _, entities := range drainPositions {
		if len(entities) > 1 {
			for _, e := range entities {
				s.despawnDrainWithFlash(world, e)
			}
		}
	}
}

// handleEntityCollisions processes collisions with non-drain entities
func (s *DrainSystem) handleEntityCollisions(world *engine.World) {
	entities := world.Drains.All()
	for _, entity := range entities {
		drainPos, ok := world.Positions.Get(entity)
		if !ok {
			continue
		}

		targets := world.Positions.GetAllAt(drainPos.X, drainPos.Y)

		for _, target := range targets {
			if target != 0 && target != entity && target != s.ctx.CursorEntity {
				// Skip other drains (handled separately)
				if _, ok := world.Drains.Get(target); ok {
					continue
				}
				s.handleCollisionAtPosition(world, target)
			}
		}
	}
}

// updateDrainMovement handles purely clock-based drain movement toward cursor
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Optimization buffer reusable for this scope
	var collisionBuf [constants.MaxEntitiesPerCell]engine.Entity

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

		// Process collisions
		for _, collidingEntity := range collidingEntities {
			if collidingEntity != 0 && collidingEntity != drainEntity && collidingEntity != s.ctx.CursorEntity {
				// Skip other drains - they will be handled by handleDrainDrainCollisions
				if _, isDrain := world.Drains.Get(collidingEntity); isDrain {
					continue
				}
				s.handleCollisionAtPosition(world, collidingEntity)
			}
		}

		// Movement succeeded - update components
		drainPos.X = newX
		drainPos.Y = newY

		// Recalculate IsOnCursor after position change
		drain.IsOnCursor = drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Update position
		world.Positions.Add(drainEntity, drainPos)

		// Save updated drain component
		drain.LastMoveTime = now
		world.Drains.Add(drainEntity, drain)
	}
}

// handleCollisionAtPosition processes collision with a specific entity at a given position
func (s *DrainSystem) handleCollisionAtPosition(world *engine.World, entity engine.Entity) {
	// Check protection before any collision handling
	if prot, ok := world.Protections.Get(entity); ok {
		timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
		if !prot.IsExpired(timeRes.GameTime.UnixNano()) && prot.Mask.Has(components.ProtectFromDrain) {
			return
		}
	}

	// Skip cursor entity
	if entity == s.ctx.CursorEntity {
		return
	}

	// Check it's a gold sequence entity - destroy entire sequence
	if seq, ok := world.Sequences.Get(entity); ok {
		if seq.Type == components.SequenceGold {
			s.handleGoldSequenceCollision(world, entity, seq.ID)
			return
		}
	}

	// Check if it's a nugget, destroy and clean up the ID
	if world.Nuggets.Has(entity) {
		s.handleNuggetCollision(world, entity)
		return
	}

	// Destroy the entity (Handles standard chars, Decay entities, etc.)
	world.DestroyEntity(entity)
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

	// Emit event BEFORE destroying entities (SplashSystem needs sequenceID)
	s.ctx.PushEvent(engine.EventGoldDestroyed, &engine.GoldCompletionPayload{
		SequenceID: sequenceID,
	}, now)

	// Find and destroy all gold sequence entities with this ID
	goldSequenceEntities := world.Sequences.All()
	for _, goldSequenceEntity := range goldSequenceEntities {
		seq, ok := world.Sequences.Get(goldSequenceEntity)
		if !ok {
			continue // Already destroyed or typed
		}

		// Only destroy gold sequence entities with matching ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			// Flash for gold character destruction
			if pos, ok := world.Positions.Get(goldSequenceEntity); ok {
				if char, ok := world.Characters.Get(goldSequenceEntity); ok {
					SpawnDestructionFlash(world, pos.X, pos.Y, char.Rune, now)
				}
			}
			world.DestroyEntity(goldSequenceEntity)
		}
	}

	// Trigger phase transition to PhaseGoldComplete
	s.ctx.State.DeactivateGoldSequence(now)
}

// handleNuggetCollision destroys the nugget entity and clears active nugget state using generic stores
func (s *DrainSystem) handleNuggetCollision(world *engine.World, entity engine.Entity) {
	// Clear active nugget
	s.ctx.State.ClearActiveNuggetID(uint64(entity))

	// Destroy the nugget entity
	world.DestroyEntity(entity)
}