package content

import "github.com/lixenwraith/vi-fighter/internal/core"

// Source is one loaded content file; every Source holds at least one block
type Source struct {
	Name   string
	Blocks []core.CodeBlock
	Lines  int
}

// Rejection records a file that did not enter the corpus
type Rejection struct {
	Name   string
	Reason string
}

// Corpus is the immutable result of a single Load
type Corpus struct {
	Sources  []Source
	Rejected []Rejection
	Bytes    int64
}

// IsEmpty reports whether the corpus can serve any block
func (c *Corpus) IsEmpty() bool { return c == nil || len(c.Sources) == 0 }

// BlockCount returns the total block count across all sources
func (c *Corpus) BlockCount() int {
	n := 0
	for i := range c.Sources {
		n += len(c.Sources[i].Blocks)
	}
	return n
}

// LineCount returns the total line count across all sources
func (c *Corpus) LineCount() int {
	n := 0
	for i := range c.Sources {
		n += c.Sources[i].Lines
	}
	return n
}

// IndexOf resolves a source name, returning -1 when absent
func (c *Corpus) IndexOf(name string) int {
	for i := range c.Sources {
		if c.Sources[i].Name == name {
			return i
		}
	}
	return -1
}

func (c *Corpus) reject(name, reason string) {
	c.Rejected = append(c.Rejected, Rejection{Name: name, Reason: reason})
}
