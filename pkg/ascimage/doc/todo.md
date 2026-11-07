# TODO

Where ascimage may grow. Each item states what remains and the smallest thing
that would close it. Delete an item when it lands; this file is not a changelog.
Source files do not carry TODO comments.

Priorities: P1 is wanted next, P2 is convenient follow-up, and P3 is an idea.

## Conversion

### Filter a shrinking image

- Priority: P1
- Affected files: `converter.go`

Each sample is the pixel under its centre, so a photo shrunk ten times or more
aliases into noise. Averaging a few points inside each sample's footprint
(four to sixteen) would cure it at that many reads a sample; the windowed
conversion keeps the cost on screen cells, about 3 ms for 7,500 cells today.
The cells would differ from today's, so vif's sample is regenerated with it.

### Read the common image types without image.At

- Priority: P2
- Affected files: `converter.go`

`image.At` boxes each pixel, four allocations a cell (30,720 for a 160x48
window, 3.2 ms). Reading `*image.YCbCr`, `*image.RGBA`, `*image.NRGBA`,
`*image.Paletted` and `*image.Gray` directly, `At` the fallback, would take
them away; measure the window before and after.

### Partly transparent cells

- Priority: P2
- Affected files: `converter.go`, `dual.go`, `aimg.go`, `doc/format.md`

A cell whose samples are partly transparent fits the transparent ones as black.
A flag for a transparent background (bit 1, format version 2) would draw the
opaque quadrants as the glyph's foreground over what lies beneath, as vif's
walls and the TUI's backdrop already allow a whole cell.

## Format

### A layout that compresses photos

- Priority: P3
- Affected files: `aimg.go`, `doc/format.md`

Planes of one field each, the colors as differences from the cell to the left,
take a photo at 300 cells from 45% of its plain size to 18% under DEFLATE but
grow pixel art by half again. A writer could pick per file, the header naming
the layout, in a new version.

## Inputs

- Priority: P3. WebP, BMP and TIFF through `golang.org/x/image`, a dependency
  the module does not take today; the animated GIF's frames, shown in turn.

## The TUI

- Priority: P2. The cell aspect read from the terminal (`CSI 16 t`, a cell's
  pixels) where it answers, in place of `-aspect`.
- Priority: P3. A pan that shifts the window's cells and converts only the strip
  it exposes, today 3.1 ms a step at 160x48.
- Priority: P3. A folder's preview: how many images it holds, or a sheet of
  their thumbnails.
