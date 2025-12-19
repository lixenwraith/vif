package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// SplashSystem manages the lifecycle of splash entities
type SplashSystem struct {
	world *engine.World
	res   engine.CoreResources

	splashStore *engine.Store[component.SplashComponent]
	goldStore   *engine.Store[component.GoldSequenceComponent]
	seqStore    *engine.Store[component.SequenceComponent]
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(world *engine.World) engine.System {
	return &SplashSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		splashStore: engine.GetStore[component.SplashComponent](world),
		goldStore:   engine.GetStore[component.GoldSequenceComponent](world),
		seqStore:    engine.GetStore[component.SequenceComponent](world),
	}
}

// Init
func (s *SplashSystem) Init() {}

// Priority returns the system's priority (low, after game logic)
func (s *SplashSystem) Priority() int {
	return constant.PrioritySplash
}

// EventTypes defines the events this system subscribes to
func (s *SplashSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSplashRequest,
		event.EventGoldSpawned,
		event.EventGoldComplete,
		event.EventGoldTimeout,
		event.EventGoldDestroyed,
	}
}

// HandleEvent processes events to create or destroy splash entities
func (s *SplashSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventSplashRequest:
		if payload, ok := ev.Payload.(*event.SplashRequestPayload); ok {
			s.handleSplashRequest(payload)
		}

	case event.EventGoldSpawned:
		if payload, ok := ev.Payload.(*event.GoldSpawnedPayload); ok {
			s.handleGoldSpawn(payload)
		}

	case event.EventGoldComplete, event.EventGoldTimeout, event.EventGoldDestroyed:
		if payload, ok := ev.Payload.(*event.GoldCompletionPayload); ok {
			s.handleGoldFinish(payload.SequenceID)
		}
	}
}

// Update manages lifecycle of splashes (expiry, timer updates)
func (s *SplashSystem) Update() {
	dt := s.res.Time.DeltaTime
	var toDestroy []core.Entity

	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}

		// Delta-based time tracking (Robust against clock jumps/resets)
		splash.Remaining -= dt

		// Persistent Splash Logic (Gold Timer)
		if splash.Mode == component.SplashModePersistent {
			// Calculate display digit based on remaining time
			// TODO: migrate to timer system
			remainingSec := splash.Remaining.Seconds()
			if remainingSec < 0 {
				remainingSec = 0
			}

			// TODO: this should support >9 seconds
			digit := int(remainingSec)
			if digit > 9 {
				digit = 9
			}

			// Update content if changed
			newChar := rune('0' + digit)
			if splash.Content[0] != newChar {
				splash.Content[0] = newChar
			}
		}

		// Write back component (state changed)
		s.splashStore.Add(entity, splash)
	}

	for _, e := range toDestroy {
		s.world.DestroyEntity(e)
	}
}

// handleSplashRequest creates a transient splash with smart layout
func (s *SplashSystem) handleSplashRequest(payload *event.SplashRequestPayload) {
	// 1. Enforce Uniqueness: Destroy existing transient splashes
	s.cleanupSplashesByMode(component.SplashModeTransient)

	// 2. Prepare Content
	runes := []rune(payload.Text)
	length := len(runes)
	if length > constant.SplashMaxLength {
		length = constant.SplashMaxLength
	}

	// 3. Smart Layout
	anchorX, anchorY := s.calculateSmartLayout(payload.OriginX, payload.OriginY, length)

	// 4. Create Component with Delta Timer
	splash := component.SplashComponent{
		Length:    length,
		Color:     payload.Color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		Mode:      component.SplashModeTransient,
		Remaining: constant.SplashDuration,
		Duration:  constant.SplashDuration,
	}
	copy(splash.Content[:], runes[:length])

	// 5. Spawn
	entity := s.world.CreateEntity()
	s.splashStore.Add(entity, splash)

	// 6. Register with TimeKeeper for destruction
	s.world.PushEvent(event.EventTimerStart, &event.TimerStartPayload{
		Entity:   entity,
		Duration: constant.SplashDuration,
	})
}

// handleGoldSpawn creates the persistent gold timer anchored to the sequence
func (s *SplashSystem) handleGoldSpawn(payload *event.GoldSpawnedPayload) {
	// 1. Enforce Uniqueness: Destroy existing timer
	s.cleanupSplashesByMode(component.SplashModePersistent)

	// 2. Calculate Anchored Position
	// Center horizontally over the sequence
	// Position above, fallback to below if too close to top

	// Anchor X: Center of sequence
	// Sequence Center (Cells) = OriginX + Length/2
	// Timer Center (Cells) = AnchorX + TimerWidth/2
	// AnchorX = OriginX + Length/2 - TimerWidth/2
	timerCellWidth := 1 * (constant.SplashCharWidth + constant.SplashCharSpacing)
	seqCenter := payload.OriginX + (payload.Length / 2)
	anchorX := seqCenter - (timerCellWidth / 2)

	// Anchor Y: Above sequence
	// Timer Height = constants.SplashCharHeight (12 rows)
	// Sequence Y is in Rows
	// Place padding rows above sequence top (Sequence is 1 row high)
	anchorY := payload.OriginY - constant.SplashCharHeight - constant.SplashTimerPadding

	// Fallback: If offscreen top, place below
	if anchorY < 0 {
		anchorY = payload.OriginY + 1 + constant.SplashTimerPadding
	}

	// 3. Create Component with Delta Timer
	splash := component.SplashComponent{
		Length:     1,
		Color:      component.SplashColorGold,
		AnchorX:    anchorX,
		AnchorY:    anchorY,
		Mode:       component.SplashModePersistent,
		Remaining:  payload.Duration,
		Duration:   payload.Duration,
		SequenceID: payload.SequenceID,
	}
	// TODO: make it flexible for > 10 and not bound to gold - future expansion
	splash.Content[0] = '9' // Start at 9

	// 4. Spawn
	entity := s.world.CreateEntity()
	s.splashStore.Add(entity, splash)
}

// handleGoldFinish destroys the gold timer
func (s *SplashSystem) handleGoldFinish(sequenceID int) {
	// Find and destroy specific timer
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}
		if splash.Mode == component.SplashModePersistent && splash.SequenceID == sequenceID {
			s.world.DestroyEntity(entity)
			return // Found it
		}
	}
}

// cleanupSplashesByMode removes all splashes of a specific mode
func (s *SplashSystem) cleanupSplashesByMode(mode component.SplashMode) {
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}
		if splash.Mode == mode {
			s.world.DestroyEntity(entity)
		}
	}
}

// calculateSmartLayout determines the best position for a transient splash
// Avoids Cursor and Gold Sequences
func (s *SplashSystem) calculateSmartLayout(cursorX, cursorY, charCount int) (int, int) {
	config := s.res.Config
	width := config.GameWidth
	height := config.GameHeight

	// Splash Dimensions
	splashW := charCount * (constant.SplashCharWidth + constant.SplashCharSpacing)
	splashH := constant.SplashCharHeight

	// Define Quadrant Centers
	// Q0: Top-Left, Q1: Top-Right, Q2: Bottom-Left, Q3: Bottom-Right
	centers := []struct{ x, y int }{
		{width / 4, height / 4},         // Q0
		{width * 3 / 4, height / 4},     // Q1
		{width / 4, height * 3 / 4},     // Q2
		{width * 3 / 4, height * 3 / 4}, // Q3
	}

	// Score Quadrants (Higher is better)
	scores := []int{constant.SplashQuadrantBaseScore, constant.SplashQuadrantBaseScore, constant.SplashQuadrantBaseScore, constant.SplashQuadrantBaseScore}

	// 1. Cursor Penalty
	// Determine cursor quadrant
	cursorQ := 0
	if cursorX >= width/2 {
		cursorQ |= 1
	}
	if cursorY >= height/2 {
		cursorQ |= 2
	}
	scores[cursorQ] -= constant.SplashCursorPenalty

	// 2. Gold Sequence Penalty (-500)
	// Iterate active gold sequences
	goldEntities := s.goldStore.All()
	for _, e := range goldEntities {
		gs, ok := s.goldStore.Get(e)
		if !ok || !gs.Active {
			continue
		}

		// Get position of start of sequence (we need to query positions of member entities?)
		// Since GoldSequenceComponent doesn't store position, we rely on the fact that
		// GoldSystem creates entities
		// Optimization: Check the "Restricted Zone" via stored components?
		// We don't have a direct "Box" component
		// However, we can scan `world.Positions` for entities with `SequenceID`
		// This is O(N) where N is total entities. Fast enough for 200 entities
		// Better: We iterate `s.seqStore` which is smaller

		seqEntities := s.seqStore.All()
		for _, se := range seqEntities {
			seq, ok := s.seqStore.Get(se)
			if !ok || seq.ID != gs.SequenceID {
				continue
			}
			pos, ok := s.world.Positions.Get(se)
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
			// Deduct per char, effectively vetoing the quadrant
			scores[goldQ] -= constant.SplashGoldSequencePenalty
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