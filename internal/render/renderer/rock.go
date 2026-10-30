package renderer

import (
	"math"
	"slices"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/render"
)

// stoneGroupWidth is a cave wall's width as stone art sizes a group: the wall runs the
// cave's length, which has no end
const stoneGroupWidth = math.MaxInt32

// stoneSealed stands for a column beyond the field: rock throughout, so no opening
// there brings a cell's face nearer
var stoneSealed = engine.RockColumn{Ceil: math.MaxInt, Floor: math.MaxInt}

// stoneNeighbourhood is a column and those within pattern.FaceReach either side of it,
// all a rock cell's depth and face are read from
type stoneNeighbourhood [2*pattern.FaceReach + 1]engine.RockColumn

// stoneCell is one cell as its stone art draws it; a zero rune is open
type stoneCell struct {
	r      rune
	fg, bg color.RGB
}

// stoneColumns keeps each profile column's stone with the neighbourhood it was read
// from. A column's art is a function of its profile index and that neighbourhood, so a
// scrolling field draws a column once it has its neighbours and then keeps it: a frame
// draws only what entered since the last, a few columns at the field's right edge.
type stoneColumns struct {
	look     engine.RockLook
	height   int
	index256 bool    // cells hold pattern.Art256's indices, as the 256 tier draws them
	tags     []int64 // the profile column each slot holds, or -1
	around   []stoneNeighbourhood
	cells    []stoneCell // height cells a slot
}

// renderRock draws the rock field's cells the viewport shows, each map column the one
// carry columns on, from the columns ahead past the field's end: each column's ceiling
// rows, then its floor rows, in its visual or its stone
func (r *WallRenderer) renderRock(ctx *render.RenderContext, buf *render.RenderBuffer, rock *engine.RockField, carry int) {
	r.drawn = append(append(r.drawn[:0], rock.Columns()...), rock.Ahead()...)
	look := r.gameCtx.World.Positions.RockLook()
	if look.Stone {
		r.renderStone(ctx, buf, rock, carry)
		return
	}
	vis := look.Visual
	if !(vis.RenderFg || vis.RenderBg) {
		return
	}
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	draw := func(x, y int) {
		if screenX, screenY, visible := ctx.MapToScreen(x, y); visible {
			r.renderCell(buf, screenX, screenY, vis.Char, vis.FgColor, vis.BgColor, vis.RenderFg, vis.RenderBg, terminal.AttrNone)
		}
	}
	for x := max(minX, 0); x <= min(maxX, rock.Width()-1); x++ {
		c := r.drawn[x+carry]
		for y := minY; y < min(c.Ceil, maxY+1); y++ {
			draw(x, y)
		}
		for y := max(c.Floor, minY); y <= maxY; y++ {
			draw(x, y)
		}
	}
}

// renderStone draws the visible rock in its stone art, each column from the cache
func (r *WallRenderer) renderStone(ctx *render.RenderContext, buf *render.RenderBuffer, rock *engine.RockField, carry int) {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	height := r.gameCtx.World.Resources.Config.MapHeight
	r.stone.fit(r.gameCtx.World.Positions.RockLook(), rock.Width(), height)
	attrs := terminal.AttrNone
	if r.stone.index256 {
		attrs = terminal.AttrFg256 | terminal.AttrBg256
	}
	minY, maxY = max(minY, 0), min(maxY, height-1)
	for x := max(minX, 0); x <= min(maxX, rock.Width()-1); x++ {
		column := r.stone.column(r.drawn, x+carry, rock.Origin)
		for y := minY; y <= maxY; y++ {
			cell := &column[y]
			if cell.r == 0 {
				continue
			}
			if screenX, screenY, visible := ctx.MapToScreen(x, y); visible {
				r.renderCell(buf, screenX, screenY, cell.r, cell.fg, cell.bg, true, true, attrs)
			}
		}
	}
}

// fit sizes the cache for a field width columns wide and height rows tall in look,
// forgetting every column when either changes
func (s *stoneColumns) fit(look engine.RockLook, width, height int) {
	if s.look == look && s.height == height && len(s.tags) == width {
		return
	}
	s.look, s.height = look, height
	s.tags = slices.Grow(s.tags[:0], width)[:width]
	s.around = slices.Grow(s.around[:0], width)[:width]
	s.cells = slices.Grow(s.cells[:0], width*height)[:width*height]
	for i := range s.tags {
		s.tags[i] = -1
	}
}

// column returns map column x's stone, drawing it again where its profile column is
// not the slot's or its neighbourhood moved, as a column entering at the edge gains
// its neighbours
func (s *stoneColumns) column(cols []engine.RockColumn, x int, origin int64) []stoneCell {
	var around stoneNeighbourhood
	for i := range around {
		if nx := x + i - pattern.FaceReach; nx >= 0 && nx < len(cols) {
			around[i] = cols[nx]
		} else {
			around[i] = stoneSealed
		}
	}
	index := origin + int64(x)
	slot := int(index % int64(len(s.tags)))
	cells := s.cells[slot*s.height : (slot+1)*s.height]
	if s.tags[slot] == index && s.around[slot] == around {
		return cells
	}
	s.tags[slot], s.around[slot] = index, around
	for y := range cells {
		if !around[pattern.FaceReach].Rock(y) {
			cells[y] = stoneCell{}
			continue
		}
		depth, face := stoneDepth(&around, y)
		cell := pattern.ArtCell{X: index, Y: int64(y), Depth: depth, Face: face, W: stoneGroupWidth, H: s.height}
		if s.index256 {
			r, fg, bg := pattern.Art256(pattern.ArtPreset(s.look.Art), s.look.Seed, cell)
			cells[y] = stoneCell{r: r, fg: color.RGB{R: fg}, bg: color.RGB{R: bg}}
			continue
		}
		cells[y].r, cells[y].fg, cells[y].bg = pattern.Art(pattern.ArtPreset(s.look.Art), s.look.Seed, cell)
	}
	return cells
}

// stoneDepth is how deep row y of the neighbourhood's own column lies in its rock and
// the side its nearest opening lies on: below a ceiling, above a floor, or along the
// row where a column within reach opens nearer
func stoneDepth(around *stoneNeighbourhood, y int) (int, pattern.Face) {
	c := around[pattern.FaceReach]
	depth, face := c.Ceil-1-y, pattern.FaceDown
	if y >= c.Floor {
		depth, face = y-c.Floor, pattern.FaceUp
	}
	for d := 1; d <= min(depth, pattern.FaceReach); d++ {
		if !around[pattern.FaceReach-d].Rock(y) {
			return d - 1, pattern.FaceLeft
		}
		if !around[pattern.FaceReach+d].Rock(y) {
			return d - 1, pattern.FaceRight
		}
	}
	return depth, face
}
