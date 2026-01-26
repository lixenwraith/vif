package mode

import (
	"github.com/lixenwraith/vi-fighter/engine"
)

// MotionLeft implements 'h' motion
func MotionLeft(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX := x
	for i := 0; i < count && endX > 0; i++ {
		endX -= (bounds.MaxX-bounds.MinX)/2 + 1
	}
	if endX < 0 {
		endX = 0
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
	bounds := ctx.GetPingBounds()
	endY := y
	maxY := ctx.World.Resources.Config.GameHeight - 1
	for i := 0; i < count && endY < maxY; i++ {
		endY += (bounds.MaxY-bounds.MinY)/2 + 1
	}
	if endY > maxY {
		endY = maxY
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
	bounds := ctx.GetPingBounds()
	endY := y
	for i := 0; i < count && endY > 0; i++ {
		endY -= (bounds.MaxY-bounds.MinY)/2 + 1
	}
	if endY < 0 {
		endY = 0
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
	bounds := ctx.GetPingBounds()
	endX := x
	maxX := ctx.World.Resources.Config.GameWidth - 1
	for i := 0; i < count && endX < maxX; i++ {
		endX += (bounds.MaxX-bounds.MinX)/2 + 1
	}
	if endX > maxX {
		endX = maxX
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
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findNextWordStartInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionWORDForward implements 'W' motion
func MotionWORDForward(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findNextWORDStartInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionWordEnd implements 'e' motion
func MotionWordEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findWordEndInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionWORDEnd implements 'E' motion
func MotionWORDEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findWORDEndInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionWordBack implements 'b' motion
func MotionWordBack(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findPrevWordStartInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionWORDBack implements 'B' motion
func MotionWORDBack(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findPrevWORDStartInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	endX, endY = validatePosition(ctx, endX, endY)
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleExclusive,
		Valid: endX != x || endY != y,
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
	bounds := ctx.GetPingBounds()

	endX, endY := findFirstNonWhitespaceInBounds(ctx, bounds)

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionLineEnd implements '$' motion
func MotionLineEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()

	lastEntityX := findLineEndInBounds(ctx, bounds)

	var endX int
	// Jump to screen edge if:
	// 1. No entities found (lastEntityX == -1)
	// 2. Already at the last entity (x == lastEntityX)
	if lastEntityX == -1 || x == lastEntityX {
		endX = ctx.World.Resources.Config.GameWidth - 1
	} else {
		endX = lastEntityX
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}

// MotionScreenVerticalMid implements 'M' motion
func MotionScreenVerticalMid(ctx *engine.GameContext, x, y, count int) MotionResult {
	midY := ctx.World.Resources.Config.GameHeight / 2
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: midY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: y != midY,
	}
}

// MotionScreenHorizontalMid implements 'm' motion
func MotionScreenHorizontalMid(ctx *engine.GameContext, x, y, count int) MotionResult {
	midX := ctx.World.Resources.Config.GameWidth / 2
	return MotionResult{
		StartX: x, StartY: y,
		EndX: midX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != midX,
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

// MotionScreenBottom implements 'G' motion
func MotionScreenBottom(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := ctx.World.Resources.Config.GameHeight - 1
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: y != endY,
	}
}

// MotionScreenTop implements 'gg' motion
func MotionScreenTop(ctx *engine.GameContext, x, y, count int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: 0,
		Type: RangeLine, Style: StyleInclusive,
		Valid: y != 0,
	}
}

// MotionEnd implements 'g$' motion (GameWidth-1,GameHeight-1)
func MotionEnd(ctx *engine.GameContext, x, y, count int) MotionResult {
	rightX := ctx.World.Resources.Config.GameWidth - 1
	botY := ctx.World.Resources.Config.GameHeight - 1
	return MotionResult{
		StartX: x, StartY: y,
		EndX: rightX, EndY: botY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != rightX || y != botY,
	}
}

// MotionCenter implements 'gm' motion (GameWidth/2,GameHeight/2)
func MotionCenter(ctx *engine.GameContext, x, y, count int) MotionResult {
	midX := ctx.World.Resources.Config.GameWidth / 2
	midY := ctx.World.Resources.Config.GameHeight / 2
	return MotionResult{
		StartX: x, StartY: y,
		EndX: midX, EndY: midY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != midX || y != midY,
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
	bounds := ctx.GetPingBounds()

	endX, endY, found := findCharInBounds(ctx, x, y, char, count, true, bounds)

	if !found {
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

// MotionFindBack implements 'F' motion (CharMotionFunc)
func MotionFindBack(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	bounds := ctx.GetPingBounds()

	endX, endY, found := findCharInBounds(ctx, x, y, char, count, false, bounds)

	if !found {
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

// MotionTillForward implements 't' motion (CharMotionFunc)
func MotionTillForward(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	bounds := ctx.GetPingBounds()

	endX, endY, found := findCharInBounds(ctx, x, y, char, count, true, bounds)

	if !found {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}

	// Till: stop one position before target
	// For bounds mode, need to handle row wrapping
	endX--
	if endX < 0 {
		endY--
		if endY < bounds.MinY {
			return MotionResult{
				StartX: x, StartY: y,
				EndX: x, EndY: y,
				Type: RangeChar, Style: StyleInclusive,
				Valid: false,
			}
		}
		endX = ctx.World.Resources.Config.GameWidth - 1
	}

	// Check we actually moved
	if endX == x && endY == y {
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

// MotionTillBack implements 'T' motion (CharMotionFunc)
func MotionTillBack(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	bounds := ctx.GetPingBounds()

	endX, endY, found := findCharInBounds(ctx, x, y, char, count, false, bounds)

	if !found {
		return MotionResult{
			StartX: x, StartY: y,
			EndX: x, EndY: y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: false,
		}
	}

	// Till: stop one position after target (moving backward)
	endX++
	if endX >= ctx.World.Resources.Config.GameWidth {
		endY++
		if endY > bounds.MaxY {
			return MotionResult{
				StartX: x, StartY: y,
				EndX: x, EndY: y,
				Type: RangeChar, Style: StyleInclusive,
				Valid: false,
			}
		}
		endX = 0
	}

	// Check we actually moved
	if endX == x && endY == y {
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

// MotionHalfPageLeft implements 'H' motion
func MotionHalfPageLeft(ctx *engine.GameContext, x, y, count int) MotionResult {
	halfWidth := ctx.World.Resources.Config.GameWidth / 2
	endX := x - (halfWidth * count)
	if endX < 0 {
		endX = 0
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionHalfPageRight implements 'L' motion
func MotionHalfPageRight(ctx *engine.GameContext, x, y, count int) MotionResult {
	halfWidth := ctx.World.Resources.Config.GameWidth / 2
	endX := x + (halfWidth * count)
	if endX >= ctx.World.Resources.Config.GameWidth {
		endX = ctx.World.Resources.Config.GameWidth - 1
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionHalfPageUp implements 'K' and PgUp motion
func MotionHalfPageUp(ctx *engine.GameContext, x, y, count int) MotionResult {
	halfHeight := ctx.World.Resources.Config.GameHeight / 2
	endY := y - (halfHeight * count)
	if endY < 0 {
		endY = 0
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionHalfPageDown implements 'J' and PgDown motion
func MotionHalfPageDown(ctx *engine.GameContext, x, y, count int) MotionResult {
	halfHeight := ctx.World.Resources.Config.GameHeight / 2
	endY := y + (halfHeight * count)
	if endY >= ctx.World.Resources.Config.GameHeight {
		endY = ctx.World.Resources.Config.GameHeight - 1
	}
	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionColumnUp implements [ and gk - jump to first non-space above in same column
func MotionColumnUp(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findColumnUpInBounds(ctx, endX, endY, bounds)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionColumnDown implements ] and gj - jump to first non-space below in same column
func MotionColumnDown(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.GetPingBounds()
	endX, endY := x, y

	for i := 0; i < count; i++ {
		newX, newY := findColumnDownInBounds(ctx, endX, endY, bounds, ctx.World.Resources.Config.GameHeight)
		if newX == endX && newY == endY {
			break
		}
		endX, endY = newX, newY
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}