package modes

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ExecuteMotion executes a vi motion command
func ExecuteMotion(ctx *engine.GameContext, cmd rune, count int) {
	if count == 0 {
		count = 1
	}

	// Get cursor position from ECS
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return // Cursor entity missing - should never happen
	}
	cursorX := pos.X
	cursorY := pos.Y

	switch cmd {
	case 'w': // Word forward (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findNextWordStartVim(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case 'W': // WORD forward (space-delimited)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findNextWORDStart(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case 'e': // Word end (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findWordEndVim(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case 'E': // WORD end (space-delimited)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findWORDEnd(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case 'b': // Word backward (vim-style: considers punctuation)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findPrevWordStartVim(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case 'B': // WORD backward (space-delimited)
		for i := 0; i < count; i++ {
			prevX := cursorX
			cursorX = findPrevWORDStart(ctx, cursorX, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorX == prevX {
				break
			}
		}
	case '0': // Line start
		cursorX = 0
	case '^': // First non-whitespace character
		cursorX = findFirstNonWhitespace(ctx, cursorY)
	case '$': // Line end (rightmost character)
		cursorX = findLineEnd(ctx, cursorY)
	case 'H': // Top of screen (same column)
		cursorY = 0
	case 'M': // Middle of screen (same column)
		cursorY = ctx.GameHeight / 2
	case 'L': // Bottom of screen (same column)
		cursorY = ctx.GameHeight - 1
	case '{': // Previous empty line (paragraph backward)
		for i := 0; i < count; i++ {
			prevY := cursorY
			cursorY = findPrevEmptyLine(ctx, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorY == prevY {
				break
			}
		}
	case '}': // Next empty line (paragraph forward)
		for i := 0; i < count; i++ {
			prevY := cursorY
			cursorY = findNextEmptyLine(ctx, cursorY)
			// Break if cursor didn't move (can't advance further)
			if cursorY == prevY {
				break
			}
		}
	case '%': // Matching bracket
		newX, newY := findMatchingBracket(ctx, cursorX, cursorY)
		if newX != -1 && newY != -1 {
			cursorX = newX
			cursorY = newY
		}
	case 'G': // Bottom
		cursorY = ctx.GameHeight - 1
	case 'g': // Top (when preceded by another 'g')
		cursorY = 0
	case 'x': // Delete character
		deleteCharAt(ctx, cursorX, cursorY)
	case 'h': // Left
		for i := 0; i < count; i++ {
			if cursorX > 0 {
				cursorX--
			} else {
				// Break early if we can't move further left
				break
			}
		}
	case 'j': // Down
		for i := 0; i < count; i++ {
			if cursorY < ctx.GameHeight-1 {
				cursorY++
			} else {
				// Break early if we can't move further down
				break
			}
		}
	case 'k': // Up
		for i := 0; i < count; i++ {
			if cursorY > 0 {
				cursorY--
			} else {
				// Break early if we can't move further up
				break
			}
		}
	case 'l': // Right
		for i := 0; i < count; i++ {
			if cursorX < ctx.GameWidth-1 {
				cursorX++
			} else {
				// Break early if we can't move further right
				break
			}
		}
	case ' ': // Space - behaves like 'l' in normal mode
		for i := 0; i < count; i++ {
			if cursorX < ctx.GameWidth-1 {
				cursorX++
			} else {
				// Break early if we can't move further right
				break
			}
		}
	}

	// Validate cursor position after motion
	cursorX, cursorY = validatePosition(ctx, cursorX, cursorY)

	// Write cursor position TO ECS
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: cursorX,
		Y: cursorY,
	})

	// Check for consecutive motion keys (heat penalty)
	if cmd == ctx.LastMoveKey && (cmd == 'h' || cmd == 'j' || cmd == 'k' || cmd == 'l') {
		ctx.ConsecutiveCount++
		if ctx.ConsecutiveCount > 3 {
			ctx.State.SetHeat(0) // Reset heat after 3+ consecutive moves
		}
	} else {
		ctx.LastMoveKey = cmd
		ctx.ConsecutiveCount = 1
	}
}

// ExecuteFindChar executes the 'f' (find character) command
// Finds the Nth occurrence of targetChar on the current line, where N = count
// If count is 0 or 1, finds the first occurrence
// If count > 1, finds the Nth occurrence (e.g., 2fa finds the 2nd 'a')
// If count exceeds available matches, moves to the last match found
func ExecuteFindChar(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	// Get cursor position
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return // Cursor entity missing - should never happen
	}
	cursorX := pos.X
	cursorY := pos.Y

	// Store last find state for ; and , commands
	ctx.LastFindChar = targetChar
	ctx.LastFindForward = true
	ctx.LastFindType = 'f'

	occurrencesFound := 0
	lastMatchX := -1

	for x := cursorX + 1; x < ctx.GameWidth; x++ {
		// O(1) lookup of entities at position
		entities := ctx.World.Positions.GetAllAt(x, cursorY)

		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == targetChar {
				occurrencesFound++
				lastMatchX = x
				if occurrencesFound == count {
					cursorX = x
					ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
						X: cursorX,
						Y: cursorY,
					})
					return
				}
			}
		}
	}

	// If count exceeds available matches but we found at least one match,
	// move to the last match found
	if lastMatchX != -1 {
		cursorX = lastMatchX
	}
	// Otherwise, cursor doesn't move (no matches found)

	// Write cursor position to ECS
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: cursorX,
		Y: cursorY,
	})
}

// ExecuteFindCharBackward executes the 'F' (find character backward) command
// Searches backward from CursorX - 1 to 0 on the current line
// Finds the Nth occurrence of targetChar, where N = count
// If count is 0 or 1, finds the first occurrence backward
// If count > 1, finds the Nth occurrence backward (e.g., 2Fa finds the 2nd 'a' backward)
// If count exceeds available matches, moves to the first match found (furthest back)
func ExecuteFindCharBackward(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	// Get cursor position from ECS
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}
	cursorX := pos.X
	cursorY := pos.Y

	// Store last find state for ; and , commands
	ctx.LastFindChar = targetChar
	ctx.LastFindForward = false
	ctx.LastFindType = 'F'

	occurrencesFound := 0
	firstMatchX := -1

	// Search backward from CursorX - 1 to 0
	for x := cursorX - 1; x >= 0; x-- {
		entities := ctx.World.Positions.GetAllAt(x, cursorY)

		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == targetChar {
				occurrencesFound++
				firstMatchX = x
				if occurrencesFound == count {
					cursorX = x
					ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
						X: cursorX,
						Y: cursorY,
					})
					return
				}
			}
		}
	}

	// If count exceeds available matches but we found at least one match,
	// move to the first match found (furthest back)
	if firstMatchX != -1 {
		cursorX = firstMatchX
	}
	// Otherwise, cursor doesn't move (no matches found)

	// Write cursor position to ECS
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: cursorX,
		Y: cursorY,
	})
}

// ExecuteTillChar executes the 't' (till character) command
// Finds the Nth occurrence of targetChar on the current line, then moves one position before it
// If count is 0 or 1, finds the first occurrence
// If count > 1, finds the Nth occurrence (e.g., 2ta finds the 2nd 'a')
// If count exceeds available matches, moves one position before the last match found
func ExecuteTillChar(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	// Get cursor position from ECS
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return // Cursor entity missing - should never happen
	}
	cursorX := pos.X
	cursorY := pos.Y

	// Store last find state for ; and , commands
	ctx.LastFindChar = targetChar
	ctx.LastFindForward = true
	ctx.LastFindType = 't'

	occurrencesFound := 0
	lastMatchX := -1

	for x := cursorX + 1; x < ctx.GameWidth; x++ {
		entities := ctx.World.Positions.GetAllAt(x, cursorY)

		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == targetChar {
				occurrencesFound++
				lastMatchX = x
				if occurrencesFound == count {
					// Move to one position before the match
					if x > cursorX+1 {
						cursorX = x - 1
					}
					ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
						X: cursorX,
						Y: cursorY,
					})
					return
				}
			}
		}
	}

	// If count exceeds available matches but we found at least one match,
	// move to one position before the last match found
	if lastMatchX != -1 && lastMatchX > cursorX+1 {
		cursorX = lastMatchX - 1
	}
	// Otherwise, cursor doesn't move (no matches found or match is too close)

	// Write cursor position to ECS
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: cursorX,
		Y: cursorY,
	})
}

// ExecuteTillCharBackward executes the 'T' (till character backward) command
// Searches backward from CursorX - 1 to 0 on the current line
// Finds the Nth occurrence of targetChar, then moves one position after it
// If count is 0 or 1, finds the first occurrence backward
// If count > 1, finds the Nth occurrence backward (e.g., 2Ta finds the 2nd 'a' backward)
// If count exceeds available matches, moves one position after the first match found (furthest back)
func ExecuteTillCharBackward(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	// Get cursor position from ECS
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return // Cursor entity missing - should never happen
	}
	cursorX := pos.X
	cursorY := pos.Y

	// Store last find state for ; and , commands
	ctx.LastFindChar = targetChar
	ctx.LastFindForward = false
	ctx.LastFindType = 'T'

	occurrencesFound := 0
	firstMatchX := -1

	// Search backward from CursorX - 1 to 0
	for x := cursorX - 1; x >= 0; x-- {
		entities := ctx.World.Positions.GetAllAt(x, cursorY)

		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == targetChar {
				occurrencesFound++
				firstMatchX = x
				if occurrencesFound == count {
					// Move to one position after the match
					if x < cursorX-1 {
						cursorX = x + 1
					}
					ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
						X: cursorX,
						Y: cursorY,
					})
					return
				}
			}
		}
	}

	// If count exceeds available matches but we found at least one match,
	// move to one position after the first match found (furthest back)
	if firstMatchX != -1 && firstMatchX < cursorX-1 {
		cursorX = firstMatchX + 1
	}
	// Otherwise, cursor doesn't move (no matches found or match is too close)

	// Write cursor position to ECS
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: cursorX,
		Y: cursorY,
	})
}

// RepeatFindChar executes the ';' and ',' commands to repeat the last find/till motion
// If reverse is false (';'), repeats in the same direction as the last find/till
// If reverse is true (','), repeats in the opposite direction
func RepeatFindChar(ctx *engine.GameContext, reverse bool) {
	// If no previous find/till command, do nothing
	if ctx.LastFindType == 0 {
		return
	}

	// Save the original find state (so we don't overwrite it during repeat)
	originalChar := ctx.LastFindChar
	originalType := ctx.LastFindType
	originalForward := ctx.LastFindForward

	// Determine the type to execute
	var executeType rune

	if reverse {
		// Reverse the type (f<->F, t<->T)
		switch ctx.LastFindType {
		case 'f':
			executeType = 'F'
		case 'F':
			executeType = 'f'
		case 't':
			executeType = 'T'
		case 'T':
			executeType = 't'
		}
	} else {
		// Same direction
		executeType = ctx.LastFindType
	}

	// Execute the appropriate find/till command
	switch executeType {
	case 'f':
		ExecuteFindChar(ctx, ctx.LastFindChar, 1)
	case 'F':
		ExecuteFindCharBackward(ctx, ctx.LastFindChar, 1)
	case 't':
		ExecuteTillChar(ctx, ctx.LastFindChar, 1)
	case 'T':
		ExecuteTillCharBackward(ctx, ctx.LastFindChar, 1)
	}

	// Restore the original find state (so ; and , don't change the original command)
	ctx.LastFindChar = originalChar
	ctx.LastFindType = originalType
	ctx.LastFindForward = originalForward
}

// isWordChar returns true if the rune is a word character (alphanumeric or underscore)
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// getCharAt returns the character at the given position, or 0 if empty.
// Returns:
//   - 0 for empty positions (no entity at position)
//   - 0 for space character entities (defensive handling - spaces should not exist as entities)
//   - The actual rune for all other characters
func getCharAt(ctx *engine.GameContext, x, y int) rune {
	// Use spatial grid to find entities at this position
	entities := ctx.World.Positions.GetAllAt(x, y)

	for _, entity := range entities {
		// Explicitly skip the cursor entity to see what's underneath
		// Also skip 0/invalid entities
		if entity == ctx.CursorEntity || entity == 0 {
			continue
		}

		char, ok := ctx.World.Characters.Get(entity)
		if ok {
			// Treat space characters as empty positions
			if char.Rune == ' ' {
				return 0
			}
			return char.Rune
		}
	}

	return 0
}

// findNextWordStartVim finds the start of the next word (Vim-style: considers punctuation)
// Three-phase logic:
// Phase 1: Skip current character type (if not on space)
// Phase 2: Skip any spaces
// Phase 3: Stop at the first character of next word
func findNextWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX

	// Get current character type
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	// Phase 1: Skip current character type (if not on space)
	if currentType != CharTypeSpace {
		// Skip all characters of the same type
		for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == currentType {
			x++
		}
	} else {
		// On space: move at least one position forward
		x++
	}

	// Phase 2: Skip any spaces
	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	// Phase 3: We're now at the first character of next word (or at edge)
	if x >= ctx.GameWidth {
		return cursorX // Stay in place if we hit the edge
	}

	// Ensure we advanced at least one position
	if x == cursorX {
		x++
		if x >= ctx.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findWordEndVim finds the end of the current/next word (Vim-style)
// If on a word character, move forward one then skip to end of next word
// If on space, skip spaces then find end of next word
// If on punctuation, move forward one then skip to end of next word
func findWordEndVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX

	// Move forward at least one position
	x++
	if x >= ctx.GameWidth {
		return cursorX // Can't move forward
	}

	// Skip any whitespace
	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.GameWidth {
		return cursorX // Only whitespace ahead
	}

	// Now we're on the start of a word, find its end
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	// Move to the end of this word/punctuation group
	for x < ctx.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)

		// Stop if next char is space
		if nextType == CharTypeSpace {
			break
		}

		// Stop if changing character type
		if nextType != currentType {
			break
		}

		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findPrevWordStartVim finds the start of the previous word (Vim-style)
// Reverse three-phase logic:
// Phase 1: Move back one position
// Phase 2: Skip backward over any spaces
// Phase 3: Skip backward over the character type, then return start position
func findPrevWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX

	// Phase 1: Move back at least one position
	x--
	if x < 0 {
		return cursorX // Can't move back
	}

	// Phase 2: Skip backward over any spaces
	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}

	if x < 0 {
		// Only spaces before cursor - check if entire line is empty
		// If we went all the way back, stay in place on empty line
		return cursorX
	}

	// Phase 3: We're on a character, skip backward over same type to find start
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	// Move backward while still in same character type
	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)

		// Stop if previous char is space
		if prevType == CharTypeSpace {
			break
		}

		// Stop if changing character type
		if prevType != currentType {
			break
		}

		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findLineEnd finds the rightmost character on the current line
func findLineEnd(ctx *engine.GameContext, cursorY int) int {
	entities := ctx.World.Query().With(ctx.World.Positions).Execute()

	maxX := 0
	for _, entity := range entities {
		pos, _ := ctx.World.Positions.Get(entity)
		if pos.Y == cursorY && pos.X > maxX {
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
	// Get all entities to find the actual character/nugget, ignoring cursor/drain
	entities := ctx.World.Positions.GetAllAt(x, y)

	var targetEntity engine.Entity

	for _, e := range entities {
		if e == 0 || e == ctx.CursorEntity {
			continue
		}

		// Prioritize deleting nuggets or sequences
		if ctx.World.Nuggets.Has(e) || ctx.World.Sequences.Has(e) {
			targetEntity = e
			break
		}
	}

	if targetEntity == 0 {
		return // Nothing deletable found
	}

	// Check if it's green or blue to reset heat
	if seq, ok := ctx.World.Sequences.Get(targetEntity); ok {
		if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
			ctx.State.SetHeat(0) // Reset heat
		}
	}

	// Safely destroy entity (handles spatial index removal)
	ctx.World.DestroyEntity(targetEntity)
}

// WORD motion functions (space-delimited, treat all non-space as WORD)

// findNextWORDStart finds the start of the next WORD (space-delimited)
// WORD is any sequence of non-space characters separated by spaces
func findNextWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	// Phase 1: Skip current WORD (all non-spaces)
	if currentType != CharTypeSpace {
		for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			x++
		}
	} else {
		// On space: move at least one position forward
		x++
	}

	// Phase 2: Skip any spaces
	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	// We're now at the first character of next WORD (or at edge)
	if x >= ctx.GameWidth {
		return cursorX // Stay in place if we hit the edge
	}

	// Ensure we advanced at least one position
	if x == cursorX {
		x++
		if x >= ctx.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findWORDEnd finds the end of the current/next WORD (space-delimited)
// Moves forward to the last character of the next WORD
func findWORDEnd(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX

	// Move forward at least one position
	x++
	if x >= ctx.GameWidth {
		return cursorX // Can't move forward
	}

	// Skip any whitespace
	for x < ctx.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.GameWidth {
		return cursorX // Only whitespace ahead
	}

	// Now we're on a non-space character, find the end of this WORD
	for x < ctx.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)
		if nextType == CharTypeSpace { // Next is space or edge
			break
		}
		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findPrevWORDStart finds the start of the previous WORD (space-delimited)
// Moves backward to the first character of the previous WORD
func findPrevWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX

	// Move back at least one position
	x--
	if x < 0 {
		return cursorX // Can't move back
	}

	// Skip backward over any spaces
	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}

	if x < 0 {
		return cursorX // Only spaces before cursor
	}

	// We're on a non-space character, skip backward to find the start of this WORD
	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)
		if prevType == CharTypeSpace { // Previous is space
			break
		}
		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// findFirstNonWhitespace finds the first non-whitespace character on the current line
func findFirstNonWhitespace(ctx *engine.GameContext, cursorY int) int {
	for x := 0; x < ctx.GameWidth; x++ {
		if getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			validX, _ := validatePosition(ctx, x, cursorY)
			return validX
		}
	}
	return 0 // No non-whitespace found, go to line start
}

// findPrevEmptyLine finds the previous empty line (paragraph backward)
func findPrevEmptyLine(ctx *engine.GameContext, cursorY int) int {
	entities := ctx.World.Query().With(ctx.World.Positions).Execute()

	// Start searching from the line above current
	for y := cursorY - 1; y >= 0; y-- {
		// Check if this line has any characters
		hasChar := false
		for _, entity := range entities {
			pos, _ := ctx.World.Positions.Get(entity)
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
func findNextEmptyLine(ctx *engine.GameContext, cursorY int) int {
	entities := ctx.World.Query().With(ctx.World.Positions).Execute()

	// Start searching from the line below current
	for y := cursorY + 1; y < ctx.GameHeight; y++ {
		// Check if this line has any characters
		hasChar := false
		for _, entity := range entities {
			pos, _ := ctx.World.Positions.Get(entity)
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