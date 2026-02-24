package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// EyeSystem manages eye composite entity lifecycle
// Eyes are 5×3 composites that home toward an assigned target group and self-destruct on contact
type EyeSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

func NewEyeSystem(world *engine.World) engine.System {
	s := &EyeSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("eye.active")
	s.statCount = world.Resources.Status.Ints.Get("eye.count")

	s.Init()
	return s
}

func (s *EyeSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *EyeSystem) Name() string {
	return "eye"
}

func (s *EyeSystem) Priority() int {
	return parameter.PriorityEye
}

func (s *EyeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventEyeSpawnRequest,
		event.EventEyeCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *EyeSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventEyeSpawnRequest:
		if payload, ok := ev.Payload.(*event.EyeSpawnRequestPayload); ok {
			s.spawnEye(payload.X, payload.Y, payload.Type, payload.TargetGroupID)
		}

	case event.EventEyeCancelRequest:
		headerEntities := s.world.Components.Eye.GetAllEntities()
		for _, headerEntity := range headerEntities {
			s.despawnEye(headerEntity)
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorEye {
				s.despawnEye(payload.HeaderEntity)
			}
		}
	}
}

func (s *EyeSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtFixed := vmath.FromFloat(dt.Seconds())
	if dtCap := vmath.FromFloat(0.1); dtFixed > dtCap {
		dtFixed = dtCap
	}

	headerEntities := s.world.Components.Eye.GetAllEntities()
	activeCount := 0

	for _, headerEntity := range headerEntities {
		eyeComp, ok := s.world.Components.Eye.GetComponent(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// HP check → death
		if combatComp.HitPoints <= 0 {
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
					Entity:  headerEntity,
					Species: component.SpeciesEye,
					X:       headerPos.X,
					Y:       headerPos.Y,
				})
			}
			s.despawnEye(headerEntity)
			continue
		}

		// Stun: skip movement and animation
		if combatComp.StunnedRemaining > 0 {
			activeCount++
			continue
		}

		// Animation frame cycling
		s.updateAnimationFrame(&eyeComp)

		// Homing movement
		s.updateHomingMovement(headerEntity, &eyeComp, &combatComp, dtFixed)

		// Physics integration and member position sync
		s.integrateAndSync(headerEntity, dtFixed)

		// Target contact → self-destruct + combat damage
		if s.checkTargetContact(headerEntity) {
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
					Entity:  headerEntity,
					Species: component.SpeciesEye,
					X:       headerPos.X,
					Y:       headerPos.Y,
				})
			}
			combatComp.HitPoints = 0
			s.world.Components.Combat.SetComponent(headerEntity, combatComp)
			s.despawnEye(headerEntity)
			activeCount++
			continue
		}

		// Cursor/shield interaction (incidental, not target-related)
		s.handleCursorInteraction(headerEntity)

		// Persist animation state
		s.world.Components.Eye.SetComponent(headerEntity, eyeComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// === Spawn ===

func (s *EyeSystem) spawnEye(targetX, targetY int, eyeType component.EyeType, groupID uint8) {
	if int(eyeType) >= parameter.EyeTypeCount {
		return
	}

	headerX, headerY := targetX, targetY
	topLeftX := headerX - parameter.EyeHeaderOffsetX
	topLeftY := headerY - parameter.EyeHeaderOffsetY

	if s.world.Positions.HasBlockingWallInArea(
		topLeftX, topLeftY,
		parameter.EyeWidth, parameter.EyeHeight,
		component.WallBlockSpawn,
	) {
		var found bool
		topLeftX, topLeftY, found = s.world.Positions.FindFreeAreaSpiral(
			headerX, headerY,
			parameter.EyeWidth, parameter.EyeHeight,
			parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
			component.WallBlockSpawn,
			0,
		)
		if !found {
			return
		}
		headerX = topLeftX + parameter.EyeHeaderOffsetX
		headerY = topLeftY + parameter.EyeHeaderOffsetY
	}

	s.clearSpawnArea(headerX, headerY)
	headerEntity := s.createEyeComposite(headerX, headerY, eyeType, groupID)

	s.world.PushEvent(event.EventEyeSpawned, &event.EyeSpawnedPayload{
		HeaderEntity: headerEntity,
	})
}

func (s *EyeSystem) clearSpawnArea(headerX, headerY int) {
	topLeftX := headerX - parameter.EyeHeaderOffsetX
	topLeftY := headerY - parameter.EyeHeaderOffsetY

	cursorEntity := s.world.Resources.Player.Entity
	var toDestroy []core.Entity

	for row := 0; row < parameter.EyeHeight; row++ {
		for col := 0; col < parameter.EyeWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllEntityAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity {
					continue
				}
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

func (s *EyeSystem) createEyeComposite(headerX, headerY int, eyeType component.EyeType, groupID uint8) core.Entity {
	topLeftX := headerX - parameter.EyeHeaderOffsetX
	topLeftY := headerY - parameter.EyeHeaderOffsetY
	params := &parameter.EyeTypeTable[eyeType]

	// Phantom head
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: headerX, Y: headerY})

	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	s.world.Components.Eye.SetComponent(headerEntity, component.EyeComponent{
		Type:           eyeType,
		FrameIndex:     0,
		FrameRemaining: params.FrameDuration,
	})

	// Kinetic with centered position
	preciseX, preciseY := vmath.CenteredFromGrid(headerX, headerY)
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{
		Kinetic: core.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// Navigation (single consolidated write, includes path diversity defaults)
	s.world.Components.Navigation.SetComponent(headerEntity, component.NavigationComponent{
		Width:            parameter.EyeWidth,
		Height:           parameter.EyeHeight,
		FlowLookahead:    parameter.NavFlowLookaheadDefault,
		BudgetMultiplier: parameter.EyeNavBudgetMultiplierDefault,
		ExplorationBias:  parameter.EyeNavExplorationBiasDefault,
	})

	// Combat
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityEye,
		HitPoints:        params.HP,
	})

	// Target group assignment
	if groupID > 0 {
		s.world.Components.Target.SetComponent(headerEntity, component.TargetComponent{
			GroupID: groupID,
		})
	}

	// Build member entities (5×3 = 15)
	members := make([]component.MemberEntry, 0, parameter.EyeWidth*parameter.EyeHeight)

	for row := 0; row < parameter.EyeHeight; row++ {
		for col := 0; col < parameter.EyeWidth; col++ {
			memberX := topLeftX + col
			memberY := topLeftY + row
			offsetX := col - parameter.EyeHeaderOffsetX
			offsetY := row - parameter.EyeHeaderOffsetY

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
		Behavior:      component.BehaviorEye,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
	})

	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  headerEntity,
		Species: component.SpeciesEye,
	})

	return headerEntity
}

// === Movement ===

func (s *EyeSystem) updateHomingMovement(headerEntity core.Entity, eyeComp *component.EyeComponent, combatComp *component.CombatComponent, dtFixed int64) {
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Skip homing during kinetic immunity (knockback in progress)
	if combatComp.RemainingKineticImmunity > 0 {
		return
	}

	targetX, targetY, usingDirectPath := ResolveMovementTarget(s.world, headerEntity, &kineticComp)

	// Cornering drag
	var extraDrag int64
	currentSpeed := vmath.Magnitude(kineticComp.VelX, kineticComp.VelY)
	if currentSpeed > vmath.Scale {
		nx := vmath.Div(kineticComp.VelX, currentSpeed)
		ny := vmath.Div(kineticComp.VelY, currentSpeed)

		dx := targetX - kineticComp.PreciseX
		dy := targetY - kineticComp.PreciseY
		dnx, dny := vmath.Normalize2D(dx, dy)

		alignment := vmath.DotProduct(nx, ny, dnx, dny)
		if alignment < parameter.NavCorneringThreshold {
			turnSeverity := parameter.NavCorneringThreshold - alignment
			extraDrag = vmath.Mul(turnSeverity, parameter.NavCorneringBrake)
		}
	}

	homingProfile := &physics.EyeHomingProfiles[eyeComp.Type]
	physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		targetX, targetY,
		homingProfile,
		vmath.Scale,
		dtFixed,
		usingDirectPath,
	)

	if extraDrag > 0 {
		dragFactor := vmath.Scale - vmath.Mul(extraDrag, dtFixed)
		if dragFactor < 0 {
			dragFactor = 0
		}
		kineticComp.VelX = vmath.Mul(kineticComp.VelX, dragFactor)
		kineticComp.VelY = vmath.Mul(kineticComp.VelY, dragFactor)
	}

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)
}

func (s *EyeSystem) integrateAndSync(headerEntity core.Entity, dtFixed int64) {
	config := s.world.Resources.Config

	kineticComp, ok := s.world.Components.Kinetic.GetComponent(headerEntity)
	if !ok {
		return
	}

	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return
	}

	wallCheck := func(topLeftX, topLeftY int) bool {
		return s.world.Positions.HasBlockingWallInArea(
			topLeftX, topLeftY,
			parameter.EyeWidth, parameter.EyeHeight,
			component.WallBlockKinetic,
		)
	}

	minHeaderX := parameter.EyeHeaderOffsetX
	maxHeaderX := config.MapWidth - (parameter.EyeWidth - parameter.EyeHeaderOffsetX)
	minHeaderY := parameter.EyeHeaderOffsetY
	maxHeaderY := config.MapHeight - (parameter.EyeHeight - parameter.EyeHeaderOffsetY)

	newX, newY, _ := physics.IntegrateWithBounce(
		&kineticComp.Kinetic,
		dtFixed,
		parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.EyeRestitution,
		wallCheck,
	)

	s.world.Components.Kinetic.SetComponent(headerEntity, kineticComp)

	if newX != headerPos.X || newY != headerPos.Y {
		s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: newX, Y: newY})
		s.syncMemberPositions(headerEntity, newX, newY)
	}
}

func (s *EyeSystem) syncMemberPositions(headerEntity core.Entity, headerX, headerY int) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}
		s.world.Positions.SetPosition(member.Entity, component.PositionComponent{
			X: headerX + member.OffsetX,
			Y: headerY + member.OffsetY,
		})
	}
}

// === Animation ===

func (s *EyeSystem) updateAnimationFrame(eyeComp *component.EyeComponent) {
	params := &parameter.EyeTypeTable[eyeComp.Type]
	eyeComp.FrameRemaining -= s.world.Resources.Time.DeltaTime
	if eyeComp.FrameRemaining <= 0 {
		eyeComp.FrameRemaining = params.FrameDuration
		eyeComp.FrameIndex = (eyeComp.FrameIndex + 1) % params.FrameCount
	}
}

// === Target Contact ===

// checkTargetContact detects overlap between eye members and target entity members
// Emits CombatAttackDirectRequest on contact, returns true if self-destruct triggered
func (s *EyeSystem) checkTargetContact(headerEntity core.Entity) bool {
	// Resolve target group
	groupID := uint8(0)
	if tc, ok := s.world.Components.Target.GetComponent(headerEntity); ok {
		groupID = tc.GroupID
	}

	state := s.world.Resources.Target.GetGroup(groupID)
	if !state.Valid || state.Type != component.TargetEntity {
		return false
	}

	targetEntity := state.Entity
	if targetEntity == 0 || !s.world.Components.Combat.HasEntity(targetEntity) {
		return false
	}

	// Distance pre-check to avoid per-member spatial queries when far from target
	headerPos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return false
	}
	dx := headerPos.X - state.PosX
	dy := headerPos.Y - state.PosY
	if dx*dx+dy*dy > parameter.EyeContactCheckDistSq {
		return false
	}

	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return false
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}
		memberPos, ok := s.world.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		entities := s.world.Positions.GetAllEntityAt(memberPos.X, memberPos.Y)
		for _, e := range entities {
			if e == headerEntity {
				continue
			}
			// Skip own members
			if mc, ok := s.world.Components.Member.GetComponent(e); ok {
				if mc.HeaderEntity == headerEntity {
					continue
				}
			}

			resolvedTarget, hitEntity, valid := ResolveTargetFromEntity(s.world, e, headerEntity)
			if !valid || resolvedTarget != targetEntity {
				continue
			}

			// Contact confirmed — emit combat attack before self-destruct
			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   component.CombatAttackSelfDestruct,
				OwnerEntity:  headerEntity,
				OriginEntity: headerEntity,
				TargetEntity: resolvedTarget,
				HitEntity:    hitEntity,
			})
			return true
		}
	}

	return false
}

// === Interactions ===

func (s *EyeSystem) handleCursorInteraction(headerEntity core.Entity) {
	cursorEntity := s.world.Resources.Player.Entity
	overlap := CheckCursorOverlap(s.world, headerEntity)

	if len(overlap.ShieldMembers) > 0 {
		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  cursorEntity,
			OriginEntity: cursorEntity,
			TargetEntity: headerEntity,
			HitEntities:  overlap.ShieldMembers,
		})
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Value: parameter.EyeShieldDrain,
		})
	} else if overlap.OnCursor && !overlap.ShieldActive {
		s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
			Delta: -parameter.EyeDamageHeat,
		})
	}
}

// === Lifecycle ===

func (s *EyeSystem) despawnEye(headerEntity core.Entity) {
	if !s.world.Components.Eye.HasEntity(headerEntity) {
		return
	}

	s.world.PushEvent(event.EventEyeDestroyed, &event.EyeDestroyedPayload{
		HeaderEntity: headerEntity,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}