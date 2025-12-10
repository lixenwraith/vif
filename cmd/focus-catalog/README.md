# focus-catalog

Interactive TUI for selecting Go codebase subsets as LLM context. Indexes packages by focus tags, resolves local import dependencies, supports keyword filtering.

## Installation
```bash
go build -o focus-catalog .
```

**Optional:** `ripgrep` (`rg`) for content search mode.

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
| `Esc` | Exit view / clear filters |

### Editing

| Key | Action |
|-----|--------|
| `e` | Edit tags for current file (left pane only) |
| `r` | Re-index entire tree |

### Selection
| Key | Action |
|-----|--------|
| `Space` | Toggle selection (file, directory, or tag) |
| `a` | Select all visible files |
| `c` | Clear all selections and filters |

### Search & Filtering
| Key | Action |
|-----|--------|
| `/` | Start keyword search |
| `s` | Toggle search mode (metadata/content) |
| `m` | Toggle filter mode (OR/AND) |
| `i` | Toggle case sensitivity |
| `Escape` | Clear active filters |

### Dependencies & Output
| Key | Action |
|-----|--------|
| `d` | Toggle dependency expansion |
| `+`/`-` | Adjust expansion depth (1-5) |
| `p` | Preview output files |
| `Enter` | Write output file (stays in app) |
| `q` | Quit |

## Search Modes

Toggle with `s` key.

### Metadata Search (default)
Searches file path, package name, group names, and tag names. Fast, no external dependencies.

**Example:** Searching `ecs` matches:
- Files in path containing "ecs" (e.g., `systems/ecs.go`)
- Files in package named "ecs"
- Files with `#core{ecs}` or similar tag

### Content Search (requires `rg`)
Searches actual file contents using ripgrep. Useful for finding files that reference specific identifiers, functions, or strings.

**Example:** Searching `cache` matches files containing the word "cache" anywhere in source code.

## Filter Modes

Toggle with `m` key.

### OR Mode (default)
File matches if it has **any** of the selected tags.

### AND Mode
File matches if it has **at least one selected tag from each group** that has selections.

**Example:** With `#core{ecs}` and `#game{collision}` selected:
- OR: Files with `ecs` tag OR `collision` tag
- AND: Files with BOTH `ecs` AND `collision` tags

## Features

### Split-Pane Interface
- **Left pane:** Directory tree with files, shows selection counts `[n/m]`
- **Right pane:** Tag groups and tags with file counts

### Visual Indicators
- `[x]` Directly selected
- `[+]` Included via dependency expansion
- `[ ]` Not selected
- Dimmed files don't match current filters
- Orange files have `#all` tag (always included)

### Dependency Expansion
Selecting files auto-includes their package's local imports (transitive, configurable depth 1-5).

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
- `j/k` or arrows: Navigate
- `Space`: Toggle selection
- `a`: Select all visible
- `c`: Clear visible selections
- `0/$` or `Home/End`: Jump to start/end
- `Esc` or `q`: Return to main view

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