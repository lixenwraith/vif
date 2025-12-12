# lixen-map

Terminal TUI for selecting Go codebase subsets as LLM context. Indexes files by `@lixen:` annotations (`#focus` classification, `#interact` relationship tags), resolves local import dependencies.

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

| Syntax                        | Meaning                                        |
|-------------------------------|------------------------------------------------|
| `#focus{group[tag1,tag2]}`    | Classification tags (what the file IS)         |
| `#interact{group[tag1,tag2]}` | Relationship tags (what the file USES/AFFECTS) |
| `#focus{all[*]}`              | Include file in every output                   |

Multiple `// @lixen:` lines accumulate. Whitespace ignored.

## Interface Layout

Three-pane split view:

| Left Pane           | Center Pane       | Right Pane           |
|---------------------|-------------------|----------------------|
| Packages/Files tree | Focus Groups/Tags | Interact Groups/Tags |

Minimum terminal size: 120x24

## Key Bindings

### Navigation

| Key                   | Action              |
|-----------------------|---------------------|
| `j`/`k`, `↑`/`↓`      | Move cursor         |
| `h`/`l`, `←`/`→`      | Collapse/expand     |
| `H`/`L`               | Collapse/expand all |
| `0`/`$`, `Home`/`End` | Jump start/end      |
| `PgUp`/`PgDn`         | Page scroll         |
| `Tab`                 | Cycle pane focus    ||

### Selection

| Key     | Action                                                        |
|---------|---------------------------------------------------------------|
| `Space` | Toggle selection                                              |
| `a`     | Select all in pane                                            |
| `c`     | Clear all selections                                          |
| `F`     | Select all filtered files <br/>(transfer filter to selection) |

### Filtering

Two-key sequences for targeted filtering:

| Sequence | Action                            |
|----------|-----------------------------------|
| `ff`     | Toggle filter on cursor item      |
| `fg`     | Filter by focus groups (query)    |
| `ft`     | Filter by focus tags (query)      |
| `if`     | Toggle filter on cursor item      |
| `ig`     | Filter by interact groups (query) |
| `it`     | Filter by interact tags (query)   |
| `/`      | Content search (ripgrep)          |
| `m`      | Cycle mode: OR → AND → NOT → XOR  |
| `Esc`    | Clear filter                      |


### Filter Modes

| Mode | Behavior                                   |
|------|--------------------------------------------|
| OR   | Union - adds to existing filter            |
| AND  | Intersection - narrows existing filter     |
| NOT  | Subtraction - removes from existing filter |
| XOR  | Symmetric difference - toggles membership  |

### Views

| Key     | Action                            |
|---------|-----------------------------------|
| `Enter` | Open mindmap view (from any pane) |
| `p`     | Preview output files              |

### Dependencies & Output

| Key      | Action                       |
|----------|------------------------------|
| `d`      | Toggle dependency expansion  |
| `+`/`-`  | Adjust expansion depth (1-5) |
| `Ctrl+S` | Write output file            |
| `Ctrl+Q` | Quit                         |

### Editing

| Key | Action                                        |
|-----|-----------------------------------------------|
| `e` | Edit tags for file at cursor (left pane only) |
| `r` | Re-index entire tree                          |

## Selection Indicators

**Left Pane (files):**

| Symbol | Meaning                           |
|--------|-----------------------------------|
| `[x]`  | Directly selected                 |
| `[+]`  | Included via dependency expansion |
| `[ ]`  | Not selected                      |

**Center/Right Panes (tags/groups):**

| Symbol | Meaning                                 |
|--------|-----------------------------------------|
| `[x]`  | All files with this tag/group selected  |
| `[o]`  | Some files with this tag/group selected |
| `[ ]`  | No files with this tag/group selected   |

## Mindmap View

Press `Enter` to open contextual mindmap from any pane:

- **From Left Pane:** Hierarchical view of package/directory structure
- **From Center Pane:** Files containing the focus tag/group
- **From Right Pane:** Files containing the interact tag/group

Files display both `#focus{...}` and `#interact{...}` annotations.

**Mindmap Controls:**

| Key         | Action                     |
|-------------|----------------------------|
| `j`/`k`     | Navigate                   |
| `Space`     | Toggle selection           |
| `a`/`c`     | Select all / Clear visible |
| `0`/`$`     | Jump to start/end          |
| `f`         | Filter at cursor           |
| `F`         | Select filtered files      |
| `/`/`t`/`g` | Search content/tags/groups |
| `Enter`     | Open dive view             |
| `q`         | Return to main view        |

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

## Output

Files with `#focus{all[*]}` always included. Sorted alphabetically.