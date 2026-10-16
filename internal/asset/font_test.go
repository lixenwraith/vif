//go:build !vif_headless

package asset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/pkg/glyph"
)

// TestTheFontAtlasCoversEveryGlyphTheCodeDraws: the atlas holds printable ASCII
// and every non-ASCII rune in the packages that draw, outside comments and tests,
// the set tool/font-atlas renders. A rune drawn elsewhere extends both lists.
func TestTheFontAtlasCoversEveryGlyphTheCodeDraws(t *testing.T) {
	a, err := glyph.Parse(DefaultFont)
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{"../parameter/visual", "../render/renderer", "../parameter", "../mode", "../app", "../help", "."}
	var missing []rune
	seen := map[rune]bool{}
	want := func(r rune) {
		if _, ok := a.Rows(r); !ok && !seen[r] {
			missing = append(missing, r)
		}
		seen[r] = true
	}
	for r := rune(' '); r <= '~'; r++ {
		want(r)
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				for _, r := range line {
					if r > 0x7f {
						want(r)
					}
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the atlas (%d glyphs) lacks %q", a.Len(), string(missing))
	}
}
