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

// Equal returns true if colors match
func (c RGB) Equal(other RGB) bool {
	return c.R == other.R && c.G == other.G && c.B == other.B
}

// Color cube values for 6x6x6 palette (indices 16-231)
// Levels: 0, 95, 135, 175, 215, 255
var cubeValues = [6]uint8{0, 95, 135, 175, 215, 255}

// cubeIndex maps 0-255 to nearest cube index 0-5
// Pre-computed at init time
var cubeIndex [256]uint8

// grayscaleStart is the first grayscale index (232-255 = 24 shades)
const grayscaleStart = 232

// rgb256LUT is a full lookup table for RGB → 256-color index
// 256 * 256 * 256 = 16MB, computed at init
// Access: rgb256LUT[r][g][b]
var rgb256LUT [256][256][256]uint8

func init() {
	// Build cube index lookup (which cube level is nearest for each 0-255 value)
	for i := 0; i < 256; i++ {
		best := 0
		bestDist := abs(i - int(cubeValues[0]))
		for j := 1; j < 6; j++ {
			d := abs(i - int(cubeValues[j]))
			if d < bestDist {
				bestDist = d
				best = j
			}
		}
		cubeIndex[i] = uint8(best)
	}

	// Build full RGB → 256 lookup table
	for r := 0; r < 256; r++ {
		for g := 0; g < 256; g++ {
			for b := 0; b < 256; b++ {
				rgb256LUT[r][g][b] = computeRGB256(uint8(r), uint8(g), uint8(b))
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// computeRGB256 finds the nearest 256-color palette index for an RGB value
// Used during init to populate the LUT
func computeRGB256(r, g, b uint8) uint8 {
	// Check if grayscale is a better match (when r ≈ g ≈ b)
	// Grayscale ramp: 232-255 maps to luminance 8, 18, 28, ..., 238
	gray := int(r+g+b) / 3
	maxDiff := max(abs(int(r)-gray), abs(int(g)-gray), abs(int(b)-gray))

	if maxDiff < 10 {
		// Close to grayscale, check grayscale ramp
		// Grayscale index = 232 + (gray - 8) / 10, clamped
		if gray < 4 {
			// Pure black is index 16 (cube 0,0,0) or 232 (first gray)
			// Check which is closer
			return 16
		}
		if gray > 243 {
			// Pure white is index 231 (cube 5,5,5) or 255 (last gray)
			return 231
		}
		grayIdx := uint8(232 + (gray-8)/10)
		if grayIdx > 255 {
			grayIdx = 255
		}

		// Compare grayscale match vs color cube match
		grayLevel := 8 + int(grayIdx-232)*10
		grayDist := abs(int(r)-grayLevel) + abs(int(g)-grayLevel) + abs(int(b)-grayLevel)

		cubeR := cubeIndex[r]
		cubeG := cubeIndex[g]
		cubeB := cubeIndex[b]
		cubeDist := abs(int(r)-int(cubeValues[cubeR])) +
			abs(int(g)-int(cubeValues[cubeG])) +
			abs(int(b)-int(cubeValues[cubeB]))

		if grayDist < cubeDist {
			return grayIdx
		}
	}

	// Use color cube
	return 16 + 36*cubeIndex[r] + 6*cubeIndex[g] + cubeIndex[b]
}

// RGBTo256 converts RGB to nearest 256-color palette index
// O(1) lookup via pre-computed table
func RGBTo256(c RGB) uint8 {
	return rgb256LUT[c.R][c.G][c.B]
}

// DetectColorMode determines terminal color capability from environment
func DetectColorMode() ColorMode {
	// 1. Check COLORTERM (highest priority, set by modern terminals)
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		return ColorModeTrueColor
	}

	// 2. Check terminal-specific env vars
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return ColorModeTrueColor
	}
	if os.Getenv("KONSOLE_VERSION") != "" {
		return ColorModeTrueColor
	}
	if os.Getenv("ITERM_SESSION_ID") != "" {
		return ColorModeTrueColor
	}
	if os.Getenv("ALACRITTY_WINDOW_ID") != "" {
		return ColorModeTrueColor
	}
	if os.Getenv("ALACRITTY_LOG") != "" {
		return ColorModeTrueColor
	}
	if os.Getenv("WEZTERM_PANE") != "" {
		return ColorModeTrueColor
	}

	// 3. Check TERM for known true color terminals
	term := os.Getenv("TERM")
	termLower := strings.ToLower(term)
	if strings.Contains(termLower, "truecolor") ||
		strings.Contains(termLower, "24bit") ||
		strings.Contains(termLower, "direct") {
		return ColorModeTrueColor
	}

	// 4. Default to 256-color
	return ColorMode256
}
