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

### Controls

| Key | Action | Scope |
|---|---|---|
| `q` / `Esc` | Quit | All |
| `f` | Toggle fit/actual | Image only |
| `m` | Toggle quadrant/bg mode | Image only |
| `c` | Toggle TrueColor/256 | All |
| `+` / `-` | Zoom | Image only |
| `hjkl` / arrows | Pan | All |
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
`event WallPatternSpawnRequest {path="wad/image/test.vifimg",x=0,y=0,block_mask=0}`

The path is resolved against the process working directory, so run `vif` from
the repository root — or give an absolute path.

### Sizing

Output cell dimensions from a source image of W×H at target width T:
- **quadrant**: T × floor(T × H/W × 0.5)
- **bg**: T × floor(T × H/W × 0.5)

Choose `-w` to fit the target viewport. Transparent pixels (alpha=0) in source are skipped — no wall entity created at those positions.
