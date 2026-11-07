package main

import (
	"image"
	"math"
	"slices"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
)

// zooms are the steps + and - move between, in percent of 1:1
var zooms = []int{5, 10, 15, 20, 25, 33, 50, 67, 75, 100, 125, 150, 200, 300, 400, 600, 800}

// look is how what is shown converts: the render mode a raster takes, the
// color depth every cell takes, and the display's cell aspect
type look struct {
	mode   ascimage.RenderMode
	depth  terminal.ColorMode
	aspect float64
}

// base is a source's grid at 1:1: a raster's pixel a column of quadrant
// samples, or a cell in background mode; an .aimg's own cells
func (s *source) base(lk look) (int, int) {
	if s.dual != nil {
		return s.w, s.h
	}
	w := s.w
	if lk.mode == ascimage.ModeQuadrant {
		w = (s.w + 1) / 2
	}
	return s.gridAt(w, lk)
}

// gridAt is a source's grid w cells wide
func (s *source) gridAt(w int, lk look) (int, int) {
	w = max(1, w)
	if s.dual != nil {
		return w, max(1, int(math.Round(float64(s.h)*float64(w)/float64(s.w))))
	}
	return ascimage.CalculateOutputSize(s.w, s.h, w, lk.aspect)
}

// fitGrid is the largest grid inside a viewport; an .aimg's never passes 1:1,
// where a larger one would only repeat its cells
func (s *source) fitGrid(vw, vh int, lk look) (int, int) {
	if s.dual != nil {
		scale := min(1, float64(vw)/float64(s.w), float64(vh)/float64(s.h))
		return s.gridAt(int(float64(s.w)*scale), lk)
	}
	return s.gridAt(fitWidth(s.w, s.h, vw, vh, lk.aspect), lk)
}

// window is the cells of r in a grid gw by gh over the source: a raster
// converted, an .aimg's cells each the one under the grid cell's centre
func (s *source) window(dst []terminal.Cell, gw, gh int, r image.Rectangle, lk look) []terminal.Cell {
	if s.dual == nil {
		return ascimage.ConvertRect(dst, s.img, gw, gh, r, lk.mode, lk.depth)
	}
	src := s.cellsIn(lk.depth)
	n := r.Dx() * r.Dy()
	dst = slices.Grow(dst[:0], n)[:n]
	i := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := (2*y + 1) * src.Height / (2 * gh) * src.Width
		for x := r.Min.X; x < r.Max.X; x++ {
			dst[i] = src.Cells[row+(2*x+1)*src.Width/(2*gw)]
			i++
		}
	}
	return dst
}

// cellsIn is an .aimg's cells in a color depth, kept until the depth changes
func (s *source) cellsIn(depth terminal.ColorMode) *ascimage.ConvertedImage {
	if s.conv == nil || s.convDepth != depth {
		s.conv, s.convDepth = s.dual.ToConvertedImage(depth), depth
	}
	return s.conv
}

// view is how an image sits in a viewport: fit to it, or at a zoom of 1:1,
// centred on a point held as a fraction of the grid, which a zoom or a
// resize keeps where it is
type view struct {
	fit    bool
	zoom   int
	cx, cy float64
}

func newView(fit bool, zoom int) view { return view{fit: fit, zoom: zoom, cx: 0.5, cy: 0.5} }

// frame is a view laid out in a viewport: the grid's size, and the grid cell
// at the viewport's top left, negative where a grid smaller than the
// viewport sits centred in it
type frame struct {
	gw, gh, ox, oy, vw, vh int
}

func (v *view) frame(s *source, vw, vh int, lk look) frame {
	f := frame{vw: vw, vh: vh}
	if v.fit {
		f.gw, f.gh = s.fitGrid(vw, vh, lk)
	} else {
		bw, _ := s.base(lk)
		f.gw, f.gh = s.gridAt(bw*v.zoom/100, lk)
	}
	f.ox, f.oy = place(f.gw, vw, v.cx), place(f.gh, vh, v.cy)
	return f
}

// place is the grid's first cell in a viewport: centred in a larger viewport,
// else about the view's centre, as far as the grid's edges
func place(grid, port int, c float64) int {
	if grid <= port {
		return -(port - grid) / 2
	}
	return max(0, min(int(math.Round(c*float64(grid)-float64(port)/2)), grid-port))
}

// percent is the zoom a frame shows, of 1:1
func (f frame) percent(s *source, lk look) int {
	bw, _ := s.base(lk)
	return max(1, int(math.Round(float64(f.gw)*100/float64(bw))))
}

// rect is the part of the grid the viewport shows
func (f frame) rect() image.Rectangle {
	return image.Rectangle{
		Min: image.Pt(max(f.ox, 0), max(f.oy, 0)),
		Max: image.Pt(min(f.ox+f.vw, f.gw), min(f.oy+f.vh, f.gh)),
	}
}

// overflows reports whether the grid is larger than the viewport either way
func (f frame) overflows() bool { return f.gw > f.vw || f.gh > f.vh }

// pan moves the view by cells, as far as the grid's edges
func (v *view) pan(f frame, dx, dy int) {
	if f.gw > f.vw {
		o := max(0, min(f.ox+dx, f.gw-f.vw))
		v.cx = (float64(o) + float64(f.vw)/2) / float64(f.gw)
	}
	if f.gh > f.vh {
		o := max(0, min(f.oy+dy, f.gh-f.vh))
		v.cy = (float64(o) + float64(f.vh)/2) / float64(f.gh)
	}
}

// zoomTo sets a zoom, keeping the image's point under viewport cell (mx, my)
// under it
func (v *view) zoomTo(s *source, f frame, pct, mx, my int, lk look) {
	fx := (float64(f.ox+mx) + 0.5) / float64(f.gw)
	fy := (float64(f.oy+my) + 0.5) / float64(f.gh)
	v.fit, v.zoom = false, pct
	n := v.frame(s, f.vw, f.vh, lk)
	v.cx = (fx*float64(n.gw) - float64(mx) - 0.5 + float64(f.vw)/2) / float64(n.gw)
	v.cy = (fy*float64(n.gh) - float64(my) - 0.5 + float64(f.vh)/2) / float64(n.gh)
}

// step zooms to the next of zooms past the one shown, in (dir 1) or out (-1),
// about viewport cell (mx, my)
func (v *view) step(s *source, f frame, dir, mx, my int, lk look) {
	cur, next := f.percent(s, lk), 0
	for i := range zooms {
		z := zooms[i]
		if dir < 0 {
			z = zooms[len(zooms)-1-i]
		}
		if dir > 0 && z > cur || dir < 0 && z < cur {
			next = z
			break
		}
	}
	if next != 0 {
		v.zoomTo(s, f, next, mx, my, lk)
	}
}

// window holds the converted cells of the part of a grid last shown, so a
// frame that shows the same part converts nothing
type window struct {
	key   windowKey
	cells []terminal.Cell
}

type windowKey struct {
	src    *source
	gw, gh int
	r      image.Rectangle
	lk     look
}

func (w *window) cellsFor(s *source, f frame, lk look) ([]terminal.Cell, image.Rectangle) {
	r := f.rect()
	if k := (windowKey{s, f.gw, f.gh, r, lk}); k != w.key {
		w.cells, w.key = s.window(w.cells, f.gw, f.gh, r, lk), k
	}
	return w.cells, r
}
