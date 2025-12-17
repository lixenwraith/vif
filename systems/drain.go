package systems

import (
	"cmp"
	"math/rand"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// pendingDrainSpawn represents a queued drain spawn awaiting materialization
type pendingDrainSpawn struct {
	targetX            int    // Spawn position X
	targetY            int    // Spawn position Y
	scheduledTick      uint64 // Game tick when materialization should start
	materializeStarted bool   // Prevent materializer accounting gap (1 tick in-flight event)
}

// DrainSystem manages the drain entity lifecycle
// Drain count = floor(heat / 10), max 10
// Drains spawn based on Heat only
// Priority: 25 (after CleanerSystem:22, before DecaySystem:30)
type DrainSystem struct {
	world *engine.World
	res   engine.CoreResources

	drainStore  *engine.Store[components.DrainComponent]
	matStore    *engine.Store[components.MaterializeComponent]
	protStore   *engine.Store[components.ProtectionComponent]
	shieldStore *engine.Store[components.ShieldComponent]
	heatStore   *engine.Store[components.HeatComponent]
	seqStore    *engine.Store[components.SequenceComponent]
	charStore   *engine.Store[components.CharacterComponent]
	nuggetStore *engine.Store[components.NuggetComponent]

	// Spawn queue for staggered materialization
	pendingSpawns []pendingDrainSpawn

	// Monotonic counter for LIFO spawn ordering
	nextSpawnOrder int64

	// Spawn failure backoff (game ticks)
	spawnCooldownUntil uint64
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(world *engine.World) engine.System {
	return &DrainSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		drainStore:  engine.GetStore[components.DrainComponent](world),
		matStore:    engine.GetStore[components.MaterializeComponent](world),
		protStore:   engine.GetStore[components.ProtectionComponent](world),
		shieldStore: engine.GetStore[components.ShieldComponent](world),
		heatStore:   engine.GetStore[components.HeatComponent](world),
		seqStore:    engine.GetStore[components.SequenceComponent](world),
		charStore:   engine.GetStore[components.CharacterComponent](world),
		nuggetStore: engine.GetStore[components.NuggetComponent](world),

		pendingSpawns: make([]pendingDrainSpawn, 0, constants.DrainMaxCount),
	}
}

// Priority returns the system's priority
func (s *DrainSystem) Priority() int {
	return constants.PriorityDrain
}

// EventTypes returns the event types DrainSystem handles
func (s *DrainSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventMaterializeComplete,
	}
}

// HandleEvent processes events
func (s *DrainSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventMaterializeComplete {
		if payload, ok := event.Payload.(*events.SpawnCompletePayload); ok {
			if payload.Type == components.SpawnTypeDrain {
				s.removeCompletedSpawn(payload.X, payload.Y)
				s.materializeDrainAt(world, payload.X, payload.Y)
			}
		}
	}
}

// Update runs the drain system logic
func (s *DrainSystem) Update(world *engine.World, dt time.Duration) {
	// TODO: revisit the tick source
	currentTick := s.res.State.State.GetGameTicks()

	// Process pending spawn queue first
	s.processPendingSpawns(world)

	// Multi-drain lifecycle based on heat
	currentCount := s.drainStore.Count()
	pendingCount := len(s.pendingSpawns)

	targetCount := s.calcTargetDrainCount(world)
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
	if s.drainStore.Count() > 0 {
		s.updateDrainMovement(world)
		s.handleDrainInteractions(world)
	}
}

// removeCompletedSpawn removes spawn entry after materialize completion
func (s *DrainSystem) removeCompletedSpawn(x, y int) {
	for i, spawn := range s.pendingSpawns {
		if spawn.targetX == x && spawn.targetY == y && spawn.materializeStarted {
			s.pendingSpawns[i] = s.pendingSpawns[len(s.pendingSpawns)-1]
			s.pendingSpawns = s.pendingSpawns[:len(s.pendingSpawns)-1]
			return
		}
	}
}

// getHeat reads heat value from HeatComponent
func (s *DrainSystem) getHeat(world *engine.World) int {
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
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

	currentTick := s.res.State.State.GetGameTicks()
	for i := range s.pendingSpawns {
		spawn := &s.pendingSpawns[i]
		if !spawn.materializeStarted && currentTick >= spawn.scheduledTick {
			world.PushEvent(events.EventMaterializeRequest, &events.MaterializeRequestPayload{
				X:    spawn.targetX,
				Y:    spawn.targetY,
				Type: components.SpawnTypeDrain,
			})
			spawn.materializeStarted = true
		}
	}
}

// queueDrainSpawn adds a drain spawn to the pending queue with stagger timing
func (s *DrainSystem) queueDrainSpawn(targetX, targetY int, staggerIndex int) {
	currentTick := s.res.State.State.GetGameTicks()
	scheduledTick := currentTick + uint64(staggerIndex)*uint64(constants.DrainSpawnStaggerTicks)

	s.pendingSpawns = append(s.pendingSpawns, pendingDrainSpawn{
		targetX:       targetX,
		targetY:       targetY,
		scheduledTick: scheduledTick,
	})
}

// calcTargetDrainCount returns the desired number of drains based on current heat
// Formula: floor(heat / 10), capped at DrainMaxCount
func (s *DrainSystem) calcTargetDrainCount(world *engine.World) int {
	heat := s.getHeat(world)
	count := heat / 10 // Integer division = floor
	if count > constants.DrainMaxCount {
		count = constants.DrainMaxCount
	}
	return count
}

// getActiveDrainsBySpawnOrder returns drains sorted by SpawnOrder descending (newest first)
func (s *DrainSystem) getActiveDrainsBySpawnOrder(world *engine.World) []core.Entity {
	entities := s.drainStore.All()
	if len(entities) <= 1 {
		return entities
	}

	// Sort by SpawnOrder descending (LIFO - highest order first)
	type drainWithOrder struct {
		entity core.Entity
		order  int64
	}

	ordered := make([]drainWithOrder, 0, len(entities))
	for _, e := range entities {
		if drain, ok := s.drainStore.Get(e); ok {
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

	result := make([]core.Entity, len(ordered))
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

// buildQueuedPositionSet creates position exclusion map from all spawn sources
func (s *DrainSystem) buildQueuedPositionSet(world *engine.World) map[uint64]bool {
	queuedPositions := make(map[uint64]bool, len(s.pendingSpawns)+s.drainStore.Count()+s.matStore.Count()/4)

	// Pending spawns
	for _, ps := range s.pendingSpawns {
		key := uint64(ps.targetX)<<32 | uint64(ps.targetY)
		queuedPositions[key] = true
	}

	// Active materializer targets
	matEntities := s.matStore.All()
	for _, e := range matEntities {
		if mat, ok := s.matStore.Get(e); ok {
			key := uint64(mat.TargetX)<<32 | uint64(mat.TargetY)
			queuedPositions[key] = true
		}
	}

	// Existing drain positions (authoritative iteration, not spatial query)
	drainEntities := s.drainStore.All()
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
	drainEntities := s.drainStore.All()
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
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity

	cursorPos, ok := world.Positions.Get(cursorEntity)
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
	drains := s.drainStore.All()
	for _, e := range drains {
		s.despawnDrainWithFlash(world, e)
	}
}

// despawnDrainWithFlash removes a single drain entity and triggers destruction flash
func (s *DrainSystem) despawnDrainWithFlash(world *engine.World, entity core.Entity) {
	// Get position for flash effect before destruction
	if pos, ok := world.Positions.Get(entity); ok {
		world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: constants.DrainChar,
		})
	}
	world.DestroyEntity(entity)
}

// materializeDrainAt creates a drain entity at the specified position
func (s *DrainSystem) materializeDrainAt(world *engine.World, spawnX, spawnY int) {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity
	now := s.res.Time.GameTime

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

	cursorPos, ok := world.Positions.Get(cursorEntity)
	if !ok {
		return
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
		if e != cursorEntity {
			s.handleCollisionAtPosition(world, e)
		}
	}

	world.Positions.Add(entity, pos)
	s.drainStore.Add(entity, drain)
}

// requeueSpawnWithOffset attempts to find alternate position and re-queue spawn
// Called when target position blocked by drain that moved into it
func (s *DrainSystem) requeueSpawnWithOffset(world *engine.World, blockedX, blockedY int) {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity

	cursorPos, ok := world.Positions.Get(cursorEntity)
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

// isInsideShieldEllipse checks if position is within the shield ellipse
func (s *DrainSystem) isInsideShieldEllipse(world *engine.World, x, y int) bool {
	cursorEntity := s.res.Cursor.Entity

	shield, ok := s.shieldStore.Get(cursorEntity)
	if !ok {
		return false
	}

	cursorPos, ok := world.Positions.Get(cursorEntity)
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
	cursorEntity := s.res.Cursor.Entity
	now := s.res.Time.GameTime

	cursorPos, ok := world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// Phase 1: Detect drain-drain collisions (same cell)
	s.handleDrainDrainCollisions(world)

	// Phase 2: Handle shield zone and cursor interactions
	drainEntities := s.drainStore.All()
	for _, drainEntity := range drainEntities {
		drain, ok := s.drainStore.Get(drainEntity)
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
			s.drainStore.Add(drainEntity, drain)
		}

		// Check shield state from component
		shield, shieldOk := s.shieldStore.Get(cursorEntity)
		shieldActive := shieldOk && shield.Active

		// Shield zone energy drain (applies to drains anywhere in shield ellipse)
		if shieldActive && s.isInsideShieldEllipse(world, drainPos.X, drainPos.Y) {
			if now.Sub(drain.LastDrainTime) >= constants.DrainEnergyDrainInterval {
				world.PushEvent(events.EventShieldDrain, &events.ShieldDrainPayload{
					Amount: constants.DrainShieldEnergyDrainAmount,
				})
				drain.LastDrainTime = now
				s.drainStore.Add(drainEntity, drain)
			}
			// Drain persists when shield is active
			continue
		}

		// Cursor collision (shield not active or drain outside shield)
		if isOnCursor {
			// No shield protection: reduce heat and despawn
			world.PushEvent(events.EventHeatAdd, &events.HeatAddPayload{
				Delta: -constants.DrainHeatReductionAmount,
			})
		}
	}

	// Phase 3: Handle non-drain entity collisions
	s.handleEntityCollisions(world)
}

// handleDrainDrainCollisions detects and removes all drains sharing a cell
func (s *DrainSystem) handleDrainDrainCollisions(world *engine.World) {
	// Build position -> drain entities map
	drainPositions := make(map[uint64][]core.Entity)

	drainEntities := s.drainStore.All()
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
	cursorEntity := s.res.Cursor.Entity

	entities := s.drainStore.All()
	for _, entity := range entities {
		drainPos, ok := world.Positions.Get(entity)
		if !ok {
			continue
		}

		targets := world.Positions.GetAllAt(drainPos.X, drainPos.Y)

		for _, target := range targets {
			if target != 0 && target != entity && target != cursorEntity {
				// Skip other drains (handled separately)
				if _, ok := s.drainStore.Get(target); ok {
					continue
				}
				s.handleCollisionAtPosition(world, target)
			}
		}
	}
}

// updateDrainMovement handles purely clock-based drain movement toward cursor
func (s *DrainSystem) updateDrainMovement(world *engine.World) {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity
	now := s.res.Time.GameTime

	// Optimization buffer reusable for this scope
	var collisionBuf [constants.MaxEntitiesPerCell]core.Entity

	// Get and iterate on all drains
	drainEntities := s.drainStore.All()
	for _, drainEntity := range drainEntities {
		drain, ok := s.drainStore.Get(drainEntity)
		if !ok {
			continue
		}

		// Purely clock-based movement: only move when interval has elapsed
		timeSinceLastMove := now.Sub(drain.LastMoveTime)
		if timeSinceLastMove < constants.DrainMoveInterval {
			continue
		}

		cursorPos, ok := world.Positions.Get(cursorEntity)
		if !ok {
			continue
		}

		drainPos, ok := world.Positions.Get(drainEntity)
		if !ok {
			continue
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
			if collidingEntity != 0 && collidingEntity != drainEntity && collidingEntity != cursorEntity {
				// Skip other drains - they will be handled by handleDrainDrainCollisions
				if _, isDrain := s.drainStore.Get(collidingEntity); isDrain {
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
		s.drainStore.Add(drainEntity, drain)
	}
}

// handleCollisionAtPosition processes collision with a specific entity at a given position
func (s *DrainSystem) handleCollisionAtPosition(world *engine.World, entity core.Entity) {
	cursorEntity := s.res.Cursor.Entity

	// Check protection before any collision handling
	if prot, ok := s.protStore.Get(entity); ok {
		now := s.res.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(components.ProtectFromDrain) {
			return
		}
	}

	// Skip cursor entity
	if entity == cursorEntity {
		return
	}

	// Check it's a gold sequence entity - destroy entire sequence
	if seq, ok := s.seqStore.Get(entity); ok {
		if seq.Type == components.SequenceGold {
			s.handleGoldSequenceCollision(world, seq.ID)
			return
		}
	}

	// Check if it's a nugget, destroy and clean up the ID
	if s.nuggetStore.Has(entity) {
		s.handleNuggetCollision(world, entity)
		return
	}

	// Destroy the entity (Handles standard chars, Decay entities, etc.)
	world.DestroyEntity(entity)
}

// handleGoldSequenceCollision removes all gold sequence entities and emits destruction event
func (s *DrainSystem) handleGoldSequenceCollision(world *engine.World, sequenceID int) {
	// Emit event BEFORE destroying entities - GoldSystem validates if ID matches
	world.PushEvent(events.EventGoldDestroyed, &events.GoldCompletionPayload{
		SequenceID: sequenceID,
	})

	// Find and destroy all gold sequence entities with this ID
	goldSequenceEntities := s.seqStore.All()
	for _, goldSequenceEntity := range goldSequenceEntities {
		seq, ok := s.seqStore.Get(goldSequenceEntity)
		if !ok {
			continue
		}

		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			if pos, ok := world.Positions.Get(goldSequenceEntity); ok {
				if char, ok := s.charStore.Get(goldSequenceEntity); ok {
					world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
						X: pos.X, Y: pos.Y, Char: char.Rune,
					})
				}
			}
			world.DestroyEntity(goldSequenceEntity)
		}
	}
}

// handleNuggetCollision destroys the nugget entity and emits destruction event
func (s *DrainSystem) handleNuggetCollision(world *engine.World, entity core.Entity) {
	world.PushEvent(events.EventNuggetDestroyed, &events.NuggetDestroyedPayload{
		Entity: entity,
	})

	// Destroy the nugget entity
	world.DestroyEntity(entity)
}