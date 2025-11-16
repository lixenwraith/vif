package systems

import (
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DecaySystem handles character decay animation and logic
type DecaySystem struct {
	animating     bool
	currentRow    int
	startTime     time.Time
	lastUpdate    time.Time
	nextDecayTime time.Time // When the next decay will trigger
	gameWidth     int
	gameHeight    int
	screenWidth   int
	heatIncrement int
}

// NewDecaySystem creates a new decay system
func NewDecaySystem(gameWidth, gameHeight, screenWidth, heatIncrement int) *DecaySystem {
	s := &DecaySystem{
		animating:     false,
		currentRow:    0,
		lastUpdate:    time.Now(),
		gameWidth:     gameWidth,
		gameHeight:    gameHeight,
		screenWidth:   screenWidth,
		heatIncrement: heatIncrement,
	}
	s.startTicker()
	return s
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
		now := time.Now()
		if now.After(s.nextDecayTime) || now.Equal(s.nextDecayTime) {
			s.animating = true
			s.currentRow = 0
			s.startTime = now
		}
		// Timer is only recalculated after decay animation completes
	}
}

// updateAnimation progresses the decay animation
func (s *DecaySystem) updateAnimation(world *engine.World) {
	elapsed := time.Since(s.startTime).Seconds()
	targetRow := int(elapsed / 0.1)

	// Apply decay to rows that we've passed
	for s.currentRow <= targetRow && s.currentRow < s.gameHeight {
		s.applyDecayToRow(world, s.currentRow)
		s.currentRow++
	}

	// Check if animation complete (currentRow > gameHeight-1 means we've processed all rows)
	if s.currentRow > s.gameHeight-1 {
		s.animating = false
		s.currentRow = 0
		// Schedule next decay
		interval := s.calculateInterval()
		s.nextDecayTime = time.Now().Add(interval)
	}
}

// applyDecayToRow applies decay logic to all characters at the given row
func (s *DecaySystem) applyDecayToRow(world *engine.World, row int) {
	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := world.GetEntitiesWith(seqType, posType)

	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y != row {
			continue // Not on this row
		}

		seqComp, _ := world.GetComponent(entity, seqType)
		seq := seqComp.(components.SequenceComponent)

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
			} else {
				// Red at LevelDark - remove entity
				world.DestroyEntity(entity)
			}
		}
	}
}

// startTicker initializes the decay timer (called once at startup)
func (s *DecaySystem) startTicker() {
	interval := s.calculateInterval()
	s.nextDecayTime = time.Now().Add(interval)
	s.lastUpdate = time.Now()
}

// calculateInterval calculates the decay interval based on heat
// Formula: 60 - 50 * (heat_filled / heat_max)
// Empty heat bar (0): 60 - 50 * 0 = 60 seconds
// Full heat bar (max): 60 - 50 * 1 = 10 seconds
func (s *DecaySystem) calculateInterval() time.Duration {
	heatBarWidth := s.screenWidth - 6
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

	// Simple formula: 60 - 50 * (heat_filled / heat_max)
	intervalSeconds := 60.0 - 50.0*heatPercentage
	return time.Duration(intervalSeconds * float64(time.Second))
}

// IsAnimating returns true if decay animation is active
func (s *DecaySystem) IsAnimating() bool {
	return s.animating
}

// CurrentRow returns the current decay row being displayed
func (s *DecaySystem) CurrentRow() int {
	// currentRow tracks the next row to process
	// For display, we want the last row that was processed
	if s.currentRow > 0 {
		return s.currentRow - 1
	}
	return 0
}

// GetTimeUntilDecay returns seconds until next decay trigger
func (s *DecaySystem) GetTimeUntilDecay() float64 {
	if s.animating {
		return 0.0
	}
	remaining := s.nextDecayTime.Sub(time.Now()).Seconds()
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
