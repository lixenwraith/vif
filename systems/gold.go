package systems

import (
	"math"
	"math/rand"
	"sync"
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

	// Internal state (migrated from GameState)
	active      bool
	sequenceID  int
	startTime   time.Time
	timeoutTime time.Time

	// Sequence ID generator
	nextSeqID int
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(world *engine.World) *GoldSystem {
	return &GoldSystem{
		world:     world,
		nextSeqID: 1,
	}
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return constants.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGoldComplete,
		events.EventGoldDestroyed,
		events.EventGameReset,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventGoldComplete:
		if payload, ok := event.Payload.(*events.GoldCompletionPayload); ok {
			s.handleCompletion(world, payload.SequenceID, event.Timestamp)
		}

	case events.EventGoldDestroyed:
		if payload, ok := event.Payload.(*events.GoldCompletionPayload); ok {
			s.handleDestroyed(world, payload.SequenceID, event.Timestamp)
		}

	case events.EventGameReset:
		s.mu.Lock()
		s.active = false
		s.sequenceID = 0
		s.startTime = time.Time{}
		s.timeoutTime = time.Time{}
		s.mu.Unlock()
	}
}

// Update runs the gold sequence system logic
func (s *GoldSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	s.mu.Lock()
	active := s.active
	timeoutTime := s.timeoutTime
	seqID := s.sequenceID
	s.mu.Unlock()

	// Timeout check
	if active && now.After(timeoutTime) {
		s.failSequence(world, seqID, now, true)
		return
	}

	// Spawn check: only in PhaseNormal when inactive
	stateRes := engine.MustGetResource[*engine.GameStateResource](world.Resources)
	phase := stateRes.State.ReadPhaseState(now)

	if phase.Phase == engine.PhaseNormal && !active {
		s.spawnGold(world)
	}
}

// TODO: can we do without this
// pushEvent is resource event wrapper
func (s *GoldSystem) pushEvent(eventType events.EventType, payload any, now time.Time) {
	stateRes := engine.MustGetResource[*engine.GameStateResource](s.world.Resources)
	eqRes := engine.MustGetResource[*engine.EventQueueResource](s.world.Resources)
	event := events.GameEvent{
		Type:      eventType,
		Payload:   payload,
		Frame:     stateRes.State.GetFrameNumber(),
		Timestamp: now,
	}
	eqRes.Queue.Push(event)
}

// spawnGold creates a new gold sequence
func (s *GoldSystem) spawnGold(world *engine.World) bool {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock() // Unlock before expensive operation

	// Generate random 10-character sequence
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	}

	// Caller must NOT hold s.mu lock
	x, y := s.findValidPosition(world, constants.GoldSequenceLength)

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
		entity := world.CreateEntity()
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
	batch := world.Positions.BeginBatch()
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.Commit(); err != nil {
		// Collision detected - cleanup entities
		for _, ed := range entities {
			world.DestroyEntity(ed.entity)
		}
		return false
	}

	// Add other components (positions already committed)
	for _, ed := range entities {
		world.Characters.Add(ed.entity, ed.char)
		world.Sequences.Add(ed.entity, ed.seq)
		// Protect gold entities from delete operators
		// TODO: protect from decay here as well instead of decay system check
		world.Protections.Add(ed.entity, components.ProtectionComponent{
			Mask: components.ProtectFromDelete,
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
	s.pushEvent(events.EventGoldSpawned, &events.GoldSpawnedPayload{
		SequenceID: sequenceID,
		OriginX:    x,
		OriginY:    y,
		Length:     constants.GoldSequenceLength,
		Duration:   constants.GoldDuration,
	}, now)

	return true
}

// removeGold removes all gold sequence entities
func (s *GoldSystem) removeGold(world *engine.World, sequenceID int) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	entities := world.Sequences.All()
	for _, entity := range entities {
		seq, ok := world.Sequences.Get(entity)
		if !ok {
			continue
		}
		// Only remove gold sequence entities with provided ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			world.DestroyEntity(entity)
		}
	}

	s.mu.Lock()
	s.active = false
	s.startTime = time.Time{}
	s.timeoutTime = time.Time{}
	s.mu.Unlock()
}

// failSequence handles gold failure (timeout or destruction)
func (s *GoldSystem) failSequence(world *engine.World, sequenceID int, now time.Time, isTimeout bool) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	if isTimeout {
		s.pushEvent(events.EventGoldTimeout, &events.GoldCompletionPayload{
			SequenceID: sequenceID,
		}, now)
	}

	s.removeGold(world, sequenceID)
}

// handleCompletion processes successful gold sequence
func (s *GoldSystem) handleCompletion(world *engine.World, sequenceID int, now time.Time) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.removeGold(world, sequenceID)

	// Play sound
	if audioRes, ok := engine.GetResource[*engine.AudioResource](world.Resources); ok && audioRes.Player != nil {
		audioRes.Player.Play(audio.SoundCoin)
	}
}

// handleDestroyed processes external gold destruction
func (s *GoldSystem) handleDestroyed(world *engine.World, sequenceID int, now time.Time) {
	s.mu.RLock()
	if !s.active || sequenceID != s.sequenceID {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.removeGold(world, sequenceID)
}

// findValidPosition finds a valid random position for the gold sequence
// Caller must NOT hold s.mu lock
func (s *GoldSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	cursorRes := engine.MustGetResource[*engine.CursorResource](world.Resources)

	// Read cursor position for exclusion
	cursorPos, ok := world.Positions.Get(cursorRes.Entity)
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
			if world.Positions.HasAny(x+i, y) {
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