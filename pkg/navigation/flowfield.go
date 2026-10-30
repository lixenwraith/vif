package navigation

import (
	"github.com/lixenwraith/vif/pkg/position"
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

	search  SearchGrid // Compute's own grid, empty after a clone; ComputeOn shares one
	changed []int32    // the cells a repair moved, scratch
}

// NewFlowField creates an empty flow field for the given dimensions; the first
// compute allocates its arrays, so a field nothing reads costs nothing.
func NewFlowField(width, height int) *FlowField {
	return &FlowField{Width: width, Height: height, Costs: refCosts, TargetX: -1, TargetY: -1}
}

// Resize adjusts field dimensions, invalidates cache
func (f *FlowField) Resize(width, height int) {
	f.Directions, f.Distances, f.VisitedGen = nil, nil, nil
	f.Width, f.Height = width, height
	f.CurrentGen = 0
	f.Valid = false
}

// alloc sizes the arrays to the field, fresh when they are not its size yet.
func (f *FlowField) alloc() {
	if size := f.Width * f.Height; len(f.Directions) != size {
		f.Directions = make([]int8, size)
		for i := range f.Directions {
			f.Directions[i] = DirNone
		}
		f.Distances, f.VisitedGen, f.CurrentGen = make([]int32, size), make([]uint32, size), 0
	}
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
// valid field, reading the walls into the field's own grid.
func (f *FlowField) Compute(targets []vmath.Point, isBlocked WallChecker) {
	f.search.Fill(f.Width, f.Height, isBlocked)
	f.ComputeOn(&f.search, targets)
}

// ComputeOn is Compute on a grid already filled at the field's size, which every
// field of one tier shares until the passability moves.
func (f *FlowField) ComputeOn(g *SearchGrid, targets []vmath.Point) {
	if len(targets) == 0 || g.w != f.Width || g.h != f.Height {
		f.Valid = false
		return
	}

	w := f.Width
	f.alloc()
	f.CurrentGen++
	if f.CurrentGen == 0 {
		for i := range f.VisitedGen {
			f.VisitedGen[i] = 0
		}
		f.CurrentGen = 1
	}
	g.reset(0, f.Costs.seedSpan())

	for _, t := range targets {
		if g.Blocked(t.X, t.Y) {
			if t.X >= 0 && t.Y >= 0 && t.X < w && t.Y < f.Height {
				f.seedVirtualTargets(g, t.X, t.Y)
			}
			continue
		}
		targetIdx := int32(t.Y*w + t.X)
		if f.VisitedGen[targetIdx] != f.CurrentGen {
			f.Distances[targetIdx] = 0
			f.VisitedGen[targetIdx] = f.CurrentGen
			f.Directions[targetIdx] = DirTarget
			g.push(targetIdx, 0)
		}
	}

	if g.pending == 0 {
		f.Valid = false
		return
	}
	f.settle(g, false)
	f.Valid = true
}

// settle runs the queued cells down, relaxing each neighbour: a cell reached
// shorter takes that step, one reached equally the lower-numbered step, so a
// direction is a function of the distances alone and a repair agrees with a
// recompute. track records every cell whose distance moved.
func (f *FlowField) settle(g *SearchGrid, track bool) {
	for idx, d, ok := g.pop(); ok; idx, d, ok = g.pop() {
		if f.Distances[idx] != d {
			continue // a shorter distance reached it since
		}
		for m := g.open[idx]; m != 0; m &= m - 1 {
			k := bits.TrailingZeros8(m)
			nIdx := idx + g.step[k]
			nd := d + f.Costs.dir[k]
			switch {
			case f.VisitedGen[nIdx] != f.CurrentGen || nd < f.Distances[nIdx]:
				f.Distances[nIdx] = nd
				f.VisitedGen[nIdx] = f.CurrentGen
				f.Directions[nIdx] = DirOpposite[k]
				g.push(nIdx, nd)
				if track {
					f.changed = append(f.changed, nIdx)
				}
			case nd == f.Distances[nIdx] && f.Directions[nIdx] != DirTarget && DirOpposite[k] < f.Directions[nIdx]:
				f.Directions[nIdx] = DirOpposite[k]
			}
		}
	}
}

// Opened repairs a computed field after cells became passable, settling only what
// the openings improve: the distances and directions a recompute would give. It
// declines, for a recompute, when an opening lies on or within the seed ring of a
// target, which would reseed the field.
func (f *FlowField) Opened(g *SearchGrid, cells, targets []vmath.Point) bool {
	if !f.Valid || g.w != f.Width || g.h != f.Height {
		return false
	}
	for _, o := range cells {
		for _, t := range targets {
			if o == t || g.Blocked(t.X, t.Y) && max(abs(o.X-t.X), abs(o.Y-t.Y)) <= maxSearchRadius {
				return false
			}
		}
	}
	c := &f.Costs
	f.changed = f.changed[:0]
	for _, o := range cells {
		if g.Blocked(o.X, o.Y) {
			continue
		}
		idx := int32(o.Y*f.Width + o.X)
		best, from := int32(CostUnreachable), DirNone
		for m := g.open[idx]; m != 0; m &= m - 1 {
			k := bits.TrailingZeros8(m)
			if n := idx + g.step[k]; f.VisitedGen[n] == f.CurrentGen && f.Distances[n]+c.dir[k] < best {
				best, from = f.Distances[n]+c.dir[k], int8(k)
			}
		}
		if best >= CostUnreachable {
			continue // nothing reaches the opening, so it opens nothing
		}
		if f.VisitedGen[idx] != f.CurrentGen || best < f.Distances[idx] {
			f.Distances[idx], f.VisitedGen[idx], f.Directions[idx] = best, f.CurrentGen, from
			f.changed = append(f.changed, idx)
		}
		// Only the opening and the neighbours within a step of it can improve
		// anything through the edges it added.
		d := f.Distances[idx]
		g.reset(max(0, d-c.Y), 2*c.Y+c.Diag)
		g.push(idx, d)
		for k := DirN; k < DirCount; k += 2 {
			if n := idx + g.step[k]; g.open[idx]&(1<<k) != 0 && f.VisitedGen[n] == f.CurrentGen && f.Distances[n] <= d+c.Y {
				g.push(n, f.Distances[n])
			}
		}
		f.settle(g, true)
	}
	for _, idx := range f.changed {
		f.Directions[idx] = f.canonical(g, idx)
	}
	return true
}

// canonical is the step a cell takes toward the goal: the lowest direction to a
// neighbour whose distance plus the step is the cell's own, DirTarget when none.
func (f *FlowField) canonical(g *SearchGrid, idx int32) int8 {
	for m := g.open[idx]; m != 0; m &= m - 1 {
		k := bits.TrailingZeros8(m)
		if n := idx + g.step[k]; f.VisitedGen[n] == f.CurrentGen && f.Distances[n]+f.Costs.dir[k] == f.Distances[idx] {
			return int8(k)
		}
	}
	return DirTarget
}

// SearchGrid is what every Dijkstra over one passability runs on: the wall test
// asked once per cell into a grid with a blocked border, the directions open from
// each cell as one mask with the corner rule folded in, and a ring of buckets
// indexed by distance, which pops in constant time while no queued distance runs
// a whole ring ahead of the one settling. One grid serves every field, route and
// reach on its tier until the passability moves.
type SearchGrid struct {
	w, h    int
	free    []uint8         // bordered, (w+2)×(h+2): 1 where passable
	open    []uint8         // per cell, a bit per DirVectors index
	step    [DirCount]int32 // to each neighbour, in field cells
	padStep [DirCount]int   // the same, in the bordered grid
	ring    [][]int32
	pending int
	at      int32
	marks   []uint32 // the Move each cell was last remasked in, so it is remasked once
	mark    uint32
}

// Fill reads the passability of a w×h field and the directions open from each
// cell: into a passable cell, and along a diagonal only past both corners beside it.
func (g *SearchGrid) Fill(w, h int, isBlocked WallChecker) {
	pw := w + 2
	g.free = grow(g.free, pw*(h+2))
	clear(g.free)
	for y := range h {
		row := g.free[(y+1)*pw+1 : (y+1)*pw+1+w]
		for x := range row {
			if !isBlocked(x, y) {
				row[x] = 1
			}
		}
	}
	g.finish(w, h)
}

// FillBlocked is Fill from a grid of bits, set where a cell is impassable.
func (g *SearchGrid) FillBlocked(w, h int, blocked position.Bits) {
	pw := w + 2
	g.free = grow(g.free, pw*(h+2))
	clear(g.free)
	for y := range h {
		row := g.free[(y+1)*pw+1 : (y+1)*pw+1+w]
		for i, word := range blocked.Row(y) {
			for x := i * 64; x < min(i*64+64, w); x++ {
				if word>>(uint(x)&63)&1 == 0 {
					row[x] = 1
				}
			}
		}
	}
	g.finish(w, h)
}

// finish reads the directions open from each filled cell
func (g *SearchGrid) finish(w, h int) {
	pw := w + 2
	g.w, g.h = w, h
	for k := range DirCount {
		g.step[k] = int32(DirVectors[k][1]*w + DirVectors[k][0])
		g.padStep[k] = DirVectors[k][1]*pw + DirVectors[k][0]
	}
	g.open = grow(g.open, w*h)
	for y := range h {
		up, mid, down := g.free[y*pw:], g.free[(y+1)*pw:], g.free[(y+2)*pw:]
		row := g.open[y*w : y*w+w]
		for x := range row {
			row[x] = openMask(up[x:x+3], mid[x:x+3], down[x:x+3])
		}
	}
}

// mask is the directions open from the cell at bordered index p.
func (g *SearchGrid) mask(p int) uint8 {
	pw := g.w + 2
	return openMask(g.free[p-pw-1:p-pw+2], g.free[p-1:p+2], g.free[p+pw-1:p+pw+2])
}

// openMask is the directions open from the middle of three rows of three cells:
// into a passable cell, and along a diagonal only past both corners beside it.
func openMask(up, mid, down []uint8) uint8 {
	m := up[1] | up[2]<<DirNE | mid[2]<<DirE | down[2]<<DirSE | down[1]<<DirS | down[0]<<DirSW | mid[0]<<DirW | up[0]<<DirNW
	return m & (0x55 | bits.RotateLeft8(m, 1)&bits.RotateLeft8(m, -1)) & -mid[1]
}

// Move closes and opens cells, then remasks the directions open from every cell
// within a step of one, each once however many changed cells it neighbours.
func (g *SearchGrid) Move(closed, opened []vmath.Point) {
	pw := g.w + 2
	for _, c := range closed {
		if g.inside(c) {
			g.free[(c.Y+1)*pw+c.X+1] = 0
		}
	}
	for _, c := range opened {
		if g.inside(c) {
			g.free[(c.Y+1)*pw+c.X+1] = 1
		}
	}
	g.marks = grow(g.marks, g.w*g.h)
	if g.mark++; g.mark == 0 {
		clear(g.marks[:cap(g.marks)])
		g.mark = 1
	}
	for _, cells := range [2][]vmath.Point{closed, opened} {
		for _, c := range cells {
			if !g.inside(c) {
				continue
			}
			for ny := max(0, c.Y-1); ny <= min(g.h-1, c.Y+1); ny++ {
				for nx := max(0, c.X-1); nx <= min(g.w-1, c.X+1); nx++ {
					if i := ny*g.w + nx; g.marks[i] != g.mark {
						g.marks[i] = g.mark
						g.open[i] = g.mask((ny+1)*pw + nx + 1)
					}
				}
			}
		}
	}
}

func (g *SearchGrid) inside(c vmath.Point) bool {
	return c.X >= 0 && c.Y >= 0 && c.X < g.w && c.Y < g.h
}

// Size is the field size the grid was filled at.
func (g *SearchGrid) Size() (w, h int) { return g.w, g.h }

// Blocked reports whether a cell is impassable; every cell off the grid is.
func (g *SearchGrid) Blocked(x, y int) bool {
	return x < 0 || y < 0 || x >= g.w || y >= g.h || g.free[(y+1)*(g.w+2)+x+1] == 0
}

// pad is a field cell's index in the bordered grid.
func (g *SearchGrid) pad(idx int32) int {
	return (int(idx)/g.w+1)*(g.w+2) + int(idx)%g.w + 1
}

// reset empties the queue to start popping at distance at, and sizes its ring
// past span, the furthest a queued distance may run ahead of the one settling.
func (g *SearchGrid) reset(at, span int32) {
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
	g.pending, g.at = 0, at
}

func (g *SearchGrid) push(idx, dist int32) {
	b := &g.ring[dist&int32(len(g.ring)-1)]
	*b = append(*b, idx)
	g.pending++
}

// pop is the nearest queued cell and the distance it was queued at; a cell queued
// again is popped again, and ok is false once nothing is queued.
func (g *SearchGrid) pop() (idx, dist int32, ok bool) {
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

// Reachable reports whether ComputeOn would seed anything for targets, so a field
// whose compute is deferred can say now whether it will be valid.
func (f *FlowField) Reachable(g *SearchGrid, targets []vmath.Point) bool {
	for _, t := range targets {
		if t.X < 0 || t.Y < 0 || t.X >= f.Width || t.Y >= f.Height {
			continue
		}
		if !g.Blocked(t.X, t.Y) {
			return true
		}
		for dy := -maxSearchRadius; dy <= maxSearchRadius; dy++ {
			for dx := -maxSearchRadius; dx <= maxSearchRadius; dx++ {
				if (dx != 0 || dy != 0) && !g.Blocked(t.X+dx, t.Y+dy) {
					return true
				}
			}
		}
	}
	return false
}

// seedVirtualTargets seeds the first ring around a blocked target with a passable
// cell, each at its axis-weighted gap, leaving the target itself unmarked.
func (f *FlowField) seedVirtualTargets(g *SearchGrid, targetX, targetY int) {
	seedRing(g, &f.Costs, targetX, targetY, func(idx, cost int32) {
		if f.VisitedGen[idx] != f.CurrentGen || cost < f.Distances[idx] {
			f.Distances[idx] = cost
			f.VisitedGen[idx] = f.CurrentGen
			f.Directions[idx] = DirTarget
			g.push(idx, cost)
		}
	})
}

// seedRing calls seed for every passable cell of the first ring around (x, y)
// that holds one, out to maxSearchRadius, with the ring cell's gap cost.
func seedRing(g *SearchGrid, c *Costs, x, y int, seed func(idx, cost int32)) {
	for radius := 1; radius <= maxSearchRadius; radius++ {
		found := false
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if abs(dx) != radius && abs(dy) != radius || g.Blocked(x+dx, y+dy) {
					continue
				}
				seed(int32((y+dy)*g.w+x+dx), int32(abs(dx))*c.X+int32(abs(dy))*c.Y)
				found = true
			}
		}
		if found {
			return
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
