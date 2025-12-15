package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// AppState holds complete application runtime state
type AppState struct {
	Term  terminal.Terminal
	Index *Index

	// Pane focus
	FocusPane Pane

	// Tree view (second pane from left)
	TreeRoot   *TreeNode   // Root of directory tree
	TreeFlat   []*TreeNode // Flattened visible nodes for rendering
	TreeCursor int         // Cursor position in flattened list
	TreeScroll int         // Scroll offset

	// Selection (file paths)
	Selected   map[string]bool // file path → selected
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	// Category view (left pane) - dynamic per category
	CurrentCategory string                      // active category name
	CategoryNames   []string                    // sorted category names from index
	CategoryUI      map[string]*CategoryUIState // per-category UI state

	// Detail Panes (Right side)
	DepByState *DetailPaneState // State for "Depended By" pane
	DepOnState *DetailPaneState // State for "Depends On" pane

	// Dependency Analysis Cache (Forward deps)
	DepAnalysisCache map[string]*DependencyAnalysis // file path → analysis

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

	// Editor state
	EditMode   bool   // true when editing tags
	EditTarget string // file path being edited
	EditCursor int    // cursor position within InputBuffer

	// Help overlay
	HelpMode bool

	// Dimensions
	Width  int
	Height int
}

// DetailPaneState tracks UI state for dependency tree views
type DetailPaneState struct {
	Cursor    int
	Scroll    int
	Expanded  map[string]bool // item key → expanded
	FlatItems []DetailItem    // Flattened list for rendering
}

// NewDetailPaneState creates initialized DetailPaneState
func NewDetailPaneState() *DetailPaneState {
	return &DetailPaneState{
		Expanded: make(map[string]bool),
	}
}

// DetailItem represents a row in the dependency panes
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

// DependencyAnalysis holds symbol usage for a file
type DependencyAnalysis struct {
	// ImportPath → List of used symbols
	UsedSymbols map[string][]string
}

// CategoryUIState holds UI state for a single category pane
type CategoryUIState struct {
	Flat           []TagItem       // Flattened groups/modules/tags for rendering
	Cursor         int             // Cursor position
	Scroll         int             // Scroll offset
	GroupExpanded  map[string]bool // group name → expanded state
	ModuleExpanded map[string]bool // "group.module" → expanded state
}

// NewCategoryUIState creates initialized CategoryUIState
func NewCategoryUIState() *CategoryUIState {
	return &CategoryUIState{
		Flat:           nil,
		Cursor:         0,
		Scroll:         0,
		GroupExpanded:  make(map[string]bool),
		ModuleExpanded: make(map[string]bool),
	}
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
	colorModuleFg        = terminal.RGB{R: 80, G: 200, B: 80}
	colorExternalPkgFg   = terminal.RGB{R: 100, G: 140, B: 160} // Cyan/Dim for external/stdlib
	colorSymbolFg        = terminal.RGB{R: 180, G: 220, B: 220} // Light cyan for symbols
)

// Layout
const (
	headerHeight = 1
	statusHeight = 2
	helpHeight   = 1
	minWidth     = 80
	minHeight    = 20
)

const defaultModulePath = "github.com/lixenwraith/vi-fighter"

// Pane identifies which pane has keyboard focus
type Pane int

const (
	PaneLixen Pane = iota // Category tags (left)
	PaneTree              // Packages/Files tree (second from left)
	PaneDepBy             // Depended by (third from left)
	PaneDepOn             // Depends on (right)
)

// FilterMode determines how successive filters combine
type FilterMode int

const (
	FilterOR  FilterMode = iota // Match ANY selected tag (union)
	FilterAND                   // Match ALL selected groups (intersection)
	FilterNOT                   // Remove from existing (subtraction)
	FilterXOR                   // Toggle membership (symmetric difference)
)

// FileInfo holds parsed metadata for a single Go source file
type FileInfo struct {
	Path    string
	Package string
	// Tags: category → group → module → tags
	Tags        map[string]map[string]map[string][]string
	Imports     []string
	Definitions []string // Exported symbols defined in this file
	IsAll       bool
	Size        int64
}

// SizeWarningThreshold is output size (bytes) above which size displays in warning color
const SizeWarningThreshold = 300 * 1024

// CategoryTags returns the tag map for specified category, nil if not present
func (fi *FileInfo) CategoryTags(cat string) map[string]map[string][]string {
	if fi.Tags == nil {
		return nil
	}
	return fi.Tags[cat]
}

// HasCategory returns true if file has tags in specified category
func (fi *FileInfo) HasCategory(cat string) bool {
	if fi.Tags == nil {
		return false
	}
	_, ok := fi.Tags[cat]
	return ok
}

// PackageInfo aggregates file data for a package directory
type PackageInfo struct {
	Name      string
	Dir       string
	Files     []*FileInfo
	LocalDeps []string
	HasAll    bool
}

// CategoryIndex holds indexed tag data for a single category
type CategoryIndex struct {
	Groups   []string                       // sorted group names
	Modules  map[string][]string            // group → sorted module names (excludes DirectTagsModule)
	Tags     map[string]map[string][]string // group → module → sorted tags
	ByGroup  map[string][]string            // "group" → file paths
	ByModule map[string][]string            // "group.module" → file paths
	ByTag    map[string][]string            // "group.module.tag" or "group..tag" → file paths
}

// NewCategoryIndex creates initialized CategoryIndex
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

// Index holds the complete indexed codebase representation
type Index struct {
	ModulePath  string
	Packages    map[string]*PackageInfo
	Files       map[string]*FileInfo
	ReverseDeps map[string][]string // package dir → packages that import it

	// Dynamic category indexes
	Categories    map[string]*CategoryIndex // category name → index
	CategoryNames []string                  // sorted category names
}

// Category returns CategoryIndex for name, nil if not present
func (idx *Index) Category(name string) *CategoryIndex {
	if idx.Categories == nil {
		return nil
	}
	return idx.Categories[name]
}

// HasCategory returns true if category exists in index
func (idx *Index) HasCategory(name string) bool {
	if idx.Categories == nil {
		return false
	}
	_, ok := idx.Categories[name]
	return ok
}

// FilterState holds visual filter highlight state
type FilterState struct {
	FilteredPaths        map[string]bool                                  // files matching current filter
	FilteredCategoryTags map[string]map[string]map[string]map[string]bool // category → group → module → tag → highlighted
	Mode                 FilterMode
}

// NewFilterState creates initialized empty FilterState
func NewFilterState() *FilterState {
	return &FilterState{
		FilteredPaths:        make(map[string]bool),
		FilteredCategoryTags: make(map[string]map[string]map[string]map[string]bool),
		Mode:                 FilterOR,
	}
}

// FilteredTags returns the filtered tags map for specified category
func (f *FilterState) FilteredTags(cat string) map[string]map[string]map[string]bool {
	if f.FilteredCategoryTags == nil {
		return nil
	}
	return f.FilteredCategoryTags[cat]
}

// HasActiveFilter returns true if any paths are filtered
func (f *FilterState) HasActiveFilter() bool {
	return len(f.FilteredPaths) > 0
}

// TreeNode represents a directory or file in hierarchical tree
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

// TagSelectionState represents selection state for a tag/group
type TagSelectionState int

const (
	TagSelectNone TagSelectionState = iota
	TagSelectPartial
	TagSelectFull
)

// DirectTagsModule is sentinel for 2-level group(tag) format without modules
const DirectTagsModule = ""

// TagItemType distinguishes item kinds in tag pane list
type TagItemType uint8

const (
	TagItemTypeGroup TagItemType = iota
	TagItemTypeModule
	TagItemTypeTag
)

// TagItem represents a group, module, or tag in pane list
type TagItem struct {
	Type     TagItemType
	Group    string
	Module   string // empty for groups, DirectTagsModule for 2-level tags
	Tag      string // empty for groups and modules
	Expanded bool   // groups and modules can expand
}