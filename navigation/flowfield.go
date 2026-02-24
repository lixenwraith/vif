package navigation

import (
	"github.com/lixenwraith/vi-fighter/core"
)

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

// Weighted edge costs accounting for terminal 2:1 aspect ratio (Width:Height)
// X distance = 10, Y distance = 20
// Diagonal distance = sqrt(10^2 + 20^2) ≈ 22.36
const (
	costX           = 10
	costY           = 20
	costDiagonal    = 22
	costUnreachable = 1<<30 - 1
)

// Per-direction costs matching DirVectors index order (N, NE, E, SE, S, SW, W, NW)
var dirCosts = [8]int{
	costY, costDiagonal, costX, costDiagonal,
	costY, costDiagonal, costX, costDiagonal,
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
	Distances     []int  // Aspect-weighted distance from target (X=10, Y=20, diag=22)

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
	dirs := make([]int8, size)
	for i := range dirs {
		dirs[i] = DirNone
	}
	return &FlowField{
		Width:      width,
		Height:     height,
		Directions: dirs,
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
		// Clear stale generation markers to prevent collision after CurrentGen reset
		for i := range f.VisitedGen {
			f.VisitedGen[i] = 0
		}
	}
	// Ensure no stale valid-looking directions survive resize
	for i := range f.Directions {
		f.Directions[i] = DirNone
	}
	f.Width = width
	f.Height = height
	f.CurrentGen = 0
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

// WallChecker is a function that returns true if cell blocks navigation
type WallChecker func(x, y int) bool

// Compute performs weighted Dijkstra from all target points across the entire valid field
func (f *FlowField) Compute(targets []core.Point, isBlocked WallChecker) {
	if len(targets) == 0 {
		f.Valid = false
		return
	}

	w := f.Width
	f.CurrentGen++
	if f.CurrentGen == 0 {
		for i := range f.VisitedGen {
			f.VisitedGen[i] = 0
		}
		f.CurrentGen = 1
	}

	f.heap = f.heap[:0]

	// Seed all valid targets
	for _, t := range targets {
		if t.X < 0 || t.Y < 0 || t.X >= f.Width || t.Y >= f.Height {
			continue
		}

		targetIdx := t.Y*w + t.X
		if !isBlocked(t.X, t.Y) {
			if f.VisitedGen[targetIdx] != f.CurrentGen {
				f.Distances[targetIdx] = 0
				f.VisitedGen[targetIdx] = f.CurrentGen
				f.Directions[targetIdx] = DirTarget
				f.heap.push(heapEntry{idx: targetIdx, dist: 0})
			}
		} else {
			f.seedVirtualTargets(t.X, t.Y, isBlocked)
		}
	}

	if len(f.heap) == 0 {
		f.Valid = false
		return
	}

	// Phase 1: Weighted Dijkstra (Unrestricted Expansion)
	for len(f.heap) > 0 {
		entry := f.heap.pop()
		idx := entry.idx

		if f.VisitedGen[idx] == f.CurrentGen && entry.dist > f.Distances[idx] {
			continue
		}

		cx := idx % w
		cy := idx / w

		for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
			nx := cx + DirVectors[dirIdx][0]
			ny := cy + DirVectors[dirIdx][1]

			if nx < 0 || nx >= f.Width || ny < 0 || ny >= f.Height {
				continue
			}

			if isBlocked(nx, ny) {
				continue
			}

			dx, dy := DirVectors[dirIdx][0], DirVectors[dirIdx][1]
			if dx != 0 && dy != 0 {
				if isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy) {
					continue
				}
			}

			nIdx := ny*w + nx
			newDist := entry.dist + dirCosts[dirIdx]

			if f.VisitedGen[nIdx] != f.CurrentGen || newDist < f.Distances[nIdx] {
				f.Distances[nIdx] = newDist
				f.VisitedGen[nIdx] = f.CurrentGen
				f.Directions[nIdx] = DirOpposite[dirIdx]
				f.heap.push(heapEntry{idx: nIdx, dist: newDist})
			}
		}
	}

	f.Valid = true
}

// seedVirtualTargets finds passable cells near blocked target using spiral search
// Seeds passable cells with weighted distance - does NOT mark blocked target
func (f *FlowField) seedVirtualTargets(targetX, targetY int, isBlocked WallChecker) {
	w := f.Width
	const maxSearchRadius = 8

	// DO NOT mark the blocked target cell - only seed passable neighbors

	for radius := 1; radius <= maxSearchRadius; radius++ {
		foundAny := false

		// Check ring at this radius
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				// Only check cells on the ring perimeter
				if abs(dx) != radius && abs(dy) != radius {
					continue
				}

				nx := targetX + dx
				ny := targetY + dy

				if nx < 0 || nx >= w || ny < 0 || ny >= f.Height {
					continue
				}

				if isBlocked(nx, ny) {
					continue
				}

				nIdx := ny*w + nx

				// Seed as virtual target with distance based on manhattan distance from real target
				// Weighted by axis costs for consistency with Dijkstra
				cost := abs(dx)*costX + abs(dy)*costY
				if f.VisitedGen[nIdx] != f.CurrentGen || cost < f.Distances[nIdx] {
					f.Distances[nIdx] = cost
					f.VisitedGen[nIdx] = f.CurrentGen
					f.Directions[nIdx] = DirTarget // Mark as target for direction derivation
					f.heap.push(heapEntry{idx: nIdx, dist: cost})
					foundAny = true
				}
			}
		}

		// Found at least one passable cell at this radius - stop expanding
		if foundAny {
			break
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}