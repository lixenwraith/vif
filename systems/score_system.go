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
	ctx            *engine.GameContext
	lastCorrect    time.Time
	errorCursorSet bool
}

// NewScoreSystem creates a new score system
func NewScoreSystem(ctx *engine.GameContext) *ScoreSystem {
	return &ScoreSystem{
		ctx:         ctx,
		lastCorrect: time.Now(),
	}
}

// Priority returns the system's priority
func (s *ScoreSystem) Priority() int {
	return 10 // High priority, run before other systems
}

// Update runs the score system (unused for now, character typing is event-driven)
func (s *ScoreSystem) Update(world *engine.World, dt time.Duration) {
	// Clear error cursor after timeout
	if s.ctx.CursorError && time.Since(s.ctx.CursorErrorTime) > constants.ErrorCursorTimeout {
		s.ctx.CursorError = false
	}

	// Clear score blink after timeout
	if s.ctx.ScoreBlinkActive && time.Since(s.ctx.ScoreBlinkTime) > constants.ScoreBlinkTimeout {
		s.ctx.ScoreBlinkActive = false
	}
}

// HandleCharacterTyping processes a character typed in insert mode
func (s *ScoreSystem) HandleCharacterTyping(world *engine.World, cursorX, cursorY int, typedRune rune) {
	// Get entity at cursor position
	entity := world.GetEntityAtPosition(cursorX, cursorY)
	if entity == 0 {
		// No character at cursor - flash error cursor
		s.ctx.CursorError = true
		s.ctx.CursorErrorTime = time.Now()
		s.ctx.ScoreIncrement = 0 // Reset heat
		return
	}

	// Get character component
	charType := reflect.TypeOf(components.CharacterComponent{})
	charComp, ok := world.GetComponent(entity, charType)
	if !ok {
		s.ctx.CursorError = true
		s.ctx.CursorErrorTime = time.Now()
		s.ctx.ScoreIncrement = 0
		return
	}
	char := charComp.(components.CharacterComponent)

	// Get sequence component
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		s.ctx.CursorError = true
		s.ctx.CursorErrorTime = time.Now()
		s.ctx.ScoreIncrement = 0
		return
	}
	seq := seqComp.(components.SequenceComponent)

	// Check if typed character matches
	if char.Rune == typedRune {
		// Correct character
		// RED characters reset heat instead of incrementing it
		if seq.Type == components.SequenceRed {
			s.ctx.ScoreIncrement = 0
		} else {
			// Apply heat gain with boost multiplier
			heatGain := 1
			if s.ctx.BoostEnabled {
				heatGain = 2
			}
			s.ctx.ScoreIncrement += heatGain
		}
		s.lastCorrect = time.Now()

		// Calculate points: increment * level_multiplier * (red?-1:1)
		levelMultipliers := map[components.SequenceLevel]int{
			components.LevelDark:   1,
			components.LevelNormal: 2,
			components.LevelBright: 3,
		}
		levelMult := levelMultipliers[seq.Level]
		points := s.ctx.ScoreIncrement * levelMult

		// Red characters give negative points
		if seq.Type == components.SequenceRed {
			points = -points
		}

		s.ctx.Score += points

		// Blue character adds boost time
		if seq.Type == components.SequenceBlue {
			s.extendBoost(constants.BoostExtensionDuration)
		}

		// Trigger score blink with character color
		s.ctx.ScoreBlinkActive = true
		fgColor, _, _ := render.GetStyleForSequence(seq.Type, seq.Level).Decompose()
		s.ctx.ScoreBlinkColor = fgColor
		s.ctx.ScoreBlinkTime = time.Now()

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
		s.ctx.CursorError = true
		s.ctx.CursorErrorTime = time.Now()
		s.ctx.ScoreIncrement = 0
	}
}

// extendBoost extends the boost timer by the given duration
func (s *ScoreSystem) extendBoost(duration time.Duration) {
	if s.ctx.BoostTimer != nil {
		s.ctx.BoostTimer.Stop()
	}

	// If boost is already active, add to existing end time; otherwise start fresh
	now := time.Now()
	wasActive := s.ctx.BoostEnabled && s.ctx.BoostEndTime.After(now)
	if wasActive {
		s.ctx.BoostEndTime = s.ctx.BoostEndTime.Add(duration)
	} else {
		s.ctx.BoostEndTime = now.Add(duration)
	}
	s.ctx.BoostEnabled = true

	// Calculate remaining time for the timer
	remaining := s.ctx.BoostEndTime.Sub(now)
	s.ctx.BoostTimer = time.AfterFunc(remaining, func() {
		s.ctx.BoostEnabled = false
	})
}
