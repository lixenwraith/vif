package navigation

import (
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// referenceCompute is Compute as it stood before its wall answers were memoized
// and its heap became a bucket queue: every neighbour and both corners of every
// diagonal asked of isBlocked directly.
func referenceCompute(f *FlowField, targets []vmath.Point, isBlocked WallChecker) {
	if len(targets) == 0 {
		f.Valid = false
		return
	}
	w := f.Width
	f.CurrentGen++
	if f.CurrentGen == 0 {
		clear(f.VisitedGen)
		f.CurrentGen = 1
	}
	var heap minHeap
	for _, t := range targets {
		if t.X < 0 || t.Y < 0 || t.X >= f.Width || t.Y >= f.Height {
			continue
		}
		targetIdx := int32(t.Y*w + t.X)
		if !isBlocked(t.X, t.Y) {
			if f.VisitedGen[targetIdx] != f.CurrentGen {
				f.Distances[targetIdx] = 0
				f.VisitedGen[targetIdx] = f.CurrentGen
				f.Directions[targetIdx] = DirTarget
				heap.push(heapEntry{idx: targetIdx, dist: 0})
			}
		} else {
			referenceSeed(f, &heap, t.X, t.Y, isBlocked)
		}
	}
	if len(heap) == 0 {
		f.Valid = false
		return
	}
	for len(heap) > 0 {
		entry := heap.pop()
		idx := entry.idx
		if f.VisitedGen[idx] == f.CurrentGen && entry.dist > f.Distances[idx] {
			continue
		}
		cx, cy := int(idx)%w, int(idx)/w
		for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
			nx, ny := cx+DirVectors[dirIdx][0], cy+DirVectors[dirIdx][1]
			if nx < 0 || nx >= f.Width || ny < 0 || ny >= f.Height || isBlocked(nx, ny) {
				continue
			}
			dx, dy := DirVectors[dirIdx][0], DirVectors[dirIdx][1]
			if dx != 0 && dy != 0 && (isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy)) {
				continue
			}
			nIdx := int32(ny*w + nx)
			newDist := entry.dist + dirCosts[dirIdx]
			if f.VisitedGen[nIdx] != f.CurrentGen || newDist < f.Distances[nIdx] {
				f.Distances[nIdx] = newDist
				f.VisitedGen[nIdx] = f.CurrentGen
				f.Directions[nIdx] = DirOpposite[dirIdx]
				heap.push(heapEntry{idx: nIdx, dist: newDist})
			}
		}
	}
	f.Valid = true
}

// referenceSeed is seedVirtualTargets onto a heap
func referenceSeed(f *FlowField, heap *minHeap, tx, ty int, isBlocked WallChecker) {
	for radius := 1; radius <= maxSearchRadius; radius++ {
		found := false
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				nx, ny := tx+dx, ty+dy
				if abs(dx) != radius && abs(dy) != radius || nx < 0 || nx >= f.Width || ny < 0 || ny >= f.Height || isBlocked(nx, ny) {
					continue
				}
				idx := int32(ny*f.Width + nx)
				cost := int32(abs(dx)*CostX + abs(dy)*CostY)
				if f.VisitedGen[idx] != f.CurrentGen || cost < f.Distances[idx] {
					f.Distances[idx], f.VisitedGen[idx], f.Directions[idx] = cost, f.CurrentGen, DirTarget
					heap.push(heapEntry{idx: idx, dist: cost})
					found = true
				}
			}
		}
		if found {
			return
		}
	}
}

// shortestStep reports whether a direction at (x, y) leads to a neighbour the
// field may step to whose distance plus the step's cost is the cell's own.
func shortestStep(f *FlowField, x, y int, isBlocked WallChecker) bool {
	d := f.GetDirection(x, y)
	if d == DirTarget || d == DirNone {
		return true
	}
	dx, dy := DirVectors[d][0], DirVectors[d][1]
	if dx != 0 && dy != 0 && (isBlocked(x+dx, y) || isBlocked(x, y+dy)) {
		return false
	}
	next := f.GetDistance(x+dx, y+dy)
	return next >= 0 && next+int(dirCosts[d]) == f.GetDistance(x, y)
}

// FuzzComputeMatchesReference: on any wall layout — rooms, jagged edges left by
// destroyed cells, targets inside walls, either out-of-bounds convention — Compute
// gives every cell the reference's distance and a direction that is a shortest step
// (equal-cost ties may settle differently), and a recompute after walls change sees
// the new walls.
func FuzzComputeMatchesReference(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed, uint8(seed*7), uint8(seed*13))
	}
	f.Fuzz(func(t *testing.T, seed uint64, density, targets uint8) {
		rng := rand.New(rand.NewPCG(seed, uint64(density)<<8|uint64(targets)))
		w, h := 2+rng.IntN(40), 2+rng.IntN(20)
		walls := make([]bool, w*h)
		for range rng.IntN(8) { // rectangles, then cells carved out of them
			x0, y0 := rng.IntN(w), rng.IntN(h)
			for y := y0; y < min(h, y0+1+rng.IntN(6)); y++ {
				for x := x0; x < min(w, x0+1+rng.IntN(10)); x++ {
					walls[y*w+x] = true
				}
			}
		}
		for i := range walls {
			if rng.IntN(256) < int(density)/2 {
				walls[i] = !walls[i]
			}
		}
		oobBlocked := seed&1 == 1
		isBlocked := func(x, y int) bool {
			if x < 0 || y < 0 || x >= w || y >= h {
				return oobBlocked
			}
			return walls[y*w+x]
		}
		goals := make([]vmath.Point, 1+int(targets)%3)
		for i := range goals {
			goals[i] = vmath.Point{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1}
		}

		got := NewFlowField(w, h)
		for round := range 2 {
			if round == 1 { // a storm takes cells out of the walls between recomputes
				for i := range walls {
					if walls[i] && rng.IntN(4) == 0 {
						walls[i] = false
					}
				}
			}
			want := NewFlowField(w, h)
			referenceCompute(want, goals, isBlocked)
			got.Compute(goals, isBlocked)
			if got.Valid != want.Valid {
				t.Fatalf("round %d: valid %t, reference %t", round, got.Valid, want.Valid)
			}
			for y := range h {
				for x := range w {
					if gd, wd := got.GetDirection(x, y), want.GetDirection(x, y); (gd < 0 || wd < 0) && gd != wd || !shortestStep(got, x, y, isBlocked) {
						t.Fatalf("round %d (%d,%d): direction %d is not a shortest step (reference %d)", round, x, y, gd, wd)
					}
					if gd, wd := got.GetDistance(x, y), want.GetDistance(x, y); gd != wd {
						t.Fatalf("round %d (%d,%d): distance %d, reference %d", round, x, y, gd, wd)
					}
				}
			}
		}
	})
}

// TestCacheRefreshesSmallTargetMovesAndChangedWalls: a target a cell off waits the
// settle interval rather than the cooldown, and a wall change recomputes at once.
func TestCacheRefreshesSmallTargetMovesAndChangedWalls(t *testing.T) {
	c := NewFlowFieldCache(8, 6, 3, 4, 6)
	walls := make([]bool, 48)
	blocked := func(x, y int) bool { return walls[y*8+x] }
	goal := []vmath.Point{{X: 1, Y: 1}}
	c.ObserveGrid(walls, 1)
	c.Update(goal, blocked)
	goal[0].X++
	for range 5 {
		if c.Update(goal, blocked) {
			t.Fatal("a one-cell move recomputed inside the settle interval")
		}
	}
	if !c.Update(goal, blocked) || c.GetDistance(2, 1) != 0 {
		t.Fatal("one-cell motion never refreshed the goal")
	}
	walls[1*8+3] = true
	c.ObserveGrid(walls, 2)
	if !c.Update(goal, blocked) || c.GetDistance(3, 1) != -1 {
		t.Fatal("changed walls left a stale route")
	}
	walls[1*8+3] = false
	c.ObserveGrid(walls, 3)
	c.Update(goal, blocked)
	if c.GetDistance(3, 1) < 0 {
		t.Fatal("a removed wall stayed blocked")
	}
}

// TestALazyCacheKeepsTheEagerPhase: deferring computes to the first reader changes
// neither when a cache falls due nor what it reads once materialized, so instances
// that read a field and instances that do not stay in one D-17 phase.
func TestALazyCacheKeepsTheEagerPhase(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	walls := make([]bool, 30*12)
	for i := range walls {
		walls[i] = rng.IntN(5) == 0
	}
	blocked := func(x, y int) bool { return x < 0 || y < 0 || x >= 30 || y >= 12 || walls[y*30+x] }
	eager, lazy := NewFlowFieldCache(30, 12, 3, 4, 8), NewFlowFieldCache(30, 12, 3, 4, 8)
	lazy.Lazy = true
	goal := []vmath.Point{{X: 15, Y: 6}}
	for tick := range 200 {
		goal[0].X = max(0, min(29, goal[0].X+rng.IntN(3)-1))
		goal[0].Y = max(-1, min(12, goal[0].Y+rng.IntN(3)-1)) // out of bounds now and then
		if eager.Update(goal, blocked) != lazy.Update(goal, blocked) ||
			eager.TicksSinceCompute != lazy.TicksSinceCompute || eager.Computed() != lazy.Computed() {
			t.Fatalf("tick %d: the lazy cache left the eager phase", tick)
		}
		if tick%7 == 0 {
			lazy.Materialize(blocked)
			for i := range eager.Field.Distances {
				if x, y := i%30, i/30; eager.GetDistance(x, y) != lazy.GetDistance(x, y) {
					t.Fatalf("tick %d: materialized distance at (%d,%d) is %d, eager %d", tick, x, y, lazy.GetDistance(x, y), eager.GetDistance(x, y))
				}
			}
		}
	}
}
