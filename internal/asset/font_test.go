//go:build !vif_headless

package asset

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/pkg/glyph"
)

// TestTheFontAtlasCoversEveryGlyphTheCodeDraws: the atlas holds printable ASCII
// and every non-ASCII rune in the packages that draw and in the shipped scenarios,
// whose programs' frames and messages draw too, outside comments and tests: the set
// tool/font-atlas renders. A rune drawn elsewhere extends both lists.
func TestTheFontAtlasCoversEveryGlyphTheCodeDraws(t *testing.T) {
	a, err := glyph.Parse(DefaultFont)
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{"../parameter/visual", "../render/renderer", "../parameter", "../mode", "../app", "../help", ".", "../prof"}
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
	for _, root := range []string{"../../wad/scenario", "scenario"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".toml" {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(src), "\n") {
				for _, r := range tomlCode(line) {
					if r > 0x7f {
						want(r)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the atlas (%d glyphs) lacks %q", a.Len(), string(missing))
	}
}

// tomlCode is a TOML line up to its comment, a # outside a string
func tomlCode(line string) string {
	var quote rune
	escaped := false
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case quote == '"' && r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			return line[:i]
		}
	}
	return line
}
