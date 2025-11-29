package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ExecuteMotion executes a vi motion command
func ExecuteMotion(ctx *engine.GameContext, cmd rune, count int) {
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
	case 'l', ' ': // Right
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
		// TODO: Expand to everything, not only vi-keys
		if ctx.ConsecutiveCount > 3 {
			ctx.State.SetHeat(0) // Reset heat after 3+ consecutive moves
		}
	} else {
		ctx.LastMoveKey = cmd
		ctx.ConsecutiveCount = 1
	}
}

// ExecuteFindChar executes the 'f' (find character) command
func ExecuteFindChar(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	ctx.LastFindChar = targetChar
	ctx.LastFindForward = true
	ctx.LastFindType = 'f'

	newX, found := findCharInDirection(ctx, pos.X, pos.Y, targetChar, count, true)
	if found {
		ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
			X: newX,
			Y: pos.Y,
		})
	}
}

// ExecuteFindCharBackward executes the 'F' (find character backward) command
func ExecuteFindCharBackward(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	ctx.LastFindChar = targetChar
	ctx.LastFindForward = false
	ctx.LastFindType = 'F'

	newX, found := findCharInDirection(ctx, pos.X, pos.Y, targetChar, count, false)
	if found {
		ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
			X: newX,
			Y: pos.Y,
		})
	}
}

// ExecuteTillChar executes the 't' (till character) command
func ExecuteTillChar(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	ctx.LastFindChar = targetChar
	ctx.LastFindForward = true
	ctx.LastFindType = 't'

	newX, found := findCharInDirection(ctx, pos.X, pos.Y, targetChar, count, true)
	if found && newX > pos.X+1 {
		ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
			X: newX - 1,
			Y: pos.Y,
		})
	} else if found && newX == pos.X+1 {
		// Target is adjacent, cursor doesn't move for 't'
	}
}

// ExecuteTillCharBackward executes the 'T' (till character backward) command
func ExecuteTillCharBackward(ctx *engine.GameContext, targetChar rune, count int) {
	if count == 0 {
		count = 1
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	ctx.LastFindChar = targetChar
	ctx.LastFindForward = false
	ctx.LastFindType = 'T'

	newX, found := findCharInDirection(ctx, pos.X, pos.Y, targetChar, count, false)
	if found && newX < pos.X-1 {
		ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
			X: newX + 1,
			Y: pos.Y,
		})
	}
}

// RepeatFindChar executes the ';' and ',' commands
func RepeatFindChar(ctx *engine.GameContext, reverse bool) {
	if ctx.LastFindType == 0 {
		return
	}

	originalChar := ctx.LastFindChar
	originalType := ctx.LastFindType
	originalForward := ctx.LastFindForward

	var executeType rune
	if reverse {
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
		executeType = ctx.LastFindType
	}

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

	ctx.LastFindChar = originalChar
	ctx.LastFindType = originalType
	ctx.LastFindForward = originalForward
}