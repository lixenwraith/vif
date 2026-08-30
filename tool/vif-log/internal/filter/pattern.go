package filter

import (
	"bytes"
	"regexp"
	"strings"
)

// Pattern is a smart-case matcher: an all-lowercase pattern matches
// case-insensitively, any upper-case character makes it case-sensitive.
// A pattern free of regexp metacharacters takes an allocation-free
// substring path; everything else compiles to stdlib RE2.
type Pattern struct {
	Src  string
	re   *regexp.Regexp
	lit  []byte
	fold bool
}

// NewPattern compiles s; an empty s yields an inert pattern.
func NewPattern(s string) (Pattern, error) {
	p := Pattern{Src: s, fold: s == strings.ToLower(s)}
	if s == "" {
		return p, nil
	}
	if regexp.QuoteMeta(s) == s {
		if p.fold {
			p.lit = []byte(strings.ToLower(s))
		} else {
			p.lit = []byte(s)
		}
		return p, nil
	}
	expr := s
	if p.fold {
		expr = "(?i)" + s
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return Pattern{}, err
	}
	p.re = re
	return p, nil
}

// Empty reports whether the pattern constrains nothing.
func (p Pattern) Empty() bool { return p.Src == "" }

// MatchBytes reports whether b contains a match.
func (p Pattern) MatchBytes(b []byte) bool {
	switch {
	case p.Src == "":
		return true
	case p.lit != nil && p.fold:
		return foldContains(b, p.lit)
	case p.lit != nil:
		return bytes.Contains(b, p.lit)
	default:
		return p.re.Match(b)
	}
}

// MatchString reports whether s contains a match.
func (p Pattern) MatchString(s string) bool {
	switch {
	case p.Src == "":
		return true
	case p.lit != nil && p.fold:
		return foldContains([]byte(s), p.lit)
	case p.lit != nil:
		return strings.Contains(s, string(p.lit))
	default:
		return p.re.MatchString(s)
	}
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// foldContains reports whether hay contains needle, ASCII case-insensitively.
// needle must already be lowercased.
func foldContains(hay, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(hay) < len(needle) {
		return false
	}
	first := needle[0]
	for i := 0; i+len(needle) <= len(hay); i++ {
		if lowerASCII(hay[i]) != first {
			continue
		}
		match := true
		for j := 1; j < len(needle); j++ {
			if lowerASCII(hay[i+j]) != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
