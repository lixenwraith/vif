package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// execCharMotion executes f/F/t/T commands
func execCharMotion(ctx *engine.GameContext, cmd rune, target rune, count int) {
	switch cmd {
	case 'f':
		ExecuteFindChar(ctx, target, count)
	case 'F':
		ExecuteFindCharBackward(ctx, target, count)
	case 't':
		ExecuteTillChar(ctx, target, count)
	case 'T':
		ExecuteTillCharBackward(ctx, target, count)
	}
	// Store for ; and , repeat (already done in Execute* functions, but ensure consistency)
	ctx.LastFindChar = target
	ctx.LastFindType = cmd
	ctx.LastFindForward = (cmd == 'f' || cmd == 't')
}

// execDeleteWithCharMotion executes df{char}, dt{char}, dF{char}, dT{char}
func execDeleteWithCharMotion(ctx *engine.GameContext, charCmd rune, target rune, count int) {
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}
	startX := pos.X
	startY := pos.Y

	// Find target position
	var endX int
	found := false

	switch charCmd {
	case 'f':
		// Find forward, delete up to and including target
		endX, found = findCharForward(ctx, startX, startY, target, count)
	case 't':
		// Till forward, delete up to but not including target
		endX, found = findCharForward(ctx, startX, startY, target, count)
		if found && endX > startX {
			endX--
		}
	case 'F':
		// Find backward, delete from target to cursor (exclusive of cursor)
		endX, found = findCharBackward(ctx, startX, startY, target, count)
		if found {
			// Delete from endX to startX-1
			deleteRange(ctx, endX, startX-1, startY)
			// Move cursor to endX
			ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: endX, Y: startY})
			return
		}
	case 'T':
		// Till backward, delete from after target to cursor (exclusive of cursor)
		endX, found = findCharBackward(ctx, startX, startY, target, count)
		if found && endX < startX-1 {
			endX++
			deleteRange(ctx, endX, startX-1, startY)
			ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: endX, Y: startY})
			return
		}
		return
	}

	if found && endX >= startX {
		deleteRange(ctx, startX, endX, startY)
	}

	// Store for ; and , repeat
	ctx.LastFindChar = target
	ctx.LastFindType = charCmd
	ctx.LastFindForward = (charCmd == 'f' || charCmd == 't')
}

// findCharForward finds the Nth occurrence of target char forward from startX
// Returns x position and whether found
func findCharForward(ctx *engine.GameContext, startX, startY int, target rune, count int) (int, bool) {
	occurrences := 0
	lastX := -1

	for x := startX + 1; x < ctx.GameWidth; x++ {
		entities := ctx.World.Positions.GetAllAt(x, startY)
		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == target {
				occurrences++
				lastX = x
				if occurrences == count {
					return x, true
				}
			}
		}
	}

	// Return last found if count exceeded available
	if lastX != -1 {
		return lastX, true
	}
	return -1, false
}

// findCharBackward finds the Nth occurrence of target char backward from startX
// Returns x position and whether found
func findCharBackward(ctx *engine.GameContext, startX, startY int, target rune, count int) (int, bool) {
	occurrences := 0
	firstX := -1

	for x := startX - 1; x >= 0; x-- {
		entities := ctx.World.Positions.GetAllAt(x, startY)
		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			char, ok := ctx.World.Characters.Get(entity)
			if ok && char.Rune == target {
				occurrences++
				if firstX == -1 {
					firstX = x
				}
				if occurrences == count {
					return x, true
				}
			}
		}
	}

	// Return first found (furthest back) if count exceeded available
	if firstX != -1 {
		return firstX, true
	}
	return -1, false
}

func execDeleteChar(ctx *engine.GameContext, count int) {
	ExecuteDeleteMotion(ctx, 'x', count)
}

func execDeleteToEOL(ctx *engine.GameContext, count int) {
	ExecuteDeleteMotion(ctx, '$', count)
}

func execSearchNext(ctx *engine.GameContext, count int) {
	RepeatSearch(ctx, true)
}

func execSearchPrev(ctx *engine.GameContext, count int) {
	RepeatSearch(ctx, false)
}

func execRepeatFind(ctx *engine.GameContext, count int) {
	RepeatFindChar(ctx, false)
}

func execRepeatFindReverse(ctx *engine.GameContext, count int) {
	RepeatFindChar(ctx, true)
}

func execGotoTop(ctx *engine.GameContext, count int) {
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if ok {
		pos.Y = 0
		ctx.World.Positions.Add(ctx.CursorEntity, pos)
	}
}

func execGotoOrigin(ctx *engine.GameContext, count int) {
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: 0, Y: 0})
}
