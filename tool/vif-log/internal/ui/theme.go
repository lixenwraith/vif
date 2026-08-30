package ui

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif-log/internal/logfile"
)

// Theme extends the tui theme with log-specific colors.
type Theme struct {
	tui.Theme
	Accent  color.RGB
	Accent2 color.RGB
	Level   [logfile.LevelCount]color.RGB
	KeyFg   color.RGB
	NumFg   color.RGB
	StrFg   color.RGB
	SnapFg  color.RGB
}

// DefaultTheme is the built-in theme.
var DefaultTheme = Theme{
	Theme: tui.Theme{
		Bg: color.Gunmetal, Fg: color.RGB{R: 192, G: 202, B: 245},
		FocusBg: color.DarkSlate, CursorBg: color.RGB{R: 45, G: 50, B: 80},
		Selected: color.RGB{R: 158, G: 206, B: 106}, Unselected: color.RGB{R: 86, G: 95, B: 137},
		Partial: color.RGB{R: 125, G: 207, B: 255}, Error: color.RGB{R: 247, G: 118, B: 142},
		Warning: color.RGB{R: 224, G: 175, B: 104}, Border: color.RGB{R: 59, G: 66, B: 97},
		HeaderBg: color.RGB{R: 22, G: 22, B: 30}, HeaderFg: color.RGB{R: 192, G: 202, B: 245},
		StatusFg: color.RGB{R: 140, G: 152, B: 200}, HintFg: color.RGB{R: 86, G: 95, B: 137},
		InputBg: color.RGB{R: 31, G: 32, B: 46},
	},
	Accent:  color.RGB{R: 122, G: 162, B: 247},
	Accent2: color.RGB{R: 187, G: 154, B: 247},
	Level: [logfile.LevelCount]color.RGB{
		logfile.LevelTrace: {R: 100, G: 108, B: 140},
		logfile.LevelDebug: {R: 125, G: 207, B: 255},
		logfile.LevelInfo:  {R: 158, G: 206, B: 106},
		logfile.LevelWarn:  {R: 224, G: 175, B: 104},
		logfile.LevelError: {R: 247, G: 118, B: 142},
		logfile.LevelProc:  {R: 187, G: 154, B: 247},
		logfile.LevelBad:   {R: 255, G: 100, B: 100},
	},
	KeyFg:  color.RGB{R: 140, G: 152, B: 200},
	NumFg:  color.RGB{R: 224, G: 175, B: 104},
	StrFg:  color.RGB{R: 158, G: 206, B: 106},
	SnapFg: color.RGB{R: 125, G: 207, B: 255},
}

// subPalette gives ad-hoc subsystem tags stable colors without a lookup table.
var subPalette = []color.RGB{
	{R: 122, G: 162, B: 247}, {R: 158, G: 206, B: 106}, {R: 224, G: 175, B: 104},
	{R: 187, G: 154, B: 247}, {R: 125, G: 207, B: 255}, {R: 247, G: 118, B: 142},
	{R: 180, G: 220, B: 200}, {R: 220, G: 200, B: 140},
}

// SubColor returns a stable color for a subsystem tag.
func (t *Theme) SubColor(s string) color.RGB {
	if s == "" {
		return t.StatusFg
	}
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return subPalette[h%uint32(len(subPalette))]
}

// SrcColor returns a stable color for a source id, drawn from the same palette
// so gutter marks and subsystem tags share a visual language.
func (t *Theme) SrcColor(id int) color.RGB {
	return subPalette[uint32(id)%uint32(len(subPalette))]
}
