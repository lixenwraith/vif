package renderer

import (
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestAnOrbIsDrawnWhereItStrikes: an orb turning round the outer orbit is drawn at
// the cell its strike lands from, the tick's, however far the frame's clock, which a
// live run starts at its launch and a new game does not rewind, stands from it
func TestAnOrbIsDrawnWhereItStrikes(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 140, 40, engine.NewManualClock())
	cfg := w.Resources.Config
	cfg.ColorMode = terminal.ColorModeTrueColor
	sq := component.SquareComponent{Digits: [component.SquareBlocks]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9},
		Width: parameter.SquareBlockWidth, Height: 1}
	width, height := sq.Size()
	sq.X, sq.Y = (cfg.MapWidth-width)/2, (cfg.MapHeight-height)/2
	w.Components.Square.SetComponent(w.CreateEntity(core.DomainShared), sq)

	// A tick a second in, its frame read off a clock half the orbit's turn out
	pace := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(parameter.SquareOrbIdlePeriod / 2)
	w.Resources.Time.Update(engine.SimTime(20, parameter.GameUpdateInterval), pace, parameter.GameUpdateInterval)
	w.Resources.Time.PaceTime = pace
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, cfg.MapWidth, cfg.MapHeight)
	NewSquareRenderer(game).Render(render.RenderContext{GameTime: pace, MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
		ViewportWidth: cfg.MapWidth, ViewportHeight: cfg.MapHeight, CellAspect: cfg.CellAspect}, buf)
	at := sq.OrbOf(component.SquareAttack, w.Resources.Time.GameElapsed(), cfg.CellAspect, cfg.MapWidth, cfg.MapHeight)
	for y := range cfg.MapHeight {
		for x := range cfg.MapWidth {
			if buf.CellAt(x, y).Rune == visual.CircleBlock && (x != at.X || y != at.Y) {
				t.Fatalf("the orb is drawn at (%d,%d), not at (%d,%d) where it strikes from", x, y, at.X, at.Y)
			}
		}
	}
	if buf.CellAt(at.X, at.Y).Rune != visual.CircleBlock {
		t.Fatalf("no orb is drawn at (%d,%d), where it strikes from", at.X, at.Y)
	}
}
