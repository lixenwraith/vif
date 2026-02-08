package ascimage

import (
	"image"
	"image/color"

	"github.com/lixenwraith/vi-fighter/terminal"
)

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

// ConvertedImage holds the conversion result
type ConvertedImage struct {
	Cells  []terminal.Cell
	Width  int
	Height int
}

// ConvertImage converts an image to terminal cells
// targetWidth: desired output width in terminal columns
// mode: background-only or quadrant rendering
// colorMode: truecolor or 256-color palette
func ConvertImage(img image.Image, targetWidth int, mode RenderMode, colorMode terminal.ColorMode) *ConvertedImage {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW == 0 || srcH == 0 || targetWidth <= 0 {
		return &ConvertedImage{Width: 0, Height: 0}
	}

	// Calculate output dimensions preserving aspect ratio
	// Terminal chars are roughly 2:1 (height:width), so multiply height factor by 0.5
	aspectRatio := float64(srcH) / float64(srcW)
	charAspect := 0.5 // compensate for terminal character proportions

	outW := targetWidth
	outH := int(float64(targetWidth) * aspectRatio * charAspect)
	if outH < 1 {
		outH = 1
	}

	cells := make([]terminal.Cell, outW*outH)

	switch mode {
	case ModeBackgroundOnly:
		convertBackground(img, cells, outW, outH, colorMode)
	case ModeQuadrant:
		convertQuadrant(img, cells, outW, outH, colorMode)
	}

	return &ConvertedImage{
		Cells:  cells,
		Width:  outW,
		Height: outH,
	}
}

// CalculateOutputSize returns the output dimensions for given parameters without converting
func CalculateOutputSize(srcW, srcH, targetWidth int) (outW, outH int) {
	if srcW == 0 || srcH == 0 || targetWidth <= 0 {
		return 0, 0
	}
	aspectRatio := float64(srcH) / float64(srcW)
	outW = targetWidth
	outH = int(float64(targetWidth) * aspectRatio * 0.5)
	if outH < 1 {
		outH = 1
	}
	return outW, outH
}

// convertBackground renders using background colors only (1 cell = 1 sampled region)
func convertBackground(img image.Image, cells []terminal.Cell, outW, outH int, colorMode terminal.ColorMode) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			// Sample center of the corresponding region
			sx := bounds.Min.X + (x*srcW+srcW/2)/outW
			sy := bounds.Min.Y + (y*srcH+srcH/2)/outH

			// Clamp to bounds
			if sx >= bounds.Max.X {
				sx = bounds.Max.X - 1
			}
			if sy >= bounds.Max.Y {
				sy = bounds.Max.Y - 1
			}

			rgb := colorToRGB(img.At(sx, sy))
			idx := y*outW + x
			cells[idx].Rune = ' '

			if colorMode == terminal.ColorMode256 {
				palIdx := terminal.RGBTo256(rgb)
				cells[idx].Bg = terminal.RGB{R: palIdx}
				cells[idx].Attrs = terminal.AttrBg256
			} else {
				cells[idx].Bg = rgb
			}
		}
	}
}

// convertQuadrant renders using quadrant characters with fg/bg colors (2x effective resolution)
func convertQuadrant(img image.Image, cells []terminal.Cell, outW, outH int, colorMode terminal.ColorMode) {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Effective pixel grid is 2x output dimensions
	gridW := outW * 2
	gridH := outH * 2

	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			// Sample 4 pixels for this cell (UL, UR, LL, LR)
			var pixels [4]terminal.RGB

			// Grid positions for the 2x2 block
			gx := x * 2
			gy := y * 2

			// Sample positions: [0]=UL, [1]=UR, [2]=LL, [3]=LR
			offsets := [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}}

			for i, off := range offsets {
				// Map grid position to source image
				sx := bounds.Min.X + ((gx+off[0])*srcW+srcW/2)/gridW
				sy := bounds.Min.Y + ((gy+off[1])*srcH+srcH/2)/gridH

				// Clamp
				if sx >= bounds.Max.X {
					sx = bounds.Max.X - 1
				}
				if sy >= bounds.Max.Y {
					sy = bounds.Max.Y - 1
				}

				pixels[i] = colorToRGB(img.At(sx, sy))
			}

			// Find optimal quadrant character and colors
			char, fg, bg := findBestQuadrant(pixels)

			idx := y*outW + x
			cells[idx].Rune = char

			if colorMode == terminal.ColorMode256 {
				fgIdx := terminal.RGBTo256(fg)
				bgIdx := terminal.RGBTo256(bg)
				cells[idx].Fg = terminal.RGB{R: fgIdx}
				cells[idx].Bg = terminal.RGB{R: bgIdx}
				cells[idx].Attrs = terminal.AttrFg256 | terminal.AttrBg256
			} else {
				cells[idx].Fg = fg
				cells[idx].Bg = bg
			}
		}
	}
}

// findBestQuadrant finds the optimal quadrant character and fg/bg colors for 4 pixels
func findBestQuadrant(pixels [4]terminal.RGB) (rune, terminal.RGB, terminal.RGB) {
	bestError := int(^uint(0) >> 1)
	bestPattern := 0
	var bestFg, bestBg terminal.RGB

	for pattern := 0; pattern < 16; pattern++ {
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

// computePatternColors computes optimal fg/bg colors for a given bit pattern
func computePatternColors(pixels [4]terminal.RGB, pattern int) (fg, bg terminal.RGB, totalError int) {
	var fgR, fgG, fgB, fgCount int
	var bgR, bgG, bgB, bgCount int

	for i := 0; i < 4; i++ {
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
		fg = terminal.RGB{
			R: uint8(fgR / fgCount),
			G: uint8(fgG / fgCount),
			B: uint8(fgB / fgCount),
		}
	}
	if bgCount > 0 {
		bg = terminal.RGB{
			R: uint8(bgR / bgCount),
			G: uint8(bgG / bgCount),
			B: uint8(bgB / bgCount),
		}
	}

	for i := 0; i < 4; i++ {
		var target terminal.RGB
		if pattern&(1<<i) != 0 {
			target = fg
		} else {
			target = bg
		}
		totalError += colorDistanceSq(pixels[i], target)
	}

	return fg, bg, totalError
}

func colorDistanceSq(a, b terminal.RGB) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

func colorToRGB(c color.Color) terminal.RGB {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return terminal.RGB{R: 0, G: 0, B: 0}
	}
	return terminal.RGB{
		R: uint8((r * 0xff) / a),
		G: uint8((g * 0xff) / a),
		B: uint8((b * 0xff) / a),
	}
}