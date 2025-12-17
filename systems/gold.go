package systems

import (
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// GoldSystem manages the gold sequence mechanic autonomously
type GoldSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.CoreResources

	// Cached stores (resolved once at construction)
	goldStore *engine.Store[components.GoldSequenceComponent]
	seqStore  *engine.Store[components.SequenceComponent]
	charStore *engine.Store[components.CharacterComponent]
	protStore *engine.Store[components.ProtectionComponent]

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
	return &GoldSystem{
		world: world,
		res:   res,

		goldStore: engine.GetStore[components.GoldSequenceComponent](world),
		seqStore:  engine.GetStore[components.SequenceComponent](world),
		charStore: engine.GetStore[components.CharacterComponent](world),
		protStore: engine.GetStore[components.ProtectionComponent](world),

		nextSeqID:    1,
		spawnEnabled: true,
		statActive:   res.Status.Bools.Get("gold.active"),
		statSeqID:    res.Status.Ints.Get("gold.seq_id"),
		statTimer:    res.Status.Ints.Get("gold.timer"),
	}
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return constants.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGoldEnable,
		events.EventGoldSpawnRequest,
		events.EventGoldComplete,
		events.EventGoldDestroyed,
		events.EventGameReset,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventGoldSpawnRequest:
		s.mu.RLock()
		enabled := s.spawnEnabled
		active := s.active
		s.mu.RUnlock()

		if !enabled || active {
			s.world.PushEvent(events.EventGoldSpawnFailed, nil)
			return
		}

		if s.spawnGold() {
			// EventGoldSpawned emitted inside spawnGold
		} else {
			s.world.PushEvent(events.EventGoldSpawnFailed, nil)
		}

	case events.EventGoldComplete:
		if payload, ok := event.Payload.(*events.GoldCompletionPayload); ok {
			s.handleCompletion(s.world, payload.SequenceID)
		}

	case events.EventGoldDestroyed:
		if payload, ok := event.Payload.(*events.GoldCompletionPayload); ok {
			s.handleDestroyed(s.world, payload.SequenceID)
		}

		// TODO: implement enabled event for all systems
	case events.EventGoldEnable:
		if payload, ok := event.Payload.(*events.GoldEnablePayload); ok {
			s.mu.Lock()
			s.spawnEnabled = payload.Enabled
			s.mu.Unlock()
		}

	case events.EventGameReset:
		s.mu.Lock()
		s.active = false
		s.sequenceID = 0
		s.startTime = time.Time{}
		s.timeoutTime = time.Time{}
		s.mu.Unlock()

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
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	}

	// Caller must NOT hold s.mu lock
	x, y := s.findValidPosition(constants.GoldSequenceLength)

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
		pos    components.PositionComponent
		char   components.CharacterComponent
		seq    components.SequenceComponent
	}

	entities := make([]entityData, 0, constants.GoldSequenceLength)

	for i := 0; i < constants.GoldSequenceLength; i++ {
		entity := s.world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos:    components.PositionComponent{X: x + i, Y: y},
			char: components.CharacterComponent{
				Rune: sequence[i],
				// Color defaults to ColorNone, renderer uses SeqType/SeqLevel
				Style:    components.StyleNormal,
				SeqType:  components.SequenceGold,
				SeqLevel: components.LevelBright,
			},
			seq: components.SequenceComponent{
				ID:    sequenceID,
				Index: i,
				Type:  components.SequenceGold,
				Level: components.LevelBright,
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
		s.protStore.Add(ed.entity, components.ProtectionComponent{
			Mask: components.ProtectFromDelete | components.ProtectFromDecay,
		})
	}

	// Activate internal state
	s.mu.Lock()
	s.active = true
	s.sequenceID = sequenceID
	s.startTime = now
	s.timeoutTime = now.Add(constants.GoldDuration)
	s.mu.Unlock()

	// Emit spawn event
	s.world.PushEvent(events.EventGoldSpawned, &events.GoldSpawnedPayload{
		SequenceID: sequenceID,
		OriginX:    x,
		OriginY:    y,
		Length:     constants.GoldSequenceLength,
		Duration:   constants.GoldDuration,
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
	for _, entity := range entities {
		seq, ok := s.seqStore.Get(entity)
		if !ok {
			continue
		}
		// Only remove gold sequence entities with provided ID
		if seq.Type == components.SequenceGold && (sequenceID == 0 || seq.ID == sequenceID) {
			s.world.DestroyEntity(entity)
		}
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
		s.world.PushEvent(events.EventGoldTimeout, &events.GoldCompletionPayload{
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

	for attempt := 0; attempt < constants.GoldSpawnMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-cursorPos.X)) <= constants.CursorExclusionX ||
			math.Abs(float64(y-cursorPos.Y)) <= constants.CursorExclusionY {
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