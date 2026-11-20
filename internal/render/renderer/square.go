package renderer

import (
	"math"
	"strconv"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareRenderer draws a magic square: each block's cells in its kind's colors,
// darker as they wear, its digit at its middle, a fallen block's outline, a frame
// round the viewer's selection, the shield while it holds, and under the square
// each kind's state
type SquareRenderer struct {
	gameCtx *engine.GameContext
	console *render.Console // nil draws true colour
}

func NewSquareRenderer(ctx *engine.GameContext) *SquareRenderer {
	r := &SquareRenderer{gameCtx: ctx}
	if cfg := ctx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.console = render.ConsoleFor(cfg.ConsolePalette)
	}
	return r
}

func (r *SquareRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	if world.Components.Square.CountEntities() == 0 {
		return
	}
	buf.SetWriteMask(visual.MaskComposite)
	slot := world.Resources.Player.ViewSlot()
	world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
		for kind, spec := range component.SquareKinds {
			if spec.Shield && sq.Effects[kind].Active > 0 {
				r.shield(ctx, buf, sq, &visual.SquareKinds[kind])
			}
		}
		for p, block := range sq.Places {
			r.block(ctx, buf, sq, p, block)
		}
		if int(slot) < len(sq.Selected) {
			r.frame(ctx, buf, sq, sq.Selected[slot])
		}
		r.legend(ctx, buf, sq)
		return true
	})
}

// block draws the block at place p: its standing cells, or a fallen one's outline,
// and its digit at its middle
func (r *SquareRenderer) block(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, p int, block core.Entity) {
	world := r.gameCtx.World
	b, ok := world.Components.Block.GetPtr(block)
	header, hok := world.Components.Header.GetPtr(block)
	if !ok || !hok || int(b.Kind) >= len(visual.SquareKinds) {
		return
	}
	colors := &visual.SquareKinds[b.Kind]
	x, y := sq.Place(p)
	midX, midY := x+sq.Width/2, y+sq.Height/2
	if b.Fallen {
		r.outline(ctx, buf, x, y, sq.Width, sq.Height)
	}
	middle := false
	for _, m := range header.MemberEntries {
		c, ok := world.Components.Combat.GetPtr(m.Entity)
		pos, pok := world.Positions.GetPosition(m.Entity)
		if m.Entity == 0 || !ok || !pok {
			continue
		}
		middle = middle || pos.X == midX && pos.Y == midY
		sx, sy, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			continue
		}
		health := min(max(float64(c.HitPoints)/float64(max(b.HP, 1)), 0), 1)
		if r.console != nil {
			r.cell256(buf, sx, sy, colors, health)
			continue
		}
		bg := color.Lerp(colors.Worn, colors.Whole, health)
		if c.RemainingHitFlash > 0 {
			bg = color.Lerp(bg, visual.RgbWhite, visual.SquareHitFlash)
		}
		buf.SetWithBg(sx, sy, ' ', bg, bg)
	}

	sx, sy, visible := ctx.MapToScreen(midX, midY)
	if !visible {
		return
	}
	digit := '0' + rune(b.Digit)
	switch {
	case b.Fallen:
		r.mark(buf, sx, sy, digit, visual.RgbSquareFallen, visual.ConGray, terminal.AttrNone)
	case middle:
		r.mark(buf, sx, sy, digit, visual.RgbSquareDigit, visual.ConBlack, terminal.AttrBold)
	default:
		r.mark(buf, sx, sy, digit, colors.Whole, colors.Console, terminal.AttrBold)
	}
}

// cell256 shows a cell worn past half as shading over black, as a console's eight
// backgrounds hold no shade between
func (r *SquareRenderer) cell256(buf *render.RenderBuffer, sx, sy int, colors *visual.SquareKindColors, health float64) {
	buf.SetWithBg(sx, sy, ' ', visual.RgbBlack, visual.RgbBlack)
	if health >= visual.SquareWorn256 {
		buf.SetBg256(sx, sy, r.console.Background(colors.Console))
		return
	}
	buf.SetBg256(sx, sy, visual.ConBlack)
	buf.SetFgOnly(sx, sy, visual.SquareWornChar256, color.RGB{R: colors.Console}, terminal.AttrFg256)
}

// mark writes a glyph over a cell's background, in tc or a console's entry
func (r *SquareRenderer) mark(buf *render.RenderBuffer, sx, sy int, ch rune, tc color.RGB, console uint8, attr terminal.Attr) {
	if r.console != nil {
		buf.SetFgOnly(sx, sy, ch, color.RGB{R: console}, attr|terminal.AttrFg256)
		return
	}
	buf.SetFgOnly(sx, sy, ch, tc, attr)
}

// outline dots a fallen block's edge
func (r *SquareRenderer) outline(ctx render.RenderContext, buf *render.RenderBuffer, x, y, w, h int) {
	for dy := range h {
		for dx := range w {
			if dy != 0 && dy != h-1 && dx != 0 && dx != w-1 {
				continue
			}
			if sx, sy, visible := ctx.MapToScreen(x+dx, y+dy); visible {
				r.mark(buf, sx, sy, visual.SquareFallenChar, visual.RgbSquareFallen, visual.ConGray, terminal.AttrNone)
			}
		}
	}
}

// frame rings the viewer's selected block in the gap round it
func (r *SquareRenderer) frame(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, selected core.Entity) {
	place := -1
	for p, block := range sq.Places {
		if block == selected && block != 0 {
			place = p
		}
	}
	if place < 0 {
		return
	}
	x, y := sq.Place(place)
	left, top, right, bottom := x-1, y-1, x+sq.Width, y+sq.Height
	for fy := top; fy <= bottom; fy++ {
		for fx := left; fx <= right; fx++ {
			var ch rune
			switch {
			case fy == top && fx == left:
				ch = visual.BorderSingleTopLeft
			case fy == top && fx == right:
				ch = visual.BorderSingleTopRight
			case fy == bottom && fx == left:
				ch = visual.BorderSingleBottomLeft
			case fy == bottom && fx == right:
				ch = visual.BorderSingleBottomRight
			case fy == top || fy == bottom:
				ch = visual.BorderSingleHorizontal
			case fx == left || fx == right:
				ch = visual.BorderSingleVertical
			default:
				continue
			}
			if sx, sy, visible := ctx.MapToScreen(fx, fy); visible {
				r.mark(buf, sx, sy, ch, visual.RgbSquareFrame, visual.ConBrightWhite, terminal.AttrBold)
			}
		}
	}
}

// shield glows a band inside the shield's rim while it holds, pulsing; a console
// shows the band in the kind's color
func (r *SquareRenderer) shield(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, colors *visual.SquareKindColors) {
	w, h := sq.Size()
	cx, cy := sq.X+w/2, sq.Y+h/2
	rx, ry := sq.ShieldRadii(ctx.CellAspect)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	period := int64(visual.SquareShieldGlowPeriodMs)
	angle := float64(r.gameCtx.World.Resources.Time.GameTime.UnixMilli()%period) / float64(period) * vmath.TwoPi
	intensity := visual.SquareShieldGlowMin + (visual.SquareShieldGlowMax-visual.SquareShieldGlowMin)*(0.5+0.5*vmath.Sin(angle))

	for my := max(0, cy-int(ry)-1); my <= min(ctx.MapHeight-1, cy+int(ry)+1); my++ {
		for mx := max(0, cx-int(rx)-1); mx <= min(ctx.MapWidth-1, cx+int(rx)+1); mx++ {
			d := math.Sqrt(vmath.EllipseDistSq(float64(mx-cx), float64(my-cy), invRxSq, invRySq))
			alpha := 1 - (1-d)/visual.SquareShieldGlowBand
			if d > 1 || alpha <= 0 {
				continue
			}
			sx, sy, visible := ctx.MapToScreen(mx, my)
			if !visible {
				continue
			}
			if r.console != nil {
				if alpha >= visual.SquareShieldRim256 {
					buf.SetBg256(sx, sy, r.console.Background(colors.Console))
				}
				continue
			}
			buf.SetBgScreen(sx, sy, colors.Glow, visual.RgbBackground, intensity*alpha)
		}
	}
}

// legend writes each kind's state on the row under the square: its name in its
// color while ready, white with the seconds it holds for, gray with those it
// recharges for
func (r *SquareRenderer) legend(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent) {
	w, h := sq.Size()
	y := sq.Y + h + 1
	if y >= ctx.MapHeight {
		return
	}
	var texts [component.SquareKindCount][24]byte
	var lens [component.SquareKindCount]int
	total := (len(texts) - 1) * visual.SquareLegendGap
	for kind, spec := range component.SquareKinds {
		text := append(texts[kind][:0], spec.Name...)
		if left := max(sq.Effects[kind].Active, sq.Effects[kind].Recharge); left > 0 {
			text = append(text, ' ')
			text = strconv.AppendInt(text, int64((left+time.Second-1)/time.Second), 10)
			text = append(text, 's')
		}
		lens[kind] = len(text)
		total += len(text)
	}

	x := sq.X + (w-total)/2
	for kind := range texts {
		effect, colors := sq.Effects[kind], &visual.SquareKinds[kind]
		tc, console, attr := colors.Whole, colors.Console, terminal.AttrBold
		switch {
		case effect.Active > 0:
			tc, console = visual.RgbSquareFrame, visual.ConBrightWhite
		case effect.Recharge > 0:
			tc, console, attr = visual.RgbSquareFallen, visual.ConGray, terminal.AttrNone
		}
		for i := range lens[kind] {
			if sx, sy, visible := ctx.MapToScreen(x+i, y); visible {
				r.mark(buf, sx, sy, rune(texts[kind][i]), tc, console, attr)
			}
		}
		x += lens[kind] + visual.SquareLegendGap
	}
}
