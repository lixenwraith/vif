package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// SwarmSystem manages the elite enemy entity lifecycle
// Swarm is a 3x5 animated composite, spawned by materialize, that tracks cursor at 2x drain speed, charges the cursor and doesn't get deflected by shield when charging
// Removes one heat on direct cursor collision without shield, only despawns after hitpoints reach zero or by FSM, when dies spawns 2 drains
type SwarmSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewSwarmSystem creates a new quasar system
func NewSwarmSystem(world *engine.World) engine.System {
	s := &SwarmSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("swarm.active")
	s.statCount = world.Resources.Status.Ints.Get("swarm.count")

	s.Init()
	return s
}

func (s *SwarmSystem) Init() {
	s.active = false
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *SwarmSystem) Name() string {
	return "swarm"
}

func (s *SwarmSystem) Priority() int {
	return constant.PrioritySwarm
}

func (s *SwarmSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSwarmSpawned,
		event.EventSwarmCancelRequest,
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
	case event.EventSwarmSpawned:
		if payload, ok := ev.Payload.(*event.SwarmSpawnedPayload); ok {
			s.initializeSwarm(payload.HeaderEntity)
		}

	case event.EventSwarmCancelRequest:
		s.cancelAllSwarms()
	}
}

func (s *SwarmSystem) Update() {
	if !s.enabled {
		return
	}

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

		// HP check → despawn
		if combatComp.HitPoints <= 0 {
			s.despawnSwarm(headerEntity, 1) // reason: HP
			continue
		}

		// Charges check → despawn
		if swarmComp.ChargesCompleted >= constant.SwarmMaxCharges {
			s.despawnSwarm(headerEntity, 2) // reason: charges
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
		case component.SwarmStateDecelerate:
			s.updateDecelerateState(headerEntity, &swarmComp, &combatComp, dtFixed, dt)
		}

		// Interactions with cursor and shield
		s.handleCursorInteractions(headerEntity, &swarmComp, &combatComp)

		// Drain absorption check
		s.checkDrainAbsorption(headerEntity, &swarmComp, &combatComp)

		// Persist components
		s.world.Components.Swarm.SetComponent(headerEntity, swarmComp)
		s.world.Components.Combat.SetComponent(headerEntity, combatComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// initializeSwarm sets up initial kinetic state for newly spawned swarm
func (s *SwarmSystem) initializeSwarm(headerEntity core.Entity) {
	cursorEntity := s.world.Resources.Cursor.Entity

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Initial velocity toward cursor
	dx := vmath.FromInt(cursorPos.X - headerPos.X)
	dy := vmath.FromInt(cursorPos.Y - headerPos.Y)
	dirX, dirY := vmath.Normalize2D(dx, dy)

	kineticComp.VelX = vmath.Mul(dirX, constant.SwarmChaseSpeed)
	kineticComp.VelY = vmath.Mul(dirY, constant.SwarmChaseSpeed)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// updatePatternCycle advances pattern animation
func (s *SwarmSystem) updatePatternCycle(swarmComp *component.SwarmComponent, dt time.Duration) {
	swarmComp.PatternRemaining -= dt
	if swarmComp.PatternRemaining <= 0 {
		swarmComp.PatternRemaining = constant.SwarmPatternDuration
		swarmComp.PatternIndex = (swarmComp.PatternIndex + 1) % constant.SwarmPatternCount
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
		s.enterDecelerateState(headerEntity, swarmComp)
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

	// Integrate and sync
	s.integrateAndSync(headerEntity, dtFixed)
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
		s.enterChaseState(swarmComp)
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
	cursorEntity := s.world.Resources.Cursor.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	swarmComp.State = component.SwarmStateLock
	swarmComp.LockRemaining = constant.SwarmLockDuration
	swarmComp.LockedTargetX = cursorPos.X
	swarmComp.LockedTargetY = cursorPos.Y

	// Zero velocity during lock
	if kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
	}
}

// enterChargeState transitions to charge phase
func (s *SwarmSystem) enterChargeState(headerEntity core.Entity, swarmComp *component.SwarmComponent) {
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	swarmComp.State = component.SwarmStateCharge
	swarmComp.ChargeRemaining = constant.SwarmChargeDuration

	// Store charge start and target positions
	swarmComp.ChargeStartX = kineticComp.PreciseX
	swarmComp.ChargeStartY = kineticComp.PreciseY
	swarmComp.ChargeTargetX = vmath.FromInt(swarmComp.LockedTargetX)
	swarmComp.ChargeTargetY = vmath.FromInt(swarmComp.LockedTargetY)

	// Calculate initial charge velocity
	dx := swarmComp.ChargeTargetX - swarmComp.ChargeStartX
	dy := swarmComp.ChargeTargetY - swarmComp.ChargeStartY
	chargeSec := constant.SwarmChargeDuration.Seconds()

	kineticComp.VelX = vmath.Div(dx, vmath.FromFloat(chargeSec))
	kineticComp.VelY = vmath.Div(dy, vmath.FromFloat(chargeSec))

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// enterDecelerateState transitions to deceleration phase
func (s *SwarmSystem) enterDecelerateState(headerEntity core.Entity, swarmComp *component.SwarmComponent) {
	swarmComp.State = component.SwarmStateDecelerate
	swarmComp.DecelRemaining = constant.SwarmDecelerationDuration
	swarmComp.ChargesCompleted++
}

// enterChaseState transitions back to chase phase
func (s *SwarmSystem) enterChaseState(swarmComp *component.SwarmComponent) {
	swarmComp.State = component.SwarmStateChase
	swarmComp.ChargeIntervalRemaining = constant.SwarmChargeInterval
}

// applyHomingMovement applies homing physics toward cursor
func (s *SwarmSystem) applyHomingMovement(headerEntity core.Entity, dtFixed int64) {
	cursorEntity := s.world.Resources.Cursor.Entity
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	cursorXFixed := vmath.FromInt(cursorPos.X)
	cursorYFixed := vmath.FromInt(cursorPos.Y)

	physics.ApplyHoming(
		&kineticComp.Kinetic,
		cursorXFixed, cursorYFixed,
		&physics.SwarmHoming,
		dtFixed,
	)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

// integrateAndSync integrates physics and syncs member positions
func (s *SwarmSystem) integrateAndSync(headerEntity core.Entity, dtFixed int64) {
	config := s.world.Resources.Config

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	// Integrate position
	newX, newY := physics.Integrate(&kineticComp.Kinetic, dtFixed)

	// Boundary constraints (swarm footprint must stay in bounds)
	minHeaderX := constant.SwarmHeaderOffsetX
	maxHeaderX := config.GameWidth - (constant.SwarmWidth - constant.SwarmHeaderOffsetX)
	minHeaderY := constant.SwarmHeaderOffsetY
	maxHeaderY := config.GameHeight - (constant.SwarmHeight - constant.SwarmHeaderOffsetY)

	physics.ReflectBoundsX(&kineticComp.Kinetic, minHeaderX, maxHeaderX)
	physics.ReflectBoundsY(&kineticComp.Kinetic, minHeaderY, maxHeaderY)
	newX, newY = physics.GridPos(&kineticComp.Kinetic)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)

	// Update positions if changed
	if newX != headerPos.X || newY != headerPos.Y {
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
		s.syncMemberPositions(headerEntity, newX, newY)
	}
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
	swarmComp *component.SwarmComponent,
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

		// Skip inactive pattern cells
		if member.Layer != component.LayerGlyph {
			continue
		}

		memberPos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		entities := s.world.Positions.GetAllEntityAt(memberPos.X, memberPos.Y)
		for _, e := range entities {
			if e == 0 || e == member.Entity || e == headerEntity {
				continue
			}

			// Check if it's a drain
			drainComp, ok := s.world.Components.Drain.GetComponent(e)
			if !ok {
				continue
			}
			_ = drainComp // Used for type check

			// Get drain HP before destruction
			drainCombat, ok := s.world.Components.Combat.GetComponent(e)
			hpAbsorbed := 0
			if ok {
				hpAbsorbed = drainCombat.HitPoints
			}

			// Absorb: add HP to swarm (no cap - overheal allowed)
			combatComp.HitPoints += hpAbsorbed

			// Destroy drain silently
			event.EmitDeathOne(s.world.Resources.Event.Queue, e, 0)

			// Emit absorption event
			s.world.PushEvent(event.EventDrainAbsorbed, &event.DrainAbsorbedPayload{
				SwarmEntity: headerEntity,
				DrainEntity: e,
				HPAbsorbed:  hpAbsorbed,
			})
		}
	}
}

// handleCursorInteractions processes shield overlap and cursor collision
func (s *SwarmSystem) handleCursorInteractions(
	headerEntity core.Entity,
	swarmComp *component.SwarmComponent,
	combatComp *component.CombatComponent,
) {
	cursorEntity := s.world.Resources.Cursor.Entity

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
		if member.Entity == 0 || member.Layer != component.LayerGlyph {
			continue
		}

		memberPos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && s.isInsideShieldEllipse(memberPos.X, memberPos.Y, cursorPos, &shieldComp) {
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
			Value: constant.QuasarShieldDrain, // Same drain rate as quasar
		})
	} else if anyOnCursor && !shieldActive {
		// Direct cursor collision without shield - reduce heat
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Delta: -constant.DrainHeatReductionAmount,
		})
	}
}

// isInsideShieldEllipse checks if position is within shield
func (s *SwarmSystem) isInsideShieldEllipse(x, y int, cursorPos component.PositionComponent, shieldComp *component.ShieldComponent) bool {
	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)
	return vmath.EllipseContains(dx, dy, shieldComp.InvRxSq, shieldComp.InvRySq)
}

// despawnSwarm destroys swarm composite and emits event
func (s *SwarmSystem) despawnSwarm(headerEntity core.Entity, reason uint8) {
	s.world.PushEvent(event.EventSwarmDespawned, &event.SwarmDespawnedPayload{
		HeaderEntity: headerEntity,
		Reason:       reason,
	})

	s.destroySwarmComposite(headerEntity)
}

// cancelAllSwarms destroys all active swarm composites
func (s *SwarmSystem) cancelAllSwarms() {
	headerEntities := s.world.Components.Swarm.GetAllEntities()
	for _, headerEntity := range headerEntities {
		s.destroySwarmComposite(headerEntity)
	}
	s.statCount.Store(0)
	s.statActive.Store(false)
}

// destroySwarmComposite removes swarm entity structure
func (s *SwarmSystem) destroySwarmComposite(headerEntity core.Entity) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Destroy all members
	for _, m := range headerComp.MemberEntries {
		if m.Entity != 0 {
			s.world.Components.Member.RemoveEntity(m.Entity)
			s.world.DestroyEntity(m.Entity)
		}
	}

	// Remove components from phantom head
	s.world.Components.Swarm.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	s.world.Components.Combat.RemoveEntity(headerEntity)
	s.world.Components.Kinetic.RemoveEntity(headerEntity)
	s.world.Components.Timer.RemoveEntity(headerEntity)
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}