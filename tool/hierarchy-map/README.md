# hierarchy-map

Terminal TUI for selecting Go codebase subsets as LLM context. Indexes files by `@lixen:` annotations, resolves local import dependencies, and performs AST analysis to visualize symbol-level coupling.

## Installation

It lives in the `vif` module. From the repository root:

```bash
make tools                 # builds bin/hierarchy-map alongside the other tools
go build -o bin/hierarchy-map ./tool/hierarchy-map
```

**Optional:** `ripgrep` (`rg`) for content search; without it `/` matches file paths instead.

## Usage
```bash
# Run in project root, writes to './catalog.txt'
./bin/hierarchy-map

# Custom output file
./bin/hierarchy-map -o context.txt
```

Minimum terminal size: 120x24. The index covers `.go` files other than `_test.go`, skipping `vendor/`, `testdata/` and dot directories.

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

Categories are arbitrary names discovered at index time. Multiple `// @lixen:` lines accumulate. Whitespace ignored. vif's own sources carry no annotations, so in this repository the HIERARCHY pane is empty and selection works through the file tree and the dependency panes.

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
| `Ctrl+L` | Load selection from the output file |

### Navigation (All Panes)

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Move cursor |
| `h`/`l`, `←`/`→` | Collapse / Expand |
| `H` / `L` | Collapse / Expand all |
| `0` / `$`, `Home` / `End` | Jump to start / end |
| `PgUp` / `PgDn` | Page scroll |
| `Enter` | Open the file viewer (tree pane); expand (dependency panes) |

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
| `/` | Content search with ripgrep; a path substring match without it |
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
| `d` | Toggle dependency expansion (on at start) |
| `+` (or `=`) / `-` | Adjust expansion depth (1-5, default 2) |
| `r` | Reindex codebase |

### File Viewer

Press `Enter` on a file in the tree pane to open the viewer.

| Key | Action |
|-----|--------|
| `q` / `Esc` | Close viewer |
| `j`/`k`, `↑`/`↓` | Move cursor |
| `Ctrl+U` / `Ctrl+D`, `PgUp` / `PgDn` | Half page up / down |
| `g` / `G`, `Home` / `End` | First / last line |
| `/` | Search |
| `n` / `N` | Next / Previous match |
| `o` | Toggle fold at cursor |
| `h` / `l`, `←` / `→` | Collapse / Expand fold |
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
| `[x]` | Directly selected (a directory: all its files) |
| `[o]` | Directory with some of its files selected; `[N/M]` counts them |
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
| `*_gen.go` | A slash-free pattern also matches the file name anywhere |
| `pkg/*.go` | Glob match in directory |

Lines starting with `#` are comments.

## Header Stats

The header bar displays:
- **Deps:** `N (+depth)`, the dependency-expanded file count and depth limit, or `OFF`
- **Output:** Total file count for output
- **Size:** Total size (with dependency size if expansion enabled)

Size displays warning color when exceeding 300KB.