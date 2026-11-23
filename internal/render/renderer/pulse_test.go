package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/render"
)

// TestAWavePulseDrawsAFlatBandBehindItsEdge: a wave pulse leaves the cells ahead of
// its edge and those its band has left untouched, and draws the band in two tones,
// one for the rim at its edge and one for the wake behind it
func TestAWavePulseDrawsAFlatBandBehindItsEdge(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	// Half grown: its edge stands 30 columns from its center on row 12
	w.Components.Pulse.SetComponent(w.CreateEntity(core.DomainPlayer), component.PulseComponent{
		PulseSpec: component.PulseSpec{Radius: 60, Duration: time.Second, Grow: time.Second, Start: 0.5, Style: component.PulseWave},
		X:         40, Y: 12,
	})
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	NewPulseRenderer(game).Render(render.RenderContext{
		MapWidth: 80, MapHeight: 24, ViewportWidth: 80, ViewportHeight: 24, CellAspect: w.Resources.Config.CellAspect,
	}, buf)

	band := int(waveRim + waveWake)
	var zero terminal.Cell
	if ahead, left := buf.CellAt(71, 12), buf.CellAt(70-band-1, 12); ahead != zero || left != zero {
		t.Fatalf("drew %+v ahead of the edge and %+v behind the band, want neither touched", ahead, left)
	}
	rim, wake := buf.CellAt(70, 12).Bg, buf.CellAt(70-band+1, 12).Bg
	if rim == wake || rim == zero.Bg || wake == zero.Bg {
		t.Fatalf("the rim draws %v and the wake %v, want two tones", rim, wake)
	}
	for x := 70 - band + 1; x <= 70; x++ {
		if bg := buf.CellAt(x, 12).Bg; bg != rim && bg != wake {
			t.Fatalf("column %d of the band draws %v, want the rim's %v or the wake's %v", x, bg, rim, wake)
		}
	}
	if left, right := buf.CellAt(10, 12).Bg, buf.CellAt(70, 12).Bg; left != right {
		t.Fatalf("the rim draws %v left of the center and %v right of it, want one tone", left, right)
	}
}
