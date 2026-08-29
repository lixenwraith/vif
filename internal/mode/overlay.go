package mode

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// handleOverlayScroll moves the card selection when the overlay supports it,
// otherwise scrolls the document by rows
func (r *Router) handleOverlayScroll(intent *input.Intent) bool {
	if r.ctx.IsOverlaySelectable() {
		if delta := selectedCardScrollDelta(
			r.ctx.OverlayCards(), r.ctx.GetOverlaySelection(), r.ctx.GetOverlayScroll(),
			r.ctx.OverlayGeometry().ContentH, intent.Motion,
		); delta != 0 {
			r.scrollOverlay(delta)
			return true
		}
		return r.moveOverlaySelection(intent.Motion)
	}

	switch intent.Motion {
	case input.MotionScreenTop:
		r.ctx.SetOverlayScroll(0)
	case input.MotionScreenBottom:
		r.scrollOverlay(r.ctx.GetOverlayContentH())
	default:
		r.scrollOverlay(int(intent.ScrollDir))
	}
	return true
}

// selectedCardScrollDelta keeps row navigation inside a clipped selected card
// before moving to its neighbour.
func selectedCardScrollDelta(cards []engine.OverlayCardRef, selected string, offset, viewportH int, motion input.MotionOp) int {
	if viewportH < 1 || (motion != input.MotionUp && motion != input.MotionDown) {
		return 0
	}
	for i := range cards {
		card := &cards[i]
		if card.Key != selected {
			continue
		}
		if motion == input.MotionUp && card.Y < offset {
			return -1
		}
		if motion == input.MotionDown && card.Y+card.H > offset+viewportH {
			return 1
		}
		return 0
	}
	return 0
}

// handleOverlayPageScroll pages the view without disturbing the selection
func (r *Router) handleOverlayPageScroll(direction int) bool {
	g := r.ctx.OverlayGeometry()
	r.scrollOverlay(direction * max(g.ContentH-parameter.OverlayPageOverlap, 1))
	return true
}

// scrollOverlay applies a row delta clamped to the rendered content height
func (r *Router) scrollOverlay(delta int) {
	if delta == 0 {
		return
	}
	g := r.ctx.OverlayGeometry()
	maxOffset := max(r.ctx.GetOverlayContentH()-g.ContentH, 0)
	r.ctx.SetOverlayScroll(min(max(r.ctx.GetOverlayScroll()+delta, 0), maxOffset))
}

// handleOverlayActivate pins or unpins the selected card and requests a rebuild
func (r *Router) handleOverlayActivate() bool {
	key := r.ctx.GetOverlaySelection()
	if !r.ctx.IsOverlaySelectable() || key == "" {
		return true
	}
	r.ctx.ToggleOverlayPin(key)
	// The debug overlay is the only card layout; MetaSystem owns the projection
	r.ctx.PushLocal(event.EventMetaDebugRequest, nil)
	return true
}

// moveOverlaySelection resolves a directional move against the published card index
func (r *Router) moveOverlaySelection(motion input.MotionOp) bool {
	cards := r.ctx.OverlayCards()
	if len(cards) == 0 {
		return true
	}

	cur := 0
	if sel := r.ctx.GetOverlaySelection(); sel != "" {
		for i := range cards {
			if cards[i].Key == sel {
				cur = i
				break
			}
		}
	}

	next := cur
	switch motion {
	case input.MotionScreenTop:
		next = 0
	case input.MotionScreenBottom:
		next = len(cards) - 1
	case input.MotionUp:
		next = nearestInColumn(cards, cur, false)
	case input.MotionDown:
		next = nearestInColumn(cards, cur, true)
	case input.MotionLeft:
		next = nearestInColumn2(cards, cur, false)
	case input.MotionRight:
		next = nearestInColumn2(cards, cur, true)
	}

	r.ctx.SetOverlaySelection(cards[next].Key)
	return true
}

// nearestInColumn returns the adjacent card in the same masonry column
func nearestInColumn(cards []engine.OverlayCardRef, cur int, down bool) int {
	best, bestY := cur, 0
	for i := range cards {
		if i == cur || cards[i].X != cards[cur].X {
			continue
		}
		dy := cards[i].Y - cards[cur].Y
		if (down && dy <= 0) || (!down && dy >= 0) {
			continue
		}
		if dy < 0 {
			dy = -dy
		}
		if best == cur || dy < bestY {
			best, bestY = i, dy
		}
	}
	return best
}

// nearestInColumn2 returns the vertically closest card in the adjacent column
func nearestInColumn2(cards []engine.OverlayCardRef, cur int, right bool) int {
	// Resolve the neighbouring column first, then the closest card within it
	col, found := 0, false
	for i := range cards {
		x := cards[i].X
		if (right && x <= cards[cur].X) || (!right && x >= cards[cur].X) {
			continue
		}
		if !found || (right && x < col) || (!right && x > col) {
			col, found = x, true
		}
	}
	if !found {
		return cur
	}

	best, bestD := cur, 0
	for i := range cards {
		if cards[i].X != col {
			continue
		}
		d := cards[i].Y - cards[cur].Y
		if d < 0 {
			d = -d
		}
		if best == cur || d < bestD {
			best, bestD = i, d
		}
	}
	return best
}
