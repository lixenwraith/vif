package content

import (
	"slices"
	"testing"
)

// A line under the minimum leaves its block, yet its closing brace still brings the
// depth back to the top level, where an indent shift splits the next block off
func TestShortLinesLeaveBlocksButCloseTheirBraces(t *testing.T) {
	p := DefaultPolicy()
	blocks := parseText([]byte("func a() {\n\ttotal := 0\n}\n\tstray := 1\n\tother := 2\n"), &p)
	want := [][]string{{"func a() {", "total := 0"}, {"stray := 1", "other := 2"}}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %v, want %v", blocks, want)
	}
	for i, b := range blocks {
		if !slices.Equal(b.Lines, want[i]) {
			t.Fatalf("block %d = %q, want %q", i, b.Lines, want[i])
		}
	}
}
