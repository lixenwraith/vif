package renderer

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestEmber256WearsTheHeatBarLeadColour: the solid 256-colour ember takes the
// palette of the heat bar's last filled cell, at every heat and screen width.
func TestEmber256WearsTheHeatBarLeadColour(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 1)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorMode256
	pos := component.PositionComponent{X: 40, Y: 10}
	gameCtx.World.Positions.SetPosition(cursors[0], pos)
	gameCtx.World.Components.Shield.SetComponent(cursors[0], component.ShieldComponent{Type: component.ShieldTypePlayer})
	bar, ember := NewHeatRenderer(gameCtx), NewEmberRenderer(gameCtx)

	for _, width := range []int{80, 97, 203} {
		rc := peerContext(gameCtx)
		rc.ScreenWidth = width
		for heat := 1; heat <= 100; heat++ {
			gameCtx.World.Components.Heat.SetComponent(cursors[0], component.HeatComponent{Current: heat, EmberActive: true})
			buf := render.NewRenderBuffer(terminal.ColorMode256, width, 24)
			bar.Render(rc, buf)
			lead := -1
			for x := range width {
				if buf.CellAt(x, 0).Attrs&terminal.AttrBg256 != 0 {
					lead = x
				}
			}
			if lead < 0 {
				continue
			}
			want := buf.CellAt(lead, 0).Bg.R

			buf.Clear()
			ember.Render(rc, buf)
			if got := buf.CellAt(pos.X+1, pos.Y); got.Attrs&terminal.AttrBg256 == 0 || got.Bg.R != want {
				t.Fatalf("width %d heat %d: ember palette (%d, %v), heat bar lead %d", width, heat, got.Bg.R, got.Attrs, want)
			}
		}
	}
}
