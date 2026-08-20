package system

import (
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// CleanerSystem manages the cleaner animation and logic using vector physics
type CleanerSystem struct {
	world *engine.World

	entityBuf []core.Entity

	statActive  *atomic.Int64
	statSpawned *atomic.Int64

	enabled bool
}

// NewCleanerSystem creates a new cleaner system
func NewCleanerSystem(world *engine.World) engine.System {
	s := &CleanerSystem{
		world:     world,
		entityBuf: make([]core.Entity, 0),
	}

	s.statActive = s.world.Resources.Status.Ints.Get("cleaner.active")
	s.statSpawned = s.world.Resources.Status.Ints.Get("cleaner.spawned")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *CleanerSystem) Init() {
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
		event.EventGameResetRequest,
	}
}

// HandleEvent processes cleaner-related events from the router
func (s *CleanerSystem) HandleEvent(ev event.GameEvent) {
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

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventCleanerSweepingRequest:
		if payload, ok := ev.Payload.(*event.CleanerSweepingRequestPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.spawnSweepingCleaners(cursor)
			}
		}

	case event.EventCleanerDirectionalRequest:
		if payload, ok := ev.Payload.(*event.DirectionalCleanerPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.spawnDirectionalCleaners(cursor, payload.OriginX, payload.OriginY, payload.ColorType)
			}
		}
	}
}

// Update handles spawning, movement, collision, and cleanup synchronously
func (s *CleanerSystem) Update() {
	if !s.enabled {
		return
	}

	config := s.world.Resources.Config

	// Cleaners are destroyed immediately to free their spatial cells for later
	// cleaners in this tick, so the entity order must remain detached.
	s.entityBuf = append(s.entityBuf[:0], s.world.Components.Cleaner.Entities()...)
	s.statActive.Store(int64(len(s.entityBuf)))

	// Push EventCleanerSweepingFinished when all cleaners have completed their animation
	if len(s.entityBuf) == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)
	gameWidth := config.MapWidth
	gameHeight := config.MapHeight

	for _, cleanerEntity := range s.entityBuf {
		cleanerComp, ok := s.world.Components.Cleaner.GetPtr(cleanerEntity)
		if !ok {
			continue
		}

		// --- Drain phase: head stationary, trail shrinking ---
		if cleanerComp.Blocked {
			cleanerComp.DrainRemaining -= cleanerComp.DrainSpeed * dtSec
			if cleanerComp.DrainRemaining <= 0 {
				s.world.DestroyEntity(cleanerEntity)
				continue
			}
			continue
		}

		// --- Active phase ---
		kineticComp, ok := s.world.Components.Kinetic.GetPtr(cleanerEntity)
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
		physics.Integrate(&kineticComp.Kinetic, dtSec)
		prevCell := vmath.PointAtF(prevPreciseX, prevPreciseY)
		newCell := vmath.PointAtF(kineticComp.PreciseX, kineticComp.PreciseY)

		// Swept collision with wall/enemy blocking
		blocked := false
		var blockGridX, blockGridY int

		if kineticComp.VelY != 0 && kineticComp.VelX == 0 {
			// Vertical sweep
			fromY, toY := prevCell.Y, newCell.Y
			x := oldPos.X
			step := 1
			if kineticComp.VelY < 0 {
				step = -1
			}

			lastValidY := fromY
			for y := fromY + step; (step > 0 && y <= toY) || (step < 0 && y >= toY); y += step {
				// Skip OOB cells (cleaner flies off-screen, lifecycle handles destruction)
				if y < 0 || y >= gameHeight {
					appendCleanerTrail(cleanerComp, x, y)
					lastValidY = y
					continue
				}

				// Wall blocks head at previous cell
				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					blocked = true
					blockGridX, blockGridY = x, lastValidY
					break
				}

				// Combat + glyph; enemy blocks head at this cell
				if s.checkCollisions(x, y, cleanerEntity, cleanerComp.OwnerEntity, cleanerComp.ColorType) {
					blocked = true
					blockGridX, blockGridY = x, y
					appendCleanerTrail(cleanerComp, x, y)
					break
				}

				appendCleanerTrail(cleanerComp, x, y)
				lastValidY = y
			}
		} else if kineticComp.VelX != 0 {
			// Horizontal sweep
			fromX, toX := prevCell.X, newCell.X
			y := oldPos.Y
			step := 1
			if kineticComp.VelX < 0 {
				step = -1
			}

			lastValidX := fromX
			for x := fromX + step; (step > 0 && x <= toX) || (step < 0 && x >= toX); x += step {
				if x < 0 || x >= gameWidth {
					appendCleanerTrail(cleanerComp, x, y)
					lastValidX = x
					continue
				}

				if s.world.Positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
					blocked = true
					blockGridX, blockGridY = lastValidX, y
					break
				}

				if s.checkCollisions(x, y, cleanerEntity, cleanerComp.OwnerEntity, cleanerComp.ColorType) {
					blocked = true
					blockGridX, blockGridY = x, y
					appendCleanerTrail(cleanerComp, x, y)
					break
				}

				appendCleanerTrail(cleanerComp, x, y)
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

			drainDist := float64(cleanerComp.TrailLen)
			cleanerComp.DrainRemaining = drainDist
			cleanerComp.DrainTotal = drainDist

			// Update precise position to block point (velocity preserved for combat resolution)
			blockPreciseX, blockPreciseY := vmath.Point{X: blockGridX, Y: blockGridY}.CenterF()
			kineticComp.PreciseX = blockPreciseX
			kineticComp.PreciseY = blockPreciseY

			s.world.Positions.SetPosition(cleanerEntity, component.PositionComponent{X: blockGridX, Y: blockGridY})
			continue
		}

		// --- Unblocked: normal trail update and grid sync ---
		newGridX, newGridY := newCell.X, newCell.Y

		if newGridX != oldPos.X || newGridY != oldPos.Y {
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
		}
	}

	if s.world.Components.Cleaner.CountEntities() == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
	}
}

// appendCleanerTrail records every crossed cell, rather than only the final
// cell of a tick. This keeps fast cleaners visually continuous and mirrors the
// swept collision path without allocating.
func appendCleanerTrail(cleaner *component.CleanerComponent, x, y int) {
	if cleaner.TrailLen > 0 {
		last := cleaner.TrailRing[cleaner.TrailHead]
		if last.X == x && last.Y == y {
			return
		}
	}

	cleaner.TrailHead = (cleaner.TrailHead + 1) % parameter.CleanerTrailLength
	cleaner.TrailRing[cleaner.TrailHead] = vmath.Point{X: x, Y: y}
	if cleaner.TrailLen < parameter.CleanerTrailLength {
		cleaner.TrailLen++
	}
}

// cleanerFlightBounds returns cell-centered off-map targets far enough for a
// full trail to clear the map, with no extra off-map lifetime.
func cleanerFlightBounds(size int) (negative, positive float64) {
	margin := float64(parameter.CleanerTrailLength)
	return -margin + 0.5, float64(size) + margin - 0.5
}

// spawnSweepingCleaners generates cleaner entities for one cursor.
func (s *CleanerSystem) spawnSweepingCleaners(owner core.Entity) {
	config := s.world.Resources.Config

	rows := s.scanTargetRows(owner)

	spawnCount := len(rows)
	// No rows to clean
	if spawnCount == 0 {
		s.world.PushEvent(event.EventCleanerSweepingFinished, nil)
		return
	}
	s.statSpawned.Add(int64(spawnCount))

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Ring,
	})

	// Determine color type from energy polarity
	colorType := component.CleanerColorPositive
	if energyComp, ok := s.world.Components.Energy.GetPtr(owner); ok {
		if energyComp.Current < 0 {
			colorType = component.CleanerColorNegative
		}
	}

	minFlightX, maxFlightX := cleanerFlightBounds(config.MapWidth)
	baseSpeed := parameter.CleanerBaseHorizontalSpeed

	// Spawn one cleaner per row with Red entities, alternating L→R and R→L direction
	for _, row := range rows {
		var startX, targetX, velX float64
		// Row index -> precise Y at the cell center
		_, rowCenterY := vmath.Point{Y: row}.CenterF()

		if row%2 != 0 {
			// Left to right
			startX = minFlightX
			targetX = maxFlightX
			velX = baseSpeed
		} else {
			// Right to left
			startX = maxFlightX
			targetX = minFlightX
			velX = -baseSpeed
		}

		startGridX := vmath.PointAtF(startX, rowCenterY).X
		startGridY := row

		// Initialize trail ring buffer with starting position
		var trailRing [parameter.CleanerTrailLength]vmath.Point
		trailRing[0] = vmath.Point{X: startGridX, Y: startGridY}

		cleanerComp := component.CleanerComponent{
			OwnerEntity: owner,
			TargetX:     targetX,
			TargetY:     rowCenterY,
			TrailRing:   trailRing,
			TrailHead:   0,
			TrailLen:    1,
			ColorType:   colorType,
		}
		kinetic := physics.Kinetic{
			PreciseX: startX,
			PreciseY: rowCenterY,
			VelX:     velX,
			VelY:     0,
		}
		kineticComp := component.KineticComponent{Kinetic: kinetic}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent (float overlay)
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, cleanerComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromSpecies | component.ProtectFromDeath,
		})
	}
}

// checkCollisions handles combat and glyph interactions at a single cell
// Returns true if a combat entity was hit (blocks cleaner head)
func (s *CleanerSystem) checkCollisions(x, y int, selfEntity, owner core.Entity, colorType component.CleanerColorType) bool {
	var entityBuf [parameter.MaxEntitiesPerCell]core.Entity
	n := s.world.Positions.GetAllEntitiesAtInto(x, y, entityBuf[:])
	if n == 0 {
		return false
	}
	entities := entityBuf[:n]

	blocked := false

	for _, entity := range entities {
		if entity == 0 || entity == selfEntity {
			continue
		}

		// Fast-path: skip other cleaners (frequent co-location, no combat components)
		if s.world.Components.Cleaner.HasEntity(entity) {
			continue
		}

		target, hit, valid := ResolveTargetFromEntity(s.world, entity, selfEntity)
		if !valid {
			continue
		}

		// Skip cursors, their orbs, and the firing cursor's owned entities.
		if isCursorOrOwnedOrb(s.world, target) || isOwnedBy(s.world, target, owner) {
			continue
		}

		s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   component.CombatAttackProjectile,
			OwnerEntity:  owner,
			OriginEntity: selfEntity,
			TargetEntity: target,
			HitEntity:    hit,
		})
		blocked = true
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
		if glyphComp, ok := s.world.Components.Glyph.GetPtr(targetEntity); ok {
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

		glyphComp, ok := s.world.Components.Glyph.GetPtr(targetEntity)
		if !ok || glyphComp.Type != component.GlyphBlue {
			continue
		}

		// Mutate Blue → Green, preserving level
		glyphComp.Type = component.GlyphGreen

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
		if glyphComp, ok := s.world.Components.Glyph.GetPtr(targetEntity); ok {
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

// spawnDirectionalCleaners generates four cleaner entities owned by one cursor.
func (s *CleanerSystem) spawnDirectionalCleaners(owner core.Entity, originX, originY int, colorType component.CleanerColorType) {
	config := s.world.Resources.Config

	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Bullet,
	})

	minFlightX, maxFlightX := cleanerFlightBounds(config.MapWidth)
	minFlightY, maxFlightY := cleanerFlightBounds(config.MapHeight)

	horizontalSpeed := parameter.CleanerBaseHorizontalSpeed
	verticalSpeed := parameter.CleanerBaseVerticalSpeed

	// Launch from the origin cell center.
	ox, oy := vmath.Point{X: originX, Y: originY}.CenterF()

	type dirDef struct {
		velX, velY       float64
		startX, startY   float64
		targetX, targetY float64
	}

	directions := []dirDef{
		{horizontalSpeed, 0, ox, oy, maxFlightX, oy},
		{-horizontalSpeed, 0, ox, oy, minFlightX, oy},
		{0, verticalSpeed, ox, oy, ox, maxFlightY},
		{0, -verticalSpeed, ox, oy, ox, minFlightY},
	}

	// Spawn 4 cleaners from origin, each traveling in a cardinal direction
	for _, dir := range directions {
		startCell := vmath.PointAtF(dir.startX, dir.startY)
		startGridX, startGridY := startCell.X, startCell.Y

		// Initialize trail ring buffer with starting position
		var trailRing [parameter.CleanerTrailLength]vmath.Point
		trailRing[0] = vmath.Point{X: startGridX, Y: startGridY}

		cleanerComp := component.CleanerComponent{
			OwnerEntity: owner,
			TargetX:     dir.targetX,
			TargetY:     dir.targetY,
			TrailRing:   trailRing,
			TrailHead:   0,
			TrailLen:    1,
			ColorType:   colorType,
		}
		kinetic := physics.Kinetic{
			PreciseX: dir.startX,
			PreciseY: dir.startY,
			VelX:     dir.velX,
			VelY:     dir.velY,
		}
		kineticComp := component.KineticComponent{Kinetic: kinetic}

		// Spawn Protocol: CreateEntity → PositionComponent (grid registration) → CleanerComponent
		entity := s.world.CreateEntity()
		s.world.Positions.SetPosition(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.world.Components.Cleaner.SetComponent(entity, cleanerComp)
		s.world.Components.Kinetic.SetComponent(entity, kineticComp)
		s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
			Mask: component.ProtectFromSpecies | component.ProtectFromDeath,
		})
		s.statSpawned.Add(1)
	}
}

// scanTargetRows finds rows containing target character type based on energy polarity
// Returns rows with TypeRed (energy >= 0) or TypeBlue (energy < 0)
func (s *CleanerSystem) scanTargetRows(owner core.Entity) []int {
	config := s.world.Resources.Config
	gameHeight := config.MapHeight

	// Determine target type based on energy polarity
	targetType := component.GlyphRed
	if energyComp, ok := s.world.Components.Energy.GetPtr(owner); ok {
		if energyComp.Current < 0 {
			targetType = component.GlyphBlue
		}
	}

	targetRows := make(map[int]bool)

	entities := s.world.Components.Glyph.Entities()

	for _, entity := range entities {
		glyph, ok := s.world.Components.Glyph.GetPtr(entity)
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
	// Spawn order assigns entity IDs, so map order must not reach it
	slices.Sort(rows)
	return rows
}
