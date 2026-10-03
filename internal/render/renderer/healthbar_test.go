package renderer

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

func TestKrakenHealthBarStaysCenteredAndScalesInBothColorModes(t *testing.T) {
	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		w := engine.NewWorld()
		game := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
		w.Resources.Config.ColorMode = mode
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: 20, Y: 12})
		r := NewHealthBarRenderer(game)
		ctx := render.RenderContext{MapWidth: 40, MapHeight: 24, ViewportWidth: 40, ViewportHeight: 24}
		for _, hp := range []int{parameter.KrakenInitialHP, parameter.KrakenInitialHP / 2, 1} {
			w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntityKraken, HitPoints: hp})
			buf := render.NewRenderBuffer(mode, 40, 24)
			r.Render(ctx, buf)
			want := (hp*visual.KrakenHealthBarWidth + parameter.KrakenInitialHP - 1) / parameter.KrakenInitialHP
			count := 0
			for y := range 24 {
				for x := range 40 {
					cell := buf.CellAt(x, y)
					if cell.Rune != visual.HealthBarChar {
						continue
					}
					count++
					if y != 12 || x < 20-want/2 || x >= 20-want/2+want {
						t.Fatalf("health bar shifted from center: (%d,%d)", x, y)
					}
				}
			}
			if count != want {
				t.Fatalf("health %d: %d bar cells, want %d", hp, count, want)
			}
		}
	}
}
