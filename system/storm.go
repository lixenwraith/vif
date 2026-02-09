package system

import (
	"math"
	"sync/atomic"

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

	// Telemetry
	statActive      *atomic.Bool
	statCircleCount *atomic.Int64

	enabled bool
}

func NewStormSystem(world *engine.World) engine.System {
	s := &StormSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("storm.active")
	s.statCircleCount = world.Resources.Status.Ints.Get("storm.circle_count")

	s.Init()
	return s
}

func (s *StormSystem) Init() {
	s.rootEntity = 0
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.swarmCache = make([]swarmCacheEntry, 0, 10)
	s.quasarCache = make([]quasarCacheEntry, 0, 1)
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
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

	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if !ok {
		s.rootEntity = 0
		s.statActive.Store(false)
		return
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
	s.checkCircleCombat(&stormComp)
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
	centerX := config.GameWidth / 2
	centerY := config.GameHeight / 2

	// Create root phantom header
	rootEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(rootEntity, component.PositionComponent{X: centerX, Y: centerY})
	s.world.Components.Protection.SetComponent(rootEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// Initialize storm component
	stormComp := component.StormComponent{}

	// Create 3 circle headers in equilateral triangle, staggered Z
	angleOffsets := [3]float64{0, 2 * math.Pi / 3, 4 * math.Pi / 3}
	zOffsets := [3]float64{-1.0, 0.0, 1.0}
	initialRadius := parameter.StormInitialRadiusFloat
	initialSpeed := parameter.StormInitialSpeedFloat

	// Base Z in middle of depth range
	baseZ := (parameter.StormZMinFloat + parameter.StormZMaxFloat) / 2

	for i := 0; i < component.StormCircleCount; i++ {
		angle := angleOffsets[i]
		offsetX := initialRadius * math.Cos(angle)
		offsetY := initialRadius * math.Sin(angle) * 0.5 // Terminal aspect ratio
		offsetZ := zOffsets[i] * parameter.StormZSpawnOffsetFloat

		pos3D := vmath.Vec3{
			X: vmath.FromFloat(float64(centerX) + offsetX),
			Y: vmath.FromFloat(float64(centerY) + offsetY),
			Z: vmath.FromFloat(baseZ + offsetZ),
		}

		// Tangential velocity for orbital motion + random Z component
		vel3D := vmath.Vec3{
			X: vmath.FromFloat(-initialSpeed * math.Sin(angle)),
			Y: vmath.FromFloat(initialSpeed * math.Cos(angle) * 0.5),
			Z: vmath.FromFloat(float64(s.rng.Intn(6)-3) * 0.8),
		}

		circleEntity := s.createCircleHeader(rootEntity, i, pos3D, vel3D)
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
			Layer:   component.LayerEffect,
		}
	}

	s.world.Components.Header.SetComponent(rootEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
		MemberEntries: rootMembers,
	})

	s.world.Components.Storm.SetComponent(rootEntity, stormComp)

	s.rootEntity = rootEntity
	s.statActive.Store(true)
	s.statCircleCount.Store(component.StormCircleCount)
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
		Mask: component.ProtectAll,
	})

	// Storm circle component (3D physics state)
	s.world.Components.StormCircle.SetComponent(circleEntity, component.StormCircleComponent{
		Pos3D: pos3D,
		Vel3D: vel3D,
		Index: index,
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
		HitPoints:        parameter.CombatInitialHPStorm,
	})

	// Generate members
	members := s.createCircleMembers(circleEntity, gridX, gridY)

	// Header component linking back to root
	s.world.Components.Header.SetComponent(circleEntity, component.HeaderComponent{
		Behavior:      component.BehaviorStorm,
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

	// Pre-calculate ellipse constants for this circle size
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(parameter.StormCircleRadiusX, parameter.StormCircleRadiusY)

	var members []component.MemberEntry

	// Iterate bounding box
	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			// Check if cell is inside ellipse
			dx := vmath.FromInt(x)
			dy := vmath.FromInt(y)

			if vmath.EllipseDistSq(dx, dy, invRxSq, invRySq) <= vmath.Scale {

				// Create the member entity
				memberEntity := s.world.CreateEntity()

				// Set member position
				s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
					X: headerX + x,
					Y: headerY + y,
				})

				// Member protection
				s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
					Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromDrain,
				})

				// Backlink
				s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
					HeaderEntity: headerEntity,
				})

				members = append(members, component.MemberEntry{
					Entity:  memberEntity,
					OffsetX: x,
					OffsetY: y,
					Layer:   component.LayerGlyph,
				})
			}
		}
	}
	return members
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

	// Accumulate gravitational acceleration with repulsion from other circles
	for i := range circles {
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

		// Integrate velocity
		circles[i].circle.Vel3D.X += vmath.Mul(accelX, dtFixed)
		circles[i].circle.Vel3D.Y += vmath.Mul(accelY, dtFixed)
		circles[i].circle.Vel3D.Z += vmath.Mul(accelZ, dtFixed)

		// Apply dt-dependent damping
		circles[i].circle.Vel3D = vmath.V3DampDt(circles[i].circle.Vel3D, parameter.StormDamping, dtFixed)

		// Clamp velocity
		circles[i].circle.Vel3D = vmath.V3ClampMagnitude(circles[i].circle.Vel3D, parameter.StormMaxVelocity)

		// Integrate position
		circles[i].circle.Pos3D = vmath.V3Add(
			circles[i].circle.Pos3D,
			vmath.V3Scale(circles[i].circle.Vel3D, dtFixed),
		)
	}

	// Boundary reflection with insets for visual radius
	insetX := parameter.StormBoundaryInsetX
	insetY := parameter.StormBoundaryInsetY
	gameMinX := vmath.FromInt(insetX)
	gameMaxX := vmath.FromInt(config.GameWidth - 1 - insetX)
	gameMinY := vmath.FromInt(insetY)
	gameMaxY := vmath.FromInt(config.GameHeight - 1 - insetY)

	for i := range circles {
		// XY boundary reflection
		physics.ReflectAxis3D(&circles[i].circle.Pos3D.X, &circles[i].circle.Vel3D.X,
			gameMinX, gameMaxX, parameter.StormRestitution)
		physics.ReflectAxis3D(&circles[i].circle.Pos3D.Y, &circles[i].circle.Vel3D.Y,
			gameMinY, gameMaxY, parameter.StormRestitution)

		// Z depth bounds
		physics.ReflectAxis3D(&circles[i].circle.Pos3D.Z, &circles[i].circle.Vel3D.Z,
			parameter.StormZMin, parameter.StormZMax, parameter.StormRestitution)

		// Wall collision check at circle center
		gridX := vmath.ToInt(circles[i].circle.Pos3D.X)
		gridY := vmath.ToInt(circles[i].circle.Pos3D.Y)

		if s.world.Positions.HasBlockingWallAt(gridX, gridY, component.WallBlockKinetic) {
			// Reverse velocity on wall hit
			circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X
			circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y
		}
	}

	// Inter-circle collision (kept for hard overlaps, but repulsion reduces frequency)
	for i := 0; i < len(circles); i++ {
		for j := i + 1; j < len(circles); j++ {
			s.resolveCircleCollision(circles[i].circle, circles[j].circle)
		}
	}

	// Sync 3D position to 2D components
	for i := range circles {
		s.syncCircleTo2D(circles[i].entity, circles[i].circle)
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

// syncCircleTo2D updates grid position and kinetic component from 3D state
func (s *StormSystem) syncCircleTo2D(entity core.Entity, circle *component.StormCircleComponent) {
	newGridX := vmath.ToInt(circle.Pos3D.X)
	newGridY := vmath.ToInt(circle.Pos3D.Y)

	// Update grid position
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		if pos.X != newGridX || pos.Y != newGridY {
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: newGridX, Y: newGridY})
		}
	}

	// Update kinetic for 2D collision compatibility
	if kinetic, ok := s.world.Components.Kinetic.GetComponent(entity); ok {
		kinetic.PreciseX = circle.Pos3D.X
		kinetic.PreciseY = circle.Pos3D.Y
		kinetic.VelX = circle.Vel3D.X
		kinetic.VelY = circle.Vel3D.Y
		s.world.Components.Kinetic.SetComponent(entity, kinetic)
	}
}

// checkCircleCombat processes HP checks for each circle
func (s *StormSystem) checkCircleCombat(stormComp *component.StormComponent) {
	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		combatComp, ok := s.world.Components.Combat.GetComponent(circleEntity)
		if !ok {
			continue
		}

		if combatComp.HitPoints <= 0 {
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

	// Emit enemy killed for potential loot
	s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
		EnemyType: component.CombatEntityStorm,
		X:         posX,
		Y:         posY,
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
		circlePos, ok := s.world.Positions.GetPosition(circleEntity)
		if !ok {
			continue
		}

		// Check direct cursor collision
		if circlePos.X == cursorPos.X && circlePos.Y == cursorPos.Y {
			if !shieldActive {
				// Reset heat on direct collision without shield
				s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
					Delta: -parameter.HeatMax,
				})
			}
			continue
		}

		// Check shield overlap (ellipse containment)
		if shieldActive {
			dx := vmath.FromInt(circlePos.X - cursorPos.X)
			dy := vmath.FromInt(circlePos.Y - cursorPos.Y)
			if vmath.EllipseContains(dx, dy, shieldComp.InvRxSq, shieldComp.InvRySq) {
				// Shield drain
				s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
					Value: parameter.QuasarShieldDrain, // Use quasar drain rate for now
				})

				// Knockback via combat system
				s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
					AttackType:   component.CombatAttackShield,
					OwnerEntity:  cursorEntity,
					OriginEntity: cursorEntity,
					TargetEntity: circleEntity,
					HitEntities:  []core.Entity{circleEntity},
				})
			}
		}
	}
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