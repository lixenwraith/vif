package systems

import (
	"math/rand"
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	animating       bool
	currentRow      int
	startTime       time.Time
	lastUpdate      time.Time
	nextDecayTime   time.Time // When the next decay will trigger
	gameWidth       int
	gameHeight      int
	screenWidth     int
	heatIncrement   int
	ctx             *engine.GameContext
	spawnSystem     *SpawnSystem
	fallingEntities []engine.Entity // Entities representing falling decay characters
	decayedThisFrame map[engine.Entity]bool // Track which entities were decayed this frame
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(gameWidth, gameHeight, screenWidth, heatIncrement int, ctx *engine.GameContext) *DecaySystem {
	s := &DecaySystem{
		animating:        false,
		currentRow:       0,
		lastUpdate:       ctx.TimeProvider.Now(),
		gameWidth:        gameWidth,
		gameHeight:       gameHeight,
		screenWidth:      screenWidth,
		heatIncrement:    heatIncrement,
		ctx:              ctx,
		fallingEntities:  make([]engine.Entity, 0),
		decayedThisFrame: make(map[engine.Entity]bool),
	}
	s.startTicker()
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
func (s *DecaySystem) Update(world *engine.World, dt time.Duration) {
	// Update animation if active
	if s.animating {
		s.updateAnimation(world)
	} else {
		// Check if it's time to start decay animation
		now := s.ctx.TimeProvider.Now()
		if now.After(s.nextDecayTime) || now.Equal(s.nextDecayTime) {
			s.animating = true
			s.currentRow = 0
			s.startTime = now
			// Spawn falling decay entities
			s.spawnFallingEntities(world)
		}
		// Timer is only recalculated after decay animation completes
	}
}

// updateAnimation progresses the decay animation
func (s *DecaySystem) updateAnimation(world *engine.World) {
	elapsed := s.ctx.TimeProvider.Now().Sub(s.startTime).Seconds()

	// Clear the decayed tracking map for this frame
	s.decayedThisFrame = make(map[engine.Entity]bool)

	// Update falling entity positions and apply decay
	s.updateFallingEntities(world, elapsed)

	// Check if animation complete based on elapsed time
	// Animation duration is based on the slowest falling entity reaching the bottom
	animationDuration := float64(s.gameHeight) / constants.FallingDecayMinSpeed
	if elapsed >= animationDuration {
		s.animating = false
		s.currentRow = 0
		// Clean up falling entities
		s.cleanupFallingEntities(world)
		// Schedule next decay
		interval := s.calculateInterval()
		s.nextDecayTime = s.ctx.TimeProvider.Now().Add(interval)
	}
}

// applyDecayToRow applies decay logic to all characters at the given row (for testing/compatibility)
func (s *DecaySystem) applyDecayToRow(world *engine.World, row int) {
	posType := reflect.TypeOf(components.PositionComponent{})
	seqType := reflect.TypeOf(components.SequenceComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

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
	if !ok {
		return // Not a sequence entity
	}
	seq := seqComp.(components.SequenceComponent)

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
		if ok {
			char := charComp.(components.CharacterComponent)
			char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
			world.AddComponent(entity, char)
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
			if ok {
				char := charComp.(components.CharacterComponent)
				char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
				world.AddComponent(entity, char)
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
			if ok {
				char := charComp.(components.CharacterComponent)
				char.Style = render.GetStyleForSequence(seq.Type, seq.Level)
				world.AddComponent(entity, char)
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
	// Clear any existing falling entities
	s.fallingEntities = make([]engine.Entity, 0)

	// Character pool for random selection
	characters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

	// Create one falling entity per column to ensure complete coverage
	for column := 0; column < s.gameWidth; column++ {
		// Random speed for each entity
		speed := constants.FallingDecayMinSpeed + rand.Float64()*(constants.FallingDecayMaxSpeed-constants.FallingDecayMinSpeed)

		// Random character for each entity
		char := rune(characters[rand.Intn(len(characters))])

		// Create falling entity
		entity := world.CreateEntity()
		world.AddComponent(entity, components.FallingDecayComponent{
			Column:    column,
			YPosition: 0.0,
			Speed:     speed,
			Char:      char,
		})

		s.fallingEntities = append(s.fallingEntities, entity)
	}
}

// updateFallingEntities updates falling entity positions and applies decay
func (s *DecaySystem) updateFallingEntities(world *engine.World, elapsed float64) {
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	for _, entity := range s.fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			continue
		}
		fall := fallComp.(components.FallingDecayComponent)

		// Update Y position based on speed and elapsed time
		fall.YPosition = fall.Speed * elapsed

		// Check if within game bounds
		if fall.YPosition < float64(s.gameHeight) {
			// Update component
			world.AddComponent(entity, fall)

			// Check for character at this position and apply decay
			currentRow := int(fall.YPosition)
			targetEntity := world.GetEntityAtPosition(fall.Column, currentRow)
			if targetEntity != 0 && !s.decayedThisFrame[targetEntity] {
				// Apply decay to this character
				s.applyDecayToCharacter(world, targetEntity)
				s.decayedThisFrame[targetEntity] = true
			}
		}
	}
}

// cleanupFallingEntities removes all falling decay entities
func (s *DecaySystem) cleanupFallingEntities(world *engine.World) {
	for _, entity := range s.fallingEntities {
		world.DestroyEntity(entity)
	}
	s.fallingEntities = make([]engine.Entity, 0)
	s.decayedThisFrame = make(map[engine.Entity]bool)
}

// startTicker initializes the decay timer (called once at startup)
func (s *DecaySystem) startTicker() {
	interval := s.calculateInterval()
	s.nextDecayTime = s.ctx.TimeProvider.Now().Add(interval)
	s.lastUpdate = s.ctx.TimeProvider.Now()
}

// calculateInterval calculates the decay interval based on heat
// Formula: DecayIntervalBaseSeconds - DecayIntervalRangeSeconds * (heat_filled / heat_max)
// Empty heat bar (0): 60 - 50 * 0 = 60 seconds
// Full heat bar (max): 60 - 50 * 1 = 10 seconds
func (s *DecaySystem) calculateInterval() time.Duration {
	heatBarWidth := s.screenWidth - constants.HeatBarIndicatorWidth
	if heatBarWidth < 1 {
		heatBarWidth = 1
	}

	heatPercentage := float64(s.heatIncrement) / float64(heatBarWidth)
	if heatPercentage > 1.0 {
		heatPercentage = 1.0
	}
	if heatPercentage < 0.0 {
		heatPercentage = 0.0
	}

	// Formula: base - range * heat_percentage
	intervalSeconds := constants.DecayIntervalBaseSeconds - constants.DecayIntervalRangeSeconds*heatPercentage
	return time.Duration(intervalSeconds * float64(time.Second))
}

// IsAnimating returns true if decay animation is active
func (s *DecaySystem) IsAnimating() bool {
	return s.animating
}

// CurrentRow returns the current decay row being displayed
func (s *DecaySystem) CurrentRow() int {
	// When animation is done, currentRow is 0, but we want to avoid displaying row 0
	// During animation, currentRow is the next row to process
	// For display, return the last processed row (currentRow - 1)
	// but clamp to valid range [0, gameHeight-1]
	if !s.animating {
		return 0
	}
	if s.currentRow > 0 {
		displayRow := s.currentRow - 1
		if displayRow >= s.gameHeight {
			return s.gameHeight - 1
		}
		return displayRow
	}
	return 0
}

// GetTimeUntilDecay returns seconds until next decay trigger
func (s *DecaySystem) GetTimeUntilDecay() float64 {
	if s.animating {
		return 0.0
	}
	remaining := s.nextDecayTime.Sub(s.ctx.TimeProvider.Now()).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// UpdateDimensions updates the game dimensions
func (s *DecaySystem) UpdateDimensions(gameWidth, gameHeight, screenWidth, heatIncrement int) {
	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	s.screenWidth = screenWidth
	s.heatIncrement = heatIncrement
}
