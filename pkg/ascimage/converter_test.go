package ascimage

import (
	"image"
	"image/color"
	"testing"

	"github.com/lixenwraith/terminal"
)

func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(255)
			if x < w/5 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x ^ y) * 7), A: a})
		}
	}
	return img
}

// TestConvertRectGivesTheWholeConversionsCells: a viewer converting only its
// window sees the cells the whole image converts to, and none past the grid
func TestConvertRectGivesTheWholeConversionsCells(t *testing.T) {
	img := gradient(97, 61)
	for _, mode := range []RenderMode{ModeBackgroundOnly, ModeQuadrant} {
		for _, cm := range []terminal.ColorMode{terminal.ColorModeTrueColor, terminal.ColorMode256, terminal.ColorMode16} {
			whole := ConvertImage(img, 40, mode, cm, DefaultAspect)
			r := image.Rect(31, whole.Height-5, 47, whole.Height+2) // over the grid's right and bottom edges
			got := ConvertRect(nil, img, whole.Width, whole.Height, r, mode, cm)
			if len(got) != r.Dx()*r.Dy() {
				t.Fatalf("mode %v: %d cells for a %v window", mode, len(got), r)
			}
			for i, c := range got {
				x, y := r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx()
				want := terminal.Cell{}
				if x < whole.Width && y < whole.Height {
					want = whole.Cells[y*whole.Width+x]
				}
				if c != want {
					t.Fatalf("mode %v color %v: cell %d,%d = %+v, want %+v", mode, cm, x, y, c, want)
				}
			}
		}
	}
}
