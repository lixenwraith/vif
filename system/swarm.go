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

// swarmCacheEntry holds cached swarm data for flocking and soft collision
type swarmCacheEntry struct {
	entity core.Entity
	x, y   int // Grid position of header
}

// SwarmSystem manages the elite enemy entity lifecycle
// Swarm is a 4x2 animated composite, spawned by fusing 2 enraged drains, that tracks cursor at 4x drain speed, charges the cursor and doesn't get deflected by shield when charging due to enrage
// Removes one heat on direct cursor collision without shield, despawns after hitpoints reach zero, uses 5 charges, or 30 second timer runs out
// Does not pause drain spawn, absorbs drains to increase its health (fused and absorbed drains are respawned by drain system)
type SwarmSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Per-tick cache for soft collision and flocking
	swarmCache  []swarmCacheEntry
	quasarCache []quasarCacheEntry

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

	s.swarmCache = make([]swarmCacheEntry, 0, 10)
	s.quasarCache = make([]quasarCacheEntry, 0, 1)

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

	// Cache combat entities for soft collision and flocking
	s.cacheCombatEntities()

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

// cacheCombatEntities populates caches for soft collision and flocking
func (s *SwarmSystem) cacheCombatEntities() {
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

func (s *SwarmSystem) spawnSwarm(targetX, targetY int) {
	// 1. Find valid spawn position via spiral search
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		targetX, targetY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		return // No valid position
	}

	// Header position from found top-left
	headerX := topLeftX + parameter.SwarmHeaderOffsetX
	headerY := topLeftY + parameter.SwarmHeaderOffsetY

	// 2. Clear area
	s.clearSwarmSpawnArea(headerX, headerY)

	// 3. Create Entity
	headerEntity := s.createSwarmComposite(headerX, headerY)

	// 4. Notify world
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
					if prot.Mask&component.ProtectFromDrain != 0 {
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
		Mask: component.ProtectAll,
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
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromDrain,
			})

			s.world.Components.Member.SetComponent(entity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			// Layer determined by pattern visibility (LayerGlyph = active, LayerEffect = inactive)
			layer := component.LayerGlyph
			if !parameter.SwarmPatternActive[0][row][col] {
				layer = component.LayerEffect
			}

			members = append(members, component.MemberEntry{
				Entity:  entity,
				OffsetX: offsetX,
				OffsetY: offsetY,
				Layer:   layer,
			})
		}
	}

	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorSwarm,
		MemberEntries: members,
	})

	return headerEntity
}

// calculateFlockingSeparation returns separation acceleration from nearby swarms and quasar, only used during chase state
func (s *SwarmSystem) calculateFlockingSeparation(headerEntity core.Entity, headerX, headerY int) (sepX, sepY int64) {
	// Separation from other swarms
	for _, sc := range s.swarmCache {
		if sc.entity == headerEntity {
			continue
		}

		// Check if within separation ellipse
		if !vmath.EllipseContainsPoint(sc.x, sc.y, headerX, headerY,
			parameter.SwarmSeparationInvRxSq, parameter.SwarmSeparationInvRySq) {
			continue
		}

		// Calculate separation direction: other swarm → this swarm (push away)
		dx := vmath.FromInt(headerX - sc.x)
		dy := vmath.FromInt(headerY - sc.y)

		if dx == 0 && dy == 0 {
			dx = vmath.Scale // Fallback direction
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
	for _, qc := range s.quasarCache {
		if !vmath.EllipseContainsPoint(qc.x, qc.y, headerX, headerY,
			parameter.SwarmSeparationInvRxSq, parameter.SwarmSeparationInvRySq) {
			continue
		}

		dx := vmath.FromInt(headerX - qc.x)
		dy := vmath.FromInt(headerY - qc.y)

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
	if combatComp.RemainingKineticImmunity > 0 {
		return
	}

	// Check collision with other swarms
	for _, sc := range s.swarmCache {
		if sc.entity == headerEntity {
			continue
		}

		if !vmath.EllipseContainsPoint(headerX, headerY, sc.x, sc.y,
			parameter.SwarmCollisionInvRxSq, parameter.SwarmCollisionInvRySq) {
			continue
		}

		radialX := vmath.FromInt(headerX - sc.x)
		radialY := vmath.FromInt(headerY - sc.y)
		if radialX == 0 && radialY == 0 {
			radialX = vmath.Scale
		}

		physics.ApplyCollision(
			&kineticComp.Kinetic,
			radialX, radialY,
			&physics.SoftCollisionSwarmToSwarm,
			s.rng,
		)
		combatComp.RemainingKineticImmunity = parameter.SoftCollisionImmunityDuration
		return
	}

	// Check collision with quasar
	for _, qc := range s.quasarCache {
		if !vmath.EllipseContainsPoint(headerX, headerY, qc.x, qc.y,
			parameter.QuasarCollisionInvRxSq, parameter.QuasarCollisionInvRySq) {
			continue
		}

		radialX := vmath.FromInt(headerX - qc.x)
		radialY := vmath.FromInt(headerY - qc.y)
		if radialX == 0 && radialY == 0 {
			radialX = vmath.Scale
		}

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

// enterChargeState transitions to charge phase
func (s *SwarmSystem) enterChargeState(headerEntity core.Entity, swarmComp *component.SwarmComponent) {
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	swarmComp.State = component.SwarmStateCharge
	swarmComp.ChargeRemaining = parameter.SwarmChargeDuration

	// Store charge start and target positions (target is centered)
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

	// Target cursor center
	cursorXFixed, cursorYFixed := vmath.CenteredFromGrid(cursorPos.X, cursorPos.Y)

	physics.ApplyHoming(
		&kineticComp.Kinetic,
		cursorXFixed, cursorYFixed,
		&physics.SwarmHoming,
		dtFixed,
	)

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
	maxHeaderX := config.GameWidth - (parameter.SwarmWidth - parameter.SwarmHeaderOffsetX)
	minHeaderY := parameter.SwarmHeaderOffsetY
	maxHeaderY := config.GameHeight - (parameter.SwarmHeight - parameter.SwarmHeaderOffsetY)

	// Restitution: 0.5 (Dampens the "Super-Knockback" significantly)
	restitution := int64(vmath.Scale / 2)

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
			s.world.PushEvent(event.EventSwarmAbsorbedDrain, &event.SwarmAbsorbedDrainPayload{
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
	s.world.PushEvent(event.EventSwarmDespawned, &event.SwarmDespawnedPayload{
		HeaderEntity: headerEntity,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}