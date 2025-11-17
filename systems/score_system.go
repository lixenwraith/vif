package systems

import (
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// ScoreSystem handles character typing and score calculation
type ScoreSystem struct {
	ctx               *engine.GameContext
	lastCorrect       time.Time
	errorCursorSet    bool
	goldSequenceSystem *GoldSequenceSystem
	spawnSystem       *SpawnSystem
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

// Update runs the score system (unused for now, character typing is event-driven)
func (s *ScoreSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()

	// Clear error cursor after timeout
	if s.ctx.GetCursorError() && now.Sub(s.ctx.GetCursorErrorTime()) > constants.ErrorCursorTimeout {
		s.ctx.SetCursorError(false)
	}

	// Clear score blink after timeout
	if s.ctx.GetScoreBlinkActive() && now.Sub(s.ctx.GetScoreBlinkTime()) > constants.ScoreBlinkTimeout {
		s.ctx.SetScoreBlinkActive(false)
	}
}

// HandleCharacterTyping processes a character typed in insert mode
func (s *ScoreSystem) HandleCharacterTyping(world *engine.World, cursorX, cursorY int, typedRune rune) {
	now := s.ctx.TimeProvider.Now()

	// Get entity at cursor position
	entity := world.GetEntityAtPosition(cursorX, cursorY)
	if entity == 0 {
		// No character at cursor - flash error cursor
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(now)
		s.ctx.SetScoreIncrement(0) // Reset heat
		return
	}

	// Get character component
	charType := reflect.TypeOf(components.CharacterComponent{})
	charComp, ok := world.GetComponent(entity, charType)
	if !ok {
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(now)
		s.ctx.SetScoreIncrement(0)
		return
	}
	char := charComp.(components.CharacterComponent)

	// Get sequence component
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(now)
		s.ctx.SetScoreIncrement(0)
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
			s.ctx.SetScoreIncrement(0)
		} else {
			// Apply heat gain with boost multiplier
			heatGain := 1
			if s.ctx.GetBoostEnabled() {
				heatGain = 2
			}
			s.ctx.AddScoreIncrement(heatGain)
		}
		s.lastCorrect = now

		// Calculate points: increment * level_multiplier * (red?-1:1)
		levelMultipliers := map[components.SequenceLevel]int{
			components.LevelDark:   1,
			components.LevelNormal: 2,
			components.LevelBright: 3,
		}
		levelMult := levelMultipliers[seq.Level]
		points := s.ctx.GetScoreIncrement() * levelMult

		// Red characters give negative points
		if seq.Type == components.SequenceRed {
			points = -points
		}

		s.ctx.AddScore(points)

		// Blue character adds boost time
		if seq.Type == components.SequenceBlue {
			s.extendBoost(constants.BoostExtensionDuration)
		}

		// Trigger score blink with character color
		s.ctx.SetScoreBlinkActive(true)
		fgColor, _, _ := render.GetStyleForSequence(seq.Type, seq.Level).Decompose()
		s.ctx.SetScoreBlinkColor(fgColor)
		s.ctx.SetScoreBlinkTime(now)

		// Decrement color counter (only for Blue and Green, not Red or Gold)
		if s.spawnSystem != nil && (seq.Type == components.SequenceBlue || seq.Type == components.SequenceGreen) {
			s.spawnSystem.AddColorCount(seq.Type, seq.Level, -1)
		}

		// Remove entity from spatial index first
		posType := reflect.TypeOf(components.PositionComponent{})
		posComp, ok := world.GetComponent(entity, posType)
		if ok {
			pos := posComp.(components.PositionComponent)
			world.RemoveFromSpatialIndex(pos.X, pos.Y)
		}

		// Destroy the character entity
		world.DestroyEntity(entity)

		// Move cursor right
		s.ctx.CursorX++
		if s.ctx.CursorX >= s.ctx.GameWidth {
			s.ctx.CursorX = s.ctx.GameWidth - 1
		}

	} else {
		// Incorrect character - flash error cursor and reset heat
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(now)
		s.ctx.SetScoreIncrement(0)
	}
}

// extendBoost extends the boost timer by the given duration
func (s *ScoreSystem) extendBoost(duration time.Duration) {
	now := s.ctx.TimeProvider.Now()

	// If boost is already active, add to existing end time; otherwise start fresh
	currentEndTime := s.ctx.GetBoostEndTime()
	wasActive := s.ctx.GetBoostEnabled() && currentEndTime.After(now)

	if wasActive {
		s.ctx.SetBoostEndTime(currentEndTime.Add(duration))
	} else {
		s.ctx.SetBoostEndTime(now.Add(duration))
	}

	s.ctx.SetBoostEnabled(true)
}

// handleGoldSequenceTyping processes typing of gold sequence characters
func (s *ScoreSystem) handleGoldSequenceTyping(world *engine.World, entity engine.Entity, char components.CharacterComponent, seq components.SequenceComponent, typedRune rune, cursorX, cursorY int) {
	now := s.ctx.TimeProvider.Now()

	// Check if typed character matches
	if char.Rune != typedRune {
		// Incorrect character - flash error cursor but DON'T reset heat for gold sequence
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(now)
		return
	}

	// Correct character - remove entity and move cursor
	// Remove from spatial index first
	posType := reflect.TypeOf(components.PositionComponent{})
	posComp, ok := world.GetComponent(entity, posType)
	if ok {
		pos := posComp.(components.PositionComponent)
		world.RemoveFromSpatialIndex(pos.X, pos.Y)
	}

	// Destroy the character entity
	world.DestroyEntity(entity)

	// Move cursor right
	s.ctx.CursorX++
	if s.ctx.CursorX >= s.ctx.GameWidth {
		s.ctx.CursorX = s.ctx.GameWidth - 1
	}

	// Check if this was the last character of the gold sequence
	isLastChar := (seq.Index == constants.GoldSequenceLength-1)

	if isLastChar {
		// Gold sequence completed! Fill heat to max (if not already higher)
		heatBarWidth := s.ctx.Width - constants.HeatBarIndicatorWidth
		if heatBarWidth < 1 {
			heatBarWidth = 1
		}

		currentHeat := s.ctx.GetScoreIncrement()
		if currentHeat < heatBarWidth {
			s.ctx.SetScoreIncrement(heatBarWidth)
		}

		// Mark gold sequence as complete
		s.goldSequenceSystem.CompleteGoldSequence(world)
	}
}
