package system

import (
	"math"
	"strconv"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// BBox represents an axis-aligned bounding box for collision detection
type BBox struct {
	X, Y, W, H int
}

// SplashSystem manages the lifecycle of splash entities
type SplashSystem struct {
	world *engine.World

	enabled bool
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(world *engine.World) engine.System {
	s := &SplashSystem{
		world: world,
	}
	s.Init()
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
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes events to create or destroy splash entities
func (s *SplashSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
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
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok {
			s.handleCursorMoved(payload)
		}
	}
}

// Update manages lifecycle of splashes (expiry, timer updates, magnifier validation)
func (s *SplashSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	// Cache timer bboxes for magnifier collision checks to avoid alloc inside loop if not needed
	var cachedTimerBBoxes []BBox
	timersCached := false

	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if !ok {
			continue
		}

		// Delta-based time tracking (Robust against clock jumps/resets)
		splashComp.Remaining -= dt

		switch splashComp.Slot {
		case component.SlotTimer:
			anchorEntity := splashComp.AnchorEntity
			if anchorEntity != 0 && !s.world.Components.Header.HasEntity(anchorEntity) {
				// Anchored to entity and anchor entity destroyed
				s.world.DestroyEntity(splashEntity)
				continue
			}

			// Display digits ceiling math - "1" shows for 1.0→0.001s, dies at 0
			remainingSec := int(math.Ceil(splashComp.Remaining.Seconds()))

			if remainingSec <= 0 {
				// Timer expired - mark for destruction
				s.world.DestroyEntity(splashEntity)
				continue
			}

			// Multi-digit support
			digits := strconv.Itoa(remainingSec)
			newLength := len(digits)

			// TODO: Defensive, trace if necessary
			if newLength > parameter.SplashMaxLength {
				newLength = parameter.SplashMaxLength
			}

			// Update content if changed
			splashComp.Length = newLength
			for i, d := range digits {
				splashComp.Content[i] = d
			}

			// Recalculate offset with inter-timer collision (exclude self)
			timerBBoxes := s.getTimerBBoxes(splashEntity)
			s.calculateTimerOffset(&splashComp, timerBBoxes)

			s.world.Components.Splash.SetComponent(splashEntity, splashComp)

		case component.SlotMagnifier:
			// Validate magnifier - re-query entity under cursor
			if !s.validateMagnifier(splashEntity, &splashComp) {
				continue // Entity was destroyed
			}

			// Validate Positions against Timers (Continuous Validation)
			if !timersCached {
				cachedTimerBBoxes = s.getTimerBBoxes(0)
				timersCached = true
			}

			// Construct current BBox for magnifier
			magW := splashComp.Length * parameter.SplashCharWidth
			magH := parameter.SplashCharHeight
			magBBox := BBox{X: splashComp.AnchorX, Y: splashComp.AnchorY, W: magW, H: magH}

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
				if pos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity); ok {
					newX, newY := s.calculateProximityAnchor(pos.X, pos.Y, splashComp.Length)
					if newX != splashComp.AnchorX || newY != splashComp.AnchorY {
						splashComp.AnchorX = newX
						splashComp.AnchorY = newY
					}
				}
			}

		}

		// Write back component (state changed)
		s.world.Components.Splash.SetComponent(splashEntity, splashComp)
	}
}

// validateMagnifier checks if magnifier is still valid and updates content if entity changed, returns false if magnifier was destroyed
func (s *SplashSystem) validateMagnifier(splashEntity core.Entity, splash *component.SplashComponent) bool {
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	if !ok {
		s.world.DestroyEntity(splashEntity)
		return false
	}

	// Zero-allocation lookup
	var buf [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(cursorPos.X, cursorPos.Y, buf[:])

	var glyphEntity core.Entity

	for i := 0; i < count; i++ {
		if s.world.Components.Glyph.HasEntity(buf[i]) {
			glyphEntity = buf[i]
			break
		}
	}

	if glyphEntity == 0 {
		s.world.DestroyEntity(splashEntity)
		return false
	}

	glyph, ok := s.world.Components.Glyph.GetComponent(glyphEntity)
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

// handleTimerSpawn creates the persistent timer anchored to the anchor entity
func (s *SplashSystem) handleTimerSpawn(payload *event.SplashTimerRequestPayload) {
	s.cleanupSplashesBySlotAndAnchor(component.SlotTimer, payload.AnchorEntity)

	initialSec := int(math.Ceil(payload.Duration.Seconds()))
	digits := strconv.Itoa(initialSec)
	digitCount := len(digits)

	splashComp := component.SplashComponent{
		Length:       digitCount,
		Color:        payload.Color,
		AnchorEntity: payload.AnchorEntity,
		MarginLeft:   payload.MarginLeft,
		MarginRight:  payload.MarginRight,
		MarginTop:    payload.MarginTop,
		MarginBottom: payload.MarginBottom,
		Slot:         component.SlotTimer,
		Remaining:    payload.Duration,
		Duration:     payload.Duration,
	}

	for i, d := range digits {
		if i < len(splashComp.Content) {
			splashComp.Content[i] = d
		}
	}

	// Get existing timer bboxes (new timer not yet created, no exclusion needed)
	timerBBoxes := s.getTimerBBoxes(0)
	s.calculateTimerOffset(&splashComp, timerBBoxes)

	entity := s.world.CreateEntity()
	s.world.Components.Splash.SetComponent(entity, splashComp)
	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromDrain | component.ProtectFromDelete,
	})
}

// handleTimerCancel destroys existing timer splash
func (s *SplashSystem) handleTimerCancel(anchorEntity core.Entity) {
	// Find and destroy specific timer
	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if !ok {
			continue
		}
		if splashComp.Slot == component.SlotTimer && splashComp.AnchorEntity == anchorEntity {
			s.world.DestroyEntity(splashEntity)
			return // Found it
		}
	}
}

// cleanupSplashesBySlot removes all splashes of a specific slot
func (s *SplashSystem) cleanupSplashesBySlot(slot component.SplashSlot) {
	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if !ok {
			continue
		}
		if splashComp.Slot == slot {
			s.world.DestroyEntity(splashEntity)
		}
	}
}

// cleanupSplashesBySlot removes all splashes of a specific slot for a composite anchor
func (s *SplashSystem) cleanupSplashesBySlotAndAnchor(slot component.SplashSlot, anchor core.Entity) {
	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if !ok {
			continue
		}
		if splashComp.Slot == slot && splashComp.AnchorEntity == anchor {
			s.world.DestroyEntity(splashEntity)
		}
	}
}

// handleCursorMoved updates the magnifier splash based on cursor position
func (s *SplashSystem) handleCursorMoved(payload *event.CursorMovedPayload) {
	cursorX, cursorY := payload.X, payload.Y

	// Zero-allocation lookup of glyph under cursor
	var buf [parameter.MaxEntitiesPerCell]core.Entity
	count := s.world.Positions.GetAllEntitiesAtInto(cursorX, cursorY, buf[:])

	var entity core.Entity

	for i := 0; i < count; i++ {
		if s.world.Components.Glyph.HasEntity(buf[i]) {
			entity = buf[i]
			break
		}
	}

	if entity == 0 {
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
	color := s.glyphToSplashColor(glyphComp.Type)

	// Calculate proximity anchor (between cursor and center, min 15 chars away)
	anchorX, anchorY := s.calculateProximityAnchor(cursorX, cursorY, 1)

	// Check for existing magnifier to update in place
	existingSplashEntity := s.findSplashEntityBySlot(component.SlotMagnifier)
	if existingSplashEntity != 0 {
		splashComp, ok := s.world.Components.Splash.GetComponent(existingSplashEntity)
		if ok {
			splashComp.Content[0] = glyphComp.Rune
			splashComp.Length = 1
			splashComp.Color = color
			splashComp.AnchorX = anchorX
			splashComp.AnchorY = anchorY
			s.world.Components.Splash.SetComponent(existingSplashEntity, splashComp)
			return
		}
	}

	// Create new magnifier (persistent - no expiry while on char)
	splashComp := component.SplashComponent{
		Length:    1,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		Slot:      component.SlotMagnifier,
		Remaining: 0, // Persistent - managed by cursor movement
		Duration:  0,
	}
	splashComp.Content[0] = glyphComp.Rune

	newEntity := s.world.CreateEntity()
	s.world.Components.Splash.SetComponent(newEntity, splashComp)
	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectFromDrain | component.ProtectFromDelete,
	})
}

// calculateProximityAnchor uses spiral search to find valid magnifier position
// Works in virtual circular space, converts back to game elliptical space
// Searches diagonals first to avoid overlapping with ping crosshair lines
func (s *SplashSystem) calculateProximityAnchor(cursorX, cursorY, charCount int) (int, int) {
	config := s.world.Resources.Config

	splashW := charCount * parameter.SplashCharWidth
	splashH := parameter.SplashCharHeight

	if splashW > config.GameWidth || splashH > config.GameHeight {
		return max(0, cursorX-splashW/2), max(0, cursorY-splashH/2)
	}

	timerBBoxes := s.getTimerBBoxes(0)

	checkTimers := func(absX, absY, w, h int) bool {
		candidate := BBox{X: absX, Y: absY, W: w, H: h}
		return !s.checkBBoxCollisionAny(candidate, timerBBoxes)
	}

	absX, absY, found := s.world.Positions.FindFreeFromPattern(
		cursorX, cursorY,
		splashW, splashH,
		engine.PatternDiagonalFirst,
		parameter.SplashMinDistance,
		parameter.SplashMinDistance*3,
		true,
		component.WallBlockAll,
		checkTimers,
	)

	if found {
		return absX, absY
	}

	anchorX := max(0, min(cursorX-splashW/2, config.GameWidth-splashW))
	anchorY := max(0, min(cursorY-splashH/2, config.GameHeight-splashH))
	return anchorX, anchorY
}

// checkBBoxCollisionAny returns true if candidate collides with any bbox in slice
func (s *SplashSystem) checkBBoxCollisionAny(candidate BBox, boxes []BBox) bool {
	for _, box := range boxes {
		if s.checkBBoxCollision(candidate, box) {
			return true
		}
	}
	return false
}

// calculateTimerOffset finds valid offset for timer relative to anchor entity
// Uses 8-direction search: cardinals first, then diagonals
func (s *SplashSystem) calculateTimerOffset(splashComp *component.SplashComponent, timerBBoxes []BBox) {
	timerWidth := splashComp.Length * parameter.SplashCharWidth
	timerHeight := parameter.SplashCharHeight

	var anchorX, anchorY int
	if splashComp.AnchorEntity != 0 {
		if pos, ok := s.world.Positions.GetPosition(splashComp.AnchorEntity); ok {
			anchorX = pos.X
			anchorY = pos.Y
		}
	} else {
		anchorX = s.world.Resources.Config.GameWidth / 2
		anchorY = s.world.Resources.Config.GameHeight / 2
	}

	checkTimers := func(absX, absY, w, h int) bool {
		candidate := BBox{X: absX, Y: absY, W: w, H: h}
		return !s.checkBBoxCollisionAny(candidate, timerBBoxes)
	}

	exclusion := engine.Exclusion{
		Left:   splashComp.MarginLeft,
		Right:  splashComp.MarginRight,
		Top:    splashComp.MarginTop,
		Bottom: splashComp.MarginBottom,
	}

	offsetX, offsetY, _ := s.world.Positions.FindPlacementAroundExclusion(
		anchorX, anchorY,
		timerWidth, timerHeight,
		exclusion,
		parameter.SplashTimerPadding,
		parameter.SplashTopPadding,
		engine.PatternCardinalFirst,
		component.WallBlockAll,
		checkTimers,
	)

	splashComp.OffsetX = offsetX
	splashComp.OffsetY = offsetY
}

// getTimerBBoxes returns bounding boxes for all SlotTimer splashes
func (s *SplashSystem) getTimerBBoxes(excludeEntity core.Entity) []BBox {
	var boxes []BBox

	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		if splashEntity == excludeEntity {
			continue
		}

		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if !ok || splashComp.Slot != component.SlotTimer {
			continue
		}

		// Resolve position
		x, y := splashComp.AnchorX, splashComp.AnchorY
		if splashComp.AnchorEntity != 0 {
			if pos, ok := s.world.Positions.GetPosition(splashComp.AnchorEntity); ok {
				x = pos.X + splashComp.OffsetX
				y = pos.Y + splashComp.OffsetY
			}
		}

		// Dynamic width for timer
		w := splashComp.Length * parameter.SplashCharWidth
		h := parameter.SplashCharHeight

		// Apply padding for inter-splash collision (Inflated Bounding Box)
		pad := parameter.SplashCollisionPadding
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

// findSplashEntityBySlot returns entity ID of first splash with given slot, or 0
func (s *SplashSystem) findSplashEntityBySlot(slot component.SplashSlot) core.Entity {
	splashEntities := s.world.Components.Splash.GetAllEntities()
	for _, splashEntity := range splashEntities {
		splashComp, ok := s.world.Components.Splash.GetComponent(splashEntity)
		if ok && splashComp.Slot == slot {
			return splashEntity
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
		return component.SplashColorWhite
	}
}