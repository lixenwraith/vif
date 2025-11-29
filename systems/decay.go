package systems

import (
	"math/rand"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DecaySystem handles character decay animation and logic, stateless decay entity list
type DecaySystem struct {
	mu                 sync.RWMutex
	currentRow         int
	lastUpdate         time.Time
	ctx                *engine.GameContext
	spawnSystem        *SpawnSystem
	decayedThisFrame   map[engine.Entity]bool
	processedGridCells map[int]bool // Key is flat index: (y * gameWidth) + x
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(ctx *engine.GameContext) *DecaySystem {
	s := &DecaySystem{
		currentRow:         0,
		lastUpdate:         time.Time{},
		ctx:                ctx,
		decayedThisFrame:   make(map[engine.Entity]bool),
		processedGridCells: make(map[int]bool),
	}
	return s
}

// SetSpawnSystem sets the spawn system reference for color counter updates
func (s *DecaySystem) SetSpawnSystem(spawnSystem *SpawnSystem) {
	s.spawnSystem = spawnSystem
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return constants.PriorityDecay
}

// Update runs the decay system animation update
func (s *DecaySystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read decay state snapshot for consistent check
	decaySnapshot := s.ctx.State.ReadDecayState(now)

	// Update animation if active
	if decaySnapshot.Animating {
		s.updateAnimation(world, dt)
	}
}

// updateAnimation progresses the decay animation
func (s *DecaySystem) updateAnimation(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Use Delta Time (dt) for physics integration
	s.updateDecayEntities(world, dt.Seconds())

	// Check actual entity count from the Store
	// This prevents "Zombie Phase" by ensuring phase ends exactly when entities are gone
	count := world.Decays.Count()

	if count == 0 {
		s.mu.Lock()
		s.currentRow = 0
		s.mu.Unlock()

		// Ensure cleanup of any artifacts
		s.cleanupDecayEntities(world)

		// Stop decay animation in GameState (transitions to PhaseNormal)
		if !s.ctx.State.StopDecayAnimation(now) {
			return
		}
	}
}

// spawnDecayEntities creates one decay entity per column using generic stores
func (s *DecaySystem) spawnDecayEntities(world *engine.World) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	gameWidth := config.GameWidth

	// Create one falling entity per column to ensure complete coverage
	for column := 0; column < gameWidth; column++ {
		// Random speed for each entity
		speed := constants.DecayMinSpeed + rand.Float64()*(constants.DecayMaxSpeed-constants.DecayMinSpeed)

		// Random character for each entity
		char := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]

		// Create falling entity using world
		entity := world.CreateEntity()
		world.Decays.Add(entity, components.DecayComponent{
			Column:        column,
			YPosition:     0.0,
			Speed:         speed,
			Char:          char,
			LastChangeRow: -1,

			// Initialize coordinate latch to force first-frame processing
			LastIntX: -1,
			LastIntY: -1,

			// Initialize physics history to spawn position
			PrevPreciseX: float64(column),
			PrevPreciseY: 0.0,
		})
	}
}

// updateDecayEntities updates entity positions and applies decay using generic stores
func (s *DecaySystem) updateDecayEntities(world *engine.World, dtSeconds float64) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	gameHeight := config.GameHeight
	gameWidth := config.GameWidth

	// Clamp dt to prevent tunneling on huge lag spikes (e.g. Resume from pause)
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	// Query all decay entities
	decayEntities := world.Decays.All()

	// Clear deduplication maps for this frame
	// processedGridCells: prevents same cell being processed by multiple decay entities
	// decayedThisFrame: prevents same target being decayed twice (by any decay entity)
	for k := range s.processedGridCells {
		delete(s.processedGridCells, k)
	}

	// Stack-allocated buffer for spatial queries (zero allocation)
	var collisionBuf [engine.MaxEntitiesPerCell]engine.Entity

	for _, entity := range decayEntities {
		fall, ok := world.Decays.Get(entity)
		if !ok {
			continue
		}

		// 1. Update Physics
		startY := fall.YPosition
		fall.YPosition += fall.Speed * dtSeconds
		fall.PrevPreciseY = startY

		// Boundary Check
		if fall.YPosition >= float64(gameHeight) {
			world.DestroyEntity(entity)
			continue
		}

		// 2. Swept Traversal (From StartY to EndY)
		y1 := int(startY)
		y2 := int(fall.YPosition)

		startRow, endRow := y1, y2
		if y1 > y2 {
			startRow, endRow = y2, y1
		}

		// Clamp to screen
		if startRow < 0 {
			startRow = 0
		}
		if endRow >= gameHeight {
			endRow = gameHeight - 1
		}

		for row := startRow; row <= endRow; row++ {
			col := fall.Column

			// A. Coordinate latch check
			if col == fall.LastIntX && row == fall.LastIntY {
				continue
			}

			if col < 0 || col >= gameWidth {
				continue
			}

			// B. Frame deduplication (spatial)
			// If another decay entity already processed this cell this frame, skip
			flatIdx := (row * gameWidth) + col
			if s.processedGridCells[flatIdx] {
				continue
			}

			// C. Interaction - use zero-alloc buffer
			n := world.Positions.GetAllAtInto(col, row, collisionBuf[:])

			// Process all targets at this cell
			for i := 0; i < n; i++ {
				targetEntity := collisionBuf[i]
				if targetEntity == 0 {
					continue
				}

				// Check if this target was already decayed this frame (by any decay entity)
				s.mu.RLock()
				alreadyHit := s.decayedThisFrame[targetEntity]
				s.mu.RUnlock()

				if alreadyHit {
					continue
				}

				// Apply decay effect to target
				if world.Nuggets.Has(targetEntity) {
					if char, ok := world.Characters.Get(targetEntity); ok {
						timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
						SpawnDestructionFlash(world, col, row, char.Rune, timeRes.GameTime)
					}
					world.DestroyEntity(targetEntity)
					s.ctx.State.ClearActiveNuggetID(uint64(targetEntity))
				} else {
					s.applyDecayToCharacter(world, targetEntity)
				}

				// Mark target as processed for this frame
				s.mu.Lock()
				s.decayedThisFrame[targetEntity] = true
				s.mu.Unlock()
			}

			// D. Mark cell as processed AFTER handling all targets
			// This ensures: if 2 decay entities hit same cell, first one processes all targets,
			// second one skips entirely (correct behavior - decay applies once per cell per frame)
			s.processedGridCells[flatIdx] = true

			// E. Update latch & visuals
			fall.LastIntX = col
			fall.LastIntY = row

			if row != fall.LastChangeRow {
				fall.LastChangeRow = row
				if row > 0 && rand.Float64() < constants.DecayChangeChance {
					fall.Char = constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
				}
			}
		}

		fall.PrevPreciseX = float64(fall.Column)
		world.Decays.Add(entity, fall)
	}
}

// applyDecayToRow applies decay logic to all characters at the given row using generic stores
func (s *DecaySystem) applyDecayToRow(world *engine.World, row int) {
	// Query entities with both position and sequence components
	entities := world.Query().
		With(world.Positions).
		With(world.Sequences).
		Execute()

	for _, entity := range entities {
		pos, ok := world.Positions.Get(entity)
		if !ok {
			continue
		}

		if pos.Y == row {
			s.applyDecayToCharacter(world, entity)
		}
	}
}

// applyDecayToCharacter applies decay logic to a single character entity using generic stores
func (s *DecaySystem) applyDecayToCharacter(world *engine.World, entity engine.Entity) {
	seq, ok := world.Sequences.Get(entity)
	if !ok {
		return // Not a sequence entity
	}

	// Don't decay gold sequences
	if seq.Type == components.SequenceGold {
		return
	}

	// Apply decay logic
	if seq.Level > components.LevelDark {
		// Reduce level by 1 when not dark
		seq.Level--
		world.Sequences.Add(entity, seq)

		// Update character style
		if char, ok := world.Characters.Get(entity); ok {
			char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
			world.Characters.Add(entity, char)
		}
	} else {
		// Dark level decay color chain: Blue → Green → Red → destroy
		if seq.Type == components.SequenceBlue {
			seq.Type = components.SequenceGreen
			seq.Level = components.LevelBright
			world.Sequences.Add(entity, seq)

			if char, ok := world.Characters.Get(entity); ok {
				char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
				world.Characters.Add(entity, char)
			}
		} else if seq.Type == components.SequenceGreen {
			seq.Type = components.SequenceRed
			seq.Level = components.LevelBright
			world.Sequences.Add(entity, seq)

			if char, ok := world.Characters.Get(entity); ok {
				char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
				world.Characters.Add(entity, char)
			}
		} else {
			// Red at LevelDark - spawn flash then remove entity
			if pos, ok := world.Positions.Get(entity); ok {
				if char, ok := world.Characters.Get(entity); ok {
					timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
					SpawnDestructionFlash(world, pos.X, pos.Y, char.Rune, timeRes.GameTime)
				}
			}
			world.DestroyEntity(entity)
		}
	}
}

// cleanupDecayEntities removes all decay entities using generic stores
func (s *DecaySystem) cleanupDecayEntities(world *engine.World) {
	// Get all falling decays and iterate to destroy
	entities := world.Decays.All()
	for _, entity := range entities {
		world.DestroyEntity(entity)
	}

	s.mu.Lock()
	clear(s.decayedThisFrame)
	s.mu.Unlock()
}

// TriggerDecayAnimation is called by ClockScheduler to start decay animation
func (s *DecaySystem) TriggerDecayAnimation(world *engine.World) {
	s.mu.Lock()
	s.currentRow = 0
	// Reset the decay tracking map for the new animation sequence
	clear(s.decayedThisFrame)
	s.mu.Unlock()

	// Spawn falling decay entities
	s.spawnDecayEntities(world)
}

// IsAnimating returns true if decay animation is active
func (s *DecaySystem) IsAnimating(now time.Time) bool {
	decaySnapshot := s.ctx.State.ReadDecayState(now)
	return decaySnapshot.Animating
}

// CurrentRow returns the current decay row being displayed
func (s *DecaySystem) CurrentRow(now time.Time) int {
	s.mu.RLock()
	currentRow := s.currentRow
	s.mu.RUnlock()

	config := engine.MustGetResource[*engine.ConfigResource](s.ctx.World.Resources)
	gameHeight := config.GameHeight

	decaySnapshot := s.ctx.State.ReadDecayState(now)

	// When animation is done, currentRow is 0, but we want to avoid displaying row 0
	// During animation, currentRow is the next row to process
	// For display, return the last processed row (currentRow - 1)
	// but clamp to valid range [0, gameHeight-1]
	if !decaySnapshot.Animating {
		return 0
	}
	if currentRow > 0 {
		displayRow := currentRow - 1
		if displayRow >= gameHeight {
			return gameHeight - 1
		}
		return displayRow
	}
	return 0
}

// GetTimeUntilDecay returns seconds until next decay trigger
func (s *DecaySystem) GetTimeUntilDecay(now time.Time) float64 {
	decaySnapshot := s.ctx.State.ReadDecayState(now)
	return decaySnapshot.TimeUntil
}