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
	startX, startY := pos.X, pos.Y

	switch charCmd {
	case 'f':
		endX, found := findCharInDirection(ctx, startX, startY, target, count, true)
		if found && endX >= startX {
			deleteRange(ctx, startX, endX, startY)
		}
	case 't':
		endX, found := findCharInDirection(ctx, startX, startY, target, count, true)
		if found && endX > startX+1 {
			deleteRange(ctx, startX, endX-1, startY)
		}
	case 'F':
		endX, found := findCharInDirection(ctx, startX, startY, target, count, false)
		if found {
			deleteRange(ctx, endX, startX-1, startY)
			ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: endX, Y: startY})
		}
	case 'T':
		endX, found := findCharInDirection(ctx, startX, startY, target, count, false)
		if found && endX < startX-1 {
			deleteRange(ctx, endX+1, startX-1, startY)
			ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: endX + 1, Y: startY})
		}
	}

	ctx.LastFindChar = target
	ctx.LastFindType = charCmd
	ctx.LastFindForward = (charCmd == 'f' || charCmd == 't')
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