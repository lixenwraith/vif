package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// TowerSystem manages tower entity lifecycle
// Tower is a stationary ablative composite owned by the player
// Blocks cursor movement via WallBlockCursor on members
// Attacked by eye species via proximity self-destruct
type TowerSystem struct {
	world *engine.World

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

func NewTowerSystem(world *engine.World) engine.System {
	s := &TowerSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("tower.active")
	s.statCount = world.Resources.Status.Ints.Get("tower.count")

	s.Init()
	return s
}

func (s *TowerSystem) Init() {
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *TowerSystem) Name() string {
	return "tower"
}

func (s *TowerSystem) Priority() int {
	return parameter.PriorityTower
}

func (s *TowerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventTowerSpawnRequest,
		event.EventTowerCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *TowerSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventTowerSpawnRequest:
		if payload, ok := ev.Payload.(*event.TowerSpawnRequestPayload); ok {
			s.spawnTower(payload)
		}

	case event.EventTowerCancelRequest:
		s.terminateAll()

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorTower && payload.RemainingCount == 0 {
				s.handleTowerDeath(payload.HeaderEntity)
			}
		}
	}
}

func (s *TowerSystem) Update() {
	if !s.enabled {
		return
	}

	towerEntities := s.world.Components.Tower.GetAllEntities()
	if len(towerEntities) == 0 {
		s.statActive.Store(false)
		s.statCount.Store(0)
		return
	}

	activeCount := 0

	for _, headerEntity := range towerEntities {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Ablative combat: detect member HP<=0 and route deaths
		s.processAblativeCombat(headerEntity, &headerComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

// === Spawn ===

func (s *TowerSystem) spawnTower(payload *event.TowerSpawnRequestPayload) {
	radiusX := payload.RadiusX
	radiusY := payload.RadiusY
	if radiusX <= 0 {
		radiusX = parameter.TowerDefaultRadiusX
	}
	if radiusY <= 0 {
		radiusY = parameter.TowerDefaultRadiusY
	}

	minHP := payload.MinHP
	maxHP := payload.MaxHP
	if minHP <= 0 {
		minHP = parameter.CombatInitialHPTowerMin
	}
	if maxHP <= 0 {
		maxHP = parameter.CombatInitialHPTowerMax
	}
	if minHP > maxHP {
		minHP = maxHP
	}

	var centerX, centerY int

	if payload.X == 0 && payload.Y == 0 {
		var ok bool
		centerX, centerY, ok = s.findTowerPosition(radiusX, radiusY)
		if !ok {
			s.world.PushEvent(event.EventTowerSpawnFailed, nil)
			return
		}
	} else {
		centerX, centerY = payload.X, payload.Y
		if !s.validateTowerPosition(centerX, centerY, radiusX, radiusY) {
			s.world.PushEvent(event.EventTowerSpawnFailed, nil)
			return
		}
	}

	cursorEntity := s.world.Resources.Player.Entity

	// Create header entity
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: centerX, Y: centerY})

	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	s.world.Components.Tower.SetComponent(headerEntity, component.TowerComponent{
		SpawnX:  centerX,
		SpawnY:  centerY,
		RadiusX: radiusX,
		RadiusY: radiusY,
		MinHP:   minHP,
		MaxHP:   maxHP,
	})

	s.world.Components.Target.SetComponent(headerEntity, component.TargetComponent{
		GroupID: payload.TargetGroupID,
	})

	// Ablative header: HP=0, damage routes to members
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      cursorEntity,
		CombatEntityType: component.CombatEntityTower,
		HitPoints:        0,
	})

	// Generate disc members
	members := s.createDiscMembers(headerEntity, cursorEntity, centerX, centerY, radiusX, radiusY, minHP, maxHP)

	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorTower,
		Type:          component.CompositeTypeAblative,
		MemberEntries: members,
	})

	if payload.TargetGroupID > 0 {
		s.world.Components.TargetAnchor.SetComponent(headerEntity, component.TargetAnchorComponent{
			GroupID: payload.TargetGroupID,
		})
	}

	s.world.PushEvent(event.EventTowerSpawned, &event.TowerSpawnedPayload{
		HeaderEntity: headerEntity,
		MemberCount:  len(members),
		X:            centerX,
		Y:            centerY,
	})
}

func (s *TowerSystem) validateTowerPosition(centerX, centerY, radiusX, radiusY int) bool {
	topLeftX := centerX - radiusX
	topLeftY := centerY - radiusY
	width := 2*radiusX + 1
	height := 2*radiusY + 1

	return s.world.Positions.IsAreaFree(topLeftX, topLeftY, width, height, component.WallBlockSpawn)
}

func (s *TowerSystem) findTowerPosition(radiusX, radiusY int) (int, int, bool) {
	config := s.world.Resources.Config
	width := 2*radiusX + 1
	height := 2*radiusY + 1

	rng := vmath.NewFastRand(uint64(s.world.Resources.Time.GameTime.UnixNano()))

	minCX := radiusX
	maxCX := config.MapWidth - radiusX - 1
	minCY := radiusY
	maxCY := config.MapHeight - radiusY - 1

	if maxCX < minCX || maxCY < minCY {
		return 0, 0, false
	}

	rangeX := maxCX - minCX + 1
	rangeY := maxCY - minCY + 1

	lastCX, lastCY := config.MapWidth/2, config.MapHeight/2

	for attempt := 0; attempt < parameter.TowerSpawnMaxAttempts; attempt++ {
		cx := minCX + rng.Intn(rangeX)
		cy := minCY + rng.Intn(rangeY)
		lastCX, lastCY = cx, cy

		if s.validateTowerPosition(cx, cy, radiusX, radiusY) {
			return cx, cy, true
		}
	}

	// Spiral fallback from last attempt
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		lastCX, lastCY,
		width, height,
		radiusX, radiusY,
		component.WallBlockSpawn,
		parameter.TowerSpawnSpiralMaxRadius,
	)
	if found {
		return topLeftX + radiusX, topLeftY + radiusY, true
	}

	return 0, 0, false
}

func (s *TowerSystem) createDiscMembers(
	headerEntity, cursorEntity core.Entity,
	centerX, centerY, radiusX, radiusY, minHP, maxHP int,
) []component.MemberEntry {
	var members []component.MemberEntry

	rxFixed := vmath.FromInt(radiusX)
	ryFixed := vmath.FromInt(radiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rxFixed, ryFixed)

	hpRange := maxHP - minHP

	for dy := -radiusY; dy <= radiusY; dy++ {
		for dx := -radiusX; dx <= radiusX; dx++ {
			dxFixed := vmath.FromInt(dx)
			dyFixed := vmath.FromInt(dy)
			if !vmath.EllipseContains(dxFixed, dyFixed, invRxSq, invRySq) {
				continue
			}

			var hp int
			if hpRange > 0 {
				normDistSq := vmath.EllipseDistSq(dxFixed, dyFixed, invRxSq, invRySq)
				normDist := vmath.ToFloat(vmath.Sqrt(normDistSq))
				if normDist > 1.0 {
					normDist = 1.0
				}
				hp = maxHP - int(float64(hpRange)*normDist)
			} else {
				hp = maxHP
			}
			if hp < minHP {
				hp = minHP
			}

			memberX := centerX + dx
			memberY := centerY + dy

			memberEntity := s.world.CreateEntity()
			s.world.Positions.SetPosition(memberEntity, component.PositionComponent{
				X: memberX,
				Y: memberY,
			})

			// Wall: blocks cursor movement and entity spawning
			s.world.Components.Wall.SetComponent(memberEntity, component.WallComponent{
				BlockMask: component.WallBlockCursor | component.WallBlockSpawn,
			})

			s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
			})

			// Per-member ablative combat, owned by player
			s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
				OwnerEntity:      cursorEntity,
				CombatEntityType: component.CombatEntityTower,
				HitPoints:        hp,
			})

			s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
				HeaderEntity: headerEntity,
			})

			members = append(members, component.MemberEntry{
				Entity:  memberEntity,
				OffsetX: dx,
				OffsetY: dy,
			})
		}
	}

	return members
}

// === Ablative Combat ===

func (s *TowerSystem) processAblativeCombat(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	var deadMembers []core.Entity
	livingCount := 0

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(member.Entity)
		if !ok {
			continue
		}

		if combatComp.HitPoints <= 0 {
			deadMembers = append(deadMembers, member.Entity)
		} else {
			livingCount++
		}
	}

	for _, memberEntity := range deadMembers {
		// Remove wall component before death routing so spatial queries reflect cleared cells
		s.world.Components.Wall.RemoveEntity(memberEntity)

		s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
			HeaderEntity: headerEntity,
			MemberEntity: memberEntity,
		})
	}

	if livingCount == 0 && len(headerComp.MemberEntries) > 0 {
		s.handleTowerDeath(headerEntity)
	}
}

// === Lifecycle ===

func (s *TowerSystem) handleTowerDeath(headerEntity core.Entity) {
	towerComp, ok := s.world.Components.Tower.GetComponent(headerEntity)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventTowerDestroyed, &event.TowerDestroyedPayload{
		HeaderEntity: headerEntity,
		X:            towerComp.SpawnX,
		Y:            towerComp.SpawnY,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *TowerSystem) terminateTower(headerEntity core.Entity) {
	if !s.world.Components.Tower.HasEntity(headerEntity) {
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
			HeaderEntity: headerEntity,
			Effect:       0,
		})
		return
	}

	s.world.Components.Tower.RemoveEntity(headerEntity)
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *TowerSystem) terminateAll() {
	for _, entity := range s.world.Components.Tower.GetAllEntities() {
		s.terminateTower(entity)
	}
}