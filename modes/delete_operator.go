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

	case '$': // d$ - delete to line end
		endX := findLineEnd(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'w': // dw - delete word
		endX := findNextWordStart(ctx)
		if endX > startX {
			deletedGreenOrBlue = deleteRange(ctx, startX, endX-1, startY)
		}

	case 'e': // de - delete to end of word
		endX := findWordEnd(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'b': // db - delete word backward
		startWordX := findPrevWordStart(ctx)
		deletedGreenOrBlue = deleteRange(ctx, startWordX, startX, startY)

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
		ctx.SetScoreIncrement(0)
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
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)
		ctx.World.RemoveFromSpatialIndex(pos.X, pos.Y)
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}

// deleteRange deletes all characters in a range on a line
func deleteRange(ctx *engine.GameContext, startX, endX, y int) bool {
	posType := reflect.TypeOf(components.PositionComponent{})
	seqType := reflect.TypeOf(components.SequenceComponent{})

	entities := ctx.World.GetEntitiesWith(posType)

	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	// Ensure startX <= endX
	if startX > endX {
		startX, endX = endX, startX
	}

	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == y && pos.X >= startX && pos.X <= endX {
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

	// Delete all entities in range
	for _, entity := range entitiesToDelete {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)
		ctx.World.RemoveFromSpatialIndex(pos.X, pos.Y)
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}
