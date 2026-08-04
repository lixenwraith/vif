# Vi-Fighter Content Corpus

Files in this directory are loaded once at startup and spawned as typeable
glyphs. Nothing here is read again while the game runs. The detailed design and
resolution behavior are documented in
[`doc/content-assets-and-tools.md`](../doc/content-assets-and-tools.md).

## Accepted files

- Extensions: `.txt` and `.toml` at the directory root.
- Files starting with `.` and all subdirectories are skipped.
- Input must be valid UTF-8; stored content is printable ASCII (`0x20`–`0x7e`).
- ANSI escape sequences, controls, and unsupported runes are stripped.
- Lines are capped at 256 runes; glyph placement also crops to map width.
- A file may be at most 4 MiB; a corpus admits up to 256 files and targets a
  32 MiB byte budget.

Rejected eligible files are reported in content telemetry; they do not abort a
directory load unless an explicitly selected source yields no usable content.

## Plain text

Plain `.txt` files use source-oriented grouping:

- blank lines and lines beginning `//`, `#`, or `/*` are dropped;
- leading indentation is measured for block boundaries and omitted from stored
  text; leading tabs count as four columns and interior tabs become one space;
- consecutive usable lines form blocks of 2–5 lines;
- a block ends at five lines or at a top-level indent shift of at least two
  columns; brace depth delays indent-based splitting.

This is a heuristic text shaper, not a language parser.

## Authored TOML

Use schema 1 when block boundaries must be exact:

```toml
schema = 1

[[blocks]]
id = "example"
lines = [
  "first typeable line",
  "second typeable line",
]
```

TOML lines do not use plain-text comment filtering and may form a one-line
block. Safety sanitization still applies. A block longer than five usable lines
is split into consecutive blocks.

## Selection

One accepted source is selected randomly and walked block by block. At EOF,
another source is selected; a single source cycles.

`-f <file>` pins one file and cycles it. `-f <directory>` uses that directory.
Without `-f`, resolution is `./data`, then the user config directory's `data`,
then the embedded tutorial corpus. `-d` skips discovery and forces embedded FSM
and content.

Run `vif -check -f <path>` to validate with the real loader. Verify licensing
before adding third-party source text.
