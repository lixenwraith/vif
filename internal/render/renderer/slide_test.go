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

// TestASlidePulseRaisesALitSwellBehindItsEdge: a slide pulse leaves the cells ahead
// of its edge and those its band has left as they were, and draws the band in half
// cells, brighter on the slope turned to the lamp than on the one turned from it,
// with a glyph under it shown nearer the edge than it stands
func TestASlidePulseRaisesALitSwellBehindItsEdge(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	// Half grown: its edge stands 30 columns from its center on row 12
	w.Components.Pulse.SetComponent(w.CreateEntity(core.DomainPlayer), component.PulseComponent{
		PulseSpec: component.PulseSpec{Radius: 60, Duration: time.Second, Grow: time.Second, Start: 0.5, Style: component.PulseSlide},
		X:         40, Y: 12,
	})
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	buf.SetWithBg(60, 12, 'x', color.White, visual.RgbBackground)
	NewSlideRenderer(game).Render(render.RenderContext{
		MapWidth: 80, MapHeight: 24, ViewportWidth: 80, ViewportHeight: 24, CellAspect: w.Resources.Config.CellAspect,
	}, buf)

	if ahead, left := buf.CellAt(71, 12), buf.CellAt(40+30-int(visual.SlideBand)-1, 12); ahead.Rune != 0 || left.Rune != 0 {
		t.Fatalf("drew %q ahead of the edge and %q behind the band, want neither touched", ahead.Rune, left.Rune)
	}
	// The lamp stands up and to the left, so the leading slope faces it on the left
	toward, away := buf.CellAt(40-24, 12), buf.CellAt(40+24, 12)
	if toward.Rune != '▀' || away.Rune != '▀' || color.Luma(toward.Fg) <= color.Luma(away.Fg) {
		t.Fatalf("the leading slope draws %q in %v toward the lamp and %q in %v away from it, want half cells, the first the brighter",
			toward.Rune, toward.Fg, away.Rune, away.Fg)
	}
	bent := 0
	for x := 61; x < 70; x++ {
		if buf.CellAt(x, 12).Rune == 'x' {
			bent++
		}
	}
	if buf.CellAt(60, 12).Rune == 'x' || bent == 0 {
		t.Fatalf("the glyph under the swell shows in its own cell or in none of the %d toward the edge, want it bent that way", bent)
	}
}
