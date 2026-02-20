package system

import (
	"math"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// pylonCacheEntry holds cached entity position for soft collision
type pylonCacheEntry struct {
	entity core.Entity
	x, y   int
}

// PylonSystem manages pylon enemy entity lifecycle
// Pylon is a stationary ablative composite that acts as damage sponge
// Pushes other enemies away via soft collision
type PylonSystem struct {
	world *engine.World

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewPylonSystem creates a new pylon system
func NewPylonSystem(world *engine.World) engine.System {
	s := &PylonSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("pylon.active")
	s.statCount = world.Resources.Status.Ints.Get("pylon.count")

	s.Init()
	return s
}

func (s *PylonSystem) Init() {
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

func (s *PylonSystem) Name() string {
	return "pylon"
}

func (s *PylonSystem) Priority() int {
	return parameter.PriorityPylon
}

func (s *PylonSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPylonSpawnRequest,
		event.EventPylonCancelRequest,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *PylonSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventPylonSpawnRequest:
		if payload, ok := ev.Payload.(*event.PylonSpawnRequestPayload); ok {
			s.spawnPylon(payload)
		}

	case event.EventPylonCancelRequest:
		s.terminateAll()

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.Behavior == component.BehaviorPylon && payload.RemainingCount == 0 {
				s.handlePylonDeath(payload.HeaderEntity)
			}
		}
	}
}

func (s *PylonSystem) Update() {
	if !s.enabled {
		return
	}

	pylonEntities := s.world.Components.Pylon.GetAllEntities()
	if len(pylonEntities) == 0 {
		s.statActive.Store(false)
		s.statCount.Store(0)
		return
	}

	activeCount := 0

	for _, headerEntity := range pylonEntities {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Process member combat (HP <= 0 detection)
		// Deaths routed through CompositeSystem; IntegrityBreach triggers handlePylonDeath
		s.processAblativeCombat(headerEntity, &headerComp)

		activeCount++
	}

	s.statCount.Store(int64(activeCount))
	s.statActive.Store(activeCount > 0)
}

func (s *PylonSystem) spawnPylon(payload *event.PylonSpawnRequestPayload) {
	radius := payload.Radius
	if radius <= 0 {
		radius = parameter.PylonDefaultRadius
	}

	minHP := payload.MinHP
	maxHP := payload.MaxHP
	if minHP <= 0 {
		minHP = parameter.CombatInitialHPPylonMin
	}
	if maxHP <= 0 {
		maxHP = parameter.CombatInitialHPPylonMax
	}
	if minHP > maxHP {
		minHP = maxHP
	}

	centerX, centerY := payload.X, payload.Y

	// Create header entity
	headerEntity := s.world.CreateEntity()
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: centerX, Y: centerY})

	// Header is protected from destruction except explicit death
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// Pylon component
	s.world.Components.Pylon.SetComponent(headerEntity, component.PylonComponent{
		SpawnX: centerX,
		SpawnY: centerY,
		Radius: radius,
		MinHP:  minHP,
		MaxHP:  maxHP,
	})

	// Combat component on header (HP=0 for ablative, damage routes to members)
	s.world.Components.Combat.SetComponent(headerEntity, component.CombatComponent{
		OwnerEntity:      headerEntity,
		CombatEntityType: component.CombatEntityPylon,
		HitPoints:        0,
	})

	// Generate disc members
	members := s.createDiscMembers(headerEntity, centerX, centerY, radius, minHP, maxHP)

	// Header component
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorPylon,
		Type:          component.CompositeTypeAblative,
		MemberEntries: members,
	})

	// Emit creation events
	s.world.PushEvent(event.EventEnemyCreated, &event.EnemyCreatedPayload{
		Entity:  headerEntity,
		Species: component.SpeciesPylon,
	})

	s.world.PushEvent(event.EventPylonSpawned, &event.PylonSpawnedPayload{
		HeaderEntity: headerEntity,
		MemberCount:  len(members),
	})
}

// createDiscMembers generates solid disc of member entities with HP falloff
func (s *PylonSystem) createDiscMembers(
	headerEntity core.Entity,
	centerX, centerY, radius, minHP, maxHP int,
) []component.MemberEntry {
	var members []component.MemberEntry

	radiusSq := radius * radius
	radiusF := float64(radius)

	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			distSq := dx*dx + dy*dy
			if distSq > radiusSq {
				continue
			}

			// Calculate HP based on distance from center
			dist := 0.0
			if distSq > 0 {
				dist = math.Sqrt(float64(distSq))
			}

			// Linear interpolation: center=maxHP, edge=minHP
			var hp int
			if radiusF > 0 {
				ratio := dist / radiusF
				hp = maxHP - int(float64(maxHP-minHP)*ratio)
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

			// Protection from game mechanics, not combat
			s.world.Components.Protection.SetComponent(memberEntity, component.ProtectionComponent{
				Mask: component.ProtectFromDecay | component.ProtectFromDelete | component.ProtectFromSpecies,
			})

			// Per-member combat component (ablative HP)
			s.world.Components.Combat.SetComponent(memberEntity, component.CombatComponent{
				OwnerEntity:      headerEntity,
				CombatEntityType: component.CombatEntityPylon,
				HitPoints:        hp,
			})

			// Backlink to header
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

// processAblativeCombat scans members for HP<=0 and routes deaths through CompositeSystem
func (s *PylonSystem) processAblativeCombat(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := s.world.Components.Combat.GetComponent(member.Entity)
		if !ok {
			continue
		}

		if combatComp.HitPoints <= 0 {
			// Route through CompositeSystem for proper lifecycle handling
			s.world.PushEvent(event.EventMemberTyped, &event.MemberTypedPayload{
				HeaderEntity: headerEntity,
				MemberEntity: member.Entity,
			})
		}
	}
}

func (s *PylonSystem) handlePylonDeath(headerEntity core.Entity) {
	pylonComp, ok := s.world.Components.Pylon.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Remove component immediately to prevent re-entry from concurrent paths
	s.world.Components.Pylon.RemoveEntity(headerEntity)

	// Emit destroyed event
	s.world.PushEvent(event.EventPylonDestroyed, &event.PylonDestroyedPayload{
		HeaderEntity: headerEntity,
		X:            pylonComp.SpawnX,
		Y:            pylonComp.SpawnY,
	})

	// Emit enemy killed for loot/scoring
	s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
		Entity:  headerEntity,
		Species: component.SpeciesPylon,
		X:       pylonComp.SpawnX,
		Y:       pylonComp.SpawnY,
	})

	// Request composite destruction (header + remaining members)
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *PylonSystem) terminatePylon(headerEntity core.Entity) {
	// Guard: skip if already removed by handlePylonDeath
	if !s.world.Components.Pylon.HasEntity(headerEntity) {
		// Still request composite cleanup for orphaned headers
		s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
			HeaderEntity: headerEntity,
			Effect:       0,
		})
		return
	}

	s.world.Components.Pylon.RemoveEntity(headerEntity)
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})
}

func (s *PylonSystem) terminateAll() {
	for _, entity := range s.world.Components.Pylon.GetAllEntities() {
		s.terminatePylon(entity)
	}
}