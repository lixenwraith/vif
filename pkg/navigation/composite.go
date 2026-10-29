package navigation

import (
	"slices"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Footprint is a body's cells around the header it navigates by: Width×Height
// cells, the header HeaderX, HeaderY from the top-left
type Footprint struct {
	Width, Height, HeaderX, HeaderY int
}

// Point is the footprint of a body one cell across
var Point = Footprint{Width: 1, Height: 1}

// Covers reports whether f's cells around a header hold o's around the same
// header, so every header f fits on o fits on too
func (f Footprint) Covers(o Footprint) bool {
	return f.HeaderX >= o.HeaderX && f.HeaderY >= o.HeaderY &&
		f.Width-f.HeaderX >= o.Width-o.HeaderX && f.Height-f.HeaderY >= o.Height-o.HeaderY
}

// CompositePassability pre-computes valid header positions for a fixed footprint
// A cell is valid iff the entire footprint fits within bounds and contains no walls
type CompositePassability struct {
	Width, Height          int
	FootprintW, FootprintH int
	HeaderOffX, HeaderOffY int
	Valid                  []bool

	// walls counts the wall cells under each header's footprint, clipped to the
	// grid, so a cell that turns moves only the headers over it (Moved)
	walls   []uint16
	cells   []uint16 // Compute's scratch: each cell's wall, and a row's sums
	line    []uint16
	touched []int32  // headers whose count reached or left zero, Moved's scratch
	marks   []uint32 // the Moved each header was last touched in, so it is named once
	mark    uint32
}

// NewCompositePassability creates the passability grid of a footprint on a map
func NewCompositePassability(mapW, mapH int, foot Footprint) *CompositePassability {
	size := mapW * mapH
	return &CompositePassability{
		Width:      mapW,
		Height:     mapH,
		FootprintW: foot.Width,
		FootprintH: foot.Height,
		HeaderOffX: foot.HeaderX,
		HeaderOffY: foot.HeaderY,
		Valid:      make([]bool, size),
		walls:      make([]uint16, size),
	}
}

// Clone returns a grid sharing no storage with p.
func (p *CompositePassability) Clone() *CompositePassability {
	n := *p
	n.Valid, n.walls = slices.Clone(p.Valid), slices.Clone(p.walls)
	n.cells, n.line, n.touched, n.marks, n.mark = nil, nil, nil, nil, 0
	return &n
}

// Resize adjusts dimensions, invalidates all cells
func (p *CompositePassability) Resize(width, height int) {
	size := width * height
	if cap(p.Valid) < size {
		p.Valid = make([]bool, size)
	} else {
		p.Valid = p.Valid[:size]
		clear(p.Valid)
	}
	if cap(p.walls) < size {
		p.walls = make([]uint16, size)
	} else {
		p.walls = p.walls[:size]
		clear(p.walls)
	}
	p.Width = width
	p.Height = height
}

// Compute rebuilds passability grid from wall state and reports whether any cell changed
// isWall: returns true if cell blocks composite movement. Each cell is asked once,
// and its wall summed down a footprint's height a row at a time, then across its width.
func (p *CompositePassability) Compute(isWall WallChecker) bool {
	w, h := p.Width, p.Height
	if len(p.walls) != w*h {
		p.walls = make([]uint16, w*h)
	}
	p.cells, p.line = grow(p.cells, w*h), grow(p.line, 2*w)
	for y := range h {
		row := p.cells[y*w : y*w+w]
		for x := range row {
			row[x] = 0
			if isWall(x, y) {
				row[x] = 1
			}
		}
	}
	cellRow := func(r int) []uint16 { // row r's walls, none off the grid
		if r < 0 || r >= h {
			return nil
		}
		return p.cells[r*w : r*w+w]
	}
	run, line := p.line[:w], p.line[w:]
	clear(run)
	for r := -p.HeaderOffY; r < p.FootprintH-p.HeaderOffY; r++ {
		for x, c := range cellRow(r) {
			run[x] += c
		}
	}
	changed := false
	for y := range h {
		row := p.walls[y*w : y*w+w]
		copy(line, run)
		for x, c := range cellRow(y - p.HeaderOffY) {
			run[x] -= c
		}
		for x, c := range cellRow(y - p.HeaderOffY + p.FootprintH) {
			run[x] += c
		}
		slide(row, line, p.FootprintW, p.HeaderOffX)
		for x, n := range row {
			if v := n == 0 && p.fits(x, y); v != p.Valid[y*w+x] {
				p.Valid[y*w+x], changed = v, true
			}
		}
	}
	return changed
}

// slide sets out[i] to the sum of in over a footprint's span of size cells from off
// before i, clipped to the line
func slide(out, in []uint16, size, off int) {
	var run uint16
	for j := max(-off, 0); j < min(size-off, len(in)); j++ {
		run += in[j]
	}
	for i := range out {
		out[i] = run
		if j := i - off; j >= 0 && j < len(in) {
			run -= in[j]
		}
		if j := i - off + size; j >= 0 && j < len(in) {
			run += in[j]
		}
	}
}

// fits reports whether the footprint at header (x, y) lies within the grid
func (p *CompositePassability) fits(x, y int) bool {
	x0, y0 := x-p.HeaderOffX, y-p.HeaderOffY
	return x0 >= 0 && y0 >= 0 && x0+p.FootprintW <= p.Width && y0+p.FootprintH <= p.Height
}

// Moved brings passability up to cells that closed and opened since it was
// computed, each named once, and calls flip once for every header whose fit
// changed, with its new validity.
func (p *CompositePassability) Moved(closed, opened []vmath.Point, flip func(x, y int, valid bool)) {
	p.touched = p.touched[:0]
	p.marks = grow(p.marks, len(p.walls))
	if p.mark++; p.mark == 0 {
		clear(p.marks[:cap(p.marks)])
		p.mark = 1
	}
	turn := func(c vmath.Point, wall bool) {
		if c.X < 0 || c.Y < 0 || c.X >= p.Width || c.Y >= p.Height {
			return
		}
		for hy := max(c.Y+p.HeaderOffY-p.FootprintH+1, 0); hy <= min(c.Y+p.HeaderOffY, p.Height-1); hy++ {
			for hx := max(c.X+p.HeaderOffX-p.FootprintW+1, 0); hx <= min(c.X+p.HeaderOffX, p.Width-1); hx++ {
				i := hy*p.Width + hx
				if wall {
					p.walls[i]++
				} else {
					p.walls[i]--
				}
				if (p.walls[i] == 0 || wall && p.walls[i] == 1) && p.marks[i] != p.mark { // reached or left zero
					p.marks[i] = p.mark
					p.touched = append(p.touched, int32(i))
				}
			}
		}
	}
	for _, c := range closed {
		turn(c, true)
	}
	for _, c := range opened {
		turn(c, false)
	}
	// A header two cells turned may stand where it was: only its count says
	for _, i := range p.touched {
		x, y := int(i)%p.Width, int(i)/p.Width
		if v := p.walls[i] == 0 && p.fits(x, y); v != p.Valid[i] {
			p.Valid[i] = v
			if flip != nil {
				flip(x, y, v)
			}
		}
	}
}

// IsBlocked returns true if composite cannot occupy header position (x,y)
// Used as WallChecker for composite flow field computation
func (p *CompositePassability) IsBlocked(x, y int) bool {
	if x < 0 || y < 0 || x >= p.Width || y >= p.Height {
		return true
	}
	return !p.Valid[y*p.Width+x]
}

// IsValid returns true if composite can occupy header position (x,y)
func (p *CompositePassability) IsValid(x, y int) bool {
	if x < 0 || y < 0 || x >= p.Width || y >= p.Height {
		return false
	}
	return p.Valid[y*p.Width+x]
}

// --- DEBUG ---

// GetDimensions returns grid dimensions
func (p *CompositePassability) GetDimensions() (width, height int) {
	return p.Width, p.Height
}

// Footprint is the footprint the grid fits
func (p *CompositePassability) Footprint() Footprint {
	return Footprint{Width: p.FootprintW, Height: p.FootprintH, HeaderX: p.HeaderOffX, HeaderY: p.HeaderOffY}
}
