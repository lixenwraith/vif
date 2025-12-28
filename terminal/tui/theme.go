package tui

import "github.com/lixenwraith/vi-fighter/terminal"

// Theme defines semantic colors for TUI components
type Theme struct {
	Bg       terminal.RGB
	Fg       terminal.RGB
	FocusBg  terminal.RGB
	CursorBg terminal.RGB

	Selected   terminal.RGB
	Unselected terminal.RGB
	Error      terminal.RGB
	Warning    terminal.RGB

	Border   terminal.RGB
	HeaderBg terminal.RGB
	HeaderFg terminal.RGB
	HintFg   terminal.RGB

	// Tree/syntax
	DirFg    terminal.RGB
	FileFg   terminal.RGB
	SymbolFg terminal.RGB
}

// DefaultTheme provides reasonable defaults
var DefaultTheme = Theme{
	Bg:         terminal.RGB{R: 20, G: 20, B: 30},
	Fg:         terminal.RGB{R: 200, G: 200, B: 200},
	FocusBg:    terminal.RGB{R: 30, G: 35, B: 50},
	CursorBg:   terminal.RGB{R: 60, G: 80, B: 120},
	Selected:   terminal.RGB{R: 80, G: 200, B: 80},
	Unselected: terminal.RGB{R: 100, G: 100, B: 100},
	Error:      terminal.RGB{R: 255, G: 80, B: 80},
	Warning:    terminal.RGB{R: 255, G: 200, B: 100},
	Border:     terminal.RGB{R: 80, G: 80, B: 100},
	HeaderBg:   terminal.RGB{R: 40, G: 60, B: 90},
	HeaderFg:   terminal.RGB{R: 255, G: 255, B: 255},
	HintFg:     terminal.RGB{R: 150, G: 150, B: 170},
	DirFg:      terminal.RGB{R: 100, G: 180, B: 255},
	FileFg:     terminal.RGB{R: 200, G: 200, B: 200},
	SymbolFg:   terminal.RGB{R: 180, G: 140, B: 255},
}