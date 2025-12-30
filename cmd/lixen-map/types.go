package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

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

	CategoryNames []string         // sorted category names from index
	LixenUI       *CategoryUIState // Hierarchy UI state

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
	Editor *EditorState     // Editor overlay state
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
	Nodes     []tui.TreeNode     // Cached nodes with Data populated
	TreeState *tui.TreeState     // Cursor and scroll management
	Expansion *tui.TreeExpansion // Group/module expansion state
}

func (ui *CategoryUIState) CurrentNode() *tui.TreeNode {
	if ui.TreeState.Cursor < 0 || ui.TreeState.Cursor >= len(ui.Nodes) {
		return nil
	}
	return &ui.Nodes[ui.TreeState.Cursor]
}

func (ui *CategoryUIState) CurrentData() *LixenNodeData {
	node := ui.CurrentNode()
	if node == nil || node.Data == nil {
		return nil
	}
	if data, ok := node.Data.(LixenNodeData); ok {
		return &data
	}
	return nil
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

const DirectTagsModule = "" // #cat{group(tag)}
const DirectTagsGroup = ""  // #cat(tag)

// TagItemType identifies the level of a tag item
type TagItemType uint8

const (
	TagItemTypeCategory TagItemType = iota
	TagItemTypeGroup
	TagItemTypeModule
	TagItemTypeTag
)

// LixenNodeData for TreeNode.Data payload
type LixenNodeData struct {
	Type     TagItemType
	Category string
	Group    string
	Module   string
	Tag      string
}

// TagItem represents a single row in the lixen category pane
type TagItem struct {
	Type     TagItemType
	Category string
	Group    string
	Module   string // empty for groups, DirectTagsModule for 2-level tags
	Tag      string // empty for groups and modules
}