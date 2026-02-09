package system

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// CameraSystem updates camera position to follow cursor with dead zone
type CameraSystem struct {
	world *engine.World
}

// NewCameraSystem creates camera following system
func NewCameraSystem(world *engine.World) *CameraSystem {
	s := &CameraSystem{
		world: world,
	}

	s.Init()
	return s
}

func (s *CameraSystem) Init() {
	// TODO: duplicated with game context, to be refactored
	// Reset camera to origin on init/reset
	config := s.world.Resources.Config
	config.CameraX = 0
	config.CameraY = 0
}

func (s *CameraSystem) Name() string {
	return "camera"
}

func (s *CameraSystem) Priority() int {
	return parameter.PriorityCamera // Run early, before rendering-related systems
}

func (s *CameraSystem) Update() {
	// No-op: camera updates via event handler
}

// EventTypes returns events this system handles
func (s *CameraSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventCursorMoved}
}

// HandleEvent processes cursor movement for camera updates
func (s *CameraSystem) HandleEvent(ev event.GameEvent) {
	if !parameter.CameraEnabled {
		return
	}

	switch ev.Type {
	case event.EventCursorMoved:
		if payload, ok := ev.Payload.(*event.CursorMovedPayload); ok {
			s.updateCamera(payload.X, payload.Y)
		}
	}
}

// updateCamera adjusts camera position based on cursor location
// Uses soft-follow: camera shifts minimally to keep cursor within dead zone
func (s *CameraSystem) updateCamera(cursorX, cursorY int) {
	config := s.world.Resources.Config

	// Skip if map fits within viewport (no scrolling needed)
	if config.MapWidth <= config.ViewportWidth && config.MapHeight <= config.ViewportHeight {
		config.CameraX = 0
		config.CameraY = 0
		return
	}

	// Current cursor position in viewport space
	cursorVX := cursorX - config.CameraX
	cursorVY := cursorY - config.CameraY

	// Dead zone boundaries (viewport-relative)
	marginX := parameter.CameraDeadZoneMarginX
	marginY := parameter.CameraDeadZoneMarginY

	// Clamp margins to half viewport to ensure dead zone exists
	if marginX > config.ViewportWidth/2 {
		marginX = config.ViewportWidth / 2
	}
	if marginY > config.ViewportHeight/2 {
		marginY = config.ViewportHeight / 2
	}

	deadZoneLeft := marginX
	deadZoneRight := config.ViewportWidth - marginX - 1
	deadZoneTop := marginY
	deadZoneBottom := config.ViewportHeight - marginY - 1

	// Calculate camera shift needed to bring cursor into dead zone
	var shiftX, shiftY int

	// Horizontal
	if config.MapWidth > config.ViewportWidth {
		if cursorVX < deadZoneLeft {
			// Cursor in left margin: shift camera left (decrease CameraX)
			shiftX = cursorVX - deadZoneLeft
		} else if cursorVX > deadZoneRight {
			// Cursor in right margin: shift camera right (increase CameraX)
			shiftX = cursorVX - deadZoneRight
		}
	}

	// Vertical
	if config.MapHeight > config.ViewportHeight {
		if cursorVY < deadZoneTop {
			// Cursor in top margin: shift camera up (decrease CameraY)
			shiftY = cursorVY - deadZoneTop
		} else if cursorVY > deadZoneBottom {
			// Cursor in bottom margin: shift camera down (increase CameraY)
			shiftY = cursorVY - deadZoneBottom
		}
	}

	// Apply shift
	if shiftX != 0 || shiftY != 0 {
		newCameraX := config.CameraX + shiftX
		newCameraY := config.CameraY + shiftY

		// Clamp to valid range
		maxCameraX := config.MapWidth - config.ViewportWidth
		maxCameraY := config.MapHeight - config.ViewportHeight

		if newCameraX < 0 {
			newCameraX = 0
		} else if newCameraX > maxCameraX {
			newCameraX = maxCameraX
		}

		if newCameraY < 0 {
			newCameraY = 0
		} else if newCameraY > maxCameraY {
			newCameraY = maxCameraY
		}

		config.CameraX = newCameraX
		config.CameraY = newCameraY
	}
}

// CenterCameraOn positions camera to center on given map coordinates
// Used for teleport, level transitions, or explicit centering
func (s *CameraSystem) CenterCameraOn(mapX, mapY int) {
	config := s.world.Resources.Config

	// Skip if no scrolling needed
	if config.MapWidth <= config.ViewportWidth && config.MapHeight <= config.ViewportHeight {
		config.CameraX = 0
		config.CameraY = 0
		return
	}

	// Target camera position to center cursor
	targetCameraX := mapX - config.ViewportWidth/2
	targetCameraY := mapY - config.ViewportHeight/2

	// Clamp to valid range
	maxCameraX := config.MapWidth - config.ViewportWidth
	maxCameraY := config.MapHeight - config.ViewportHeight

	if maxCameraX < 0 {
		maxCameraX = 0
	}
	if maxCameraY < 0 {
		maxCameraY = 0
	}

	if targetCameraX < 0 {
		targetCameraX = 0
	} else if targetCameraX > maxCameraX {
		targetCameraX = maxCameraX
	}

	if targetCameraY < 0 {
		targetCameraY = 0
	} else if targetCameraY > maxCameraY {
		targetCameraY = maxCameraY
	}

	config.CameraX = targetCameraX
	config.CameraY = targetCameraY
}

// ResetCamera sets camera to origin
func (s *CameraSystem) ResetCamera() {
	config := s.world.Resources.Config
	config.CameraX = 0
	config.CameraY = 0
}