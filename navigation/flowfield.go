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

	// Generation tracking for zero-allocation reset
	VisitedGen []uint32
	CurrentGen uint32

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
		VisitedGen: make([]uint32, size),
		CurrentGen: 0,
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
		f.VisitedGen = make([]uint32, size)
	} else {
		f.Directions = f.Directions[:size]
		f.Distances = f.Distances[:size]
		f.VisitedGen = f.VisitedGen[:size]
	}
	f.Width = width
	f.Height = height
	f.CurrentGen = 0 // Reset generation on resize
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
	idx := y*f.Width + x
	// Check generation validity
	if f.VisitedGen[idx] != f.CurrentGen {
		return DirNone
	}
	return f.Directions[idx]
}

// GetDistance returns weighted distance from target, -1 if unreachable
func (f *FlowField) GetDistance(x, y int) int {
	if !f.Valid || x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return -1
	}
	idx := y*f.Width + x
	// Check generation validity
	if f.VisitedGen[idx] != f.CurrentGen {
		return -1
	}
	d := f.Distances[idx]
	if d >= costUnreachable {
		return -1
	}
	return d
}

// ROIBounds defines rectangular region of interest for bounded computation
type ROIBounds struct {
	MinX, MinY, MaxX, MaxY int
}

// FullBounds returns ROI covering entire field
func (f *FlowField) FullBounds() ROIBounds {
	return ROIBounds{0, 0, f.Width - 1, f.Height - 1}
}

// WallChecker is a function that returns true if cell blocks navigation
type WallChecker func(x, y int) bool

// Compute performs weighted Dijkstra from target within ROI bounds
// Pass nil for roi to compute full field
func (f *FlowField) Compute(targetX, targetY int, isBlocked WallChecker, roi *ROIBounds) {
	if targetX < 0 || targetY < 0 || targetX >= f.Width || targetY >= f.Height {
		f.Valid = false
		return
	}

	// Determine bounds
	minX, minY, maxX, maxY := 0, 0, f.Width-1, f.Height-1
	if roi != nil {
		minX = max(0, roi.MinX)
		minY = max(0, roi.MinY)
		maxX = min(f.Width-1, roi.MaxX)
		maxY = min(f.Height-1, roi.MaxY)
	}

	// Ensure target is within bounds
	if targetX < minX || targetX > maxX || targetY < minY || targetY > maxY {
		// Expand bounds to include target
		minX = min(minX, targetX)
		minY = min(minY, targetY)
		maxX = max(maxX, targetX)
		maxY = max(maxY, targetY)
	}

	w := f.Width

	// Increment generation (zero-allocation reset)
	f.CurrentGen++
	if f.CurrentGen == 0 {
		// Handle overflow by clearing all
		for i := range f.VisitedGen {
			f.VisitedGen[i] = 0
		}
		f.CurrentGen = 1
	}

	// Phase 1: Weighted Dijkstra within ROI
	targetIdx := targetY*w + targetX
	f.Distances[targetIdx] = 0
	f.VisitedGen[targetIdx] = f.CurrentGen

	f.heap = f.heap[:0]
	f.heap.push(heapEntry{idx: targetIdx, dist: 0})

	for len(f.heap) > 0 {
		entry := f.heap.pop()
		idx := entry.idx

		// Skip if we've found a better path
		if f.VisitedGen[idx] == f.CurrentGen && entry.dist > f.Distances[idx] {
			continue
		}

		cx := idx % w
		cy := idx / w

		for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
			nx := cx + DirVectors[dirIdx][0]
			ny := cy + DirVectors[dirIdx][1]

			// ROI boundary check
			if nx < minX || nx > maxX || ny < minY || ny > maxY {
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

			// Check if unvisited this generation or found better path
			if f.VisitedGen[nIdx] != f.CurrentGen || newDist < f.Distances[nIdx] {
				f.Distances[nIdx] = newDist
				f.VisitedGen[nIdx] = f.CurrentGen
				f.heap.push(heapEntry{idx: nIdx, dist: newDist})
			}
		}
	}

	// Phase 2: Derive flow directions from distance gradient
	f.Directions[targetIdx] = DirTarget
	f.VisitedGen[targetIdx] = f.CurrentGen

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			idx := y*w + x

			// Skip unvisited cells
			if f.VisitedGen[idx] != f.CurrentGen {
				f.Directions[idx] = DirNone
				continue
			}

			dist := f.Distances[idx]
			if dist >= costUnreachable || dist == 0 {
				continue
			}

			bestDir := DirNone
			bestDist := dist

			for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
				nx := x + DirVectors[dirIdx][0]
				ny := y + DirVectors[dirIdx][1]

				if nx < minX || nx > maxX || ny < minY || ny > maxY {
					continue
				}

				nIdx := ny*w + nx

				// Skip unvisited neighbors
				if f.VisitedGen[nIdx] != f.CurrentGen {
					continue
				}

				nDist := f.Distances[nIdx]
				if nDist >= bestDist {
					continue
				}

				// Diagonal corner cutting prevention
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

// IncrementalUpdate patches newly-free cells within current generation
func (f *FlowField) IncrementalUpdate(isBlocked WallChecker) {
	if !f.Valid {
		return
	}

	w := f.Width

	for y := 0; y < f.Height; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x

			// Only consider cells from current generation with no direction
			if f.VisitedGen[idx] == f.CurrentGen && f.Directions[idx] != DirNone {
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

				// Neighbor must be from current generation
				if f.VisitedGen[nIdx] != f.CurrentGen {
					continue
				}

				nDist := f.Distances[nIdx]
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
				f.VisitedGen[idx] = f.CurrentGen
			}
		}
	}
}