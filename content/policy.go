package content

import (
	"github.com/lixenwraith/vi-fighter/parameter"
)

// File extensions recognised during corpus load
const (
	ExtText = ".txt"
	ExtTOML = ".toml"
)

// Policy controls corpus ingest
type Policy struct {
	MaxLineRunes    int
	MinBlockLines   int
	MaxBlockLines   int
	IndentDelta     int
	TabWidth        int
	CommentPrefixes []string
	MaxFileBytes    int64
	MaxCorpusBytes  int64
	MaxFiles        int
}

// DefaultPolicy returns the ingest policy used by the game
func DefaultPolicy() Policy {
	return Policy{
		MaxLineRunes:    parameter.ContentMaxLineRunes,
		MinBlockLines:   parameter.ContentMinBlockLines,
		MaxBlockLines:   parameter.ContentMaxBlockLines,
		IndentDelta:     parameter.ContentIndentDelta,
		TabWidth:        parameter.TabWidth,
		CommentPrefixes: parameter.ContentCommentPrefixes,
		MaxFileBytes:    parameter.ContentMaxFileBytes,
		MaxCorpusBytes:  parameter.ContentMaxCorpusBytes,
		MaxFiles:        parameter.ContentMaxFiles,
	}
}

// admits reports whether a rune may enter the corpus. Printable ASCII only:
// anything else is unreachable from the keymap and cannot be cleared by typing.
func admits(r rune) bool {
	return r >= 0x20 && r <= 0x7e
}
