package navigation

// Direction constants for flow field
// Index into NavSenseDirections: N=0, NE=1, E=2, SE=3, S=4, SW=5, W=6, NW=7
const (
	DirNone   int8 = -1 // Blocked or unreachable
	DirTarget int8 = -2 // At target cell
	DirN      int8 = 0
	DirNE     int8 = 1
	DirE      int8 = 2
	DirSE     int8 = 3
	DirS      int8 = 4
	DirSW     int8 = 5
	DirW      int8 = 6
	DirNW     int8 = 7
	DirCount  int8 = 8
)

// Direction vectors matching DirN..DirNW
// Order: N, NE, E, SE, S, SW, W, NW
var DirVectors = [8][2]int{
	{0, -1}, {1, -1}, {1, 0}, {1, 1},
	{0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

// Opposite direction lookup
var DirOpposite = [8]int8{
	DirS, DirSW, DirW, DirNW,
	DirN, DirNE, DirE, DirSE,
}

// Weighted edge costs: cardinal = 10, diagonal = 14 (≈10√2)
// Approximates Euclidean distance to eliminate Chebyshev artifacts
const (
	costCardinal    = 10
	costDiagonal    = 14
	costUnreachable = 1<<30 - 1
)

// Per-direction costs matching DirVectors index order
var dirCosts = [8]int{
	costCardinal, costDiagonal, costCardinal, costDiagonal,
	costCardinal, costDiagonal, costCardinal, costDiagonal,
}

// --- Min-heap for Dijkstra ---

type heapEntry struct {
	idx  int // Flat grid index (y*width + x)
	dist int // Weighted distance from target
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

// FlowField stores precomputed navigation directions toward a target
type FlowField struct {
	Width, Height int
	Directions    []int8 // Per-cell direction index, DirNone if blocked
	Distances     []int  // Weighted distance from target (cardinal=10, diagonal=14)

	// Cache state
	TargetX, TargetY int  // Target position this field was computed for
	Valid            bool // False if field needs recomputation

	// Reusable heap buffer to reduce allocations across recomputes
	heap minHeap
}

// NewFlowField creates an empty flow field for the given dimensions
func NewFlowField(width, height int) *FlowField {
	size := width * height
	return &FlowField{
		Width:      width,
		Height:     height,
		Directions: make([]int8, size),
		Distances:  make([]int, size),
		TargetX:    -1,
		TargetY:    -1,
		Valid:      false,
		heap:       make(minHeap, 0, size/4),
	}
}

// Resize adjusts field dimensions, invalidates cache
func (f *FlowField) Resize(width, height int) {
	size := width * height
	if cap(f.Directions) < size {
		f.Directions = make([]int8, size)
		f.Distances = make([]int, size)
	} else {
		f.Directions = f.Directions[:size]
		f.Distances = f.Distances[:size]
	}
	f.Width = width
	f.Height = height
	f.Valid = false
}

// Invalidate marks field for recomputation
func (f *FlowField) Invalidate() {
	f.Valid = false
}

// GetDirection returns flow direction at cell, DirNone if invalid/blocked
func (f *FlowField) GetDirection(x, y int) int8 {
	if !f.Valid || x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return DirNone
	}
	return f.Directions[y*f.Width+x]
}

// GetDistance returns weighted distance from target, -1 if unreachable
func (f *FlowField) GetDistance(x, y int) int {
	if !f.Valid || x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return -1
	}
	d := f.Distances[y*f.Width+x]
	if d >= costUnreachable {
		return -1
	}
	return d
}

// WallChecker is a function that returns true if cell blocks navigation
type WallChecker func(x, y int) bool

// Compute performs weighted Dijkstra from target, then derives flow directions from distance gradient (steepest descent toward target)
//
// Phase 1: Dijkstra with cardinal=10, diagonal=14 edge weights
// Phase 2: Per-cell gradient — pick neighbor with minimum distance
func (f *FlowField) Compute(targetX, targetY int, isBlocked WallChecker) {
	if targetX < 0 || targetY < 0 || targetX >= f.Width || targetY >= f.Height {
		f.Valid = false
		return
	}

	size := f.Width * f.Height
	w := f.Width

	// Reset
	for i := 0; i < size; i++ {
		f.Directions[i] = DirNone
		f.Distances[i] = costUnreachable
	}

	// Phase 1: Weighted Dijkstra
	targetIdx := targetY*w + targetX
	f.Distances[targetIdx] = 0

	f.heap = f.heap[:0]
	f.heap.push(heapEntry{idx: targetIdx, dist: 0})

	for len(f.heap) > 0 {
		entry := f.heap.pop()

		if entry.dist > f.Distances[entry.idx] {
			continue // Stale entry
		}

		cx := entry.idx % w
		cy := entry.idx / w

		for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
			nx := cx + DirVectors[dirIdx][0]
			ny := cy + DirVectors[dirIdx][1]

			if nx < 0 || ny < 0 || nx >= f.Width || ny >= f.Height {
				continue
			}

			if isBlocked(nx, ny) {
				continue
			}

			// Diagonal corner cutting prevention
			if DirVectors[dirIdx][0] != 0 && DirVectors[dirIdx][1] != 0 {
				if isBlocked(cx+DirVectors[dirIdx][0], cy) || isBlocked(cx, cy+DirVectors[dirIdx][1]) {
					continue
				}
			}

			nIdx := ny*w + nx
			newDist := entry.dist + dirCosts[dirIdx]

			if newDist < f.Distances[nIdx] {
				f.Distances[nIdx] = newDist
				f.heap.push(heapEntry{idx: nIdx, dist: newDist})
			}
		}
	}

	// Phase 2: Derive flow directions from distance gradient (steepest descent)
	f.Directions[targetIdx] = DirTarget

	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			idx := y*w + x
			dist := f.Distances[idx]
			if dist >= costUnreachable || dist == 0 {
				continue
			}

			bestDir := DirNone
			bestDist := dist

			for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
				nx := x + DirVectors[dirIdx][0]
				ny := y + DirVectors[dirIdx][1]

				if nx < 0 || ny < 0 || nx >= f.Width || ny >= f.Height {
					continue
				}

				nDist := f.Distances[ny*w+nx]
				if nDist >= bestDist {
					continue
				}

				// Diagonal corner cutting prevention (must check here too to avoid pointing into invalid corner)
				if DirVectors[dirIdx][0] != 0 && DirVectors[dirIdx][1] != 0 {
					if isBlocked(x+DirVectors[dirIdx][0], y) || isBlocked(x, y+DirVectors[dirIdx][1]) {
						continue
					}
				}

				bestDist = nDist
				bestDir = dirIdx
			}

			f.Directions[idx] = bestDir
		}
	}

	f.TargetX = targetX
	f.TargetY = targetY
	f.Valid = true
}

// IncrementalUpdate patches newly-free cells without full recompute
// Detects cells that are free but have no direction and propagates from neighbors
func (f *FlowField) IncrementalUpdate(isBlocked WallChecker) {
	if !f.Valid {
		return
	}

	w := f.Width

	for y := 0; y < f.Height; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x

			// Skip if already has valid direction
			if f.Directions[idx] != DirNone {
				continue
			}

			// Skip if still blocked
			if isBlocked(x, y) {
				continue
			}

			// Cell is free but has no direction - find best neighbor
			bestDir := DirNone
			bestDist := costUnreachable

			for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
				nx := x + DirVectors[dirIdx][0]
				ny := y + DirVectors[dirIdx][1]

				if nx < 0 || ny < 0 || nx >= w || ny >= f.Height {
					continue
				}

				nIdx := ny*w + nx
				nDist := f.Distances[nIdx]

				// Neighbor must have valid distance
				if nDist >= costUnreachable {
					continue
				}

				// Diagonal corner-cutting check
				if DirVectors[dirIdx][0] != 0 && DirVectors[dirIdx][1] != 0 {
					if isBlocked(x+DirVectors[dirIdx][0], y) || isBlocked(x, y+DirVectors[dirIdx][1]) {
						continue
					}
				}

				cost := nDist + dirCosts[dirIdx]
				if cost < bestDist {
					bestDist = cost
					bestDir = dirIdx
				}
			}

			if bestDir != DirNone {
				f.Directions[idx] = bestDir
				f.Distances[idx] = bestDist
			}
		}
	}

	return
}