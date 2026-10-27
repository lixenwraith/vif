package pattern

import (
	"fmt"
	"hash/fnv"
	"html"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

// previewGroups are the preview's three groups, each open along its right side, and the
// sizes the property tests sweep
var previewGroups = []struct {
	name string
	x, y int64
	w, h int
}{
	{"a thin ceiling", 0, 0, 40, 4},
	{"a slab", 100, 20, 60, 12},
	{"a block", 200, 40, 30, 30},
}

// sweep calls f with every preset over a few seeds and each preview group open on each
// side in turn, at its own place, at a negative column and at the end of the int64 range
func sweep(f func(ArtPreset, uint64, ArtCell)) {
	for p := range ArtPresetCount {
		for _, seed := range []uint64{0, 7, math.MaxUint64} {
			for _, g := range previewGroups {
				for face := range faceCount {
					for _, x0 := range []int64{g.x, -1 << 40, math.MaxInt64 - int64(g.w)} {
						for y := range g.h {
							for x := range g.w {
								depth := [faceCount]int{FaceUp: y, FaceDown: g.h - 1 - y, FaceLeft: x, FaceRight: g.w - 1 - x}[face]
								f(p, seed, ArtCell{X: x0 + int64(x), Y: g.y + int64(y), Depth: depth, Face: face, W: g.w, H: g.h})
							}
						}
					}
				}
			}
		}
	}
}

// Art keeps no state: the sweep drawn again, each cell after another preset's, matches
func TestArtDependsOnlyOnItsArguments(t *testing.T) {
	type drawn struct {
		r      rune
		fg, bg color.RGB
	}
	var first []drawn
	sweep(func(p ArtPreset, seed uint64, c ArtCell) {
		r, fg, bg := Art(p, seed, c)
		first = append(first, drawn{r, fg, bg})
	})
	i := 0
	sweep(func(p ArtPreset, seed uint64, c ArtCell) {
		Art((p+1)%ArtPresetCount, ^seed, ArtCell{X: -c.X, Y: c.Y + 1, W: 1, H: 1})
		if r, fg, bg := Art(p, seed, c); (drawn{r, fg, bg}) != first[i] {
			t.Fatalf("%v seed %d %+v: %v, then %v", p, seed, c, first[i], drawn{r, fg, bg})
		}
		i++
	})
}

// The digest pins the stone itself: it moves with a change of look and nothing else, and
// it holds on every platform
func TestArtIsFixedByItsPresetSeedAndCell(t *testing.T) {
	h := fnv.New64a()
	var b [9]byte
	sweep(func(p ArtPreset, seed uint64, c ArtCell) {
		r, fg, bg := Art(p, seed, c)
		b = [9]byte{byte(r >> 16), byte(r >> 8), byte(r), fg.R, fg.G, fg.B, bg.R, bg.G, bg.B}
		h.Write(b[:])
	})
	if got, want := h.Sum64(), uint64(0xad8ab6a81e6cf4c); got != want {
		t.Fatalf("stone digest %#x, want %#x", got, want)
	}
}

// Every glyph shows: a printing rune, its foreground apart from its background even in
// the 256 colours a wall falls back to
func TestEveryPresetDrawsAVisibleGlyph(t *testing.T) {
	sweep(func(p ArtPreset, seed uint64, c ArtCell) {
		r, fg, bg := Art(p, seed, c)
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) || color.RGBTo256(fg) == color.RGBTo256(bg) {
			t.Fatalf("%v seed %d %+v: %q in %v on %v", p, seed, c, r, fg, bg)
		}
	})
}

// W and H as ArtCell documents them: masonry's joints thin out as W grows, crystals
// thicken as W×H does, and every other preset draws a cell alike at any size
func TestArtScalesWithItsGroupAsDocumented(t *testing.T) {
	// count is how many cells of a w×h group draw one of glyphs, over seeds
	count := func(p ArtPreset, w, h int, glyphs ...rune) (n int) {
		for seed := range uint64(32) {
			for y := range h {
				for x := range w {
					if r, _, _ := Art(p, seed, ArtCell{X: int64(x), Y: int64(y), Depth: w + h, W: w, H: h}); slices.Contains(glyphs, r) {
						n++
					}
				}
			}
		}
		return n
	}
	// Densities compare with the groups' areas cross-multiplied
	narrow, wide := count(ArtMasonry, 24, 8, visual.MasonryJoint), count(ArtMasonry, 72, 8, visual.MasonryJoint)
	if narrow*72 <= wide*24 {
		t.Errorf("masonry: %d joints over 24×8 and %d over 72×8, want them sparser in the wider", narrow, wide)
	}
	small, large := count(ArtCrystal, 40, 4, visual.CrystalGlyphs[:]...), count(ArtCrystal, 30, 30, visual.CrystalGlyphs[:]...)
	if small*900 >= large*160 {
		t.Errorf("crystal: %d points over 40×4 and %d over 30×30, want them sparser in the smaller", small, large)
	}
	sweep(func(p ArtPreset, seed uint64, c ArtCell) {
		if p == ArtMasonry || p == ArtCrystal {
			return
		}
		r, fg, bg := Art(p, seed, c)
		c.W, c.H = 1, 1
		if r1, fg1, bg1 := Art(p, seed, c); r1 != r || fg1 != fg || bg1 != bg {
			t.Fatalf("%v seed %d %+v: %q %v %v in its group, %q %v %v alone", p, seed, c, r, fg, bg, r1, fg1, bg1)
		}
	})
}

// TestArtPreview writes every preset at the three group sizes to the HTML page
// VIF_ART_PREVIEW names, monospace cells in inline styles, for a person to look at
func TestArtPreview(t *testing.T) {
	path := os.Getenv("VIF_ART_PREVIEW")
	if path == "" {
		t.Skip("set VIF_ART_PREVIEW to a file path to write the preview")
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Stone art presets</title><style>
body{background:#1a1b26;color:#a9b1d6;font:13px sans-serif;margin:16px}
h2{font-size:15px;margin:20px 0 6px}
.row{display:flex;flex-wrap:wrap;gap:28px;align-items:flex-start}
figure{margin:0}figcaption{color:#787c99;margin-bottom:4px}
.g{font:15px/18px "DejaVu Sans Mono",Menlo,Consolas,monospace;white-space:pre}
.g div{height:18px}.g span{display:inline-block;height:18px;vertical-align:top}
</style></head><body>
`)
	for p := range ArtPresetCount {
		fmt.Fprintf(&b, "<h2>%s</h2>\n<div class=\"row\">\n", p)
		for _, g := range previewGroups {
			fmt.Fprintf(&b, "<figure><figcaption>%s, %d×%d, open face right</figcaption><div class=\"g\">\n", g.name, g.w, g.h)
			for y := range g.h {
				b.WriteString("<div>")
				var run strings.Builder
				var fg, bg color.RGB
				flush := func() {
					if run.Len() > 0 {
						fmt.Fprintf(&b, `<span style="color:%s;background:%s">%s</span>`, fg.Hex(), bg.Hex(), html.EscapeString(run.String()))
						run.Reset()
					}
				}
				for x := range g.w {
					r, f, k := Art(p, 1, ArtCell{X: g.x + int64(x), Y: g.y + int64(y), Depth: g.w - 1 - x, Face: FaceRight, W: g.w, H: g.h})
					if f != fg || k != bg {
						flush()
						fg, bg = f, k
					}
					run.WriteRune(r)
				}
				flush()
				b.WriteString("</div>\n")
			}
			b.WriteString("</div></figure>\n")
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</body></html>\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	sinkRune rune
	sinkRGB  color.RGB
)

// BenchmarkArt times each preset over a cave's worth of cells at every depth and face,
// so ns/op is per cell
func BenchmarkArt(b *testing.B) {
	cells := make([]ArtCell, 1<<12)
	for i := range cells {
		cells[i] = ArtCell{X: int64(i % 128), Y: int64(i / 128), Depth: i % 6, Face: Face(i / 6 % 4), W: 200, H: 12}
	}
	for p := range ArtPresetCount {
		b.Run(p.String(), func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				sinkRune, sinkRGB, _ = Art(p, 7, cells[i&(len(cells)-1)])
				i++
			}
		})
	}
}
