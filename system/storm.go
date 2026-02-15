package system

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// stormCacheEntry holds cached position for soft collision checks
type stormCacheEntry struct {
	entity core.Entity
	x, y   int
}

// pendingBlueSpawn tracks storm-initiated swarm spawns waiting for visual completion
type pendingBlueSpawn struct {
	TargetX int
	TargetY int
	Timer   time.Duration
}

// StormSystem manages the storm boss entity lifecycle
// Storm is a 3-part composite boss with 3D orbital physics
// Each circle is an independent sub-header that can be destroyed individually
// Spawned when SwarmSystem accumulates enough player-damage kills
type StormSystem struct {
	world *engine.World

	// Root storm entity (owns 3 circle headers)
	rootEntity core.Entity

	// Random source
	rng *vmath.FastRand

	// Per-tick cache for soft collision (push other enemies away)
	swarmCache  []swarmCacheEntry
	quasarCache []quasarCacheEntry

	// Precomputed ellipse cell offsets for wall collision
	ellipseOffsets []struct{ X, Y int }

	// Reusable map
	memberExcludeSet map[core.Entity]struct{}

	// Pending blue attack spawns (waiting for materialize completion)
	pendingBlueSpawns []pendingBlueSpawn

	// Telemetry
	statActive           *atomic.Bool
	statCircleCount      *atomic.Int64
	statGreenActiveFrame *atomic.Int64
	statRedActiveFrame   *atomic.Int64
	statBlueActiveFrame  *atomic.Int64

	enabled bool
}

func NewStormSystem(world *engine.World) engine.System {
	s := &StormSystem{
		world: world,
	}

	s.swarmCache = make([]swarmCacheEntry, 0, 10)
	s.quasarCache = make([]quasarCacheEntry, 0, 1)
	s.memberExcludeSet = make(map[core.Entity]struct{}, 256)
	s.pendingBlueSpawns = make([]pendingBlueSpawn, 0, 4)

	// Precompute ellipse cell offsets for wall collision checks
	s.buildEllipseOffsets()

	s.statActive = world.Resources.Status.Bools.Get("storm.active")
	s.statCircleCount = world.Resources.Status.Ints.Get("storm.circle_count")
	s.statGreenActiveFrame = world.Resources.Status.Ints.Get("storm.green_active_frames")
	s.statRedActiveFrame = world.Resources.Status.Ints.Get("storm.red_active_frames")
	s.statBlueActiveFrame = world.Resources.Status.Ints.Get("storm.blue_active_frames")

	s.Init()
	return s
}

func (s *StormSystem) Init() {
	s.rootEntity = 0
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.swarmCache = s.swarmCache[:0]
	s.quasarCache = s.quasarCache[:0]
	clear(s.memberExcludeSet)
	s.pendingBlueSpawns = s.pendingBlueSpawns[:0]
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
	s.statGreenActiveFrame.Store(0)
	s.statRedActiveFrame.Store(0)
	s.statBlueActiveFrame.Store(0)
	s.enabled = true
}

func (s *StormSystem) Name() string {
	return "storm"
}

func (s *StormSystem) Priority() int {
	return parameter.PriorityStorm
}

func (s *StormSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventStormSpawnRequest,
		event.EventStormCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *StormSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		if s.rootEntity != 0 {
			s.terminateStorm()
		}
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
	case event.EventStormSpawnRequest:
		if s.rootEntity == 0 {
			s.spawnStorm()
		}

	case event.EventStormCancelRequest:
		if s.rootEntity != 0 {
			s.terminateStorm()
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorStorm {
				s.handleCircleBreach(payload.HeaderEntity)
			}
		}
	}
}

func (s *StormSystem) Update() {
	if !s.enabled || s.rootEntity == 0 {
		return
	}

	// Process pending blue spawns regardless of root entity state
	s.processPendingBlueSpawns()

	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if !ok {
		s.rootEntity = 0
		s.statActive.Store(false)
		return
	}

	// Check liveness via Header existence (CompositeSystem authority)
	for i := 0; i < component.StormCircleCount; i++ {
		if stormComp.CirclesAlive[i] && !s.world.Components.Header.HasEntity(stormComp.Circles[i]) {
			stormComp.CirclesAlive[i] = false
		}
	}

	// Check if all circles dead
	aliveCount := stormComp.AliveCount()
	if aliveCount == 0 {
		s.terminateStorm()
		return
	}

	// Cache other combat entities for soft collision
	s.cacheCombatEntities()

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	// Process each alive circle
	s.updateCirclePhysics(&stormComp, dtFixed)
	s.updateCircleDamageImmunity(&stormComp)
	s.updateCircleAttacks(&stormComp, dt)
	s.processCircleMemberCombat(&stormComp)
	s.handleCircleInteractions(&stormComp)

	// Apply soft collision per circle (push other enemies away)
	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}
		circlePos, ok := s.world.Positions.GetPosition(stormComp.Circles[i])
		if !ok {
			continue
		}
		s.applySoftCollisions(circlePos.X, circlePos.Y)
	}

	s.world.Components.Storm.SetComponent(s.rootEntity, stormComp)
	s.statCircleCount.Store(int64(aliveCount))
}

// buildEllipseOffsets populates the LUT of cell offsets inside the circle ellipse
func (s *StormSystem) buildEllipseOffsets() {
	radiusX := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusY := vmath.ToInt(parameter.StormCircleRadiusY)

	// Preallocate approximate capacity: Ï€ * rx * ry
	capacity := int(3.2 * float64(radiusX) * float64(radiusY))
	s.ellipseOffsets = make([]struct{ X, Y int }, 0, capacity)

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			if vmath.EllipseDistSq(dx, dy, parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq) <= vmath.Scale {
				s.ellipseOffsets = append(s.ellipseOffsets, struct{ X, Y int }{x, y})
			}
		}
	}
}

// cacheCombatEntities populates caches for soft collision detection
func (s *StormSystem) cacheCombatEntities() {
	s.swarmCache = s.swarmCache[:0]
	s.quasarCache = s.quasarCache[:0]

	// Cache all swarm headers
	swarmEntities := s.world.Components.Swarm.GetAllEntities()
	for _, entity := range swarmEntities {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		s.swarmCache = append(s.swarmCache, swarmCacheEntry{
			entity: entity,
			x:      pos.X,
			y:      pos.Y,
		})
	}

	// Cache quasar header
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

// applySoftCollisions pushes other combat entities away from storm circle
// Storm acts as immovable repulsion source; applies impulse to OTHER entities
func (s *StormSystem) applySoftCollisions(circleX, circleY int) {
	// Push swarms away from this circle
	for _, sc := range s.swarmCache {
		// Check if swarm overlaps with storm circle collision area
		radialX, radialY, hit := physics.CheckSoftCollision(
			sc.x, sc.y,
			circleX, circleY,
			parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq,
		)
		if !hit {
			continue
		}

		swarmKinetic, ok := s.world.Components.Kinetic.GetComponent(sc.entity)
		if !ok {
			continue
		}
		swarmCombat, ok := s.world.Components.Combat.GetComponent(sc.entity)
		if !ok || swarmCombat.RemainingKineticImmunity > 0 || swarmCombat.IsEnraged {
			continue
		}

		// Apply collision impulse to swarm (push away from storm)
		physics.ApplyCollision(
			&swarmKinetic.Kinetic,
			radialX, radialY,
			&physics.SoftCollisionQuasarToSwarm, // Reuse quasar profile as placeholder
			s.rng,
		)
		swarmCombat.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration

		s.world.Components.Kinetic.SetComponent(sc.entity, swarmKinetic)
		s.world.Components.Combat.SetComponent(sc.entity, swarmCombat)
	}

	// Push quasar away from this circle
	for _, qc := range s.quasarCache {
		radialX, radialY, hit := physics.CheckSoftCollision(
			qc.x, qc.y,
			circleX, circleY,
			parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq,
		)
		if !hit {
			continue
		}

		quasarKinetic, ok := s.world.Components.Kinetic.GetComponent(qc.entity)
		if !ok {
			continue
		}
		quasarCombat, ok := s.world.Components.Combat.GetComponent(qc.entity)
		if !ok || quasarCombat.RemainingKineticImmunity > 0 || quasarCombat.IsEnraged {
			continue
		}

		physics.ApplyCollision(
			&quasarKinetic.Kinetic,
			radialX, radialY,
			&physics.SoftCollisionSwarmToQuasar, // Reversed: storm pushing quasar
			s.rng,
		)
		quasarCombat.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration

		s.world.Components.Kinetic.SetComponent(qc.entity, quasarKinetic)
		s.world.Components.Combat.SetComponent(qc.entity, quasarCombat)
	}
}

// spawnStorm creates the root header and 3 circle sub-headers
func (s *StormSystem) spawnStorm() {
	config := s.world.Resources.Config
	centerX := config.MapWidth / 2
	centerY := config.MapHeight / 2

	// Pre-calculate circle spawn parameters
	angleOffsets := [3]float64{0, 2 * math.Pi / 3, 4 * math.Pi / 3}
	zOffsets := [3]float64{-1.0, 0.0, 1.0}
	initialRadius := parameter.StormInitialRadiusFloat
	initialSpeed := parameter.StormInitialSpeedFloat
	baseZ := parameter.StormZMidFloat

	type circleSpawnInfo struct {
		gridX, gridY int
		angle        float64
		pos3D        vmath.Vec3
		vel3D        vmath.Vec3
	}

	var circleInfos [component.StormCircleCount]circleSpawnInfo

	// 1. Calculate target positions and validate all circles
	for i := 0; i < component.StormCircleCount; i++ {
		angle := angleOffsets[i]
		offsetX := initialRadius * math.Cos(angle)
		offsetY := initialRadius * math.Sin(angle) * 0.5 // Terminal aspect ratio

		targetX := int(float64(centerX) + offsetX)
		targetY := int(float64(centerY) + offsetY)

		// Find valid position via spiral search
		foundX, foundY, found := s.findCirclePosition(targetX, targetY)
		if !found {
			return // Abort entire spawn - one circle failed
		}

		circleInfos[i] = circleSpawnInfo{
			gridX: foundX,
			gridY: foundY,
			angle: angle,
			pos3D: vmath.Vec3{
				X: vmath.FromInt(foundX),
				Y: vmath.FromInt(foundY),
				Z: vmath.FromFloat(baseZ + zOffsets[i]*parameter.StormZSpawnOffsetFloat),
			},
			vel3D: vmath.Vec3{
				X: vmath.FromFloat(-initialSpeed * math.Sin(angle)),
				Y: vmath.FromFloat(initialSpeed * math.Cos(angle) * 0.5),
				Z: vmath.FromFloat(float64(s.rng.Intn(6)-3) * 0.8),
			},
		}
	}

	// 2. Clear all spawn areas (validation passed)
	for i := 0; i < component.StormCircleCount; i++ {
		s.clearCircleSpawnArea(circleInfos[i].gridX, circleInfos[i].gridY)
	}

	// 3. Create entities
	rootEntity := s.world.CreateEntity()
	s.world.Components.Protection.SetComponent(rootEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	stormComp := component.StormComponent{}

	for i := 0; i < component.StormCircleCount; i++ {
		circleEntity := s.createCircleHeader(rootEntity, i, circleInfos[i].pos3D, circleInfos[i].vel3D)
		stormComp.Circles[i] = circleEntity
		stormComp.CirclesAlive[i] = true
	}

	// Root header component linking circles
	rootMembers := make([]component.MemberEntry, component.StormCircleCount)
	for i := 0; i < component.StormCircleCount; i++ {
		rootMembers[i] = component.MemberEntry{
			Entity:  stormComp.Circles[i],
			OffsetX: 0,
			OffsetY: 0,
		}
	}

	s.world.Components.Header.SetComponent(rootEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
		Type:          component.CompositeTypeContainer,
		MemberEntries: rootMembers,
	})

	s.world.Components.Storm.SetComponent(rootEntity, stormComp)

	s.rootEntity = rootEntity
	s.statActive.Store(true)
	s.statCircleCount.Store(component.StormCircleCount)

	// Emit storm creation
	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  rootEntity,
		Species: component.SpeciesStorm,
	})
}

// findCirclePosition searches for valid position for circle's elliptical footprint
// Returns (gridX, gridY, found) where gridX/gridY is the circle center
func (s *StormSystem) findCirclePosition(targetX, targetY int) (int, int, bool) {
	const maxSearchRadius = 20

	// Check target position first (radius 0)
	if s.isCirclePositionValid(targetX, targetY) {
		return targetX, targetY, true
	}

	// Spiral outward
	for radius := 1; radius <= maxSearchRadius; radius++ {
		vertRadius := (radius + 1) / 2 // Aspect correction for terminal cells

		for _, dir := range engine.SpiralSearchDirs {
			checkX := targetX + dir[0]*radius
			checkY := targetY + dir[1]*vertRadius

			if s.isCirclePositionValid(checkX, checkY) {
				return checkX, checkY, true
			}
		}
	}

	return 0, 0, false
}

// isCirclePositionValid checks if all ellipse member cells at this center are valid
func (s *StormSystem) isCirclePositionValid(centerX, centerY int) bool {
	config := s.world.Resources.Config
	radiusX := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusY := vmath.ToInt(parameter.StormCircleRadiusY)

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			// Skip cells outside ellipse
			if vmath.EllipseDistSq(dx, dy, parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq) > vmath.Scale {
				continue
			}

			cellX := centerX + x
			cellY := centerY + y

			// Bounds check
			if cellX < 0 || cellX >= config.MapWidth || cellY < 0 || cellY >= config.MapHeight {
				return false
			}

			// Wall check
			if s.world.Positions.HasBlockingWallAt(cellX, cellY, component.WallBlockSpawn) {
				return false
			}
		}
	}

	return true
}

// clearCircleSpawnArea destroys entities within circle's elliptical footprint
func (s *StormSystem) clearCircleSpawnArea(centerX, centerY int) {
	radiusX := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusY := vmath.ToInt(parameter.StormCircleRadiusY)

	cursorEntity := s.world.Resources.Player.Entity
	var toDestroy []core.Entity

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			// Skip cells outside ellipse
			if vmath.EllipseDistSq(dx, dy, parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq) > vmath.Scale {
				continue
			}

			cellX := centerX + x
			cellY := centerY + y

			entities := s.world.Positions.GetAllEntityAt(cellX, cellY)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
				// Skip walls - they block, not get cleared
				if s.world.Components.Wall.HasEntity(e) {
					continue
				}
				// Check protection
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask&component.ProtectFromSpecies != 0 {
						continue
					}
				}
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		s.world.DestroyEntitiesBatch(toDestroy)
	}
}

// createCircleHeader builds a single circle sub-header entity
func (s *StormSystem) createCircleHeader(
	parentEntity core.Entity,
	index int,
	pos3D, vel3D vmath.Vec3,
) core.Entity {
	gridX, gridY := vmath.ToInt(pos3D.X), vmath.ToInt(pos3D.Y)

	circleEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: gridX, Y: gridY})

	// Circle headers are protected
	s.world.Components.Protection.SetComponent(circleEntity, component.ProtectionComponent{
		// Mask: component.ProtectAll ^ component.ProtectFromDeath,
		Mask: component.ProtectAll,
	})

	// Storm circle component (3D physics + attack state)
	s.world.Components.StormCircle.SetComponent(circleEntity, component.StormCircleComponent{
		Pos3D:       pos3D,
		Vel3D:       vel3D,
		Index:       index,
		AttackState: component.StormCircleAttackIdle,
	})

	// Kinetic component for 2D collision compatibility
	preciseX, preciseY := pos3D.X, pos3D.Y
	s.world.Components.Kinetic.SetComponent(circleEntity, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// Combat component
	s.world.Components.Combat.SetComponent(circleEntity, component.CombatComponent{
		OwnerEntity:      circleEntity,
		CombatEntityType: component.CombatEntityStorm,
		HitPoints:        0, // Damage routed to members via ablative model
	})

	// Generate members
	members := s.createCircleMembers(circleEntity, gridX, gridY)

	// Header component linking back to root
	s.world.Components.Header.SetComponent(circleEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
		Type:          component.CompositeTypeAblative,
		MemberEntries: members,
		ParentHeader:  parentEntity,
	})

	// Backlink as member of root
	s.world.Components.Member.SetComponent(circleEntity, component.MemberComponent{
		HeaderEntity: parentEntity,
	})

	return circleEntity
}

// createCircleMembers builds a single circle's member entities
func (s *StormSystem) createCircleMembers(headerEntity core.Entity, headerX, headerY int) []component.MemberEntry {
	radiusX := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusY := vmath.ToInt(parameter.StormCircleRadiusY)

	var members []component.MemberEntry

	// Iterate bounding box
	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			// Check if cell is inside ellipse
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			if vmath.EllipseDistSq(dx, dy, parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq) <= vmath.Scale {

				// Create the member entity
				memberEntity := s.world.CreateEntity()

				// Set member position
				s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
					X: headerX + x,
					Y: headerY + y,
				})

				// Member protection
				s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
					Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
				})

				// Ablative health: per-member HP for combat damage
				s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
					OwnerEntity:      headerEntity,
					CombatEntityType: component.CombatEntityStorm,
					HitPoints:        parameter.CombatInitialHPStormMember,
				})

				// Backlink
				s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
					HeaderEntity: headerEntity,
				})

				members = append(members, component.MemberEntry{
					Entity:  memberEntity,
					OffsetX: x,
					OffsetY: y,
				})
			}
		}
	}
	return members
}

// collectAndDestroyWallsInEllipse finds walls in ellipse footprint, emits despawn requests, returns true if any found
func (s *StormSystem) collectAndDestroyWallsInEllipse(centerX, centerY int) bool {
	found := false

	for _, off := range s.ellipseOffsets {
		cellX := centerX + off.X
		cellY := centerY + off.Y

		if s.world.Positions.HasBlockingWallAt(cellX, cellY, component.WallBlockKinetic) {
			s.world.PushEvent(event.EventWallDespawnRequest, &event.WallDespawnRequestPayload{
				X:      cellX,
				Y:      cellY,
				Width:  1,
				Height: 1,
			})
			found = true
		}
	}

	return found
}

// updateCirclePhysics handles 3D gravitational orbits and inter-circle collision
func (s *StormSystem) updateCirclePhysics(stormComp *component.StormComponent, dtFixed int64) {
	config := s.world.Resources.Config

	// Collect alive circles
	type circleState struct {
		entity core.Entity
		circle *component.StormCircleComponent
		index  int
	}

	var circles []circleState
	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}
		circleComp, ok := s.world.Components.StormCircle.GetComponent(stormComp.Circles[i])
		if !ok {
			stormComp.CirclesAlive[i] = false
			continue
		}
		circles = append(circles, circleState{
			entity: stormComp.Circles[i],
			circle: &circleComp,
			index:  i,
		})
	}

	if len(circles) == 0 {
		return
	}

	// Precompute boundary limits accounting for ellipse radius
	insetX := parameter.StormBoundaryInsetX
	insetY := parameter.StormBoundaryInsetY
	boundMinX := vmath.FromInt(insetX)
	boundMaxX := vmath.FromInt(config.MapWidth - 1 - insetX)
	boundMinY := vmath.FromInt(insetY)
	boundMaxY := vmath.FromInt(config.MapHeight - 1 - insetY)

	for i := range circles {
		// 1. Accumulate gravitational acceleration with repulsion
		var accelX, accelY, accelZ int64
		for j := range circles {
			if i == j {
				continue
			}
			accel := physics.GravitationalAccelWithRepulsion3D(
				circles[i].circle.Pos3D,
				circles[j].circle.Pos3D,
				physics.MassStorm,
				parameter.StormGravity,
				parameter.StormRepulsionRadius,
				parameter.StormRepulsionStrength,
			)
			accelX += accel.X
			accelY += accel.Y
			accelZ += accel.Z
		}

		// 1b. Z-axis equilibrium spring: accelZ += stiffness * (zMid - z)
		// Provides restoring force toward vulnerability boundary
		zDelta := parameter.StormZMid - circles[i].circle.Pos3D.Z
		accelZ += vmath.Mul(parameter.StormZEquilibriumStiffness, zDelta)

		// 2. Integrate velocity
		circles[i].circle.Vel3D.X += vmath.Mul(accelX, dtFixed)
		circles[i].circle.Vel3D.Y += vmath.Mul(accelY, dtFixed)
		circles[i].circle.Vel3D.Z += vmath.Mul(accelZ, dtFixed)

		// 3. Apply damping
		circles[i].circle.Vel3D = vmath.V3DampDt(circles[i].circle.Vel3D, parameter.StormDamping, dtFixed)

		// 4. Clamp velocity
		circles[i].circle.Vel3D = vmath.V3ClampMagnitude(circles[i].circle.Vel3D, parameter.StormMaxVelocity)

		// 5. Axis-separated position integration with collision

		// --- X Axis ---
		oldPosX := circles[i].circle.Pos3D.X
		circles[i].circle.Pos3D.X += vmath.Mul(circles[i].circle.Vel3D.X, dtFixed)

		// Boundary check X
		if circles[i].circle.Pos3D.X < boundMinX {
			circles[i].circle.Pos3D.X = boundMinX
			if circles[i].circle.Vel3D.X < 0 {
				circles[i].circle.Vel3D.X = -vmath.Mul(circles[i].circle.Vel3D.X, parameter.StormRestitution)
			}
		} else if circles[i].circle.Pos3D.X > boundMaxX {
			circles[i].circle.Pos3D.X = boundMaxX
			if circles[i].circle.Vel3D.X > 0 {
				circles[i].circle.Vel3D.X = -vmath.Mul(circles[i].circle.Vel3D.X, parameter.StormRestitution)
			}
		} else {
			// Wall check X (only if within bounds)
			gridX := vmath.ToInt(circles[i].circle.Pos3D.X)
			gridY := vmath.ToInt(circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(gridX, gridY) {
				circles[i].circle.Pos3D.X = oldPosX
				circles[i].circle.Vel3D.X = -vmath.Mul(circles[i].circle.Vel3D.X, parameter.StormRestitution)
			}
		}

		// --- Y Axis ---
		oldPosY := circles[i].circle.Pos3D.Y
		circles[i].circle.Pos3D.Y += vmath.Mul(circles[i].circle.Vel3D.Y, dtFixed)

		// Boundary check Y
		if circles[i].circle.Pos3D.Y < boundMinY {
			circles[i].circle.Pos3D.Y = boundMinY
			if circles[i].circle.Vel3D.Y < 0 {
				circles[i].circle.Vel3D.Y = -vmath.Mul(circles[i].circle.Vel3D.Y, parameter.StormRestitution)
			}
		} else if circles[i].circle.Pos3D.Y > boundMaxY {
			circles[i].circle.Pos3D.Y = boundMaxY
			if circles[i].circle.Vel3D.Y > 0 {
				circles[i].circle.Vel3D.Y = -vmath.Mul(circles[i].circle.Vel3D.Y, parameter.StormRestitution)
			}
		} else {
			// Wall check Y (uses potentially updated X position)
			gridX := vmath.ToInt(circles[i].circle.Pos3D.X)
			gridY := vmath.ToInt(circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(gridX, gridY) {
				circles[i].circle.Pos3D.Y = oldPosY
				circles[i].circle.Vel3D.Y = -vmath.Mul(circles[i].circle.Vel3D.Y, parameter.StormRestitution)
			}
		}

		// --- Z Axis (depth bounds only, no walls) ---
		circles[i].circle.Pos3D.Z += vmath.Mul(circles[i].circle.Vel3D.Z, dtFixed)
		physics.ReflectAxis3D(&circles[i].circle.Pos3D.Z, &circles[i].circle.Vel3D.Z,
			parameter.StormZMin, parameter.StormZMax, parameter.StormRestitution)

		// --- ATTACK PHYSICS OVERRIDE ---
		// If attacking, physically trap the circle in the convex
		if circles[i].circle.AttackState == component.StormCircleAttackActive {
			// Define a boundary slightly in front of the Mid point to ensure IsConvex returns true
			zLimit := parameter.StormZMid - vmath.Scale

			if circles[i].circle.Pos3D.Z > zLimit {
				circles[i].circle.Pos3D.Z = zLimit

				// Kill outward (receding) momentum, but allow it to move further forward
				if circles[i].circle.Vel3D.Z > 0 {
					circles[i].circle.Vel3D.Z = 0
				}
			}
		}
	}

	// Inter-circle collision
	for i := 0; i < len(circles); i++ {
		for j := i + 1; j < len(circles); j++ {
			s.resolveCircleCollision(circles[i].circle, circles[j].circle)
		}
	}

	// Sync 3D position to 2D components
	for i := range circles {
		circle := circles[i].circle
		circleEntity := circles[i].entity

		newGridX := vmath.ToInt(circle.Pos3D.X)
		newGridY := vmath.ToInt(circle.Pos3D.Y)

		// Update grid position
		if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
			if pos.X != newGridX || pos.Y != newGridY {
				s.processCircleCollisions(circleEntity, newGridX, newGridY)
				s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: newGridX, Y: newGridY})
			}
		}

		// Update kinetic for 2D collision compatibility
		if kinetic, ok := s.world.Components.Kinetic.GetComponent(circleEntity); ok {
			kinetic.PreciseX = circle.Pos3D.X
			kinetic.PreciseY = circle.Pos3D.Y
			kinetic.VelX = circle.Vel3D.X
			kinetic.VelY = circle.Vel3D.Y
			s.world.Components.Kinetic.SetComponent(circleEntity, kinetic)
		}

		s.world.Components.StormCircle.SetComponent(circles[i].entity, *circles[i].circle)
	}
}

// resolveCircleCollision handles elastic collision between two circles
func (s *StormSystem) resolveCircleCollision(a, b *component.StormCircleComponent) {
	delta := vmath.V3Sub(b.Pos3D, a.Pos3D)
	dist := vmath.V3Mag(delta)
	minDist := parameter.StormCollisionRadius * 2

	if dist >= minDist || dist == 0 {
		return
	}

	// Separate overlap
	newPosA, newPosB, separated := physics.SeparateOverlap3D(
		a.Pos3D, b.Pos3D,
		parameter.StormCollisionRadius, parameter.StormCollisionRadius,
		physics.MassStorm, physics.MassStorm,
	)
	if separated {
		a.Pos3D = newPosA
		b.Pos3D = newPosB
	}

	// Elastic collision response (in-place modification)
	collided := physics.ElasticCollision3DInPlace(
		&a.Pos3D, &b.Pos3D,
		&a.Vel3D, &b.Vel3D,
		physics.MassStorm, physics.MassStorm,
		parameter.StormRestitution,
	)
	if collided {
		s.world.PushEvent(event.EventDustAllRequest, nil)
	}
}

// processCircleCollisions destroys non-protected entities at circle's elliptical footprint
func (s *StormSystem) processCircleCollisions(circleEntity core.Entity, newGridX, newGridY int) {
	radiusX := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusY := vmath.ToInt(parameter.StormCircleRadiusY)

	cursorEntity := s.world.Resources.Player.Entity

	// Build member exclusion set
	headerComp, hasHeader := s.world.Components.Header.GetComponent(circleEntity)
	clear(s.memberExcludeSet)
	s.memberExcludeSet[circleEntity] = struct{}{}
	if hasHeader {
		for _, m := range headerComp.MemberEntries {
			if m.Entity != 0 {
				s.memberExcludeSet[m.Entity] = struct{}{}
			}
		}
	}

	var toDestroy []core.Entity

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			if vmath.EllipseDistSq(dx, dy, parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq) > vmath.Scale {
				continue
			}

			cellX := newGridX + x
			cellY := newGridY + y

			entities := s.world.Positions.GetAllEntityAt(cellX, cellY)
			for _, e := range entities {
				_, excluded := s.memberExcludeSet[e]
				if e == 0 || e == cursorEntity || excluded {
					continue
				}

				if s.world.Components.Wall.HasEntity(e) {
					continue
				}

				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask&component.ProtectFromSpecies != 0 || prot.Mask == component.ProtectAll {
						continue
					}
				}

				if s.world.Components.Nugget.HasEntity(e) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: e,
					})
				}

				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy)
	}
}

// processCircleMemberCombat scans members for HP<=0 and routes deaths through CompositeSystem, storm system it the combat-based lifecycle authority
func (s *StormSystem) processCircleMemberCombat(stormComp *component.StormComponent) {
	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		headerComp, ok := s.world.Components.Header.GetComponent(circleEntity)
		if !ok {
			continue
		}

		// Scan members for combat deaths
		var deadMembers []core.Entity
		livingCount := 0

		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			combatComp, ok := s.world.Components.Combat.GetComponent(member.Entity)
			if !ok {
				// Storm members always have CombatComponent; absence = dead
				continue
			}

			if combatComp.HitPoints <= 0 {
				deadMembers = append(deadMembers, member.Entity)
			} else {
				livingCount++
			}
		}

		// Emit deaths for members with HP<=0
		for _, memberEntity := range deadMembers {
			s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
				HeaderEntity: circleEntity,
				MemberEntity: memberEntity,
			})
		}

		// Circle destruction: trigger when no living members remain
		if livingCount == 0 && stormComp.CirclesAlive[i] {
			s.destroyCircle(stormComp, i)
		}
	}
}

// destroyCircle handles individual circle death
func (s *StormSystem) destroyCircle(stormComp *component.StormComponent, index int) {
	circleEntity := stormComp.Circles[index]

	// Get position for event
	var posX, posY int
	if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
		posX, posY = pos.X, pos.Y
	}

	// Mark as dead
	stormComp.CirclesAlive[index] = false

	// Emit circle death event
	s.world.PushEvent(event.EventStormCircleDied, &event.StormCircleDiedPayload{
		CircleEntity: circleEntity,
		RootEntity:   s.rootEntity,
		Index:        index,
	})

	// Destroy circle header via composite system
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: circleEntity,
		Effect:       0, // Silent destruction
	})

	// Check if all dead
	if stormComp.AliveCount() == 0 {
		s.world.PushEvent(event.EventStormDied, &event.StormDiedPayload{
			RootEntity: s.rootEntity,
		})

		// Emit enemy killed
		s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
			Species: component.SpeciesStorm,
			X:       posX,
			Y:       posY,
		})
	}
}

// handleCircleBreach processes external destruction of a circle
func (s *StormSystem) handleCircleBreach(headerEntity core.Entity) {
	if s.rootEntity == 0 {
		return
	}

	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if !ok {
		return
	}

	// Find which circle was breached
	for i := 0; i < component.StormCircleCount; i++ {
		if stormComp.Circles[i] == headerEntity && stormComp.CirclesAlive[i] {
			stormComp.CirclesAlive[i] = false

			s.world.PushEvent(event.EventStormCircleDied, &event.StormCircleDiedPayload{
				CircleEntity: headerEntity,
				RootEntity:   s.rootEntity,
				Index:        i,
			})

			if stormComp.AliveCount() == 0 {
				s.world.PushEvent(event.EventStormDied, &event.StormDiedPayload{
					RootEntity: s.rootEntity,
				})
			}

			s.world.Components.Storm.SetComponent(s.rootEntity, stormComp)
			return
		}
	}
}

// handleCircleInteractions processes player collision and shield drain
func (s *StormSystem) handleCircleInteractions(stormComp *component.StormComponent) {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	shieldComp, shieldOK := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOK && shieldComp.Active

	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]

		headerComp, ok := s.world.Components.Header.GetComponent(circleEntity)
		if !ok {
			continue
		}

		// Check cursor collision across all member positions
		anyOnCursor := false
		var hitEntities []core.Entity

		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberPos, ok := s.world.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
				anyOnCursor = true
			}

			if shieldActive && vmath.EllipseContainsPoint(
				memberPos.X, memberPos.Y,
				cursorPos.X, cursorPos.Y,
				shieldComp.InvRxSq, shieldComp.InvRySq,
			) {
				hitEntities = append(hitEntities, member.Entity)
			}
		}

		// Shield interaction
		if len(hitEntities) > 0 {
			s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Value: parameter.QuasarShieldDrain,
			})

			s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackShield,
				OwnerEntity:  cursorEntity,
				OriginEntity: cursorEntity,
				TargetEntity: circleEntity,
				HitEntities:  hitEntities,
			})
		} else if anyOnCursor && !shieldActive {
			// Direct cursor collision without shield - reset heat
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Delta: -parameter.HeatMax,
			})
		}
	}
}

// updateCircleDamageImmunity sets immunity for concave circles and handles anti-deadlock nudge
func (s *StormSystem) updateCircleDamageImmunity(stormComp *component.StormComponent) {
	nowNano := s.world.Resources.Time.GameTime.UnixNano()

	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetComponent(circleEntity)
		if !ok {
			continue
		}

		isInvulnerable := circleComp.Pos3D.Z >= parameter.StormZMid

		if isInvulnerable {
			// Track invulnerability duration
			if circleComp.InvulnerableSince == 0 {
				circleComp.InvulnerableSince = nowNano
			} else {
				// Check for timeout - apply nudge if stuck too long
				elapsed := time.Duration(nowNano - circleComp.InvulnerableSince)
				if elapsed > parameter.StormInvulnerabilityMaxDuration {
					// Apply downward nudge
					circleComp.Vel3D.Z -= parameter.StormInvulnerabilityNudge
					circleComp.InvulnerableSince = nowNano // Reset timer

					// Telemetry (optional)
					s.world.Resources.Status.Ints.Get("storm.nudge_count").Add(1)
				}
			}

			// Set immunity on members
			headerComp, ok := s.world.Components.Header.GetComponent(circleEntity)
			if !ok {
				continue
			}

			for _, member := range headerComp.MemberEntries {
				if member.Entity == 0 {
					continue
				}
				memberCombat, ok := s.world.Components.Combat.GetComponent(member.Entity)
				if !ok {
					continue
				}
				memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
				s.world.Components.Combat.SetComponent(member.Entity, memberCombat)
			}
		} else {
			// Reset invulnerability tracking when vulnerable
			circleComp.InvulnerableSince = 0
		}

		s.world.Components.StormCircle.SetComponent(circleEntity, circleComp)
	}
}

// updateCircleAttacks manages attack state machine for each circle
func (s *StormSystem) updateCircleAttacks(stormComp *component.StormComponent, dt time.Duration) {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetComponent(circleEntity)
		if !ok {
			continue
		}

		circlePos, ok := s.world.Positions.GetPosition(circleEntity)
		if !ok {
			continue
		}

		circleType := circleComp.CircleType()
		// isConvex is guaranteed true with physics override
		isConvex := circleComp.IsConvex()

		switch circleComp.AttackState {
		case component.StormCircleAttackIdle:
			// Start the cooldown timer
			circleComp.AttackState = component.StormCircleAttackCooldown
			circleComp.CooldownRemaining = s.getInitialCooldown(circleType)

		case component.StormCircleAttackCooldown:
			// Cooldown: Always tick down, regardless of position
			circleComp.CooldownRemaining -= dt
			if circleComp.CooldownRemaining <= 0 {
				// Fire attack if in convex (natural z orbiting)
				if isConvex {
					circleComp.AttackState = component.StormCircleAttackActive
					circleComp.AttackRemaining = s.getAttackDuration(circleType)
					circleComp.AttackProgress = 0

					// Lock target for red cone
					if circleType == component.StormCircleRed {
						circleComp.LockedTargetX = cursorPos.X
						circleComp.LockedTargetY = cursorPos.Y
					}

					// Blue: init attack (calculate target, trigger spawn)
					if circleType == component.StormCircleBlue {
						s.initBlueAttack(&circleComp, circlePos.X, circlePos.Y)
					}
				}
			}

		case component.StormCircleAttackActive:
			// ACTIVE: Run the attack, lock physics in convex
			s.processCircleAttack(&circleComp, circlePos.X, circlePos.Y, cursorEntity, cursorPos)

			circleComp.AttackRemaining -= dt
			if circleComp.AttackRemaining <= 0 {
				// Attack complete, cycle to cooldown
				circleComp.AttackState = component.StormCircleAttackCooldown
				circleComp.CooldownRemaining = s.getRepeatCooldown(circleType)
				circleComp.AttackProgress = 0
			}
		}

		s.world.Components.StormCircle.SetComponent(circleEntity, circleComp)
	}
}

// getInitialCooldown returns the first cooldown when entering convex
func (s *StormSystem) getInitialCooldown(circleType component.StormCircleType) time.Duration {
	switch circleType {
	case component.StormCircleGreen:
		return parameter.StormGreenInitialCooldown
	case component.StormCircleRed:
		return parameter.StormRedInitialCooldown
	case component.StormCircleBlue:
		return parameter.StormBlueInitialCooldown
	default:
		return 0
	}
}

// getAttackDuration returns how long the attack phase lasts
func (s *StormSystem) getAttackDuration(circleType component.StormCircleType) time.Duration {
	switch circleType {
	case component.StormCircleGreen:
		return parameter.StormGreenRepeatInterval
	case component.StormCircleRed:
		return parameter.StormRedTravelDuration
	case component.StormCircleBlue:
		return parameter.StormBlueEffectDuration
	default:
		return 0
	}
}

// getRepeatCooldown returns cooldown between repeated attacks
func (s *StormSystem) getRepeatCooldown(circleType component.StormCircleType) time.Duration {
	switch circleType {
	case component.StormCircleGreen:
		return parameter.StormGreenRepeatInterval
	case component.StormCircleRed:
		return parameter.StormRedPostAttackDelay
	case component.StormCircleBlue:
		return parameter.StormBlueRepeatCooldown
	default:
		return 0
	}
}

// processCircleAttack handles per-tick damage for active attacks
func (s *StormSystem) processCircleAttack(
	circleComp *component.StormCircleComponent,
	circleX, circleY int,
	cursorEntity core.Entity,
	cursorPos component.PositionComponent,
) {
	circleType := circleComp.CircleType()

	switch circleType {
	case component.StormCircleGreen:
		s.processGreenAttack(circleComp, circleX, circleY, cursorEntity, cursorPos)
	case component.StormCircleRed:
		s.processRedAttack(circleComp, circleX, circleY, cursorPos)
	case component.StormCircleBlue:
		s.processBlueAttack(circleComp)
	}
}

// processGreenAttack handles area pulse damage around green circle
func (s *StormSystem) processGreenAttack(
	circleComp *component.StormCircleComponent,
	circleX, circleY int,
	cursorEntity core.Entity,
	cursorPos component.PositionComponent,
) {
	// Update visual progress unconditionally
	attackDuration := parameter.StormGreenRepeatInterval.Seconds()
	remaining := circleComp.AttackRemaining.Seconds()
	circleComp.AttackProgress = 1.0 - (remaining / attackDuration)

	// Telemetry
	s.statGreenActiveFrame.Add(1)

	// Check cursor in attack area
	dx := vmath.FromInt(cursorPos.X - circleX)
	dy := vmath.FromInt(cursorPos.Y - circleY)

	if vmath.EllipseDistSq(dx, dy, parameter.StormGreenInvRxSq, parameter.StormGreenInvRySq) > vmath.Scale {
		return
	}

	shieldComp, shieldOK := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOK && shieldComp.Active

	if shieldActive {
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.StormGreenDamageEnergy,
		})
	} else {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Delta: -parameter.StormGreenDamageHeat,
		})
	}
}

// processRedAttack handles cone projectile damage toward locked target
func (s *StormSystem) processRedAttack(
	circleComp *component.StormCircleComponent,
	circleX, circleY int,
	cursorPos component.PositionComponent,
) {
	totalDuration := parameter.StormRedTravelDuration.Seconds()
	remaining := circleComp.AttackRemaining.Seconds()
	progress := 1.0 - (remaining / totalDuration)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	circleComp.AttackProgress = progress

	s.statRedActiveFrame.Add(1)

	// Direction from circle center to current cursor position (tracks cursor)
	dx := float64(cursorPos.X - circleX)
	dy := float64(cursorPos.Y - circleY)
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		return
	}
	dx /= dist
	dy /= dist

	// Spawn at exterior of circle ellipse with margin
	angle := math.Atan2(dy, dx)
	spawnOffX := parameter.StormCircleRadiusXFloat * parameter.StormRedBulletSpawnMargin * math.Cos(angle)
	spawnOffY := parameter.StormCircleRadiusYFloat * parameter.StormRedBulletSpawnMargin * math.Sin(angle)

	originX := vmath.FromFloat(float64(circleX)+spawnOffX) + vmath.CellCenter
	originY := vmath.FromFloat(float64(circleY)+spawnOffY) + vmath.CellCenter

	// Random spread within cone half-angle
	spreadFrac := float64(s.rng.Intn(1000))/1000.0 - 0.5 // [-0.5, 0.5)
	spreadRad := spreadFrac * 2.0 * parameter.StormRedBulletSpreadHalfAngle
	cosS := math.Cos(spreadRad)
	sinS := math.Sin(spreadRad)
	bulletDirX := dx*cosS - dy*sinS
	bulletDirY := dx*sinS + dy*cosS

	velX := vmath.FromFloat(bulletDirX * parameter.StormRedBulletSpeedFloat)
	velY := vmath.FromFloat(bulletDirY * parameter.StormRedBulletSpeedFloat)

	s.world.PushEvent(event.EventBulletSpawnRequest, &event.BulletSpawnRequestPayload{
		OriginX:     originX,
		OriginY:     originY,
		VelX:        velX,
		VelY:        velY,
		Owner:       s.rootEntity,
		MaxLifetime: parameter.StormRedBulletMaxLifetime,
		Damage: component.BulletDamage{
			EnergyDrain: parameter.StormRedDamageBulletEnergy,
			HeatDelta:   -parameter.StormRedDamageHeat,
		},
	})
}

// initBlueAttack calculates target position at attack start
func (s *StormSystem) initBlueAttack(
	circleComp *component.StormCircleComponent,
	circleX, circleY int,
) {
	config := s.world.Resources.Config

	angle := (float64(s.rng.Intn(100)) / 100.0) * 2 * math.Pi
	distance := parameter.StormBlueSpawnDistanceFloat

	targetX := circleX + int(distance*math.Cos(angle))
	targetY := circleY + int(distance*math.Sin(angle)*0.5)

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

	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		targetX, targetY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		circleComp.LockedTargetX = 0
		circleComp.LockedTargetY = 0
		return
	}

	spawnX := topLeftX + parameter.SwarmHeaderOffsetX
	spawnY := topLeftY + parameter.SwarmHeaderOffsetY
	circleComp.LockedTargetX = spawnX
	circleComp.LockedTargetY = spawnY
}

// processPendingBlueSpawns handles swarm spawns after materialize animation completes
func (s *StormSystem) processPendingBlueSpawns() {
	if len(s.pendingBlueSpawns) == 0 {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	for i := len(s.pendingBlueSpawns) - 1; i >= 0; i-- {
		s.pendingBlueSpawns[i].Timer -= dt

		if s.pendingBlueSpawns[i].Timer <= 0 {
			spawn := s.pendingBlueSpawns[i]

			s.world.PushEvent(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
				SpawnX: spawn.TargetX,
				SpawnY: spawn.TargetY,
			})

			// Remove completed spawn (swap-remove)
			s.pendingBlueSpawns[i] = s.pendingBlueSpawns[len(s.pendingBlueSpawns)-1]
			s.pendingBlueSpawns = s.pendingBlueSpawns[:len(s.pendingBlueSpawns)-1]
		}
	}
}

// processBlueAttack updates visual progress and triggers materialize at threshold
func (s *StormSystem) processBlueAttack(
	circleComp *component.StormCircleComponent,
) {
	s.statBlueActiveFrame.Add(1)

	totalDuration := parameter.StormBlueEffectDuration.Seconds()
	remaining := circleComp.AttackRemaining.Seconds()
	progress := 1.0 - (remaining / totalDuration)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	circleComp.AttackProgress = progress

	// Trigger materialize at 80% (one-shot via pending spawn check)
	if progress >= parameter.StormBlueMaterializeAt &&
		circleComp.LockedTargetX != 0 &&
		!s.hasPendingBlueSpawn(circleComp.LockedTargetX, circleComp.LockedTargetY) {

		topLeftX := circleComp.LockedTargetX - parameter.SwarmHeaderOffsetX
		topLeftY := circleComp.LockedTargetY - parameter.SwarmHeaderOffsetY

		s.world.PushEvent(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
			X:          topLeftX,
			Y:          topLeftY,
			AreaWidth:  parameter.SwarmWidth,
			AreaHeight: parameter.SwarmHeight,
			Type:       component.SpawnTypeSwarm,
		})

		s.pendingBlueSpawns = append(s.pendingBlueSpawns, pendingBlueSpawn{
			TargetX: circleComp.LockedTargetX,
			TargetY: circleComp.LockedTargetY,
			Timer:   parameter.MaterializeAnimationDuration,
		})
	}
}

// hasPendingBlueSpawn checks if spawn is already pending for target
func (s *StormSystem) hasPendingBlueSpawn(targetX, targetY int) bool {
	for _, p := range s.pendingBlueSpawns {
		if p.TargetX == targetX && p.TargetY == targetY {
			return true
		}
	}
	return false
}

// terminateStorm destroys the entire storm entity
func (s *StormSystem) terminateStorm() {
	if s.rootEntity == 0 {
		return
	}

	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if ok {
		// Destroy remaining circles
		for i := 0; i < component.StormCircleCount; i++ {
			if stormComp.CirclesAlive[i] {
				s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
					HeaderEntity: stormComp.Circles[i],
					Effect:       0,
				})
			}
		}
	}

	// Destroy root
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: s.rootEntity,
		Effect:       0,
	})

	s.rootEntity = 0
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
}