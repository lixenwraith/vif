# The .aimg format

An `.aimg` holds one image as terminal cells, each in true color and in the
xterm-256 palette at once, so a reader draws it on either kind of terminal
without converting. An `.aimgz` is the same file gzipped whole, as an `.svgz`
is an `.svg`. `WriteDualMode` writes the plain form, `SaveDualMode` either by
the name it is given, and `ReadDualMode` reads both (`aimg.go`). This document
is the contract they keep; a change to the bytes bumps the version here and
in `aimg.go` in one commit.

A reader goes by a file's bytes, never its name: gzip's two magic bytes
(`1f 8b`) open an `.aimgz`, `ASCIMG` an `.aimg`, under any extension. The
extension `.aimg` is also registered for the unrelated `image/prs.aimg`, an AI
provenance container whose files start with `AIMG`; the magic tells the two
apart.

## Layout

A text header, a blank line, then the cells, uncompressed:

```
ASCIMG
v:1
w:140
h:39
m:1
ax:0
ay:0

<w*h cells of 13 bytes>
```

## Header

- The first line is the magic, `ASCIMG`.
- Each line after it is `key:value`, ended by `\n` alone and at most 64 bytes
  with it. A blank line ends the header within 16 lines of the magic.
- `v`: the format version, 1.
- `w`, `h`: the grid's width and height in cells, each 1 to 16384, the grid at
  most 4194304 cells (`MaxSide`, `MaxCells`).
- `m`: the render mode the cells were made in: 0 background, one color a cell
  and the rune a space; 1 quadrant, a quadrant glyph and two colors.
- `ax`, `ay`: the anchor, an offset the image's author gives its user, 0 when
  left out; the format gives it no meaning.
- Every value is a decimal integer. A key appears at most once; `v`, `w`, `h`
  and `m` are required, and a key version 1 does not name is skipped.

## Cells

`w*h` cells follow row by row from the top left, 13 bytes each, and the file
ends with the last:

```
offset  size  field
0       4     rune, little-endian uint32: a Unicode scalar value, or 0
4       3     true color foreground, R G B
7       3     true color background, R G B
10      1     xterm-256 foreground index
11      1     xterm-256 background index
12      1     flags: bit 0 transparent, the other bits 0
```

- A transparent cell shows what lies under the image: its other fields are
  written 0, and a reader ignores them.
- The 256 indices are the `color` module's `RGBTo256` of the true colors,
  written beside them so a reader on a 256-color terminal needs no table.
- The plain form carries no checksum. An `.aimgz` carries gzip's CRC-32 and
  length, which the reader checks as the stream ends.

## Compression

`SaveDualMode` gzips at gzip's default level (6). Over 54 shapes, nine images
(photos, graphics, an icon with alpha, a GIF) at 80, 140 and 300 cells wide in
both modes, 8.09 MB of plain cells, DEFLATE leaves:

```
level           of the plain   encode, 363 KB plain   inflate, 585 KB plain
1 (BestSpeed)   18.6%          2.6 ms                 2.2 ms at most
6 (default)     16.0%          5.0 ms
9 (Best)        15.1%          13.7 ms
```

Photos compress worst: vif's 140x39 backdrop is 71,017 bytes plain and 23,928
gzipped, a 1600x1000 photo at 140 cells 76.5 KiB and 36.8 KiB. Planes of one
field each, the colors as differences from the cell to the left, would take a
photo at 300 cells from 45% of its plain size to 18%, but grow pixel art by
half again; the layout stays one cell after another (`doc/todo.md`).

## What a reader refuses

`ReadDualMode` refuses a file that breaks a rule above, and refuses a header
before it allocates the grid: another magic or version, a missing or doubled
key, a size past the limits, an unknown mode, a cell holding a surrogate or a
value past U+10FFFF, a flag bit other than bit 0, fewer cells than the header
names or bytes after them, and a gzip stream that is cut or fails its
checksum. Joined gzip members read as one stream, as gzip reads them, and what
they hold must be one plain file.

## Versions

- 1: this document. vif's `.vifimg` (magic `VFIMG`, a raw deflate body or none)
  came before it, and no reader here opens one.
