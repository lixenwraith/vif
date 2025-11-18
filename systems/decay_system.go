package systems

import (
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DecaySystem handles character decay animation and logic
//
// GAME FLOW: Decay timer calculation starts AFTER Gold sequence ends
// Phase 3: Timer and animation state migrated to GameState
// 1. Gold spawns at game start
// 2. Gold ends (timeout or completion) → GameState.StartDecayTimer() called
// 3. Timer calculates interval based on heat at Gold end time (no caching!)
// 4. Decay animation triggered by ClockScheduler when timer expires
// 5. After decay animation ends → Gold spawns again
type DecaySystem struct {
	mu              sync.RWMutex // Protects fields below
	// Phase 3: Removed animating, timerStarted, nextDecayTime - now in GameState
	// Phase 3: Removed heatIncrement - was causing race condition (cached stale value)
	currentRow      int
	lastUpdate      time.Time
	gameWidth       int
	gameHeight      int
	ctx             *engine.GameContext
	spawnSystem     *SpawnSystem
	fallingEntities []engine.Entity // Entities representing falling decay characters
	decayedThisFrame map[engine.Entity]bool // Track which entities were decayed this frame
}

// NewDecaySystem creates a new decay system
// Phase 3: Timer and animation state now managed by GameState
// Phase 4: Removed deprecated heatIncrement parameter (was causing race condition)
func NewDecaySystem(gameWidth, gameHeight int, ctx *engine.GameContext) *DecaySystem {
	s := &DecaySystem{
		// Phase 3: animating, timerStarted, nextDecayTime now in GameState
		// Phase 3: heatIncrement removed - was causing race condition
		currentRow:       0,
		lastUpdate:       ctx.TimeProvider.Now(),
		gameWidth:        gameWidth,
		gameHeight:       gameHeight,
		ctx:              ctx,
		fallingEntities:  make([]engine.Entity, 0),
		decayedThisFrame: make(map[engine.Entity]bool),
	}
	return s
}

// SetSpawnSystem sets the spawn system reference for color counter updates
func (s *DecaySystem) SetSpawnSystem(spawnSystem *SpawnSystem) {
	s.spawnSystem = spawnSystem
}

// Priority returns the system's priority
func (s *DecaySystem) Priority() int {
	return 30
}

// Update runs the decay system
// Phase 3: Animation trigger moved to ClockScheduler, this just updates animation
func (s *DecaySystem) Update(world *engine.World, dt time.Duration) {
	// Phase 3: Check animation state from GameState
	animating := s.ctx.State.GetDecayAnimating()

	// Update animation if active
	if animating {
		s.updateAnimation(world)
	}

	// Phase 3: Timer checking and animation trigger moved to ClockScheduler
	// No need to check timer here
}

// updateAnimation progresses the decay animation
// Phase 3: Uses GameState for startTime
func (s *DecaySystem) updateAnimation(world *engine.World) {
	s.mu.RLock()
	gameHeight := s.gameHeight
	s.mu.RUnlock()

	// Phase 3: Read start time from GameState
	startTime := s.ctx.State.GetDecayStartTime()
	elapsed := s.ctx.TimeProvider.Now().Sub(startTime).Seconds()

	// Update falling entity positions and apply decay
	s.updateFallingEntities(world, elapsed)

	// Check if animation complete based on elapsed time
	// Animation duration is based on the slowest falling entity reaching the bottom
	animationDuration := float64(gameHeight) / constants.FallingDecayMinSpeed
	if elapsed >= animationDuration {
		s.mu.Lock()
		s.currentRow = 0
		s.mu.Unlock()

		// Clean up falling entities and clear decay tracking
		s.cleanupFallingEntities(world)

		// Phase 3: Stop decay animation in GameState (transitions to PhaseNormal)
		s.ctx.State.StopDecayAnimation()
	}
}

// applyDecayToRow applies decay logic to all characters at the given row (for testing/compatibility)
func (s *DecaySystem) applyDecayToRow(world *engine.World, row int) {
	posType := reflect.TypeOf(components.PositionComponent{})
	seqType := reflect.TypeOf(components.SequenceComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		posComp, ok := world.GetComponent(entity, posType)
		if !ok || posComp == nil {
			continue
		}
		pos, ok := posComp.(components.PositionComponent)
		if !ok {
			continue
		}

		if pos.Y == row {
			s.applyDecayToCharacter(world, entity)
		}
	}
}

// applyDecayToCharacter applies decay logic to a single character entity
func (s *DecaySystem) applyDecayToCharacter(world *engine.World, entity engine.Entity) {
	seqType := reflect.TypeOf(components.SequenceComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok || seqComp == nil {
		return // Not a sequence entity
	}
	seq, ok := seqComp.(components.SequenceComponent)
	if !ok {
		return
	}

	// Don't decay gold sequences
	if seq.Type == components.SequenceGold {
		return
	}

	// Store old values for counter updates
	oldType := seq.Type
	oldLevel := seq.Level

	// Apply decay logic
	if seq.Level > components.LevelDark {
		// Reduce level by 1 and update style
		seq.Level--
		world.AddComponent(entity, seq)

		// Update character style
		charComp, ok := world.GetComponent(entity, charType)
		if ok && charComp != nil {
			char, charOk := charComp.(components.CharacterComponent)
			if charOk {
				char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
				world.AddComponent(entity, char)
			}
		}

		// Update counters: decrement old level, increment new level (only for Blue/Green)
		if s.spawnSystem != nil && (oldType == components.SequenceBlue || oldType == components.SequenceGreen) {
			s.spawnSystem.AddColorCount(oldType, oldLevel, -1)
			s.spawnSystem.AddColorCount(seq.Type, seq.Level, 1)
		}
	} else {
		// Level is LevelDark - decay color: Blue → Green → Red → disappear
		if seq.Type == components.SequenceBlue {
			seq.Type = components.SequenceGreen
			seq.Level = components.LevelBright
			world.AddComponent(entity, seq)

			charComp, ok := world.GetComponent(entity, charType)
			if ok && charComp != nil {
				char, charOk := charComp.(components.CharacterComponent)
				if charOk {
					char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
					world.AddComponent(entity, char)
				}
			}

			// Update counters: Blue Dark → Green Bright
			if s.spawnSystem != nil {
				s.spawnSystem.AddColorCount(oldType, oldLevel, -1)
				s.spawnSystem.AddColorCount(seq.Type, seq.Level, 1)
			}
		} else if seq.Type == components.SequenceGreen {
			seq.Type = components.SequenceRed
			seq.Level = components.LevelBright
			world.AddComponent(entity, seq)

			charComp, ok := world.GetComponent(entity, charType)
			if ok && charComp != nil {
				char, charOk := charComp.(components.CharacterComponent)
				if charOk {
					char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
					world.AddComponent(entity, char)
				}
			}

			// Update counters: Green Dark → Red Bright (only decrement Green, Red is not tracked)
			if s.spawnSystem != nil {
				s.spawnSystem.AddColorCount(oldType, oldLevel, -1)
			}
		} else {
			// Red at LevelDark - remove entity (no counter change, Red is not tracked)
			// Safely destroy entity (handles spatial index removal)
			world.SafeDestroyEntity(entity)
		}
	}
}

// spawnFallingEntities creates falling decay character entities
// One entity is created per column to ensure complete column coverage
func (s *DecaySystem) spawnFallingEntities(world *engine.World) {
	// Get gameWidth with lock
	s.mu.RLock()
	gameWidth := s.gameWidth
	s.mu.RUnlock()

	// Create new falling entities list
	newFallingEntities := make([]engine.Entity, 0, gameWidth)

	// Character pool for random selection
	characters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

	// Create one falling entity per column to ensure complete coverage
	for column := 0; column < gameWidth; column++ {
		// Random speed for each entity
		speed := constants.FallingDecayMinSpeed + rand.Float64()*(constants.FallingDecayMaxSpeed-constants.FallingDecayMinSpeed)

		// Random character for each entity
		char := rune(characters[rand.Intn(len(characters))])

		// Create falling entity
		entity := world.CreateEntity()
		world.AddComponent(entity, components.FallingDecayComponent{
			Column:        column,
			YPosition:     0.0,
			Speed:         speed,
			Char:          char,
			LastChangeRow: -1, // Initialize to -1 to trigger change on first row
		})

		newFallingEntities = append(newFallingEntities, entity)
	}

	// Update falling entities list with lock
	s.mu.Lock()
	s.fallingEntities = newFallingEntities
	s.mu.Unlock()
}

// updateFallingEntities updates falling entity positions and applies decay
func (s *DecaySystem) updateFallingEntities(world *engine.World, elapsed float64) {
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	// Get fields with lock
	s.mu.RLock()
	gameHeight := s.gameHeight
	fallingEntities := s.fallingEntities
	s.mu.RUnlock()

	// Track entities to destroy and keep
	remainingEntities := make([]engine.Entity, 0, len(fallingEntities))

	for _, entity := range fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			continue
		}
		fall := fallComp.(components.FallingDecayComponent)

		// Update Y position based on speed and elapsed time
		fall.YPosition = fall.Speed * elapsed

		// Check if entity has passed the bottom boundary
		if fall.YPosition >= float64(gameHeight) {
			// Entity has gone beyond the bottom - destroy immediately
			world.DestroyEntity(entity)
			// Don't add to remaining entities
			continue
		}

		// Calculate current row
		currentRow := int(fall.YPosition)

		// Matrix-style character change: when crossing row boundaries, randomly change character
		// Note: LastChangeRow tracks the last row we checked to ensure we only attempt
		// one change per row. It must be updated on every row to prevent re-checking.
		if currentRow != fall.LastChangeRow {
			// Calculate distance since last row check
			rowsSinceLastChange := currentRow - fall.LastChangeRow
			// Handle initial case when LastChangeRow is -1
			if fall.LastChangeRow < 0 {
				rowsSinceLastChange = currentRow + 1
			}

			// Always update LastChangeRow to current row to prevent re-checking same row
			fall.LastChangeRow = currentRow

			// Only consider changing if minimum rows have passed since last check
			if rowsSinceLastChange >= constants.FallingDecayMinRowsBetweenChanges {
				// Random chance to change character (40% probability)
				if rand.Float64() < constants.FallingDecayChangeChance {
					characters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
					fall.Char = rune(characters[rand.Intn(len(characters))])
				}
			}
		}

		// Entity is still within bounds
		// Update component
		world.AddComponent(entity, fall)

		// Check for character at this position and apply decay
		targetEntity := world.GetEntityAtPosition(fall.Column, currentRow)
		if targetEntity != 0 {
			// Check if already decayed with lock
			s.mu.RLock()
			alreadyDecayed := s.decayedThisFrame[targetEntity]
			s.mu.RUnlock()

			if !alreadyDecayed {
				// Apply decay to this character
				s.applyDecayToCharacter(world, targetEntity)

				// Mark as decayed with lock
				s.mu.Lock()
				s.decayedThisFrame[targetEntity] = true
				s.mu.Unlock()
			}
		}

		// Keep this entity in the list
		remainingEntities = append(remainingEntities, entity)
	}

	// Update the falling entities list with lock
	s.mu.Lock()
	s.fallingEntities = remainingEntities
	s.mu.Unlock()
}

// cleanupFallingEntities removes all falling decay entities
func (s *DecaySystem) cleanupFallingEntities(world *engine.World) {
	s.mu.Lock()
	fallingEntities := s.fallingEntities
	s.fallingEntities = make([]engine.Entity, 0)
	s.decayedThisFrame = make(map[engine.Entity]bool)
	s.mu.Unlock()

	// Destroy entities outside of lock
	for _, entity := range fallingEntities {
		world.DestroyEntity(entity)
	}
}

// TriggerDecayAnimation is called by ClockScheduler to start decay animation
// Phase 3: Required by DecaySystemInterface
func (s *DecaySystem) TriggerDecayAnimation(world *engine.World) {
	// Initialize decay tracking map for the entire animation duration
	s.mu.Lock()
	s.currentRow = 0
	s.decayedThisFrame = make(map[engine.Entity]bool)
	s.mu.Unlock()

	// Spawn falling decay entities
	s.spawnFallingEntities(world)
}

// IsAnimating returns true if decay animation is active
// Phase 3: Reads from GameState
func (s *DecaySystem) IsAnimating() bool {
	return s.ctx.State.GetDecayAnimating()
}

// CurrentRow returns the current decay row being displayed
// Phase 3: Uses GameState for animating check
func (s *DecaySystem) CurrentRow() int {
	s.mu.RLock()
	currentRow := s.currentRow
	gameHeight := s.gameHeight
	s.mu.RUnlock()

	// Phase 3: Check animating from GameState
	animating := s.ctx.State.GetDecayAnimating()

	// When animation is done, currentRow is 0, but we want to avoid displaying row 0
	// During animation, currentRow is the next row to process
	// For display, return the last processed row (currentRow - 1)
	// but clamp to valid range [0, gameHeight-1]
	if !animating {
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
// Phase 3: Reads from GameState
func (s *DecaySystem) GetTimeUntilDecay() float64 {
	return s.ctx.State.GetTimeUntilDecay()
}

// UpdateDimensions updates the game dimensions
// Phase 3: heatIncrement parameter deprecated (no longer used)
func (s *DecaySystem) UpdateDimensions(gameWidth, gameHeight, screenWidth, heatIncrement int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	// Phase 3: No longer storing screenWidth or heatIncrement
}

// GetSystemState returns the current state of the decay system for debugging
// Phase 3: Uses GameState
func (s *DecaySystem) GetSystemState() string {
	s.mu.RLock()
	fallingCount := len(s.fallingEntities)
	s.mu.RUnlock()

	// Phase 3: Read from GameState
	snapshot := s.ctx.State.ReadDecayState()

	if snapshot.Animating {
		startTime := snapshot.StartTime
		elapsed := s.ctx.TimeProvider.Now().Sub(startTime).Seconds()
		return fmt.Sprintf("Decay[animating=true, elapsed=%.2fs, fallingEntities=%d]",
			elapsed, fallingCount)
	} else if snapshot.TimerActive {
		return fmt.Sprintf("Decay[timer=active, timeUntil=%.2fs, nextDecay=%v]",
			snapshot.TimeUntil, snapshot.NextTime)
	}
	return "Decay[inactive]"
}
