package renderer

import (
	"math"
	"slices"
	"time"
	"unicode"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareRenderer draws a magic square over its cursor's shield and every field:
// what its kinds show round the shield, then each block in its kind's color, lit
// near white as its line fires, the viewer's selection and the legend
type SquareRenderer struct {
	gameCtx *engine.GameContext
	console *render.Console // nil draws true color
	orbGlow
}

func NewSquareRenderer(ctx *engine.GameContext) *SquareRenderer {
	cfg := ctx.World.Resources.Config
	r := &SquareRenderer{gameCtx: ctx, orbGlow: newOrbGlow(cfg.CellAspect)}
	if cfg.ColorMode == terminal.ColorMode256 {
		r.console = render.ConsoleFor(cfg.ConsolePalette)
	}
	return r
}

func (r *SquareRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	if world.Components.Square.CountEntities() == 0 {
		return
	}
	slot := world.Resources.Player.ViewSlot()
	world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
		buf.SetWriteMask(visual.MaskTransient)
		r.fields(ctx, buf, sq)
		buf.SetWriteMask(visual.MaskComposite)
		for p := range sq.Digits {
			r.block(ctx, buf, sq, p)
		}
		r.spell(ctx, buf, sq)
		if int(slot) < len(sq.Selected) {
			if place := slices.Index(sq.Digits[:], sq.Selected[slot]); sq.Selected[slot] != 0 && place >= 0 {
				r.frame(ctx, buf, sq, place, visual.RgbSquareFrame, visual.ConBrightWhite)
			}
		}
		burst := 0
		if heat, ok := world.Components.Heat.GetPtr(world.Resources.Player.Slot(slot)); ok {
			burst = heat.Burst
		}
		r.legend(ctx, buf, sq, burst)
		return true
	})
}

// fields draws what each kind shows round the shield, by what the kind fires: a
// boost's glow on the rim and a turning wind's streaks while they hold, and for a
// kind that strikes the flash of the part it landed in and its orb, at the middle
// of the side its line faces, bursting as it fires, or dim and turning round the
// outer orbit while no line holds the kind
func (r *SquareRenderer) fields(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent) {
	period := int64(parameter.SquareOrbIdlePeriod)
	turn := float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi
	for kind, spec := range component.SquareKinds {
		effect := sq.Effects[kind]
		if held := float64(effect.Active) / float64(max(spec.Duration, 1)); held > 0 {
			if spec.Boost {
				r.surge(ctx, buf, sq, kind, held)
			}
			if spec.Wind.Turn != 0 {
				r.streaks(ctx, buf, sq, kind, held, math.Sin(spec.Wind.Turn))
			}
		}
		if spec.Attack == component.CombatAttackNone {
			continue
		}
		look := &visual.SquareOrbs[kind]
		line := sq.Line(component.SquareKind(kind))
		if line < 0 {
			r.orb(ctx, buf, sq.OrbAt(turn, ctx.CellAspect, ctx.MapWidth, ctx.MapHeight), look, spec.Name, visual.SquareOrbIdle, 0)
			continue
		}
		since := spec.Cooldown - effect.Cooldown
		if effect.Struck && effect.Cooldown > 0 && since < visual.SquareStrikeFlash {
			r.flash(ctx, buf, sq, kind, line, 1-float64(since)/float64(visual.SquareStrikeFlash))
		}
		burst := 0.0
		if effect.Cooldown > 0 && since < parameter.OrbFlashDuration {
			burst = 1 - since.Seconds()/parameter.OrbFlashDuration.Seconds()
		}
		r.orb(ctx, buf, sq.Orb(line, ctx.CellAspect, ctx.MapWidth, ctx.MapHeight), look, spec.Name, 1, burst)
	}
}

// flash tints where a line's strike lands in its kind's glow, a share of the way
// from full to gone; a console draws none
func (r *SquareRenderer) flash(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, kind, line int, left float64) {
	if r.console != nil {
		return
	}
	faces := sq.Strikes(line, ctx.CellAspect)
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if sx, sy, visible := ctx.MapToScreen(x, y); visible && faces(x, y) {
				buf.SetBgScreen(sx, sy, visual.SquareKinds[kind].Glow, visual.RgbBackground, visual.SquareStrikeAlpha*left)
			}
		}
	}
}

// streaks draws a turning wind on the square's outer orbit, held a share of its
// time: runs of cells in the kind's glow, each fading back from a head that turns
// the way the wind carries bodies, all fading over the wind's last third; a console
// draws their heads in dots
func (r *SquareRenderer) streaks(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, kind int, held, sense float64) {
	period := int64(visual.SquareTempestTurn)
	turn := sense * float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi
	orbit := sq.ShieldRadius(ctx.CellAspect) + parameter.SquareOrbGap
	colors := &visual.SquareKinds[kind]
	for i := range visual.SquareTempestStreaks {
		head := turn + float64(i)*vmath.TwoPi/visual.SquareTempestStreaks
		last := vmath.Point{X: -1}
		for n := range visual.SquareTempestTail {
			at := sq.OrbAt(head-sense*float64(n)/orbit, ctx.CellAspect, ctx.MapWidth, ctx.MapHeight)
			sx, sy, visible := ctx.MapToScreen(at.X, at.Y)
			if at == last || !visible {
				continue
			}
			last = at
			if r.console == nil {
				buf.SetBgScreen(sx, sy, colors.Glow, visual.RgbBackground,
					visual.SquareTempestAlpha*min(3*held, 1)*(1-float64(n)/visual.SquareTempestTail))
			} else if n == 0 {
				buf.SetFgOnly(sx, sy, '·', color.RGB{R: colors.Console}, terminal.AttrFg256)
			}
		}
	}
}

// surge lights the shield's rim for a boosting kind, held a share of its boost's
// time: a band in the kind's glow inside the rim, heaviest in two lobes that turn
// round it, screened over the shield and fading over the boost's last third; a
// console draws the rim in dots
func (r *SquareRenderer) surge(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, kind int, held float64) {
	cx, cy := sq.Center()
	rim := sq.ShieldRadius(ctx.CellAspect)
	startX, endX, startY, endY, ok := effectBounds(ctx, cx, cy, rim+1, ctx.CellAspect.FromCircular(rim+1))
	if !ok {
		return
	}
	period := int64(visual.SquareSurgeTurn)
	lobeX, lobeY := math.Sincos(2 * float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi)
	colors := &visual.SquareKinds[kind]
	for y := startY; y <= endY; y++ {
		dy := ctx.CellAspect.Circular(float64(y - cy))
		for x := startX; x <= endX; x++ {
			// Columns inside the rim; past it the band ends within one
			dx := float64(x - cx)
			distSq := dx*dx + dy*dy
			depth := rim - math.Sqrt(distSq)
			shape := 1 - depth/visual.SquareSurgeWidth
			if depth < 0 {
				shape = 1 + depth
			}
			sx, sy, visible := ctx.MapToScreen(x, y)
			if shape <= 0 || distSq == 0 || !visible {
				continue
			}
			if r.console != nil {
				if depth >= 0 && depth < 1 {
					buf.SetFgOnly(sx, sy, '·', color.RGB{R: colors.Console}, terminal.AttrFg256)
				}
				continue
			}
			// The cosine of twice the cell's angle from the lobes', which puts two on the rim
			lobe := ((dx*dx-dy*dy)*lobeY + 2*dx*dy*lobeX) / distSq
			buf.SetBgScreen(sx, sy, colors.Glow, visual.RgbBackground, visual.SquareSurgeAlpha*min(3*held, 1)*shape*(0.6+0.4*lobe))
		}
	}
}

// orb draws one orb at a cell: its glyph in its core color under a corona of its
// other two, at a share of full light, or bursting as its line fires; a console
// draws its kind's initial in the kind's color
func (r *SquareRenderer) orb(ctx render.RenderContext, buf *render.RenderBuffer, at vmath.Point, look *visual.SquareOrb, name string, light, burst float64) {
	if !ctx.IsInViewport(at.X, at.Y) {
		return
	}
	screenX, screenY, _ := ctx.MapToScreen(at.X, at.Y)
	if r.console != nil {
		attr := terminal.AttrFg256
		if light >= 1 {
			attr |= terminal.AttrBold
		}
		buf.SetFgOnly(screenX, screenY, unicode.ToUpper(rune(name[0])), color.RGB{R: look.Console}, attr)
		return
	}
	if burst > 0 {
		r.renderBurst(ctx, buf, at.X, at.Y, look.Lobe, visual.CircleBlock, burst)
		return
	}
	angle := float64(ctx.GameTime.UnixMilli()%parameter.OrbCoronaPeriodMs) / float64(parameter.OrbCoronaPeriodMs) * vmath.TwoPi
	r.renderCorona(ctx, buf, at.X, at.Y, look.Lobe, look.Counter, vmath.Cos(angle), vmath.Sin(angle), light)
	buf.SetFgOnly(screenX, screenY, visual.CircleBlock, color.Lerp(visual.RgbBackground, look.Core, light), terminal.AttrNone)
}

// block draws the block at place p and its digit at its middle
func (r *SquareRenderer) block(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, p int) {
	kind := sq.KindAt(p)
	if int(kind) >= len(visual.SquareKinds) {
		return
	}
	colors := &visual.SquareKinds[kind]
	lit := fired(sq, kind)
	bg, console := color.Lerp(colors.Block, visual.RgbSquareFire, visual.SquareFireLight*lit), colors.Console
	if lit > 0.5 {
		console = visual.ConBrightWhite
	}
	x, y := sq.Place(p)
	for dy := range sq.Height {
		for dx := range sq.Width {
			sx, sy, visible := ctx.MapToScreen(x+dx, y+dy)
			if !visible {
				continue
			}
			if r.console != nil {
				r.cell256(buf, sx, sy, console)
			} else {
				buf.SetWithBg(sx, sy, ' ', bg, bg)
			}
		}
	}
	if sx, sy, visible := ctx.MapToScreen(x+sq.Width/2, y+sq.Height/2); visible {
		r.mark(buf, sx, sy, '0'+rune(sq.Digits[p]), visual.RgbSquareDigit, visual.ConBlack, terminal.AttrBold)
	}
}

// fired is how far a kind's blocks are lit by its fire: wholly as its line fires,
// fading over visual.SquareFireFlash
func fired(sq *component.SquareComponent, kind component.SquareKind) float64 {
	effect, spec := sq.Effects[kind], &component.SquareKinds[kind]
	since := spec.Cooldown - effect.Cooldown
	if effect.Cooldown <= 0 || since >= visual.SquareFireFlash {
		return 0
	}
	return 1 - float64(since)/float64(visual.SquareFireFlash)
}

// cell256 fills a cell with a console background
func (r *SquareRenderer) cell256(buf *render.RenderBuffer, sx, sy int, console uint8) {
	buf.SetWithBg(sx, sy, ' ', visual.RgbBlack, visual.RgbBlack)
	buf.SetBg256(sx, sy, r.console.Background(console))
}

// mark writes a glyph over a cell's background, in tc or a console's entry
func (r *SquareRenderer) mark(buf *render.RenderBuffer, sx, sy int, ch rune, tc color.RGB, console uint8, attr terminal.Attr) {
	if r.console != nil {
		buf.SetFgOnly(sx, sy, ch, color.RGB{R: console}, attr|terminal.AttrFg256)
		return
	}
	buf.SetFgOnly(sx, sy, ch, tc, attr)
}

// spell frames each place a lit spell holds, turning between its colors, and, as
// one is filled, tints what is within reach in the kind's glow, fading
func (r *SquareRenderer) spell(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent) {
	if sq.SpellLit {
		period := int64(visual.SquareSpellPulse)
		phase := float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi
		tc := color.Lerp(visual.SquareSpell.Block, visual.SquareSpell.Glow, (1+vmath.Sin(phase))/2)
		for p := range component.SquareBlocks {
			if sq.Spell&(1<<p) != 0 {
				r.frame(ctx, buf, sq, p, tc, visual.SquareSpell.Console)
			}
		}
		return
	}
	since := parameter.SquareSpellWait - sq.SpellTimer
	if sq.SpellCast == 0 || since >= visual.SquareStrikeFlash || r.console != nil {
		return
	}
	within, glow := sq.Within(ctx.CellAspect), visual.SquareKinds[sq.SpellCast-1].Glow
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if sx, sy, visible := ctx.MapToScreen(x, y); visible && within(x, y) {
				buf.SetBgScreen(sx, sy, glow, visual.RgbBackground, visual.SquareStrikeAlpha*(1-float64(since)/float64(visual.SquareStrikeFlash)))
			}
		}
	}
}

// frame rings the block at a place in the gap round it, doubled where a lit spell
// holds the place
func (r *SquareRenderer) frame(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, place int, tc color.RGB, console uint8) {
	edges := [6]rune{visual.BorderSingleTopLeft, visual.BorderSingleTopRight, visual.BorderSingleBottomLeft,
		visual.BorderSingleBottomRight, visual.BorderSingleHorizontal, visual.BorderSingleVertical}
	if sq.SpellLit && sq.Spell&(1<<place) != 0 {
		edges = [6]rune{visual.BorderDoubleTopLeft, visual.BorderDoubleTopRight, visual.BorderDoubleBottomLeft,
			visual.BorderDoubleBottomRight, visual.BorderDoubleHorizontal, visual.BorderDoubleVertical}
	}
	x, y := sq.Place(place)
	left, top, right, bottom := x-1, y-1, x+sq.Width, y+sq.Height
	for fy := top; fy <= bottom; fy++ {
		for fx := left; fx <= right; fx++ {
			var ch rune
			switch {
			case fy == top && fx == left:
				ch = edges[0]
			case fy == top && fx == right:
				ch = edges[1]
			case fy == bottom && fx == left:
				ch = edges[2]
			case fy == bottom && fx == right:
				ch = edges[3]
			case fy == top || fy == bottom:
				ch = edges[4]
			case fx == left || fx == right:
				ch = edges[5]
			default:
				continue
			}
			if sx, sy, visible := ctx.MapToScreen(fx, fy); visible {
				r.mark(buf, sx, sy, ch, tc, console, terminal.AttrBold)
			}
		}
	}
}

// legend stands each kind under a column of the square, in kind order, lit while a
// line holds three of the kind, and over its columns its dust meter, its spell's,
// filling to the spell's lighting and draining while it stays lit, and its viewer's
// burst meter, each lit when full
func (r *SquareRenderer) legend(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, burst int) {
	_, h := sq.Size()
	var aligned [component.SquareKindCount]bool
	for _, kind := range sq.Lines {
		if kind != 0 {
			aligned[kind-1] = true
		}
	}
	for kind := range min(len(component.SquareKinds), component.SquareSide) {
		x, _ := sq.Place(kind)
		r.entry(ctx, buf, x, sq.Y+h+1, sq.Width, &component.SquareKinds[kind], sq.Effects[kind], &visual.SquareKinds[kind], aligned[kind])
	}
	x, y := sq.Legend(0)
	r.entry(ctx, buf, x, y, sq.Width, &squareDust, component.SquareEffect{Cooldown: parameter.SquareDustFull - sq.Dust},
		&visual.SquareDust, sq.Dust >= parameter.SquareDustFull)
	spell, wait := squareSpell, component.SquareEffect{Cooldown: sq.SpellTimer}
	if sq.SpellLit {
		spell.Cooldown = parameter.SquareSpellHold
		wait = component.SquareEffect{Active: sq.SpellTimer, Cooldown: parameter.SquareSpellHold - sq.SpellTimer}
	}
	x, y = sq.Legend(component.SquareSide / 2)
	r.entry(ctx, buf, x, y, sq.Width, &spell, wait, &visual.SquareSpell, sq.SpellLit)
	x, y = sq.Legend(component.SquareSide - 1)
	r.entry(ctx, buf, x, y, sq.Width, &squareBurst, component.SquareEffect{Cooldown: time.Duration(parameter.BurstFull - burst)},
		&visual.SquareBurst, burst >= parameter.BurstFull)
}

// squareDust, squareSpell and squareBurst are the legend's entries over the square:
// a name, and in the place of a cooldown what its meter is full at
var (
	squareDust  = component.SquareKindSpec{Name: "dust", Cooldown: parameter.SquareDustFull}
	squareSpell = component.SquareKindSpec{Name: "spell", Cooldown: parameter.SquareSpellWait}
	squareBurst = component.SquareKindSpec{Name: "burst", Cooldown: parameter.BurstFull}
)

// entry draws one legend entry at a column: its name, in its color when lit,
// centered over a meter of cells
func (r *SquareRenderer) entry(ctx render.RenderContext, buf *render.RenderBuffer, x, y, cells int, spec *component.SquareKindSpec, effect component.SquareEffect, colors *visual.SquareKindColors, lit bool) {
	tc, console, attr := visual.RgbSquareIdle, visual.ConGray, terminal.AttrNone
	if lit {
		tc, console, attr = colors.Block, colors.Console, terminal.AttrBold
	}
	for i := range len(spec.Name) {
		if sx, sy, visible := ctx.MapToScreen(x+(cells-len(spec.Name))/2+i, y); visible && y >= 0 && y < ctx.MapHeight {
			r.mark(buf, sx, sy, rune(spec.Name[i]), tc, console, attr)
		}
	}
	if y+1 >= 0 && y+1 < ctx.MapHeight {
		r.meter(ctx, buf, x, y+1, cells, spec, effect, colors)
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
			r.cell256(buf, sx, sy, colors.Console)
		case n == 0:
			buf.SetWithBg(sx, sy, ' ', track, track)
		case n == 8:
			buf.SetWithBg(sx, sy, ' ', fill, fill)
		default:
			buf.SetWithBg(sx, sy, visual.SquareMeterEighths[n-1], fill, track)
		}
	}
}
