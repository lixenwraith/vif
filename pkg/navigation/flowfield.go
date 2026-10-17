package navigation

import (
	"math"
	"math/bits"

	"github.com/lixenwraith/vif/pkg/vmath"
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

// CostUnreachable is the distance of a cell no path reaches
const CostUnreachable = 1<<30 - 1

// Costs are the weighted edge costs of one cell aspect: a horizontal step is X, a
// vertical one X over the aspect and a diagonal the hypotenuse, so a path's cost
// reads the same in every direction on the screen; the reference 2:1 cell gives
// 10, 20 and 22. Unit is DirVectors as steering vectors at that aspect.
type Costs struct {
	X, Y, Diag int32
	dir        [8]int32 // per DirVectors index
	Unit       [8][2]float64
}

// CostsFor are the costs of one cell aspect
func CostsFor(a vmath.Aspect) Costs {
	c := Costs{X: 10}
	c.Y = int32(math.Round(float64(c.X) / float64(a)))
	c.Diag = int32(math.Round(math.Hypot(float64(c.X), float64(c.Y))))
	for i, vec := range DirVectors {
		switch {
		case vec[0] != 0 && vec[1] != 0:
			c.dir[i] = c.Diag
		case vec[0] != 0:
			c.dir[i] = c.X
		default:
			c.dir[i] = c.Y
		}
		fx, fy := float64(vec[0]), a.FromCircular(float64(vec[1]))
		if fx != 0 || fy != 0 {
			fx, fy = vmath.Normalize2D(fx, fy)
		}
		c.Unit[i] = [2]float64{fx, fy}
	}
	return c
}

// refCosts are the costs every field and solver starts with
var refCosts = CostsFor(vmath.RefAspect)

// seedSpan is the furthest a seed lies from its target: maxSearchRadius cells
// along each axis
func (c *Costs) seedSpan() int32 { return maxSearchRadius * (c.X + c.Y) }

// FlowField stores precomputed navigation directions toward a target
type FlowField struct {
	Width, Height int
	Directions    []int8  // Per-cell direction index, DirNone if blocked
	Distances     []int32 // Weighted distance from target by Costs; 22 per cell at the reference fits any field under 97M cells
	Costs         Costs   // the aspect the distances are weighted at

	// Generation tracking for zero-allocation reset
	VisitedGen []uint32
	CurrentGen uint32

	// Cache state
	TargetX, TargetY int  // Target position this field was computed for
	Valid            bool // False if field needs recomputation

	search gridSearch // scratch reused across recomputes, empty after a clone
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
		Costs:      refCosts,
		Distances:  make([]int32, size),
		VisitedGen: make([]uint32, size),
		CurrentGen: 0,
		TargetX:    -1,
		TargetY:    -1,
		Valid:      false,
	}
}

// Resize adjusts field dimensions, invalidates cache
func (f *FlowField) Resize(width, height int) {
	size := width * height
	if cap(f.Directions) < size {
		f.Directions = make([]int8, size)
		f.Distances = make([]int32, size)
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
	d := int(f.Distances[idx])
	if d >= CostUnreachable {
		return -1
	}
	return d
}

// WallChecker is a function that returns true if cell blocks navigation
type WallChecker func(x, y int) bool

const maxSearchRadius = 8

// Compute performs weighted Dijkstra from all target points across the entire
// valid field.
func (f *FlowField) Compute(targets []vmath.Point, isBlocked WallChecker) {
	f.computeWith(&f.search, targets, isBlocked)
}

// computeWith is Compute on the search g, which a caller computing many fields
// at once shares among them
func (f *FlowField) computeWith(g *gridSearch, targets []vmath.Point, isBlocked WallChecker) {
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
	g.fill(w, f.Height, isBlocked)
	g.reset(f.Costs.seedSpan())

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
				g.push(targetIdx, 0)
			}
		} else {
			f.seedVirtualTargets(g, t.X, t.Y, isBlocked)
		}
	}

	if g.pending == 0 {
		f.Valid = false
		return
	}

	for idx, d, ok := g.pop(); ok; idx, d, ok = g.pop() {
		if f.Distances[idx] != d {
			continue // a shorter distance reached it since
		}
		for m := g.open(idx); m != 0; m &= m - 1 {
			k := bits.TrailingZeros8(m)
			nIdx := idx + g.step[k]
			nd := d + f.Costs.dir[k]
			if f.VisitedGen[nIdx] != f.CurrentGen || nd < f.Distances[nIdx] {
				f.Distances[nIdx] = nd
				f.VisitedGen[nIdx] = f.CurrentGen
				f.Directions[nIdx] = DirOpposite[k]
				g.push(nIdx, nd)
			}
		}
	}

	f.Valid = true
}

// gridSearch is the scratch every Dijkstra over the grid runs on: isBlocked asked
// once per cell into a grid with a blocked border, so a neighbour is one load and
// no bounds test, and a ring of buckets indexed by distance, which pops in constant
// time while no queued distance runs a whole ring ahead of the one settling.
type gridSearch struct {
	w       int
	blocked []bool
	step    [DirCount]int32 // to each neighbour, in field cells
	padStep [DirCount]int   // the same, in the bordered grid
	ring    [][]int32
	pending int
	at      int32
}

// fill reads the passability of a w×h field.
func (g *gridSearch) fill(w, h int, isBlocked WallChecker) {
	pw, ph := w+2, h+2
	if cap(g.blocked) < pw*ph {
		g.blocked = make([]bool, pw*ph)
	}
	g.blocked = g.blocked[:pw*ph]
	for x := range pw {
		g.blocked[x], g.blocked[(ph-1)*pw+x] = true, true
	}
	for y := range h {
		row := g.blocked[(y+1)*pw : (y+2)*pw]
		row[0], row[pw-1] = true, true
		for x := range w {
			row[x+1] = isBlocked(x, y)
		}
	}
	g.w = w
	for k := range DirCount {
		g.step[k] = int32(DirVectors[k][1]*w + DirVectors[k][0])
		g.padStep[k] = DirVectors[k][1]*pw + DirVectors[k][0]
	}
}

// pad is a field cell's index in the bordered grid.
func (g *gridSearch) pad(idx int32) int {
	return (int(idx)/g.w+1)*(g.w+2) + int(idx)%g.w + 1
}

// open is the directions a search steps along from idx, as bits: into a passable
// cell, and along a diagonal only past both corners beside it.
func (g *gridSearch) open(idx int32) uint8 {
	p := g.pad(idx)
	var m uint8
	for k := range DirCount {
		if !g.blocked[p+g.padStep[k]] {
			m |= 1 << k
		}
	}
	return m & (0x55 | (m<<1|m>>7)&(m>>1|m<<7))
}

// reset empties the queue and sizes its ring past span, the furthest a queued
// distance may run ahead of the one settling.
func (g *gridSearch) reset(span int32) {
	n := max(len(g.ring), 64)
	for int32(n) <= span {
		n *= 2
	}
	if n > len(g.ring) {
		g.ring = append(g.ring, make([][]int32, n-len(g.ring))...)
	}
	for i := range g.ring {
		g.ring[i] = g.ring[i][:0]
	}
	g.pending, g.at = 0, 0
}

func (g *gridSearch) push(idx, dist int32) {
	b := &g.ring[dist&int32(len(g.ring)-1)]
	*b = append(*b, idx)
	g.pending++
}

// pop is the nearest queued cell and the distance it was queued at; a cell queued
// again is popped again, and ok is false once nothing is queued.
func (g *gridSearch) pop() (idx, dist int32, ok bool) {
	mask := int32(len(g.ring) - 1)
	for g.pending > 0 {
		b := &g.ring[g.at&mask]
		if n := len(*b); n > 0 {
			idx = (*b)[n-1]
			*b = (*b)[:n-1]
			g.pending--
			return idx, g.at, true
		}
		g.at++
	}
	return 0, 0, false
}

// Reachable reports whether Compute would seed anything for targets, so a field
// whose compute is deferred can say now whether it will be valid.
func (f *FlowField) Reachable(targets []vmath.Point, isBlocked WallChecker) bool {
	for _, t := range targets {
		if t.X < 0 || t.Y < 0 || t.X >= f.Width || t.Y >= f.Height {
			continue
		}
		if !isBlocked(t.X, t.Y) {
			return true
		}
		for dy := -maxSearchRadius; dy <= maxSearchRadius; dy++ {
			for dx := -maxSearchRadius; dx <= maxSearchRadius; dx++ {
				nx, ny := t.X+dx, t.Y+dy
				if (dx != 0 || dy != 0) && nx >= 0 && nx < f.Width && ny >= 0 && ny < f.Height && !isBlocked(nx, ny) {
					return true
				}
			}
		}
	}
	return false
}

// seedVirtualTargets finds passable cells near blocked target using spiral search
// Seeds passable cells with weighted distance - does NOT mark blocked target
func (f *FlowField) seedVirtualTargets(g *gridSearch, targetX, targetY int, isBlocked WallChecker) {
	w := f.Width

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
				cost := int32(abs(dx))*f.Costs.X + int32(abs(dy))*f.Costs.Y
				if f.VisitedGen[nIdx] != f.CurrentGen || cost < f.Distances[nIdx] {
					f.Distances[nIdx] = cost
					f.VisitedGen[nIdx] = f.CurrentGen
					f.Directions[nIdx] = DirTarget // Mark as target for direction derivation
					g.push(int32(nIdx), cost)
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
