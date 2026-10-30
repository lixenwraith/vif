package position

import (
	"math"

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
// for a width×height area free of walls matching mask (0 = any wall). The anchor
// offset is the header's position from the area's top-left; maxRadius 0 means
// DefaultSearchRadius, and a negative one, MapSearchRadius: rays reaching every
// row and column of the map.
// Returns the area's top-left and whether one was found.
func (x *Index[E]) FindFreeAreaSpiral(
	originX, originY int,
	width, height int,
	anchorOffsetX, anchorOffsetY int,
	mask Mask,
	maxRadius int,
) (int, int, bool) {
	px, py, found := x.SpiralSearch(originX, originY, maxRadius, func(px, py int) bool {
		return x.IsAreaFree(px-anchorOffsetX, py-anchorOffsetY, width, height, mask)
	})
	if !found {
		return 0, 0, false
	}
	return px - anchorOffsetX, py - anchorOffsetY, true
}

// SpiralSearch is FindFreeAreaSpiral's walk for a footprint no rectangle holds: the
// origin first, then each radius in 8 directions, fewer rows than columns so the
// ring reads as round on the cell aspect, to the first cell fits takes. maxRadius
// is FindFreeAreaSpiral's.
func (x *Index[E]) SpiralSearch(originX, originY, maxRadius int, fits func(px, py int) bool) (int, int, bool) {
	switch {
	case maxRadius < 0:
		maxRadius = x.MapSearchRadius()
	case maxRadius == 0:
		maxRadius = DefaultSearchRadius
	}
	if fits(originX, originY) {
		return originX, originY, true
	}
	aspect := x.frame.Aspect()
	for radius := 1; radius <= maxRadius; radius++ {
		vertRadius := int(math.Ceil(aspect.FromCircular(float64(radius))))
		for _, dir := range spiralDirs {
			px, py := originX+dir[0]*radius, originY+dir[1]*vertRadius
			if fits(px, py) {
				return px, py, true
			}
		}
	}
	return 0, 0, false
}

// FindFreeAreaInside is FindFreeAreaSpiral from the origin pulled in from the
// map's edges until the area with its anchor there lies inside the map: an origin
// on an edge, or past it, searches from the area flush against that edge on the
// origin's row or column, whichever edge it is, so a caller names an edge cell
// blind to the area's size and anchor. An area larger than the map finds nothing.
func (x *Index[E]) FindFreeAreaInside(
	originX, originY int,
	width, height int,
	anchorOffsetX, anchorOffsetY int,
	mask Mask,
	maxRadius int,
) (int, int, bool) {
	w, h := x.frame.Bounds()
	originX = max(anchorOffsetX, min(originX, w-width+anchorOffsetX))
	originY = max(anchorOffsetY, min(originY, h-height+anchorOffsetY))
	return x.FindFreeAreaSpiral(originX, originY, width, height, anchorOffsetX, anchorOffsetY, mask, maxRadius)
}

// MapSearchRadius is the spiral radius whose rays reach every column and every row
// of the map from any cell in it, rows being searched at the cell aspect
func (x *Index[E]) MapSearchRadius() int {
	w, h := x.frame.Bounds()
	return max(w, int(math.Ceil(x.frame.Aspect().Circular(float64(h)))))
}

// IsAreaFree reports whether [x, x+width) × [y, y+height) lies whole inside the
// map and holds no wall matching mask
func (x *Index[E]) IsAreaFree(px, py, width, height int, mask Mask) bool {
	w, h := x.frame.Bounds()
	if px < 0 || py < 0 || px+width > w || py+height > h {
		return false
	}
	return !x.BlocksArea(px, py, width, height, mask)
}

// IsBlocked reports whether (x, y) is outside the map or a wall matching mask
func (x *Index[E]) IsBlocked(px, py int, mask Mask) bool {
	w, h := x.frame.Bounds()
	if px < 0 || px >= w || py < 0 || py >= h {
		return true
	}
	return x.blocks(px, py, w, h, mask)
}

// IsOutOfBounds reports whether (x, y) is outside the map
func (x *Index[E]) IsOutOfBounds(px, py int) bool {
	w, h := x.frame.Bounds()
	return px < 0 || px >= w || py < 0 || py >= h
}

// CheckBlockedBatch reports for each point whether it is outside the map or a wall
// matching mask, aligned with the points
func (x *Index[E]) CheckBlockedBatch(points []vmath.Point, mask Mask) []bool {
	result := make([]bool, len(points))
	for i, pt := range points {
		result[i] = x.IsBlocked(pt.X, pt.Y, mask)
	}
	return result
}

// IsAnyBlockedInSet reports whether any point is outside the map or a wall
// matching mask
func (x *Index[E]) IsAnyBlockedInSet(points []vmath.Point, mask Mask) bool {
	for _, pt := range points {
		if x.IsBlocked(pt.X, pt.Y, mask) {
			return true
		}
	}
	return false
}

// HasLineOfSight reports whether no cell strictly between (x0, y0) and (x1, y1) on
// the Bresenham line is a wall matching mask
func (x *Index[E]) HasLineOfSight(x0, y0, x1, y1 int, mask Mask) bool {
	w, h := x.frame.Bounds()
	for line := vmath.NewLine(x0, y0, x1, y1).Next(); !line.AtEnd(); line = line.Next() {
		if x.blocks(line.X(), line.Y(), w, h, mask) {
			return false
		}
	}
	return true
}

// FindLastFreeOnRay returns the last unblocked cell on a ray from (startX, startY)
// toward (endX, endY), the start taken as free, and whether the whole ray was
func (x *Index[E]) FindLastFreeOnRay(startX, startY, endX, endY int, mask Mask) (int, int, bool) {
	w, h := x.frame.Bounds()
	lastFreeX, lastFreeY := startX, startY
	line := vmath.NewLine(startX, startY, endX, endY)
	for !line.AtEnd() {
		line = line.Next()
		if x.blocks(line.X(), line.Y(), w, h, mask) {
			return lastFreeX, lastFreeY, false
		}
		lastFreeX, lastFreeY = line.X(), line.Y()
	}
	return lastFreeX, lastFreeY, true
}

// HasAreaLineOfSight reports whether a width×height body centred on each cell of
// the line strictly between (x0, y0) and (x1, y1) lies inside the map and clear of
// walls matching mask
func (x *Index[E]) HasAreaLineOfSight(x0, y0, x1, y1, width, height int, mask Mask) bool {
	// Degenerate case: point entity
	if width <= 1 && height <= 1 {
		return x.HasLineOfSight(x0, y0, x1, y1, mask)
	}

	w, h := x.frame.Bounds()
	halfW := width / 2
	halfH := height / 2

	for line := vmath.NewLine(x0, y0, x1, y1).Next(); !line.AtEnd(); line = line.Next() {
		boxX := line.X() - halfW
		boxY := line.Y() - halfH

		if boxX < 0 || boxY < 0 || boxX+width > w || boxY+height > h {
			return false
		}
		if x.BlocksArea(boxX, boxY, width, height, mask) {
			return false
		}
	}
	return true
}

// HasAreaLineOfSightRotatable is HasAreaLineOfSight for the body as given, then
// turned 90°
func (x *Index[E]) HasAreaLineOfSightRotatable(x0, y0, x1, y1, width, height int, mask Mask) bool {
	if x.HasAreaLineOfSight(x0, y0, x1, y1, width, height, mask) {
		return true
	}
	// Square entities cannot benefit from rotation
	if width == height {
		return false
	}
	return x.HasAreaLineOfSight(x0, y0, x1, y1, height, width, mask)
}

// Rect is an inclusive range of cells: the cross axis a directional scan is held to.
type Rect struct {
	MinX, MaxX int
	MinY, MaxY int
}

// FindClosestEntityInDirection scans from (startX, startY) in one cardinal
// direction, row by row or column by column to the grid's edge, each held to
// bounds on the cross axis, and returns the first row's or column's occupant
// nearest the start that scope selects and filter passes.
func (x *Index[E]) FindClosestEntityInDirection(startX, startY, dx, dy int, bounds Rect, scope Scope, filter func(E) bool) (E, int, int, bool) {
	var zero E
	g := x.Grid
	if dy != 0 {
		stepY := 1
		if dy < 0 {
			stepY = -1
		}
		for py := startY + stepY; py >= 0 && py < g.Height; py += stepY {
			var best E
			bestX, minDist, found := -1, math.MaxInt, false
			for px := bounds.MinX; px <= bounds.MaxX; px++ {
				if px < 0 || px >= g.Width {
					continue
				}
				for _, e := range g.Cells[py*g.Width+px].view(scope) {
					if filter == nil || filter(e) {
						if dist := vmath.IntAbs(px - startX); dist < minDist {
							minDist, best, bestX, found = dist, e, px, true
						}
					}
				}
			}
			// The first row holding anything settles it, on its occupant nearest the centre
			if found {
				return best, bestX, py, true
			}
		}
	} else if dx != 0 {
		stepX := 1
		if dx < 0 {
			stepX = -1
		}
		for px := startX + stepX; px >= 0 && px < g.Width; px += stepX {
			var best E
			bestY, minDist, found := -1, math.MaxInt, false
			for py := bounds.MinY; py <= bounds.MaxY; py++ {
				if py < 0 || py >= g.Height {
					continue
				}
				for _, e := range g.Cells[py*g.Width+px].view(scope) {
					if filter == nil || filter(e) {
						if dist := vmath.IntAbs(py - startY); dist < minDist {
							minDist, best, bestY, found = dist, e, py, true
						}
					}
				}
			}
			if found {
				return best, px, bestY, true
			}
		}
	}
	return zero, -1, -1, false
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

// FindFreeFromPattern searches 8 directions at expanding radii for a width×height
// area free of walls matching mask, centred on the search point, with Y offsets
// scaled to the cell aspect when aspectCorrect; additionalCheck, when not nil, may
// refuse a candidate. It returns the area's top-left.
func (x *Index[E]) FindFreeFromPattern(
	originX, originY int,
	width, height int,
	pattern Pattern,
	startRadius, maxRadius int,
	aspectCorrect bool,
	mask Mask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	w, h := x.frame.Bounds()
	aspect := x.frame.Aspect()
	direction := searchDirection(originX, w/2)

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

			// absX, absY is the top-left of the candidate centred on the search point
			absX := originX + offsetX - (width / 2)
			absY := originY + offsetY - (height / 2)

			if absX < 0 || absX+width > w || absY < 0 || absY+height > h {
				continue
			}
			if !x.IsAreaFree(absX, absY, width, height, mask) {
				continue
			}
			if additionalCheck != nil && !additionalCheck(absX, absY, width, height) {
				continue
			}
			return absX, absY, true
		}
	}

	return 0, 0, false
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

// FindPlacementAroundExclusion finds a place for a width×height object outside the
// exclusion around the anchor, padding cells off it, and returns its offset from
// the anchor: the 8 slots in pattern order, then at twice the distance, then
// without additionalCheck, then clamped to the map. Where no slot is clear, it
// returns the one overlapping the fewest blocked cells, with found false.
func (x *Index[E]) FindPlacementAroundExclusion(
	anchorX, anchorY int,
	objectW, objectH int,
	exclusion Exclusion,
	padding int,
	pattern Pattern,
	mask Mask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	w, h := x.frame.Bounds()
	direction := searchDirection(anchorX, w/2)

	// Centering offsets. An even width has no middle column, so it centres half a
	// cell right: a countdown's digits ink the left of the box they share.
	objHalfW := (objectW - 1) / 2
	objHalfH := objectH / 2
	hCenter := (exclusion.Right - exclusion.Left) / 2

	// 8 positions: cardinals (0-3), diagonals (4-7)
	offsets := [8][2]int{
		{hCenter - objHalfW, exclusion.Bottom + padding},                          // 0: Bottom
		{hCenter - objHalfW, -exclusion.Top - objectH - padding},                  // 1: Top
		{exclusion.Right + padding, -objHalfH},                                    // 2: Right
		{-exclusion.Left - objectW - padding, -objHalfH},                          // 3: Left
		{exclusion.Right + padding, exclusion.Bottom + padding},                   // 4: Bottom-right
		{-exclusion.Left - objectW - padding, exclusion.Bottom + padding},         // 5: Bottom-left
		{exclusion.Right + padding, -exclusion.Top - objectH + padding},           // 6: Top-right
		{-exclusion.Left - objectW - padding, -exclusion.Top - objectH + padding}, // 7: Top-left
	}

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

	// Primary: all 8 positions
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		if absX < 0 || absX+objectW > w || absY < 0 || absY+objectH > h {
			continue
		}
		if additionalCheck != nil && !additionalCheck(absX, absY, objectW, objectH) {
			continue
		}
		if !x.IsAreaFree(absX, absY, objectW, objectH, mask) {
			continue
		}
		return offsets[idx][0], offsets[idx][1], true
	}

	// Secondary: 2x distance
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]*2
		absY := anchorY + offsets[idx][1]*2

		if absX < 0 || absX+objectW > w || absY < 0 || absY+objectH > h {
			continue
		}
		if additionalCheck != nil && !additionalCheck(absX, absY, objectW, objectH) {
			continue
		}
		if !x.IsAreaFree(absX, absY, objectW, objectH, mask) {
			continue
		}
		return offsets[idx][0] * 2, offsets[idx][1] * 2, true
	}

	// Tertiary: skip additionalCheck, keep wall avoidance
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		if absX < 0 || absX+objectW > w || absY < 0 || absY+objectH > h {
			continue
		}
		if x.IsAreaFree(absX, absY, objectW, objectH, mask) {
			return offsets[idx][0], offsets[idx][1], true
		}
	}

	// Quaternary: clamp to bounds, skip walls
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		absX = max(0, min(absX, w-objectW))
		absY = max(0, min(absY, h-objectH))

		if x.BlocksArea(absX, absY, objectW, objectH, mask) {
			continue
		}
		return absX - anchorX, absY - anchorY, true
	}

	// Ultimate: every slot overlaps a wall, so the one, clamped to the map, at either
	// distance, overlapping the fewest blocked cells; ties keep the search order
	bestX, bestY, best := 0, 0, -1
	for _, scale := range [2]int{1, 2} {
		for _, idx := range order {
			absX := max(0, min(anchorX+offsets[idx][0]*scale, w-objectW))
			absY := max(0, min(anchorY+offsets[idx][1]*scale, h-objectH))
			if n := x.blockedCells(absX, absY, objectW, objectH, mask); best < 0 || n < best {
				bestX, bestY, best = absX, absY, n
			}
		}
	}
	return bestX - anchorX, bestY - anchorY, false
}

// searchDirection is CCW right of the centre column, CW otherwise
func searchDirection(originX, centerX int) Direction {
	if originX >= centerX {
		return SearchCCW
	}
	return SearchCW
}
