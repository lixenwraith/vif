package ascimage

import (
	"image"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// DualModeImage holds both TrueColor and 256-color representations
type DualModeImage struct {
	Width      int
	Height     int
	RenderMode RenderMode
	AnchorX    int
	AnchorY    int
	Cells      []DualCell
}

// DualCell stores both color mode representations for one cell
type DualCell struct {
	Rune         rune
	TrueFg       lcolor.RGB
	TrueBg       lcolor.RGB
	Palette256Fg uint8
	Palette256Bg uint8
	Transparent  bool
}

// ConvertImageDual converts an image to the cells CalculateOutputSize gives at
// a width and aspect, in both color modes in one pass
func ConvertImageDual(img image.Image, targetWidth int, mode RenderMode, aspect float64) *DualModeImage {
	b := img.Bounds()
	w, h := CalculateOutputSize(b.Dx(), b.Dy(), targetWidth, aspect)
	d := &DualModeImage{Width: w, Height: h, RenderMode: mode, Cells: make([]DualCell, w*h)}
	s := newSampler(img, w, h, mode)
	for y := range h {
		for x := range w {
			c := s.cell(x, y)
			c.index(mode)
			d.Cells[y*w+x] = c
		}
	}
	return d
}

// index fills the 256 palette entries of an opaque cell's true colors; a
// background-mode cell is a space, so it leaves the foreground's at 0
func (c *DualCell) index(mode RenderMode) {
	if c.Transparent {
		return
	}
	c.Palette256Bg = lcolor.RGBTo256(c.TrueBg)
	if mode == ModeQuadrant {
		c.Palette256Fg = lcolor.RGBTo256(c.TrueFg)
	}
}

// cell is c drawn in a color mode: true color as it is, 256 from its palette
// entries, 16 and none as the ANSI 16 nearest its true colors. A transparent
// cell is the zero Cell; a background-mode cell's foreground stays unset.
func (c DualCell) cell(colorMode terminal.ColorMode, mode RenderMode) terminal.Cell {
	if c.Transparent {
		return terminal.Cell{}
	}
	out := terminal.Cell{Rune: c.Rune}
	fg := mode == ModeQuadrant
	switch colorMode {
	case terminal.ColorModeTrueColor:
		out.Bg = c.TrueBg
		if fg {
			out.Fg = c.TrueFg
		}
	case terminal.ColorMode256:
		out.Bg, out.Attrs = lcolor.RGB{R: c.Palette256Bg}, terminal.AttrBg256
		if fg {
			out.Fg, out.Attrs = lcolor.RGB{R: c.Palette256Fg}, out.Attrs|terminal.AttrFg256
		}
	default:
		out.Bg, out.Attrs = lcolor.RGB{R: lcolor.RGBTo16(c.TrueBg)}, terminal.AttrBg256
		if fg {
			out.Fg, out.Attrs = lcolor.RGB{R: lcolor.RGBTo16(c.TrueFg)}, out.Attrs|terminal.AttrFg256
		}
	}
	return out
}

// ToConvertedImage extracts single-mode ConvertedImage from dual representation
func (d *DualModeImage) ToConvertedImage(colorMode terminal.ColorMode) *ConvertedImage {
	cells := make([]terminal.Cell, len(d.Cells))
	for i, dc := range d.Cells {
		cells[i] = dc.cell(colorMode, d.RenderMode)
	}
	return &ConvertedImage{Cells: cells, Width: d.Width, Height: d.Height}
}
