package system

// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	mu sync.Mutex
	engine.SystemBase

	spawned           map[int64]bool // Track which frames already spawned cleaners
	hasSpawnedSession bool           // Track if we spawned cleaners this session

	deflectedAnchors map[core.Entity]core.Entity // anchor -> cleaner that deflected it for deduplication of large entity hits

	rng *vmath.FastRand

	statActive  *atomic.Int64
	statSpawned *atomic.Int64

	enabled bool
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(world *engine.World) engine.System {
	s := &CleanerSystem{
		SystemBase: engine.NewSystemBase(world),
	}

	s.spawned = make(map[int64]bool)

	s.statActive = s.Resource.Status.Ints.Get("cleaner.active")
	s.statSpawned = s.Resource.Status.Ints.Get("cleaner.spawned")

	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *CleanerSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *CleanerSystem) initLocked() {
	clear(s.spawned)
	s.hasSpawnedSession = false
	s.rng = vmath.NewFastRand(uint32(s.Resource.Time.RealTime.UnixNano()))
	s.deflectedAnchors = make(map[core.Entity]core.Entity, 4)
	s.enabled = true
}

// Priority returns the system's priority
func (s *CleanerSystem) Priority() int {
	return constant.PriorityCleaner
}

// EventTypes returns the event types CleanerSystem handles
func (s *CleanerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCleanerSweepingRequest,
		event.EventCleanerDirectionalRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes cleaner-related events from the router
func (s *CleanerSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	// Check if we already spawned for this frame (deduplication)
	if s.spawned[ev.Frame] {
		return
	}

	switch ev.Type {
	case event.EventCleanerSweepingRequest:
		s.spawnCleaners()
		s.spawned[ev.Frame] = true
		s.hasSpawnedSession = true

	case event.EventCleanerDirectionalRequest:
		if payload, ok := ev.Payload.(*event.DirectionalCleanerPayload); ok {
			s.spawnDirectionalCleaners(payload.OriginX, payload.OriginY)
			s.spawned[ev.Frame] = true
			s.hasSpawnedSession = true
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.Resource.Config

	// Clean old entries from spawned map
	currentFrame := s.Resource.Time.FrameNumber
	for frame := range s.spawned {
		if currentFrame-frame > constant.CleanerDeduplicationWindow {
			delete(s.spawned, frame)
		}
	}

	entities := s.Component.Cleaner.All()
	s.statActive.Store(int64(len(entities)))

	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(entities) == 0 && s.hasSpawnedSession {
		s.World.PushEvent(event.EventCleanerSweepingFinished, nil)
		s.hasSpawnedSession = false
		return
	}

	// Clean dead cleaners from deflection tracking
	for anchor, cleaner := range s.deflectedAnchors {
		if !s.Component.Cleaner.Has(cleaner) {
			delete(s.deflectedAnchors, anchor)
		}
	}

	// Early return if no cleaners
	if len(entities) == 0 {
		return
	}

	dtFixed := vmath.FromFloat(s.Resource.Time.DeltaTime.Seconds())
	gameWidth := config.GameWidth
	gameHeight := config.GameHeight

	for _, entity := range entities {
		c, ok := s.Component.Cleaner.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore (authoritative for spatial queries)
		oldPos, hasPos := s.World.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Update: Integrate velocity into float position (overlay state)
		prevPreciseX := c.PreciseX
		prevPreciseY := c.PreciseY
		c.Integrate(dtFixed)

		// Swept Collision Detection: Check all cells between previous and current position
		if c.VelY != 0 && c.VelX == 0 {
			// Vertical cleaner: sweep Y axis
			prevY := vmath.ToInt(prevPreciseY)
			currY := vmath.ToInt(c.PreciseY)
			startY, endY := prevY, currY
			if startY > endY {
				startY, endY = endY, startY
			}

			if startY < 0 {
				startY = 0
			}
			if endY >= gameHeight {
				endY = gameHeight - 1
			}

			// Check all traversed rows for collisions (with self-exclusion)
			if startY <= endY {
				for y := startY; y <= endY; y++ {
					s.checkCollisions(oldPos.X, y, entity)
				}
			}
		} else if c.VelX != 0 {
			// Horizontal cleaner: sweep X axis
			prevX := vmath.ToInt(prevPreciseX)
			currX := vmath.ToInt(c.PreciseX)
			startX, endX := prevX, currX
			if startX > endX {
				startX, endX = endX, startX
			}

			if startX < 0 {
				startX = 0
			}
			if endX >= gameWidth {
				endX = gameWidth - 1
			}

			// Check all traversed columns for collisions (with self-exclusion)
			if startX <= endX {
				for x := startX; x <= endX; x++ {
					s.checkCollisions(x, oldPos.Y, entity)
				}
			}
		}

		// Trail Update & Grid Sync: Update trail ring buffer and sync PositionStore if cell changed
		newGridX := vmath.ToInt(c.PreciseX)
		newGridY := vmath.ToInt(c.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			c.TrailHead = (c.TrailHead + 1) % constant.CleanerTrailLength
			c.TrailRing[c.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if c.TrailLen < constant.CleanerTrailLength {
				c.TrailLen++
			}

			// Sync grid position to PositionStore
			s.World.Positions.Set(entity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		// Lifecycle Check: Destroy cleaner when it reaches target position
		shouldDestroy := false
		if c.VelX > 0 && c.PreciseX >= c.TargetX {
			shouldDestroy = true
		} else if c.VelX < 0 && c.PreciseX <= c.TargetX {
			shouldDestroy = true
		} else if c.VelY > 0 && c.PreciseY >= c.TargetY {
			shouldDestroy = true
		} else if c.VelY < 0 && c.PreciseY <= c.TargetY {
			shouldDestroy = true
		}

		if shouldDestroy {
			s.World.DestroyEntity(entity)
		} else {
			s.Component.Cleaner.Set(entity, c)
		}
	}

	entities = s.Component.Cleaner.All()
	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(entities) == 0 && s.hasSpawnedSession {
		s.World.PushEvent(event.EventCleanerSweepingFinished, nil)
		s.hasSpawnedSession = false
	}
}

// spawnCleaners generates cleaner entities using generic stores
func (s *CleanerSystem) spawnCleaners() {
	config := s.Resource.Config

	rows := s.scanTargetRows()

	spawnCount := len(rows)
	// No rows to clean, trigger fuse drains if not in grayout
	if spawnCount == 0 {
		if !s.Resource.State.State.GrayoutPersist.Load() {
			s.World.PushEvent(event.EventFuseDrains, nil)
		}
		s.World.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}
	s.statSpawned.Add(int64(spawnCount))

	s.World.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	// Determine energy polarity once for entire batch
	negativeEnergy := false
	if energyComp, ok := s.Component.Energy.Get(s.Resource.Cursor.Entity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	gameWidthFixed := vmath.FromInt(config.GameWidth)
	trailLenFixed := vmath.FromInt(constant.CleanerTrailLength)
	durationFixed := vmath.FromFloat(constant.CleanerAnimationDuration.Seconds())
	baseSpeed := vmath.Div(gameWidthFixed, durationFixed)

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range rows {
		var startX, targetX, velX int32
		rowFixed := vmath.FromInt(row)

		if row%2 != 0 {
			// Left to right
			startX = -trailLenFixed
			targetX = gameWidthFixed + trailLenFixed
			velX = baseSpeed
		} else {
			// Right to left
			startX = gameWidthFixed + trailLenFixed
			targetX = -trailLenFixed
			velX = -baseSpeed
		}

		startGridX := vmath.ToInt(startX)
		startGridY := row

		// Initialize trail ring buffer with starting position
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			KineticState: component.KineticState{
				PreciseX: startX,
				PreciseY: rowFixed,
				VelX:     velX,
				VelY:     0,
			},
			TargetX:        targetX,
			TargetY:        rowFixed,
			TrailRing:      trailRing,
			TrailHead:      0,
			TrailLen:       1,
			Char:           constant.CleanerChar,
			NegativeEnergy: negativeEnergy,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.World.CreateEntity()
		s.World.Positions.Set(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.Component.Cleaner.Set(entity, comp)
		s.Component.Protection.Set(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// checkCollisions handles collision logic with self-exclusion
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity core.Entity) {
	// Query all entities at position (includes cleaner itself due to PositionStore registration)
	targetEntities := s.World.Positions.GetAllAt(x, y)
	if len(targetEntities) == 0 {
		return
	}

	// Get cleaner velocity for drain deflection
	cleaner, ok := s.Component.Cleaner.Get(selfEntity)
	if !ok {
		return
	}

	// Deflect drains (energy-independent, cleaner passes through)
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}
		if s.Component.Drain.Has(e) {
			s.deflectDrain(e, cleaner.VelX, cleaner.VelY)
		}
	}

	// Deflect quasar composites
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}
		member, ok := s.Component.Member.Get(e)
		if !ok {
			continue
		}
		if lastCleaner, exists := s.deflectedAnchors[member.AnchorID]; exists && lastCleaner == selfEntity {
			continue
		}
		header, ok := s.Component.Header.Get(member.AnchorID)
		if !ok {
			continue
		}
		if header.BehaviorID == component.BehaviorQuasar {
			s.deflectQuasar(member.AnchorID, e, cleaner.VelX, cleaner.VelY)
			s.deflectedAnchors[member.AnchorID] = selfEntity
		}
	}

	// Determine mode based on energy polarity
	cursorEntity := s.Resource.Cursor.Entity
	negativeEnergy := false
	if energyComp, ok := s.Component.Energy.Get(cursorEntity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	if negativeEnergy {
		s.processNegativeEnergy(x, y, targetEntities, selfEntity)
	} else {
		s.processPositiveEnergy(targetEntities, selfEntity)
	}
}

// deflectDrain applies deflection impulse to a drain entity
// Physics-based impulse - additive to drain velocity, direction from cleaner
func (s *CleanerSystem) deflectDrain(drainEntity core.Entity, cleanerVelX, cleanerVelY int32) {
	drain, ok := s.Component.Drain.Get(drainEntity)
	if !ok {
		return
	}

	// Calculate collision impulse from cleaner velocity
	impulseX, impulseY := vmath.ApplyCollisionImpulse(
		cleanerVelX, cleanerVelY,
		vmath.MassRatioEqual,
		constant.DrainDeflectAngleVar,
		constant.DrainDeflectImpulseMin,
		constant.DrainDeflectImpulseMax,
		s.rng,
	)

	// Zero impulse fallback (cleaner stationary - shouldn't happen)
	if impulseX == 0 && impulseY == 0 {
		return
	}

	// Add impulse to current velocity (physics-based momentum transfer)
	drain.VelX += impulseX
	drain.VelY += impulseY

	// Set immunity window
	drain.DeflectUntil = s.Resource.Time.GameTime.Add(constant.DrainDeflectImmunity)

	s.Component.Drain.Set(drainEntity, drain)
}

// deflectQuasar applies offset-aware collision impulse to quasar composite
func (s *CleanerSystem) deflectQuasar(anchorEntity, hitMember core.Entity, cleanerVelX, cleanerVelY int32) {
	quasar, ok := s.Component.Quasar.Get(anchorEntity)
	if !ok {
		return
	}

	// Shield blocks all cleaner interaction
	if quasar.ShieldActive {
		return
	}

	// Flash immunity blocks new damage (debounce)
	if quasar.HitFlashRemaining > 0 {
		return
	}

	// Apply damage and start flash
	quasar.HitPoints--
	quasar.HitFlashRemaining = constant.QuasarHitFlashDuration

	// Knockback only when not enraged
	isEnraged := quasar.IsCharging || quasar.IsZapping
	if !isEnraged {
		anchorPos, ok := s.World.Positions.Get(anchorEntity)
		if !ok {
			s.Component.Quasar.Set(anchorEntity, quasar)
			return
		}
		hitPos, ok := s.World.Positions.Get(hitMember)
		if !ok {
			s.Component.Quasar.Set(anchorEntity, quasar)
			return
		}

		offsetX := hitPos.X - anchorPos.X
		offsetY := hitPos.Y - anchorPos.Y

		impulseX, impulseY := vmath.ApplyOffsetCollisionImpulse(
			cleanerVelX, cleanerVelY,
			offsetX, offsetY,
			vmath.OffsetInfluenceDefault,
			vmath.MassRatioCleanerToQuasar,
			constant.DrainDeflectAngleVar,
			constant.QuasarDeflectImpulseMin,
			constant.QuasarDeflectImpulseMax,
			s.rng,
		)

		if impulseX == 0 && impulseY == 0 {
			return
		}

		// Reset velocity and apply impulse (clean knockback)
		quasar.VelX = impulseX
		quasar.VelY = impulseY

		quasar.DeflectUntil = s.Resource.Time.GameTime.Add(constant.QuasarHitFlashDuration)
	}

	s.Component.Quasar.Set(anchorEntity, quasar)
}

// processPositiveEnergy handles Red destruction with Blossom spawnLightning
func (s *CleanerSystem) processPositiveEnergy(targetEntities []core.Entity, selfEntity core.Entity) {
	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}
		if glyph, ok := s.Component.Glyph.Get(e); ok {
			if glyph.Type == component.GlyphRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	event.EmitDeathBatch(s.Resource.Events.Queue, event.EventBlossomSpawnOne, toDestroy, s.Resource.Time.FrameNumber)
}

// processNegativeEnergy handles Blue mutation to Green with Decay spawnLightning
func (s *CleanerSystem) processNegativeEnergy(x, y int, targetEntities []core.Entity, selfEntity core.Entity) {
	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}

		glyph, ok := s.Component.Glyph.Get(e)
		if !ok || glyph.Type != component.GlyphBlue {
			continue
		}

		// Mutate Blue → Green, preserving level
		glyph.Type = component.GlyphGreen
		s.Component.Glyph.Set(e, glyph)

		// Spawn decay at same position (particle skips starting cell via LastIntX/Y)
		s.World.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             x,
			Y:             y,
			Char:          glyph.Rune,
			SkipStartCell: true,
		})
	}
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (s *CleanerSystem) spawnDirectionalCleaners(originX, originY int) {
	config := s.Resource.Config

	s.World.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	// Determine energy polarity once for entire batch
	negativeEnergy := false
	if energyComp, ok := s.Component.Energy.Get(s.Resource.Cursor.Entity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	gameWidthFixed := vmath.FromInt(config.GameWidth)
	gameHeightFixed := vmath.FromInt(config.GameHeight)
	trailLenFixed := vmath.FromInt(constant.CleanerTrailLength)
	durationFixed := vmath.FromFloat(constant.CleanerAnimationDuration.Seconds())

	horizontalSpeed := vmath.Div(gameWidthFixed, durationFixed)
	verticalSpeed := vmath.Div(gameHeightFixed, durationFixed)

	oxFixed := vmath.FromInt(originX)
	oyFixed := vmath.FromInt(originY)

	type dirDef struct {
		velX, velY       int32
		startX, startY   int32
		targetX, targetY int32
	}

	directions := []dirDef{
		{horizontalSpeed, 0, oxFixed, oyFixed, gameWidthFixed + trailLenFixed, oyFixed},
		{-horizontalSpeed, 0, oxFixed, oyFixed, -trailLenFixed, oyFixed},
		{0, verticalSpeed, oxFixed, oyFixed, oxFixed, gameHeightFixed + trailLenFixed},
		{0, -verticalSpeed, oxFixed, oyFixed, oxFixed, -trailLenFixed},
	}

	// Spawn 4 cleaners from origin, each traveling in a cardinal direction
	for _, dir := range directions {
		startGridX := vmath.ToInt(dir.startX)
		startGridY := vmath.ToInt(dir.startY)

		// Initialize trail ring buffer with starting position
		var trailRing [constant.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.CleanerComponent{
			KineticState: component.KineticState{
				PreciseX: dir.startX,
				PreciseY: dir.startY,
				VelX:     dir.velX,
				VelY:     dir.velY,
			},
			TargetX:        dir.targetX,
			TargetY:        dir.targetY,
			TrailRing:      trailRing,
			TrailHead:      0,
			TrailLen:       1,
			Char:           constant.CleanerChar,
			NegativeEnergy: negativeEnergy,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.World.CreateEntity()
		s.World.Positions.Set(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.Component.Cleaner.Set(entity, comp)
		// TODO: centralize protection via entity factory
		s.Component.Protection.Set(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// scanTargetRows finds rows containing target character type based on energy polarity
// Returns rows with TypeRed (energy >= 0) or TypeBlue (energy < 0)
func (s *CleanerSystem) scanTargetRows() []int {
	config := s.Resource.Config
	gameHeight := config.GameHeight

	// Determine target type based on energy polarity
	targetType := component.GlyphRed
	cursorEntity := s.Resource.Cursor.Entity
	if energyComp, ok := s.Component.Energy.Get(cursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			targetType = component.GlyphBlue
		}
	}

	targetRows := make(map[int]bool)

	entities := s.World.Query().
		With(s.Component.Glyph).
		With(s.World.Positions).
		Execute()

	for _, entity := range entities {
		glyph, ok := s.Component.Glyph.Get(entity)
		if !ok || glyph.Type != targetType {
			continue
		}

		pos, hasPos := s.World.Positions.Get(entity)
		if !hasPos {
			continue
		}

		if pos.Y >= 0 && pos.Y < gameHeight {
			targetRows[pos.Y] = true
		}
	}

	rows := make([]int, 0, len(targetRows))
	for row := range targetRows {
		rows = append(rows, row)
	}
	return rows
}