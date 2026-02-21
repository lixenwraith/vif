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
			s.spawnDirectionalCleaners(payload.OriginX, payload.OriginY, payload.ColorType)
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

	dtFixed := vmath.FromFloat(s.world.Resources.Time.DeltaTime.Seconds())
	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	for _, cleanerEntity := range cleanerEntities {
		cleanerComp, ok := s.world.Components.Cleaner.GetComponent(cleanerEntity)
		if !ok {
			continue
		}

		// --- Drain phase: head stationary, trail shrinking ---
		if cleanerComp.Blocked {
			cleanerComp.DrainRemaining -= vmath.Mul(cleanerComp.DrainSpeed, dtFixed)
			if cleanerComp.DrainRemaining <= 0 {
				s.world.DestroyEntity(cleanerEntity)
				continue
			}
			s.world.Components.Cleaner.SetComponent(cleanerEntity, cleanerComp)
			continue
		}

		// --- Active phase ---
		kineticComp, ok := s.world.Components.Kinetic.GetComponent(cleanerEntity)
		if !ok {
			continue
		}

		oldPos, ok := s.world.Positions.GetPosition(cleanerEntity)
		if !ok {
			continue
		}

		// Physics integration
		prevPreciseX := kineticComp.PreciseX
		prevPreciseY := kineticComp.PreciseY
		physics.Integrate(&kineticComp.Kinetic, dtFixed)

		// Swept collision with wall/enemy blocking
		blocked := false
		var blockGridX, blockGridY int

		if kineticComp.VelY != 0 && kineticComp.VelX == 0 {
			// Vertical sweep
			fromY := vmath.ToInt(prevPreciseY)
			toY := vmath.ToInt(kineticComp.PreciseY)
			x := oldPos.X
			step := 1
			if kineticComp.VelY < 0 {
				step = -1
			}

			lastValidY := fromY
			for y := fromY + step; (step > 0 && y <= toY) || (step < 0 && y >= toY); y += step {
				// Skip OOB cells (cleaner flies off-screen, lifecycle handles destruction)
				if y < 0 || y >= gameHeight {
					continue
				}

				// Wall blocks head at previous cell
				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					blocked = true
					blockGridX, blockGridY = x, lastValidY
					break
				}

				// Combat + glyph; enemy blocks head at this cell
				if s.checkCollisions(x, y, cleanerEntity, cleanerComp.ColorType) {
					blocked = true
					blockGridX, blockGridY = x, y
					break
				}

				lastValidY = y
			}
		} else if kineticComp.VelX != 0 {
			// Horizontal sweep
			fromX := vmath.ToInt(prevPreciseX)
			toX := vmath.ToInt(kineticComp.PreciseX)
			y := oldPos.Y
			step := 1
			if kineticComp.VelX < 0 {
				step = -1
			}

			lastValidX := fromX
			for x := fromX + step; (step > 0 && x <= toX) || (step < 0 && x >= toX); x += step {
				if x < 0 || x >= gameWidth {
					continue
				}

				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					blocked = true
					blockGridX, blockGridY = lastValidX, y
					break
				}

				if s.checkCollisions(x, y, cleanerEntity, cleanerComp.ColorType) {
					blocked = true
					blockGridX, blockGridY = x, y
					break
				}

				lastValidX = x
			}
		}

		if blocked {
			cleanerComp.Blocked = true

			// Capture drain speed from current velocity (don't zero — combat events need it)
			drainSpeed := kineticComp.VelX
			if drainSpeed < 0 {
				drainSpeed = -drainSpeed
			}
			if drainSpeed == 0 {
				drainSpeed = kineticComp.VelY
				if drainSpeed < 0 {
					drainSpeed = -drainSpeed
				}
			}
			cleanerComp.DrainSpeed = drainSpeed

			drainDist := vmath.FromInt(cleanerComp.TrailLen)
			cleanerComp.DrainRemaining = drainDist
			cleanerComp.DrainTotal = drainDist

			// Update precise position to block point (velocity preserved for combat resolution)
			blockPreciseX, blockPreciseY := vmath.CenteredFromGrid(blockGridX, blockGridY)
			kineticComp.PreciseX = blockPreciseX
			kineticComp.PreciseY = blockPreciseY

			// Trail update to block position
			if blockGridX != oldPos.X || blockGridY != oldPos.Y {
				cleanerComp.TrailHead = (cleanerComp.TrailHead + 1) % parameter.CleanerTrailLength
				cleanerComp.TrailRing[cleanerComp.TrailHead] = core.Point{X: blockGridX, Y: blockGridY}
				if cleanerComp.TrailLen < parameter.CleanerTrailLength {
					cleanerComp.TrailLen++
				}
			}

			s.world.Positions.SetPosition(cleanerEntity, component.PositionComponent{X: blockGridX, Y: blockGridY})
			s.world.Components.Cleaner.SetComponent(cleanerEntity, cleanerComp)
			s.world.Components.Kinetic.SetComponent(cleanerEntity, kineticComp)
			continue
		}

		// --- Unblocked: normal trail update and grid sync ---
		newGridX := vmath.ToInt(kineticComp.PreciseX)
		newGridY := vmath.ToInt(kineticComp.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			cleanerComp.TrailHead = (cleanerComp.TrailHead + 1) % parameter.CleanerTrailLength
			cleanerComp.TrailRing[cleanerComp.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if cleanerComp.TrailLen < parameter.CleanerTrailLength {
				cleanerComp.TrailLen++
			}
			s.world.Positions.SetPosition(cleanerEntity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		// Lifecycle: destroy at target (off-screen)
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
	if len(cleanerEntities) == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
	}
}

// spawnSweepingCleaners generates cleaner entities
func (s *CleanerSystem) spawnSweepingCleaners() {
	config := s.world.Resources.Config

	rows := s.scanTargetRows()

	spawnCount := len(rows)
	// No rows to clean
	if spawnCount == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}
	s.statSpawned.Add(int64(spawnCount))

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

	// Determine color type from energy polarity
	colorType := component.CleanerColorPositive
	if energyComp, ok := s.world.Components.Energy.GetComponent(s.world.Resources.Player.Entity); ok {
		if energyComp.Current < 0 {
			colorType = component.CleanerColorNegative
		}
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
			TargetX:   targetX,
			TargetY:   rowFixed,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Rune:      visual.CleanerChar,
			ColorType: colorType,
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

// checkCollisions handles combat and glyph interactions at a single cell
// Returns true if a combat entity was hit (blocks cleaner head)
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity core.Entity, colorType component.CleanerColorType) bool {
	cursorEntity := s.world.Resources.Player.Entity
	entities := s.world.Positions.GetAllEntityAt(x, y)
	if len(entities) == 0 {
		return false
	}

	blocked := false

	for _, entity := range entities {
		if entity == 0 || entity == selfEntity {
			continue
		}

		// Skip other cleaners
		if s.world.Components.Cleaner.HasEntity(entity) {
			continue
		}

		// Step 2: Header entity — check CompositeType
		if headerComp, ok := s.world.Components.Header.GetComponent(entity); ok {
			switch headerComp.Type {
			case component.CompositeTypeContainer:
				continue
			case component.CompositeTypeUnit:
				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackProjectile,
					OwnerEntity:  cursorEntity,
					OriginEntity: selfEntity,
					TargetEntity: entity,
					HitEntity:    entity,
				})
				blocked = true
				continue
			case component.CompositeTypeAblative:
				// Ablative header not directly targetable; damage through members only
				continue
			}
		}

		// Step 3: Member entity (non-header, guaranteed by Step 2 catching headers first)
		if memberComp, ok := s.world.Components.Member.GetComponent(entity); ok {
			headerEntity := memberComp.HeaderEntity
			headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
			if !ok {
				continue
			}

			switch headerComp.Type {
			case component.CompositeTypeContainer:
				continue
			case component.CompositeTypeUnit, component.CompositeTypeAblative:
				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackProjectile,
					OwnerEntity:  cursorEntity,
					OriginEntity: selfEntity,
					TargetEntity: headerEntity,
					HitEntity:    entity,
				})
				blocked = true
				continue
			}
		}

		// Step 4: Simple combat entity (drain, non-composite)
		if s.world.Components.Combat.HasEntity(entity) {
			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   component.CombatAttackProjectile,
				OwnerEntity:  cursorEntity,
				OriginEntity: selfEntity,
				TargetEntity: entity,
				HitEntity:    entity,
			})
			blocked = true
			continue
		}
	}

	// Glyph processing (always runs, non-blocking)
	switch colorType {
	case component.CleanerColorPositive:
		s.processPositiveEnergy(entities, selfEntity)
	case component.CleanerColorNegative:
		s.processNegativeEnergy(x, y, entities, selfEntity)
	case component.CleanerColorNugget:
		s.processNuggetEnergy(entities, selfEntity)
	}

	return blocked
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

// processNuggetEnergy handles Green destruction with Blossom spawn
func (s *CleanerSystem) processNuggetEnergy(targetEntities []core.Entity, selfEntity core.Entity) {
	var toDestroy []core.Entity

	for _, targetEntity := range targetEntities {
		if targetEntity == 0 || targetEntity == selfEntity {
			continue
		}
		if glyphComp, ok := s.world.Components.Glyph.GetComponent(targetEntity); ok {
			if glyphComp.Type == component.GlyphGreen {
				toDestroy = append(toDestroy, targetEntity)
			}
		}
	}

	if len(toDestroy) == 0 {
		return
	}

	event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventBlossomSpawnOne, toDestroy)
}

// spawnDirectionalCleaners generates 4 cleaner entities from origin position
func (s *CleanerSystem) spawnDirectionalCleaners(originX, originY int, colorType component.CleanerColorType) {
	config := s.world.Resources.Config

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		SoundType: core.SoundWhoosh,
	})

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
			TargetX:   dir.targetX,
			TargetY:   dir.targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Rune:      visual.CleanerChar,
			ColorType: colorType,
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

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent
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