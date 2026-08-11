package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// lightningBoltRenderer defines the signature for mode-specific bolt rendering
// Called per lightning entity with accumulated path data
type lightningBoltRenderer func(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64)

// LightningRenderer draws transient energy beams using sub-pixel resolution
// Supports dual rendering paths: TrueColor (quadrant chars) and 256-color (half-blocks)
type LightningRenderer struct {
	gameCtx *engine.GameContext

	// Mode-specific renderer selected at construction
	renderLightning lightningBoltRenderer
}

// NewLightningRenderer creates a new lightning renderer with mode-appropriate rendering path
func NewLightningRenderer(ctx *engine.GameContext) *LightningRenderer {
	r := &LightningRenderer{
		gameCtx: ctx,
	}

	if r.gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderLightning = r.renderLightning256
	} else {
		r.renderLightning = r.renderLightningTrueColor
	}

	return r
}

// Render draws all active lightning bolts using the mode-appropriate renderer
func (r *LightningRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	lightnings := r.gameCtx.World.Components.Lightning
	if lightnings.CountEntities() == 0 {
		return
	}

	// Use MaskNone for fg-only rendering - prevents OcclusionDim from dimming underlying bg
	// Works for 256, if any issue, branch and switch 256 back to MaskTransient
	buf.SetWriteMask(visual.MaskNone)

	lightnings.Each(func(_ core.Entity, l *component.LightningComponent) bool {
		if l.Remaining <= 0 {
			return true
		}

		// Resolve origin position (map coords)
		originX, originY := l.OriginX, l.OriginY
		if l.OriginEntity != 0 {
			if pos, ok := r.gameCtx.World.Positions.GetPosition(l.OriginEntity); ok {
				originX, originY = pos.X, pos.Y
			}
		}

		// Resolve target position (map coords)
		targetX, targetY := l.TargetX, l.TargetY
		if l.TargetEntity != 0 {
			if pos, ok := r.gameCtx.World.Positions.GetPosition(l.TargetEntity); ok {
				targetX, targetY = pos.X, pos.Y
			}
		}

		// Deterministic path: seed combines PathSeed + AnimFrame
		// XOR with golden ratio constant ensures full avalanche on AnimFrame increment
		seed := l.PathSeed ^ (uint64(l.AnimFrame) * 0x9E3779B97F4A7C15)
		rng := vmath.NewFastRand(seed)

		// Generate fractal path in sub-pixel coordinates (2x resolution)
		// Shared between both rendering modes for consistent path shape
		points := r.generateFractalPath(originX, originY, targetX, targetY, rng)

		// Dispatch to mode-specific renderer
		r.renderLightning(ctx, buf, points, l.ColorType, parameter.LightningAlpha)
		return true
	})
}

// generateFractalPath creates a jagged lightning path using midpoint displacement
// Uses sine envelope for oval shape and coherent spine for natural flow
func (r *LightningRenderer) generateFractalPath(x1, y1, x2, y2 int, rng *vmath.FastRand) []struct{ X, Y int } {
	sx1, sy1 := x1*2, y1*2
	sx2, sy2 := x2*2, y2*2

	dx := sx2 - sx1
	dy := sy2 - sy1

	dxF := float64(dx)
	dyF := float64(dy)
	dist := vmath.MagnitudeF(dxF, dyF)
	if dist < 1.0 {
		return []struct{ X, Y int }{{sx1, sy1}, {sx2, sy2}}
	}

	// Segment count: ~1 per 10 sub-pixels, capped min and max segments
	segments := min(max(int(dist/10.0), 4), 32)

	// Normalized perpendicular: (-dy/dist, dx/dist)
	perpX := -dyF / dist
	perpY := dxF / dist

	// === Two-octave jitter ===
	// Octave 1: Coherent spine offset (single random value for whole path)
	// Creates gentle arc, prevents "straight bundle" appearance
	spineRand := rng.Next()
	spineOffset := float64(int64(spineRand>>32))/(1<<31) - 1.0
	spine := spineOffset * 4.0 // Max 4 sub-pixel spine curve

	// Octave 2: Per-segment detail jitter
	detailMagnitude := 6.0 // Max 6 sub-pixel detail

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{sx1, sy1})

	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)

		// Base point on line
		bx := float64(sx1) + dxF*t
		by := float64(sy1) + dyF*t

		// === Sine envelope: sin(t * π) ===
		// Maps t ∈ [0,1] to envelope ∈ [0,1], max at t=0.5
		envelope := vmath.SinF(t * vmath.TwoPi / 2.0)
		if envelope < 0 {
			envelope = -envelope // Ensure positive (shouldn't happen in [0, 0.5] but safety)
		}

		// Spine contribution: coherent arc, modulated by envelope
		// Parabolic envelope for spine: 4*t*(1-t), peaks at 0.5
		spineEnvelope := 4.0 * t * (1.0 - t)
		spineJitter := spine * spineEnvelope

		// Floor envelope to prevent static endpoints
		envelopeFloor := 0.15
		if envelope < envelopeFloor {
			envelope = envelopeFloor
		}
		if spineEnvelope < envelopeFloor {
			spineEnvelope = envelopeFloor
		}

		// Detail contribution: random per-segment, modulated by envelope
		detailRand := rng.Next()
		detailFrac := float64(int64(detailRand>>32))/(1<<31) - 1.0
		detailJitter := detailFrac * detailMagnitude * envelope

		// Combined jitter
		totalJitter := spineJitter + detailJitter

		// Apply perpendicular displacement
		jx := perpX * totalJitter
		jy := perpY * totalJitter
		point := vmath.PointAtF(bx+jx, by+jy)

		points = append(points, struct{ X, Y int }{
			point.X,
			point.Y,
		})
	}

	points = append(points, struct{ X, Y int }{sx2, sy2})
	return points
}

// renderLightningTrueColor draws lightning using quadrant block characters with screen blending
// Provides full 2x2 sub-pixel resolution with smooth color gradients
func (r *LightningRenderer) renderLightningTrueColor(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64) {

	c := visual.LightningTrueColorLUT[colorType][0]

	// Accumulate quadrant hits per cell
	// Key: packed (x,y), Value: quadrant bitmap
	cellHits := make(map[uint64]uint8)

	for i := range len(points) - 1 {
		r.traceSubPixelLineQuadrant(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Render accumulated quadrants with screen blend foreground
	for key, bits := range cellHits {
		// Unpack cell coordinates from map key
		mapX := int(int64(key >> 32))
		mapY := int(int64(key & 0xFFFFFFFF))

		// Transform to screen with visibility check
		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		// Get quadrant character from bitmap
		char := visual.QuadrantChars[bits]
		if char == ' ' {
			continue
		}

		// Screen blend foreground only - background untouched for theme preservation
		buf.Set(screenX, screenY, char, c, visual.RgbBlack, render.BlendScreenFg, alpha, terminal.AttrNone)
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
		key := uint64(cx)<<32 | uint64(cy)
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
	paletteIdx := visual.Lightning256ColorLUT[colorType]

	// Accumulate vertical half hits per cell
	// Key: packed (x,y), Value: half bitmap (bit0=top, bit1=bottom)
	cellHits := make(map[uint64]uint8)

	for i := range len(points) - 1 {
		r.traceSubPixelLineHalf(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Render accumulated half-blocks with foreground-only write
	for key, bits := range cellHits {
		// Unpack cell coordinates from map key
		mapX := int(int64(key >> 32))
		mapY := int(int64(key & 0xFFFFFFFF))

		// Transform to screen with visibility check
		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		// Get half-block character from bitmap
		char := visual.Half256Chars[bits]
		if char == ' ' {
			continue
		}

		// SetFgOnly: write character and foreground color, preserve existing background
		// This allows finalize() to set theme background on untouched cells
		// Fg.R stores palette index when AttrFg256 is set
		buf.SetFgOnly(screenX, screenY, char, color.RGB{R: paletteIdx}, terminal.AttrFg256)
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
		key := uint64(cx)<<32 | uint64(cy)
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
