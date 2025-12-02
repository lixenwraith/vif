package terminal

import (
	"os"
	"strings"
)

// ColorMode indicates terminal color capability
type ColorMode uint8

const (
	ColorMode256       ColorMode = iota // xterm-256 palette
	ColorModeTrueColor                  // 24-bit RGB
)

// RGB represents a 24-bit color
type RGB struct {
	R, G, B uint8
}

// RGBBlack is the zero value black color
var RGBBlack = RGB{0, 0, 0}

// Color cube values for 6x6x6 palette (indices 16-231)
var cubeValues = [6]int{0, 95, 135, 175, 215, 255}

// cubeIndex maps 0-255 to nearest cube index 0-5
var cubeIndex [256]uint8

func init() {
	for i := 0; i < 256; i++ {
		best := 0
		bestDist := abs(i - cubeValues[0])
		for j := 1; j < 6; j++ {
			d := abs(i - cubeValues[j])
			if d < bestDist {
				bestDist = d
				best = j
			}
		}
		cubeIndex[i] = uint8(best)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// RGBTo256 converts RGB to nearest 256-color palette index
// Compute-based: ~20 ALU ops, cache-friendly
func RGBTo256(c RGB) uint8 {
	r, g, b := int(c.R), int(c.G), int(c.B)

	// Exact grayscale fast path
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 238 {
			return 231
		}
		return uint8(232 + (r-8)/10)
	}

	// Near-grayscale check (threshold 6)
	avg := (r + g + b) / 3
	dr, dg, db := abs(r-avg), abs(g-avg), abs(b-avg)
	if dr < 6 && dg < 6 && db < 6 {
		if avg < 8 {
			return 16
		}
		if avg > 238 {
			return 231
		}
		return uint8(232 + (avg-8)/10)
	}

	// 6x6x6 color cube
	return 16 + 36*cubeIndex[r] + 6*cubeIndex[g] + cubeIndex[b]
}

// DetectColorMode determines terminal color capability from environment
func DetectColorMode() ColorMode {
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		return ColorModeTrueColor
	}

	if os.Getenv("KITTY_WINDOW_ID") != "" ||
		os.Getenv("KONSOLE_VERSION") != "" ||
		os.Getenv("ITERM_SESSION_ID") != "" ||
		os.Getenv("ALACRITTY_WINDOW_ID") != "" ||
		os.Getenv("ALACRITTY_LOG") != "" ||
		os.Getenv("WEZTERM_PANE") != "" {
		return ColorModeTrueColor
	}

	term := os.Getenv("TERM")
	if strings.Contains(term, "truecolor") ||
		strings.Contains(term, "24bit") ||
		strings.Contains(term, "direct") {
		return ColorModeTrueColor
	}

	return ColorMode256
}
