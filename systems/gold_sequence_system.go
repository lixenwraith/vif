package systems

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// GoldSequenceSystem manages the gold sequence mechanic
// Migrated to use GameState for gold state management
type GoldSequenceSystem struct {
	mu          sync.RWMutex
	ctx         *engine.GameContext
	decaySystem *DecaySystem
}

// NewGoldSequenceSystem creates a new gold sequence system
func NewGoldSequenceSystem(ctx *engine.GameContext, decaySystem *DecaySystem, gameWidth, gameHeight, cursorX, cursorY int) *GoldSequenceSystem {
	return &GoldSequenceSystem{
		ctx:         ctx,
		decaySystem: decaySystem,
	}
}

// Priority returns the system's priority (runs between spawn and decay)
func (s *GoldSequenceSystem) Priority() int {
	return 20
}

// Update runs the gold sequence system logic
// Gold timeout is now handled by ClockScheduler
func (s *GoldSequenceSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()

	// Initialize FirstUpdateTime on first call (using GameState)
	s.ctx.State.SetFirstUpdateTime(now)
	firstUpdateTime := s.ctx.State.GetFirstUpdateTime()

	// Read state snapshots from GameState for consistent reads
	goldSnapshot := s.ctx.State.ReadGoldState()
	phaseSnapshot := s.ctx.State.ReadPhaseState()
	initialSpawnComplete := s.ctx.State.GetInitialSpawnComplete()

	// Spawn gold sequence at game start with delay (150ms)
	const initialSpawnDelay = 150 * time.Millisecond
	if !goldSnapshot.Active && !initialSpawnComplete && now.Sub(firstUpdateTime) >= initialSpawnDelay {
		// Spawn initial gold sequence after delay
		// If spawn fails, system will remain in PhaseNormal and can retry on next update
		if s.spawnGoldSequence(world) {
			// Mark initial spawn as complete (whether it succeeded or not)
			s.ctx.State.SetInitialSpawnComplete()
		}
	}

	// Detect transition from decay animation to normal phase (decay just ended)
	// Phase transitions: PhaseDecayAnimation -> PhaseNormal (handled by DecaySystem.StopDecayAnimation)
	// When we detect PhaseNormal and no active gold, spawn new gold
	if !goldSnapshot.Active && phaseSnapshot.Phase == engine.PhaseNormal && initialSpawnComplete {
		// Decay ended and returned to normal phase - spawn gold sequence
		// If spawn fails, system will remain in PhaseNormal and can retry on next update
		s.spawnGoldSequence(world)
	}

	// Gold timeout is now handled by ClockScheduler
	// No need to check timeout here
}

// spawnGoldSequence creates a new gold sequence at a random position on the screen
// Returns true if spawn succeeded, false if spawn failed (e.g., no valid position)
func (s *GoldSequenceSystem) spawnGoldSequence(world *engine.World) bool {
	// Read phase and gold state snapshots for consistent checks
	phaseSnapshot := s.ctx.State.ReadPhaseState()
	goldSnapshot := s.ctx.State.ReadGoldState()

	// Phase consistency check: Gold can only spawn in PhaseNormal
	if phaseSnapshot.Phase != engine.PhaseNormal {
		return false
	}

	// Check active state from snapshot
	if goldSnapshot.Active {
		// Already have an active gold sequence
		return false
	}

	s.mu.Lock()
	// Generate random 10-character sequence
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = rune(constants.AlphanumericString[rand.Intn(len(constants.AlphanumericString))])
	}

	// Find random valid position (similar to spawn system)
	x, y := s.findValidPosition(world, constants.GoldSequenceLength)
	s.mu.Unlock()

	if x < 0 || y < 0 {
		// No valid position found - spawn failed
		return false
	}

	// Get next sequence ID from GameState
	sequenceID := s.ctx.State.IncrementGoldSequenceID()

	// Get style for gold sequence
	style := render.GetStyleForSequence(components.SequenceGold, components.LevelBright)

	// Begin spatial transaction for atomic gold sequence creation
	tx := world.BeginSpatialTransaction()

	// Track created entities for cleanup if needed
	createdEntities := make([]engine.Entity, 0, constants.GoldSequenceLength)

	// Create entities for each character in the gold sequence
	for i := 0; i < constants.GoldSequenceLength; i++ {
		entity := world.CreateEntity()
		createdEntities = append(createdEntities, entity)

		// Add position component
		world.AddComponent(entity, components.PositionComponent{
			X: x + i,
			Y: y,
		})

		// Add character component
		world.AddComponent(entity, components.CharacterComponent{
			Rune:  sequence[i],
			Style: style,
		})

		// Add sequence component
		world.AddComponent(entity, components.SequenceComponent{
			ID:    sequenceID,
			Index: i,
			Type:  components.SequenceGold,
			Level: components.LevelBright,
		})

		// Add spawn operation to transaction
		result := tx.Spawn(entity, x+i, y)
		if result.HasCollision {
			// Collision detected - rollback and cleanup
			tx.Rollback()
			for _, e := range createdEntities {
				world.DestroyEntity(e)
			}
			return false
		}
	}

	// Commit spatial transaction atomically
	tx.Commit()

	// Activate gold sequence in GameState (sets phase to PhaseGoldActive)
	if !s.ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration) {
		// Phase transition failed - clean up created entities
		for _, entity := range createdEntities {
			world.SafeDestroyEntity(entity)
		}
		return false
	}
	return true
}

// removeGoldSequence removes all gold sequence entities from the world
// Now uses GameState for state management
func (s *GoldSequenceSystem) removeGoldSequence(world *engine.World, sequenceID int) {
	// Read gold state snapshot for consistent check
	goldSnapshot := s.ctx.State.ReadGoldState()

	// Check active state from snapshot
	if !goldSnapshot.Active {
		return
	}

	// Only remove if the sequenceID matches
	if sequenceID != goldSnapshot.SequenceID {
		return
	}

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		// Only remove gold sequence entities with our ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			// Safely destroy entity (handles spatial index removal)
			world.SafeDestroyEntity(entity)
		}
	}

	// Check current phase - if cleaners are pending, don't deactivate gold
	// (cleaners were requested while gold was active, so we're already in PhaseCleanerPending)
	phaseSnapshot := s.ctx.State.ReadPhaseState()
	if phaseSnapshot.Phase == engine.PhaseCleanerPending || phaseSnapshot.Phase == engine.PhaseCleanerActive {
		// Cleaners are pending/active - gold entities removed, but stay in cleaner phase
		// Don't call DeactivateGoldSequence as it would try to transition to PhaseGoldComplete
		return
	}

	// Deactivate gold sequence in GameState (transitions to PhaseGoldComplete)
	if !s.ctx.State.DeactivateGoldSequence() {
		// Phase transition failed - this shouldn't happen but log for debugging
		return
	}

	// Note: Decay timer will be started by ClockScheduler when it sees PhaseGoldComplete
	// No need to start it here
}

// TimeoutGoldSequence is called by ClockScheduler when gold sequence times out
// Required by GoldSequenceSystemInterface
func (s *GoldSequenceSystem) TimeoutGoldSequence(world *engine.World) {
	// Read gold state snapshot to get current sequence ID
	goldSnapshot := s.ctx.State.ReadGoldState()
	// Remove gold sequence entities (also starts decay timer)
	s.removeGoldSequence(world, goldSnapshot.SequenceID)
}

// IsActive returns whether a gold sequence is currently active
// Reads from GameState snapshot
func (s *GoldSequenceSystem) IsActive() bool {
	goldSnapshot := s.ctx.State.ReadGoldState()
	return goldSnapshot.Active
}

// GetSequenceID returns the current gold sequence ID
// Reads from GameState snapshot
func (s *GoldSequenceSystem) GetSequenceID() int {
	goldSnapshot := s.ctx.State.ReadGoldState()
	return goldSnapshot.SequenceID
}

// GetExpectedCharacter returns the expected character at the given index for the active gold sequence
// Returns 0 and false if no active gold sequence or index is invalid
// Uses GameState snapshot for active check
func (s *GoldSequenceSystem) GetExpectedCharacter(sequenceID int, index int) (rune, bool) {
	// Read gold state snapshot for consistent check
	goldSnapshot := s.ctx.State.ReadGoldState()

	if !goldSnapshot.Active || sequenceID != goldSnapshot.SequenceID {
		return 0, false
	}

	// Find the entity with this sequence ID and index
	seqType := reflect.TypeOf(components.SequenceComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := s.ctx.World.GetEntitiesWith(seqType, charType)

	for _, entity := range entities {
		seqComp, ok := s.ctx.World.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		if seq.Type == components.SequenceGold && seq.ID == sequenceID && seq.Index == index {
			charComp, ok := s.ctx.World.GetComponent(entity, charType)
			if !ok {
				return 0, false
			}
			char := charComp.(components.CharacterComponent)
			return char.Rune, true
		}
	}

	return 0, false
}

// CompleteGoldSequence is called when the gold sequence is successfully completed
// Gold removal triggers decay timer restart in removeGoldSequence()
// Uses GameState snapshot
func (s *GoldSequenceSystem) CompleteGoldSequence(world *engine.World) bool {
	// Read gold state snapshot for consistent check
	goldSnapshot := s.ctx.State.ReadGoldState()

	if !goldSnapshot.Active {
		return false
	}

	// Remove gold sequence entities
	// This will also trigger decay timer restart
	s.removeGoldSequence(world, goldSnapshot.SequenceID)

	// Fill heat to max (handled by ScoreSystem)
	return true
}

// findValidPosition finds a valid random position for the gold sequence
// Caller holds s.mu lock
func (s *GoldSequenceSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	// Read dimensions from context
	gameWidth := s.ctx.GameWidth
	gameHeight := s.ctx.GameHeight

	// Read cursor position from GameState (atomic reads)
	cursor := s.ctx.State.ReadCursorPosition()

	maxAttempts := 100
	for attempt := 0; attempt < maxAttempts; attempt++ {
		x := rand.Intn(gameWidth)
		y := rand.Intn(gameHeight)

		// Check if far enough from cursor (same exclusion zone as spawn system)
		if math.Abs(float64(x-cursor.X)) <= 5 || math.Abs(float64(y-cursor.Y)) <= 3 {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > gameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if world.GetEntityAtPosition(x+i, y) != 0 {
				overlaps = true
				break
			}
		}

		if !overlaps {
			return x, y
		}
	}

	return -1, -1 // No valid position found
}

// Removed SetCleanerTrigger and TriggerCleanersIfHeatFull
// Cleaner triggering is now managed by GameState.RequestCleaners() and ClockScheduler

// GetSystemState returns the current state of the gold sequence system for debugging
// Uses GameState
func (s *GoldSequenceSystem) GetSystemState() string {
	// Read from GameState
	snapshot := s.ctx.State.ReadGoldState()

	if snapshot.Active {
		return fmt.Sprintf("Gold[active=true, sequenceID=%d, timeRemaining=%.2fs]",
			snapshot.SequenceID, snapshot.Remaining.Seconds())
	}
	return "Gold[inactive]"
}