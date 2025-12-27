package system

import (
	"cmp"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// QuasarSystem manages the quasar boss entity lifecycle
// Quasar is a 3x5 composite that tracks cursor at 2x drain speed
// Drains 1000 energy/tick when any part overlaps shield
// Resets heat to 0 on direct cursor collision without shield
type QuasarSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	quasarStore *engine.Store[component.QuasarComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	shieldStore *engine.Store[component.ShieldComponent]
	heatStore   *engine.Store[component.HeatComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	charStore   *engine.Store[component.CharacterComponent]
	nuggetStore *engine.Store[component.NuggetComponent]

	// Runtime state
	active       bool
	anchorEntity core.Entity

	// Telemetry
	statActive *atomic.Bool

	enabled bool
}

// NewQuasarSystem creates a new quasar system
func NewQuasarSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &QuasarSystem{
		world: world,
		res:   res,

		quasarStore: engine.GetStore[component.QuasarComponent](world),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		shieldStore: engine.GetStore[component.ShieldComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		charStore:   engine.GetStore[component.CharacterComponent](world),
		nuggetStore: engine.GetStore[component.NuggetComponent](world),

		statActive: res.Status.Bools.Get("quasar.active"),
	}
	s.initLocked()
	return s
}

func (s *QuasarSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

func (s *QuasarSystem) initLocked() {
	s.active = false
	s.anchorEntity = 0
	s.statActive.Store(false)
	s.enabled = true
}

func (s *QuasarSystem) Priority() int {
	return constant.PriorityQuasar
}

func (s *QuasarSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventQuasarSpawned,
		event.EventGoldComplete,
		event.EventGameReset,
	}
}

func (s *QuasarSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.mu.Lock()
		if s.active && s.anchorEntity != 0 {
			s.terminateQuasarLocked()
		}
		s.mu.Unlock()
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventQuasarSpawned:
		payload, ok := ev.Payload.(*event.QuasarSpawnedPayload)
		if !ok {
			return
		}
		s.mu.Lock()
		s.active = true
		s.anchorEntity = payload.AnchorEntity
		s.statActive.Store(true)
		s.mu.Unlock()

		// Activate persistent grayout
		s.res.State.State.StartGrayout()

	case event.EventGoldComplete:
		s.mu.RLock()
		active := s.active
		s.mu.RUnlock()

		if active {
			s.terminateQuasar()
		}
	}
}

func (s *QuasarSystem) Update() {
	if !s.enabled {
		return
	}

	s.mu.RLock()
	active := s.active
	anchorEntity := s.anchorEntity
	s.mu.RUnlock()

	if !active || anchorEntity == 0 {
		return
	}

	// Check heat for termination (heat=0 ends quasar phase)
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		if hc.Current.Load() <= 0 {
			s.terminateQuasar()
			return
		}
	}

	// Verify composite still exists
	header, ok := s.headerStore.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	quasar, ok := s.quasarStore.Get(anchorEntity)
	if !ok {
		s.terminateQuasar()
		return
	}

	now := s.res.Time.GameTime

	// Movement update (clock-based, 2x drain speed)
	if now.Sub(quasar.LastMoveTime) >= constant.QuasarMoveInterval {
		s.updateMovement(anchorEntity, &header, &quasar)
		quasar.LastMoveTime = now
		s.quasarStore.Set(anchorEntity, quasar)
	}

	// Shield and cursor interaction
	s.handleInteractions(anchorEntity, &header, &quasar)
}

// updateMovement moves quasar toward cursor
func (s *QuasarSystem) updateMovement(anchorEntity core.Entity, header *component.CompositeHeaderComponent, quasar *component.QuasarComponent) {
	config := s.res.Config
	cursorEntity := s.res.Cursor.Entity

	anchorPos, ok := s.world.Positions.Get(anchorEntity)
	if !ok {
		return
	}

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	// Calculate movement direction (Manhattan, 8-directional)
	dx := cmp.Compare(cursorPos.X, anchorPos.X)
	dy := cmp.Compare(cursorPos.Y, anchorPos.Y)

	newAnchorX := anchorPos.X + dx
	newAnchorY := anchorPos.Y + dy

	// Calculate bounds for the entire quasar footprint
	topLeftX := newAnchorX - constant.QuasarAnchorOffsetX
	topLeftY := newAnchorY - constant.QuasarAnchorOffsetY
	bottomRightX := topLeftX + constant.QuasarWidth - 1
	bottomRightY := topLeftY + constant.QuasarHeight - 1

	// Clamp to keep entire quasar within bounds
	if topLeftX < 0 {
		newAnchorX -= topLeftX
	}
	if topLeftY < 0 {
		newAnchorY -= topLeftY
	}
	if bottomRightX >= config.GameWidth {
		newAnchorX -= (bottomRightX - config.GameWidth + 1)
	}
	if bottomRightY >= config.GameHeight {
		newAnchorY -= (bottomRightY - config.GameHeight + 1)
	}

	// Skip if no movement
	if newAnchorX == anchorPos.X && newAnchorY == anchorPos.Y {
		return
	}

	// Process collisions at new member positions before moving
	s.processCollisionsAtNewPositions(newAnchorX, newAnchorY, header)

	// Update anchor position (CompositeSystem will propagate to members)
	s.world.Positions.Set(anchorEntity, component.PositionComponent{X: newAnchorX, Y: newAnchorY})

	// Sync member positions immediately (don't wait for CompositeSystem)
	for i := range header.Members {
		member := &header.Members[i]
		if member.Entity == 0 {
			continue
		}
		memberX := newAnchorX + int(member.OffsetX)
		memberY := newAnchorY + int(member.OffsetY)
		s.world.Positions.Set(member.Entity, component.PositionComponent{X: memberX, Y: memberY})
	}
}

// processCollisionsAtNewPositions destroys entities at quasar's destination
func (s *QuasarSystem) processCollisionsAtNewPositions(anchorX, anchorY int, header *component.CompositeHeaderComponent) {
	cursorEntity := s.res.Cursor.Entity

	// Build set of member entity IDs for exclusion
	memberSet := make(map[core.Entity]bool, len(header.Members)+1)
	memberSet[s.anchorEntity] = true
	for _, m := range header.Members {
		if m.Entity != 0 {
			memberSet[m.Entity] = true
		}
	}

	var toDestroy []core.Entity

	// Check each cell the quasar will occupy
	topLeftX := anchorX - constant.QuasarAnchorOffsetX
	topLeftY := anchorY - constant.QuasarAnchorOffsetY

	for row := 0; row < constant.QuasarHeight; row++ {
		for col := 0; col < constant.QuasarWidth; col++ {
			x := topLeftX + col
			y := topLeftY + row

			entities := s.world.Positions.GetAllAt(x, y)
			for _, e := range entities {
				if e == 0 || e == cursorEntity || memberSet[e] {
					continue
				}

				// Check protection
				if prot, ok := s.protStore.Get(e); ok {
					if prot.Mask == component.ProtectAll {
						continue
					}
				}

				// Handle nugget collision
				if s.nuggetStore.Has(e) {
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: e,
					})
				}

				// Handle gold composite collision
				if member, ok := s.memberStore.Get(e); ok {
					if h, hOk := s.headerStore.Get(member.AnchorID); hOk && h.BehaviorID == component.BehaviorGold {
						s.destroyGoldComposite(member.AnchorID)
						continue
					}
				}

				toDestroy = append(toDestroy, e)
			}
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, 0, toDestroy, s.res.Time.FrameNumber)
	}
}

// destroyGoldComposite handles gold sequence destruction by quasar
func (s *QuasarSystem) destroyGoldComposite(anchorID core.Entity) {
	header, ok := s.headerStore.Get(anchorID)
	if !ok {
		return
	}

	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		AnchorEntity: anchorID,
	})

	// Destroy all members
	var toDestroy []core.Entity
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.memberStore.Remove(m.Entity)
			toDestroy = append(toDestroy, m.Entity)
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.res.Events.Queue, 0, toDestroy, s.res.Time.FrameNumber)
	}

	// Destroy phantom head
	s.protStore.Remove(anchorID)
	s.headerStore.Remove(anchorID)
	s.world.DestroyEntity(anchorID)
}

// handleInteractions processes shield drain and cursor collision
func (s *QuasarSystem) handleInteractions(anchorEntity core.Entity, header *component.CompositeHeaderComponent, quasar *component.QuasarComponent) {
	cursorEntity := s.res.Cursor.Entity

	cursorPos, ok := s.world.Positions.Get(cursorEntity)
	if !ok {
		return
	}

	shield, shieldOk := s.shieldStore.Get(cursorEntity)
	shieldActive := shieldOk && shield.Active

	// Check each member position
	anyInShield := false
	anyOnCursor := false

	for _, m := range header.Members {
		if m.Entity == 0 {
			continue
		}

		memberPos, ok := s.world.Positions.Get(m.Entity)
		if !ok {
			continue
		}

		// Cursor collision check
		if memberPos.X == cursorPos.X && memberPos.Y == cursorPos.Y {
			anyOnCursor = true
		}

		// Shield overlap check
		if shieldActive && s.isInsideShieldEllipse(memberPos.X, memberPos.Y, cursorPos, &shield) {
			anyInShield = true
		}
	}

	// Update cached state
	if quasar.IsOnCursor != anyOnCursor {
		quasar.IsOnCursor = anyOnCursor
		s.quasarStore.Set(anchorEntity, *quasar)
	}

	// Shield drain (once per tick if any overlap)
	if anyInShield {
		s.world.PushEvent(event.EventShieldDrain, &event.ShieldDrainPayload{
			Amount: constant.QuasarShieldDrain,
		})
		return // Shield protects from direct collision
	}

	// Direct cursor collision without shield → reset heat to 0
	if anyOnCursor && !shieldActive {
		s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: 0})
		// Heat=0 will trigger termination in next Update()
	}
}

// isInsideShieldEllipse checks if position is within shield using fixed-point math
func (s *QuasarSystem) isInsideShieldEllipse(x, y int, cursorPos component.PositionComponent, shield *component.ShieldComponent) bool {
	dx := vmath.FromInt(x - cursorPos.X)
	dy := vmath.FromInt(y - cursorPos.Y)
	return vmath.EllipseContains(dx, dy, shield.InvRxSq, shield.InvRySq)
}

// terminateQuasar ends the quasar phase
func (s *QuasarSystem) terminateQuasar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminateQuasarLocked()
}

// terminateQuasarLocked ends quasar phase, caller must hold s.mu
func (s *QuasarSystem) terminateQuasarLocked() {
	if !s.active {
		return
	}

	// Destroy composite
	if s.anchorEntity != 0 {
		s.destroyQuasarComposite(s.anchorEntity)
	}

	// End grayout
	s.res.State.State.EndGrayout()

	// Resume drain spawning
	s.world.PushEvent(event.EventDrainResume, nil)

	// Emit destroyed event (for future audio/effects)
	s.world.PushEvent(event.EventQuasarDestroyed, nil)

	s.active = false
	s.anchorEntity = 0
	s.statActive.Store(false)
}

// destroyQuasarComposite removes the quasar entity structure
func (s *QuasarSystem) destroyQuasarComposite(anchorEntity core.Entity) {
	header, ok := s.headerStore.Get(anchorEntity)
	if !ok {
		return
	}

	// Destroy all members
	for _, m := range header.Members {
		if m.Entity != 0 {
			s.memberStore.Remove(m.Entity)
			s.world.DestroyEntity(m.Entity)
		}
	}

	// Remove components from phantom head
	s.quasarStore.Remove(anchorEntity)
	s.headerStore.Remove(anchorEntity)
	s.protStore.Remove(anchorEntity)
	s.world.DestroyEntity(anchorEntity)
}