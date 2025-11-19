package systems

import (
	"math"
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ScoreSystem handles character typing and score calculation
type ScoreSystem struct {
	ctx               *engine.GameContext
	lastCorrect       time.Time
	errorCursorSet    bool
	goldSequenceSystem *GoldSequenceSystem
	spawnSystem       *SpawnSystem
	nuggetSystem      *NuggetSystem
}

// NewScoreSystem creates a new score system
func NewScoreSystem(ctx *engine.GameContext) *ScoreSystem {
	return &ScoreSystem{
		ctx:         ctx,
		lastCorrect: ctx.TimeProvider.Now(),
	}
}

// Priority returns the system's priority
func (s *ScoreSystem) Priority() int {
	return 10 // High priority, run before other systems
}

// SetGoldSequenceSystem sets the gold sequence system reference
func (s *ScoreSystem) SetGoldSequenceSystem(goldSystem *GoldSequenceSystem) {
	s.goldSequenceSystem = goldSystem
}

// SetSpawnSystem sets the spawn system reference for color counter updates
func (s *ScoreSystem) SetSpawnSystem(spawnSystem *SpawnSystem) {
	s.spawnSystem = spawnSystem
}

// SetNuggetSystem sets the nugget system reference for nugget collection
func (s *ScoreSystem) SetNuggetSystem(nuggetSystem *NuggetSystem) {
	s.nuggetSystem = nuggetSystem
}

// Update runs the score system (unused for now, character typing is event-driven)
func (s *ScoreSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()

	// Clear error cursor after timeout
	if s.ctx.State.GetCursorError() && now.Sub(s.ctx.State.GetCursorErrorTime()) > constants.ErrorCursorTimeout {
		s.ctx.State.SetCursorError(false)
	}

	// Clear score blink after timeout
	if s.ctx.State.GetScoreBlinkActive() && now.Sub(s.ctx.State.GetScoreBlinkTime()) > constants.ScoreBlinkTimeout {
		s.ctx.State.SetScoreBlinkActive(false)
	}
}

// HandleCharacterTyping processes a character typed in insert mode
func (s *ScoreSystem) HandleCharacterTyping(world *engine.World, cursorX, cursorY int, typedRune rune) {
	now := s.ctx.TimeProvider.Now()

	// Get entity at cursor position
	entity := world.GetEntityAtPosition(cursorX, cursorY)
	if entity == 0 {
		// No character at cursor - flash error cursor and deactivate boost
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		s.ctx.State.SetHeat(0) // Reset heat
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0) // 0 = None
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
		return
	}

	// Get character component
	charType := reflect.TypeOf(components.CharacterComponent{})
	charComp, ok := world.GetComponent(entity, charType)
	if !ok {
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		s.ctx.State.SetHeat(0)
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0)
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
		return
	}
	char := charComp.(components.CharacterComponent)

	// Check if this is a nugget - handle before sequence logic
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if _, hasNugget := world.GetComponent(entity, nuggetType); hasNugget && s.nuggetSystem != nil {
		// Handle nugget collection (requires matching character)
		s.handleNuggetCollection(world, entity, char, typedRune, cursorX, cursorY)
		return
	}

	// Get sequence component
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		s.ctx.State.SetHeat(0)
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0)
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
		return
	}
	seq := seqComp.(components.SequenceComponent)

	// Check if this is a gold sequence character
	if seq.Type == components.SequenceGold && s.goldSequenceSystem != nil && s.goldSequenceSystem.IsActive() {
		// Handle gold sequence typing
		s.handleGoldSequenceTyping(world, entity, char, seq, typedRune, cursorX, cursorY)
		return
	}

	// Check if typed character matches
	if char.Rune == typedRune {
		// Correct character
		// RED characters reset heat instead of incrementing it
		if seq.Type == components.SequenceRed {
			s.ctx.State.SetHeat(0)
			// Red character also deactivates boost
			s.ctx.State.SetBoostEnabled(false)
			s.ctx.State.SetBoostColor(0) // 0 = None
		} else {
			// Read boost state snapshot for consistent checks
			boostState := s.ctx.State.ReadBoostState()

			// Apply heat gain with boost multiplier
			heatGain := 1
			if boostState.Enabled {
				heatGain = 2
			}
			s.ctx.State.AddHeat(heatGain)

			// Get max heat (heat bar width)
			heatBarWidth := s.ctx.Width
			if heatBarWidth < 1 {
				heatBarWidth = 1
			}

			// Handle boost activation and maintenance
			currentHeat := s.ctx.State.GetHeat()

			// Convert sequence type to color code: 1=Blue, 2=Green
			var charColorCode int32
			if seq.Type == components.SequenceBlue {
				charColorCode = 1
			} else if seq.Type == components.SequenceGreen {
				charColorCode = 2
			}

			if currentHeat >= heatBarWidth {
				// Heat is at max
				if !boostState.Enabled {
					// Activate boost for the first time
					s.ctx.State.SetBoostEnabled(true)
					s.ctx.State.SetBoostColor(charColorCode)
					s.ctx.State.SetBoostEndTime(now.Add(constants.BoostExtensionDuration))
				} else if boostState.Color == charColorCode {
					// Same color - extend boost timer
					s.extendBoost(constants.BoostExtensionDuration)
				} else {
					// Different color - reset boost timer but keep heat at max
					// Set timer to current time (effectively deactivates until rebuilt)
					s.ctx.State.SetBoostEndTime(now)
					s.ctx.State.SetBoostEnabled(false)
					// Update color for potential rebuild
					s.ctx.State.SetBoostColor(charColorCode)
				}
			}
		}
		s.lastCorrect = now

		// Calculate points: increment * level_multiplier * (red?-1:1)
		levelMultipliers := map[components.SequenceLevel]int{
			components.LevelDark:   1,
			components.LevelNormal: 2,
			components.LevelBright: 3,
		}
		levelMult := levelMultipliers[seq.Level]
		points := s.ctx.State.GetHeat() * levelMult

		// Red characters give negative points
		if seq.Type == components.SequenceRed {
			points = -points
		}

		s.ctx.State.AddScore(points)

		// Trigger score blink with character type and level
		s.ctx.State.SetScoreBlinkActive(true)
		// Map sequence types to uint32: 0=error, 1=blue, 2=green, 3=red, 4=gold
		var typeCode uint32
		switch seq.Type {
		case components.SequenceBlue:
			typeCode = 1
		case components.SequenceGreen:
			typeCode = 2
		case components.SequenceRed:
			typeCode = 3
		case components.SequenceGold:
			typeCode = 4
		default:
			typeCode = 0 // Error state
		}
		// Map levels to uint32: 0=dark, 1=normal, 2=bright
		var levelCode uint32
		switch seq.Level {
		case components.LevelDark:
			levelCode = 0
		case components.LevelNormal:
			levelCode = 1
		case components.LevelBright:
			levelCode = 2
		}
		s.ctx.State.SetScoreBlinkType(typeCode)
		s.ctx.State.SetScoreBlinkLevel(levelCode)
		s.ctx.State.SetScoreBlinkTime(now)

		// Decrement color counter (only for Blue and Green, not Red or Gold)
		if s.spawnSystem != nil && (seq.Type == components.SequenceBlue || seq.Type == components.SequenceGreen) {
			s.spawnSystem.AddColorCount(seq.Type, seq.Level, -1)
		}

		// Safely destroy the character entity (handles spatial index removal)
		world.SafeDestroyEntity(entity)

		// Move cursor right
		s.ctx.CursorX++
		if s.ctx.CursorX >= s.ctx.GameWidth {
			s.ctx.CursorX = s.ctx.GameWidth - 1
		}
		// Sync cursor position to GameState
		s.ctx.State.SetCursorX(s.ctx.CursorX)

	} else {
		// Incorrect character - flash error cursor, reset heat, and deactivate boost
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		s.ctx.State.SetHeat(0)
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0) // 0 = None
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
	}
}

// extendBoost extends the boost timer by the given duration
func (s *ScoreSystem) extendBoost(duration time.Duration) {
	now := s.ctx.TimeProvider.Now()

	// If boost is already active, add to existing end time; otherwise start fresh
	currentEndTime := s.ctx.State.GetBoostEndTime()
	wasActive := s.ctx.State.GetBoostEnabled() && currentEndTime.After(now)

	if wasActive {
		s.ctx.State.SetBoostEndTime(currentEndTime.Add(duration))
	} else {
		s.ctx.State.SetBoostEndTime(now.Add(duration))
	}

	s.ctx.State.SetBoostEnabled(true)
}

// handleNuggetCollection processes nugget collection (requires typing matching character)
func (s *ScoreSystem) handleNuggetCollection(world *engine.World, entity engine.Entity, char components.CharacterComponent, typedRune rune, cursorX, cursorY int) {
	now := s.ctx.TimeProvider.Now()

	// Check if typed character matches the nugget character
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor and reset heat
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		s.ctx.State.SetHeat(0) // Reset heat on incorrect nugget typing
		s.ctx.State.SetBoostEnabled(false)
		s.ctx.State.SetBoostColor(0)
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
		return
	}

	// Correct character - collect nugget
	// Calculate heat increase: 10% of max heat (screen width)
	// Use ceiling to ensure at least 10% even for widths not divisible by 10
	maxHeat := s.ctx.Width
	if maxHeat < 1 {
		maxHeat = 1
	}
	heatIncrease := int(math.Ceil(float64(maxHeat) / 10.0))
	if heatIncrease < 1 {
		heatIncrease = 1 // Minimum increase of 1
	}

	// Add heat (10% of max)
	s.ctx.State.AddHeat(heatIncrease)

	// Destroy the nugget entity
	world.SafeDestroyEntity(entity)

	// Clear the active nugget reference to trigger respawn
	// Use CAS to ensure we only clear if this is still the active nugget
	s.nuggetSystem.ClearActiveNuggetIfMatches(uint64(entity))

	// Move cursor right
	s.ctx.CursorX++
	if s.ctx.CursorX >= s.ctx.GameWidth {
		s.ctx.CursorX = s.ctx.GameWidth - 1
	}
	// Sync cursor position to GameState
	s.ctx.State.SetCursorX(s.ctx.CursorX)

	// No score effects on successful collection
}

// handleGoldSequenceTyping processes typing of gold sequence characters
func (s *ScoreSystem) handleGoldSequenceTyping(world *engine.World, entity engine.Entity, char components.CharacterComponent, seq components.SequenceComponent, typedRune rune, cursorX, cursorY int) {
	now := s.ctx.TimeProvider.Now()

	// Check if typed character matches
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor but DON'T reset heat for gold sequence
		s.ctx.State.SetCursorError(true)
		s.ctx.State.SetCursorErrorTime(now)
		// Set score blink to error state (black background with bright red text)
		s.ctx.State.SetScoreBlinkActive(true)
		s.ctx.State.SetScoreBlinkType(0)  // 0 = error
		s.ctx.State.SetScoreBlinkLevel(0) // 0 = dark
		s.ctx.State.SetScoreBlinkTime(now)
		return
	}

	// Correct character - remove entity and move cursor
	// Trigger score blink with Gold type and level
	s.ctx.State.SetScoreBlinkActive(true)
	// Map sequence types to uint32: 0=error, 1=blue, 2=green, 3=red, 4=gold
	var typeCode uint32
	switch seq.Type {
	case components.SequenceBlue:
		typeCode = 1
	case components.SequenceGreen:
		typeCode = 2
	case components.SequenceRed:
		typeCode = 3
	case components.SequenceGold:
		typeCode = 4
	default:
		typeCode = 0 // Error state
	}
	// Map levels to uint32: 0=dark, 1=normal, 2=bright
	var levelCode uint32
	switch seq.Level {
	case components.LevelDark:
		levelCode = 0
	case components.LevelNormal:
		levelCode = 1
	case components.LevelBright:
		levelCode = 2
	}
	s.ctx.State.SetScoreBlinkType(typeCode)
	s.ctx.State.SetScoreBlinkLevel(levelCode)
	s.ctx.State.SetScoreBlinkTime(now)

	// Safely destroy the character entity (handles spatial index removal)
	world.SafeDestroyEntity(entity)

	// Move cursor right
	s.ctx.CursorX++
	if s.ctx.CursorX >= s.ctx.GameWidth {
		s.ctx.CursorX = s.ctx.GameWidth - 1
	}
	// Sync cursor position to GameState
	s.ctx.State.SetCursorX(s.ctx.CursorX)

	// Check if this was the last character of the gold sequence
	isLastChar := (seq.Index == constants.GoldSequenceLength-1)

	if isLastChar {
		// Gold sequence completed! Check if we should trigger cleaners
		heatBarWidth := s.ctx.Width
		if heatBarWidth < 1 {
			heatBarWidth = 1
		}

		currentHeat := s.ctx.State.GetHeat()

		// Request cleaners if heat is already at max
		// Clock scheduler will trigger cleaners on next tick (within 50ms)
		// If transition fails (e.g., wrong phase), cleaners won't be triggered
		if currentHeat >= heatBarWidth {
			s.ctx.State.RequestCleaners()
			// Note: If transition fails, CompleteGoldSequence below will still proceed
			// and transition to PhaseGoldComplete instead
		}

		// Fill heat to max (if not already higher)
		if currentHeat < heatBarWidth {
			s.ctx.State.SetHeat(heatBarWidth)
		}

		// Mark gold sequence as complete
		s.goldSequenceSystem.CompleteGoldSequence(world)
	}
}
