package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

var (
	duration = flag.Duration("duration", 20*time.Second, "Benchmark duration")
)

// Entity represents a visual object
type Entity struct {
	X, Y       float64
	DX, DY     float64
	Radius     float64
	Color      render.RGB
	RenderType int
	Phase      float64
}

// Terminal character aspect ratio correction
// Standard fonts are roughly 1:2 (width:height)
const aspectRatio = 2.1

func main() {
	flag.Parse()

	// Initialize Terminal
	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	// Clean exit handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Fini()
		os.Exit(0)
	}()

	w, h := term.Size()
	cells := make([]terminal.Cell, w*h)
	start := time.Now()

	// Create 3 distinct entities demonstrating different blend modes
	entities := []*Entity{
		// 1. "The Sun" - Additive blending, hot core
		{
			X: 20, Y: 10, DX: 0.8, DY: 0.4, Radius: 14,
			Color:      render.RGB{R: 255, G: 160, B: 60}, // Orange
			RenderType: 0,
		},
		// 2. "The Bubble" - SoftLight/Overlay, distinct rim
		{
			X: 60, Y: 20, DX: -0.6, DY: 0.7, Radius: 18,
			Color:      render.RGB{R: 60, G: 220, B: 255}, // Cyan
			RenderType: 1,
		},
		// 3. "The Pulse" - Screen blending, interference pattern
		{
			X: 40, Y: 30, DX: 0.4, DY: -0.5, Radius: 22,
			Color:      render.RGB{R: 200, G: 60, B: 255}, // Purple
			RenderType: 2,
		},
	}

	// Starfield background
	type Star struct {
		X, Y, Brightness float64
	}
	stars := make([]Star, 100)
	for i := range stars {
		stars[i] = Star{
			X:          rand.Float64() * float64(w),
			Y:          rand.Float64() * float64(h),
			Brightness: 0.2 + rand.Float64()*0.8,
		}
	}

	var frames int64
	var renderTime, flushTime time.Duration

	// Main Loop
	for time.Since(start) < *duration {
		frameStart := time.Now()
		currTime := time.Since(start).Seconds()

		// --- 1. Physics & Update ---
		w, h = term.Size()
		if len(cells) != w*h {
			cells = make([]terminal.Cell, w*h)
			// Re-distribute stars on resize
			for i := range stars {
				stars[i].X = rand.Float64() * float64(w)
				stars[i].Y = rand.Float64() * float64(h)
			}
		}

		for _, e := range entities {
			e.X += e.DX
			e.Y += e.DY
			e.Phase += 0.05

			// Bounce
			if e.X < e.Radius || e.X > float64(w)-e.Radius {
				e.DX = -e.DX
				e.X += e.DX
			}
			if e.Y < e.Radius/aspectRatio || e.Y > float64(h)-e.Radius/aspectRatio {
				e.DY = -e.DY
				e.Y += e.DY
			}
		}

		// --- 2. Render ---
		tRender := time.Now()

		// Base background: Deep space gradient (Dark Blue -> Black)
		// Iterating 1D array is faster than 2D loop
		for y := 0; y < h; y++ {
			// Precompute row factors
			rowOffset := y * w
			// Vertical gradient factor (0.0 to 1.0)
			gy := float64(y) / float64(h)

			// Base color: very dark blue fading to black
			baseR := uint8(5)
			baseG := uint8(5 + gy*10)
			baseB := uint8(15 + gy*20)

			for x := 0; x < w; x++ {
				cells[rowOffset+x] = terminal.Cell{
					Rune: ' ',
					Bg:   terminal.RGB{R: baseR, G: baseG, B: baseB},
				}
			}
		}

		// Render Stars (Additive simple)
		for _, s := range stars {
			sx, sy := int(s.X), int(s.Y)
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				idx := sy*w + sx
				bg := cells[idx].Bg

				// Twinkle
				brite := s.Brightness * (0.8 + 0.2*math.Sin(currTime*5.0+s.X))
				val := uint8(255 * brite)

				// Convert terminal.RGB -> render.RGB -> terminal.RGB
				bgRender := render.RGB{R: bg.R, G: bg.G, B: bg.B}
				res := render.Add(bgRender, render.RGB{R: val, G: val, B: val})
				cells[idx].Bg = terminal.RGB{R: res.R, G: res.G, B: res.B}
			}
		}

		// Render Entities (Shader passes)
		for _, e := range entities {
			// Bounding box optimization
			minX := int(e.X - e.Radius - 1)
			maxX := int(e.X + e.Radius + 1)
			minY := int(e.Y - e.Radius/aspectRatio - 1)
			maxY := int(e.Y + e.Radius/aspectRatio + 1)

			// Clamp to screen
			if minX < 0 {
				minX = 0
			}
			if maxX > w {
				maxX = w
			}
			if minY < 0 {
				minY = 0
			}
			if maxY > h {
				maxY = h
			}

			radSq := e.Radius * e.Radius

			for y := minY; y < maxY; y++ {
				// Aspect ratio corrected Y distance
				dy := (float64(y) - e.Y) * aspectRatio
				dySq := dy * dy
				rowOff := y * w

				for x := minX; x < maxX; x++ {
					dx := float64(x) - e.X
					distSq := dx*dx + dySq

					if distSq > radSq {
						continue
					}

					// Normalized distance (0.0 center -> 1.0 edge)
					dist := math.Sqrt(distSq)
					normDist := dist / e.Radius
					idx := rowOff + x
					bg := render.RGB{R: cells[idx].Bg.R, G: cells[idx].Bg.G, B: cells[idx].Bg.B}

					var finalColor render.RGB

					switch e.RenderType {
					case 0: // "The Sun" - Additive Core + Corona
						// Core: Hot white center
						core := math.Max(0, 1.0-normDist*2.0) // Sharp falloff
						// Corona: Soft glow
						corona := math.Pow(1.0-normDist, 2.0)

						// Turbulence
						noise := math.Sin(normDist*20.0-currTime*4.0) * 0.1

						val := render.Scale(e.Color, corona+noise)
						// Add white core
						if core > 0 {
							val = render.Add(val, render.Scale(render.RGB{R: 255, G: 255, B: 255}, core))
						}
						finalColor = render.Add(bg, val)

					case 1: // "The Bubble" - Overlay Body + SoftLight Rim
						// Rim lighting (strong at edges)
						rim := math.Pow(normDist, 3.0)
						// Internal volume
						body := math.Sqrt(1.0 - normDist) // Sphere-like volume

						// Combine
						bubbleCol := render.Scale(e.Color, body*0.6+rim*0.8)

						// Overlay preserves background details (stars) behind the bubble
						finalColor = render.Overlay(bg, bubbleCol)

						// Add distinct rim highlight
						if normDist > 0.85 {
							finalColor = render.Add(finalColor, render.Scale(render.RGB{R: 200, G: 255, B: 255}, (normDist-0.85)*6.0))
						}

					case 2: // "The Pulse" - Screen Interference
						// Ripples
						ripple := math.Sin(normDist*30.0 - currTime*8.0)
						alpha := (1.0 - normDist) * (0.5 + 0.5*ripple)

						pulseCol := render.Scale(e.Color, alpha)

						// Screen blend makes it look like a hologram/light projection
						finalColor = render.Screen(bg, pulseCol)
					}

					cells[idx].Bg = terminal.RGB{R: finalColor.R, G: finalColor.G, B: finalColor.B}
				}
			}
		}
		renderTime += time.Since(tRender)

		// --- 3. Flush ---
		tFlush := time.Now()
		term.Flush(cells, w, h)
		flushTime += time.Since(tFlush)

		frames++

		// Cap FPS slightly to allow OS processing (~120 FPS limit)
		elapsed := time.Since(frameStart)
		if elapsed < 8*time.Millisecond {
			time.Sleep(8*time.Millisecond - elapsed)
		}
	}

	term.Fini()

	totalTime := time.Since(start)
	avgFPS := float64(frames) / totalTime.Seconds()

	fmt.Println("\n=== Visual Benchmark Results ===")
	fmt.Printf("Resolution:   %dx%d (%d cells)\n", w, h, w*h)
	fmt.Printf("Total Frames: %d\n", frames)
	fmt.Printf("Total Time:   %.2fs\n", totalTime.Seconds())
	fmt.Printf("Average FPS:  %.2f\n", avgFPS)
	fmt.Println("------------------------------")
	fmt.Printf("Avg Render:   %v\n", renderTime/time.Duration(frames))
	fmt.Printf("Avg Flush:    %v\n", flushTime/time.Duration(frames))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Total Alloc:  %d bytes\n", m.TotalAlloc)
	fmt.Printf("Mallocs:      %d\n", m.Mallocs)
}