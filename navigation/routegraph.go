package navigation

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
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
	TotalDistance int     // Weighted path distance (CostX/CostY/CostDiagonal)
	Weight        float64 // Normalized sampling weight (inverse distance, floored)
}

// RouteGraph holds all viable routes between a source-target pair
type RouteGraph struct {
	Routes           []Route
	SourceX, SourceY int
	TargetX, TargetY int

	FootprintW, FootprintH int // Entity footprint used for passability
	HeaderOffX, HeaderOffY int
}

// routeCellPenalty per prior-corridor cell; a ~100-cell prior path accrues
// penalty far above routeTolerance, so any in-tolerance disjoint path wins next pass
const routeCellPenalty = CostDiagonal

// ComputeRouteGraph computes routes using iterative penalized Dijkstra
func ComputeRouteGraph(
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

	rg := &RouteGraph{
		SourceX: sourceX, SourceY: sourceY,
		TargetX: targetX, TargetY: targetY,
		FootprintW: footprintW, FootprintH: footprintH,
		HeaderOffX: headerOffX, HeaderOffY: headerOffY,
	}

	size := mapW * mapH
	tolerance := routeTolerance(mapW, mapH)
	penalty := make([]int, size)
	usedDilated := make([]bool, size)
	var acceptedPaths [][]int
	optDist := -1

	maxAttempts := parameter.RouteGraphMaxRoutes * 2
	for attempt := 0; attempt < maxAttempts && len(rg.Routes) < parameter.RouteGraphMaxRoutes; attempt++ {
		path, trueCost := penalizedShortestPath(sourceX, sourceY, targetX, targetY, mapW, mapH, isBlocked, penalty)
		if path == nil {
			break
		}
		if optDist < 0 {
			optDist = trueCost // first pass: zero penalties → true optimum
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
			shared*100 <= len(path)*parameter.RouteGraphMaxOverlapPct

		// Penalize regardless of acceptance to drive out near-duplicates
		dilate(path, parameter.RouteCorridorRadius, mapW, mapH, isBlocked, func(idx int) {
			penalty[idx] += routeCellPenalty
		})

		if !distinct {
			continue
		}

		dilate(path, parameter.RouteCorridorRadius, mapW, mapH, isBlocked, func(idx int) {
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
	computePathFields(rg, acceptedPaths, mapW, mapH, isBlocked)
	return rg
}

// penalizedShortestPath is Dijkstra over (dirCost + penalty), predecessor extraction, corner-cut rule kept
func penalizedShortestPath(
	sx, sy, tx, ty, mapW, mapH int,
	isBlocked WallChecker,
	penalty []int,
) ([]int, int) {
	size := mapW * mapH
	dist := make([]int, size)
	prev := make([]int32, size)
	for i := range dist {
		dist[i] = CostUnreachable
		prev[i] = -1
	}

	start := sy*mapW + sx
	goal := ty*mapW + tx
	dist[start] = 0
	h := make(minHeap, 0, size/8)
	h.push(heapEntry{idx: start, dist: 0})

	for len(h) > 0 {
		e := h.pop()
		if e.dist > dist[e.idx] {
			continue
		}
		if e.idx == goal {
			break
		}
		cx, cy := e.idx%mapW, e.idx/mapW
		for d := range int8(DirCount) {
			dx, dy := DirVectors[d][0], DirVectors[d][1]
			nx, ny := cx+dx, cy+dy
			if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH || isBlocked(nx, ny) {
				continue
			}
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}
			nIdx := ny*mapW + nx
			nd := e.dist + dirCosts[d] + penalty[nIdx]
			if nd < dist[nIdx] {
				dist[nIdx] = nd
				prev[nIdx] = int32(e.idx)
				h.push(heapEntry{idx: nIdx, dist: nd})
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
		trueCost += stepCost(p, cur, mapW)
		cur = p
	}
	path = append(path, start)
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, trueCost
}

// stepCost returns the aspect-weighted cost of one step between adjacent flat indices
func stepCost(from, to, mapW int) int {
	dx := to%mapW - from%mapW
	dy := to/mapW - from/mapW
	switch {
	case dx != 0 && dy != 0:
		return CostDiagonal
	case dx != 0:
		return CostX
	default:
		return CostY
	}
}

// dilate performs BFS dilation over passable cells to radius; fn invoked once per unique cell
func dilate(path []int, radius, mapW, mapH int, isBlocked WallChecker, fn func(idx int)) {
	seen := make([]bool, mapW*mapH)
	frontier := make([]int, 0, len(path)*2)
	for _, idx := range path {
		if !seen[idx] {
			seen[idx] = true
			fn(idx)
			frontier = append(frontier, idx)
		}
	}
	for depth := 0; depth < radius && len(frontier) > 0; depth++ {
		var next []int
		for _, idx := range frontier {
			cx, cy := idx%mapW, idx/mapW
			for d := int8(0); d < DirCount; d++ {
				nx, ny := cx+DirVectors[d][0], cy+DirVectors[d][1]
				if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH || isBlocked(nx, ny) {
					continue
				}
				nIdx := ny*mapW + nx
				if !seen[nIdx] {
					seen[nIdx] = true
					fn(nIdx)
					next = append(next, nIdx)
				}
			}
		}
		frontier = next
	}
}

// decimateWaypoints returns Waypoints as decimated path samples
func decimateWaypoints(path []int, mapW int) []Waypoint {
	stride := parameter.RouteGraphWaypointStride
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
func computePathFields(rg *RouteGraph, paths [][]int, mapW, mapH int, isBlocked WallChecker) {
	targets := []core.Point{{X: rg.TargetX, Y: rg.TargetY}}
	size := mapW * mapH

	for ri, path := range paths {
		allowed := make([]bool, size)
		dilate(path, parameter.RouteCorridorRadius, mapW, mapH, isBlocked, func(idx int) {
			allowed[idx] = true
		})

		routeBlocked := func(x, y int) bool {
			if isBlocked(x, y) {
				return true
			}
			return !allowed[y*mapW+x]
		}

		field := NewFlowField(mapW, mapH)
		field.Compute(targets, routeBlocked)
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
		if w < parameter.RouteGraphMinWeightFloor {
			w = parameter.RouteGraphMinWeightFloor
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

// routeTolerance returns additive distance tolerance for band inclusion
// Formula: 0.5 * max(mapW, mapH) * CostDiagonal
func routeTolerance(mapW, mapH int) int {
	m := max(mapW, mapH)
	return (m * CostDiagonal) / 2
}
