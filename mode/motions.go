package mode

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// MotionLeft implements 'h' motion
func MotionLeft(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count && endX > 0; i++ {
		endX--
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionDown implements 'j' motion
func MotionDown(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := y
	for i := 0; i < count && endY < ctx.GameHeight-1; i++ {
		endY++
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionUp implements 'k' motion
func MotionUp(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := y
	for i := 0; i < count && endY > 0; i++ {
		endY--
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionRight implements 'l' and space motion
func MotionRight(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count && endX < ctx.GameWidth-1; i++ {
		endX++
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionWordForward implements 'w' motion
func MotionWordForward(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findNextWordStartVim(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x,
	}
}

// MotionWORDForward implements 'W' motion
func MotionWORDForward(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findNextWORDStart(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x,
	}
}

// MotionWordEnd implements 'e' motion
func MotionWordEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findWordEndVim(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionWORDEnd implements 'E' motion
func MotionWORDEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findWORDEnd(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionWordBack implements 'b' motion
func MotionWordBack(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findPrevWordStartVim(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x,
	}
}

// MotionWORDBack implements 'B' motion
func MotionWORDBack(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := x
	for i := 0; i < count; i++ {
		prev := endX
		endX = findPrevWORDStart(ctx, endX, y)
		if endX == prev {
			break
		}
	}
	endX, y = validatePosition(ctx, endX, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x,
	}
}

// MotionLineStart implements '0' motion
func MotionLineStart(ctx *engine.GameContext, x, y, count int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: 0, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != 0,
	}
}

// MotionFirstNonWS implements '^' motion
func MotionFirstNonWS(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX := findFirstNonWhitespace(ctx, y)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionLineEnd implements '$' motion
func MotionLineEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	lastEntityX := findLineEnd(ctx, y)
	var endX int

	// Jump to screen edge if:
	// 1. Line is empty (lastEntityX == -1)
	// 2. We are already at the last entity (x == lastEntityX)
	if lastEntityX == -1 || x == lastEntityX {
		endX = ctx.GameWidth - 1
	} else {
		// Otherwise jump to the last entity
		endX = lastEntityX
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}

// MotionScreenTop implements 'H' motion
func MotionScreenTop(ctx *engine.GameContext, x, y, count int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: 0,
		Type: RangeChar, Style: StyleInclusive,
		Valid: y != 0,
	}
}

// MotionScreenMid implements 'M' motion
func MotionScreenMid(ctx *engine.GameContext, x, y, count int) MotionResult {
	midY := ctx.GameHeight / 2
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: midY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: y != midY,
	}
}

// MotionScreenBot implements 'L' motion
func MotionScreenBot(ctx *engine.GameContext, x, y, count int) MotionResult {
	botY := ctx.GameHeight - 1
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: botY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: y != botY,
	}
}

// MotionParaBack implements '{' motion
func MotionParaBack(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := y
	for i := 0; i < count; i++ {
		prev := endY
		endY = findPrevEmptyLine(ctx, endY)
		if endY == prev {
			break
		}
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionParaForward implements '}' motion
func MotionParaForward(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := y
	for i := 0; i < count; i++ {
		prev := endY
		endY = findNextEmptyLine(ctx, endY)
		if endY == prev {
			break
		}
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionMatchBracket implements '%' motion
func MotionMatchBracket(ctx *engine.GameContext, x, y, count int) MotionResult {
	endX, endY := findMatchingBracket(ctx, x, y)
	if endX == -1 || endY == -1 {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}

// MotionFileEnd implements 'G' motion
func MotionFileEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := ctx.GameHeight - 1
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: y != endY,
	}
}

// MotionFileStart implements 'gg' motion
func MotionFileStart(ctx *engine.GameContext, x, y, count int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: 0,
		Type: RangeLine, Style: StyleInclusive,
		Valid: y != 0,
	}
}

// MotionOrigin implements 'go' motion (0,0)
func MotionOrigin(ctx *engine.GameContext, x, y, count int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: 0, EndY: 0,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != 0 || y != 0,
	}
}

// MotionFindForward implements 'f' motion (CharMotionFunc)
func MotionFindForward(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	endX, found := findCharInDirection(ctx, x, y, char, count, true)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: found,
	}
}

// MotionFindBack implements 'F' motion (CharMotionFunc)
func MotionFindBack(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	endX, found := findCharInDirection(ctx, x, y, char, count, false)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: found,
	}
}

// MotionTillForward implements 't' motion (CharMotionFunc)
// Returns adjusted position (target-1) with StyleInclusive per clarification
func MotionTillForward(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	endX, found := findCharInDirection(ctx, x, y, char, count, true)
	if !found {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}
	// Adjacent target: no valid motion (Vim behavior)
	if endX <= x+1 {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX - 1, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}

// MotionTillBack implements 'T' motion (CharMotionFunc)
// Returns adjusted position (target+1) with StyleInclusive per clarification
func MotionTillBack(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	endX, found := findCharInDirection(ctx, x, y, char, count, false)
	if !found {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}
	// Adjacent target: no valid motion (Vim behavior)
	if endX >= x-1 {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX + 1, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}