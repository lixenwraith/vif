package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Theme provides consistent styling across all UI components.
// Centralizes color definitions for easy theming and maintenance.
type Theme struct {
	Bg         terminal.RGB // Default background
	Fg         terminal.RGB // Default foreground text
	FocusBg    terminal.RGB // Background for focused pane
	CursorBg   terminal.RGB // Background for cursor row
	Selected   terminal.RGB // Checkbox/indicator for fully selected
	Unselected terminal.RGB // Checkbox/indicator for unselected or dimmed
	Partial    terminal.RGB // Checkbox/indicator for partially selected
	Expanded   terminal.RGB // Indicator for dependency-expanded files
	Border     terminal.RGB // Pane borders and dividers
	HeaderBg   terminal.RGB // Top header bar background
	HeaderFg   terminal.RGB // Top header bar text
	StatusFg   terminal.RGB // Status bar and secondary text
	HintFg     terminal.RGB // Hint text and suffixes
	DirFg      terminal.RGB // Directory names in tree
	FileFg     terminal.RGB // File names in tree
	GroupFg    terminal.RGB // Tag group names (#group)
	ModuleFg   terminal.RGB // Module names in tags
	TagFg      terminal.RGB // Individual tag names
	ExternalFg terminal.RGB // External package references
	SymbolFg   terminal.RGB // Symbol names in dependency analysis
	AllTagFg   terminal.RGB // Files marked with #all tag
	WarningFg  terminal.RGB // Warning indicators (e.g., size threshold)
	InputBg    terminal.RGB // Input field background
	// Viewer syntax highlighting
	ViewerComment    terminal.RGB // Comments
	ViewerString     terminal.RGB // String literals
	ViewerKeyword    terminal.RGB // Keywords
	ViewerDefinition terminal.RGB // Defined symbols
	ViewerType       terminal.RGB // Type references
	ViewerNumber     terminal.RGB // Numeric literals
	ViewerFold       terminal.RGB // Fold indicators and collapsed text
	ViewerMatch      terminal.RGB // Search match background
}

// DefaultTheme provides the standard dark color scheme
var DefaultTheme = Theme{
	Bg:         terminal.RGB{R: 20, G: 20, B: 30},
	Fg:         terminal.RGB{R: 200, G: 200, B: 200},
	FocusBg:    terminal.RGB{R: 30, G: 35, B: 45},
	CursorBg:   terminal.RGB{R: 50, G: 50, B: 70},
	Selected:   terminal.RGB{R: 80, G: 200, B: 80},
	Unselected: terminal.RGB{R: 100, G: 100, B: 100},
	Partial:    terminal.RGB{R: 80, G: 160, B: 220},
	Expanded:   terminal.RGB{R: 180, G: 140, B: 220},
	Border:     terminal.RGB{R: 60, G: 80, B: 100},
	HeaderBg:   terminal.RGB{R: 40, G: 60, B: 90},
	HeaderFg:   terminal.RGB{R: 255, G: 255, B: 255},
	StatusFg:   terminal.RGB{R: 140, G: 140, B: 140},
	HintFg:     terminal.RGB{R: 100, G: 180, B: 200},
	DirFg:      terminal.RGB{R: 130, G: 170, B: 220},
	FileFg:     terminal.RGB{R: 200, G: 200, B: 200},
	GroupFg:    terminal.RGB{R: 220, G: 180, B: 80},
	ModuleFg:   terminal.RGB{R: 80, G: 200, B: 80},
	TagFg:      terminal.RGB{R: 100, G: 200, B: 220},
	ExternalFg: terminal.RGB{R: 100, G: 140, B: 160},
	SymbolFg:   terminal.RGB{R: 180, G: 220, B: 220},
	AllTagFg:   terminal.RGB{R: 255, G: 180, B: 100},
	WarningFg:  terminal.RGB{R: 255, G: 80, B: 80},
	InputBg:    terminal.RGB{R: 30, G: 30, B: 50},

	ViewerComment:    terminal.RGB{R: 100, G: 110, B: 120},
	ViewerString:     terminal.RGB{R: 180, G: 140, B: 100},
	ViewerKeyword:    terminal.RGB{R: 180, G: 100, B: 180},
	ViewerDefinition: terminal.RGB{R: 100, G: 200, B: 180},
	ViewerType:       terminal.RGB{R: 100, G: 180, B: 220},
	ViewerNumber:     terminal.RGB{R: 180, G: 180, B: 100},
	ViewerFold:       terminal.RGB{R: 180, G: 180, B: 200},
	ViewerMatch:      terminal.RGB{R: 80, G: 80, B: 40},
}