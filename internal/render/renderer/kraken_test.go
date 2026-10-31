package renderer

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// krakenByDefinition shades the samples one after another as the definition reads: a
// disc covering a cell makes it solid in the sample's color, a rim reaching a cell no
// disc covered yet draws the rim's rune there, and the body's void blends over both.
func krakenByDefinition(k *component.KrakenComponent, samples []component.KrakenSample, x, y float64, minX, minY, maxX, maxY int) []terminal.Cell {
	width := maxX - minX + 1
	cells := make([]terminal.Cell, width*(maxY-minY+1))
	for _, s := range samples {
		heat := s.Step * s.Step * k.MoveBlend * visual.KrakenMoveHeat
		if s.Attacking {
			heat = max(heat, s.Step*s.Step*k.AttackT)
		}
		c := color.Lerp(color.Lerp(visual.RgbKrakenLegBase, visual.RgbKrakenLegTip, s.Step), visual.RgbKrakenAttack, heat)
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
					*cell = terminal.Cell{Rune: visual.KrakenRimRune, Fg: c, Bg: visual.RgbBackground}
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
				*cell = terminal.Cell{Rune: ' ', Bg: visual.RgbBackground}
			}
			alpha := 1 - distance
			alpha *= alpha * (3 - 2*alpha)
			cell.Bg = color.Lerp(cell.Bg, visual.RgbKrakenVoid, alpha)
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
	edges := 0
	for n := range 1200 {
		k := component.KrakenComponent{
			State: component.KrakenState(rng.IntN(4)), AttackLegs: rng.IntN(2), AttackT: rng.Float64(),
			MoveBlend: rng.Float64(), Angle: rng.Float64() * 2 * math.Pi, Time: rng.Float64() * 1000,
			RotSpeed: rng.Float64()*10 - 5,
		}
		x, y := rng.Float64()*200, rng.Float64()*60
		minX, minY := int(x)-rng.IntN(120), int(y)-rng.IntN(50)
		maxX, maxY := minX+rng.IntN(200), minY+rng.IntN(70)
		samples := k.AppendTentacleSamples(nil, x, y)
		if n%2 == 1 {
			samples = krakenEdgeSamples(rng, minX, minY, maxX, maxY)
		}
		r.palette = n%3 == 0
		r.shade(&k, samples, x, y, minX, minY, maxX, maxY)
		want := krakenByDefinition(&k, samples, x, y, minX, minY, maxX, maxY)
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
			if r.palette && (c.fg != color.RGBTo256(want[i].Fg) || c.bg != color.RGBTo256(want[i].Bg)) {
				t.Fatalf("shade %d cell %d palette (%d,%d), want (%d,%d)", n, i, c.fg, c.bg, color.RGBTo256(want[i].Fg), color.RGBTo256(want[i].Bg))
			}
		}
		if len(r.drawn) != drawn {
			t.Fatalf("shade %d draws %d cells, want %d", n, len(r.drawn), drawn)
		}
		if n%2 == 1 {
			edges += drawn
		}
	}
	if edges == 0 {
		t.Fatal("no edge sample shaded a cell")
	}
}
