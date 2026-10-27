package navigation

import (
	"slices"

	"github.com/lixenwraith/vif/pkg/vmath"
)

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
	touched []int32 // headers whose count reached or left zero, Moved's scratch
}

// NewCompositePassability creates passability grid for given footprint dimensions
// headerOffX/Y: offset from top-left to header position (e.g., 2,1 for 5×3 eye)
func NewCompositePassability(mapW, mapH, footW, footH, headerOffX, headerOffY int) *CompositePassability {
	size := mapW * mapH
	return &CompositePassability{
		Width:      mapW,
		Height:     mapH,
		FootprintW: footW,
		FootprintH: footH,
		HeaderOffX: headerOffX,
		HeaderOffY: headerOffY,
		Valid:      make([]bool, size),
		walls:      make([]uint16, size),
	}
}

// Clone returns a grid sharing no storage with p.
func (p *CompositePassability) Clone() *CompositePassability {
	n := *p
	n.Valid, n.walls, n.touched = slices.Clone(p.Valid), slices.Clone(p.walls), nil
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
// and its wall summed down a footprint's height, then across its width.
func (p *CompositePassability) Compute(isWall WallChecker) bool {
	w, h := p.Width, p.Height
	if len(p.walls) != w*h {
		p.walls = make([]uint16, w*h)
	}
	for y := range h {
		for x := range w {
			p.walls[y*w+x] = 0
			if isWall(x, y) {
				p.walls[y*w+x] = 1
			}
		}
	}
	line := make([]uint16, max(w, h))
	// window sums, along a line of n cells, every footprint's span of size cells
	// from off before its header to the header's count
	window := func(n, size, off int, at func(i int) *uint16) {
		for i := range n {
			line[i] = *at(i)
		}
		var run uint16
		for lead := -off; lead < n-off+size; lead++ {
			if lead >= 0 && lead < n {
				run += line[lead]
			}
			if out := lead - size; out >= 0 && out < n {
				run -= line[out]
			}
			if hd := lead + off - size + 1; hd >= 0 && hd < n {
				*at(hd) = run
			}
		}
	}
	for x := range w {
		window(h, p.FootprintH, p.HeaderOffY, func(y int) *uint16 { return &p.walls[y*w+x] })
	}
	changed := false
	for y := range h {
		window(w, p.FootprintW, p.HeaderOffX, func(x int) *uint16 { return &p.walls[y*w+x] })
		for x := range w {
			i := y*w + x
			if v := p.walls[i] == 0 && p.fits(x, y); v != p.Valid[i] {
				p.Valid[i], changed = v, true
			}
		}
	}
	return changed
}

// fits reports whether the footprint at header (x, y) lies within the grid
func (p *CompositePassability) fits(x, y int) bool {
	x0, y0 := x-p.HeaderOffX, y-p.HeaderOffY
	return x0 >= 0 && y0 >= 0 && x0+p.FootprintW <= p.Width && y0+p.FootprintH <= p.Height
}

// Moved brings passability up to cells that closed and opened since it was
// computed, each named once, and calls flip once for every header whose fit
// changed, in grid order, with its new validity.
func (p *CompositePassability) Moved(closed, opened []vmath.Point, flip func(x, y int, valid bool)) {
	p.touched = p.touched[:0]
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
				if p.walls[i] == 0 || wall && p.walls[i] == 1 { // reached or left zero
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
	slices.Sort(p.touched)
	for _, i := range slices.Compact(p.touched) {
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

// DebugStats returns total/valid/blocked cell counts for diagnostics
func (p *CompositePassability) DebugStats() (total, valid, blocked int) {
	total = len(p.Valid)
	for _, v := range p.Valid {
		if v {
			valid++
		} else {
			blocked++
		}
	}
	return total, valid, blocked
}

// GetDimensions returns grid dimensions
func (p *CompositePassability) GetDimensions() (width, height int) {
	return p.Width, p.Height
}

// GetFootprint returns footprint configuration
func (p *CompositePassability) GetFootprint() (footW, footH, offX, offY int) {
	return p.FootprintW, p.FootprintH, p.HeaderOffX, p.HeaderOffY
}
