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

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.CoreResources

	nuggetStore *engine.Store[components.NuggetComponent]
	energyStore *engine.Store[components.EnergyComponent]
	charStore   *engine.Store[components.CharacterComponent]

	nuggetID           atomic.Int32
	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(world *engine.World) engine.System {
	return &NuggetSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		nuggetStore: engine.GetStore[components.NuggetComponent](world),
		energyStore: engine.GetStore[components.EnergyComponent](world),
		charStore:   engine.GetStore[components.CharacterComponent](world),
	}
}

// Init
func (s *NuggetSystem) Init() {}

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
func (s *NuggetSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventNuggetJumpRequest:
		s.handleJumpRequest()

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

// Update runs the nugget system logic
func (s *NuggetSystem) Update() {
	now := s.res.Time.GameTime

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constants.NuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget()
		}
		return
	}

	// Validate entity still exists
	if !s.nuggetStore.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest() {
	cursorEntity := s.res.Cursor.Entity

	// 1. Check Energy from component
	energyComp, ok := s.energyStore.Get(cursorEntity)
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
	nuggetPos, ok := s.world.Positions.Get(nuggetEntity)
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
	s.world.Positions.Add(cursorEntity, components.PositionComponent{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	// 5. Pay Energy Cost
	s.world.PushEvent(events.EventEnergyAdd, &events.EnergyAddPayload{
		Delta: -constants.NuggetJumpCost,
	})

	// 6. Play Sound
	if audioRes, ok := engine.GetResource[*engine.AudioResource](s.world.Resources); ok && audioRes.Player != nil {
		audioRes.Player.Play(audio.SoundBell)
	}
}

// spawnNugget creates a new nugget at a random valid position using generic stores
// Caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget() {
	now := s.res.Time.GameTime
	x, y := s.findValidPosition()
	if x < 0 || y < 0 {
		return
	}

	nuggetID := s.nuggetID.Add(1)
	entity := s.world.CreateEntity()

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
	batch := s.world.Positions.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Position was taken while we were creating the nugget
		s.world.DestroyEntity(entity)
		return
	}

	// Add other components after position is committed
	s.charStore.Add(entity, char)
	s.nuggetStore.Add(entity, nugget)

	s.activeNuggetEntity = entity
}

// findValidPosition finds a valid random position for a nugget
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.res.Config
	cursorPos, ok := s.world.Positions.Get(s.res.Cursor.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < constants.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		if math.Abs(float64(x-cursorPos.X)) <= constants.CursorExclusionX || math.Abs(float64(y-cursorPos.Y)) <= constants.CursorExclusionY {
			continue
		}

		if s.world.Positions.HasAny(x, y) {
			continue
		}

		return x, y
	}

	return -1, -1
}