package main

import "github.com/lixenwraith/color"

// Theme is the map's colors by role
type Theme struct {
	Bg, Fg, FocusBg, CursorBg, InputBg color.RGB
	HeaderBg, HeaderFg, StatusFg       color.RGB
	HintFg, Border                     color.RGB

	// Selection states
	Selected, Unselected, Partial color.RGB
	Expanded                      color.RGB // Dependency-expanded files indicator
	Error, Warning                color.RGB

	// File tree
	DirFg, FileFg, SymbolFg color.RGB

	// Tag hierarchy colors
	GroupFg    color.RGB // Tag group names (#group)
	ModuleFg   color.RGB // Module names in tags
	TagFg      color.RGB // Individual tag names
	ExternalFg color.RGB // External package references
	AllTagFg   color.RGB // Files marked with #all tag

	// Viewer syntax highlighting
	SyntaxComment, SyntaxString, SyntaxKeyword color.RGB
	SyntaxType, SyntaxNumber                   color.RGB
	ViewerDefinition                           color.RGB
	ViewerFold                                 color.RGB
	ViewerMatch                                color.RGB
}

var DefaultTheme = Theme{
	Bg:       color.RGB{R: 20, G: 20, B: 30},
	Fg:       color.RGB{R: 200, G: 200, B: 200},
	FocusBg:  color.RGB{R: 30, G: 35, B: 45},
	CursorBg: color.RGB{R: 50, G: 50, B: 70},
	InputBg:  color.RGB{R: 30, G: 30, B: 50},
	HeaderBg: color.RGB{R: 40, G: 60, B: 90},
	HeaderFg: color.RGB{R: 255, G: 255, B: 255},
	StatusFg: color.RGB{R: 140, G: 140, B: 140},
	HintFg:   color.RGB{R: 100, G: 180, B: 200},
	Border:   color.RGB{R: 60, G: 80, B: 100},

	Selected:   color.RGB{R: 80, G: 200, B: 80},
	Unselected: color.RGB{R: 100, G: 100, B: 100},
	Partial:    color.RGB{R: 80, G: 160, B: 220},
	Expanded:   color.RGB{R: 180, G: 140, B: 220},
	Error:      color.RGB{R: 255, G: 80, B: 80},
	Warning:    color.RGB{R: 255, G: 80, B: 80},

	DirFg:    color.RGB{R: 130, G: 170, B: 220},
	FileFg:   color.RGB{R: 200, G: 200, B: 200},
	SymbolFg: color.RGB{R: 180, G: 220, B: 220},

	GroupFg:    color.RGB{R: 220, G: 180, B: 80},
	ModuleFg:   color.RGB{R: 80, G: 200, B: 80},
	TagFg:      color.RGB{R: 100, G: 200, B: 220},
	ExternalFg: color.RGB{R: 100, G: 140, B: 160},
	AllTagFg:   color.RGB{R: 255, G: 180, B: 100},

	SyntaxComment:    color.RGB{R: 100, G: 110, B: 120},
	SyntaxString:     color.RGB{R: 180, G: 220, B: 140},
	SyntaxKeyword:    color.RGB{R: 180, G: 140, B: 220},
	SyntaxType:       color.RGB{R: 80, G: 200, B: 200},
	SyntaxNumber:     color.RGB{R: 220, G: 180, B: 120},
	ViewerDefinition: color.RGB{R: 220, G: 180, B: 80},
	ViewerFold:       color.RGB{R: 100, G: 140, B: 180},
	ViewerMatch:      color.RGB{R: 80, G: 120, B: 60},
}
