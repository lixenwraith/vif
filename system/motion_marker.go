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
		event.EventModeChanged,
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

	case event.EventModeChanged:
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
	if !s.enabled {
		return
	}

	// Initial generation only when no markers exist
	if len(s.baseMarkers) == 0 {
		cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
		if ok {
			s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)
		}
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

// regenerateBaseMarkers uses the consolidated engine logic to place markers
func (s *MotionMarkerSystem) regenerateBaseMarkers(cursorX, cursorY int) {
	s.clearBaseMarkers()

	bounds := s.world.GetPingAbsoluteBounds()

	// Filter for glyphs
	glyphStore := s.world.Components.Glyph
	glyphFilter := func(e core.Entity) bool {
		return glyphStore.HasEntity(e)
	}

	// Cardinal directions: Up, Down, Left, Right
	directions := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	for _, dir := range directions {
		// Use the consolidated search logic from engine
		_, x, y, found := s.world.Positions.FindClosestEntityInDirection(cursorX, cursorY, dir[0], dir[1], bounds, glyphFilter)

		if found {
			s.spawnMarker(x, y, &s.baseMarkers)
			s.basePositions = append(s.basePositions, core.Point{X: x, Y: y})
		}
	}
}

// showColoredMarkers displays markers for g-motions using the same logic
func (s *MotionMarkerSystem) showColoredMarkers(dx, dy int) {
	s.clearColoredMarkers()

	cursorPos, ok := s.world.Positions.GetPosition(s.world.Resources.Player.Entity)
	if !ok {
		return
	}

	bounds := s.world.GetPingAbsoluteBounds()
	colors := []component.GlyphType{component.GlyphRed, component.GlyphGreen, component.GlyphBlue}

	for _, glyphType := range colors {
		targetType := glyphType
		filter := func(e core.Entity) bool {
			glyph, ok := s.world.Components.Glyph.GetComponent(e)
			return ok && glyph.Type == targetType
		}

		// Use the consolidated search logic from engine
		_, x, y, found := s.world.Positions.FindClosestEntityInDirection(cursorPos.X, cursorPos.Y, dx, dy, bounds, filter)

		if found {
			s.spawnMarker(x, y, &s.coloredMarkers)
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

	bounds := s.world.GetPingAbsoluteBounds()
	glyphFilter := func(e core.Entity) bool {
		return s.world.Components.Glyph.HasEntity(e)
	}

	directions := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	// Compute expected positions for all 4 directions
	var expectedPositions [4]core.Point
	var expectedFound [4]bool
	expectedCount := 0

	for i, dir := range directions {
		_, x, y, found := s.world.Positions.FindClosestEntityInDirection(cursorPos.X, cursorPos.Y, dir[0], dir[1], bounds, glyphFilter)
		expectedFound[i] = found
		if found {
			expectedPositions[i] = core.Point{X: x, Y: y}
			expectedCount++
		}
	}

	// Quick check: count mismatch
	if len(s.basePositions) != expectedCount {
		s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)
		return
	}

	// Check each expected position exists in current set
	for i, found := range expectedFound {
		if !found {
			continue
		}
		exp := expectedPositions[i]
		exists := false
		for _, pos := range s.basePositions {
			if pos.X == exp.X && pos.Y == exp.Y {
				exists = true
				break
			}
		}
		if !exists {
			s.regenerateBaseMarkers(cursorPos.X, cursorPos.Y)
			return
		}
	}
}