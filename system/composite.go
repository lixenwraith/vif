package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// CompositeSystem manages composite entity groups
// Responsibilities:
// - Fixed-point movement integration
// - Member position propagation from phantom head
// - Liveness validation and tombstone marking
// - Lazy compaction of dead members
type CompositeSystem struct {
	world *engine.World

	enabled bool
}

// NewCompositeSystem creates a new composite system
func NewCompositeSystem(world *engine.World) engine.System {
	s := &CompositeSystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *CompositeSystem) Init() {
	s.enabled = true
}

func (s *CompositeSystem) Priority() int {
	return constant.PriorityComposite
}

func (s *CompositeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventMemberTyped,
	}
}

func (s *CompositeSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type != event.EventMemberTyped {
		return
	}

	payload, ok := ev.Payload.(*event.MemberTypedPayload)
	if !ok {
		return
	}

	// 1. Mark tombstone immediately (Single Authority)
	s.markMemberTombstone(payload.HeaderEntity, payload.MemberEntity)

	// 2. Request death for the member entity
	event.EmitDeathOne(s.world.Resources.Event.Queue, payload.MemberEntity, 0, s.world.Resources.Time.FrameNumber)
}

func (s *CompositeSystem) Update() {
	if !s.enabled {
		return
	}

	headerEntities := s.world.Components.Header.AllEntities()

	for _, headerEntity := range headerEntities {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerPos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		// 1. Fixed-point movement integration
		deltaX, deltaY := s.integrateMovement(&headerComp)

		// 2. Update phantom head position if integer delta occurred
		if deltaX != 0 || deltaY != 0 {
			headerPos.X += deltaX
			headerPos.Y += deltaY
			s.world.Positions.SetPosition(headerEntity, headerPos)
		}

		// 3. Propagate offsets to members + liveness validation
		s.syncMembers(&headerComp, headerPos.X, headerPos.Y)

		// 4. Compact if dirty
		if headerComp.Dirty {
			s.compactMembers(&headerComp)
			headerComp.Dirty = false
		}

		// 5. Check composite lifecycle
		if len(headerComp.MemberEntries) == 0 {
			s.handleEmptyComposite(headerEntity, &headerComp)
			continue
		}

		// Write back headerComp
		s.world.Components.Header.SetComponent(headerEntity, headerComp)
	}
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

// TODO: migrate to Q32.32, this should be handled in kinetic psudo-comp
// integrateMovement applies 16.16 fixed-point velocity to accumulator
// Returns integer delta when accumulator overflows
func (s *CompositeSystem) integrateMovement(headerComp *component.HeaderComponent) (int, int) {
	headerComp.AccX += headerComp.VelX
	headerComp.AccY += headerComp.VelY

	// Integrate X
	deltaX := int(headerComp.AccX / 65536)
	headerComp.AccX %= 65536

	// Integrate Y
	deltaY := int(headerComp.AccY / 65536)
	headerComp.AccY %= 65536

	return deltaX, deltaY
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
		if !s.world.Positions.HasEntity(memberEntry.Entity) {
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

	case component.BehaviorBubble, component.BehaviorBoss, component.BehaviorShield:
		// Future: emit behavior-specific completion events
		s.destroyHead(headerEntity)

	default:
		s.destroyHead(headerEntity)
	}
}

// destroyHead removes protection and destroys the phantom head
func (s *CompositeSystem) destroyHead(headerEntity core.Entity) {
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}

// CreateHeader spawns an invisible head entity, returns phantom head entity
func (s *CompositeSystem) CreateHeader(x, y int, behaviorID component.Behavior) core.Entity {
	entity := s.world.CreateEntity()

	// Positions at anchor point
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})

	// HeaderEntity component with empty member slice
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
func (s *CompositeSystem) AddMember(headerEntity, memberEntity core.Entity, offsetX, offsetY int8, layer uint8) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// SetPosition member entry
	headerComp.MemberEntries = append(headerComp.MemberEntries, component.MemberEntry{
		Entity:  memberEntity,
		OffsetX: offsetX,
		OffsetY: offsetY,
		Layer:   layer,
	})
	s.world.Components.Header.SetComponent(headerEntity, headerComp)

	// SetPosition backlink to member
	s.world.Components.Member.SetComponent(memberEntity, component.MemberComponent{
		HeaderEntity: headerEntity,
	})
}

// SetVelocity configures composite movement in 16.16 fixed-point
func (s *CompositeSystem) SetVelocity(headerEntity core.Entity, velX, velY int64) {
	headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}
	headerComp.VelX = velX
	headerComp.VelY = velY
	s.world.Components.Header.SetComponent(headerEntity, headerComp)
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

	// Remove protection and destroy phantom head
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