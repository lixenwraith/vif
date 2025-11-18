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
