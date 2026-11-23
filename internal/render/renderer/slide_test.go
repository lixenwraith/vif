package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestASlidePulseTurnsTheCellsBehindItsEdge: a slide pulse leaves the cells ahead of
// its edge and those its band has left as they were, and draws each cell in the band
// a tile part-turned: lit and sinking to the cell's foot before edge-on, its shaded
// underside coming down from the cell's head after
func TestASlidePulseTurnsTheCellsBehindItsEdge(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	// Half grown: its edge stands 40 columns right of its center on row 12
	w.Components.Pulse.SetComponent(w.CreateEntity(core.DomainPlayer), component.PulseComponent{
		PulseSpec: component.PulseSpec{Radius: 80, Duration: time.Second, Grow: time.Second, Start: 0.5, Style: component.PulseSlide},
		X:         0, Y: 12,
	})
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	NewSlideRenderer(game).Render(render.RenderContext{
		MapWidth: 80, MapHeight: 24, ViewportWidth: 80, ViewportHeight: 24, CellAspect: w.Resources.Config.CellAspect,
	}, buf)

	if ahead, left := buf.CellAt(41, 12), buf.CellAt(40-int(visual.SlideBand)-1, 12); ahead.Rune != 0 || left.Rune != 0 {
		t.Fatalf("drew %q ahead of the edge and %q behind the band, want neither touched", ahead.Rune, left.Rune)
	}
	// Four columns behind the edge the tile shows five eighths, ten behind as much of its underside
	tilting, falling := buf.CellAt(36, 12), buf.CellAt(30, 12)
	if tilting.Rune != '▅' || tilting.Bg != visual.RgbSlideGap || color.Luma(tilting.Fg) <= color.Luma(visual.RgbBackground) {
		t.Fatalf("a tilting tile draws %q in %v over %v, want five eighths lit over the gap", tilting.Rune, tilting.Fg, tilting.Bg)
	}
	if falling.Rune != '▃' || falling.Fg != visual.RgbSlideGap || color.Luma(falling.Bg) >= color.Luma(visual.RgbBackground) {
		t.Fatalf("a tile past edge-on draws %q in %v over %v, want the gap under five eighths of a shaded face", falling.Rune, falling.Fg, falling.Bg)
	}
}
