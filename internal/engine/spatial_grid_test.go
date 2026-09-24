package engine

import (
	"testing"
	"unsafe"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestCellFitsFourCacheLines pins the layout the partition byte was taken from.
func TestCellFitsFourCacheLines(t *testing.T) {
	if got := unsafe.Sizeof(Cell{}); got != 256 {
		t.Fatalf("unsafe.Sizeof(Cell{}) = %d, want 256", got)
	}
}

func TestGridPartitionSurvivesRemoval(t *testing.T) {
	g := NewSpatialGrid(4, 4)
	shared := []core.Entity{core.MakeEntity(core.DomainShared, 1), core.MakeEntity(core.DomainShared, 2)}
	player := []core.Entity{
		core.MakeEntity(core.DomainPlayer, 1),
		core.MakeEntity(core.DomainPlayer, 2),
		core.MakeEntity(core.DomainPlayer, 3),
	}

	// Interleaved insertion must still yield a partitioned cell
	g.Set(player[0], 1, 1)
	g.Set(shared[0], 1, 1)
	g.Set(player[1], 1, 1)
	g.Set(shared[1], 1, 1)
	g.Set(player[2], 1, 1)
	assertPartition(t, g, 1, 1, 2, 3)

	g.RemoveEntityAt(shared[0], 1, 1)
	assertPartition(t, g, 1, 1, 1, 3)

	g.RemoveEntityAt(player[1], 1, 1)
	assertPartition(t, g, 1, 1, 1, 2)

	if got := g.EntitiesAt(1, 1, ScopeShared)[0]; got != shared[1] {
		t.Fatalf("surviving shared entity = %d, want %d", got, shared[1])
	}
}

func TestPlayerBudgetPreservesSharedCapacity(t *testing.T) {
	g := NewSpatialGrid(1, 1)
	for i := range parameter.ReservedPlayerPerCell {
		if !g.Set(core.MakeEntity(core.DomainPlayer, uint64(i+1)), 0, 0) {
			t.Fatalf("player %d rejected inside the reserved budget", i)
		}
	}
	if g.Set(core.MakeEntity(core.DomainPlayer, 9999), 0, 0) {
		t.Fatal("player insert exceeded the reserved budget")
	}
	for i := range parameter.MaxEntitiesPerCell - parameter.ReservedPlayerPerCell {
		if !g.Set(core.MakeEntity(core.DomainShared, uint64(i+1)), 0, 0) {
			t.Fatalf("shared %d displaced by a saturated player run", i)
		}
	}
}

// assertPartition checks the domain invariant and the expected run lengths.
func assertPartition(t *testing.T, g *SpatialGrid, x, y, wantShared, wantPlayer int) {
	t.Helper()
	sharedView := g.EntitiesAt(x, y, ScopeShared)
	playerView := g.EntitiesAt(x, y, ScopePlayer)
	if len(sharedView) != wantShared || len(playerView) != wantPlayer {
		t.Fatalf("cell runs = (%d, %d), want (%d, %d)", len(sharedView), len(playerView), wantShared, wantPlayer)
	}
	for _, e := range sharedView {
		if e.Domain() != core.DomainShared {
			t.Fatalf("player entity %d inside the shared run", e)
		}
	}
	for _, e := range playerView {
		if e.Domain() != core.DomainPlayer {
			t.Fatalf("shared entity %d inside the player run", e)
		}
	}
}

// TestWallTestAnswersAsHasBlockingWallAt: the grid a derivation reads is the live
// query cell for cell — mask, domain, soft-clipped walls and out-of-bounds included.
func TestWallTestAnswersAsHasBlockingWallAt(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	wall := func(domain core.Domain, x, y int, mask component.WallBlockMask) {
		e := w.CreateEntity(domain)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: mask})
	}
	wall(core.DomainShared, 2, 2, component.WallBlockKinetic)
	wall(core.DomainShared, 3, 2, component.WallBlockCursor)
	wall(core.DomainPlayer, 4, 2, component.WallBlockKinetic)
	for range parameter.MaxEntitiesPerCell {
		w.Positions.SetPosition(w.CreateEntity(core.DomainShared), component.PositionComponent{X: 5, Y: 2})
	}
	wall(core.DomainShared, 5, 2, component.WallBlockKinetic) // clipped out of the grid

	cfg := w.Resources.Config
	var buf []bool
	for _, mask := range []component.WallBlockMask{0, component.WallBlockKinetic} {
		test := w.Positions.WallTest(mask, &buf)
		for y := -1; y <= cfg.MapHeight; y++ {
			for x := -1; x <= cfg.MapWidth; x++ {
				if got, want := test(x, y), w.Positions.HasBlockingWallAt(x, y, mask); got != want {
					t.Fatalf("mask %d at (%d,%d): WallTest %t, HasBlockingWallAt %t", mask, x, y, got, want)
				}
			}
		}
	}
	if !w.Positions.WallTest(component.WallBlockKinetic, &buf)(2, 2) {
		t.Fatal("the kinetic wall the test is built around was not seen")
	}
}
