package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// EyeSystem manages eye composite entity lifecycle
// Eyes are 5×3 composites that home toward an assigned target group and self-destruct on contact
type EyeSystem struct {
	world *engine.World

	// Telemetry
	statCount     *atomic.Int64
	statProtected *atomic.Int64
	lifecycle     lifecycleTelemetry
	motion        bounceTelemetry

	enabled bool
}

func NewEyeSystem(world *engine.World) engine.System {
	s := &EyeSystem{
		world: world,
	}

	s.statCount = world.Resources.Status.Ints.Get("eye.count")
	s.statProtected = world.Resources.Status.Ints.Get("eye.protected_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "eye")
	s.motion = newBounceTelemetry(world.Resources.Status, "eye")

	s.Init()
	return s
}

func (s *EyeSystem) Init() {
	s.statCount.Store(0)
	s.statProtected.Store(0)
	s.lifecycle.Reset()
	s.motion.Reset()
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
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *EyeSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}
	if ev.Type == event.EventSpeciesKilled {
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesEye {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		// Release GA evaluations for spawn requests dropped while disabled
		if ev.Type == event.EventEyeSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
			if payload, ok := ev.Payload.(*event.EyeSpawnRequestPayload); ok {
				s.abandonEval(payload)
			}
		}
		return
	}

	switch ev.Type {
	case event.EventEyeSpawnRequest:
		if payload, ok := ev.Payload.(*event.EyeSpawnRequestPayload); ok {
			s.spawnEye(payload)
		}

	case event.EventEyeCancelRequest:
		// despawnEye removes from Eye immediately.
		headerEntities := s.world.Components.Eye.GetAllEntities()
		s.lifecycle.despawned.Add(int64(len(headerEntities)))
		for _, headerEntity := range headerEntities {
			s.despawnEye(headerEntity)
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorEye {
				if s.world.Components.Eye.HasEntity(payload.HeaderEntity) {
					s.lifecycle.despawned.Add(1)
				}
				s.despawnEye(payload.HeaderEntity)
			}
		}
	}
}

func (s *EyeSystem) Update() {
	if !s.enabled {
		return
	}

	dtSec := min(s.world.Resources.Time.DeltaTime.Seconds(), 0.1)

	// Detached copy: despawnEye may remove from the Eye store mid-iteration
	headerEntities := s.world.Components.Eye.GetAllEntities()
	activeCount := 0

	for _, headerEntity := range headerEntities {
		eyeComp, ok := s.world.Components.Eye.GetPtr(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetPtr(headerEntity)
		if !ok {
			continue
		}

		kineticComp, ok := s.world.Components.Kinetic.GetPtr(headerEntity)
		if !ok {
			continue
		}

		// HP check → death
		if combatComp.HitPoints <= 0 {
			killX, killY := -1, -1
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				killX, killY = headerPos.X, headerPos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:       headerEntity,
				KillerEntity: combatComp.LastDamagedBy,
				Species:      component.SpeciesEye,
				SubType:      uint8(eyeComp.Type),
				X:            killX,
				Y:            killY,
			})
			s.despawnEye(headerEntity)
			continue
		}

		// Stun: skip movement and animation
		if combatComp.StunnedRemaining > 0 {
			activeCount++
			continue
		}

		// Animation frame cycling
		s.updateAnimationFrame(eyeComp)

		// Homing movement
		s.updateHomingMovement(headerEntity, eyeComp, combatComp, kineticComp, dtSec)

		// Physics integration and member position sync
		s.integrateAndSync(headerEntity, kineticComp, dtSec)

		// Target contact → self-destruct + combat damage
		if s.checkTargetContact(headerEntity) {
			killX, killY := -1, -1
			if headerPos, ok := s.world.Positions.GetPosition(headerEntity); ok {
				killX, killY = headerPos.X, headerPos.Y
				s.world.PushEvent(event.EventExplosionRequest, &event.ExplosionRequestPayload{
					X:      headerPos.X,
					Y:      headerPos.Y,
					Radius: parameter.EyeSelfDestructRadius,
					Type:   event.ExplosionTypeEye,
					Attack: component.CombatAttackNone,
				})
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:  headerEntity,
				Species: component.SpeciesEye,
				SubType: uint8(eyeComp.Type),
				X:       killX,
				Y:       killY,
			})
			combatComp.HitPoints = 0
			s.despawnEye(headerEntity)
			activeCount++
			continue
		}

		// Cursor/shield interaction (incidental, not target-related)
		s.handleCursorInteraction(headerEntity)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
}

// === Spawn ===

func (s *EyeSystem) spawnEye(payload *event.EyeSpawnRequestPayload) {
	if int(payload.Type) >= parameter.EyeTypeCount {
		s.lifecycle.spawnFailures.Add(1)
		s.abandonEval(payload)
		return
	}

	headerX, headerY := payload.X, payload.Y
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
			s.lifecycle.spawnFailures.Add(1)
			s.abandonEval(payload)
			return
		}
		headerX = topLeftX + parameter.EyeHeaderOffsetX
		headerY = topLeftY + parameter.EyeHeaderOffsetY
	}

	s.clearSpawnArea(headerX, headerY)
	s.createEyeComposite(headerX, headerY, payload.Type, payload.TargetGroupID, payload.RouteGraphID, payload.RouteID, payload.EvalID, payload.Genes)
	s.lifecycle.spawned.Add(1)
}

// abandonEval releases a GA evaluation whose eye never spawned
func (s *EyeSystem) abandonEval(payload *event.EyeSpawnRequestPayload) {
	if payload.EvalID == 0 {
		return
	}
	s.world.PushEvent(event.EventGeneticAbandonEval, &event.GeneticAbandonEvalPayload{
		EvalID:  payload.EvalID,
		Species: component.SpeciesEye,
	})
}

func (s *EyeSystem) clearSpawnArea(headerX, headerY int) {
	topLeftX := headerX - parameter.EyeHeaderOffsetX
	topLeftY := headerY - parameter.EyeHeaderOffsetY

	var toDestroy []core.Entity
	var entities [parameter.MaxEntitiesPerCell]core.Entity

	for row := range parameter.EyeHeight {
		for col := range parameter.EyeWidth {
			x := topLeftX + col
			y := topLeftY + row

			count := s.world.Positions.GetAllEntitiesAtInto(x, y, entities[:])
			for i := range count {
				e := entities[i]
				if e == 0 || s.world.Components.Cursor.HasEntity(e) {
					continue
				}
				if s.world.Components.Wall.HasEntity(e) {
					continue
				}
				if prot, ok := s.world.Components.Protection.GetComponent(e); ok {
					if prot.Mask&component.ProtectFromSpecies != 0 {
						s.statProtected.Add(1)
						continue
					}
				}
				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, toDestroy...)
	}
}

func (s *EyeSystem) createEyeComposite(headerX, headerY int, eyeType component.EyeType, groupID uint8, routeGraphID uint32, routeID int, evalID uint64, genes []float64) core.Entity {
	topLeftX := headerX - parameter.EyeHeaderOffsetX
	topLeftY := headerY - parameter.EyeHeaderOffsetY
	params := &parameter.EyeTypeTable[eyeType]

	// Phantom head
	headerEntity := s.world.CreateEntity(core.DomainShared)
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
	preciseX, preciseY := vmath.Point{X: headerX, Y: headerY}.CenterF()
	s.world.Components.Kinetic.SetComponent(headerEntity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// Navigation (single consolidated write, includes route assignment)
	navComp := component.NavigationComponent{
		Width:         parameter.EyeWidth,
		Height:        parameter.EyeHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	}
	if routeID >= 0 {
		navComp.UseRouteGraph = true
		navComp.RouteGraphID = routeGraphID
		navComp.RouteID = routeID
	} else {
		navComp.RouteID = -1
	}
	s.world.Components.Navigation.SetComponent(headerEntity, navComp)

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

	for row := range parameter.EyeHeight {
		for col := range parameter.EyeWidth {
			memberX := topLeftX + col
			memberY := topLeftY + row
			offsetX := col - parameter.EyeHeaderOffsetX
			offsetY := row - parameter.EyeHeaderOffsetY

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
		Behavior:      component.BehaviorEye,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
		// EyeSystem propagates member positions on header cell change
		SkipPositionSync: true,
	})

	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		EvalID:      evalID,
		Genes:       genes,
		Entity:      headerEntity,
		Species:     component.SpeciesEye,
		SubType:     uint8(eyeType),
		X:           headerX,
		Y:           headerY,
		MemberCount: len(members),
	})

	return headerEntity
}

// === Movement ===

func (s *EyeSystem) updateHomingMovement(
	headerEntity core.Entity,
	eyeComp *component.EyeComponent,
	combatComp *component.CombatComponent,
	kineticComp *component.KineticComponent,
	dtSec float64,
) {
	// Skip homing during kinetic immunity (knockback in progress)
	if combatComp.RemainingKineticImmunity > 0 {
		return
	}

	targetX, targetY, usingDirectPath := ResolveMovementTarget(s.world, headerEntity, kineticComp)

	// Cornering drag
	turnSeverity := physics.TurnSeverity(&kineticComp.Kinetic, targetX, targetY,
		parameter.NavCorneringThreshold, 1.0)

	physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		targetX, targetY,
		&profile.EyeHomingProfiles[eyeComp.Type],
		1.0,
		dtSec,
		usingDirectPath,
	)

	if turnSeverity > 0 {
		physics.ApplyLinearDrag(&kineticComp.Kinetic,
			turnSeverity*parameter.NavCorneringBrake, dtSec)
	}
}

func (s *EyeSystem) integrateAndSync(headerEntity core.Entity, kineticComp *component.KineticComponent, dtSec float64) {
	config := s.world.Resources.Config

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

	newX, newY, motion := physics.IntegrateWithBounceStats(
		&kineticComp.Kinetic,
		dtSec,
		parameter.EyeHeaderOffsetX, parameter.EyeHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.EyeRestitution,
		wallCheck,
	)
	s.motion.Record(motion)

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

// checkTargetContact detects proximity between eye and assigned entity target
// For composite targets: triggers when any member is within self-destruct radius
// For simple targets: triggers when target entity is within self-destruct radius
// Emits CombatAttackAreaRequest with all in-radius members as HitEntities
func (s *EyeSystem) checkTargetContact(headerEntity core.Entity) bool {
	// Resolve target group
	groupID := uint8(0)
	if tc, ok := s.world.Components.Target.GetPtr(headerEntity); ok {
		groupID = tc.GroupID
	}

	state := s.world.Resources.Target.GetGroup(groupID)
	if !state.Valid || state.Count == 0 || state.Type != component.TargetEntity {
		return false
	}

	eyePos, ok := s.world.Positions.GetPosition(headerEntity)
	if !ok {
		return false
	}

	radiusSq := parameter.EyeSelfDestructRadiusSq

	for i := range state.Count {
		targetEntity := state.Targets[i].Entity
		if targetEntity == 0 || !s.world.Components.Combat.HasEntity(targetEntity) {
			continue
		}

		// Header-distance gate: avoids per-member iteration for distant targets.
		// Gate radius = target half-extent + self-destruct radius, so it never
		// rejects a member that would actually be in range
		gateSq := parameter.EyeContactCheckDistSq
		if tower, ok := s.world.Components.Tower.GetPtr(targetEntity); ok {
			r := max(tower.RadiusX, tower.RadiusY) + parameter.EyeSelfDestructRadius
			gateSq = r * r
		}
		gdx := eyePos.X - state.Targets[i].PosX
		gdy := eyePos.Y - state.Targets[i].PosY
		if gdx*gdx+gdy*gdy > gateSq {
			continue
		}

		// Composite target: check any member within explosion radius
		if targetHeader, ok := s.world.Components.Header.GetPtr(targetEntity); ok {
			var hitMembers []core.Entity
			for _, member := range targetHeader.MemberEntries {
				if member.Entity == 0 {
					continue
				}
				memberPos, ok := s.world.Positions.GetPosition(member.Entity)
				if !ok {
					continue
				}
				dx := eyePos.X - memberPos.X
				dy := eyePos.Y - memberPos.Y
				if dx*dx+dy*dy <= radiusSq {
					hitMembers = append(hitMembers, member.Entity)
				}
			}

			if len(hitMembers) > 0 {
				s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
					AttackType:   component.CombatAttackSelfDestruct,
					OwnerEntity:  headerEntity,
					OriginEntity: headerEntity,
					TargetEntity: targetEntity,
					HitEntities:  hitMembers,
					HasOrigin:    true,
					OriginX:      eyePos.X,
					OriginY:      eyePos.Y,
				})
				return true
			}
		} else {
			// Simple entity: distance check
			targetPos, ok := s.world.Positions.GetPosition(targetEntity)
			if !ok {
				continue
			}
			dx := eyePos.X - targetPos.X
			dy := eyePos.Y - targetPos.Y
			if dx*dx+dy*dy <= radiusSq {
				s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
					AttackType:   component.CombatAttackSelfDestruct,
					OwnerEntity:  headerEntity,
					OriginEntity: headerEntity,
					TargetEntity: targetEntity,
					OriginX:      eyePos.X,
					OriginY:      eyePos.Y,
				})
				return true
			}
		}
	}

	return false
}

// === Interactions ===

func (s *EyeSystem) handleCursorInteraction(headerEntity core.Entity) {
	overlaps := CheckCursorOverlaps(s.world, headerEntity)
	for i := range overlaps.Count {
		overlap := &overlaps.Entries[i]
		if len(overlap.ShieldMembers) > 0 {
			s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
				AttackType:   component.CombatAttackShield,
				OwnerEntity:  overlap.Cursor,
				OriginEntity: overlap.Cursor,
				TargetEntity: headerEntity,
				HitEntities:  overlap.ShieldMembers,
			})
			s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Entity: overlap.Cursor,
				Value:  parameter.EyeShieldDrain,
			})
		} else if overlap.OnCursor && !overlap.ShieldActive {
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: overlap.Cursor,
				Delta:  -parameter.EyeDamageHeat,
			})
		}
	}
}

// === Lifecycle ===

func (s *EyeSystem) despawnEye(headerEntity core.Entity) {
	if !s.world.Components.Eye.HasEntity(headerEntity) {
		return
	}

	// Latch: destruction is deferred to the event loop; drop the tag so a
	// re-entrant Update cannot emit SpeciesKilled twice.
	s.world.Components.Eye.RemoveEntity(headerEntity)

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}
