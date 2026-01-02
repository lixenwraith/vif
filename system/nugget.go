package system

import (
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

// NuggetSystem manages nugget spawnLightning and respawn logic
type NuggetSystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	nuggetStore *engine.Store[component.NuggetComponent]
	energyStore *engine.Store[component.EnergyComponent]
	heatStore   *engine.Store[component.HeatComponent]
	glyphStore  *engine.Store[component.GlyphComponent]

	nuggetID           atomic.Int32
	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity

	statActive    *atomic.Bool
	statSpawned   *atomic.Int64
	statCollected *atomic.Int64
	statJumps     *atomic.Int64

	enabled bool
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &NuggetSystem{
		world: world,
		res:   res,

		nuggetStore: engine.GetStore[component.NuggetComponent](world),
		energyStore: engine.GetStore[component.EnergyComponent](world),
		heatStore:   engine.GetStore[component.HeatComponent](world),
		glyphStore:  engine.GetStore[component.GlyphComponent](world),

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
	s.enabled = true
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
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

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
	}
}

// Update runs the nugget system logic
func (s *NuggetSystem) Update() {
	if !s.enabled {
		return
	}

	now := s.res.Time.GameTime
	cursorEntity := s.res.Cursor.Entity

	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate active nugget still exists
	if s.activeNuggetEntity != 0 && !s.nuggetStore.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	// Check cursor overlap for auto-collection
	if s.activeNuggetEntity != 0 {
		cursorPos, cursorOk := s.world.Positions.Get(cursorEntity)
		nuggetPos, nuggetOk := s.world.Positions.Get(s.activeNuggetEntity)
		if cursorOk && nuggetOk && cursorPos.X == nuggetPos.X && cursorPos.Y == nuggetPos.Y {
			s.collectNugget()
		}
	}

	// Spawn if no active nugget and cooldown elapsed
	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constant.NuggetSpawnIntervalSeconds {
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
	if !ok {
		return
	}

	energy := energyComp.Current.Load()
	cost := int64(constant.NuggetJumpCost)
	// Allow jump if magnitude is sufficient in either direction
	if energy < cost && energy > -cost {
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
	s.world.Positions.Set(cursorEntity, component.PositionComponent{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	// 5. Pay Energy Cost (move towards 0)
	delta := -constant.NuggetJumpCost
	if energy < 0 {
		delta = constant.NuggetJumpCost
	}

	s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
		Delta: delta,
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
	nugget := component.NuggetComponent{
		Char:      randomChar,
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

	// Set component after position is committed
	s.nuggetStore.Set(entity, nugget)

	s.activeNuggetEntity = entity

	s.statSpawned.Add(1)
}

// findValidPosition finds a valid random position for a nugget
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.res.Config
	cursorPos, ok := s.world.Positions.Get(s.res.Cursor.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < constant.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		dx := x - cursorPos.X
		if dx < 0 {
			dx = -dx
		}
		dy := y - cursorPos.Y
		if dy < 0 {
			dy = -dy
		}

		if dx <= constant.CursorExclusionX || dy <= constant.CursorExclusionY {
			continue
		}

		return x, y
	}

	return -1, -1
}

// collectNugget handles auto-collection when cursor overlaps nugget
func (s *NuggetSystem) collectNugget() {
	if s.activeNuggetEntity == 0 {
		return
	}

	nuggetPos, ok := s.world.Positions.Get(s.activeNuggetEntity)
	if !ok {
		return
	}

	cursorEntity := s.res.Cursor.Entity
	var currentHeat int64
	if hc, ok := s.heatStore.Get(cursorEntity); ok {
		currentHeat = hc.Current.Load()
	}

	if currentHeat >= constant.MaxHeat {
		s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
			OriginX: nuggetPos.X,
			OriginY: nuggetPos.Y,
		})
	} else {
		s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: constant.NuggetHeatIncrease})
	}

	// TODO: death
	s.world.DestroyEntity(s.activeNuggetEntity)
	s.activeNuggetEntity = 0

	s.statCollected.Add(1)
}