package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// TestAPlacementWithNoClearSlotTakesTheOneOverlappingTheLeast: with walls on every
// slot, the search no longer falls back to its first slot clamped to the map but to
// the slot covering the fewest blocked cells, and still reports none was clear.
func TestAPlacementWithNoClearSlotTakesTheOneOverlappingTheLeast(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 80, 40, NewManualClock())
	cfg := w.Resources.Config
	const objW, objH, padding = 8, 9, 1
	exclusion := Exclusion{Left: 1, Right: 1, Top: 1, Bottom: 1}
	ax, ay := cfg.MapWidth/2, cfg.MapHeight/2

	// The top slot sits padding above the exclusion, centred on it, an even width
	// half a cell right; all of it but one cell is open, and every other cell of the
	// map is a wall
	topX, topY := ax-(objW-1)/2, ay-exclusion.Top-objH-padding
	open := func(x, y int) bool {
		return x >= topX && x < topX+objW && y >= topY && y < topY+objH && (x != topX || y != topY)
	}
	for y := range cfg.MapHeight {
		for x := range cfg.MapWidth {
			if open(x, y) {
				continue
			}
			e := w.CreateEntity(core.DomainShared)
			w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
			w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockAll})
		}
	}

	dx, dy, found := w.Positions.FindPlacementAroundExclusion(ax, ay, objW, objH, exclusion, padding,
		PatternCardinalFirst, component.WallBlockAll, nil)
	if found || ax+dx != topX || ay+dy != topY {
		t.Fatalf("placed at (%d, %d) found %v, want the top slot (%d, %d) with one wall, not found",
			ax+dx, ay+dy, found, topX, topY)
	}
}

// TestAFreeAreaInsideSearchesFromTheEdgeItsOriginPassed: an origin on an edge or past
// it searches from the area flush against that edge, on the origin's row or column,
// so the area keeps the line it was asked for; an area larger than the map finds none
func TestAFreeAreaInsideSearchesFromTheEdgeItsOriginPassed(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 80, 40, NewManualClock())
	cfg := w.Resources.Config
	const width, height, anchorX, anchorY = 5, 3, 2, 1
	row, col := cfg.MapHeight/2, cfg.MapWidth/2
	right, bottom := cfg.MapWidth-width, cfg.MapHeight-height
	for _, tc := range []struct {
		edge              string
		x, y              int
		wantLeft, wantTop int
	}{
		{"inside", col, row, col - anchorX, row - anchorY},
		{"right", cfg.MapWidth - 1, row, right, row - anchorY},
		{"past the right", cfg.MapWidth + 7, row, right, row - anchorY},
		{"left", 0, row, 0, row - anchorY},
		{"past the left", -7, row, 0, row - anchorY},
		{"top", col, 0, col - anchorX, 0},
		{"bottom", col, cfg.MapHeight - 1, col - anchorX, bottom},
		{"corner", -3, cfg.MapHeight + 3, 0, bottom},
	} {
		left, top, found := w.Positions.FindFreeAreaInside(tc.x, tc.y, width, height, anchorX, anchorY, component.WallBlockAll, 0)
		if !found || left != tc.wantLeft || top != tc.wantTop {
			t.Errorf("%s: (%d,%d) found %v at (%d,%d), want the area at (%d,%d)",
				tc.edge, tc.x, tc.y, found, left, top, tc.wantLeft, tc.wantTop)
		}
	}
	if _, _, found := w.Positions.FindFreeAreaInside(col, row, cfg.MapWidth+1, height, 0, 0, component.WallBlockAll, -1); found {
		t.Error("an area wider than the map found a place")
	}
}
