package render

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

// TestRGBWritesComposeOverPaletteColour: an RGB write or blend onto a cell drawn with
// a palette index composes over that index's colour and clears the palette flag, so the
// terminal never reads a blended R byte as an index.
func TestRGBWritesComposeOverPaletteColour(t *testing.T) {
	t.Parallel()
	rim := visual.Palette256RGB(color.P256Yellow)
	flash := color.RGB{R: 200, G: 60, B: 25}
	tests := []struct {
		name  string
		fg    bool
		write func(b *RenderBuffer)
		want  color.RGB
	}{
		{"screen overlay", false, func(b *RenderBuffer) { b.SetBgScreen(0, 0, flash, visual.RgbBackground, 0.5) }, color.Screen(rim, flash, 0.5)},
		{"blend", false, func(b *RenderBuffer) { b.Set(0, 0, 0, visual.RgbBlack, flash, BlendScreen, 0.5, terminal.AttrNone) }, color.Screen(rim, flash, 0.5)},
		{"overwrite", false, func(b *RenderBuffer) { b.SetBgOnly(0, 0, flash) }, flash},
		{"dim", false, func(b *RenderBuffer) { b.MutateDim(0.5, visual.MaskField) }, color.Scale(rim, 0.5)},
		{"fg blend", true, func(b *RenderBuffer) { b.Set(0, 0, 'x', flash, visual.RgbBlack, BlendAddFg, 1, terminal.AttrNone) }, color.Add(rim, flash, 1)},
	}
	for _, tt := range tests {
		b := NewRenderBuffer(terminal.ColorMode256, 1, 1)
		b.SetWriteMask(visual.MaskField)
		b.SetBg256(0, 0, color.P256Yellow)
		b.SetFgOnly(0, 0, 'x', color.RGB{R: color.P256Yellow}, terminal.AttrFg256)
		tt.write(b)

		cell := b.CellAt(0, 0)
		got, flag := cell.Bg, terminal.AttrBg256
		if tt.fg {
			got, flag = cell.Fg, terminal.AttrFg256
		}
		if cell.Attrs&flag != 0 || got != tt.want {
			t.Errorf("%s: got %v with palette flag %v, want %v", tt.name, got, cell.Attrs&flag != 0, tt.want)
		}
	}
}
