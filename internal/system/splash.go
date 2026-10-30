package system

import (
	"strconv"
	"time"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/position"
)

// BBox represents an axis-aligned bounding box for collision detection
type BBox struct {
	X, Y, W, H int
}

// SplashSystem manages the lifecycle of splash entities
type SplashSystem struct {
	world *engine.World

	// boxes is every countdown's padded box, filled once a layout pass and moved
	// with each countdown placed, so a placement sees the ones placed before it
	boxes []timerBox

	toggle
}

// timerBox is a countdown's padded box and the countdown it belongs to
type timerBox struct {
	entity core.Entity
	BBox
}

// timerInk is the box a countdown's digits ink inside their cells, the union over 0-9
// so a countdown holds still as its count changes. Placement reserves the ink, not
// the cells, whose blank rows and columns would keep the count off rows it fits in.
var timerInk = func() BBox {
	left, top, right, bottom := parameter.SplashCharWidth, parameter.SplashCharHeight, 0, 0
	for digit := '0'; digit <= '9'; digit++ {
		x, y, w, h := asset.SplashInkBounds(asset.SplashFont[digit-' '])
		left, top = min(left, x), min(top, y)
		right, bottom = max(right, x+w), max(bottom, y+h)
	}
	return BBox{X: left, Y: top, W: right - left, H: bottom - top}
}()

// timerInkBox is the box a countdown of length digits inks, from its first cell's origin
func timerInkBox(length int) BBox {
	return BBox{X: timerInk.X, Y: timerInk.Y, W: (max(length, 1)-1)*parameter.SplashCharWidth + timerInk.W, H: timerInk.H}
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(world *engine.World) engine.System {
	s := &SplashSystem{
		world: world,
	}
	return s
}

// Init resets session state for new game
func (s *SplashSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *SplashSystem) Name() string {
	return "splash"
}

// Priority returns the system's priority (low, after game logic)
func (s *SplashSystem) Priority() int {
	return parameter.PrioritySplash
}

// EventTypes defines the events this system subscribes to
func (s *SplashSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSplashTimerRequest,
		event.EventSplashTimerCancel,
		event.EventCursorMoved,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
	}
}

// HandleEvent processes events to create or destroy splash entities
func (s *SplashSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if ev.Type == event.EventCursorDespawned {
		if payload, ok := ev.Payload.(*event.CursorDespawnedPayload); ok &&
			payload.Slot == s.world.Resources.Player.LocalSlot() {
			s.cleanupSplashesBySlot(component.SlotMagnifier)
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventSplashTimerRequest:
		if payload, ok := ev.Payload.(*event.SplashTimerRequestPayload); ok {
			s.handleTimerSpawn(payload)
		}

	case event.EventSplashTimerCancel:
		if payload, ok := ev.Payload.(*event.SplashTimerCancelPayload); ok {
			s.handleTimerCancel(payload.AnchorEntity)
		}

	case event.EventCursorMoved:
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok &&
			s.world.Resources.Player.IsLocal(payload.Entity) {
			s.handleCursorMoved(payload)
		}
	}
}

// Update manages lifecycle of splashes (expiry, timer updates, magnifier validation)
func (s *SplashSystem) Update() {
	if !s.enabled {
		return
	}

	s.fillTimerBoxes()
	next := 0 // the next countdown's own box; boxes follow the store's order

	splashes := s.world.Components.Splash
	splashEntities := splashes.Entities()
	for _, splashEntity := range splashEntities {
		// Expiry and invalid-anchor exits intentionally skip write-back while the
		// death event is queued, so keep the component value detached.
		splashComp, ok := splashes.GetComponent(splashEntity)
		if !ok {
			continue
		}

		switch splashComp.Slot {
		case component.SlotTimer:
			// Its own box, which its placement skips and then moves with it
			self := -1
			if next < len(s.boxes) && s.boxes[next].entity == splashEntity {
				self = next
				next++
			}

			// An expired countdown is already leaving through the timer's death batch
			timer, ok := s.world.Components.Timer.GetPtr(splashEntity)
			if !ok || timer.Expired(component.TimerLifetime) {
				continue
			}

			anchorEntity := splashComp.AnchorEntity
			if anchorEntity != 0 && !s.world.Components.Header.HasEntity(anchorEntity) {
				// Anchored to entity and anchor entity destroyed
				event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
				continue
			}

			setTimerContent(&splashComp, timer.Slots[component.TimerLifetime].Remaining)

			s.calculateTimerOffset(&splashComp, self)
			if self >= 0 {
				s.boxes[self].BBox, _ = s.paddedBox(&splashComp)
			}
			splashes.SetComponent(splashEntity, splashComp)

		case component.SlotMagnifier:
			// Validate magnifier - re-query entity under cursor
			if !s.validateMagnifier(splashEntity, &splashComp) {
				continue // Entity was destroyed
			}

			// Construct current BBox for magnifier
			magW := splashComp.Length * parameter.SplashCharWidth
			magH := parameter.SplashCharHeight
			magBBox := BBox{X: splashComp.AnchorX, Y: splashComp.AnchorY, W: magW, H: magH}

			// A countdown placed over the magnifier moves the magnifier
			if s.collidesTimer(magBBox, -1) {
				if pos, ok := s.world.LocalCursor(); ok {
					newX, newY := s.calculateProximityAnchor(pos.X, pos.Y, splashComp.Length)
					if newX != splashComp.AnchorX || newY != splashComp.AnchorY {
						splashComp.AnchorX = newX
						splashComp.AnchorY = newY
					}
				}
			}

		}

		splashes.SetComponent(splashEntity, splashComp)
	}
}

// validateMagnifier checks if magnifier is still valid and updates content if entity changed, returns false if magnifier was destroyed
func (s *SplashSystem) validateMagnifier(splashEntity core.Entity, splash *component.SplashComponent) bool {
	cursorPos, ok := s.world.LocalCursor()
	if !ok {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		return false
	}

	glyphEntity, ok := s.world.Positions.TopGlyphAt(cursorPos.X, cursorPos.Y)
	if !ok {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		return false
	}

	glyph, ok := s.world.Components.Glyph.GetComponent(glyphEntity)
	if !ok {
		event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		return false
	}

	// Update if character or type changed (handles entity swap, moving entities)
	newColor := visual.GlyphColorLUT[glyph.Type][component.GlyphNormal]
	if splash.Content[0] != glyph.Rune || splash.Color != newColor {
		splash.Content[0] = glyph.Rune
		splash.Color = newColor
	}

	return true
}

// handleTimerSpawn creates the persistent timer anchored to the anchor entity, or
// placed around its origin cell without one
func (s *SplashSystem) handleTimerSpawn(payload *event.SplashTimerRequestPayload) {
	s.cleanupTimers(payload.AnchorEntity, payload.OriginX, payload.OriginY)

	splashComp := component.SplashComponent{
		Color:        payload.Color,
		AnchorEntity: payload.AnchorEntity,
		AnchorX:      payload.OriginX,
		AnchorY:      payload.OriginY,
		MarginLeft:   payload.MarginLeft,
		MarginRight:  payload.MarginRight,
		MarginTop:    payload.MarginTop,
		MarginBottom: payload.MarginBottom,
		Slot:         component.SlotTimer,
	}
	setTimerContent(&splashComp, payload.Duration)

	// The new countdown is not in the store yet, so no box is its own
	s.fillTimerBoxes()
	s.calculateTimerOffset(&splashComp, -1)

	splashEntity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Components.Splash.SetComponent(splashEntity, splashComp)
	s.world.Components.Protection.SetComponent(splashEntity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies,
	})
	// A countdown already at zero still ends on the first pass instead of leaving it idle
	armTimer(s.world, splashEntity, component.TimerLifetime, max(payload.Duration, time.Nanosecond))
}

// setTimerContent is the single representation boundary for a countdown. A
// duration can hold more decimal seconds than SplashComponent.Content can hold;
// saturating that exceptional value preserves the component invariant and makes
// both layout and rendering safe. Positive fractional seconds round up so "1"
// remains visible until expiry.
func setTimerContent(splash *component.SplashComponent, remaining time.Duration) {
	seconds := int64(remaining / time.Second)
	if remaining > 0 && remaining%time.Second != 0 {
		seconds++
	}
	if seconds < 0 {
		seconds = 0
	}

	digits := strconv.FormatInt(seconds, 10)
	if len(digits) > len(splash.Content) {
		splash.Length = len(splash.Content)
		for i := range splash.Content {
			splash.Content[i] = '9'
		}
		return
	}

	splash.Length = len(digits)
	for i := range digits {
		splash.Content[i] = rune(digits[i])
	}
}

// handleTimerCancel destroys existing timer splash
func (s *SplashSystem) handleTimerCancel(anchorEntity core.Entity) {
	// Find and destroy specific timer
	splashEntities := s.world.Components.Splash.Entities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if !ok {
			continue
		}
		if splashComp.Slot == component.SlotTimer && splashComp.AnchorEntity == anchorEntity {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
			return
		}
	}
}

// cleanupSplashesBySlot removes all splashes of a specific slot
func (s *SplashSystem) cleanupSplashesBySlot(slot component.SplashSlot) {
	splashEntities := s.world.Components.Splash.Entities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if !ok {
			continue
		}
		if splashComp.Slot == slot {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		}
	}
}

// cleanupTimers removes the countdowns of a composite anchor, or without one those
// of the origin cell
func (s *SplashSystem) cleanupTimers(anchor core.Entity, x, y int) {
	splashEntities := s.world.Components.Splash.Entities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if !ok || splashComp.Slot != component.SlotTimer || splashComp.AnchorEntity != anchor {
			continue
		}
		if anchor != 0 || splashComp.AnchorX == x && splashComp.AnchorY == y {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		}
	}
}

// handleCursorMoved updates the magnifier splash based on cursor position
func (s *SplashSystem) handleCursorMoved(payload *event.CursorMovedPayload) {
	cursorX, cursorY := payload.X, payload.Y

	entity, ok := s.world.Positions.TopGlyphAt(cursorX, cursorY)
	if !ok {
		// No glyph entity - clear magnifier
		s.cleanupSplashesBySlot(component.SlotMagnifier)
		return
	}

	// Get the character to display
	glyphComp, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		s.cleanupSplashesBySlot(component.SlotMagnifier)
		return
	}

	// Resolve color from glyph type
	color := visual.GlyphColorLUT[glyphComp.Type][component.GlyphNormal]

	// Calculate proximity anchor, at least SplashMinDistance from the cursor
	s.fillTimerBoxes()
	anchorX, anchorY := s.calculateProximityAnchor(cursorX, cursorY, 1)

	// Check for existing magnifier to update in place
	existingSplashEntity := s.findSplashEntityBySlot(component.SlotMagnifier)
	if existingSplashEntity != 0 {
		splashComp, ok := s.world.Components.Splash.GetPtr(existingSplashEntity)
		if ok {
			splashComp.Content[0] = glyphComp.Rune
			splashComp.Length = 1
			splashComp.Color = color
			splashComp.AnchorX = anchorX
			splashComp.AnchorY = anchorY
			return
		}
	}

	// Create new magnifier (persistent - no expiry while on char)
	splashComp := component.SplashComponent{
		Length:  1,
		Color:   color,
		AnchorX: anchorX,
		AnchorY: anchorY,
		Slot:    component.SlotMagnifier,
	}
	splashComp.Content[0] = glyphComp.Rune

	splashEntity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Components.Splash.SetComponent(splashEntity, splashComp)
	s.world.Components.Protection.SetComponent(splashEntity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies,
	})
}

// calculateProximityAnchor uses spiral search to find valid magnifier position
// Works in virtual circular space, converts back to game elliptical space
// Searches diagonals first to avoid overlapping with ping crosshair lines, clear of
// the countdown boxes its caller filled
func (s *SplashSystem) calculateProximityAnchor(cursorX, cursorY, charCount int) (int, int) {
	config := s.world.Resources.Config

	splashW := charCount * parameter.SplashCharWidth
	splashH := parameter.SplashCharHeight

	if splashW > config.MapWidth || splashH > config.MapHeight {
		return max(0, cursorX-splashW/2), max(0, cursorY-splashH/2)
	}

	checkTimers := func(absX, absY, w, h int) bool {
		return !s.collidesTimer(BBox{X: absX, Y: absY, W: w, H: h}, -1)
	}

	absX, absY, found := s.world.Positions.FindFreeFromPattern(
		cursorX, cursorY,
		splashW, splashH,
		position.PatternDiagonalFirst,
		parameter.SplashMinDistance,
		parameter.SplashMinDistance*3,
		true,
		component.WallBlockAll,
		checkTimers,
	)

	if found {
		return absX, absY
	}

	anchorX := max(0, min(cursorX-splashW/2, config.MapWidth-splashW))
	anchorY := max(0, min(cursorY-splashH/2, config.MapHeight-splashH))
	return anchorX, anchorY
}

// collidesTimer reports whether candidate overlaps any countdown's box but the one at skip
func (s *SplashSystem) collidesTimer(candidate BBox, skip int) bool {
	for i := range s.boxes {
		if i != skip && s.checkBBoxCollision(candidate, s.boxes[i].BBox) {
			return true
		}
	}
	return false
}

// calculateTimerOffset finds valid offset for timer relative to anchor entity, clear
// of every countdown's box but its own at self
// Uses 8-direction search: cardinals first, then diagonals
func (s *SplashSystem) calculateTimerOffset(splashComp *component.SplashComponent, self int) {
	ink := timerInkBox(splashComp.Length)

	anchorX, anchorY := splashComp.AnchorX, splashComp.AnchorY
	if splashComp.AnchorEntity != 0 {
		anchorX, anchorY = 0, 0
		if pos, ok := s.world.Positions.GetPosition(splashComp.AnchorEntity); ok {
			anchorX = pos.X
			anchorY = pos.Y
		}
	}

	checkTimers := func(absX, absY, w, h int) bool {
		return !s.collidesTimer(BBox{X: absX, Y: absY, W: w, H: h}, self)
	}

	exclusion := position.Exclusion{
		Left:   splashComp.MarginLeft,
		Right:  splashComp.MarginRight,
		Top:    splashComp.MarginTop,
		Bottom: splashComp.MarginBottom,
	}

	// Where nothing is clear, the search returns the slot overlapping the least
	offsetX, offsetY, _ := s.world.Positions.FindPlacementAroundExclusion(
		anchorX, anchorY,
		ink.W, ink.H,
		exclusion,
		parameter.SplashTimerPadding,
		position.PatternCardinalFirst,
		component.WallBlockAll,
		checkTimers,
	)

	// The search placed the ink; the cells start that far up and left of it
	splashComp.OffsetX = offsetX - ink.X
	splashComp.OffsetY = offsetY - ink.Y
}

// fillTimerBoxes rebuilds the box of every countdown that holds space, in store order
func (s *SplashSystem) fillTimerBoxes() {
	s.boxes = s.boxes[:0]
	for _, splashEntity := range s.world.Components.Splash.Entities() {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if !ok || splashComp.Slot != component.SlotTimer {
			continue
		}
		if box, ok := s.paddedBox(splashComp); ok {
			s.boxes = append(s.boxes, timerBox{entity: splashEntity, BBox: box})
		}
	}
}

// paddedBox is the space a countdown holds against the other splashes; a countdown
// whose anchor is gone is leaving and holds none
func (s *SplashSystem) paddedBox(splashComp *component.SplashComponent) (BBox, bool) {
	x, y := splashComp.AnchorX, splashComp.AnchorY
	if splashComp.AnchorEntity != 0 {
		pos, ok := s.world.Positions.GetPosition(splashComp.AnchorEntity)
		if !ok {
			return BBox{}, false
		}
		x, y = pos.X, pos.Y
	}
	x, y = x+splashComp.OffsetX, y+splashComp.OffsetY

	pad := parameter.SplashCollisionPadding
	ink := timerInkBox(splashComp.Length)
	return BBox{
		X: x + ink.X - pad,
		Y: y + ink.Y - pad,
		W: ink.W + pad*2,
		H: ink.H + pad*2,
	}, true
}

// checkBBoxCollision returns true if two bounding boxes overlap
func (s *SplashSystem) checkBBoxCollision(a, b BBox) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X &&
		a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

// findSplashEntityBySlot returns entity ID of first splash with given slot, or 0
func (s *SplashSystem) findSplashEntityBySlot(slot component.SplashSlot) core.Entity {
	splashEntities := s.world.Components.Splash.Entities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if ok && splashComp.Slot == slot {
			return splashEntity
		}
	}
	return 0
}
