package tui

import "github.com/lixenwraith/vi-fighter/terminal"

// AdjustScroll returns new scroll offset keeping cursor visible
func AdjustScroll(cursor, scroll, visible, total int) int {
	if total <= visible {
		return 0
	}
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+visible {
		return cursor - visible + 1
	}
	return scroll
}

// ScrollPercent returns scroll position as 0-100 percentage
func ScrollPercent(scroll, visible, total int) int {
	if total <= visible {
		return 0
	}
	maxScroll := total - visible
	if maxScroll <= 0 {
		return 0
	}
	pct := (scroll * 100) / maxScroll
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// ScrollBar draws vertical scrollbar track with thumb
func ScrollBar(r Region, x int, offset, visible, total int, fg terminal.RGB) {
	if x < 0 || x >= r.W || r.H < 1 {
		return
	}

	trackH := r.H
	if total <= visible || trackH < 3 {
		// No scrolling needed or track too small
		for y := 0; y < trackH; y++ {
			r.Cell(x, y, '│', fg, terminal.RGB{}, terminal.AttrDim)
		}
		return
	}

	// Calculate thumb size and position
	thumbH := (visible * trackH) / total
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > trackH {
		thumbH = trackH
	}

	maxScroll := total - visible
	thumbY := 0
	if maxScroll > 0 {
		thumbY = (offset * (trackH - thumbH)) / maxScroll
	}
	if thumbY < 0 {
		thumbY = 0
	}
	if thumbY+thumbH > trackH {
		thumbY = trackH - thumbH
	}

	// Draw track and thumb
	for y := 0; y < trackH; y++ {
		var ch rune
		if y >= thumbY && y < thumbY+thumbH {
			ch = '█'
		} else {
			ch = '░'
		}
		r.Cell(x, y, ch, fg, terminal.RGB{}, terminal.AttrNone)
	}
}

// ScrollIndicator draws compact indicator text
// Returns one of: "Top", "Bot", or "XX%"
func ScrollIndicator(r Region, y int, offset, visible, total int, fg terminal.RGB) {
	if y < 0 || y >= r.H {
		return
	}

	var text string
	if total <= visible || offset <= 0 {
		text = "Top"
	} else if offset+visible >= total {
		text = "Bot"
	} else {
		pct := ScrollPercent(offset, visible, total)
		if pct >= 100 {
			text = "99%"
		} else if pct >= 10 {
			text = string(rune('0'+pct/10)) + string(rune('0'+pct%10)) + "%"
		} else {
			text = " " + string(rune('0'+pct)) + "%"
		}
	}

	r.TextRight(y, text, fg, terminal.RGB{}, terminal.AttrDim)
}

// PageDelta returns recommended page scroll amount
func PageDelta(visible int) int {
	delta := visible / 2
	if delta < 1 {
		delta = 1
	}
	return delta
}

// ClampScroll ensures scroll offset is within valid range
func ClampScroll(scroll, visible, total int) int {
	if total <= visible {
		return 0
	}
	maxScroll := total - visible
	if scroll < 0 {
		return 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}

// ClampCursor ensures cursor is within valid range
func ClampCursor(cursor, total int) int {
	if total <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= total {
		return total - 1
	}
	return cursor
}