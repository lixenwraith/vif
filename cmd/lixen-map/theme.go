package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// Theme provides consistent styling across all UI components, extends tui.Theme with app-specific colors
type Theme struct {
	tui.Theme // Embedded base theme

	// Selection states
	Expanded terminal.RGB // Dependency-expanded files indicator

	// Tag hierarchy colors
	GroupFg    terminal.RGB // Tag group names (#group)
	ModuleFg   terminal.RGB // Module names in tags
	TagFg      terminal.RGB // Individual tag names
	ExternalFg terminal.RGB // External package references
	AllTagFg   terminal.RGB // Files marked with #all tag

	// Viewer-specific (extend syntax highlighting)
	ViewerDefinition terminal.RGB
	ViewerFold       terminal.RGB
	ViewerMatch      terminal.RGB
}

var DefaultTheme = Theme{
	Theme: tui.DefaultTheme,

	Expanded:   terminal.RGB{R: 180, G: 140, B: 220},
	GroupFg:    terminal.RGB{R: 220, G: 180, B: 80},
	ModuleFg:   terminal.RGB{R: 80, G: 200, B: 80},
	TagFg:      terminal.RGB{R: 100, G: 200, B: 220},
	ExternalFg: terminal.RGB{R: 100, G: 140, B: 160},
	AllTagFg:   terminal.RGB{R: 255, G: 180, B: 100},

	ViewerDefinition: terminal.RGB{R: 220, G: 180, B: 80},
	ViewerFold:       terminal.RGB{R: 100, G: 140, B: 180},
	ViewerMatch:      terminal.RGB{R: 80, G: 120, B: 60},
}