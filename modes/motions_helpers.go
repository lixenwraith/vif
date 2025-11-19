package modes

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// CharType represents the type of character at a position
type CharType int

const (
	// CharTypeSpace represents empty space (no character)
	CharTypeSpace CharType = 0
	// CharTypeWord represents alphanumeric characters and underscore
	CharTypeWord CharType = 1
	// CharTypePunctuation represents all other visible characters
	CharTypePunctuation CharType = 2
)

// getCharacterTypeAt returns the character type at the given position
// This replaces inline character type checking for clearer, more maintainable code
func getCharacterTypeAt(ctx *engine.GameContext, x, y int) CharType {
	ch := getCharAt(ctx, x, y)

	// Both 0 (no entity) and ' ' (space entity) are spaces
	if ch == 0 || ch == ' ' {
		return CharTypeSpace
	}

	if isWordChar(ch) {
		return CharTypeWord
	}

	return CharTypePunctuation
}

// validatePosition ensures the cursor position is valid after a motion
// Returns the validated position that is guaranteed to be within bounds
func validatePosition(ctx *engine.GameContext, x, y int) (validX, validY int) {
	validX = x
	validY = y

	// Ensure X is within bounds
	if validX < 0 {
		validX = 0
	} else if validX >= ctx.GameWidth {
		validX = ctx.GameWidth - 1
	}

	// Ensure Y is within bounds
	if validY < 0 {
		validY = 0
	} else if validY >= ctx.GameHeight {
		validY = ctx.GameHeight - 1
	}

	return validX, validY
}

// isBracket returns true if the rune is a bracket character
func isBracket(r rune) bool {
	return r == '(' || r == ')' || r == '{' || r == '}' || r == '[' || r == ']' || r == '<' || r == '>'
}

// isOpeningBracket returns true if the rune is an opening bracket
func isOpeningBracket(r rune) bool {
	return r == '(' || r == '{' || r == '[' || r == '<'
}

// isClosingBracket returns true if the rune is a closing bracket
func isClosingBracket(r rune) bool {
	return r == ')' || r == '}' || r == ']' || r == '>'
}

// getMatchingBracket returns the matching bracket for a given bracket
func getMatchingBracket(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '{':
		return '}'
	case '}':
		return '{'
	case '[':
		return ']'
	case ']':
		return '['
	case '<':
		return '>'
	case '>':
		return '<'
	}
	return 0
}

// findMatchingBracket finds the matching bracket for the bracket at cursor position
// Returns the new cursor position (x, y) or (-1, -1) if no match found
func findMatchingBracket(ctx *engine.GameContext) (int, int) {
	// Get character at current cursor position
	currentChar := getCharAt(ctx, ctx.CursorX, ctx.CursorY)

	// Check if cursor is on a bracket
	if !isBracket(currentChar) {
		return -1, -1
	}

	matchingChar := getMatchingBracket(currentChar)

	// Determine search direction
	if isOpeningBracket(currentChar) {
		// Search forward for matching closing bracket
		return findMatchingBracketForward(ctx, ctx.CursorX, ctx.CursorY, currentChar, matchingChar)
	} else {
		// Search backward for matching opening bracket
		return findMatchingBracketBackward(ctx, ctx.CursorX, ctx.CursorY, currentChar, matchingChar)
	}
}

// findMatchingBracketForward searches forward for matching closing bracket using a stack
func findMatchingBracketForward(ctx *engine.GameContext, startX, startY int, openChar, closeChar rune) (int, int) {
	depth := 0

	// Start from the position after the opening bracket
	x := startX
	y := startY

	// Move to next position
	x++
	if x >= ctx.GameWidth {
		x = 0
		y++
	}

	// Search through all positions forward
	for y < ctx.GameHeight {
		for x < ctx.GameWidth {
			ch := getCharAt(ctx, x, y)

			if ch == openChar {
				depth++
			} else if ch == closeChar {
				if depth == 0 {
					// Found matching bracket
					return x, y
				}
				depth--
			}

			x++
		}
		x = 0
		y++
	}

	// No match found
	return -1, -1
}

// findMatchingBracketBackward searches backward for matching opening bracket using a stack
func findMatchingBracketBackward(ctx *engine.GameContext, startX, startY int, closeChar, openChar rune) (int, int) {
	depth := 0

	// Start from the position before the closing bracket
	x := startX
	y := startY

	// Move to previous position
	x--
	if x < 0 {
		y--
		if y >= 0 {
			x = ctx.GameWidth - 1
		}
	}

	// Search through all positions backward
	for y >= 0 {
		for x >= 0 {
			ch := getCharAt(ctx, x, y)

			if ch == closeChar {
				depth++
			} else if ch == openChar {
				if depth == 0 {
					// Found matching bracket
					return x, y
				}
				depth--
			}

			x--
		}
		y--
		if y >= 0 {
			x = ctx.GameWidth - 1
		}
	}

	// No match found
	return -1, -1
}
