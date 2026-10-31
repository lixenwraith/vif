package vmath

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// lineRef is the hand-rolled loop Line reproduces, appending the cells from (x0, y0)
// through (x1, y1) to dst
func lineRef(dst []Point, x0, y0, x1, y1 int) []Point {
	dx := x1 - x0
	dy := y1 - y0
	absDx, absDy := IntAbs(dx), IntAbs(dy)

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	err := absDx - absDy
	x, y := x0, y0

	for {
		dst = append(dst, Point{X: x, Y: y})
		if x == x1 && y == y1 {
			return dst
		}

		e2 := 2 * err
		if e2 > -absDy {
			err -= absDy
			x += stepX
		}
		if e2 < absDx {
			err += absDx
			y += stepY
		}
	}
}

// lineCells appends the cells a Line visits to dst, in lineRef's shape
func lineCells(dst []Point, x0, y0, x1, y1 int) []Point {
	for l := NewLine(x0, y0, x1, y1); ; l = l.Next() {
		dst = append(dst, Point{X: l.X(), Y: l.Y()})
		if l.AtEnd() {
			return dst
		}
	}
}

// lineSegments returns {x0, y0, x1, y1} segments in every octant at random lengths,
// each octant with its axis and diagonal edges and a zero length, then random ones,
// some far from the origin
func lineSegments(rng *FastRand) [][4]int {
	var segs [][4]int
	for octant := range 8 {
		for range 200 {
			major := 1 + rng.Intn(64)
			for _, minor := range [3]int{0, rng.Intn(major + 1), major} {
				dx, dy := major, minor
				if octant&1 != 0 {
					dx, dy = minor, major
				}
				if octant&2 != 0 {
					dx = -dx
				}
				if octant&4 != 0 {
					dy = -dy
				}
				x0, y0 := rng.Intn(128)-64, rng.Intn(128)-64
				segs = append(segs, [4]int{x0, y0, x0 + dx, y0 + dy})
			}
		}
		x, y := rng.Intn(128)-64, rng.Intn(128)-64
		segs = append(segs, [4]int{x, y, x, y})
	}
	for i := range 4000 {
		base := 0
		if i%8 == 0 {
			base = rng.Intn(1<<25) - 1<<24
		}
		near := func() int { return base + rng.Intn(256) - 128 }
		segs = append(segs, [4]int{near(), near(), near(), near()})
	}
	return segs
}

// TestLineWalksTheReferenceCells: Line visits the reference loop's cells in its order
// in every octant, on the axes and diagonals and at zero length, in max(|dx|, |dy|)
// steps, and a step on the end stays there
func TestLineWalksTheReferenceCells(t *testing.T) {
	for _, s := range lineSegments(NewFastRand(0x11e)) {
		want := lineRef(nil, s[0], s[1], s[2], s[3])
		l := NewLine(s[0], s[1], s[2], s[3])
		if l.Steps() != len(want)-1 {
			t.Fatalf("segment %v: %d steps, the reference takes %d", s, l.Steps(), len(want)-1)
		}
		for i, c := range want {
			if l.X() != c.X || l.Y() != c.Y || l.AtEnd() != (i == len(want)-1) {
				t.Fatalf("segment %v: cell %d is (%d, %d) at end %v, the reference's %v", s, i, l.X(), l.Y(), l.AtEnd(), c)
			}
			l = l.Next()
		}
		if l.X() != s[2] || l.Y() != s[3] || !l.AtEnd() {
			t.Fatalf("segment %v: a step on the end moved to (%d, %d)", s, l.X(), l.Y())
		}
	}
}

// benchSegments are random segments across a 160x48 map
func benchSegments() [][4]int {
	rng := NewFastRand(0xbe9c)
	segs := make([][4]int, 64)
	for i := range segs {
		segs[i] = [4]int{rng.Intn(160), rng.Intn(48), rng.Intn(160), rng.Intn(48)}
	}
	return segs
}

func BenchmarkLine(b *testing.B) {
	segs, cells := benchSegments(), make([]Point, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range segs {
			cells = lineCells(cells[:0], s[0], s[1], s[2], s[3])
		}
	}
	sinkI = int64(len(cells))
}

func BenchmarkLineReference(b *testing.B) {
	segs, cells := benchSegments(), make([]Point, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range segs {
			cells = lineRef(cells[:0], s[0], s[1], s[2], s[3])
		}
	}
	sinkI = int64(len(cells))
}

// TestLine3WalksTheLongestSpanOneCellAStep: a walk through a volume starts on a,
// ends on b after as many steps as its longest span, moves at most one cell on
// each axis a step, and in one plane walks Line's cells.
func TestLine3WalksTheLongestSpanOneCellAStep(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 3))
	for range 3000 {
		a := Point3{X: rng.IntN(31) - 15, Y: rng.IntN(31) - 15, Z: rng.IntN(31) - 15}
		b := Point3{X: rng.IntN(31) - 15, Y: rng.IntN(31) - 15, Z: rng.IntN(31) - 15}
		if rng.IntN(2) == 0 {
			b.Z = a.Z
		}
		span := max(IntAbs(b.X-a.X), IntAbs(b.Y-a.Y), IntAbs(b.Z-a.Z))
		l := NewLine3(a, b)
		if l.Point() != a || l.Steps() != span {
			t.Fatalf("%v→%v starts on %v with %d steps, want %d", a, b, l.Point(), l.Steps(), span)
		}
		prev, cells := a, []Point3(nil)
		for !l.AtEnd() {
			l = l.Next()
			c := l.Point()
			if c == prev || IntAbs(c.X-prev.X) > 1 || IntAbs(c.Y-prev.Y) > 1 || IntAbs(c.Z-prev.Z) > 1 {
				t.Fatalf("%v→%v stepped from %v to %v", a, b, prev, c)
			}
			prev, cells = c, append(cells, c)
		}
		if prev != b || len(cells) != span {
			t.Fatalf("%v→%v ended on %v after %d steps, want %d", a, b, prev, len(cells), span)
		}
		if b.Z != a.Z {
			continue
		}
		var flat []Point3
		for f := NewLine(a.X, a.Y, b.X, b.Y); !f.AtEnd(); {
			f = f.Next()
			flat = append(flat, Point3{X: f.X(), Y: f.Y(), Z: a.Z})
		}
		if !slices.Equal(flat, cells) {
			t.Fatalf("%v→%v in one plane walks %v, Line walks %v", a, b, cells, flat)
		}
	}
}
