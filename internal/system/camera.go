package system

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// CameraSystem updates camera position to follow cursor with dead zone
type CameraSystem struct {
	world *engine.World
}

// NewCameraSystem creates camera following system
func NewCameraSystem(world *engine.World) engine.System {
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
		payload, ok := ev.Payload.(*event.CursorMovedPayload)
		// The camera follows one cursor; a remote or bot move is not a viewport change
		if !ok || !s.world.Resources.Player.IsLocal(payload.Entity) {
			return
		}
		s.updateCamera(payload.X, payload.Y)
	}
}

// updateCamera adjusts camera position based on cursor location.
// The soft-follow itself is ConfigResource.FollowCamera, shared with the resize
// reflow: a resize re-anchors the view through the same code rather than by
// announcing a cursor move it did not make.
func (s *CameraSystem) updateCamera(cursorX, cursorY int) {
	s.world.Resources.Config.FollowCamera(cursorX, cursorY)
}
