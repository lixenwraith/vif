package renderer

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
)

type shieldScene struct {
	name    string
	mode    terminal.ColorMode
	w, h    int
	players int
	boost   bool
	loot    int
	resize  bool // the cursor's radius differs every frame, so every frame builds its table
}

var shieldScenes = []shieldScene{
	{"solo", terminal.ColorModeTrueColor, 40, 20, 1, false, 0, false},
	{"solo-boost", terminal.ColorModeTrueColor, 40, 20, 1, true, 0, false},
	{"solo-256", terminal.ColorMode256, 40, 20, 1, false, 0, false},
	{"solo-resize", terminal.ColorModeTrueColor, 40, 20, 1, false, 0, true},
	{"peers16", terminal.ColorModeTrueColor, 200, 60, 16, false, 0, false},
	{"loot32", terminal.ColorModeTrueColor, 200, 60, 1, false, 32, false},
}

func buildShieldScene(tb testing.TB, s shieldScene) (*ShieldRenderer, render.RenderContext, *render.RenderBuffer) {
	tb.Helper()
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, s.w, s.h, engine.NewManualClock())
	w.Resources.Config.ColorMode = s.mode
	w.Resources.Config.MapWidth, w.Resources.Config.MapHeight = s.w, s.h
	w.Resources.Config.ViewportWidth, w.Resources.Config.ViewportHeight = s.w, s.h
	for slot := range s.players {
		e := w.CreateEntity(core.DomainShared)
		w.Components.Cursor.SetComponent(e, component.CursorComponent{Slot: uint8(slot)})
		x, y := s.w/2, s.h/2
		if s.players > 1 {
			x, y = 12+(slot%8)*24, 12+(slot/8)*30
		}
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		w.Resources.Player.Bind(uint8(slot), e)
		energy := int64(1000)
		if slot%2 == 1 {
			energy = -1000
		}
		w.Components.Energy.SetComponent(e, component.EnergyComponent{Current: energy})
		w.Components.Shield.SetComponent(e, component.ShieldComponent{Type: component.ShieldTypePlayer, Active: true,
			Radius: parameter.PlayerShieldRadius})
		if s.boost {
			w.Components.Boost.SetComponent(e, component.BoostComponent{Active: true})
		}
	}
	w.Resources.Player.SetLocal(0)
	for i := range s.loot {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: 5 + (i%16)*12, Y: 40 + (i/16)*8})
		w.Components.Loot.SetComponent(e, component.LootComponent{Look: component.LootLook{Rune: '*',
			Color: color.RGB{R: 255, G: 255, B: 255}, Glow: color.RGB{R: 60, G: 60, B: 200}}})
		w.Components.Shield.SetComponent(e, component.ShieldComponent{Type: component.ShieldTypeLoot, Active: true,
			Radius: parameter.LootShieldRadius})
	}
	rc := render.RenderContext{GameTime: engine.SimEpoch.Add(time.Second), MapWidth: s.w, MapHeight: s.h,
		ViewportWidth: s.w, ViewportHeight: s.h, ScreenWidth: s.w, ScreenHeight: s.h, CellAspect: w.Resources.Config.CellAspect}
	return NewShieldRenderer(game), rc, render.NewRenderBuffer(s.mode, s.w, s.h)
}

// BenchmarkShieldRender paints a frame of shields on a cleared buffer: one cursor's
// alone, with its boost glow, in 256 colours and growing every frame, sixteen
// cursors', and a field of drops
func BenchmarkShieldRender(b *testing.B) {
	for _, s := range shieldScenes {
		b.Run(s.name, func(b *testing.B) {
			r, rc, buf := buildShieldScene(b, s)
			shield, _ := r.gameCtx.World.Components.Shield.GetPtr(r.gameCtx.World.Resources.Player.Entity)
			b.ReportAllocs()
			frame := 0
			for b.Loop() {
				if s.resize {
					frame++
					shield.Radius = parameter.PlayerShieldRadius * (0.5 + float64(frame%1000)/1000)
				}
				buf.Clear()
				r.Render(rc, buf)
			}
		})
		b.Run(fmt.Sprintf("%s-clear", s.name), func(b *testing.B) {
			_, _, buf := buildShieldScene(b, s)
			for b.Loop() {
				buf.Clear()
			}
		})
	}
}
