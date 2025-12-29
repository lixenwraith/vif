package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
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
}

// AppState holds all application state including UI, index, and selections
type AppState struct {
	Term  terminal.Terminal
	Index *Index
	Theme Theme

	FocusPane Pane

	TreeRoot   *TreeNode   // Root of directory tree
	TreeFlat   []*TreeNode // Flattened visible nodes for rendering
	TreeState  *tui.TreeState
	TreeExpand *tui.TreeExpansion

	Selected   map[string]bool // file path → selected
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	CurrentCategory string                      // active category name
	CategoryNames   []string                    // sorted category names from index
	CategoryUI      map[string]*CategoryUIState // per-category UI state

	DepByState *DetailPaneState // State for "Depended By" pane
	DepOnState *DetailPaneState // State for "Depends On" pane

	DepAnalysisCache map[string]*DependencyAnalysis // file path → analysis

	Filter      *FilterState
	RgAvailable bool // ripgrep installed

	InputMode  bool                // true when typing filter query
	InputField *tui.TextFieldState // text input state for filter
	Message    string              // status message

	Width  int
	Height int

	Viewer *FileViewerState // File viewer overlay state
}

// DetailPaneState manages UI state for dependency panes (DepBy, DepOn)
type DetailPaneState struct {
	FlatItems []DetailItem       // Flattened list for rendering
	TreeState *tui.TreeState     // Cursor and scroll management
	Expansion *tui.TreeExpansion // Header expansion state
}

// NewDetailPaneState creates initialized detail pane state
func NewDetailPaneState() *DetailPaneState {
	return &DetailPaneState{
		TreeState: tui.NewTreeState(20),
		Expansion: tui.NewTreeExpansion(),
	}
}

// DetailItem represents a single row in dependency panes
type DetailItem struct {
	Level    int    // Indentation level (0 or 1)
	Label    string // Display text
	Key      string // Unique key for expansion/navigation
	IsHeader bool   // True if expandable parent
	Expanded bool   // Current expansion state
	IsFile   bool   // True if it points to a file
	IsSymbol bool   // True if it is a symbol
	Path     string // Associated file path (for files)
	PkgDir   string // Package directory (for selection on headers/files)
	IsLocal  bool   // True if internal to module (for coloring)
	HasUsage bool   // True if file actively uses symbols from target
}

// DependencyAnalysis holds parsed import/symbol usage for a file
type DependencyAnalysis struct {
	UsedSymbols map[string][]string
}

// CategoryUIState manages UI state for a single lixen category
type CategoryUIState struct {
	Flat      []TagItem          // Flattened groups/modules/tags for rendering
	TreeState *tui.TreeState     // Cursor and scroll management
	Expansion *tui.TreeExpansion // Group/module expansion state
}

// NewCategoryUIState creates initialized category UI state
func NewCategoryUIState() *CategoryUIState {
	return &CategoryUIState{
		TreeState: tui.NewTreeState(20),
		Expansion: tui.NewTreeExpansion(),
	}
}

// Layout constants
const (
	SizeWarningThreshold = 300 * 1024
	defaultModulePath    = "github.com/lixenwraith/vi-fighter"
)

// Pane identifies which pane has focus
type Pane int

const (
	PaneLixen Pane = iota // Category tags (left)
	PaneTree              // Packages/Files tree (second from left)
	PaneDepBy             // Depended by (third from left)
	PaneDepOn             // Depends on (right)
)

// FilterMode determines how multiple filter operations combine
type FilterMode int

const (
	FilterOR  FilterMode = iota // Match ANY selected tag (union)
	FilterAND                   // Match ALL selected groups (intersection)
	FilterNOT                   // Remove from existing (subtraction)
	FilterXOR                   // Toggle membership (symmetric difference)
)

// FileInfo holds parsed metadata for a single Go source file
type FileInfo struct {
	Path        string
	Package     string
	Tags        map[string]map[string]map[string][]string // category → group → module → tags
	Imports     []string
	Definitions []string // Exported symbols defined in this file
	IsAll       bool
	Size        int64
}

// CategoryTags returns the tag hierarchy for a specific category
func (fi *FileInfo) CategoryTags(cat string) map[string]map[string][]string {
	if fi.Tags == nil {
		return nil
	}
	return fi.Tags[cat]
}

// HasCategory checks if file has any tags in the given category
func (fi *FileInfo) HasCategory(cat string) bool {
	if fi.Tags == nil {
		return false
	}
	_, ok := fi.Tags[cat]
	return ok
}

// PackageInfo holds metadata for a Go package directory
type PackageInfo struct {
	Name      string
	Dir       string
	Files     []*FileInfo
	LocalDeps []string
	HasAll    bool
}

// CategoryIndex provides fast lookups for a single lixen category
type CategoryIndex struct {
	Groups   []string                       // sorted group names
	Modules  map[string][]string            // group → sorted module names (excludes DirectTagsModule)
	Tags     map[string]map[string][]string // group → module → sorted tags
	ByGroup  map[string][]string            // "group" → file paths
	ByModule map[string][]string            // "group.module" → file paths
	ByTag    map[string][]string            // "group.module.tag" or "group..tag" → file paths
}

// NewCategoryIndex creates an empty category index
func NewCategoryIndex() *CategoryIndex {
	return &CategoryIndex{
		Groups:   nil,
		Modules:  make(map[string][]string),
		Tags:     make(map[string]map[string][]string),
		ByGroup:  make(map[string][]string),
		ByModule: make(map[string][]string),
		ByTag:    make(map[string][]string),
	}
}

// Index holds the complete parsed project state
type Index struct {
	ModulePath  string
	Packages    map[string]*PackageInfo
	Files       map[string]*FileInfo
	ReverseDeps map[string][]string // package dir → packages that import it

	Categories    map[string]*CategoryIndex // category name → index
	CategoryNames []string                  // sorted category names
}

// Category returns the index for a specific category name
func (idx *Index) Category(name string) *CategoryIndex {
	if idx.Categories == nil {
		return nil
	}
	return idx.Categories[name]
}

// HasCategory checks if the index contains a specific category
func (idx *Index) HasCategory(name string) bool {
	if idx.Categories == nil {
		return false
	}
	_, ok := idx.Categories[name]
	return ok
}

// FilterState tracks active filter criteria and matching files
type FilterState struct {
	FilteredPaths        map[string]bool                                  // files matching current filter
	FilteredCategoryTags map[string]map[string]map[string]map[string]bool // category → group → module → tag → highlighted
	Mode                 FilterMode
}

// NewFilterState creates an empty filter state
func NewFilterState() *FilterState {
	return &FilterState{
		FilteredPaths:        make(map[string]bool),
		FilteredCategoryTags: make(map[string]map[string]map[string]map[string]bool),
		Mode:                 FilterOR,
	}
}

// FilteredTags returns highlighted tags for a category
func (f *FilterState) FilteredTags(cat string) map[string]map[string]map[string]bool {
	if f.FilteredCategoryTags == nil {
		return nil
	}
	return f.FilteredCategoryTags[cat]
}

// HasActiveFilter returns true if any filter is applied
func (f *FilterState) HasActiveFilter() bool {
	return len(f.FilteredPaths) > 0
}

// TreeNode represents a node in the file/directory tree
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

// TagSelectionState indicates selection status of a tag/group/module
type TagSelectionState int

const (
	TagSelectNone TagSelectionState = iota
	TagSelectPartial
	TagSelectFull
)

// DirectTagsModule is the key for tags directly under a group (2-level tags)
const DirectTagsModule = ""

// TagItemType identifies the level of a tag item
type TagItemType uint8

const (
	TagItemTypeGroup TagItemType = iota
	TagItemTypeModule
	TagItemTypeTag
)

// TagItem represents a single row in the lixen category pane
type TagItem struct {
	Type     TagItemType
	Group    string
	Module   string // empty for groups, DirectTagsModule for 2-level tags
	Tag      string // empty for groups and modules
	Expanded bool   // groups and modules can expand
}