package vmath

import (
	"math"
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
