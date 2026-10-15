# vif-log

A terminal viewer for vif's JSONL logs. It indexes each file once, evaluates
filters incrementally, and merges several files into one chronological view.

It reads both streams the game writes:

* **`vif-log-*.jsonl`** — the diagnostic log: levelled, scoped records with a `sub` tag and a `fields.msg` discriminator.
* **`vif-jrn-*.jsonl`** — the replay journal: one `sub:"journal"` record per replayable event, with `anchor` headers and `capture`, `digest` and `end` records (`internal/event/journal_sink.go`). A second logger instance writes it outside the session's level and scope, so a capture cannot be silenced.

The two share an envelope (`time`, `level`, `sub`, `run`, `tick`, `fields`), so they open together and merge into one chronological view — which is how a journal record is read against the diagnostics of the tick that produced it.

## Architecture

* **Index pass**: a hand-rolled RFC3339Nano parser and JSON tokenizer (`internal/logfile`) build a pointer-free 48-byte `Meta` row per record; `sub` and `msg` strings are interned to integer IDs.
* **Background indexing**: the index publishes to the render loop without locks. A single file publishes as it scans, so the UI is usable while a large one loads; several files scan in parallel and publish once, merged.
* **Multi-source merge**: several `.jsonl` files (a client and a server, or a log and its journal) merge stably by nanosecond timestamp without rewriting the row index.
* **Snapshot collapse**: the status registry's periodic `sub:"stat"` snapshots (around 40 records each) collapse into one navigable row (`filter.Collapse`); `Enter` expands one.
* **Cost-ordered filters**: predicates that read only the index row (`level`, `snap`, `dom`, `tick`, `run`, `sub`, `msg`, pins) run first; `fields` and an all-column or `fields` search read the line through a per-source sliding window, only for records that survive. The pass is incremental: 40 ms of filtering on a filter change, then 8 ms per tick.

## Journal Support

A journal record is not shaped like a diagnostic one, and the viewer indexes the difference rather than rendering it as a wall of key-value text.

* **Event names in the `msg` column.** Journal records carry no `fields.msg`; `fields.ev` names them, so the discriminator resolves `msg` → `type` → `ev`. Every column-scoped operation follows: `msg:` filters, `/msg` searches, sorting and `f`/`F` follow all work on event names. The `anchor`, `capture`, `digest` and `end` records carry no discriminator at all, so each one's `sub` stands in for it.
* **Domain attribution.** `fields.domain` is resolved during the index pass into the `Meta` row, at no cost in memory — it fits in the struct's tail padding. Selecting a domain is therefore a byte comparison per record and never reads a line.
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

`D` cycles the three. Selecting a domain excludes records that carry none — the journal's anchor, capture, digest and end records and the whole diagnostic log — because a record without a domain is not evidence about either one.

Two pieces of the frame carry the state:

* A **strip** in the header, beside the level strip: `s p`, filled for the admitted domains, outlined for the excluded one. It appears once a journal is loaded, or at once when `-f dom:` selected a domain.
* A **gutter** in the record list, between the level and tick columns, marking each row `s` or `p`. It is claimed only when the loaded set has a journal in it, so a diagnostic log alone gets no domain column.

The status bar reports an active selection as `dom:shared` in the filter stack summary, and `jgaps` joins `bad` on the right-hand pairs.

## Build & Run

`vif-log` lives in the `vif` module and builds with its toolchain (Go 1.27.2). From the repository root:

```bash
make tools                 # builds bin/vif-log alongside the other tools
go build -o bin/vif-log ./tool/vif-log/cmd/vif-log
```

The terminal must be at least 80x24.

## Usage

```bash
# Open files, or one directory in the file browser (no argument: the working directory)
vif-log path/to/run.jsonl
vif-log ./logs/

# Multi-file merge
vif-log server.jsonl client.jsonl

# A run and its journal, read together
vif-log vif-log-260829-211523.jsonl vif-jrn-260829-211523.jsonl

# Pre-seed filter stack
vif-log -f 'level:>=WARN' -f 'sub:^(fsm|event)$' -f tick:1000-5000 run.jsonl
vif-log -f dom:player -f 'msg:^EventNugget' vif-jrn-260829-211523.jsonl
```

### Predicates (Filters)

Filters stack. Use `\` in the UI or `-f` on the command line.
* `level`: a set of initials (`IWE`) or a threshold (`>=WARN`). Levels are `TRACE DEBUG INFO WARN ERROR`, plus `PROC` (the logger's own records) and `BAD` (malformed lines).
* `sub` / `msg`: smart-case regex over the interned vocabulary.
* `tick` / `run`: a value or an inclusive span (`100`, `100-200`, `150-`, `-50`).
* `dom`: journal domain — `both`, `shared` or `player`.
* `fields`: smart-case regex over the parsed JSON fields.
* `find`: regex search; from `-f` it covers every column, from `/` the focused one.

Snapshot collapse and pins are driven by their keys, not by a spec.

## Keybindings

`?` shows the full table, generated from the same bindings (`internal/keys`).

### Navigation & UI
| Key | Action |
| :--- | :--- |
| `q` / `Ctrl+c` | Quit |
| `?` | Help |
| `j` / `k`, arrows | Move cursor down/up |
| `PgDn` / `PgUp` | Page down/up |
| `Ctrl+d` / `Ctrl+u` | Half-page down/up |
| `gg` / `G`, `Home` / `End` | Jump to first/last record |
| `Tab` / `Shift+Tab`| Cycle column focus |
| `s` | Sort by focused column (asc/desc/off) |
| `J` / `K` | Scroll detail pane down/up |
| `Enter` | Expand/collapse a stat snapshot |
| `n` / `N` | Jump to next/prev landmark: stat snapshot or journal anchor |
| `Ctrl+l` | Force redraw |

### Search & Filtering
| Key | Action |
| :--- | :--- |
| `/` | Regex search in the currently focused column |
| `\` | Add/replace a filter in the stack (e.g., `msg:transition`) |
| `f` / `F` | Follow: jump to next/prev record like this one |
| `D` | Cycle journal domain: both / shared / player |
| `t` `d` `i` `w` `e` `p` `b` | Toggle one level (`p` or `6` PROC, `b` or `7` BAD) |
| `1`-`5` | Hide below a level (1=TRACE, 5=ERROR) |
| `0` | Show all levels |
| `<` / `>` | Lower/raise level threshold |
| `Esc` | Clear the search and the `sub`, `msg`, `tick`, `run` and `fields` filters; levels, domain, snapshot collapse and pins stay |

### Buffer & File Management
| Key | Action |
| :--- | :--- |
| `Space` | Toggle pin on current record |
| `P` | Toggle pinned-only view |
| `C` | Clear all pins |
| `o` | Open file browser (`Space` marks files for a multi-open) |
| `x` | Export the pins, or the current view, to a new `.jsonl` file |
