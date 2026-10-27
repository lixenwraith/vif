# font-atlas

Generates the bitmap font atlas a graphical host draws the cell grid with:
every glyph the code and the shipped scenarios draw, rendered from a monospace
TrueType into one cell size, written as `internal/asset/font/mono.toml`. The game
embeds the document and `pkg/glyph` reads it; `internal/asset/font_test.go`
checks that it covers every rune the scanned packages and scenarios draw.

```bash
python3 tool/font-atlas/font-atlas.py \
    --font /usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf \
    --size 20 --width 12 --height 24 --out internal/asset/font/mono.toml
```

Needs Python 3 and Pillow. The scanned packages and scenario roots
(`wad/scenario` and the embedded campaign, every `.toml` outside its comments, so
a behavior program's frames count) are listed at the top of the script and
mirrored in the test; a rune drawn elsewhere needs both extended. A scenario
loaded from outside them, or received from a peer, can draw a rune the atlas
lacks, which a graphical host has no bitmap for (`Texture.Slot` is -1).
The threshold turns the rasterizer's coverage into one bit per pixel: lower it
if thin strokes vanish at a small size.
