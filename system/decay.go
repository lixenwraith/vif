package system

import (
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	mu    sync.RWMutex
	world *engine.World
	res   engine.Resources

	decayStore    *engine.Store[component.DecayComponent]
	protStore     *engine.Store[component.ProtectionComponent]
	deathStore    *engine.Store[component.DeathComponent]
	nuggetStore   *engine.Store[component.NuggetComponent]
	charStore     *engine.Store[component.CharacterComponent]
	typeableStore *engine.Store[component.TypeableComponent]

	// State
	animating bool

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	statActive  *atomic.Bool
	statCount   *atomic.Int64
	statApplied *atomic.Int64
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &DecaySystem{
		world: world,
		res:   res,

		decayStore:    engine.GetStore[component.DecayComponent](world),
		protStore:     engine.GetStore[component.ProtectionComponent](world),
		deathStore:    engine.GetStore[component.DeathComponent](world),
		nuggetStore:   engine.GetStore[component.NuggetComponent](world),
		charStore:     engine.GetStore[component.CharacterComponent](world),
		typeableStore: engine.GetStore[component.TypeableComponent](world),

		decayedThisFrame:   make(map[core.Entity]bool),
		processedGridCells: make(map[int]bool),

		statActive:  res.Status.Bools.Get("decay.active"),
		statCount:   res.Status.Ints.Get("decay.count"),
		statApplied: res.Status.Ints.Get("decay.applied"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *DecaySystem) Init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
}

// initLocked performs session state reset, caller must hold s.mu
func (s *DecaySystem) initLocked() {
	s.animating = false
	clear(s.decayedThisFrame)
	clear(s.processedGridCells)
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statApplied.Store(0)
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return constant.PriorityDecay
}

// EventTypes returns the event types DecaySystem handles
func (s *DecaySystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDecayStart,
		event.EventDecayCancel,
		event.EventDecaySpawnOne,
		event.EventGameReset,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventDecayStart:
		s.spawnPhaseDecay()

	case event.EventDecayCancel:
		s.despawnDecayEntities()

	case event.EventDecaySpawnOne:
		if payload, ok := ev.Payload.(*event.DecaySpawnPayload); ok {
			s.spawnSingleDecay(payload.X, payload.Y, payload.Char)
		}

	case event.EventGameReset:
		s.Init()
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update() {
	// Always process decay entities regardless of phase animation state
	count := s.decayStore.Count()
	if count == 0 {
		// Check if phase animation completed (FSM needs EventDecayComplete)
		s.mu.RLock()
		animating := s.animating
		s.mu.RUnlock()

		if animating {
			s.despawnDecayEntities()
			s.world.PushEvent(event.EventDecayComplete, nil)
		}

		s.statActive.Store(false)
		s.statCount.Store(0)
		return
	}

	s.updateDecayEntities()

	// Re-check count after update (entities may have been destroyed)
	count = s.decayStore.Count()
	if count == 0 {
		s.mu.Lock()
		animating := s.animating
		s.mu.Unlock()

		if animating {
			s.despawnDecayEntities()
			s.world.PushEvent(event.EventDecayComplete, nil)
		}
	}

	s.statActive.Store(s.animating)
	s.statCount.Store(int64(s.decayStore.Count()))
}

// spawnSingleDecay creates one decay entity at specified position
// Used by cleaner-triggered decay (negative energy) and phase decay
func (s *DecaySystem) spawnSingleDecay(x, y int, char rune) {
	speed := constant.DecayMinSpeed + rand.Float64()*(constant.DecayMaxSpeed-constant.DecayMinSpeed)

	entity := s.world.CreateEntity()

	s.world.Positions.Add(entity, component.PositionComponent{X: x, Y: y})
	s.decayStore.Add(entity, component.DecayComponent{
		PreciseX:      float64(x),
		PreciseY:      float64(y),
		Speed:         speed,
		Char:          char,
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  float64(x),
		PrevPreciseY:  float64(y),
	})
}

// spawnPhaseDecay creates decay entities for FSM-triggered phase transition
func (s *DecaySystem) spawnPhaseDecay() {
	s.mu.Lock()
	s.animating = true
	clear(s.decayedThisFrame)
	s.mu.Unlock()

	gameWidth := s.res.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		char := constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
		s.spawnSingleDecay(column, 0, char)
	}
}

// updateDecayEntities updates entity positions and applies decay
func (s *DecaySystem) updateDecayEntities() {
	dtSeconds := s.res.Time.DeltaTime.Seconds()
	gameHeight := s.res.Config.GameHeight
	gameWidth := s.res.Config.GameWidth

	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	decayEntities := s.decayStore.All()

	// Clear frame deduplication maps
	for k := range s.processedGridCells {
		delete(s.processedGridCells, k)
	}
	// Clear one-decay-per-tick map
	for k := range s.decayedThisFrame {
		delete(s.decayedThisFrame, k)
	}

	var collisionBuf [constant.MaxEntitiesPerCell]core.Entity

	for _, entity := range decayEntities {
		fall, ok := s.decayStore.Get(entity)
		if !ok {
			continue
		}

		pos, hasPos := s.world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Integration: Update float position (overlay state)
		startY := fall.PreciseY
		fall.PreciseY += fall.Speed * dtSeconds
		fall.PrevPreciseY = startY

		// Destroy if entity falls below game area
		if fall.PreciseY >= float64(gameHeight) {
			s.world.DestroyEntity(entity)
			continue
		}

		// Swept Traversal: Check all rows between previous and current position for collisions
		y1 := int(startY)
		y2 := int(fall.PreciseY)

		startRow, endRow := y1, y2
		if y1 > y2 {
			startRow, endRow = y2, y1
		}
		if startRow < 0 {
			startRow = 0
		}
		if endRow >= gameHeight {
			endRow = gameHeight - 1
		}

		col := int(fall.PreciseX)

		// Check each traversed row for entity collisions
		for row := startRow; row <= endRow; row++ {
			// Coordinate latch: skip if already processed this exact coordinate
			if col == fall.LastIntX && row == fall.LastIntY {
				continue
			}
			if col < 0 || col >= gameWidth {
				continue
			}

			// Frame deduplication: skip if this cell was already processed this frame
			flatIdx := (row * gameWidth) + col
			if s.processedGridCells[flatIdx] {
				continue
			}

			// Query entities at position using zero-alloc buffer
			n := s.world.Positions.GetAllAtInto(col, row, collisionBuf[:])

			// Process collisions with self-exclusion
			for i := 0; i < n; i++ {
				targetEntity := collisionBuf[i]
				if targetEntity == 0 || targetEntity == entity {
					continue // Self-exclusion
				}

				// Entity deduplication: skip if already hit this frame
				s.mu.RLock()
				alreadyHit := s.decayedThisFrame[targetEntity]
				s.mu.RUnlock()

				if alreadyHit {
					continue
				}

				if s.nuggetStore.Has(targetEntity) {
					// Signal nugget destruction to NuggetSystem
					s.world.PushEvent(event.EventNuggetDestroyed, &event.NuggetDestroyedPayload{
						Entity: targetEntity,
					})
					// TODO: death to inform system of entity of their deaths instead of spread out event? Easier to manage logic
					event.EmitDeathOne(s.res.Events.Queue, targetEntity, event.EventFlashRequest, s.res.Time.FrameNumber)
				} else {
					s.applyDecayToCharacter(targetEntity)
				}

				s.mu.Lock()
				s.decayedThisFrame[targetEntity] = true
				s.mu.Unlock()
			}

			s.processedGridCells[flatIdx] = true
		}

		// Coordinate Latch Update: Track last processed position to prevent re-processing
		fall.LastIntX = col
		fall.LastIntY = int(fall.PreciseY)

		// Visual character randomization (matrix effect)
		currentRow := int(fall.PreciseY)
		if currentRow != fall.LastChangeRow {
			fall.LastChangeRow = currentRow
			if currentRow > 0 && rand.Float64() < constant.DecayChangeChance {
				fall.Char = constant.AlphanumericRunes[rand.Intn(len(constant.AlphanumericRunes))]
			}
		}

		fall.PrevPreciseX = fall.PreciseX

		// Grid Sync Protocol: Update PositionStore if integer position changed
		newGridY := int(fall.PreciseY)
		if newGridY != pos.Y {
			s.world.Positions.Add(entity, component.PositionComponent{X: pos.X, Y: newGridY})
		}

		s.decayStore.Add(entity, fall)
	}
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(entity core.Entity) {
	typeable, ok := s.typeableStore.Get(entity)
	if !ok {
		return
	}

	// Check protection
	if prot, ok := s.protStore.Get(entity); ok {
		now := s.res.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(component.ProtectFromDecay) {
			return
		}
	}

	// Get character component for renderer sync
	char, hasChar := s.charStore.Get(entity)

	// Apply decay logic
	if typeable.Level > component.LevelDark {
		// Reduce level by 1
		typeable.Level--
		s.typeableStore.Add(entity, typeable)

		// Sync renderer
		if hasChar {
			char.SeqLevel = typeable.Level
			s.charStore.Add(entity, char)
		}
	} else {
		// Dark level: type chain Blue→Green→Red→destroy
		switch typeable.Type {
		case component.TypeBlue:
			typeable.Type = component.TypeGreen
			typeable.Level = component.LevelBright
			s.typeableStore.Add(entity, typeable)
			if hasChar {
				char.SeqType = component.CharacterGreen
				char.SeqLevel = component.LevelBright
				s.charStore.Add(entity, char)
			}

		case component.TypeGreen:
			typeable.Type = component.TypeRed
			typeable.Level = component.LevelBright
			s.typeableStore.Add(entity, typeable)
			if hasChar {
				char.SeqType = component.CharacterRed
				char.SeqLevel = component.LevelBright
				s.charStore.Add(entity, char)
			}

		default:
			// Red or other: destroy
			event.EmitDeathOne(s.res.Events.Queue, entity, event.EventFlashRequest, s.res.Time.FrameNumber)
		}
	}

	s.statApplied.Add(1)
}

// despawnDecayEntities marks all decay entities for death
func (s *DecaySystem) despawnDecayEntities() {
	s.mu.Lock()
	s.animating = false // Stop processing updates immediately
	clear(s.decayedThisFrame)
	clear(s.processedGridCells) // Reset frame state
	s.mu.Unlock()

	// Mark all existing decay entities for death
	// We use MarkedForDeath to allow CullSystem to clean them up properly in the same frame
	entities := s.decayStore.All()
	for _, entity := range entities {
		s.deathStore.Add(entity, component.DeathComponent{})
	}
}