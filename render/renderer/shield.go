package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ShieldStyle configures per-invocation shield rendering parameters
type ShieldStyle struct {
	// Halo appearance
	Color      terminal.RGB
	Palette256 uint8
	MaxOpacity int64 // Q32.32 peak alpha at ellipse edge

	// Precomputed ellipse containment
	InvRxSq    int64
	InvRySq    int64
	RadiusXInt int // Integer bounding box half-width
	RadiusYInt int // Integer bounding box half-height

	// 256-color rim threshold (Q32.32, cells below this are transparent)
	Threshold256 int64

	// Game-space position to skip (-1 = disabled)
	SkipX, SkipY int

	// Rotating glow overlay (disabled if Period == 0)
	GlowColor         terminal.RGB
	GlowEdgeThreshold int64         // Q32.32 distSq below which glow is suppressed
	GlowIntensity     int64         // Q32.32 peak glow alpha
	GlowPeriod        time.Duration // Full rotation duration (0 = disabled)
}

// shieldCellFunc renders a single cell within the shield ellipse
type shieldCellFunc func(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// ShieldPainter is a reusable shield halo renderer supporting TrueColor gradient
// and 256-color rim modes with optional rotating glow overlay
type ShieldPainter struct {
	renderCell shieldCellFunc

	// Per-Paint transient state
	style            *ShieldStyle
	glowActive       bool
	rotDirX, rotDirY int64
	cellDx, cellDy   int64
}

// NewShieldPainter creates a painter dispatching to the appropriate color mode
func NewShieldPainter(colorMode terminal.ColorMode) *ShieldPainter {
	p := &ShieldPainter{}
	if colorMode == terminal.ColorMode256 {
		p.renderCell = shieldCell256
	} else {
		p.renderCell = shieldCellTrueColor
	}
	return p
}

// Paint renders a shield halo centered at (centerX, centerY) in game-space
// Caller must set write mask before invocation
func (p *ShieldPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, style *ShieldStyle) {
	p.style = style

	p.glowActive = style.GlowPeriod > 0
	if p.glowActive {
		period := int64(style.GlowPeriod)
		phase := ctx.GameTime.UnixNano() % period
		angle := (phase * vmath.Scale) / period
		p.rotDirX = vmath.Cos(angle)
		p.rotDirY = vmath.Sin(angle)
	}

	startX := max(0, centerX-style.RadiusXInt)
	endX := min(ctx.GameWidth-1, centerX+style.RadiusXInt)
	startY := max(0, centerY-style.RadiusYInt)
	endY := min(ctx.GameHeight-1, centerY+style.RadiusYInt)

	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			if x == style.SkipX && y == style.SkipY {
				continue
			}

			dx := vmath.FromInt(x - centerX)
			dy := vmath.FromInt(y - centerY)
			normalizedDistSq := vmath.EllipseDistSq(dx, dy, style.InvRxSq, style.InvRySq)
			if normalizedDistSq > vmath.Scale {
				continue
			}

			p.cellDx = dx
			p.cellDy = dy
			p.renderCell(p, buf, ctx.GameXOffset+x, ctx.GameYOffset+y, normalizedDistSq)
		}
	}
}

// shieldCellTrueColor renders quadratic gradient with optional rotating glow
func shieldCellTrueColor(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	s := p.style

	alphaFixed := vmath.Mul(normalizedDistSq, s.MaxOpacity)
	buf.Set(screenX, screenY, 0, visual.RgbBlack, s.Color, render.BlendScreen, vmath.ToFloat(alphaFixed), terminal.AttrNone)

	if !p.glowActive || normalizedDistSq <= s.GlowEdgeThreshold {
		return
	}

	cellDirX, cellDirY := vmath.Normalize2D(p.cellDx, p.cellDy)
	dot := vmath.DotProduct(cellDirX, cellDirY, p.rotDirX, p.rotDirY)
	if dot <= 0 {
		return
	}

	edgeFactor := vmath.Div(normalizedDistSq-s.GlowEdgeThreshold, vmath.Scale-s.GlowEdgeThreshold)
	intensity := vmath.Mul(vmath.Mul(dot, edgeFactor), s.GlowIntensity)
	buf.Set(screenX, screenY, 0, visual.RgbBlack, s.GlowColor, render.BlendSoftLight, vmath.ToFloat(intensity), terminal.AttrNone)
}

// shieldCell256 renders discrete rim for 256-color terminals
func shieldCell256(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	if normalizedDistSq < p.style.Threshold256 {
		return
	}
	buf.SetBg256(screenX, screenY, p.style.Palette256)
}

// --- Cursor Shield Renderer (SystemRenderer) ---

// ShieldRenderer renders active player shields with dynamic energy-based coloring
type ShieldRenderer struct {
	gameCtx *engine.GameContext
	painter *ShieldPainter
}

// NewShieldRenderer creates the cursor shield system renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	return &ShieldRenderer{
		gameCtx: gameCtx,
		painter: NewShieldPainter(gameCtx.World.Resources.Render.ColorMode),
	}
}

// Render draws all active player shields
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskField)

	shieldEntities := r.gameCtx.World.Components.Shield.GetAllEntities()
	if len(shieldEntities) == 0 {
		return
	}

	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	for _, shieldEntity := range shieldEntities {
		shieldComp, ok := r.gameCtx.World.Components.Shield.GetComponent(shieldEntity)
		if !ok || !shieldComp.Active {
			continue
		}

		shieldPos, ok := r.gameCtx.World.Positions.GetPosition(shieldEntity)
		if !ok {
			continue
		}

		// Determine skip logic (only player shields skip the center to show cursor)
		skipX, skipY := -1, -1
		if shieldEntity == cursorEntity {
			skipX = shieldPos.X
			skipY = shieldPos.Y
		}

		// Construct style from component
		style := ShieldStyle{
			Color:             shieldComp.Color,
			Palette256:        shieldComp.Palette256,
			MaxOpacity:        vmath.FromFloat(shieldComp.MaxOpacity),
			InvRxSq:           shieldComp.InvRxSq,
			InvRySq:           shieldComp.InvRySq,
			RadiusXInt:        vmath.ToInt(shieldComp.RadiusX),
			RadiusYInt:        vmath.ToInt(shieldComp.RadiusY),
			Threshold256:      vmath.FromFloat(0.64), // Standard rim threshold (0.8^2)
			SkipX:             skipX,
			SkipY:             skipY,
			GlowColor:         shieldComp.GlowColor,
			GlowEdgeThreshold: vmath.FromFloat(0.36), // Standard glow threshold (0.6^2)
			GlowIntensity:     vmath.FromFloat(shieldComp.GlowIntensity),
			GlowPeriod:        shieldComp.GlowPeriod,
		}

		r.painter.Paint(buf, ctx, shieldPos.X, shieldPos.Y, &style)
	}
}