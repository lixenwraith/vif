package toml

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Lexer state machine
type Lexer struct {
	input []byte
	pos   int // current position in input (points to current char)
	start int // start position of this token
	line  int
	col   int
	width int // width of last rune read
}

func NewLexer(input []byte) *Lexer {
	return &Lexer{
		input: input,
		line:  1,
		col:   0,
	}
}

// NextToken returns the next token in the stream
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return l.newToken(TokenEOF, "")
	}

	ch := l.peek()

	// Handle Newlines explicitly as they are statement terminators in TOML
	if ch == '\n' {
		l.advance()
		return l.newToken(TokenNewline, "\n")
	}

	// Handle Comments
	if ch == '#' {
		return l.readComment()
	}

	// Operators
	switch ch {
	case '=':
		l.advance()
		return l.newToken(TokenEqual, "=")
	case '.':
		l.advance()
		return l.newToken(TokenDot, ".")
	case ',':
		l.advance()
		return l.newToken(TokenComma, ",")
	case '[':
		l.advance()
		return l.newToken(TokenLBracket, "[")
	case ']':
		l.advance()
		return l.newToken(TokenRBracket, "]")
	case '{':
		l.advance()
		return l.newToken(TokenLBrace, "{")
	case '}':
		l.advance()
		return l.newToken(TokenRBrace, "}")
	case '"':
		return l.readString()
	}

	// Numbers (Start with digit, or +/-, check next char for sign)
	if isDigit(ch) || ch == '+' || ch == '-' {
		// Edge case: '-' or '+' followed by non-digit is invalid, or could be part of string if not handled
		// But in TOML bare keys are A-Za-z0-9_-. So 123 is num, 123a is ident (unquoted key) needed?
		// Actually, TOML spec: Bare keys must be non-empty, composed of A-Za-z0-9_-
		// Values: 123 is int.
		// We treat ambiguity by lookahead in parser? No, lexer should decide.
		// If it starts with digit/+/- it attempts number. If fails (contains letters), it might be Ident?
		// No, standard TOML: Bare keys can only contain digits if they are fully digits? No "123a" is valid bare key.
		// Strategy: Read until boundary. Check if valid number. If not, check if valid ident.
		return l.readBareOrNumber()
	}

	// Identifiers / Booleans
	if isAlpha(ch) || ch == '_' {
		return l.readBareOrNumber()
	}

	// Unknown
	l.advance()
	return l.newToken(TokenError, fmt.Sprintf("unexpected character: %c", ch))
}

func (l *Lexer) newToken(typ TokenType, literal string) Token {
	return Token{Type: typ, Literal: literal, Line: l.line, Col: l.col - len(literal)}
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.input) {
		l.width = 0
		return 0
	}
	r, w := utf8.DecodeRune(l.input[l.pos:])
	l.width = w
	l.pos += w
	if r == '\n' {
		l.line++
		l.col = 0
	} else {
		l.col++
	}
	return r
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	r, _ := utf8.DecodeRune(l.input[l.pos:])
	return r
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.advance()
		} else {
			break
		}
	}
}

func (l *Lexer) readComment() Token {
	// Consume '#'
	l.advance()
	start := l.pos
	for l.pos < len(l.input) {
		if l.peek() == '\n' {
			break
		}
		l.advance()
	}
	// We treat comments as skippable in parser usually, but returning them helps if we want to preserve them.
	// For this strict data parser, we usually ignore them, but let's return it and let parser skip.
	return l.newToken(TokenComment, string(l.input[start:l.pos]))
}

func (l *Lexer) readString() Token {
	// Consume opening quote
	l.advance()
	start := l.pos
	escaped := false
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\n' {
			return l.newToken(TokenError, "unterminated string (newlines not allowed in basic strings)")
		}
		if ch == '"' && !escaped {
			lit := string(l.input[start:l.pos])
			l.advance() // consume closing quote
			return l.newToken(TokenString, l.unescape(lit))
		}
		if ch == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
		l.advance()
	}
	return l.newToken(TokenError, "unterminated string")
}

// Simple unescape for basic JSON-like escapes
func (l *Lexer) unescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	// Simplified: use Go's unquote if possible or manual
	// Since we are no-dep, manual replacement of common escapes
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\r`, "\r")
	return s
}

func (l *Lexer) readBareOrNumber() Token {
	start := l.pos
	firstCh := l.peek()
	// Numbers start with digit or sign; bare keys start with alpha/_
	isNumber := isDigit(firstCh) || firstCh == '+' || firstCh == '-'

	for l.pos < len(l.input) {
		ch := l.peek()
		// Bare keys: A-Za-z0-9_-
		if isAlpha(ch) || isDigit(ch) || ch == '_' || ch == '-' || ch == '+' {
			l.advance()
		} else if ch == '.' && isNumber {
			// Allow '.' only in numeric context (floats)
			l.advance()
		} else {
			break
		}
	}
	lit := string(l.input[start:l.pos])

	// Classify
	if lit == "true" || lit == "false" {
		return l.newToken(TokenBool, lit)
	}

	// Check for prefixed integers: 0x, 0o, 0b (with optional leading sign)
	checkLit := lit
	if len(checkLit) > 0 && (checkLit[0] == '+' || checkLit[0] == '-') {
		checkLit = checkLit[1:]
	}
	if len(checkLit) > 2 && checkLit[0] == '0' {
		switch checkLit[1] {
		case 'x', 'X', 'o', 'O', 'b', 'B':
			return l.newToken(TokenInteger, lit)
		}
	}

	// Try Number
	// Strict TOML number rules are complex, we use a heuristic here.
	// If it contains only digits, +, -, . (and valid structure), it's a number.
	// If it contains letters (except e in scientific notation), it is a bare key.
	// For this implementation, let's look for letters.
	hasLetter := false
	for _, r := range lit {
		if isAlpha(r) && r != 'e' && r != 'E' {
			hasLetter = true
			break
		}
	}

	if !hasLetter {
		// Likely a number
		if strings.Contains(lit, ".") || strings.Contains(lit, "e") || strings.Contains(lit, "E") {
			return l.newToken(TokenFloat, lit)
		}
		// Also standard ints shouldn't have multiple dots etc, but assuming valid input for now
		return l.newToken(TokenInteger, lit)
	}

	return l.newToken(TokenIdent, lit)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}