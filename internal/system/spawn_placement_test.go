package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// TestASpeciesAskedForAtAMapCornerStandsWholeInsideIt: a spawn request names the
// header's cell blind to the footprint around it, so a species asked for at either
// corner stands every cell of its body inside the map, none past an edge
func TestASpeciesAskedForAtAMapCornerStandsWholeInsideIt(t *testing.T) {
	const width, height = 80, 40
	for _, kind := range []struct {
		name    string
		system  func(*engine.World) engine.System
		request func(x, y int) event.GameEvent
	}{
		{"swarm", NewSwarmSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventSwarmSpawnRequest, Payload: &event.SwarmSpawnRequestPayload{X: x, Y: y}}
		}},
		{"quasar", NewQuasarSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventQuasarSpawnRequest, Payload: &event.QuasarSpawnRequestPayload{X: x, Y: y}}
		}},
		{"eye", NewEyeSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventEyeSpawnRequest, Payload: &event.EyeSpawnRequestPayload{X: x, Y: y, RouteID: -1}}
		}},
		{"snake", NewSnakeSystem, func(x, y int) event.GameEvent {
			return event.GameEvent{Type: event.EventSnakeSpawnRequest, Payload: &event.SnakeSpawnRequestPayload{X: x, Y: y}}
		}},
	} {
		for _, corner := range [][2]int{{0, 0}, {width - 1, height - 1}} {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, width, height, engine.NewManualClock())
			ready(kind.system(w)).(event.Handler).HandleEvent(kind.request(corner[0], corner[1]))
			if w.Positions.CountEntities() == 0 {
				t.Fatalf("%s asked for at %v: nothing stood", kind.name, corner)
			}
			for _, e := range w.Positions.Entities() {
				if pos, _ := w.Positions.GetPosition(e); w.Positions.IsOutOfBounds(pos.X, pos.Y) {
					t.Fatalf("%s asked for at %v: a cell stands at (%d, %d), past the %dx%d map", kind.name, corner, pos.X, pos.Y, width, height)
				}
			}
		}
	}
}
