package position

import (
	"math"
	"math/bits"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// DefaultSearchRadius is the spiral radius FindFreeAreaSpiral walks when a caller
// names none
const DefaultSearchRadius = 20

// spiralDirs are the spiral's directions, counter-clockwise from the top: top,
// top-left, left, bottom-left, bottom, bottom-right, right, top-right
var spiralDirs = [8][2]int{
	{0, -1}, {-1, -1}, {-1, 0}, {-1, 1}, {0, 1}, {1, 1}, {1, 0}, {1, -1},
}

// FindFreeAreaSpiral searches outward from origin, 45° counter-clockwise from top,
// for a header cell whose body is free of walls matching mask (0 = any wall);
// maxRadius 0 means DefaultSearchRadius, and a negative one, MapSearchRadius: rays
// reaching every row and column of the map. Returns the body's corner nearest the
// map's origin, its top-left, or the zero point and false when none is free.
func (x *Index[E]) FindFreeAreaSpiral[P Coord, F Body](origin P, foot F, mask Mask, maxRadius int) (P, bool) {
	f, headerZ, depth, _ := body(foot)
	plane := x.probe(mask)
	header, found := x.SpiralSearch(origin, maxRadius, func(p P) bool {
		c, _ := at(p)
		return x.clear(plane, f.At(c.X, c.Y), c.Z-headerZ, depth)
	})
	if !found {
		return header, false
	}
	c, _ := at(header)
	return as[P](Point3{X: c.X - f.HeaderX, Y: c.Y - f.HeaderY, Z: c.Z - headerZ}), true
}

// SpiralSearch is FindFreeAreaSpiral's walk for a body no footprint holds: the
// origin first, then each radius in 8 directions of its layer, fewer rows than
// columns so the ring reads as round on the cell aspect, to the first cell fits
// takes. maxRadius is FindFreeAreaSpiral's.
func (x *Index[E]) SpiralSearch[P Coord](origin P, maxRadius int, fits func(P) bool) (P, bool) {
	switch {
	case maxRadius < 0:
		maxRadius = x.MapSearchRadius()
	case maxRadius == 0:
		maxRadius = DefaultSearchRadius
	}
	if fits(origin) {
		return origin, true
	}
	o, _ := at(origin)
	aspect := x.frame.Aspect()
	for radius := 1; radius <= maxRadius; radius++ {
		vertRadius := int(math.Ceil(aspect.FromCircular(float64(radius))))
		for _, dir := range spiralDirs {
			p := as[P](Point3{X: o.X + dir[0]*radius, Y: o.Y + dir[1]*vertRadius, Z: o.Z})
			if fits(p) {
				return p, true
			}
		}
	}
	var none P
	return none, false
}

// FindFreeAreaInside is FindFreeAreaSpiral from the origin pulled in from the
// map's edges until the body with its header there lies inside the map: an origin
// on an edge, or past it, searches from the body flush against that edge on the
// origin's row or column, so a caller names an edge cell blind to the body. A body
// larger than the map finds nothing.
func (x *Index[E]) FindFreeAreaInside[P Coord, F Body](origin P, foot F, mask Mask, maxRadius int) (P, bool) {
	o, _ := at(origin)
	f, headerZ, depth, _ := body(foot)
	o.X = max(f.HeaderX, min(o.X, x.Width-f.Width+f.HeaderX))
	o.Y = max(f.HeaderY, min(o.Y, x.Height-f.Height+f.HeaderY))
	o.Z = max(headerZ, min(o.Z, x.Depth-depth+headerZ))
	return x.FindFreeAreaSpiral(as[P](o), foot, mask, maxRadius)
}

// MapSearchRadius is the spiral radius whose rays reach every column and every row
// of the map from any cell in it, rows being searched at the cell aspect
func (x *Index[E]) MapSearchRadius() int {
	return max(x.Width, int(math.Ceil(x.frame.Aspect().Circular(float64(x.Height)))))
}

// IsAreaFree reports whether r lies whole inside the map and holds no wall
// matching mask
func (x *Index[E]) IsAreaFree[R Region](r R, mask Mask) bool {
	a, z, depth, _ := box(r)
	return x.clear(x.probe(mask), a, z, depth)
}

// IsBlocked reports whether p's cell is outside the map or a wall matching mask
func (x *Index[E]) IsBlocked[P Coord](p P, mask Mask) bool {
	c, flat := at(p)
	return !x.in(c, flat) || x.probe(mask).blocked(c.X, x.row(c, flat))
}

// HasLineOfSight reports whether no cell strictly between a and b on the line is a
// wall matching mask: the Bresenham line on a flat map, vmath.Line3 through a
// volume, which in one layer walks the same cells
func (x *Index[E]) HasLineOfSight[P Coord](a, b P, mask Mask) bool {
	plane := x.probe(mask)
	ca, flat := at(a)
	cb, _ := at(b)
	if flat {
		for line := vmath.NewLine(ca.X, ca.Y, cb.X, cb.Y).Next(); !line.AtEnd(); line = line.Next() {
			if px, py := line.X(), line.Y(); uint(px) < uint(x.Width) && uint(py) < uint(x.Height) && plane.blocked(px, py) {
				return false
			}
		}
		return true
	}
	line := vmath.NewLine3(ca, cb)
	for line.Advance(); !line.AtEnd(); line.Advance() {
		if c := line.Point(); x.in(c, false) && plane.blocked(c.X, x.row(c, false)) {
			return false
		}
	}
	return true
}

// FindLastFreeOnRay returns the last unblocked cell on a ray from start toward
// end, the start taken as free, and whether the whole ray was
func (x *Index[E]) FindLastFreeOnRay[P Coord](start, end P, mask Mask) (P, bool) {
	last, _ := at(start)
	to, _ := at(end)
	for line := vmath.NewLine3(last, to); !line.AtEnd(); last = line.Point() {
		if line.Advance(); x.Blocks(line.Point(), mask) {
			return as[P](last), false
		}
	}
	return as[P](last), true
}

// HasAreaLineOfSight reports whether a body of the footprint lies inside the map and
// clear of walls matching mask with its header on each cell of the line strictly
// between a and b
func (x *Index[E]) HasAreaLineOfSight[P Coord, F Body](a, b P, foot F, mask Mask) bool {
	f, headerZ, depth, thin := body(foot)
	if f.Width <= 1 && f.Height <= 1 && depth <= 1 {
		return x.HasLineOfSight(a, b, mask)
	}
	ca, flat := at(a)
	cb, _ := at(b)
	plane := x.probe(mask)
	if flat && thin {
		for line := vmath.NewLine(ca.X, ca.Y, cb.X, cb.Y).Next(); !line.AtEnd(); line = line.Next() {
			px, py := line.X()-f.HeaderX, line.Y()-f.HeaderY
			if px < 0 || py < 0 || px+f.Width > x.Width || py+f.Height > x.Height ||
				plane.any(px, px+f.Width, py, py+f.Height) {
				return false
			}
		}
		return true
	}
	line := vmath.NewLine3(ca, cb)
	for line.Advance(); !line.AtEnd(); line.Advance() {
		if c := line.Point(); !x.clear(plane, f.At(c.X, c.Y), c.Z-headerZ, depth) {
			return false
		}
	}
	return true
}

// Rect is an inclusive range of cells: the cross axis a directional scan is held to.
type Rect struct {
	MinX, MaxX int
	MinY, MaxY int
}

// FindClosestEntityInDirection scans start's layer from it in one cardinal
// direction, row by row or column by column to the grid's edge, each held to
// bounds on the cross axis, and returns the first row's or column's occupant
// nearest the start that scope selects and filter passes, with its cell. It reads
// the presence bits a word at a time, so empty cells cost it nothing.
func (x *Index[E]) FindClosestEntityInDirection[P Coord](start P, dx, dy int, bounds Rect, scope Scope, filter func(E) bool) (E, P, bool) {
	var best E
	var none P
	g := x.Grid
	from, flat := at(start)
	if !flat && uint(from.Z) >= uint(g.Depth) {
		return best, none, false
	}
	startX, startY, base := from.X, from.Y, from.Z*g.Height // base the first of the layer's rows
	hit, minDist := -1, math.MaxInt
	// nearest keeps the occupant of cell (px, py) nearest the start on the cross axis
	nearest := func(px, py, dist, cross int) {
		for _, e := range g.view(&g.cells[(base+py)*g.Width+px], scope) {
			if dist < minDist && (filter == nil || filter(e)) {
				minDist, best, hit = dist, e, cross
			}
		}
	}
	if dy != 0 {
		step := 1
		if dy < 0 {
			step = -1
		}
		x0, x1 := max(bounds.MinX, 0), min(bounds.MaxX+1, g.Width)
		if x0 >= x1 {
			return best, none, false
		}
		first, last := ^uint64(0)<<(uint(x0)&63), ^uint64(0)>>(63-uint(x1-1)&63)
		for py := startY + step; py >= 0 && py < g.Height; py += step {
			for w := x0 >> 6; w <= (x1-1)>>6; w++ {
				word := g.occupied(base+py, w, scope)
				if w == x0>>6 {
					word &= first
				}
				if w == (x1-1)>>6 {
					word &= last
				}
				for ; word != 0; word &= word - 1 {
					px := w<<6 + bits.TrailingZeros64(word)
					nearest(px, py, vmath.IntAbs(px-startX), px)
				}
			}
			// The first row holding anything settles it, on its occupant nearest the centre
			if hit >= 0 {
				return best, as[P](Point3{X: hit, Y: py, Z: from.Z}), true
			}
		}
	} else if dx != 0 {
		step := 1
		if dx < 0 {
			step = -1
		}
		y0, y1 := max(bounds.MinY, 0), min(bounds.MaxY+1, g.Height)
		px := startX + step
		if y0 >= y1 || px < 0 || px >= g.Width {
			return best, none, false
		}
		for w := px >> 6; w >= 0 && w < g.present[ClassShared].Stride; w += step {
			var word uint64
			for py := y0; py < y1; py++ {
				word |= g.occupied(base+py, w, scope)
			}
			if w == px>>6 && step > 0 { // the columns from px on, the way the scan goes
				word &= ^uint64(0) << (uint(px) & 63)
			} else if w == px>>6 {
				word &= ^uint64(0) >> (63 - uint(px)&63)
			}
			for word != 0 {
				b := bits.TrailingZeros64(word)
				if step < 0 {
					b = 63 - bits.LeadingZeros64(word)
				}
				word &^= 1 << uint(b)
				col := w<<6 + b
				for py := y0; py < y1; py++ {
					if g.holds(col, base+py, scope) {
						nearest(col, py, vmath.IntAbs(py-startY), py)
					}
				}
				if hit >= 0 {
					return best, as[P](Point3{X: col, Y: hit, Z: from.Z}), true
				}
			}
		}
	}
	return best, none, false
}

// Pattern is the order FindFreeFromPattern tries its directions in
type Pattern uint8

const (
	// PatternCardinalFirst searches cardinals (N,S,E,W) then diagonals
	PatternCardinalFirst Pattern = iota
	// PatternDiagonalFirst searches diagonals then cardinals
	PatternDiagonalFirst
)

// Direction is the way a pattern turns
type Direction uint8

const (
	SearchCW Direction = iota
	SearchCCW
)

// patternDirections are the 8 unit offsets, 0 the top, proceeding clockwise
var patternDirections = [8][2]int{
	{0, -1},  // 0: N
	{1, -1},  // 1: NE
	{1, 0},   // 2: E
	{1, 1},   // 3: SE
	{0, 1},   // 4: S
	{-1, 1},  // 5: SW
	{-1, 0},  // 6: W
	{-1, -1}, // 7: NW
}

// Angle orders for pattern searches, as patternDirections indices
var (
	cardinalFirstCW  = [8]int{0, 2, 4, 6, 1, 3, 5, 7} // N→E→S→W, then NE→SE→SW→NW
	cardinalFirstCCW = [8]int{0, 6, 4, 2, 7, 5, 3, 1} // N→W→S→E, then NW→SW→SE→NE
	diagonalFirstCW  = [8]int{1, 3, 5, 7, 0, 2, 4, 6} // NE→SE→SW→NW, then N→E→S→W
	diagonalFirstCCW = [8]int{7, 5, 3, 1, 0, 6, 4, 2} // NW→SW→SE→NE, then N→W→S→E
)

// FindFreeFromPattern searches 8 directions of origin's layer at expanding radii
// for a width×height area free of walls matching mask, centred on the search
// point, with Y offsets scaled to the cell aspect when aspectCorrect;
// additionalCheck, when not nil, may refuse a candidate by its top-left. It
// returns the area's top-left.
func (x *Index[E]) FindFreeFromPattern[P Coord](
	origin P,
	width, height int,
	pattern Pattern,
	startRadius, maxRadius int,
	aspectCorrect bool,
	mask Mask,
	additionalCheck func(topLeft P, w, h int) bool,
) (P, bool) {
	o, _ := at(origin)
	aspect := x.frame.Aspect()
	direction := searchDirection(o.X, x.Width/2)

	var order [8]int
	switch {
	case pattern == PatternCardinalFirst && direction == SearchCW:
		order = cardinalFirstCW
	case pattern == PatternCardinalFirst && direction == SearchCCW:
		order = cardinalFirstCCW
	case pattern == PatternDiagonalFirst && direction == SearchCW:
		order = diagonalFirstCW
	default:
		order = diagonalFirstCCW
	}

	for radius := startRadius; radius <= maxRadius; radius++ {
		for _, idx := range order {
			dir := patternDirections[idx]

			// Integer circular approximation: diagonals scale by 7/10 for 1/√2
			r := radius
			if dir[0] != 0 && dir[1] != 0 {
				r = (radius * vmath.DiagonalNum) / vmath.DiagonalDen
			}

			offsetX := dir[0] * r
			offsetY := dir[1] * r

			if aspectCorrect {
				offsetY = int(aspect.FromCircular(float64(offsetY)))
			}

			// The candidate centred on the search point, on its layer
			area := vmath.Area{X: o.X + offsetX - (width / 2), Y: o.Y + offsetY - (height / 2), Width: width, Height: height}
			if !x.clear(x.probe(mask), area, o.Z, 1) {
				continue
			}
			topLeft := as[P](Point3{X: area.X, Y: area.Y, Z: o.Z})
			if additionalCheck != nil && !additionalCheck(topLeft, width, height) {
				continue
			}
			return topLeft, true
		}
	}

	var none P
	return none, false
}

// Exclusion is a rectangular keep-out zone relative to an anchor point
type Exclusion struct {
	Left, Right, Top, Bottom int
}

// Orders relative to the anchor over the offsets: 0 bottom, 1 top, 2 right, 3 left,
// 4 bottom-right, 5 bottom-left, 6 top-right, 7 top-left
var (
	anchorRelativeCardinalFirstCW  = [8]int{0, 2, 1, 3, 4, 6, 7, 5} // Bottom→Right→Top→Left, then BR→TR→TL→BL
	anchorRelativeCardinalFirstCCW = [8]int{0, 3, 1, 2, 5, 7, 6, 4} // Bottom→Left→Top→Right, then BL→TL→TR→BR
	anchorRelativeDiagonalFirstCW  = [8]int{4, 6, 7, 5, 0, 2, 1, 3} // BR→TR→TL→BL, then Bottom→Right→Top→Left
	anchorRelativeDiagonalFirstCCW = [8]int{5, 7, 6, 4, 0, 3, 1, 2} // BL→TL→TR→BR, then Bottom→Left→Top→Right
)

// exclusionSlots are the 8 places for a width×height object around an exclusion,
// padding cells off it, as its top-left's offsets from the anchor: the cardinals
// (0-3), then the diagonals (4-7)
func exclusionSlots(objectW, objectH int, exclusion Exclusion, padding int) [8][2]int {
	// An even width has no middle column, so it centres half a cell right: a
	// countdown's digits ink the left of the box they share.
	objHalfW := (objectW - 1) / 2
	objHalfH := objectH / 2
	hCenter := (exclusion.Right - exclusion.Left) / 2
	return [8][2]int{
		{hCenter - objHalfW, exclusion.Bottom + padding},                          // 0: Bottom
		{hCenter - objHalfW, -exclusion.Top - objectH - padding},                  // 1: Top
		{exclusion.Right + padding, -objHalfH},                                    // 2: Right
		{-exclusion.Left - objectW - padding, -objHalfH},                          // 3: Left
		{exclusion.Right + padding, exclusion.Bottom + padding},                   // 4: Bottom-right
		{-exclusion.Left - objectW - padding, exclusion.Bottom + padding},         // 5: Bottom-left
		{exclusion.Right + padding, -exclusion.Top - objectH + padding},           // 6: Top-right
		{-exclusion.Left - objectW - padding, -exclusion.Top - objectH + padding}, // 7: Top-left
	}
}

// FindPlacementAroundExclusion finds a place on the anchor's layer for a
// width×height object outside the exclusion around the anchor, padding cells off
// it, and returns its offset from the anchor: the 8 slots in pattern order, then
// at twice the distance, then without additionalCheck, then clamped to the map,
// then, with found false, the slot overlapping the fewest blocked cells.
func (x *Index[E]) FindPlacementAroundExclusion[P Coord](
	anchor P,
	objectW, objectH int,
	exclusion Exclusion,
	padding int,
	pattern Pattern,
	mask Mask,
	additionalCheck func(topLeft P, w, h int) bool,
) (int, int, bool) {
	a, _ := at(anchor)
	w, h := x.Width, x.Height
	direction := searchDirection(a.X, w/2)

	offsets := exclusionSlots(objectW, objectH, exclusion, padding)

	var order [8]int
	switch {
	case pattern == PatternCardinalFirst && direction == SearchCW:
		order = anchorRelativeCardinalFirstCW
	case pattern == PatternCardinalFirst && direction == SearchCCW:
		order = anchorRelativeCardinalFirstCCW
	case pattern == PatternDiagonalFirst && direction == SearchCW:
		order = anchorRelativeDiagonalFirstCW
	default:
		order = anchorRelativeDiagonalFirstCCW
	}

	// The 8 slots, then at twice the distance, then without additionalCheck, which
	// asks the first pass again where there is none
	for _, pass := range [3]struct {
		scale   int
		checked bool
	}{{1, true}, {2, true}, {1, false}} {
		if !pass.checked && additionalCheck == nil {
			break
		}
		for _, idx := range order {
			dx, dy := offsets[idx][0]*pass.scale, offsets[idx][1]*pass.scale
			slot := vmath.Area{X: a.X + dx, Y: a.Y + dy, Width: objectW, Height: objectH}
			if slot.X < 0 || slot.X+objectW > w || slot.Y < 0 || slot.Y+objectH > h {
				continue
			}
			if pass.checked && additionalCheck != nil && !additionalCheck(as[P](Point3{X: slot.X, Y: slot.Y, Z: a.Z}), objectW, objectH) {
				continue
			}
			if x.clear(x.probe(mask), slot, a.Z, 1) {
				return dx, dy, true
			}
		}
	}

	// Clamped to the map, the first slot off every wall
	for _, idx := range order {
		absX := max(0, min(a.X+offsets[idx][0], w-objectW))
		absY := max(0, min(a.Y+offsets[idx][1], h-objectH))
		if !x.BlocksArea(vmath.Box{X: absX, Y: absY, Z: a.Z, Width: objectW, Height: objectH, Depth: 1}, mask) {
			return absX - a.X, absY - a.Y, true
		}
	}

	// Every slot overlaps a wall, so the one, clamped to the map, at either
	// distance, overlapping the fewest blocked cells; ties keep the search order
	bestX, bestY, best := 0, 0, -1
	for _, scale := range [2]int{1, 2} {
		for _, idx := range order {
			absX := max(0, min(a.X+offsets[idx][0]*scale, w-objectW))
			absY := max(0, min(a.Y+offsets[idx][1]*scale, h-objectH))
			if n := x.blockedCells(vmath.Area{X: absX, Y: absY, Width: objectW, Height: objectH}, a.Z, mask); best < 0 || n < best {
				bestX, bestY, best = absX, absY, n
			}
		}
	}
	return bestX - a.X, bestY - a.Y, false
}

// searchDirection is CCW right of the centre column, CW otherwise
func searchDirection(originX, centerX int) Direction {
	if originX >= centerX {
		return SearchCCW
	}
	return SearchCW
}
