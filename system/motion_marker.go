package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MotionMarkerSystem generates inverse markers on first glyph in each cardinal direction
// Interim implementation for visual system validation; designed for expansion to 12+ markers
type MotionMarkerSystem struct {
	world *engine.World

	// Base markers (always visible, first glyph each direction)
	baseMarkers   []core.Entity
	basePositions []core.Point

	// Colored markers (shown after g+direction)
	coloredMarkers []core.Entity

	enabled bool
}

func NewMotionMarkerSystem(world *engine.World) engine.System {
	s := &MotionMarkerSystem{
		world:          world,
		baseMarkers:    make([]core.Entity, 0, 4),
		basePositions:  make([]core.Point, 0, 4),
		coloredMarkers: make([]core.Entity, 0, 12),
	}
	s.Init()
	return s
}

func (s *MotionMarkerSystem) Init() {
	s.clearAllMarkers()
	s.enabled = true
}

func (s *MotionMarkerSystem) Name() string {
	return "motion_marker"
}

func (s *MotionMarkerSystem) Priority() int {
	return parameter.PriorityMotionMarker
}

func (s *MotionMarkerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCursorMoved,
		event.EventModeChangeNotification,
		event.EventMotionMarkerShowColored,
		event.EventMotionMarkerClearColored,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *MotionMarkerSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventCursorMoved:
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok {
			s.regenerateBaseMarkers(payload.X, payload.Y)
		}

	case event.EventModeChangeNotification:
		// Payload is ignored, just regenerate
		cursorEntity := s.world.Resources.Player.Entity
		cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
		if !ok {
			return
		}
		s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)

	case event.EventMotionMarkerShowColored:
		if payload, ok := ev.Payload.(*event.MotionMarkerShowPayload); ok {
			s.showColoredMarkers(payload.DirectionX, payload.DirectionY)
		}

	case event.EventMotionMarkerClearColored:
		s.clearColoredMarkers()
	}
}

func (s *MotionMarkerSystem) Update() {
	if !s.enabled || len(s.baseMarkers) == 0 {
		return
	}
	s.validateBaseMarkers()
}

func (s *MotionMarkerSystem) clearAllMarkers() {
	s.clearBaseMarkers()
	s.clearColoredMarkers()
}

func (s *MotionMarkerSystem) clearBaseMarkers() {
	for _, entity := range s.baseMarkers {
		s.world.Components.Marker.RemoveEntity(entity)
		s.world.DestroyEntity(entity)
	}
	s.baseMarkers = s.baseMarkers[:0]
	s.basePositions = s.basePositions[:0]
}

func (s *MotionMarkerSystem) clearColoredMarkers() {
	for _, entity := range s.coloredMarkers {
		s.world.Components.Marker.RemoveEntity(entity)
		s.world.DestroyEntity(entity)
	}
	s.coloredMarkers = s.coloredMarkers[:0]
}

func (s *MotionMarkerSystem) showColoredMarkers(dx, dy int) {
	// s.clearColoredMarkers()
	//
	// cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	// if !ok {
	// 	return
	// }
	//
	// bounds := s.world.GetPingAbsoluteBounds()
	// config := s.world.Resources.Config
	//
	// dir := [2]int{dx, dy}
	// // maxSteps := s.calcMaxSteps(dir, cursorPos.X, cursorPos.Y, bounds, config)
	// // if maxSteps <= 0 {
	// // 	return
	// // }
	//
	// colors := []component.GlyphType{component.GlyphRed, component.GlyphGreen, component.GlyphBlue}
	//
	// for _, glyphType := range colors {
	// 	targetType := glyphType // Capture for closure
	// 	filter := func(e core.Entity) bool {
	// 		glyph, ok := s.world.Components.Glyph.GetComponent(e)
	// 		return ok && glyph.Type == targetType
	// 	}
	//
	// 	_, x, y := s.world.Positions.ScanLineFirst(
	// 		cursorPos.X+dx, cursorPos.Y+dy,
	// 		dx, dy,
	// 		maxSteps,
	// 		filter,
	// 	)
	//
	// 	if x != -1 {
	// 		s.spawnMarker(x, y, &s.coloredMarkers)
	// 	}
	// }
}

// scanForGlyph finds first glyph entity along direction vector from start position
// Returns nil if bounds reached without finding glyph
func (s *MotionMarkerSystem) scanForGlyph(startX, startY, dx, dy, width, height int) *core.Point {
	x, y := startX+dx, startY+dy

	var buf [parameter.MaxEntitiesPerCell]core.Entity

	for x >= 0 && x < width && y >= 0 && y < height {
		count := s.world.Positions.GetAllEntitiesAtInto(x, y, buf[:])

		for i := 0; i < count; i++ {
			if s.world.Components.Glyph.HasEntity(buf[i]) {
				return &core.Point{X: x, Y: y}
			}
		}

		x += dx
		y += dy
	}

	return nil
}

func (s *MotionMarkerSystem) spawnMarker(x, y int, slice *[]core.Entity) {
	entity := s.world.CreateEntity()

	s.world.Components.Marker.SetComponent(entity, component.MarkerComponent{
		X:         x,
		Y:         y,
		Width:     1,
		Height:    1,
		Shape:     component.MarkerShapeInvert,
		Color:     visual.RgbWhite,
		Intensity: vmath.Scale,
		PulseRate: 0,
		FadeMode:  0,
	})

	*slice = append(*slice, entity)
}

// scanFirstGlyph finds first glyph in direction using appropriate strategy
// Visual mode (bounds.Active): area scan within bounds rectangle
// Normal mode: single line scan to screen edge
// Returns (-1, -1) if no glyph found
func (s *MotionMarkerSystem) scanFirstGlyph(cursorX, cursorY, dx, dy int, bounds engine.PingAbsoluteBounds, filter func(core.Entity) bool) (int, int) {
	if bounds.Active {
		return s.scanBoundsFirst(cursorX, cursorY, dx, dy, bounds, filter)
	}

	// Normal mode: single line scan
	config := s.world.Resources.Config
	maxSteps := max(config.GameWidth, config.GameHeight)
	_, x, y := s.world.Positions.ScanLineFirst(
		cursorX+dx, cursorY+dy,
		dx, dy,
		maxSteps,
		filter,
	)
	return x, y
}

// scanBoundsFirst finds first glyph in direction within bounds using area scan
// Horizontal (dx != 0): iterates columns in direction, scans all rows per column
// Vertical (dy != 0): iterates rows in direction, scans all columns per row
// scanBoundsFirst finds first glyph in direction within bounds using area scan
// Direction axis: unbounded (scans to screen edge)
// Perpendicular axis: bounded (only positions within bounds.Min/Max)
func (s *MotionMarkerSystem) scanBoundsFirst(cursorX, cursorY, dx, dy int, bounds engine.PingAbsoluteBounds, filter func(core.Entity) bool) (int, int) {
	var buf [parameter.MaxEntitiesPerCell]core.Entity
	config := s.world.Resources.Config

	if dx != 0 {
		// Horizontal direction: X unbounded, Y bounded
		startX := cursorX + dx
		var endX int
		if dx > 0 {
			endX = config.GameWidth - 1
		} else {
			endX = 0
		}

		for x := startX; (dx > 0 && x <= endX) || (dx < 0 && x >= endX); x += dx {
			for y := bounds.MinY; y <= bounds.MaxY; y++ {
				count := s.world.Positions.GetAllEntitiesAtInto(x, y, buf[:])
				for i := 0; i < count; i++ {
					if filter(buf[i]) {
						return x, y
					}
				}
			}
		}
	} else if dy != 0 {
		// Vertical direction: Y unbounded, X bounded
		startY := cursorY + dy
		var endY int
		if dy > 0 {
			endY = config.GameHeight - 1
		} else {
			endY = 0
		}

		for y := startY; (dy > 0 && y <= endY) || (dy < 0 && y >= endY); y += dy {
			for x := bounds.MinX; x <= bounds.MaxX; x++ {
				count := s.world.Positions.GetAllEntitiesAtInto(x, y, buf[:])
				for i := 0; i < count; i++ {
					if filter(buf[i]) {
						return x, y
					}
				}
			}
		}
	}

	return -1, -1
}

func (s *MotionMarkerSystem) regenerateBaseMarkers(cursorX, cursorY int) {
	s.clearBaseMarkers()

	bounds := s.world.GetPingAbsoluteBounds()

	glyphFilter := func(e core.Entity) bool {
		return s.world.Components.Glyph.HasEntity(e)
	}

	directions := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	for _, dir := range directions {
		x, y := s.scanFirstGlyph(cursorX, cursorY, dir[0], dir[1], bounds, glyphFilter)

		if x != -1 {
			s.spawnMarker(x, y, &s.baseMarkers)
			s.basePositions = append(s.basePositions, core.Point{X: x, Y: y})
		}
	}
}

// validateBaseMarkers checks if glyphs still exist at marker positions, regenerates if changed
func (s *MotionMarkerSystem) validateBaseMarkers() {
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	if !ok {
		s.clearBaseMarkers()
		return
	}

	needsRegenerate := false
	glyphFilter := func(e core.Entity) bool {
		return s.world.Components.Glyph.HasEntity(e)
	}

	bounds := s.world.GetPingAbsoluteBounds()

	// Check each marker position still has glyph and is within bounds
	for i, pos := range s.basePositions {
		if i >= len(s.baseMarkers) {
			break
		}

		// Check if marker position is within current bounds (visual mode)
		if bounds.Active {
			if pos.X < bounds.MinX || pos.X > bounds.MaxX ||
				pos.Y < bounds.MinY || pos.Y > bounds.MaxY {
				needsRegenerate = true
				break
			}
		}

		var buf [parameter.MaxEntitiesPerCell]core.Entity
		count := s.world.Positions.GetAllEntitiesAtInto(pos.X, pos.Y, buf[:])

		hasGlyph := false
		for j := 0; j < count; j++ {
			if glyphFilter(buf[j]) {
				hasGlyph = true
				break
			}
		}

		if !hasGlyph {
			needsRegenerate = true
			break
		}
	}

	// Check if closer glyph exists in any direction
	if !needsRegenerate {
		directions := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
		for i, dir := range directions {
			if i >= len(s.basePositions) {
				break
			}

			x, y := s.scanFirstGlyph(cursorPos.X, cursorPos.Y, dir[0], dir[1], bounds, glyphFilter)

			if (x != -1 && (x != s.basePositions[i].X || y != s.basePositions[i].Y)) ||
				(x == -1 && i < len(s.basePositions)) {
				needsRegenerate = true
				break
			}
		}
	}

	if needsRegenerate {
		s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)
	}
}