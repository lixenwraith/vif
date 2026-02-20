package system

import (
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

// SwarmSystem manages the elite enemy entity lifecycle
// Swarm is a 4x2 animated composite, spawned by fusing 2 enraged drains, that tracks cursor at 4x drain speed, charges the cursor and doesn't get deflected by shield when charging due to enrage, teleports to target location if charge LOS blocked
// Removes one heat on direct cursor collision without shield, despawns after hitpoints reach zero, uses 5 charges, or 30 second timer runs out
// Does not pause drain spawn, absorbs drains to increase its health (fused and absorbed drains are respawned by drain system)
type SwarmSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive      *atomic.Bool
	statCount       *atomic.Int64
	statPlayerKills *atomic.Int64

	enabled bool
}

// NewSwarmSystem creates a new quasar system
func NewSwarmSystem(world *engine.World) engine.System {
	s := &SwarmSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("swarm.active")
	s.statCount = world.Resources.Status.Ints.Get("swarm.count")
	s.statPlayerKills = world.Resources.Status.Ints.Get("swarm.player_kills")

	s.Init()
	return s
}

func (s *SwarmSystem) Init() {
	s.active = false
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statPlayerKills.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *SwarmSystem) Name() string {
	return "swarm"
}

func (s *SwarmSystem) Priority() int {
	return parameter.PrioritySwarm
}

func (s *SwarmSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSwarmSpawnRequest,
		event.EventSwarmCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *SwarmSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventSwarmSpawnRequest:
		if payload, ok := ev.Payload.(*event.SwarmSpawnRequestPayload); ok {
			s.spawnSwarm(payload.SpawnX, payload.SpawnY)
		}

	case event.EventSwarmCancelRequest:
		headerEntities := s.world.Components.Swarm.GetAllEntities()
		for _, headerEntity := range headerEntities {
			s.despawnSwarm(headerEntity)
		}
		s.statCount.Store(0)
		s.statActive.Store(false)

	case event.EventCompositeIntegrityBreach:
		// OOB or other mechanics that have destroyed swarm member entities
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorSwarm {
				s.despawnSwarm(payload.HeaderEntity)
			}
		}
	}
}

func (s *SwarmSystem) Update() {
	if !s.enabled {
		return
	}

	s.world.Resources.SpeciesCache.Refresh()

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	headerEntities := s.world.Components.Swarm.GetAllEntities()
	activeCount := 0

	for _, headerEntity := range headerEntities {
		swarmComp, ok := s.world.Components.Swarm.GetComponent(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}
		// Stun check: skip movement, reset state machine
		if combatComp.StunnedRemaining > 0 {
			// Reset state machine on first stunned tick
			if swarmComp.State != component.SwarmStateChase {
				s.resetSwarmState(&swarmComp)
			}
			// Animation frozen during stun - no pattern cycle update via updatePatternCycle
			s.world.Components.Swarm.SetComponent(headerEntity, swarmComp)
			continue
		}

		// HP check → player kill, despawn
		if combatComp.HitPoints <= 0 {
			// Get position for loot before destruction
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
					Species: component.SpeciesSwarm,
					X:       headerPos.X,
					Y:       headerPos.Y,
				})
			}

			// Track player damage kills
			s.statPlayerKills.Add(1)

			s.despawnSwarm(headerEntity)
			continue
		}

		// Charges check → despawn
		if swarmComp.ChargesCompleted >= parameter.SwarmMaxCharges {
			s.despawnSwarm(headerEntity)
			continue
		}

		// Pattern cycling (all states)
		s.updatePatternCycle(&swarmComp, dt)

		// State machine
		switch swarmComp.State {
		case component.SwarmStateChase:
			s.updateChaseState(headerEntity, &swarmComp, &combatComp, dtFixed, dt)
		case component.SwarmStateLock:
			s.updateLockState(headerEntity, &swarmComp, &combatComp, dt)
		case component.SwarmStateCharge:
			s.updateChargeState(headerEntity, &swarmComp, &combatComp, dtFixed, dt)
		case component.SwarmStateTeleport:
			s.updateTeleportState(headerEntity, &swarmComp, &combatComp, dt)
		case component.SwarmStateDecelerate:
			s.updateDecelerateState(headerEntity, &swarmComp, &combatComp, dtFixed, dt)
		}

		// Interactions with cursor and shield
		s.handleCursorInteractions(headerEntity)

		// Drain absorption check
		s.checkDrainAbsorption(headerEntity, &combatComp)

		// Persist components
		s.world.Components.Swarm.SetComponent(headerEntity, swarmComp)
		s.world.Components.Combat.SetComponent(headerEntity, combatComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

func (s *SwarmSystem) spawnSwarm(targetX, targetY int) {
	// Trust fuse-validated position, cheap verification only
	headerX, headerY := targetX, targetY
	topLeftX := headerX - parameter.SwarmHeaderOffsetX
	topLeftY := headerY - parameter.SwarmHeaderOffsetY

	// O(8) wall overlap check - fuse already validated, this catches edge cases
	if s.world.Positions.HasBlockingWallInArea(
		topLeftX, topLeftY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		component.WallBlockSpawn,
	) {
		// Rare: wall appeared during animation, fallback to spiral
		var found bool
		topLeftX, topLeftY, found = s.world.Positions.FindFreeAreaSpiral(
			headerX, headerY,
			parameter.SwarmWidth, parameter.SwarmHeight,
			parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
			component.WallBlockSpawn,
			0,
		)
		if !found {
			return
		}
		headerX = topLeftX + parameter.SwarmHeaderOffsetX
		headerY = topLeftY + parameter.SwarmHeaderOffsetY
	}

	// Clear area (retained as defensive measure)
	s.clearSwarmSpawnArea(headerX, headerY)

	// Create entity
	headerEntity := s.createSwarmComposite(headerX, headerY)

	// Notify world
	s.world.PushEvent(event.EventSwarmSpawned, &event.SwarmSpawnedPayload{
		HeaderEntity: headerEntity,
	})
}

// clearSwarmSpawnArea destroys entities within swarm footprint
func (s *SwarmSystem) clearSwarmSpawnArea(headerX, headerY int) {
	topLeftX := headerX - parameter.SwarmHeaderOffsetX
	topLeftY := headerY - parameter.SwarmHeaderOffsetY

	cursorEntity := s.world.Resources.Player.Entity
	var toDestroy []core.Entity

	for row := 0; row < parameter.SwarmHeight; row++ {
		for col := 0; col < parameter.SwarmWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
				// Skip walls
				if s.world.Components.Wall.HasEntity(e) {
					continue
				}
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
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, toDestroy)
	}
}

// createSwarmComposite builds the 4×2 swarm entity structure
func (s *SwarmSystem) createSwarmComposite(headerX, headerY int) core.Entity {
	topLeftX := headerX - parameter.SwarmHeaderOffsetX
	topLeftY := headerY - parameter.SwarmHeaderOffsetY

	// Create phantom head
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	// Phantom head is indestructible
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Initialize swarm component
	s.world.Components.Swarm.SetComponent(headerEntity, component.SwarmComponent{
		State:                   component.SwarmStateChase,
		PatternIndex:            0,
		PatternRemaining:        parameter.SwarmPatternDuration,
		ChargeIntervalRemaining: parameter.SwarmChargeInterval,
		ChargesCompleted:        0,
	})

	// Initialize kinetic with cell-centered position
	preciseX, preciseY := vmath.CenteredFromGrid(headerX, headerY)
	kinetic := core.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Navigation component for flow field guidance around obstacles
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:          parameter.SwarmWidth,
		Height:         parameter.SwarmHeight,
		TurnThreshold:  parameter.NavTurnThresholdDefault,
		BrakeIntensity: parameter.NavBrakeIntensityDefault,
		FlowLookahead:  parameter.NavFlowLookaheadDefault,
	})

	// Initialize combat
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntitySwarm,
		HitPoints:        parameter.CombatInitialHPSwarm,
	})

	// Lifetime timer for automatic despawn
	s.world.Components.Timer.SetComponent(headerEntity, component.TimerComponent{
		Remaining: parameter.SwarmLifetime,
	})

	// Build member entities (pre-allocate all 8 positions)
	members := make([]component.MemberEntry, 0, parameter.SwarmWidth*parameter.SwarmHeight)

	for row := 0; row < parameter.SwarmHeight; row++ {
		for col := 0; col < parameter.SwarmWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row

			offsetX := col - parameter.SwarmHeaderOffsetX
			offsetY := row - parameter.SwarmHeaderOffsetY

			entity := s.world.CreateEntity()
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
			})

			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
			})
		}
	}

	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorSwarm,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
	})

	// Emit swarm creation
	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  headerEntity,
		Species: component.SpeciesSwarm,
	})

	return headerEntity
}

// calculateFlockingSeparation returns separation acceleration from nearby swarms and quasar, only used during chase state
func (s *SwarmSystem) calculateFlockingSeparation(headerEntity core.Entity, headerX, headerY int) (sepX, sepY int64) {
	cache := s.world.Resources.SpeciesCache

	// Separation from other swarms
	for i := range cache.Swarms {
		sc := &cache.Swarms[i]
		if sc.Entity == headerEntity {
			continue
		}

		// Check if within separation ellipse
		if !vmath.EllipseContainsPoint(sc.X, sc.Y, headerX, headerY,
			parameter.SwarmSeparationInvRxSq, parameter.SwarmSeparationInvRySq) {
			continue
		}

		// Calculate separation direction: other swarm → this swarm (push away)
		dx := vmath.FromInt(headerX - sc.X)
		dy := vmath.FromInt(headerY - sc.Y)

		if dx == 0 && dy == 0 {
			dx = vmath.Scale
		}

		// Weight inversely proportional to distance
		dist := vmath.Magnitude(dx, dy)
		if dist == 0 {
			dist = 1
		}
		dirX, dirY := vmath.Normalize2D(dx, dy)

		// Closer = stronger separation
		maxDist := vmath.FromFloat(parameter.SwarmSeparationRadiusXFloat)
		weight := vmath.Div(maxDist-dist, maxDist)
		if weight < 0 {
			weight = 0
		}

		sepX += vmath.Mul(vmath.Mul(dirX, parameter.SwarmSeparationStrength), weight)
		sepY += vmath.Mul(vmath.Mul(dirY, parameter.SwarmSeparationStrength), weight)
	}

	// Separation from quasar (weighted lower)
	for i := range cache.Quasars {
		qc := &cache.Quasars[i]
		if !vmath.EllipseContainsPoint(qc.X, qc.Y, headerX, headerY,
			parameter.SwarmSeparationInvRxSq, parameter.SwarmSeparationInvRySq) {
			continue
		}

		dx := vmath.FromInt(headerX - qc.X)
		dy := vmath.FromInt(headerY - qc.Y)

		if dx == 0 && dy == 0 {
			dx = vmath.Scale
		}

		dist := vmath.Magnitude(dx, dy)
		if dist == 0 {
			dist = 1
		}
		dirX, dirY := vmath.Normalize2D(dx, dy)

		maxDist := vmath.FromFloat(parameter.SwarmSeparationRadiusXFloat)
		weight := vmath.Div(maxDist-dist, maxDist)
		if weight < 0 {
			weight = 0
		}

		// Apply quasar weight modifier
		quasarWeight := vmath.FromFloat(parameter.SwarmQuasarSeparationWeight)
		weight = vmath.Mul(weight, quasarWeight)

		sepX += vmath.Mul(vmath.Mul(dirX, parameter.SwarmSeparationStrength), weight)
		sepY += vmath.Mul(vmath.Mul(dirY, parameter.SwarmSeparationStrength), weight)
	}

	return sepX, sepY
}

// applySoftCollisions checks overlap with other combat entities and applies repulsion
func (s *SwarmSystem) applySoftCollisions(
	headerEntity core.Entity,
	kineticComp *component.KineticComponent,
	combatComp *component.CombatComponent,
	headerX, headerY int,
) {
	cache := s.world.Resources.SpeciesCache

	// Check collision with other swarms
	for i := range cache.Swarms {
		sc := &cache.Swarms[i]
		if sc.Entity == headerEntity {
			continue
		}

		radialX, radialY, hit := physics.CheckSoftCollision(
			headerX, headerY, sc.X, sc.Y,
			parameter.SwarmCollisionInvRxSq, parameter.SwarmCollisionInvRySq,
		)

		if hit {
			physics.ApplyCollision(
				&kineticComp.Kinetic,
				radialX, radialY,
				&physics.SoftCollisionSwarmToSwarm,
				s.rng,
			)
			combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
			return
		}
	}

	// Check collision with quasar
	for i := range cache.Quasars {
		qc := &cache.Quasars[i]
		radialX, radialY, hit := physics.CheckSoftCollision(
			headerX, headerY, qc.X, qc.Y,
			parameter.QuasarCollisionInvRxSq, parameter.QuasarCollisionInvRySq,
		)

		if hit {
			physics.ApplyCollision(
				&kineticComp.Kinetic,
				radialX, radialY,
				&physics.SoftCollisionSwarmToQuasar,
				s.rng,
			)
			combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
			return
		}
	}
}

// updatePatternCycle advances pattern animation
func (s *SwarmSystem) updatePatternCycle(swarmComp *component.SwarmComponent, dt time.Duration) {
	swarmComp.PatternRemaining -= dt
	if swarmComp.PatternRemaining <= 0 {
		swarmComp.PatternRemaining = parameter.SwarmPatternDuration
		swarmComp.PatternIndex = (swarmComp.PatternIndex + 1) % parameter.SwarmPatternCount
	}
}

// updateChaseState handles homing movement and charge interval countdown
func (s *SwarmSystem) updateChaseState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dtFixed int64,
	dt time.Duration,
) {
	// Not enraged during chase
	combatComp.IsEnraged = false

	// Charge interval countdown
	swarmComp.ChargeIntervalRemaining -= dt
	if swarmComp.ChargeIntervalRemaining <= 0 {
		// Transition to Lock
		s.enterLockState(headerEntity, swarmComp)
		return
	}

	// Homing movement (only if not in kinetic immunity)
	if combatComp.RemainingKineticImmunity <= 0 {
		s.applyHomingMovement(headerEntity, dtFixed)
	}

	// Integrate and sync positions
	s.integrateAndSync(headerEntity, dtFixed)
}

// updateLockState handles freeze and timer countdown
func (s *SwarmSystem) updateLockState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dt time.Duration,
) {
	// Enraged during lock (immune to kinetic)
	combatComp.IsEnraged = true

	// Timer countdown
	swarmComp.LockRemaining -= dt
	if swarmComp.LockRemaining <= 0 {
		// Transition to Charge
		s.enterChargeState(headerEntity, swarmComp)
	}
	// No movement during lock - freeze in place
}

// updateChargeState handles linear movement toward locked target
func (s *SwarmSystem) updateChargeState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dtFixed int64,
	dt time.Duration,
) {
	// Enraged during charge (immune to kinetic)
	combatComp.IsEnraged = true

	// Timer countdown
	swarmComp.ChargeRemaining -= dt
	if swarmComp.ChargeRemaining <= 0 {
		// Transition to Decelerate
		s.enterDecelerateState(swarmComp)
		return
	}

	// Linear interpolation toward target
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Calculate required velocity to reach target in remaining time
	remainingSec := swarmComp.ChargeRemaining.Seconds()
	if remainingSec <= 0 {
		remainingSec = 0.001 // Prevent division by zero
	}

	dx := swarmComp.ChargeTargetX - kineticComp.PreciseX
	dy := swarmComp.ChargeTargetY - kineticComp.PreciseY

	kineticComp.VelX = vmath.Div(dx, vmath.FromFloat(remainingSec))
	kineticComp.VelY = vmath.Div(dy, vmath.FromFloat(remainingSec))

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)

	// Integrate and sync - Check for wall impact
	hitWall := s.integrateAndSync(headerEntity, dtFixed)

	if hitWall {
		// Impact detected!
		// The physics integration has already reflected the velocity.
		// We immediately transition to decelerate to preserve this bounce
		// and prevent the charge logic from overriding it next frame.
		s.enterDecelerateState(swarmComp)
	}
}

// updateDecelerateState handles rapid stop after charge
func (s *SwarmSystem) updateDecelerateState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dtFixed int64,
	dt time.Duration,
) {
	// Remain enraged during deceleration
	combatComp.IsEnraged = true

	// Timer countdown
	swarmComp.DecelRemaining -= dt
	if swarmComp.DecelRemaining <= 0 {
		// Transition back to Chase
		swarmComp.State = component.SwarmStateChase
		swarmComp.ChargeIntervalRemaining = parameter.SwarmChargeInterval
		return
	}

	// Apply heavy drag
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Drag factor: reduce velocity by 90% per 100ms
	dragFactor := vmath.FromFloat(0.1)
	kineticComp.VelX = vmath.Mul(kineticComp.VelX, dragFactor)
	kineticComp.VelY = vmath.Mul(kineticComp.VelY, dragFactor)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)

	// Integrate and sync (minimal movement due to drag)
	s.integrateAndSync(headerEntity, dtFixed)
}

// enterLockState transitions to lock phase
func (s *SwarmSystem) enterLockState(headerEntity core.Entity, swarmComp *component.SwarmComponent) {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	swarmComp.State = component.SwarmStateLock
	swarmComp.LockRemaining = parameter.SwarmLockDuration
	swarmComp.LockedTargetX = cursorPos.X
	swarmComp.LockedTargetY = cursorPos.Y

	// Zero velocity during lock
	if kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	}
}

// enterChargeState transitions to charge phase, or teleport if LOS blocked
func (s *SwarmSystem) enterChargeState(headerEntity core.Entity, swarmComp *component.SwarmComponent) {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	// Check LOS to locked target
	hasLOS := s.world.Positions.HasLineOfSight(
		headerPos.X, headerPos.Y,
		swarmComp.LockedTargetX, swarmComp.LockedTargetY,
		component.WallBlockKinetic,
	)

	if !hasLOS {
		s.enterTeleportState(headerEntity, swarmComp, headerPos.X, headerPos.Y)
		return
	}

	// Normal charge
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	swarmComp.State = component.SwarmStateCharge
	swarmComp.ChargeRemaining = parameter.SwarmChargeDuration

	// Store charge start and target positions
	swarmComp.ChargeStartX = kineticComp.PreciseX
	swarmComp.ChargeStartY = kineticComp.PreciseY
	swarmComp.ChargeTargetX, swarmComp.ChargeTargetY = vmath.CenteredFromGrid(swarmComp.LockedTargetX, swarmComp.LockedTargetY)

	// Calculate initial charge velocity
	dx := swarmComp.ChargeTargetX - swarmComp.ChargeStartX
	dy := swarmComp.ChargeTargetY - swarmComp.ChargeStartY
	chargeSec := parameter.SwarmChargeDuration.Seconds()

	kineticComp.VelX = vmath.Div(dx, vmath.FromFloat(chargeSec))
	kineticComp.VelY = vmath.Div(dy, vmath.FromFloat(chargeSec))

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// enterTeleportState initiates teleport to locked target
func (s *SwarmSystem) enterTeleportState(headerEntity core.Entity, swarmComp *component.SwarmComponent, fromX, fromY int) {
	// Find valid landing near locked target
	targetX, targetY, found := s.world.Positions.FindFreeAreaSpiral(
		swarmComp.LockedTargetX, swarmComp.LockedTargetY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		// Fallback: skip to decelerate, counts as failed charge
		s.enterDecelerateState(swarmComp)
		return
	}

	headerTargetX := targetX + parameter.SwarmHeaderOffsetX
	headerTargetY := targetY + parameter.SwarmHeaderOffsetY

	swarmComp.State = component.SwarmStateTeleport
	swarmComp.TeleportRemaining = parameter.SwarmTeleportDuration
	swarmComp.TeleportStartX = fromX
	swarmComp.TeleportStartY = fromY
	swarmComp.TeleportTargetX = headerTargetX
	swarmComp.TeleportTargetY = headerTargetY

	// Zero velocity
	if kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	}
}

// updateTeleportState handles teleport visual duration then instant reposition
func (s *SwarmSystem) updateTeleportState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dt time.Duration,
) {
	combatComp.IsEnraged = true

	swarmComp.TeleportRemaining -= dt
	if swarmComp.TeleportRemaining > 0 {
		return
	}

	// Teleport complete
	newX := swarmComp.TeleportTargetX
	newY := swarmComp.TeleportTargetY

	s.clearSwarmSpawnArea(newX, newY)

	if kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity); ok {
		kineticComp.PreciseX, kineticComp.PreciseY = vmath.CenteredFromGrid(newX, newY)
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	}

	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
	s.syncMemberPositions(headerEntity, newX, newY)

	s.enterDecelerateState(swarmComp)
}

// enterDecelerateState transitions to deceleration phase
func (s *SwarmSystem) enterDecelerateState(swarmComp *component.SwarmComponent) {
	swarmComp.State = component.SwarmStateDecelerate
	swarmComp.DecelRemaining = parameter.SwarmDecelerationDuration
	swarmComp.ChargesCompleted++
}

// applyHomingMovement applies homing physics toward cursor
func (s *SwarmSystem) applyHomingMovement(headerEntity core.Entity, dtFixed int64) {
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Default target: cursor center
	cursorXFixed, cursorYFixed := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)
	targetX, targetY := cursorXFixed, cursorYFixed

	// Navigation: use flow field when LOS blocked
	navComp, hasNav := s.world.Components.Navigation.GetComponent(headerEntity)
	if hasNav {
		if navComp.HasDirectPath {
			// Direct LOS available
			targetX, targetY = cursorXFixed, cursorYFixed
		} else if navComp.FlowX != 0 || navComp.FlowY != 0 {
			// Blocked: follow flow field
			targetX = kineticComp.PreciseX + vmath.Mul(navComp.FlowX, navComp.FlowLookahead)
			targetY = kineticComp.PreciseY + vmath.Mul(navComp.FlowY, navComp.FlowLookahead)
		} else {
			// Flow zero (trapped): snap to cursor if very close
			distToCursor := vmath.DistanceApprox(kineticComp.PreciseX-cursorXFixed, kineticComp.PreciseY-cursorYFixed)
			if distToCursor < vmath.FromInt(2) {
				targetX, targetY = cursorXFixed, cursorYFixed
			}
		}
	}

	// Cornering drag: slow down during sharp turns
	var extraDrag int64
	if hasNav {
		currentSpeed := vmath.Magnitude(kineticComp.VelX, kineticComp.VelY)
		if currentSpeed > vmath.Scale {
			nx := vmath.Div(kineticComp.VelX, currentSpeed)
			ny := vmath.Div(kineticComp.VelY, currentSpeed)

			dx := targetX - kineticComp.PreciseX
			dy := targetY - kineticComp.PreciseY
			dnx, dny := vmath.Normalize2D(dx, dy)

			alignment := vmath.DotProduct(nx, ny, dnx, dny)

			if alignment < navComp.TurnThreshold {
				turnSeverity := navComp.TurnThreshold - alignment
				extraDrag = vmath.Mul(turnSeverity, navComp.BrakeIntensity)
			}
		}
	}

	physics.ApplyHoming(
		&kineticComp.Kinetic,
		targetX, targetY,
		&physics.SwarmHoming,
		dtFixed,
	)

	// Apply cornering drag
	if extraDrag > 0 {
		dragFactor := vmath.Scale - vmath.Mul(extraDrag, dtFixed)
		if dragFactor < 0 {
			dragFactor = 0
		}
		kineticComp.VelX = vmath.Mul(kineticComp.VelX, dragFactor)
		kineticComp.VelY = vmath.Mul(kineticComp.VelY, dragFactor)
	}

	// Apply flocking separation acceleration
	sepX, sepY := s.calculateFlockingSeparation(headerEntity, headerPos.X, headerPos.Y)
	if sepX != 0 || sepY != 0 {
		kineticComp.VelX += vmath.Mul(sepX, dtFixed)
		kineticComp.VelY += vmath.Mul(sepY, dtFixed)
	}

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// integrateAndSync integrates physics and syncs member positions, returns true if a wall/boundary was hit
func (s *SwarmSystem) integrateAndSync(headerEntity core.Entity, dtFixed int64) bool {
	config := s.world.Resources.Config

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return false
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return false
	}

	combatComp, ok := s.world.Components.Combat.GetComponent(headerEntity)
	if !ok {
		return false
	}

	// Physics Integration with Wall Constraints
	wallCheck := func(topLeftX, topLeftY int) bool {
		return s.world.Positions.HasBlockingWallInArea(
			topLeftX, topLeftY,
			parameter.SwarmWidth, parameter.SwarmHeight,
			component.WallBlockKinetic,
		)
	}

	// Bounds
	minHeaderX := parameter.SwarmHeaderOffsetX
	maxHeaderX := config.MapWidth - (parameter.SwarmWidth - parameter.SwarmHeaderOffsetX)
	minHeaderY := parameter.SwarmHeaderOffsetY
	maxHeaderY := config.MapHeight - (parameter.SwarmHeight - parameter.SwarmHeaderOffsetY)

	// TODO: magic number
	// Restitution: 0.5 (Dampens the "Super-Knockback" significantly)
	restitution := vmath.Scale / 2

	// Integrate with Bounce
	newX, newY, hitWall := physics.IntegrateWithBounce(
		&kineticComp.Kinetic,
		dtFixed,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		restitution,
		wallCheck,
	)

	// Soft collision
	s.applySoftCollisions(headerEntity, &kineticComp, &combatComp, newX, newY)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	s.world.Components.Combat.SetComponent(headerEntity, combatComp)

	// Update positions
	if newX != headerPos.X || newY != headerPos.Y {
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
		s.syncMemberPositions(headerEntity, newX, newY)
	}

	return hitWall
}

// syncMemberPositions updates all member positions relative to header
func (s *SwarmSystem) syncMemberPositions(headerEntity core.Entity, headerX, headerY int) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		memberX := headerX + member.OffsetX
		memberY := headerY + member.OffsetY

		s.world.Positions.SetPosition(member.Entity, component.PositionComponent{X: memberX, Y: memberY})
	}
}

// checkDrainAbsorption detects and absorbs colliding drains
func (s *SwarmSystem) checkDrainAbsorption(
	headerEntity core.Entity,
	combatComp *component.CombatComponent,
) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Check each active member position for drain collision
	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		memberPos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		entities := s.world.Positions.GetAllEntityAt(memberPos.X, memberPos.Y)
		for _, entity := range entities {
			if entity == 0 || entity == member.Entity || entity == headerEntity {
				continue
			}

			// Check if it's a drain
			drainComp, ok := s.world.Components.Drain.GetComponent(entity)
			if !ok {
				continue
			}
			_ = drainComp // Used for type check

			// Get drain HP before destruction
			drainCombatComp, ok := s.world.Components.Combat.GetComponent(entity)
			hpAbsorbed := 0
			if ok {
				hpAbsorbed = drainCombatComp.HitPoints
			}

			// Absorb: add HP to swarm (no cap - overheal allowed)
			combatComp.HitPoints += hpAbsorbed

			// Destroy drain silently
			event.EmitDeathOne(s.world.Resources.Event.Queue, entity, 0)

			// Emit absorption event
			s.world.PushEvent(event.EventSwarmAbsorbedDrain, &event.SwarmAbsorbedDrainPayload{
				SwarmEntity: headerEntity,
				DrainEntity: entity,
				HPAbsorbed:  hpAbsorbed,
			})
		}
	}
}

// handleCursorInteractions processes shield overlap and cursor collision
func (s *SwarmSystem) handleCursorInteractions(
	headerEntity core.Entity,
) {
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := ok && shieldComp.Active

	anyOnCursor := false
	var hitEntities []core.Entity

	// Check each active member
	for _, member := range headerComp.MemberEntries {
		memberPos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && vmath.EllipseContainsPoint(memberPos.X, memberPos.Y, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
			hitEntities = append(hitEntities, member.Entity)
		}
	}

	// Shield interaction (knockback only when not enraged)
	if len(hitEntities) > 0 {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  hitEntities,
		})

		// Energy drain
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.QuasarShieldDrain, // Same drain rate as quasar
		})
	} else if anyOnCursor && !shieldActive {
		// Direct cursor collision without shield - reduce heat
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Delta: -parameter.DrainHeatReductionAmount,
		})
	}
}

// TODO: uniform names of methods among enemy entities, check if drain system needs despawn notification for genetic system
// despawnSwarm emits events and delegates destruction to CompositeSystem
func (s *SwarmSystem) despawnSwarm(headerEntity core.Entity) {
	s.world.PushEvent(event.EventSwarmDestroyed, &event.SwarmDestroyedPayload{
		HeaderEntity: headerEntity,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

// resetSwarmState resets swarm to Chase state (called on stun detection)
func (s *SwarmSystem) resetSwarmState(swarmComp *component.SwarmComponent) {
	swarmComp.State = component.SwarmStateChase
	swarmComp.ChargeIntervalRemaining = parameter.SwarmChargeInterval
	swarmComp.LockRemaining = 0
	swarmComp.ChargeRemaining = 0
	swarmComp.DecelRemaining = 0
}