package system

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// BenchmarkMazeSpawn is td's maze laid over the map in box-drawn walls and taken
// down again: every wall's box character read from its neighbours, the cost a
// storm of walls pays
func BenchmarkMazeSpawn(b *testing.B) {
	for _, size := range caveBenchSizes {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			world := engine.NewWorld()
			engine.NewGameContextWithClock(world, size.w, size.h, engine.NewManualClock())
			walls := ready(NewWallSystem(world)).(*WallSystem)
			maze := &event.MazeSpawnRequestPayload{CellWidth: 5, CellHeight: 3, Braiding: 0.5, BlockMask: component.WallBlockAll,
				Visual: component.WallVisualConfig{BoxStyle: component.BoxDrawSingle, RenderFg: true}}
			laid := 0
			b.ReportAllocs()
			for b.Loop() {
				walls.HandleEvent(event.GameEvent{Type: event.EventMazeSpawnRequest, Payload: maze})
				world.Resources.Event.Queue.Consume()
				laid = world.Components.Wall.CountEntities()
				walls.HandleEvent(event.GameEvent{Type: event.EventWallDespawnAll})
				world.Resources.Event.Queue.Consume()
			}
			b.ReportMetric(float64(laid), "walls")
		})
	}
}
