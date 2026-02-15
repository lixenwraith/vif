package pattern

import (
	"github.com/lixenwraith/vi-fighter/cmd/ascimage/ascimage"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// ImageConfig controls image-to-pattern conversion
type ImageConfig struct {
	// Reserved for future: transparency handling
}

// FromImage converts ascimage output to wall pattern
// Preserves color mode attributes from conversion (TrueColor or 256-palette)
func FromImage(img *ascimage.ConvertedImage, cfg ImageConfig) PatternResult {
	if img == nil || len(img.Cells) == 0 {
		return PatternResult{}
	}

	cells := make([]PatternCell, 0, len(img.Cells))

	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			idx := y*img.Width + x
			src := img.Cells[idx]

			// Determine render flags based on content
			renderFg := src.Rune != 0 && src.Rune != ' '
			renderBg := true // Images always have background

			cells = append(cells, PatternCell{
				OffsetX:  x,
				OffsetY:  y,
				Rune:     src.Rune,
				Fg:       src.Fg,
				Bg:       src.Bg,
				Attrs:    src.Attrs,
				RenderFg: renderFg,
				RenderBg: renderBg,
			})
		}
	}

	return PatternResult{
		Cells:  cells,
		Width:  img.Width,
		Height: img.Height,
	}
}

// FromImageWithFilter converts ascimage output, skipping cells matching predicate
func FromImageWithFilter(img *ascimage.ConvertedImage, skip func(cell terminal.Cell, x, y int) bool) PatternResult {
	if img == nil || len(img.Cells) == 0 {
		return PatternResult{}
	}

	cells := make([]PatternCell, 0, len(img.Cells))

	for y := 0; y < img.Height; y++ {
		for x := 0; x < img.Width; x++ {
			idx := y*img.Width + x
			src := img.Cells[idx]

			if skip != nil && skip(src, x, y) {
				continue
			}

			renderFg := src.Rune != 0 && src.Rune != ' '
			renderBg := true

			cells = append(cells, PatternCell{
				OffsetX:  x,
				OffsetY:  y,
				Rune:     src.Rune,
				Fg:       src.Fg,
				Bg:       src.Bg,
				Attrs:    src.Attrs,
				RenderFg: renderFg,
				RenderBg: renderBg,
			})
		}
	}

	return PatternResult{
		Cells:  cells,
		Width:  img.Width,
		Height: img.Height,
	}
}