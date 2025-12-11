// @focus: #control { action }
package modes

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

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

	// Determine motion based on direction and reversal
	if reverse {
		switch ctx.LastFindType {
		case 'f':
			charMotion = MotionFindBack
		case 'F':
			charMotion = MotionFindForward
		case 't':
			charMotion = MotionTillBack
		case 'T':
			charMotion = MotionTillForward
		}
	} else {
		switch ctx.LastFindType {
		case 'f':
			charMotion = MotionFindForward
		case 'F':
			charMotion = MotionFindBack
		case 't':
			charMotion = MotionTillForward
		case 'T':
			charMotion = MotionTillBack
		}
	}

	result := charMotion(ctx, pos.X, pos.Y, 1, ctx.LastFindChar)
	OpMove(ctx, result)

	// Restore original state because OpMove/CharMotion logic might update it to the 'reversed' type
	ctx.LastFindChar = originalChar
	ctx.LastFindType = originalType
	ctx.LastFindForward = originalForward
}