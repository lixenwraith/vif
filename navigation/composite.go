package navigation

// CompositePassability pre-computes valid header positions for a fixed footprint
// A cell is valid iff the entire footprint fits within bounds and contains no walls
type CompositePassability struct {
	Width, Height          int
	FootprintW, FootprintH int
	HeaderOffX, HeaderOffY int
	Valid                  []bool
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
	}
}

// Resize adjusts dimensions, invalidates all cells
func (p *CompositePassability) Resize(width, height int) {
	size := width * height
	if cap(p.Valid) < size {
		p.Valid = make([]bool, size)
	} else {
		p.Valid = p.Valid[:size]
		for i := range p.Valid {
			p.Valid[i] = false
		}
	}
	p.Width = width
	p.Height = height
}

// Compute rebuilds passability grid from wall state
// isWall: returns true if cell blocks composite movement
func (p *CompositePassability) Compute(isWall WallChecker) {
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			p.Valid[y*p.Width+x] = p.canOccupy(x, y, isWall)
		}
	}
}

// canOccupy checks if composite footprint fits at header position (x,y)
func (p *CompositePassability) canOccupy(headerX, headerY int, isWall WallChecker) bool {
	topLeftX := headerX - p.HeaderOffX
	topLeftY := headerY - p.HeaderOffY

	// Bounds check
	if topLeftX < 0 || topLeftY < 0 ||
		topLeftX+p.FootprintW > p.Width ||
		topLeftY+p.FootprintH > p.Height {
		return false
	}

	// Wall check for entire footprint
	for dy := 0; dy < p.FootprintH; dy++ {
		for dx := 0; dx < p.FootprintW; dx++ {
			if isWall(topLeftX+dx, topLeftY+dy) {
				return false
			}
		}
	}
	return true
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

// ComputeROI rebuilds passability for header positions within [minX,maxX] × [minY,maxY]
// Bounds are clamped to grid dimensions. Footprint checks extend beyond ROI into the full map
// via isWall callback — only the iteration bounds are clamped
func (p *CompositePassability) ComputeROI(isWall WallChecker, minX, minY, maxX, maxY int) {
	minX = max(0, minX)
	minY = max(0, minY)
	maxX = min(p.Width-1, maxX)
	maxY = min(p.Height-1, maxY)

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			p.Valid[y*p.Width+x] = p.canOccupy(x, y, isWall)
		}
	}
}

// --- DEBUG ---

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