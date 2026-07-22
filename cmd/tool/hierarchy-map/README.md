# hierarchy-map

Terminal TUI for selecting Go codebase subsets as LLM context. Indexes files by `@lixen:` annotations, resolves local import dependencies, and performs AST analysis to visualize symbol-level coupling.

## Installation
```bash
go build -o hierarchy-map ./cmd/hierarchy-map
```

**Optional:** `ripgrep` (`rg`) for content search.

## Usage
```bash
# Run in project root, writes to './catalog.txt'
./hierarchy-map

# Custom output file
./hierarchy-map -o context.txt
```

Minimum terminal size: 120x24

## Annotations

Declare after `package` statement:
```go
package systems
// @lixen: #focus(term,io),#interact{sys[cursor(init),state(gold)]}
```

### Syntax

| Level | Format | Example |
|-------|--------|---------|
| 2-level | `#category(tag1,tag2)` | `#focus(term,io)` |
| 3-level | `#category{group(tag1,tag2)}` | `#focus{sys(term,io)}` |
| 4-level | `#category{group[module(tag1,tag2)]}` | `#focus{sys[term(vt100,ansi)]}` |
| Mixed | Combine formats | `#cat{grp(t1),grp2[mod(t2)]}` |
| Multi-category | Comma-separated | `#focus{...},#interact{...}` |
| Always include | `all(*)` in any category | `#focus{all(*)}` |

Categories are arbitrary names discovered at index time. Multiple `// @lixen:` lines accumulate. Whitespace ignored.

## Interface Layout

Four-pane split view:

| HIERARCHY | PACKAGES / FILES | DEPENDED BY | DEPENDS ON |
|-----------|------------------|-------------|------------|
| Tag tree by category | Directory/file tree | Reverse deps (who imports this) | Forward deps (what this imports) |

### Dependency Pane Features

**DEPENDED BY:**
- Files importing the selected file's package
- `★` badge indicates file actively uses symbols from selected file (not just package import)
- Active-usage files sorted to top

**DEPENDS ON:**
- Local packages the selected file imports
- Expandable to show specific symbols (functions/types/vars) used

## Key Bindings

Press `?` in any view for full help overlay.

### Global

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Next / Previous pane |
| `?` | Toggle help |
| `Ctrl+C` / `Ctrl+Q` | Quit |
| `Ctrl+S` | Write output file |
| `Ctrl+L` | Load selection from file |

### Navigation (All Panes)

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Move cursor |
| `h`/`l`, `←`/`→` | Collapse / Expand |
| `H` / `L` | Collapse / Expand all |
| `g` / `G`, `0` / `$` | Jump to start / end |
| `PgUp` / `PgDn` | Page scroll |

### Selection

| Key | Action |
|-----|--------|
| `Space` | Toggle selection |
| `s` | Select and advance to next sibling |
| `a` | Select all visible |
| `c` | Clear all selections |
| `F` | Select all filtered files |

### Filtering

| Key | Action |
|-----|--------|
| `f` | Toggle filter on cursor item |
| `/` | Content search (uses ripgrep if available) |
| `m` | Cycle mode: OR → AND → NOT → XOR |
| `Esc` | Clear filter |

**Filter Modes:**

| Mode | Behavior |
|------|----------|
| OR | Union - adds to existing filter |
| AND | Intersection - narrows filter |
| NOT | Subtraction - removes from filter |
| XOR | Symmetric difference - toggles membership |

### Dependencies

| Key | Action |
|-----|--------|
| `d` | Toggle dependency expansion |
| `+` / `-` | Adjust expansion depth (1-5) |
| `r` | Reindex codebase |

### File Viewer

Press `Enter` on a file to open viewer.

| Key | Action |
|-----|--------|
| `q` / `Esc` | Close viewer |
| `/` | Search |
| `n` / `N` | Next / Previous match |
| `o` | Toggle fold at cursor |
| `h` / `l` | Collapse / Expand fold |
| `M` / `R` | Collapse / Expand all folds |

### Tag Editor

Press `e` with files selected to open editor.

| Key | Action |
|-----|--------|
| `Tab` | Cycle panes: Tags → Input → Files |
| `Space` / `d` | Mark tag for deletion |
| `Enter` | Add tag from input field |
| `Ctrl+S` | Save changes |
| `Esc` | Cancel |

**Editor Panes:**
- **SELECTED FILES:** Read-only list of files being edited
- **TAGS:** Existing tags with deletion toggles; shows coverage `[N/M]` per tag
- **ADD TAG:** Raw input for new tags (e.g., `#focus{sys(new)}`)

## Visual Indicators

### Tree Pane

| Symbol | Meaning |
|--------|---------|
| `[x]` | Directly selected |
| `[+]` | Included via dependency expansion |
| `[ ]` | Not selected |

### Hierarchy Pane

| Symbol | Meaning |
|--------|---------|
| `[x]` | All files with this tag selected |
| `[o]` | Some files with this tag selected |
| `[ ]` | No files with this tag selected |

### Dependency Panes

| Symbol | Pane | Meaning |
|--------|------|---------|
| `★` | Depended By | File uses symbols defined in selected file |
| `•` | Depends On | Specific symbol (function/type/var) |

## Output

Output includes:
1. All directly selected files
2. Dependency-expanded files (if enabled)
3. Files marked with `all(*)` in any category

Sorted alphabetically, prefixed with `./`.

### Loading Selections

`Ctrl+L` loads from catalog file. Supported patterns:

| Pattern | Matches |
|---------|---------|
| `./path/to/file.go` | Exact file |
| `cmd/*` or `cmd/**` | All files recursively under `cmd/` |
| `*_test.go` | All test files |
| `pkg/*.go` | Glob match in directory |

Lines starting with `#` are comments.

## Header Stats

The header bar displays:
- **Deps:** Dependency expansion status and depth limit
- **Output:** Total file count for output
- **Size:** Total size (with dependency size if expansion enabled)

Size displays warning color when exceeding 300KB.