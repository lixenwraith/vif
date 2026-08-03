package content

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Cursor walks a corpus one block at a time: sequential within a source, random
// hop to another source at end of file. Not safe for concurrent use.
type Cursor struct {
	corpus *Corpus
	rng    *vmath.FastRand
	src    int
	blk    int
	pinned bool
}

// NewCursor starts a walk at a random source
func NewCursor(c *Corpus, rng *vmath.FastRand) *Cursor {
	cu := &Cursor{corpus: c, rng: rng}
	if !c.IsEmpty() {
		cu.src = rng.Intn(len(c.Sources))
	}
	return cu
}

// Pin restricts the walk to one source, cycling it indefinitely
func (cu *Cursor) Pin(name string) bool {
	i := cu.corpus.IndexOf(name)
	if i < 0 {
		return false
	}
	cu.src, cu.blk, cu.pinned = i, 0, true
	return true
}

// Next returns the next block; Load guarantees every source holds at least one
func (cu *Cursor) Next() (core.CodeBlock, bool) {
	if cu.corpus.IsEmpty() {
		return core.CodeBlock{}, false
	}
	if cu.blk >= len(cu.corpus.Sources[cu.src].Blocks) {
		cu.hop()
	}
	b := cu.corpus.Sources[cu.src].Blocks[cu.blk]
	cu.blk++
	return b, true
}

// hop restarts at another source, or at the top of the same one when pinned
func (cu *Cursor) hop() {
	cu.blk = 0
	n := len(cu.corpus.Sources)
	if cu.pinned || n < 2 {
		return
	}
	next := cu.rng.Intn(n - 1)
	if next >= cu.src {
		next++
	}
	cu.src = next
}

// Source returns the name of the file currently being walked
func (cu *Cursor) Source() string {
	if cu.corpus.IsEmpty() {
		return ""
	}
	return cu.corpus.Sources[cu.src].Name
}
