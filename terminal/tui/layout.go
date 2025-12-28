package tui

// Center returns a centered region of given size within outer
func Center(outer Region, w, h int) Region {
	x := (outer.W - w) / 2
	y := (outer.H - h) / 2
	return outer.Sub(x, y, w, h)
}

// SplitH splits region horizontally by ratios (0.0-1.0)
// Ratios are normalized if they don't sum to 1.0
func SplitH(r Region, ratios ...float64) []Region {
	if len(ratios) == 0 {
		return nil
	}

	// Normalize ratios
	var sum float64
	for _, ratio := range ratios {
		sum += ratio
	}
	if sum <= 0 {
		sum = 1
	}

	regions := make([]Region, len(ratios))
	x := 0
	remaining := r.W

	for i, ratio := range ratios {
		var w int
		if i == len(ratios)-1 {
			w = remaining // Last one gets remainder to avoid rounding gaps
		} else {
			w = int((float64(r.W) * ratio / sum) + 0.5) // Round to nearest cell
			if w > remaining {
				w = remaining
			}
		}
		regions[i] = r.Sub(x, 0, w, r.H)
		x += w
		remaining -= w
	}

	return regions
}

// SplitV splits region vertically by ratios (0.0-1.0)
func SplitV(r Region, ratios ...float64) []Region {
	if len(ratios) == 0 {
		return nil
	}

	var sum float64
	for _, ratio := range ratios {
		sum += ratio
	}
	if sum <= 0 {
		sum = 1
	}

	regions := make([]Region, len(ratios))
	y := 0
	remaining := r.H

	for i, ratio := range ratios {
		var h int
		if i == len(ratios)-1 {
			h = remaining
		} else {
			h = int(float64(r.H) * ratio / sum)
		}
		regions[i] = r.Sub(0, y, r.W, h)
		y += h
		remaining -= h
	}

	return regions
}

// SplitHFixed splits with fixed left width, rest to right
func SplitHFixed(r Region, leftW int) (left, right Region) {
	if leftW > r.W {
		leftW = r.W
	}
	if leftW < 0 {
		leftW = 0
	}
	left = r.Sub(0, 0, leftW, r.H)
	right = r.Sub(leftW, 0, r.W-leftW, r.H)
	return
}

// SplitVFixed splits with fixed top height, rest to bottom
func SplitVFixed(r Region, topH int) (top, bottom Region) {
	if topH > r.H {
		topH = r.H
	}
	if topH < 0 {
		topH = 0
	}
	top = r.Sub(0, 0, r.W, topH)
	bottom = r.Sub(0, topH, r.W, r.H-topH)
	return
}

// Columns calculates how many columns fit in width
func Columns(availableW, itemW, gap int) int {
	if itemW <= 0 {
		return 0
	}
	if availableW < itemW {
		return 0
	}
	// First item has no gap, subsequent items need gap + itemW
	cols := 1 + (availableW-itemW)/(itemW+gap)
	if cols < 0 {
		cols = 0
	}
	return cols
}

// GridLayout returns a grid of equally sized regions
func GridLayout(r Region, cols, rows, gapX, gapY int) []Region {
	if cols <= 0 || rows <= 0 {
		return nil
	}

	cellW := (r.W - gapX*(cols-1)) / cols
	cellH := (r.H - gapY*(rows-1)) / rows

	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}

	regions := make([]Region, cols*rows)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			x := col * (cellW + gapX)
			y := row * (cellH + gapY)
			regions[row*cols+col] = r.Sub(x, y, cellW, cellH)
		}
	}

	return regions
}

// FitOrScroll returns true if content exceeds available height
func FitOrScroll(contentH, availableH int) bool {
	return contentH > availableH
}

// BreakpointH returns index of first breakpoint <= w
// Breakpoints should be in descending order
// Returns len(breakpoints) if w is less than all breakpoints
func BreakpointH(w int, breakpoints ...int) int {
	for i, bp := range breakpoints {
		if w >= bp {
			return i
		}
	}
	return len(breakpoints)
}

// BreakpointV returns index of first breakpoint <= h
func BreakpointV(h int, breakpoints ...int) int {
	return BreakpointH(h, breakpoints...)
}