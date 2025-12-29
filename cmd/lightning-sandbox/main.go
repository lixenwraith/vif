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
// TUNING VARIABLES - PLAY WITH THESE
// ==========================================

// Visual Style
var (
	// Color Core (Start of lifetime / Cool)
	ColorCore = render.RGB{R: 0, G: 200, B: 255} // Cyan/Blue
	// Color Hot (End of lifetime / Hot)
	ColorHot = render.RGB{R: 255, G: 255, B: 255} // White

	// Blend Mode:
	// render.BlendAdd    = Glowing, accumulative (can blow out to pure white)
	// render.BlendScreen = Clean lightening, never exceeds white (Recommended)
	// render.BlendAlpha  = Standard transparency
	BlendMode = render.BlendScreen

	// Opacity
	MaxOpacity = 0.8 // 0.0 to 1.0

	// Thickness (Pseudo-thickness via jitter)
	DrawDoubleLine = true // Draw a second faint line for glow/thickness?
)

// Animation / Vibration
var (
	VibrationInterval = 40 * time.Millisecond // How often shape changes (lower = faster)
	BoltDuration      = 2 * time.Second       // How long the demo bolt lasts before resetting
)

// Geometry / Fractal
var (
	SegmentDensity = 4.0 // Higher = fewer segments (1 segment every N cells)
	MinSegments    = 2
	JitterScale    = 0.15 // Distortion amount relative to length (0.15 = 15%)
)

// ==========================================

func main() {
	// 1. Initialize Terminal
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	// 2. Setup Render Buffer
	w, h := term.Size()
	buf := render.NewRenderBuffer(w, h)

	// 3. Demo State
	// startTime := time.Now()
	boltStart := time.Now()

	// Static "Background" characters to test blending over content
	bgChars := []struct {
		x, y int
		c    rune
	}{
		{10, 10, '@'}, {11, 10, '#'}, {12, 10, '&'},
		{40, 15, 'M'}, {41, 15, 'M'}, {42, 15, 'M'},
	}

	// Input Loop (Non-blocking)
	go func() {
		for {
			ev := term.PollEvent()
			if ev.Type == terminal.EventKey && (ev.Key == terminal.KeyEscape || ev.Rune == 'q') {
				term.Fini()
				os.Exit(0)
			}
		}
	}()

	// 4. Render Loop
	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	for now := range ticker.C {
		buf.Clear()

		// --- Draw Background ---
		for _, bg := range bgChars {
			buf.Set(bg.x, bg.y, bg.c, render.RGB{100, 100, 100}, render.RGBBlack, render.BlendReplace, 1.0, 0)
		}

		// --- Logic ---
		// Reset bolt every BoltDuration seconds
		elapsed := now.Sub(boltStart)
		if elapsed > BoltDuration {
			boltStart = now
			elapsed = 0
		}

		remaining := BoltDuration - elapsed

		// Simulate Component State
		originX, originY := 5, 5
		targetX, targetY := 60, 20

		// --- RENDER LIGHTNING (The Logic to Copy) ---

		// 1. Calculate Opacity/Life
		lifeRatio := float64(remaining) / float64(BoltDuration)

		// Fade in/out curve (Trapezoid or Sine)
		// Simple linear fade out for now
		alpha := lifeRatio
		if alpha > MaxOpacity {
			alpha = MaxOpacity
		}

		// 2. Calculate Color
		color := render.Lerp(ColorCore, ColorHot, 1.0-lifeRatio) // Fade Hot -> Core

		// 3. Vibration Seed
		// Quantize time to create "frames" of static shape
		timeBucket := now.UnixMilli() / VibrationInterval.Milliseconds()
		seed := int64(1)*31337 + timeBucket
		rng := rand.New(rand.NewSource(seed))

		// 4. Generate Path
		points := generateFractalPath(originX, originY, targetX, targetY, rng)

		// 5. Draw
		for i := 0; i < len(points)-1; i++ {
			p1 := points[i]
			p2 := points[i+1]
			drawLine(buf, p1.X, p1.Y, p2.X, p2.Y, color, alpha)

			if DrawDoubleLine {
				// Faint glow/echo line
				drawLine(buf, p1.X, p1.Y+1, p2.X, p2.Y+1, color, alpha*0.5)
			}
		}

		// --- Debug Info ---
		debugStr := fmt.Sprintf("Time: %.2fs | Alpha: %.2f | Mode: %s", elapsed.Seconds(), alpha, blendModeName(BlendMode))
		drawText(buf, 2, h-2, debugStr)

		buf.FlushToTerminal(term)

		// Reset cursor to avoid flicker
		term.SetCursorVisible(false)
	}
}

// --- Lightning Logic Helpers ---

func generateFractalPath(x1, y1, x2, y2 int, rng *rand.Rand) []struct{ X, Y int } {
	dx := x2 - x1
	dy := y2 - y1
	distSq := dx*dx + dy*dy

	// Dynamic segment count
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

		// Perpendicular Jitter
		// We want a vector perpendicular to (dx, dy). That is (-dy, dx).
		// Jitter magnitude
		jitter := JitterScale * (rng.Float64() - 0.5) // -0.5 to 0.5

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

func drawLine(buf *render.RenderBuffer, x0, y0, x1, y1 int, color render.RGB, alpha float64) {
	// Bresenham's Algorithm
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy

	for {
		// Set pixel with blend mode
		// Using 0 as rune means "keep existing rune" (if logic allows) or space
		// In RenderBuffer.Set, mainRune=0 typically preserves rune if BlendMode flags handle it
		// But here we might want to just paint color.

		// Hack for sandbox: Using 0 rune with BlendScreen
		// We use a dummy rune ' ' if we want to draw *over* empty space,
		// but 0 if we want to tint existing char.
		// RenderBuffer behavior depends on implementation.
		// Assuming Set(..., 0, ...) preserves rune if it exists in buffer logic, or we pass 0.

		buf.Set(x0, y0, 0, render.RGBBlack, color, BlendMode, alpha, terminal.AttrNone)

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

// --- Utils ---

func drawText(buf *render.RenderBuffer, x, y int, text string) {
	for i, r := range text {
		buf.Set(x+i, y, r, render.RGB{255, 255, 255}, render.RGBBlack, render.BlendReplace, 1.0, 0)
	}
}

func blendModeName(m render.BlendMode) string {
	switch m {
	case render.BlendAdd:
		return "Add"
	case render.BlendScreen:
		return "Screen"
	case render.BlendSoftLight:
		return "SoftLight"
	case render.BlendAlpha:
		return "Alpha"
	default:
		return "Unknown"
	}
}