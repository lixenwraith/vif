# ascimage Pipeline

Terminal image converter and viewer for the `vif` wall/backdrop system. Assets
live in `wad/image`; see [filesystem layout](../../doc/filesystem-layout.md).

## Conversion

```bash
# Image → .vifimg (dual-mode, both TrueColor and 256-color)
ascimage -dual output.vifimg -w 140 -m quadrant input.png

# With anchor offset (spawn origin hint for game)
ascimage -dual output.vifimg -w 80 -m quadrant -ax 5 -ay 3 input.jpeg

# Image → ANSI file (single color mode)
ascimage -o output.ans -w 120 -m bg -c 256 input.png

# .vifimg → ANSI file
ascimage -o output.ans -c true file.vifimg
```

## Viewing

```bash
# View image interactively
ascimage input.png

# View .vifimg (color mode toggle, pan)
ascimage file.vifimg

# View without status bar
ascimage -no-status file.vifimg
```

### Flags

| Flag | Meaning |
|---|---|
| `-m bg\|quadrant` | Render mode (default `quadrant`) |
| `-c auto\|true\|256` | Color depth (default `auto`) |
| `-w <n>` | Output width in cells when an image is written with `-o` or `-dual` (`0` = 80) |
| `-dual <file>` | Write a dual-mode `.vifimg` from an image |
| `-o <file>` | Write ANSI (`-` = stdout); omit both outputs for the viewer |
| `-ax`, `-ay` | Anchor offset stored in a `.vifimg` |
| `-aspect <w/h>` | Cell width over height of the display the output is for (default `0.5`) |
| `-fit`, `-z <pct>` | Image viewer start state: fit (default on), or a zoom other than 100 |
| `-no-status` | Start the viewer without its status bar |

### Controls

| Key | Action | Scope |
|---|---|---|
| `q` / `Esc` / `^C` / `^D` | Quit | All |
| `f` | Toggle fit/actual | Image only |
| `m` | Toggle quadrant/bg mode | Image only |
| `c` | Toggle TrueColor/256 | All |
| `+` / `-` | Zoom, 10–400% | Image only |
| `0` | Zoom to 100% | Image only |
| `hjkl` / arrows | Pan; `HJKL` / Shift+arrows by 10 | All |
| `g` / `G`, `Home` / `End`, `PgUp` / `PgDn` | Pan to top-left/bottom-left, left/right edge, up/down half a screen | All |
| `s` | Toggle status bar | All |

## Render Modes

- **quadrant** (`-m quadrant`): 2×2 pixel blocks per cell using Unicode quadrant characters. Double effective resolution
- **bg** (`-m bg`): Background color only, one pixel per cell. Simpler, no foreground artifacts

## Game Integration

Spawn as blocking wall:
```
EventWallPatternSpawnRequest { Path, X, Y, BlockMask: WallBlockAll }
```

Spawn as non-blocking backdrop:
```
EventWallPatternSpawnRequest { Path, X, Y, BlockMask: WallBlockNone }
```

### Example:
vifimg creation (width 140):
```
bin/ascimage -dual wad/image/backdrop.vifimg -w 140 -m quadrant ~/image/test.jpeg
```

Game command to drop the shipped sample as a non-blocking wall background:
`event WallPatternSpawnRequest {path="test.vifimg",x=0,y=0,block_mask=0}`

The name is looked up below `image/` in each configuration root: `-config-dir`,
the user root, the checkout `wad/` beside a `bin/` binary, then the system roots.
Only a regular file there opens, never a host path, absolute or relative. A
checkout's `bin/vif`, or `-config-dir wad`, resolves the example to
`wad/image/test.vifimg`, and the same event works after installation.

`.vifimg` files carry a versioned header and a deflate-compressed cell body; the
reader also accepts the legacy unversioned, uncompressed format. Both carry the
`ax`/`ay` anchor offset, which the loader keeps but wall spawn does not use.

### Sizing

Output cell dimensions from a source image of W×H at target width T:
- **quadrant**: T × floor(T × H/W × 0.5)
- **bg**: T × floor(T × H/W × 0.5)

Choose `-w` to fit the target viewport. Transparent pixels (alpha=0) in source are skipped — no wall entity created at those positions.
