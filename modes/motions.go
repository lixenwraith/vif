package modes

import (
	"reflect"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ExecuteMotion executes a vi motion command
func ExecuteMotion(ctx *engine.GameContext, cmd rune, count int) {
	if count == 0 {
		count = 1
	}

	switch cmd {
	case 'w': // Word forward (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			ctx.CursorX = findNextWordStartVim(ctx)
		}
	case 'W': // WORD forward (space-delimited)
		for i := 0; i < count; i++ {
			ctx.CursorX = findNextWORDStart(ctx)
		}
	case 'e': // Word end (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			ctx.CursorX = findWordEndVim(ctx)
		}
	case 'E': // WORD end (space-delimited)
		for i := 0; i < count; i++ {
			ctx.CursorX = findWORDEnd(ctx)
		}
	case 'b': // Word backward (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			ctx.CursorX = findPrevWordStartVim(ctx)
		}
	case 'B': // WORD backward (space-delimited)
		for i := 0; i < count; i++ {
			ctx.CursorX = findPrevWORDStart(ctx)
		}
	case '0': // Line start
		ctx.CursorX = 0
	case '^': // First non-whitespace character
		ctx.CursorX = findFirstNonWhitespace(ctx)
	case '$': // Line end (rightmost character)
		ctx.CursorX = findLineEnd(ctx)
	case 'H': // Top of screen (same column)
		ctx.CursorY = 0
	case 'M': // Middle of screen (same column)
		ctx.CursorY = ctx.GameHeight / 2
	case 'L': // Bottom of screen (same column)
		ctx.CursorY = ctx.GameHeight - 1
	case '{': // Previous empty line (paragraph backward)
		for i := 0; i < count; i++ {
			ctx.CursorY = findPrevEmptyLine(ctx)
		}
	case '}': // Next empty line (paragraph forward)
		for i := 0; i < count; i++ {
			ctx.CursorY = findNextEmptyLine(ctx)
		}
	case 'G': // Bottom
		ctx.CursorY = ctx.GameHeight - 1
	case 'g': // Top (when preceded by another 'g')
		ctx.CursorY = 0
	case 'f': // Find character (set waiting state)
		ctx.WaitingForF = true
	case 'x': // Delete character
		deleteCharAt(ctx, ctx.CursorX, ctx.CursorY)
	case 'h': // Left
		for i := 0; i < count; i++ {
			if ctx.CursorX > 0 {
				ctx.CursorX--
			}
		}
	case 'j': // Down
		for i := 0; i < count; i++ {
			if ctx.CursorY < ctx.GameHeight-1 {
				ctx.CursorY++
			}
		}
	case 'k': // Up
		for i := 0; i < count; i++ {
			if ctx.CursorY > 0 {
				ctx.CursorY--
			}
		}
	case 'l': // Right
		for i := 0; i < count; i++ {
			if ctx.CursorX < ctx.GameWidth-1 {
				ctx.CursorX++
			}
		}
	case ' ': // Space - behaves like 'l' in normal mode
		for i := 0; i < count; i++ {
			if ctx.CursorX < ctx.GameWidth-1 {
				ctx.CursorX++
			}
		}
	}

	// Check for consecutive motion keys (heat penalty)
	if cmd == ctx.LastMoveKey && (cmd == 'h' || cmd == 'j' || cmd == 'k' || cmd == 'l') {
		ctx.ConsecutiveCount++
		if ctx.ConsecutiveCount > 3 {
			ctx.SetScoreIncrement(0) // Reset heat after 3+ consecutive moves
		}
	} else {
		ctx.LastMoveKey = cmd
		ctx.ConsecutiveCount = 1
	}
}

// ExecuteFindChar executes the 'f' (find character) command
func ExecuteFindChar(ctx *engine.GameContext, targetChar rune) {
	// Search forward on current line for the character
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)

	for x := ctx.CursorX + 1; x < ctx.GameWidth; x++ {
		for _, entity := range entities {
			posComp, _ := ctx.World.GetComponent(entity, posType)
			pos := posComp.(components.PositionComponent)

			if pos.Y == ctx.CursorY && pos.X == x {
				charComp, _ := ctx.World.GetComponent(entity, charType)
				char := charComp.(components.CharacterComponent)

				if char.Rune == targetChar {
					ctx.CursorX = x
					return
				}
			}
		}
	}
}

// isWordChar returns true if the rune is a word character (alphanumeric or underscore)
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// isPunctuation returns true if the rune is punctuation (not word char, not space)
func isPunctuation(r rune) bool {
	return !isWordChar(r) && r != ' '
}

// getCharAt returns the character at the given position, or 0 if empty
func getCharAt(ctx *engine.GameContext, x, y int) rune {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == y && pos.X == x {
			charComp, _ := ctx.World.GetComponent(entity, charType)
			char := charComp.(components.CharacterComponent)
			return char.Rune
		}
	}
	return 0 // No character at position (space)
}

// findNextWordStartVim finds the start of the next word (Vim-style: considers punctuation)
// Three-phase logic:
// Phase 1: Skip current character type (if not on space)
// Phase 2: Skip any spaces
// Phase 3: Stop at the first character of next word
func findNextWordStartVim(ctx *engine.GameContext) int {
	x := ctx.CursorX

	// Get current character
	currentChar := getCharAt(ctx, x, ctx.CursorY)

	// Phase 1: Skip current character type (if not on space)
	if currentChar != 0 { // Not on space
		if isWordChar(currentChar) {
			// Skip all word characters
			for x < ctx.GameWidth && isWordChar(getCharAt(ctx, x, ctx.CursorY)) {
				x++
			}
		} else if isPunctuation(currentChar) {
			// Skip all punctuation
			for x < ctx.GameWidth && isPunctuation(getCharAt(ctx, x, ctx.CursorY)) {
				x++
			}
		}
	} else {
		// On space: move at least one position forward
		x++
	}

	// Phase 2: Skip any spaces
	for x < ctx.GameWidth && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x++
	}

	// Phase 3: We're now at the first character of next word (or at edge)
	if x >= ctx.GameWidth {
		return ctx.CursorX // Stay in place if we hit the edge
	}

	// Ensure we advanced at least one position
	if x == ctx.CursorX {
		x++
		if x >= ctx.GameWidth {
			return ctx.CursorX
		}
	}

	return x
}

// findWordEndVim finds the end of the current/next word (Vim-style)
// If on a word character, move forward one then skip to end of next word
// If on space, skip spaces then find end of next word
// If on punctuation, move forward one then skip to end of next word
func findWordEndVim(ctx *engine.GameContext) int {
	x := ctx.CursorX

	// Move forward at least one position
	x++
	if x >= ctx.GameWidth {
		return ctx.CursorX // Can't move forward
	}

	// Skip any whitespace
	for x < ctx.GameWidth && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x++
	}

	if x >= ctx.GameWidth {
		return ctx.CursorX // Only whitespace ahead
	}

	// Now we're on the start of a word, find its end
	ch := getCharAt(ctx, x, ctx.CursorY)
	isWord := isWordChar(ch)

	// Move to the end of this word/punctuation group
	for x < ctx.GameWidth {
		nextChar := getCharAt(ctx, x+1, ctx.CursorY)

		// Stop if next char is space
		if nextChar == 0 {
			break
		}

		// Stop if changing from word to non-word (or vice versa)
		if isWord && !isWordChar(nextChar) {
			break
		}
		if !isWord && !isPunctuation(nextChar) {
			break
		}

		x++
	}

	return x
}

// findPrevWordStartVim finds the start of the previous word (Vim-style)
// Reverse three-phase logic:
// Phase 1: Move back one position
// Phase 2: Skip backward over any spaces
// Phase 3: Skip backward over the character type, then return start position
func findPrevWordStartVim(ctx *engine.GameContext) int {
	x := ctx.CursorX

	// Phase 1: Move back at least one position
	x--
	if x < 0 {
		return ctx.CursorX // Can't move back
	}

	// Phase 2: Skip backward over any spaces
	for x >= 0 && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x--
	}

	if x < 0 {
		// Only spaces before cursor - check if entire line is empty
		// If we went all the way back, stay in place on empty line
		return ctx.CursorX
	}

	// Phase 3: We're on a character, skip backward over same type to find start
	ch := getCharAt(ctx, x, ctx.CursorY)
	isWord := isWordChar(ch)

	// Move backward while still in same character type
	for x > 0 {
		prevChar := getCharAt(ctx, x-1, ctx.CursorY)

		// Stop if previous char is space
		if prevChar == 0 {
			break
		}

		// Stop if changing character type
		if isWord && !isWordChar(prevChar) {
			break
		}
		if !isWord && !isPunctuation(prevChar) {
			break
		}

		x--
	}

	return x
}

// findLineEnd finds the rightmost character on the current line
func findLineEnd(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := ctx.World.GetEntitiesWith(posType)

	maxX := 0
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == ctx.CursorY && pos.X > maxX {
			maxX = pos.X
		}
	}

	if maxX == 0 {
		return ctx.GameWidth - 1
	}
	return maxX
}

// deleteCharAt deletes the character at the given position
func deleteCharAt(ctx *engine.GameContext, x, y int) {
	entity := ctx.World.GetEntityAtPosition(x, y)
	if entity == 0 {
		return // No entity at position
	}

	// Check if it's green or blue to reset heat
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := ctx.World.GetComponent(entity, seqType)
	if ok {
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
			ctx.SetScoreIncrement(0) // Reset heat
		}
	}

	// Safely destroy entity (handles spatial index removal)
	ctx.World.SafeDestroyEntity(entity)
}

// WORD motion functions (space-delimited, treat all non-space as WORD)

// findNextWORDStart finds the start of the next WORD (space-delimited)
// WORD is any sequence of non-space characters separated by spaces
func findNextWORDStart(ctx *engine.GameContext) int {
	x := ctx.CursorX
	currentChar := getCharAt(ctx, x, ctx.CursorY)

	// Phase 1: Skip current WORD (all non-spaces)
	if currentChar != 0 { // Not on space
		for x < ctx.GameWidth && getCharAt(ctx, x, ctx.CursorY) != 0 {
			x++
		}
	} else {
		// On space: move at least one position forward
		x++
	}

	// Phase 2: Skip any spaces
	for x < ctx.GameWidth && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x++
	}

	// We're now at the first character of next WORD (or at edge)
	if x >= ctx.GameWidth {
		return ctx.CursorX // Stay in place if we hit the edge
	}

	// Ensure we advanced at least one position
	if x == ctx.CursorX {
		x++
		if x >= ctx.GameWidth {
			return ctx.CursorX
		}
	}

	return x
}

// findWORDEnd finds the end of the current/next WORD (space-delimited)
// Moves forward to the last character of the next WORD
func findWORDEnd(ctx *engine.GameContext) int {
	x := ctx.CursorX

	// Move forward at least one position
	x++
	if x >= ctx.GameWidth {
		return ctx.CursorX // Can't move forward
	}

	// Skip any whitespace
	for x < ctx.GameWidth && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x++
	}

	if x >= ctx.GameWidth {
		return ctx.CursorX // Only whitespace ahead
	}

	// Now we're on a non-space character, find the end of this WORD
	for x < ctx.GameWidth {
		nextChar := getCharAt(ctx, x+1, ctx.CursorY)
		if nextChar == 0 { // Next is space or edge
			break
		}
		x++
	}

	return x
}

// findPrevWORDStart finds the start of the previous WORD (space-delimited)
// Moves backward to the first character of the previous WORD
func findPrevWORDStart(ctx *engine.GameContext) int {
	x := ctx.CursorX

	// Move back at least one position
	x--
	if x < 0 {
		return ctx.CursorX // Can't move back
	}

	// Skip backward over any spaces
	for x >= 0 && getCharAt(ctx, x, ctx.CursorY) == 0 {
		x--
	}

	if x < 0 {
		return ctx.CursorX // Only spaces before cursor
	}

	// We're on a non-space character, skip backward to find the start of this WORD
	for x > 0 {
		prevChar := getCharAt(ctx, x-1, ctx.CursorY)
		if prevChar == 0 { // Previous is space
			break
		}
		x--
	}

	return x
}

// findFirstNonWhitespace finds the first non-whitespace character on the current line
func findFirstNonWhitespace(ctx *engine.GameContext) int {
	for x := 0; x < ctx.GameWidth; x++ {
		if getCharAt(ctx, x, ctx.CursorY) != 0 {
			return x
		}
	}
	return 0 // No non-whitespace found, go to line start
}

// findPrevEmptyLine finds the previous empty line (paragraph backward)
func findPrevEmptyLine(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})

	// Start searching from the line above current
	for y := ctx.CursorY - 1; y >= 0; y-- {
		// Check if this line has any characters
		hasChar := false
		entities := ctx.World.GetEntitiesWith(posType)
		for _, entity := range entities {
			posComp, _ := ctx.World.GetComponent(entity, posType)
			pos := posComp.(components.PositionComponent)
			if pos.Y == y {
				hasChar = true
				break
			}
		}

		if !hasChar {
			return y // Found empty line
		}
	}

	return 0 // No empty line found, go to top
}

// findNextEmptyLine finds the next empty line (paragraph forward)
func findNextEmptyLine(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})

	// Start searching from the line below current
	for y := ctx.CursorY + 1; y < ctx.GameHeight; y++ {
		// Check if this line has any characters
		hasChar := false
		entities := ctx.World.GetEntitiesWith(posType)
		for _, entity := range entities {
			posComp, _ := ctx.World.GetComponent(entity, posType)
			pos := posComp.(components.PositionComponent)
			if pos.Y == y {
				hasChar = true
				break
			}
		}

		if !hasChar {
			return y // Found empty line
		}
	}

	return ctx.GameHeight - 1 // No empty line found, go to bottom
}
