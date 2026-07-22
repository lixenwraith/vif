package main

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal/tui"
)

// Theme provides consistent styling across all UI components, extends tui.Theme with app-specific colors
type Theme struct {
	tui.Theme // Embedded base theme

	// Selection states
	Expanded color.RGB // Dependency-expanded files indicator

	// Tag hierarchy colors
	GroupFg    color.RGB // Tag group names (#group)
	ModuleFg   color.RGB // Module names in tags
	TagFg      color.RGB // Individual tag names
	ExternalFg color.RGB // External package references
	AllTagFg   color.RGB // Files marked with #all tag

	// Viewer-specific (extend syntax highlighting)
	ViewerDefinition color.RGB
	ViewerFold       color.RGB
	ViewerMatch      color.RGB
}

var DefaultTheme = Theme{
	Theme: tui.DefaultTheme,

	Expanded:   color.RGB{R: 180, G: 140, B: 220},
	GroupFg:    color.RGB{R: 220, G: 180, B: 80},
	ModuleFg:   color.RGB{R: 80, G: 200, B: 80},
	TagFg:      color.RGB{R: 100, G: 200, B: 220},
	ExternalFg: color.RGB{R: 100, G: 140, B: 160},
	AllTagFg:   color.RGB{R: 255, G: 180, B: 100},

	ViewerDefinition: color.RGB{R: 220, G: 180, B: 80},
	ViewerFold:       color.RGB{R: 100, G: 140, B: 180},
	ViewerMatch:      color.RGB{R: 80, G: 120, B: 60},
}

