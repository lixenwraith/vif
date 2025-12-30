// FILE: cmd/lightning-sandbox-256/main.go
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// ==========================================
// TUNING VARIABLES
// ==========================================

var (
	VibrationInterval = 40 * time.Millisecond
	BoltDuration      = 2 * time.Second
	JitterScale       = 0.15
)

// ==========================================
// 256-COLOR CHARACTER SETS
// ==========================================

// half256Chars provides vertical half-cell resolution for 256-color mode
// Bitmap encoding: bit0=top, bit1=bottom
var half256Chars = [4]rune{
	' ',      // 00 - empty
	'\u2580', // 01 - top half only (▀)
	'\u2584', // 10 - bottom half only (▄)
	'\u2588', // 11 - both halves (█)
}

// density256Chars for reference - intensity variants
var density256Chars = [4]rune{
	'\u2591', // ░ - light shade (25%)
	'\u2592', // ▒ - medium shade (50%)
	'\u2593', // ▓ - dark shade (75%)
	'\u2588', // █ - full block (100%)
}

// Fixed 256-palette cyan color index
const paletteIdxCyan uint8 = 51

// ==========================================

func main() {
	// Force 256-color mode for testing
	term := terminal.New(terminal.ColorMode256)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	w, h := term.Size()
	buf := render.NewRenderBuffer(w, h)

	boltStart := time.Now()

	// Background test characters to verify fg-only rendering
	bgChars := []struct {
		x, y int
		c    rune
	}{
		{w/4 + 5, h / 3, '@'},
		{w/4 + 6, h / 3, '#'},
		{w/4 + 7, h / 3, '&'},
		{w / 2, h / 2, 'X'},
		{w/2 + 1, h / 2, 'Y'},
		{w/2 + 2, h / 2, 'Z'},
	}

	go func() {
		for {
			ev := term.PollEvent()
			if ev.Type == terminal.EventKey && (ev.Key == terminal.KeyEscape || ev.Rune == 'q') {
				term.Fini()
				os.Exit(0)
			}
		}
	}()

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for now := range ticker.C {
		w, h = term.Size()
		buf.Resize(w, h)
		buf.Clear()

		// Draw background chars with 256-color
		for _, bg := range bgChars {
			buf.SetFgOnly(bg.x, bg.y, bg.c, render.RGB{R: 245}, terminal.AttrFg256)
		}

		elapsed := now.Sub(boltStart)
		if elapsed > BoltDuration {
			boltStart = now
			elapsed = 0
		}
		remaining := BoltDuration - elapsed

		lifeRatio := float64(remaining) / float64(BoltDuration)
		alpha := lifeRatio
		if alpha > 0.8 {
			alpha = 0.8
		}

		timeBucket := now.UnixMilli() / VibrationInterval.Milliseconds()

		// ========================================
		// SINGLE LIGHTNING BOLT (256-color half-blocks)
		// ========================================
		{
			ox, oy := 5, 5
			tx, ty := w-10, h-5

			seed := int64(1)*31337 + timeBucket
			rng := rand.New(rand.NewSource(seed))
			points := generateFractalPathSubPixel(ox, oy, tx, ty, rng)

			// Skip if nearly faded
			if alpha >= 0.1 {
				cellHits := make(map[uint64]uint8)
				for i := 0; i < len(points)-1; i++ {
					traceSubPixelLineHalf(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
				}

				// Render with SetFgOnly - preserves background
				for key, bits := range cellHits {
					cx := int(int32(key >> 32))
					cy := int(int32(key & 0xFFFFFFFF))

					if cx < 0 || cx >= w || cy < 0 || cy >= h {
						continue
					}

					char := half256Chars[bits]
					if char == ' ' {
						continue
					}

					// SetFgOnly with 256-color palette index in R channel
					buf.SetFgOnly(cx, cy, char, render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
				}
			}

			drawText256(buf, ox, oy-2, "256-COLOR LIGHTNING (half-blocks: 219/220/223)")
		}

		// Character reference display
		drawText256(buf, 2, h-4, "Characters used:")
		buf.SetFgOnly(20, h-4, '\u2584', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		drawText256(buf, 21, h-4, "(bottom)")
		buf.SetFgOnly(30, h-4, '\u2580', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		drawText256(buf, 31, h-4, "(top)")
		buf.SetFgOnly(37, h-4, '\u2588', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		drawText256(buf, 38, h-4, "(full)")

		// Density chars reference
		drawText256(buf, 2, h-3, "Density chars:")
		buf.SetFgOnly(17, h-3, '\u2591', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		buf.SetFgOnly(18, h-3, '\u2592', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		buf.SetFgOnly(19, h-3, '\u2593', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		buf.SetFgOnly(20, h-3, '\u2588', render.RGB{R: paletteIdxCyan}, terminal.AttrFg256)
		drawText256(buf, 22, h-3, "(light->full)")

		// Debug footer
		debugStr := fmt.Sprintf("Time: %.2fs | Alpha: %.2f | Size: %dx%d | Mode: 256-color | 'q' to exit", elapsed.Seconds(), alpha, w, h)
		drawText256(buf, 2, h-1, debugStr)

		buf.FlushToTerminal(term)
		term.SetCursorVisible(false)
	}
}

// ==========================================
// SUB-PIXEL PATH GENERATION
// ==========================================

func generateFractalPathSubPixel(x1, y1, x2, y2 int, rng *rand.Rand) []struct{ X, Y int } {
	// Convert cell coordinates to sub-pixel (2x resolution)
	sx1, sy1 := x1*2, y1*2
	sx2, sy2 := x2*2, y2*2

	dx := sx2 - sx1
	dy := sy2 - sy1
	distSq := float64(dx*dx + dy*dy)
	dist := math.Sqrt(distSq)

	if dist < 1.0 {
		dist = 1.0
	}

	// ~1 segment every 6 sub-pixels
	segments := int(dist / 6.0)
	if segments < 4 {
		segments = 4
	}

	// Jitter scaled for sub-pixel coordinates
	jitterScale := JitterScale + (4.0 / dist)

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{sx1, sy1})

	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)

		bx := float64(sx1) + float64(dx)*t
		by := float64(sy1) + float64(dy)*t

		jitter := jitterScale * (rng.Float64() - 0.5)
		jx := -float64(dy) * jitter
		jy := float64(dx) * jitter

		points = append(points, struct{ X, Y int }{
			int(bx + jx),
			int(by + jy),
		})
	}

	points = append(points, struct{ X, Y int }{sx2, sy2})
	return points
}

// ==========================================
// SUB-PIXEL LINE TRACING (HALF-BLOCK)
// ==========================================

// traceSubPixelLineHalf traces a line in sub-pixel space, accumulating vertical half hits
// Half bitmap: bit0=top (sy%2==0), bit1=bottom (sy%2==1)
func traceSubPixelLineHalf(hits map[uint64]uint8, sx0, sy0, sx1, sy1 int) {
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

// ==========================================
// UTILS
// ==========================================

func drawText256(buf *render.RenderBuffer, x, y int, text string) {
	// Use 256-color light gray (250) for text
	for i, r := range text {
		buf.SetFgOnly(x+i, y, r, render.RGB{R: 250}, terminal.AttrFg256)
	}
}