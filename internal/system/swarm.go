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

// SwarmSystem manages the elite swarm species lifecycle
// Swarm is a 4x2 animated composite, spawned by fusing 2 enraged drains, that tracks cursor at 4x drain speed, charges the cursor and doesn't get deflected by shield when charging due to enrage, teleports to target location if charge LOS blocked
// Removes one heat on direct cursor collision without shield, despawns after hitpoints reach zero, uses 5 charges, or 30 second timer runs out
// Does not pause drain spawn; drain collisions heal it through the combat event boundary
type SwarmSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive          *atomic.Bool
	statCount           *atomic.Int64
	statPlayerKills     *atomic.Int64
	statProtected       *atomic.Int64
	statProtectedPlayer *atomic.Int64
	statStalls          *atomic.Int64
	lifecycle           lifecycleTelemetry
	motion              bounceTelemetry
	sweep               cellSweep

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
	s.statProtected = world.Resources.Status.Ints.Get("swarm.protected_rejects")
	s.statProtectedPlayer = world.Resources.Status.Ints.Get("swarm.protected_player_rejects")
	s.statStalls = world.Resources.Status.Ints.Get("swarm.transition_stalls")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "swarm")
	s.motion = newBounceTelemetry(world.Resources.Status, "swarm")

	s.Init()
	return s
}

func (s *SwarmSystem) Init() {
	s.active = false
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statPlayerKills.Store(0)
	s.statProtected.Store(0)
	s.statProtectedPlayer.Store(0)
	s.statStalls.Store(0)
	s.lifecycle.Reset()
	s.motion.Reset()
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
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *SwarmSystem) HandleEvent(ev event.GameEvent) {
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
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesSwarm {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
			if s.world.ResolveCursor(payload.KillerEntity) != 0 {
				s.statPlayerKills.Add(1)
			}
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventSwarmSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventSwarmSpawnRequest:
		if payload, ok := ev.Payload.(*event.SwarmSpawnRequestPayload); ok {
			s.spawnSwarm(payload.X, payload.Y)
		}

	case event.EventSwarmCancelRequest:
		headerEntities := s.world.Components.Swarm.Entities()
		s.lifecycle.despawned.Add(int64(len(headerEntities)))
		for _, headerEntity := range headerEntities {
			s.despawnSwarm(headerEntity)
		}
		s.statCount.Store(0)
		s.statActive.Store(false)

	case event.EventCompositeIntegrityBreach:
		// OOB or other mechanics that have destroyed swarm member entities
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorSwarm {
				if s.world.Components.Swarm.HasEntity(payload.HeaderEntity) {
					s.lifecycle.despawned.Add(1)
				}
				s.despawnSwarm(payload.HeaderEntity)
			}
		}
	}
}

func (s *SwarmSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := min(dt.Seconds(), 0.1)

	swarms := s.world.Components.Swarm
	headerEntities := swarms.Entities()
	activeCount := 0

	for _, headerEntity := range headerEntities {
		swarmComp, ok := swarms.GetPtr(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetPtr(headerEntity)
		if !ok {
			continue
		}
		// Stun check: skip movement, reset state machine
		if combatComp.StunnedRemaining > 0 {
			// Reset state machine on first stunned tick
			if swarmComp.State != component.SwarmStateChase {
				s.resetSwarmState(swarmComp)
			}
			// Animation frozen during stun - no pattern cycle update via updatePatternCycle
			continue
		}

		// HP check → player kill, despawn
		if combatComp.HitPoints <= 0 {
			killX, killY := -1, -1
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				killX, killY = headerPos.X, headerPos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       headerEntity,
				KillerEntity: combatComp.LastDamagedBy,
				Species:      component.SpeciesSwarm,
				X:            killX,
				Y:            killY,
			})

			s.despawnSwarm(headerEntity)
			continue
		}

		// Charges check → despawn
		if swarmComp.ChargesCompleted >= parameter.SwarmMaxCharges {
			s.lifecycle.killedLifecycle.Add(1)
			s.despawnSwarm(headerEntity)
			continue
		}

		// Pattern cycling (all states)
		s.updatePatternCycle(swarmComp, dt)

		// State machine
		switch swarmComp.State {
		case component.SwarmStateChase:
			s.updateChaseState(headerEntity, swarmComp, combatComp, dtSec, dt)
		case component.SwarmStateLock:
			s.updateLockState(headerEntity, swarmComp, combatComp, dt)
		case component.SwarmStateCharge:
			s.updateChargeState(headerEntity, swarmComp, combatComp, dtSec, dt)
		case component.SwarmStateTeleport:
			s.updateTeleportState(headerEntity, swarmComp, combatComp, dt)
		case component.SwarmStateDecelerate:
			s.updateDecelerateState(headerEntity, swarmComp, combatComp, dtSec, dt)
		}

		// Interactions with cursor and shield
		s.handleCursorInteractions(headerEntity)

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
			s.lifecycle.spawnFailures.Add(1)
			return
		}
		headerX = topLeftX + parameter.SwarmHeaderOffsetX
		headerY = topLeftY + parameter.SwarmHeaderOffsetY
	}

	// Clear area (retained as defensive measure)
	s.clearSwarmSpawnArea(headerX, headerY)

	// createSwarmComposite publishes EventSpeciesCreated only after every
	// component and member is installed.
	s.createSwarmComposite(headerX, headerY)
	s.lifecycle.spawned.Add(1)
}

// clearSwarmSpawnArea empties the swarm footprint in both domains (D-12)
func (s *SwarmSystem) clearSwarmSpawnArea(headerX, headerY int) {
	topLeftX := headerX - parameter.SwarmHeaderOffsetX
	topLeftY := headerY - parameter.SwarmHeaderOffsetY

	s.sweep.reset()
	for row := range parameter.SwarmHeight {
		for col := range parameter.SwarmWidth {
			s.sweep.collect(s.world, topLeftX+col, topLeftY+row, func(e core.Entity) bool {
				return speciesClearable(s.world, e, s.statProtected, s.statProtectedPlayer)
			})
		}
	}
	s.sweep.emit(s.world, 0)
}

// createSwarmComposite builds the 4×2 swarm entity structure
func (s *SwarmSystem) createSwarmComposite(headerX, headerY int) core.Entity {
	topLeftX := headerX - parameter.SwarmHeaderOffsetX
	topLeftY := headerY - parameter.SwarmHeaderOffsetY

	// Create phantom head
	headerEntity := s.world.CreateEntity(core.DomainShared)
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
	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.CenterF()
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Navigation component for flow field guidance around obstacles
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:         parameter.SwarmWidth,
		Height:        parameter.SwarmHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
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

	for row := range parameter.SwarmHeight {
		for col := range parameter.SwarmWidth {
			memberX := topLeftX + col
			memberY := topLeftY + row

			offsetX := col - parameter.SwarmHeaderOffsetX
			offsetY := row - parameter.SwarmHeaderOffsetY

			entity := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(entity, component.PositionComponent{X: memberX, Y: memberY})

			s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromSpecies,
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

	// Announce the fully initialized species instance.
	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      headerEntity,
		Species:     component.SpeciesSwarm,
		X:           headerX,
		Y:           headerY,
		MemberCount: len(members),
	})

	return headerEntity
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
	dtSec float64,
	dt time.Duration,
) {
	// Not enraged during chase
	combatComp.IsEnraged = false

	// Charge interval countdown
	swarmComp.ChargeIntervalRemaining -= dt
	if swarmComp.ChargeIntervalRemaining <= 0 {
		if s.enterLockState(headerEntity, swarmComp) {
			return
		}
		// The lock could not resolve a target this tick. Re-arm the interval
		// before falling through, because an expired interval takes this branch
		// again on every following tick and the early return above skips both the
		// homing and integrateAndSync — the swarm would stop integrating for good.
		// A knockback still lands on its velocity, but nothing turns that velocity
		// into movement, so it sits exactly where it stopped while the shield
		// strikes it every tick and never ejects it. That is the swarm found
		// parked inside a shield on 2026-08-31.
		swarmComp.ChargeIntervalRemaining = parameter.SwarmTransitionRetryInterval
		s.statStalls.Add(1)
	}

	// Homing movement (only if not in kinetic immunity)
	if combatComp.RemainingKineticImmunity <= 0 {
		s.applyHomingMovement(headerEntity, dtSec)
	}

	// Integrate and sync positions
	s.integrateAndSync(headerEntity, dtSec)
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
		if !s.enterChargeState(headerEntity, swarmComp) {
			// Lock freezes the swarm in place and holds it enraged, which is what
			// makes it immune to the shield's ejection. Retrying a charge entry
			// that keeps failing would hold it there permanently. Deceleration
			// always succeeds and leads back to chase, which is a state that moves
			// and can be knocked back.
			s.enterDecelerateState(swarmComp)
			s.statStalls.Add(1)
		}
	}
	// No movement during lock - freeze in place
}

// updateChargeState handles linear movement toward locked target
func (s *SwarmSystem) updateChargeState(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
	dtSec float64,
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
	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
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

	kineticComp.VelX = dx / remainingSec
	kineticComp.VelY = dy / remainingSec

	// Integrate and sync - Check for wall impact
	hitWall := s.integrateAndSync(headerEntity, dtSec)

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
	dtSec float64,
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
	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Reduce velocity by 90% per 100ms
	physics.ScaleVelocity(&kineticComp.Kinetic, parameter.SwarmDecelDrag)

	// Integrate and sync (minimal movement due to drag)
	s.integrateAndSync(headerEntity, dtSec)
}

// enterLockState transitions to lock phase, locking current target position for
// charge. It reports whether the transition took: a caller that assumed it always
// did would leave the swarm in a state whose timer has already expired, which is
// a wedge rather than a delay.
func (s *SwarmSystem) enterLockState(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
	// Resolve base target for this swarm's group (cursor or tower etc)
	baseX, baseY, ok := resolveBaseTarget(s.world, headerEntity)
	if !ok {
		return false
	}

	swarmComp.State = component.SwarmStateLock
	swarmComp.LockRemaining = parameter.SwarmLockDuration
	swarmComp.LockedTargetX = baseX
	swarmComp.LockedTargetY = baseY

	// Zero velocity during lock
	if kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
	}
	return true
}

// enterChargeState transitions to charge phase, or teleport if LOS blocked.
// Reports whether the swarm left the lock; see enterLockState.
func (s *SwarmSystem) enterChargeState(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return false
	}

	// Check LOS to locked target
	hasLOS := s.world.Positions.HasLineOfSight(
		headerPos.X, headerPos.Y,
		swarmComp.LockedTargetX, swarmComp.LockedTargetY,
		component.WallBlockKinetic,
	)

	if !hasLOS {
		// Teleport entry is total: it either takes, or falls back to deceleration.
		s.enterTeleportState(headerEntity, swarmComp, headerPos.X, headerPos.Y)
		return true
	}

	// Normal charge
	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return false
	}

	swarmComp.State = component.SwarmStateCharge
	swarmComp.ChargeRemaining = parameter.SwarmChargeDuration

	// Store charge start and target positions
	swarmComp.ChargeStartX = kineticComp.PreciseX
	swarmComp.ChargeStartY = kineticComp.PreciseY
	swarmComp.ChargeTargetX, swarmComp.ChargeTargetY =
		vmath.Point{X: swarmComp.LockedTargetX, Y: swarmComp.LockedTargetY}.CenterF()

	// Calculate initial charge velocity
	dx := swarmComp.ChargeTargetX - swarmComp.ChargeStartX
	dy := swarmComp.ChargeTargetY - swarmComp.ChargeStartY
	chargeSec := parameter.SwarmChargeDuration.Seconds()

	kineticComp.VelX = dx / chargeSec
	kineticComp.VelY = dy / chargeSec
	return true
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
	if kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
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

	if kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity); ok {
		kineticComp.PreciseX, kineticComp.PreciseY = vmath.Point{X: newX, Y: newY}.CenterF()
		kineticComp.VelX = 0
		kineticComp.VelY = 0
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
func (s *SwarmSystem) applyHomingMovement(headerEntity core.Entity, dtSec float64) {
	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return
	}

	// Group-based target resolution + navigation routing
	// (direct path vs flow field vs stuck fallback)
	targetX, targetY, _ := ResolveMovementTarget(s.world, headerEntity, kineticComp)

	// Cornering drag
	turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
		parameter.NavCorneringThreshold, 1.0)

	physics.ApplyHoming(&kineticComp.Kinetic, targetX, targetY, &profile.SwarmHoming, dtSec)

	if turnSeverity > 0 {
		physics.ApplyLinearDrag(&kineticComp.Kinetic,
			turnSeverity*parameter.NavCorneringBrake, dtSec)
	}
}

// integrateAndSync integrates physics and syncs member positions, returns true if a wall/boundary was hit
func (s *SwarmSystem) integrateAndSync(headerEntity core.Entity, dtSec float64) bool {
	config := s.world.Resources.Config

	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
	if !ok {
		return false
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
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

	// Integrate with Bounce
	newX, newY, motion := physics.IntegrateWithBounceStats(
		&kineticComp.Kinetic,
		dtSec,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.SwarmRestitution,
		wallCheck,
	)
	s.motion.Record(motion)

	// Update positions
	if newX != headerPos.X || newY != headerPos.Y {
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
		s.syncMemberPositions(headerEntity, newX, newY)
	}

	return motion.Hit()
}

// syncMemberPositions updates all member positions relative to header
func (s *SwarmSystem) syncMemberPositions(headerEntity core.Entity, headerX, headerY int) {
	headerComp, ok := s.world.Components.Header.GetPtr(headerEntity)
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

// handleCursorInteractions processes shield overlap and cursor collision
func (s *SwarmSystem) handleCursorInteractions(
	headerEntity core.Entity,
) {
	overlaps := CheckCursorOverlaps(s.world, headerEntity)
	for i := range overlaps.Count {
		overlap := &overlaps.Entries[i]
		if !s.world.SimulatesLocally(overlap.Cursor) {
			continue
		}
		// Combat applies shield knockback and enrage immunity.
		if len(overlap.ShieldMembers) > 0 {
			s.world.PushCrossing(event.EventCombatAttackAreaCrossingRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackShield,
				OwnerEntity:  overlap.Cursor,
				OriginEntity: overlap.Cursor,
				TargetEntity: headerEntity,
				HitEntities:  overlap.ShieldMembers,
			})

			s.world.PushLocal(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Entity: overlap.Cursor,
				Value:  parameter.QuasarShieldDrain,
			})
		} else if overlap.OnCursor && !overlap.ShieldActive {
			// Direct cursor collision without a shield reduces heat.
			s.world.PushLocal(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: overlap.Cursor,
				Delta:  -parameter.DrainHeatReductionAmount,
			})
		}
	}
}

// despawnSwarm delegates destruction to CompositeSystem.
func (s *SwarmSystem) despawnSwarm(headerEntity core.Entity) {
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
