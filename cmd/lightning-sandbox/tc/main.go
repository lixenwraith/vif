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
	ColorCore = terminal.RGB{R: 0, G: 200, B: 255}
	ColorHot  = terminal.RGB{R: 255, G: 255, B: 255}

	BlendMode  = render.BlendScreen
	MaxOpacity = 0.8

	VibrationInterval = 40 * time.Millisecond
	BoltDuration      = 2 * time.Second

	SegmentDensity = 4.0
	MinSegments    = 2
	JitterScale    = 0.15
)

// Custom blend mode: Screen on Fg only, leave Bg untouched
// opScreen(0x05) | flagFg(0x20) = 0x25
const BlendScreenFgOnly = render.BlendMode(0x25)

// ==========================================
// SUB-PIXEL RENDERING
// ==========================================

var quadrantChars = [16]rune{
	' ', // 0000
	'▘', // 0001 UL
	'▝', // 0010 UR
	'▀', // 0011 upper half
	'▖', // 0100 LL
	'▌', // 0101 left half
	'▞', // 0110 anti-diagonal
	'▛', // 0111
	'▗', // 1000 LR
	'▚', // 1001 diagonal
	'▐', // 1010 right half
	'▜', // 1011
	'▄', // 1100 lower half
	'▙', // 1101
	'▟', // 1110
	'█', // 1111 full
}

// ==========================================

func main() {
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	w, h := term.Size()
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, w, h)

	boltStart := time.Now()

	// Background test characters in all quadrants
	bgChars := []struct {
		x, y int
		c    rune
	}{
		// Top-left (original)
		{8, 6, '@'}, {9, 6, '#'}, {10, 6, '&'},
		// Top-right (fg-only)
		{w/2 + 8, 6, '@'}, {w/2 + 9, 6, '#'}, {w/2 + 10, 6, '&'},
		// Bottom-left (bg glow)
		{8, h/2 + 6, '@'}, {9, h/2 + 6, '#'}, {10, h/2 + 6, '&'},
		// Bottom-right (bg blend)
		{w/2 + 8, h/2 + 6, '@'}, {w/2 + 9, h/2 + 6, '#'}, {w/2 + 10, h/2 + 6, '&'},
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
		buf.Clear()

		// Draw background chars
		for _, bg := range bgChars {
			buf.Set(bg.x, bg.y, bg.c, terminal.RGB{100, 100, 100}, terminal.RGBBlack, render.BlendReplace, 1.0, 0)
		}

		elapsed := now.Sub(boltStart)
		if elapsed > BoltDuration {
			boltStart = now
			elapsed = 0
		}
		remaining := BoltDuration - elapsed

		lifeRatio := float64(remaining) / float64(BoltDuration)
		alpha := lifeRatio
		if alpha > MaxOpacity {
			alpha = MaxOpacity
		}
		color := render.Lerp(ColorCore, ColorHot, 1.0-lifeRatio)

		timeBucket := now.UnixMilli() / VibrationInterval.Milliseconds()

		halfW, halfH := w/2, h/2

		// ========================================
		// TOP-LEFT: ORIGINAL (Background cells)
		// ========================================
		{
			ox, oy := 3, 3
			tx, ty := halfW-5, halfH-3

			seed := int64(1)*31337 + timeBucket
			rng := rand.New(rand.NewSource(seed))
			points := generateFractalPath(ox, oy, tx, ty, rng)

			for i := 0; i < len(points)-1; i++ {
				p1, p2 := points[i], points[i+1]
				drawLineBg(buf, p1.X, p1.Y, p2.X, p2.Y, color, alpha)
			}
			drawText(buf, ox, oy-1, "ORIGINAL (bg cells)")
		}

		// ========================================
		// TOP-RIGHT: SUB-PIXEL FG-ONLY (no bg touch)
		// ========================================
		{
			ox, oy := halfW+3, 3
			tx, ty := w-5, halfH-3

			seed := int64(2)*31337 + timeBucket
			rng := rand.New(rand.NewSource(seed))
			points := generateFractalPathSubPixel(ox, oy, tx, ty, rng)

			drawSubPixelBoltFgOnly(buf, points, color, alpha)
			drawText(buf, ox, oy-1, "SUB-PIXEL (fg-only, no bg touch)")
		}

		// ========================================
		// BOTTOM-LEFT: SUB-PIXEL WITH BG GLOW
		// ========================================
		{
			ox, oy := 3, halfH+3
			tx, ty := halfW-5, h-5

			seed := int64(3)*31337 + timeBucket
			rng := rand.New(rand.NewSource(seed))
			points := generateFractalPathSubPixel(ox, oy, tx, ty, rng)

			drawSubPixelBoltWithGlow(buf, points, color, alpha)
			drawText(buf, ox, oy-1, "SUB-PIXEL (bg glow)")
		}

		// ========================================
		// BOTTOM-RIGHT: SUB-PIXEL WITH BG BLEND
		// ========================================
		{
			ox, oy := halfW+3, halfH+3
			tx, ty := w-5, h-5

			seed := int64(4)*31337 + timeBucket
			rng := rand.New(rand.NewSource(seed))
			points := generateFractalPathSubPixel(ox, oy, tx, ty, rng)

			drawSubPixelBoltWithBgBlend(buf, points, color, alpha)
			drawText(buf, ox, oy-1, "SUB-PIXEL (bg screen blend)")
		}

		// Debug footer
		debugStr := fmt.Sprintf("Time: %.2fs | Alpha: %.2f | Size: %dx%d | 'q' to exit", elapsed.Seconds(), alpha, w, h)
		drawText(buf, 2, h-1, debugStr)

		buf.FlushToTerminal(term)
		term.SetCursorVisible(false)
	}
}

// ==========================================
// ORIGINAL LIGHTNING (Background-based)
// ==========================================

func generateFractalPath(x1, y1, x2, y2 int, rng *rand.Rand) []struct{ X, Y int } {
	dx := x2 - x1
	dy := y2 - y1
	distSq := dx*dx + dy*dy

	segments := 2
	if distSq > 16 {
		dist := int(math.Sqrt(float64(distSq)))
		segments = int(float64(dist) / SegmentDensity)
		if segments < MinSegments {
			segments = MinSegments
		}
	}

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{x1, y1})

	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)
		bx := float64(x1) + float64(dx)*t
		by := float64(y1) + float64(dy)*t

		jitter := JitterScale * (rng.Float64() - 0.5)
		jx := -float64(dy) * jitter
		jy := float64(dx) * jitter

		points = append(points, struct{ X, Y int }{
			int(bx + jx),
			int(by + jy),
		})
	}

	points = append(points, struct{ X, Y int }{x2, y2})
	return points
}

func drawLineBg(buf *render.RenderBuffer, x0, y0, x1, y1 int, color terminal.RGB, alpha float64) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx - dy

	for {
		buf.Set(x0, y0, 0, terminal.RGBBlack, color, BlendMode, alpha, terminal.AttrNone)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// ==========================================
// SUB-PIXEL PATH GENERATION
// ==========================================

func generateFractalPathSubPixel(x1, y1, x2, y2 int, rng *rand.Rand) []struct{ X, Y int } {
	sx1, sy1 := x1*2, y1*2
	sx2, sy2 := x2*2, y2*2

	dx := sx2 - sx1
	dy := sy2 - sy1
	distSq := dx*dx + dy*dy

	segments := 4
	if distSq > 64 {
		dist := int(math.Sqrt(float64(distSq)))
		segments = int(float64(dist) / (SegmentDensity * 1.5))
		if segments < MinSegments*2 {
			segments = MinSegments * 2
		}
	}

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{sx1, sy1})

	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)
		bx := float64(sx1) + float64(dx)*t
		by := float64(sy1) + float64(dy)*t

		jitter := JitterScale * (rng.Float64() - 0.5)
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
// SUB-PIXEL DRAWING VARIANTS
// ==========================================

// drawSubPixelBoltFgOnly: Foreground only, background untouched (theme color shows through)
func drawSubPixelBoltFgOnly(buf *render.RenderBuffer, points []struct{ X, Y int }, color terminal.RGB, alpha float64) {
	cellHits := make(map[uint64]uint8)

	for i := 0; i < len(points)-1; i++ {
		traceSubPixelLine(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	for key, bits := range cellHits {
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))
		r := quadrantChars[bits]
		if r != ' ' {
			// Fg-only: screen blend foreground, bg completely untouched
			buf.Set(cx, cy, r, color, terminal.RGBBlack, BlendScreenFgOnly, alpha, terminal.AttrNone)
		}
	}
}

// drawSubPixelBoltWithGlow: Fg + soft background glow (dims with distance from center)
func drawSubPixelBoltWithGlow(buf *render.RenderBuffer, points []struct{ X, Y int }, color terminal.RGB, alpha float64) {
	cellHits := make(map[uint64]uint8)

	for i := 0; i < len(points)-1; i++ {
		traceSubPixelLine(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Pass 1: Background glow (dimmed color)
	glowColor := terminal.RGB{
		R: uint8(float64(color.R) * 0.3),
		G: uint8(float64(color.G) * 0.3),
		B: uint8(float64(color.B) * 0.3),
	}

	for key, bits := range cellHits {
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))
		if bits != 0 {
			// Set background glow - use Max to not darken existing bg
			buf.Set(cx, cy, 0, terminal.RGBBlack, glowColor, render.BlendMax, alpha, terminal.AttrNone)
		}
	}

	// Pass 2: Foreground characters on top
	for key, bits := range cellHits {
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))
		r := quadrantChars[bits]
		if r != ' ' {
			buf.Set(cx, cy, r, color, terminal.RGBBlack, BlendScreenFgOnly, alpha, terminal.AttrNone)
		}
	}
}

// drawSubPixelBoltWithBgBlend: Both fg and bg get screen blended
func drawSubPixelBoltWithBgBlend(buf *render.RenderBuffer, points []struct{ X, Y int }, color terminal.RGB, alpha float64) {
	cellHits := make(map[uint64]uint8)

	for i := 0; i < len(points)-1; i++ {
		traceSubPixelLine(cellHits, points[i].X, points[i].Y, points[i+1].X, points[i+1].Y)
	}

	// Dimmer bg color for subtle fill
	bgColor := terminal.RGB{
		R: uint8(float64(color.R) * 0.4),
		G: uint8(float64(color.G) * 0.4),
		B: uint8(float64(color.B) * 0.4),
	}

	for key, bits := range cellHits {
		cx := int(int32(key >> 32))
		cy := int(int32(key & 0xFFFFFFFF))
		r := quadrantChars[bits]
		if r != ' ' {
			// Screen blend both fg and bg
			buf.Set(cx, cy, r, color, bgColor, render.BlendScreen, alpha, terminal.AttrNone)
		}
	}
}

// ==========================================
// SUB-PIXEL LINE TRACING
// ==========================================

func traceSubPixelLine(hits map[uint64]uint8, sx0, sy0, sx1, sy1 int) {
	dx := abs(sx1 - sx0)
	dy := abs(sy1 - sy0)
	stepX, stepY := sign(sx1-sx0), sign(sy1-sy0)
	err := dx - dy

	for {
		cx, cy := sx0/2, sy0/2
		qx, qy := sx0&1, sy0&1
		quadrant := uint8(1 << (qy*2 + qx))

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

// ==========================================
// UTILS
// ==========================================

func drawText(buf *render.RenderBuffer, x, y int, text string) {
	for i, r := range text {
		buf.Set(x+i, y, r, terminal.RGB{200, 200, 200}, terminal.RGBBlack, render.BlendReplace, 1.0, 0)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func sign(x int) int {
	if x > 0 {
		return 1
	}
	if x < 0 {
		return -1
	}
	return 0
}