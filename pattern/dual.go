package pattern

import (
	"github.com/lixenwraith/vi-fighter/cmd/ascimage/ascimage"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// FromDualModeImage converts dual-mode image to pattern using specified color mode
func FromDualModeImage(img *ascimage.DualModeImage, colorMode terminal.ColorMode) PatternResult {
	if img == nil || len(img.Cells) == 0 {
		return PatternResult{}
	}

	cells := make([]PatternCell, 0, len(img.Cells))

	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			idx := y*img.Width + x
			src := img.Cells[idx]

			if src.Transparent {
				continue
			}

			renderFg := src.Rune != 0 && src.Rune != ' '
			renderBg := true

			var fg, bg terminal.RGB
			var attrs terminal.Attr

			if colorMode == terminal.ColorMode256 {
				fg = terminal.RGB{R: src.Palette256Fg}
				bg = terminal.RGB{R: src.Palette256Bg}
				attrs = terminal.AttrFg256 | terminal.AttrBg256
			} else {
				fg = src.TrueFg
				bg = src.TrueBg
			}

			cells = append(cells, PatternCell{
				OffsetX:  x,
				OffsetY:  y,
				Rune:     src.Rune,
				Fg:       fg,
				Bg:       bg,
				Attrs:    attrs,
				RenderFg: renderFg,
				RenderBg: renderBg,
			})
		}
	}

	return PatternResult{
		Cells:   cells,
		Width:   img.Width,
		Height:  img.Height,
		AnchorX: img.AnchorX,
		AnchorY: img.AnchorY,
	}
}

// LoadDualModePattern loads a .vfimg file and converts to pattern
func LoadDualModePattern(path string, colorMode terminal.ColorMode) (PatternResult, error) {
	img, err := ascimage.LoadDualMode(path)
	if err != nil {
		return PatternResult{}, err
	}
	return FromDualModeImage(img, colorMode), nil
}