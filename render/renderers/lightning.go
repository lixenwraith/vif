package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// quadrantChars provides 2x2 sub-cell resolution for TrueColor mode
// Bitmap encoding: bit0=UL, bit1=UR, bit2=LL, bit3=LR
// Layout: [UL][UR]
//
//	[LL][LR]
var quadrantChars = [16]rune{
	' ', // 0000 - empty
	'▘', // 0001 - upper-left
	'▝', // 0010 - upper-right
	'▀', // 0011 - upper half
	'▖', // 0100 - lower-left
	'▌', // 0101 - left half
	'▞', // 0110 - anti-diagonal
	'▛', // 0111 - UL + UR + LL
	'▗', // 1000 - lower-right
	'▚', // 1001 - diagonal
	'▐', // 1010 - right half
	'▜', // 1011 - UL + UR + LR
	'▄', // 1100 - lower half
	'▙', // 1101 - UL + LL + LR
	'▟', // 1110 - UR + LL + LR
	'█', // 1111 - full block
}

// TODO: test in naked TTY
// half256Chars provides vertical half-cell resolution for 256-color mode
// CP437 block characters compatible with naked TTY
// Uses Unicode block characters (equivalent to CP437 visuals)
// Bitmap encoding: bit0=top, bit1=bottom
var half256Chars = [4]rune{
	' ',      // 00 - empty
	'\u2580', // 01 - top half only (▀) - was 223
	'\u2584', // 10 - bottom half only (▄) - was 220
	'\u2588', // 11 - both halves (█) - was 219
}

// density256Chars provides intensity variants for future effects (trail, glow)
// Ordered from lowest to highest density
// Currently unused (future design) - renderLightning256 uses full density (219)
var density256Chars = [4]rune{
	'\u2591', // ░ - light shade (25%) - was 176
	'\u2592', // ▒ - medium shade (50%) - was 177
	'\u2593', // ▓ - dark shade (75%) - was 178
	'\u2588', // █ - full block (100%) - was 219
}

// horizontal256Chars provides horizontal half-cell characters
// Reserved for future horizontal sub-pixel support
var horizontal256Chars = [2]rune{
	'\u258C', // ▌ - left half - was 221
	'\u258E', // ▐ - right half - was 222
}

// COLOR LOOKUP TABLES
// TrueColor gradient endpoints per lightning color type
// Index by LightningColorType to get (core, hot) RGB pair
// Core = base color at end of life, Hot = bright color at full life
var lightningTrueColorLUT = [5][2]render.RGB{
	// Cyan: cool cyan core -> white hot center
	{render.RgbDrain, render.RgbEnergyBlinkWhite},
	// Red: dark red core -> bright red-white
	{{180, 40, 40}, {255, 200, 200}},
	// Gold: orange core -> bright yellow-white
	{{200, 150, 0}, {255, 255, 200}},
	// Green: dark green core -> bright green-white
	{{40, 150, 40}, {200, 255, 200}},
	// Purple: dark purple core -> bright purple-white
	{{120, 40, 180}, {220, 180, 255}},
}

// 256-color fixed palette indices per lightning color type
// Uses xterm 256-palette for consistent appearance without blending
var lightning256ColorLUT = [5]uint8{
	51,  // Cyan (0,5,5) - bright cyan
	196, // Red (5,0,0) - bright red
	220, // Gold (5,4,0) - yellow-orange
	46,  // Green (0,5,0) - bright green
	129, // Purple (3,0,5) - medium purple
}

// lightningBoltRenderer defines the signature for mode-specific bolt rendering
// Called per lightning entity with accumulated path data
type lightningBoltRenderer func(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64)

// LightningRenderer draws transient energy beams using sub-pixel resolution
// Supports dual rendering paths: TrueColor (quadrant chars) and 256-color (half-blocks)
type LightningRenderer struct {
	gameCtx        *engine.GameContext
	lightningStore *engine.Store[component.LightningComponent]

	// Mode-specific renderer selected at construction
	renderLightning lightningBoltRenderer

	rng *vmath.FastRand // Cached RNG
}

// NewLightningRenderer creates a new lightning renderer with mode-appropriate rendering path
func NewLightningRenderer(ctx *engine.GameContext) *LightningRenderer {
	r := &LightningRenderer{
		gameCtx:        ctx,
		lightningStore: engine.GetStore[component.LightningComponent](ctx.World),
		rng:            vmath.NewFastRand(1),
	}

	// Select rendering strategy based on terminal color capability
	cfg := engine.MustGetResource[*engine.RenderConfig](ctx.World.Resources)

	if cfg.ColorMode == terminal.ColorMode256 {
		r.renderLightning = r.renderLightning256
	} else {
		r.renderLightning = r.renderLightningTrueColor
	}

	return r
}

// Render draws all active lightning bolts using the mode-appropriate renderer
func (r *LightningRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.lightningStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	for _, e := range entities {
		l, ok := r.lightningStore.Get(e)
		if !ok || l.Remaining <= 0 {
			continue
		}

		// Generate fractal path in sub-pixel coordinates (2x resolution)
		// Shared between both rendering modes for consistent path shape
		points := r.generateFractalPath(l.OriginX, l.OriginY, l.TargetX, l.TargetY, r.rng)

		// Dispatch to mode-specific renderer
		r.renderLightning(ctx, buf, points, l.ColorType, constant.LightningAlpha)
	}
}

// generateFractalPath creates a jagged lightning path using midpoint displacement
// Uses sine envelope for oval shape and coherent spine for natural flow
func (r *LightningRenderer) generateFractalPath(x1, y1, x2, y2 int, rng *vmath.FastRand) []struct{ X, Y int } {
	sx1, sy1 := x1*2, y1*2
	sx2, sy2 := x2*2, y2*2

	dx := sx2 - sx1
	dy := sy2 - sy1

	dxFixed := vmath.FromInt(dx)
	dyFixed := vmath.FromInt(dy)

	distFixed := vmath.DistanceApprox(dxFixed, dyFixed)
	if distFixed < vmath.Scale {
		return []struct{ X, Y int }{{sx1, sy1}, {sx2, sy2}}
	}

	// Segment count: ~1 per 10 sub-pixels
	segments := vmath.ToInt(vmath.Div(distFixed, vmath.FromInt(10)))
	if segments < 4 {
		segments = 4
	}
	if segments > 32 {
		segments = 32 // Cap for very long lines
	}

	// Normalized perpendicular: (-dy/dist, dx/dist)
	perpXFixed := vmath.Div(-dyFixed, distFixed)
	perpYFixed := vmath.Div(dxFixed, distFixed)

	// === Two-octave jitter ===
	// Octave 1: Coherent spine offset (single random value for whole path)
	// Creates gentle arc, prevents "straight bundle" appearance
	spineRand := rng.Next()
	spineOffset := int32(spineRand>>16) - int32(vmath.Scale>>1)
	spineOffset <<= 1                  // [-1.0, 1.0) in Q16.16
	spineMagnitude := vmath.FromInt(4) // Max 4 sub-pixel spine curve
	spineFixed := vmath.Mul(spineOffset, spineMagnitude)

	// Octave 2: Per-segment detail jitter
	detailMagnitude := vmath.FromInt(6) // Max 6 sub-pixel detail

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{sx1, sy1})

	sx1Fixed := vmath.FromInt(sx1)
	sy1Fixed := vmath.FromInt(sy1)
	segmentsFixed := vmath.FromInt(segments)

	for i := 1; i < segments; i++ {
		tFixed := vmath.Div(vmath.FromInt(i), segmentsFixed)

		// Base point on line
		bxFixed := sx1Fixed + vmath.Mul(dxFixed, tFixed)
		byFixed := sy1Fixed + vmath.Mul(dyFixed, tFixed)

		// === Sine envelope: sin(t * π) ===
		// Maps t ∈ [0,1] to envelope ∈ [0,1], max at t=0.5
		// vmath.Sin expects angle where Scale = 2π, so t*Scale/2 = t*π
		envelopeAngle := tFixed >> 1 // t * 0.5 in angle space (t*π when Sin expects 0..Scale = 0..2π)
		envelope := vmath.Sin(envelopeAngle)
		if envelope < 0 {
			envelope = -envelope // Ensure positive (shouldn't happen in [0, 0.5] but safety)
		}

		// Spine contribution: coherent arc, modulated by envelope
		// Parabolic envelope for spine: 4*t*(1-t), peaks at 0.5
		oneMinusT := vmath.Scale - tFixed
		spineEnvelope := vmath.Mul(vmath.Mul(tFixed, oneMinusT), vmath.FromInt(4))
		spineJitter := vmath.Mul(spineFixed, spineEnvelope)

		// Detail contribution: random per-segment, modulated by envelope
		detailRand := rng.Next()
		detailFrac := int32(detailRand>>16) - int32(vmath.Scale>>1)
		detailFrac <<= 1
		detailJitter := vmath.Mul(vmath.Mul(detailFrac, detailMagnitude), envelope)

		// Combined jitter
		totalJitter := spineJitter + detailJitter

		// Apply perpendicular displacement
		jxFixed := vmath.Mul(perpXFixed, totalJitter)
		jyFixed := vmath.Mul(perpYFixed, totalJitter)

		points = append(points, struct{ X, Y int }{
			vmath.ToInt(bxFixed + jxFixed),
			vmath.ToInt(byFixed + jyFixed),
		})
	}

	points = append(points, struct{ X, Y int }{sx2, sy2})
	return points
}

// renderLightningTrueColor draws lightning using quadrant block characters with screen blending
// Provides full 2x2 sub-pixel resolution with smooth color gradients
func (r *LightningRenderer) renderLightningTrueColor(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64) {

	color := lightningTrueColorLUT[colorType][0]

	// Accumulate quadrant hits per cell
	// Key: packed (x,y), Value: quadrant bitmap
	cellHits := make(map[uint64]uint8)

	for i := 0; i < len(points)-1; i++ {
		r.traceSubPixelLineQuadrant(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Render accumulated quadrants with screen blend foreground
	for key, bits := range cellHits {
		// Unpack cell coordinates from map key
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))

		// Map to screen coordinates
		screenX := ctx.GameX + cx
		screenY := ctx.GameY + cy

		// Bounds check against game area
		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Get quadrant character from bitmap
		char := quadrantChars[bits]
		if char == ' ' {
			continue
		}

		// Screen blend foreground only - background untouched for theme preservation
		buf.Set(screenX, screenY, char, color, render.RGBBlack, render.BlendScreenFg, alpha, terminal.AttrNone)
	}
}

// traceSubPixelLineQuadrant traces a line in sub-pixel space, accumulating quadrant hits
// Uses Bresenham's algorithm at 2x resolution for smooth diagonal coverage
// Quadrant bitmap: bit0=UL, bit1=UR, bit2=LL, bit3=LR
func (r *LightningRenderer) traceSubPixelLineQuadrant(hits map[uint64]uint8, sx0, sy0, sx1, sy1 int) {
	dx := sx1 - sx0
	if dx < 0 {
		dx = -dx
	}
	dy := sy1 - sy0
	if dy < 0 {
		dy = -dy
	}

	stepX := -1
	if sx0 < sx1 {
		stepX = 1
	}
	stepY := -1
	if sy0 < sy1 {
		stepY = 1
	}

	err := dx - dy

	for {
		// Convert sub-pixel to cell + quadrant position
		cx, cy := sx0/2, sy0/2
		qx, qy := sx0&1, sy0&1

		// Quadrant bitmap encoding: row-major 2x2
		// qy=0: top row (UL=0, UR=1)
		// qy=1: bottom row (LL=2, LR=3)
		quadrant := uint8(1 << (qy*2 + qx))

		// Pack cell coordinates into 64-bit map key
		key := uint64(uint32(cx))<<32 | uint64(uint32(cy))
		hits[key] |= quadrant

		if sx0 == sx1 && sy0 == sy1 {
			break
		}

		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			sx0 += stepX
		}
		if e2 < dx {
			err += dx
			sy0 += stepY
		}
	}
}

// renderLightning256 draws lightning using CP437 half-block characters
// Provides vertical half-cell resolution with fixed palette color
// Uses SetFgOnly to preserve theme background during finalize
func (r *LightningRenderer) renderLightning256(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64) {

	// Skip rendering if nearly faded out
	// No alpha blending in 256-color mode - binary visibility threshold
	if alpha < 0.1 {
		return
	}

	// Get fixed palette color for this lightning type
	paletteIdx := lightning256ColorLUT[colorType]

	// Accumulate vertical half hits per cell
	// Key: packed (x,y), Value: half bitmap (bit0=top, bit1=bottom)
	cellHits := make(map[uint64]uint8)

	for i := 0; i < len(points)-1; i++ {
		r.traceSubPixelLineHalf(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Render accumulated half-blocks with foreground-only write
	for key, bits := range cellHits {
		// Unpack cell coordinates from map key
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))

		// Map to screen coordinates
		screenX := ctx.GameX + cx
		screenY := ctx.GameY + cy

		// Bounds check against game area
		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Get half-block character from bitmap
		char := half256Chars[bits]
		if char == ' ' {
			continue
		}

		// SetFgOnly: write character and foreground color, preserve existing background
		// This allows finalize() to set theme background on untouched cells
		// Fg.R stores palette index when AttrFg256 is set
		buf.SetFgOnly(screenX, screenY, char, render.RGB{R: paletteIdx}, terminal.AttrFg256)
	}
}

// traceSubPixelLineHalf traces a line in sub-pixel space, accumulating vertical half hits
// Uses Bresenham's algorithm at 2x resolution
// Half bitmap: bit0=top (sy%2==0), bit1=bottom (sy%2==1)
func (r *LightningRenderer) traceSubPixelLineHalf(hits map[uint64]uint8, sx0, sy0, sx1, sy1 int) {
	dx := sx1 - sx0
	if dx < 0 {
		dx = -dx
	}
	dy := sy1 - sy0
	if dy < 0 {
		dy = -dy
	}

	stepX := -1
	if sx0 < sx1 {
		stepX = 1
	}
	stepY := -1
	if sy0 < sy1 {
		stepY = 1
	}

	err := dx - dy

	for {
		// Convert sub-pixel to cell + vertical half position
		cx, cy := sx0/2, sy0/2
		halfY := sy0 & 1 // 0 = top half, 1 = bottom half

		// Half bitmap encoding: bit0=top, bit1=bottom
		halfBit := uint8(1 << halfY)

		// Pack cell coordinates into 64-bit map key
		key := uint64(uint32(cx))<<32 | uint64(uint32(cy))
		hits[key] |= halfBit

		if sx0 == sx1 && sy0 == sy1 {
			break
		}

		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			sx0 += stepX
		}
		if e2 < dx {
			err += dx
			sy0 += stepY
		}
	}
}