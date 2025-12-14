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

	// Tree view (left pane)
	TreeRoot   *TreeNode   // Root of directory tree
	TreeFlat   []*TreeNode // Flattened visible nodes for rendering
	TreeCursor int         // Cursor position in flattened list
	TreeScroll int         // Scroll offset for left pane

	// Selection (file paths)
	Selected   map[string]bool // file path → selected
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	// Focus view (center pane)
	TagFlat        []TagItem       // Flattened groups and tags for rendering
	TagCursor      int             // Cursor position in right pane
	TagScroll      int             // Scroll offset for right pane
	GroupExpanded  map[string]bool // group name → expanded state
	ModuleExpanded map[string]bool // "group.module" → expanded state  // NEW

	// Interact view (right pane)
	InteractFlat           []TagItem
	InteractCursor         int
	InteractScroll         int
	InteractGroupExpanded  map[string]bool
	ModuleInteractExpanded map[string]bool

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

	// Preferences (future: persist to disk)
	StartCollapsed bool // Start with all panes collapsed

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
	colorModuleFg        = terminal.RGB{R: 80, G: 200, B: 80}
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
	PaneLeft   Pane = iota // Packages/Files tree
	PaneCenter             // Focus Groups/Tags
	PaneRight              // Interact Groups/Tags
)

// FilterMode determines how successive filters combine
type FilterMode int

const (
	FilterOR  FilterMode = iota // Match ANY selected tag (union)
	FilterAND                   // Match ALL selected groups (intersection)
	FilterNOT                   // Remove from existing (subtraction)
	FilterXOR                   // Toggle membership (symmetric difference)
)

// Category discriminates between Focus and Interact tag collections
type Category int

const (
	CategoryFocus Category = iota
	CategoryInteract
)

// FileInfo holds parsed metadata for a single Go source file
type FileInfo struct {
	Path    string
	Package string
	// Focus: group → module → tags
	// For 2-level format group(tag): module key is DirectTagsModule ("")
	// For 3-level format group[mod(tag)]: module key is the module name
	Focus    map[string]map[string][]string
	Interact map[string]map[string][]string
	Imports  []string
	IsAll    bool
	Size     int64
}

// SizeWarningThreshold is output size (bytes) above which size displays in warning color
const SizeWarningThreshold = 300 * 1024

// TagMap returns the appropriate tag map for the category
func (fi *FileInfo) TagMap(cat Category) map[string]map[string][]string {
	if cat == CategoryInteract {
		return fi.Interact
	}
	return fi.Focus
}

// PackageInfo aggregates file data for a package directory
type PackageInfo struct {
	Name        string
	Dir         string
	Files       []*FileInfo
	AllFocus    map[string][]string // union of all file focus tags
	AllInteract map[string][]string // union of all file interact tags
	LocalDeps   []string
	HasAll      bool
}

// Index holds the complete indexed codebase representation
type Index struct {
	ModulePath  string
	Packages    map[string]*PackageInfo
	Files       map[string]*FileInfo
	ReverseDeps map[string][]string

	// Focus index
	FocusGroups   []string                       // sorted group names
	FocusModules  map[string][]string            // group → sorted module names (excludes DirectTagsModule)
	FocusTags     map[string]map[string][]string // group → module → sorted tags
	FocusByGroup  map[string][]string            // "group" → file paths
	FocusByModule map[string][]string            // "group.module" → file paths
	FocusByTag    map[string][]string            // "group.module.tag" → file paths

	// Interact index
	InteractGroups   []string
	InteractModules  map[string][]string
	InteractTags     map[string]map[string][]string
	InteractByGroup  map[string][]string
	InteractByModule map[string][]string
	InteractByTag    map[string][]string
}

// TagGroups returns sorted group names for the category
func (idx *Index) TagGroups(cat Category) []string {
	if cat == CategoryInteract {
		return idx.InteractGroups
	}
	return idx.FocusGroups
}

// TagModules returns group → modules map for the category
func (idx *Index) TagModules(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractModules
	}
	return idx.FocusModules
}

// TagsByGroupModule returns group → module → tags map for the category
func (idx *Index) TagsByGroupModule(cat Category) map[string]map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractTags
	}
	return idx.FocusTags
}

// FilesByGroup returns group → file paths map for the category
func (idx *Index) FilesByGroup(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractByGroup
	}
	return idx.FocusByGroup
}

// FilesByModule returns "group.module" → file paths map for the category
func (idx *Index) FilesByModule(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractByModule
	}
	return idx.FocusByModule
}

// FilesByTag returns "group.module.tag" → file paths map for the category
func (idx *Index) FilesByTag(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractByTag
	}
	return idx.FocusByTag
}

// FilterState holds visual filter highlight state
type FilterState struct {
	FilteredPaths        map[string]bool                       // files matching current filter
	FilteredFocusTags    map[string]map[string]map[string]bool // group → module → tag → highlighted
	FilteredInteractTags map[string]map[string]map[string]bool
	Mode                 FilterMode
}

// NewFilterState creates initialized empty FilterState
func NewFilterState() *FilterState {
	return &FilterState{
		FilteredPaths:        make(map[string]bool),
		FilteredFocusTags:    make(map[string]map[string]map[string]bool),
		FilteredInteractTags: make(map[string]map[string]map[string]bool),
		Mode:                 FilterOR,
	}
}

// FilteredTags returns the appropriate filtered tags map for the category
func (f *FilterState) FilteredTags(cat Category) map[string]map[string]map[string]bool {
	if cat == CategoryInteract {
		return f.FilteredInteractTags
	}
	return f.FilteredFocusTags
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

// SearchType indicates active search mode for input
type SearchType uint8

const (
	SearchTypeContent SearchType = iota
	SearchTypeTags
	SearchTypeGroups
)

// SearchCategory indicates which tag category to search
type SearchCategory uint8

const (
	SearchCategoryFocus SearchCategory = iota
	SearchCategoryInteract
)

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