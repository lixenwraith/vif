package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// SplashSystem manages the lifecycle of splash entities
type SplashSystem struct {
	ctx *engine.GameContext
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(ctx *engine.GameContext) *SplashSystem {
	return &SplashSystem{ctx: ctx}
}

// Priority returns the system's priority (low, after game logic)
func (s *SplashSystem) Priority() int {
	return constants.PrioritySplash
}

// EventTypes defines the events this system subscribes to
func (s *SplashSystem) EventTypes() []engine.EventType {
	return []engine.EventType{
		engine.EventSplashRequest,
		engine.EventGoldSpawned,
		engine.EventGoldComplete,
		engine.EventGoldTimeout,
	}
}

// HandleEvent processes events to create or destroy splash entities
func (s *SplashSystem) HandleEvent(world *engine.World, event engine.GameEvent) {
	switch event.Type {
	case engine.EventSplashRequest:
		if payload, ok := event.Payload.(*engine.SplashRequestPayload); ok {
			s.handleSplashRequest(world, payload, event.Timestamp)
		}

	case engine.EventGoldSpawned:
		if payload, ok := event.Payload.(*engine.GoldSpawnedPayload); ok {
			s.handleGoldSpawn(world, payload, event.Timestamp)
		}

	case engine.EventGoldComplete, engine.EventGoldTimeout:
		if payload, ok := event.Payload.(*engine.GoldCompletionPayload); ok {
			s.handleGoldFinish(world, payload.SequenceID)
		}
	}
}

// Update manages lifecycle of splashes (expiry, timer updates)
func (s *SplashSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	nowNano := timeRes.GameTime.UnixNano()

	var toDestroy []engine.Entity

	entities := world.Splashes.All()
	for _, entity := range entities {
		splash, ok := world.Splashes.Get(entity)
		if !ok {
			continue
		}

		switch splash.Mode {
		case components.SplashModeTransient:
			// Check expiry
			if nowNano-splash.StartNano >= splash.Duration {
				toDestroy = append(toDestroy, entity)
			}

		case components.SplashModePersistent:
			// Update Timer logic
			// Calculate remaining time
			elapsedSeconds := float64(nowNano-splash.StartNano) / float64(time.Second)
			totalSeconds := float64(splash.Duration) / float64(time.Second)
			remaining := totalSeconds - elapsedSeconds

			// Clamp and Convert
			// Logic: 9.9s -> '9', 0.9s -> '0'
			digit := int(remaining)
			if digit > 9 {
				digit = 9
			}
			if digit < 0 {
				digit = 0
			}

			// Update content if changed
			newChar := rune('0' + digit)
			if splash.Content[0] != newChar {
				splash.Content[0] = newChar
				// Reset animation start for pulse effect on change?
				// Optional: subtle pulse effect could be handled here or in renderer
				world.Splashes.Add(entity, splash)
			}
		}
	}

	for _, e := range toDestroy {
		world.DestroyEntity(e)
	}
}

// handleSplashRequest creates a transient splash with smart layout
func (s *SplashSystem) handleSplashRequest(world *engine.World, payload *engine.SplashRequestPayload, now time.Time) {
	// 1. Enforce Uniqueness: Destroy existing transient splashes
	s.cleanupSplashesByMode(world, components.SplashModeTransient)

	// 2. Prepare Content
	runes := []rune(payload.Text)
	length := len(runes)
	if length > constants.SplashMaxLength {
		length = constants.SplashMaxLength
	}

	// 3. Smart Layout
	anchorX, anchorY := s.calculateSmartLayout(world, payload.OriginX, payload.OriginY, length)

	// 4. Create Component
	splash := components.SplashComponent{
		Length:    length,
		Color:     payload.Color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		Mode:      components.SplashModeTransient,
		StartNano: now.UnixNano(),
		Duration:  constants.SplashDuration.Nanoseconds(),
	}
	copy(splash.Content[:], runes[:length])

	// 5. Spawn
	entity := world.CreateEntity()
	world.Splashes.Add(entity, splash)
}

// handleGoldSpawn creates the persistent gold timer anchored to the sequence
func (s *SplashSystem) handleGoldSpawn(world *engine.World, payload *engine.GoldSpawnedPayload, now time.Time) {
	// 1. Enforce Uniqueness: Destroy existing timer
	s.cleanupSplashesByMode(world, components.SplashModePersistent)

	// 2. Calculate Anchored Position
	// Center horizontally over the sequence
	// Position above, fallback to below if too close to top

	// Anchor X: Center of sequence
	// Sequence Center (Cells) = OriginX + Length/2
	// Timer Center (Cells) = AnchorX + TimerWidth/2
	// AnchorX = OriginX + Length/2 - TimerWidth/2
	timerCellWidth := 1 * (constants.SplashCharWidth + constants.SplashCharSpacing)
	seqCenter := payload.OriginX + (payload.Length / 2)
	anchorX := seqCenter - (timerCellWidth / 2)

	// Anchor Y: Above sequence
	// Timer Height = constants.SplashCharHeight (12 rows)
	// Sequence Y is in Rows.
	// Place 2 rows above sequence top (Sequence is 1 row high)
	padding := 2
	anchorY := payload.OriginY - constants.SplashCharHeight - padding

	// Fallback: If offscreen top, place below
	if anchorY < 0 {
		anchorY = payload.OriginY + 1 + padding
	}

	// 3. Create Component
	splash := components.SplashComponent{
		Length:     1,
		Color:      components.SplashColorGold,
		AnchorX:    anchorX,
		AnchorY:    anchorY,
		Mode:       components.SplashModePersistent,
		StartNano:  now.UnixNano(),
		Duration:   payload.Duration.Nanoseconds(),
		SequenceID: payload.SequenceID,
	}
	// TODO: double counts 9
	splash.Content[0] = '9' // Start at 9

	// 4. Spawn
	entity := world.CreateEntity()
	world.Splashes.Add(entity, splash)
}

// handleGoldFinish destroys the gold timer
func (s *SplashSystem) handleGoldFinish(world *engine.World, sequenceID int) {
	// Find and destroy specific timer
	entities := world.Splashes.All()
	for _, entity := range entities {
		splash, ok := world.Splashes.Get(entity)
		if !ok {
			continue
		}
		if splash.Mode == components.SplashModePersistent && splash.SequenceID == sequenceID {
			world.DestroyEntity(entity)
			return // Found it
		}
	}
}

// cleanupSplashesByMode removes all splashes of a specific mode
func (s *SplashSystem) cleanupSplashesByMode(world *engine.World, mode components.SplashMode) {
	entities := world.Splashes.All()
	for _, entity := range entities {
		splash, ok := world.Splashes.Get(entity)
		if !ok {
			continue
		}
		if splash.Mode == mode {
			world.DestroyEntity(entity)
		}
	}
}

// calculateSmartLayout determines the best position for a transient splash
// Avoids Cursor and Gold Sequences
func (s *SplashSystem) calculateSmartLayout(world *engine.World, cursorX, cursorY, charCount int) (int, int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	width := config.GameWidth
	height := config.GameHeight

	// Splash Dimensions
	splashW := charCount * (constants.SplashCharWidth + constants.SplashCharSpacing)
	splashH := constants.SplashCharHeight

	// Define Quadrant Centers
	// Q0: Top-Left, Q1: Top-Right, Q2: Bottom-Left, Q3: Bottom-Right
	centers := []struct{ x, y int }{
		{width / 4, height / 4},         // Q0
		{width * 3 / 4, height / 4},     // Q1
		{width / 4, height * 3 / 4},     // Q2
		{width * 3 / 4, height * 3 / 4}, // Q3
	}

	// Score Quadrants (Higher is better)
	scores := []int{100, 100, 100, 100}

	// 1. Cursor Penalty (-1000)
	// Determine cursor quadrant
	cursorQ := 0
	if cursorX >= width/2 {
		cursorQ |= 1
	}
	if cursorY >= height/2 {
		cursorQ |= 2
	}
	scores[cursorQ] -= 1000

	// 2. Gold Sequence Penalty (-500)
	// Iterate active gold sequences
	goldEntities := world.GoldSequences.All()
	for _, e := range goldEntities {
		gs, ok := world.GoldSequences.Get(e)
		if !ok || !gs.Active {
			continue
		}

		// Get position of start of sequence (we need to query positions of member entities?)
		// Since GoldSequenceComponent doesn't store position, we rely on the fact that
		// GoldSystem creates entities.
		// Optimization: Check the "Restricted Zone" via stored components?
		// We don't have a direct "Box" component.
		// However, we can scan `world.Positions` for entities with `SequenceID`.
		// This is O(N) where N is total entities. Fast enough for 200 entities.
		// Better: We iterate `world.Sequences` which is smaller.

		seqEntities := world.Sequences.All()
		for _, se := range seqEntities {
			seq, ok := world.Sequences.Get(se)
			if !ok || seq.ID != gs.SequenceID {
				continue
			}
			pos, ok := world.Positions.Get(se)
			if !ok {
				continue
			}

			// Determine quadrant of this gold character
			goldQ := 0
			if pos.X >= width/2 {
				goldQ |= 1
			}
			if pos.Y >= height/2 {
				goldQ |= 2
			}
			// Apply soft penalty (cumulative, but clamped or just flag)
			// Deduct 50 per char, effectively vetoing the quadrant
			scores[goldQ] -= 50
		}
	}

	// 3. Select Best Quadrant
	bestQ := -1
	maxScore := -9999

	// Prefer opposite to cursor if scores tied
	oppositeQ := cursorQ ^ 3 // 0<->3, 1<->2

	// Check opposite first to give it precedence on ties
	if scores[oppositeQ] > maxScore {
		maxScore = scores[oppositeQ]
		bestQ = oppositeQ
	}

	for i := 0; i < 4; i++ {
		if i == oppositeQ {
			continue
		}
		if scores[i] > maxScore {
			maxScore = scores[i]
			bestQ = i
		}
	}

	// Calculate Anchor
	cx, cy := centers[bestQ].x, centers[bestQ].y
	anchorX := cx - splashW/2
	anchorY := cy - splashH/2

	// Clamp to Game Area
	if anchorX < 0 {
		anchorX = 0
	}
	if anchorX+splashW > width {
		anchorX = width - splashW
	}
	if anchorY < 0 {
		anchorY = 0
	}
	if anchorY+splashH > height {
		anchorY = height - splashH
	}

	return anchorX, anchorY
}