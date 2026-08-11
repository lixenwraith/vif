package vmath

import "testing"

const traceCap = 8192

func randFloat(rng *FastRand, cells int) float64 {
	return float64(rng.Intn(cells)) + rng.Float64()
}

func collectIterF(x1, y1, x2, y2 float64) [][2]int {
	out := make([][2]int, 0, 64)
	tr := NewGridTraverserF(x1, y1, x2, y2)
	for tr.Next() {
		x, y := tr.Pos()
		out = append(out, [2]int{x, y})
		if len(out) >= traceCap {
			break
		}
	}
	return out
}

// assertPath validates supercover invariants: correct endpoints, no gaps,
// no repeats, and bounded length
func assertPath(t *testing.T, path [][2]int, sx, sy, tx, ty int) {
	t.Helper()
	if len(path) == 0 {
		t.Fatal("empty path")
	}
	if len(path) >= traceCap {
		t.Fatalf("path did not terminate within %d cells", traceCap)
	}
	if path[0] != [2]int{sx, sy} {
		t.Fatalf("first cell %v, want (%d,%d)", path[0], sx, sy)
	}
	last := path[len(path)-1]
	if last != [2]int{tx, ty} {
		t.Fatalf("last cell %v, want (%d,%d)", last, tx, ty)
	}
	for i := 1; i < len(path); i++ {
		dx := path[i][0] - path[i-1][0]
		dy := path[i][1] - path[i-1][1]
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
			t.Fatalf("gap between %v and %v", path[i-1], path[i])
		}
		if dx == 0 && dy == 0 {
			t.Fatalf("repeated cell at index %d: %v", i, path[i])
		}
	}
}

func TestTraverseSameCell(t *testing.T) {
	x, y := 3.25, 7.75
	if p := collectIterF(x, y, x+0.1, y+0.1); len(p) != 1 || p[0] != [2]int{3, 7} {
		t.Fatalf("sub-cell iterator = %v, want a single cell", p)
	}
}

func TestTraverseAxisAligned(t *testing.T) {
	sx, sy := 2.5, 4.5
	p := collectIterF(sx, sy, 7.5, sy)
	assertPath(t, p, 2, 4, 7, 4)
	if len(p) != 6 {
		t.Fatalf("horizontal run length %d, want 6", len(p))
	}
	for _, c := range p {
		if c[1] != 4 {
			t.Fatalf("horizontal run left its row: %v", c)
		}
	}
}

func TestTraverseNegativeCoordinates(t *testing.T) {
	p := collectIterF(-3.5, -2.5, 2.5, 1.5)
	assertPath(t, p, -4, -3, 2, 1)
}

func TestGridTraverserFInvariants(t *testing.T) {
	rng := NewFastRand(0x6218)
	for range 5000 {
		x1, y1 := randFloat(rng, 48), randFloat(rng, 48)
		x2, y2 := randFloat(rng, 48), randFloat(rng, 48)
		p := collectIterF(x1, y1, x2, y2)
		start := PointAtF(x1, y1)
		target := PointAtF(x2, y2)
		assertPath(t, p, start.X, start.Y, target.X, target.Y)
	}
}

func TestFloatGridConversionsUseCellCentersAndFloor(t *testing.T) {
	for _, p := range []Point{{X: 0, Y: 0}, {X: 3, Y: -4}, {X: -9, Y: 9}} {
		x, y := CenteredFromGridF(p.X, p.Y)
		wantX, wantY := p.CenterF()
		if x != wantX || y != wantY {
			t.Errorf("CenteredFromGridF(%v) = (%v,%v), want (%v,%v)", p, x, y, wantX, wantY)
		}
		gx, gy := GridFromCenteredF(x, y)
		if gx != p.X || gy != p.Y {
			t.Errorf("GridFromCenteredF(%v.CenterF()) = (%d,%d)", p, gx, gy)
		}
	}

	if x, y := GridFromCenteredF(-0.25, -1.75); x != -1 || y != -2 {
		t.Errorf("GridFromCenteredF negative = (%d,%d), want (-1,-2)", x, y)
	}
}

func TestCalculateCentroid(t *testing.T) {
	if x, y := CalculateCentroid([]int{0, 0, 4, 8}); x != 2 || y != 4 {
		t.Errorf("centroid = (%d,%d), want (2,4)", x, y)
	}
	if x, y := CalculateCentroid(nil); x != 0 || y != 0 {
		t.Error("nil centroid must be origin")
	}
	if x, y := CalculateCentroid([]int{1, 2, 3}); x != 0 || y != 0 {
		t.Error("odd-length centroid must be origin")
	}
}

func TestCalculateCentroidFMatchesInt(t *testing.T) {
	x, y := CalculateCentroidF([]float64{0, 0, 4, 8})
	if x != 2 || y != 4 {
		t.Errorf("centroidF = (%v,%v), want (2,4)", x, y)
	}
}
