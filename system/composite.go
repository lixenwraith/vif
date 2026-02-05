package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// CompositeSystem manages composite entity groups
// Responsibilities:
// - Fixed-point movement integration
// - Member position propagation from phantom head
// - Liveness validation and tombstone marking
// - Lazy compaction of dead members
type CompositeSystem struct {
	world *engine.World

	// TODO: typed -> expected
	// Track expected deaths per header to distinguish typed from external
	expectedDeaths map[core.Entity]int

	enabled bool
}

// NewCompositeSystem creates a new composite system
func NewCompositeSystem(world *engine.World) engine.System {
	s := &CompositeSystem{
		world:          world,
		expectedDeaths: make(map[core.Entity]int),
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *CompositeSystem) Init() {
	clear(s.expectedDeaths)
	s.enabled = true
}

// Name returns system's name
func (s *CompositeSystem) Name() string {
	return "composite"
}

func (s *CompositeSystem) Priority() int {
	return parameter.PriorityComposite
}

func (s *CompositeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMemberTyped,
		event.EventCompositeDestroyRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *CompositeSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventMemberTyped:
		if payload, ok := ev.Payload.(*event.MemberTypedPayload); ok {
			s.markMemberTombstone(payload.HeaderEntity, payload.MemberEntity)
			// Track as expected death (typing, not external)
			s.expectedDeaths[payload.HeaderEntity]++
			event.EmitDeathOne(s.world.Resources.Event.Queue, payload.MemberEntity, 0)
		}

	case event.EventCompositeDestroyRequest:
		if payload, ok := ev.Payload.(*event.CompositeDestroyRequestPayload); ok {
			s.destroyComposite(payload.HeaderEntity, payload.Effect)
		}
	}
}

func (s *CompositeSystem) Update() {
	if !s.enabled {
		return
	}

	headerEntities := s.world.Components.Header.GetAllEntities()

	for _, headerEntity := range headerEntities {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerPos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		// TODO: proper integration with kinetic (internal kinetic fields removed)
		// // 1. Fixed-point movement integration
		// deltaX, deltaY := s.integrateMovement(&headerComp)
		//
		// // 2. Update phantom head position if integer delta occurred
		// if deltaX != 0 || deltaY != 0 {
		// 	headerPos.X += deltaX
		// 	headerPos.Y += deltaY
		// 	s.world.Positions.SetPosition(headerEntity, headerPos)
		// }

		// Count living members before sync
		countBefore := s.countLiving(&headerComp)

		// Sync: validate liveness, propagate positions, tombstone dead
		s.syncMembers(&headerComp, headerPos.X, headerPos.Y)

		// Compact tombstones
		if headerComp.Dirty {
			s.compactMembers(&headerComp)
			headerComp.Dirty = false
		}

		// Calculate external deaths
		countAfter := len(headerComp.MemberEntries)
		totalDeaths := countBefore - countAfter
		expectedDeaths := s.expectedDeaths[headerEntity]
		externalDeaths := totalDeaths - expectedDeaths

		// Emit integrity breach if external deaths detected
		if externalDeaths > 0 {
			s.world.PushEvent(event.EventCompositeIntegrityBreach, &event.CompositeIntegrityBreachPayload{
				HeaderEntity:   headerEntity,
				Behavior:       headerComp.Behavior,
				LostCount:      externalDeaths,
				RemainingCount: countAfter,
			})
		}

		// Check if composite still exists after potential destruction from event handling
		if !s.world.Components.Header.HasEntity(headerEntity) {
			continue
		}

		// Empty composite cleanup (all members gone, no owner claimed it)
		if len(headerComp.MemberEntries) == 0 {
			s.destroyHead(headerEntity)
			continue
		}

		s.world.Components.Header.SetComponent(headerEntity, headerComp)
	}

	// Clear per-tick expected death tracking
	clear(s.expectedDeaths)
}

func (s *CompositeSystem) countLiving(header *component.HeaderComponent) int {
	count := 0
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			count++
		}
	}
	return count
}

// destroyComposite handles centralized composite destruction via death system
func (s *CompositeSystem) destroyComposite(headerEntity core.Entity, effect event.EventType) {
	header, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Collect living members
	var members []core.Entity
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			s.world.Components.Member.RemoveEntity(m.Entity)
			members = append(members, m.Entity)
		}
	}

	// Route members through death system
	if len(members) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, effect, members)
	}

	// Destroy phantom head
	s.destroyHead(headerEntity)
}

// destroyHead removes protection and destroys phantom head directly
func (s *CompositeSystem) destroyHead(headerEntity core.Entity) {
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	event.EmitDeathOne(s.world.Resources.Event.Queue, headerEntity, 0)
	delete(s.expectedDeaths, headerEntity)
}

// markMemberTombstone internal helper for authoritative state update
func (s *CompositeSystem) markMemberTombstone(headerEntity, memberEntity core.Entity) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	for i := range headerComp.MemberEntries {
		if headerComp.MemberEntries[i].Entity == memberEntity {
			headerComp.MemberEntries[i].Entity = 0
			headerComp.Dirty = true
			break
		}
	}
	s.world.Components.Header.SetComponent(headerEntity, headerComp)
}

// syncMembers updates member positions and validates liveness
func (s *CompositeSystem) syncMembers(headerComp *component.HeaderComponent, headerX, headerY int) {
	config := s.world.Resources.Config

	for i := range headerComp.MemberEntries {
		memberEntry := &headerComp.MemberEntries[i]

		// Skip tombstones
		if memberEntry.Entity == 0 {
			continue
		}

		// Liveness check: if entity no longer has position, it was destroyed
		if !s.world.Positions.HasPosition(memberEntry.Entity) {
			memberEntry.Entity = 0 // Tombstone
			headerComp.Dirty = true
			continue
		}

		// Propagate offset
		newX := headerX + int(memberEntry.OffsetX)
		newY := headerY + int(memberEntry.OffsetY)

		// Bounds check - destroy before tombstoning
		if newX < 0 || newX >= config.GameWidth || newY < 0 || newY >= config.GameHeight {
			s.world.DestroyEntity(memberEntry.Entity)
			memberEntry.Entity = 0
			headerComp.Dirty = true
			continue
		}

		// Use MoveEntity for existing entities (updates spatial grid)
		_ = s.world.Positions.MoveEntity(memberEntry.Entity, component.PositionComponent{
			X: newX,
			Y: newY,
		})
	}
}

// compactMembers removes tombstones via swap-remove
func (s *CompositeSystem) compactMembers(headerComp *component.HeaderComponent) {
	write := 0
	for read := 0; read < len(headerComp.MemberEntries); read++ {
		if headerComp.MemberEntries[read].Entity != 0 {
			if write != read {
				headerComp.MemberEntries[write] = headerComp.MemberEntries[read]
			}
			write++
		}
	}
	headerComp.MemberEntries = headerComp.MemberEntries[:write]
}

// handleEmptyComposite processes a composite with no remaining members
func (s *CompositeSystem) handleEmptyComposite(headerEntity core.Entity, headerComp *component.HeaderComponent) {
	switch headerComp.Behavior {
	case component.BehaviorGold:
		// Gold completion handled by GoldSystem via events
		s.destroyHead(headerEntity)

	case component.BehaviorSwarm, component.BehaviorStorm:
		// Future: emit behavior-specific completion events
		s.destroyHead(headerEntity)

	default:
		s.destroyHead(headerEntity)
	}
}

// CreateHeader spawns an invisible head entity, returns phantom head entity
func (s *CompositeSystem) CreateHeader(x, y int, behaviorID component.Behavior) core.Entity {
	entity := s.world.CreateEntity()

	// Positions at anchor point
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// Header component with empty member slice
	s.world.Components.Header.SetComponent(entity, component.HeaderComponent{
		Behavior:      behaviorID,
		MemberEntries: make([]component.MemberEntry, 0, 16),
	})

	// Phantom heads are protected from all destruction except explicit removal
	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	return entity
}

// AddMember attaches a member entity to an existing composite
func (s *CompositeSystem) AddMember(headerEntity, memberEntity core.Entity, offsetX, offsetY int, layer uint8) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Set member entry
	headerComp.MemberEntries = append(headerComp.MemberEntries, component.MemberEntry{
		Entity:  memberEntity,
		OffsetX: offsetX,
		OffsetY: offsetY,
		Layer:   layer,
	})
	s.world.Components.Header.SetComponent(headerEntity, headerComp)

	// Set backlink to member
	s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
		HeaderEntity: headerEntity,
	})
}

// DestroyComposite removes the phantom head and all members
func (s *CompositeSystem) DestroyComposite(headerEntity core.Entity) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Destroy all living members
	for _, member := range headerComp.MemberEntries {
		if member.Entity != 0 {
			s.world.Components.Member.RemoveEntity(member.Entity)
			s.world.DestroyEntity(member.Entity)
		}
	}

	// RemoveEntityAt protection and destroy phantom head
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}

// GetAnchorForMember resolves the phantom head from a member entity
func (s *CompositeSystem) GetAnchorForMember(memberEntity core.Entity) (core.Entity, bool) {
	memberComp, ok := s.world.Components.Member.GetComponent(memberEntity)
	if !ok {
		return 0, false
	}
	return memberComp.HeaderEntity, true
}