package renderer

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

func TestConfigMenuKeepsSelectionVisibleAcrossResize(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 100, 40)
	menu := &core.OverlayMenu{}
	for i := range 20 {
		menu.Rows = append(menu.Rows, core.OverlayMenuRow{Key: fmt.Sprint(i), Label: "Setting", Value: "on", Description: "A setting that changes immediately."})
	}
	ctx.SetOverlayContent(&core.OverlayContent{Title: "Configuration", Layout: core.OverlayLayoutMenu, Menu: menu})
	ctx.SetOverlaySelection("19")
	r := NewOverlayRenderer(ctx)
	for _, size := range [][2]int{{100, 40}, {40, 15}, {18, 8}, {100, 40}} {
		ctx.SetPresentationSize(size[0], size[1])
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, size[0], size[1])
		r.Render(render.RenderContext{}, buf)
		g := ctx.OverlayGeometry()
		visible := max(g.ContentH-parameter.OverlayMenuDetailRows, 1)
		offset := ctx.GetOverlayScroll()
		if offset > 19 || offset+visible <= 19 {
			t.Fatalf("size %v hides selected row: offset=%d visible=%d", size, offset, visible)
		}
		cell := buf.CellAt(g.X+g.ContentX, g.Y+g.ContentY+19-offset)
		if cell.Bg != visual.RgbOverlaySelected {
			t.Fatalf("size %v lost selection highlight", size)
		}
	}
}
