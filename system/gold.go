package system

// @lixen: #dev{feature[dust(render,system)]}

import (
	"math"
	"math/rand"
	"sync"
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
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	// Cached stores (resolved once at construction)
	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	glyphStore  *engine.Store[component.GlyphComponent]
	protStore   *engine.Store[component.ProtectionComponent]
	heatStore   *engine.Store[component.HeatComponent]

	// Internal state
	active       bool
	anchorEntity core.Entity // Phantom Head
	startTime    time.Time
	timeoutTime  time.Time
	spawnEnabled bool

	// Cached metric pointers
	statActive   *atomic.Bool
	statAnchorID *atomic.Int64
	statTimer    *atomic.Int64

	enabled bool
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &GoldSystem{
		world: world,
		res:   res,

		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		memberStore: engine.GetStore[component.MemberComponent](world),
		glyphStore:  engine.GetStore[component.GlyphComponent](world),
		protStore:   engine.GetStore[component.ProtectionComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),

		statActive:   res.Status.Bools.Get("gold.active"),
		statAnchorID: res.Status.Ints.Get("gold.anchor_id"),
		statTimer:    res.Status.Ints.Get("gold.timer"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *GoldSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *GoldSystem) initLocked() {
	s.active = false
	s.anchorEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.spawnEnabled = true
	s.statActive.Store(false)
	s.statAnchorID.Store(0)
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
			s.mu.Lock()
			s.spawnEnabled = payload.Enabled
			s.mu.Unlock()
		}

	case event.EventGoldSpawnRequest:
		s.mu.RLock()
		enabled := s.spawnEnabled
		active := s.active
		s.mu.RUnlock()

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

		s.mu.RLock()
		isGoldAnchor := payload.AnchorID == s.anchorEntity
		s.mu.RUnlock()

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

	now := s.res.Time.GameTime

	s.mu.Lock()
	active := s.active
	timeoutTime := s.timeoutTime
	anchorEntity := s.anchorEntity
	s.mu.Unlock()

	// Publish metrics
	s.statActive.Store(active)
	if active {
		remaining := timeoutTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		s.statTimer.Store(int64(remaining))
		s.statAnchorID.Store(int64(s.anchorEntity))
	} else {
		s.statTimer.Store(0)
	}

	if !active {
		return
	}

	// Check if composite still exists (external destruction detection)
	if anchorEntity != 0 {
		header, ok := s.headerStore.Get(anchorEntity)
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

// spawnGold creates a new gold sequence
func (s *GoldSystem) spawnGold() bool {
	now := s.res.Time.GameTime

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

	// Phase 1: Create Phantom Head entity (NO position yet)
	anchorEntity := s.world.CreateEntity()

	// Phase 2: Create member entities
	type entityData struct {
		entity core.Entity
		pos    component.PositionComponent
		offset int8
	}
	entities := make([]entityData, 0, constant.GoldSequenceLength)
	// Create member entities
	members := make([]component.MemberEntry, 0, constant.GoldSequenceLength)

	// Set position component to gold entities
	for i := 0; i < constant.GoldSequenceLength; i++ {
		entity := s.world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos:    component.PositionComponent{X: x + i, Y: y},
			offset: int8(i),
		})
	}

	// Phase 3: Batch position commit (anchor NOT in grid - no collision at x,y)
	batch := s.world.Positions.BeginBatch()
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.Commit(); err != nil {
		for _, ed := range entities {
			s.world.DestroyEntity(ed.entity)
		}
		s.world.DestroyEntity(anchorEntity)
		return false
	}

	// Phase 4: Set Phantom Head to Positions AFTER batch success
	// Direct Set bypasses HasAny validation, colocates with member 0
	// TODO: check protectAll, it may conflicts with OOB bound, set specific protections
	s.world.Positions.Set(anchorEntity, component.PositionComponent{X: x, Y: y})
	s.protStore.Set(anchorEntity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	// Phase 5: Set components to members
	for i, ed := range entities {
		// Typing target
		s.glyphStore.Set(ed.entity, component.GlyphComponent{
			Rune:  sequence[i],
			Type:  component.GlyphGold,
			Level: component.GlyphBright,
		})

		// Composite membership
		s.memberStore.Set(ed.entity, component.MemberComponent{
			AnchorID: anchorEntity,
		})

		// Protect gold entities from decay/delete
		s.protStore.Set(ed.entity, component.ProtectionComponent{
			Mask: component.ProtectFromDelete | component.ProtectFromDecay,
		})

		// Set gold entity to composite member entities
		members = append(members, component.MemberEntry{
			Entity:  ed.entity,
			OffsetX: ed.offset,
			OffsetY: 0,
			Layer:   component.LayerCore,
		})
	}

	// Phase 6: Create composite header
	s.headerStore.Set(anchorEntity, component.CompositeHeaderComponent{
		BehaviorID: component.BehaviorGold,
		Members:    members,
	})

	// Phase 7: Activate internal state
	s.mu.Lock()
	s.active = true
	s.anchorEntity = anchorEntity
	s.startTime = now
	s.timeoutTime = now.Add(constant.GoldDuration)
	s.mu.Unlock()

	// Emit spawnLightning event
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		AnchorEntity: anchorEntity,
		OriginX:      x,
		OriginY:      y,
		Length:       constant.GoldSequenceLength,
		Duration:     constant.GoldDuration,
	})

	return true
}

// handleMemberTyped processes a gold character being typed
func (s *GoldSystem) handleMemberTyped(payload *event.MemberTypedPayload) {
	s.mu.RLock()
	if !s.active || payload.AnchorID != s.anchorEntity {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	// Check if sequence complete
	if payload.RemainingCount == 0 {
		s.handleGoldComplete()
	}
}

// handleGoldComplete processes successful gold sequence completion
func (s *GoldSystem) handleGoldComplete() {
	s.mu.RLock()
	anchorEntity := s.anchorEntity
	s.mu.RUnlock()

	// Check heat for cleaner trigger
	cursorEntity := s.res.Cursor.Entity
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		if hc.Current.Load() >= constant.MaxHeat {
			// At max head trigger cleaners
			s.world.PushEvent(event.EventCleanerSweepingRequest, nil)
		} else {
			// Fill heat to max
			s.world.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: constant.MaxHeat})
		}
	} else {
		panic("heat store doesn't exist")
	}

	// Emit completion event
	s.world.PushEvent(event.EventGoldComplete, &event.GoldCompletionPayload{
		AnchorEntity: anchorEntity,
	})

	// Play sound
	if s.res.Audio != nil && s.res.Audio.Player != nil {
		s.res.Audio.Player.Play(core.SoundCoin)
	}

	// Destroy composite
	s.destroyComposite(anchorEntity)

	s.mu.Lock()
	s.active = false
	s.anchorEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.mu.Unlock()

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.statAnchorID.Store(0)
}

// handleGoldTimeout processes gold sequence expiration
func (s *GoldSystem) handleGoldTimeout() {
	s.mu.RLock()
	anchorEntity := s.anchorEntity
	s.mu.RUnlock()

	s.world.PushEvent(event.EventGoldTimeout, &event.GoldCompletionPayload{
		AnchorEntity: anchorEntity,
	})

	s.destroyComposite(anchorEntity)

	s.mu.Lock()
	s.active = false
	s.anchorEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.mu.Unlock()

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.statAnchorID.Store(0)
}

// handleGoldDestroyed processes external gold destruction
func (s *GoldSystem) handleGoldDestroyed() {
	s.mu.RLock()
	anchorEntity := s.anchorEntity
	s.mu.RUnlock()

	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		AnchorEntity: anchorEntity,
	})

	if anchorEntity != 0 {
		s.destroyComposite(anchorEntity)
	}

	s.mu.Lock()
	s.active = false
	s.anchorEntity = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.mu.Unlock()

	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.statAnchorID.Store(0)
}

// destroyCurrentGold destroys the current gold if active
func (s *GoldSystem) destroyCurrentGold() {
	s.mu.RLock()
	anchorEntity := s.anchorEntity
	active := s.active
	s.mu.RUnlock()

	if active && anchorEntity != 0 {
		s.destroyComposite(anchorEntity)
	}
}

// destroyComposite removes phantom head and all members
func (s *GoldSystem) destroyComposite(anchorEntity core.Entity) {
	header, ok := s.headerStore.Get(anchorEntity)
	if !ok {
		return
	}

	// Collect living members for batch death
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

	// Remove protection and destroy phantom head
	s.protStore.Remove(anchorEntity)
	s.headerStore.Remove(anchorEntity)
	s.world.DestroyEntity(anchorEntity)
}

// countLivingMembers returns count of non-tombstone members
func (s *GoldSystem) countLivingMembers(header *component.CompositeHeaderComponent) int {
	count := 0
	for _, m := range header.Members {
		if m.Entity != 0 {
			count++
		}
	}
	return count
}

// findValidPosition finds a valid random position for the gold sequence
// Caller must NOT hold s.mu lock
func (s *GoldSystem) findValidPosition(seqLength int) (int, int) {
	config := s.res.Config
	cursorPos, ok := s.world.Positions.Get(s.res.Cursor.Entity)
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