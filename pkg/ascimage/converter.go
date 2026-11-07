// Package ascimage converts raster images into terminal cells, a Unicode quadrant
// glyph fitted to each cell's four samples or one background color per sample,
// and reads and writes them as one file holding true color and xterm-256 cells
// at once.
package ascimage

import (
	"image"
	"image/color"
	"slices"

	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// DefaultAspect is a terminal cell's width over its height
const DefaultAspect = 0.5

// QuadrantChars maps 4-bit patterns to Unicode quadrant characters
// Bit order: 0=UL, 1=UR, 2=LL, 3=LR (1 = foreground)
var QuadrantChars = [16]rune{
	' ', // 0000 - empty
	'▘', // 0001 - upper-left
	'▝', // 0010 - upper-right
	'▀', // 0011 - upper half
	'▖', // 0100 - lower-left
	'▌', // 0101 - left half
	'▞', // 0110 - anti-diagonal
	'▛', // 0111 - UL + UR + LL
	'▗', // 1000 - lower-right
	'▚', // 1001 - diagonal
	'▐', // 1010 - right half
	'▜', // 1011 - UL + UR + LR
	'▄', // 1100 - lower half
	'▙', // 1101 - UL + LL + LR
	'▟', // 1110 - UR + LL + LR
	'█', // 1111 - full block
}

// RenderMode determines the rendering approach
type RenderMode uint8

const (
	ModeBackgroundOnly RenderMode = iota
	ModeQuadrant
)

// String returns human-readable mode name
func (m RenderMode) String() string {
	switch m {
	case ModeBackgroundOnly:
		return "Background"
	case ModeQuadrant:
		return "Quadrant"
	default:
		return "Unknown"
	}
}

// ConvertedImage holds an image's cells in one color mode; Rune 0 marks a
// transparent cell, which shows what lies under the image
type ConvertedImage struct {
	Cells  []terminal.Cell
	Width  int
	Height int
}

// ConvertImage converts an image to the cells CalculateOutputSize gives at a
// width and aspect, in one color mode
func ConvertImage(img image.Image, targetWidth int, mode RenderMode, colorMode terminal.ColorMode, aspect float64) *ConvertedImage {
	b := img.Bounds()
	w, h := CalculateOutputSize(b.Dx(), b.Dy(), targetWidth, aspect)
	return &ConvertedImage{
		Cells:  ConvertRect(nil, img, w, h, image.Rect(0, 0, w, h), mode, colorMode),
		Width:  w,
		Height: h,
	}
}

// ConvertRect converts the cells r covers of img's grid of gridW by gridH
// cells into dst, row by row, growing it as it must: the cells ConvertImage
// gives at that size, so a viewer converts only what it shows. A cell of r
// outside the grid is transparent.
func ConvertRect(dst []terminal.Cell, img image.Image, gridW, gridH int, r image.Rectangle, mode RenderMode, colorMode terminal.ColorMode) []terminal.Cell {
	n := max(r.Dx(), 0) * max(r.Dy(), 0)
	dst = slices.Grow(dst[:0], n)[:n]
	s := newSampler(img, gridW, gridH, mode)
	i := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := s.cell(x, y)
			if colorMode == terminal.ColorMode256 {
				c.index(mode)
			}
			dst[i] = c.cell(colorMode, mode)
			i++
		}
	}
	return dst
}

// CalculateOutputSize is the cell grid an image fills at a width, at least one row
func CalculateOutputSize(srcW, srcH, targetWidth int, aspect float64) (outW, outH int) {
	if srcW == 0 || srcH == 0 || targetWidth <= 0 {
		return 0, 0
	}
	imageRatio := float64(srcH) / float64(srcW)
	return targetWidth, max(int(float64(targetWidth)*imageRatio*aspect), 1)
}

// sampler reads the samples of a grid of cells over an image: one a cell in
// background mode, a cell's four quadrants in quadrant mode, each the source
// pixel under its centre
type sampler struct {
	img            image.Image
	b              image.Rectangle
	cellsW, cellsH int
	gridW, gridH   int // samples across and down
	mode           RenderMode
}

func newSampler(img image.Image, cellsW, cellsH int, mode RenderMode) sampler {
	s := sampler{img: img, b: img.Bounds(), cellsW: cellsW, cellsH: cellsH, gridW: cellsW, gridH: cellsH, mode: mode}
	if mode == ModeQuadrant {
		s.gridW, s.gridH = 2*cellsW, 2*cellsH
	}
	if s.b.Empty() {
		s.cellsW, s.cellsH = 0, 0
	}
	return s
}

func (s *sampler) at(gx, gy int) (lcolor.RGB, bool) {
	srcW, srcH := int64(s.b.Dx()), int64(s.b.Dy())
	sx := s.b.Min.X + int((int64(gx)*srcW+srcW/2)/int64(s.gridW))
	sy := s.b.Min.Y + int((int64(gy)*srcH+srcH/2)/int64(s.gridH))
	return sampleRGB(s.img.At(min(sx, s.b.Max.X-1), min(sy, s.b.Max.Y-1)))
}

// cell is cell (x, y) in true color; one outside the grid, or one whose every
// sample is transparent, is transparent
func (s *sampler) cell(x, y int) DualCell {
	if x < 0 || y < 0 || x >= s.cellsW || y >= s.cellsH {
		return DualCell{Transparent: true}
	}
	if s.mode != ModeQuadrant {
		rgb, ok := s.at(x, y)
		if !ok {
			return DualCell{Transparent: true}
		}
		return DualCell{Rune: ' ', TrueBg: rgb}
	}
	var px [4]lcolor.RGB
	opaque := false
	for i := range px {
		var ok bool
		px[i], ok = s.at(2*x+i%2, 2*y+i/2)
		opaque = opaque || ok
	}
	if !opaque {
		return DualCell{Transparent: true}
	}
	r, fg, bg := FitQuadrant(px)
	return DualCell{Rune: r, TrueFg: fg, TrueBg: bg}
}

// FitQuadrant returns the quadrant glyph and its two colors that show a cell's four
// quadrants, UL UR LL LR, with the least squared error
func FitQuadrant(pixels [4]lcolor.RGB) (rune, lcolor.RGB, lcolor.RGB) {
	bestError := int(^uint(0) >> 1)
	bestPattern := 0
	var bestFg, bestBg lcolor.RGB

	for pattern := range 16 {
		fg, bg, err := computePatternColors(pixels, pattern)
		if err < bestError {
			bestError = err
			bestPattern = pattern
			bestFg = fg
			bestBg = bg
		}
	}

	return QuadrantChars[bestPattern], bestFg, bestBg
}

func computePatternColors(pixels [4]lcolor.RGB, pattern int) (fg, bg lcolor.RGB, totalError int) {
	var fgR, fgG, fgB, fgCount int
	var bgR, bgG, bgB, bgCount int

	for i := range 4 {
		if pattern&(1<<i) != 0 {
			fgR += int(pixels[i].R)
			fgG += int(pixels[i].G)
			fgB += int(pixels[i].B)
			fgCount++
		} else {
			bgR += int(pixels[i].R)
			bgG += int(pixels[i].G)
			bgB += int(pixels[i].B)
			bgCount++
		}
	}

	if fgCount > 0 {
		fg = lcolor.RGB{
			R: uint8(fgR / fgCount),
			G: uint8(fgG / fgCount),
			B: uint8(fgB / fgCount),
		}
	}
	if bgCount > 0 {
		bg = lcolor.RGB{
			R: uint8(bgR / bgCount),
			G: uint8(bgG / bgCount),
			B: uint8(bgB / bgCount),
		}
	}

	for i := range 4 {
		var target lcolor.RGB
		if pattern&(1<<i) != 0 {
			target = fg
		} else {
			target = bg
		}
		totalError += colorDistanceSq(pixels[i], target)
	}

	return fg, bg, totalError
}

func colorDistanceSq(a, b lcolor.RGB) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

// sampleRGB is a pixel's color, its alpha divided out, and whether it shows at all
func sampleRGB(c color.Color) (lcolor.RGB, bool) {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return lcolor.RGB{}, false
	}
	return lcolor.RGB{R: uint8((r * 0xff) / a), G: uint8((g * 0xff) / a), B: uint8((b * 0xff) / a)}, true
}
