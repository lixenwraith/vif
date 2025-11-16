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
	ticker        *time.Timer
	animating     bool
	currentRow    int
	startTime     time.Time
	lastUpdate    time.Time
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
		// Restart ticker if heat changed significantly
		if time.Since(s.lastUpdate).Seconds() > 1.0 {
			s.startTicker()
		}
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

	// Check if animation complete
	if s.currentRow >= s.gameHeight {
		s.animating = false
		s.currentRow = 0
		s.startTicker()
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

// startTicker starts or restarts the decay ticker
func (s *DecaySystem) startTicker() {
	if s.ticker != nil {
		s.ticker.Stop()
	}

	interval := s.calculateInterval()
	s.ticker = time.AfterFunc(interval, func() {
		s.animating = true
		s.currentRow = 0
		s.startTime = time.Now()
	})
	s.lastUpdate = time.Now()
}

// calculateInterval calculates the decay interval based on screen size and heat
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

	screenArea := float64(s.gameWidth * s.gameHeight)
	baseInterval := 60.0 + (screenArea / 50.0)

	minInterval := baseInterval * 0.1
	if minInterval < 5.0 {
		minInterval = 5.0
	}

	intervalSeconds := baseInterval*(1.0-heatPercentage*0.9) + minInterval
	return time.Duration(intervalSeconds * float64(time.Second))
}

// IsAnimating returns true if decay animation is active
func (s *DecaySystem) IsAnimating() bool {
	return s.animating
}

// CurrentRow returns the current decay row
func (s *DecaySystem) CurrentRow() int {
	return s.currentRow
}

// UpdateDimensions updates the game dimensions
func (s *DecaySystem) UpdateDimensions(gameWidth, gameHeight, screenWidth, heatIncrement int) {
	s.gameWidth = gameWidth
	s.gameHeight = gameHeight
	s.screenWidth = screenWidth
	s.heatIncrement = heatIncrement
}
