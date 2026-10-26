package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/ascimage"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// logoInk is what the mark shows at a point: a wedge, numbered as visual.RgbLogoWedges,
// a stroke, or nothing beyond its square
type logoInk int8

const (
	logoNone   logoInk = -1
	logoStroke logoInk = logoInk(len(visual.RgbLogoWedges))
)

// logoAt is the mark's ink at u, v, each 0 to 1 across its square: the ring takes the
// square's edge, a straight cross and two diagonals meet at its centre
func logoAt(u, v float64) logoInk {
	if u < 0 || u > 1 || v < 0 || v > 1 {
		return logoNone
	}
	s := visual.LogoStroke / 2
	du, dv := u-0.5, v-0.5
	if u < 2*s || u > 1-2*s || v < 2*s || v > 1-2*s || math.Abs(du) < s || math.Abs(dv) < s ||
		math.Abs(du-dv) < s*math.Sqrt2 || math.Abs(du+dv) < s*math.Sqrt2 {
		return logoStroke
	}
	wide := math.Abs(du) > math.Abs(dv) // the wedge on the side edge rather than the top or bottom
	switch {
	case du < 0 && dv > 0:
		return pick(wide, 1, 0)
	case du < 0:
		return pick(wide, 2, 3)
	case dv < 0:
		return pick(wide, 5, 4)
	}
	return pick(wide, 6, 7)
}

func pick(c bool, a, b logoInk) logoInk {
	if c {
		return a
	}
	return b
}

// logoCells is the mark laid out in a box of cells, kept until the box, the cell aspect,
// the background or the console changes. Truecolor gives each cell the quadrant glyph
// and two colors that best fit its quadrants, each the mean of its samples, so an edge
// blends into the background; a console gives a whole cell the entry most of it shows,
// since its fonts have no quadrants. A cell left at Rune 0 shows what is under it.
type logoCells struct {
	w, h    int
	aspect  vmath.Aspect
	bg      color.RGB
	console *render.Console
	cells   []terminal.Cell
}

// layout returns the mark w by h cells, its square centred in the box; a box's height
// counts in cell widths over the aspect, so the square stays square on screen
func (l *logoCells) layout(w, h int, aspect vmath.Aspect, bg color.RGB, console *render.Console) []terminal.Cell {
	if w < 1 || h < 1 || aspect <= 0 {
		return nil
	}
	if l.cells != nil && l.w == w && l.h == h && l.aspect == aspect && l.bg == bg && l.console == console {
		return l.cells
	}
	l.w, l.h, l.aspect, l.bg, l.console = w, h, aspect, bg, console
	fw, fh, a := float64(w), float64(h), float64(aspect)
	side := min(fw, fh/a)
	ox, oy := (fw-side)/2, (fh/a-side)/2
	n := visual.LogoSamples
	l.cells = make([]terminal.Cell, w*h)
	for cy := range h {
		for cx := range w {
			var quad [4]color.RGB // UL, UR, LL, LR: the mean color, the background outside the mark
			var votes [len(visual.RgbLogoWedges) + 1]int
			for q := range quad {
				var sum [3]float64
				for j := range n {
					for i := range n {
						x := float64(cx) + (float64(q%2)+(float64(i)+0.5)/float64(n))/2
						y := float64(cy) + (float64(q/2)+(float64(j)+0.5)/float64(n))/2
						c := bg
						switch ink := logoAt((x-ox)/side, (y/a-oy)/side); ink {
						case logoNone:
						case logoStroke:
							c, votes[ink] = visual.RgbLogoStroke, votes[ink]+1
						default:
							c, votes[ink] = visual.RgbLogoWedges[ink], votes[ink]+1
						}
						sum[0], sum[1], sum[2] = sum[0]+float64(c.R), sum[1]+float64(c.G), sum[2]+float64(c.B)
					}
				}
				k := float64(n * n)
				quad[q] = color.RGB{R: uint8(math.Round(sum[0] / k)), G: uint8(math.Round(sum[1] / k)), B: uint8(math.Round(sum[2] / k))}
			}
			cell := &l.cells[cy*w+cx]
			if console != nil {
				*cell = logoConsoleCell(votes, 4*n*n, console)
				continue
			}
			if inked := votes != [len(votes)]int{}; inked {
				cell.Rune, cell.Fg, cell.Bg = ascimage.FitQuadrant(quad)
			}
		}
	}
	return l.cells
}

// logoConsoleCell is a whole cell in the entry most of its samples show; a cell mostly
// outside the mark shows what is under it
func logoConsoleCell(votes [len(visual.RgbLogoWedges) + 1]int, samples int, console *render.Console) terminal.Cell {
	best, most := 0, 0
	for ink, v := range votes {
		if v > most {
			best, most = ink, v
		}
	}
	if 2*most < samples {
		return terminal.Cell{}
	}
	entry := visual.LogoStroke256
	if logoInk(best) != logoStroke {
		entry = visual.LogoWedges256[best]
	}
	c := console.Color(entry)
	return terminal.Cell{Rune: '█', Fg: c, Bg: c}
}
