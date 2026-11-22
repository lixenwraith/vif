package renderer

import (
	"math"
	"slices"
	"testing"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestAHitLightsAGutWhereItLanded: a gut's flash tints its cells within the flash's
// reach of the member struck, the core most, and leaves the rest of the gut and
// the flesh as they stood; a console lights the core alone
func TestAHitLightsAGutWhereItLanded(t *testing.T) {
	for _, mode := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256} {
		w := engine.NewWorld()
		game := engine.NewGameContextWithClock(w, 120, 44, engine.NewManualClock())
		cfg := w.Resources.Config
		cfg.ColorMode = mode
		width, height := cfg.MapWidth, cfg.MapHeight
		devourer, gut, struck := w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared)
		d := component.DevourerComponent{Elapsed: parameter.DevourerCreep, Seed: 7}
		d.Guts[0] = gut
		w.Components.Devourer.SetComponent(devourer, d)
		// Straight across the middle, as a gut forms
		w.Components.DevourerGut.SetComponent(gut, component.DevourerGutComponent{Devourer: devourer, From: math.Pi, To: 0, Formed: d.Frame()})
		hitX, row := width/2-12, height/2
		w.Positions.SetPosition(struck, component.PositionComponent{X: hitX, Y: row})
		w.Components.Combat.SetComponent(gut, component.CombatComponent{HitPoints: 1, HitEntity: struck})

		r := NewDevourerRenderer(game)
		ctx := render.RenderContext{MapWidth: width, MapHeight: height, ViewportWidth: width, ViewportHeight: height}
		calm, lit := render.NewRenderBuffer(mode, width, height), render.NewRenderBuffer(mode, width, height)
		r.Render(ctx, calm)
		combat, _ := w.Components.Combat.GetPtr(gut)
		combat.RemainingHitFlash = parameter.CombatHitFlashDuration / 2
		r.Render(ctx, lit)

		tint := hitFlashColor(combat.RemainingHitFlash)
		for y := range height {
			for x := range width {
				was, is := calm.CellAt(x, y), lit.CellAt(x, y)
				dist := math.Hypot(float64(x-hitX), 2*float64(y-row))
				switch onGut := y == row && x > width/4 && x < 3*width/4; {
				case onGut && dist <= visual.DevourerGutHitCore && mode == terminal.ColorMode256:
					if is.Rune != visual.Devourer256HitRune || is.Fg.R != visual.Devourer256Hit {
						t.Fatalf("mode %d: the core's cell %d,%d shows %q in %d", mode, x, y, is.Rune, is.Fg.R)
					}
				case onGut && dist <= visual.DevourerGutHitCore:
					if near, far := int(is.Bg.B)-int(tint.B), int(was.Bg.B)-int(tint.B); is == was || near*near > far*far {
						t.Fatalf("mode %d: the core's cell %d,%d went from %v to %v, no nearer %v", mode, x, y, was.Bg, is.Bg, tint)
					}
				case dist >= visual.DevourerGutHitReach:
					if is != was {
						t.Fatalf("mode %d: cell %d,%d, %.1f from the hit, changed from %+v to %+v", mode, x, y, dist, was, is)
					}
				}
			}
		}
	}
}

// TestACellHoldsTillItsLookMovesPastTheHold: from one frame of the body to the next a
// truecolor cell keeps the cell drawn there while each of its quadrants stays within
// DevourerHold of the new fit, and shows the new fit once one moves further
func TestACellHoldsTillItsLookMovesPastTheHold(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 120, 44, engine.NewManualClock())
	cfg := w.Resources.Config
	cfg.ColorMode = terminal.ColorModeTrueColor
	width, height := cfg.MapWidth, cfg.MapHeight
	devourer := w.CreateEntity(core.DomainShared)
	w.Components.Devourer.SetComponent(devourer, component.DevourerComponent{Elapsed: parameter.DevourerCreep, Seed: 7})
	ctx := render.RenderContext{MapWidth: width, MapHeight: height, ViewportWidth: width, ViewportHeight: height}
	r := NewDevourerRenderer(game)
	was := render.NewRenderBuffer(terminal.ColorModeTrueColor, width, height)
	r.Render(ctx, was)
	d, _ := w.Components.Devourer.GetPtr(devourer)
	d.Elapsed += parameter.DevourerFrame
	is, fit := render.NewRenderBuffer(terminal.ColorModeTrueColor, width, height), render.NewRenderBuffer(terminal.ColorModeTrueColor, width, height)
	r.Render(ctx, is)
	NewDevourerRenderer(game).Render(ctx, fit)

	// How far apart two cells look: the largest CIELAB distance between their quadrants
	apart := func(a, b terminal.Cell) float64 {
		qa, qb := slices.Index(ascimage.QuadrantChars[:], a.Rune), slices.Index(ascimage.QuadrantChars[:], b.Rune)
		far := 0.0
		for q := range 4 {
			ca, cb := a.Bg, b.Bg
			if qa>>q&1 == 1 {
				ca = a.Fg
			}
			if qb>>q&1 == 1 {
				cb = b.Fg
			}
			la, lb := render.Lab(ca), render.Lab(cb)
			far = max(far, math.Sqrt((la[0]-lb[0])*(la[0]-lb[0])+(la[1]-lb[1])*(la[1]-lb[1])+(la[2]-lb[2])*(la[2]-lb[2])))
		}
		return far
	}
	held, moved := 0, 0
	for y := range height {
		for x := range width {
			a, b, f := was.CellAt(x, y), is.CellAt(x, y), fit.CellAt(x, y)
			if f.Rune == 0 {
				continue
			}
			far := math.Inf(1)
			if a.Rune != 0 {
				far = apart(a, f)
			}
			switch {
			case math.Abs(far-visual.DevourerHold) < 1e-3: // float32 and float64 may part at the hold
			case far < visual.DevourerHold && b != a:
				t.Fatalf("cell %d,%d, %.2f from its fit, shows %+v, not %+v held", x, y, far, b, a)
			case far >= visual.DevourerHold && b != f:
				t.Fatalf("cell %d,%d, %.2f from its fit, shows %+v, not the fit %+v", x, y, far, b, f)
			case far < visual.DevourerHold && a != f:
				held++
			case far >= visual.DevourerHold:
				moved++
			}
		}
	}
	if held == 0 || moved == 0 {
		t.Fatalf("%d cells held and %d moved: a frame both holds and redraws", held, moved)
	}
}
