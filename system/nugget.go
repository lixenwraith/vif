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

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	world *engine.World

	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity
	nuggetOverload     int

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

	s.statActive = world.Resources.Status.Bools.Get("nugget.active")
	s.statSpawned = world.Resources.Status.Ints.Get("nugget.spawned")
	s.statCollected = world.Resources.Status.Ints.Get("nugget.collected")
	s.statJumps = world.Resources.Status.Ints.Get("nugget.jumps")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *NuggetSystem) Init() {
	s.lastSpawnAttempt = time.Time{}
	s.activeNuggetEntity = 0
	s.nuggetOverload = 0
	s.statActive.Store(false)
	s.statSpawned.Store(0)
	s.statCollected.Store(0)
	s.statJumps.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *NuggetSystem) Name() string {
	return "nugget"
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
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes nugget-related events
func (s *NuggetSystem) HandleEvent(ev event.GameEvent) {
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

	now := s.world.Resources.Time.GameTime
	cursorEntity := s.world.Resources.Cursor.Entity

	// Validate active nugget still exists
	if s.activeNuggetEntity != 0 && !s.world.Components.Nugget.HasEntity(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	// Check cursor overlap for auto-collection
	if s.activeNuggetEntity != 0 {
		cursorPos, cursorOk := s.world.Positions.GetPosition(cursorEntity)
		nuggetPos, nuggetOk := s.world.Positions.GetPosition(s.activeNuggetEntity)
		if cursorOk && nuggetOk && cursorPos.X == nuggetPos.X && cursorPos.Y == nuggetPos.Y {
			s.collectNugget(nuggetPos.X, nuggetPos.Y)
		}
	}

	// Spawn if no active nugget and cooldown elapsed
	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constant.NuggetSpawnInterval {
			s.lastSpawnAttempt = now
			s.spawnNugget()
		}
		return
	}

	// Validate entity still exists
	if !s.world.Components.Nugget.HasEntity(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	s.statActive.Store(s.activeNuggetEntity != 0)
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest() {
	cursorEntity := s.world.Resources.Cursor.Entity

	// 1. Check Active Nugget
	nuggetEntity := s.activeNuggetEntity

	if nuggetEntity == 0 {
		return
	}

	// 2. Get Nugget Positions
	nuggetPos, ok := s.world.Positions.GetPosition(nuggetEntity)
	if !ok {
		// Stale reference - clear it
		if s.activeNuggetEntity == nuggetEntity {
			s.activeNuggetEntity = 0
		}
		return
	}

	// 3. Move Cursor
	s.world.Positions.SetPosition(cursorEntity, component.PositionComponent{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	s.world.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{
		X: nuggetPos.X,
		Y: nuggetPos.Y,
	})

	// 4. Pay Energy Cost (spend, non-convergent)
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Delta:      constant.NuggetJumpCost,
		Percentage: false,
		Type:       event.EnergyDeltaSpend,
	})

	// 5. Collect nugget that overlaps with cursor
	s.collectNugget(nuggetPos.X, nuggetPos.Y)

	// 5. Update stats
	s.statJumps.Add(1)
}

// spawnNugget creates a new nugget at a random valid position, caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget() {
	now := s.world.Resources.Time.GameTime
	x, y := s.findValidPosition()
	if x < 0 || y < 0 {
		return
	}

	entity := s.world.CreateEntity()

	pos := component.PositionComponent{
		X: x,
		Y: y,
	}

	randomChar := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
	nugget := component.NuggetComponent{
		Char:      randomChar,
		SpawnTime: now,
	}

	// Use batch for atomic position validation
	batch := s.world.Positions.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Positions was taken while we were creating the nugget
		s.world.DestroyEntity(entity)
		return
	}

	// SetPosition component after position is committed
	s.world.Components.Nugget.SetComponent(entity, nugget)
	// Render component
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  randomChar,
		Color: component.SigilNugget,
	})

	s.activeNuggetEntity = entity

	s.statSpawned.Add(1)

	// Emit directional cleaners on spawn
	s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
		OriginX: x,
		OriginY: y,
	})
}

// findValidPosition finds a valid random position for a nugget
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.world.Resources.Config
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Cursor.Entity)
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
func (s *NuggetSystem) collectNugget(nuggetPosX, nuggetPosY int) {
	// 1. Play Sound
	s.world.Resources.Audio.Player.Play(core.SoundBell)

	// 2. Emit directional cleaner and nugget death events
	event.EmitDeathOne(s.world.Resources.Event.Queue, s.activeNuggetEntity, 0)
	s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
		OriginX: nuggetPosX,
		OriginY: nuggetPosY,
	})

	heatComp, ok := s.world.Components.Heat.GetComponent(s.world.Resources.Cursor.Entity)
	if ok {
		if heatComp.Current == constant.MaxHeat {
			s.nuggetOverload++
			if s.nuggetOverload >= constant.NuggetOverloadCount {
				s.world.PushEvent(event.EventNuggetOverloadNotification, nil)
				s.nuggetOverload = 0
			}
		} else {
			s.world.PushEvent(event.EventHeatAdd, &event.HeatAddPayload{Delta: constant.NuggetHeatIncrease})
		}
	}

	// 3. Update system state and stats
	s.activeNuggetEntity = 0
	s.statCollected.Add(1)
}