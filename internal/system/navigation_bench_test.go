package system

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// BenchmarkNavigationWallMoves is a tick of navigation on an open map where one
// wall steps a cell, what a few walls changing on a large map cost: the walls are
// re-read where they moved, not over the map
func BenchmarkNavigationWallMoves(b *testing.B) {
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			world := engine.NewWorld()
			engine.NewGameContextWithClock(world, size.w, size.h, engine.NewManualClock())
			cfg := world.Resources.Config
			ready(NewCursorSystem(world)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
				Payload: &event.CursorSpawnRequestPayload{X: 4, Y: cfg.MapHeight / 2, Control: uint8(component.ControlLocal)}})
			world.Resources.Event.Queue.Consume()
			wall := world.CreateEntity(core.DomainShared)
			world.Positions.SetPosition(wall, component.PositionComponent{X: cfg.MapWidth / 2, Y: 0})
			world.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockKinetic})
			nav := ready(NewNavigationSystem(world)).(*NavigationSystem)
			nav.Update()
			y := 0
			b.ReportAllocs()
			for b.Loop() {
				y = (y + 1) % cfg.MapHeight
				world.Positions.Move(wall, component.PositionComponent{X: cfg.MapWidth / 2, Y: y})
				nav.Update()
			}
		})
	}
}
