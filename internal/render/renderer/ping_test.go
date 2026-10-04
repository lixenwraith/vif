package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/render"
)

// Ping lines must remain inside the map when the viewport extends beyond it.
func TestPingLinesStopAtTheMapEdge(t *testing.T) {
	t.Parallel()

	world := engine.NewWorld()
	gameCtx := engine.NewGameContextWithClock(world, 80, 24, engine.NewManualClock())
	world.SetupLevel(10, 6, false, false, false)

	cursor := world.CreateEntity(core.DomainShared)
	world.Components.Cursor.SetComponent(cursor, component.CursorComponent{})
	world.Positions.SetPosition(cursor, component.PositionComponent{X: 4, Y: 2})
	world.Components.Ping.SetComponent(cursor, component.PingComponent{
		ShowCrosshair: true,
		GridActive:    true,
	})
	world.Resources.Player.Bind(0, cursor)
	world.Resources.Player.SetLocal(0)

	cfg := world.Resources.Config
	ctx := render.RenderContext{
		GameXOffset: 3, GameYOffset: 1,
		ViewportWidth: 40, ViewportHeight: 20,
		MapOffsetX: (40 - cfg.MapWidth) / 2, MapOffsetY: (20 - cfg.MapHeight) / 2,
		MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
		CursorX: 4, CursorY: 2,
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 60, 30)
	NewPingRenderer(gameCtx).Render(ctx, buf)

	pf := ctx.PlayfieldRect()
	var zero color.RGB
	drawnInside := false
	for y := range 30 {
		for x := range 60 {
			drawn := buf.CellAt(x, y).Bg != zero
			if drawn && !pf.Contains(x, y) {
				t.Fatalf("ping drew outside the map at screen (%d,%d); playfield is %+v", x, y, pf)
			}
			drawnInside = drawnInside || drawn
		}
	}
	if !drawnInside {
		t.Fatal("ping drew nothing inside the map")
	}
}
