package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Colors
var (
	colorHeaderBg     = terminal.RGB{R: 40, G: 60, B: 90}
	colorHeaderFg     = terminal.RGB{R: 255, G: 255, B: 255}
	colorSelected     = terminal.RGB{R: 80, G: 200, B: 80}
	colorUnselected   = terminal.RGB{R: 100, G: 100, B: 100}
	colorCursorBg     = terminal.RGB{R: 50, G: 50, B: 70}
	colorTagFg        = terminal.RGB{R: 100, G: 200, B: 220}
	colorGroupFg      = terminal.RGB{R: 220, G: 180, B: 80}
	colorStatusFg     = terminal.RGB{R: 140, G: 140, B: 140}
	colorHelpFg       = terminal.RGB{R: 100, G: 180, B: 200}
	colorInputBg      = terminal.RGB{R: 30, G: 30, B: 50}
	colorDefaultBg    = terminal.RGB{R: 20, G: 20, B: 30}
	colorDefaultFg    = terminal.RGB{R: 200, G: 200, B: 200}
	colorExpandedFg   = terminal.RGB{R: 180, G: 140, B: 220}
	colorAllTagFg     = terminal.RGB{R: 255, G: 180, B: 100}
	colorMatchCountFg = terminal.RGB{R: 180, G: 220, B: 180}
)

// Layout
const (
	headerHeight = 2
	statusHeight = 3
	helpHeight   = 2
	minWidth     = 60
	minHeight    = 15
)

const defaultModulePath = "github.com/USER/vi-fighter"

// FileInfo holds parsed data for a single Go file
type FileInfo struct {
	Path    string              // relative path: "systems/drain.go"
	Package string              // package name: "systems"
	Tags    map[string][]string // group → tags: {"core": ["ecs"], "game": ["drain"]}
	Imports []string            // local package names: ["events", "engine"]
	IsAll   bool                // has #all group
}

// PackageInfo aggregates files in a package
type PackageInfo struct {
	Name      string // "systems"
	Dir       string // "systems" or "cmd/focus-catalog"
	Files     []*FileInfo
	AllTags   map[string][]string // union of all file tags
	LocalDeps []string            // union of all file imports (local only)
	HasAll    bool                // any file has #all
}

// Index holds the complete codebase index
type Index struct {
	ModulePath string
	Packages   map[string]*PackageInfo // package name → info
	Files      map[string]*FileInfo    // relative path → info
	Groups     []string                // sorted list of all group names
}

// AppState holds all application state
type AppState struct {
	Term  terminal.Terminal
	Index *Index

	// Selection
	Selected   map[string]bool // selected package names
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	// Filtering
	ActiveGroup    string          // "" = all, else specific group
	GroupIndex     int             // index into Groups slice for cycling
	KeywordFilter  string          // current keyword (empty = none)
	KeywordMatches map[string]bool // file paths matching keyword
	CaseSensitive  bool            // keyword case sensitivity
	RgAvailable    bool            // ripgrep installed

	// UI state
	PackageList   []string // sorted package names (filtered for display)
	AllPackages   []string // all package names (unfiltered)
	CursorPos     int      // currently highlighted package index
	ScrollOffset  int      // for scrolling long lists
	InputMode     bool     // true when typing keyword
	InputBuffer   string   // keyword input buffer
	Message       string   // status message (clears on next action)
	PreviewMode   bool     // showing file preview
	PreviewFiles  []string // files to preview
	PreviewScroll int      // preview scroll offset

	// Dimensions
	Width  int
	Height int
}
