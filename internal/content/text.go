package content

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// ANSI stripping states
const (
	escNone = iota
	escStart
	escCSI
	escOSC
)

var cr = []byte{'\r'}

// parseText splits a raw file into blocks of consecutive content lines.
// A block ends at MaxBlockLines, or at a top-level indent shift once the
// minimum length is met; brace depth is file-scoped so the split lands on
// declaration boundaries in source files.
func parseText(data []byte, p *Policy) []core.CodeBlock {
	var (
		blocks  []core.CodeBlock
		cur     []string
		anchor  int
		depth   int
		scratch []byte
	)

	flush := func() {
		if len(cur) >= p.MinBlockLines {
			blocks = append(blocks, core.CodeBlock{Lines: cur})
		}
		cur = nil
	}

	for len(data) > 0 {
		var raw []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			raw, data = data[:i], data[i+1:]
		} else {
			raw, data = data, nil
		}
		raw = bytes.TrimSuffix(raw, cr)

		text, indent, s := sanitizeLine(raw, scratch, p)
		scratch = s
		if text == "" || hasAnyPrefix(text, p.CommentPrefixes) {
			continue
		}

		if len(cur) > 0 && (len(cur) >= p.MaxBlockLines ||
			(depth == 0 && len(cur) >= p.MinBlockLines &&
				vmath.IntAbs(indent-anchor) >= p.IndentDelta)) {
			flush()
		}
		if len(cur) == 0 {
			anchor = indent
		}

		cur = append(cur, text)
		depth += strings.Count(text, "{") - strings.Count(text, "}")
		if depth < 0 {
			depth = 0
		}
	}
	flush()

	return blocks
}

// sanitizeLine strips ANSI sequences and inadmissible runes, expands tabs, and
// trims. Returns the trimmed text and its indent width in columns.
// scratch is reused across calls; the returned buffer replaces it.
func sanitizeLine(raw, scratch []byte, p *Policy) (string, int, []byte) {
	scratch = scratch[:0]

	var (
		indent int
		body   int
		esc    = escNone
	)

	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		i += size

		switch esc {
		case escStart:
			switch r {
			case '[':
				esc = escCSI
			case ']':
				esc = escOSC
			default:
				esc = escNone
			}
			continue
		case escCSI:
			if r >= 0x40 && r <= 0x7e {
				esc = escNone
			}
			continue
		case escOSC:
			switch r {
			case 0x07:
				esc = escNone
			case 0x1b:
				esc = escStart
			}
			continue
		}

		if r == 0x1b {
			esc = escStart
			continue
		}

		// Leading tabs carry indent; interior tabs collapse to one space
		if r == '\t' {
			if body == 0 {
				for range p.TabWidth {
					scratch = append(scratch, ' ')
					indent++
				}
				continue
			}
			r = ' '
		}

		if !admits(r) {
			continue
		}
		if body == 0 && r == ' ' {
			scratch = append(scratch, ' ')
			indent++
			continue
		}
		if body >= p.MaxLineRunes {
			break
		}
		scratch = utf8.AppendRune(scratch, r)
		body++
	}

	if body == 0 {
		return "", 0, scratch
	}
	return strings.TrimRight(string(scratch[indent:]), " "), indent, scratch
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
