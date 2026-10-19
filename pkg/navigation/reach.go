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
	live    []bool // goals the running pass may still settle
}

// reachSeed is one goal's seed at a cell: reaching the cell measures the goal at
// the cell's distance plus gap. next chains the seeds of one cell.
type reachSeed struct {
	goal, gap, next int32
}

// Measure writes to out, per goal, the distance from (x, y), or CostUnreachable.
// The search stops once every goal that can be measured is settled.
func (r *Reach) Measure(g *SearchGrid, c *Costs, x, y int, goals []vmath.Point, out []int32) {
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
	if !seeded {
		return
	}
	if !g.Blocked(x, y) {
		r.starts = append(r.starts[:0], int32(y*g.w+x))
		if r.search(g, c, 0, out) {
			return
		}
	}
	r.starts = r.starts[:0]
	for _, d := range DirVectors {
		if nx, ny := x+d[0], y+d[1]; !g.Blocked(nx, ny) {
			r.starts = append(r.starts, int32(ny*g.w+nx))
		}
	}
	if len(r.starts) > 0 {
		r.search(g, c, c.Diag, out)
	}
}

// seed records that reaching idx measures goal at the distance plus gap.
func (r *Reach) seed(idx, goal, gap int32) {
	if r.seedGen[idx] != r.pass {
		r.seedGen[idx], r.seedAt[idx] = r.pass, -1
	}
	r.seeds = append(r.seeds, reachSeed{goal: goal, gap: gap, next: r.seedAt[idx]})
	r.seedAt[idx] = int32(len(r.seeds) - 1)
}

// search runs Dijkstra from starts at startCost, measuring every live goal still
// unmeasured, and reports whether none is left.
func (r *Reach) search(g *SearchGrid, c *Costs, startCost int32, out []int32) bool {
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
		return true
	}
	g.reset(c.Diag)
	for _, s := range r.starts {
		r.dist[s], r.seen[s] = startCost, r.gen
		g.push(s, startCost)
	}
	bound := int32(CostUnreachable)
	for idx, d, ok := g.pop(); ok; idx, d, ok = g.pop() {
		if r.dist[idx] != d {
			continue
		}
		if d >= bound {
			break // every live goal is measured below this distance
		}
		if r.seedGen[idx] == r.pass {
			for s := r.seedAt[idx]; s >= 0; s = r.seeds[s].next {
				j, cost := r.seeds[s].goal, d+r.seeds[s].gap
				if !r.live[j] || cost >= out[j] {
					continue
				}
				if out[j] == CostUnreachable {
					pending--
				}
				out[j] = cost
			}
			if pending == 0 {
				bound = 0
				for j := range out {
					if r.live[j] {
						bound = max(bound, out[j])
					}
				}
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
	return pending == 0
}

// grow is s at n elements, keeping what it holds where it already has the room
func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
