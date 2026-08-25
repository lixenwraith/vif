package system

import (
	"math"
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

// snakeLateralOffsets are the perpendicular offsets of a segment's three members
var snakeLateralOffsets = [3]int{0, -1, 1}

// SnakeSystem manages snake entity lifecycle
type SnakeSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	// Telemetry
	statActive    *atomic.Bool
	statCount     *atomic.Int64
	statProtected *atomic.Int64
	lifecycle     lifecycleTelemetry
	motion        bounceTelemetry

	enabled bool
}

func NewSnakeSystem(world *engine.World) engine.System {
	s := &SnakeSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("snake.active")
	s.statCount = world.Resources.Status.Ints.Get("snake.count")
	s.statProtected = world.Resources.Status.Ints.Get("snake.protected_rejects")
	s.lifecycle = newLifecycleTelemetry(world.Resources.Status, "snake")
	s.motion = newBounceTelemetry(world.Resources.Status, "snake")

	s.Init()
	return s
}

func (s *SnakeSystem) Init() {
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statProtected.Store(0)
	s.lifecycle.Reset()
	s.motion.Reset()
	s.enabled = true
}

func (s *SnakeSystem) Name() string {
	return "snake"
}

func (s *SnakeSystem) Priority() int {
	return parameter.PrioritySnake
}

func (s *SnakeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSnakeSpawnRequest,
		event.EventSnakeCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventSpeciesKilled,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *SnakeSystem) HandleEvent(ev event.GameEvent) {
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
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && payload.Species == component.SpeciesSnake {
			s.lifecycle.RecordKill(s.world, payload.KillerEntity)
		}
		return
	}

	if !s.enabled {
		if ev.Type == event.EventSnakeSpawnRequest {
			s.lifecycle.spawnFailures.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventSnakeSpawnRequest:
		if payload, ok := ev.Payload.(*event.SnakeSpawnRequestPayload); ok {
			s.spawnSnake(payload)
		}

	case event.EventSnakeCancelRequest:
		s.lifecycle.despawned.Add(int64(s.world.Components.Snake.CountEntities()))
		s.terminateAll()

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorSnake {
				s.handleIntegrityBreach(payload)
			}
		}
	}
}

func (s *SnakeSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	dtSec := min(dt.Seconds(), 0.1)

	snakes := s.world.Components.Snake
	snakeEntities := snakes.Entities()
	activeCount := 0

	for _, rootEntity := range snakeEntities {
		snakeComp, ok := snakes.GetPtr(rootEntity)
		if !ok {
			continue
		}

		// Validate head exists
		if !s.world.Components.Header.HasEntity(snakeComp.HeadEntity) {
			s.lifecycle.despawned.Add(1)
			s.terminateSnake(rootEntity)
			continue
		}

		headComp, ok := s.world.Components.SnakeHead.GetPtr(snakeComp.HeadEntity)
		if !ok {
			s.lifecycle.despawned.Add(1)
			s.terminateSnake(rootEntity)
			continue
		}

		// Head combat state
		// Growth can append Combat components later in this iteration, so keep the
		// head value detached until the growth phase has finished.
		headCombat, ok := s.world.Components.Combat.GetComponent(snakeComp.HeadEntity)
		if !ok {
			continue
		}

		// Check head death (unshielded and HP <= 0)
		if !snakeComp.IsShielded && headCombat.HitPoints <= 0 {
			s.handleSnakeDeath(rootEntity, snakeComp, headCombat.LastDamagedBy)
			continue
		}

		// Process spawn sequence
		if !snakeComp.SpawnComplete {
			s.processSpawnSequence(snakeComp, headComp)
		}

		// Update head movement (stun check)
		if headCombat.StunnedRemaining <= 0 {
			s.updateHeadMovement(snakeComp.HeadEntity, headComp, dtSec)
		}

		// Sync head members to header position (CompositeSystem already ran this tick)
		if headPos, ok := s.world.Positions.GetPosition(snakeComp.HeadEntity); ok {
			headHeader, ok := s.world.Components.Header.GetComponent(snakeComp.HeadEntity)
			if ok {
				for _, member := range headHeader.MemberEntries {
					if member.Entity == 0 {
						continue
					}
					newX := headPos.X + member.OffsetX
					newY := headPos.Y + member.OffsetY
					s.world.Positions.SetPosition(member.Entity, component.PositionComponent{X: newX, Y: newY})
				}
			}
		}

		// Update trail
		s.updateTrail(snakeComp.HeadEntity, headComp)

		// Process body if exists
		if snakeComp.BodyEntity != 0 && s.world.Components.Header.HasEntity(snakeComp.BodyEntity) {
			// Sync body header position to head (body header has SkipPositionSync, CompositeSystem won't update it; keep spatial queries consistent)
			if headPos, ok := s.world.Positions.GetPosition(snakeComp.HeadEntity); ok {
				s.world.Positions.SetPosition(snakeComp.BodyEntity, component.PositionComponent{
					X: headPos.X, Y: headPos.Y,
				})
			}

			bodyComp, ok := s.world.Components.SnakeBody.GetPtr(snakeComp.BodyEntity)
			if ok {
				// Resolve living members from HeaderComponent (single source of truth) and emit deaths for HP<=0 members in one pass
				resolved := s.resolveAndProcessCombat(snakeComp.BodyEntity, len(bodyComp.Segments))

				// Update segment rest positions from trail
				s.updateSegmentRestPositions(headComp, bodyComp)

				// Cascade disconnection from first dead segment
				s.checkConnectivity(snakeComp.BodyEntity, bodyComp, resolved)

				// Apply spring physics to living connected members
				s.applyBodySpringPhysics(bodyComp, headComp, resolved, dtSec)

				// Update shield state from resolved liveness
				s.updateShieldState(snakeComp, bodyComp, resolved)
			}
		} else {
			// No body: unshield head
			if snakeComp.IsShielded {
				snakeComp.IsShielded = false
			}
		}

		// Sync head immunity based on shield
		if snakeComp.IsShielded {
			headCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
			headCombat.RemainingKineticImmunity = parameter.CombatKineticImmunityDuration
		}

		// Process interactions
		s.handleInteractions(snakeComp)

		// Process growth
		s.processGrowth(snakeComp, headComp)

		s.world.Components.Combat.SetComponent(snakeComp.HeadEntity, headCombat)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// spawnSnake creates the complete snake entity structure
func (s *SnakeSystem) spawnSnake(payload *event.SnakeSpawnRequestPayload) {
	headX, headY := payload.X, payload.Y
	segmentCount := payload.SegmentCount
	if segmentCount <= 0 {
		segmentCount = parameter.SnakeDefaultSegmentCount
	}

	// Validate spawn area for head
	topLeftX := headX - parameter.SnakeHeadHeaderOffsetX
	topLeftY := headY - parameter.SnakeHeadHeaderOffsetY

	if s.world.Positions.HasBlockingWallInArea(
		topLeftX, topLeftY,
		parameter.SnakeHeadWidth, parameter.SnakeHeadHeight,
		component.WallBlockSpawn,
	) {
		var found bool
		topLeftX, topLeftY, found = s.world.Positions.FindFreeAreaSpiral(
			headX, headY,
			parameter.SnakeHeadWidth, parameter.SnakeHeadHeight,
			parameter.SnakeHeadHeaderOffsetX, parameter.SnakeHeadHeaderOffsetY,
			component.WallBlockSpawn,
			0,
		)
		if !found {
			s.lifecycle.spawnFailures.Add(1)
			return
		}
		headX = topLeftX + parameter.SnakeHeadHeaderOffsetX
		headY = topLeftY + parameter.SnakeHeadHeaderOffsetY
	}

	// Clear spawn area
	s.clearSpawnArea(headX, headY, parameter.SnakeHeadWidth, parameter.SnakeHeadHeight,
		parameter.SnakeHeadHeaderOffsetX, parameter.SnakeHeadHeaderOffsetY)

	// Create root entity (container) - does not have a position
	rootEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Components.Protection.SetComponent(rootEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Create head
	headEntity := s.createHead(rootEntity, headX, headY)

	// Create body header (empty, segments added during spawn sequence)
	bodyEntity := s.createBodyHeader(rootEntity, headX, headY)

	// Root snake component
	s.world.Components.Snake.SetComponent(rootEntity, component.SnakeComponent{
		HeadEntity:     headEntity,
		BodyEntity:     bodyEntity,
		SpawnOriginX:   headX,
		SpawnOriginY:   headY,
		SpawnRemaining: segmentCount,
		SpawnComplete:  false,
		IsShielded:     true, // Start shielded
	})

	// Root header (container type)
	s.world.Components.Header.SetComponent(rootEntity, component.HeaderComponent{
		Behavior: component.BehaviorSnake,
		Type:     component.CompositeTypeContainer,
		MemberEntries: []component.MemberEntry{
			{Entity: headEntity, OffsetX: 0, OffsetY: 0},
			{Entity: bodyEntity, OffsetX: 0, OffsetY: 0},
		},
	})

	s.world.PushEvent(event.EventSpeciesCreated, &event.SpeciesCreatedPayload{
		Entity:      rootEntity,
		Species:     component.SpeciesSnake,
		X:           headX,
		Y:           headY,
		MemberCount: 2,
	})
	s.lifecycle.spawned.Add(1)
}

func (s *SnakeSystem) createHead(rootEntity core.Entity, headX, headY int) core.Entity {
	headEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(headEntity, component.PositionComponent{X: headX, Y: headY})

	// Protected header
	s.world.Components.Protection.SetComponent(headEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Snake head component with initial trail
	headComp := component.SnakeHeadComponent{
		FacingX:    1.0, // Initial facing right
		FacingY:    0,
		LastTrailX: headX,
		LastTrailY: headY,
	}
	// Seed trail with spawn point, enough copies for initial body formation
	for i := range component.SnakeTrailCapacity {
		headComp.Trail[i] = vmath.Point{X: headX, Y: headY}
	}
	headComp.TrailHead = 0
	headComp.TrailLen = component.SnakeTrailCapacity
	s.world.Components.SnakeHead.SetComponent(headEntity, headComp)

	// Combat component
	s.world.Components.Combat.SetComponent(headEntity, component.CombatComponent{
		OwnerEntity:      rootEntity,
		CombatEntityType: component.CombatEntitySnakeHead,
		HitPoints:        parameter.CombatInitialHPSnakeHead,
	})

	// Kinetic component
	preciseX, preciseY := vmath.Point{X: headX, Y: headY}.CenterF()
	s.world.Components.Kinetic.SetComponent(headEntity, component.KineticComponent{
		Kinetic: physics.Kinetic{
			PreciseX: preciseX,
			PreciseY: preciseY,
		},
	})

	// Navigation component (same pattern as Quasar)
	s.world.Components.Navigation.SetComponent(headEntity, component.NavigationComponent{
		Width:         parameter.SnakeHeadWidth,
		Height:        parameter.SnakeHeadHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})

	// Create head members (5×3 grid)
	members := s.createHeadMembers(headEntity, headX, headY)
	s.world.Components.Header.SetComponent(headEntity, component.HeaderComponent{
		Behavior:      component.BehaviorSnake,
		Type:          component.CompositeTypeUnit,
		MemberEntries: members,
		ParentHeader:  rootEntity,
	})

	// Backlink to root
	s.world.Components.Member.SetComponent(headEntity, component.MemberComponent{
		HeaderEntity: rootEntity,
	})

	return headEntity
}

func (s *SnakeSystem) createHeadMembers(headEntity core.Entity, headX, headY int) []component.MemberEntry {
	members := make([]component.MemberEntry, 0, parameter.SnakeHeadWidth*parameter.SnakeHeadHeight)

	for row := range parameter.SnakeHeadHeight {
		for col := range parameter.SnakeHeadWidth {
			offsetX := col - parameter.SnakeHeadHeaderOffsetX
			offsetY := row - parameter.SnakeHeadHeaderOffsetY

			memberX := headX + offsetX
			memberY := headY + offsetY

			memberEntity := s.world.CreateEntity(core.DomainShared)
			s.world.Positions.SetPosition(memberEntity, component.PositionComponent{X: memberX, Y: memberY})

			s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromSpecies,
			})

			s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
				HeaderEntity: headEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  memberEntity,
				OffsetX: offsetX,
				OffsetY: offsetY,
			})
		}
	}

	return members
}

func (s *SnakeSystem) createBodyHeader(rootEntity core.Entity, headX, headY int) core.Entity {
	bodyEntity := s.world.CreateEntity(core.DomainShared)
	s.world.Positions.SetPosition(bodyEntity, component.PositionComponent{X: headX, Y: headY})

	s.world.Components.Protection.SetComponent(bodyEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	s.world.Components.SnakeBody.SetComponent(bodyEntity, component.SnakeBodyComponent{
		Segments: make([]component.SnakeSegment, 0, parameter.SnakeMaxSegments),
	})

	s.world.Components.Combat.SetComponent(bodyEntity, component.CombatComponent{
		OwnerEntity:      rootEntity,
		CombatEntityType: component.CombatEntitySnakeBody,
		HitPoints:        0,
	})

	s.world.Components.Header.SetComponent(bodyEntity, component.HeaderComponent{
		Behavior:         component.BehaviorSnake,
		Type:             component.CompositeTypeAblative,
		MemberEntries:    make([]component.MemberEntry, 0, parameter.SnakeMaxSegments*3),
		ParentHeader:     rootEntity,
		SkipPositionSync: true, // SnakeSystem owns body member positions via spring physics
	})

	s.world.Components.Member.SetComponent(bodyEntity, component.MemberComponent{
		HeaderEntity: rootEntity,
	})

	return bodyEntity
}

func (s *SnakeSystem) processSpawnSequence(snakeComp *component.SnakeComponent, headComp *component.SnakeHeadComponent) {
	if snakeComp.SpawnRemaining <= 0 {
		snakeComp.SpawnComplete = true
		return
	}

	snakeComp.SpawnTickCounter++
	if snakeComp.SpawnTickCounter < parameter.SnakeSpawnIntervalTicks {
		return
	}
	snakeComp.SpawnTickCounter = 0

	bodyComp, ok := s.world.Components.SnakeBody.GetPtr(snakeComp.BodyEntity)
	if !ok {
		snakeComp.SpawnComplete = true
		return
	}

	segmentIndex := len(bodyComp.Segments)
	totalSegments := segmentIndex + snakeComp.SpawnRemaining
	memberHP := calculateSegmentHP(segmentIndex, totalSegments)

	if !s.createBodySegmentMembers(snakeComp.BodyEntity, snakeComp.SpawnOriginX, snakeComp.SpawnOriginY, segmentIndex, headComp, memberHP) {
		snakeComp.SpawnComplete = true
		return
	}

	restX, restY := vmath.Point{X: snakeComp.SpawnOriginX, Y: snakeComp.SpawnOriginY}.CenterF()
	bodyComp.Segments = append(bodyComp.Segments, component.SnakeSegment{
		RestX:     restX,
		RestY:     restY,
		Connected: true,
	})
	snakeComp.SpawnRemaining--

	if snakeComp.SpawnRemaining <= 0 {
		snakeComp.SpawnComplete = true
	}

}

// calculateSegmentHP returns HP for segment based on position (head-adjacent = max, tail = min)
func calculateSegmentHP(segmentIndex, totalSegments int) int {
	if totalSegments <= 1 {
		return parameter.CombatInitialHPSnakeMemberMax
	}

	// Linear interpolation: index 0 = max HP, index N-1 = min HP
	t := float64(segmentIndex) / float64(totalSegments-1)
	hp := float64(parameter.CombatInitialHPSnakeMemberMax) - t*float64(parameter.CombatInitialHPSnakeMemberMax-parameter.CombatInitialHPSnakeMemberMin)
	return int(hp)
}

// createBodySegmentMembers creates member entities for a body segment and adds to header.
// Returns true if at least one member was created.
func (s *SnakeSystem) createBodySegmentMembers(bodyEntity core.Entity, centerX, centerY, segmentIndex int, headComp *component.SnakeHeadComponent, memberHP int) bool {
	bodyHeader, ok := s.world.Components.Header.GetComponent(bodyEntity)
	if !ok {
		return false
	}

	perpX, perpY := vmath.PerpendicularF(headComp.FacingX, headComp.FacingY)
	createdAny := false

	for _, lateralOffset := range snakeLateralOffsets {
		// Floor keeps negative offsets in the preceding grid cell.
		memberX := centerX + int(math.Floor(perpX*float64(lateralOffset)))
		memberY := centerY + int(math.Floor(perpY*float64(lateralOffset)))

		if s.world.Positions.HasBlockingWallAt(memberX, memberY, component.WallBlockSpawn) {
			continue
		}

		memberEntity := s.world.CreateEntity(core.DomainShared)
		s.world.Positions.SetPosition(memberEntity, component.PositionComponent{X: memberX, Y: memberY})

		s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
			Mask: component.ProtectFromDecay | component.ProtectFromSpecies,
		})

		s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
			OwnerEntity:      bodyEntity,
			CombatEntityType: component.CombatEntitySnakeBody,
			HitPoints:        memberHP,
		})

		preciseX, preciseY := vmath.Point{X: memberX, Y: memberY}.CenterF()
		s.world.Components.Kinetic.SetComponent(memberEntity, component.KineticComponent{
			Kinetic: physics.Kinetic{
				PreciseX: preciseX,
				PreciseY: preciseY,
			},
		})

		s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
			HeaderEntity: bodyEntity,
		})

		s.world.Components.SnakeMember.SetComponent(memberEntity, component.SnakeMemberComponent{
			SegmentIndex:  segmentIndex,
			LateralOffset: lateralOffset,
			MaxHitPoints:  memberHP,
		})

		bodyHeader.MemberEntries = append(bodyHeader.MemberEntries, component.MemberEntry{
			Entity:  memberEntity,
			OffsetX: 0, // Unused: SkipPositionSync
			OffsetY: 0,
		})

		createdAny = true
	}

	s.world.Components.Header.SetComponent(bodyEntity, bodyHeader)
	return createdAny
}

func (s *SnakeSystem) updateHeadMovement(headEntity core.Entity, headComp *component.SnakeHeadComponent, dtSec float64) {
	headPos, ok := s.world.Positions.GetPosition(headEntity)
	if !ok {
		return
	}

	kineticComp, ok := s.world.Components.Kinetic.GetPtr(headEntity)
	if !ok {
		return
	}

	// Group-based target resolution + navigation routing
	// (direct path vs flow field vs stuck fallback)
	targetX, targetY, usingDirectPath := ResolveMovementTarget(s.world, headEntity, kineticComp)

	// Apply homing
	physics.ApplyHomingScaled(
		&kineticComp.Kinetic,
		targetX, targetY,
		&profile.SnakeHoming,
		1.0,
		dtSec,
		usingDirectPath,
	)

	// Cap velocity
	kineticComp.VelX, kineticComp.VelY = physics.CapSpeed(kineticComp.VelX, kineticComp.VelY, parameter.SnakeMaxSpeed)

	// Update facing direction from velocity
	velMag := vmath.MagnitudeF(kineticComp.VelX, kineticComp.VelY)
	if velMag > 0.5 {
		headComp.FacingX, headComp.FacingY = vmath.Normalize2DF(kineticComp.VelX, kineticComp.VelY)
	}

	// Wall collision check
	config := s.world.Resources.Config
	wallCheck := func(topLeftX, topLeftY int) bool {
		return s.world.Positions.HasBlockingWallInArea(
			topLeftX, topLeftY,
			parameter.SnakeHeadWidth, parameter.SnakeHeadHeight,
			component.WallBlockKinetic,
		)
	}

	minHeaderX := parameter.SnakeHeadHeaderOffsetX
	maxHeaderX := config.MapWidth - (parameter.SnakeHeadWidth - parameter.SnakeHeadHeaderOffsetX)
	minHeaderY := parameter.SnakeHeadHeaderOffsetY
	maxHeaderY := config.MapHeight - (parameter.SnakeHeadHeight - parameter.SnakeHeadHeaderOffsetY)

	newX, newY, motion := physics.IntegrateWithBounceStats(
		&kineticComp.Kinetic,
		dtSec,
		parameter.SnakeHeadHeaderOffsetX, parameter.SnakeHeadHeaderOffsetY,
		minHeaderX, maxHeaderX,
		minHeaderY, maxHeaderY,
		parameter.SnakeRestitution,
		wallCheck,
	)
	s.motion.Record(motion)

	if newX != headPos.X || newY != headPos.Y {
		s.world.Positions.SetPosition(headEntity, component.PositionComponent{X: newX, Y: newY})
	}

}

func (s *SnakeSystem) updateTrail(headEntity core.Entity, headComp *component.SnakeHeadComponent) {
	headPos, ok := s.world.Positions.GetPosition(headEntity)
	if !ok {
		return
	}

	// Sub-cell threshold against a cell-integer delta: any cell change passes
	dx := float64(headPos.X - headComp.LastTrailX)
	dy := float64(headPos.Y - headComp.LastTrailY)
	if vmath.MagnitudeSqF(dx, dy) < parameter.SnakeTrailSampleIntervalSq {
		return
	}

	// Add to ring buffer
	headComp.Trail[headComp.TrailHead] = vmath.Point{X: headPos.X, Y: headPos.Y}
	headComp.TrailHead = (headComp.TrailHead + 1) % component.SnakeTrailCapacity
	if headComp.TrailLen < component.SnakeTrailCapacity {
		headComp.TrailLen++
	}

	headComp.LastTrailX = headPos.X
	headComp.LastTrailY = headPos.Y
}

func (s *SnakeSystem) updateSegmentRestPositions(headComp *component.SnakeHeadComponent, bodyComp *component.SnakeBodyComponent) {
	if headComp.TrailLen == 0 {
		return
	}

	spacingCells := int(parameter.SnakeSegmentSpacing)

	for i := range bodyComp.Segments {
		// Sample trail at position based on segment index
		trailOffset := (i + 1) * spacingCells
		if trailOffset >= headComp.TrailLen {
			trailOffset = headComp.TrailLen - 1
		}

		// Ring buffer read: TrailHead - 1 is most recent
		idx := (headComp.TrailHead - 1 - trailOffset + component.SnakeTrailCapacity) % component.SnakeTrailCapacity
		if idx < 0 {
			idx += component.SnakeTrailCapacity
		}

		pos := headComp.Trail[idx]
		bodyComp.Segments[i].RestX, bodyComp.Segments[i].RestY = pos.CenterF()
	}
}

func (s *SnakeSystem) applyBodySpringPhysics(bodyComp *component.SnakeBodyComponent, headComp *component.SnakeHeadComponent, resolved []resolvedSegment, dtSec float64) {
	perpX, perpY := vmath.PerpendicularF(headComp.FacingX, headComp.FacingY)

	for i := range bodyComp.Segments {
		seg := &bodyComp.Segments[i]
		if !seg.Connected {
			continue
		}

		members := [3]core.Entity{resolved[i].Center, resolved[i].Left, resolved[i].Right}

		for j, memberEntity := range members {
			if memberEntity == 0 {
				continue
			}

			kineticComp, ok := s.world.Components.Kinetic.GetPtr(memberEntity)
			if !ok {
				continue
			}

			// Per-member rest position with perpendicular offset
			memberRestX := seg.RestX + perpX*float64(snakeLateralOffsets[j])
			memberRestY := seg.RestY + perpY*float64(snakeLateralOffsets[j])

			// Check if member was knocked away (has kinetic immunity from combat hit)
			combatComp, hasCombat := s.world.Components.Combat.GetComponent(memberEntity)
			isDisplaced := hasCombat && combatComp.RemainingKineticImmunity > 0

			if isDisplaced {
				physics.SpringToRest(&kineticComp.Kinetic, memberRestX, memberRestY,
					parameter.SnakeSpringStiffness, parameter.SnakeSpringDamping,
					parameter.SnakeSpringMaxForce, dtSec)
				physics.IntegratePosition(&kineticComp.Kinetic, dtSec)
			} else {
				// Direct follow: snap to rest position
				kineticComp.PreciseX = memberRestX
				kineticComp.PreciseY = memberRestY
				kineticComp.VelX = 0
				kineticComp.VelY = 0
			}

			// Update grid position
			newX, newY := physics.GridPos(&kineticComp.Kinetic)

			if pos, ok := s.world.Positions.GetPosition(memberEntity); ok {
				if pos.X != newX || pos.Y != newY {
					s.world.Positions.SetPosition(memberEntity, component.PositionComponent{X: newX, Y: newY})
				}
			}

		}
	}
}

func (s *SnakeSystem) checkConnectivity(bodyEntity core.Entity, bodyComp *component.SnakeBodyComponent, resolved []resolvedSegment) {
	if len(bodyComp.Segments) == 0 {
		return
	}

	// Find first dead segment from head
	firstDeadIdx := -1
	for i := range bodyComp.Segments {
		if !bodyComp.Segments[i].Connected {
			continue
		}
		if !resolvedSegmentAlive(&resolved[i]) {
			firstDeadIdx = i
			break
		}
	}

	if firstDeadIdx == -1 {
		return
	}

	// Cascade: disconnect all segments after gap, kill their living members
	for i := firstDeadIdx + 1; i < len(bodyComp.Segments); i++ {
		if !bodyComp.Segments[i].Connected {
			continue
		}
		bodyComp.Segments[i].Connected = false

		members := [3]core.Entity{resolved[i].Center, resolved[i].Left, resolved[i].Right}
		for _, memberEntity := range members {
			if memberEntity == 0 {
				continue
			}
			s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
				HeaderEntity: bodyEntity,
				MemberEntity: memberEntity,
			})
		}

		// Zero resolved so physics skips this segment
		resolved[i] = resolvedSegment{}
	}
}

// resolveAndProcessCombat resolves living body members from HeaderComponent and emits
// deaths for HP<=0 members. Returns per-segment resolved members excluding dead.
// HeaderComponent.MemberEntries is the single source of truth for member liveness.
func (s *SnakeSystem) resolveAndProcessCombat(bodyEntity core.Entity, segmentCount int) []resolvedSegment {
	headerComp, ok := s.world.Components.Header.GetComponent(bodyEntity)
	if !ok {
		return make([]resolvedSegment, segmentCount)
	}

	resolved := make([]resolvedSegment, segmentCount)

	for _, entry := range headerComp.MemberEntries {
		if entry.Entity == 0 {
			continue
		}

		sm, ok := s.world.Components.SnakeMember.GetComponent(entry.Entity)
		if !ok {
			continue
		}
		if sm.SegmentIndex >= segmentCount {
			continue
		}

		// Combat death check
		if combatComp, ok := s.world.Components.Combat.GetComponent(entry.Entity); ok {
			if combatComp.HitPoints <= 0 {
				s.world.PushEvent(event.EventCompositeMemberDestroyed, &event.CompositeMemberDestroyedPayload{
					HeaderEntity: bodyEntity,
					MemberEntity: entry.Entity,
				})
				continue
			}
		}

		switch sm.LateralOffset {
		case 0:
			resolved[sm.SegmentIndex].Center = entry.Entity
		case -1:
			resolved[sm.SegmentIndex].Left = entry.Entity
		case 1:
			resolved[sm.SegmentIndex].Right = entry.Entity
		}
	}

	return resolved
}

func (s *SnakeSystem) updateShieldState(snakeComp *component.SnakeComponent, bodyComp *component.SnakeBodyComponent, resolved []resolvedSegment) {
	hasLiving := false
	for i := range bodyComp.Segments {
		if bodyComp.Segments[i].Connected && resolvedSegmentAlive(&resolved[i]) {
			hasLiving = true
			break
		}
	}
	snakeComp.IsShielded = hasLiving
}

func (s *SnakeSystem) handleInteractions(snakeComp *component.SnakeComponent) {
	// Apply the head interaction to every overlapping cursor.
	headOverlaps := CheckCursorOverlaps(s.world, snakeComp.HeadEntity)
	for i := range headOverlaps.Count {
		headOverlap := &headOverlaps.Entries[i]
		if !headOverlap.OnCursor {
			continue
		}
		if headOverlap.ShieldActive {
			s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
				Entity: headOverlap.Cursor,
				Value:  parameter.SnakeShieldDrainPerTick,
			})
		} else if !snakeComp.IsShielded {
			// The head damages a cursor only when the snake is unshielded.
			s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
				Entity: headOverlap.Cursor,
				Delta:  -parameter.SnakeDamageHeat,
			})
		}
	}

	// Apply the body interaction to every overlapping player shield.
	if snakeComp.BodyEntity == 0 {
		return
	}

	bodyOverlaps := CheckCursorOverlaps(s.world, snakeComp.BodyEntity)
	for i := range bodyOverlaps.Count {
		bodyOverlap := &bodyOverlaps.Entries[i]
		if len(bodyOverlap.ShieldMembers) == 0 {
			continue
		}
		s.world.PushEvent(event.EventShieldDrainRequest, &event.ShieldDrainRequestPayload{
			Entity: bodyOverlap.Cursor,
			Value:  parameter.SnakeShieldDrainPerTick,
		})

		s.world.PushEvent(event.EventCombatAttackAreaRequest, &event.CombatAttackAreaRequestPayload{
			AttackType:   component.CombatAttackShield,
			OwnerEntity:  bodyOverlap.Cursor,
			OriginEntity: bodyOverlap.Cursor,
			TargetEntity: snakeComp.BodyEntity,
			HitEntities:  bodyOverlap.ShieldMembers,
		})
	}
}

func (s *SnakeSystem) processGrowth(snakeComp *component.SnakeComponent, headComp *component.SnakeHeadComponent) {
	if headComp.GrowthPending <= 0 {
		return
	}

	bodyComp, ok := s.world.Components.SnakeBody.GetPtr(snakeComp.BodyEntity)
	if !ok {
		return
	}

	if len(bodyComp.Segments) >= parameter.SnakeMaxSegments {
		headComp.GrowthPending = 0
		return
	}

	var tailX, tailY int
	if len(bodyComp.Segments) > 0 {
		lastSeg := &bodyComp.Segments[len(bodyComp.Segments)-1]
		tail := vmath.PointAtF(lastSeg.RestX, lastSeg.RestY)
		tailX, tailY = tail.X, tail.Y
	} else {
		headPos, ok := s.world.Positions.GetPosition(snakeComp.HeadEntity)
		if !ok {
			return
		}
		tailX = headPos.X
		tailY = headPos.Y
	}

	if s.createBodySegmentMembers(snakeComp.BodyEntity, tailX, tailY, len(bodyComp.Segments), headComp, parameter.CombatInitialHPSnakeMemberMin) {
		restX, restY := vmath.Point{X: snakeComp.SpawnOriginX, Y: snakeComp.SpawnOriginY}.CenterF()
		bodyComp.Segments = append(bodyComp.Segments, component.SnakeSegment{
			RestX:     restX,
			RestY:     restY,
			Connected: true,
		})
		headComp.GrowthPending--
	}
}

func (s *SnakeSystem) handleSnakeDeath(rootEntity core.Entity, snakeComp *component.SnakeComponent, killerEntity core.Entity) {
	headPos, ok := s.world.Positions.GetPosition(snakeComp.HeadEntity)
	posX, posY := -1, -1
	if ok {
		posX, posY = headPos.X, headPos.Y
	}

	s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
		Entity:       rootEntity,
		KillerEntity: killerEntity,
		Species:      component.SpeciesSnake,
		X:            posX,
		Y:            posY,
	})

	s.terminateSnake(rootEntity)
}

func (s *SnakeSystem) handleIntegrityBreach(payload *event.CompositeIntegrityBreachPayload) {
	// Find root for this header
	rootEntities := s.world.Components.Snake.Entities()
	for _, rootEntity := range rootEntities {
		snakeComp, ok := s.world.Components.Snake.GetPtr(rootEntity)
		if !ok {
			continue
		}

		if payload.HeaderEntity == snakeComp.HeadEntity || payload.HeaderEntity == snakeComp.BodyEntity {
			// Body breach: update shield state
			if payload.HeaderEntity == snakeComp.BodyEntity && payload.RemainingCount == 0 {
				snakeComp.IsShielded = false
			}
			return
		}
	}
}

func (s *SnakeSystem) terminateSnake(rootEntity core.Entity) {
	snakeComp, ok := s.world.Components.Snake.GetComponent(rootEntity)
	if !ok {
		return
	}

	// Destroy body composite
	if snakeComp.BodyEntity != 0 {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
			HeaderEntity: snakeComp.BodyEntity,
			Effect:       event.EventFlashSpawnOneRequest,
		})
	}

	// Destroy head composite
	if snakeComp.HeadEntity != 0 {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
			HeaderEntity: snakeComp.HeadEntity,
			Effect:       event.EventFlashSpawnOneRequest,
		})
	}

	// Destroy root
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: rootEntity,
		Effect:       0,
	})
}

func (s *SnakeSystem) terminateAll() {
	for _, entity := range s.world.Components.Snake.Entities() {
		s.terminateSnake(entity)
	}
}

func (s *SnakeSystem) clearSpawnArea(centerX, centerY, width, height, offsetX, offsetY int) {
	topLeftX := centerX - offsetX
	topLeftY := centerY - offsetY

	var toDestroy []core.Entity
	var entities [parameter.MaxEntitiesPerCell]core.Entity

	for row := range height {
		for col := range width {
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

// resolvedSegment holds per-segment member entities resolved from HeaderComponent
type resolvedSegment struct {
	Center core.Entity
	Left   core.Entity
	Right  core.Entity
}

func resolvedSegmentAlive(r *resolvedSegment) bool {
	return r.Center != 0 || r.Left != 0 || r.Right != 0
}
