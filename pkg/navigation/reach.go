package navigation

import (
	"math/bits"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Reach measures, from one cell, the weighted distance to each of several goals
// over one grid: what a field seeded at a goal would report at the cell, since a
// step costs the same both ways, a blocked goal counting from its first passable
// ring at the ring's gap costs. A blocked cell, or a goal the cell cannot reach,
// measures from the cell's passable neighbours a diagonal away, as a field read
// there falls back to. Scratch is kept between calls.
type Reach struct {
	dist    []int32
	seen    []uint32 // generation a cell's distance was written at
	gen     uint32
	seeds   []reachSeed
	seedAt  []int32 // per cell, its first seed while seedGen holds pass
	seedGen []uint32
	pass    uint32
	starts  []int32
	live    []bool  // goals the running pass may still settle
	out     []int32 // Nearest's distances
	held    int     // the goal Nearest keeps within num/den of the nearest, or -1
	num     int64
	den     int64
}

// reachSeed is one goal's seed at a cell: reaching the cell measures the goal at
// the cell's distance plus gap. next chains the seeds of one cell.
type reachSeed struct {
	goal, gap, next int32
}

// Measure writes to out, per goal, the distance from (x, y), or CostUnreachable.
// The search stops once every goal that can be measured is settled.
func (r *Reach) Measure(g *SearchGrid, c *Costs, x, y int, goals []vmath.Point, out []int32) {
	if !r.seedGoals(g, c, goals, out) {
		return
	}
	if !g.Blocked(x, y) {
		r.starts = append(r.starts[:0], int32(y*g.w+x))
		if r.search(g, c, 0, out, CostUnreachable, false) == 0 {
			return
		}
	}
	r.around(g, c, x, y, out, false)
}

// Nearest is the goal Measure would measure nearest from (x, y), the first of
// equals, or -1 when it measures none; held, a goal or -1, is chosen instead
// while its distance is at most num/den of the nearest's. The search stops once
// that choice is settled, short of the goals it cannot change.
func (r *Reach) Nearest(g *SearchGrid, c *Costs, x, y int, goals []vmath.Point, held int, num, den int32) int {
	r.out = grow(r.out, len(goals))
	out := r.out
	r.held, r.num, r.den = held, int64(num), int64(den)
	if !r.seedGoals(g, c, goals, out) {
		return -1
	}
	if g.Blocked(x, y) {
		r.around(g, c, x, y, out, true)
		return r.choice(out)
	}
	r.starts = append(r.starts[:0], int32(y*g.w+x))
	if r.search(g, c, 0, out, CostUnreachable, true) > 0 && r.pinched(g, c, x, y, out) {
		r.Measure(g, c, x, y, goals, out)
	}
	return r.choice(out)
}

// seedGoals readies a pass over goals, out reset, and reports whether any goal
// seeded a cell.
func (r *Reach) seedGoals(g *SearchGrid, c *Costs, goals []vmath.Point, out []int32) bool {
	n := g.w * g.h
	r.dist, r.seen = grow(r.dist, n), grow(r.seen, n)
	r.seedAt, r.seedGen = grow(r.seedAt, n), grow(r.seedGen, n)
	r.live = grow(r.live, len(goals))
	if r.pass++; r.pass == 0 {
		clear(r.seedGen[:cap(r.seedGen)])
		r.pass = 1
	}
	r.seeds = r.seeds[:0]
	seeded := false
	for j, goal := range goals {
		out[j], r.live[j] = CostUnreachable, false
		if goal.X < 0 || goal.Y < 0 || goal.X >= g.w || goal.Y >= g.h {
			continue
		}
		if !g.Blocked(goal.X, goal.Y) {
			r.seed(int32(goal.Y*g.w+goal.X), int32(j), 0)
			r.live[j], seeded = true, true
			continue
		}
		seedRing(g, c, goal.X, goal.Y, func(idx, cost int32) {
			r.seed(idx, int32(j), cost)
			r.live[j], seeded = true, true
		})
	}
	return seeded
}

// seed records that reaching idx measures goal at the distance plus gap.
func (r *Reach) seed(idx, goal, gap int32) {
	if r.seedGen[idx] != r.pass {
		r.seedGen[idx], r.seedAt[idx] = r.pass, -1
	}
	r.seeds = append(r.seeds, reachSeed{goal: goal, gap: gap, next: r.seedAt[idx]})
	r.seedAt[idx] = int32(len(r.seeds) - 1)
}

// around searches from the passable neighbours of (x, y), each a diagonal away.
func (r *Reach) around(g *SearchGrid, c *Costs, x, y int, out []int32, choosing bool) {
	r.starts = r.starts[:0]
	for _, d := range DirVectors {
		if nx, ny := x+d[0], y+d[1]; !g.Blocked(nx, ny) {
			r.starts = append(r.starts, int32(ny*g.w+nx))
		}
	}
	if len(r.starts) > 0 {
		r.search(g, c, c.Diag, out, CostUnreachable, choosing)
	}
}

// pinched reports whether a goal the cell's search left unmeasured could change
// Nearest's choice from past a corner: only a diagonal neighbour with both corners
// blocked can lie beyond what the cell reaches, so a goal measured from those
// within the choice's distances sends the choice to a whole Measure.
func (r *Reach) pinched(g *SearchGrid, c *Costs, x, y int, out []int32) bool {
	r.starts = r.starts[:0]
	for k := DirNE; k < DirCount; k += 2 {
		dx, dy := DirVectors[k][0], DirVectors[k][1]
		if !g.Blocked(x+dx, y+dy) && g.Blocked(x+dx, y) && g.Blocked(x, y+dy) {
			r.starts = append(r.starts, int32((y+dy)*g.w+x+dx))
		}
	}
	if len(r.starts) == 0 {
		return false
	}
	limit := int32(CostUnreachable - 1)
	if near := nearest(out); near < CostUnreachable {
		limit = near
		if h := r.held; h >= 0 && r.live[h] && out[h] == CostUnreachable {
			limit = max(near, r.keepLimit(near))
		}
	}
	r.search(g, c, c.Diag, out, limit+1, false)
	for j, d := range out {
		if r.live[j] && d <= limit {
			return true
		}
	}
	return false
}

// search runs Dijkstra from starts at startCost, measuring every live goal still
// unmeasured short of limit, and reports how many are left. Choosing, it stops
// once Nearest's choice can no longer change.
func (r *Reach) search(g *SearchGrid, c *Costs, startCost int32, out []int32, limit int32, choosing bool) int {
	if r.gen++; r.gen == 0 {
		clear(r.seen[:cap(r.seen)])
		r.gen = 1
	}
	pending := 0
	for j := range out {
		if r.live[j] = r.live[j] && out[j] == CostUnreachable; r.live[j] {
			pending++
		}
	}
	if pending == 0 {
		return 0
	}
	g.reset(0, c.Diag)
	for _, s := range r.starts {
		r.dist[s], r.seen[s] = startCost, r.gen
		g.push(s, startCost)
	}
	stop := r.stopAt(out, pending, limit, choosing)
	for idx, d, ok := g.pop(); ok; idx, d, ok = g.pop() {
		if r.dist[idx] != d {
			continue
		}
		if d >= stop {
			break // every goal the search answers for is measured below this distance
		}
		if r.seedGen[idx] == r.pass {
			measured := false
			for s := r.seedAt[idx]; s >= 0; s = r.seeds[s].next {
				j, cost := r.seeds[s].goal, d+r.seeds[s].gap
				if !r.live[j] || cost >= out[j] {
					continue
				}
				if out[j] == CostUnreachable {
					pending--
				}
				out[j], measured = cost, true
			}
			if measured {
				stop = r.stopAt(out, pending, limit, choosing)
			}
		}
		for m := g.open[idx]; m != 0; m &= m - 1 {
			k := bits.TrailingZeros8(m)
			nIdx := idx + g.step[k]
			if nd := d + c.dir[k]; r.seen[nIdx] != r.gen || nd < r.dist[nIdx] {
				r.dist[nIdx], r.seen[nIdx] = nd, r.gen
				g.push(nIdx, nd)
			}
		}
	}
	return pending
}

// stopAt is the distance a search may stop at: limit, past the farthest live
// goal once all are measured, and choosing, past the nearest goal so every equal
// is measured and past held's distance or the most it may be kept at.
func (r *Reach) stopAt(out []int32, pending int, limit int32, choosing bool) int32 {
	stop := limit
	if pending == 0 {
		far := int32(0)
		for j, d := range out {
			if r.live[j] {
				far = max(far, d)
			}
		}
		stop = min(stop, far)
	}
	if !choosing {
		return stop
	}
	if near := nearest(out); near < CostUnreachable {
		at := near + 1
		if h := r.held; h >= 0 && r.live[h] {
			at = max(at, min(out[h], r.keepLimit(near)+1))
		}
		stop = min(stop, at)
	}
	return stop
}

// keepLimit is the farthest held may be and be kept over a nearest goal at near.
func (r *Reach) keepLimit(near int32) int32 {
	return int32(min(int64(near)*r.num/r.den, CostUnreachable-1))
}

// choice is Nearest's answer from the distances its searches left.
func (r *Reach) choice(out []int32) int {
	best := -1
	for j, d := range out {
		if d < CostUnreachable && (best < 0 || d < out[best]) {
			best = j
		}
	}
	if h := r.held; h >= 0 && best >= 0 && out[h] < CostUnreachable && int64(out[h])*r.den <= int64(out[best])*r.num {
		return h
	}
	return best
}

// nearest is the least distance in out
func nearest(out []int32) int32 {
	near := int32(CostUnreachable)
	for _, d := range out {
		near = min(near, d)
	}
	return near
}

// grow is s at n elements, keeping what it holds where it already has the room
func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
