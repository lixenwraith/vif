package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// SwarmSystem manages the elite swarm species lifecycle
// Swarm is a 4x2 animated composite, spawned by fusing 2 enraged drains, that tracks cursor at 4x drain speed, charges the cursor and doesn't get deflected by shield when charging due to enrage, teleports to target location if charge LOS blocked
// Removes one heat on direct cursor collision without shield, despawns after hitpoints reach zero, uses its charges, or its lifetime timer runs out
// Does not pause drain spawn; drain collisions heal it through the combat event boundary
type SwarmSystem struct {
	world *engine.World

	graph *machine.Graph[component.SwarmState, *swarmCtx]
	ctx   swarmCtx // the swarm being stepped, refilled per entity

	// Telemetry
	statActive          *atomic.Bool
	statCount           *atomic.Int64
	statProtected       *atomic.Int64
	statProtectedPlayer *atomic.Int64
	statStalls          *atomic.Int64
	lifecycle           lifecycleTelemetry
	motion              bounceTelemetry
	sweep               cellSweep

	toggle
}

func NewSwarmSystem(world *engine.World) engine.System {
	s := &SwarmSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("swarm.active")
	s.statCount = world.Resources.Status.Ints.Get("swarm.count")
	s.statProtected = world.Resources.Status.Ints.Get("swarm.protected_rejects")
	s.statProtectedPlayer = world.Resources.Status.Ints.Get("swarm.protected_player_rejects")
	s.statStalls = world.Resources.Status.Ints.Get("swarm.transition_stalls")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "swarm")
	s.motion = newBounceTelemetry(world.Resources.Status, "swarm")

	// A timed chase, then a lock that ends in a charge along a clear line, a
	// teleport around a blocked one or a brake at once; a teleport arrives beside
	// whatever took its landing, or brakes where it stands with no room anywhere,
	// and the brake and the arrival go back into the chase. A stun sends an attack
	// back to the chase with the full interval and freezes the swarm.
	type edge = machine.Edge[component.SwarmState, *swarmCtx]
	type target = machine.Target[component.SwarmState, *swarmCtx]
	chase := []target{{State: component.SwarmStateChase, Duration: parameter.SwarmChargeInterval}}
	decelerate := []target{{State: component.SwarmStateDecelerate}}
	attacking := machine.MaskOf(component.SwarmStateLock, component.SwarmStateCharge,
		component.SwarmStateTeleport, component.SwarmStateArrive, component.SwarmStateDecelerate)

	s.graph = machine.Must(machine.New[component.SwarmState, *swarmCtx](s.observe).
		State(component.SwarmStateChase, machine.State[*swarmCtx]{
			Name:     "chase",
			Duration: parameter.SwarmChargeInterval,
			Tick:     s.chase,
		}).
		State(component.SwarmStateLock, machine.State[*swarmCtx]{
			Name:     "lock",
			Duration: parameter.SwarmLockDuration,
			Flags:    swarmEnraged,
			Probe:    s.probeTarget,
			Enter:    s.enterLock,
		}).
		State(component.SwarmStateCharge, machine.State[*swarmCtx]{
			Name:     "charge",
			Duration: parameter.SwarmChargeDuration,
			Flags:    swarmEnraged,
			Probe:    s.probeKinetic,
			Enter:    s.enterCharge,
			Tick:     s.charge,
		}).
		State(component.SwarmStateTeleport, machine.State[*swarmCtx]{
			Name:     "teleport",
			Duration: parameter.SwarmTeleportDuration,
			Flags:    swarmEnraged,
			Probe:    s.probeLanding,
			Enter:    s.enterTeleport,
		}).
		State(component.SwarmStateDecelerate, machine.State[*swarmCtx]{
			Name:     "decelerate",
			Duration: parameter.SwarmDecelerationDuration,
			Flags:    swarmEnraged,
			Enter:    s.enterDecelerate,
			Tick:     s.brake,
		}).
		State(component.SwarmStateArrive, machine.State[*swarmCtx]{
			Name:     "arrive",
			Duration: parameter.SwarmArriveDuration,
			Flags:    swarmEnraged,
			Probe:    s.probeArrival,
			Enter:    s.enterArrive,
		}).
		Global(edge{From: attacking, Guard: swarmStunned, To: chase}).
		Suspend(swarmStunned).
		// No target to lock delays the lock, and the chase keeps moving meanwhile
		Timeout(component.SwarmStateChase, edge{
			To:       []target{{State: component.SwarmStateLock}},
			Fallback: machine.Retry(parameter.SwarmTransitionRetryInterval),
		}).
		Timeout(component.SwarmStateLock, edge{To: []target{
			{State: component.SwarmStateCharge, Guard: s.hasLineOfSight},
			{State: component.SwarmStateTeleport},
			{State: component.SwarmStateDecelerate},
		}}).
		Timeout(component.SwarmStateCharge, edge{To: decelerate}).
		// A wall ends the charge in the step that meets it, so the bounce stands
		PostTick(component.SwarmStateCharge, edge{Guard: swarmHitWall, To: decelerate}).
		// The landing is the arrival's, so a stun that ends a teleport moves nothing,
		// and is found as the teleport ends, so rock that scrolled in meanwhile never
		// holds the swarm
		Timeout(component.SwarmStateTeleport, edge{To: []target{
			{State: component.SwarmStateArrive},
			{State: component.SwarmStateDecelerate},
		}}).
		Timeout(component.SwarmStateArrive, edge{To: chase}).
		Timeout(component.SwarmStateDecelerate, edge{To: chase}).
		Build())

	return s
}

// Swarm flags, which Update writes to the components after every step
const (
	swarmEnraged uint64 = 1 << iota // combat's enrage, which refuses kinetic knockback
)

// swarmCtx is what one swarm's step reads and its hooks write through
type swarmCtx struct {
	entity  core.Entity
	swarm   *component.SwarmComponent
	combat  *component.CombatComponent
	kinetic *component.KineticComponent // nil without one, which refuses a charge
	stunned bool
	dtSec   float64 // dt capped against tunneling

	targetX, targetY   int  // the lock probe's target cell
	landingX, landingY int  // the header cell a teleport or arrival probe found
	hitWall            bool // the charge's movement met a wall or the map edge
}

func swarmStunned(c *swarmCtx) bool { return c.stunned }
func swarmHitWall(c *swarmCtx) bool { return c.hitWall }

// observe counts the transitions a refused entry stalls: a lock with no target,
// which retries, a lock that can neither charge nor teleport, and a teleport with
// no room to arrive, which brake
func (s *SwarmSystem) observe(from, to component.SwarmState, cause machine.Cause) {
	if cause == machine.CauseRetry || (to == component.SwarmStateDecelerate &&
		(from == component.SwarmStateLock || from == component.SwarmStateTeleport)) {
		s.statStalls.Add(1)
	}
}

func (s *SwarmSystem) Init() {
	s.statActive.Store(false)
	s.statCount.Store(0)
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
	}
}

func (s *SwarmSystem) HandleEvent(ev event.GameEvent) {
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
	dtSec := min(dt.Seconds(), parameter.MaxSimulationDeltaSeconds)

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
		// Hit points ahead of the step, as every species orders it: a stunned swarm
		// still dies
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

		// Lifetime check → despawn the whole composite; a lifetime ending the header
		// alone would leave its members behind
		if timer, ok := s.world.Components.Timer.GetPtr(headerEntity); ok && timer.Expired(component.TimerDeadline) {
			s.lifecycle.killedLifecycle.Add(1)
			s.despawnSwarm(headerEntity)
			continue
		}

		kineticComp, _ := s.world.Components.Kinetic.GetPtr(headerEntity)
		s.ctx = swarmCtx{
			entity:  headerEntity,
			swarm:   swarmComp,
			combat:  combatComp,
			kinetic: kineticComp,
			stunned: combatComp.Status[component.StatusStun].Remaining > 0,
			dtSec:   dtSec,
		}

		// A stun freezes the animation as it freezes the step
		if !s.ctx.stunned {
			swarmComp.PatternElapsed += dt
			for swarmComp.PatternElapsed >= parameter.SwarmPatternDuration {
				swarmComp.PatternElapsed -= parameter.SwarmPatternDuration
				swarmComp.PatternIndex = (swarmComp.PatternIndex + 1) % parameter.SwarmPatternCount
			}
		}

		flags, suspended := s.graph.Step(&swarmComp.Machine, &s.ctx, dt)

		// The state's output, written on every step
		combatComp.IsEnraged = flags&swarmEnraged != 0

		// A stunned swarm breaks and strikes nothing, and still counts
		if !suspended {
			breakGoldUnder(s.world, &s.sweep, headerEntity)
			strikeContacts(s.world, headerEntity, profile.Contact[component.SpeciesSwarm], true)
		}
		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// spawnSwarm stands a swarm with its header at the cell, its footprint pulled inside
// the map, or on the nearest footprint free of walls a spawn is kept from
func (s *SwarmSystem) spawnSwarm(targetX, targetY int) {
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaInside(
		targetX, targetY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		s.lifecycle.spawnFailures.Add(1)
		return
	}
	headerX := topLeftX + parameter.SwarmHeaderOffsetX
	headerY := topLeftY + parameter.SwarmHeaderOffsetY

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

	s.world.Components.Swarm.SetComponent(headerEntity, component.SwarmComponent{})

	// Initialize kinetic with cell-centered position
	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.Center()
	kinetic := physics.Kinetic{
		PreciseX: preciseX,
		PreciseY: preciseY,
	}
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{Kinetic: kinetic})

	// Navigation component for flow field guidance around obstacles
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:         parameter.SwarmWidth,
		Height:        parameter.SwarmHeight,
		HeaderX:       parameter.SwarmHeaderOffsetX,
		HeaderY:       parameter.SwarmHeaderOffsetY,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})

	// Initialize combat
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntitySwarm,
		HitPoints:        parameter.CombatInitialHPSwarm,
	})

	armTimer(s.world, headerEntity, component.TimerDeadline, parameter.SwarmLifetime)

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
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
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

	// The machine starts chasing once every component a hook may read is in place
	swarmComp, _ := s.world.Components.Swarm.GetPtr(headerEntity)
	s.ctx = swarmCtx{entity: headerEntity, swarm: swarmComp}
	s.graph.Start(&swarmComp.Machine, &s.ctx, component.SwarmStateChase, parameter.SwarmChargeInterval)

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

// chase homes on the target and moves: the chase tick, which a retried lock runs
// too
func (s *SwarmSystem) chase(c *swarmCtx) {
	// A knocked-back swarm flies before it steers again
	if c.combat.RemainingKineticImmunity <= 0 {
		s.applyHomingMovement(c.entity, c.dtSec)
	}
	s.integrateAndSync(c.entity, c.dtSec)
}

// probeTarget admits a lock only on a target the swarm's group resolves
func (s *SwarmSystem) probeTarget(c *swarmCtx) bool {
	var ok bool
	c.targetX, c.targetY, ok = resolveBaseTarget(s.world, c.entity)
	return ok
}

// enterLock stores the cell the attack goes to and stops the swarm on its own
func (s *SwarmSystem) enterLock(c *swarmCtx) {
	c.swarm.LockedTargetX, c.swarm.LockedTargetY = c.targetX, c.targetY
	if c.kinetic != nil {
		c.kinetic.VelX, c.kinetic.VelY = 0, 0
	}
}

// hasLineOfSight admits a charge only along a path the body clears to the stored cell
func (s *SwarmSystem) hasLineOfSight(c *swarmCtx) bool {
	pos, ok := s.world.Positions.GetPosition(c.entity)
	return ok && bodyHasLineOfSight(s.world, pos.X, pos.Y, c.swarm.LockedTargetX, c.swarm.LockedTargetY,
		parameter.SwarmWidth, parameter.SwarmHeight, parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY)
}

// probeKinetic admits a charge only with a body to fly
func (s *SwarmSystem) probeKinetic(c *swarmCtx) bool { return c.kinetic != nil }

// enterCharge aims at the stored cell's centre, to arrive as the entry ends
func (s *SwarmSystem) enterCharge(c *swarmCtx) {
	sw, k := c.swarm, c.kinetic
	sw.ChargeStartX, sw.ChargeStartY = k.PreciseX, k.PreciseY
	sw.ChargeTargetX, sw.ChargeTargetY = vmath.Point{X: sw.LockedTargetX, Y: sw.LockedTargetY}.Center()
	sec := sw.Machine.Duration.Seconds()
	k.VelX = (sw.ChargeTargetX - sw.ChargeStartX) / sec
	k.VelY = (sw.ChargeTargetY - sw.ChargeStartY) / sec
}

// charge steers onto the stored cell over what is left of the entry and moves,
// noting a wall it meets
func (s *SwarmSystem) charge(c *swarmCtx) {
	sw, k := c.swarm, c.kinetic
	if k == nil {
		return
	}
	// Positive: an expired charge leaves on its timeout before this tick
	remaining := (sw.Machine.Duration - sw.Machine.Elapsed).Seconds()
	k.VelX = (sw.ChargeTargetX - k.PreciseX) / remaining
	k.VelY = (sw.ChargeTargetY - k.PreciseY) / remaining
	c.hitWall = s.integrateAndSync(c.entity, c.dtSec)
}

// probeLanding admits a teleport with a header to teleport from and room to land
// by the stored cell
func (s *SwarmSystem) probeLanding(c *swarmCtx) bool {
	if !s.world.Positions.HasPosition(c.entity) {
		return false
	}
	return s.findRoom(c, c.swarm.LockedTargetX, c.swarm.LockedTargetY)
}

// findRoom finds the footprint nearest the header cell (x, y), pulled inside the
// map, that holds no wall a spawn is kept from, the cell itself first, into the
// context's landing
func (s *SwarmSystem) findRoom(c *swarmCtx, x, y int) bool {
	left, top, found := s.world.Positions.FindFreeAreaInside(
		x, y,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	c.landingX, c.landingY = left+parameter.SwarmHeaderOffsetX, top+parameter.SwarmHeaderOffsetY
	return found
}

// enterTeleport marks where the teleport goes and stops the swarm; the line leaves
// from wherever the header stands as it runs
func (s *SwarmSystem) enterTeleport(c *swarmCtx) {
	c.swarm.TeleportTargetX, c.swarm.TeleportTargetY = c.landingX, c.landingY
	if c.kinetic != nil {
		c.kinetic.VelX, c.kinetic.VelY = 0, 0
	}
}

// probeArrival admits an arrival on the teleport's landing while its footprint is
// clear, else on the nearest clear one: rock scrolling in during the teleport
// takes the landing, and the arrival stands beside it
func (s *SwarmSystem) probeArrival(c *swarmCtx) bool {
	return s.findRoom(c, c.swarm.TeleportTargetX, c.swarm.TeleportTargetY)
}

// enterArrive lands a teleport that ran its course, counted as a charge: the
// footprint its probe found empties in both domains (D-12), leaving nothing, and
// the composite stands on it at rest
func (s *SwarmSystem) enterArrive(c *swarmCtx) {
	c.swarm.ChargesCompleted++
	x, y := c.landingX, c.landingY
	s.clearSwarmSpawnArea(x, y)
	if c.kinetic != nil {
		c.kinetic.PreciseX, c.kinetic.PreciseY = vmath.Point{X: x, Y: y}.Center()
		c.kinetic.VelX, c.kinetic.VelY = 0, 0
	}
	s.world.Positions.SetPosition(c.entity, component.PositionComponent{X: x, Y: y})
	s.syncMemberPositions(c.entity, x, y)
}

// enterDecelerate counts the charge however the lock ended
func (s *SwarmSystem) enterDecelerate(c *swarmCtx) { c.swarm.ChargesCompleted++ }

// brake sheds the attack's speed and moves on what is left
func (s *SwarmSystem) brake(c *swarmCtx) {
	if c.kinetic == nil {
		return
	}
	// A scale per tick, not a rate: the tick is fixed and the stop is tuned to it
	physics.ScaleVelocity(&c.kinetic.Kinetic, parameter.SwarmDecelDrag)
	s.integrateAndSync(c.entity, c.dtSec)
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
		parameter.NavCorneringBase, 1.0)

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
		return s.world.Positions.BlocksArea(
			topLeftX, topLeftY,
			parameter.SwarmWidth, parameter.SwarmHeight,
			component.WallBlockKinetic,
		)
	}

	// The header's first and last cells, the footprint inside the map
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

// despawnSwarm ends a swarm: its state's exit, then the composite, which
// CompositeSystem destroys
func (s *SwarmSystem) despawnSwarm(headerEntity core.Entity) {
	if swarmComp, ok := s.world.Components.Swarm.GetPtr(headerEntity); ok {
		s.ctx = swarmCtx{entity: headerEntity, swarm: swarmComp}
		s.graph.Stop(&swarmComp.Machine, &s.ctx)
	}
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}
