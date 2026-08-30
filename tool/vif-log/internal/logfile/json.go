package logfile

import (
	"bytes"
	"strconv"
)

// JSON token kinds.
const (
	KNone byte = 0
	KStr  byte = 's'
	KNum  byte = 'n'
	KBool byte = 'b'
	KNull byte = 'z'
	KObj  byte = 'o'
	KArr  byte = 'a'
)

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	return i
}

func isNumByte(c byte) bool {
	return c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || (c >= '0' && c <= '9')
}

// scanString returns the index past the closing quote; i indexes the opener.
func scanString(b []byte, i int) (int, bool) {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return i, false
}

// scanValue returns the end index and kind of the value starting at i.
func scanValue(b []byte, i int) (int, byte, bool) {
	if i >= len(b) {
		return i, KNone, false
	}
	switch c := b[i]; c {
	case '"':
		e, ok := scanString(b, i)
		return e, KStr, ok

	case '{', '[':
		opener, closer, kind := byte('{'), byte('}'), KObj
		if c == '[' {
			opener, closer, kind = '[', ']', KArr
		}
		depth := 0
		for i < len(b) {
			ch := b[i]
			if ch == '"' {
				e, ok := scanString(b, i)
				if !ok {
					return e, kind, false
				}
				i = e
				continue
			}
			if ch == opener {
				depth++
			} else if ch == closer {
				depth--
				if depth == 0 {
					return i + 1, kind, true
				}
			}
			i++
		}
		return i, kind, false

	case 't', 'f', 'n':
		kind := KBool
		if c == 'n' {
			kind = KNull
		}
		for i < len(b) && b[i] >= 'a' && b[i] <= 'z' {
			i++
		}
		return i, kind, true

	default:
		for i < len(b) && isNumByte(b[i]) {
			i++
		}
		return i, KNum, true
	}
}

// eachField calls fn for each member of the object at i, which must index '{'.
// fn returning false stops iteration. Reports whether the object is well formed.
func eachField(b []byte, i int, fn func(key, val []byte, kind byte) bool) bool {
	if i >= len(b) || b[i] != '{' {
		return false
	}
	i = skipSpace(b, i+1)
	if i < len(b) && b[i] == '}' {
		return true
	}
	for i < len(b) {
		if b[i] != '"' {
			return false
		}
		ke, ok := scanString(b, i)
		if !ok {
			return false
		}
		key := b[i+1 : ke-1]

		i = skipSpace(b, ke)
		if i >= len(b) || b[i] != ':' {
			return false
		}
		i = skipSpace(b, i+1)

		vs := i
		ve, kind, ok := scanValue(b, i)
		if !ok {
			return false
		}
		if !fn(key, b[vs:ve], kind) {
			return true
		}

		i = skipSpace(b, ve)
		if i >= len(b) {
			return false
		}
		switch b[i] {
		case ',':
			i = skipSpace(b, i+1)
		case '}':
			return true
		default:
			return false
		}
	}
	return false
}

// strTok returns the undecoded content of a string token.
func strTok(tok []byte) []byte {
	if len(tok) < 2 {
		return nil
	}
	return tok[1 : len(tok)-1]
}

// unquote returns the content of a string token, decoding escapes only when present.
func unquote(tok []byte) string {
	in := strTok(tok)
	if bytes.IndexByte(in, '\\') < 0 {
		return string(in)
	}
	if s, err := strconv.Unquote(string(tok)); err == nil {
		return s
	}
	return string(in)
}

// parseUint32 parses a leading decimal run, saturating at the type maximum.
func parseUint32(b []byte) uint32 {
	var v uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + uint64(c-'0')
		if v > 0xffffffff {
			return 0xffffffff
		}
	}
	return uint32(v)
}

// parseInt64 parses a complete decimal integer token without allocating.
func parseInt64(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	i, neg := 0, false
	if b[0] == '-' || b[0] == '+' {
		neg = b[0] == '-'
		i++
	}
	if i >= len(b) {
		return 0, false
	}
	var v int64
	for ; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, false
		}
		v = v*10 + int64(b[i]-'0')
		if v < 0 {
			return 0, false
		}
	}
	if neg {
		v = -v
	}
	return v, true
}
