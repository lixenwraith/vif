package systems

import (
	"math"
	"math/rand"
	"strconv"
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

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu                 sync.RWMutex
	world              *engine.World
	nuggetID           atomic.Int32
	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(world *engine.World) *NuggetSystem {
	return &NuggetSystem{
		world: world,
	}
}

// Priority returns the system's priority
func (s *NuggetSystem) Priority() int {
	return constants.PriorityNugget
}

// EventTypes returns the event types NuggetSystem handles
func (s *NuggetSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventNuggetJumpRequest,
		events.EventNuggetCollected,
		events.EventNuggetDestroyed,
		events.EventGameReset,
	}
}

// HandleEvent processes nugget-related events
func (s *NuggetSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventNuggetJumpRequest:
		s.handleJumpRequest(world, event.Timestamp)

	case events.EventNuggetCollected:
		if payload, ok := event.Payload.(*events.NuggetCollectedPayload); ok {
			s.mu.Lock()
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
			s.mu.Unlock()
		}

	case events.EventNuggetDestroyed:
		if payload, ok := event.Payload.(*events.NuggetDestroyedPayload); ok {
			s.mu.Lock()
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
			s.mu.Unlock()
		}

	case events.EventGameReset:
		s.mu.Lock()
		s.activeNuggetEntity = 0
		s.lastSpawnAttempt = time.Time{}
		s.mu.Unlock()
	}
}

func (s *NuggetSystem) pushEvent(eventType events.EventType, payload any, now time.Time) {
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

// Update runs the nugget system logic
func (s *NuggetSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constants.NuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget(world, now)
		}
		return
	}

	// Validate entity still exists
	if !world.Nuggets.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest(world *engine.World, now time.Time) {
	cursorRes := engine.MustGetResource[*engine.CursorResource](world.Resources)

	// 1. Check Energy from component
	energyComp, ok := world.Energies.Get(cursorRes.Entity)
	if !ok || energyComp.Current.Load() < constants.NuggetJumpCost {
		return
	}

	// 2. Check Active Nugget
	s.mu.RLock()
	nuggetEntity := s.activeNuggetEntity
	s.mu.RUnlock()

	if nuggetEntity == 0 {
		return
	}

	// 3. Get Nugget Position
	nuggetPos, ok := world.Positions.Get(nuggetEntity)
	if !ok {
		// Stale reference - clear it
		s.mu.Lock()
		if s.activeNuggetEntity == nuggetEntity {
			s.activeNuggetEntity = 0
		}
		s.mu.Unlock()
		return
	}

	// 4. Move Cursor
	world.Positions.Add(cursorRes.Entity, components.PositionComponent{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	// 5. Pay Energy Cost
	s.pushEvent(events.EventEnergyAdd, &events.EnergyAddPayload{
		Delta: -constants.NuggetJumpCost,
	}, now)

	// 6. Play Sound
	if audioRes, ok := engine.GetResource[*engine.AudioResource](world.Resources); ok && audioRes.Player != nil {
		audioRes.Player.Play(audio.SoundBell)
	}
}

// spawnNugget creates a new nugget at a random valid position using generic stores
// Caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget(world *engine.World, now time.Time) {
	x, y := s.findValidPosition(world)
	if x < 0 || y < 0 {
		return
	}

	nuggetID := s.nuggetID.Add(1)
	entity := world.CreateEntity()

	pos := components.PositionComponent{
		X: x,
		Y: y,
	}

	randomChar := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	char := components.CharacterComponent{
		Rune: randomChar,
		// Use semantic color
		Color: components.ColorNugget,
		Style: components.StyleNormal,
		// SeqType/SeqLevel default to zero values (unused for nuggets)
	}

	nugget := components.NuggetComponent{
		ID:        int(nuggetID),
		SpawnTime: now,
	}

	// Use batch for atomic position validation
	batch := world.Positions.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Position was taken while we were creating the nugget
		world.DestroyEntity(entity)
		return
	}

	// Add other components after position is committed
	world.Characters.Add(entity, char)
	world.Nuggets.Add(entity, nugget)

	s.activeNuggetEntity = entity
}

// findValidPosition finds a valid random position for a nugget
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition(world *engine.World) (int, int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	cursorRes := engine.MustGetResource[*engine.CursorResource](world.Resources)

	cursorPos, ok := world.Positions.Get(cursorRes.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < constants.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		if math.Abs(float64(x-cursorPos.X)) <= constants.CursorExclusionX || math.Abs(float64(y-cursorPos.Y)) <= constants.CursorExclusionY {
			continue
		}

		if world.Positions.HasAny(x, y) {
			continue
		}

		return x, y
	}

	return -1, -1
}

// GetSystemState returns a debug string describing the current system state
func (s *NuggetSystem) GetSystemState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.activeNuggetEntity == 0 {
		now := time.Now()
		timeSinceLastSpawn := now.Sub(s.lastSpawnAttempt)
		timeUntilNext := (constants.NuggetSpawnIntervalSeconds * time.Second) - timeSinceLastSpawn
		if timeUntilNext < 0 {
			timeUntilNext = 0
		}
		return "Nugget[inactive, nextSpawn=" + timeUntilNext.Round(100*time.Millisecond).String() + "]"
	}

	return "Nugget[active, entityID=" + strconv.Itoa(int(s.activeNuggetEntity)) + "]"
}