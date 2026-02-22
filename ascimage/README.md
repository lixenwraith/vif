# ascimage Pipeline

Terminal image converter and viewer for vi-fighter wall/backdrop system.

## Conversion

```bash
# Image → .vfimg (dual-mode, both TrueColor and 256-color)
ascimage -dual output.vfimg -w 140 -m quadrant input.png

# With anchor offset (spawn origin hint for game)
ascimage -dual output.vfimg -w 80 -m quadrant -ax 5 -ay 3 input.jpeg

# Image → ANSI file (single color mode)
ascimage -o output.ans -w 120 -m bg -c 256 input.png

# .vfimg → ANSI file
ascimage -o output.ans -c true file.vfimg
```

## Viewing

```bash
# View image interactively
ascimage input.png

# View .vfimg (color mode toggle, pan)
ascimage file.vfimg

# View without status bar
ascimage -no-status file.vfimg
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
VFIMG creation (width 140):
```
bin/ascimage -dual ./ascimage/test.vfimg -w 140 -m quadrant ~/image/test.jpeg
```

Game command to drop it as non-blocking wall background:
`event WallPatternSpawnRequest {path="./ascimage/test.vfimg",x=0,y=0,block_mask=0}`

### Sizing

Output cell dimensions from a source image of W×H at target width T:
- **quadrant**: T × floor(T × H/W × 0.5)
- **bg**: T × floor(T × H/W × 0.5)

Choose `-w` to fit the target viewport. Transparent pixels (alpha=0) in source are skipped — no wall entity created at those positions.
