package system

import (
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	world *engine.World

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
	return parameter.PriorityNugget
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
	dt := s.world.Resources.Time.DeltaTime

	// Validate active nugget still exists
	if s.activeNuggetEntity != 0 && !s.world.Components.Nugget.HasEntity(s.activeNuggetEntity) {
		s.activeNuggetEntity = 0
	}

	// Check for auto-collection (ember/shield area or exact co-location)
	if s.activeNuggetEntity != 0 {
		nuggetPos, ok := s.world.Positions.GetPosition(s.activeNuggetEntity)
		if ok && s.isNuggetInCollectionRange(nuggetPos.X, nuggetPos.Y) {
			s.collectNugget()
		}
	}

	// Spawn if no active nugget and cooldown elapsed
	if s.activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= parameter.NuggetSpawnInterval {
			s.lastSpawnAttempt = now
			s.spawnNugget()
		}
		return
	}

	// Emit beacon when interval elapses
	nugget, ok := s.world.Components.Nugget.GetComponent(s.activeNuggetEntity)
	if ok {
		nugget.BeaconRemaining -= dt
		if nugget.BeaconRemaining <= 0 {
			nuggetPos, posOk := s.world.Positions.GetPosition(s.activeNuggetEntity)
			if posOk {
				s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
					OriginX:   nuggetPos.X,
					OriginY:   nuggetPos.Y,
					ColorType: component.CleanerColorNugget,
				})
			}
			nugget.BeaconRemaining = parameter.NuggetBeaconInterval
		}
		s.world.Components.Nugget.SetComponent(s.activeNuggetEntity, nugget)
	}

	s.statActive.Store(s.activeNuggetEntity != 0)
}

// handleJumpRequest attempts to jump cursor to the active nugget
func (s *NuggetSystem) handleJumpRequest() {
	cursorEntity := s.world.Resources.Player.Entity

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
		Delta:      parameter.NuggetJumpCost,
		Percentage: false,
		Type:       event.EnergyDeltaSpend,
	})

	// 5. Collect nugget that overlaps with cursor
	s.collectNugget()

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

	randomChar := parameter.AlphanumericRunes[rand.Intn(len(parameter.AlphanumericRunes))]
	nugget := component.NuggetComponent{
		Char:            randomChar,
		SpawnTime:       now,
		BeaconRemaining: parameter.NuggetBeaconInterval,
	}

	// Use batch for atomic position validation
	batch := s.world.Positions.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Positions was taken while we were creating the nugget
		s.world.DestroyEntity(entity)
		return
	}

	// Set component after position is committed
	s.world.Components.Nugget.SetComponent(entity, nugget)
	// Render component
	s.world.Components.Sigil.SetComponent(entity, component.SigilComponent{
		Rune:  randomChar,
		Color: visual.RgbNuggetOrange,
	})

	s.activeNuggetEntity = entity

	s.statSpawned.Add(1)

	// Emit directional cleaners on spawn
	s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
		OriginX:   x,
		OriginY:   y,
		ColorType: component.CleanerColorNugget,
	})
}

// findValidPosition finds a valid random position for a nugget
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.world.Resources.Config
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	if !ok {
		return -1, -1
	}

	for attempt := 0; attempt < parameter.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.MapWidth)
		y := rand.Intn(config.MapHeight)

		dx := x - cursorPos.X
		if dx < 0 {
			dx = -dx
		}
		dy := y - cursorPos.Y
		if dy < 0 {
			dy = -dy
		}

		if dx <= parameter.CursorExclusionX || dy <= parameter.CursorExclusionY {
			continue
		}

		// Block spawn on walls or occupied cells
		if s.world.Positions.IsBlocked(x, y, component.WallBlockSpawn) {
			continue
		}

		return x, y
	}

	return -1, -1
}

// collectNugget handles auto-collection when cursor overlaps nugget
func (s *NuggetSystem) collectNugget() {
	s.world.Resources.Audio.Player.Play(core.SoundBell)

	s.world.DestroyEntity(s.activeNuggetEntity)

	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{Delta: parameter.NuggetHeatIncrease})

	s.activeNuggetEntity = 0
	s.statCollected.Add(1)
}

// isNuggetInCollectionRange checks if nugget position is within collection range
// Priority: ember ellipse > shield ellipse > exact cursor co-location
func (s *NuggetSystem) isNuggetInCollectionRange(nuggetX, nuggetY int) bool {
	cursorEntity := s.world.Resources.Player.Entity

	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return false
	}

	// Ember takes precedence (larger radius)
	if heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity); ok && heatComp.EmberActive {
		return vmath.EllipseContainsPoint(
			nuggetX, nuggetY,
			cursorPos.X, cursorPos.Y,
			visual.EmberInvRxSq, visual.EmberInvRySq,
		)
	}

	// Shield ellipse when active
	if shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity); ok && shieldComp.Active {
		return vmath.EllipseContainsPoint(
			nuggetX, nuggetY,
			cursorPos.X, cursorPos.Y,
			shieldComp.InvRxSq, shieldComp.InvRySq,
		)
	}

	// Fallback: exact co-location
	return cursorPos.X == nuggetX && cursorPos.Y == nuggetY
}