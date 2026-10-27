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
// — and one reused across storms that destroy, move, remask, respawn, clip, shift
// and re-index walls, reconcile them to a capture, or lay, shift and lift a rock
// field, sees every change; so does a reader of the wall log re-testing only the
// cells it names, and an area query is the cells' queries joined.
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
		var saved *WorldCopy
		var since uint64
		readers := make([][]bool, len(masks))
		for round := range 24 {
			if round%4 == 0 {
				saved = w.CopyOut(saved)
			}
			rockColumn := func() RockColumn {
				return RockColumn{Ceil: rng.IntN(cfg.MapHeight+2) - 1, Floor: rng.IntN(cfg.MapHeight+2) - 1}
			}
			if round%2 == 1 { // a storm of one kind between two derivations, so none hides another
				kind := rng.IntN(16)
				for _, e := range walls {
					if rng.IntN(3) != 0 {
						continue
					}
					switch kind {
					case 0:
						w.DestroyEntity(e)
					case 1:
						if wall, ok := w.Components.Wall.GetPtr(e); ok {
							wall.BlockMask = masks[rng.IntN(len(masks))]
							if rng.IntN(2) == 0 {
								w.Positions.WallChanged(e)
							} else {
								w.Positions.WallsChanged()
							}
						}
					case 2:
						to := component.PositionComponent{X: rng.IntN(cfg.MapWidth), Y: rng.IntN(cfg.MapHeight)}
						if rng.IntN(2) == 0 {
							w.Positions.Move(e, to)
						} else {
							w.Positions.SetPosition(e, to)
						}
					case 3: // re-enters its cell once it is full, clipped, and swaps back into its slot
						pos, ok := w.Positions.GetPosition(e)
						if !ok {
							break
						}
						slot, _ := w.Positions.index.get(e)
						w.Positions.RemoveEntity(e)
						for range parameter.MaxEntitiesPerCell {
							w.Positions.SetPosition(w.CreateEntity(core.DomainShared), pos)
						}
						w.Positions.SetPosition(e, pos)
						if int(slot) < len(w.Positions.entities) && w.Positions.entities[slot] != e {
							w.DestroyEntity(w.Positions.entities[slot])
						}
					case 4: // loses its wall and takes it back
						if wall, ok := w.Components.Wall.GetComponent(e); ok {
							w.Components.Wall.RemoveEntity(e)
							w.Components.Wall.SetComponent(e, wall)
						}
					case 5: // a clipped wall's cell empties and the wall retries
						pos, ok := w.Positions.GetPosition(e)
						if !ok {
							break
						}
						for _, o := range w.Positions.GetEntitiesAt(pos.X, pos.Y, ScopeBoth) {
							if o != e && !w.Components.Wall.HasEntity(o) {
								w.Positions.Move(o, component.PositionComponent{X: -1, Y: -1})
							}
						}
						w.Positions.SetPosition(e, pos)
					case 6:
						w.Positions.RemoveEntity(e)
					case 7: // an unplaced wall is placed
						if !w.Positions.HasPosition(e) {
							w.Positions.SetPosition(e, component.PositionComponent{X: rng.IntN(cfg.MapWidth), Y: rng.IntN(cfg.MapHeight)})
						}
					}
				}
				switch kind {
				case 8:
					for range 1 + rng.IntN(20) {
						spawn()
					}
				case 9:
					w.Positions.ResizeGrid(cfg.MapWidth, cfg.MapHeight)
				case 10:
					w.CopyIn(saved)
				case 11: // a correction rewrites masks over the walls already there
					capture := w.CaptureSharedWorld()
					for i := range capture.Wall {
						if rng.IntN(3) == 0 {
							capture.Wall[i].Value.BlockMask = masks[rng.IntN(len(masks))]
						}
					}
					w.ReconcileSharedWorld(capture)
				case 12:
					var group []core.Entity
					for _, e := range walls {
						if rng.IntN(3) == 0 {
							group = append(group, e)
						}
					}
					w.Positions.Shift(group, rng.IntN(5)-2, rng.IntN(5)-2, nil)
				case 13: // a field narrower or wider than the map
					cols := make([]RockColumn, rng.IntN(cfg.MapWidth+2))
					for i := range cols {
						cols[i] = rockColumn()
					}
					w.Positions.SetRock(masks[1+rng.IntN(len(masks)-1)], RockLook{}, 0, cols)
				case 14:
					w.Positions.ShiftRock(rockColumn())
				case 15:
					w.Positions.SetRock(0, RockLook{}, 0, nil)
				}
			}
			// A reader of the wall log re-testing only the cells it names keeps every
			// answer HasBlockingWallAt gives
			cells, now, logged := w.Positions.WallChanges(since)
			since = now
			for i, mask := range masks {
				if !logged || readers[i] == nil {
					readers[i] = make([]bool, cfg.MapWidth*cfg.MapHeight)
					for c := range readers[i] {
						readers[i][c] = w.Positions.HasBlockingWallAt(c%cfg.MapWidth, c/cfg.MapWidth, mask)
					}
				}
				for _, c := range cells {
					if c.X >= 0 && c.Y >= 0 && c.X < cfg.MapWidth && c.Y < cfg.MapHeight {
						readers[i][c.Y*cfg.MapWidth+c.X] = w.Positions.HasBlockingWallAt(c.X, c.Y, mask)
					}
				}
				for c, got := range readers[i] {
					if x, y := c%cfg.MapWidth, c/cfg.MapWidth; got != w.Positions.HasBlockingWallAt(x, y, mask) {
						t.Fatalf("round %d mask %d at (%d,%d): the wall log's reader holds %t", round, mask, x, y, got)
					}
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
				for range 8 {
					x, y := rng.IntN(cfg.MapWidth+4)-2, rng.IntN(cfg.MapHeight+4)-2
					aw, ah := rng.IntN(6), rng.IntN(6)
					want := false
					for cy := y; cy < y+ah; cy++ {
						for cx := x; cx < x+aw; cx++ {
							want = want || w.Positions.HasBlockingWallAt(cx, cy, mask)
						}
					}
					if got := w.Positions.HasBlockingWallInArea(x, y, aw, ah, mask); got != want {
						t.Fatalf("round %d mask %d area (%d,%d) %dx%d: %t, cells say %t", round, mask, x, y, aw, ah, got, want)
					}
				}
			}
		}
	})
}

// TestShiftMovesARigidGroupThroughFullCells: two full player halves side by side
// shift without clipping, where moving one entity at a time would overfill a cell;
// a cell that still refuses the mover is reported, not hidden.
func TestShiftMovesARigidGroupThroughFullCells(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())

	var group []core.Entity
	for x := 3; x <= 4; x++ {
		for range parameter.ReservedPlayerPerCell {
			e := w.CreateEntity(core.DomainPlayer)
			w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: 3})
			group = append(group, e)
		}
	}
	if unplaced := w.Positions.Shift(group, 1, 0, nil); len(unplaced) != 0 {
		t.Fatalf("rigid shift refused %d entities", len(unplaced))
	}
	full := parameter.ReservedPlayerPerCell
	for _, c := range []struct{ x, want int }{{3, 0}, {4, full}, {5, full}} {
		if got := len(w.Positions.grid.EntitiesAt(c.x, 3, ScopePlayer)); got != c.want {
			t.Fatalf("cell %d holds %d player entities, want %d", c.x, got, c.want)
		}
	}

	lone := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(lone, component.PositionComponent{X: 6, Y: 3})
	unplaced := w.Positions.Shift([]core.Entity{lone}, -1, 0, nil)
	if len(unplaced) != 1 || unplaced[0] != lone {
		t.Fatalf("refused mover reported as %v, want [%v]", unplaced, lone)
	}
	if pos, _ := w.Positions.GetPosition(lone); pos.X != 5 {
		t.Fatalf("refused mover at x=%d, want its destination 5", pos.X)
	}
}

// TestTopGlyphIsSharedThenNearestThenLowest: the glyph a cell shows and types is a
// shared one first, then the highest level, then the lowest entity.
func TestTopGlyphIsSharedThenNearestThenLowest(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	at := component.PositionComponent{X: 7, Y: 2}
	glyph := func(d core.Domain, level component.GlyphLevel) core.Entity {
		e := w.CreateEntity(d)
		w.Positions.SetPosition(e, at)
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'x', Level: level})
		return e
	}

	if _, ok := w.Positions.TopGlyphAt(at.X, at.Y); ok {
		t.Fatal("empty cell reported a top glyph")
	}
	w.Positions.SetPosition(w.CreateEntity(core.DomainPlayer), at)
	glyph(core.DomainPlayer, component.GlyphDark)
	nearLow := glyph(core.DomainPlayer, component.GlyphBright)
	glyph(core.DomainPlayer, component.GlyphBright)
	glyph(core.DomainPlayer, component.GlyphNormal)
	if top, _ := w.Positions.TopGlyphAt(at.X, at.Y); top != nearLow {
		t.Fatalf("top player glyph = %v, want the lower bright %v", top, nearLow)
	}
	gold := glyph(core.DomainShared, component.GlyphDark)
	if top, _ := w.Positions.TopGlyphAt(at.X, at.Y); top != gold {
		t.Fatalf("top glyph = %v, want shared %v over any player level", top, gold)
	}
}

// TestTopGlyphPassesOverScenery: typing's top glyph is the top typeable one, of any
// mask but scenery's, and the one drawn is that over any scenery, or the nearest
// scenery where only scenery stands
func TestTopGlyphPassesOverScenery(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	at := component.PositionComponent{X: 7, Y: 2}
	glyph := func(level component.GlyphLevel, mask component.ProtectionFlags) core.Entity {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, at)
		w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'x', Level: level})
		w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: mask})
		return e
	}
	drawn := func(want core.Entity) {
		t.Helper()
		for _, e := range w.Positions.GetAllEntityAt(at.X, at.Y) {
			if got := w.Positions.TopGlyphIs(e, at.X, at.Y); got != (e == want) {
				t.Fatalf("TopGlyphIs(%v) = %v, want %v drawn", e, got, want)
			}
		}
	}

	far := glyph(component.GlyphDark, component.ProtectScenery)
	drawn(far)
	near := glyph(component.GlyphBright, component.ProtectScenery)
	drawn(near)
	if _, ok := w.Positions.TopGlyphAt(at.X, at.Y); ok || w.Positions.IsTypeable(near) {
		t.Fatal("typing found scenery")
	}
	typeable := glyph(component.GlyphDark, component.ProtectFromDelete|component.ProtectFromParticle)
	glyph(component.GlyphBright, component.ProtectScenery)
	if top, _ := w.Positions.TopGlyphAt(at.X, at.Y); top != typeable || !w.Positions.IsTypeable(typeable) {
		t.Fatalf("top glyph = %v, want the typeable %v under nearer scenery", top, typeable)
	}
	drawn(typeable)
}

// TestGridOccupiedListIsTheCellsHoldingEntities holds the index a copy and the gauges
// read in place of the map: every listed cell holds something, every cell holding
// something is listed once at its slot, and the stats read off the list are the
// full scan's.
func TestGridOccupiedListIsTheCellsHoldingEntities(t *testing.T) {
	g := NewSpatialGrid(7, 5)
	rng := rand.New(rand.NewPCG(3, 9))
	type placed struct {
		e    core.Entity
		x, y int
	}
	var held []placed
	check := func() {
		t.Helper()
		seen := map[int32]bool{}
		for slot, idx := range g.Occupied() {
			c := &g.Cells[idx]
			if c.Count == 0 || int(c.slot) != slot || seen[idx] {
				t.Fatalf("occupied[%d] = cell %d: count %d, slot %d, seen %v", slot, idx, c.Count, c.slot, seen[idx])
			}
			seen[idx] = true
		}
		var want GridStats
		for i := range g.Cells {
			if n := int(g.Cells[i].Count); n > 0 {
				if !seen[int32(i)] {
					t.Fatalf("cell %d holds %d and is not listed", i, n)
				}
				want.CellsOccupied++
				want.EntitiesTotal += n
				want.EntitiesShared += int(g.Cells[i].SharedCount)
				want.MaxOccupancy = max(want.MaxOccupancy, n)
			}
		}
		if got := g.ComputeStats(); got != want {
			t.Fatalf("stats off the list %+v, off the cells %+v", got, want)
		}
	}
	for step := range 2000 {
		switch {
		case len(held) > 0 && rng.IntN(3) == 0:
			i := rng.IntN(len(held))
			g.RemoveEntityAt(held[i].e, held[i].x, held[i].y)
			held[i] = held[len(held)-1]
			held = held[:len(held)-1]
		case step%97 == 96:
			g.Clear()
			held = nil
		default:
			d := core.DomainShared
			if rng.IntN(4) == 0 {
				d = core.DomainPlayer
			}
			e := core.MakeEntity(d, uint64(step+1))
			x, y := rng.IntN(7), rng.IntN(5)
			if g.Set(e, x, y) {
				held = append(held, placed{e, x, y})
			}
		}
		check()
	}
}
