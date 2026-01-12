package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// directionalSpawnKey uniquely identifies a directional cleaner spawn request
type directionalSpawnKey struct {
	frame   int64
	originX int
	originY int
}

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	world *engine.World

	sweepingFrames    map[int64]bool               // Frame-based dedup for sweeping cleaners
	directionalSpawns map[directionalSpawnKey]bool // Positions-aware dedup for directional cleaners
	hasSpawnedSession bool                         // Track if we spawned cleaners this session

	deflectedAnchors map[core.Entity]core.Entity // anchor -> cleaner that deflected it for deduplication of large entity hits

	rng *vmath.FastRand

	statActive  *atomic.Int64
	statSpawned *atomic.Int64

	enabled bool
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(world *engine.World) engine.System {
	s := &CleanerSystem{
		world: world,
	}

	s.sweepingFrames = make(map[int64]bool)
	s.directionalSpawns = make(map[directionalSpawnKey]bool)

	s.statActive = s.world.Resources.Status.Ints.Get("cleaner.active")
	s.statSpawned = s.world.Resources.Status.Ints.Get("cleaner.spawned")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *CleanerSystem) Init() {
	clear(s.sweepingFrames)
	clear(s.directionalSpawns)
	s.hasSpawnedSession = false
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.deflectedAnchors = make(map[core.Entity]core.Entity, 4)
	s.statActive.Store(0)
	s.statSpawned.Store(0)
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

	switch ev.Type {
	case event.EventCleanerSweepingRequest:
		// Frame-only deduplication for sweeping (one global sweep per frame)
		if s.sweepingFrames[ev.Frame] {
			return
		}
		s.spawnCleaners()
		s.sweepingFrames[ev.Frame] = true
		s.hasSpawnedSession = true

	case event.EventCleanerDirectionalRequest:
		if payload, ok := ev.Payload.(*event.DirectionalCleanerPayload); ok {
			// Positions-aware deduplication for directional cleaners
			key := directionalSpawnKey{
				frame:   ev.Frame,
				originX: payload.OriginX,
				originY: payload.OriginY,
			}
			if s.directionalSpawns[key] {
				return
			}
			s.spawnDirectionalCleaners(payload.OriginX, payload.OriginY)
			s.directionalSpawns[key] = true
			s.hasSpawnedSession = true
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Clean old entries from deduplication maps
	currentFrame := s.world.Resources.Time.FrameNumber
	for frame := range s.sweepingFrames {
		if currentFrame-frame > constant.CleanerDeduplicationWindow {
			delete(s.sweepingFrames, frame)
		}
	}
	for key := range s.directionalSpawns {
		if currentFrame-key.frame > constant.CleanerDeduplicationWindow {
			delete(s.directionalSpawns, key)
		}
	}

	cleanerEntities := s.world.Components.Cleaner.AllEntities()
	s.statActive.Store(int64(len(cleanerEntities)))

	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(cleanerEntities) == 0 && s.hasSpawnedSession {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		s.hasSpawnedSession = false
		return
	}

	// Clean dead cleaners from deflection tracking
	for anchor, cleanerEntity := range s.deflectedAnchors {
		if !s.world.Components.Cleaner.HasEntity(cleanerEntity) {
			delete(s.deflectedAnchors, anchor)
		}
	}

	// Early return if no cleaners
	if len(cleanerEntities) == 0 {
		return
	}

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	gameWidth := config.GameWidth
	gameHeight := config.GameHeight

	for _, entity := range cleanerEntities {
		c, ok := s.world.Components.Cleaner.GetComponent(entity)
		if !ok {
			continue
		}

		// Read grid position from Positions (authoritative for spatial queries)
		oldPos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
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

		// Trail Update & Grid Sync: Update trail ring buffer and sync Positions if cell changed
		newGridX := vmath.ToInt(c.PreciseX)
		newGridY := vmath.ToInt(c.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			c.TrailHead = (c.TrailHead + 1) % constant.CleanerTrailLength
			c.TrailRing[c.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if c.TrailLen < constant.CleanerTrailLength {
				c.TrailLen++
			}

			// Sync grid position to Positions
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: newGridX, Y: newGridY})
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
			s.world.DestroyEntity(entity)
		} else {
			s.world.Components.Cleaner.SetComponent(entity, c)
		}
	}

	cleanerEntities = s.world.Components.Cleaner.AllEntities()
	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(cleanerEntities) == 0 && s.hasSpawnedSession {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		s.hasSpawnedSession = false
	}
}

// spawnCleaners generates cleaner entities using generic stores
func (s *CleanerSystem) spawnCleaners() {
	config := s.world.Resources.Config

	rows := s.scanTargetRows()

	spawnCount := len(rows)
	// No rows to clean, trigger fuse drains if not in grayout
	if spawnCount == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}
	s.statSpawned.Add(int64(spawnCount))

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	// Determine energy polarity once for entire batch
	negativeEnergy := false
	if energyComp, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Cursor.Entity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	gameWidthFixed := vmath.FromInt(config.GameWidth)
	trailLenFixed := vmath.FromInt(constant.CleanerTrailLength)
	durationFixed := vmath.FromFloat(constant.CleanerAnimationDuration.Seconds())
	baseSpeed := vmath.Div(gameWidthFixed, durationFixed)

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range rows {
		var startX, targetX, velX int64
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
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, comp)
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// checkCollisions handles collision logic with self-exclusion
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity core.Entity) {
	// Query all entities at position (includes cleaner itself due to Positions registration)
	entities := s.world.Positions.GetAllEntityAt(x, y)
	if len(entities) == 0 {
		return
	}

	// Get cleaner velocity for deflection
	cleanerComp, ok := s.world.Components.Cleaner.GetComponent(selfEntity)
	if !ok {
		return
	}

	// Deflect drains and quasar (energy-independent, cleaner passes through)
	for _, entity := range entities {
		if entity == 0 || entity == selfEntity {
			continue
		}

		// Drain
		if s.world.Components.Drain.HasEntity(entity) {
			s.deflectDrain(entity, cleanerComp.VelX, cleanerComp.VelY)
		}

		// Quasar
		memberComp, ok := s.world.Components.Member.GetComponent(entity)
		if !ok {
			continue
		}
		if lastCleaner, exists := s.deflectedAnchors[memberComp.HeaderEntity]; exists && lastCleaner == selfEntity {
			continue
		}
		headerComp, ok := s.world.Components.Header.GetComponent(memberComp.HeaderEntity)
		if !ok {
			continue
		}
		if headerComp.Behavior == component.BehaviorQuasar {
			s.deflectQuasar(memberComp.HeaderEntity, entity, cleanerComp.VelX, cleanerComp.VelY)
			s.deflectedAnchors[memberComp.HeaderEntity] = selfEntity
		}
	}

	// Determine mode based on energy polarity
	cursorEntity := s.world.Resources.Cursor.Entity
	negativeEnergy := false
	if energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		negativeEnergy = energyComp.Current.Load() < 0
	}

	if negativeEnergy {
		s.processNegativeEnergy(x, y, entities, selfEntity)
	} else {
		s.processPositiveEnergy(entities, selfEntity)
	}
}

// deflectDrain applies deflection impulse to a drain entity
// Physics-based impulse - additive to drain velocity, direction from cleaner
func (s *CleanerSystem) deflectDrain(drainEntity core.Entity, cleanerVelX, cleanerVelY int64) {
	drain, ok := s.world.Components.Drain.GetComponent(drainEntity)
	if !ok {
		return
	}

	now := s.world.Resources.Time.GameTime

	if physics.ApplyCollision(&drain.KineticState, cleanerVelX, cleanerVelY, &physics.CleanerToDrain, s.rng, now) {
		s.world.Components.Drain.SetComponent(drainEntity, drain)
	}
}

// deflectQuasar applies offset-aware collision impulse to quasar composite
func (s *CleanerSystem) deflectQuasar(headerEntity, hitMember core.Entity, cleanerVelX, cleanerVelY int64) {
	quasarComp, ok := s.world.Components.Quasar.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Shield blocks all cleaner interaction
	if quasarComp.IsShielded {
		return
	}

	// Flash immunity blocks new damage (debounce)
	if quasarComp.HitFlashRemaining > 0 {
		return
	}

	// Apply damage and start flash
	quasarComp.HitPoints--
	quasarComp.HitFlashRemaining = constant.QuasarHitFlashDuration

	// Knockback only when not enraged
	isEnraged := quasarComp.IsCharging || quasarComp.IsZapping
	if !isEnraged {
		anchorPos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)
			return
		}
		hitPos, ok := s.world.Positions.GetPosition(hitMember)
		if !ok {
			s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)
			return
		}

		offsetX := hitPos.X - anchorPos.X
		offsetY := hitPos.Y - anchorPos.Y

		now := s.world.Resources.Time.GameTime
		physics.ApplyOffsetCollision(
			&quasarComp.KineticState,
			cleanerVelX, cleanerVelY,
			offsetX, offsetY,
			&physics.CleanerToQuasar,
			s.rng,
			now,
		)
	}

	s.world.Components.Quasar.SetComponent(headerEntity, quasarComp)
}

// processPositiveEnergy handles Red destruction with Blossom spawn
func (s *CleanerSystem) processPositiveEnergy(targetEntities []core.Entity, selfEntity core.Entity) {
	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}
		if glyph, ok := s.world.Components.Glyph.GetComponent(e); ok {
			if glyph.Type == component.GlyphRed {
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventBlossomSpawnOne, toDestroy, s.world.Resources.Time.FrameNumber)
}

// processNegativeEnergy handles Blue mutation to Green with Decay spawn
func (s *CleanerSystem) processNegativeEnergy(x, y int, targetEntities []core.Entity, selfEntity core.Entity) {
	// Iterate candidates with self-exclusion pattern
	for _, e := range targetEntities {
		if e == 0 || e == selfEntity {
			continue
		}

		glyph, ok := s.world.Components.Glyph.GetComponent(e)
		if !ok || glyph.Type != component.GlyphBlue {
			continue
		}

		// Mutate Blue → Green, preserving level
		glyph.Type = component.GlyphGreen
		s.world.Components.Glyph.SetComponent(e, glyph)

		// Spawn decay at same position (particle skips starting cell via LastIntX/Y)
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             x,
			Y:             y,
			Char:          glyph.Rune,
			SkipStartCell: true,
		})
	}
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (s *CleanerSystem) spawnDirectionalCleaners(originX, originY int) {
	config := s.world.Resources.Config

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	// Determine energy polarity once for entire batch
	negativeEnergy := false
	if energyComp, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Cursor.Entity); ok {
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
		velX, velY       int64
		startX, startY   int64
		targetX, targetY int64
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
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, comp)
		// TODO: centralize protection via entity factory
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}

// scanTargetRows finds rows containing target character type based on energy polarity
// Returns rows with TypeRed (energy >= 0) or TypeBlue (energy < 0)
func (s *CleanerSystem) scanTargetRows() []int {
	config := s.world.Resources.Config
	gameHeight := config.GameHeight

	// Determine target type based on energy polarity
	targetType := component.GlyphRed
	cursorEntity := s.world.Resources.Cursor.Entity
	if energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			targetType = component.GlyphBlue
		}
	}

	targetRows := make(map[int]bool)

	entities := s.world.Components.Glyph.AllEntities()

	for _, entity := range entities {
		glyph, ok := s.world.Components.Glyph.GetComponent(entity)
		if !ok || glyph.Type != targetType {
			continue
		}

		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
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