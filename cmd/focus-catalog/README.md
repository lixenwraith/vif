# focus-catalog

Interactive TUI for selecting Go codebase subsets as LLM context. Indexes packages by focus tags, resolves local import dependencies, supports keyword filtering.

## Installation
```bash
go build -o focus-catalog .
```

**Optional:** `ripgrep` (`rg`) for content search in left pane.

## Usage
```bash
# Run in project root, writes to './catalog.txt'
./focus-catalog

# Custom output file
./focus-catalog -o context.txt
```

## Focus Tags

Declare in Go files before `package` statement:
```go
// @focus: #core { ecs, lifecycle } #game { drain, collision }
// @focus: #all
package systems
```

| Syntax | Meaning |
|--------|---------|
| `#group { tag1, tag2 }` | Assign tags to group |
| `#all` | Include file in every output |
| Multiple `// @focus:` lines | Accumulated |

## Key Bindings

### Navigation
| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `PgUp` / `PgDn` | Page up/down |
| `Home` / `0` | Jump to first item |
| `End` / `$` | Jump to last item |
| `Tab` | Switch pane focus |

### Tree Operations (Left Pane)

| Key | Action |
|-----|--------|
| `h` / `←` | Collapse directory |
| `l` / `→` | Expand directory |
| `H` | Collapse all directories |
| `L` | Expand all directories |

### Tag Groups (Right Pane)

| Key | Action |
|-----|--------|
| `h` / `←` | Collapse current group |
| `l` / `→` | Expand current group |
| `H` | Collapse all groups |
| `L` | Expand all groups |

### Views

| Key | Action |
|-----|--------|
| `v` | Open mindmap view |
| `p` | Preview selected files |
| `Esc` | Clear active filters |

### Editing

| Key | Action |
|-----|--------|
| `e` | Edit tags for current file (left pane only) |
| `r` | Re-index entire tree |

### Selection
| Key | Action |
|-----|--------|
| `Space` | Toggle selection (file, directory, tag, or group) |
| `a` | Select all visible items (works in both panes) |
| `c` | Clear all selections and filters |

### Filtering
| Key | Action |
|-----|--------|
| `f` | Apply filter at cursor position |
| `/` | Start keyword search |
| `m` | Toggle filter mode (OR/AND) |
| `Esc` | Clear active filters |

### Dependencies & Output
| Key | Action |
|-----|--------|
| `d` | Toggle dependency expansion |
| `+`/`-` | Adjust expansion depth (1-5) |
| `p` | Preview output files |
| `Enter` | Write output file (stays in app) |
| `q` | Quit |

## Filtering

Filtering highlights matching items without changing selection. Use filtering to preview which files match before selecting.

### Filter with `f` Key

**Left Pane:**
- On file: highlights that single file and its associated tags in right pane
- On directory: highlights all files in directory and their associated tags

**Right Pane:**
- On tag: highlights all files with that tag and the tag itself
- On group: highlights all files with any tag in that group

### Filter with `/` Search

Search behavior depends on active pane:

**Left Pane (content search):**
- Uses ripgrep to search filenames, directory names, and file contents
- Falls back to filename-only search if ripgrep unavailable
- Example: `/cache` finds files containing "cache" in name or content

**Right Pane (exact match):**
- Searches for exact group or tag name matches
- Example: `/ecs` finds files tagged with group or tag named "ecs"

### Filter Chaining

Multiple filters can be applied sequentially. The filter mode determines how filters combine:

**OR Mode (default):** New filter results are added to existing (union)
```
Filter A → Filter B (OR) → Result: A ∪ B
```

**AND Mode:** New filter results intersect with existing
```
Filter A → Filter B (AND) → Result: A ∩ B
```

Toggle mode with `m` key. Mode applies to the *next* filter operation.

**Example workflow:**
1. Press `f` on `#core` group → highlights all core files
2. Press `m` to switch to AND mode
3. Press `f` on `#game` group → highlights files with BOTH core AND game tags

Press `Esc` at any time to clear all filters.

### Filter Visual Indicators

- Filtered items: normal brightness
- Non-filtered items: dimmed
- Status bar shows: `Filter: N files [OR/AND]`

## Selection

Selection determines which files are included in the output. Selection and filtering are independent—you can select items regardless of filter state.

### Selection with `Space`

**Left Pane:**
- On file: toggles that file
- On directory: toggles all files recursively (select all if any unselected, deselect all if all selected)

**Right Pane:**
- On tag: toggles all files with that tag
- On group: toggles all files with any tag in that group

### Selection Indicators

**Left Pane (files):**
| Symbol | Meaning |
|--------|---------|
| `[x]` | Directly selected |
| `[+]` | Included via dependency expansion (purple) |
| `[ ]` | Not selected |

**Right Pane (tags/groups):**
| Symbol | Meaning |
|--------|---------|
| `[x]` | All files with this tag/group selected (green) |
| `[o]` | Some files with this tag/group selected (blue) |
| `[ ]` | No files with this tag/group selected |

### Bulk Selection

- `a` in left pane: select all visible files
- `a` in right pane: select all files matching current filter (or all tagged files if no filter)
- `c`: clear all selections and filters

## Filter Modes

Toggle with `m` key.

### OR Mode (default)
Each filter adds to the highlight set. Use for exploring related files across different criteria.

### AND Mode
Each filter narrows the highlight set. Use for finding files matching multiple criteria.

## Features

### Split-Pane Interface
- **Left pane:** Directory tree with files, shows selection counts `[n/m]`
- **Right pane:** Tag groups and tags with file counts and selection state

### Visual Indicators

**Left pane:** Files show group summary after filename (e.g., `drain.go  core(2) game(1)`)

**Right pane:** Groups and tags show directory hints after count (e.g., `#core (12)  systems engine`)

**Special files:** Orange color indicates `#all` tag (always included in output)

### Dependency Expansion
Selecting files auto-includes their package's local imports (transitive, configurable depth 1-5). Dependency-included files show `[+]` in purple.

### Cross-Pane Synchronization
- Filtering in left pane highlights corresponding tags in right pane
- Filtering in right pane highlights corresponding files in left pane
- Selection state is always synchronized between panes

## Output Format
```
./systems/drain.go
./systems/spawn.go
./events/types.go
```

Files with `#all` tag always included. Sorted alphabetically. Compatible with `combine.sh -f`.

## Mindmap View

Press `v` to open a contextual mindmap visualization:

**From Left Pane (on directory/file):**
- Shows hierarchical view of package structure
- Displays all files with their tags
- Nested packages shown with proper indentation

**From Right Pane (on tag/group):**
- Shows all files containing that tag or group
- Files listed with full paths and all their tags

**Mindmap Controls:**
| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate |
| `Space` | Toggle selection |
| `a` | Select all visible |
| `c` | Clear visible selections |
| `0`/`$` or `Home`/`End` | Jump to start/end |
| `Esc` or `q` | Return to main view |

Selections made in mindmap sync bidirectionally with the main view.

## Tag Editor

Press `e` on any file in the left pane to edit its `@focus` tags inline:

- Pre-fills with current tag content (empty if no existing tags)
- Type to edit, `Backspace` to delete
- `Enter` to save changes
- `Esc` to cancel

The editor:
- Modifies only the first `// @focus:` line before `package` statement
- Inserts new focus line at file start if none exists
- Preserves build tags (`//go:build`, `// +build`)
- Uses atomic writes (temp file + rename) for safety
- Automatically re-indexes after saving

## Workflow Examples

### Find all files related to a component
1. Navigate to component file in left pane (e.g., `components/drain.go`)
2. Press `f` to filter
3. Tab to right pane to see associated tags highlighted
4. Press `a` to select all filtered files
5. Press `Enter` to output

### Find files matching multiple tags
1. Press `f` on first tag in right pane
2. Press `m` to switch to AND mode
3. Press `f` on second tag
4. Result shows only files with both tags
5. Press `Space` to select, or `a` to select all

### Explore a subsystem
1. Press `f` on a package directory
2. Tab to right pane to see which tags are used
3. Navigate tags to understand the domain
4. Select relevant tags with `Space`
5. Press `Esc` to clear filter while keeping selection
6. Continue selecting from other areas

### Quick output of tagged files
1. Tab to right pane
2. Press `Space` on desired group to select all files in group
3. Press `Enter` to output