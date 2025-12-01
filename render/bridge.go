package render

import "github.com/gdamore/tcell/v2"

// TcellToRGB converts tcell.Color to RGB
// Treats ColorDefault as the standard background color
func TcellToRGB(c tcell.Color) RGB {
	if c == tcell.ColorDefault {
		// RgbBackground values (Tokyo Night) - hardcoded to avoid import cycle
		return RGB{26, 27, 38}
	}
	r, g, b := c.RGB()
	return RGB{uint8(r), uint8(g), uint8(b)}
}

// RGBToTcell converts RGB to tcell.Color
func RGBToTcell(rgb RGB) tcell.Color {
	return tcell.NewRGBColor(int32(rgb.R), int32(rgb.G), int32(rgb.B))
}