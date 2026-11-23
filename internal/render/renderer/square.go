package renderer

import (
	"slices"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// SquareRenderer draws a magic square: each block in its kind's color, lit as its
// line fires, its digit at its middle, a frame round the viewer's selection, and
// under the square each kind's name and cooldown
type SquareRenderer struct {
	gameCtx *engine.GameContext
	console *render.Console // nil draws true color
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
		for p := range sq.Digits {
			r.block(ctx, buf, sq, p)
		}
		if int(slot) < len(sq.Selected) {
			r.frame(ctx, buf, sq, sq.Selected[slot])
		}
		r.legend(ctx, buf, sq)
		return true
	})
}

// block draws the block at place p and its digit at its middle
func (r *SquareRenderer) block(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, p int) {
	kind := sq.KindAt(p)
	if int(kind) >= len(visual.SquareKinds) {
		return
	}
	colors := &visual.SquareKinds[kind]
	bg := color.Lerp(colors.Block, colors.Glow, fired(sq, kind))
	x, y := sq.Place(p)
	for dy := range sq.Height {
		for dx := range sq.Width {
			sx, sy, visible := ctx.MapToScreen(x+dx, y+dy)
			if !visible {
				continue
			}
			if r.console != nil {
				r.cell256(buf, sx, sy, colors)
			} else {
				buf.SetWithBg(sx, sy, ' ', bg, bg)
			}
		}
	}
	if sx, sy, visible := ctx.MapToScreen(x+sq.Width/2, y+sq.Height/2); visible {
		r.mark(buf, sx, sy, '0'+rune(sq.Digits[p]), visual.RgbSquareDigit, visual.ConBlack, terminal.AttrBold)
	}
}

// fired is how far a kind's blocks glow toward its glow color: wholly as its line
// fires, fading over visual.SquareFireFlash
func fired(sq *component.SquareComponent, kind component.SquareKind) float64 {
	effect, spec := sq.Effects[kind], &component.SquareKinds[kind]
	since := spec.Cooldown - effect.Cooldown
	if effect.Cooldown <= 0 || since >= visual.SquareFireFlash {
		return 0
	}
	return 1 - float64(since)/float64(visual.SquareFireFlash)
}

// cell256 fills a cell with a kind's console background
func (r *SquareRenderer) cell256(buf *render.RenderBuffer, sx, sy int, colors *visual.SquareKindColors) {
	buf.SetWithBg(sx, sy, ' ', visual.RgbBlack, visual.RgbBlack)
	buf.SetBg256(sx, sy, r.console.Background(colors.Console))
}

// mark writes a glyph over a cell's background, in tc or a console's entry
func (r *SquareRenderer) mark(buf *render.RenderBuffer, sx, sy int, ch rune, tc color.RGB, console uint8, attr terminal.Attr) {
	if r.console != nil {
		buf.SetFgOnly(sx, sy, ch, color.RGB{R: console}, attr|terminal.AttrFg256)
		return
	}
	buf.SetFgOnly(sx, sy, ch, tc, attr)
}

// frame rings the block of the viewer's selected digit in the gap round it
func (r *SquareRenderer) frame(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, digit uint8) {
	place := slices.Index(sq.Digits[:], digit)
	if digit == 0 || place < 0 {
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

// legend stands each kind under a column of the square, in kind order: its name, lit
// while a line holds three of the kind, centered over a meter as wide as a block
func (r *SquareRenderer) legend(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent) {
	_, h := sq.Size()
	y := sq.Y + h + 1
	var aligned [component.SquareKindCount]bool
	for _, kind := range sq.Lines {
		if kind != 0 {
			aligned[kind-1] = true
		}
	}
	for kind := range min(len(component.SquareKinds), component.SquareSide) {
		spec, colors := &component.SquareKinds[kind], &visual.SquareKinds[kind]
		tc, console, attr := visual.RgbSquareIdle, visual.ConGray, terminal.AttrNone
		if aligned[kind] {
			tc, console, attr = colors.Block, colors.Console, terminal.AttrBold
		}
		x, _ := sq.Place(kind)
		for i := range len(spec.Name) {
			if sx, sy, visible := ctx.MapToScreen(x+(sq.Width-len(spec.Name))/2+i, y); visible && y < ctx.MapHeight {
				r.mark(buf, sx, sy, rune(spec.Name[i]), tc, console, attr)
			}
		}
		if y+1 < ctx.MapHeight {
			r.meter(ctx, buf, x, y+1, sq.Width, spec, sq.Effects[kind], colors)
		}
	}
}

// meter fills a kind's bar of cells as its cooldown runs, full once it is ready, in
// its glow while its effect holds; an eighth of a cell at a time in true color, a
// whole cell on a console
func (r *SquareRenderer) meter(ctx render.RenderContext, buf *render.RenderBuffer, x, y, cells int, spec *component.SquareKindSpec, effect component.SquareEffect, colors *visual.SquareKindColors) {
	share := 1.0
	if effect.Cooldown > 0 && spec.Cooldown > 0 {
		share = 1 - float64(effect.Cooldown)/float64(spec.Cooldown)
	}
	fill := colors.Block
	if effect.Active > 0 {
		fill = colors.Glow
	}
	track := color.Lerp(visual.RgbBackground, colors.Track, visual.SquareMeterTrack)
	eighths := int(share*float64(8*cells) + 0.5)
	for i := range cells {
		sx, sy, visible := ctx.MapToScreen(x+i, y)
		if !visible {
			continue
		}
		n := min(max(eighths-8*i, 0), 8)
		switch {
		case r.console != nil && n < 4:
			r.mark(buf, sx, sy, visual.SquareMeterEmpty256, visual.RgbSquareIdle, visual.ConGray, terminal.AttrNone)
		case r.console != nil:
			r.cell256(buf, sx, sy, colors)
		case n == 0:
			buf.SetWithBg(sx, sy, ' ', track, track)
		case n == 8:
			buf.SetWithBg(sx, sy, ' ', fill, fill)
		default:
			buf.SetWithBg(sx, sy, visual.SquareMeterEighths[n-1], fill, track)
		}
	}
}
