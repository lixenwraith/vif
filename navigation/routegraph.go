package navigation

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// BranchPoint marks a cell where multiple viable route choices diverge
type BranchPoint struct {
	X, Y        int
	ChoiceCount int // Number of divergent paths
}

// Waypoint is a navigation decision point along a route
type Waypoint struct {
	X, Y          int
	BranchPointID int // Index into RouteGraph.BranchPoints, -1 if source/target
	ChoiceIndex   int // Which branch taken at this point
}

// Route is a distinct path from source to target through the maze
type Route struct {
	ID            int // Route index (used as discrete gene value)
	Waypoints     []Waypoint
	TotalDistance int        // Weighted path distance (CostX/CostY/CostDiagonal)
	Weight        float64    // Normalized sampling weight (inverse distance, floored)
	Field         *FlowField // Per-route constrained flow field
}

// RouteGraph holds all viable routes between a source-target pair
type RouteGraph struct {
	SourceX, SourceY int
	TargetX, TargetY int

	FootprintW, FootprintH int // Entity footprint used for passability
	HeaderOffX, HeaderOffY int

	Routes       []Route
	BranchPoints []BranchPoint
}

// --- Internal types for contracted graph ---

type rgNode struct {
	repX, repY int
	edges      []rgEdge
}

type rgEdge struct {
	toNode      int
	distance    int
	boundaryIdx int // flat index of source component boundary cell
	corridorIdx int // flat index of first corridor cell, -1 for direct adjacency
}

type rgPath struct {
	steps     []rgPathStep
	totalDist int
}

type rgPathStep struct {
	nodeID    int
	edgeIndex int
}

// ComputeRouteGraph computes all viable distinct routes between source and target
// isBlocked: passability checker (CompositePassability.IsBlocked for composite entities)
// mapW, mapH: grid dimensions
// Returns nil if source/target are blocked, unreachable, or no routes exist
func ComputeRouteGraph(
	sourceX, sourceY, targetX, targetY int,
	mapW, mapH int,
	footprintW, footprintH, headerOffX, headerOffY int,
	isBlocked WallChecker,
) *RouteGraph {
	if mapW <= 0 || mapH <= 0 {
		return nil
	}
	if sourceX < 0 || sourceY < 0 || sourceX >= mapW || sourceY >= mapH {
		return nil
	}
	if targetX < 0 || targetY < 0 || targetX >= mapW || targetY >= mapH {
		return nil
	}
	if isBlocked(sourceX, sourceY) || isBlocked(targetX, targetY) {
		return nil
	}

	size := mapW * mapH
	sourceIdx := sourceY*mapW + sourceX
	targetIdx := targetY*mapW + targetX

	// Bidirectional Dijkstra
	distS := computeDistMap(sourceX, sourceY, mapW, mapH, isBlocked)
	distT := computeDistMap(targetX, targetY, mapW, mapH, isBlocked)

	optDist := distS[targetIdx]
	if optDist >= costUnreachable {
		return nil
	}

	// Tolerance band: cells on paths within tolerance of optimal
	tolerance := routeTolerance(mapW, mapH)
	maxPathDist := optDist + tolerance

	band := make([]bool, size)
	for i := 0; i < size; i++ {
		if distS[i] < costUnreachable && distT[i] < costUnreachable && distS[i]+distT[i] <= maxPathDist {
			band[i] = true
		}
	}

	// Band neighbor counts for junction classification
	nbrCount := make([]int, size)
	for i := 0; i < size; i++ {
		if band[i] {
			nbrCount[i] = countBandNeighbors(i, mapW, mapH, band, isBlocked)
		}
	}

	// Junction classification + connected component flood fill
	nodeID, numNodes, sourceNode, targetNode := buildComponents(
		band, nbrCount, mapW, mapH, isBlocked, sourceIdx, targetIdx,
	)
	if sourceNode < 0 || targetNode < 0 {
		return nil
	}

	rg := &RouteGraph{
		SourceX:    sourceX,
		SourceY:    sourceY,
		TargetX:    targetX,
		TargetY:    targetY,
		FootprintW: footprintW,
		FootprintH: footprintH,
		HeaderOffX: headerOffX,
		HeaderOffY: headerOffY,
	}

	// Same component: single direct route
	if sourceNode == targetNode {
		rg.Routes = []Route{{
			ID: 0,
			Waypoints: []Waypoint{
				{X: sourceX, Y: sourceY, BranchPointID: -1},
				{X: targetX, Y: targetY, BranchPointID: -1},
			},
			TotalDistance: optDist,
			Weight:        1.0,
		}}
		return rg
	}

	// Contracted graph: junction components as nodes, corridors as edges
	graph := buildContractedGraph(nodeID, numNodes, band, mapW, mapH, isBlocked)

	// All simple source→target paths through contracted graph
	paths := enumerateRGPaths(graph, sourceNode, targetNode)
	if len(paths) == 0 {
		return nil
	}

	assembleRoutes(rg, graph, paths)
	computeRouteWeights(rg)
	computeRouteFields(rg, graph, paths, nodeID, band, mapW, mapH, targetNode, isBlocked)
	return rg
}

// --- Dijkstra distance map ---

// computeDistMap runs single-source weighted Dijkstra with aspect-ratio costs
// Returns per-cell distance; costUnreachable for blocked/unreachable cells
func computeDistMap(startX, startY, mapW, mapH int, isBlocked WallChecker) []int {
	size := mapW * mapH
	dist := make([]int, size)
	for i := range dist {
		dist[i] = costUnreachable
	}

	if isBlocked(startX, startY) {
		return dist
	}

	startIdx := startY*mapW + startX
	dist[startIdx] = 0
	h := make(minHeap, 0, size/8)
	h.push(heapEntry{idx: startIdx, dist: 0})

	for len(h) > 0 {
		e := h.pop()
		if e.dist > dist[e.idx] {
			continue
		}

		cx := e.idx % mapW
		cy := e.idx / mapW

		for d := int8(0); d < DirCount; d++ {
			nx := cx + DirVectors[d][0]
			ny := cy + DirVectors[d][1]
			if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH || isBlocked(nx, ny) {
				continue
			}
			dx, dy := DirVectors[d][0], DirVectors[d][1]
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}
			nIdx := ny*mapW + nx
			nd := e.dist + dirCosts[d]
			if nd < dist[nIdx] {
				dist[nIdx] = nd
				h.push(heapEntry{idx: nIdx, dist: nd})
			}
		}
	}

	return dist
}

// --- Tolerance ---

// routeTolerance returns additive distance tolerance for band inclusion
// Formula: 0.5 * max(mapW, mapH) * CostDiagonal
func routeTolerance(mapW, mapH int) int {
	m := mapW
	if mapH > m {
		m = mapH
	}
	return (m * CostDiagonal) / 2
}

// --- Band neighbor counting ---

func countBandNeighbors(idx, mapW, mapH int, band []bool, isBlocked WallChecker) int {
	cx := idx % mapW
	cy := idx / mapW
	count := 0
	for d := int8(0); d < DirCount; d++ {
		nx := cx + DirVectors[d][0]
		ny := cy + DirVectors[d][1]
		if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH || !band[ny*mapW+nx] {
			continue
		}
		dx, dy := DirVectors[d][0], DirVectors[d][1]
		if dx != 0 && dy != 0 {
			if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
				continue
			}
		}
		count++
	}
	return count
}

// --- Junction classification and component flood fill ---

// buildComponents classifies band cells as junction (3+ band neighbors) or corridor,
// then flood-fills 8-connected junction cells into components
// Source and target are forced junctions to guarantee node membership
// Returns per-cell component ID (-1 for corridor/non-band), total components, and source/target node IDs
func buildComponents(
	band []bool, nbrCount []int, mapW, mapH int,
	isBlocked WallChecker, sourceIdx, targetIdx int,
) (nodeID []int, numNodes int, sourceNode int, targetNode int) {
	size := mapW * mapH
	nodeID = make([]int, size)
	for i := range nodeID {
		nodeID[i] = -1
	}

	isJunction := make([]bool, size)
	for i := 0; i < size; i++ {
		if band[i] && nbrCount[i] >= 3 {
			isJunction[i] = true
		}
	}
	if band[sourceIdx] {
		isJunction[sourceIdx] = true
	}
	if band[targetIdx] {
		isJunction[targetIdx] = true
	}

	compID := 0
	queue := make([]int, 0, 128)

	for i := 0; i < size; i++ {
		if !isJunction[i] || nodeID[i] >= 0 {
			continue
		}
		queue = queue[:0]
		queue = append(queue, i)
		nodeID[i] = compID

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			cx := cur % mapW
			cy := cur / mapW
			for d := int8(0); d < DirCount; d++ {
				nx := cx + DirVectors[d][0]
				ny := cy + DirVectors[d][1]
				if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH {
					continue
				}
				nIdx := ny*mapW + nx
				if !isJunction[nIdx] || nodeID[nIdx] >= 0 {
					continue
				}
				dx, dy := DirVectors[d][0], DirVectors[d][1]
				if dx != 0 && dy != 0 {
					if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
						continue
					}
				}
				nodeID[nIdx] = compID
				queue = append(queue, nIdx)
			}
		}
		compID++
	}

	sourceNode = nodeID[sourceIdx]
	targetNode = nodeID[targetIdx]
	return nodeID, compID, sourceNode, targetNode
}

// --- Corridor tracing ---

// traceCorridor walks through non-junction band cells from startIdx until reaching
// a junction component. Returns destination component ID and accumulated distance.
// prevIdx is the junction boundary cell the trace originated from (excluded from walk)
func traceCorridor(
	prevIdx, startIdx, mapW, mapH int,
	band []bool, nodeID []int, isBlocked WallChecker,
) (toNode int, dist int, ok bool) {
	maxSteps := mapW + mapH
	prev := prevIdx
	cur := startIdx
	accumulated := 0

	for step := 0; step < maxSteps; step++ {
		if nodeID[cur] >= 0 {
			return nodeID[cur], accumulated, true
		}

		cx := cur % mapW
		cy := cur / mapW
		next := -1
		nextCost := 0

		for d := int8(0); d < DirCount; d++ {
			nx := cx + DirVectors[d][0]
			ny := cy + DirVectors[d][1]
			if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH {
				continue
			}
			nIdx := ny*mapW + nx
			if nIdx == prev || !band[nIdx] {
				continue
			}
			dx, dy := DirVectors[d][0], DirVectors[d][1]
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}
			next = nIdx
			nextCost = dirCosts[d]
			break // corridor cell: at most 1 non-prev band neighbor
		}

		if next < 0 {
			return -1, 0, false
		}

		accumulated += nextCost
		prev = cur
		cur = next
	}

	return -1, 0, false
}

// --- Contracted graph construction ---

// buildContractedGraph discovers edges between junction components via corridor tracing
// and direct adjacency. Computes centroid representative positions per component
func buildContractedGraph(
	nodeID []int, numNodes int,
	band []bool, mapW, mapH int, isBlocked WallChecker,
) []rgNode {
	size := mapW * mapH
	nodes := make([]rgNode, numNodes)

	// Centroid per component
	sumX := make([]int64, numNodes)
	sumY := make([]int64, numNodes)
	cnt := make([]int, numNodes)
	for i := 0; i < size; i++ {
		nid := nodeID[i]
		if nid < 0 {
			continue
		}
		sumX[nid] += int64(i % mapW)
		sumY[nid] += int64(i / mapW)
		cnt[nid]++
	}
	for i := 0; i < numNodes; i++ {
		if cnt[i] > 0 {
			nodes[i].repX = int(sumX[i] / int64(cnt[i]))
			nodes[i].repY = int(sumY[i] / int64(cnt[i]))
		}
	}

	type corridorKey struct{ from, to, start int }
	corridorSeen := make(map[corridorKey]bool)

	type directKey struct{ from, to int }
	directSeen := make(map[directKey]bool)

	for idx := 0; idx < size; idx++ {
		fromNode := nodeID[idx]
		if fromNode < 0 {
			continue
		}

		cx := idx % mapW
		cy := idx / mapW

		for d := int8(0); d < DirCount; d++ {
			nx := cx + DirVectors[d][0]
			ny := cy + DirVectors[d][1]
			if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH || !band[ny*mapW+nx] {
				continue
			}
			dx, dy := DirVectors[d][0], DirVectors[d][1]
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}

			nIdx := ny*mapW + nx
			nNode := nodeID[nIdx]

			if nNode >= 0 && nNode != fromNode {
				dk := directKey{fromNode, nNode}
				if !directSeen[dk] {
					directSeen[dk] = true
					nodes[fromNode].edges = append(nodes[fromNode].edges, rgEdge{
						toNode:      nNode,
						distance:    dirCosts[d],
						boundaryIdx: idx,
						corridorIdx: -1,
					})
				}
			} else if nNode < 0 {
				toNode, corridorDist, found := traceCorridor(idx, nIdx, mapW, mapH, band, nodeID, isBlocked)
				if found && toNode >= 0 && toNode != fromNode {
					ck := corridorKey{fromNode, toNode, nIdx}
					if !corridorSeen[ck] {
						corridorSeen[ck] = true
						nodes[fromNode].edges = append(nodes[fromNode].edges, rgEdge{
							toNode:      toNode,
							distance:    dirCosts[d] + corridorDist,
							boundaryIdx: idx,
							corridorIdx: nIdx,
						})
					}
				}
			}
		}
	}

	return nodes
}

// --- Path enumeration ---

// enumerateRGPaths finds all simple paths from sourceNode to targetNode
// in the contracted graph via DFS. Capped by RouteGraphMaxRoutes and
// RouteGraphMaxBranchFanout per node
func enumerateRGPaths(graph []rgNode, sourceNode, targetNode int) []rgPath {
	var paths []rgPath
	visited := make([]bool, len(graph))
	visited[sourceNode] = true
	steps := make([]rgPathStep, 0, 16)

	var dfs func(current, dist int)
	dfs = func(current, dist int) {
		if current == targetNode {
			result := make([]rgPathStep, len(steps))
			copy(result, steps)
			paths = append(paths, rgPath{steps: result, totalDist: dist})
			return
		}
		if len(paths) >= parameter.RouteGraphMaxRoutes {
			return
		}

		edges := graph[current].edges
		limit := len(edges)
		if limit > parameter.RouteGraphMaxBranchFanout {
			limit = parameter.RouteGraphMaxBranchFanout
		}

		for i := 0; i < limit; i++ {
			edge := edges[i]
			if visited[edge.toNode] {
				continue
			}
			visited[edge.toNode] = true
			steps = append(steps, rgPathStep{nodeID: current, edgeIndex: i})
			dfs(edge.toNode, dist+edge.distance)
			steps = steps[:len(steps)-1]
			visited[edge.toNode] = false
		}
	}

	dfs(sourceNode, 0)
	return paths
}

// --- Route assembly ---

// assembleRoutes converts contracted graph paths into Route structs with
// shared BranchPoint registry. Only nodes with 2+ edges become branch points
func assembleRoutes(rg *RouteGraph, graph []rgNode, paths []rgPath) {
	// Discover branch points: contracted nodes with 2+ edges that appear in paths
	branchMap := make(map[int]int) // contracted nodeID → BranchPoint index
	for _, path := range paths {
		for _, step := range path.steps {
			nid := step.nodeID
			if len(graph[nid].edges) >= 2 {
				if _, exists := branchMap[nid]; !exists {
					bpIdx := len(rg.BranchPoints)
					node := graph[nid]
					rg.BranchPoints = append(rg.BranchPoints, BranchPoint{
						X:           node.repX,
						Y:           node.repY,
						ChoiceCount: len(node.edges),
					})
					branchMap[nid] = bpIdx
				}
			}
		}
	}

	rg.Routes = make([]Route, 0, len(paths))
	for i, path := range paths {
		route := Route{
			ID:            i,
			TotalDistance: path.totalDist,
		}

		// Source waypoint
		route.Waypoints = append(route.Waypoints, Waypoint{
			X: rg.SourceX, Y: rg.SourceY,
			BranchPointID: -1,
		})

		// Intermediate branch point waypoints
		for _, step := range path.steps {
			bpIdx, isBranch := branchMap[step.nodeID]
			if !isBranch {
				continue
			}
			route.Waypoints = append(route.Waypoints, Waypoint{
				X:             graph[step.nodeID].repX,
				Y:             graph[step.nodeID].repY,
				BranchPointID: bpIdx,
				ChoiceIndex:   step.edgeIndex,
			})
		}

		// Target waypoint
		route.Waypoints = append(route.Waypoints, Waypoint{
			X: rg.TargetX, Y: rg.TargetY,
			BranchPointID: -1,
		})

		rg.Routes = append(rg.Routes, route)
	}
}

// --- Weight computation ---

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

// computeRouteFields generates a constrained flow field per route
// At branch points, only the route's chosen corridor is passable;
// alternative branch corridors are blocked, forcing Dijkstra through route-specific cells
func computeRouteFields(
	rg *RouteGraph,
	graph []rgNode, paths []rgPath,
	nodeID []int, band []bool,
	mapW, mapH int, targetNode int,
	isBlocked WallChecker,
) {
	size := mapW * mapH
	targets := []core.Point{{X: rg.TargetX, Y: rg.TargetY}}

	for ri := range rg.Routes {
		path := paths[ri]

		// Build allowed cell mask: junction components + corridors on this route
		allowed := make([]bool, size)

		// Node sequence: each step's nodeID + targetNode
		routeNodes := make(map[int]bool, len(path.steps)+1)
		routeNodes[targetNode] = true
		for _, step := range path.steps {
			routeNodes[step.nodeID] = true
		}

		// Mark all cells belonging to route's junction components
		for i := 0; i < size; i++ {
			if nodeID[i] >= 0 && routeNodes[nodeID[i]] {
				allowed[i] = true
			}
		}

		// Mark corridor cells for each edge in the route
		for _, step := range path.steps {
			edge := graph[step.nodeID].edges[step.edgeIndex]
			if edge.corridorIdx >= 0 {
				cells := traceCorridorCells(edge.boundaryIdx, edge.corridorIdx, mapW, mapH, band, nodeID, isBlocked)
				for _, c := range cells {
					allowed[c] = true
				}
			}
		}

		// Augmented blocker: original walls + band cells not on this route
		routeBlocked := func(x, y int) bool {
			if isBlocked(x, y) {
				return true
			}
			idx := y*mapW + x
			return band[idx] && !allowed[idx]
		}

		field := NewFlowField(mapW, mapH)
		field.Compute(targets, routeBlocked)
		rg.Routes[ri].Field = field
	}
}

// traceCorridorCells walks a corridor from boundaryIdx through corridorIdx,
// collecting all non-junction band cell indices until reaching a junction component
func traceCorridorCells(boundaryIdx, corridorIdx, mapW, mapH int, band []bool, nodeID []int, isBlocked WallChecker) []int {
	var cells []int
	maxSteps := mapW + mapH
	prev := boundaryIdx
	cur := corridorIdx

	for step := 0; step < maxSteps; step++ {
		if nodeID[cur] >= 0 {
			// Reached a junction component, stop
			return cells
		}
		cells = append(cells, cur)

		cx := cur % mapW
		cy := cur / mapW
		next := -1

		for d := int8(0); d < DirCount; d++ {
			nx := cx + DirVectors[d][0]
			ny := cy + DirVectors[d][1]
			if nx < 0 || nx >= mapW || ny < 0 || ny >= mapH {
				continue
			}
			nIdx := ny*mapW + nx
			if nIdx == prev || !band[nIdx] {
				continue
			}
			dx, dy := DirVectors[d][0], DirVectors[d][1]
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}
			next = nIdx
			break
		}

		if next < 0 {
			return cells
		}
		prev = cur
		cur = next
	}

	return cells
}