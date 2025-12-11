package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// AppState holds all application state
type AppState struct {
	Term  terminal.Terminal
	Index *Index

	// Pane focus
	FocusPane Pane

	// Tree view (left pane)
	TreeRoot   *TreeNode   // Root of directory tree
	TreeFlat   []*TreeNode // Flattened visible nodes for rendering
	TreeCursor int         // Cursor position in flattened list
	TreeScroll int         // Scroll offset for left pane

	// Selection (file paths)
	Selected   map[string]bool // file path → selected
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	// Tag view (right pane)
	TagFlat       []TagItem       // Flattened groups and tags for rendering
	TagCursor     int             // Cursor position in right pane
	TagScroll     int             // Scroll offset for right pane
	GroupExpanded map[string]bool // group name → expanded state

	// Filter state
	Filter      *FilterState
	RgAvailable bool // ripgrep installed

	// UI state
	InputMode     bool     // true when typing keyword
	InputBuffer   string   // keyword input buffer
	Message       string   // status message
	PreviewMode   bool     // showing file preview
	PreviewFiles  []string // files to preview
	PreviewScroll int      // preview scroll offset

	// Mindmap state
	MindmapMode  bool
	MindmapState *MindmapState

	// Editor state
	EditMode   bool   // true when editing tags
	EditTarget string // file path being edited

	// Dimensions
	Width  int
	Height int
}

// Colors
var (
	colorHeaderBg        = terminal.RGB{R: 40, G: 60, B: 90}
	colorHeaderFg        = terminal.RGB{R: 255, G: 255, B: 255}
	colorSelected        = terminal.RGB{R: 80, G: 200, B: 80}
	colorUnselected      = terminal.RGB{R: 100, G: 100, B: 100}
	colorCursorBg        = terminal.RGB{R: 50, G: 50, B: 70}
	colorTagFg           = terminal.RGB{R: 100, G: 200, B: 220}
	colorGroupFg         = terminal.RGB{R: 220, G: 180, B: 80}
	colorStatusFg        = terminal.RGB{R: 140, G: 140, B: 140}
	colorHelpFg          = terminal.RGB{R: 100, G: 180, B: 200}
	colorInputBg         = terminal.RGB{R: 30, G: 30, B: 50}
	colorDefaultBg       = terminal.RGB{R: 20, G: 20, B: 30}
	colorDefaultFg       = terminal.RGB{R: 200, G: 200, B: 200}
	colorExpandedFg      = terminal.RGB{R: 180, G: 140, B: 220}
	colorAllTagFg        = terminal.RGB{R: 255, G: 180, B: 100}
	colorMatchCountFg    = terminal.RGB{R: 180, G: 220, B: 180}
	colorDirFg           = terminal.RGB{R: 130, G: 170, B: 220}
	colorPaneBorder      = terminal.RGB{R: 60, G: 80, B: 100}
	colorPaneActiveBg    = terminal.RGB{R: 30, G: 35, B: 45}
	colorGroupHintFg     = terminal.RGB{R: 140, G: 140, B: 100}
	colorDirHintFg       = terminal.RGB{R: 110, G: 110, B: 110}
	colorGroupNameFg     = terminal.RGB{R: 220, G: 180, B: 80}
	colorTagNameFg       = terminal.RGB{R: 100, G: 200, B: 220}
	colorPartialSelectFg = terminal.RGB{R: 80, G: 160, B: 220}
)

// Layout
const (
	headerHeight = 1
	statusHeight = 2
	helpHeight   = 1
	minWidth     = 80
	minHeight    = 20
	leftPaneMin  = 35
	rightPaneMin = 30
)

const defaultModulePath = "github.com/USER/vi-fighter"

// Pane identifies which pane has focus
type Pane int

const (
	PaneLeft  Pane = iota // Packages/Files tree
	PaneRight             // Groups/Tags
)

// FilterMode for cross-group tag selection
type FilterMode int

const (
	FilterOR  FilterMode = iota // Match ANY selected tag
	FilterAND                   // Match ALL selected groups (at least one tag per group)
)

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
	AllTags    map[string][]string     // group → all tags in that group
}

// TreeNode represents a directory or file in the tree view
type TreeNode struct {
	Name        string       // Display name: "systems" or "drain.go"
	Path        string       // Full relative path
	IsDir       bool         // true for directories/packages
	Expanded    bool         // Directory expansion state
	Children    []*TreeNode  // Child nodes (dirs first, then files)
	Parent      *TreeNode    // Parent node (nil for root)
	FileInfo    *FileInfo    // Non-nil for files
	PackageInfo *PackageInfo // Non-nil for package directories
	Depth       int          // Nesting level for indentation
}

// FilterState holds filter state (visual highlighting only)
type FilterState struct {
	FilteredPaths map[string]bool            // Files matching current filter
	FilteredTags  map[string]map[string]bool // group -> tag -> highlighted (computed from FilteredPaths)
	Mode          FilterMode
}

// NewFilterState creates an initialized FilterState
func NewFilterState() *FilterState {
	return &FilterState{
		FilteredPaths: make(map[string]bool),
		FilteredTags:  make(map[string]map[string]bool),
		Mode:          FilterOR,
	}
}

// HasActiveFilter returns true if any filter is active
func (f *FilterState) HasActiveFilter() bool {
	return len(f.FilteredPaths) > 0
}

// TagItem represents a group header or tag in the right pane
type TagItem struct {
	IsGroup  bool   // true for group header, false for tag
	Group    string // group name
	Tag      string // tag name (empty for group headers)
	Expanded bool   // for groups: expansion state
}

// TagSelectionState represents selection state for a tag/group
type TagSelectionState int

const (
	TagSelectNone TagSelectionState = iota
	TagSelectPartial
	TagSelectFull
)