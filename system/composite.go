package system
// @lixen: #dev{feature[quasar(render,system)]}

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
	res   engine.Resources

	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	protStore   *engine.Store[component.ProtectionComponent]

	enabled bool
}

// NewCompositeSystem creates a new composite system
func NewCompositeSystem(world *engine.World) engine.System {
	s := &CompositeSystem{
		world: world,
		res:   engine.GetResources(world),

		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
	}
	s.initLocked()
	return s
}

func (s *CompositeSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *CompositeSystem) initLocked() {
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
	s.markMemberTombstone(payload.AnchorID, payload.MemberEntity)

	// 2. Request death for the member entity
	event.EmitDeathOne(s.res.Events.Queue, payload.MemberEntity, 0, s.res.Time.FrameNumber)
}

func (s *CompositeSystem) Update() {
	if !s.enabled {
		return
	}

	anchors := s.headerStore.All()

	for _, anchor := range anchors {
		header, ok := s.headerStore.Get(anchor)
		if !ok {
			continue
		}

		anchorPos, hasPos := s.world.Positions.Get(anchor)
		if !hasPos {
			continue
		}

		// Phase 1: Fixed-point movement integration
		deltaX, deltaY := s.integrateMovement(&header)

		// Phase 2: Update phantom head position if integer delta occurred
		if deltaX != 0 || deltaY != 0 {
			anchorPos.X += deltaX
			anchorPos.Y += deltaY
			s.world.Positions.Set(anchor, anchorPos)
		}

		// Phase 3: Propagate offsets to members + liveness validation
		s.syncMembers(&header, anchorPos.X, anchorPos.Y)

		// Phase 4: Compact if dirty
		if header.Dirty {
			s.compactMembers(&header)
			header.Dirty = false
		}

		// Phase 5: Check composite lifecycle
		if len(header.Members) == 0 {
			s.handleEmptyComposite(anchor, &header)
			continue
		}

		// Write back header
		s.headerStore.Set(anchor, header)
	}
}

// markMemberTombstone internal helper for authoritative state update
func (s *CompositeSystem) markMemberTombstone(anchorID, memberEntity core.Entity) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}

	for i := range header.Members {
		if header.Members[i].Entity == memberEntity {
			header.Members[i].Entity = 0
			header.Dirty = true
			break
		}
	}
	s.headerStore.Set(anchorID, header)
}

// integrateMovement applies 16.16 fixed-point velocity to accumulator
// Returns integer delta when accumulator overflows
func (s *CompositeSystem) integrateMovement(header *component.CompositeHeaderComponent) (int, int) {
	header.AccX += header.VelX
	header.AccY += header.VelY

	// Integrate X
	deltaX := int(header.AccX / 65536)
	header.AccX %= 65536

	// Integrate Y
	deltaY := int(header.AccY / 65536)
	header.AccY %= 65536

	return deltaX, deltaY
}

// syncMembers updates member positions and validates liveness
func (s *CompositeSystem) syncMembers(header *component.CompositeHeaderComponent, anchorX, anchorY int) {
	config := s.res.Config

	for i := range header.Members {
		member := &header.Members[i]

		// Skip tombstones
		if member.Entity == 0 {
			continue
		}

		// Liveness check: if entity no longer has position, it was destroyed
		if !s.world.Positions.Has(member.Entity) {
			member.Entity = 0 // Tombstone
			header.Dirty = true
			continue
		}

		// Propagate offset
		newX := anchorX + int(member.OffsetX)
		newY := anchorY + int(member.OffsetY)

		// Bounds check - destroy before tombstoning
		if newX < 0 || newX >= config.GameWidth || newY < 0 || newY >= config.GameHeight {
			s.world.DestroyEntity(member.Entity)
			member.Entity = 0
			header.Dirty = true
			continue
		}

		// Use Move for existing entities (updates spatial grid atomically)
		_ = s.world.Positions.Move(member.Entity, component.PositionComponent{
			X: newX,
			Y: newY,
		})
	}
}

// compactMembers removes tombstones via swap-remove
func (s *CompositeSystem) compactMembers(header *component.CompositeHeaderComponent) {
	write := 0
	for read := 0; read < len(header.Members); read++ {
		if header.Members[read].Entity != 0 {
			if write != read {
				header.Members[write] = header.Members[read]
			}
			write++
		}
	}
	header.Members = header.Members[:write]
}

// handleEmptyComposite processes a composite with no remaining members
func (s *CompositeSystem) handleEmptyComposite(anchor core.Entity, header *component.CompositeHeaderComponent) {
	switch header.BehaviorID {
	case component.BehaviorGold:
		// Gold completion handled by GoldSystem via events
		s.destroyPhantomHead(anchor)

	case component.BehaviorBubble, component.BehaviorBoss, component.BehaviorShield:
		// Future: emit behavior-specific completion events
		s.destroyPhantomHead(anchor)

	default:
		s.destroyPhantomHead(anchor)
	}
}

// destroyPhantomHead removes protection and destroys the phantom head
func (s *CompositeSystem) destroyPhantomHead(anchor core.Entity) {
	s.protStore.Remove(anchor)
	s.headerStore.Remove(anchor)
	s.world.DestroyEntity(anchor)
}

// CreatePhantomHead spawns an invisible controller entity for a composite group
// Returns the phantom head entity ID
func (s *CompositeSystem) CreatePhantomHead(x, y int, groupID uint64, behaviorID component.BehaviorID) core.Entity {
	entity := s.world.CreateEntity()

	// Position at anchor point
	s.world.Positions.Set(entity, component.PositionComponent{X: x, Y: y})

	// Header component with empty member slice
	s.headerStore.Set(entity, component.CompositeHeaderComponent{
		BehaviorID: behaviorID,
		Members:    make([]component.MemberEntry, 0, 16),
	})

	// Phantom heads are protected from all destruction except explicit removal
	s.protStore.Set(entity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	return entity
}

// AddMember attaches a member entity to an existing composite
func (s *CompositeSystem) AddMember(anchorID, memberEntity core.Entity, offsetX, offsetY int8, layer uint8) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}

	// Set member entry
	header.Members = append(header.Members, component.MemberEntry{
		Entity:  memberEntity,
		OffsetX: offsetX,
		OffsetY: offsetY,
		Layer:   layer,
	})
	s.headerStore.Set(anchorID, header)

	// Set backlink to member
	s.memberStore.Set(memberEntity, component.MemberComponent{
		AnchorID: anchorID,
	})
}

// SetVelocity configures composite movement in 16.16 fixed-point
func (s *CompositeSystem) SetVelocity(anchorID core.Entity, velX, velY int32) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}
	header.VelX = velX
	header.VelY = velY
	s.headerStore.Set(anchorID, header)
}

// DestroyComposite removes the phantom head and all members
func (s *CompositeSystem) DestroyComposite(anchorID core.Entity) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}

	// Destroy all living members
	for _, member := range header.Members {
		if member.Entity != 0 {
			s.memberStore.Remove(member.Entity)
			s.world.DestroyEntity(member.Entity)
		}
	}

	// Remove protection and destroy phantom head
	s.protStore.Remove(anchorID)
	s.world.DestroyEntity(anchorID)
}

// GetAnchorForMember resolves the phantom head from a member entity
func (s *CompositeSystem) GetAnchorForMember(memberEntity core.Entity) (core.Entity, bool) {
	member, ok := s.memberStore.Get(memberEntity)
	if !ok {
		return 0, false
	}
	return member.AnchorID, true
}

// GetHeader retrieves the composite header for an anchor
func (s *CompositeSystem) GetHeader(anchorID core.Entity) (component.CompositeHeaderComponent, bool) {
	return s.headerStore.Get(anchorID)
}

// velocityFromFloat converts float units/second to 16.16 fixed-point per-tick
func velocityFromFloat(unitsPerSec float64, ticksPerSecond int) int32 {
	return int32((unitsPerSec / float64(ticksPerSecond)) * 65536)
}