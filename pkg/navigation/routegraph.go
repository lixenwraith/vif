package navigation

import (
	"math/bits"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Route graph tuning. It lives here rather than in the game's parameter package
// so this leaf stays free of internal imports.
// Route Graph — Computation
const (
	// RouteGraphMinWeightFloor ensures every route gets minimum traffic share
	RouteGraphMinWeightFloor = 0.05

	// RouteGraphMaxRoutes caps accepted routes per graph
	RouteGraphMaxRoutes = 8

	// RouteGraphExtraAttempts is the rejected-candidate budget above the route cap
	RouteGraphExtraAttempts = 8

	// RouteTolerancePct caps route length as a percentage above the optimum
	RouteTolerancePct = 50

	// RouteGraphMaxOverlapPct: reject route candidate sharing more than this
	// percentage of its cells with already-accepted routes (dilated)
	RouteGraphMaxOverlapPct = 70

	// RouteCorridorRadius: BFS dilation (cells) around a route path; sets
	// penalty spread and per-route flow-field corridor width. Raise if
	// knockback deaths outside corridors produce excessive zero-fitness noise
	RouteCorridorRadius = 2

	// RouteGraphWaypointStride: path decimation interval for Route.Waypoints
	RouteGraphWaypointStride = 8
)

// Waypoint is a navigation decision point along a route
type Waypoint struct {
	X, Y int
}

// Route is a distinct path from source to target through the maze
type Route struct {
	Field         *FlowField // Per-route constrained flow field
	Waypoints     []Waypoint
	ID            int     // Route index within graph; bandit arm index
	TotalDistance int     // Weighted path distance (Costs)
	Weight        float64 // Normalized sampling weight (inverse distance, floored)
}

// RouteGraph holds all viable routes between a source-target pair
type RouteGraph struct {
	Routes           []Route
	SourceX, SourceY int
	TargetX, TargetY int

	FootprintW, FootprintH int // Entity footprint used for passability
	HeaderOffX, HeaderOffY int

	Grid uint64 // Caller's generation of the passability the routes were computed on
}

// RouteSolver computes route graphs on scratch kept from one computation to the
// next, so a caller rebuilding graphs as a game runs allocates only the graphs
type RouteSolver struct {
	Costs       Costs // the aspect routes are weighted at; zero is the reference
	sc          routeScratch
	fieldSearch SearchGrid // the grid every route's corridor field is computed on
	penalty     []int32
	usedDilated []bool
	allowed     []uint32
}

// ComputeRouteGraph computes routes using iterative penalized Dijkstra
func ComputeRouteGraph(
	sourceX, sourceY, targetX, targetY int,
	mapW, mapH int,
	footprintW, footprintH, headerOffX, headerOffY int,
	isBlocked WallChecker,
) *RouteGraph {
	return new(RouteSolver).Compute(sourceX, sourceY, targetX, targetY, mapW, mapH,
		footprintW, footprintH, headerOffX, headerOffY, isBlocked)
}

// Compute is ComputeRouteGraph on the solver's scratch
func (rs *RouteSolver) Compute(
	sourceX, sourceY, targetX, targetY int,
	mapW, mapH int,
	footprintW, footprintH, headerOffX, headerOffY int,
	isBlocked WallChecker,
) *RouteGraph {
	if mapW <= 0 || mapH <= 0 {
		return nil
	}
	if sourceX < 0 || sourceY < 0 || sourceX >= mapW || sourceY >= mapH ||
		targetX < 0 || targetY < 0 || targetX >= mapW || targetY >= mapH {
		return nil
	}
	if isBlocked(sourceX, sourceY) || isBlocked(targetX, targetY) {
		return nil
	}
	if rs.Costs.X == 0 {
		rs.Costs = refCosts
	}

	rg := &RouteGraph{
		SourceX: sourceX, SourceY: sourceY,
		TargetX: targetX, TargetY: targetY,
		FootprintW: footprintW, FootprintH: footprintH,
		HeaderOffX: headerOffX, HeaderOffY: headerOffY,
	}

	size := mapW * mapH
	sc := &rs.sc
	sc.grow(size)
	sc.search.Fill(mapW, mapH, isBlocked)
	tolerance := 0
	rs.penalty, rs.usedDilated = scratch(rs.penalty, size), scratch(rs.usedDilated, size)
	penalty, usedDilated := rs.penalty, rs.usedDilated
	var acceptedPaths [][]int
	optDist := -1

	maxAttempts := RouteGraphMaxRoutes + RouteGraphExtraAttempts
	for attempt := 0; attempt < maxAttempts && len(rg.Routes) < RouteGraphMaxRoutes; attempt++ {
		path, trueCost := sc.penalizedShortestPath(sourceY*mapW+sourceX, targetY*mapW+targetX, mapW, penalty, &rs.Costs)
		if path == nil {
			break
		}
		if optDist < 0 {
			optDist = trueCost // first pass: zero penalties → true optimum
			tolerance = routeTolerance(optDist)
		}
		// Penalized-optimal whose TRUE cost exceeds the band ⇒ no in-tolerance
		// disjoint (zero-penalty) candidate remains
		if trueCost > optDist+tolerance {
			break
		}

		shared := 0
		for _, idx := range path {
			if usedDilated[idx] {
				shared++
			}
		}
		distinct := len(rg.Routes) == 0 ||
			shared*100 <= len(path)*RouteGraphMaxOverlapPct

		// Penalize regardless of acceptance to drive out near-duplicates: a prior
		// corridor's cells cost a diagonal each, far above the tolerance over ~100 cells
		sc.dilate(path, RouteCorridorRadius, func(idx int) {
			penalty[idx] += rs.Costs.Diag
		})

		if !distinct {
			continue
		}

		sc.dilate(path, RouteCorridorRadius, func(idx int) {
			usedDilated[idx] = true
		})

		rg.Routes = append(rg.Routes, Route{
			ID:            len(rg.Routes),
			TotalDistance: trueCost,
			Waypoints:     decimateWaypoints(path, mapW),
		})
		acceptedPaths = append(acceptedPaths, path)
	}

	if len(rg.Routes) == 0 {
		return nil
	}

	computeRouteWeights(rg)
	rs.computePathFields(rg, acceptedPaths, mapW, mapH, isBlocked)
	return rg
}

// stepCost returns the weighted cost of one step between adjacent flat indices
func (c *Costs) stepCost(from, to, mapW int) int {
	dx := to%mapW - from%mapW
	dy := to/mapW - from/mapW
	switch {
	case dx != 0 && dy != 0:
		return int(c.Diag)
	case dx != 0:
		return int(c.X)
	default:
		return int(c.Y)
	}
}

// decimateWaypoints returns Waypoints as decimated path samples
func decimateWaypoints(path []int, mapW int) []Waypoint {
	stride := RouteGraphWaypointStride
	wps := make([]Waypoint, 0, len(path)/stride+2)
	for i := 0; i < len(path); i += stride {
		wps = append(wps, Waypoint{X: path[i] % mapW, Y: path[i] / mapW})
	}
	last := path[len(path)-1]
	if wps[len(wps)-1].X != last%mapW || wps[len(wps)-1].Y != last/mapW {
		wps = append(wps, Waypoint{X: last % mapW, Y: last / mapW})
	}
	return wps
}

// computePathFields computes per-route flow fields constrained to each route's
// dilated corridor; every accepted route receives a valid Field
func (rs *RouteSolver) computePathFields(rg *RouteGraph, paths [][]int, mapW, mapH int, isBlocked WallChecker) {
	sc := &rs.sc
	targets := []vmath.Point{{X: rg.TargetX, Y: rg.TargetY}}
	rs.allowed = scratch(rs.allowed, mapW*mapH)
	allowed := rs.allowed
	var gen uint32

	for ri, path := range paths {
		gen++
		g := gen
		sc.dilate(path, RouteCorridorRadius, func(idx int) {
			allowed[idx] = g
		})

		routeBlocked := func(x, y int) bool {
			return isBlocked(x, y) || allowed[y*mapW+x] != g
		}

		field := NewFlowField(mapW, mapH)
		field.Costs = rs.Costs
		rs.fieldSearch.Fill(mapW, mapH, routeBlocked)
		field.ComputeOn(&rs.fieldSearch, targets)
		rg.Routes[ri].Field = field
	}
}

// computeRouteWeights assigns inverse-distance weights with minimum floor, normalized to sum=1.0
func computeRouteWeights(rg *RouteGraph) {
	n := len(rg.Routes)
	if n == 0 {
		return
	}

	maxDist := 0
	for i := range rg.Routes {
		if rg.Routes[i].TotalDistance > maxDist {
			maxDist = rg.Routes[i].TotalDistance
		}
	}

	if maxDist == 0 {
		w := 1.0 / float64(n)
		for i := range rg.Routes {
			rg.Routes[i].Weight = w
		}
		return
	}

	total := 0.0
	for i := range rg.Routes {
		d := rg.Routes[i].TotalDistance
		if d <= 0 {
			d = 1
		}
		w := float64(maxDist) / float64(d)
		if w < RouteGraphMinWeightFloor {
			w = RouteGraphMinWeightFloor
		}
		rg.Routes[i].Weight = w
		total += w
	}

	if total > 0 {
		for i := range rg.Routes {
			rg.Routes[i].Weight /= total
		}
	}
}

// routeTolerance returns additive distance tolerance relative to the optimum
func routeTolerance(optDist int) int {
	return (optDist * RouteTolerancePct) / 100
}

// routeScratch holds the buffers of a route graph computation, over one
// passability its search reads for every route
type routeScratch struct {
	search         SearchGrid
	dist           []int32
	prev           []int32
	stamp          []uint32 // dilate visited marks, generation-stamped
	frontA, frontB []int
	gen            uint32
}

// grow sizes the scratch to a field of size cells, keeping its storage
func (sc *routeScratch) grow(size int) {
	sc.dist, sc.prev, sc.stamp = scratch(sc.dist, size), scratch(sc.prev, size), scratch(sc.stamp, size)
}

// scratch is s at n zeroed elements, reallocated only past its capacity
func scratch[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// dilate performs BFS dilation over passable cells to radius; fn invoked once per unique cell
func (sc *routeScratch) dilate(path []int, radius int, fn func(idx int)) {
	sc.gen++
	if sc.gen == 0 {
		clear(sc.stamp)
		sc.gen = 1
	}

	g := &sc.search
	cur, next := sc.frontA[:0], sc.frontB[:0]
	for _, idx := range path {
		if sc.stamp[idx] != sc.gen {
			sc.stamp[idx] = sc.gen
			fn(idx)
			cur = append(cur, idx)
		}
	}

	for depth := 0; depth < radius && len(cur) > 0; depth++ {
		next = next[:0]
		for _, idx := range cur {
			p := g.pad(int32(idx))
			for k := range DirCount {
				if g.blocked[p+g.padStep[k]] {
					continue
				}
				nIdx := idx + int(g.step[k])
				if sc.stamp[nIdx] != sc.gen {
					sc.stamp[nIdx] = sc.gen
					fn(nIdx)
					next = append(next, nIdx)
				}
			}
		}
		cur, next = next, cur
	}
	sc.frontA, sc.frontB = cur[:0], next[:0]
}

// penalizedShortestPath is Dijkstra from start to goal over dirCost plus the penalty
// of the cell entered, returning the path and its unpenalized cost
func (sc *routeScratch) penalizedShortestPath(start, goal, mapW int, penalty []int32, c *Costs) ([]int, int) {
	dist, prev := sc.dist, sc.prev
	var most int32
	for i := range dist {
		dist[i] = CostUnreachable
		most = max(most, penalty[i])
	}
	// prev needs no reset: it is written on every dist write, and the goal→start
	// walk only visits cells relaxed this pass (start terminates the walk)

	g := &sc.search
	g.reset(c.Diag + most)
	dist[start] = 0
	g.push(int32(start), 0)

	for at, d, ok := g.pop(); ok; at, d, ok = g.pop() {
		if d != dist[at] {
			continue
		}
		if int(at) == goal {
			break
		}
		for m := g.open[at]; m != 0; m &= m - 1 {
			k := bits.TrailingZeros8(m)
			nIdx := at + g.step[k]
			if nd := d + c.dir[k] + penalty[nIdx]; nd < dist[nIdx] {
				dist[nIdx] = nd
				prev[nIdx] = at
				g.push(nIdx, nd)
			}
		}
	}

	if dist[goal] >= CostUnreachable {
		return nil, 0
	}

	var path []int
	trueCost := 0
	for cur := goal; cur != start; {
		path = append(path, cur)
		p := int(prev[cur])
		trueCost += c.stepCost(p, cur, mapW)
		cur = p
	}
	path = append(path, start)
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, trueCost
}
