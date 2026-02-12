package system

import (
	"fmt"
	"math/rand"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/genetic/game/species"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
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
}

// DrainSystem manages the drain entity lifecycle
// If not paused, drain count = floor(heat / 10), max 10
// Drains spawn materialize based on Heat only
type DrainSystem struct {
	world *engine.World

	// Spawn queue for staggered materialization
	pendingSpawns []pendingDrainSpawn

	// Monotonic counter for LIFO materialize spawn ordering
	nextSpawnOrder int64

	// Spawn failure backoff (game ticks)
	spawnCooldownUntil uint64

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Per-tick cache to avoid repeated queries
	drainCache []drainCacheEntry

	// Per-tick cache for soft collision detection
	quasarCache []quasarCacheEntry

	// Cached metric pointers
	statCount   *atomic.Int64
	statPending *atomic.Int64

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
	s.quasarCache = make([]quasarCacheEntry, 0, 1) // Typically 0-1 quasar

	s.statCount = s.world.Resources.Status.Ints.Get("drain.count")
	s.statPending = s.world.Resources.Status.Ints.Get("drain.pending")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DrainSystem) Init() {
	s.pendingSpawns = s.pendingSpawns[:0]
	s.drainCache = s.drainCache[:0]
	s.nextSpawnOrder = 0
	s.spawnCooldownUntil = 0
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statCount.Store(0)
	s.statPending.Store(0)
	s.paused = false
	s.enabled = true
}

// Name returns system's name
func (s *DrainSystem) Name() string {
	return "drain"
}

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
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes events
func (s *DrainSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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

	// 1. Cache all drain data for this tick
	s.cacheDrainData()

	// 2. Cache quasar data for soft collision
	s.cacheQuasarData()

	// 3. Process HP checks, enrage state, termination
	s.processDrainStates()

	// 4. Detect and trigger swarm fusions (uses cached enraged state)
	s.detectSwarmFusions()

	// Skip spawn logic when paused
	if s.paused {
		s.statCount.Store(0)
		s.statPending.Store(0)
		return
	}

	// TODO: old logic, refactor
	currentTick := s.world.Resources.Game.State.GetGameTicks()

	// Process pending materialize spawn queue first
	s.processPendingSpawns()

	// Multi-drain lifecycle based on heat
	currentCount := s.world.Components.Drain.CountEntities()
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
	if s.world.Components.Drain.CountEntities() > 0 {
		s.updateDrainMovement()
		s.handleDrainInteractions()
		s.updateDrainSigil()
	}

	s.statCount.Store(int64(s.world.Components.Drain.CountEntities()))
	s.statPending.Store(int64(len(s.pendingSpawns)))
}

// cacheDrainData populates drainCache with all drain entities and components
func (s *DrainSystem) cacheDrainData() {
	s.drainCache = s.drainCache[:0]

	drainEntities := s.world.Components.Drain.GetAllEntities()
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
}

// cacheQuasarData populates quasarCache for soft collision detection
func (s *DrainSystem) cacheQuasarData() {
	s.quasarCache = s.quasarCache[:0]

	quasarEntities := s.world.Components.Quasar.GetAllEntities()
	for _, entity := range quasarEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		s.quasarCache = append(s.quasarCache, quasarCacheEntry{
			entity: entity,
			x:      pos.X,
			y:      pos.Y,
		})
	}
}

// processDrainStates handles HP checks, enrage transitions, and termination
func (s *DrainSystem) processDrainStates() {
	for i := range s.drainCache {
		entry := &s.drainCache[i]

		// Termination check
		if entry.combatComp.HitPoints <= 0 {
			event.EmitDeathOne(s.world.Resources.Event.Queue, entry.entity, event.EventFlashSpawnOneRequest)
			continue
		}

		// Enrage state transition
		shouldEnrage := entry.combatComp.HitPoints < parameter.DrainEnrageThreshold
		if shouldEnrage != entry.combatComp.IsEnraged {
			entry.combatComp.IsEnraged = shouldEnrage
			s.world.Components.Combat.SetComponent(entry.entity, entry.combatComp)
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
		// Skip already dead drains
		if entry.combatComp.HitPoints <= 0 {
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
	}
}

// updateDrainSigil updates visual state based on combat state
func (s *DrainSystem) updateDrainSigil() {
	for i := range s.drainCache {
		entry := &s.drainCache[i]

		sigilComp, ok := s.world.Components.Sigil.GetComponent(entry.entity)
		if !ok {
			continue
		}

		// Re-fetch combat component as it may have been updated
		combatComp, ok := s.world.Components.Combat.GetComponent(entry.entity)
		if !ok {
			continue
		}

		var targetColor terminal.RGB

		// Priority: hit flash > enraged > normal
		switch {
		case combatComp.RemainingHitFlash > 0:
			targetColor = visual.RgbCombatHitFlash
		case combatComp.IsEnraged:
			targetColor = visual.RgbCombatEnraged
		default:
			targetColor = visual.RgbDrain
		}

		if sigilComp.Color != targetColor {
			sigilComp.Color = targetColor
			s.world.Components.Sigil.SetComponent(entry.entity, sigilComp)
		}
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

// hasPendingSpawns returns true if materialize spawn queue is non-empty
func (s *DrainSystem) hasPendingSpawns() bool {
	return len(s.pendingSpawns) > 0
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
			// Remove stale spawn
			s.pendingSpawns[i] = s.pendingSpawns[len(s.pendingSpawns)-1]
			s.pendingSpawns = s.pendingSpawns[:len(s.pendingSpawns)-1]
			continue
		}

		// Validate coordinates still in bounds (handles resize after queue)
		if spawn.targetX >= config.MapWidth || spawn.targetY >= config.MapHeight {
			// Remove invalid spawn
			s.pendingSpawns[i] = s.pendingSpawns[len(s.pendingSpawns)-1]
			s.pendingSpawns = s.pendingSpawns[:len(s.pendingSpawns)-1]
			continue
		}

		if !spawn.materializeStarted && currentTick >= spawn.scheduledTick {
			s.world.PushEvent(event.EventMaterializeRequest, &event.MaterializeRequestPayload{
				X:    spawn.targetX,
				Y:    spawn.targetY,
				Type: component.SpawnTypeDrain,
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
	entities := s.world.Components.Drain.GetAllEntities()
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
	queuedPositions := make(map[uint64]bool, len(s.pendingSpawns)+s.world.Components.Drain.CountEntities()+s.world.Components.Materialize.CountEntities()/4)

	// Pending spawns
	for _, ps := range s.pendingSpawns {
		key := uint64(ps.targetX)<<32 | uint64(ps.targetY)
		queuedPositions[key] = true
	}

	// Active materializer targets
	matEntities := s.world.Components.Materialize.GetAllEntities()
	for _, matEntity := range matEntities {
		if matComp, ok := s.world.Components.Materialize.GetComponent(matEntity); ok {
			key := uint64(matComp.TargetX)<<32 | uint64(matComp.TargetY)
			queuedPositions[key] = true
		}
	}

	// Existing drain positions (component iteration, not spatial query)
	drainEntities := s.world.Components.Drain.GetAllEntities()
	for _, drainEntity := range drainEntities {
		if drainPos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			key := uint64(drainPos.X)<<32 | uint64(drainPos.Y)
			queuedPositions[key] = true
		}
	}

	// Wall positions (area denial)
	wallEntities := s.world.Components.Wall.GetAllEntities()
	for _, wallEntity := range wallEntities {
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
	drainEntities := s.world.Components.Drain.GetAllEntities()
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
		event.EmitDeathOne(s.world.Resources.Event.Queue, ordered[i], event.EventFlashSpawnOneRequest)
	}
}

// materializeDrainAt creates a drain entity at the specified position
func (s *DrainSystem) materializeDrainAt(spawnX, spawnY int) {
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity
	now := s.world.Resources.Time.GameTime

	// TODO: refactor to Position bound check
	// Clamp to bounds
	if spawnX < 0 {
		spawnX = 0
	}
	if spawnX >= config.MapWidth {
		spawnX = config.MapWidth - 1
	}
	if spawnY < 0 {
		spawnY = 0
	}
	if spawnY >= config.MapHeight {
		spawnY = config.MapHeight - 1
	}

	// TODO: early defensive implementation due to flash flood, test if still needed
	// Check for existing drain
	if s.hasDrainAt(spawnX, spawnY) {
		// Collision with moved drain - re-queue at alternate position
		s.requeueSpawnWithOffset(spawnX, spawnY)
		return
	}

	entity := s.world.CreateEntity()

	pos := component.PositionComponent{
		X: spawnX,
		Y: spawnY,
	}

	// Increment and assign materialize spawn order for LIFO tracking
	s.nextSpawnOrder++

	// Initialize Kinetic with centered spawn position, zero velocity
	preciseX, preciseY := vmath.CenteredFromGrid(spawnX, spawnY)
	drainComp := component.DrainComponent{
		LastDrainTime: now,
		SpawnOrder:    s.nextSpawnOrder,
		LastIntX:      spawnX,
		LastIntY:      spawnY,
	}
	kinetic := core.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
		// VelX, VelY, AccelX, AccelY zero-initialized
	}
	kineticComp := component.KineticComponent{Kinetic: kinetic}

	// Handle collisions at materialize spawn position
	entitiesAtSpawn := s.world.Positions.GetAllEntityAt(spawnX, spawnY)
	for _, e := range entitiesAtSpawn {
		if e != cursorEntity {
			s.handleCollisionAtPosition(e)
		}
	}

	s.world.Positions.SetPosition(entity, pos)
	s.world.Components.Drain.SetComponent(entity, drainComp)
	s.world.Components.Kinetic.SetComponent(entity, kineticComp)

	// Navigation component with GA-sampled cornering parameters
	navComp := component.NavigationComponent{
		TurnThreshold:  vmath.FromFloat(parameter.GADrainTurnThresholdDefault),
		BrakeIntensity: vmath.FromFloat(parameter.GADrainBrakeIntensityDefault),
		FlowLookahead:  vmath.FromFloat(parameter.GADrainFlowLookaheadDefault),
	}

	// Sample from GA if available
	if s.world.Resources.Genetic != nil && s.world.Resources.Genetic.Provider != nil {
		genes, _ := s.world.Resources.Genetic.Provider.Sample(component.SpeciesDrain)
		if phenotype := s.world.Resources.Genetic.Provider.Decode(component.SpeciesDrain, genes); phenotype != nil {
			if p, ok := phenotype.(species.DrainPhenotype); ok {
				navComp.TurnThreshold = p.TurnThreshold
				navComp.BrakeIntensity = p.BrakeIntensity
				navComp.FlowLookahead = p.FlowLookahead
			}
		}
	}

	s.world.Components.Navigation.SetComponent(entity, navComp)

	// Combat component for interactions
	s.world.Components.Combat.SetComponent(entity,
		component.CombatComponent{
			OwnerEntity:      entity,
			CombatEntityType: component.CombatEntityDrain,
			HitPoints:        parameter.CombatInitialHPDrain,
		})

	// Visual component for sigil renderer and death system flash extraction
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  visual.DrainChar,
		Color: visual.RgbDrain,
	})
}

// requeueSpawnWithOffset attempts to find alternate position and re-queue materialize spawn when target position has become occupied since initial acquisition (e.g. another drain moved into it)
func (s *DrainSystem) requeueSpawnWithOffset(blockedX, blockedY int) {
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
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

// isInsideShieldEllipse checks if position is within the shield ellipse using Q32.32 fixed-point
func (s *DrainSystem) isInsideShieldEllipse(x, y int) bool {
	cursorEntity := s.world.Resources.Player.Entity

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if !ok {
		return false
	}

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return false
	}

	return vmath.EllipseContainsPoint(x, y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq)
}

// handleDrainInteractions processes all drain interactions per tick
func (s *DrainSystem) handleDrainInteractions() {
	cursorEntity := s.world.Resources.Player.Entity
	now := s.world.Resources.Time.GameTime

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	// 1. Detect drain-drain collisions (same cell)
	s.handleDrainDrainCollisions()

	// 2. Handle shield zone and cursor interactions
	drainEntities := s.world.Components.Drain.GetAllEntities()
	for _, drainEntity := range drainEntities {
		drain, ok := s.world.Components.Drain.GetComponent(drainEntity)
		if !ok {
			continue
		}

		drainPos, ok := s.world.Positions.GetPosition(drainEntity)
		if !ok {
			continue
		}

		// Check shield state from component
		shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
		shieldActive := ok && shield.Active

		// Shield zone interaction
		if shieldActive && s.isInsideShieldEllipse(drainPos.X, drainPos.Y) {
			// Energy drain (existing timer-based)
			if now.Sub(drain.LastDrainTime) >= parameter.DrainEnergyDrainInterval {
				s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
					Value: parameter.DrainShieldEnergyDrainAmount,
				})
				drain.LastDrainTime = now
				s.world.Components.Drain.SetComponent(drainEntity, drain)
			}

			s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackShield,
				OwnerEntity:  cursorEntity,
				OriginEntity: cursorEntity,
				TargetEntity: drainEntity,
				HitEntities:  []core.Entity{drainEntity},
			})

			continue
		}

		isOnCursor := drainPos.X == cursorPos.X && drainPos.Y == cursorPos.Y

		// Cursor collision (shield not active or drain outside shield)
		if isOnCursor {
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Delta: -parameter.DrainHeatReductionAmount,
			})
			event.EmitDeathOne(s.world.Resources.Event.Queue, drainEntity, event.EventFlashSpawnOneRequest)
		}
	}

	// 3. Handle non-drain entity collisions
	s.handleEntityCollisions()
}

// handleDrainDrainCollisions detects and removes all drains sharing a cell
func (s *DrainSystem) handleDrainDrainCollisions() {
	// Build position -> drain entities map
	drainPositions := make(map[uint64][]core.Entity)

	drainEntities := s.world.Components.Drain.GetAllEntities()
	for _, drainEntity := range drainEntities {
		drainPos, ok := s.world.Positions.GetPosition(drainEntity)
		if !ok {
			continue
		}
		posKey := uint64(drainPos.X)<<32 | uint64(drainPos.Y)
		drainPositions[posKey] = append(drainPositions[posKey], drainEntity)
	}

	// Find and destroy all drains at cells with multiple drains
	for _, drainEntitiesAtPosition := range drainPositions {
		if len(drainEntitiesAtPosition) > 1 {
			for _, colocatedDrainEntity := range drainEntitiesAtPosition {
				event.EmitDeathOne(s.world.Resources.Event.Queue, colocatedDrainEntity, event.EventFlashSpawnOneRequest)
			}
		}
	}
}

// handleEntityCollisions processes collisions with non-drain entities
func (s *DrainSystem) handleEntityCollisions() {
	cursorEntity := s.world.Resources.Player.Entity

	entities := s.world.Components.Drain.GetAllEntities()
	for _, entity := range entities {
		drainPos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		targets := s.world.Positions.GetAllEntityAt(drainPos.X, drainPos.Y)

		for _, target := range targets {
			if target != 0 && target != entity && target != cursorEntity {
				// Skip other drains (handled separately)
				if _, ok := s.world.Components.Drain.GetComponent(target); ok {
					continue
				}
				// Skip walls - handled by physics, not collision
				if s.world.Components.Wall.HasEntity(target) {
					continue
				}
				s.handleCollisionAtPosition(target)
			}
		}
	}
}

// updateDrainMovement handles continuous kinetic drain movement toward cursor
func (s *DrainSystem) updateDrainMovement() {
	config := s.world.Resources.Config
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	// Target cursor center
	cursorXFixed, cursorYFixed := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)

	var collisionBuf [parameter.MaxEntitiesPerCell]core.Entity

	drainEntities := s.world.Components.Drain.GetAllEntities()
	for _, drainEntity := range drainEntities {
		drainComp, ok := s.world.Components.Drain.GetComponent(drainEntity)
		if !ok {
			continue
		}
		combatComp, ok := s.world.Components.Combat.GetComponent(drainEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(drainEntity)
		if !ok {
			continue
		}

		// 1. Navigation & Targeting
		targetX, targetY := cursorXFixed, cursorYFixed
		navComp, hasNav := s.world.Components.Navigation.GetComponent(drainEntity)

		if hasNav {
			// TODO: remove
			s.world.DebugPrint(DebugGeneString(&navComp))

			if navComp.HasDirectPath {
				// Open Space: Ignore grid, fly straight to cursor (Euclidean), fixing "Curve/Pinball" artifacts in open areas if only done based on flow field (at some process expense)
				targetX, targetY = cursorXFixed, cursorYFixed
			} else if navComp.FlowX != 0 || navComp.FlowY != 0 {
				// Blocked: Use Flow Field
				targetX = kineticComp.PreciseX + vmath.Mul(navComp.FlowX, navComp.FlowLookahead)
				targetY = kineticComp.PreciseY + vmath.Mul(navComp.FlowY, navComp.FlowLookahead)
			} else {
				// Flow is zero (Lost/Stuck). Snap to cursor if very close to prevent wall grinding
				distToCursor := vmath.DistanceApprox(kineticComp.PreciseX-cursorXFixed, kineticComp.PreciseY-cursorYFixed)
				if distToCursor < vmath.FromInt(2) {
					targetX, targetY = cursorXFixed, cursorYFixed
				}
			}
		} else {
			// No navigation component: Direct Homing
			targetX, targetY = cursorXFixed, cursorYFixed
		}

		// 2. Physics with GA-optimized cornering
		if combatComp.RemainingKineticImmunity == 0 {
			appliedDrag := parameter.DrainDrag

			// Cornering drag: check turn alignment when moving
			currentSpeed := vmath.Magnitude(kineticComp.VelX, kineticComp.VelY)
			if currentSpeed > vmath.Scale { // Moving at least 1 cell/sec
				// Normalized velocity
				nx := vmath.Div(kineticComp.VelX, currentSpeed)
				ny := vmath.Div(kineticComp.VelY, currentSpeed)

				// Direction to target
				dx := targetX - kineticComp.PreciseX
				dy := targetY - kineticComp.PreciseY
				dnx, dny := vmath.Normalize2D(dx, dy)

				// Dot product: 1.0 = aligned, 0 = perpendicular, -1 = opposite
				alignment := vmath.DotProduct(nx, ny, dnx, dny)

				// Apply cornering drag using GA parameters from navigation component
				if alignment < navComp.TurnThreshold {
					turnSeverity := navComp.TurnThreshold - alignment
					brakeMult := vmath.Scale + vmath.Mul(turnSeverity, navComp.BrakeIntensity)
					appliedDrag = vmath.Mul(parameter.DrainDrag, brakeMult)
				}
			}

			physics.ApplyHomingScaled(
				&kineticComp.Kinetic,
				targetX, targetY,
				&physics.DrainHoming,
				vmath.Scale,
				dtFixed,
				true,
			)

			// Apply cornering drag separately if higher than base
			if appliedDrag > parameter.DrainDrag {
				extraDrag := appliedDrag - parameter.DrainDrag
				dragFactor := vmath.Scale - vmath.Mul(extraDrag, dtFixed)
				if dragFactor < 0 {
					dragFactor = 0
				}
				kineticComp.VelX = vmath.Mul(kineticComp.VelX, dragFactor)
				kineticComp.VelY = vmath.Mul(kineticComp.VelY, dragFactor)
			}
		}

		// 3. Integration & Collision
		oldPreciseX, oldPreciseY := kineticComp.PreciseX, kineticComp.PreciseY
		newX, newY := physics.Integrate(&kineticComp.Kinetic, dtFixed)

		// Boundary Reflection
		if newX < 0 || newX >= gameWidth {
			physics.ReflectBoundsX(&kineticComp.Kinetic, 0, gameWidth)
			newX = vmath.ToInt(kineticComp.PreciseX)
		}
		if newY < 0 || newY >= gameHeight {
			physics.ReflectBoundsY(&kineticComp.Kinetic, 0, gameHeight)
			newY = vmath.ToInt(kineticComp.PreciseY)
		}

		// Soft Collision (Quasar)
		if combatComp.RemainingKineticImmunity == 0 {
			s.applySoftCollisionWithQuasar(&kineticComp, &combatComp, newX, newY)
		}

		// Wall Collision (Traversal)
		lastSafeX, lastSafeY := drainComp.LastIntX, drainComp.LastIntY
		hitWall := false

		vmath.Traverse(oldPreciseX, oldPreciseY, kineticComp.PreciseX, kineticComp.PreciseY, func(x, y int) bool {
			if x < 0 || x >= gameWidth || y < 0 || y >= gameHeight {
				return true
			}
			if x == drainComp.LastIntX && y == drainComp.LastIntY {
				return true
			}

			if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
				s.reflectOffWall(&kineticComp.Kinetic, lastSafeX, lastSafeY, x, y)
				hitWall = true
				return false
			}

			lastSafeX, lastSafeY = x, y

			// Entity-Entity Collision
			count := s.world.Positions.GetAllEntitiesAtInto(x, y, collisionBuf[:])
			for i := 0; i < count; i++ {
				target := collisionBuf[i]
				if target == 0 || target == drainEntity || target == cursorEntity {
					continue
				}
				if s.world.Components.Drain.HasEntity(target) {
					continue
				}
				s.handleCollisionAtPosition(target)
			}
			return true
		})

		if hitWall {
			newX, newY = lastSafeX, lastSafeY
		}

		// Update Position Component
		if newX != drainComp.LastIntX || newY != drainComp.LastIntY {
			drainComp.LastIntX = newX
			drainComp.LastIntY = newY
			s.world.Positions.SetPosition(drainEntity, component.PositionComponent{X: newX, Y: newY})
		}

		s.world.Components.Drain.SetComponent(drainEntity, drainComp)
		s.world.Components.Kinetic.SetComponent(drainEntity, kineticComp)
		s.world.Components.Combat.SetComponent(drainEntity, combatComp)
	}
}

// reflectOffWall reflects kinetic velocity when hitting wall at (wallX, wallY) from (fromX, fromY)
// Snaps position back to fromX/fromY cell center
func (s *DrainSystem) reflectOffWall(k *core.Kinetic, fromX, fromY, wallX, wallY int) {
	dx := wallX - fromX
	dy := wallY - fromY

	// Reflect based on approach axis
	if dx != 0 {
		k.VelX = -k.VelX
	}
	if dy != 0 {
		k.VelY = -k.VelY
	}

	// Snap back to safe cell center
	k.PreciseX, k.PreciseY = vmath.CenteredFromGrid(fromX, fromY)
}

// applySoftCollisionWithQuasar checks drain overlap with quasar and applies repulsion
func (s *DrainSystem) applySoftCollisionWithQuasar(
	kineticComp *component.KineticComponent,
	combatComp *component.CombatComponent,
	drainX, drainY int,
) {
	for _, qc := range s.quasarCache {
		radialX, radialY, hit := physics.CheckSoftCollision(
			drainX, drainY, qc.x, qc.y,
			parameter.QuasarCollisionInvRxSq, parameter.QuasarCollisionInvRySq,
		)

		if hit {
			physics.ApplyCollision(
				&kineticComp.Kinetic,
				radialX, radialY,
				&physics.SoftCollisionQuasarToDrain,
				s.rng,
			)

			combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
			return // One collision per tick
		}
	}
}

// handleCollisionAtPosition processes collision with a specific entity at a given position
func (s *DrainSystem) handleCollisionAtPosition(entity core.Entity) {
	cursorEntity := s.world.Resources.Player.Entity

	// Check protection before any collision handling
	if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protComp.Mask.Has(component.ProtectFromDrain) {
			return
		}
	}

	// Skip cursor entity
	if entity == cursorEntity {
		return
	}

	// Convert glyphs to dust
	if s.world.Components.Glyph.HasEntity(entity) {
		event.EmitDeathOne(s.world.Resources.Event.Queue, entity, event.EventDustSpawnOneRequest)
		return
	}

	// Check if it's a nugget, notify destruction
	if s.world.Components.Nugget.HasEntity(entity) {
		s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
			Entity: entity,
		})
	}

	// Destroy the entity
	event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)
}

// DebugGeneString returns formatted GA gene string for status bar
// Format: [GA: T=0.80 B=3.0 L=12]
func DebugGeneString(nav *component.NavigationComponent) string {
	// Convert Q32.32 to float for display
	turnThresh := float64(nav.TurnThreshold) / float64(1<<32)
	brakeInt := float64(nav.BrakeIntensity) / float64(1<<32)
	flowLook := float64(nav.FlowLookahead) / float64(1<<32)

	return fmt.Sprintf("[GA: T=%.2f B=%.1f L=%.0f]", turnThresh, brakeInt, flowLook)
}