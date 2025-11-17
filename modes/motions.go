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
	case 'w': // Word forward
		for i := 0; i < count; i++ {
			ctx.CursorX = findNextWordStart(ctx)
		}
	case 'e': // Word end
		for i := 0; i < count; i++ {
			ctx.CursorX = findWordEnd(ctx)
		}
	case 'b': // Word backward
		for i := 0; i < count; i++ {
			ctx.CursorX = findPrevWordStart(ctx)
		}
	case '0': // Line start
		ctx.CursorX = 0
	case '$': // Line end (rightmost character)
		ctx.CursorX = findLineEnd(ctx)
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
	}

	// Check for consecutive motion keys (heat penalty)
	if cmd == ctx.LastMoveKey && (cmd == 'h' || cmd == 'j' || cmd == 'k' || cmd == 'l') {
		ctx.ConsecutiveCount++
		if ctx.ConsecutiveCount > 3 {
			ctx.ScoreIncrement = 0 // Reset heat after 3+ consecutive moves
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

// findNextWordStart finds the start of the next word
func findNextWordStart(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)

	// Build map of positions with characters on current line
	charPositions := make(map[int]bool)
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == ctx.CursorY {
			charPositions[pos.X] = true
		}
	}

	// Find next gap (transition from char to no-char)
	inWord := charPositions[ctx.CursorX]
	for x := ctx.CursorX + 1; x < ctx.GameWidth; x++ {
		hasChar := charPositions[x]
		if !inWord && hasChar {
			return x // Found start of next word
		}
		inWord = hasChar
	}

	return ctx.CursorX // No word found, stay at current position
}

// findWordEnd finds the end of the current/next word
func findWordEnd(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)

	// Build map of positions with characters on current line
	charPositions := make(map[int]bool)
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == ctx.CursorY {
			charPositions[pos.X] = true
		}
	}

	// Find next gap (transition from char to no-char)
	for x := ctx.CursorX + 1; x < ctx.GameWidth; x++ {
		if charPositions[x] && !charPositions[x+1] {
			return x // Found end of word
		}
	}

	return ctx.CursorX // No word end found, stay at current position
}

// findPrevWordStart finds the start of the previous word
func findPrevWordStart(ctx *engine.GameContext) int {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)

	// Build map of positions with characters on current line
	charPositions := make(map[int]bool)
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == ctx.CursorY {
			charPositions[pos.X] = true
		}
	}

	// Start from cursor position - 1
	x := ctx.CursorX - 1

	// Skip any whitespace (positions without characters)
	for x >= 0 && !charPositions[x] {
		x--
	}

	// If we're still >= 0, we're at a character (end of previous word)
	// Find the start of this word
	if x < 0 {
		// No previous word found, stay at current position
		return ctx.CursorX
	}

	// We're at a character, find the start of this word
	for x >= 0 && charPositions[x] {
		x--
	}

	// x is now at the position before the word starts, so word starts at x+1
	return x + 1
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

	// Check if it's a green or blue character (reset heat if so)
	// Also check if it's a red character (play decay sound)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := ctx.World.GetComponent(entity, seqType)
	if ok {
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
			ctx.ScoreIncrement = 0 // Reset heat
		} else if seq.Type == components.SequenceRed {
			// Play decay sound for red character deletion
			if ctx.SoundEnabled.Load() {
				ctx.SoundMu.RLock()
				if ctx.SoundManager != nil {
					ctx.SoundManager.PlayDecay()
				}
				ctx.SoundMu.RUnlock()
			}
		}
	}

	// Remove from spatial index
	ctx.World.RemoveFromSpatialIndex(x, y)

	// Destroy entity
	ctx.World.DestroyEntity(entity)
}
