package pattern

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// PatternCell holds visual data + offset for one cell
type PatternCell struct {
	OffsetX  int
	OffsetY  int
	Rune     rune
	Fg       terminal.RGB
	Bg       terminal.RGB
	Attrs    terminal.Attr // Preserves AttrFg256/AttrBg256 from ascimage
	RenderFg bool
	RenderBg bool
}

// PatternResult is the output of any pattern generator
type PatternResult struct {
	Cells   []PatternCell
	Width   int // Bounding width
	Height  int // Bounding height
	AnchorX int // Suggested spawn anchor X
	AnchorY int // Suggested spawn anchor Y
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
		}
	}
	return defs
}

// SpawnAsComposite emits WallCompositeSpawnRequest event
func (p *PatternResult) SpawnAsComposite(
	queue *event.EventQueue,
	anchorX, anchorY int,
	mask component.WallBlockMask,
	boxStyle component.BoxDrawStyle,
) {
	if len(p.Cells) == 0 {
		return
	}

	queue.Push(event.GameEvent{
		Type: event.EventWallCompositeSpawnRequest,
		Payload: &event.WallCompositeSpawnRequestPayload{
			X:         anchorX,
			Y:         anchorY,
			BlockMask: mask,
			Cells:     p.ToWallCellDefs(),
			BoxStyle:  boxStyle,
		},
	})
}

// Bounds returns the bounding rectangle of the pattern
func (p *PatternResult) Bounds() (minX, minY, maxX, maxY int) {
	if len(p.Cells) == 0 {
		return 0, 0, 0, 0
	}

	minX, minY = p.Cells[0].OffsetX, p.Cells[0].OffsetY
	maxX, maxY = minX, minY

	for _, cell := range p.Cells[1:] {
		if cell.OffsetX < minX {
			minX = cell.OffsetX
		}
		if cell.OffsetX > maxX {
			maxX = cell.OffsetX
		}
		if cell.OffsetY < minY {
			minY = cell.OffsetY
		}
		if cell.OffsetY > maxY {
			maxY = cell.OffsetY
		}
	}
	return minX, minY, maxX, maxY
}

// Count returns number of cells in pattern
func (p *PatternResult) Count() int {
	return len(p.Cells)
}

// Empty returns true if pattern has no cells
func (p *PatternResult) Empty() bool {
	return len(p.Cells) == 0
}