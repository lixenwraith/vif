# focus-catalog

Interactive TUI for selecting Go codebase subsets as LLM context. Indexes packages by focus tags, resolves local import dependencies, supports keyword filtering via ripgrep.

## Installation
```bash
go build -o focus-catalog ./cmd/focus-catalog
```

**Optional:** `ripgrep` (`rg`) for keyword search.

## Usage
```bash
# Run in project root, writes to './catalog.txt'
./focus-catalog

# Custom output file
./focus-catalog -o context.txt

# Use file option of combine.sh to generate concatenated code context
./combine.sh -r -f catalog.txt -o combined.go.txt
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

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Navigate |
| `Space` | Toggle package selection |
| `/` | Keyword search (requires `rg`) |
| `Enter` | Output file list, exit |
| `Escape` | Clear filter / cancel input |
| `g` | Cycle group filter |
| `d` | Toggle dependency expansion |
| `+`/`-` | Adjust expansion depth (1-5) |
| `a` | Select all visible |
| `c` | Clear selection |
| `i` | Toggle case sensitivity |
| `p` | Preview output files |
| `q` | Quit without output |

## Features

**Dependency Expansion:** Selecting a package auto-includes its local imports (transitive, configurable depth).

**Group Filtering:** Press `g` to cycle through tag groups, showing only packages with matching tags.

**Keyword Search:** `/pattern<Enter>` filters to packages containing files matching the pattern.

**Output:** Writes `./path/to/file.go` lines to stdout, compatible with `combine.sh -f`.

## Output Format
```
./systems/drain.go
./systems/spawn.go
./events/types.go
```

Files with `#all` tag always included. Sorted alphabetically.