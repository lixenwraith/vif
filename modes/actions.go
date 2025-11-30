package modes

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// executeSpecial handles special commands (x, D, n, N, ;, ,)
// Called by InputMachine when ActionSpecial is triggered
func executeSpecial(ctx *engine.GameContext, target rune, count int) {
	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	switch target {
	case 'x':
		// x is effectively "delete chars forward"
		endX := pos.X + count - 1
		if endX >= ctx.GameWidth {
			endX = ctx.GameWidth - 1
		}
		result := MotionResult{
			StartX: pos.X, StartY: pos.Y,
			EndX: endX, EndY: pos.Y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: true,
		}
		if OpDelete(ctx, result) {
			ctx.State.SetHeat(0)
		}

	case 'D':
		// D is effectively "d$"
		result := MotionLineEnd(ctx, pos.X, pos.Y, 1)
		if OpDelete(ctx, result) {
			ctx.State.SetHeat(0)
		}

	case 'n':
		RepeatSearch(ctx, true)

	case 'N':
		RepeatSearch(ctx, false)

	case ';':
		executeRepeatFind(ctx, false)

	case ',':
		executeRepeatFind(ctx, true)
	}
}

// executeRepeatFind repeats the last find/till command
func executeRepeatFind(ctx *engine.GameContext, reverse bool) {
	if ctx.LastFindType == 0 {
		return
	}

	pos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	originalChar := ctx.LastFindChar
	originalType := ctx.LastFindType
	originalForward := ctx.LastFindForward

	var charMotion CharMotionFunc
	var charCmd rune

	// Determine motion based on direction and reversal
	if reverse {
		switch ctx.LastFindType {
		case 'f':
			charMotion = MotionFindBack
			charCmd = 'F'
		case 'F':
			charMotion = MotionFindForward
			charCmd = 'f'
		case 't':
			charMotion = MotionTillBack
			charCmd = 'T'
		case 'T':
			charMotion = MotionTillForward
			charCmd = 't'
		}
	} else {
		switch ctx.LastFindType {
		case 'f':
			charMotion = MotionFindForward
			charCmd = 'f'
		case 'F':
			charMotion = MotionFindBack
			charCmd = 'F'
		case 't':
			charMotion = MotionTillForward
			charCmd = 't'
		case 'T':
			charMotion = MotionTillBack
			charCmd = 'T'
		}
	}

	result := charMotion(ctx, pos.X, pos.Y, 1, ctx.LastFindChar)
	OpMove(ctx, result, charCmd)

	// Restore original state because OpMove/CharMotion logic might update it to the 'reversed' type
	ctx.LastFindChar = originalChar
	ctx.LastFindType = originalType
	ctx.LastFindForward = originalForward
}