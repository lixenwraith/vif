package system

import (
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// GoldSystem manages the gold sequence mechanic autonomously
type GoldSystem struct {
	world *engine.World

	// Internal state
	active       bool
	headerEntity core.Entity // Phantom Head
	startTime    time.Time
	timeoutTime  time.Time
	spawnEnabled bool

	// Cached metric pointers
	statActive        *atomic.Bool
	stateHeaderEntity *atomic.Int64
	statTimer         *atomic.Int64

	enabled bool
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(world *engine.World) engine.System {
	s := &GoldSystem{
		world: world,
	}

	s.statActive = s.world.Resources.Status.Bools.Get("gold.active")
	s.stateHeaderEntity = s.world.Resources.Status.Ints.Get("gold.header_entity")
	s.statTimer = s.world.Resources.Status.Ints.Get("gold.timer")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *GoldSystem) Init() {
	s.active = false
	s.headerEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.spawnEnabled = true
	s.statActive.Store(false)
	s.stateHeaderEntity.Store(0)
	s.statTimer.Store(0)
	s.enabled = true
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return constant.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGoldEnable,
		event.EventGoldSpawnRequest,
		event.EventGoldCancel,
		event.EventGoldJumpRequest,
		event.EventMemberTyped,
		event.EventGameReset,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	// TODO: implement enabled event for all systems
	case event.EventGoldEnable:
		if payload, ok := ev.Payload.(*event.GoldEnablePayload); ok {
			s.spawnEnabled = payload.Enabled
		}

	case event.EventGoldCancel:
		s.destroyCurrentGold()

	case event.EventGoldJumpRequest:
		s.handleJumpRequest()

	case event.EventGoldSpawnRequest:
		enabled := s.spawnEnabled
		active := s.active

		if !enabled || active {
			s.world.PushEvent(event.EventGoldSpawnFailed, nil)
			return
		}

		if s.spawnGold() {
			// EventGoldSpawned emitted inside spawnGold
		} else {
			s.world.PushEvent(event.EventGoldSpawnFailed, nil)
		}

	case event.EventMemberTyped:
		payload, ok := ev.Payload.(*event.MemberTypedPayload)
		if !ok {
			return
		}

		isGoldAnchor := payload.HeaderEntity == s.headerEntity

		if isGoldAnchor {
			if payload.RemainingCount == 0 {
				s.handleGoldComplete()
			}
		}
	}
}

// Update runs the gold sequence system logic
func (s *GoldSystem) Update() {
	if !s.enabled {
		return
	}

	now := s.world.Resources.Time.GameTime

	active := s.active
	timeoutTime := s.timeoutTime
	headerEntity := s.headerEntity

	// Publish metrics
	s.statActive.Store(active)
	if active {
		remaining := timeoutTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		s.statTimer.Store(int64(remaining))
		s.stateHeaderEntity.Store(int64(s.headerEntity))
	} else {
		s.statTimer.Store(0)
	}

	if !active {
		return
	}

	// Check if composite still exists (external destruction detection)
	if headerEntity != 0 {
		header, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok || s.countLivingMembers(&header) == 0 {
			s.handleGoldDestroyed()
			return
		}
	}

	// Timeout check
	if now.After(timeoutTime) {
		s.handleGoldTimeout()
	}
}

// handleJumpRequest jumps cursor to the first living member of the gold sequence
func (s *GoldSystem) handleJumpRequest() {
	if !s.active || s.headerEntity == 0 {
		return
	}

	cursorEntity := s.world.Resources.Cursor.Entity

	// 1. Check Energy from component
	energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Cost check (must have enough "absolute" energy to pay cost)
	// Logic mimics NuggetSystem: allow jump only if energy moves towards zero
	// If currently at 0, no jump
	energy := energyComp.Current.Load()
	cost := int64(constant.NuggetJumpCost)
	if energy > -cost && energy < cost {
		return
	}

	// 2. Find target position (First living member)
	header, ok := s.world.Components.Header.GetComponent(s.headerEntity)
	if !ok {
		return
	}

	var targetEntity core.Entity
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			targetEntity = m.Entity
			break
		}
	}

	if targetEntity == 0 {
		// No living members, should rely on update loop to clean up, but exit here
		return
	}

	targetPos, ok := s.world.Positions.Get(targetEntity)
	if !ok {
		return
	}

	// 3. Move Cursor
	s.world.Positions.SetPosition(cursorEntity, component.PositionComponent{
		X: targetPos.X,
		Y: targetPos.Y,
	})

	// 4. Pay Energy Cost (Convergent Spend)
	s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
		Delta:      -constant.NuggetJumpCost,
		Spend:      true,
		Convergent: true,
	})

	// 5. Play Sound
	if s.world.Resources.Audio != nil {
		s.world.Resources.Audio.Player.Play(core.SoundBell)
	}

	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{
		X: targetPos.X,
		Y: targetPos.Y,
	})
}

// spawnGold creates a new gold sequence
func (s *GoldSystem) spawnGold() bool {
	now := s.world.Resources.Time.GameTime

	// Generate random 10-character sequence
	sequence := make([]rune, constant.GoldSequenceLength)
	for i := 0; i < constant.GoldSequenceLength; i++ {
		sequence[i] = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
	}

	// Find empty space to spawnLightning gold
	x, y := s.findValidPosition(constant.GoldSequenceLength)
	if x < 0 || y < 0 {
		return false
	}

	// 1. Create Phantom Head entity (NO position yet)
	headerEntity := s.world.CreateEntity()

	// 2. Create member entities
	type entityData struct {
		entity core.Entity
		pos    component.PositionComponent
		offset int8
	}
	entities := make([]entityData, 0, constant.GoldSequenceLength)
	// Create member entities
	members := make([]component.MemberEntry, 0, constant.GoldSequenceLength)

	// SetPosition position component to gold entities
	for i := 0; i < constant.GoldSequenceLength; i++ {
		entity := s.world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos:    component.PositionComponent{X: x + i, Y: y},
			offset: int8(i),
		})
	}

	// 3. Batch position commit (anchor NOT in grid - no collision at x,y)
	batch := s.world.Positions.BeginBatch()
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.Commit(); err != nil {
		for _, ed := range entities {
			s.world.DestroyEntity(ed.entity)
		}
		s.world.DestroyEntity(headerEntity)
		return false
	}

	// 4. SetPosition Phantom Head to Positions AFTER batch success
	// TODO: check protectAll, it may conflicts with OOB bound, set specific protections
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// 5. SetPosition components to members
	for i, ed := range entities {
		// Typing target
		s.world.Components.Glyph.SetComponent(ed.entity, component.GlyphComponent{
			Rune:  sequence[i],
			Type:  component.GlyphGold,
			Level: component.GlyphBright,
		})

		// Composite membership
		s.world.Components.Member.SetComponent(ed.entity, component.MemberComponent{
			HeaderEntity: headerEntity,
		})

		// Protect gold entities from decay/delete
		s.world.Components.Protection.SetComponent(ed.entity, component.ProtectionComponent{
			Mask: component.ProtectFromDelete | component.ProtectFromDecay,
		})

		// SetPosition gold entity to composite member entities
		members = append(members, component.MemberEntry{
			Entity:  ed.entity,
			OffsetX: ed.offset,
			OffsetY: 0,
			Layer:   component.LayerGlyph,
		})
	}

	// 6. Create composite header
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		BehaviorID:    component.BehaviorGold,
		MemberEntries: members,
	})

	// 7. Activate internal state
	s.active = true
	s.headerEntity = headerEntity
	s.startTime = now
	s.timeoutTime = now.Add(constant.GoldDuration)

	// Emit spawnLightning event
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		HeaderEntity: headerEntity,
		OriginX:      x,
		OriginY:      y,
		Length:       constant.GoldSequenceLength,
		Duration:     constant.GoldDuration,
	})

	return true
}

// handleMemberTyped processes a gold character being typed
func (s *GoldSystem) handleMemberTyped(payload *event.MemberTypedPayload) {
	if !s.active || payload.HeaderEntity != s.headerEntity {
		return
	}

	// Check if sequence complete
	if payload.RemainingCount == 0 {
		s.handleGoldComplete()
	}
}

// handleGoldComplete processes successful gold sequence completion
func (s *GoldSystem) handleGoldComplete() {
	headerEntity := s.headerEntity

	// Emit completion event, FSM is the reward authority
	s.world.PushEvent(event.EventGoldComplete, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	// Play sound
	if s.world.Resources.Audio != nil && s.world.Resources.Audio.Player != nil {
		s.world.Resources.Audio.Player.Play(core.SoundCoin)
	}

	// Destroy composite
	s.destroyComposite(headerEntity)

	s.active = false
	s.headerEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.stateHeaderEntity.Store(0)
}

// handleGoldTimeout processes gold sequence expiration
func (s *GoldSystem) handleGoldTimeout() {
	headerEntity := s.headerEntity

	s.world.PushEvent(event.EventGoldTimeout, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	s.destroyComposite(headerEntity)

	s.active = false
	s.headerEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.stateHeaderEntity.Store(0)
}

// handleGoldDestroyed processes external gold destruction
func (s *GoldSystem) handleGoldDestroyed() {
	headerEntity := s.headerEntity

	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	if headerEntity != 0 {
		s.destroyComposite(headerEntity)
	}

	s.active = false
	s.headerEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.stateHeaderEntity.Store(0)
}

// destroyCurrentGold destroys the current gold if active
func (s *GoldSystem) destroyCurrentGold() {
	headerEntity := s.headerEntity
	active := s.active

	if active && headerEntity != 0 {
		s.destroyComposite(headerEntity)
	}
}

// destroyComposite removes phantom head and all members
func (s *GoldSystem) destroyComposite(headerEntity core.Entity) {
	header, ok := s.world.Components.Header.GetComponent(headerEntity)
	if !ok {
		return
	}

	// Collect living members for batch death
	var toDestroy []core.Entity
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			s.world.Components.Member.RemoveEntity(m.Entity)
			toDestroy = append(toDestroy, m.Entity)
		}
	}

	if len(toDestroy) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, toDestroy, s.world.Resources.Time.FrameNumber)
	}

	// Remove protection and destroy phantom head
	s.world.Components.Protection.RemoveEntity(headerEntity)
	s.world.Components.Header.RemoveEntity(headerEntity)
	s.world.DestroyEntity(headerEntity)
}

// countLivingMembers returns count of non-tombstone members
func (s *GoldSystem) countLivingMembers(header *component.HeaderComponent) int {
	count := 0
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			count++
		}
	}
	return count
}

// findValidPosition finds a valid random position for the gold sequence
// Caller must NOT hold s.mu lock
func (s *GoldSystem) findValidPosition(seqLength int) (int, int) {
	config := s.world.Resources.Config
	cursorPos, ok := s.world.Positions.Get(s.world.Resources.Cursor.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < constant.GoldSpawnMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-cursorPos.X)) <= constant.CursorExclusionX ||
			math.Abs(float64(y-cursorPos.Y)) <= constant.CursorExclusionY {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > config.GameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if s.world.Positions.HasAny(x+i, y) {
				overlaps = true
				break
			}
		}

		if !overlaps {
			return x, y
		}
	}

	return -1, -1
}