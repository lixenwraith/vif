package ui

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
)

// Panel is a modal overlay with a scrollable body. It owns the frame, the
// scroll state and the scrollbar; the caller renders rows into the region
// Render returns, starting at Scroll.
type Panel struct {
	Title  string
	Hint   string
	Status string // line inside the bottom of the frame, e.g. a full filename
	W, H   int    // 0 = 80% of the screen
	Cursor bool   // track a selected row rather than scrolling freely

	Sel    int
	Scroll int
	Rows   int // content rows, set by the caller before Render
	View   int // visible rows, set by Render
}

// Move advances the selection, or the scroll offset when Cursor is unset.
func (p *Panel) Move(d int) {
	if p.Cursor {
		p.Sel += d
	} else {
		p.Scroll += d
	}
	p.clamp()
}

// Page moves by n screens.
func (p *Panel) Page(n int) { p.Move(n * max(1, p.View)) }

// Half moves by n half screens.
func (p *Panel) Half(n int) { p.Move(n * max(1, p.View/2)) }

// First jumps to the top.
func (p *Panel) First() { p.Sel, p.Scroll = 0, 0 }

// Last jumps to the bottom.
func (p *Panel) Last() {
	p.Sel, p.Scroll = p.Rows-1, p.Rows
	p.clamp()
}

// Reset returns the panel to the top, for new content.
func (p *Panel) Reset() { p.Sel, p.Scroll = 0, 0 }

func (p *Panel) clamp() {
	if p.Rows <= 0 {
		p.Sel, p.Scroll = 0, 0
		return
	}
	p.Sel = tui.ClampCursor(p.Sel, p.Rows)
	if p.Cursor {
		p.Scroll = tui.AdjustScroll(p.Sel, p.Scroll, p.View, p.Rows)
	}
	p.Scroll = tui.ClampScroll(p.Scroll, p.View, p.Rows)
}

// Render draws the frame and returns the body region, scrollbar excluded.
// A zero-width result means the screen is too small to show the panel.
func (p *Panel) Render(root tui.Region, th *Theme) tui.Region {
	w, h := p.W, p.H
	if w <= 0 {
		w = root.W * 80 / 100
	}
	if h <= 0 {
		h = root.H * 80 / 100
	}
	w, h = min(w, root.W-4), min(h, root.H-2)
	if w < 24 || h < 6 {
		return tui.Region{}
	}

	t, hint := p.Title, p.Hint
	if t != "" {
		t = " " + t + " "
	}
	if hint != "" {
		hint = " " + hint + " "
	}

	content := tui.Center(root, w, h).Modal(tui.ModalOpts{
		Title: t, Hint: hint, Border: tui.LineDouble,
		BorderFg: th.Accent, TitleFg: th.HeaderFg, HintFg: th.HintFg, Bg: th.FocusBg,
	})

	body := content
	if p.Status != "" && content.H > 3 {
		body = content.Sub(0, 0, content.W, content.H-2)
		content.HLine(content.H-2, tui.LineSingle, th.Border)
		content.Text(0, content.H-1, tui.Truncate(p.Status, content.W),
			th.HintFg, th.FocusBg, terminal.AttrDim)
	}

	p.View = body.H
	p.clamp()
	if p.Rows > body.H && body.W > 2 {
		body.Sub(body.W-1, 0, 1, body.H).ScrollBar(0, p.Scroll, body.H, p.Rows, th.Border)
		body = body.Sub(0, 0, body.W-1, body.H)
	}
	return body
}
