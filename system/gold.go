package system

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// GoldSystem manages the gold sequence mechanic autonomously
type GoldSystem struct {
	world *engine.World

	rng *vmath.FastRand

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
	s.rng = vmath.NewFastRand(uint64(time.Now().UnixNano()))
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
		event.EventCompositeMemberDestroyed,
		event.EventCompositeIntegrityBreach,
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
		s.cancelGold()

	case event.EventGoldJumpRequest:
		s.handleJumpRequest()

	case event.EventGoldSpawnRequest:
		if !s.spawnEnabled || s.active {
			s.world.PushEvent(event.EventGoldSpawnFailed, nil)
			return
		}
		if !s.spawnGold() {
			s.world.PushEvent(event.EventGoldSpawnFailed, nil)
		}

	case event.EventCompositeMemberDestroyed:
		if payload, ok := ev.Payload.(*event.CompositeMemberDestroyedPayload); ok {
			if payload.HeaderEntity == s.headerEntity && payload.RemainingCount == 0 {
				s.handleGoldComplete()
			}
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.HeaderEntity == s.headerEntity {
				// Gold: any external member loss = full destruction
				s.handleGoldDestroyed()
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

	s.statActive.Store(s.active)
	if s.active {
		remaining := s.timeoutTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		s.statTimer.Store(int64(remaining))
		s.stateHeaderEntity.Store(int64(s.headerEntity))
	} else {
		s.statTimer.Store(0)
		return
	}

	// Timeout check only - integrity handled via event
	if now.After(s.timeoutTime) {
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
		Delta:      parameter.GoldJumpCostPercent,
		Percentage: true,
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
		sequence[i] = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
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
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
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
		Color:        visual.RgbSplashWhite,
		MarginRight:  parameter.GoldSequenceLength,
		MarginBottom: 1, // One line height
		Duration:     parameter.GoldDuration,
	})

	return true
}

// handleMemberTyped processes a gold character being typed
func (s *GoldSystem) handleMemberTyped(payload *event.CompositeMemberDestroyedPayload) {
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
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	// Emit completion event, FSM is the reward authority
	s.world.PushEvent(event.EventGoldCompleted, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	// Play sound
	if s.world.Resources.Audio != nil && s.world.Resources.Audio.Player != nil {
		s.world.Resources.Audio.Player.Play(core.SoundCoin)
	}

	// Silent destruction - members already dead from typing
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// handleGoldTimeout processes gold sequence expiration
func (s *GoldSystem) handleGoldTimeout() {
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	s.world.PushEvent(event.EventGoldTimeout, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// handleGoldDestroyed processes external gold destruction
func (s *GoldSystem) handleGoldDestroyed() {
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	// Emit event for FSM
	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	// Request centralized destruction with flash effect
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       event.EventFlashSpawnOneRequest,
	})

	s.clearState()
}

// cancelGold handles explicit cancellation
func (s *GoldSystem) cancelGold() {
	if !s.active || s.headerEntity == 0 {
		return
	}

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: s.headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// clearState resets gold tracking
func (s *GoldSystem) clearState() {
	s.active = false
	s.headerEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.stateHeaderEntity.Store(0)
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
		x := s.rng.Intn(config.MapWidth)
		y := s.rng.Intn(config.MapHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-cursorPos.X)) <= parameter.CursorExclusionX ||
			math.Abs(float64(y-cursorPos.Y)) <= parameter.CursorExclusionY {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > config.MapWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if s.world.Positions.IsBlocked(x+i, y, component.WallBlockParticle) {
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