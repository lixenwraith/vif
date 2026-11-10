package system

import (
	"strconv"
	"time"

	"github.com/lixenwraith/color"
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

	// boxes is the padded box of every splash that holds space, a countdown's or a
	// text's, filled once a layout pass and moved with each countdown placed, so a
	// placement sees the ones placed before it
	boxes []splashBox

	toggle
}

// splashBox is a splash's padded box and the splash it belongs to
type splashBox struct {
	entity core.Entity
	BBox
}

// digitInk is the pixel box each face's digits ink inside their cells, the union over
// 0-9 so a countdown holds still as its count changes. Placement reserves the ink,
// not the cells, whose blank rows and columns would keep the count off rows it fits in.
var digitInk = func() (ink [len(asset.SplashFaces)]BBox) {
	for i := range asset.SplashFaces {
		face := &asset.SplashFaces[i]
		left, top, right, bottom := face.Width, face.Height, 0, 0
		for digit := '0'; digit <= '9'; digit++ {
			glyph, _ := face.Glyph(digit)
			x, y, w, h := face.InkBounds(glyph)
			left, top = min(left, x), min(top, y)
			right, bottom = max(right, x+w), max(bottom, y+h)
		}
		ink[i] = BBox{X: left, Y: top, W: right - left, H: bottom - top}
	}
	return ink
}()

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
		event.EventSplashTextRequest,
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
			s.retire(func(c *component.SplashComponent) bool { return c.Slot == component.SlotMagnifier })
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
			s.retire(func(c *component.SplashComponent) bool {
				return c.Slot == component.SlotTimer && c.AnchorEntity == payload.AnchorEntity
			})
		}

	case event.EventSplashTextRequest:
		if payload, ok := ev.Payload.(*event.SplashTextPayload); ok {
			s.handleTextRequest(payload)
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

	s.fillBoxes()
	next := 0 // the next placed splash's own box; boxes follow the store's order

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

		case component.SlotText:
			// A text stands where it was written and leaves on its timer alone
			if next < len(s.boxes) && s.boxes[next].entity == splashEntity {
				next++
			}
			continue

		case component.SlotMagnifier:
			// Validate magnifier - re-query entity under cursor
			if !s.validateMagnifier(splashEntity, &splashComp) {
				continue // Entity was destroyed
			}

			// A countdown or a text placed over the magnifier moves the magnifier
			magW, magH := asset.SplashFaces[component.SplashLarge].Cells(1)
			magBBox := BBox{X: splashComp.AnchorX, Y: splashComp.AnchorY, W: splashComp.Length * magW, H: magH}
			if s.collides(magBBox, -1) {
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
		Font:         payload.Font,
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

	// The new countdown is not in the store yet, so no box is its own. A large
	// countdown with no clear slot takes the small face, which reads where the large
	// cannot stand, and keeps it: a face chosen each pass would flicker as the room
	// around a moving anchor changes.
	s.fillBoxes()
	if !s.calculateTimerOffset(&splashComp, -1) && splashComp.Font == component.SplashLarge {
		splashComp.Font = component.SplashSmall
		s.calculateTimerOffset(&splashComp, -1)
	}

	splashEntity := s.world.CreateEntity(core.DomainPlayer)
	s.world.Components.Splash.SetComponent(splashEntity, splashComp)
	s.world.Components.Protection.SetComponent(splashEntity, component.ProtectionComponent{
		Mask: component.ProtectFromSpecies,
	})
	// A countdown already at zero still ends on the first pass instead of leaving it idle
	armTimer(s.world, splashEntity, component.TimerLifetime, max(payload.Duration, time.Nanosecond))
}

// setTimerContent writes the seconds left as the countdown's digits. Positive
// fractional seconds round up so "1" remains visible until expiry.
func setTimerContent(splash *component.SplashComponent, remaining time.Duration) {
	seconds := int64(remaining / time.Second)
	if remaining > 0 && remaining%time.Second != 0 {
		seconds++
	}

	digits := strconv.FormatInt(max(seconds, 0), 10)
	splash.Length = min(len(digits), len(splash.Content))
	for i := range splash.Length {
		splash.Content[i] = rune(digits[i])
	}
}

// handleTextRequest writes text at its origin cell in place of the text standing
// there, or clears that text for an empty one
func (s *SplashSystem) handleTextRequest(payload *event.SplashTextPayload) {
	existing := s.find(func(c *component.SplashComponent) bool {
		return c.Slot == component.SlotText && c.AnchorX == payload.OriginX && c.AnchorY == payload.OriginY
	})
	text := []rune(payload.Text)
	if len(text) == 0 {
		// Gone at once: a text written at the cell in the same pass is a new one
		if existing != 0 {
			s.world.DestroyEntity(existing)
		}
		return
	}

	splashComp := component.SplashComponent{
		Color:   payload.Color,
		Font:    payload.Font,
		AnchorX: payload.OriginX,
		AnchorY: payload.OriginY,
		Slot:    component.SlotText,
	}
	if splashComp.Color == (color.RGB{}) {
		splashComp.Color = visual.RgbSplashWhite
	}
	splashComp.Length = copy(splashComp.Content[:], text)

	splashEntity := existing
	if splashEntity == 0 {
		splashEntity = s.world.CreateEntity(core.DomainPlayer)
		s.world.Components.Protection.SetComponent(splashEntity, component.ProtectionComponent{
			Mask: component.ProtectFromSpecies,
		})
	}
	s.world.Components.Splash.SetComponent(splashEntity, splashComp)
	if payload.Duration > 0 {
		armTimer(s.world, splashEntity, component.TimerLifetime, payload.Duration)
	} else if timer, ok := s.world.Components.Timer.GetPtr(splashEntity); ok {
		timer.Slots[component.TimerLifetime] = component.Countdown{}
	}
}

// find is the first splash matching keep, 0 for none
func (s *SplashSystem) find(match func(*component.SplashComponent) bool) core.Entity {
	for _, splashEntity := range s.world.Components.Splash.Entities() {
		if splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity); ok && match(splashComp) {
			return splashEntity
		}
	}
	return 0
}

// retire ends every splash matching match
func (s *SplashSystem) retire(match func(*component.SplashComponent) bool) {
	for _, splashEntity := range s.world.Components.Splash.Entities() {
		if splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity); ok && match(splashComp) {
			event.EmitDeath(s.world.Resources.Event.Queue, 0, splashEntity)
		}
	}
}

// cleanupTimers removes the countdowns of a composite anchor, or without one those
// of the origin cell
func (s *SplashSystem) cleanupTimers(anchor core.Entity, x, y int) {
	s.retire(func(c *component.SplashComponent) bool {
		return c.Slot == component.SlotTimer && c.AnchorEntity == anchor &&
			(anchor != 0 || c.AnchorX == x && c.AnchorY == y)
	})
}

// handleCursorMoved updates the magnifier splash based on cursor position
func (s *SplashSystem) handleCursorMoved(payload *event.CursorMovedPayload) {
	cursorX, cursorY := payload.X, payload.Y

	entity, ok := s.world.Positions.TopGlyphAt(cursorX, cursorY)
	if !ok {
		// No glyph entity - clear magnifier
		s.retire(func(c *component.SplashComponent) bool { return c.Slot == component.SlotMagnifier })
		return
	}

	// Get the character to display
	glyphComp, ok := s.world.Components.Glyph.GetComponent(entity)
	if !ok {
		s.retire(func(c *component.SplashComponent) bool { return c.Slot == component.SlotMagnifier })
		return
	}

	// Resolve color from glyph type
	color := visual.GlyphColorLUT[glyphComp.Type][component.GlyphNormal]

	// Calculate proximity anchor, at least SplashMinDistance from the cursor
	s.fillBoxes()
	anchorX, anchorY := s.calculateProximityAnchor(cursorX, cursorY, 1)

	// Check for existing magnifier to update in place
	existingSplashEntity := s.find(func(c *component.SplashComponent) bool { return c.Slot == component.SlotMagnifier })
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
// the boxes its caller filled
func (s *SplashSystem) calculateProximityAnchor(cursorX, cursorY, charCount int) (int, int) {
	config := s.world.Resources.Config

	splashW, splashH := asset.SplashFaces[component.SplashLarge].Cells(1)
	splashW *= charCount

	if splashW > config.MapWidth || splashH > config.MapHeight {
		return max(0, cursorX-splashW/2), max(0, cursorY-splashH/2)
	}

	clear := func(topLeft position.Point, w, h int) bool {
		return !s.collides(BBox{X: topLeft.X, Y: topLeft.Y, W: w, H: h}, -1)
	}

	topLeft, found := s.world.Positions.FindFreeFromPattern(
		position.Point{X: cursorX, Y: cursorY},
		splashW, splashH,
		position.PatternDiagonalFirst,
		parameter.SplashMinDistance,
		parameter.SplashMinDistance*3,
		true,
		component.WallBlockAll,
		clear,
	)

	if found {
		return topLeft.X, topLeft.Y
	}

	anchorX := max(0, min(cursorX-splashW/2, config.MapWidth-splashW))
	anchorY := max(0, min(cursorY-splashH/2, config.MapHeight-splashH))
	return anchorX, anchorY
}

// collides reports whether candidate overlaps any held box but the one at skip
func (s *SplashSystem) collides(candidate BBox, skip int) bool {
	for i := range s.boxes {
		if i != skip && s.checkBBoxCollision(candidate, s.boxes[i].BBox) {
			return true
		}
	}
	return false
}

// pack is the pixel rows a cell holds for font on this host
func (s *SplashSystem) pack(font component.SplashFont) int {
	return asset.SplashFaces[font].Packed(s.world.Resources.Config.TextConsole)
}

// timerInkBox is the cells a countdown of length digits in font inks, from its first
// cell's origin: the digits' pixel box, each pixel row pair one cell where the host
// packs them
func (s *SplashSystem) timerInkBox(font component.SplashFont, length int) BBox {
	ink, pack := digitInk[font], s.pack(font)
	top, bottom := ink.Y/pack, (ink.Y+ink.H+pack-1)/pack
	return BBox{X: ink.X, Y: top, W: (max(length, 1)-1)*asset.SplashFaces[font].Width + ink.W, H: bottom - top}
}

// inkBox is the cells a splash inks from its origin: a countdown's digits, a text's
// whole glyph cells
func (s *SplashSystem) inkBox(c *component.SplashComponent) BBox {
	if c.Slot == component.SlotTimer {
		return s.timerInkBox(c.Font, c.Length)
	}
	w, h := asset.SplashFaces[c.Font].Cells(s.pack(c.Font))
	return BBox{W: w * c.Length, H: h}
}

// calculateTimerOffset places the countdown's ink around its anchor, clear of every
// held box but its own at self, cardinals first and then diagonals, and reports
// whether a clear slot was found; where none is, the slot overlapping the least
func (s *SplashSystem) calculateTimerOffset(splashComp *component.SplashComponent, self int) bool {
	ink := s.timerInkBox(splashComp.Font, splashComp.Length)

	anchorX, anchorY := splashComp.AnchorX, splashComp.AnchorY
	if splashComp.AnchorEntity != 0 {
		anchorX, anchorY = 0, 0
		if pos, ok := s.world.Positions.GetPosition(splashComp.AnchorEntity); ok {
			anchorX = pos.X
			anchorY = pos.Y
		}
	}

	clear := func(topLeft position.Point, w, h int) bool {
		return !s.collides(BBox{X: topLeft.X, Y: topLeft.Y, W: w, H: h}, self)
	}

	exclusion := position.Exclusion{
		Left:   splashComp.MarginLeft,
		Right:  splashComp.MarginRight,
		Top:    splashComp.MarginTop,
		Bottom: splashComp.MarginBottom,
	}

	offsetX, offsetY, found := s.world.Positions.FindPlacementAroundExclusion(
		position.Point{X: anchorX, Y: anchorY},
		ink.W, ink.H,
		exclusion,
		parameter.SplashTimerPadding,
		position.PatternCardinalFirst,
		component.WallBlockAll,
		clear,
	)

	// The search placed the ink; the cells start that far up and left of it
	splashComp.OffsetX = offsetX - ink.X
	splashComp.OffsetY = offsetY - ink.Y
	return found
}

// fillBoxes rebuilds the box of every countdown and text that holds space, in store order
func (s *SplashSystem) fillBoxes() {
	s.boxes = s.boxes[:0]
	for _, splashEntity := range s.world.Components.Splash.Entities() {
		splashComp, ok := s.world.Components.Splash.GetPtr(splashEntity)
		if !ok || splashComp.Slot == component.SlotMagnifier {
			continue
		}
		if box, ok := s.paddedBox(splashComp); ok {
			s.boxes = append(s.boxes, splashBox{entity: splashEntity, BBox: box})
		}
	}
}

// paddedBox is the space a countdown or a text holds against the other splashes; a
// countdown whose anchor is gone is leaving and holds none
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
	ink := s.inkBox(splashComp)
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
