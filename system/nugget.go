package system

import (
	"math/rand"
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
	world *engine.World

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
	s := &NuggetSystem{
		world: world,
	}

	s.statActive = world.Resource.Status.Bools.Get("nugget.active")
	s.statSpawned = world.Resource.Status.Ints.Get("nugget.spawned")
	s.statCollected = world.Resource.Status.Ints.Get("nugget.collected")
	s.statJumps = world.Resource.Status.Ints.Get("nugget.jumps")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *NuggetSystem) Init() {
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
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
		}
		s.statCollected.Add(1)

	case event.EventNuggetDestroyed:
		if payload, ok := ev.Payload.(*event.NuggetDestroyedPayload); ok {
			if s.activeNuggetEntity == payload.Entity {
				s.activeNuggetEntity = 0
			}
		}
	}
}

// Update runs the nugget system logic
func (s *NuggetSystem) Update() {
	if !s.enabled {
		return
	}

	now := s.world.Resource.Time.GameTime
	cursorEntity := s.world.Resource.Cursor.Entity

	// Validate active nugget still exists
	if s.activeNuggetEntity != 0 && !s.world.Component.Nugget.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	// Check cursor overlap for auto-collection
	if s.activeNuggetEntity != 0 {
		cursorPos, cursorOk := s.world.Position.Get(cursorEntity)
		nuggetPos, nuggetOk := s.world.Position.Get(s.activeNuggetEntity)
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
	if !s.world.Component.Nugget.Has(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	s.statActive.Store(s.activeNuggetEntity != 0)
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest() {
	cursorEntity := s.world.Resource.Cursor.Entity

	// 1. Check Energy from component
	energyComp, ok := s.world.Component.Energy.Get(cursorEntity)
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
	nuggetEntity := s.activeNuggetEntity

	if nuggetEntity == 0 {
		return
	}

	// 3. Get Nugget Position
	nuggetPos, ok := s.world.Position.Get(nuggetEntity)
	if !ok {
		// Stale reference - clear it
		if s.activeNuggetEntity == nuggetEntity {
			s.activeNuggetEntity = 0
		}
		return
	}

	// 4. Move Cursor
	s.world.Position.Set(cursorEntity, component.PositionComponent{
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
	s.world.Resource.Audio.Player.Play(core.SoundBell)

	s.statJumps.Add(1)
}

// spawnNugget creates a new nugget at a random valid position, caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget() {
	now := s.world.Resource.Time.GameTime
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
	batch := s.world.Position.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Position was taken while we were creating the nugget
		s.world.DestroyEntity(entity)
		return
	}

	// Set component after position is committed
	s.world.Component.Nugget.Set(entity, nugget)

	s.activeNuggetEntity = entity

	s.statSpawned.Add(1)
}

// findValidPosition finds a valid random position for a nugget
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.world.Resource.Config
	cursorPos, ok := s.world.Position.Get(s.world.Resource.Cursor.Entity)
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

	nuggetPos, ok := s.world.Position.Get(s.activeNuggetEntity)
	if !ok {
		return
	}

	cursorEntity := s.world.Resource.Cursor.Entity
	var currentHeat int64
	if hc, ok := s.world.Component.Heat.Get(cursorEntity); ok {
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