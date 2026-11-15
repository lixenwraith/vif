package mode

import (
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// changeHistorySize bounds the changes undo_change takes back
const changeHistorySize = 64

// register is what the last yank or delete covered: each glyph at its offset from
// the range's start, and whether the range was whole rows
type register struct {
	cells []event.GlyphCell
	lines bool
}

// change is one edit as its undo and redo need it: the glyphs it took or retyped,
// as they stood, and those it laid or retyped them into
type change struct {
	before, after []event.GlyphCell
}

// edits is the editing state a router keeps for its player: the register and the
// changes made, of which done are applied and the rest undone, with the game and
// the glyph clear they were made after, past which none is taken back
type edits struct {
	register register
	changes  []change
	done     int
	run      uint64
	cleared  int64
}

// OpMove requests cursor placement from a motion result; CursorSystem applies it
func OpMove(ctx *engine.GameContext, result MotionResult) {
	if !result.Valid {
		return
	}
	OpJump(ctx, result.EndX, result.EndY)
}

// OpJump requests an absolute placement for producers that resolve a target
// directly. The crossing and the D-18 prediction that answers it locally leave
// together, so the next motion resolves from the cell this press selected rather
// than from the one the shared store still holds.
func OpJump(ctx *engine.GameContext, x, y int) {
	ctx.PushCursorMove(ctx.World.Resources.Player.Entity, x, y)
}

// rangeOf is the range a motion's result names, its start first: rows whole for a
// line range, else its span with an exclusive end left out; false for no range
func rangeOf(ctx *engine.GameContext, result MotionResult) (event.DeleteRequestPayload, bool) {
	if !result.Valid {
		return event.DeleteRequestPayload{}, false
	}
	if result.Type == RangeLine {
		return event.DeleteRequestPayload{
			RangeType: event.DeleteRangeLine,
			StartY:    min(result.StartY, result.EndY), EndY: max(result.StartY, result.EndY),
		}, true
	}
	sx, sy, ex, ey := result.StartX, result.StartY, result.EndX, result.EndY
	if sy > ey || (sy == ey && sx > ex) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}
	// An exclusive end leaves its last cell out: dw lands on the next word's start
	if result.Style == StyleExclusive {
		switch {
		case ex > 0:
			ex--
		case ey > 0:
			ey, ex = ey-1, ctx.World.Resources.Config.MapWidth-1
		}
		if sy > ey || (sy == ey && sx > ex) {
			return event.DeleteRequestPayload{}, false
		}
	}
	return event.DeleteRequestPayload{StartX: sx, StartY: sy, EndX: ex, EndY: ey}, true
}

// reach reads the glyphs an edit reaches in the cells a range covers, row by row
func reach(ctx *engine.GameContext, p *event.DeleteRequestPayload) []event.GlyphCell {
	config := ctx.World.Resources.Config
	var cells []event.GlyphCell
	for y := max(min(p.StartY, p.EndY), 0); y <= min(max(p.StartY, p.EndY), config.MapHeight-1); y++ {
		for x := range config.MapWidth {
			if !p.Covers(x, y) {
				continue
			}
			if c, ok := glyphAt(ctx, x, y); ok {
				cells = append(cells, c)
			}
		}
	}
	return cells
}

// glyphAt reads the glyph an edit reaches at (x, y), if one stands there
func glyphAt(ctx *engine.GameContext, x, y int) (event.GlyphCell, bool) {
	e, ok := ctx.World.Positions.EditableGlyphAt(x, y)
	if !ok {
		return event.GlyphCell{}, false
	}
	g, _ := ctx.World.Components.Glyph.GetComponent(e)
	return event.GlyphCell{X: x, Y: y, Rune: g.Rune, Type: g.Type, Level: g.Level}, true
}

// operate hands a range to an operator: d and c fill the register and delete, y
// fills it alone
func (r *Router) operate(op input.OperatorOp, p event.DeleteRequestPayload) {
	switch op {
	case input.OperatorDelete:
		r.delete(p)
	case input.OperatorYank:
		r.yank(&p)
	}
}

// yank fills the register with what an edit reaches in the range and returns it
// where it stands; a range holding nothing leaves the register as it was
func (r *Router) yank(p *event.DeleteRequestPayload) []event.GlyphCell {
	cells := reach(r.ctx, p)
	if len(cells) == 0 {
		return nil
	}
	ox, oy := p.StartX, p.StartY
	switch p.RangeType {
	case event.DeleteRangeLine:
		ox = 0
	case event.DeleteRangeBlock:
		ox, oy = min(p.StartX, p.EndX), min(p.StartY, p.EndY)
	}
	held := make([]event.GlyphCell, len(cells))
	for i, c := range cells {
		c.X, c.Y = c.X-ox, c.Y-oy
		held[i] = c
	}
	r.edit.register = register{cells: held, lines: p.RangeType == event.DeleteRangeLine}
	return cells
}

// delete asks for a range's deletion, having filled the register with it as vim's
// unnamed register is, and keeps what it takes for an undo
func (r *Router) delete(p event.DeleteRequestPayload) {
	if cells := r.yank(&p); len(cells) > 0 {
		r.record(change{before: cells})
	}
	r.ctx.PushLocal(event.EventDeleteRequest, &p)
}

// retype asks for each glyph an edit reaches in the box to read as to makes of it,
// and keeps the runes it changes for an undo
func (r *Router) retype(x0, y0, x1, y1 int, to func(rune) rune) {
	var before, after []event.GlyphCell
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c, ok := glyphAt(r.ctx, x, y)
			if now := to(c.Rune); ok && now != c.Rune && component.GlyphRune(now) {
				before = append(before, c)
				c.Rune = now
				after = append(after, c)
			}
		}
	}
	if len(after) > 0 {
		r.record(change{before: before, after: after})
		r.pushRetype(after)
	}
}

// pushRetype asks for the glyphs of cells, read row by row, to take the cells'
// runes: a request a row, a space for each cell between that keeps its own
func (r *Router) pushRetype(cells []event.GlyphCell) {
	for len(cells) > 0 {
		n := 1
		for n < len(cells) && cells[n].Y == cells[0].Y {
			n++
		}
		text := []rune(strings.Repeat(" ", cells[n-1].X-cells[0].X+1))
		for _, c := range cells[:n] {
			text[c.X-cells[0].X] = c.Rune
		}
		r.ctx.PushLocal(event.EventGlyphReplaceRequest, &event.GlyphReplaceRequestPayload{
			X: cells[0].X, Y: cells[0].Y, Text: string(text),
		})
		cells = cells[n:]
	}
}

// erase asks for the deletion of exactly cells, read row by row: a span a run
func (r *Router) erase(cells []event.GlyphCell) {
	for len(cells) > 0 {
		n := 1
		for n < len(cells) && cells[n].Y == cells[0].Y && cells[n].X == cells[n-1].X+1 {
			n++
		}
		r.ctx.PushLocal(event.EventDeleteRequest, &event.DeleteRequestPayload{
			StartX: cells[0].X, StartY: cells[0].Y, EndX: cells[n-1].X, EndY: cells[0].Y,
		})
		cells = cells[n:]
	}
}

// put lays the register count times from the cursor outward: a span from the cell
// right of it, or for before ending at the cell left of it, and rows on the rows
// below, or above. A cell a glyph, a wall or rock holds is passed over, and the
// cursor goes to the far end of what was put, where a repeat goes on from.
func (r *Router) put(x, y, count int, before bool) {
	reg := r.edit.register
	if len(reg.cells) == 0 {
		return
	}
	left, right, height := 0, 0, reg.cells[len(reg.cells)-1].Y+1
	for _, c := range reg.cells {
		left, right = min(left, c.X), max(right, c.X)
	}
	ox, oy, dx, dy := x+1-left, y, right-left+1, 0
	switch {
	case reg.lines && before:
		ox, oy, dx, dy = 0, y-height, 0, -height
	case reg.lines:
		ox, oy, dx, dy = 0, y+1, 0, height
	case before:
		ox, dx = x-1-right, -dx
	}
	var cells []event.GlyphCell
	taken := make(map[vmath.Point]bool)
	far := 0 // where the last copy that laid a glyph starts
	for i := range count {
		laid := len(cells)
		for _, c := range reg.cells {
			c.X, c.Y = ox+c.X+i*dx, oy+c.Y+i*dy
			if at := (vmath.Point{X: c.X, Y: c.Y}); !taken[at] && r.ctx.World.Positions.TakesGlyph(c.X, c.Y) {
				taken[at] = true
				cells = append(cells, c)
			}
		}
		if len(cells) == laid {
			break // this copy met no free cell, and the next lies further off
		}
		far = laid
	}
	if len(cells) == 0 {
		return
	}
	r.record(change{after: cells})
	r.ctx.PushLocal(event.EventGlyphPutRequest, &event.GlyphPutPayload{Cells: cells})
	to := cells[far]
	if !before {
		to = cells[len(cells)-1]
		if reg.lines {
			to = cells[slices.IndexFunc(cells[far:], func(c event.GlyphCell) bool { return c.Y == to.Y })+far]
		}
	}
	if !isCursorBlocked(r.ctx, to.X, to.Y) {
		OpJump(r.ctx, to.X, to.Y)
	}
}

// history returns the changes made in this game since its last glyph clear: one
// made before either is dropped, its cells no longer holding what it took
func (r *Router) history() *edits {
	h := &r.edit
	run, cleared := r.ctx.World.Resources.Event.Queue.Stamp().Run, r.cleared.Load()
	if h.run != run || h.cleared != cleared {
		h.changes, h.done, h.run, h.cleared = h.changes[:0], 0, run, cleared
	}
	return h
}

// record adds a change to the history in place of those undone, the oldest going
// once it is full
func (r *Router) record(c change) {
	h := r.history()
	h.changes = append(h.changes[:h.done], c)
	if len(h.changes) > changeHistorySize {
		h.changes = slices.Delete(h.changes, 0, 1)
	}
	h.done = len(h.changes)
}

// undoChange takes back the last change still standing, and redoChange makes the
// last one taken back again; a change whose cells changed under it is dropped
func (r *Router) undoChange() {
	for h := r.history(); h.done > 0; {
		h.done--
		if c := h.changes[h.done]; r.turn(c.after, c.before) {
			return
		}
		h.changes = slices.Delete(h.changes, h.done, h.done+1)
	}
}

func (r *Router) redoChange() {
	for h := r.history(); h.done < len(h.changes); {
		if c := h.changes[h.done]; r.turn(c.before, c.after) {
			h.done++
			return
		}
		h.changes = slices.Delete(h.changes, h.done, h.done+1)
	}
}

// turn takes a change's cells from one of its states to the other: erased, laid
// or retyped. It refuses where the glyphs of from no longer stand as it has them,
// or a cell it would lay a glyph in is held.
func (r *Router) turn(from, to []event.GlyphCell) bool {
	for _, c := range from {
		if now, ok := glyphAt(r.ctx, c.X, c.Y); !ok || now.Rune != c.Rune {
			return false
		}
	}
	switch {
	case len(to) == 0:
		r.erase(from)
	case len(from) > 0:
		r.pushRetype(to)
	default:
		for _, c := range to {
			if !r.ctx.World.Positions.TakesGlyph(c.X, c.Y) {
				return false
			}
		}
		r.ctx.PushLocal(event.EventGlyphPutRequest, &event.GlyphPutPayload{Cells: to})
	}
	return true
}
