package render

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

// VGAPalette is the palette the Linux console boots with
var VGAPalette = consolePalette(8,
	0x000000, 0xaa0000, 0x00aa00, 0xaa5500, 0x0000aa, 0xaa00aa, 0x00aaaa, 0xaaaaaa,
	0x555555, 0xff5555, 0x55ff55, 0xffff55, 0x5555ff, 0xff55ff, 0x55ffff, 0xffffff)

// FreeBSDPalette is the palette FreeBSD's vt(4) boots with; its backgrounds take all sixteen
var FreeBSDPalette = consolePalette(16,
	0x000000, 0x7f0000, 0x007f00, 0xc4a000, 0x3366a3, 0x7f007f, 0x007f7f, 0xbfbfbf,
	0x2d3335, 0xff0000, 0x00ff00, 0xffff00, 0x729ece, 0xff00ff, 0x00ffff, 0xffffff)

func consolePalette(bgColors int, hex ...uint32) engine.ConsolePalette {
	p := engine.ConsolePalette{BgColors: bgColors}
	for i, h := range hex {
		p.Colors[i] = color.RGB{R: uint8(h >> 16), G: uint8(h >> 8), B: uint8(h)}
	}
	return p
}

// DetectConsolePalette returns the palette of the console this process draws on: the Linux
// console's live one, FreeBSD vt's default, and VGA where neither is known
func DetectConsolePalette() engine.ConsolePalette {
	switch runtime.GOOS {
	case "freebsd":
		return FreeBSDPalette
	case "linux":
		if p, ok := linuxConsolePalette(); ok {
			return p
		}
	}
	return VGAPalette
}

// linuxConsolePalette reads the vt module's palette, which setvtrgb rewrites
func linuxConsolePalette() (engine.ConsolePalette, bool) {
	p := engine.ConsolePalette{BgColors: 8}
	for ch, name := range [3]string{"default_red", "default_grn", "default_blu"} {
		raw, err := os.ReadFile("/sys/module/vt/parameters/" + name)
		if err != nil {
			return p, false
		}
		fields := strings.Split(strings.TrimSpace(string(raw)), ",")
		if len(fields) != len(p.Colors) {
			return p, false
		}
		for i, f := range fields {
			v, err := strconv.ParseUint(f, 10, 8)
			if err != nil {
				return p, false
			}
			switch ch {
			case 0:
				p.Colors[i].R = uint8(v)
			case 1:
				p.Colors[i].G = uint8(v)
			default:
				p.Colors[i].B = uint8(v)
			}
		}
	}
	return p, true
}

// Console quantizes a 256-color frame to a text console's sixteen colors. A console entry
// (0-15) passes through, an xterm cube or gray index keeps its hue family, RGB takes the entry
// that looks nearest, and text that would vanish into its background takes the nearest entry
// that shows.
type Console struct {
	colors   [16]color.RGB
	bgColors uint8
	lab      [16][3]float64
	lum      [16]float64
	fgLUT    [consoleLUTSize]uint8 // by lutKey
	bgLUT    [consoleLUTSize]uint8
	fgIndex  [256]uint8 // by xterm-256 index
	bgIndex  [256]uint8
	legible  [16][16]uint8 // [fg][bg]: fg, or the nearest entry that shows on bg
	readable [16][16]uint8 // the same for UI text, held to consoleUIContrast
}

const (
	consoleLUTBits = 5
	consoleLUTSize = 1 << (3 * consoleLUTBits)
	// consoleClashDE is the CIELAB distance under which text cannot be told from its background
	consoleClashDE = 10
	// consoleUIContrast is the WCAG ratio UI text is held to; VGA's gray on black just passes
	consoleUIContrast = 2.5
)

var (
	consoleMu sync.Mutex
	consoles  = map[engine.ConsolePalette]*Console{}
)

// ConsoleFor returns the quantizer for a palette, built once; the zero palette is VGA
func ConsoleFor(p engine.ConsolePalette) *Console {
	if p.BgColors == 0 {
		p = VGAPalette
	}
	consoleMu.Lock()
	defer consoleMu.Unlock()
	if c, ok := consoles[p]; ok {
		return c
	}
	c := newConsole(p)
	consoles[p] = c
	return c
}

func newConsole(p engine.ConsolePalette) *Console {
	c := &Console{colors: p.Colors, bgColors: uint8(min(max(p.BgColors, 1), 16))}
	for i, rgb := range c.colors {
		c.lab[i], c.lum[i] = labOf(rgb), luminance(rgb)
	}
	for key := range consoleLUTSize {
		l := labOf(color.RGB{R: uint8(key>>10)<<3 | 4, G: uint8(key>>5&31)<<3 | 4, B: uint8(key&31)<<3 | 4})
		c.fgLUT[key] = c.nearestLab(l, 16)
		c.bgLUT[key] = c.nearestLab(l, c.bgColors)
	}
	for i := range 256 {
		if i < 16 {
			c.fgIndex[i], c.bgIndex[i] = uint8(i), c.Background(uint8(i))
			continue
		}
		family, rgb := cubeFamily(uint8(i)), visual.Palette256RGB(uint8(i))
		c.fgIndex[i] = c.Nearest(rgb, family, family|8)
		c.bgIndex[i] = c.Background(c.fgIndex[i])
	}
	for bg := range uint8(16) {
		for fg := range uint8(16) {
			c.legible[fg][bg] = c.nearestWhere(fg, func(e uint8) bool { return labDistance(c.lab[e], c.lab[bg]) >= consoleClashDE })
			c.readable[fg][bg] = c.nearestWhere(fg, func(e uint8) bool { return c.Contrast(e, bg) >= consoleUIContrast })
		}
	}
	return c
}

// Nearest returns whichever of the given entries looks closest to rgb
func (c *Console) Nearest(rgb color.RGB, among ...uint8) uint8 {
	l := labOf(rgb)
	best, bestD := among[0], math.Inf(1)
	for _, i := range among {
		if d := labDistance(l, c.lab[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// Color returns an entry's RGB on this console
func (c *Console) Color(i uint8) color.RGB { return c.colors[i] }

// Contrast returns the WCAG contrast ratio of two entries
func (c *Console) Contrast(a, b uint8) float64 {
	hi, lo := max(c.lum[a], c.lum[b]), min(c.lum[a], c.lum[b])
	return (hi + 0.05) / (lo + 0.05)
}

// Background is the entry a background drawn in entry i shows: a bright one falls to its
// normal counterpart where backgrounds have only eight
func (c *Console) Background(i uint8) uint8 {
	if i >= c.bgColors {
		return i &^ 8
	}
	return i
}

// TextOn returns black or bright white, whichever reads better on background entry bg
func (c *Console) TextOn(bg uint8) uint8 {
	if c.Contrast(0, bg) > c.Contrast(15, bg) {
		return 0
	}
	return 15
}

// NearestBackground returns the background entry that looks closest to rgb
func (c *Console) NearestBackground(rgb color.RGB) uint8 { return c.bgLUT[lutKey(rgb)] }

// Family returns the two entries of rgb's hue, the one that looks darker on this palette first
func (c *Console) Family(rgb color.RGB) (dark, light uint8) {
	dark = cubeFamily(color.RGBTo256(rgb))
	light = dark | 8
	if c.lab[light][0] < c.lab[dark][0] {
		dark, light = light, dark
	}
	return dark, light
}

// fgEntry is the entry a cell's foreground takes; index says the color holds a palette index
func (c *Console) fgEntry(rgb color.RGB, index bool) uint8 {
	if index {
		return c.fgIndex[rgb.R]
	}
	return c.fgLUT[lutKey(rgb)]
}

func (c *Console) bgEntry(rgb color.RGB, index bool) uint8 {
	if index {
		return c.bgIndex[rgb.R]
	}
	return c.bgLUT[lutKey(rgb)]
}

func (c *Console) nearestLab(l [3]float64, n uint8) uint8 {
	var best uint8
	bestD := math.Inf(1)
	for i := range n {
		if d := labDistance(l, c.lab[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// nearestWhere returns fg if it passes ok, else the passing entry that looks closest to it
func (c *Console) nearestWhere(fg uint8, ok func(uint8) bool) uint8 {
	if ok(fg) {
		return fg
	}
	best, bestD := fg, math.Inf(1)
	for e := range uint8(16) {
		if d := labDistance(c.lab[fg], c.lab[e]); ok(e) && d < bestD {
			best, bestD = e, d
		}
	}
	return best
}

// quantize settles every cell on the entries the console shows, leaving no style for the
// console to recolor text by: reverse is swapped in and dim takes a darker entry
func (b *RenderBuffer) quantize() {
	c := b.console
	for i := range b.cells {
		cell := &b.cells[i]
		fg, fgIndex := cell.Fg, cell.Attrs&terminal.AttrFg256 != 0
		bg, bgIndex := cell.Bg, cell.Attrs&terminal.AttrBg256 != 0
		if cell.Attrs&terminal.AttrReverse != 0 {
			fg, fgIndex, bg, bgIndex = bg, bgIndex, fg, fgIndex
		}
		f, g := c.fgEntry(fg, fgIndex), c.bgEntry(bg, bgIndex)
		// The theme background is the console's black, where text reads best, whichever
		// entry is nearest (FreeBSD's dark gray)
		if !bgIndex && bg == visual.RgbBackground {
			g = 0
		}
		if cell.Attrs&terminal.AttrDim != 0 {
			f = dimEntry(f)
		}
		// A full block is its color, not text on a background
		if cell.Rune > ' ' && cell.Rune != '█' {
			if b.masks[i]&visual.MaskUI != 0 {
				f = c.readable[f][g]
			} else {
				f = c.legible[f][g]
			}
		}
		cell.Fg, cell.Bg = color.RGB{R: f}, color.RGB{R: g}
		cell.Attrs = terminal.AttrFg256 | terminal.AttrBg256
	}
}

// dimEntry is the darker entry dim text takes, where the Linux console would gray it
func dimEntry(i uint8) uint8 {
	switch {
	case i == 7:
		return 8
	case i > 8:
		return i - 8
	}
	return i
}

func lutKey(c color.RGB) int {
	return int(c.R>>3)<<10 | int(c.G>>3)<<5 | int(c.B>>3)
}

// cubeFamily is the console hue an xterm-256 index belongs to, by the rules FreeBSD's teken
// documents: a gray is dark or light; else, with the smallest component removed, the one left,
// or the larger of two by two steps, or else their mixture
func cubeFamily(i uint8) uint8 {
	switch {
	case i < 16:
		return i &^ 8
	case i >= 232:
		return grayFamily(visual.Palette256RGB(i).R)
	}
	steps := [6]int{0, 2, 3, 4, 5, 6} // cube level over 0x28, the xterm step
	n := int(i) - 16
	r, g, b := steps[n/36], steps[n/6%6], steps[n%6]
	if r == g && g == b {
		return grayFamily(visual.Palette256RGB(i).R)
	}
	lo := min(r, g, b)
	var first, second int // the two components left after removing the smallest
	var firstBit, secondBit uint8
	for _, comp := range [3]struct {
		v   int
		bit uint8
	}{{r - lo, 1}, {g - lo, 2}, {b - lo, 4}} {
		switch {
		case comp.v == 0:
		case firstBit == 0:
			first, firstBit = comp.v, comp.bit
		default:
			second, secondBit = comp.v, comp.bit
		}
	}
	switch {
	case secondBit == 0, first >= second+2:
		return firstBit
	case second >= first+2:
		return secondBit
	}
	return firstBit | secondBit
}

func grayFamily(v uint8) uint8 {
	if v < 128 {
		return 0
	}
	return 7
}

// labOf converts sRGB to CIELAB (D65), where Euclidean distance approximates how different
// two colors look
func labOf(c color.RGB) [3]float64 {
	r, g, b := srgbLinear[c.R], srgbLinear[c.G], srgbLinear[c.B]
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	fx, fy, fz := labF(x), labF(y), labF(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

func labF(t float64) float64 {
	if t > 216.0/24389 {
		return math.Cbrt(t)
	}
	return (24389.0/27*t + 16) / 116
}

func labDistance(a, b [3]float64) float64 {
	dl, da, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return math.Sqrt(dl*dl + da*da + db*db)
}

// luminance is WCAG relative luminance
func luminance(c color.RGB) float64 {
	return 0.2126*srgbLinear[c.R] + 0.7152*srgbLinear[c.G] + 0.0722*srgbLinear[c.B]
}

var srgbLinear = func() (t [256]float64) {
	for i := range t {
		v := float64(i) / 255
		if v <= 0.04045 {
			t[i] = v / 12.92
		} else {
			t[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return t
}()
