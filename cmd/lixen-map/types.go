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
	TagFlat       []TagItem       // Flattened groups and tags for rendering
	TagCursor     int             // Cursor position in right pane
	TagScroll     int             // Scroll offset for right pane
	GroupExpanded map[string]bool // group name → expanded state

	// Interact view (right pane)
	InteractFlat          []TagItem
	InteractCursor        int
	InteractScroll        int
	InteractGroupExpanded map[string]bool

	// Filter state
	Filter      *FilterState
	RgAvailable bool // ripgrep installed

	// UI state
	InputMode      bool           // true when typing keyword
	InputBuffer    string         // keyword input buffer
	SearchType     SearchType     // tag vs group search
	SearchCategory SearchCategory // focus vs interact
	CommandPending rune           // first key of two-key sequence, 0 if none
	Message        string         // status message
	PreviewMode    bool           // showing file preview
	PreviewFiles   []string       // files to preview
	PreviewScroll  int            // preview scroll offset

	// Mindmap state
	MindmapMode  bool
	MindmapState *MindmapState

	// Dive state
	DiveMode  bool
	DiveState *DiveState

	// Editor state
	EditMode   bool   // true when editing tags
	EditTarget string // file path being edited

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
	Path     string              // relative path: "systems/drain.go"
	Package  string              // package name: "systems"
	Focus    map[string][]string // focus group -> tags: {"arch": ["ecs"], "gameplay": ["drain"]}
	Interact map[string][]string // interact group -> tags: {"init": ["cursor"], "state": ["gold"]}
	Imports  []string            // local package names: ["events", "engine"]
	IsAll    bool                // has #focus{all[*]}
	Size     int64               // file size in bytes
}

// SizeWarningThreshold is output size (bytes) above which size displays in warning color
const SizeWarningThreshold = 300 * 1024

// TagMap returns the appropriate tag map for the category
func (fi *FileInfo) TagMap(cat Category) map[string][]string {
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
	FocusGroups  []string            // sorted group names
	FocusTags    map[string][]string // group -> tags
	FocusByGroup map[string][]string // group -> file paths
	FocusByTag   map[string][]string // "group:tag" -> file paths

	// Interact index
	InteractGroups  []string            // sorted group names
	InteractTags    map[string][]string // group -> tags
	InteractByGroup map[string][]string // group -> file paths
	InteractByTag   map[string][]string // "group:tag" -> file paths
}

// TagGroups returns sorted group names for the category
func (idx *Index) TagGroups(cat Category) []string {
	if cat == CategoryInteract {
		return idx.InteractGroups
	}
	return idx.FocusGroups
}

// TagsByGroup returns group -> tags map for the category
func (idx *Index) TagsByGroup(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractTags
	}
	return idx.FocusTags
}

// FilesByGroup returns group -> file paths map for the category
func (idx *Index) FilesByGroup(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractByGroup
	}
	return idx.FocusByGroup
}

// FilesByTag returns "group:tag" -> file paths map for the category
func (idx *Index) FilesByTag(cat Category) map[string][]string {
	if cat == CategoryInteract {
		return idx.InteractByTag
	}
	return idx.FocusByTag
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

// FilterState holds visual filter highlight state
type FilterState struct {
	FilteredPaths        map[string]bool            // files matching current filter
	FilteredFocusTags    map[string]map[string]bool // focus group -> tag -> highlighted
	FilteredInteractTags map[string]map[string]bool // interact group -> tag -> highlighted
	Mode                 FilterMode
}

// NewFilterState creates initialized empty FilterState
func NewFilterState() *FilterState {
	return &FilterState{
		FilteredPaths:        make(map[string]bool),
		FilteredFocusTags:    make(map[string]map[string]bool),
		FilteredInteractTags: make(map[string]map[string]bool),
		Mode:                 FilterOR,
	}
}

// HasActiveFilter returns true if any paths are filtered
func (f *FilterState) HasActiveFilter() bool {
	return len(f.FilteredPaths) > 0
}

// FilteredTags returns the appropriate filtered tags map for the category
func (f *FilterState) FilteredTags(cat Category) map[string]map[string]bool {
	if cat == CategoryInteract {
		return f.FilteredInteractTags
	}
	return f.FilteredFocusTags
}

// TagItem represents a group header or tag in right pane list
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

// DivePane identifies active pane in dive view
type DivePane int

const (
	DivePaneDependsOn DivePane = iota
	DivePaneDependedBy
	DivePaneFocusLinks
	DivePaneInteractLinks
)

// DiveItemType distinguishes item kinds in dive list
type DiveItemType int

const (
	DiveItemDir   DiveItemType = iota // Package directory
	DiveItemGroup                     // Tag group header
	DiveItemTag                       // Tag under group
	DiveItemFile                      // Leaf file
)