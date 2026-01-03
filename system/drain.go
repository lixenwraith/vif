package system

// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

import (
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// pendingDrainSpawn represents a queued drain materialize spawn awaiting materialization
type pendingDrainSpawn struct {
	targetX            int    // Spawn position X
	targetY            int    // Spawn position Y
	scheduledTick      uint64 // Game tick when materialization should start
	materializeStarted bool   // Prevent materializer accounting gap (1 tick in-flight event)
}

// DrainSystem manages the drain entity lifecycle
// Drain count = floor(heat / 10), max 10
// Drains spawn materialize based on Heat only
// Priority: 25 (after CleanerSystem:22, before DecaySystem:30)
type DrainSystem struct {
	mu sync.RWMutex
	engine.SystemBase

	// Spawn queue for staggered materialization
	pendingSpawns []pendingDrainSpawn

	// Monotonic counter for LIFO materialize spawn ordering
	nextSpawnOrder int64

	// Spawn failure backoff (game ticks)
	spawnCooldownUntil uint64

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Cached metric pointers
	statCount   *atomic.Int64
	statPending *atomic.Int64

	paused bool

	enabled bool
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(world *engine.World) engine.System {
	s := &DrainSystem{
		SystemBase: engine.NewSystemBase(world),
	}

	s.pendingSpawns = make([]pendingDrainSpawn, constant.DrainMaxCount)

	s.statCount = s.Resource.Status.Ints.Get("drain.count")
	s.statPending = s.Resource.Status.Ints.Get("drain.pending")

	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *DrainSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *DrainSystem) initLocked() {
	s.pendingSpawns = s.pendingSpawns[:0]
	s.nextSpawnOrder = 0
	s.spawnCooldownUntil = 0
	s.rng = vmath.NewFastRand(uint32(s.Resource.Time.RealTime.UnixNano()))
	s.statCount.Store(0)
	s.statPending.Store(0)
	s.paused = false
	s.enabled = true
}

// Priority returns the system's priority
func (s *DrainSystem) Priority() int {
	return constant.PriorityDrain
}

// EventTypes returns the event types DrainSystem handles
func (s *DrainSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMaterializeComplete,
		event.EventDrainPause,
		event.EventDrainResume,
		event.EventGameReset,
	}
}

// HandleEvent processes events
func (s *DrainSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventDrainPause:
		s.paused = true
		// Clear pending spawns to prevent stale materialize
		s.pendingSpawns = s.pendingSpawns[:0]

	case event.EventDrainResume:
		s.paused = false
		// Spawning resumes naturally in Update() based on heat

	case event.EventMaterializeComplete:
		// Prevent race condition where drain materializes after fuse sequence started
		if s.paused {
			return
		}
		if payload, ok := ev.Payload.(*event.SpawnCompletePayload); ok {
			if payload.Type == component.SpawnTypeDrain {
				s.removeCompletedSpawn(payload.X, payload.Y)
				s.materializeDrainAt(payload.X, payload.Y)
			}
		}
	}
}

// Update runs the drain system logic
func (s *DrainSystem) Update() {
	if !s.enabled {
		return
	}

	// Skip all materialize spawn/despawn materialize logic during quasar phase
	if s.paused {
		s.statCount.Store(0)
		s.statPending.Store(0)
		return
	}

	currentTick := s.Resource.State.State.GetGameTicks()

	// Process pending materialize spawn queue first
	s.processPendingSpawns()

	// Multi-drain lifecycle based on heat
	currentCount := s.Component.Drain.Count()
	pendingCount := len(s.pendingSpawns)

	targetCount := s.calcTargetDrainCount()
	effectiveCount := currentCount + pendingCount

	if effectiveCount < targetCount {
		// Check materialize spawn cooldown
		if currentTick >= s.spawnCooldownUntil {
			needed := targetCount - effectiveCount
			queued := s.queueDrainSpawns(needed)

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
		s.despawnExcessDrains(currentCount - targetCount)
		// Clear cooldown on despawn materialize (positions freed up)
		s.spawnCooldownUntil = 0
	}

	// Clock-based updates for active drains
	if s.Component.Drain.Count() > 0 {
		s.updateDrainMovement()
		s.handleDrainInteractions()
	}

	s.statCount.Store(int64(s.Component.Drain.Count()))
	s.statPending.Store(int64(len(s.pendingSpawns)))
}

// removeCompletedSpawn removes materialize spawn entry after materialize completion
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
func (s *DrainSystem) getHeat() int {
	cursorEntity := s.Resource.Cursor.Entity
	if hc, ok := s.Component.Heat.Get(cursorEntity); ok {
		return int(hc.Current.Load())
	}
	return 0
}

// hasPendingSpawns returns true if materialize spawn queue is non-empty
func (s *DrainSystem) hasPendingSpawns() bool {
	return len(s.pendingSpawns) > 0
}

// processPendingSpawns starts materialization for spawns whose scheduled tick has arrived
func (s *DrainSystem) processPendingSpawns() {
	if len(s.pendingSpawns) == 0 {
		return
	}

	currentTick := s.Resource.State.State.GetGameTicks()
	for i := range s.pendingSpawns {
		spawn := &s.pendingSpawns[i]
		if !spawn.materializeStarted && currentTick >= spawn.scheduledTick {
			s.World.PushEvent(event.EventMaterializeRequest, &event.MaterializeRequestPayload{
				X:    spawn.targetX,
				Y:    spawn.targetY,
				Type: component.SpawnTypeDrain,
			})
			spawn.materializeStarted = true
		}
	}
}

// queueDrainSpawn adds a drain materialize spawn to the pending queue with stagger timing
func (s *DrainSystem) queueDrainSpawn(targetX, targetY int, staggerIndex int) {
	currentTick := s.Resource.State.State.GetGameTicks()
	scheduledTick := currentTick + uint64(staggerIndex)*uint64(constant.DrainSpawnStaggerTicks)

	s.pendingSpawns = append(s.pendingSpawns, pendingDrainSpawn{
		targetX:       targetX,
		targetY:       targetY,
		scheduledTick: scheduledTick,
	})
}

// calcTargetDrainCount returns the desired number of drains based on current heat
// Formula: floor(heat / 10), capped at DrainMaxCount
func (s *DrainSystem) calcTargetDrainCount() int {
	heat := s.getHeat()
	count := heat / 10 // Integer division = floor
	if count > constant.DrainMaxCount {
		count = constant.DrainMaxCount
	}
	return count
}

// getActiveDrainsBySpawnOrder returns drains sorted by SpawnOrder descending (newest first)
func (s *DrainSystem) getActiveDrainsBySpawnOrder() []core.Entity {
	entities := s.Component.Drain.All()
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
		if drain, ok := s.Component.Drain.Get(e); ok {
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
// When cursor is near edge, extends materialize spawn range on opposite side to maintain area
// Retries up to maxRetries times to find unoccupied cell not in pending queue
func (s *DrainSystem) randomSpawnOffset(baseX, baseY int, queuedPositions map[uint64]bool) (int, int, bool) {
	config := s.Resource.Config
	maxRetries := constant.DrainSpawnMaxRetries
	radius := constant.DrainSpawnOffsetMax
	width := config.GameWidth
	height := config.GameHeight

	// Calculate materialize spawn range with boundary stretching
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

		// Check if position already queued for materialize spawn
		key := uint64(x)<<32 | uint64(y)
		if queuedPositions[key] {
			continue
		}

		// Check if cell is occupied by existing drain (authoritative, grid-independent)
		if !s.hasDrainAt(x, y) {
			return x, y, true
		}
	}

	return 0, 0, false
}

// buildQueuedPositionSet creates position exclusion map from all materialize spawn sources
func (s *DrainSystem) buildQueuedPositionSet() map[uint64]bool {
	queuedPositions := make(map[uint64]bool, len(s.pendingSpawns)+s.Component.Drain.Count()+s.Component.Materialize.Count()/4)

	// Pending spawns
	for _, ps := range s.pendingSpawns {
		key := uint64(ps.targetX)<<32 | uint64(ps.targetY)
		queuedPositions[key] = true
	}

	// Active materializer targets
	matEntities := s.Component.Materialize.All()
	for _, e := range matEntities {
		if mat, ok := s.Component.Materialize.Get(e); ok {
			key := uint64(mat.TargetX)<<32 | uint64(mat.TargetY)
			queuedPositions[key] = true
		}
	}

	// Existing drain positions (authoritative iteration, not spatial query)
	drainEntities := s.Component.Drain.All()
	for _, e := range drainEntities {
		if pos, ok := s.World.Positions.Get(e); ok {
			key := uint64(pos.X)<<32 | uint64(pos.Y)
			queuedPositions[key] = true
		}
	}

	return queuedPositions
}

// hasDrainAt checks if any drain exists at position using authoritative Drains store
// O(n) where n = drain count (max 10), immune to spatial grid saturation
func (s *DrainSystem) hasDrainAt(x, y int) bool {
	drainEntities := s.Component.Drain.All()
	for _, e := range drainEntities {
		if pos, ok := s.World.Positions.Get(e); ok {
			if pos.X == x && pos.Y == y {
				return true
			}
		}
	}
	return false
}

// queueDrainSpawns queues multiple drain spawns with stagger timing
// Returns number of spawns successfully queued
func (s *DrainSystem) queueDrainSpawns(count int) int {
	cursorEntity := s.Resource.Cursor.Entity

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return 0
	}

	queuedPositions := s.buildQueuedPositionSet()

	queued := 0
	for i := 0; i < count; i++ {
		targetX, targetY, valid := s.randomSpawnOffset(cursorPos.X, cursorPos.Y, queuedPositions)
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
func (s *DrainSystem) despawnExcessDrains(count int) {
	if count <= 0 {
		return
	}

	ordered := s.getActiveDrainsBySpawnOrder()
	toRemove := count
	if toRemove > len(ordered) {
		toRemove = len(ordered)
	}

	for i := 0; i < toRemove; i++ {
		event.EmitDeathOne(s.Resource.Events.Queue, ordered[i], event.EventFlashRequest, s.Resource.Time.FrameNumber)
	}
}

// materializeDrainAt creates a drain entity at the specified position
func (s *DrainSystem) materializeDrainAt(spawnX, spawnY int) {
	config := s.Resource.Config
	cursorEntity := s.Resource.Cursor.Entity
	now := s.Resource.Time.GameTime

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

	// Check for existing drain using authoritative store
	if s.hasDrainAt(spawnX, spawnY) {
		// Collision with moved drain - re-queue at alternate position
		s.requeueSpawnWithOffset(spawnX, spawnY)
		return
	}

	entity := s.World.CreateEntity()

	pos := component.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// Increment and assign materialize spawn order for LIFO tracking
	s.nextSpawnOrder++

	// Initialize KineticState with spawn position, zero velocity
	drain := component.DrainComponent{
		KineticState: component.KineticState{
			PreciseX: vmath.FromInt(spawnX),
			PreciseY: vmath.FromInt(spawnY),
			// VelX, VelY, AccelX, AccelY zero-initialized
		},
		LastDrainTime: now,
		IsOnCursor:    spawnX == cursorPos.X && spawnY == cursorPos.Y,
		SpawnOrder:    s.nextSpawnOrder,
		LastIntX:      spawnX,
		LastIntY:      spawnY,
	}

	// Handle collisions at materialize spawn position
	// GetAllAt returns a copy, so iterating while destroying is safe
	entitiesAtSpawn := s.World.Positions.GetAllAt(spawnX, spawnY)
	for _, e := range entitiesAtSpawn {
		if e != cursorEntity {
			s.handleCollisionAtPosition(e)
		}
	}

	s.World.Positions.Set(entity, pos)
	s.Component.Drain.Set(entity, drain)
	// Visual component for sigil renderer and death system flash extraction
	s.Component.Sigil.Set(entity, component.SigilComponent{
		Rune:  constant.DrainChar,
		Color: component.SigilDrain,
	})
}

// requeueSpawnWithOffset attempts to find alternate position and re-queue materialize spawn
// Called when target position blocked by drain that moved into it
func (s *DrainSystem) requeueSpawnWithOffset(blockedX, blockedY int) {
	cursorEntity := s.Resource.Cursor.Entity

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	queuedPositions := s.buildQueuedPositionSet()
	// Block original position to force different selection
	queuedPositions[uint64(blockedX)<<32|uint64(blockedY)] = true

	newX, newY, valid := s.randomSpawnOffset(cursorPos.X, cursorPos.Y, queuedPositions)
	if valid {
		s.queueDrainSpawn(newX, newY, 0) // Immediate re-spawn materialize
	}
	// If no valid position, materialize spawn dropped (map saturated with drains)
}

// isInsideShieldEllipse checks if position is within the shield ellipse using Q16.16 fixed-point
func (s *DrainSystem) isInsideShieldEllipse(x, y int) bool {
	cursorEntity := s.Resource.Cursor.Entity

	shield, ok := s.Component.Shield.Get(cursorEntity)
	if !ok {
		return false
	}

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return false
	}

	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)

	// Ellipse equation: (dx²/rx² + dy²/ry²) <= 1  →  (dx² * invRxSq + dy² * invRySq) <= Scale
	// Precomputed InvRxSq/InvRySq from ShieldSystem.cacheInverseRadii
	return vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)
}

// handleDrainInteractions processes all drain interactions per tick
func (s *DrainSystem) handleDrainInteractions() {
	cursorEntity := s.Resource.Cursor.Entity
	now := s.Resource.Time.GameTime

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// 1. Detect drain-drain collisions (same cell)
	s.handleDrainDrainCollisions()

	// 2. Handle shield zone and cursor interactions
	drainEntities := s.Component.Drain.All()
	for _, drainEntity := range drainEntities {
		drain, ok := s.Component.Drain.Get(drainEntity)
		if !ok {
			continue
		}

		drainPos, ok := s.World.Positions.Get(drainEntity)
		if !ok {
			continue
		}

		isOnCursor := drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Update cached state
		if drain.IsOnCursor != isOnCursor {
			drain.IsOnCursor = isOnCursor
			s.Component.Drain.Set(drainEntity, drain)
		}

		// Check shield state from component
		shield, shieldOk := s.Component.Shield.Get(cursorEntity)
		shieldActive := shieldOk && shield.Active

		// Shield zone interaction
		if shieldActive && s.isInsideShieldEllipse(drainPos.X, drainPos.Y) {
			// Energy drain (existing timer-based)
			if now.Sub(drain.LastDrainTime) >= constant.DrainEnergyDrainInterval {
				s.World.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
					Amount: constant.DrainShieldEnergyDrainAmount,
				})
				drain.LastDrainTime = now
				s.Component.Drain.Set(drainEntity, drain)
			}

			// Shield knockback (immunity-gated)
			if !now.Before(drain.DeflectUntil) {
				s.applyShieldKnockback(drainEntity, &drain, drainPos, cursorPos)
			}

			continue
		}

		// Cursor collision (shield not active or drain outside shield)
		if isOnCursor {
			s.World.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{
				Delta: -constant.DrainHeatReductionAmount,
			})
		}
	}

	// 3. Handle non-drain entity collisions
	s.handleEntityCollisions()
}

// applyShieldKnockback applies radial impulse when drain overlaps shield
func (s *DrainSystem) applyShieldKnockback(
	drainEntity core.Entity,
	drain *component.DrainComponent,
	drainPos component.PositionComponent,
	cursorPos component.PositionComponent,
) {
	now := s.Resource.Time.GameTime

	// Radial direction: cursor → drain (shield pushes outward)
	radialX := vmath.FromInt(drainPos.X - cursorPos.X)
	radialY := vmath.FromInt(drainPos.Y - cursorPos.Y)

	// Zero vector fallback (drain centered on cursor)
	if radialX == 0 && radialY == 0 {
		radialX = vmath.Scale
	}

	// Collision impulse (same physics as cleaner, equal mass)
	impulseX, impulseY := vmath.ApplyCollisionImpulse(
		radialX, radialY,
		vmath.MassRatioEqual,
		constant.DrainDeflectAngleVar,
		constant.ShieldKnockbackImpulseMin,
		constant.ShieldKnockbackImpulseMax,
		s.rng,
	)

	if impulseX == 0 && impulseY == 0 {
		return
	}

	// Additive impulse
	drain.VelX += impulseX
	drain.VelY += impulseY

	// Set immunity window
	drain.DeflectUntil = now.Add(constant.ShieldKnockbackImmunity)

	s.Component.Drain.Set(drainEntity, *drain)
}

// handleDrainDrainCollisions detects and removes all drains sharing a cell
func (s *DrainSystem) handleDrainDrainCollisions() {
	// Build position -> drain entities map
	drainPositions := make(map[uint64][]core.Entity)

	drainEntities := s.Component.Drain.All()
	for _, drainEntity := range drainEntities {
		pos, ok := s.World.Positions.Get(drainEntity)
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
				event.EmitDeathOne(s.Resource.Events.Queue, e, event.EventFlashRequest, s.Resource.Time.FrameNumber)
			}
		}
	}
}

// handleEntityCollisions processes collisions with non-drain entities
func (s *DrainSystem) handleEntityCollisions() {
	cursorEntity := s.Resource.Cursor.Entity

	entities := s.Component.Drain.All()
	for _, entity := range entities {
		drainPos, ok := s.World.Positions.Get(entity)
		if !ok {
			continue
		}

		targets := s.World.Positions.GetAllAt(drainPos.X, drainPos.Y)

		for _, target := range targets {
			if target != 0 && target != entity && target != cursorEntity {
				// Skip other drains (handled separately)
				if _, ok := s.Component.Drain.Get(target); ok {
					continue
				}
				s.handleCollisionAtPosition(target)
			}
		}
	}
}

// updateDrainMovement handles continuous kinetic drain movement toward cursor
func (s *DrainSystem) updateDrainMovement() {
	config := s.Resource.Config
	cursorEntity := s.Resource.Cursor.Entity
	now := s.Resource.Time.GameTime

	cursorPos, ok := s.World.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	dtFixed := vmath.FromFloat(s.Resource.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := config.GameWidth
	gameHeight := config.GameHeight

	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	drainEntities := s.Component.Drain.All()
	for _, drainEntity := range drainEntities {
		drain, ok := s.Component.Drain.Get(drainEntity)
		if !ok {
			continue
		}

		// Check deflection immunity
		inDeflection := now.Before(drain.DeflectUntil)

		if !inDeflection {
			// Normal physics: homing + drag

			// Homing direction toward cursor
			dx := cursorXFixed - drain.PreciseX
			dy := cursorYFixed - drain.PreciseY
			dirX, dirY := vmath.Normalize2D(dx, dy)

			currentSpeed := vmath.Magnitude(drain.VelX, drain.VelY)

			// Scaled homing: reduce influence at high speeds for curved comeback
			homingAccel := constant.DrainHomingAccel
			if currentSpeed > constant.DrainBaseSpeed && currentSpeed > 0 {
				// Scale by (baseSpeed / currentSpeed) for gradual curve
				homingAccel = vmath.Div(vmath.Mul(constant.DrainHomingAccel, constant.DrainBaseSpeed), currentSpeed)
			}

			drain.VelX += vmath.Mul(vmath.Mul(dirX, homingAccel), dtFixed)
			drain.VelY += vmath.Mul(vmath.Mul(dirY, homingAccel), dtFixed)

			// Apply drag if overspeed
			if currentSpeed > constant.DrainBaseSpeed && currentSpeed > 0 {
				excess := currentSpeed - constant.DrainBaseSpeed
				dragScale := vmath.Div(excess, currentSpeed)
				dragAmount := vmath.Mul(vmath.Mul(constant.DrainDrag, dtFixed), dragScale)

				drain.VelX -= vmath.Mul(drain.VelX, dragAmount)
				drain.VelY -= vmath.Mul(drain.VelY, dragAmount)
			}
		}
		// During deflection immunity: pure ballistic (no homing, no drag)

		// Store previous position for traversal
		oldPreciseX, oldPreciseY := drain.PreciseX, drain.PreciseY

		// Integrate position
		newX, newY := drain.Integrate(dtFixed)

		// Boundary handling: reflect velocity on edge contact (pool table physics)
		if newX < 0 {
			newX = 0
			drain.PreciseX = 0
			drain.VelX, drain.VelY = vmath.ReflectAxisX(drain.VelX, drain.VelY)
		} else if newX >= gameWidth {
			newX = gameWidth - 1
			drain.PreciseX = vmath.FromInt(gameWidth - 1)
			drain.VelX, drain.VelY = vmath.ReflectAxisX(drain.VelX, drain.VelY)
		}
		if newY < 0 {
			newY = 0
			drain.PreciseY = 0
			drain.VelX, drain.VelY = vmath.ReflectAxisY(drain.VelX, drain.VelY)
		} else if newY >= gameHeight {
			newY = gameHeight - 1
			drain.PreciseY = vmath.FromInt(gameHeight - 1)
			drain.VelX, drain.VelY = vmath.ReflectAxisY(drain.VelX, drain.VelY)
		}

		// Swept collision detection via Traverse
		vmath.Traverse(oldPreciseX, oldPreciseY, drain.PreciseX, drain.PreciseY, func(x, y int) bool {
			if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
				return true
			}
			// Skip previous cell (already processed)
			if x == drain.LastIntX && y == drain.LastIntY {
				return true
			}

			count := s.World.Positions.GetAllAtInto(x, y, collisionBuf[:])
			for i := 0; i < count; i++ {
				target := collisionBuf[i]
				if target == 0 || target == drainEntity || target == cursorEntity {
					continue
				}
				// Skip other drains - handled by handleDrainDrainCollisions
				if s.Component.Drain.Has(target) {
					continue
				}
				s.handleCollisionAtPosition(target)
			}
			return true
		})

		// Grid sync on cell change
		if newX != drain.LastIntX || newY != drain.LastIntY {
			drain.LastIntX = newX
			drain.LastIntY = newY
			s.World.Positions.Set(drainEntity, component.PositionComponent{X: newX, Y: newY})
		}

		// Update cursor overlap state
		drain.IsOnCursor = newX == cursorPos.X && newY == cursorPos.Y

		s.Component.Drain.Set(drainEntity, drain)
	}
}

// handleCollisionAtPosition processes collision with a specific entity at a given position
func (s *DrainSystem) handleCollisionAtPosition(entity core.Entity) {
	cursorEntity := s.Resource.Cursor.Entity

	// Check protection before any collision handling
	if prot, ok := s.Component.Protection.Get(entity); ok {
		now := s.Resource.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDrain) {
			return
		}
	}

	// Skip cursor entity
	if entity == cursorEntity {
		return
	}

	// Check composite membership first (handles Gold after migration)
	if member, ok := s.Component.Member.Get(entity); ok {
		header, headerOk := s.Component.Header.Get(member.AnchorID)
		if headerOk && header.BehaviorID == component.BehaviorGold {
			s.handleGoldCompositeCollision(member.AnchorID, &header)
			return
		}
		// Non-gold composite member: destroy single entity
		s.World.DestroyEntity(entity)
		return
	}

	// Check if it's a nugget, destroy and clean up the ID
	if s.Component.Nugget.Has(entity) {
		s.handleNuggetCollision(entity)
		return
	}

	// Destroy the entity (Handles standard chars, Decay entities, etc.)
	s.World.DestroyEntity(entity)
}

// handleGoldCompositeCollision destroys entire gold composite via anchor
func (s *DrainSystem) handleGoldCompositeCollision(anchorEntity core.Entity, header *component.CompositeHeaderComponent) {
	s.World.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		AnchorEntity: anchorEntity,
	})

	// Destroy all living members with flash
	for _, m := range header.Members {
		if m.Entity == 0 {
			continue
		}
		if pos, ok := s.World.Positions.Get(m.Entity); ok {
			if glyph, ok := s.Component.Glyph.Get(m.Entity); ok {
				s.World.PushEvent(event.EventFlashRequest, &event.FlashRequestPayload{
					X: pos.X, Y: pos.Y, Char: glyph.Rune,
				})
			}
		}
		s.Component.Member.Remove(m.Entity)
		s.World.DestroyEntity(m.Entity)
	}

	// Destroy phantom head
	s.Component.Protection.Remove(anchorEntity)
	s.Component.Header.Remove(anchorEntity)
	s.World.DestroyEntity(anchorEntity)
}

// handleNuggetCollision destroys the nugget entity and emits destruction event
func (s *DrainSystem) handleNuggetCollision(entity core.Entity) {
	s.World.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
		Entity: entity,
	})

	// Destroy the nugget entity
	s.World.DestroyEntity(entity)
}