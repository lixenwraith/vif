package engine

import (
	"math/rand/v2"
	"testing"
	"unsafe"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
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

// FuzzWallTestAnswersAsHasBlockingWallAt: the grid a derivation reads is the live
// query cell for cell — any mask, either domain, soft-clipped walls, out of bounds
// — and one built after walls are destroyed, moved, remasked or spawned sees them.
func FuzzWallTestAnswersAsHasBlockingWallAt(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		rng := rand.New(rand.NewPCG(seed, seed>>7))
		w := NewWorld()
		NewGameContextWithClock(w, 12+rng.IntN(40), 8+rng.IntN(20), NewManualClock())
		cfg := w.Resources.Config
		masks := []component.WallBlockMask{component.WallBlockNone, component.WallBlockCursor,
			component.WallBlockKinetic, component.WallBlockKinetic | component.WallBlockSpawn, component.WallBlockAll}
		var walls []core.Entity
		spawn := func() {
			domain := core.DomainShared
			if rng.IntN(8) == 0 {
				domain = core.DomainPlayer
			}
			x, y := rng.IntN(cfg.MapWidth), rng.IntN(cfg.MapHeight)
			if rng.IntN(16) == 0 { // a full cell soft-clips the wall out of the grid
				for range parameter.MaxEntitiesPerCell {
					w.Positions.SetPosition(w.CreateEntity(core.DomainShared), component.PositionComponent{X: x, Y: y})
				}
			}
			e := w.CreateEntity(domain)
			w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
			w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: masks[rng.IntN(len(masks))]})
			walls = append(walls, e)
		}
		for range rng.IntN(cfg.MapWidth * cfg.MapHeight / 2) {
			spawn()
		}

		var buf []bool
		for round := range 2 {
			if round == 1 { // a storm between two derivations
				for _, e := range walls {
					switch rng.IntN(6) {
					case 0:
						w.DestroyEntity(e)
					case 1:
						if wall, ok := w.Components.Wall.GetPtr(e); ok {
							wall.BlockMask = masks[rng.IntN(len(masks))]
						}
					case 2:
						w.Positions.SetPosition(e, component.PositionComponent{X: rng.IntN(cfg.MapWidth), Y: rng.IntN(cfg.MapHeight)})
					}
				}
				for range rng.IntN(20) {
					spawn()
				}
			}
			for _, mask := range masks {
				test := w.Positions.WallTest(mask, &buf)
				for y := -1; y <= cfg.MapHeight; y++ {
					for x := -1; x <= cfg.MapWidth; x++ {
						if got, want := test(x, y), w.Positions.HasBlockingWallAt(x, y, mask); got != want {
							t.Fatalf("round %d mask %d at (%d,%d): WallTest %t, HasBlockingWallAt %t", round, mask, x, y, got, want)
						}
					}
				}
			}
		}
	})
}
