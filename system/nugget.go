package system

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

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	nuggetStore *engine.Store[component.NuggetComponent]
	energyStore *engine.Store[component.EnergyComponent]
	charStore   *engine.Store[component.CharacterComponent]

	nuggetID           atomic.Int32
	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity

	statActive    *atomic.Bool
	statSpawned   *atomic.Int64
	statCollected *atomic.Int64
	statJumps     *atomic.Int64
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &NuggetSystem{
		world: world,
		res:   res,

		nuggetStore: engine.GetStore[component.NuggetComponent](world),
		energyStore: engine.GetStore[component.EnergyComponent](world),
		charStore:   engine.GetStore[component.CharacterComponent](world),

		statActive:    res.Status.Bools.Get("nugget.active"),
		statSpawned:   res.Status.Ints.Get("nugget.spawned"),
		statCollected: res.Status.Ints.Get("nugget.collected"),
		statJumps:     res.Status.Ints.Get("nugget.jumps"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *NuggetSystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *NuggetSystem) initLocked() {
	s.nuggetID.Store(0)
	s.lastSpawnAttempt = time.Time{}
	s.activeNuggetEntity = 0
	s.statActive.Store(false)
	s.statSpawned.Store(0)
	s.statCollected.Store(0)
	s.statJumps.Store(0)
}

// Priority returns the system's priority
func (s *NuggetSystem) Priority() int {
	return constant.PriorityNugget
}

// EventTypes returns the event types NuggetSystem handles
func (s *NuggetSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventNuggetJumpRequest,
		event.EventNuggetCollected,
		event.EventNuggetDestroyed,
		event.EventGameReset,
	}
}

// HandleEvent processes nugget-related events
func (s *NuggetSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventNuggetJumpRequest:
		s.handleJumpRequest()

	case event.EventNuggetCollected:
		if payload, ok := ev.Payload.(*event.NuggetCollectedPayload); ok {
			s.mu.Lock()
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
			s.mu.Unlock()
		}
		s.statCollected.Add(1)

	case event.EventNuggetDestroyed:
		if payload, ok := ev.Payload.(*event.NuggetDestroyedPayload); ok {
			s.mu.Lock()
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
			s.mu.Unlock()
		}

	case event.EventGameReset:
		s.Init()
	}
}

// Update runs the nugget system logic
func (s *NuggetSystem) Update() {
	now := s.res.Time.GameTime

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constant.NuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget()
		}
		return
	}

	// Validate entity still exists
	if !s.nuggetStore.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	s.statActive.Store(s.activeNuggetEntity != 0)
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest() {
	cursorEntity := s.res.Cursor.Entity

	// 1. Check Energy from component
	energyComp, ok := s.energyStore.Get(cursorEntity)
	if !ok || energyComp.Current.Load() < constant.NuggetJumpCost {
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
	s.world.Positions.Add(cursorEntity, component.PositionComponent{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	// 5. Pay Energy Cost
	s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
		Delta: -constant.NuggetJumpCost,
	})

	// 6. Play Sound
	if audioRes, ok := engine.GetResource[*engine.AudioResource](s.world.Resources); ok && audioRes.Player != nil {
		audioRes.Player.Play(core.SoundBell)
	}

	s.statJumps.Add(1)
}

// spawnNugget creates a new nugget at a random valid position, caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget() {
	now := s.res.Time.GameTime
	x, y := s.findValidPosition()
	if x < 0 || y < 0 {
		return
	}

	nuggetID := s.nuggetID.Add(1)
	entity := s.world.CreateEntity()

	pos := component.PositionComponent{
		X: x,
		Y: y,
	}

	randomChar := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
	// Visual component (renderer uses this)
	char := component.CharacterComponent{
		Rune:  randomChar,
		Color: component.ColorNugget,
		Style: component.StyleNormal,
	}

	// Interaction component (typing system uses this)
	typeable := component.TypeableComponent{
		Char:  randomChar,
		Type:  component.TypeNugget,
		Level: component.LevelNormal,
	}

	nugget := component.NuggetComponent{
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
	engine.GetStore[component.TypeableComponent](s.world).Add(entity, typeable)
	s.nuggetStore.Add(entity, nugget)

	s.activeNuggetEntity = entity

	s.statSpawned.Add(1)
}

// findValidPosition finds a valid random position for a nugget
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.res.Config
	cursorPos, ok := s.world.Positions.Get(s.res.Cursor.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < constant.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		if math.Abs(float64(x-cursorPos.X)) <= constant.CursorExclusionX || math.Abs(float64(y-cursorPos.Y)) <= constant.CursorExclusionY {
			continue
		}

		if s.world.Positions.HasAny(x, y) {
			continue
		}

		return x, y
	}

	return -1, -1
}