package systems

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

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
	heatStore   *engine.Store[components.HeatComponent]
	nuggetStore *engine.Store[components.NuggetComponent]
	charStore   *engine.Store[components.CharacterComponent]
	seqStore    *engine.Store[components.SequenceComponent]

	// Internal state
	mu          sync.RWMutex
	timerActive bool
	nextTime    time.Time
	animating   bool
	startTime   time.Time

	// Per-frame tracking
	currentRow         int
	decayedThisFrame   map[core.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x

	// Cached metric pointers
	statTimer *atomic.Int64
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(world *engine.World) engine.System {
	res := engine.GetCoreResources(world)
	return &DecaySystem{
		world: world,
		res:   res,

		decayStore:       engine.GetStore[components.DecayComponent](world),
		protStore:        engine.GetStore[components.ProtectionComponent](world),
		heatStore:        engine.GetStore[components.HeatComponent](world),
		nuggetStore:      engine.GetStore[components.NuggetComponent](world),
		charStore:        engine.GetStore[components.CharacterComponent](world),
		seqStore:         engine.GetStore[components.SequenceComponent](world),
		decayedThisFrame: make(map[core.Entity]bool),

		processedGridCells: make(map[int]bool),
		statTimer:          res.Status.Ints.Get("decay.timer"),
	}
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return constants.PriorityDecay
}

// EventTypes returns the event types DecaySystem handles
func (s *DecaySystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventDecayTimerStart,
		events.EventGameReset,
	}
}

// HandleEvent processes decay-related events
func (s *DecaySystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventDecayTimerStart:
		s.startDecayTimer(world)

	case events.EventGameReset:
		s.mu.Lock()
		s.timerActive = false
		s.animating = false
		s.nextTime = time.Time{}
		s.startTime = time.Time{}
		s.currentRow = 0
		clear(s.decayedThisFrame)
		clear(s.processedGridCells)
		s.mu.Unlock()

		s.statTimer.Store(0)
	}
}

// Update runs the decay system logic
func (s *DecaySystem) Update(world *engine.World, dt time.Duration) {
	now := s.res.Time.GameTime

	s.mu.Lock()
	timerActive := s.timerActive
	nextTime := s.nextTime
	animating := s.animating
	s.mu.Unlock()

	// Publish timer remaining (direct atomic write)
	if timerActive {
		remaining := nextTime.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		s.statTimer.Store(int64(remaining))
	} else if animating {
		s.statTimer.Store(0)
	}

	// Timer expiration check
	if timerActive && now.After(nextTime) {
		s.triggerAnimation(world, now)
		return
	}

	// Animation update
	if animating {
		s.updateAnimation(world, dt)
	}
}

// startDecayTimer calculates interval based on heat and starts timer
func (s *DecaySystem) startDecayTimer(world *engine.World) {
	heatValue := 0
	if hc, ok := s.heatStore.Get(s.res.Cursor.Entity); ok {
		heatValue = int(hc.Current.Load())
	}

	// Heat-based interval calculation
	heatPercentage := float64(heatValue) / float64(constants.MaxHeat)
	if heatPercentage > 1.0 {
		heatPercentage = 1.0
	}
	if heatPercentage < 0.0 {
		heatPercentage = 0.0
	}

	intervalSeconds := constants.DecayIntervalBaseSeconds - constants.DecayIntervalRangeSeconds*heatPercentage
	interval := time.Duration(intervalSeconds * float64(time.Second))

	s.mu.Lock()
	// TODO: check if `now` should be in mutex
	now := s.res.Time.GameTime
	s.timerActive = true
	s.nextTime = now.Add(interval)
	s.mu.Unlock()
}

// triggerAnimation starts decay animation
func (s *DecaySystem) triggerAnimation(world *engine.World, now time.Time) {
	s.mu.Lock()
	s.timerActive = false
	s.animating = true
	s.startTime = now
	s.currentRow = 0
	clear(s.decayedThisFrame)
	s.mu.Unlock()

	s.spawnDecayEntities(world)
	world.PushEvent(events.EventDecayStart, nil)
}

// updateAnimation progresses the decay animation
func (s *DecaySystem) updateAnimation(world *engine.World, dt time.Duration) {
	// Use Delta Time (dt) for physics integration
	s.updateDecayEntities(world, dt.Seconds())

	// Check entity count from the Store to prevents "Zombie Phase" by ensuring phase ends exactly when entities are gone
	count := s.decayStore.Count()
	if count == 0 {
		s.mu.Lock()
		s.currentRow = 0
		s.animating = false
		s.startTime = time.Time{}
		s.mu.Unlock()

		s.statTimer.Store(0)

		// Ensure cleanup of any artifacts
		s.cleanupDecayEntities(world)
		world.PushEvent(events.EventDecayComplete, nil)
	}
}

// spawnDecayEntities creates one decay entity per column
func (s *DecaySystem) spawnDecayEntities(world *engine.World) {
	gameWidth := s.res.Config.GameWidth

	// Spawn one decay entity per column for full-width coverage
	for column := 0; column < gameWidth; column++ {
		speed := constants.DecayMinSpeed + rand.Float64()*(constants.DecayMaxSpeed-constants.DecayMinSpeed)
		char := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]

		entity := world.CreateEntity()

		world.Positions.Add(entity, components.PositionComponent{X: column, Y: 0})
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
func (s *DecaySystem) updateDecayEntities(world *engine.World, dtSeconds float64) {
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

		pos, hasPos := world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Physics Integration: Update float position (overlay state)
		startY := fall.PreciseY
		fall.PreciseY += fall.Speed * dtSeconds
		fall.PrevPreciseY = startY

		// Destroy if entity falls below game area
		if fall.PreciseY >= float64(gameHeight) {
			world.DestroyEntity(entity)
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
			n := world.Positions.GetAllAtInto(col, row, collisionBuf[:])

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
					if char, ok := s.charStore.Get(targetEntity); ok {
						world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
							X: col, Y: row, Char: char.Rune,
						})
					}
					// Signal nugget destruction to NuggetSystem
					world.PushEvent(events.EventNuggetDestroyed, &events.NuggetDestroyedPayload{
						Entity: targetEntity,
					})
					world.DestroyEntity(targetEntity)
				} else {
					s.applyDecayToCharacter(world, targetEntity)
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
			world.Positions.Add(entity, components.PositionComponent{X: pos.X, Y: newGridY})
		}

		s.decayStore.Add(entity, fall)
	}
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(world *engine.World, entity core.Entity) {
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
			// Red at LevelDark - spawn flash then remove entity
			if pos, ok := world.Positions.Get(entity); ok {
				if char, ok := s.charStore.Get(entity); ok {
					world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
						X: pos.X, Y: pos.Y, Char: char.Rune,
					})
				}
			}
			world.DestroyEntity(entity)
		}
	}
}

// cleanupDecayEntities removes all decay entities
func (s *DecaySystem) cleanupDecayEntities(world *engine.World) {
	entities := s.decayStore.All()
	for _, entity := range entities {
		world.DestroyEntity(entity)
	}

	s.mu.Lock()
	clear(s.decayedThisFrame)
	s.mu.Unlock()
}