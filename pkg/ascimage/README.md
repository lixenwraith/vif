# ascimage

`ascimage` turns images into terminal cells: a Unicode quadrant glyph and two
colors fitted to each cell's four samples, or one background color a cell. It
writes them as `.aimg` files, which hold every cell in true color and in the
xterm-256 palette at once ([format](doc/format.md)), or as ANSI text; prints
them fitted to the terminal; and browses folders of images and views them full
screen, converting rasters live at the size shown. It reads PNG, JPEG and GIF
with Go's own decoders. It began as [vif](https://github.com/lixenwraith/vif)'s
wall-image tool, and vif's image walls are made from its files.

## Install

Go as `go.mod` names it, cgo off:

- `go install github.com/lixenwraith/ascimage/cmd/ascimage@latest`
- or `make release`, a stripped `bin/ascimage`, then `make install`: the binary,
  the licence and these documents under `PREFIX` (`/usr/local`, or `~/.local`
  for a user who cannot write `/usr/local/bin`); `make uninstall` takes them away.

## Commands

```
ascimage [view flags] [dir | image]
ascimage <command> [flags] [args]
```

- `view [dir | image]`: browse a folder, the working one when none is named, or
  view an image full screen with its folder behind it. It is what runs with no
  command, or with only flags, a folder or an image (PNG, JPEG, GIF, `.aimg`,
  `.aimgz`). `-z fit` or a percentage of 1:1 starts an image's zoom;
  `-no-status` starts without the status bar.
- `convert <input> <output>`, `convert <input>... <dir>`: the output's extension
  names what is written: `.aimg` plain, `.aimgz` gzipped, any other name (`-`
  for stdout) ANSI text in `-c`'s depth. Several inputs, or a folder as the
  output, land in it under their own names in the form `-to` names (`aimg`,
  `aimgz`, `ans`). `-w` is the width in cells (80); `-ax` and `-ay` an anchor
  the file carries for its user. An `.aimg` input keeps its cells: it writes as
  ANSI, or as an `.aimg` with new anchors or the other form.
- `print <input>...`: ANSI text as large as fits the terminal, or `-w` cells
  wide; out of a terminal, 80. Several inputs print each under its name.
- `info <input>...`: a raster's format and pixels from its header alone; an
  `.aimg`'s form, cells, mode and anchor, read whole, so one `info` describes is
  one that opens; and the file's size.
- `help [command]`: the usage, or a command's flags.

The flags they share:

- `-m quadrant|bg`: a quadrant glyph and two colors a cell, or one background
  color a cell.
- `-c auto|true|256|16`: the color depth; `auto` reads `TERM` and `COLORTERM`,
  and in `view` the terminal itself, `NO_COLOR` included, which a named depth
  overrides.
- `-aspect`: a cell's width over its height on the display (0.5).

Flags come before paths. Every input is read by its bytes, whatever its name.

```bash
ascimage                                      # browse the working folder
ascimage photo.jpg                            # view it, its folder behind it
ascimage convert -w 140 photo.jpg wall.aimg
ascimage convert -w 140 photo.jpg wall.aimgz  # the same, gzipped
ascimage convert -c 256 wall.aimgz wall.ans
ascimage convert -to aimgz -w 60 a.png b.jpg thumbs/
ascimage print -w 60 photo.jpg | less -R
ascimage info wall.aimgz
```

## The browser and the view

The browser lists a folder's subfolders and the images it opens, with a
preview of the one under the cursor converted at the preview's size; Enter
views it full screen. The layout follows the room it has: list and preview
side by side from 64 columns, the preview under the list in a tall narrow
pane, one of the two at a time in a small one. The view shows an image at fit
or on a ladder of zooms from 5% to 800% of 1:1, converting only the cells on
screen, and decodes the images either side of it ahead, so stepping through a
folder does not wait. `?` lists the keys of either.

- Browser: `j k` move, `l` or Enter opens, `h` or Backspace goes up, `gg G`
  first and last, `^U ^D` and PgUp PgDn by pages, `/` filters names (Esc
  clears), Space marks, `a` marks all, `u` clears, `s` sorts by name, size or
  time, `.` shows hidden files, `~` goes home, Tab swaps list and preview when
  only one fits. The wheel moves; a click selects and a second opens.
- View: `h j k l` and the arrows pan, by 10 with `H J K L` or Shift; `^U ^D`,
  PgUp PgDn by half and whole screens; `g G` top and bottom, Home End the
  left and right edges; `+ -` zoom, `0` 1:1, `f` fit; `n` Space `]` the next
  image, `p` Backspace `[` the previous; Esc back to the folder. The wheel
  pans, Ctrl and the wheel zoom about the pointer, a drag pans, the side
  buttons step.
- Both: `m` quadrant or background cells, `c` color depth (true, 256, 16), `b`
  the backdrop under transparent cells (panel, black, white, checker), `w`
  writes the marked images or the one in hand (the view's at the width shown)
  as `convert` does, `^L` redraws, `q` quits.

## The library

The package `ascimage`, at the module root, has no state:

- `ConvertImage(img, width, mode, colorMode, aspect)`: the cells
  `CalculateOutputSize` gives at a width, in one color mode, as a
  `*ConvertedImage`; a cell with `Rune` 0 is transparent.
- `ConvertRect(dst, img, gridW, gridH, rect, mode, colorMode)`: the cells of
  one window of such a grid, written into `dst` and the same cell for cell as
  `ConvertImage`'s, so a viewer converts only what it shows.
- `ConvertImageDual(img, width, mode, aspect)`: a `*DualModeImage`, each cell in
  true color and 256 colors, with transparency; `ToConvertedImage(colorMode)`
  picks one, 16 colors as the ANSI 16 nearest the true colors.
- `WriteDualMode(w, img)` writes a plain `.aimg`, `ReadDualMode(r)` reads
  either form by its bytes, `SaveDualMode(path, img)` gzips when the path ends
  in `.aimgz`, `LoadDualMode(path)`, `Gzipped(head)`.
- `WriteANSI(w, img)`: SGR-colored lines, each cell as its attributes say.
- `FitQuadrant(pixels)`: the glyph and two colors that show four colors with
  the least squared error. `DefaultAspect`, `MaxSide`, `MaxCells`, `Extension`,
  `ExtensionGzip`, `QuadrantChars`.

```go
package main

import (
	"image"
	_ "image/jpeg"
	"log"
	"os"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
)

func main() {
	f, err := os.Open("photo.jpg")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	d := ascimage.ConvertImageDual(img, 80, ascimage.ModeQuadrant, ascimage.DefaultAspect)
	if err := ascimage.SaveDualMode("photo.aimgz", d); err != nil {
		log.Fatal(err)
	}
	if err := ascimage.WriteANSI(os.Stdout, d.ToConvertedImage(terminal.ColorModeTrueColor)); err != nil {
		log.Fatal(err)
	}
}
```

## Invariants

- The package imports the standard library, `lixenwraith/color` and
  `lixenwraith/terminal` alone; the command adds the terminal module's `tui`
  and `inline`.
- A file's form is read from its bytes, never its name.
- A reader checks every limit a header names before it allocates the grid.
- `ConvertRect`'s cells are `ConvertImage`'s at the same grid, and `convert`,
  the TUI's write and `print` go through the same conversion.

## Gotchas

- Each sample is the source pixel under its centre, so a photo shrunk many
  times aliases; `doc/todo.md` holds the filter that would cure it.
- A cell whose four samples are all transparent is transparent; a partly
  transparent one fits its transparent samples as black.
- A GIF shows its first frame.
- The 256-color cells of an `.aimg` are the indices it stores, which a reader
  on 256 colors uses as they are.
