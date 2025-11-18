package systems

import (
	"fmt"
	"log"
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
// Phase 3: Migrated to use GameState for gold state management
type GoldSequenceSystem struct {
	mu                  sync.RWMutex       // Protects fields below
	ctx                 *engine.GameContext
	decaySystem         *DecaySystem
	wasDecayAnimating   bool
	// Phase 3: Removed active, sequenceID, startTime - now in GameState
	// Phase 6: Removed cleanerTriggerFunc - now managed by GameState/ClockScheduler
	characters       string
	gameWidth        int
	gameHeight       int
	cursorX          int
	cursorY          int
	firstUpdate      bool      // Tracks if this is the first update
	initialSpawnTime time.Time // Time of first update (for delayed initial spawn)
}

// NewGoldSequenceSystem creates a new gold sequence system
func NewGoldSequenceSystem(ctx *engine.GameContext, decaySystem *DecaySystem, gameWidth, gameHeight, cursorX, cursorY int) *GoldSequenceSystem {
	return &GoldSequenceSystem{
		ctx:               ctx,
		decaySystem:       decaySystem,
		wasDecayAnimating: false,
		// Phase 3: active, sequenceID, startTime now in GameState
		characters:        "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		gameWidth:         gameWidth,
		gameHeight:        gameHeight,
		cursorX:           cursorX,
		cursorY:           cursorY,
		firstUpdate:       false,
	}
}

// Priority returns the system's priority (runs between spawn and decay)
func (s *GoldSequenceSystem) Priority() int {
	return 20
}

// Update runs the gold sequence system logic
// Phase 3: Gold timeout is now handled by ClockScheduler
func (s *GoldSequenceSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()
	isDecayAnimating := s.ctx.State.GetDecayAnimating()

	// Read and update wasDecayAnimating atomically
	s.mu.Lock()
	wasDecayAnimating := s.wasDecayAnimating
	s.wasDecayAnimating = isDecayAnimating
	firstUpdate := s.firstUpdate

	// Handle first update - record time for delayed initial spawn
	if !firstUpdate {
		s.firstUpdate = true
		s.initialSpawnTime = now
	}
	initialSpawnTime := s.initialSpawnTime
	s.mu.Unlock()

	// Phase 3: Read active state from GameState
	active := s.ctx.State.GetGoldActive()

	// Spawn gold sequence at game start with delay (150ms)
	const initialSpawnDelay = 150 * time.Millisecond
	if !active && now.Sub(initialSpawnTime) >= initialSpawnDelay && now.Sub(initialSpawnTime) < initialSpawnDelay+100*time.Millisecond {
		// Spawn initial gold sequence after delay
		success := s.spawnGoldSequence(world)
		// EDGE CASE: If first Gold spawn fails, start decay timer anyway
		if !success {
			// Phase 3: Start decay timer on GameState (reads heat atomically)
			s.ctx.State.StartDecayTimer(
				s.ctx.State.ScreenWidth,
				constants.HeatBarIndicatorWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)
		}
	}

	// Detect transition from decay animating to not animating (decay just ended)
	if wasDecayAnimating && !isDecayAnimating {
		// Decay just ended - spawn gold sequence
		success := s.spawnGoldSequence(world)
		// EDGE CASE: If Gold spawn fails after decay, start decay timer anyway
		if !success {
			// Phase 3: Start decay timer on GameState (reads heat atomically)
			s.ctx.State.StartDecayTimer(
				s.ctx.State.ScreenWidth,
				constants.HeatBarIndicatorWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)
		}
	}

	// Phase 3: Gold timeout is now handled by ClockScheduler
	// No need to check timeout here
}

// spawnGoldSequence creates a new gold sequence at a random position on the screen
// Returns true if spawn succeeded, false if spawn failed (e.g., no valid position)
func (s *GoldSequenceSystem) spawnGoldSequence(world *engine.World) bool {
	// Phase 3: Check active state from GameState
	if s.ctx.State.GetGoldActive() {
		// Already have an active gold sequence
		return false
	}

	s.mu.Lock()
	// Generate random 10-character sequence
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = rune(s.characters[rand.Intn(len(s.characters))])
	}

	// Find random valid position (similar to spawn system)
	x, y := s.findValidPosition(world, constants.GoldSequenceLength)
	s.mu.Unlock()

	if x < 0 || y < 0 {
		// No valid position found - spawn failed
		return false
	}

	// Phase 3: Get next sequence ID from GameState
	sequenceID := s.ctx.State.IncrementGoldSequenceID()

	// Get style for gold sequence
	style := render.GetStyleForSequence(components.SequenceGold, components.LevelBright)

	// Create entities for each character in the gold sequence
	for i := 0; i < constants.GoldSequenceLength; i++ {
		entity := world.CreateEntity()

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

		// Update spatial index
		world.UpdateSpatialIndex(entity, x+i, y)
	}

	// Phase 3: Activate gold sequence in GameState (sets phase to PhaseGoldActive)
	s.ctx.State.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)
	return true
}

// removeGoldSequence removes all gold sequence entities from the world
// Phase 3: Now uses GameState for state management
func (s *GoldSequenceSystem) removeGoldSequence(world *engine.World, sequenceID int) {
	// Phase 3: Check active state from GameState
	if !s.ctx.State.GetGoldActive() {
		return
	}

	// Only remove if the sequenceID matches
	if sequenceID != s.ctx.State.GetGoldSequenceID() {
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

	// Phase 3: Deactivate gold sequence in GameState
	s.ctx.State.DeactivateGoldSequence()

	// Phase 3: Start decay timer on GameState (reads heat atomically, no cached value)
	s.ctx.State.StartDecayTimer(
		s.ctx.State.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)
}

// TimeoutGoldSequence is called by ClockScheduler when gold sequence times out
// Phase 3: Required by GoldSequenceSystemInterface
func (s *GoldSequenceSystem) TimeoutGoldSequence(world *engine.World) {
	// Get current gold sequence ID from GameState
	sequenceID := s.ctx.State.GetGoldSequenceID()
	// Remove gold sequence entities (also starts decay timer)
	s.removeGoldSequence(world, sequenceID)
}

// IsActive returns whether a gold sequence is currently active
// Phase 3: Reads from GameState
func (s *GoldSequenceSystem) IsActive() bool {
	return s.ctx.State.GetGoldActive()
}

// GetSequenceID returns the current gold sequence ID
// Phase 3: Reads from GameState
func (s *GoldSequenceSystem) GetSequenceID() int {
	return s.ctx.State.GetGoldSequenceID()
}

// GetExpectedCharacter returns the expected character at the given index for the active gold sequence
// Returns 0 and false if no active gold sequence or index is invalid
// Phase 3: Uses GameState for active check
func (s *GoldSequenceSystem) GetExpectedCharacter(sequenceID int, index int) (rune, bool) {
	// Phase 3: Read from GameState
	active := s.ctx.State.GetGoldActive()
	currentSeqID := s.ctx.State.GetGoldSequenceID()

	if !active || sequenceID != currentSeqID {
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
// Phase 3: Uses GameState
func (s *GoldSequenceSystem) CompleteGoldSequence(world *engine.World) bool {
	// Phase 3: Read from GameState
	if !s.ctx.State.GetGoldActive() {
		log.Printf("[GOLD] CompleteGoldSequence called but sequence not active")
		return false
	}
	sequenceID := s.ctx.State.GetGoldSequenceID()

	log.Printf("[GOLD] CompleteGoldSequence - removing gold sequence entities")

	// Remove gold sequence entities
	// This will also trigger decay timer restart
	s.removeGoldSequence(world, sequenceID)

	// Fill heat to max (handled by ScoreSystem)
	return true
}

// findValidPosition finds a valid random position for the gold sequence
// Phase 3: Requires s.mu lock for reading dimensions
func (s *GoldSequenceSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	// Read dimensions and cursor position (caller holds lock)
	gameWidth := s.gameWidth
	gameHeight := s.gameHeight
	cursorX := s.cursorX
	cursorY := s.cursorY

	maxAttempts := 100
	for attempt := 0; attempt < maxAttempts; attempt++ {
		x := rand.Intn(gameWidth)
		y := rand.Intn(gameHeight)

		// Check if far enough from cursor (same exclusion zone as spawn system)
		if math.Abs(float64(x-cursorX)) <= 5 || math.Abs(float64(y-cursorY)) <= 3 {
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

// UpdateDimensions updates the game area dimensions and cursor position
func (s *GoldSequenceSystem) UpdateDimensions(gameWidth, gameHeight, cursorX, cursorY int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	s.cursorX = cursorX
	s.cursorY = cursorY
}

// Phase 6: Removed SetCleanerTrigger and TriggerCleanersIfHeatFull
// Cleaner triggering is now managed by GameState.RequestCleaners() and ClockScheduler

// GetSystemState returns the current state of the gold sequence system for debugging
// Phase 3: Uses GameState
func (s *GoldSequenceSystem) GetSystemState() string {
	// Phase 3: Read from GameState
	snapshot := s.ctx.State.ReadGoldState()

	if snapshot.Active {
		return fmt.Sprintf("Gold[active=true, sequenceID=%d, timeRemaining=%.2fs]",
			snapshot.SequenceID, snapshot.Remaining.Seconds())
	}
	return "Gold[inactive]"
}
