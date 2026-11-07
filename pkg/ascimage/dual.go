package ascimage

import (
	"image"
	"image/color"

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

// ConvertImageDual converts image to both color modes in single pass
func ConvertImageDual(img image.Image, targetWidth int, mode RenderMode, aspect float64) *DualModeImage {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW == 0 || srcH == 0 || targetWidth <= 0 {
		return &DualModeImage{Width: 0, Height: 0, RenderMode: mode}
	}

	outW, outH := CalculateOutputSize(srcW, srcH, targetWidth, aspect)

	cells := make([]DualCell, outW*outH)

	switch mode {
	case ModeBackgroundOnly:
		convertBackgroundDual(img, cells, outW, outH)
	case ModeQuadrant:
		convertQuadrantDual(img, cells, outW, outH)
	}

	return &DualModeImage{
		Width:      outW,
		Height:     outH,
		RenderMode: mode,
		Cells:      cells,
	}
}

func convertBackgroundDual(img image.Image, cells []DualCell, outW, outH int) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for y := range outH {
		for x := range outW {
			sx := bounds.Min.X + (x*srcW+srcW/2)/outW
			sy := bounds.Min.Y + (y*srcH+srcH/2)/outH

			if sx >= bounds.Max.X {
				sx = bounds.Max.X - 1
			}
			if sy >= bounds.Max.Y {
				sy = bounds.Max.Y - 1
			}

			idx := y*outW + x
			c := img.At(sx, sy)

			if colorIsTransparent(c) {
				cells[idx].Transparent = true
				continue
			}

			rgb := colorToRGB(c)
			cells[idx].Rune = ' '
			cells[idx].TrueBg = rgb
			cells[idx].Palette256Bg = lcolor.RGBTo256(rgb)
		}
	}
}

func convertQuadrantDual(img image.Image, cells []DualCell, outW, outH int) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	gridW := outW * 2
	gridH := outH * 2

	for y := range outH {
		for x := range outW {
			var pixels [4]lcolor.RGB
			allTransparent := true

			gx := x * 2
			gy := y * 2

			offsets := [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}}

			for i, off := range offsets {
				sx := bounds.Min.X + ((gx+off[0])*srcW+srcW/2)/gridW
				sy := bounds.Min.Y + ((gy+off[1])*srcH+srcH/2)/gridH

				if sx >= bounds.Max.X {
					sx = bounds.Max.X - 1
				}
				if sy >= bounds.Max.Y {
					sy = bounds.Max.Y - 1
				}

				c := img.At(sx, sy)
				if !colorIsTransparent(c) {
					allTransparent = false
				}
				pixels[i] = colorToRGB(c)
			}

			idx := y*outW + x

			if allTransparent {
				cells[idx].Transparent = true
				continue
			}

			char, fg, bg := FitQuadrant(pixels)

			cells[idx].Rune = char
			cells[idx].TrueFg = fg
			cells[idx].TrueBg = bg
			cells[idx].Palette256Fg = lcolor.RGBTo256(fg)
			cells[idx].Palette256Bg = lcolor.RGBTo256(bg)
		}
	}
}

func colorIsTransparent(c color.Color) bool {
	_, _, _, a := c.RGBA()
	return a == 0
}

// ToConvertedImage extracts single-mode ConvertedImage from dual representation
func (d *DualModeImage) ToConvertedImage(colorMode terminal.ColorMode) *ConvertedImage {
	cells := make([]terminal.Cell, len(d.Cells))

	for i, dc := range d.Cells {
		if dc.Transparent {
			continue
		}
		if colorMode == terminal.ColorMode256 {
			cells[i] = terminal.Cell{
				Rune:  dc.Rune,
				Fg:    lcolor.RGB{R: dc.Palette256Fg},
				Bg:    lcolor.RGB{R: dc.Palette256Bg},
				Attrs: terminal.AttrFg256 | terminal.AttrBg256,
			}
		} else {
			cells[i] = terminal.Cell{
				Rune: dc.Rune,
				Fg:   dc.TrueFg,
				Bg:   dc.TrueBg,
			}
		}
	}

	return &ConvertedImage{
		Cells:  cells,
		Width:  d.Width,
		Height: d.Height,
	}
}
