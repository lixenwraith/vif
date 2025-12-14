# lixen-map

Terminal TUI for selecting Go codebase subsets as LLM context. Indexes files by `@lixen:` annotations with arbitrary category names, resolves local import dependencies.

## Installation
```bash
go build -o lixen-map .
```

**Optional:** `ripgrep` (`rg`) for content search.

## Usage
```bash
# Run in project root, writes to './catalog.txt'
./lixen-map

# Custom output file
./lixen-map -o context.txt
```

## Lixen Annotations

Declare in Go files before `package` statement:
```go
// @lixen: #focus{sys[term,io],game[drain,collision]},#interact{init[cursor],state[gold]}
package systems
```

### Syntax

| Format | Example | Description |
|--------|---------|-------------|
| 2-level | `#category{group(tag1,tag2)}` | Group with direct tags |
| 3-level | `#category{group[mod(tag1),mod2]}` | Group with modules containing tags |
| Mixed | `#cat{grp1(t1),grp2[m(t2)]}` | Both formats in same category |
| Multi-category | `#focus{...},#interact{...}` | Multiple categories per line |
| Always include | `#focus{all(*)}` | File included in every output |

Categories are arbitrary names discovered at index time. Common conventions:
- `#focus` - Classification tags (what the file IS)
- `#interact` - Relationship tags (what the file USES/AFFECTS)

Multiple `// @lixen:` lines accumulate. Whitespace ignored.

## Interface Layout

Four-pane split view:

| Pane 1 (Left) | Pane 2 | Pane 3 | Pane 4 (Right) |
|---------------|--------|--------|----------------|
| LIXEN: category | PACKAGES / FILES | DEPENDED BY | DEPENDS ON |
| Category tags | File tree | Reverse deps | Forward deps |

- **LIXEN**: Shows groups/modules/tags for current category
- **PACKAGES / FILES**: Directory tree with file selection
- **DEPENDED BY**: Packages that import current file's package
- **DEPENDS ON**: Local packages current file imports

Minimum terminal size: 120x24

All panes start collapsed. Use `l` or `→` to expand, `L` to expand all.

## Key Bindings

Press `?` in any view to open the full key binding help overlay.

### Global Keys

| Key | Action |
|-----|--------|
| `Tab` | Next pane |
| `Shift+Tab` | Previous pane |
| `[` / `]` | Previous / Next category |
| `?` | Toggle help overlay |
| `Ctrl+Q` | Quit |

### Navigation (Lixen & Tree Panes)

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Move cursor |
| `h`/`l`, `←`/`→` | Collapse/expand |
| `H`/`L` | Collapse/expand all |
| `0`/`$`, `Home`/`End` | Jump start/end |
| `PgUp`/`PgDn` | Page scroll |

### Selection

| Key | Action |
|-----|--------|
| `Space` | Toggle selection |
| `a` | Select all visible |
| `c` | Clear all selections |
| `F` | Select all filtered files |

### Filtering

| Key | Action |
|-----|--------|
| `f` | Toggle filter on cursor item |
| `/` | Content search (ripgrep) |
| `m` | Cycle mode: OR → AND → NOT → XOR |
| `Esc` | Clear filter |

### Filter Modes

| Mode | Behavior |
|------|----------|
| OR | Union - adds to existing filter |
| AND | Intersection - narrows existing filter |
| NOT | Subtraction - removes from existing filter |
| XOR | Symmetric difference - toggles membership |

### Dependency Panes

| Key | Action |
|-----|--------|
| `Enter` or `l` | Navigate to package in tree |

### Dependencies & Output

| Key | Action |
|-----|--------|
| `d` | Toggle dependency expansion |
| `+`/`-` | Adjust expansion depth (1-5) |
| `p` | Preview output files |
| `Ctrl+S` | Write output file |
| `Ctrl+Y` | Copy output to clipboard (wl-copy) |

### Editing

| Key | Action |
|-----|--------|
| `e` | Edit tags for file at cursor (tree pane only) |
| `r` | Re-index entire codebase |

## Selection Indicators

**Tree Pane (files):**

| Symbol | Meaning |
|--------|---------|
| `[x]` | Directly selected |
| `[+]` | Included via dependency expansion |
| `[ ]` | Not selected |

**Lixen Pane (tags/groups/modules):**

| Symbol | Meaning |
|--------|---------|
| `[x]` | All files with this item selected |
| `[o]` | Some files with this item selected |
| `[ ]` | No files with this item selected |

## Category Switching

When multiple categories exist in the indexed codebase, use `[` and `]` to cycle between them. The LIXEN pane header shows the current category name.

Category state (cursor position, expanded groups) is preserved per-category. When switching categories, if the current file at tree cursor has tags in the new category, the lixen pane positions to show those tags.

## Tag Editor

Press `e` on a file in the tree pane to edit its `@lixen:` annotations inline:

- Pre-fills current content in canonical format
- Full cursor navigation (←/→, Home/End, Ctrl+A/E)
- Line editing (Backspace, Delete, Ctrl+K, Ctrl+U, Ctrl+W)
- `Enter` to save, `Esc` to cancel
- Atomic writes (temp file + rename)
- Auto re-indexes after save

### Tag Syntax in Editor
```
#category{group(tag1,tag2),group2[mod(tag)]}
```

Multiple categories separated by commas:
```
#focus{sys(term,io)},#interact{state(audio)}
```

## Output

Files with `all(*)` in any category always included. Output sorted alphabetically.

Use `Ctrl+S` to write to file, `Ctrl+Y` to copy to clipboard (requires `wl-copy`).

## Dependency Expansion

When enabled (`d` to toggle), selected files automatically include their transitive local dependencies up to the configured depth (`+`/`-` to adjust, range 1-5).

Dependency-expanded files show `[+]` in the tree pane and are counted separately in the header stats.