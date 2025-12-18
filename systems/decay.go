package systems

import (
	"math/rand"
	"sync"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	world *engine.World
	res   engine.CoreResources

	decayStore  *engine.Store[components.DecayComponent]
	protStore   *engine.Store[components.ProtectionComponent]
	deathStore  *engine.Store[components.DeathComponent]
	nuggetStore *engine.Store[components.NuggetComponent]
	charStore   *engine.Store[components.CharacterComponent]
	seqStore    *engine.Store[components.SequenceComponent]

	// Internal state
	mu        sync.RWMutex
	animating bool

	// Per-frame tracking
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	res := engine.GetCoreResources(world)
	return &DecaySystem{
		world: world,
		res:   res,

		decayStore:       engine.GetStore[components.DecayComponent](world),
		protStore:        engine.GetStore[components.ProtectionComponent](world),
		deathStore:       engine.GetStore[components.DeathComponent](world),
		nuggetStore:      engine.GetStore[components.NuggetComponent](world),
		charStore:        engine.GetStore[components.CharacterComponent](world),
		seqStore:         engine.GetStore[components.SequenceComponent](world),
		decayedThisFrame: make(map[core.Entity]bool),

		processedGridCells: make(map[int]bool),
	}
}

// Init
func (s *DecaySystem) Init() {}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return constants.PriorityDecay
}

// EventTypes returns the event types DecaySystem handles
func (s *DecaySystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventDecayStart,
		events.EventDecayCancel,
		events.EventGameReset,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventDecayStart:
		s.spawnDecayEntities()

	case events.EventDecayCancel:
		s.despawnDecayEntities()

	case events.EventGameReset:
		s.mu.Lock()
		s.animating = false
		clear(s.decayedThisFrame)
		clear(s.processedGridCells)
		s.mu.Unlock()
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update() {
	s.mu.RLock()
	animating := s.animating
	s.mu.RUnlock()

	if !animating {
		return
	}

	s.updateDecayEntities()

	// When there are no decay entities, emit EventDecayComplete once
	count := s.decayStore.Count()
	if count == 0 {
		s.mu.Lock()
		// Double check inside lock to ensure we only fire once
		shouldEmit := s.animating
		s.mu.Unlock()

		if shouldEmit {
			// Reuse despawn to reset state/flags; entity loop is no-op here since count is 0
			s.despawnDecayEntities()
			s.world.PushEvent(events.EventDecayComplete, nil)
		}
	}
}

// spawnDecayEntities creates one decay entity per column
func (s *DecaySystem) spawnDecayEntities() {
	s.mu.Lock()
	s.animating = true
	clear(s.decayedThisFrame)
	s.mu.Unlock()

	gameWidth := s.res.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		speed := constants.DecayMinSpeed + rand.Float64()*(constants.DecayMaxSpeed-constants.DecayMinSpeed)
		char := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]

		entity := s.world.CreateEntity()

		s.world.Positions.Add(entity, components.PositionComponent{X: column, Y: 0})
		// Initialize DecayComponent with PreciseX/Y float overlay and coordinate history
		s.decayStore.Add(entity, components.DecayComponent{
			PreciseX:      float64(column),
			PreciseY:      0.0,
			Speed:         speed,
			Char:          char,
			LastChangeRow: -1,
			LastIntX:      -1,
			LastIntY:      -1,
			PrevPreciseX:  float64(column),
			PrevPreciseY:  0.0,
		})
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

	var collisionBuf [constants.MaxEntitiesPerCell]core.Entity

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
					s.world.PushEvent(events.EventNuggetDestroyed, &events.NuggetDestroyedPayload{
						Entity: targetEntity,
					})
					s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
						Entities:    []core.Entity{targetEntity},
						EffectEvent: events.EventFlashRequest,
					})
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
			if currentRow > 0 && rand.Float64() < constants.DecayChangeChance {
				fall.Char = constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
			}
		}

		fall.PrevPreciseX = fall.PreciseX

		// Grid Sync Protocol: Update PositionStore if integer position changed
		newGridY := int(fall.PreciseY)
		if newGridY != pos.Y {
			s.world.Positions.Add(entity, components.PositionComponent{X: pos.X, Y: newGridY})
		}

		s.decayStore.Add(entity, fall)
	}
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(entity core.Entity) {
	seq, ok := s.seqStore.Get(entity)
	if !ok {
		return
	}

	// Check protection
	if prot, ok := s.protStore.Get(entity); ok {
		now := s.res.Time.GameTime
		if !prot.IsExpired(now.UnixNano()) && prot.Mask.Has(components.ProtectFromDecay) {
			return
		}
	}

	// Apply decay logic
	if seq.Level > components.LevelDark {
		// Reduce level by 1 when not dark
		seq.Level--
		s.seqStore.Add(entity, seq)

		// Update character semantic info (renderer resolves color)
		if char, ok := s.charStore.Get(entity); ok {
			char.SeqLevel = seq.Level
			s.charStore.Add(entity, char)
		}
	} else {
		// Dark level decay color chain: Blue → Green → Red → destroy
		if seq.Type == components.SequenceBlue {
			seq.Type = components.SequenceGreen
			seq.Level = components.LevelBright
			s.seqStore.Add(entity, seq)
			if char, ok := s.charStore.Get(entity); ok {
				char.SeqType = seq.Type
				char.SeqLevel = seq.Level
				s.charStore.Add(entity, char)
			}
		} else if seq.Type == components.SequenceGreen {
			seq.Type = components.SequenceRed
			seq.Level = components.LevelBright
			s.seqStore.Add(entity, seq)
			if char, ok := s.charStore.Get(entity); ok {
				char.SeqType = seq.Type
				char.SeqLevel = seq.Level
				s.charStore.Add(entity, char)
			}
		} else {
			// Red at LevelDark - death with flash
			s.world.PushEvent(events.EventRequestDeath, &events.DeathRequestPayload{
				Entities:    []core.Entity{entity},
				EffectEvent: events.EventFlashRequest,
			})
		}
	}
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
		s.deathStore.Add(entity, components.DeathComponent{})
	}
}