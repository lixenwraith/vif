# vif-log

A high-performance, terminal-based JSONL diagnostic log viewer engineered for the `vi-fighter` ECS game engine. 

Designed to parse and navigate high-frequency ECS event streams and state snapshots with minimal overhead. Features zero-allocation index passes, lazy-loaded line evaluation, and asynchronous chronological merging of multiple log sources.

## Core Architecture

* **Zero-Alloc Indexing**: Hand-rolled RFC3339Nano parser and JSON tokenizer (`internal/logfile`). Constructs a pointer-free 48-byte `Meta` struct per record. Raw JSON strings are interned to bitset-friendly integer IDs.
* **Asynchronous Render Pipeline**: Background indexers publish lock-free slice headers to the render thread. The UI remains responsive during multi-gigabyte ingestion.
* **Multi-Source K-Way Merge**: Loads multiple `.jsonl` files (e.g., cross-network client/server logs) and performs a stable chronological merge using nanosecond timestamps without mutating the row index.
* **Smart ECS Snapshotting**: High-frequency telemetry (ECS component states pushed per-tick) are automatically collapsed into single navigable rows (`Filter.Collapse`), expanding on demand.
* **Deferred Evaluation Stack**: The filter chain evaluates index-resident predicates (Tick, Run, Subsystem, Level) first. Costly operations (Regex over raw JSON fields) only trigger for surviving records that enter the sliding read window.

## Build & Run

Requires Go 1.26+ (Wayland environment natively supported via underlying TUI library).

```bash
go build -o vif-log ./cmd/vif-log
```

## Usage

```bash
# Open a specific log file or directory
vif-log path/to/run.jsonl
vif-log ./logs/

# Multi-file merge
vif-log server.jsonl client.jsonl

# Pre-seed filter stack
vif-log -f level:>=WARN -f sub:^(fsm|event)$ -f tick:1000-5000 ./logs/
```

### Predicates (Filters)

Filters stack. Use `\` in the UI or `-f` via CLI.
* `level`: Exact match (`IWE`) or threshold (`>=WARN`).
* `sub` / `msg`: Smart-case regex evaluated against the interned vocabulary.
* `tick` / `run`: Numeric spans (`100-200`, `150-`, `-50`).
* `fields`: Smart-case regex evaluated over the parsed JSON fields.
* `find`: Column-scoped regex search (evaluates against time, tick, sub, msg, or fields).

## Keybindings

### Navigation & UI
| Key | Action |
| :--- | :--- |
| `j` / `k` | Move cursor down/up |
| `gg` / `G` | Jump to first/last record |
| `Ctrl+d` / `Ctrl+u` | Half-page down/up |
| `Tab` / `Shift+Tab`| Cycle column focus |
| `s` | Sort by focused column (asc/desc/off) |
| `J` / `K` | Scroll detail pane down/up |
| `Enter` | Expand/collapse ECS stat snapshot |
| `n` / `N` | Jump to next/prev snapshot head |
| `Ctrl+l` | Force redraw |

### Search & Filtering
| Key | Action |
| :--- | :--- |
| `/` | Regex search in the currently focused column |
| `\` | Add/replace a filter in the stack (e.g., `msg:transition`) |
| `f` / `F` | Follow: Jump to next/prev record with identical `sub`/`msg`/context |
| `t` `d` `i` `w` `e` `p` `b` | Toggle visibility of exact log levels |
| `1`-`5` | Set minimum level threshold (1=TRACE, 5=ERROR) |
| `<` / `>` | Lower/Raise level threshold |
| `Esc` | Clear search and dynamic filters |

### Buffer & File Management
| Key | Action |
| :--- | :--- |
| `Space` | Toggle pin on current record |
| `P` | Toggle pinned-only view |
| `C` | Clear all pins |
| `o` | Open file browser (supports multi-select with `Space`) |
| `x` | Export current view (or pins) to a new `.jsonl` file |
