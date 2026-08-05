package vmath

import (
	"math"
	"testing"
)

func TestSampleEllipseGridFRejectsInvalidCount(t *testing.T) {
	for _, count := range []int{0, -1} {
		if got := SampleEllipseGridF(0, 0, 4, 2, count); got != nil {
			t.Errorf("SampleEllipseGridF count %d = %v, want nil", count, got)
		}
	}
}

func TestAngleToGridPosFUsesCenteredCell(t *testing.T) {
	x, y := AngleToGridPosF(math.Pi, 0, 0, 1, 0.5)
	if x != -1 || y != 0 {
		t.Fatalf("AngleToGridPosF(pi) = (%d,%d), want (-1,0)", x, y)
	}
}
