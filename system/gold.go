package system

import (
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
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
	res   engine.CoreResources

	// Cached stores (resolved once at construction)
	goldStore *engine.Store[component.GoldSequenceComponent]
	seqStore  *engine.Store[component.SequenceComponent]
	charStore *engine.Store[component.CharacterComponent]
	protStore *engine.Store[component.ProtectionComponent]

	// Internal state
	active       bool
	sequenceID   int
	startTime    time.Time
	timeoutTime  time.Time
	spawnEnabled bool

	// Sequence ID generator
	nextSeqID int

	// Cached metric pointers
	statActive *atomic.Bool
	statSeqID  *atomic.Int64
	statTimer  *atomic.Int64
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(world *engine.World) engine.System {
	res := engine.GetCoreResources(world)
	s := &GoldSystem{
		world: world,
		res:   res,

		goldStore: engine.GetStore[component.GoldSequenceComponent](world),
		seqStore:  engine.GetStore[component.SequenceComponent](world),
		charStore: engine.GetStore[component.CharacterComponent](world),
		protStore: engine.GetStore[component.ProtectionComponent](world),

		nextSeqID:  1,
		statActive: res.Status.Bools.Get("gold.active"),
		statSeqID:  res.Status.Ints.Get("gold.seq_id"),
		statTimer:  res.Status.Ints.Get("gold.timer"),
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
	s.sequenceID = 0
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.spawnEnabled = true
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
		event.EventGoldComplete,
		event.EventGoldDestroyed,
		event.EventGameReset,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
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

	case event.EventGoldComplete:
		if payload, ok := ev.Payload.(*event.GoldCompletionPayload); ok {
			s.handleCompletion(s.world, payload.SequenceID)
		}

	case event.EventGoldDestroyed:
		if payload, ok := ev.Payload.(*event.GoldCompletionPayload); ok {
			s.handleDestroyed(s.world, payload.SequenceID)
		}

		// TODO: implement enabled event for all systems
	case event.EventGoldEnable:
		if payload, ok := ev.Payload.(*event.GoldEnablePayload); ok {
			s.mu.Lock()
			s.spawnEnabled = payload.Enabled
			s.mu.Unlock()
		}

	case event.EventGameReset:
		s.Init()

		s.statActive.Store(false)
		s.statSeqID.Store(0)
		s.statTimer.Store(0)
	}
}

// Update runs the gold sequence system logic
func (s *GoldSystem) Update() {
	now := s.res.Time.GameTime

	s.mu.Lock()
	active := s.active
	timeoutTime := s.timeoutTime
	seqID := s.sequenceID
	s.mu.Unlock()

	// Publish metrics (direct atomic writes)
	s.statActive.Store(active)
	if active {
		remaining := timeoutTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		s.statTimer.Store(int64(remaining))
		s.statSeqID.Store(int64(seqID))
	} else {
		s.statTimer.Store(0)
	}

	// Timeout check
	if active && now.After(timeoutTime) {
		s.failSequence(seqID, true)
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

	// Caller must NOT hold s.mu lock
	x, y := s.findValidPosition(constant.GoldSequenceLength)

	if x < 0 || y < 0 {
		return false
	}

	// Increment sequence ID for new spawn
	s.mu.Lock()
	s.nextSeqID++
	s.mu.Unlock()
	sequenceID := s.nextSeqID

	// Create entities
	type entityData struct {
		entity core.Entity
		pos    component.PositionComponent
		char   component.CharacterComponent
		seq    component.SequenceComponent
	}

	entities := make([]entityData, 0, constant.GoldSequenceLength)

	for i := 0; i < constant.GoldSequenceLength; i++ {
		entity := s.world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos:    component.PositionComponent{X: x + i, Y: y},
			char: component.CharacterComponent{
				Rune: sequence[i],
				// Color defaults to ColorNone, renderer uses SeqType/SeqLevel
				Style:    component.StyleNormal,
				SeqType:  component.SequenceGold,
				SeqLevel: component.LevelBright,
			},
			seq: component.SequenceComponent{
				ID:    sequenceID,
				Index: i,
				Type:  component.SequenceGold,
				Level: component.LevelBright,
			},
		})
	}

	// Batch position commit
	batch := s.world.Positions.BeginBatch()
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.Commit(); err != nil {
		// Collision detected - cleanup entities
		for _, ed := range entities {
			s.world.DestroyEntity(ed.entity)
		}
		return false
	}

	// Add components using cached stores
	for _, ed := range entities {
		s.charStore.Add(ed.entity, ed.char)
		s.seqStore.Add(ed.entity, ed.seq)
		// Gold entities are protected from deletion AND decay
		s.protStore.Add(ed.entity, component.ProtectionComponent{
			Mask: component.ProtectFromDelete | component.ProtectFromDecay,
		})
	}

	// Activate internal state
	s.mu.Lock()
	s.active = true
	s.sequenceID = sequenceID
	s.startTime = now
	s.timeoutTime = now.Add(constant.GoldDuration)
	s.mu.Unlock()

	// Emit spawn event
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		SequenceID: sequenceID,
		OriginX:    x,
		OriginY:    y,
		Length:     constant.GoldSequenceLength,
		Duration:   constant.GoldDuration,
	})

	return true
}

// removeGold removes all gold sequence entities
func (s *GoldSystem) removeGold(sequenceID int) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	entities := s.seqStore.All()
	var toDestroy []core.Entity
	for _, entity := range entities {
		seq, ok := s.seqStore.Get(entity)
		if !ok {
			continue
		}
		// Only remove gold sequence entities with provided ID
		if seq.Type == component.SequenceGold && (sequenceID == 0 || seq.ID == sequenceID) {
			toDestroy = append(toDestroy, entity)
		}
	}

	if len(toDestroy) > 0 {
		s.world.PushEvent(event.EventRequestDeath, &event.DeathRequestPayload{
			Entities:    toDestroy,
			EffectEvent: 0,
		})
	}

	s.mu.Lock()
	s.active = false
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.mu.Unlock()

	s.statActive.Store(false)
	s.statTimer.Store(0)
}

// failSequence handles gold failure (timeout or destruction)
func (s *GoldSystem) failSequence(sequenceID int, isTimeout bool) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	if isTimeout {
		s.world.PushEvent(event.EventGoldTimeout, &event.GoldCompletionPayload{
			SequenceID: sequenceID,
		})
	}

	s.removeGold(sequenceID)
}

// handleCompletion processes successful gold sequence
func (s *GoldSystem) handleCompletion(world *engine.World, sequenceID int) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.removeGold(sequenceID)

	// Play sound
	if audioRes, ok := engine.GetResource[*engine.AudioResource](world.Resources); ok && audioRes.Player != nil {
		audioRes.Player.Play(audio.SoundCoin)
	}
}

// handleDestroyed processes external gold destruction
func (s *GoldSystem) handleDestroyed(world *engine.World, sequenceID int) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.removeGold(sequenceID)
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