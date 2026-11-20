package content

import (
	"slices"
	"strings"
	"testing"
)

// An authored line keeps its indent, a leading tab as the policy's width of spaces,
// so a block laid whole shows its nesting; a trailing space and an empty line go
func TestAnAuthoredLineKeepsItsIndent(t *testing.T) {
	p := DefaultPolicy()
	data := []byte("schema = 1\n[[blocks]]\nlines = [\"if ok {\", \"  a := 1 \", \"\\tb := 2\", \"\", \"}\"]\n")
	blocks, err := parseTOML(data, &p)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"if ok {", "  a := 1", strings.Repeat(" ", p.TabWidth) + "b := 2", "}"}
	if len(blocks) != 1 || !slices.Equal(blocks[0].Lines, want) {
		t.Fatalf("blocks = %q, want one of %q", blocks, want)
	}
}
