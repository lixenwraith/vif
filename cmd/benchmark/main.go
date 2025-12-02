package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

var (
	duration = flag.Duration("duration", 10*time.Second, "Benchmark duration")
	pattern  = flag.String("pattern", "xor", "Pattern: xor|static")
)

func main() {
	flag.Parse()

	// Initialize Terminal
	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Fini()
		os.Exit(0)
	}()

	w, h := term.Size()
	cells := make([]terminal.Cell, w*h)

	// Stats
	var frames int64
	var flushTotal time.Duration
	start := time.Now()

	// Pre-calculate colors to avoid math in loop
	// Simple XOR pattern is fast to generate

	for time.Since(start) < *duration {
		frameStart := time.Now()

		// 1. Generation Phase (Keep this extremely simple to test Terminal throughput)
		if *pattern == "xor" {
			offset := int(frames)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					idx := y*w + x
					val := (x + y + offset)
					cells[idx] = terminal.Cell{
						Rune:  '█',
						Fg:    terminal.RGB{R: uint8(val), G: uint8(val >> 1), B: uint8(255 - val)},
						Bg:    terminal.RGB{R: 0, G: 0, B: 0},
						Attrs: terminal.AttrNone,
					}
				}
			}
		} else {
			// Static: Change top-left pixel only to test diff skip
			cells[0].Fg.R = uint8(frames)
		}

		// 2. Flush Phase (Measure this separately)
		t0 := time.Now()
		term.Flush(cells, w, h)
		flushTotal += time.Since(t0)

		frames++

		// Cap at ~1000 FPS to prevent pure spin loop if too fast
		if time.Since(frameStart) < time.Millisecond {
			time.Sleep(time.Millisecond)
		}
	}

	elapsed := time.Since(start)

	term.Fini()

	fmt.Printf("Benchmark Results:\n")
	fmt.Printf("  Resolution:   %dx%d (%d cells)\n", w, h, w*h)
	fmt.Printf("  Total Frames: %d\n", frames)
	fmt.Printf("  Total Time:   %v\n", elapsed)
	fmt.Printf("  Avg FPS:      %.2f\n", float64(frames)/elapsed.Seconds())
	fmt.Printf("  Avg Flush:    %v\n", flushTotal/time.Duration(frames))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("  Total Alloc:  %d bytes\n", m.TotalAlloc)
	fmt.Printf("  Mallocs:      %d\n", m.Mallocs)
}