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

// Init has no camera state to reset. ConfigResource owns camera initialization
// and reset alongside map and viewport geometry.
func (s *CameraSystem) Init() {}

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
	return []event.EventType{event.EventCursorMoved, event.EventCursorLocalChanged}
}

// HandleEvent re-anchors the view after an announced placement or a rebind. The
// anchor is the cell the local cursor occupies, never the announced one: while a
// prediction is outstanding that cell is a playout lead behind what the renderer
// draws, and an older queued placement would walk the camera back through cells
// the player has already left (D-18).
func (s *CameraSystem) HandleEvent(ev event.GameEvent) {
	// The camera follows one cursor; a remote or bot move is not a viewport change
	if ev.Type == event.EventCursorMoved {
		p, ok := ev.Payload.(*event.CursorMovedPayload)
		if !ok || !s.world.Resources.Player.IsLocal(p.Entity) {
			return
		}
	}
	s.world.FollowLocalCursor()
}
