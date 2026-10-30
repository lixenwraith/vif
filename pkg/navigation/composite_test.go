package navigation

import (
	"github.com/lixenwraith/vif/pkg/position"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// fitsAt is the reference fit: the whole footprint within the grid and on no wall
func fitsAt(p *CompositePassability, walls []bool, x, y int) bool {
	x0, y0 := x-p.HeaderOffX, y-p.HeaderOffY
	if x0 < 0 || y0 < 0 || x0+p.FootprintW > p.Width || y0+p.FootprintH > p.Height {
		return false
	}
	for dy := range p.FootprintH {
		for dx := range p.FootprintW {
			if walls[(y0+dy)*p.Width+x0+dx] {
				return false
			}
		}
	}
	return true
}

// FuzzCompositePassabilityFollowsItsWalls: a computed grid is every header's fit,
// and one moved cell by cell through storms of walls closing and opening stays
// what computing it again gives, naming each header whose fit changed exactly once
func FuzzCompositePassabilityFollowsItsWalls(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		rng := rand.New(rand.NewPCG(seed, seed>>3))
		w, h := 1+rng.IntN(30), 1+rng.IntN(20)
		fw, fh := 1+rng.IntN(6), 1+rng.IntN(4)
		p := NewCompositePassability(w, h, Footprint{Width: fw, Height: fh, HeaderX: rng.IntN(fw), HeaderY: rng.IntN(fh)})
		walls := make([]bool, w*h)
		density := rng.Float64()
		for i := range walls {
			walls[i] = rng.Float64() < density*0.4
		}
		bitsOf := func() position.Bits {
			b := position.NewBits(w, h)
			for i, wall := range walls {
				if wall {
					b.Set(i%w, i/w)
				}
			}
			return b
		}
		valid := func() []bool {
			v := make([]bool, w*h)
			for i := range v {
				v[i] = p.IsValid(i%w, i/w)
			}
			return v
		}
		check := func(stage string) {
			t.Helper()
			for i, v := range valid() {
				if want := fitsAt(p, walls, i%w, i/w); v != want {
					t.Fatalf("%s: header (%d,%d) valid %t, its footprint says %t", stage, i%w, i/w, v, want)
				}
			}
		}
		p.Compute(bitsOf())
		check("computed")

		for storm := range 12 {
			before := valid()
			var closed, opened []vmath.Point
			for i := range walls {
				if rng.IntN(8) != 0 {
					continue
				}
				walls[i] = !walls[i]
				if c := (vmath.Point{X: i % w, Y: i / w}); walls[i] {
					closed = append(closed, c)
				} else {
					opened = append(opened, c)
				}
			}
			flipped := map[int]bool{}
			p.Moved(bitsOf(), closed, opened, func(x, y int, valid bool) {
				i := y*w + x
				if _, twice := flipped[i]; twice || valid == before[i] {
					t.Fatalf("storm %d: header (%d,%d) flipped to %t, again %t or to what it was", storm, x, y, valid, twice)
				}
				flipped[i] = valid
			})
			check("moved")
			for i, v := range valid() {
				if _, ok := flipped[i]; ok != (before[i] != v) {
					t.Fatalf("storm %d: header (%d,%d) went %t -> %t, flip named %t", storm, i%w, i/w, before[i], v, ok)
				}
			}
			again := p.Clone()
			again.Compute(bitsOf())
			if !slices.Equal(again.blocked.Words, p.blocked.Words) {
				t.Fatalf("storm %d: moved bits differ from computed ones", storm)
			}
		}
	})
}
