package mode

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CharType represents the type of character at a position
type CharType int

const (
	CharTypeSpace       CharType = 0
	CharTypeWord        CharType = 1
	CharTypePunctuation CharType = 2
)

// getCharAt returns the character at the given position, or 0 if empty
func getCharAt(ctx *engine.GameContext, x, y int) rune {
	entities := ctx.World.Positions.GetAllAt(x, y)
	charStore := engine.GetStore[components.CharacterComponent](ctx.World)

	for _, entity := range entities {
		if entity == ctx.CursorEntity || entity == 0 {
			continue
		}
		char, ok := charStore.Get(entity)
		if ok {
			if char.Rune == ' ' {
				return 0
			}
			return char.Rune
		}
	}
	return 0
}

// isWordChar returns true if the rune is a word character
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// getCharacterTypeAt returns the character type at the given position
func getCharacterTypeAt(ctx *engine.GameContext, x, y int) CharType {
	ch := getCharAt(ctx, x, y)
	if ch == 0 || ch == ' ' {
		return CharTypeSpace
	}
	if isWordChar(ch) {
		return CharTypeWord
	}
	return CharTypePunctuation
}

// validatePosition ensures the cursor position is within bounds
func validatePosition(ctx *engine.GameContext, x, y int) (validX, validY int) {
	validX, validY = x, y
	if validX < 0 {
		validX = 0
	} else if validX >= ctx.GameWidth {
		validX = ctx.GameWidth - 1
	}
	if validY < 0 {
		validY = 0
	} else if validY >= ctx.GameHeight {
		validY = ctx.GameHeight - 1
	}
	return validX, validY
}

// findCharInDirection finds the Nth occurrence of target in direction (forward=true)
// Returns (x position, found). If count exceeds matches, returns last match found
func findCharInDirection(ctx *engine.GameContext, startX, startY int, target rune, count int, forward bool) (int, bool) {
	occurrences := 0
	lastMatch := -1
	charStore := engine.GetStore[components.CharacterComponent](ctx.World)

	if forward {
		for x := startX + 1; x < ctx.GameWidth; x++ {
			entities := ctx.World.Positions.GetAllAt(x, startY)
			for _, entity := range entities {
				if entity == 0 {
					continue
				}
				char, ok := charStore.Get(entity)
				if ok && char.Rune == target {
					occurrences++
					lastMatch = x
					if occurrences == count {
						return x, true
					}
				}
			}
		}
	} else {
		for x := startX - 1; x >= 0; x-- {
			entities := ctx.World.Positions.GetAllAt(x, startY)
			for _, entity := range entities {
				if entity == 0 {
					continue
				}
				char, ok := charStore.Get(entity)
				if ok && char.Rune == target {
					occurrences++
					lastMatch = x
					if occurrences == count {
						return x, true
					}
				}
			}
		}
	}

	if lastMatch != -1 {
		return lastMatch, true
	}
	return -1, false
}

// --- Word Motion Helpers ---

func findNextWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	if currentType != CharTypeSpace {
		for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == currentType {
			x++
		}
	} else {
		x++
	}

	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.GameWidth {
		return cursorX
	}
	if x == cursorX {
		x++
		if x >= ctx.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findWordEndVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX + 1
	if x >= ctx.GameWidth {
		return cursorX
	}

	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}
	if x >= ctx.GameWidth {
		return cursorX
	}

	currentType := getCharacterTypeAt(ctx, x, cursorY)
	for x < ctx.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)
		if nextType == CharTypeSpace || nextType != currentType {
			break
		}
		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findPrevWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX - 1
	if x < 0 {
		return cursorX
	}

	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}
	if x < 0 {
		return cursorX
	}

	currentType := getCharacterTypeAt(ctx, x, cursorY)
	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)
		if prevType == CharTypeSpace || prevType != currentType {
			break
		}
		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findNextWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	if currentType != CharTypeSpace {
		for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			x++
		}
	} else {
		x++
	}

	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.GameWidth {
		return cursorX
	}
	if x == cursorX {
		x++
		if x >= ctx.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findWORDEnd(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX + 1
	if x >= ctx.GameWidth {
		return cursorX
	}

	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}
	if x >= ctx.GameWidth {
		return cursorX
	}

	for x < ctx.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)
		if nextType == CharTypeSpace {
			break
		}
		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findPrevWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX - 1
	if x < 0 {
		return cursorX
	}

	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}
	if x < 0 {
		return cursorX
	}

	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)
		if prevType == CharTypeSpace {
			break
		}
		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// --- Line Helpers ---

// findLineEnd returns the X coordinate of the last interactable entity on the line
// Returns -1 if no interactable entities are found on the line
// Optimized for high entity counts using spatial grid traversal (O(Width) instead of O(N))
func findLineEnd(ctx *engine.GameContext, cursorY int) int {
	// Stack-allocated buffer for zero-alloc queries
	var buf [constants.MaxEntitiesPerCell]core.Entity

	resolver := engine.MustGetResource[*engine.ZIndexResolver](ctx.World.Resources)

	// Scan from right to left
	for x := ctx.GameWidth - 1; x >= 0; x-- {
		// Zero-alloc query
		count := ctx.World.Positions.GetAllAtInto(x, cursorY, buf[:])

		// Check entities at this cell
		for i := 0; i < count; i++ {
			if buf[i] != 0 && resolver.IsInteractable(buf[i]) {
				// Found right-most interactable entity
				return x
			}
		}
	}

	// Line is empty (or no interactable entities)
	return -1
}

func findFirstNonWhitespace(ctx *engine.GameContext, cursorY int) int {
	for x := 0; x < ctx.GameWidth; x++ {
		if getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			validX, _ := validatePosition(ctx, x, cursorY)
			return validX
		}
	}
	return 0
}

// --- Paragraph Helpers ---

// findPrevEmptyLine finds the previous line with no interactable entities
// Optimized for high entity counts using spatial grid traversal
func findPrevEmptyLine(ctx *engine.GameContext, cursorY int) int {
	var buf [constants.MaxEntitiesPerCell]core.Entity

	resolver := engine.MustGetResource[*engine.ZIndexResolver](ctx.World.Resources)

	for y := cursorY - 1; y >= 0; y-- {
		rowEmpty := true
		// Scan row; stop early if any interactable entity is found
		for x := 0; x < ctx.GameWidth && rowEmpty; x++ {
			count := ctx.World.Positions.GetAllAtInto(x, y, buf[:])
			for i := 0; i < count; i++ {
				if buf[i] != 0 && resolver.IsInteractable(buf[i]) {
					rowEmpty = false
					break
				}
			}
		}
		if rowEmpty {
			return y
		}
	}
	return 0
}

// findNextEmptyLine finds the next line with no interactable entities
// Optimized for high entity counts using spatial grid traversal
func findNextEmptyLine(ctx *engine.GameContext, cursorY int) int {
	var buf [constants.MaxEntitiesPerCell]core.Entity

	resolver := engine.MustGetResource[*engine.ZIndexResolver](ctx.World.Resources)

	for y := cursorY + 1; y < ctx.GameHeight; y++ {
		rowEmpty := true
		// Scan row; stop early if any interactable entity is found
		for x := 0; x < ctx.GameWidth && rowEmpty; x++ {
			count := ctx.World.Positions.GetAllAtInto(x, y, buf[:])
			for i := 0; i < count; i++ {
				if buf[i] != 0 && resolver.IsInteractable(buf[i]) {
					rowEmpty = false
					break
				}
			}
		}
		if rowEmpty {
			return y
		}
	}
	return ctx.GameHeight - 1
}

// --- Bracket Helpers ---

func isBracket(r rune) bool {
	return r == '(' || r == ')' || r == '{' || r == '}' || r == '[' || r == ']' || r == '<' || r == '>'
}

func isOpeningBracket(r rune) bool {
	return r == '(' || r == '{' || r == '[' || r == '<'
}

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

func findMatchingBracket(ctx *engine.GameContext, cursorX, cursorY int) (int, int) {
	currentChar := getCharAt(ctx, cursorX, cursorY)
	if !isBracket(currentChar) {
		return -1, -1
	}

	matchingChar := getMatchingBracket(currentChar)
	if isOpeningBracket(currentChar) {
		return findMatchingBracketForward(ctx, cursorX, cursorY, currentChar, matchingChar)
	}
	return findMatchingBracketBackward(ctx, cursorX, cursorY, currentChar, matchingChar)
}

func findMatchingBracketForward(ctx *engine.GameContext, startX, startY int, openChar, closeChar rune) (int, int) {
	depth := 0
	x, y := startX+1, startY

	if x >= ctx.GameWidth {
		x = 0
		y++
	}

	for y < ctx.GameHeight {
		for x < ctx.GameWidth {
			ch := getCharAt(ctx, x, y)
			if ch == openChar {
				depth++
			} else if ch == closeChar {
				if depth == 0 {
					return x, y
				}
				depth--
			}
			x++
		}
		x = 0
		y++
	}
	return -1, -1
}

func findMatchingBracketBackward(ctx *engine.GameContext, startX, startY int, closeChar, openChar rune) (int, int) {
	depth := 0
	x, y := startX-1, startY

	if x < 0 {
		y--
		if y >= 0 {
			x = ctx.GameWidth - 1
		}
	}

	for y >= 0 {
		for x >= 0 {
			ch := getCharAt(ctx, x, y)
			if ch == closeChar {
				depth++
			} else if ch == openChar {
				if depth == 0 {
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
	return -1, -1
}