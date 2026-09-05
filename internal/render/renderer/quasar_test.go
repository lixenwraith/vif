package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

func TestQuasarZapRangeIsClippedToMapBounds(t *testing.T) {
	ctx := render.RenderContext{
		ViewportWidth:  20,
		ViewportHeight: 12,
		MapOffsetX:     5,
		MapOffsetY:     3,
		MapWidth:       10,
		MapHeight:      6,
	}
	minX, minY, _ := ctx.MapToScreen(0, 0)
	maxX, maxY, _ := ctx.MapToScreen(ctx.MapWidth-1, ctx.MapHeight-1)
	tests := []struct {
		name string
		x    int
		y    int
	}{
		{name: "left", x: 0, y: 3},
		{name: "right", x: ctx.MapWidth - 1, y: 3},
		{name: "top", x: 5, y: 0},
		{name: "bottom", x: 5, y: ctx.MapHeight - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 20, 12)
			(&QuasarRenderer{}).renderZapRange(ctx, buf, tt.x, tt.y, &component.QuasarComponent{ZapRadius: 5})

			var zero color.RGB
			drawnInside := false
			for y := range ctx.ViewportHeight {
				for x := range ctx.ViewportWidth {
					drawn := buf.CellAt(x, y).Bg != zero
					insideMap := x >= minX && x <= maxX && y >= minY && y <= maxY
					if drawn && !insideMap {
						t.Fatalf("zap range drew outside map at screen (%d,%d)", x, y)
					}
					drawnInside = drawnInside || drawn
				}
			}
			if !drawnInside {
				t.Fatal("zap range did not draw its visible in-map arc")
			}
		})
	}
}

func TestQuasarZapRangeRespectsCameraViewport(t *testing.T) {
	ctx := render.RenderContext{
		GameXOffset:    2,
		GameYOffset:    1,
		ViewportWidth:  10,
		ViewportHeight: 6,
		CameraX:        6,
		CameraY:        4,
		MapWidth:       20,
		MapHeight:      14,
	}
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 14, 8)
	(&QuasarRenderer{}).renderZapRange(ctx, buf, ctx.CameraX, ctx.CameraY+3, &component.QuasarComponent{ZapRadius: 5})

	var zero color.RGB
	drawnInside := false
	for y := range 8 {
		for x := range 14 {
			drawn := buf.CellAt(x, y).Bg != zero
			insideViewport := x >= ctx.GameXOffset && x < ctx.GameXOffset+ctx.ViewportWidth &&
				y >= ctx.GameYOffset && y < ctx.GameYOffset+ctx.ViewportHeight
			if drawn && !insideViewport {
				t.Fatalf("zap range drew outside camera viewport at screen (%d,%d)", x, y)
			}
			drawnInside = drawnInside || drawn
		}
	}
	if !drawnInside {
		t.Fatal("zap range did not draw its visible camera-cropped arc")
	}
}
