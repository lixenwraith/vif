package renderer

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// untouchedBg stands in for a cell no layer wrote, which BackgroundAt reports as given
var untouchedBg = color.RGB{R: 1, G: 2, B: 3}

// TestTheLocalFieldIsALens: every cell the local cursor's field lights starts from
// black, as a lone field always did, and a peer's field crossing it adds its light
// there; past the lens, and while the local ember burns, a peer's lights the theme
func TestTheLocalFieldIsALens(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 2)
	w := gameCtx.World
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	rc := peerContext(gameCtx)
	for i, pos := range []component.PositionComponent{{X: 30, Y: 10}, {X: 40, Y: 10}} {
		w.Positions.SetPosition(cursors[i], pos)
		w.Components.Energy.SetComponent(cursors[i], component.EnergyComponent{Current: 1000})
		w.Components.Shield.SetComponent(cursors[i], component.ShieldComponent{
			Type: component.ShieldTypePlayer, Active: true, Radius: parameter.PlayerShieldRadius,
		})
	}
	look := &visual.ShieldLooks[component.ShieldTypePlayer]
	peerAlpha := func(share float64) float64 { return share * (look.MaxOpacity * visual.PeerFieldBlend) }
	r := NewShieldRenderer(gameCtx)
	frame := func() *render.RenderBuffer {
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
		r.Render(rc, buf)
		return buf
	}

	// Halfway to both fields' edges, then eight tenths out from the peer's centre
	buf := frame()
	both := color.Screen(color.Screen(visual.RgbBlack, look.Color, peerAlpha(0.5)), look.Color, 0.5*(look.MaxOpacity*1))
	if got := buf.CellAt(35, 10).Bg; got != both {
		t.Fatalf("crossing fields = %v, want %v", got, both)
	}
	if got, want := buf.CellAt(48, 10).Bg, color.Screen(visual.RgbBackground, look.Color, peerAlpha(0.8)); got != want {
		t.Fatalf("peer field past the lens = %v, want %v", got, want)
	}

	// The ember takes the local field's place, and its lens with it
	w.Components.Heat.SetComponent(cursors[0], component.HeatComponent{Current: 100, EmberActive: true})
	if got, want := frame().CellAt(35, 10).Bg, color.Screen(visual.RgbBackground, look.Color, peerAlpha(0.5)); got != want {
		t.Fatalf("peer field under the ember = %v, want %v", got, want)
	}
}

// TestAShieldDrawsAtItsOwnRadiusAndColour: a shield reaches its radius in columns
// and the rows the cell aspect makes of it, follows a radius it changes to, and wears
// its own colour over its kind's, a zero colour leaving the kind's
func TestAShieldDrawsAtItsOwnRadiusAndColour(t *testing.T) {
	t.Parallel()
	const cx, cy = 40, 12
	tint := color.RGB{R: 200, G: 40, B: 40}
	look := &visual.ShieldLooks[component.ShieldTypeLoot]

	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		gameCtx, _ := peerWorld(t, 1)
		w := gameCtx.World
		w.Resources.Config.ColorMode = mode
		rc := peerContext(gameCtx)
		drop := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(drop, component.PositionComponent{X: cx, Y: cy})
		w.Components.Shield.SetComponent(drop, component.ShieldComponent{
			Type: component.ShieldTypeLoot, Active: true, Radius: 4, Color: tint,
		})
		shield, _ := w.Components.Shield.GetPtr(drop)
		r := NewShieldRenderer(gameCtx)
		frame := func() *render.RenderBuffer {
			buf := render.NewRenderBuffer(mode, 80, 24)
			r.Render(rc, buf)
			return buf
		}
		lit := func(buf *render.RenderBuffer, dx, dy int) bool {
			if mode == terminal.ColorMode256 {
				return buf.CellAt(cx+dx, cy+dy).Attrs&terminal.AttrBg256 != 0
			}
			return buf.BackgroundAt(cx+dx, cy+dy, untouchedBg) != untouchedBg
		}

		// Radius 4 is two rows at the reference aspect: (0, 2) is on the edge, which the
		// 256-colour rim, flat on top, meets a row lower
		top := 2
		if mode == terminal.ColorMode256 {
			top = 1
		}
		small := frame()
		for _, c := range []struct {
			dx, dy int
			want   bool
		}{{4, 0, true}, {6, 0, false}, {0, top, true}, {0, top + 1, false}} {
			if lit(small, c.dx, c.dy) != c.want {
				t.Errorf("mode %v radius 4: cell (%d, %d) lit = %v", mode, c.dx, c.dy, !c.want)
			}
		}
		shield.Radius = 6
		large := frame()
		for _, c := range [][2]int{{6, 0}, {0, top + 1}} {
			if !lit(large, c[0], c[1]) {
				t.Errorf("mode %v radius 6: cell %v unlit", mode, c)
			}
		}

		// Three columns out of four, and the rim's cell on the edge
		for _, c := range []struct {
			tint            color.RGB
			wantRGB         color.RGB
			wantPalette     uint8
			paletteDX, rgbX int
		}{
			{tint, tint, color.RGBTo256(tint), 4, 3},
			{color.RGB{}, look.Color, look.Palette256, 4, 3},
		} {
			shield.Radius, shield.Color = 4, c.tint
			cell := frame()
			if mode == terminal.ColorMode256 {
				if got := cell.CellAt(cx+c.paletteDX, cy); got.Attrs&terminal.AttrBg256 == 0 || got.Bg.R != c.wantPalette {
					t.Errorf("colour %v: rim palette = (%d, %v), want %d", c.tint, got.Bg.R, got.Attrs, c.wantPalette)
				}
				continue
			}
			want := color.Screen(visual.RgbBackground, c.wantRGB, 0.75*(look.MaxOpacity*1))
			if got := cell.CellAt(cx+c.rgbX, cy).Bg; got != want {
				t.Errorf("colour %v: field = %v, want %v", c.tint, got, want)
			}
		}
	}
}

// TestAShieldWithNoReachDrawsNothing: a radius that is zero, negative or not a number
// draws no cell, and one past any map's size paints in bounds, never off the map
func TestAShieldWithNoReachDrawsNothing(t *testing.T) {
	t.Parallel()
	const w, h, mapW, mapH = 80, 24, 40, 12
	rc := render.RenderContext{ViewportWidth: w, ViewportHeight: h, MapWidth: mapW, MapHeight: mapH}
	look := &visual.ShieldLooks[component.ShieldTypePlayer]
	style := ShieldStyle{Look: look, BlendScale: 1, Color: look.Color, Palette256: look.Palette256}

	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		p := NewShieldPainter(mode, vmath.RefAspect)
		for _, radius := range []float64{0, -3, math.NaN(), math.Inf(-1), 1e9, 1e300, math.Inf(1)} {
			buf := render.NewRenderBuffer(mode, w, h)
			p.Paint(buf, rc, mapW/2, mapH/2, radius, style)
			for y := range h {
				for x := range w {
					onMap := x < mapW && y < mapH
					if touched := buf.BackgroundAt(x, y, untouchedBg) != untouchedBg; touched && (!(radius > 0) || !onMap) {
						t.Fatalf("mode %v radius %v: drew cell (%d, %d)", mode, radius, x, y)
					}
				}
			}
		}
	}
}

// TestShieldGeometryFollowsEveryRadiusAndMap: a painter reusing its tables through a
// frame paints what a new painter per field paints, over more radii than it keeps,
// centres on and near the map's edges, maps of changing size, and a lens whose table
// a frame's other radii push out
func TestShieldGeometryFollowsEveryRadiusAndMap(t *testing.T) {
	t.Parallel()
	const w, h = 80, 30
	rng := rand.New(rand.NewPCG(0x5111e1d, 0x6e0))
	radii := make([]float64, 3*visual.ShieldGeometryCacheMax)
	for i := range radii {
		radii[i] = 0.5 + 0.25*float64(rng.IntN(120))
	}
	look := &visual.ShieldLooks[component.ShieldTypePlayer]
	style := ShieldStyle{Look: look, BlendScale: visual.PeerFieldBlend, Color: look.Color, Palette256: look.Palette256}

	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		for _, aspect := range []vmath.Aspect{vmath.RefAspect, 1} {
			kept := NewShieldPainter(mode, aspect)
			for frame := range 100 {
				rc := render.RenderContext{ViewportWidth: w, ViewportHeight: h,
					MapWidth: 20 + rng.IntN(w-19), MapHeight: 8 + rng.IntN(h-7)}
				lens, lensX, lensY := radii[rng.IntN(len(radii))], rng.IntN(rc.MapWidth), rng.IntN(rc.MapHeight)
				got, want := render.NewRenderBuffer(mode, w, h), render.NewRenderBuffer(mode, w, h)
				kept.SetLens(rc, lens, lensX, lensY)
				for range 1 + rng.IntN(2*visual.ShieldGeometryCacheMax) {
					radius, cx, cy := radii[rng.IntN(len(radii))], rng.IntN(rc.MapWidth), rng.IntN(rc.MapHeight)
					kept.Paint(got, rc, cx, cy, radius, style)
					fresh := NewShieldPainter(mode, aspect)
					fresh.SetLens(rc, lens, lensX, lensY)
					fresh.Paint(want, rc, cx, cy, radius, style)
				}
				for y := range h {
					for x := range w {
						if got.CellAt(x, y) != want.CellAt(x, y) ||
							got.BackgroundAt(x, y, untouchedBg) != want.BackgroundAt(x, y, untouchedBg) {
							t.Fatalf("mode %v aspect %v frame %d on %dx%d: cell (%d, %d) differs from new painters'",
								mode, aspect, frame, rc.MapWidth, rc.MapHeight, x, y)
						}
					}
				}
			}
		}
	}
}
