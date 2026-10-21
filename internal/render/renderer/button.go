package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// ButtonRenderer draws each button as an opaque block with its key in the splash font,
// so drifting glyphs pass behind it. The key is black and the block dim until the
// button lights; then the key turns white and the block brightens, fading back.
type ButtonRenderer struct {
	gameCtx *engine.GameContext
	console *render.Console // nil draws true colour
}

func NewButtonRenderer(ctx *engine.GameContext) *ButtonRenderer {
	r := &ButtonRenderer{gameCtx: ctx}
	if cfg := ctx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.console = render.ConsoleFor(cfg.ConsolePalette)
	}
	return r
}

// buttonCell is what one cell of a button shows
type buttonCell uint8

const (
	buttonFill  buttonCell = iota
	buttonInk              // part of the key's splash glyph
	buttonLabel            // the key as one character, when the glyph does not fit
)

// buttonKey places a key's splash glyph with its ink centred on a w by h button. A
// glyph whose ink does not fit, or a key the font lacks, falls back to the key as
// one character in the middle.
type buttonKey struct {
	glyph      splashBitmap
	big        bool
	x, y, w, h int // glyph cell origin within the button, and the button's size
}

func placeButtonKey(key rune, w, h int) buttonKey {
	glyph, ok := splashGlyph(key)
	k := buttonKey{glyph: glyph, w: w, h: h}
	left, top, inkW, inkH := splashInkBounds(glyph)
	if ok && inkW > 0 && inkW <= w && inkH <= h {
		k.big, k.x, k.y = true, (w-inkW)/2-left, (h-inkH)/2-top
	}
	return k
}

func (k buttonKey) at(dx, dy int) buttonCell {
	switch {
	case k.big && splashInk(k.glyph, dx-k.x, dy-k.y):
		return buttonInk
	case !k.big && dx == k.w/2 && dy == k.h/2:
		return buttonLabel
	}
	return buttonFill
}

func (r *ButtonRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	buttons := world.Components.Button
	count := buttons.CountEntities()
	if count == 0 {
		return
	}
	// Composite owns the cell outright and stays out of grayout and the occlusion dim
	buf.SetWriteMask(visual.MaskComposite)
	cfg := world.Resources.Config
	buttons.Each(func(e core.Entity, b *component.ButtonComponent) bool {
		x, y, w, h := component.ButtonRect(b.Slot, count, cfg.MapWidth, cfg.MapHeight)
		lit := r.lit(e, b)
		key := placeButtonKey(b.Key, w, h)
		for dy := range h {
			for dx := range w {
				sx, sy, visible := ctx.MapToScreen(x+dx, y+dy)
				if !visible {
					continue
				}
				if r.console != nil {
					r.cell256(buf, sx, sy, b, lit > 0, key.at(dx, dy))
				} else {
					r.cellTrueColor(buf, sx, sy, b, lit, key.at(dx, dy))
				}
			}
		}
		return true
	})
}

// lit is how lit a button shows, 1 to 0 as its light fades. This instance's own press
// shows by its echo, at once; the shared light it crosses lands a playout lead later,
// so it is left to everyone else, as a placement is left to its prediction (D-18).
func (r *ButtonRenderer) lit(e core.Entity, b *component.ButtonComponent) float64 {
	world := r.gameCtx.World
	lit := 0.0
	if b.PressedBy == parameter.NoPlayerSlot || b.PressedBy != world.Resources.Player.LocalSlot() {
		if b.Held {
			lit = 1
		} else if t, ok := world.Components.Timer.GetPtr(e); ok && t.Running(component.TimerDeadline) {
			lit = 1 - t.Progress(component.TimerDeadline)
		}
	}
	if echo := world.Resources.View.ButtonEcho; echo.Key == b.Key {
		if echo.Held {
			return 1
		}
		lit = max(lit, float64(echo.Remaining)/float64(parameter.ButtonPressLight))
	}
	return lit
}

func (r *ButtonRenderer) cellTrueColor(buf *render.RenderBuffer, sx, sy int, b *component.ButtonComponent, lit float64, cell buttonCell) {
	fill := color.Scale(b.Color, visual.ButtonUnlit+(1-visual.ButtonUnlit)*lit)
	ink := color.Scale(visual.RgbWhite, lit)
	switch cell {
	case buttonInk:
		buf.SetWithBg(sx, sy, ' ', ink, ink)
	case buttonLabel:
		buf.SetWithBg(sx, sy, b.Key, ink, fill)
	default:
		buf.SetWithBg(sx, sy, ' ', fill, fill)
	}
}

// cell256 has no fade: a console shows a button lit for as long as it is lit. Eight
// backgrounds cannot show a bright entry, so a lit block and a white key are drawn
// as foreground blocks.
func (r *ButtonRenderer) cell256(buf *render.RenderBuffer, sx, sy int, b *component.ButtonComponent, lit bool, cell buttonCell) {
	dark, light := r.console.Family(b.Color)
	fill, block, ink := r.console.Background(dark), ' ', visual.ConBlack
	if lit {
		fill, block, ink = r.console.Background(light), visual.ButtonLit256, visual.ConBrightWhite
	}
	fg := light
	switch cell {
	case buttonInk:
		if !lit {
			fill = visual.ConBlack
		}
		fg = ink
	case buttonLabel:
		block, fg = b.Key, ink
	}
	buf.SetWithBg(sx, sy, block, visual.RgbBlack, visual.RgbBlack)
	buf.SetBg256(sx, sy, fill)
	if block != ' ' {
		buf.SetFgOnly(sx, sy, block, color.RGB{R: fg}, terminal.AttrFg256)
	}
}
