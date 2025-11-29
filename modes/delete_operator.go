package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// ExecuteDeleteMotion executes a delete operator with a motion
func ExecuteDeleteMotion(ctx *engine.GameContext, motion rune, count int) {
	if count == 0 {
		count = 1
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	startX, startY := pos.X, pos.Y
	deletedGreenOrBlue := false

	switch motion {
	case 'd':
		deletedGreenOrBlue = deleteAllOnLine(ctx, startY)

	case 'x', 'l', ' ':
		endDel := startX + count - 1
		deletedGreenOrBlue = deleteRange(ctx, startX, endDel, startY)

	case 'h':
		startDel := startX - count
		if startDel < 0 {
			startDel = 0
		}
		if startDel < startX {
			deletedGreenOrBlue = deleteRange(ctx, startDel, startX-1, startY)
			ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: startDel, Y: startY})
		}

	case '0':
		deletedGreenOrBlue = deleteRange(ctx, 0, startX, startY)

	case '^':
		firstNonWS := findFirstNonWhitespace(ctx, startY)
		deletedGreenOrBlue = deleteRange(ctx, firstNonWS, startX, startY)

	case '$':
		endX := findLineEnd(ctx, startY)
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'w':
		endX := startX
		for i := 0; i < count; i++ {
			prevX := endX
			endX = findNextWordStartVim(ctx, endX, startY)
			if endX == prevX {
				break
			}
		}
		if endX > startX {
			deletedGreenOrBlue = deleteRange(ctx, startX, endX-1, startY)
		}

	case 'W':
		endX := startX
		for i := 0; i < count; i++ {
			prevX := endX
			endX = findNextWORDStart(ctx, endX, startY)
			if endX == prevX {
				break
			}
		}
		if endX > startX {
			deletedGreenOrBlue = deleteRange(ctx, startX, endX-1, startY)
		}

	case 'e':
		endX := startX
		for i := 0; i < count; i++ {
			prevX := endX
			endX = findWordEndVim(ctx, endX, startY)
			if endX == prevX {
				break
			}
		}
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'E':
		endX := startX
		for i := 0; i < count; i++ {
			prevX := endX
			endX = findWORDEnd(ctx, endX, startY)
			if endX == prevX {
				break
			}
		}
		deletedGreenOrBlue = deleteRange(ctx, startX, endX, startY)

	case 'b':
		startWordX := startX
		for i := 0; i < count; i++ {
			prevX := startWordX
			startWordX = findPrevWordStartVim(ctx, startWordX, startY)
			if startWordX == prevX {
				break
			}
		}
		deletedGreenOrBlue = deleteRange(ctx, startWordX, startX, startY)

	case 'B':
		startWordX := startX
		for i := 0; i < count; i++ {
			prevX := startWordX
			startWordX = findPrevWORDStart(ctx, startWordX, startY)
			if startWordX == prevX {
				break
			}
		}
		deletedGreenOrBlue = deleteRange(ctx, startWordX, startX, startY)

	case 'j':
		for i := 0; i <= count; i++ {
			y := startY + i
			if y < ctx.GameHeight {
				if deleteAllOnLine(ctx, y) {
					deletedGreenOrBlue = true
				}
			}
		}

	case 'k':
		for i := 0; i <= count; i++ {
			y := startY - i
			if y >= 0 {
				if deleteAllOnLine(ctx, y) {
					deletedGreenOrBlue = true
				}
			}
		}

	case '{':
		targetY := startY
		for i := 0; i < count; i++ {
			prevY := targetY
			targetY = findPrevEmptyLine(ctx, targetY)
			if targetY == prevY {
				break
			}
		}
		startRange, endRange := targetY, startY
		if startRange > endRange {
			startRange, endRange = endRange, startRange
		}
		for y := startRange; y <= endRange; y++ {
			if deleteAllOnLine(ctx, y) {
				deletedGreenOrBlue = true
			}
		}

	case '}':
		targetY := startY
		for i := 0; i < count; i++ {
			prevY := targetY
			targetY = findNextEmptyLine(ctx, targetY)
			if targetY == prevY {
				break
			}
		}
		startRange, endRange := startY, targetY
		if startRange > endRange {
			startRange, endRange = endRange, startRange
		}
		for y := startRange; y <= endRange; y++ {
			if deleteAllOnLine(ctx, y) {
				deletedGreenOrBlue = true
			}
		}

	case 'G':
		for y := startY; y < ctx.GameHeight; y++ {
			if y == startY {
				if deleteRange(ctx, startX, ctx.GameWidth-1, y) {
					deletedGreenOrBlue = true
				}
			} else {
				if deleteAllOnLine(ctx, y) {
					deletedGreenOrBlue = true
				}
			}
		}

	case 'g':
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

	if deletedGreenOrBlue {
		ctx.State.SetHeat(0)
	}
}

// deleteAllOnLine deletes all interactable entities on a line
func deleteAllOnLine(ctx *engine.GameContext, y int) bool {
	entities := ctx.World.Query().With(ctx.World.Positions).Execute()

	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	for _, entity := range entities {
		pos, _ := ctx.World.Positions.Get(entity)
		if pos.Y != y {
			continue
		}
		if !engine.IsInteractable(ctx.World, entity) {
			continue
		}
		if seq, ok := ctx.World.Sequences.Get(entity); ok {
			if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
				deletedGreenOrBlue = true
			}
		}
		entitiesToDelete = append(entitiesToDelete, entity)
	}

	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}

// deleteRange deletes all interactable entities in a range on a line
func deleteRange(ctx *engine.GameContext, startX, endX, y int) bool {
	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	if startX > endX {
		startX, endX = endX, startX
	}

	for x := startX; x <= endX; x++ {
		entities := ctx.World.Positions.GetAllAt(x, y)
		for _, entity := range entities {
			if !engine.IsInteractable(ctx.World, entity) {
				continue
			}
			if seq, ok := ctx.World.Sequences.Get(entity); ok {
				if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
					deletedGreenOrBlue = true
				}
			}
			entitiesToDelete = append(entitiesToDelete, entity)
		}
	}

	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}