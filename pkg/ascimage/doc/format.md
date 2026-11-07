# The .aimg format

An `.aimg` holds one image as terminal cells, each in true color and in the
xterm-256 palette at once, so a reader draws it on either kind of terminal
without converting. `WriteDualMode` writes it and `ReadDualMode` reads it
(`aimg.go`); this document is the contract both keep, and a change to the bytes
bumps the version here and in `aimg.go` in one commit.

The extension is also registered for the unrelated `image/prs.aimg`, an AI
provenance container whose files start with `AIMG`; the magic tells the two
apart.

## Layout

A text header, a blank line, then the cells as one zlib stream (RFC 1950):

```
ASCIMG
v:1
c:zlib
w:140
h:39
m:1
ax:0
ay:0
n:70980

<the zlib stream of w*h cells>
```

## Header

- The first line is the magic, `ASCIMG`.
- Each line after it is `key:value`, ended by `\n` alone and at most 64 bytes
  with it. A blank line ends the header within 16 lines of the magic.
- `v`: the format version, 1.
- `c`: the codec, `zlib`.
- `w`, `h`: the grid's width and height in cells, each 1 to 16384, the grid at
  most 4194304 cells (`MaxSide`, `MaxCells`).
- `m`: the render mode the cells were made in: 0 background, one color a cell
  and the rune a space; 1 quadrant, a quadrant glyph and two colors.
- `ax`, `ay`: the anchor, an offset the image's author gives its user, 0 when
  left out; the format gives it no meaning.
- `n`: the body's plain length, `w*h*13` bytes.
- Every value but `c`'s is a decimal integer. A key appears at most once; `v`,
  `c`, `w`, `h`, `m` and `n` are required, and a key version 1 does not name
  is skipped.

## Body

The stream holds `w*h` cells row by row from the top left, 13 bytes each:

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
- The stream ends with the cells: its Adler-32 checks them, and no byte
  follows it in the file.

## What a reader refuses

`ReadDualMode` refuses a file that breaks a rule above, and refuses a header
before it allocates the grid: another magic or version, a codec other than
zlib, a missing or doubled key, a size past the limits or an `n` that does not
match it, an unknown mode, a cell holding a surrogate or a value past U+10FFFF,
a flag bit other than bit 0, a stream that fails its checksum or holds more or
fewer bytes than `n`, and bytes after it.

## Versions

- 1: this document. vif's `.vifimg` (magic `VFIMG`, a raw deflate body or none)
  came before it, and no reader here opens one.
