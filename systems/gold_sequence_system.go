package systems

import (
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
type GoldSequenceSystem struct {
	mu                  sync.RWMutex       // Protects all fields below
	ctx                 *engine.GameContext
	decaySystem         *DecaySystem
	wasDecayAnimating   bool
	active              bool
	sequenceID          int
	startTime           time.Time
	characters          string
	gameWidth           int
	gameHeight          int
	cursorX             int
	cursorY             int
	cleanerTriggerFunc  func(*engine.World) // Callback to trigger cleaners when gold completed at max heat
	firstUpdate         bool                // Tracks if this is the first update
	initialSpawnTime    time.Time           // Time of first update (for delayed initial spawn)
}

// NewGoldSequenceSystem creates a new gold sequence system
func NewGoldSequenceSystem(ctx *engine.GameContext, decaySystem *DecaySystem, gameWidth, gameHeight, cursorX, cursorY int) *GoldSequenceSystem {
	return &GoldSequenceSystem{
		ctx:               ctx,
		decaySystem:       decaySystem,
		wasDecayAnimating: false,
		active:            false,
		sequenceID:        0,
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
func (s *GoldSequenceSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()
	isDecayAnimating := s.decaySystem.IsAnimating()

	// Read and update wasDecayAnimating atomically
	s.mu.Lock()
	wasDecayAnimating := s.wasDecayAnimating
	s.wasDecayAnimating = isDecayAnimating
	active := s.active
	firstUpdate := s.firstUpdate

	// Handle first update - record time for delayed initial spawn
	if !firstUpdate {
		s.firstUpdate = true
		s.initialSpawnTime = now
	}
	initialSpawnTime := s.initialSpawnTime
	s.mu.Unlock()

	// Spawn gold sequence at game start with delay (150ms)
	const initialSpawnDelay = 150 * time.Millisecond
	if !active && now.Sub(initialSpawnTime) >= initialSpawnDelay && now.Sub(initialSpawnTime) < initialSpawnDelay+100*time.Millisecond {
		// Spawn initial gold sequence after delay
		success := s.spawnGoldSequence(world)
		// EDGE CASE: If first Gold spawn fails, start decay timer anyway
		if !success && s.decaySystem != nil {
			s.decaySystem.StartDecayTimer()
		}
	}

	// Detect transition from decay animating to not animating (decay just ended)
	if wasDecayAnimating && !isDecayAnimating {
		// Decay just ended - spawn gold sequence
		success := s.spawnGoldSequence(world)
		// EDGE CASE: If Gold spawn fails after decay, start decay timer anyway
		if !success && s.decaySystem != nil {
			s.decaySystem.StartDecayTimer()
		}
	}

	// If gold sequence is active, check timeout
	if active {
		s.mu.RLock()
		startTime := s.startTime
		sequenceID := s.sequenceID
		s.mu.RUnlock()

		elapsed := now.Sub(startTime)
		if elapsed >= constants.GoldSequenceDuration {
			// Timeout - remove gold sequence
			s.removeGoldSequence(world, sequenceID)
		}
	}
}

// spawnGoldSequence creates a new gold sequence at a random position on the screen
// Returns true if spawn succeeded, false if spawn failed (e.g., no valid position)
func (s *GoldSequenceSystem) spawnGoldSequence(world *engine.World) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		// Already have an active gold sequence
		return false
	}

	// Generate random 10-character sequence
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = rune(s.characters[rand.Intn(len(s.characters))])
	}

	// Find random valid position (similar to spawn system)
	x, y := s.findValidPosition(world, constants.GoldSequenceLength)
	if x < 0 || y < 0 {
		// No valid position found - spawn failed
		return false
	}

	// Create unique sequence ID
	s.sequenceID++
	sequenceID := s.sequenceID

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

	// Mark gold sequence as active (already holding lock from defer)
	s.active = true
	s.startTime = s.ctx.TimeProvider.Now()
	return true
}

// removeGoldSequence removes all gold sequence entities from the world
// and triggers decay timer restart
func (s *GoldSequenceSystem) removeGoldSequence(world *engine.World, sequenceID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	// Only remove if the sequenceID matches
	if sequenceID != s.sequenceID {
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

	s.active = false

	// IMPORTANT: Start decay timer now that Gold sequence has ended
	// Timer interval is calculated based on current heat value
	if s.decaySystem != nil {
		s.decaySystem.StartDecayTimer()
	}
}

// IsActive returns whether a gold sequence is currently active
func (s *GoldSequenceSystem) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// GetSequenceID returns the current gold sequence ID
func (s *GoldSequenceSystem) GetSequenceID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sequenceID
}

// GetExpectedCharacter returns the expected character at the given index for the active gold sequence
// Returns 0 and false if no active gold sequence or index is invalid
func (s *GoldSequenceSystem) GetExpectedCharacter(sequenceID int, index int) (rune, bool) {
	s.mu.RLock()
	active := s.active
	currentSeqID := s.sequenceID
	s.mu.RUnlock()

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
func (s *GoldSequenceSystem) CompleteGoldSequence(world *engine.World) bool {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		log.Printf("[GOLD] CompleteGoldSequence called but sequence not active")
		return false
	}
	sequenceID := s.sequenceID
	s.mu.Unlock()

	log.Printf("[GOLD] CompleteGoldSequence - removing gold sequence entities")

	// Remove gold sequence entities (has its own locking)
	// This will also trigger decay timer restart
	s.removeGoldSequence(world, sequenceID)

	// Fill heat to max (handled by ScoreSystem)
	return true
}

// findValidPosition finds a valid random position for the gold sequence
// Must be called with s.mu locked
func (s *GoldSequenceSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	// Read dimensions and cursor position (caller already holds lock)
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

// SetCleanerTrigger sets the callback function to trigger cleaners
func (s *GoldSequenceSystem) SetCleanerTrigger(triggerFunc func(*engine.World)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanerTriggerFunc = triggerFunc
}

// TriggerCleanersIfHeatFull triggers cleaners if heat is at maximum
// This is called by ScoreSystem when gold sequence is completed
func (s *GoldSequenceSystem) TriggerCleanersIfHeatFull(world *engine.World, currentHeat, maxHeat int) {
	s.mu.RLock()
	triggerFunc := s.cleanerTriggerFunc
	s.mu.RUnlock()

	// DEBUG: Log trigger attempt
	log.Printf("[GOLD] TriggerCleanersIfHeatFull called: currentHeat=%d, maxHeat=%d, hasFunc=%v",
		currentHeat, maxHeat, triggerFunc != nil)

	if triggerFunc == nil {
		log.Printf("[GOLD] ISSUE: Cleaner trigger function is nil!")
		return
	}

	// Only trigger if heat is already at max (or higher)
	if currentHeat >= maxHeat {
		log.Printf("[GOLD] Heat condition MET - triggering cleaners!")
		// Call outside lock to avoid potential deadlock
		triggerFunc(world)
	} else {
		log.Printf("[GOLD] Heat condition NOT met - cleaners NOT triggered")
	}
}
