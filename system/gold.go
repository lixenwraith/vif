package system

import (
	"math"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
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

// Name returns system's name
func (s *GoldSystem) Name() string {
	return "gold"
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return parameter.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGoldSpawnRequest,
		event.EventGoldCancel,
		event.EventGoldJumpRequest,
		event.EventMemberTyped,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(ev event.GameEvent) {
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

	cursorEntity := s.world.Resources.Player.Entity

	// 1. Find target position (First living member)
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

	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	// 2. Move Cursor
	s.world.Positions.SetPosition(cursorEntity, component.PositionComponent{
		X: targetPos.X,
		Y: targetPos.Y,
	})

	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{
		X: targetPos.X,
		Y: targetPos.Y,
	})

	// 3. Pay Energy Cost (spend, non-convergent)
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Delta:      parameter.GoldJumpCost,
		Percentage: false,
		Type:       event.EnergyDeltaSpend,
	})

	// 4. Play Sound
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
	sequence := make([]rune, parameter.GoldSequenceLength)
	for i := 0; i < parameter.GoldSequenceLength; i++ {
		sequence[i] = parameter.AlphanumericRunes[rand.Intn(len(parameter.AlphanumericRunes))]
	}

	// Find empty space to spawn gold
	x, y := s.findValidPosition(parameter.GoldSequenceLength)
	if x < 0 || y < 0 {
		return false
	}

	// 1. Create Phantom Head entity (NO position yet)
	headerEntity := s.world.CreateEntity()

	// 2. Create member entities
	type entityData struct {
		entity core.Entity
		pos    component.PositionComponent
		offset int
	}
	entities := make([]entityData, 0, parameter.GoldSequenceLength)
	// Create member entities
	members := make([]component.MemberEntry, 0, parameter.GoldSequenceLength)

	// Set position component to gold entities
	for i := 0; i < parameter.GoldSequenceLength; i++ {
		entity := s.world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos:    component.PositionComponent{X: x + i, Y: y},
			offset: i,
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

	// 4. Set Phantom Head to Positions AFTER batch success
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// 5. Set components to members
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

		// Set gold entity to composite member entities
		members = append(members, component.MemberEntry{
			Entity:  ed.entity,
			OffsetX: ed.offset,
			OffsetY: 0,
			Layer:   component.LayerGlyph,
		})
	}

	// 6. Create composite header
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorGold,
		MemberEntries: members,
	})

	// 7. Activate internal state
	s.active = true
	s.headerEntity = headerEntity
	s.startTime = now
	s.timeoutTime = now.Add(parameter.GoldDuration)

	// Emit spawn event
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		HeaderEntity: headerEntity,
		Length:       parameter.GoldSequenceLength,
		Duration:     parameter.GoldDuration,
	})
	// Splash timer spawn event, no need for splash cancel event, automatically cancelled when anchor is destroyed
	s.world.PushEvent(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
		AnchorEntity: headerEntity,
		Color:        component.SplashColorWhite,
		MarginRight:  parameter.GoldSequenceLength,
		MarginBottom: 1, // One line height
		Duration:     parameter.GoldDuration,
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

	// Emit event for FSM
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
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, toDestroy)
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
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < parameter.GoldSpawnMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-cursorPos.X)) <= parameter.CursorExclusionX ||
			math.Abs(float64(y-cursorPos.Y)) <= parameter.CursorExclusionY {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > config.GameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if s.world.Positions.IsBlockedForSpawn(x+i, y) {
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