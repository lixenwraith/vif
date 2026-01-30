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

// validateBaseMarkers checks if glyphs still exist at marker positions, regenerates if changed
func (s *MotionMarkerSystem) validateBaseMarkers() {
	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Cursor.Entity)
	if !ok {
		s.clearBaseMarkers()
		return
	}

	needsRegenerate := false
	glyphFilter := func(e core.Entity) bool {
		return s.world.Components.Glyph.HasEntity(e)
	}

	// Check each marker position still has glyph
	for i, pos := range s.basePositions {
		if i >= len(s.baseMarkers) {
			break
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

	// Check if closer glyph exists
	if !needsRegenerate {
		bounds := s.world.Resources.Game.State // Need bounds from context
		directions := s.getBoundAwareDirections(cursorPos.X, cursorPos.Y)
		config := s.world.Resources.Config

		for i, dir := range directions {
			if i >= len(s.basePositions) {
				break
			}

			_, x, y := s.world.Positions.ScanLineFirst(
				cursorPos.X+dir[0], cursorPos.Y+dir[1],
				dir[0], dir[1],
				max(config.GameWidth, config.GameHeight),
				glyphFilter,
			)

			if x != -1 && (x != s.basePositions[i].X || y != s.basePositions[i].Y) {
				needsRegenerate = true
				break
			}
		}
	}

	if needsRegenerate {
		s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)
	}
}

func (s *MotionMarkerSystem) regenerateBaseMarkers(cursorX, cursorY int) {
	s.clearBaseMarkers()

	config := s.world.Resources.Config
	glyphFilter := func(e core.Entity) bool {
		return s.world.Components.Glyph.HasEntity(e)
	}

	directions := s.getBoundAwareDirections(cursorX, cursorY)
	maxSteps := max(config.GameWidth, config.GameHeight)

	for _, dir := range directions {
		_, x, y := s.world.Positions.ScanLineFirst(
			cursorX+dir[0], cursorY+dir[1],
			dir[0], dir[1],
			maxSteps,
			glyphFilter,
		)

		if x != -1 {
			s.spawnMarker(x, y, &s.baseMarkers)
			s.basePositions = append(s.basePositions, core.Point{X: x, Y: y})
		}
	}
}

func (s *MotionMarkerSystem) getBoundAwareDirections(cursorX, cursorY int) [][2]int {
	// For now, return cardinal directions
	// TODO: When bounds are active, could expand to diagonal or multi-row
	return [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
}

func (s *MotionMarkerSystem) showColoredMarkers(dx, dy int) {
	s.clearColoredMarkers()

	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Cursor.Entity)
	if !ok {
		return
	}

	config := s.world.Resources.Config
	maxSteps := max(config.GameWidth, config.GameHeight)

	colors := []component.GlyphType{component.GlyphRed, component.GlyphGreen, component.GlyphBlue}

	for _, glyphType := range colors {
		filter := func(e core.Entity) bool {
			glyph, ok := s.world.Components.Glyph.GetComponent(e)
			return ok && glyph.Type == glyphType
		}

		_, x, y := s.world.Positions.ScanLineFirst(
			cursorPos.X+dx, cursorPos.Y+dy,
			dx, dy,
			maxSteps,
			filter,
		)

		if x != -1 {
			s.spawnMarker(x, y, &s.coloredMarkers)
		}
	}
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