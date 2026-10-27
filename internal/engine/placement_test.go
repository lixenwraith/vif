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

	// The top slot sits padding above the exclusion, centred on it; all of it but
	// one cell is open, and every other cell of the map is a wall
	topX, topY := ax-objW/2, ay-exclusion.Top-objH-padding
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
