package renderer

import (
	"math"
	"time"

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

// ShieldStyle is how one Paint lights a field: its kind's look, the colour and
// palette it wears, its glow, the blend a peer's takes, and whether it is the local
// cursor's own, the lens every other field lights over
type ShieldStyle struct {
	Look       *visual.ShieldLook
	GlowPeriod time.Duration
	BlendScale float64
	Color      color.RGB
	GlowColor  color.RGB
	Palette256 uint8
	Local      bool
}

// shieldGeometry is a radius's field at the cell aspect, worked out once. A
// quadrant's cells, by |dy|*stride+|dx|, hold their alpha at full opacity and their
// glow terms; each row, the columns it draws and the ones its 256-colour rim covers.
type shieldGeometry struct {
	radius         float64
	invRadiusSq    float64
	reachX, reachY int // the box's half extents, the feather included
	stride         int
	used           uint64 // the lookup that last took it

	shape      []float64 // zero draws nothing: the centre, and past the feather
	edge       []float64 // the turning glow's edge factor, zero inside its threshold
	dirX, dirY []float64 // the cell's direction from the centre
	span       []int     // by |dy|: the widest |dx| drawn, -1 for none
	rimLo      []int     // by |dy|: the |dx| the rim covers, none when rimLo > rimHi
	rimHi      []int
}

// distSq is a cell's normalized squared distance from the centre, its row given in
// columns (vmath.Aspect.Circular)
func (g *shieldGeometry) distSq(dx int, dyCirc float64) float64 {
	return (float64(dx*dx) + dyCirc*dyCirc) * g.invRadiusSq
}

// build works the field out for a radius over a box, keeping the tables' storage
func (g *shieldGeometry) build(radius float64, reachX, reachY int, aspect vmath.Aspect) {
	g.radius, g.invRadiusSq = radius, 1/(radius*radius)
	g.reachX, g.reachY, g.stride = reachX, reachY, reachX+1
	cells, rows := g.stride*(reachY+1), reachY+1
	g.shape, g.edge = resized(g.shape, cells), resized(g.edge, cells)
	g.dirX, g.dirY = resized(g.dirX, cells), resized(g.dirY, cells)
	g.span, g.rimLo, g.rimHi = resized(g.span, rows), resized(g.rimLo, rows), resized(g.rimHi, rows)

	rimMin, rimMax := g.rimBand(aspect)
	edgeAlpha := math.Sqrt(visual.ShieldFeatherStart)
	for ady := range rows {
		dyCirc, nextCirc := aspect.Circular(float64(ady)), aspect.Circular(float64(ady+1))
		g.span[ady], g.rimLo[ady], g.rimHi[ady] = -1, g.stride, -1
		for adx := range g.stride {
			i := ady*g.stride + adx
			g.shape[i], g.edge[i] = 0, 0
			g.dirX[i], g.dirY[i] = vmath.Normalize2D(float64(adx), float64(ady))
			if adx == 0 && ady == 0 {
				continue
			}
			d := g.distSq(adx, dyCirc)
			if d <= visual.ShieldFeatherEnd {
				if d <= visual.ShieldFeatherStart {
					g.shape[i] = math.Sqrt(d)
				} else {
					g.shape[i] = edgeAlpha * (1 - (d-visual.ShieldFeatherStart)/visual.ShieldFeatherRange)
				}
				g.span[ady] = adx
				if d > visual.ShieldGlowEdgeThreshold {
					g.edge[i] = (d - visual.ShieldGlowEdgeThreshold) / (1 - visual.ShieldGlowEdgeThreshold)
				}
			}
			// A cell whose outward neighbour leaves the rim's disc joins the band, so the
			// rim of any radius closes into a ring; a row's rim stays one run
			if d <= rimMax && (d >= rimMin || g.distSq(adx+1, dyCirc) > rimMax || g.distSq(adx, nextCirc) > rimMax) {
				g.rimLo[ady] = min(g.rimLo[ady], adx)
				g.rimHi[ady] = adx
			}
		}
	}

	// A rim whose top is its axis cell alone lowers it onto the row beneath, so the
	// ring is flat there instead of carrying a lone cell; the centre row never takes it
	top := rows - 1
	for top > 0 && g.rimLo[top] > g.rimHi[top] {
		top--
	}
	if top >= 2 && g.rimHi[top] == 0 {
		g.rimLo[top], g.rimHi[top] = g.stride, -1
		g.rimLo[top-1] = 0
	}
}

// rimBand fits the 256-colour rim to the cell grid: it spans from the outermost drawn
// cell on each axis inward, starting before Shield256RimStart when a shield is too
// small for that band to hold a whole row or column
func (g *shieldGeometry) rimBand(aspect vmath.Aspect) (float64, float64) {
	var edgeX, edgeY float64
	for n := g.reachX; n > 0 && edgeX == 0; n-- {
		if d := g.distSq(n, 0); d <= visual.ShieldFeatherEnd {
			edgeX = d
		}
	}
	for n := g.reachY; n > 0 && edgeY == 0; n-- {
		if d := g.distSq(0, aspect.Circular(float64(n))); d <= visual.ShieldFeatherEnd {
			edgeY = d
		}
	}
	return min(visual.Shield256RimStart, edgeX, edgeY), max(edgeX, edgeY)
}

// resized is s holding n elements, reusing its storage when it can
func resized[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// ShieldPainter paints shield fields from per-radius tables. In TrueColor the local
// cursor's field is a lens: every cell it lights starts from black, as the field
// alone always did, and any other field crossing it adds its light there.
type ShieldPainter struct {
	trueColor bool
	aspect    vmath.Aspect
	geoms     [visual.ShieldGeometryCacheMax]shieldGeometry
	lookups   uint64

	lensSpan     []int // the local field's span by |dy|, copied so no rebuild moves it
	lensX, lensY int
	lensOn       bool

	// Per-Paint state
	style                 ShieldStyle
	alphaScale, glowScale float64
	glowActive            bool
	rotDirX, rotDirY      float64
	glow256               uint8
}

// NewShieldPainter creates a painter for a color mode and cell aspect
func NewShieldPainter(colorMode terminal.ColorMode, aspect vmath.Aspect) *ShieldPainter {
	return &ShieldPainter{trueColor: colorMode != terminal.ColorMode256, aspect: aspect}
}

// geometry is the field for a radius, built on first use and kept for the most
// recently used radii. The box stops at the map's size, which a cell's distance from
// a centre on the map never passes.
func (p *ShieldPainter) geometry(radius float64, ctx *render.RenderContext) *shieldGeometry {
	reachX := int(min(math.Ceil(radius*parameter.ShieldFeatherEndRatio)+1, float64(ctx.MapWidth)))
	reachY := int(min(math.Ceil(p.aspect.FromCircular(radius)*parameter.ShieldFeatherEndRatio)+1, float64(ctx.MapHeight)))
	p.lookups++
	oldest := &p.geoms[0]
	for i := range p.geoms {
		g := &p.geoms[i]
		if g.radius == radius && g.reachX == reachX && g.reachY == reachY {
			g.used = p.lookups
			return g
		}
		if g.used < oldest.used {
			oldest = g
		}
	}
	oldest.build(radius, reachX, reachY, p.aspect)
	oldest.used = p.lookups
	return oldest
}

// SetLens makes the field of a radius centred on (x, y) this frame's lens
func (p *ShieldPainter) SetLens(ctx render.RenderContext, radius float64, x, y int) {
	p.lensSpan = append(p.lensSpan[:0], p.geometry(radius, &ctx).span...)
	p.lensX, p.lensY, p.lensOn = x, y, true
}

// ClearLens leaves every field over the theme background
func (p *ShieldPainter) ClearLens() { p.lensOn = false }

// lensRow is the map columns of a row the lens covers, none when lo > hi
func (p *ShieldPainter) lensRow(mapY int) (int, int) {
	if p.style.Local {
		return math.MinInt, math.MaxInt
	}
	ady := mapY - p.lensY
	if ady < 0 {
		ady = -ady
	}
	if !p.lensOn || ady >= len(p.lensSpan) || p.lensSpan[ady] < 0 {
		return 1, 0
	}
	return p.lensX - p.lensSpan[ady], p.lensX + p.lensSpan[ady]
}

// Paint draws a field of a radius centred on (centerX, centerY) in map coordinates,
// over the visible map; a radius that is not positive draws nothing.
// Caller must set write mask.
func (p *ShieldPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, radius float64, style ShieldStyle) {
	if !(radius > 0) {
		return
	}
	g := p.geometry(radius, &ctx)
	visMinX, visMinY, visMaxX, visMaxY := ctx.VisibleMapBounds()
	x0, x1 := max(visMinX, centerX-g.reachX), min(visMaxX, centerX+g.reachX)
	y0, y1 := max(visMinY, centerY-g.reachY), min(visMaxY, centerY+g.reachY)
	if x0 > x1 || y0 > y1 {
		return
	}

	p.style = style
	p.alphaScale = style.Look.MaxOpacity * style.BlendScale
	p.glowScale = style.Look.GlowIntensity * style.BlendScale
	p.glowActive = style.GlowPeriod > 0
	if p.glowActive {
		period := int64(style.GlowPeriod)
		angle := float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi
		p.rotDirX, p.rotDirY = vmath.Cos(angle), vmath.Sin(angle)
	}

	// Every cell within the visible bounds maps to the screen by one offset
	offX := ctx.GameXOffset + ctx.MapOffsetX - ctx.CameraX
	offY := ctx.GameYOffset + ctx.MapOffsetY - ctx.CameraY
	if p.trueColor {
		p.paintTrueColor(buf, g, centerX, centerY, x0, x1, y0, y1, offX, offY)
		return
	}
	p.glow256 = color.RGBTo256(style.GlowColor)
	p.paint256(buf, g, centerX, centerY, x0, x1, y0, y1, offX, offY)
}

// paintTrueColor screens the field over each row's drawn span, left of the centre and
// then from it, so a half's columns share a sign and walk its row of the quadrant
func (p *ShieldPainter) paintTrueColor(buf *render.RenderBuffer, g *shieldGeometry, cx, cy, x0, x1, y0, y1, offX, offY int) {
	for mapY := y0; mapY <= y1; mapY++ {
		ady, qy := mapY-cy, p.rotDirY
		if ady < 0 {
			ady, qy = -ady, -qy
		}
		span := g.span[ady]
		if span < 0 {
			continue
		}
		first := 0
		if ady == 0 {
			first = 1 // the centre draws nothing
		}
		lensLo, lensHi := p.lensRow(mapY)
		row, screenY := ady*g.stride, mapY+offY
		p.fieldSpan(buf, g, row, max(x0, cx-span), min(x1, cx-1), cx, -1, screenY, offX, lensLo, lensHi, -p.rotDirX, qy)
		p.fieldSpan(buf, g, row, max(x0, cx+first), min(x1, cx+span), cx, 1, screenY, offX, lensLo, lensHi, p.rotDirX, qy)
	}
}

// fieldSpan screens one half-row, the lens's columns over black and the rest over the
// theme background where nothing has drawn, and lights the turning glow
func (p *ShieldPainter) fieldSpan(buf *render.RenderBuffer, g *shieldGeometry, row, lo, hi, cx, sign, screenY, offX, lensLo, lensHi int, qx, qy float64) {
	for mapX := lo; mapX <= hi; mapX++ {
		i := row + (mapX-cx)*sign
		base := visual.RgbBackground
		if mapX >= lensLo && mapX <= lensHi {
			base = visual.RgbBlack
		}
		screenX := mapX + offX
		buf.SetBgScreen(screenX, screenY, p.style.Color, base, g.shape[i]*p.alphaScale)
		if !p.glowActive {
			continue
		}
		if edge := g.edge[i]; edge > 0 {
			if dot := g.dirX[i]*qx + g.dirY[i]*qy; dot > 0 {
				buf.Set(screenX, screenY, 0, visual.RgbBlack, p.style.GlowColor, render.BlendSoftLight, dot*edge*p.glowScale, terminal.AttrNone)
			}
		}
	}
}

// paint256 draws each row's rim cells solid, as paintTrueColor walks its spans. A
// peer's rim is not dimmed: the console shows a dimmed rim as black, and the peer
// cursor already says whose it is.
func (p *ShieldPainter) paint256(buf *render.RenderBuffer, g *shieldGeometry, cx, cy, x0, x1, y0, y1, offX, offY int) {
	for mapY := y0; mapY <= y1; mapY++ {
		ady, qy := mapY-cy, p.rotDirY
		if ady < 0 {
			ady, qy = -ady, -qy
		}
		lo, hi := g.rimLo[ady], g.rimHi[ady]
		if lo > hi {
			continue
		}
		row, screenY := ady*g.stride, mapY+offY
		p.rimSpan(buf, g, row, max(x0, cx-hi), min(x1, cx-max(lo, 1)), cx, -1, screenY, offX, -p.rotDirX, qy)
		p.rimSpan(buf, g, row, max(x0, cx+lo), min(x1, cx+hi), cx, 1, screenY, offX, p.rotDirX, qy)
	}
}

// rimSpan draws one half-row's rim, lit in the glow colour where it faces the glow
func (p *ShieldPainter) rimSpan(buf *render.RenderBuffer, g *shieldGeometry, row, lo, hi, cx, sign, screenY, offX int, qx, qy float64) {
	for mapX := lo; mapX <= hi; mapX++ {
		i := row + (mapX-cx)*sign
		palette := p.style.Palette256
		if p.glowActive && g.dirX[i]*qx+g.dirY[i]*qy >= visual.Shield256GlowDot {
			palette = p.glow256
		}
		buf.SetBg256(mapX+offX, screenY, palette)
	}
}

// --- Cursor Shield Renderer ---

// emberTransitionState tracks per-entity ember-to-shield transition
type emberTransitionState struct {
	wasEmberActive  bool
	transitionStart time.Time
}

// ShieldRenderer renders every active shield at its own radius and colour, and the
// Wraith field round every Wraith
type ShieldRenderer struct {
	gameCtx *engine.GameContext
	painter *ShieldPainter

	// Per-entity ember transition tracking (keyed by entity)
	emberTransitions map[core.Entity]*emberTransitionState
}

// NewShieldRenderer creates the cursor shield system renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	cfg := gameCtx.World.Resources.Config
	return &ShieldRenderer{
		gameCtx:          gameCtx,
		painter:          NewShieldPainter(cfg.ColorMode, cfg.CellAspect),
		emberTransitions: make(map[core.Entity]*emberTransitionState),
	}
}

// Render lays the local field's lens, then draws the Wraith fields and every active
// shield, the local cursor's last
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskField)
	buf.SetCoarseBg(true)
	r.layLens(ctx)
	r.renderWraiths(ctx, buf)

	shields := r.gameCtx.World.Components.Shield
	if shields.CountEntities() == 0 {
		return
	}

	localEntity := r.gameCtx.World.Resources.Player.Entity
	var localShield *component.ShieldComponent

	shields.Each(func(shieldEntity core.Entity, shieldComp *component.ShieldComponent) bool {
		if shieldEntity == localEntity {
			localShield = shieldComp
			return true
		}
		r.renderShield(ctx, buf, shieldEntity, shieldComp, false)
		return true
	})
	if localShield != nil {
		r.renderShield(ctx, buf, localEntity, localShield, true)
	}
}

// layLens takes the local cursor's shield, when it draws this frame, as the lens. In
// 256 colours a field is a solid rim, with no base to darken.
func (r *ShieldRenderer) layLens(ctx render.RenderContext) {
	r.painter.ClearLens()
	world := r.gameCtx.World
	local := world.Resources.Player.Entity
	shield, ok := world.Components.Shield.GetPtr(local)
	if !r.painter.trueColor || !ok || !shield.Active || !(shield.Radius > 0) {
		return
	}
	if heat, ok := world.Components.Heat.GetPtr(local); ok && heat.EmberActive {
		return
	}
	if pos, ok := world.CursorCell(local); ok {
		r.painter.SetLens(ctx, shield.Radius, pos.X, pos.Y)
	}
}

// renderWraiths paints the Wraith field at the cell each Wraith holds, none for one
// off the map, subordinate to the live fields as a peer's is.
func (r *ShieldRenderer) renderWraiths(ctx render.RenderContext, buf *render.RenderBuffer) {
	look := &visual.WraithField
	style := ShieldStyle{
		Look: look, BlendScale: visual.PeerFieldBlend, Color: look.Color, GlowColor: look.GlowColor,
		Palette256: look.Palette256, GlowPeriod: look.GlowPeriod,
	}
	r.gameCtx.World.Components.Wraith.Each(func(_ core.Entity, w *component.WraithComponent) bool {
		if w.X >= 0 {
			r.painter.Paint(buf, ctx, w.X, w.Y, parameter.PlayerShieldRadius, style)
		}
		return true
	})
}

func (r *ShieldRenderer) renderShield(ctx render.RenderContext, buf *render.RenderBuffer, shieldEntity core.Entity, shieldComp *component.ShieldComponent, local bool) {
	if !shieldComp.Active {
		return
	}

	heatComp, hasHeat := r.gameCtx.World.Components.Heat.GetPtr(shieldEntity)
	emberActive := hasHeat && heatComp.EmberActive

	var transitionIntensity float64
	if local {
		transition := r.getOrCreateTransition(shieldEntity)
		transitionIntensity = r.updateTransition(transition, emberActive, ctx.GameTime)
	}

	if emberActive || !(shieldComp.Radius > 0) {
		return
	}

	// D-18 prediction supplies the local cell; every other owner uses the store.
	shieldPos, ok := r.gameCtx.World.CursorCell(shieldEntity)
	if !ok {
		return
	}

	look := &visual.ShieldLooks[shieldComp.Type]
	style := ShieldStyle{
		Look:       look,
		BlendScale: 1,
		Color:      look.Color,
		Palette256: look.Palette256,
		GlowColor:  look.GlowColor,
		GlowPeriod: look.GlowPeriod,
		Local:      local,
	}

	var lootLook *component.LootLook
	switch shieldComp.Type {
	case component.ShieldTypePlayer:
		if energy, ok := r.gameCtx.World.Components.Energy.GetPtr(shieldEntity); ok && energy.Current < 0 {
			style.Color = look.ColorAlt
			style.Palette256 = look.Palette256Alt
		}
		if boost, ok := r.gameCtx.World.Components.Boost.GetPtr(shieldEntity); ok && boost.Active {
			style.GlowPeriod = parameter.ShieldBoostRotationDuration
		}
		if !local {
			style.BlendScale = visual.PeerFieldBlend
		}

	case component.ShieldTypeLoot:
		if loot, ok := r.gameCtx.World.Components.Loot.GetPtr(shieldEntity); ok && loot.Look.Rune != 0 {
			lootLook = &loot.Look
			style.GlowColor = lootLook.Glow
		}
	}
	if shieldComp.Color != (color.RGB{}) {
		style.Color, style.Palette256 = shieldComp.Color, color.RGBTo256(shieldComp.Color)
	}

	r.painter.Paint(buf, ctx, shieldPos.X, shieldPos.Y, shieldComp.Radius, style)

	// Loot's rune draws with its halo, after the species layers, so no body hides a drop.
	if lootLook != nil {
		if screenX, screenY, visible := ctx.MapToScreen(shieldPos.X, shieldPos.Y); visible {
			buf.SetFgOnly(screenX, screenY, lootLook.Rune, lootLook.Color, terminal.AttrNone)
		}
	}

	if transitionIntensity > 0.001 {
		r.renderTransitionOverlay(buf, ctx, shieldPos.X, shieldPos.Y, shieldComp.Radius, transitionIntensity)
	}
}

// getOrCreateTransition returns existing or new transition state for entity
func (r *ShieldRenderer) getOrCreateTransition(entity core.Entity) *emberTransitionState {
	if t, ok := r.emberTransitions[entity]; ok {
		return t
	}
	t := &emberTransitionState{}
	r.emberTransitions[entity] = t
	return t
}

// updateTransition handles state machine and returns current overlay intensity [0,1]
func (r *ShieldRenderer) updateTransition(t *emberTransitionState, emberActive bool, now time.Time) float64 {
	// Ember reactivated - cancel any transition
	if emberActive {
		t.wasEmberActive = true
		t.transitionStart = time.Time{} // Zero value = no transition
		return 0
	}

	// Ember just ended - start transition
	if t.wasEmberActive && !emberActive {
		t.wasEmberActive = false
		t.transitionStart = now
	}

	// No active transition
	if t.transitionStart.IsZero() {
		return 0
	}

	// Calculate transition progress
	elapsed := now.Sub(t.transitionStart)
	if elapsed >= visual.EmberTransitionDuration {
		t.transitionStart = time.Time{} // Transition complete
		return 0
	}

	progress := float64(elapsed) / float64(visual.EmberTransitionDuration)
	return r.transitionEnvelope(progress)
}

// transitionEnvelope computes intensity for strobe-like fade
// Fast rise (10%) + slow fall (90%)
func (r *ShieldRenderer) transitionEnvelope(progress float64) float64 {
	rise := visual.EmberTransitionRiseRatio

	if progress < rise {
		// Fast rise: 0 → max in first 10%
		return (progress / rise) * visual.EmberTransitionMaxIntensity
	}

	// Slow fall: max → 0 in remaining 90%
	fallProgress := (progress - rise) / (1.0 - rise)
	return (1.0 - fallProgress) * visual.EmberTransitionMaxIntensity
}

// renderTransitionOverlay screens the ember's edge colour over the shield's field,
// stronger towards its edge, so the shield rises out of the ember that ended
func (r *ShieldRenderer) renderTransitionOverlay(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, radius float64, intensity float64) {
	g := r.painter.geometry(radius, &ctx)
	visMinX, visMinY, visMaxX, visMaxY := ctx.VisibleMapBounds()
	offX := ctx.GameXOffset + ctx.MapOffsetX - ctx.CameraX
	offY := ctx.GameYOffset + ctx.MapOffsetY - ctx.CameraY
	centre := visual.EmberTransitionCentreShare
	for mapY := max(visMinY, centerY-g.reachY); mapY <= min(visMaxY, centerY+g.reachY); mapY++ {
		dyCirc := r.painter.aspect.Circular(float64(mapY - centerY))
		for mapX := max(visMinX, centerX-g.reachX); mapX <= min(visMaxX, centerX+g.reachX); mapX++ {
			normDistSq := g.distSq(mapX-centerX, dyCirc)
			if normDistSq > visual.ShieldFeatherEnd {
				continue
			}
			radial := min(math.Sqrt(normDistSq), 1.0)
			buf.SetBgScreen(mapX+offX, mapY+offY, visual.RgbEmberEdgeLow, visual.RgbBackground, intensity*(centre+(1-centre)*radial))
		}
	}
}
