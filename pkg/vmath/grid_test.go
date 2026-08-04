package vmath

import "testing"

const traceCap = 8192

func randFixed(rng *FastRand, cells int) int64 {
	return FromInt(rng.Intn(cells)) + int64(rng.Next()&uint64(Mask))
}

func collectTraverse(x1, y1, x2, y2 int64) [][2]int {
	out := make([][2]int, 0, 64)
	Traverse(x1, y1, x2, y2, func(x, y int) bool {
		out = append(out, [2]int{x, y})
		return len(out) < traceCap
	})
	return out
}

func collectIter(x1, y1, x2, y2 int64) [][2]int {
	out := make([][2]int, 0, 64)
	tr := NewGridTraverser(x1, y1, x2, y2)
	for tr.Next() {
		x, y := tr.Pos()
		out = append(out, [2]int{x, y})
		if len(out) >= traceCap {
			break
		}
	}
	return out
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

func TestTraverseMatchesIterator(t *testing.T) {
	rng := NewFastRand(0x6217)
	for range 5000 {
		x1, y1 := randFixed(rng, 48), randFixed(rng, 48)
		x2, y2 := randFixed(rng, 48), randFixed(rng, 48)

		cb := collectTraverse(x1, y1, x2, y2)
		it := collectIter(x1, y1, x2, y2)

		if len(cb) != len(it) {
			t.Fatalf("length mismatch %d vs %d for (%d,%d)->(%d,%d)", len(cb), len(it), x1, y1, x2, y2)
		}
		for i := range cb {
			if cb[i] != it[i] {
				t.Fatalf("cell %d mismatch %v vs %v", i, cb[i], it[i])
			}
		}
		assertPath(t, it, ToInt(x1), ToInt(y1), ToInt(x2), ToInt(y2))
	}
}

func TestTraverseSameCell(t *testing.T) {
	x, y := FromFloat(3.25), FromFloat(7.75)
	if p := collectTraverse(x, y, x+1, y+1); len(p) != 1 || p[0] != [2]int{3, 7} {
		t.Fatalf("sub-cell traverse = %v, want a single cell", p)
	}
	if p := collectIter(x, y, x+1, y+1); len(p) != 1 || p[0] != [2]int{3, 7} {
		t.Fatalf("sub-cell iterator = %v, want a single cell", p)
	}
}

func TestTraverseAxisAligned(t *testing.T) {
	sx, sy := FromFloat(2.5), FromFloat(4.5)
	p := collectIter(sx, sy, FromFloat(7.5), sy)
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
	p := collectIter(FromFloat(-3.5), FromFloat(-2.5), FromFloat(2.5), FromFloat(1.5))
	assertPath(t, p, -4, -3, 2, 1)
}

func TestGridTraverserFInvariants(t *testing.T) {
	rng := NewFastRand(0x6218)
	for range 5000 {
		x1, y1 := randFixed(rng, 48), randFixed(rng, 48)
		x2, y2 := randFixed(rng, 48), randFixed(rng, 48)
		p := collectIterF(ToFloat(x1), ToFloat(y1), ToFloat(x2), ToFloat(y2))
		assertPath(t, p, ToInt(x1), ToInt(y1), ToInt(x2), ToInt(y2))
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
