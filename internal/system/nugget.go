package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	world *engine.World

	rng *vmath.FastRand

	lastSpawnAttempt   time.Time
	activeNuggetEntity core.Entity

	statActive        *atomic.Bool
	statSpawned       *atomic.Int64
	statCollected     *atomic.Int64
	statJumps         *atomic.Int64
	statSpawnFailures *atomic.Int64
	rejects           rejectionTelemetry

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
	s.statSpawnFailures = world.Resources.Status.Ints.Get("nugget.spawn_failures")
	s.rejects = newRejectionTelemetry(world.Resources.Status, "nugget")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *NuggetSystem) Init() {
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.lastSpawnAttempt = time.Time{}
	s.activeNuggetEntity = 0
	s.statActive.Store(false)
	s.statSpawned.Store(0)
	s.statCollected.Store(0)
	s.statJumps.Store(0)
	s.statSpawnFailures.Store(0)
	s.rejects.Reset()
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
		event.EventGameResetRequest,
	}
}

// HandleEvent processes nugget-related events
func (s *NuggetSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
		if ev.Type != event.EventMetaSystemCommandRequest {
			s.rejects.disabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventNuggetJumpRequest:
		if payload, ok := ev.Payload.(*event.NuggetJumpRequestPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.handleJumpRequest(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

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
		if ok {
			if cursor := s.collectionCursor(nuggetPos.X, nuggetPos.Y); cursor != 0 {
				s.collectNugget(cursor)
			}
		}
	}

	// Spawn if no active nugget and cooldown elapsed
	if s.activeNuggetEntity == 0 {
		s.statActive.Store(false)
		if now.Sub(s.lastSpawnAttempt) >= parameter.NuggetSpawnInterval {
			s.lastSpawnAttempt = now
			s.spawnNugget()
		}
		return
	}

	// Emit beacon when interval elapses
	nugget, ok := s.world.Components.Nugget.GetPtr(s.activeNuggetEntity)
	if ok {
		nugget.BeaconRemaining -= dt
		if nugget.BeaconRemaining <= 0 {
			if nuggetPos, posOk := s.world.Positions.GetPosition(s.activeNuggetEntity); posOk {
				s.emitBeacon(nuggetPos.X, nuggetPos.Y)
			}
			nugget.BeaconRemaining = parameter.NuggetBeaconInterval
		}
	}

	s.statActive.Store(true)
}

// handleJumpRequest attempts to jump one cursor to the active nugget.
func (s *NuggetSystem) handleJumpRequest(cursorEntity core.Entity) {
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
	s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
		Entity: cursorEntity,
		X:      nuggetPos.X,
		Y:      nuggetPos.Y,
	})

	// 4. Pay Energy Cost (spend, non-convergent)
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Entity:     cursorEntity,
		Delta:      parameter.NuggetJumpCostPercent,
		Percentage: true,
		Type:       component.EnergyDeltaSpend,
	})

	// 5. Collect nugget that overlaps with cursor
	s.collectNugget(cursorEntity)

	// 5. Update stats
	s.statJumps.Add(1)
}

// emitBeacon fires the nugget's directional cleaner from the nearest rostered cursor,
// so a shared payload never names the local cursor
func (s *NuggetSystem) emitBeacon(x, y int) {
	cursor, _, _, ok := ClosestCursor(s.world, x, y)
	if !ok {
		return
	}
	s.world.PushEvent(event.EventCleanerDirectionalRequest, &event.DirectionalCleanerPayload{
		Entity:    cursor,
		OriginX:   x,
		OriginY:   y,
		ColorType: component.CleanerColorNugget,
	})
}

// spawnNugget creates a new nugget at a random valid position, caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget() {
	now := s.world.Resources.Time.GameTime
	x, y := s.findValidPosition()
	if x < 0 || y < 0 {
		s.statSpawnFailures.Add(1)
		return
	}

	entity := s.world.CreateEntity(core.DomainShared)

	pos := component.PositionComponent{
		X: x,
		Y: y,
	}

	randomChar := parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
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
		s.statSpawnFailures.Add(1)
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
	s.emitBeacon(x, y)
}

// findValidPosition finds a valid random position for a nugget
func (s *NuggetSystem) findValidPosition() (int, int) {
	config := s.world.Resources.Config
	if s.world.Resources.Player.Count() == 0 {
		return -1, -1
	}

	for range parameter.NuggetMaxAttempts {
		x := s.rng.Intn(config.MapWidth)
		y := s.rng.Intn(config.MapHeight)

		nearCursor := false
		s.world.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
			cursorPos, ok := s.world.Positions.GetPosition(e)
			if !ok {
				return true
			}
			dx := max(x-cursorPos.X, cursorPos.X-x)
			dy := max(y-cursorPos.Y, cursorPos.Y-y)
			if dx <= parameter.CursorExclusionX || dy <= parameter.CursorExclusionY {
				nearCursor = true
				return false
			}
			return true
		})
		if nearCursor {
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

// collectNugget rewards the cursor that collected the active nugget.
func (s *NuggetSystem) collectNugget(cursor core.Entity) {
	s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Whoosh,
	})

	s.world.DestroyEntity(s.activeNuggetEntity)

	s.world.PushEvent(event.EventHeatAddRequest, &event.HeatAddRequestPayload{
		Entity: cursor,
		Delta:  parameter.NuggetHeatIncrease,
	})

	s.activeNuggetEntity = 0
	s.statCollected.Add(1)
}

// collectionCursor returns the first rostered cursor whose collection area contains the nugget.
func (s *NuggetSystem) collectionCursor(nuggetX, nuggetY int) core.Entity {
	for i := range parameter.MaxPlayers {
		cursor := s.world.Resources.Player.Slot(uint8(i))
		cursorPos, ok := s.world.Positions.GetPosition(cursor)
		if !ok {
			continue
		}

		if heatComp, ok := s.world.Components.Heat.GetComponent(cursor); ok && heatComp.EmberActive &&
			vmath.EllipseContainsPointF(nuggetX, nuggetY, cursorPos.X, cursorPos.Y, visual.EmberInvRxSq, visual.EmberInvRySq) {
			return cursor
		}
		if shieldComp, ok := s.world.Components.Shield.GetComponent(cursor); ok && shieldComp.Active &&
			vmath.EllipseContainsPointF(nuggetX, nuggetY, cursorPos.X, cursorPos.Y, shieldComp.InvRxSq, shieldComp.InvRySq) {
			return cursor
		}
		if cursorPos.X == nuggetX && cursorPos.Y == nuggetY {
			return cursor
		}
	}
	return 0
}
