# lixen-map

Terminal TUI for selecting Go codebase subsets as LLM context. Indexes files by `@lixen:` annotations with arbitrary category names, resolves local import dependencies, and performs lightweight AST analysis to visualize coupling.

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

| Format         | Example                            | Description                        |
|----------------|------------------------------------|------------------------------------|
| 2-level        | `#category{group(tag1,tag2)}`      | Group with direct tags             |
| 3-level        | `#category{group[mod(tag1),mod2]}` | Group with modules containing tags |
| Mixed          | `#cat{grp1(t1),grp2[m(t2)]}`       | Both formats in same category      |
| Multi-category | `#focus{...},#interact{...}`       | Multiple categories per line       |
| Always include | `#focus{all(*)}`                   | File included in every output      |

Categories are arbitrary names discovered at index time. Common conventions:
- `#focus` - Classification tags (what the file IS)
- `#interact` - Relationship tags (what the file USES/AFFECTS)

Multiple `// @lixen:` lines accumulate. Whitespace ignored.

## Interface Layout

Four-pane split view:

| Pane 1 (Left)   | Pane 2           | Pane 3       | Pane 4 (Right) |
|-----------------|------------------|--------------|----------------|
| LIXEN: category | PACKAGES / FILES | DEPENDED BY  | DEPENDS ON     |
| Category tags   | File tree        | Reverse deps | Forward deps   |

- **LIXEN**: Shows groups/modules/tags for current category.
- **PACKAGES / FILES**: Directory tree with file selection.
- **DEPENDED BY**: Files that import the currently selected file/package.
    - **Smart Coupling**: Distinguishes between files that simply *import* the package vs files that *actively use* symbols defined in the selected file.
    - **Sorting**: Files with active usage are sorted to the top.
- **DEPENDS ON**: Local packages/symbols the currently selected file imports.
    - **Symbol Drill-down**: Expandable to show specific functions/types used.

Minimum terminal size: 120x24

All panes start collapsed. Use `l` or `→` to expand, `L` to expand all.

## Key Bindings

Press `?` in any view to open the full key binding help overlay.

### Global Keys

| Key         | Action                   |
|-------------|--------------------------|
| `Tab`       | Next pane                |
| `Shift+Tab` | Previous pane            |
| `[` / `]`   | Previous / Next category |
| `?`         | Toggle help overlay      |
| `Ctrl+Q`    | Quit                     |

### Navigation (Lixen & Tree Panes)

| Key                   | Action              |
|-----------------------|---------------------|
| `j`/`k`, `↑`/`↓`      | Move cursor         |
| `h`/`l`, `←`/`→`      | Collapse/expand     |
| `H`/`L`               | Collapse/expand all |
| `0`/`$`, `Home`/`End` | Jump start/end      |
| `PgUp`/`PgDn`         | Page scroll         |

### Selection

| Key     | Action                    |
|---------|---------------------------|
| `Space` | Toggle selection          |
| `a`     | Select all visible        |
| `c`     | Clear all selections      |
| `F`     | Select all filtered files |

### Filtering

| Key   | Action                           |
|-------|----------------------------------|
| `f`   | Toggle filter on cursor item     |
| `/`   | Content search (ripgrep)         |
| `m`   | Cycle mode: OR → AND → NOT → XOR |
| `Esc` | Clear filter                     |

### Filter Modes

| Mode | Behavior                                   |
|------|--------------------------------------------|
| OR   | Union - adds to existing filter            |
| AND  | Intersection - narrows existing filter     |
| NOT  | Subtraction - removes from existing filter |
| XOR  | Symmetric difference - toggles membership  |

### Dependency Panes

| Key            | Action                           |
|----------------|----------------------------------|
| `Enter` or `l` | Navigate to file/package in tree |
| `L`            | Expand all headers               |
| `H`            | Collapse all headers             |

### Dependencies & Output

| Key      | Action                             |
|----------|------------------------------------|
| `d`      | Toggle dependency expansion        |
| `+`/`-`  | Adjust expansion depth (1-5)       |
| `r`      | Preview output files               |
| `Ctrl+S` | Write output file                  |
| `Ctrl+L` | Load selection from file           |
| `Ctrl+Y` | Copy output to clipboard (wl-copy) |

### Editing

| Key      | Action                                        |
|----------|-----------------------------------------------|
| `e`      | Edit tags for file at cursor (tree pane only) |
| `Ctrl+E` | Batch edit tags for all selected files        |
| `r`      | Re-index entire codebase                      |

## Batch Tag Editor

Press `Ctrl+E` with files selected to open the batch tag editor overlay. This allows editing tags across multiple files simultaneously.

### Layout

```
╔══════════════════════════════════════════════════════════════════════════╗
║ BATCH EDIT (12 files)                    Ctrl+S:save  Esc:cancel  i:new  ║
╠════════════════════════╦═══════════════╦═════════════════════════════════╣
║ TAG TREE               ║ INFO          ║ FILES                           ║
║ ────────────────────── ║ ───────────── ║ ─────────────────────────────── ║
║ ▼ #focus               ║ Selected:     ║ term.go        render.go        ║
║   ▼ sys                ║ #focus        ║ input.go       buffer.go        ║
║     [x]    term        ║               ║ drain.go       collision.go     ║
║     [o]→[x] io         ║ Changes:      ║ state.go       audio.go         ║
║     [ ]→[x] render     ║ +3 additions  ║                                 ║
║   ▶ game               ║ 2 files       ║                                 ║
║ ▶ #interact            ║               ║                                 ║
╚════════════════════════╩═══════════════╩═════════════════════════════════╝
```

### Visual Indicators

| Symbol         | Meaning                                   |
|----------------|-------------------------------------------|
| `[x]`          | All selected files have this item         |
| `[o]`          | Some selected files have this item        |
| `[ ]`          | No selected files have this item          |
| `→[x]`         | Pending: will be added to all files       |
| `→[ ]`         | Pending: will be removed from all files   |
| `→[o]`         | Pending: partial change                   |
| Red filename   | File had parse error, excluded from edits |
| Green filename | File matches item at cursor               |

### Key Bindings

| Key                   | Action                         |
|-----------------------|--------------------------------|
| `j`/`k`, `↑`/`↓`      | Move cursor                    |
| `h`/`l`, `←`/`→`      | Collapse/expand                |
| `H`/`L`               | Collapse/expand all            |
| `0`/`$`, `Home`/`End` | Jump start/end                 |
| `PgUp`/`PgDn`         | Page scroll                    |
| `Space`               | Toggle selection for all files |
| `i`                   | Add new item at cursor level   |
| `Ctrl+S`              | Save all changes               |
| `Esc`                 | Cancel and discard changes     |

### Toggle Behavior

Pressing `Space` on an item cycles through states:

| Current State     | Action | Result                 |
|-------------------|--------|------------------------|
| `[ ]` (none have) | Toggle | Add to all files       |
| `[o]` (some have) | Toggle | Add to remaining files |
| `[x]` (all have)  | Toggle | Remove from all files  |

Toggling a category/group/module affects all tags within it.

### Adding New Items

Press `i` to add a new item at the same level as the cursor:

- Cursor on category → add new category
- Cursor on group → add new group in same category
- Cursor on module → add new module in same group
- Cursor on tag → add new tag in same module/group

New tags are automatically marked as pending addition for all valid files.

### Write Behavior

`Ctrl+S` writes changes to all files atomically:

1. Computes final tag state for each file
2. Generates canonical `@lixen:` content (alphabetically sorted)
3. Writes to each file, replacing existing `@lixen:` lines
4. Reports success/failure count
5. Triggers full reindex on success

Files with parse errors are excluded from writes. If any write fails, a partial failure is reported.

### Tag Syntax Reference

The batch editor respects existing tag format conventions:

| Format  | Syntax                           | Example                             |
|---------|----------------------------------|-------------------------------------|
| 2-level | `#category{group(tag1,tag2)}`    | `#focus{sys(term,io)}`              |
| 3-level | `#category{group[module(tag1)]}` | `#focus{sys[term(vt100)]}`          |
| Mixed   | Both formats in same category    | `#focus{sys(io),game[drain(tick)]}` |

## Visual Indicators

**Tree Pane (Files):**

| Symbol | Meaning                           |
|--------|-----------------------------------|
| `[x]`  | Directly selected                 |
| `[+]`  | Included via dependency expansion |
| `[ ]`  | Not selected                      |

**Lixen Pane (Tags):**

| Symbol | Meaning                            |
|--------|------------------------------------|
| `[x]`  | All files with this item selected  |
| `[o]`  | Some files with this item selected |
| `[ ]`  | No files with this item selected   |

**Dependency Panes (Analysis):**

| Symbol  | Pane        | Meaning                                                      |
|---------|-------------|--------------------------------------------------------------|
| `★`     | Depended By | **Active Usage**: File uses symbols defined in selected file |
| `•`     | Depends On  | **Symbol**: Specific function/type/var used                  |
| `▼`/`▶` | Headers     | Package or File group (expandable)                           |

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

## Selection Loading

Use `Ctrl+L` to load selections from the catalog ile (default `catalog.txt`, or path specified via -o`).

**Supported patterns:**

| Pattern             | Matches                            |
|---------------------|------------------------------------|
| `./path/to/file.go` | Exact file                         |
| `cmd/*`             | All files recursively under `cmd/` |
| `*_test.go`         | All test files project-wide        |
| `pkg/*.go`          | Files matching glob in `pkg/`      |

Lines starting with `#` are treated as comments. Loading clears existing selection before applying.

## Dependency Expansion

When enabled (`d` to toggle), selected files automatically include their transitive local dependencies up to the configured depth (`+`/`-` to adjust, range 1-5).

Dependency-expanded files show `[+]` in the tree pane and are counted separately in the header stats.