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
	if s.ctx.GetCursorError() && time.Since(s.ctx.GetCursorErrorTime()) > constants.ErrorCursorTimeout {
		s.ctx.SetCursorError(false)
	}

	// Clear score blink after timeout
	if s.ctx.GetScoreBlinkActive() && time.Since(s.ctx.GetScoreBlinkTime()) > constants.ScoreBlinkTimeout {
		s.ctx.SetScoreBlinkActive(false)
	}
}

// HandleCharacterTyping processes a character typed in insert mode
func (s *ScoreSystem) HandleCharacterTyping(world *engine.World, cursorX, cursorY int, typedRune rune) {
	// Get entity at cursor position
	entity := world.GetEntityAtPosition(cursorX, cursorY)
	if entity == 0 {
		// No character at cursor - flash error cursor
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(time.Now())
		s.ctx.SetScoreIncrement(0) // Reset heat
		return
	}

	// Get character component
	charType := reflect.TypeOf(components.CharacterComponent{})
	charComp, ok := world.GetComponent(entity, charType)
	if !ok {
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(time.Now())
		s.ctx.SetScoreIncrement(0)
		return
	}
	char := charComp.(components.CharacterComponent)

	// Get sequence component
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		s.ctx.SetCursorError(true)
		s.ctx.SetCursorErrorTime(time.Now())
		s.ctx.SetScoreIncrement(0)
		return
	}
	seq := seqComp.(components.SequenceComponent)

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
		s.lastCorrect = time.Now()

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
		s.ctx.SetScoreBlinkTime(time.Now())

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
		s.ctx.SetCursorErrorTime(time.Now())
		s.ctx.SetScoreIncrement(0)
	}
}

// extendBoost extends the boost timer by the given duration
func (s *ScoreSystem) extendBoost(duration time.Duration) {
	now := time.Now()

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
