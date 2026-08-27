package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// pendingDrainSpawn represents a queued drain materialize spawn awaiting materialization
type pendingDrainSpawn struct {
	targetX            int    // Spawn position X
	targetY            int    // Spawn position Y
	scheduledTick      uint64 // Game tick when materialization should start
	materializeStarted bool   // Prevent materializer accounting gap (1 tick in-flight event)
}

// drainCacheEntry holds cached drain data for single-pass processing
type drainCacheEntry struct {
	entity     core.Entity
	drainComp  component.DrainComponent
	combatComp component.CombatComponent
	pos        component.PositionComponent
	hasPos     bool
	// dying marks a death already emitted this tick; every later pass skips it
	dying bool
}

// DrainSystem manages the drain entity lifecycle
// If not paused, drain count = floor(heat / 10), max 10
// Drains spawn materialize based on Heat only
type DrainSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Spawn queue for staggered materialization
	pendingSpawns []pendingDrainSpawn

	// Monotonic counter for LIFO materialize spawn ordering
	nextSpawnOrder int

	// Spawn failure backoff (game ticks)
	spawnCooldownUntil uint64

	// Per-tick cache to avoid repeated queries
	drainCache []drainCacheEntry

	// Cached metric pointers
	statCount               *atomic.Int64
	statPending             *atomic.Int64
	statCollisions          *atomic.Int64
	statSuicides            *atomic.Int64
	statSpawned             *atomic.Int64
	statFusions             *atomic.Int64
	statDespawned           *atomic.Int64
	statSpawnFailure        *atomic.Int64
	lifecycle               lifecycleTelemetry
	statWallCollisions      *atomic.Int64
	statBoundaryReflections *atomic.Int64
	statGridSteps           *atomic.Int64
	statProtectedRejects    *atomic.Int64
	buffers                 bufferTelemetry

	paused bool

	enabled bool
}

// NewDrainSystem creates a new drain system
func NewDrainSystem(world *engine.World) engine.System {
	s := &DrainSystem{
		world: world,
	}

	s.pendingSpawns = make([]pendingDrainSpawn, parameter.DrainMaxCount)
	s.drainCache = make([]drainCacheEntry, 0, parameter.DrainMaxCount)

	s.statCount = s.world.Resources.Status.Ints.Get("drain.count")
	s.statPending = s.world.Resources.Status.Ints.Get("drain.pending")
	s.statCollisions = s.world.Resources.Status.Ints.Get("drain.collisions")
	s.statSuicides = s.world.Resources.Status.Ints.Get("drain.suicides")
	s.statFusions = s.world.Resources.Status.Ints.Get("drain.fusions")
	s.lifecycle = newLifecycleTelemetry(s.world.Resources.Status, "drain")
	s.statSpawned = s.lifecycle.spawned
	s.statDespawned = s.lifecycle.despawned
	s.statSpawnFailure = s.lifecycle.spawnFailures
	s.statWallCollisions = s.world.Resources.Status.Ints.Get("drain.wall_collisions")
	s.statBoundaryReflections = s.world.Resources.Status.Ints.Get("drain.boundary_reflections")
	s.statGridSteps = s.world.Resources.Status.Ints.Get("drain.grid_steps")
	s.statProtectedRejects = s.world.Resources.Status.Ints.Get("drain.protected_rejects")
	s.buffers = newBufferTelemetry(s.world.Resources.Status, "drain", "pending_spawns", "drain_cache")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DrainSystem) Init() {
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.pendingSpawns = s.pendingSpawns[:0]
	s.drainCache = s.drainCache[:0]
	s.nextSpawnOrder = 0
	s.spawnCooldownUntil = 0
	s.statCount.Store(0)
	s.statPending.Store(0)
	s.statCollisions.Store(0)
	s.statSuicides.Store(0)
	s.statFusions.Store(0)
	s.lifecycle.Reset()
	s.statWallCollisions.Store(0)
	s.statBoundaryReflections.Store(0)
	s.statGridSteps.Store(0)
	s.statProtectedRejects.Store(0)
	s.buffers.Reset()
	s.paused = false
	s.enabled = true
}

// Name returns system's name
func (s *DrainSystem) Name() string {
	return "drain"
}

// Domain reports player: it draws the player stream and creates player entities.
func (s *DrainSystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Priority returns the system's priority
func (s *DrainSystem) Priority() int {
	return parameter.PriorityDrain
}

// EventTypes returns the event types DrainSystem handles
func (s *DrainSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMaterializeComplete,
		event.EventDrainPause,
		event.EventDrainResume,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes events
func (s *DrainSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesDrain {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventMaterializeComplete {
			s.statSpawnFailure.Add(1)
		}
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
			s.statSpawnFailure.Add(1)
			return
		}
		if payload, ok := ev.Payload.(*event.MaterializeCompletedPayload); ok {
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

	// Cache all drain data for this tick, not covered by SpeciesCache, internal drain logic use
	s.cacheDrainData()

	// Process HP checks, enrage state, termination
	s.processDrainStates()

	// Detect and trigger swarm fusions (uses cached enraged state)
	s.detectSwarmFusions()

	// Skip spawn logic when paused
	if s.paused {
		s.statCount.Store(int64(s.liveDrainCount()))
		s.statPending.Store(0)
		return
	}

	// TODO: old logic, refactor
	currentTick := s.world.Resources.Game.State.GetGameTicks()

	// Process pending materialize spawn queue first
	s.processPendingSpawns()

	// Multi-drain lifecycle based on heat
	currentCount := s.liveDrainCount()
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
				s.statSpawnFailure.Add(int64(needed - queued))
				// Exponential backoff: 8 ticks base, doubles on consecutive failures
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
	if len(s.drainCache) > 0 {
		s.updateDrainMovement()
		s.handleDrainInteractions()
	}

	s.statCount.Store(int64(s.liveDrainCount()))
	s.statPending.Store(int64(len(s.pendingSpawns)))
}

// cacheDrainData populates drainCache with all drain entities and components
func (s *DrainSystem) cacheDrainData() {
	s.drainCache = s.drainCache[:0]

	// The cache intentionally snapshots component values before state processing.
	drainEntities := s.world.Components.Drain.Entities()
	for _, entity := range drainEntities {
		drainComp, ok := s.world.Components.Drain.GetComponent(entity)
		if !ok {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(entity)
		if !ok {
			continue
		}

		entry := drainCacheEntry{
			entity:     entity,
			drainComp:  drainComp,
			combatComp: combatComp,
		}

		if pos, ok := s.world.Positions.GetPosition(entity); ok {
			entry.pos = pos
			entry.hasPos = true
		}

		s.drainCache = append(s.drainCache, entry)
	}
	s.buffers.Observe(1, len(s.drainCache))
}

// processDrainStates handles HP checks, enrage transitions, and termination
func (s *DrainSystem) processDrainStates() {
	for i := range s.drainCache {
		entry := &s.drainCache[i]

		// Termination check
		if entry.combatComp.HitPoints <= 0 {
			entry.dying = true
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, entry.entity)

			// A positionless drain yields no death coordinate; -1 marks it absent
			killX, killY := entry.killPos()
			s.world.PushEventDomain(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       entry.entity,
				KillerEntity: entry.combatComp.LastDamagedBy,
				Species:      component.SpeciesDrain,
				X:            killX,
				Y:            killY,
			}, core.DomainPlayer)
			continue
		}

		// Enrage state transition, written through the store
		shouldEnrage := entry.combatComp.HitPoints < parameter.DrainEnrageThreshold
		if shouldEnrage != entry.combatComp.IsEnraged {
			entry.combatComp.IsEnraged = shouldEnrage
			if cc, ok := s.world.Components.Combat.GetPtr(entry.entity); ok {
				cc.IsEnraged = shouldEnrage
			}
		}
	}
}

// detectSwarmFusions pairs enraged drains and emits fusion requests
func (s *DrainSystem) detectSwarmFusions() {
	if s.paused {
		return
	}

	// Collect enraged drain entities
	var enragedDrains []core.Entity
	for i := range s.drainCache {
		entry := &s.drainCache[i]
		// Skip already dead or dying drains
		if entry.combatComp.HitPoints <= 0 || entry.dying {
			continue
		}
		if entry.combatComp.IsEnraged {
			enragedDrains = append(enragedDrains, entry.entity)
		}
	}

	// Pair enraged drains and emit fusion requests
	for len(enragedDrains) >= 2 {
		drainA := enragedDrains[0]
		drainB := enragedDrains[1]
		enragedDrains = enragedDrains[2:]

		s.world.PushEvent(event.EventFuseSwarmRequest, &event.FuseSwarmRequestPayload{
			DrainA: drainA,
			DrainB: drainB,
			Effect: event.FuseEffectSpirit,
		})
		s.statFusions.Add(1)
	}
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

// processPendingSpawns starts materialization for spawns whose scheduled tick has arrived, and purges stale spawns that failed to complete within timeout
func (s *DrainSystem) processPendingSpawns() {
	if len(s.pendingSpawns) == 0 {
		return
	}

	currentTick := s.world.Resources.Game.State.GetGameTicks()
	config := s.world.Resources.Config

	// Process in reverse to allow safe removal during iteration
	for i := len(s.pendingSpawns) - 1; i >= 0; i-- {
		spawn := &s.pendingSpawns[i]

		// Stale spawn detection: started but not completed within timeout
		// ~5 seconds at 20 ticks/sec = 100 ticks (materialize animation is ~0.5s)
		const staleThreshold = 100
		if spawn.materializeStarted && currentTick > spawn.scheduledTick+staleThreshold {
			s.statSpawnFailure.Add(1)
			// Remove stale spawn
			s.pendingSpawns[i] = s.pendingSpawns[len(s.pendingSpawns)-1]
			s.pendingSpawns = s.pendingSpawns[:len(s.pendingSpawns)-1]
			continue
		}

		// Validate coordinates still in bounds (handles resize after queue)
		if spawn.targetX >= config.MapWidth || spawn.targetY >= config.MapHeight {
			s.statSpawnFailure.Add(1)
			// Remove invalid spawn
			s.pendingSpawns[i] = s.pendingSpawns[len(s.pendingSpawns)-1]
			s.pendingSpawns = s.pendingSpawns[:len(s.pendingSpawns)-1]
			continue
		}

		if !spawn.materializeStarted && currentTick >= spawn.scheduledTick {
			// Drain is player-domain, so its materialize effect is too
			s.world.WithDomain(core.DomainPlayer, func() {
				s.world.PushEvent(event.EventMaterializeRequest, &event.MaterializeRequestPayload{
					X:    spawn.targetX,
					Y:    spawn.targetY,
					Type: component.SpawnTypeDrain,
				})
			})
			spawn.materializeStarted = true
		}
	}
}

// queueDrainSpawn adds a drain spawn to the pending queue with stagger timing
// Coordinates are clamped to current game bounds to prevent mismatch with materialize system (e.g. window resize in between materialize and spawn)
func (s *DrainSystem) queueDrainSpawn(targetX, targetY int, staggerIndex int) {
	config := s.world.Resources.Config
	currentTick := s.world.Resources.Game.State.GetGameTicks()
	scheduledTick := currentTick + uint64(staggerIndex)*uint64(parameter.DrainSpawnStaggerTicks)

	// Clamp to current bounds (prevents coordinate mismatch if resize occurs)
	if targetX < 0 {
		targetX = 0
	}
	if targetX >= config.MapWidth {
		targetX = config.MapWidth - 1
	}
	if targetY < 0 {
		targetY = 0
	}
	if targetY >= config.MapHeight {
		targetY = config.MapHeight - 1
	}

	s.pendingSpawns = append(s.pendingSpawns, pendingDrainSpawn{
		targetX:       targetX,
		targetY:       targetY,
		scheduledTick: scheduledTick,
	})
	s.buffers.Observe(0, len(s.pendingSpawns))
}

// calcTargetDrainCount returns the desired number of drains based on current heat
// Formula: floor(heat / 10), capped at DrainMaxCount
func (s *DrainSystem) calcTargetDrainCount() int {
	cursorEntity := s.world.Resources.Player.Entity
	currentHeat := 0
	if heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity); ok {
		currentHeat = heatComp.Current
	}

	count := currentHeat / 10 // int div floor
	if count > parameter.DrainMaxCount {
		count = parameter.DrainMaxCount
	}
	return count
}

// getActiveDrainsBySpawnOrder returns drains sorted by SpawnOrder descending (newest first)
func (s *DrainSystem) getActiveDrainsBySpawnOrder() []core.Entity {
	entities := s.world.Components.Drain.Entities()
	if len(entities) <= 1 {
		return entities
	}

	// Sort by SpawnOrder descending (LIFO - highest order first)
	type drainWithOrder struct {
		entity core.Entity
		order  int
	}

	ordered := make([]drainWithOrder, 0, len(entities))
	for _, e := range entities {
		if s.isDying(e) {
			continue
		}
		if drain, ok := s.world.Components.Drain.GetComponent(e); ok {
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
	config := s.world.Resources.Config
	maxRetries := parameter.DrainSpawnMaxRetries
	radius := parameter.DrainSpawnOffsetMax
	width := config.MapWidth
	height := config.MapHeight

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

	for range maxRetries {
		x := minX + s.rng.Intn(rangeX)
		y := minY + s.rng.Intn(rangeY)

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
	queuedPositions := make(map[uint64]bool, len(s.pendingSpawns)+s.world.Components.Drain.CountEntities()+s.world.Components.Materialize.CountEntities()/4)

	// Pending spawns
	for _, ps := range s.pendingSpawns {
		key := uint64(ps.targetX)<<32 | uint64(ps.targetY)
		queuedPositions[key] = true
	}

	// Active materializer targets
	matEntities := s.world.Components.Materialize.Entities()
	for _, matEntity := range matEntities {
		if matComp, ok := s.world.Components.Materialize.GetPtr(matEntity); ok {
			key := uint64(matComp.TargetX)<<32 | uint64(matComp.TargetY)
			queuedPositions[key] = true
		}
	}

	// Existing drain positions (component iteration, not spatial query)
	drainEntities := s.world.Components.Drain.Entities()
	for _, drainEntity := range drainEntities {
		if drainPos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			key := uint64(drainPos.X)<<32 | uint64(drainPos.Y)
			queuedPositions[key] = true
		}
	}

	// Wall positions (area denial)
	wallEntities := s.world.Components.Wall.Entities()
	for _, wallEntity := range wallEntities {
		wall, ok := s.world.Components.Wall.GetPtr(wallEntity)
		if !ok || wall.BlockMask&component.WallBlockSpawn == 0 {
			continue
		}
		if wallPos, ok := s.world.Positions.GetPosition(wallEntity); ok {
			key := uint64(wallPos.X)<<32 | uint64(wallPos.Y)
			queuedPositions[key] = true
		}
	}

	return queuedPositions
}

// hasDrainAt checks if any drain exists at position using authoritative Drains store
// O(n) where n = drain count (max 10), immune to spatial grid saturation
func (s *DrainSystem) hasDrainAt(x, y int) bool {
	drainEntities := s.world.Components.Drain.Entities()
	for _, e := range drainEntities {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
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
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return 0
	}

	queuedPositions := s.buildQueuedPositionSet()

	queued := 0
	for range count {
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
	toRemove := min(count, len(ordered))

	for i := range toRemove {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, ordered[i])
		s.statDespawned.Add(1)
	}
}

// materializeDrainAt creates a drain entity at the specified position
func (s *DrainSystem) materializeDrainAt(spawnX, spawnY int) {
	config := s.world.Resources.Config
	now := s.world.Resources.Time.GameTime

	// Clamp to bounds
	spawnX = max(min(spawnX, config.MapWidth-1), 0)
	spawnY = max(min(spawnY, config.MapHeight-1), 0)

	// Check for existing drain
	if s.hasDrainAt(spawnX, spawnY) {
		// Collision with moved drain - re-queue at alternate position
		s.requeueSpawnWithOffset(spawnX, spawnY)
		return
	}

	entity := s.world.CreateEntity(core.DomainPlayer)

	pos := component.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	// Increment and assign materialize spawn order for LIFO tracking
	s.nextSpawnOrder++

	// Initialize Kinetic with centered spawn position, zero velocity
	preciseX, preciseY := vmath.Point{X: spawnX, Y: spawnY}.CenterF()
	drainComp := component.DrainComponent{
		LastDrainTime: now,
		SpawnOrder:    s.nextSpawnOrder,
		LastIntX:      spawnX,
		LastIntY:      spawnY,
	}
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
		// VelX, VelY, AccelX, AccelY zero-initialized
	}
	kineticComp := component.KineticComponent{Kinetic: kinetic}
	spawnEntry := drainCacheEntry{
		entity: entity,
		combatComp: component.CombatComponent{
			OwnerEntity:      entity,
			CombatEntityType: component.CombatEntityDrain,
			HitPoints:        parameter.CombatInitialHPDrain,
		},
	}

	// Handle collisions at materialize spawn position
	var entitiesAtSpawn [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(spawnX, spawnY, entitiesAtSpawn[:])

	s.world.Positions.SetPosition(entity, pos)
	s.world.Components.Drain.SetComponent(entity, drainComp)
	s.world.Components.Kinetic.SetComponent(entity, kineticComp)

	// Navigation component with defaults (GA will override via event)
	navComp := component.NavigationComponent{
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	}
	s.world.Components.Navigation.SetComponent(entity, navComp)

	// Combat component for interactions
	s.world.Components.Combat.SetComponent(entity, spawnEntry.combatComp)

	// Sigil component for death system flash extraction, drain renderer renders on top
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  visual.DrainChar,
		Color: visual.RgbDrain,
	})

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:  entity,
		Species: component.SpeciesDrain,
		X:       spawnX,
		Y:       spawnY,
	})

	// Resolve the occupants captured before the drain entered the position grid.
	// Publishing creation first preserves lifecycle ordering when the new drain
	// is immediately consumed by a shared species.
	for i := range count {
		e := entitiesAtSpawn[i]
		if !s.world.Components.Cursor.HasEntity(e) {
			s.handleCollisionAtPosition(&spawnEntry, e)
			if spawnEntry.dying {
				break
			}
		}
	}
	s.statSpawned.Add(1)
}

// requeueSpawnWithOffset attempts to find alternate position and re-queue materialize spawn when target position has become occupied since initial acquisition (e.g. another drain moved into it)
func (s *DrainSystem) requeueSpawnWithOffset(blockedX, blockedY int) {
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		s.statSpawnFailure.Add(1)
		return
	}

	queuedPositions := s.buildQueuedPositionSet()
	// Block original position to force different selection
	queuedPositions[uint64(blockedX)<<32|uint64(blockedY)] = true

	newX, newY, valid := s.randomSpawnOffset(cursorPos.X, cursorPos.Y, queuedPositions)
	if valid {
		s.queueDrainSpawn(newX, newY, 0) // Immediate re-spawn materialize
	} else {
		s.statSpawnFailure.Add(1)
	}
	// If no valid position, materialize spawn dropped (map saturated with drains)
}

// handleDrainInteractions processes all drain interactions per tick.
// Caller MUST have populated drainCache via cacheDrainData this tick.
func (s *DrainSystem) handleDrainInteractions() {
	now := s.world.Resources.Time.GameTime

	// Integration moved drains after cacheDrainData, so the snapshot is stale here
	s.refreshCachePositions()

	// 1. Detect drain-drain collisions (same cell)
	s.handleDrainDrainCollisions()

	// 2. Handle shield zone and cursor interactions
	for i := range s.drainCache {
		entry := &s.drainCache[i]
		if entry.dying {
			continue
		}
		drain, ok := s.world.Components.Drain.GetPtr(entry.entity)
		if !ok {
			continue
		}

		overlaps := CheckCursorOverlaps(s.world, entry.entity)
		drainReady := now.Sub(drain.LastDrainTime) >= parameter.DrainEnergyDrainInterval
		drainedShield := false
		destroyDrain := false

		for j := range overlaps.Count {
			overlap := &overlaps.Entries[j]
			// Apply shield-zone interactions before exact cursor contact.
			if len(overlap.ShieldMembers) > 0 {
				if drainReady {
					drainedShield = true
					s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
						Entity: overlap.Cursor,
						Value:  parameter.DrainShieldEnergyDrainAmount,
					})
				}

				s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
					AttackType:   component.CombatAttackShield,
					OwnerEntity:  overlap.Cursor,
					OriginEntity: overlap.Cursor,
					TargetEntity: entry.entity,
					HitEntities:  overlap.ShieldMembers,
				})
				continue
			}

			if overlap.OnCursor {
				s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
					Entity: overlap.Cursor,
					Delta:  -parameter.DrainHeatReductionAmount,
				})
				destroyDrain = true
			}
		}
		if drainedShield {
			drain.LastDrainTime = now
		}
		if destroyDrain {
			entry.dying = true
			event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, entry.entity)

			// Counted as a kill, credited to no cursor: the drain spent itself on the
			// player, so it grants no boost. Loot still drops as compensation.
			killX, killY := entry.killPos()
			s.world.PushEventDomain(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:  entry.entity,
				Species: component.SpeciesDrain,
				X:       killX,
				Y:       killY,
			}, core.DomainPlayer)
			s.statSuicides.Add(1)
		}
	}

	// 3. Handle non-drain entity collisions
	s.handleEntityCollisions()
}

// handleDrainDrainCollisions destroys every drain sharing a cell with another.
// Cache order is dense store order, so death and kill events reach the queue in
// the same order every run. Credit follows the last damaging cursor: the
// knockback that stacked them is player-caused.
func (s *DrainSystem) handleDrainDrainCollisions() {
	for i := range s.drainCache {
		a := &s.drainCache[i]
		if a.dying || !a.hasPos {
			continue
		}

		shared := false
		for j := range s.drainCache {
			b := &s.drainCache[j]
			// A drain already dying still occupies its cell this tick
			if i == j || !b.hasPos {
				continue
			}
			if b.pos.X == a.pos.X && b.pos.Y == a.pos.Y {
				shared = true
				break
			}
		}
		if !shared {
			continue
		}

		a.dying = true
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, a.entity)
		s.world.PushEventDomain(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
			Entity:       a.entity,
			KillerEntity: a.combatComp.LastDamagedBy,
			Species:      component.SpeciesDrain,
			X:            a.pos.X,
			Y:            a.pos.Y,
		}, core.DomainPlayer)
		s.statCollisions.Add(1)
	}
}

// handleEntityCollisions processes collisions with non-drain entities
func (s *DrainSystem) handleEntityCollisions() {
	var targets [parameter.MaxEntitiesPerCell]core.Entity
	for i := range s.drainCache {
		entry := &s.drainCache[i]
		if entry.dying || !entry.hasPos {
			continue
		}

		count := s.world.Positions.GetAllEntitiesAtInto(entry.pos.X, entry.pos.Y, targets[:])
		for j := range count {
			target := targets[j]
			if target == 0 || target == entry.entity || s.world.Components.Cursor.HasEntity(target) {
				continue
			}
			// Drains resolve against each other in their own pass
			if s.world.Components.Drain.HasEntity(target) {
				continue
			}
			// Walls are handled by physics, not collision
			if s.world.Components.Wall.HasEntity(target) {
				continue
			}
			s.handleCollisionAtPosition(entry, target)
			if entry.dying {
				break
			}
		}
	}
}

// updateDrainMovement handles continuous kinetic drain movement toward cursor.
// Caller MUST have populated drainCache via cacheDrainData this tick.
func (s *DrainSystem) updateDrainMovement() {
	config := s.world.Resources.Config

	// TODO: cap dtSec to become configurable, live game tick change
	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity

	drains := s.world.Components.Drain
	for i := range s.drainCache {
		if s.drainCache[i].dying {
			continue
		}
		entry := &s.drainCache[i]
		drainEntity := entry.entity
		drainComp, ok := drains.GetPtr(drainEntity)
		if !ok {
			continue
		}
		combatComp, ok := s.world.Components.Combat.GetPtr(drainEntity)
		if !ok {
			continue
		}
		// A stun freezes the drain completely. Kinetic immunity only suppresses
		// homing and drag below: collision velocity must still displace the drain.
		if combatComp.StunnedRemaining > 0 {
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetPtr(drainEntity)
		if !ok {
			continue
		}

		// 1. Steering is disabled during kinetic immunity so the collision
		// impulse remains authoritative, matching the composite movers.
		if combatComp.RemainingKineticImmunity == 0 {
			// ResolveMovementTarget handles group-based target resolution + nav routing
			// (direct path vs flow field vs stuck fallback)
			targetX, targetY, _ := ResolveMovementTarget(s.world, drainEntity, kineticComp)

			// Cornering drag scales the base drag by turn severity
			turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
				parameter.NavCorneringThreshold, 1.0)

			physics.ApplyHomingScaled(&kineticComp.Kinetic, targetX, targetY,
				&profile.DrainHoming, 1.0, dtSec, true)

			if turnSeverity > 0 {
				brake := turnSeverity * parameter.NavCorneringBrake * parameter.DrainDrag
				physics.ApplyLinearDrag(&kineticComp.Kinetic, brake, dtSec)
			}
		}

		// 2. Integration and collision always run for an unstunned drain,
		// including while a shield or explosion knockback is immune to re-hit.
		oldPreciseX, oldPreciseY := kineticComp.PreciseX, kineticComp.PreciseY
		newX, newY := physics.Integrate(&kineticComp.Kinetic, dtSec)

		if physics.ReflectBounds(&kineticComp.Kinetic, gameWidth, gameHeight) {
			s.statBoundaryReflections.Add(1)
			newX, newY = physics.GridPos(&kineticComp.Kinetic)
		}

		// Wall Collision (Traversal)
		lastSafeX, lastSafeY := drainComp.LastIntX, drainComp.LastIntY
		hitWall := false

		traverser := vmath.NewGridTraverserF(oldPreciseX, oldPreciseY, kineticComp.PreciseX, kineticComp.PreciseY)
		for traverser.Next() {
			s.statGridSteps.Add(1)
			x, y := traverser.Pos()

			if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
				continue
			}
			if x == drainComp.LastIntX && y == drainComp.LastIntY {
				continue
			}

			if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
				s.statWallCollisions.Add(1)
				s.reflectOffWall(&kineticComp.Kinetic, lastSafeX, lastSafeY, x, y)
				hitWall = true
				break
			}

			lastSafeX, lastSafeY = x, y

			// Entity-Entity Collision
			count := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])
			for j := range count {
				target := collisionBuf[j]
				if target == 0 || target == drainEntity || s.world.Components.Cursor.HasEntity(target) {
					continue
				}
				if s.world.Components.Drain.HasEntity(target) {
					continue
				}
				s.handleCollisionAtPosition(entry, target)
				if entry.dying {
					break
				}
			}
			if entry.dying {
				break
			}
		}

		if entry.dying {
			continue
		}

		if hitWall {
			newX, newY = lastSafeX, lastSafeY
		}

		// Update Position Component
		if newX != drainComp.LastIntX || newY != drainComp.LastIntY {
			drainComp.LastIntX = newX
			drainComp.LastIntY = newY
			s.world.Positions.SetPosition(drainEntity, component.PositionComponent{X: newX, Y: newY})
		}

	}
}

// reflectOffWall reflects velocity on the approach axis and snaps back to the safe cell
func (s *DrainSystem) reflectOffWall(k *physics.Kinetic, fromX, fromY, wallX, wallY int) {
	if wallX != fromX {
		physics.ReflectVelocityX(k, 1.0)
	}
	if wallY != fromY {
		physics.ReflectVelocityY(k, 1.0)
	}
	k.PreciseX, k.PreciseY = vmath.Point{X: fromX, Y: fromY}.CenterF()
}

// handleCollisionAtPosition processes collision with a specific entity at a
// given position. Shared species are handled before protection: their members
// carry ProtectFromSpecies, but a drain collision is now a one-way event
// boundary rather than shared-side collision physics.
func (s *DrainSystem) handleCollisionAtPosition(drain *drainCacheEntry, entity core.Entity) {
	if s.consumeIntoSharedSpecies(drain, entity) {
		return
	}

	// Check protection before any collision handling
	if protComp, ok := s.world.Components.Protection.GetPtr(entity); ok {
		if protComp.Mask&component.ProtectFromSpecies != 0 {
			s.statProtectedRejects.Add(1)
			return
		}
	}

	// Skip cursor entities.
	if s.world.Components.Cursor.HasEntity(entity) {
		return
	}

	// Convert glyphs to dust
	if s.world.Components.Glyph.HasEntity(entity) {
		event.EmitDeath(s.world.Resources.Event.Queue, event.EventDustSpawnOneRequest, entity)
		return
	}

	// Check if it's a nugget, notify destruction
	if s.world.Components.Nugget.HasEntity(entity) {
		s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
			Entity: entity,
		})
	}

	// Destroy the entity
	event.EmitDeath(s.world.Resources.Event.Queue, 0, entity)
}

// consumeIntoSharedSpecies converts a drain collision with a swarm or quasar
// into a value-only combat event. The consumer receives no drain entity and
// therefore never needs to read player-local state. The drain is silently
// consumed and will be replenished by the normal heat-based spawn accounting.
func (s *DrainSystem) consumeIntoSharedSpecies(drain *drainCacheEntry, entity core.Entity) bool {
	target, _, ok := ResolveTargetFromEntity(s.world, entity, drain.entity)
	if !ok || (!s.world.Components.Swarm.HasEntity(target) && !s.world.Components.Quasar.HasEntity(target)) {
		return false
	}

	targetCombat, ok := s.world.Components.Combat.GetPtr(target)
	if !ok || targetCombat.HitPoints <= 0 || drain.combatComp.HitPoints <= 0 {
		return false
	}

	s.world.PushEvent(event.EventCombatHealRequest, &event.CombatHealRequestPayload{
		TargetEntity: target,
		Amount:       drain.combatComp.HitPoints,
	})
	event.EmitDeath(s.world.Resources.Event.Queue, 0, drain.entity)
	drain.dying = true
	return true
}

// refreshCachePositions re-reads positions after integration; the tick-start snapshot predates movement
func (s *DrainSystem) refreshCachePositions() {
	for i := range s.drainCache {
		entry := &s.drainCache[i]
		entry.pos, entry.hasPos = s.world.Positions.GetPosition(entry.entity)
	}
}

// liveDrainCount counts drains whose death has not already been emitted this tick
func (s *DrainSystem) liveDrainCount() int {
	n := 0
	for i := range s.drainCache {
		if !s.drainCache[i].dying {
			n++
		}
	}
	return n
}

// isDying reports whether a drain's death was already emitted this tick
func (s *DrainSystem) isDying(e core.Entity) bool {
	for i := range s.drainCache {
		if s.drainCache[i].entity == e {
			return s.drainCache[i].dying
		}
	}
	return false
}

// killPos returns the entry's death coordinate; -1 marks an absent position
func (e *drainCacheEntry) killPos() (int, int) {
	if !e.hasPos {
		return -1, -1
	}
	return e.pos.X, e.pos.Y
}
