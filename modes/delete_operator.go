package modes

import (
	"reflect"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ExecuteDeleteMotion executes a delete operator with a motion
func ExecuteDeleteMotion(ctx *engine.GameContext, motion rune, count int) {
	if count == 0 {
		count = 1
	}

	startX, startY := ctx.CursorX, ctx.CursorY
	deletedGreenOrBlue := false

	switch motion {
	case 'd': // dd - delete line
		deletedGreenOrBlue = deleteAllOnLine(ctx, ctx.CursorY)

	case '0': // d0 - delete to line start
		deletedGreenOrBlue = deleteRange(ctx, 0, startX, startY)

	case '^': // d^ - delete to first non-whitespace
		firstNonWS := findFirstNonWhitespace(ctx)
		deletedGreenOrBlue = deleteRange(ctx, firstNonWS, startX, startY)

	case '$': // d$ - delete to line end
		endX := findLineEnd(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'w': // dw - delete word (vim-style)
		endX := findNextWordStartVim(ctx)
		if endX > startX {
			deletedGreenOrBlue = deleteRange(ctx, startX, endX-1, startY)
		}

	case 'W': // dW - delete WORD (space-delimited)
		endX := findNextWORDStart(ctx)
		if endX > startX {
			deletedGreenOrBlue = deleteRange(ctx, startX, endX-1, startY)
		}

	case 'e': // de - delete to end of word (vim-style)
		endX := findWordEndVim(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'E': // dE - delete to end of WORD (space-delimited)
		endX := findWORDEnd(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'b': // db - delete word backward (vim-style)
		startWordX := findPrevWordStartVim(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startWordX, startX, startY)

	case 'B': // dB - delete WORD backward (space-delimited)
		startWordX := findPrevWORDStart(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startWordX, startX, startY)

	case '{': // d{ - delete to previous empty line
		targetY := findPrevEmptyLine(ctx)
		for y := targetY; y <= startY; y++ {
			if deleteAllOnLine(ctx, y) {
				deletedGreenOrBlue = true
			}
		}

	case '}': // d} - delete to next empty line
		targetY := findNextEmptyLine(ctx)
		for y := startY; y <= targetY; y++ {
			if deleteAllOnLine(ctx, y) {
				deletedGreenOrBlue = true
			}
		}

	case 'G': // dG - delete to end of file
		// Delete from cursor to end of screen
		for y := startY; y < ctx.GameHeight; y++ {
			if y == startY {
				endX := ctx.GameWidth - 1
				if deleteRange(ctx, startX, endX, y) {
					deletedGreenOrBlue = true
				}
			} else {
				if deleteAllOnLine(ctx, y) {
					deletedGreenOrBlue = true
				}
			}
		}

	case 'g': // dgg - delete to beginning of file (when count==2 for 'gg')
		// Delete from beginning to cursor
		for y := 0; y <= startY; y++ {
			if y == startY {
				if deleteRange(ctx, 0, startX, y) {
					deletedGreenOrBlue = true
				}
			} else {
				if deleteAllOnLine(ctx, y) {
					deletedGreenOrBlue = true
				}
			}
		}
	}

	// Reset heat only if green or blue was deleted
	if deletedGreenOrBlue {
		ctx.State.SetHeat(0)
	}

	ctx.DeleteOperator = false
}

// deleteAllOnLine deletes all characters on a line
func deleteAllOnLine(ctx *engine.GameContext, y int) bool {
	posType := reflect.TypeOf(components.PositionComponent{})
	seqType := reflect.TypeOf(components.SequenceComponent{})

	entities := ctx.World.GetEntitiesWith(posType)

	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == y {
			// Check if green or blue
			seqComp, ok := ctx.World.GetComponent(entity, seqType)
			if ok {
				seq := seqComp.(components.SequenceComponent)
				if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
					deletedGreenOrBlue = true
				}
			}

			entitiesToDelete = append(entitiesToDelete, entity)
		}
	}

	// Delete all entities
	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}

// deleteRange deletes all characters in a range on a line
// Now uses spatial index to efficiently handle gaps (positions without entities)
func deleteRange(ctx *engine.GameContext, startX, endX, y int) bool {
	seqType := reflect.TypeOf(components.SequenceComponent{})

	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	// Ensure startX <= endX
	if startX > endX {
		startX, endX = endX, startX
	}

	// Iterate through the position range and use spatial index to find entities
	for x := startX; x <= endX; x++ {
		entity := ctx.World.GetEntityAtPosition(x, y)

		// Skip positions without entities (gaps, including spaces)
		if entity == 0 {
			continue
		}

		// Check if green or blue
		seqComp, ok := ctx.World.GetComponent(entity, seqType)
		if ok {
			seq := seqComp.(components.SequenceComponent)
			if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
				deletedGreenOrBlue = true
			}
		}

		entitiesToDelete = append(entitiesToDelete, entity)
	}

	// Delete all entities in range
	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}