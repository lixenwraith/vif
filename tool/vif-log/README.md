# vif-log

A high-performance, terminal-based JSONL diagnostic log viewer engineered for the `vi-fighter` ECS game engine.

Designed to parse and navigate high-frequency ECS event streams and state snapshots with minimal overhead. Features zero-allocation index passes, lazy-loaded line evaluation, and asynchronous chronological merging of multiple log sources.

It reads both streams the game writes:

* **`vif-log-*.jsonl`** — the diagnostic log: levelled, scoped records with a `sub` tag and a `fields.msg` discriminator.
* **`vif-jrn-*.jsonl`** — the replay journal: one `sub:"journal"` record per replayable event, punctuated by `sub:"anchor"` headers. Written by a second logger instance outside the session's level and scope, so a capture cannot be silenced.

The two share an envelope (`time`, `level`, `sub`, `run`, `tick`, `frame`, `fields`), so they open together and merge into one chronological view — which is how a journal record is read against the diagnostics of the tick that produced it.

## Core Architecture

* **Zero-Alloc Indexing**: Hand-rolled RFC3339Nano parser and JSON tokenizer (`internal/logfile`). Constructs a pointer-free 48-byte `Meta` struct per record. Raw JSON strings are interned to bitset-friendly integer IDs.
* **Asynchronous Render Pipeline**: Background indexers publish lock-free slice headers to the render thread. The UI remains responsive during multi-gigabyte ingestion.
* **Multi-Source K-Way Merge**: Loads multiple `.jsonl` files (e.g. cross-network client/server logs, or a log and its journal) and performs a stable chronological merge using nanosecond timestamps without mutating the row index.
* **Smart ECS Snapshotting**: High-frequency telemetry (ECS component states pushed per-tick) are automatically collapsed into single navigable rows (`Filter.Collapse`), expanding on demand.
* **Deferred Evaluation Stack**: The filter chain evaluates index-resident predicates (Tick, Run, Subsystem, Level, Domain) first. Costly operations (Regex over raw JSON fields) only trigger for surviving records that enter the sliding read window.

## Journal Support

A journal record is not shaped like a diagnostic one, and the viewer indexes the difference rather than rendering it as a wall of key-value text.

* **Event names in the `msg` column.** Journal records carry no `fields.msg`; `fields.ev` names them, so the discriminator resolves `msg` → `type` → `ev`. Every column-scoped operation follows: `msg:` filters, `/msg` searches, sorting and `f`/`F` follow all work on event names. The anchor carries no discriminator at all, so its `sub` stands in for one.
* **Domain attribution.** `fields.domain` is resolved during the index pass into the `Meta` row, at no cost in memory — it fits in the struct's existing tail padding. Selecting a domain is therefore a byte comparison per record and never reads a line.
* **Anchors as landmarks.** An anchor is the journal's self-describing header, re-emitted on a cadence so a rotated file replays standalone. `n` / `N` jump between landmarks — stat snapshot heads in a diagnostic log, anchors in a capture — and an anchor row is marked `⚑`.
* **Dropped-record detection.** `jseq` is dense by construction, so any step other than `+1` is a lost record. The status bar reports the break count as `jgaps`; a non-zero count means the capture will not replay as it stands.
* **Payloads stay on one row.** A payload is TOML and carries newlines. The list and search text flatten control characters in place; the detail pane keeps them and wraps, so the payload reads as the document it is.
* **Anchor durations.** `tick_ns` renders through the duration table (`50ms`, not `50000000`).

### Domain

Domains are the journal's replication scope: **shared** state is identical on every instance, **player** state is local and never replicated. The viewer treats this as three states rather than a mask, because a record belongs to exactly one domain:

| State | Shows |
| :--- | :--- |
| `both` | every record (the default) |
| `shared` | records stamped `domain=shared` |
| `player` | records stamped `domain=player` |

`D` cycles the three. Selecting a domain excludes records that carry none — anchors and the whole diagnostic log — because a record without a domain is not evidence about either one.

Two pieces of the frame carry the state, alongside the existing bars rather than in place of them:

* A **strip** in the header, beside the level strip: `s p`, filled for the admitted domains, outlined for the excluded one. It appears only once a journal is loaded.
* A **gutter** in the record list, between the level and tick columns, marking each row `s` or `p`. It too is claimed only when the loaded set has a journal in it, so a diagnostic log lays out exactly as it did before.

The status bar reports an active selection as `dom:shared` in the filter stack summary, and `jgaps` joins `bad` on the right-hand pairs.

## Build & Run

Requires Go 1.26+ (Wayland environment natively supported via underlying TUI library).

`vif-log` lives in the `vi-fighter` module. From the repository root:

```bash
make tools                 # builds bin/vif-log alongside the other tools
go build -o bin/vif-log ./tool/vif-log/cmd/vif-log
```

## Usage

```bash
# Open a specific log file or directory
vif-log path/to/run.jsonl
vif-log ./logs/

# Multi-file merge
vif-log server.jsonl client.jsonl

# A run and its journal, read together
vif-log vif-log-260829-211523.jsonl vif-jrn-260829-211523.jsonl

# Pre-seed filter stack
vif-log -f level:>=WARN -f sub:^(fsm|event)$ -f tick:1000-5000 ./logs/
vif-log -f dom:player -f msg:^EventNugget vif-jrn-260829-211523.jsonl
```

### Predicates (Filters)

Filters stack. Use `\` in the UI or `-f` via CLI.
* `level`: Exact match (`IWE`) or threshold (`>=WARN`).
* `sub` / `msg`: Smart-case regex evaluated against the interned vocabulary.
* `tick` / `run`: Numeric spans (`100-200`, `150-`, `-50`).
* `dom`: Journal domain — `both`, `shared` or `player`.
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
| `n` / `N` | Jump to next/prev landmark: stat snapshot or journal anchor |
| `Ctrl+l` | Force redraw |

### Search & Filtering
| Key | Action |
| :--- | :--- |
| `/` | Regex search in the currently focused column |
| `\` | Add/replace a filter in the stack (e.g., `msg:transition`) |
| `f` / `F` | Follow: Jump to next/prev record with identical `sub`/`msg`/context |
| `D` | Cycle journal domain: both / shared / player |
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
