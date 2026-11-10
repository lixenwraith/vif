package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// CompositeSystem keeps headers and members consistent: member positions propagated
// from the phantom head, liveness tombstones, compaction, integrity breach events and
// destroy requests
type CompositeSystem struct {
	world   *engine.World
	headers []core.Entity // the tick's headers, detached: syncMembers and destroyHead move the store
	toggle
}

// NewCompositeSystem creates a new composite system
func NewCompositeSystem(world *engine.World) engine.System {
	s := &CompositeSystem{
		world: world,
	}
	return s
}

// Init resets session state for new game
func (s *CompositeSystem) Init() {
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
		event.EventCompositeMemberDestroyed,
		event.EventCompositeDestroyRequest,
		event.EventMetaSystemCommandRequest,
	}
}

func (s *CompositeSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventCompositeMemberDestroyed:
		if payload, ok := ev.Payload.(*event.CompositeMemberDestroyedPayload); ok {
			s.markMemberTombstone(payload.HeaderEntity, payload.MemberEntity)
			s.world.DestroyEntity(payload.MemberEntity)
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

	// syncMembers and destroyHead can structurally mutate Header/Member stores;
	// preserve the original header order and keep component values detached.
	s.headers = append(s.headers[:0], s.world.Components.Header.Entities()...)

	for _, headerEntity := range s.headers {
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerPos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

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
		externalDeaths := countBefore - countAfter

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

		// Generic ablative detection via Header HP=0 for header destruction
		// Ablative composites: Header HP=0, authoritative system handles destruction
		// Non-ablative composites: Header HP>0, CompositeSystem destroys when empty
		if len(headerComp.MemberEntries) == 0 {
			if combat, ok := s.world.Components.Combat.GetComponent(headerEntity); ok && combat.HitPoints == 0 {
				s.world.Components.Header.SetComponent(headerEntity, headerComp)
				continue
			}
			s.destroyHead(headerEntity)
			continue
		}

		s.world.Components.Header.SetComponent(headerEntity, headerComp)
	}
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
func (s *CompositeSystem) destroyComposite(headerEntity core.Entity, effect component.DeathEffect) {
	header, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Collect living members
	var members []core.Entity
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			members = append(members, m.Entity)
		}
	}

	// Route members through death system
	if len(members) > 0 {
		event.EmitDeath(s.world.Resources.Event.Queue, effect, members...)
	}

	// Destroy phantom head
	s.destroyHead(headerEntity)
}

// destroyHead destroys the phantom head directly
func (s *CompositeSystem) destroyHead(headerEntity core.Entity) {
	s.world.DestroyEntity(headerEntity)
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
		if !s.world.Components.Member.HasEntity(memberEntry.Entity) {
			memberEntry.Entity = 0 // Tombstone
			headerComp.Dirty = true
			continue
		}

		// Owner system manages member positions
		if headerComp.SkipPositionSync {
			continue
		}

		// Propagate offset
		newX := headerX + int(memberEntry.OffsetX)
		newY := headerY + int(memberEntry.OffsetY)

		// Bounds check - destroy before tombstoning
		if newX < 0 || newX >= config.MapWidth || newY < 0 || newY >= config.MapHeight {
			s.world.DestroyEntity(memberEntry.Entity)
			memberEntry.Entity = 0
			headerComp.Dirty = true
			continue
		}

		s.world.Positions.SetPosition(memberEntry.Entity, component.PositionComponent{
			X: newX,
			Y: newY,
		})
	}
}

// compactMembers removes tombstones via swap-remove
func (s *CompositeSystem) compactMembers(headerComp *component.HeaderComponent) {
	write := 0
	for read := range len(headerComp.MemberEntries) {
		if headerComp.MemberEntries[read].Entity != 0 {
			if write != read {
				headerComp.MemberEntries[write] = headerComp.MemberEntries[read]
			}
			write++
		}
	}
	headerComp.MemberEntries = headerComp.MemberEntries[:write]
}
