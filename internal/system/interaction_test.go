package system

import (
	"math"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestAShieldCoversItsRadiusAtTheCellAspect: a shield reaches its radius in columns
// and the rows the cell aspect makes of it; one down, or with no reach, covers no
// cell, not even its cursor's, and no shot far off meets it
func TestAShieldCoversItsRadiusAtTheCellAspect(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	shield, _ := w.Components.Shield.GetPtr(first)
	pos, _ := w.Positions.GetPosition(first)
	covers := func(dx, dy int) bool { return shieldCovers(w, shield, pos.X, pos.Y, pos.X+dx, pos.Y+dy) }

	for _, tc := range []struct {
		aspect  vmath.Aspect
		in, out [][2]int
	}{
		{vmath.RefAspect, [][2]int{{4, 0}, {-4, 0}, {0, 2}, {0, -2}, {3, 1}}, [][2]int{{5, 0}, {0, 3}, {4, 1}}},
		{1, [][2]int{{0, 4}, {0, -4}, {2, 3}}, [][2]int{{0, 5}, {3, 3}}},
	} {
		w.Resources.Config.CellAspect = tc.aspect
		shield.Active, shield.Radius = true, 4
		for _, c := range tc.in {
			if !covers(c[0], c[1]) {
				t.Errorf("aspect %v: radius 4 misses %v", tc.aspect, c)
			}
		}
		for _, c := range tc.out {
			if covers(c[0], c[1]) {
				t.Errorf("aspect %v: radius 4 covers %v", tc.aspect, c)
			}
		}
	}

	w.Resources.Config.CellAspect = vmath.RefAspect
	for _, radius := range []float64{0, -1, math.NaN()} {
		shield.Active, shield.Radius = true, radius
		if covers(0, 0) {
			t.Errorf("radius %v covers its cursor's cell", radius)
		}
		if hit := CursorContactAt(w, pos.X+10, pos.Y+5); hit != 0 {
			t.Errorf("radius %v: a shot far off meets cursor %d", radius, hit)
		}
	}
	shield.Active, shield.Radius = false, 4
	if covers(1, 0) {
		t.Error("a shield that is down covers a cell")
	}
}

// TestAShieldsReachStopsAtTheMap: any radius converts to a box no larger than the map
func TestAShieldsReachStopsAtTheMap(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	shield, _ := w.Components.Shield.GetPtr(first)
	cfg := w.Resources.Config
	for _, tc := range []struct {
		radius float64
		rx, ry int
	}{
		{4, 4, 2}, {4.75, 4, 2}, {0, 0, 0}, {-2, 0, 0}, {math.NaN(), 0, 0},
		{1e300, cfg.MapWidth, cfg.MapHeight}, {math.Inf(1), cfg.MapWidth, cfg.MapHeight},
	} {
		shield.Radius = tc.radius
		if rx, ry := w.ShieldReach(shield); rx != tc.rx || ry != tc.ry {
			t.Errorf("radius %v reaches (%d, %d), want (%d, %d)", tc.radius, rx, ry, tc.rx, tc.ry)
		}
	}
}
