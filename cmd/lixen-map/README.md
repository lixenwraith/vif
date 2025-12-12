# lixen-map

Interactive TUI for selecting Go codebase subsets as LLM context. Indexes packages by lixen annotations (`#focus` and `#interact` tags), resolves local import dependencies, supports keyword filtering.

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
// @lixen: #focus{arch[ecs,types],game[drain,collision]},#interact{init[cursor],state[gold]}
package systems
```

| Syntax | Meaning |
|--------|---------|
| `#focus{group[tag1,tag2]}` | Classification tags (what the file IS) |
| `#interact{group[tag1,tag2]}` | Relationship tags (what the file USES/AFFECTS) |
| `#focus{all[*]}` | Include file in every output |
| Multiple `// @lixen:` lines | Accumulated |

Whitespace inside annotation content is ignored.

## Interface Layout

Three-pane split view:

| Left Pane | Center Pane | Right Pane |
|-----------|-------------|------------|
| Packages/Files tree | Focus Groups/Tags | Interact Groups/Tags |

Minimum terminal size: 120x24

## Key Bindings

### Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `h` / `←` | Collapse directory/group |
| `l` / `→` | Expand directory/group |
| `PgUp` / `PgDn` | Page up/down |
| `0` / `Home` | Jump to first item |
| `$` / `End` | Jump to last item |
| `H` | Collapse all directories/groups |
| `L` | Expand all directories/groups |
| `Tab` | Cycle pane focus (Left → Center → Right) |

### Selection

| Key | Action |
|-----|--------|
| `Space` | Toggle selection (file, directory, tag, or group) |
| `a` | Select all visible items in current pane |
| `c` | Clear all selections |
| `F` | Select all filtered files (transfer filter to selection) |

### Filtering

Two-key sequences for targeted filtering:

| Sequence | Action |
|----------|--------|
| `fg` | Filter by focus groups (enter query) |
| `ft` | Filter by focus tags (enter query) |
| `ig` | Filter by interact groups (enter query) |
| `it` | Filter by interact tags (enter query) |
| `/` | Filter by content (ripgrep search) |
| `m` | Cycle filter mode (OR → AND → NOT → XOR) |
| `Esc` | Clear active filter |

Filtering highlights matching items without changing selection. Use filtering to preview matches before selecting.

### Filter Modes

| Mode | Behavior |
|------|----------|
| OR | Union - adds to existing filter |
| AND | Intersection - narrows existing filter |
| NOT | Subtraction - removes from existing filter |
| XOR | Symmetric difference - toggles membership |

### Views

| Key | Action |
|-----|--------|
| `Enter` | Open mindmap view (from any pane) |
| `p` | Preview output files |

### Dependencies & Output

| Key | Action |
|-----|--------|
| `d` | Toggle dependency expansion |
| `+`/`-` | Adjust expansion depth (1-5) |
| `Ctrl+S` | Write output file |
| `Ctrl+Q` | Quit |

### Editing

| Key | Action |
|-----|--------|
| `e` | Edit tags for file at cursor (left pane only) |
| `r` | Re-index entire tree |

## Selection Indicators

**Left Pane (files):**

| Symbol | Meaning |
|--------|---------|
| `[x]` | Directly selected |
| `[+]` | Included via dependency expansion |
| `[ ]` | Not selected |

**Center/Right Panes (tags/groups):**

| Symbol | Meaning |
|--------|---------|
| `[x]` | All files with this tag/group selected |
| `[o]` | Some files with this tag/group selected |
| `[ ]` | No files with this tag/group selected |

## Mindmap View

Press `Enter` to open contextual mindmap from any pane:

- **From Left Pane:** Hierarchical view of package/directory structure
- **From Center Pane:** Files containing the focus tag/group
- **From Right Pane:** Files containing the interact tag/group

Files display both `#focus{...}` and `#interact{...}` annotations.

**Mindmap Controls:**

| Key | Action |
|-----|--------|
| `j`/`k` | Navigate |
| `Space` | Toggle selection |
| `a`/`c` | Select all / Clear visible |
| `0`/`$` | Jump to start/end |
| `f` | Filter at cursor |
| `F` | Select filtered files |
| `/`/`t`/`g` | Search content/tags/groups |
| `Enter` | Open dive view |
| `q` | Return to main view |

## Dive View

Press `Enter` on a file in mindmap to see relationships:

- **DEPENDS ON:** Packages this file imports
- **DEPENDED BY:** Packages importing this file's package
- **Focus Box:** File details with tag counts
- **FOCUS LINKS:** Files sharing focus tags
- **INTERACT LINKS:** Files sharing interact tags

Press `Esc` or `q` to return.

## Tag Editor

Press `e` on a file to edit its `@lixen:` annotations inline:

- Pre-fills current content
- `Enter` to save, `Esc` to cancel
- Atomic writes (temp file + rename)
- Auto re-indexes after save

## Output Format

```
./systems/drain.go
./systems/spawn.go
./events/types.go
```

Files with `#focus{all[*]}` always included. Sorted alphabetically.

## Workflow Examples

### Find files by tag intersection

1. Press `ft`, type tag prefix, Enter
2. Press `m` to switch to AND mode
3. Press `ft`, type second tag, Enter
4. Press `F` to select all filtered
5. `Ctrl+S` to output

### Explore package relationships

1. Navigate to package in left pane
2. Press `Enter` for mindmap view
3. Select file, press `Enter` for dive view
4. Review dependencies and tag relationships

### Filter by interact, select by focus

1. Press `it`, type interaction tag, Enter
2. Tab to center pane to see highlighted focus tags
3. Use `Space` to select specific focus groups
4. `Ctrl+S` to output