package vmath

import (
	"math"
	"slices"
	"testing"
)

func TestSampleEllipseGridRejectsInvalidCount(t *testing.T) {
	for _, count := range []int{0, -1} {
		if got := SampleEllipseGrid(0, 0, 4, 2, count); got != nil {
			t.Errorf("SampleEllipseGrid count %d = %v, want nil", count, got)
		}
	}
}

func TestAngleToGridPosUsesCenteredCell(t *testing.T) {
	x, y := AngleToGridPos(math.Pi, 0, 0, 1, 0.5)
	if x != -1 || y != 0 {
		t.Fatalf("AngleToGridPos(pi) = (%d,%d), want (-1,0)", x, y)
	}
}

// TestEllipseSamplesAreTheirAnglesCells: a sample is the cell of its angle's point
// on the ellipse, whether its count reads the sine table or computes each angle
func TestEllipseSamplesAreTheirAnglesCells(t *testing.T) {
	rng := NewFastRand(0x5a)
	for _, count := range []int{1, EllipseSampleCount, 48, LUTSize} {
		for range 200 {
			cx, cy, rx, ry := rng.Intn(4000)-2000, rng.Intn(4000)-2000, rng.Float64()*300, rng.Float64()*300
			cells := SampleEllipseGrid(cx, cy, rx, ry, count)
			if len(cells) != count {
				t.Fatalf("%d samples of %d", len(cells), count)
			}
			for i, cell := range cells {
				x, y := AngleToGridPos(float64(i)*(TwoPi/float64(count)), cx, cy, rx, ry)
				if cell != [2]int{x, y} {
					t.Fatalf("sample %d of %d about (%d,%d) radii (%v,%v) = %v, want (%d,%d)", i, count, cx, cy, rx, ry, cell, x, y)
				}
			}
		}
	}
}

// TestArcAppendFormsReuseTheirStorage: sampling an orbit, finding its free arcs
// and spreading angles over them after what a caller's storage holds give what
// the allocating forms return, and allocate nothing once the storage has grown.
func TestArcAppendFormsReuseTheirStorage(t *testing.T) {
	rng := NewFastRand(0xa7c)
	var cells [][2]int
	var arcs []ArcSegment
	var angles []float64
	blocked := make([]bool, EllipseSampleCount)
	for round := range 400 {
		for i := range blocked {
			blocked[i] = round%8 != 0 && rng.Intn(1+round%7) == 0
		}
		if round%9 == 1 {
			clear(blocked)
		}
		cx, cy, rx, ry, n := rng.Intn(80), rng.Intn(40), rng.Float64()*9, rng.Float64()*5, 1+rng.Intn(6)
		cells = AppendEllipseGrid(append(cells[:0], [2]int{-1, -1}), cx, cy, rx, ry, EllipseSampleCount)
		arcs = AppendUnblockedArcs(append(arcs[:0], ArcSegment{Length: -1}), blocked)
		angles = AppendDistributedAngles(append(angles[:0], -1), arcs[1:], n)
		want := FindUnblockedArcs(blocked)
		if !slices.Equal(cells[1:], SampleEllipseGrid(cx, cy, rx, ry, EllipseSampleCount)) || !slices.Equal(arcs[1:], want) ||
			!slices.Equal(angles[1:], DistributeAngles(want, n)) {
			t.Fatalf("round %d: the append forms part from the allocating ones over %v", round, blocked)
		}
		if cells[0] != [2]int{-1, -1} || arcs[0].Length != -1 || angles[0] != -1 {
			t.Fatalf("round %d: an append form wrote over what its storage held", round)
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		cells = AppendEllipseGrid(cells[:0], 40, 20, 6, 3, EllipseSampleCount)
		arcs = AppendUnblockedArcs(arcs[:0], blocked)
		angles = AppendDistributedAngles(angles[:0], arcs, 4)
	}); n != 0 {
		t.Fatalf("the append forms allocate %v times a round into grown storage", n)
	}
}
