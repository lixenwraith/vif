// Package pattern turns an authored .vifimg into wall cells. It is the one
// path from image asset to ECS entity; nothing here draws to the terminal.
package pattern

import (
	"io"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/pkg/ascimage"
)

// PatternCell holds visual data + offset for one cell
type PatternCell struct {
	OffsetX  int
	OffsetY  int
	Rune     rune
	Fg       color.RGB
	Bg       color.RGB
	Attrs    terminal.Attr // Preserves AttrFg256/AttrBg256 from ascimage
	RenderFg bool
	RenderBg bool
}

// PatternResult is one loaded image, in pattern-local coordinates.
type PatternResult struct {
	Cells   []PatternCell
	Width   int // Bounding width
	Height  int // Bounding height
	AnchorX int // Authored anchor X offset
	AnchorY int // Authored anchor Y offset
}

// ToWallCellDefs converts pattern cells to wall spawn payload format
func (p *PatternResult) ToWallCellDefs() []component.WallCellDef {
	defs := make([]component.WallCellDef, len(p.Cells))
	for i, cell := range p.Cells {
		defs[i] = component.WallCellDef{
			OffsetX: cell.OffsetX,
			OffsetY: cell.OffsetY,
			WallVisualConfig: component.WallVisualConfig{
				Char:     cell.Rune,
				FgColor:  cell.Fg,
				BgColor:  cell.Bg,
				RenderFg: cell.RenderFg,
				RenderBg: cell.RenderBg,
			},
			Attrs: cell.Attrs,
		}
	}
	return defs
}

func (p *PatternResult) Empty() bool {
	return len(p.Cells) == 0
}

// fromDualModeImage converts a dual-mode image using the given color mode.
func fromDualModeImage(img *ascimage.DualModeImage, colorMode terminal.ColorMode) PatternResult {
	if img == nil || len(img.Cells) == 0 {
		return PatternResult{}
	}

	cells := make([]PatternCell, 0, len(img.Cells))

	for y := range img.Height {
		for x := range img.Width {
			idx := y*img.Width + x
			src := img.Cells[idx]

			if src.Transparent {
				continue
			}

			renderFg := src.Rune != 0 && src.Rune != ' '
			renderBg := true

			var fg, bg color.RGB
			var attrs terminal.Attr

			if colorMode == terminal.ColorMode256 {
				fg = color.RGB{R: src.Palette256Fg}
				bg = color.RGB{R: src.Palette256Bg}
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

// ReadDualModePattern decodes a .vifimg supplied by an external file
// capability. The pattern package does not discover or open host paths.
func ReadDualModePattern(r io.Reader, colorMode terminal.ColorMode) (PatternResult, error) {
	img, err := ascimage.ReadDualMode(r)
	if err != nil {
		return PatternResult{}, err
	}
	return fromDualModeImage(img, colorMode), nil
}
