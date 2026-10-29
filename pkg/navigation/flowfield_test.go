package navigation

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// minHeap is the queue the reference searches run on.
type heapEntry struct {
	idx  int32 // Flat grid index (y*width + x)
	dist int32 // Weighted distance from target
}

type minHeap []heapEntry

func (h *minHeap) push(e heapEntry) {
	*h = append(*h, e)
	// Sift up
	i := len(*h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if (*h)[parent].dist <= (*h)[i].dist {
			break
		}
		(*h)[parent], (*h)[i] = (*h)[i], (*h)[parent]
		i = parent
	}
}

func (h *minHeap) pop() heapEntry {
	old := *h
	n := len(old)
	e := old[0]
	old[0] = old[n-1]
	*h = old[:n-1]

	// Sift down
	i := 0
	for {
		left := 2*i + 1
		if left >= len(*h) {
			break
		}
		smallest := left
		if right := left + 1; right < len(*h) && (*h)[right].dist < (*h)[left].dist {
			smallest = right
		}
		if (*h)[i].dist <= (*h)[smallest].dist {
			break
		}
		(*h)[i], (*h)[smallest] = (*h)[smallest], (*h)[i]
		i = smallest
	}
	return e
}

// referenceCompute is Compute as it stood before its wall answers were memoized
// and its heap became a bucket queue: every neighbour and both corners of every
// diagonal asked of isBlocked directly.
func referenceCompute(f *FlowField, targets []vmath.Point, isBlocked WallChecker) {
	if len(targets) == 0 {
		f.Valid = false
		return
	}
	w := f.Width
	f.alloc()
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
			newDist := entry.dist + refCosts.dir[dirIdx]
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
				cost := int32(abs(dx))*refCosts.X + int32(abs(dy))*refCosts.Y
				if f.VisitedGen[idx] != f.CurrentGen || cost < f.Distances[idx] {
					f.Distances[idx], f.VisitedGen[idx], f.Directions[idx] = cost, f.CurrentGen, DirTarget
					heap.push(heapEntry{idx: idx, dist: cost})
				}
				found = true
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
	return next >= 0 && next+int(refCosts.dir[d]) == f.GetDistance(x, y)
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

// FuzzReachMeasuresWhatTheFieldsRead: from any cell, on any walls, a reach reports
// for every goal what a field seeded at that goal holds at the cell, and where the
// cell holds nothing the least over its neighbours a diagonal away, as the snake
// lock read the fields; the scratch one measure leaves does not reach the next.
func FuzzReachMeasuresWhatTheFieldsRead(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed, uint8(seed*5))
	}
	f.Fuzz(func(t *testing.T, seed uint64, density uint8) {
		rng := rand.New(rand.NewPCG(seed, uint64(density)))
		w, h := 2+rng.IntN(40), 2+rng.IntN(20)
		walls := make([]bool, w*h)
		for range rng.IntN(8) {
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
		isBlocked := func(x, y int) bool { return x < 0 || y < 0 || x >= w || y >= h || walls[y*w+x] }
		goals := make([]vmath.Point, 1+rng.IntN(4))
		for i := range goals {
			goals[i] = vmath.Point{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1}
		}
		g, got := new(SearchGrid), make([]int32, len(goals))
		var r Reach
		for round := range 2 {
			if round == 1 {
				for i := range walls {
					if walls[i] && rng.IntN(4) == 0 {
						walls[i] = false
					}
				}
			}
			g.Fill(w, h, isBlocked)
			from := vmath.Point{X: rng.IntN(w), Y: rng.IntN(h)}
			r.Measure(g, &refCosts, from.X, from.Y, goals, got)
			for j, goal := range goals {
				field := NewFlowField(w, h)
				field.Compute([]vmath.Point{goal}, isBlocked)
				want := field.GetDistance(from.X, from.Y)
				if want < 0 {
					want = CostUnreachable
					for _, d := range DirVectors {
						if next := field.GetDistance(from.X+d[0], from.Y+d[1]); next >= 0 {
							want = min(want, next+int(refCosts.Diag))
						}
					}
				}
				if int(got[j]) != want {
					t.Fatalf("round %d: goal %v from %v measured %d, field reads %d", round, goal, from, got[j], want)
				}
			}
		}
	})
}

// FuzzOpenedRepairsAsARecomputeWould: on any walls, opening any cells leaves a
// repaired field with the distances and directions a recompute on the new grid
// gives, or declines where an opening would reseed a target; and a grid moved cell
// by cell is the grid filled whole.
func FuzzOpenedRepairsAsARecomputeWould(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed, uint8(seed*9))
	}
	f.Fuzz(func(t *testing.T, seed uint64, density uint8) {
		rng := rand.New(rand.NewPCG(seed, uint64(density)))
		w, h := 2+rng.IntN(40), 2+rng.IntN(20)
		walls := make([]bool, w*h)
		for range rng.IntN(8) {
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
		isBlocked := func(x, y int) bool { return x < 0 || y < 0 || x >= w || y >= h || walls[y*w+x] }
		goals := make([]vmath.Point, 1+rng.IntN(3))
		for i := range goals {
			goals[i] = vmath.Point{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1}
		}
		g := new(SearchGrid)
		g.Fill(w, h, isBlocked)
		field := NewFlowField(w, h)
		field.ComputeOn(g, goals)
		if !field.Valid {
			return
		}
		var opened []vmath.Point
		for range 1 + rng.IntN(4) {
			if i := rng.IntN(w * h); walls[i] {
				walls[i] = false
				opened = append(opened, vmath.Point{X: i % w, Y: i / w})
			}
		}
		if len(opened) == 0 {
			return
		}
		g.Move(nil, opened)
		fresh := new(SearchGrid)
		fresh.Fill(w, h, isBlocked)
		if !slices.Equal(g.open, fresh.open) || !slices.Equal(g.free, fresh.free) {
			t.Fatal("a moved grid differs from one filled whole")
		}
		want := NewFlowField(w, h)
		want.ComputeOn(fresh, goals)
		if !field.Opened(g, opened, goals) {
			for _, o := range opened {
				for _, goal := range goals {
					if o == goal || isBlocked(goal.X, goal.Y) && max(abs(o.X-goal.X), abs(o.Y-goal.Y)) <= maxSearchRadius {
						return
					}
				}
			}
			t.Fatal("the repair declined with no opening near a target")
		}
		for y := range h {
			for x := range w {
				if field.GetDistance(x, y) != want.GetDistance(x, y) || field.GetDirection(x, y) != want.GetDirection(x, y) {
					t.Fatalf("(%d,%d): repaired %d/%d, recomputed %d/%d", x, y,
						field.GetDistance(x, y), field.GetDirection(x, y), want.GetDistance(x, y), want.GetDirection(x, y))
				}
			}
		}
	})
}

// TestCacheRefreshesSmallTargetMovesAndChangedWalls: a target a cell off waits the
// settle interval rather than the cooldown, a closed cell recomputes at once, and
// an opened one repairs the field in place without a recompute.
func TestCacheRefreshesSmallTargetMovesAndChangedWalls(t *testing.T) {
	c := NewFlowFieldCache(8, 6, 3, 4, 6)
	walls := make([]bool, 48)
	blocked := func(x, y int) bool { return walls[y*8+x] }
	g := new(SearchGrid)
	g.Fill(8, 6, blocked)
	goal := []vmath.Point{{X: 1, Y: 1}}
	c.Update(goal, g)
	goal[0].X++
	for range 5 {
		if c.Update(goal, g) {
			t.Fatal("a one-cell move recomputed inside the settle interval")
		}
	}
	if !c.Update(goal, g) || c.GetDistance(2, 1) != 0 {
		t.Fatal("one-cell motion never refreshed the goal")
	}
	g.Move([]vmath.Point{{X: 3, Y: 1}}, nil)
	c.Closed()
	if !c.Update(goal, g) || c.GetDistance(3, 1) != -1 {
		t.Fatal("a closed cell left a stale route")
	}
	g.Move(nil, []vmath.Point{{X: 3, Y: 1}})
	c.Moved(g, nil, []vmath.Point{{X: 3, Y: 1}})
	if c.Update(goal, g) || c.GetDistance(3, 1) != 10 {
		t.Fatal("an opened cell recomputed instead of repairing, or stayed blocked")
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
	g := new(SearchGrid)
	g.Fill(30, 12, blocked)
	eager, lazy := NewFlowFieldCache(30, 12, 3, 4, 8), NewFlowFieldCache(30, 12, 3, 4, 8)
	lazy.Lazy = true
	goal := []vmath.Point{{X: 15, Y: 6}}
	for tick := range 200 {
		goal[0].X = max(0, min(29, goal[0].X+rng.IntN(3)-1))
		goal[0].Y = max(-1, min(12, goal[0].Y+rng.IntN(3)-1)) // out of bounds now and then
		if eager.Update(goal, g) != lazy.Update(goal, g) ||
			eager.TicksSinceCompute != lazy.TicksSinceCompute || eager.Computed() != lazy.Computed() {
			t.Fatalf("tick %d: the lazy cache left the eager phase", tick)
		}
		if tick%7 == 0 {
			lazy.Materialize(g)
			for i := range eager.Field.Distances {
				if x, y := i%30, i/30; eager.GetDistance(x, y) != lazy.GetDistance(x, y) {
					t.Fatalf("tick %d: materialized distance at (%d,%d) is %d, eager %d", tick, x, y, lazy.GetDistance(x, y), eager.GetDistance(x, y))
				}
			}
		}
	}
}

// FuzzRouteSearchFindsACheapestLegalPath: under any walls and corridor penalties,
// the route search returns a path of legal steps from start to goal, with the cost
// it reports, whose penalized cost is the least any path has; relaxing every edge
// to a fixpoint is the reference, which no queue order can bias.
func FuzzRouteSearchFindsACheapestLegalPath(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed, uint8(seed*11))
	}
	f.Fuzz(func(t *testing.T, seed uint64, density uint8) {
		rng := rand.New(rand.NewPCG(seed, uint64(density)))
		w, h := 2+rng.IntN(40), 2+rng.IntN(20)
		walls, penalty := make([]bool, w*h), make([]int32, w*h)
		for i := range walls {
			walls[i] = rng.IntN(256) < int(density)/2
			penalty[i] = int32(rng.IntN(RouteGraphMaxRoutes+RouteGraphExtraAttempts+1)) * refCosts.Diag
		}
		isBlocked := func(x, y int) bool { return walls[y*w+x] }
		start, goal := rng.IntN(w*h), rng.IntN(w*h)
		walls[start], walls[goal] = false, false

		legal := func(from, to int) bool {
			fx, fy, tx, ty := from%w, from/w, to%w, to/w
			dx, dy := tx-fx, ty-fy
			return max(abs(dx), abs(dy)) == 1 && !walls[to] && (dx == 0 || dy == 0 || !walls[fy*w+tx] && !walls[ty*w+fx])
		}
		want := make([]int32, w*h)
		for i := range want {
			want[i] = CostUnreachable
		}
		want[start] = 0
		for changed := true; changed; {
			changed = false
			for at := range want {
				for k := range DirCount {
					x, y := at%w+DirVectors[k][0], at/w+DirVectors[k][1]
					if x < 0 || y < 0 || x >= w || y >= h || want[at] == CostUnreachable || !legal(at, y*w+x) {
						continue
					}
					if nd := want[at] + refCosts.dir[k] + penalty[y*w+x]; nd < want[y*w+x] {
						want[y*w+x], changed = nd, true
					}
				}
			}
		}

		sc := new(routeScratch)
		sc.grow(w * h)
		sc.search.Fill(w, h, isBlocked)
		path, cost := sc.penalizedShortestPath(start, goal, w, penalty, &refCosts)
		if path == nil {
			if want[goal] != CostUnreachable {
				t.Fatalf("no path, reference reaches the goal at %d", want[goal])
			}
			return
		}
		if path[0] != start || path[len(path)-1] != goal {
			t.Fatalf("path runs %d→%d, want %d→%d", path[0], path[len(path)-1], start, goal)
		}
		var penalized int32
		trueCost := 0
		for i := 1; i < len(path); i++ {
			if !legal(path[i-1], path[i]) {
				t.Fatalf("step %d→%d is not legal", path[i-1], path[i])
			}
			penalized += int32(refCosts.stepCost(path[i-1], path[i], w)) + penalty[path[i]]
			trueCost += refCosts.stepCost(path[i-1], path[i], w)
		}
		if penalized != want[goal] || cost != trueCost {
			t.Fatalf("penalized cost %d (reference %d), reported cost %d for a path costing %d", penalized, want[goal], cost, trueCost)
		}
	})
}

// TestASolverReusedAcrossGraphsComputesEachAfresh: the penalties, corridors and
// searches one computation leaves must not reach the next, or a graph would depend
// on what the solver computed before it.
// TestCostsFollowTheAspect: the reference keeps the 10/20/22 weighting every
// field was tuned at, a square cell costs the same on both axes, and the steering
// vectors are unit length at either.
func TestCostsFollowTheAspect(t *testing.T) {
	if refCosts.X != 10 || refCosts.Y != 20 || refCosts.Diag != 22 {
		t.Fatalf("reference costs = %d/%d/%d, want 10/20/22", refCosts.X, refCosts.Y, refCosts.Diag)
	}
	square := CostsFor(1)
	if square.X != 10 || square.Y != 10 || square.Diag != 14 {
		t.Fatalf("square costs = %d/%d/%d, want 10/10/14", square.X, square.Y, square.Diag)
	}
	for _, c := range []Costs{refCosts, square} {
		for k, u := range c.Unit {
			if d := math.Hypot(u[0], u[1]); math.Abs(d-1) > 1e-12 {
				t.Fatalf("unit vector %d at costs %d/%d has length %v", k, c.X, c.Y, d)
			}
		}
	}
}

func TestASolverReusedAcrossGraphsComputesEachAfresh(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 5))
	const w, h = 40, 24
	walls := make([]bool, w*h)
	for i := range walls {
		walls[i] = rng.IntN(4) == 0
	}
	for i := range w { // open rows and columns keep every pair reachable
		walls[1*w+i], walls[12*w+i] = false, false
	}
	for i := range h {
		walls[i*w+1], walls[i*w+38] = false, false
	}
	blocked := func(x, y int) bool { return x < 0 || y < 0 || x >= w || y >= h || walls[y*w+x] }

	fresh := ComputeRouteGraph(1, 1, 38, 22, w, h, Point, blocked)
	rs := new(RouteSolver)
	rs.Compute(20, 12, 1, 1, w, h, Point, blocked)
	reused := rs.Compute(1, 1, 38, 22, w, h, Point, blocked)
	if fresh == nil || reused == nil || len(fresh.Routes) != len(reused.Routes) {
		t.Fatalf("fresh %v, reused %v", fresh, reused)
	}
	for i := range fresh.Routes {
		a, b := fresh.Routes[i], reused.Routes[i]
		if a.TotalDistance != b.TotalDistance || a.Weight != b.Weight || !slices.Equal(a.Waypoints, b.Waypoints) {
			t.Fatalf("route %d differs: %+v vs %+v", i, a, b)
		}
		for y := range h {
			for x := range w {
				if a.Field.GetDistance(x, y) != b.Field.GetDistance(x, y) || a.Field.GetDirection(x, y) != b.Field.GetDirection(x, y) {
					t.Fatalf("route %d field differs at (%d,%d)", i, x, y)
				}
			}
		}
	}
}

// TestACorridorFieldIsTheFieldConfinedToItsCorridor: a route's field holds its
// corridor and only it — every waypoint inside, nothing impassable — and over
// those cells the distances and directions a flow field confined to them gives.
func TestACorridorFieldIsTheFieldConfinedToItsCorridor(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 9))
	const w, h = 60, 30
	walls := make([]bool, w*h)
	for i := range walls {
		walls[i] = rng.IntN(4) == 0
	}
	for i := range w { // open rows and columns keep the ends reachable
		walls[1*w+i], walls[15*w+i] = false, false
	}
	for i := range h {
		walls[i*w+1], walls[i*w+58] = false, false
	}
	blocked := func(x, y int) bool { return x < 0 || y < 0 || x >= w || y >= h || walls[y*w+x] }
	rg := ComputeRouteGraph(1, 1, 58, 15, w, h, Point, blocked)
	if rg == nil || len(rg.Routes) < 2 {
		t.Fatalf("graph %v has too few routes to compare", rg)
	}
	for ri, route := range rg.Routes {
		corridor := make(map[int]bool)
		for y := range h {
			for x := range w {
				if route.Field.GetDistance(x, y) < 0 {
					continue
				}
				if blocked(x, y) {
					t.Fatalf("route %d: (%d,%d) is a wall with a distance", ri, x, y)
				}
				corridor[y*w+x] = true
			}
		}
		if route.Field.Len() != len(corridor) {
			t.Fatalf("route %d: %d cells stored, %d read", ri, route.Field.Len(), len(corridor))
		}
		for _, wp := range route.Waypoints {
			if !corridor[wp.Y*w+wp.X] {
				t.Fatalf("route %d: waypoint %v lies outside its corridor", ri, wp)
			}
		}
		want := NewFlowField(w, h)
		want.Compute([]vmath.Point{{X: rg.TargetX, Y: rg.TargetY}}, func(x, y int) bool {
			return blocked(x, y) || !corridor[y*w+x]
		})
		for y := range h {
			for x := range w {
				if route.Field.GetDistance(x, y) != want.GetDistance(x, y) || route.Field.GetDirection(x, y) != want.GetDirection(x, y) {
					t.Fatalf("route %d (%d,%d): corridor %d/%d, confined field %d/%d", ri, x, y,
						route.Field.GetDistance(x, y), route.Field.GetDirection(x, y), want.GetDistance(x, y), want.GetDirection(x, y))
				}
			}
		}
	}
}

// TestAResetCacheIsANewCache: a cache a removed group leaves, reset for the next
// group, falls due and computes exactly as a freshly built one, so recycling moves
// no instance out of the D-17 phase.
func TestAResetCacheIsANewCache(t *testing.T) {
	walls := make([]bool, 8*6)
	walls[2*8+3] = true
	blocked := func(x, y int) bool { return x < 0 || y < 0 || x >= 8 || y >= 6 || walls[y*8+x] }
	g := new(SearchGrid)
	g.Fill(8, 6, blocked)
	used := NewFlowFieldCache(8, 6, 3, 4, 6)
	used.Lazy = true
	goal := []vmath.Point{{X: 1, Y: 1}}
	for range 5 {
		used.Update(goal, g)
		goal[0].X++
	}
	used.Materialize(g)
	used.Reset(8, 6)
	fresh := NewFlowFieldCache(8, 6, 3, 4, 6)
	fresh.Lazy = true
	goal = []vmath.Point{{X: 6, Y: 4}}
	for tick := range 12 {
		if used.Update(goal, g) != fresh.Update(goal, g) ||
			used.TicksSinceCompute != fresh.TicksSinceCompute || used.Computed() != fresh.Computed() {
			t.Fatalf("tick %d: the reset cache left the fresh cache's phase", tick)
		}
		if tick%4 == 3 {
			goal[0].X--
		}
	}
	used.Materialize(g)
	fresh.Materialize(g)
	for i := range 48 {
		if x, y := i%8, i/8; used.GetDistance(x, y) != fresh.GetDistance(x, y) {
			t.Fatalf("distance at (%d,%d) is %d, fresh %d", x, y, used.GetDistance(x, y), fresh.GetDistance(x, y))
		}
	}
}
