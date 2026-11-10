package renderer

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// krakenByDefinition shades the samples one after another as the definition reads: a
// disc covering a cell makes it solid in the sample's color, a rim reaching a cell no
// disc covered yet draws the rim's rune there, and the body's void blends over both.
// In 256 colors a color is a console entry and the void solid past the threshold.
// The dead have lost their legs past the cut and glow behind it, into a closing void.
func krakenByDefinition(of *krakenShade, samples []component.KrakenSample, palette bool) []terminal.Cell {
	k, x, y, minX, minY, maxX, maxY := &of.kraken, of.x, of.y, of.minX, of.minY, of.maxX, of.maxY
	width := maxX - minX + 1
	cells := make([]terminal.Cell, width*(maxY-minY+1))
	background := visual.RgbBackground
	if palette {
		background = color.RGB{R: visual.ConBlack}
	}
	cut, closing, void, body := 1.0, 1.0, visual.RgbKrakenVoid, visual.Kraken256Leg
	if of.dead {
		cut, closing = 1-of.burn/visual.KrakenBurnLegs, min(1, (1-of.burn)/(1-visual.KrakenBurnLegs))
		ember := 1 - cut/visual.KrakenBurnEmber
		void = color.Lerp(void, visual.RgbKrakenAttack, ember)
		if ember >= visual.Effect256Threshold {
			body = visual.Kraken256Heat
		}
	}
	for _, s := range samples {
		if s.Step > cut {
			continue
		}
		heat := s.Step * s.Step * k.MoveBlend * visual.KrakenMoveHeat
		if s.Attacking {
			heat = max(heat, s.Step*s.Step*k.AttackT)
		}
		if of.dead {
			heat = max(heat, 1-(cut-s.Step)/visual.KrakenBurnEmber)
		}
		c := color.Lerp(color.Lerp(visual.RgbKrakenLegBase, visual.RgbKrakenLegTip, s.Step), visual.RgbKrakenAttack, heat)
		if palette {
			c = color.RGB{R: visual.Kraken256Leg}
			if heat >= visual.Effect256Threshold {
				c = color.RGB{R: visual.Kraken256Heat}
			}
		}
		reach := s.Radius + visual.KrakenRimReach + 1
		for cy := max(minY, int(math.Floor(s.Y-reach))); cy <= min(maxY, int(math.Ceil(s.Y+reach))); cy++ {
			for cx := max(minX, int(math.Floor(s.X-reach))); cx <= min(maxX, int(math.Ceil(s.X+reach))); cx++ {
				dx, dy := float64(cx)+vmath.CellCenter-s.X, (float64(cy)+vmath.CellCenter-s.Y)*parameter.KrakenRowSquash
				distance := math.Sqrt(dx*dx + dy*dy)
				cell := &cells[(cy-minY)*width+cx-minX]
				if distance <= s.Radius {
					*cell = terminal.Cell{Rune: ' ', Fg: c, Bg: c}
					continue
				}
				if cell.Rune == 0 && distance-s.Radius < visual.KrakenRimReach {
					*cell = terminal.Cell{Rune: visual.KrakenRimRune, Fg: c, Bg: background}
				}
			}
		}
	}
	rx, ry := parameter.KrakenBodyRadius*parameter.KrakenRowSquash, parameter.KrakenBodyRadius
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			nx, ny := (float64(cx)+vmath.CellCenter-x)/rx, (float64(cy)+vmath.CellCenter-y)/ry
			distance := math.Sqrt(nx*nx + ny*ny)
			if distance > 1 {
				continue
			}
			cell := &cells[(cy-minY)*width+cx-minX]
			if cell.Rune == 0 {
				*cell = terminal.Cell{Rune: ' ', Bg: background}
			}
			alpha := 1 - distance
			alpha *= alpha * (3 - 2*alpha) * closing
			if palette {
				if alpha >= visual.Effect256Threshold {
					leg := color.RGB{R: body}
					*cell = terminal.Cell{Rune: ' ', Fg: leg, Bg: leg}
				}
				continue
			}
			cell.Bg = color.Lerp(cell.Bg, void, alpha)
			if alpha > visual.KrakenVoidOpaque {
				cell.Rune = ' '
			}
		}
	}
	return cells
}

// krakenEdgeSamples centers discs on cell centers and corners, with radii that put a
// cell exactly on a disc's edge or a rim's, where the comparison by squared distance
// hands the decision to the distance itself
func krakenEdgeSamples(rng *rand.Rand, minX, minY, maxX, maxY int) []component.KrakenSample {
	samples := make([]component.KrakenSample, 1+rng.IntN(40))
	for i := range samples {
		dx, dy := float64(rng.IntN(8))*vmath.CellCenter, float64(rng.IntN(5))
		distance := math.Sqrt(dx*dx + dy*dy)
		radius := distance
		if rng.IntN(2) == 0 {
			radius = max(0, distance-visual.KrakenRimReach)
		}
		samples[i] = component.KrakenSample{
			X:      float64(minX+rng.IntN(maxX-minX+1)) + vmath.CellCenter*float64(rng.IntN(2)),
			Y:      float64(minY+rng.IntN(maxY-minY+1)) + vmath.CellCenter*float64(rng.IntN(2)),
			Radius: radius, Step: rng.Float64(), Index: i, Attacking: rng.IntN(2) == 0,
		}
	}
	return samples
}

func TestKrakenShadingMatchesItsDefinition(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 5))
	r := &KrakenRenderer{}
	edges, burnt := 0, 0
	for n := range 1200 {
		k := component.KrakenComponent{
			State: component.KrakenState(rng.IntN(4)), AttackLegs: rng.IntN(2), AttackT: rng.Float64(),
			MoveBlend: rng.Float64(), Angle: rng.Float64() * 2 * math.Pi, Time: rng.Float64() * 1000,
			RotSpeed: rng.Float64()*10 - 5,
		}
		x, y := rng.Float64()*200, rng.Float64()*60
		minX, minY := int(x)-rng.IntN(120), int(y)-rng.IntN(50)
		maxX, maxY := minX+rng.IntN(200), minY+rng.IntN(70)
		of := krakenShade{kraken: k, x: x, y: y, minX: minX, minY: minY, maxX: maxX, maxY: maxY}
		if n%5 >= 3 {
			of.dead, of.burn = true, rng.Float64()
		}
		samples := k.AppendTentacleSamples(nil, x, y, of.burn)
		if n%2 == 1 {
			samples = krakenEdgeSamples(rng, minX, minY, maxX, maxY)
		}
		r.palette = n%3 == 0
		r.shade(&of, slices.Clone(samples))
		want := krakenByDefinition(&of, samples, r.palette)
		width, drawn := maxX-minX+1, 0
		for i := range want {
			if r.cells[i] != want[i] {
				t.Fatalf("shade %d cell (%d,%d): %+v, want %+v", n, minX+i%width, minY+i/width, r.cells[i], want[i])
			}
			if want[i].Rune != 0 {
				drawn++
			}
		}
		for _, c := range r.drawn {
			i := int(c.index)
			if want[i].Rune == 0 || int(c.x) != minX+i%width || int(c.y) != minY+i/width {
				t.Fatalf("shade %d draws %+v, which is no shaded cell of its own", n, c)
			}
		}
		if len(r.drawn) != drawn {
			t.Fatalf("shade %d draws %d cells, want %d", n, len(r.drawn), drawn)
		}
		if n%2 == 1 {
			edges += drawn
		}
		if of.dead {
			burnt += drawn
		}
	}
	if edges == 0 || burnt == 0 {
		t.Fatalf("edge samples shaded %d cells and the dead %d, want some of both", edges, burnt)
	}
}

// TestADeadKrakenDrawsItsLastPoseBurning: a frame draws a burn as its pose shades
// by definition at how far it has burnt, its legs drawn in by as much: as it dies,
// the living's cells with the legs' ends lit. A living Kraken draws over it, and
// nothing is drawn once the burn is over
func TestADeadKrakenDrawsItsLastPoseBurning(t *testing.T) {
	w := engine.NewWorld()
	game := engine.NewGameContextWithClock(w, 150, 44, engine.NewManualClock())
	w.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	r, config, transient := NewKrakenRenderer(game), w.Resources.Config, w.Resources.Transient
	const x, y = 75.5, 22.5
	pose := component.KrakenComponent{AttackLegs: 1, Angle: 0.3, Time: 12.3}
	frame := func() []terminal.Cell {
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, config.MapWidth, config.MapHeight)
		r.Render(render.RenderContext{MapWidth: config.MapWidth, MapHeight: config.MapHeight,
			ViewportWidth: config.MapWidth, ViewportHeight: config.MapHeight}, buf)
		cells := make([]terminal.Cell, 0, config.MapWidth*config.MapHeight)
		for cy := range config.MapHeight {
			for cx := range config.MapWidth {
				cells = append(cells, buf.CellAt(cx, cy))
			}
		}
		return cells
	}
	stand := func() core.Entity {
		e := w.CreateEntity(core.DomainShared)
		w.Components.Kraken.SetComponent(e, pose)
		w.Components.Kinetic.SetComponent(e, component.KineticComponent{Kinetic: physics.Kinetic{PreciseX: x, PreciseY: y}})
		return e
	}

	e := stand()
	alive := frame()
	w.DestroyEntity(e)
	for _, burn := range []float64{0, 0.3, 0.6, 0.9} {
		transient.BurnBacking[0], transient.BurnCount = component.KrakenBurn{Entity: e, Kraken: pose, X: x, Y: y, Burn: burn}, 1
		of := krakenShade{kraken: pose, x: x, y: y, dead: true, burn: burn, maxX: config.MapWidth - 1, maxY: config.MapHeight - 1}
		got := frame()
		if !slices.Equal(got, krakenByDefinition(&of, pose.AppendTentacleSamples(nil, x, y, burn), false)) {
			t.Fatalf("burnt %v, the frame is not its pose's shade at that burn", burn)
		}
		if burn != 0 {
			continue
		}
		lit := false
		for i := range got {
			if (got[i].Rune == 0) != (alive[i].Rune == 0) {
				t.Fatalf("as it died cell %d drew %q where the living drew %q", i, got[i].Rune, alive[i].Rune)
			}
			lit = lit || got[i].Fg.R > alive[i].Fg.R
		}
		if !lit {
			t.Fatal("as it died no leg's end was lit")
		}
	}
	stand()
	if !slices.Equal(frame(), alive) {
		t.Fatal("a burn showed through the living Kraken over it")
	}
	w.DestroyEntity(w.Components.Kraken.Entities()[0])
	transient.Clear()
	if slices.ContainsFunc(frame(), func(c terminal.Cell) bool { return c.Rune != 0 }) {
		t.Fatal("with no burn left a cell was drawn")
	}
}
