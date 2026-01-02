package system

import (
	"math"
	"strconv"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// BBox represents an axis-aligned bounding box for collision detection
type BBox struct {
	X, Y, W, H int
}

// Pre-computed direction vectors for 8 angles (45° increments) in virtual space
// Index 0 = top (0°), 1 = top-right (45°), etc. counter-clockwise
// Values: (cos(angle), sin(angle)) where angle starts from -90° (top)
var spiralDirections = [8][2]float64{
	{0.0, -1.0},      // 0: Top (0°)
	{0.707, -0.707},  // 1: Top-right (45°)
	{1.0, 0.0},       // 2: Right (90°)
	{0.707, 0.707},   // 3: Bottom-right (135°)
	{0.0, 1.0},       // 4: Bottom (180°)
	{-0.707, 0.707},  // 5: Bottom-left (225°)
	{-1.0, 0.0},      // 6: Left (270°)
	{-0.707, -0.707}, // 7: Top-left (315°)
}

// Magnifier angle orders (diagonals first to avoid ping lines, then cardinals)
var magnifierAnglesCCW = [8]int{7, 5, 3, 1, 0, 6, 4, 2} // 315°→225°→135°→45°→0°→270°→180°→90°
var magnifierAnglesCW = [8]int{1, 3, 5, 7, 0, 2, 4, 6}  // 45°→135°→225°→315°→0°→90°→180°→270°

// Timer angle orders (cardinals first, then diagonals as fallback)
var timerAnglesCCW = [8]int{0, 3, 1, 2, 5, 7, 6, 4} // Bottom→Left→Top→Right, then BL→TL→TR→BR
var timerAnglesCW = [8]int{0, 2, 1, 3, 4, 6, 7, 5}  // Bottom→Right→Top→Left, then BR→TR→TL→BL

// SplashSystem manages the lifecycle of splash entities
type SplashSystem struct {
	world *engine.World
	res   engine.Resources

	splashStore *engine.Store[component.SplashComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]
	memberStore *engine.Store[component.MemberComponent]
	glyphStore  *engine.Store[component.GlyphComponent]

	enabled bool
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(world *engine.World) engine.System {
	s := &SplashSystem{
		world: world,
		res:   engine.GetResources(world),

		splashStore: engine.GetStore[component.SplashComponent](world),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](world),
		glyphStore:  engine.GetStore[component.GlyphComponent](world),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *SplashSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *SplashSystem) initLocked() {
	s.enabled = true
}

// Priority returns the system's priority (low, after game logic)
func (s *SplashSystem) Priority() int {
	return constant.PrioritySplash
}

// EventTypes defines the events this system subscribes to
func (s *SplashSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventSplashRequest,
		event.EventGoldSpawned,
		event.EventGoldComplete,
		event.EventGoldTimeout,
		event.EventGoldDestroyed,
		event.EventCursorMoved,
		event.EventQuasarChargeStart,
		event.EventQuasarChargeCancel,
	}
}

// HandleEvent processes events to create or destroy splash entities
func (s *SplashSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

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
			s.handleGoldFinish(payload.AnchorEntity)
		}

	case event.EventCursorMoved:
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok {
			s.handleCursorMoved(payload)
		}

	case event.EventQuasarChargeStart:
		if payload, ok := ev.Payload.(*event.QuasarChargeStartPayload); ok {
			s.handleQuasarChargeStart(payload)
		}

	case event.EventQuasarChargeCancel:
		if payload, ok := ev.Payload.(*event.QuasarChargeCancelPayload); ok {
			s.cleanupSplashesBySlotAndAnchor(component.SlotTimer, payload.AnchorEntity)
		}
	}
}

// Update manages lifecycle of splashes (expiry, timer updates, magnifier validation)
func (s *SplashSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.res.Time.DeltaTime

	// Cache timer bboxes for magnifier collision checks to avoid allocs inside loop if not needed
	var cachedTimerBBoxes []BBox
	timersCached := false

	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}

		// Delta-based time tracking (Robust against clock jumps/resets)
		splash.Remaining -= dt

		switch splash.Slot {
		case component.SlotTimer:

			// Use Slot for timer behavior
			if splash.Slot == component.SlotTimer {
				// Display digits ceiling math - "1" shows for 1.0→0.001s, dies at 0
				remainingSec := int(math.Ceil(splash.Remaining.Seconds()))

				if remainingSec <= 0 {
					// Timer expired - mark for destruction
					s.world.DestroyEntity(entity)
					continue
				}

				// Multi-digit support
				digits := strconv.Itoa(remainingSec)
				newLength := len(digits)

				// Update content if changed
				contentChanged := newLength != splash.Length
				if !contentChanged {
					for i, d := range digits {
						if splash.Content[i] != d {
							contentChanged = true
							break
						}
					}
				}

				if contentChanged {
					splash.Length = newLength
					for i, d := range digits {
						if i < len(splash.Content) {
							splash.Content[i] = d
						}
					}
				}
			}

		case component.SlotMagnifier:
			// Validate magnifier - re-query entity under cursor
			if !s.validateMagnifier(entity, &splash) {
				continue // Entity was destroyed
			}

			// Validate Position against Timers (Continuous Validation)
			if !timersCached {
				cachedTimerBBoxes = s.getTimerBBoxes()
				timersCached = true
			}

			// Construct current BBox for magnifier
			magW := splash.Length * constant.SplashCharWidth
			magH := constant.SplashCharHeight
			magBBox := BBox{X: splash.AnchorX, Y: splash.AnchorY, W: magW, H: magH}

			// Check against inflated timer boxes
			collision := false
			for _, timerBBox := range cachedTimerBBoxes {
				if s.checkBBoxCollision(magBBox, timerBBox) {
					collision = true
					break
				}
			}

			// If collision detected, attempt to find a new valid position
			if collision {
				if pos, ok := s.world.Positions.Get(s.res.Cursor.Entity); ok {
					newX, newY := s.calculateProximityAnchor(pos.X, pos.Y, splash.Length)
					if newX != splash.AnchorX || newY != splash.AnchorY {
						splash.AnchorX = newX
						splash.AnchorY = newY
					}
				}
			}

		}

		// Write back component (state changed)
		s.splashStore.Set(entity, splash)
	}
}

// handleSplashRequest creates a transient splash with smart layout
func (s *SplashSystem) handleSplashRequest(payload *event.SplashRequestPayload) {
	// 1. Enforce Uniqueness: Destroy existing in slot
	s.cleanupSplashesBySlot(component.SlotAction)

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
		Slot:      component.SlotAction,
		Remaining: constant.SplashDuration,
		Duration:  constant.SplashDuration,
	}
	copy(splash.Content[:], runes[:length])

	// 5. Spawn
	entity := s.world.CreateEntity()
	s.splashStore.Set(entity, splash)

	// 6. Register with TimeKeeper for destruction
	s.world.PushEvent(event.EventTimerStart, &event.TimerStartPayload{
		Entity:   entity,
		Duration: constant.SplashDuration,
	})
}

// validateMagnifier checks if magnifier is still valid and updates content if entity changed, returns false if magnifier was destroyed
func (s *SplashSystem) validateMagnifier(splashEntity core.Entity, splash *component.SplashComponent) bool {
	cursorPos, ok := s.world.Positions.Get(s.res.Cursor.Entity)
	if !ok {
		s.world.DestroyEntity(splashEntity)
		return false
	}

	glyphEntity := s.world.Positions.GetTopEntityFiltered(cursorPos.X, cursorPos.Y, func(e core.Entity) bool {
		return s.glyphStore.Has(e)
	})

	if glyphEntity == 0 {
		s.world.DestroyEntity(splashEntity)
		return false
	}

	glyph, ok := s.glyphStore.Get(glyphEntity)
	if !ok {
		s.world.DestroyEntity(splashEntity)
		return false
	}

	// Update if character or type changed (handles entity swap, moving entities)
	newColor := s.glyphToSplashColor(glyph.Type)
	if splash.Content[0] != glyph.Rune || splash.Color != newColor {
		splash.Content[0] = glyph.Rune
		splash.Color = newColor
	}

	return true
}

// TODO: change to generic timer, make gold system send splash timer request with gold anchor
// handleGoldSpawn creates the persistent gold timer anchored to the gold entity
func (s *SplashSystem) handleGoldSpawn(payload *event.GoldSpawnedPayload) {
	s.cleanupSplashesBySlot(component.SlotTimer)

	initialSec := int(math.Ceil(payload.Duration.Seconds()))
	digits := strconv.Itoa(initialSec)
	digitCount := len(digits)

	timerCellWidth := digitCount * constant.SplashCharWidth

	// Calculate offset using cardinal spiral search
	offsetX, offsetY := s.calculateTimerOffset(
		payload.AnchorEntity,
		payload.Length,
		timerCellWidth,
	)

	splash := component.SplashComponent{
		Length:       digitCount,
		Color:        component.SplashColorWhite,
		AnchorEntity: payload.AnchorEntity,
		OffsetX:      offsetX,
		OffsetY:      offsetY,
		Slot:         component.SlotTimer,
		Remaining:    payload.Duration,
		Duration:     payload.Duration,
	}

	for i, d := range digits {
		if i < len(splash.Content) {
			splash.Content[i] = d
		}
	}

	entity := s.world.CreateEntity()
	s.splashStore.Set(entity, splash)
}

// handleGoldFinish destroys the gold timer
func (s *SplashSystem) handleGoldFinish(anchorEntity core.Entity) {
	// Find and destroy specific timer
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}
		if splash.Slot == component.SlotTimer && splash.AnchorEntity == anchorEntity {
			s.world.DestroyEntity(entity)
			return // Found it
		}
	}
}

// handleQuasarChargeStart creates the quasar charge countdown timer
func (s *SplashSystem) handleQuasarChargeStart(payload *event.QuasarChargeStartPayload) {
	s.cleanupSplashesBySlotAndAnchor(component.SlotTimer, payload.AnchorEntity)

	initialSec := int(math.Ceil(payload.Duration.Seconds()))
	digits := strconv.Itoa(initialSec)
	digitCount := len(digits)

	timerCellWidth := digitCount * constant.SplashCharWidth
	offsetX, offsetY := s.calculateTimerOffsetForQuasar(payload.AnchorEntity, timerCellWidth)

	splash := component.SplashComponent{
		Length:       digitCount,
		Color:        component.SplashColorCyan,
		AnchorEntity: payload.AnchorEntity,
		OffsetX:      offsetX,
		OffsetY:      offsetY,
		Slot:         component.SlotTimer,
		Remaining:    payload.Duration,
		Duration:     payload.Duration,
	}

	for i, d := range digits {
		if i < len(splash.Content) {
			splash.Content[i] = d
		}
	}

	entity := s.world.CreateEntity()
	s.splashStore.Set(entity, splash)
}

// calculateTimerOffsetForQuasar positions timer above quasar center
func (s *SplashSystem) calculateTimerOffsetForQuasar(anchorEntity core.Entity, timerWidth int) (int, int) {
	config := s.res.Config
	timerH := constant.SplashCharHeight
	padding := constant.SplashTimerPadding

	var anchorX, anchorY int
	if anchorEntity != 0 {
		if pos, ok := s.world.Positions.Get(anchorEntity); ok {
			anchorX = pos.X
			anchorY = pos.Y
		}
	}

	// Center above quasar
	offsetX := -timerWidth / 2
	offsetY := -constant.QuasarAnchorOffsetY - timerH - padding

	// Bounds adjustment
	absX := anchorX + offsetX
	absY := anchorY + offsetY

	if absY < 0 {
		// Place below instead
		offsetY = constant.QuasarHeight - constant.QuasarAnchorOffsetY + padding
	}
	if absX < 0 {
		offsetX = -anchorX
	}
	if absX+timerWidth > config.GameWidth {
		offsetX = config.GameWidth - anchorX - timerWidth
	}

	return offsetX, offsetY
}

// cleanupSplashesBySlot removes all splashes of a specific slot
func (s *SplashSystem) cleanupSplashesBySlot(slot component.SplashSlot) {
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}
		if splash.Slot == slot {
			s.world.DestroyEntity(entity)
		}
	}
}

// cleanupSplashesBySlot removes all splashes of a specific slot for a composite anchor
func (s *SplashSystem) cleanupSplashesBySlotAndAnchor(slot component.SplashSlot, anchor core.Entity) {
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok {
			continue
		}
		if splash.Slot == slot && splash.AnchorEntity == anchor {
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
	splashW := charCount * constant.SplashCharWidth
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

	// 2. Gold Sequence Penalty
	// Iterate composites with BehaviorGold
	anchors := s.headerStore.All()
	for _, anchor := range anchors {
		header, ok := s.headerStore.Get(anchor)
		if !ok || header.BehaviorID != component.BehaviorGold {
			continue
		}

		// Check each living member's position
		for _, m := range header.Members {
			if m.Entity == 0 {
				continue
			}
			pos, ok := s.world.Positions.Get(m.Entity)
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
			// Apply soft penalty (cumulative, effectively vetoes the quadrant)
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

// handleCursorMoved updates the magnifier splash based on cursor position
func (s *SplashSystem) handleCursorMoved(payload *event.CursorMovedPayload) {
	cursorX, cursorY := payload.X, payload.Y

	// Query glyph entity under cursor
	entity := s.world.Positions.GetTopEntityFiltered(cursorX, cursorY, func(e core.Entity) bool {
		return s.glyphStore.Has(e)
	})

	if entity == 0 {
		// No glyph entity - clear magnifier
		s.cleanupSplashesBySlot(component.SlotMagnifier)
		return
	}

	// Get the character to display
	glyph, ok := s.glyphStore.Get(entity)
	if !ok {
		s.cleanupSplashesBySlot(component.SlotMagnifier)
		return
	}

	// Resolve color from glyph type
	color := s.glyphToSplashColor(glyph.Type)

	// Calculate proximity anchor (between cursor and center, min 15 chars away)
	anchorX, anchorY := s.calculateProximityAnchor(cursorX, cursorY, 1)

	// Check for existing magnifier to update in place
	existing := s.findSplashBySlot(component.SlotMagnifier)
	if existing != 0 {
		splash, ok := s.splashStore.Get(existing)
		if ok {
			splash.Content[0] = glyph.Rune
			splash.Length = 1
			splash.Color = color
			splash.AnchorX = anchorX
			splash.AnchorY = anchorY
			s.splashStore.Set(existing, splash)
			return
		}
	}

	// Create new magnifier (persistent - no expiry while on char)
	splash := component.SplashComponent{
		Length:    1,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		Slot:      component.SlotMagnifier,
		Remaining: 0, // Persistent - managed by cursor movement
		Duration:  0,
	}
	splash.Content[0] = glyph.Rune

	newEntity := s.world.CreateEntity()
	s.splashStore.Set(newEntity, splash)
}

// calculateProximityAnchor uses spiral search to find valid magnifier position
// Works in virtual circular space, converts back to game elliptical space
// Searches diagonals first to avoid overlapping with ping crosshair lines
func (s *SplashSystem) calculateProximityAnchor(cursorX, cursorY, charCount int) (int, int) {
	config := s.res.Config
	centerX := config.GameWidth / 2
	centerY := config.GameHeight / 2

	splashW := charCount * constant.SplashCharWidth
	splashH := constant.SplashCharHeight
	minDist := float64(constant.SplashMinDistance)

	timerBBoxes := s.getTimerBBoxes()

	// Determine search direction and select angle order
	searchCCW := s.getSearchDirection(cursorX, cursorY, centerX, centerY)
	var angleOrder [8]int
	if searchCCW {
		angleOrder = magnifierAnglesCCW
	} else {
		angleOrder = magnifierAnglesCW
	}

	// Spiral search: 3 radius levels, 8 angles each (diagonals first)
	for radiusLevel := 0; radiusLevel < 3; radiusLevel++ {
		radius := minDist * float64(1+radiusLevel)

		for _, angleIdx := range angleOrder {
			dir := spiralDirections[angleIdx]

			// Calculate position in virtual space (circular)
			vX := float64(cursorX) + dir[0]*radius
			vY := float64(cursorY)*2.0 + dir[1]*radius // Convert cursor Y to virtual

			// Convert back to game space
			anchorX := int(vX) - splashW/2
			anchorY := int(vY/2.0) - splashH/2

			// Bounds check
			if anchorX < 0 || anchorX+splashW > config.GameWidth ||
				anchorY < 0 || anchorY+splashH > config.GameHeight {
				continue
			}

			// Collision check against timers
			candidate := BBox{X: anchorX, Y: anchorY, W: splashW, H: splashH}
			collides := false
			for _, timer := range timerBBoxes {
				if s.checkBBoxCollision(candidate, timer) {
					collides = true
					break
				}
			}

			if !collides {
				return anchorX, anchorY
			}
		}
	}

	// Final fallback: top-left diagonal position, clamped
	anchorX := cursorX - splashW/2
	anchorY := cursorY - splashH - int(minDist)

	if anchorX < 0 {
		anchorX = 0
	}
	if anchorX+splashW > config.GameWidth {
		anchorX = config.GameWidth - splashW
	}
	if anchorY < 0 {
		anchorY = 0
	}
	if anchorY+splashH > config.GameHeight {
		anchorY = config.GameHeight - splashH
	}

	return anchorX, anchorY
}

// calculateTimerOffset finds valid offset for timer relative to anchor entity
// Uses 8-direction search: cardinals first, then diagonals
func (s *SplashSystem) calculateTimerOffset(anchorEntity core.Entity, seqLength, timerWidth int) (int, int) {
	config := s.res.Config
	centerX := config.GameWidth / 2
	timerH := constant.SplashCharHeight
	padding := constant.SplashTimerPadding

	var anchorX, anchorY int
	if anchorEntity != 0 {
		if pos, ok := s.world.Positions.Get(anchorEntity); ok {
			anchorX = pos.X
			anchorY = pos.Y
		}
	}

	seqCenter := seqLength / 2
	timerHalfW := timerWidth / 2
	timerHalfH := timerH / 2

	// 8 position offsets: cardinals (0-3), then diagonals (4-7)
	type posOffset struct{ x, y int }
	positions := []posOffset{
		{seqCenter - timerHalfW, 1 + padding},       // 0: Bottom
		{seqCenter - timerHalfW, -timerH - padding}, // 1: Top
		{seqLength + padding, -timerHalfH},          // 2: Right
		{-timerWidth - padding, -timerHalfH},        // 3: Left
		{seqLength + padding, 1 + padding},          // 4: Bottom-right
		{-timerWidth - padding, 1 + padding},        // 5: Bottom-left
		{seqLength + padding, -timerH - padding},    // 6: Top-right
		{-timerWidth - padding, -timerH - padding},  // 7: Top-left
	}

	// Select angle order based on anchor quadrant
	var order [8]int
	if anchorX > centerX {
		order = timerAnglesCCW
	} else {
		order = timerAnglesCW
	}

	for _, idx := range order {
		offset := positions[idx]
		absX := anchorX + offset.x
		absY := anchorY + offset.y

		if absX >= 0 && absX+timerWidth <= config.GameWidth &&
			absY >= 0 && absY+timerH <= config.GameHeight {
			return offset.x, offset.y
		}
	}

	// Fallback: bottom position (may be OOB)
	return seqCenter - timerHalfW, 1 + padding
}

// getSearchDirection determines spiral rotation direction
// Returns true for counter-clockwise, false for clockwise
// Search rotates from top (0°) towards the direction of screen center
func (s *SplashSystem) getSearchDirection(cursorX, cursorY, centerX, centerY int) bool {
	// Edge cases: exactly at center top/bottom
	if cursorX == centerX {
		return true // counter-clockwise
	}

	// Quadrant-based direction
	// Top-right or bottom-right: counter-clockwise (search left toward center)
	// Top-left or bottom-left: clockwise (search right toward center)
	return cursorX > centerX
}

// getTimerBBoxes returns bounding boxes for all SlotTimer splashes
func (s *SplashSystem) getTimerBBoxes() []BBox {
	var boxes []BBox

	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if !ok || splash.Slot != component.SlotTimer {
			continue
		}

		// Resolve actual position
		x, y := splash.AnchorX, splash.AnchorY
		if splash.AnchorEntity != 0 {
			if pos, ok := s.world.Positions.Get(splash.AnchorEntity); ok {
				x = pos.X + splash.OffsetX
				y = pos.Y + splash.OffsetY
			}
		}

		// Dynamic width for timer
		w := splash.Length * constant.SplashCharWidth
		h := constant.SplashCharHeight

		// Apply padding for inter-splash collision (Inflated Bounding Box)
		pad := constant.SplashCollisionPadding
		boxes = append(boxes, BBox{
			X: x - pad,
			Y: y - pad,
			W: w + (pad * 2),
			H: h + (pad * 2),
		})
	}

	return boxes
}

// checkBBoxCollision returns true if two bounding boxes overlap
func (s *SplashSystem) checkBBoxCollision(a, b BBox) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X &&
		a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

// findSplashBySlot returns entity ID of first splash with given slot, or 0
func (s *SplashSystem) findSplashBySlot(slot component.SplashSlot) core.Entity {
	entities := s.splashStore.All()
	for _, entity := range entities {
		splash, ok := s.splashStore.Get(entity)
		if ok && splash.Slot == slot {
			return entity
		}
	}
	return 0
}

// glyphToSplashColor maps GlyphType to SplashColor
func (s *SplashSystem) glyphToSplashColor(t component.GlyphType) component.SplashColor {
	switch t {
	case component.GlyphGreen:
		return component.SplashColorGreen
	case component.GlyphBlue:
		return component.SplashColorBlue
	case component.GlyphRed:
		return component.SplashColorRed
	case component.GlyphGold:
		return component.SplashColorGold
	default:
		return component.SplashColorNormal
	}
}