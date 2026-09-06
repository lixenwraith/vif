package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestPingLinesStopAtTheMapEdge is the reported defect: on a pane zoomed larger
// than the session's latched map, the crosshair and grid ran the full width and
// height of the terminal, across cells no cursor can ever reach.
//
// The renderer is driven directly rather than through the orchestrator, so this
// asserts the renderer's own bound rather than the compositor clip that backs it.
func TestPingLinesStopAtTheMapEdge(t *testing.T) {
	t.Parallel()

	world := engine.NewWorld()
	gameCtx := engine.NewGameContextWithClock(world, 80, 24, engine.NewManualClock())
	world.SetupLevel(10, 6, false, false)

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
