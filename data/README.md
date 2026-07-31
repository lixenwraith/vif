# Vi-Fighter Content Corpus

Files in this directory are loaded once at startup and spawned as typeable
glyphs. Nothing here is read again while the game runs.

## Format

- Extension: `.txt`. Files starting with `.` are skipped, as are subdirectories.
- Encoding: UTF-8. A file that fails validation is rejected whole.
- Charset: printable ASCII (0x20-0x7E). Any other rune is dropped from the
  line, because glyphs outside ASCII cannot be typed with the default keymap.
- ANSI escape sequences and control characters are stripped.
- Lines starting with `//`, `#`, or `/*` are dropped, as are blank lines.
- Leading tabs expand to 4 columns; interior tabs collapse to one space.
- Lines are capped at 256 runes; the glyph system crops to the map width.

## Blocks

Consecutive lines are grouped into blocks of 2-5 lines. A block ends at five
lines, or at a top-level indent shift of 2+ columns. Blocks spawn whole, so
they read in file order.

## Selection

One file is picked at random and walked block by block. At end of file another
file is picked at random. `-f <file>` pins a single file, which then cycles.
`-f <dir>` overrides this directory. With neither, `./data` is used, then the
user config dir, then the corpus embedded in the binary.
`-d` skips discovery entirely and uses the embedded corpus.

## Adding files

Any text works. Go standard library sources are the current corpus; comment
stripping is tuned for them. Check licensing before adding third-party source.

