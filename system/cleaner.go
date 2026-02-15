package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	world *engine.World

	collidedHeaders map[core.Entity]core.Entity // anchor -> cleaner that deflected it for deduplication of large entity hits

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

	s.statActive = s.world.Resources.Status.Ints.Get("cleaner.active")
	s.statSpawned = s.world.Resources.Status.Ints.Get("cleaner.spawned")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *CleanerSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.collidedHeaders = make(map[core.Entity]core.Entity, 4)
	s.statActive.Store(0)
	s.statSpawned.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *CleanerSystem) Name() string {
	return "cleaner"
}

// Priority returns the system's priority
func (s *CleanerSystem) Priority() int {
	return parameter.PriorityCleaner
}

// EventTypes returns the event types CleanerSystem handles
func (s *CleanerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCleanerSweepingRequest,
		event.EventCleanerDirectionalRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes cleaner-related events from the router
func (s *CleanerSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventCleanerSweepingRequest:
		s.spawnSweepingCleaners()

	case event.EventCleanerDirectionalRequest:
		if payload, ok := ev.Payload.(*event.DirectionalCleanerPayload); ok {
			// Positions-aware deduplication for directional cleaners
			s.spawnDirectionalCleaners(payload.OriginX, payload.OriginY)
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	cleanerEntities := s.world.Components.Cleaner.GetAllEntities()
	s.statActive.Store(int64(len(cleanerEntities)))

	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(cleanerEntities) == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}

	// Clean dead cleaners from deflection tracking
	for anchor, cleanerEntity := range s.collidedHeaders {
		if !s.world.Components.Cleaner.HasEntity(cleanerEntity) {
			delete(s.collidedHeaders, anchor)
		}
	}

	// Early return if no cleaners
	if len(cleanerEntities) == 0 {
		return
	}

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	for _, cleanerEntity := range cleanerEntities {
		cleanerComp, ok := s.world.Components.Cleaner.GetComponent(cleanerEntity)
		if !ok {
			continue
		}
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(cleanerEntity)
		if !ok {
			continue
		}

		// Read grid position from Positions (authoritative for spatial queries)
		oldPos, ok := s.world.Positions.GetPosition(cleanerEntity)
		if !ok {
			continue
		}

		// Physics Update: Integrate velocity into float position (overlay state)
		prevPreciseX := kineticComp.PreciseX
		prevPreciseY := kineticComp.PreciseY
		physics.Integrate(&kineticComp.Kinetic, dtFixed)

		// Swept Collision Detection: Check all cells between previous and current position
		if kineticComp.VelY != 0 && kineticComp.VelX == 0 {
			// Vertical cleaner: sweep Y axis
			prevY := vmath.ToInt(prevPreciseY)
			currY := vmath.ToInt(kineticComp.PreciseY)
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
					s.checkCollisions(oldPos.X, y, cleanerEntity)
				}
			}
		} else if kineticComp.VelX != 0 {
			// Horizontal cleaner: sweep X axis
			prevX := vmath.ToInt(prevPreciseX)
			currX := vmath.ToInt(kineticComp.PreciseX)
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
					s.checkCollisions(x, oldPos.Y, cleanerEntity)
				}
			}
		}

		// Trail Update & Grid Sync: Update trail ring buffer and sync Positions if cell changed
		newGridX := vmath.ToInt(kineticComp.PreciseX)
		newGridY := vmath.ToInt(kineticComp.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			// Update trail: add new grid position to ring buffer
			cleanerComp.TrailHead = (cleanerComp.TrailHead + 1) % parameter.CleanerTrailLength
			cleanerComp.TrailRing[cleanerComp.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if cleanerComp.TrailLen < parameter.CleanerTrailLength {
				cleanerComp.TrailLen++
			}

			// Sync grid position to Positions
			s.world.Positions.SetPosition(cleanerEntity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		// Lifecycle Check: Destroy cleaner when it reaches target position
		shouldDestroy := false
		if kineticComp.VelX > 0 && kineticComp.PreciseX >= cleanerComp.TargetX {
			shouldDestroy = true
		} else if kineticComp.VelX < 0 && kineticComp.PreciseX <= cleanerComp.TargetX {
			shouldDestroy = true
		} else if kineticComp.VelY > 0 && kineticComp.PreciseY >= cleanerComp.TargetY {
			shouldDestroy = true
		} else if kineticComp.VelY < 0 && kineticComp.PreciseY <= cleanerComp.TargetY {
			shouldDestroy = true
		}

		if shouldDestroy {
			s.world.DestroyEntity(cleanerEntity)
		} else {
			s.world.Components.Cleaner.SetComponent(cleanerEntity, cleanerComp)
			s.world.Components.Kinetic.SetComponent(cleanerEntity, kineticComp)
		}
	}

	cleanerEntities = s.world.Components.Cleaner.GetAllEntities()
	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(cleanerEntities) == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
	}
}

// spawnSweepingCleaners generates cleaner entities
func (s *CleanerSystem) spawnSweepingCleaners() {
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
	if energyComp, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Player.Entity); ok {
		negativeEnergy = energyComp.Current < 0
	}

	gameWidthFixed := vmath.FromInt(config.MapWidth)
	trailLenFixed := parameter.CleanerTrailLenFixed
	baseSpeed := parameter.CleanerBaseHorizontalSpeed

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range rows {
		var startX, targetX, velX int64
		rowFixed := vmath.FromInt(row)

		if row%2 != 0 {
			// Left to right
			startX = -trailLenFixed
			targetX = gameWidthFixed + trailLenFixed*2
			velX = baseSpeed
		} else {
			// Right to left
			startX = gameWidthFixed + trailLenFixed
			targetX = -trailLenFixed * 2
			velX = -baseSpeed
		}

		startGridX := vmath.ToInt(startX)
		startGridY := row

		// Initialize trail ring buffer with starting position
		var trailRing [parameter.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		cleanerComp := component.CleanerComponent{
			TargetX:        targetX,
			TargetY:        rowFixed,
			TrailRing:      trailRing,
			TrailHead:      0,
			TrailLen:       1,
			Rune:           visual.CleanerChar,
			NegativeEnergy: negativeEnergy,
		}
		kinetic := core.Kinetic{
			PreciseX: startX,
			PreciseY: rowFixed,
			VelX:     velX,
			VelY:     0,
		}
		kineticComp := component.KineticComponent{kinetic}
		combatComp := component.CombatComponent{
			OwnerEntity:      s.world.Resources.Player.Entity,
			CombatEntityType: component.CombatEntityCleaner,
			HitPoints:        1,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, cleanerComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
		s.world.Components.Combat.SetComponent(entity, combatComp)
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromSpecies | component.ProtectFromDeath,
		})
	}
}

// checkCollisions handles collision logic with self-exclusion
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity core.Entity) {
	cursorEntity := s.world.Resources.Player.Entity
	// Query all entities at position (includes cleaner)
	entities := s.world.Positions.GetAllEntityAt(x, y)
	if len(entities) == 0 {
		return
	}

	// Deflect drains and quasar (energy-independent, cleaner passes through)
	for _, entity := range entities {
		if entity == 0 || entity == selfEntity {
			continue
		}

		// Check Member first because of complex composites that have both combat and member
		if s.world.Components.Member.HasEntity(entity) {
			memberComp, ok := s.world.Components.Member.GetComponent(entity)
			if !ok {
				continue
			}
			headerEntity := memberComp.HeaderEntity
			if !s.world.Components.Combat.HasEntity(headerEntity) {
				continue
			}
			if lastCleaner, exists := s.collidedHeaders[headerEntity]; exists && lastCleaner == selfEntity {
				continue
			}
			s.collidedHeaders[headerEntity] = selfEntity

			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   component.CombatAttackProjectile,
				OwnerEntity:  cursorEntity,
				OriginEntity: selfEntity,
				TargetEntity: headerEntity,
				HitEntity:    entity,
			})
			continue
		}

		// Simple combat entities (drain, non-composite headers)
		if s.world.Components.Combat.HasEntity(entity) {
			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   component.CombatAttackProjectile,
				OwnerEntity:  cursorEntity,
				OriginEntity: selfEntity,
				TargetEntity: entity,
				HitEntity:    entity,
			})
			continue
		}

	}

	// Determine mode based on energy polarity
	// TODO: migrate to status?
	negativeEnergy := false
	if energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		negativeEnergy = energyComp.Current < 0
	}

	if negativeEnergy {
		s.processNegativeEnergy(x, y, entities, selfEntity)
	} else {
		s.processPositiveEnergy(entities, selfEntity)
	}
}

// processPositiveEnergy handles Red destruction with Blossom spawn
func (s *CleanerSystem) processPositiveEnergy(targetEntities []core.Entity, selfEntity core.Entity) {
	var toDestroy []core.Entity

	// Iterate candidates with self-exclusion pattern
	for _, targetEntity := range targetEntities {
		if targetEntity == 0 || targetEntity == selfEntity {
			continue
		}
		if glyphComp, ok := s.world.Components.Glyph.GetComponent(targetEntity); ok {
			if glyphComp.Type == component.GlyphRed {
				toDestroy = append(toDestroy, targetEntity)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventBlossomSpawnOne, toDestroy)
}

// processNegativeEnergy handles Blue mutation to Green with Decay spawn
func (s *CleanerSystem) processNegativeEnergy(x, y int, targetEntities []core.Entity, selfEntity core.Entity) {
	// Iterate candidates with self-exclusion pattern
	for _, targetEntity := range targetEntities {
		if targetEntity == 0 || targetEntity == selfEntity {
			continue
		}

		glyphComp, ok := s.world.Components.Glyph.GetComponent(targetEntity)
		if !ok || glyphComp.Type != component.GlyphBlue {
			continue
		}

		// Mutate Blue → Green, preserving level
		glyphComp.Type = component.GlyphGreen
		s.world.Components.Glyph.SetComponent(targetEntity, glyphComp)

		// Spawn decay at same position (particle skips starting cell via LastIntX/Y)
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             x,
			Y:             y,
			Char:          glyphComp.Rune,
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
	if energyComp, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Player.Entity); ok {
		negativeEnergy = energyComp.Current < 0
	}

	gameWidthFixed := vmath.FromInt(config.MapWidth)
	gameHeightFixed := vmath.FromInt(config.MapHeight)
	trailLenFixed := parameter.CleanerTrailLenFixed

	horizontalSpeed := parameter.CleanerBaseHorizontalSpeed
	verticalSpeed := parameter.CleanerBaseVerticalSpeed

	// Shift for cell center precise coordinate adjustment
	oxFixed := vmath.FromInt(originX) + vmath.Scale>>1
	oyFixed := vmath.FromInt(originY) + vmath.Scale>>1

	type dirDef struct {
		velX, velY       int64
		startX, startY   int64
		targetX, targetY int64
	}

	directions := []dirDef{
		{horizontalSpeed, 0, oxFixed, oyFixed, gameWidthFixed + 2*trailLenFixed, oyFixed},
		{-horizontalSpeed, 0, oxFixed, oyFixed, -trailLenFixed * 2, oyFixed},
		{0, verticalSpeed, oxFixed, oyFixed, oxFixed, gameHeightFixed + trailLenFixed*2},
		{0, -verticalSpeed, oxFixed, oyFixed, oxFixed, -trailLenFixed * 2},
	}

	// Spawn 4 cleaners from origin, each traveling in a cardinal direction
	for _, dir := range directions {
		startGridX := vmath.ToInt(dir.startX)
		startGridY := vmath.ToInt(dir.startY)

		// Initialize trail ring buffer with starting position
		var trailRing [parameter.CleanerTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		cleanerComp := component.CleanerComponent{
			TargetX:        dir.targetX,
			TargetY:        dir.targetY,
			TrailRing:      trailRing,
			TrailHead:      0,
			TrailLen:       1,
			Rune:           visual.CleanerChar,
			NegativeEnergy: negativeEnergy,
		}
		kinetic := core.Kinetic{
			PreciseX: dir.startX,
			PreciseY: dir.startY,
			VelX:     dir.velX,
			VelY:     dir.velY,
		}
		kineticComp := component.KineticComponent{kinetic}
		combatComp := component.CombatComponent{
			OwnerEntity:      s.world.Resources.Player.Entity,
			CombatEntityType: component.CombatEntityCleaner,
			HitPoints:        1,
		}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, cleanerComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
		s.world.Components.Combat.SetComponent(entity, combatComp)
		// TODO: centralize protection via entity factory
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromSpecies | component.ProtectFromDeath,
		})
	}
}

// scanTargetRows finds rows containing target character type based on energy polarity
// Returns rows with TypeRed (energy >= 0) or TypeBlue (energy < 0)
func (s *CleanerSystem) scanTargetRows() []int {
	config := s.world.Resources.Config
	gameHeight := config.MapHeight

	// Determine target type based on energy polarity
	targetType := component.GlyphRed
	cursorEntity := s.world.Resources.Player.Entity
	if energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		if energyComp.Current < 0 {
			targetType = component.GlyphBlue
		}
	}

	targetRows := make(map[int]bool)

	entities := s.world.Components.Glyph.GetAllEntities()

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