package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

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

	// Precomputed ellipse cell offsets for wall collision
	ellipseOffsets []vmath.Point

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
	statNudges           *atomic.Int64
	statProtected        *atomic.Int64
	lifecycle            lifecycleTelemetry
	buffers              bufferTelemetry
	motion               bounceTelemetry
	sweep                cellSweep

	enabled bool
}

func NewStormSystem(world *engine.World) engine.System {
	s := &StormSystem{
		world: world,
	}

	s.memberExcludeSet = make(map[core.Entity]struct{}, 256)
	s.pendingBlueSpawns = make([]pendingBlueSpawn, 0, 4)
	s.buffers = newBufferTelemetry(world.Resources.Status, "storm", "ellipse_offsets", "member_excludes", "pending_blue_spawns")

	// Precompute ellipse cell offsets for wall collision checks
	s.buildEllipseOffsets()

	s.statActive = world.Resources.Status.Bools.Get("storm.active")
	s.statCircleCount = world.Resources.Status.Ints.Get("storm.circle_count")
	s.statGreenActiveFrame = world.Resources.Status.Ints.Get("storm.green_active_frames")
	s.statRedActiveFrame = world.Resources.Status.Ints.Get("storm.red_active_frames")
	s.statBlueActiveFrame = world.Resources.Status.Ints.Get("storm.blue_active_frames")
	s.statNudges = world.Resources.Status.Ints.Get("storm.nudge_count")
	s.statProtected = world.Resources.Status.Ints.Get("storm.protected_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "storm")
	s.motion = newBounceTelemetry(world.Resources.Status, "storm")

	s.Init()
	return s
}

func (s *StormSystem) Init() {
	s.rootEntity = 0
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	clear(s.memberExcludeSet)
	s.pendingBlueSpawns = s.pendingBlueSpawns[:0]
	s.statActive.Store(false)
	s.statCircleCount.Store(0)
	s.statGreenActiveFrame.Store(0)
	s.statRedActiveFrame.Store(0)
	s.statBlueActiveFrame.Store(0)
	s.statNudges.Store(0)
	s.statProtected.Store(0)
	s.lifecycle.Reset()
	s.buffers.Reset()
	s.motion.Reset()
	s.buffers.Observe(0, len(s.ellipseOffsets))
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
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *StormSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesStorm {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventStormSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventStormSpawnRequest:
		if s.rootEntity == 0 {
			s.spawnStorm()
		} else {
			s.lifecycle.spawnFailures.Add(1)
		}

	case event.EventStormCancelRequest:
		if s.rootEntity != 0 {
			s.lifecycle.despawned.Add(1)
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

	// Process pending blue spawns regardless of root entity state (unless dead already)
	s.processPendingBlueSpawns()

	// When termination is requested, liveness edits intentionally are not
	// committed; retain the detached root value across that early return.
	stormComp, ok := s.world.Components.Storm.GetComponent(s.rootEntity)
	if !ok {
		s.rootEntity = 0
		s.statActive.Store(false)
		return
	}

	// Check liveness via Header existence (CompositeSystem authority)
	for i := range component.StormCircleCount {
		if stormComp.CirclesAlive[i] && !s.world.Components.Header.HasEntity(stormComp.Circles[i]) {
			stormComp.CirclesAlive[i] = false
		}
	}

	// Check if all circles dead
	aliveCount := s.AliveCount(&stormComp)
	if aliveCount == 0 {
		s.terminateStorm()
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := min(dt.Seconds(), 0.1)

	// Process each alive circle
	s.updateCirclePhysics(&stormComp, dtSec)
	s.updateCircleDamageImmunity(&stormComp, dt)
	s.updateCircleAttacks(&stormComp, dt)
	s.processCircleMemberCombat(&stormComp)
	s.handleCircleInteractions(&stormComp)

	s.world.Components.Storm.SetComponent(s.rootEntity, stormComp)
	s.statCircleCount.Store(int64(aliveCount))
}

// buildEllipseOffsets populates the LUT of cell offsets inside the circle ellipse
func (s *StormSystem) buildEllipseOffsets() {
	radiusX := int(parameter.StormCircleRadiusX)
	radiusY := int(parameter.StormCircleRadiusY)

	// Preallocate approximate capacity: Ï€ * rx * ry
	capacity := int(3.2 * float64(radiusX) * float64(radiusY))
	s.ellipseOffsets = make([]vmath.Point, 0, capacity)

	for y := -radiusY; y <= radiusY; y++ {
		for x := -radiusX; x <= radiusX; x++ {
			if vmath.EllipseContainsF(float64(x), float64(y),
				parameter.StormCircleCollisionInvRxSq, parameter.StormCircleCollisionInvRySq) {
				s.ellipseOffsets = append(s.ellipseOffsets, vmath.Point{X: x, Y: y})
			}
		}
	}
}

// AliveCount returns number of living circles
func (s *StormSystem) AliveCount(c *component.StormComponent) int {
	count := 0
	for _, alive := range c.CirclesAlive {
		if alive {
			count++
		}
	}
	return count
}

// spawnStorm creates the root header and 3 circle sub-headers
func (s *StormSystem) spawnStorm() {
	config := s.world.Resources.Config
	centerX := config.MapWidth / 2
	centerY := config.MapHeight / 2
	centerPX, centerPY := (vmath.Point{X: centerX, Y: centerY}).CenterF()

	// Pre-calculate circle spawn parameters
	angleOffsets := [3]float64{0, vmath.TwoPi / 3, 2 * vmath.TwoPi / 3}
	zOffsets := [3]float64{-1.0, 0.0, 1.0}
	initialRadius := parameter.StormInitialRadius
	initialSpeed := parameter.StormInitialSpeed
	baseZ := parameter.StormZMid

	type circleSpawnInfo struct {
		gridX, gridY int
		pos3D        vmath.Vec3F
		vel3D        vmath.Vec3F
	}

	var circleInfos [component.StormCircleCount]circleSpawnInfo

	// 1. Calculate target positions and validate all circles
	for i := range component.StormCircleCount {
		angle := angleOffsets[i]
		offsetX := initialRadius * vmath.CosF(angle)
		offsetY := initialRadius * vmath.SinF(angle) * 0.5 // Terminal aspect ratio

		target := vmath.PointAtF(centerPX+offsetX, centerPY+offsetY)
		targetX, targetY := target.X, target.Y

		// Find valid position via spiral search
		foundX, foundY, found := s.findCirclePosition(targetX, targetY)
		if !found {
			s.lifecycle.spawnFailures.Add(1)
			return // Abort entire spawn - one circle failed
		}

		// Circle center at the cell center, matching every other spawn path
		centerPX, centerPY := vmath.Point{X: foundX, Y: foundY}.CenterF()

		circleInfos[i] = circleSpawnInfo{
			gridX: foundX,
			gridY: foundY,
			pos3D: vmath.Vec3F{
				X: centerPX,
				Y: centerPY,
				Z: baseZ + zOffsets[i]*parameter.StormZSpawnOffset,
			},
			vel3D: vmath.Vec3F{
				X: -initialSpeed * vmath.SinF(angle),
				Y: initialSpeed * vmath.CosF(angle) * 0.5,
				Z: float64(s.rng.Intn(6)-3) * 0.8,
			},
		}
	}

	// 2. Clear all spawn areas (validation passed)
	for i := range component.StormCircleCount {
		s.clearCircleSpawnArea(circleInfos[i].gridX, circleInfos[i].gridY)
	}

	// 3. Create entities
	rootEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Protection.SetComponent(rootEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	stormComp := component.StormComponent{}

	for i := range component.StormCircleCount {
		circleEntity := s.createCircleHeader(rootEntity, i, circleInfos[i].pos3D, circleInfos[i].vel3D)
		stormComp.Circles[i] = circleEntity
		stormComp.CirclesAlive[i] = true
	}

	// Root header component linking circles
	rootMembers := make([]component.MemberEntry, component.StormCircleCount)
	for i := range component.StormCircleCount {
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

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      rootEntity,
		Species:     component.SpeciesStorm,
		X:           -1,
		Y:           -1,
		MemberCount: len(rootMembers),
	})
	s.lifecycle.spawned.Add(1)
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

	for _, off := range s.ellipseOffsets {
		cellX := centerX + off.X
		cellY := centerY + off.Y

		if cellX < 0 || cellX >= config.MapWidth || cellY < 0 || cellY >= config.MapHeight {
			return false
		}
		if s.world.Positions.HasBlockingWallAt(cellX, cellY, component.WallBlockSpawn) {
			return false
		}
	}

	return true
}

// clearCircleSpawnArea empties a circle footprint in both domains (D-12)
func (s *StormSystem) clearCircleSpawnArea(centerX, centerY int) {
	s.sweep.reset()
	for _, off := range s.ellipseOffsets {
		s.sweep.collect(s.world, centerX+off.X, centerY+off.Y, func(e core.Entity) bool {
			return speciesClearable(s.world, e, s.statProtected)
		})
	}
	s.sweep.destroy(s.world)
}

// createCircleHeader builds a single circle sub-header entity
func (s *StormSystem) createCircleHeader(
	parentEntity core.Entity,
	index int,
	pos3D, vel3D vmath.Vec3F,
) core.Entity {
	cell := vmath.PointAtF(pos3D.X, pos3D.Y)

	circleEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: cell.X, Y: cell.Y})

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
	s.world.Components.Kinetic.SetComponent(circleEntity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: pos3D.X,
			PreciseY: pos3D.Y,
			// Seeded so the first impulse-absorption delta is zero
			VelX: vel3D.X,
			VelY: vel3D.Y,
		},
	})

	// Combat component
	s.world.Components.Combat.SetComponent(circleEntity, component.CombatComponent{
		OwnerEntity:      circleEntity,
		CombatEntityType: component.CombatEntityStorm,
		HitPoints:        0, // Damage routed to members via ablative model
	})

	// Generate members
	members := s.createCircleMembers(circleEntity, cell.X, cell.Y)

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
	members := make([]component.MemberEntry, 0, len(s.ellipseOffsets))

	for _, off := range s.ellipseOffsets {
		memberEntity := s.world.CreateEntity(core.DomainShared)

		s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
			X: headerX + off.X,
			Y: headerY + off.Y,
		})

		s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
			Mask: component.ProtectFromDecay | component.ProtectFromSpecies,
		})

		// Ablative health: per-member HP for combat damage
		s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
			OwnerEntity:      headerEntity,
			CombatEntityType: component.CombatEntityStorm,
			HitPoints:        parameter.CombatInitialHPStormMember,
		})

		s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
			HeaderEntity: headerEntity,
		})

		members = append(members, component.MemberEntry{
			Entity:  memberEntity,
			OffsetX: off.X,
			OffsetY: off.Y,
		})
	}
	return members
}

// concave reports whether a circle sits behind the vulnerability boundary
func concave(c *component.StormCircleComponent) bool {
	return c.Pos3D.Z >= parameter.StormZMid
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
func (s *StormSystem) updateCirclePhysics(stormComp *component.StormComponent, dtSec float64) {
	config := s.world.Resources.Config

	// Collect alive circles
	type circleState struct {
		circle  *component.StormCircleComponent
		entity  core.Entity
		stunned bool
	}

	// StormCircle lifecycle changes are event-queued, so all direct pointers stay
	// valid through the physics, collision, and synchronization phases below.
	var circles []circleState
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}
		circleComp, ok := s.world.Components.StormCircle.GetPtr(stormComp.Circles[i])
		if !ok {
			stormComp.CirclesAlive[i] = false
			continue
		}
		// Check stun state on circle header
		stunned := false
		if combatComp, ok := s.world.Components.Combat.GetComponent(stormComp.Circles[i]); ok {
			stunned = combatComp.StunnedRemaining > 0
		}
		circles = append(circles, circleState{
			entity:  stormComp.Circles[i],
			circle:  circleComp,
			stunned: stunned,
		})
	}

	if len(circles) == 0 {
		return
	}

	// Precompute center-aligned boundary limits accounting for ellipse radius.
	minX, minY := (vmath.Point{X: 0, Y: 0}).CenterF()
	maxX, maxY := (vmath.Point{X: config.MapWidth - 1, Y: config.MapHeight - 1}).CenterF()
	boundMinX := minX + parameter.StormBoundaryInsetX
	boundMaxX := maxX - parameter.StormBoundaryInsetX
	boundMinY := minY + parameter.StormBoundaryInsetY
	boundMaxY := maxY - parameter.StormBoundaryInsetY

	for i := range circles {
		// 1. Stunned circles: skip physics, velocity already zeroed by combat system
		if circles[i].stunned {
			continue
		}
		motion := physics.BounceStats{Steps: 1}

		// 2. Fold combat/dust knockback from the 2D Kinetic into Vel3D
		s.absorbExternalImpulse(circles[i].entity, circles[i].circle)

		// 3. Accumulate gravitational acceleration with repulsion
		var accelX, accelY, accelZ float64
		for j := range circles {
			if i == j {
				continue
			}
			accel := physics.GravitationalAccelWithRepulsion3D(
				circles[i].circle.Pos3D,
				circles[j].circle.Pos3D,
				profile.MassStorm,
				parameter.StormGravity,
				parameter.StormRepulsionRadius,
				parameter.StormRepulsionStrength,
			)
			accelX += accel.X
			accelY += accel.Y
			accelZ += accel.Z
		}

		// 4. Z-axis equilibrium spring: accelZ += stiffness * (zMid - z)
		// Provides restoring force toward vulnerability boundary
		zDelta := parameter.StormZMid - circles[i].circle.Pos3D.Z
		accelZ += parameter.StormZEquilibriumStiffness * zDelta

		// 5. Integrate velocity
		circles[i].circle.Vel3D.X += accelX * dtSec
		circles[i].circle.Vel3D.Y += accelY * dtSec
		circles[i].circle.Vel3D.Z += accelZ * dtSec

		// 6. Apply damping
		circles[i].circle.Vel3D = vmath.V3FDampDt(circles[i].circle.Vel3D, parameter.StormDamping, dtSec)

		// 7. Clamp velocity
		circles[i].circle.Vel3D = vmath.V3FClampMagnitude(circles[i].circle.Vel3D, parameter.StormMaxVelocity)

		// 8. Axis-separated position integration with collision

		// --- X Axis ---
		oldPosX := circles[i].circle.Pos3D.X
		circles[i].circle.Pos3D.X += circles[i].circle.Vel3D.X * dtSec

		// Boundary check X
		if circles[i].circle.Pos3D.X < boundMinX {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.X = boundMinX
			if circles[i].circle.Vel3D.X < 0 {
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		} else if circles[i].circle.Pos3D.X > boundMaxX {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.X = boundMaxX
			if circles[i].circle.Vel3D.X > 0 {
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		} else {
			// Wall check X (only if within bounds)
			cell := vmath.PointAtF(circles[i].circle.Pos3D.X, circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(cell.X, cell.Y) {
				motion.WallCollisions++
				circles[i].circle.Pos3D.X = oldPosX
				circles[i].circle.Vel3D.X = -circles[i].circle.Vel3D.X * parameter.StormRestitution
			}
		}

		// --- Y Axis ---
		oldPosY := circles[i].circle.Pos3D.Y
		circles[i].circle.Pos3D.Y += circles[i].circle.Vel3D.Y * dtSec

		// Boundary check Y
		if circles[i].circle.Pos3D.Y < boundMinY {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.Y = boundMinY
			if circles[i].circle.Vel3D.Y < 0 {
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		} else if circles[i].circle.Pos3D.Y > boundMaxY {
			motion.BoundaryReflections++
			circles[i].circle.Pos3D.Y = boundMaxY
			if circles[i].circle.Vel3D.Y > 0 {
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		} else {
			// Wall check Y (uses potentially updated X position)
			cell := vmath.PointAtF(circles[i].circle.Pos3D.X, circles[i].circle.Pos3D.Y)
			if s.collectAndDestroyWallsInEllipse(cell.X, cell.Y) {
				motion.WallCollisions++
				circles[i].circle.Pos3D.Y = oldPosY
				circles[i].circle.Vel3D.Y = -circles[i].circle.Vel3D.Y * parameter.StormRestitution
			}
		}

		// --- Z Axis (depth bounds only, no walls) ---
		circles[i].circle.Pos3D.Z += circles[i].circle.Vel3D.Z * dtSec
		if physics.ReflectAxis3D(&circles[i].circle.Pos3D.Z, &circles[i].circle.Vel3D.Z,
			parameter.StormZMin, parameter.StormZMax, parameter.StormRestitution) {
			motion.BoundaryReflections++
		}

		// --- ATTACK PHYSICS OVERRIDE ---
		// If attacking, physically trap the circle in the convex
		if circles[i].circle.AttackState == component.StormCircleAttackActive {
			// Define a boundary slightly in front of the Mid point to ensure IsConvex returns true
			zLimit := parameter.StormZMid - 1.0

			if circles[i].circle.Pos3D.Z > zLimit {
				circles[i].circle.Pos3D.Z = zLimit

				// Kill outward (receding) momentum, but allow it to move further forward
				if circles[i].circle.Vel3D.Z > 0 {
					circles[i].circle.Vel3D.Z = 0
				}
			}
		}
		s.motion.Record(motion)
	}

	// Inter-circle collision (skip stunned circles)
	for i := range len(circles) {
		if circles[i].stunned {
			continue
		}
		for j := i + 1; j < len(circles); j++ {
			if circles[j].stunned {
				continue
			}
			s.resolveCircleCollision(circles[i].circle, circles[j].circle)
		}
	}

	// Sync 3D position to 2D components
	for i := range circles {
		circle := circles[i].circle
		circleEntity := circles[i].entity

		cell := vmath.PointAtF(circle.Pos3D.X, circle.Pos3D.Y)
		newGridX, newGridY := cell.X, cell.Y

		// Update grid position
		if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
			if pos.X != newGridX || pos.Y != newGridY {
				s.processCircleCollisions(circleEntity, newGridX, newGridY)
				s.world.Positions.SetPosition(circleEntity, component.PositionComponent{X: newGridX, Y: newGridY})
			}
		}

		// Update kinetic for 2D collision compatibility
		if kinetic, ok := s.world.Components.Kinetic.GetPtr(circleEntity); ok {
			kinetic.PreciseX = circle.Pos3D.X
			kinetic.PreciseY = circle.Pos3D.Y
			kinetic.VelX = circle.Vel3D.X
			kinetic.VelY = circle.Vel3D.Y
		}
	}
}

// absorbExternalImpulse folds knockback written to the 2D Kinetic into the
// authoritative 3D velocity. The end-of-tick sync leaves Kinetic.Vel equal to
// Vel3D.XY, so any difference is an external impulse from combat or dust.
// Z is untouched: knockback sources act in the terminal plane.
func (s *StormSystem) absorbExternalImpulse(circleEntity core.Entity, circle *component.StormCircleComponent) {
	kinetic, ok := s.world.Components.Kinetic.GetPtr(circleEntity)
	if !ok {
		return
	}

	deltaX := kinetic.VelX - circle.Vel3D.X
	deltaY := kinetic.VelY - circle.Vel3D.Y
	if deltaX == 0 && deltaY == 0 {
		return
	}

	circle.Vel3D.X += deltaX
	circle.Vel3D.Y += deltaY
}

// resolveCircleCollision handles elastic collision between two circles
func (s *StormSystem) resolveCircleCollision(a, b *component.StormCircleComponent) {
	minDist := parameter.StormCircleCollisionRadius * 2
	if vmath.V3FMagSq(vmath.V3FSub(b.Pos3D, a.Pos3D)) >= minDist*minDist {
		return
	}

	// SeparateOverlap3D already rejects non-overlapping and coincident pairs
	physics.SeparateOverlap3D(
		&a.Pos3D, &b.Pos3D,
		parameter.StormCircleCollisionRadius, parameter.StormCircleCollisionRadius,
		profile.MassStorm, profile.MassStorm,
	)

	// Elastic collision response (in-place modification)
	collided := physics.ElasticCollision3D(
		&a.Pos3D, &b.Pos3D,
		&a.Vel3D, &b.Vel3D,
		profile.MassStorm, profile.MassStorm,
		parameter.StormRestitution,
	)
	if collided {
		s.world.PushEvent(event.EventDustAllRequest, nil)
	}
}

// processCircleCollisions destroys non-protected entities at a circle's footprint
func (s *StormSystem) processCircleCollisions(circleEntity core.Entity, newGridX, newGridY int) {
	// Build member exclusion set
	headerComp, hasHeader := s.world.Components.Header.GetPtr(circleEntity)
	clear(s.memberExcludeSet)
	s.memberExcludeSet[circleEntity] = struct{}{}
	if hasHeader {
		for _, m := range headerComp.MemberEntries {
			if m.Entity != 0 {
				s.memberExcludeSet[m.Entity] = struct{}{}
			}
		}
	}
	s.buffers.Observe(1, len(s.memberExcludeSet))

	s.sweep.reset()
	for _, off := range s.ellipseOffsets {
		s.sweep.collect(s.world, newGridX+off.X, newGridY+off.Y, func(e core.Entity) bool {
			if _, excluded := s.memberExcludeSet[e]; excluded {
				return false
			}
			if s.world.Components.Cursor.HasEntity(e) || s.world.Components.Wall.HasEntity(e) {
				return false
			}
			if prot, ok := s.world.Components.Protection.GetPtr(e); ok &&
				(prot.Mask&component.ProtectFromSpecies != 0 || prot.Mask == component.ProtectAll) {
				s.statProtected.Add(1)
				return false
			}
			return true
		})
	}

	// Personal nuggets receive a local lifecycle notification before the player batch dies.
	for _, e := range s.sweep.player {
		if s.world.Components.Nugget.HasEntity(e) {
			s.world.PushLocal(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{Entity: e})
		}
	}
	s.sweep.emit(s.world, event.EventFlashSpawnOneRequest)
}

// processCircleMemberCombat scans members for HP<=0 and routes deaths through CompositeSystem, storm system it the combat-based lifecycle authority
func (s *StormSystem) processCircleMemberCombat(stormComp *component.StormComponent) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		headerComp, ok := s.world.Components.Header.GetPtr(circleEntity)
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

			combatComp, ok := s.world.Components.Combat.GetPtr(member.Entity)
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
			s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
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
	var killerEntity core.Entity
	if combatComp, ok := s.world.Components.Combat.GetComponent(circleEntity); ok {
		killerEntity = combatComp.LastDamagedBy
	}

	// Get position for event; the final circle may already be positionless.
	posX, posY := -1, -1
	if pos, ok := s.world.Positions.GetPosition(circleEntity); ok {
		posX, posY = pos.X, pos.Y
	}

	// Mark as dead
	stormComp.CirclesAlive[index] = false

	// Destroy circle header via composite system
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: circleEntity,
		Effect:       0, // Silent destruction
	})

	// Check if all dead
	if s.AliveCount(stormComp) == 0 {
		// The root is the species identity; circles are implementation details.
		s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
			Entity:       s.rootEntity,
			KillerEntity: killerEntity,
			Species:      component.SpeciesStorm,
			X:            posX,
			Y:            posY,
		})
	}
}

// handleCircleBreach processes external destruction of a circle
func (s *StormSystem) handleCircleBreach(headerEntity core.Entity) {
	if s.rootEntity == 0 {
		return
	}

	stormComp, ok := s.world.Components.Storm.GetPtr(s.rootEntity)
	if !ok {
		return
	}

	// Find which circle was breached
	for i := range component.StormCircleCount {
		if stormComp.Circles[i] == headerEntity && stormComp.CirclesAlive[i] {
			stormComp.CirclesAlive[i] = false

			if s.AliveCount(stormComp) == 0 {
				// The last circle disappeared through lifecycle cleanup rather than
				// credited combat. Negative coordinates suppress loot placement.
				s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
					Entity:  s.rootEntity,
					Species: component.SpeciesStorm,
					X:       -1,
					Y:       -1,
				})
			}

			return
		}
	}
}

// handleCircleInteractions processes player collision and shield drain
func (s *StormSystem) handleCircleInteractions(stormComp *component.StormComponent) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]

		overlaps := CheckCursorOverlaps(s.world, circleEntity)
		for j := range overlaps.Count {
			overlap := &overlaps.Entries[j]
			// Apply shield interaction before exact cursor contact.
			if len(overlap.ShieldMembers) > 0 {
				s.world.PushLocal(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
					Entity: overlap.Cursor,
					Value:  parameter.QuasarShieldDrain,
				})

				s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
					AttackType:   component.CombatAttackShield,
					OwnerEntity:  overlap.Cursor,
					OriginEntity: overlap.Cursor,
					TargetEntity: circleEntity,
					HitEntities:  overlap.ShieldMembers,
				})
			} else if overlap.OnCursor && !overlap.ShieldActive {
				// Direct cursor collision without a shield resets heat.
				s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
					Entity: overlap.Cursor,
					Delta:  -parameter.HeatMax,
				})
			}
		}
	}
}

// updateCircleDamageImmunity sets immunity for concave circles and handles anti-deadlock nudge
func (s *StormSystem) updateCircleDamageImmunity(stormComp *component.StormComponent, dt time.Duration) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetPtr(circleEntity)
		if !ok {
			continue
		}

		if !concave(circleComp) {
			circleComp.InvulnerableElapsed = 0
			continue
		}

		// Nudge out of a stalled concave orbit, then restart the accumulator
		circleComp.InvulnerableElapsed += dt
		if circleComp.InvulnerableElapsed > parameter.StormInvulnerabilityMaxDuration {
			circleComp.Vel3D.Z -= parameter.StormInvulnerabilityNudge
			circleComp.InvulnerableElapsed = 0
			s.statNudges.Add(1)
		}

		headerComp, ok := s.world.Components.Header.GetPtr(circleEntity)
		if !ok {
			continue
		}
		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			memberCombat, ok := s.world.Components.Combat.GetPtr(member.Entity)
			if !ok {
				continue
			}
			memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
		}
	}
}

// updateCircleAttacks manages attack state machine for each circle
func (s *StormSystem) updateCircleAttacks(stormComp *component.StormComponent, dt time.Duration) {
	for i := range component.StormCircleCount {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := s.world.Components.StormCircle.GetPtr(circleEntity)
		if !ok {
			continue
		}

		circlePos, ok := s.world.Positions.GetPosition(circleEntity)
		if !ok {
			continue
		}
		_, cursorX, cursorY, hasCursor := ClosestCursor(s.world, circlePos.X, circlePos.Y)
		if !hasCursor {
			continue
		}
		cursorPos := component.PositionComponent{X: cursorX, Y: cursorY}

		circleType := component.StormCircleType(circleComp.Index)
		// Update invulnerable state, isConvex is guaranteed true with physics override
		circleComp.IsInvulnerable = concave(circleComp) && circleComp.AttackState != component.StormCircleAttackActive

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
				if !circleComp.IsInvulnerable {
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
						s.initBlueAttack(circleComp, circlePos.X, circlePos.Y)
					}
				}
			}

		case component.StormCircleAttackActive:
			// ACTIVE: Run the attack, lock physics in convex
			s.processCircleAttack(circleComp, circlePos.X, circlePos.Y, cursorPos)

			circleComp.AttackRemaining -= dt
			if circleComp.AttackRemaining <= 0 {
				// Attack complete, cycle to cooldown
				circleComp.AttackState = component.StormCircleAttackCooldown
				circleComp.CooldownRemaining = s.getRepeatCooldown(circleType)
				circleComp.AttackProgress = 0
			}
		}

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
	cursorPos component.PositionComponent,
) {
	circleType := component.StormCircleType(circleComp.Index)

	switch circleType {
	case component.StormCircleGreen:
		s.processGreenAttack(circleComp, circleX, circleY)
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
) {
	// Update visual progress unconditionally
	attackDuration := parameter.StormGreenRepeatInterval.Seconds()
	remaining := circleComp.AttackRemaining.Seconds()
	circleComp.AttackProgress = 1.0 - (remaining / attackDuration)

	// Telemetry
	s.statGreenActiveFrame.Add(1)

	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		cursorPos, ok := s.world.Positions.GetPosition(cursor)
		if !ok || !vmath.EllipseContainsPointF(cursorPos.X, cursorPos.Y, circleX, circleY,
			parameter.StormGreenInvRxSq, parameter.StormGreenInvRySq) {
			continue
		}

		shieldComp, shieldOK := s.world.Components.Shield.GetComponent(cursor)
		if shieldOK && shieldComp.Active {
			s.world.PushLocal(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Entity: cursor,
				Value:  parameter.StormGreenDamageEnergy,
			})
		} else {
			s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: cursor,
				Delta:  -parameter.StormGreenDamageHeat,
			})
		}
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
	dist := vmath.MagnitudeF(dx, dy)
	if dist < 1 {
		return
	}
	dx /= dist
	dy /= dist

	// Spawn at exterior of circle ellipse with margin
	// dx,dy are already the unit direction: cos(atan2(dy,dx)) == dx, sin == dy
	spawnOffX := parameter.StormCircleRadiusX * parameter.StormRedBulletSpawnMargin * dx
	spawnOffY := parameter.StormCircleRadiusY * parameter.StormRedBulletSpawnMargin * dy

	circleCenterX, circleCenterY := vmath.Point{X: circleX, Y: circleY}.CenterF()
	originX := circleCenterX + spawnOffX
	originY := circleCenterY + spawnOffY

	// Random spread within cone half-angle
	spreadFrac := s.rng.Float64() - 0.5 // [-0.5, 0.5)
	spreadRad := spreadFrac * 2.0 * parameter.StormRedBulletSpreadHalfAngle
	bulletDirX, bulletDirY := vmath.RotateVectorF(dx, dy, spreadRad)

	velX := bulletDirX * parameter.StormRedBulletSpeed
	velY := bulletDirY * parameter.StormRedBulletSpeed

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

	angle := s.rng.Float64() * vmath.TwoPi
	distance := parameter.StormBlueSpawnDistance

	circlePX, circlePY := (vmath.Point{X: circleX, Y: circleY}).CenterF()
	target := vmath.PointAtF(
		circlePX+distance*vmath.CosF(angle),
		circlePY+distance*vmath.SinF(angle)*0.5,
	)
	targetX, targetY := target.X, target.Y

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
				X: spawn.TargetX,
				Y: spawn.TargetY,
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
		s.buffers.Observe(2, len(s.pendingBlueSpawns))
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
		for i := range component.StormCircleCount {
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
