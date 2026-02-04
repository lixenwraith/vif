package mode

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/input"
)

// MotionLeft implements 'h' motion
func MotionLeft(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.World.GetPingAbsoluteBounds()
	endX := x

	for i := 0; i < count && endX > 0; i++ {
		r := max(bounds.MaxX-x, x-bounds.MinX)
		if r == 0 {
			r = 1
		}
		nextX := endX - r
		if nextX < 0 {
			nextX = 0
		}
		if isCursorBlocked(ctx, nextX, y) {
			break
		}
		endX = nextX
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
	bounds := ctx.World.GetPingAbsoluteBounds()
	endY := y
	maxY := ctx.World.Resources.Config.GameHeight - 1

	for i := 0; i < count && endY < maxY; i++ {
		r := max(bounds.MaxY-y, y-bounds.MinY)
		if r == 0 {
			r = 1
		}
		nextY := endY + r
		if nextY > maxY {
			nextY = maxY
		}
		if isCursorBlocked(ctx, x, nextY) {
			break
		}
		endY = nextY
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
	bounds := ctx.World.GetPingAbsoluteBounds()
	endY := y

	for i := 0; i < count && endY > 0; i++ {
		r := max(bounds.MaxY-y, y-bounds.MinY)
		if r == 0 {
			r = 1
		}
		nextY := endY - r
		if nextY < 0 {
			nextY = 0
		}
		if isCursorBlocked(ctx, x, nextY) {
			break
		}
		endY = nextY
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
	bounds := ctx.World.GetPingAbsoluteBounds()
	endX := x
	maxX := ctx.World.Resources.Config.GameWidth - 1

	for i := 0; i < count && endX < maxX; i++ {
		r := max(bounds.MaxX-x, x-bounds.MinX)
		if r == 0 {
			r = 1
		}
		nextX := endX + r
		if nextX > maxX {
			nextX = maxX
		}
		if isCursorBlocked(ctx, nextX, y) {
			break
		}
		endX = nextX
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	bounds := ctx.World.GetPingAbsoluteBounds()
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
	endX := 0

	// Scan rightward to find first unblocked position
	maxX := ctx.World.Resources.Config.GameWidth - 1
	for endX < x && isCursorBlocked(ctx, endX, y) {
		endX++
		if endX > maxX {
			endX = x
			break
		}
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x,
	}
}

// MotionFirstNonWS implements '^' motion
func MotionFirstNonWS(ctx *engine.GameContext, x, y, count int) MotionResult {
	bounds := ctx.World.GetPingAbsoluteBounds()

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
	bounds := ctx.World.GetPingAbsoluteBounds()

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

	// If target is blocked, scan backward to find last unblocked position
	for endX > x && isCursorBlocked(ctx, endX, y) {
		endX--
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

	if isCursorBlocked(ctx, x, midY) {
		// Search both directions from midY
		upY, downY := midY-1, midY+1
		maxY := ctx.World.Resources.Config.GameHeight - 1
		for upY >= 0 || downY <= maxY {
			if upY >= 0 && !isCursorBlocked(ctx, x, upY) {
				midY = upY
				break
			}
			if downY <= maxY && !isCursorBlocked(ctx, x, downY) {
				midY = downY
				break
			}
			upY--
			downY++
		}
	}

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

	if isCursorBlocked(ctx, midX, y) {
		// Search both directions from midX
		leftX, rightX := midX-1, midX+1
		maxX := ctx.World.Resources.Config.GameWidth - 1
		for leftX >= 0 || rightX <= maxX {
			if leftX >= 0 && !isCursorBlocked(ctx, leftX, y) {
				midX = leftX
				break
			}
			if rightX <= maxX && !isCursorBlocked(ctx, rightX, y) {
				midX = rightX
				break
			}
			leftX--
			rightX++
		}
	}

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

	// Scan upward to find first unblocked position
	for endY > y && isCursorBlocked(ctx, x, endY) {
		endY--
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: y != endY,
	}
}

// MotionScreenTop implements 'gg' motion
func MotionScreenTop(ctx *engine.GameContext, x, y, count int) MotionResult {
	endY := 0

	// Scan downward to find first unblocked position
	maxY := ctx.World.Resources.Config.GameHeight - 1
	for endY < y && isCursorBlocked(ctx, x, endY) {
		endY++
		if endY > maxY {
			endY = y // No valid position found, stay put
			break
		}
	}

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

	// Check if target is blocked, find nearest valid
	if isCursorBlocked(ctx, rightX, botY) {
		newX, newY, found := ctx.World.Positions.FindFreeFromPattern(
			rightX, botY, 1, 1,
			engine.PatternCardinalFirst, 1, 20, true,
			component.WallBlockCursor, nil,
		)
		if found {
			rightX, botY = newX, newY
		} else {
			return MotionResult{StartX: x, StartY: y, EndX: x, EndY: y, Valid: false}
		}
	}

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

	if isCursorBlocked(ctx, midX, midY) {
		// TODO: arbitrary 20 max radius to be put in parameters
		newX, newY, found := ctx.World.Positions.FindFreeFromPattern(
			midX, midY, 1, 1,
			engine.PatternCardinalFirst, 1, 20, true,
			component.WallBlockCursor, nil,
		)
		if found {
			midX, midY = newX, newY
		} else {
			return MotionResult{StartX: x, StartY: y, EndX: x, EndY: y, Valid: false}
		}
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: midX, EndY: midY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: x != midX || y != midY,
	}
}

// MotionOrigin implements 'go' motion (0,0)
func MotionOrigin(ctx *engine.GameContext, x, y, count int) MotionResult {
	originX, originY := 0, 0

	if isCursorBlocked(ctx, originX, originY) {
		newX, newY, found := ctx.World.Positions.FindFreeFromPattern(
			originX, originY, 1, 1,
			engine.PatternCardinalFirst, 1, 20, true,
			component.WallBlockCursor, nil,
		)
		if found {
			originX, originY = newX, newY
		} else {
			return MotionResult{StartX: x, StartY: y, EndX: x, EndY: y, Valid: false}
		}
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: originX, EndY: originY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: originX != x || originY != y,
	}
}

// MotionFindForward implements 'f' motion (CharMotionFunc)
func MotionFindForward(ctx *engine.GameContext, x, y, count int, char rune) MotionResult {
	bounds := ctx.World.GetPingAbsoluteBounds()

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
	bounds := ctx.World.GetPingAbsoluteBounds()

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
	bounds := ctx.World.GetPingAbsoluteBounds()

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
	bounds := ctx.World.GetPingAbsoluteBounds()

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

	// Scan backward to find last unblocked position
	for endX < x && isCursorBlocked(ctx, endX, y) {
		endX++
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

	// Scan backward to find last unblocked position
	for endX > x && isCursorBlocked(ctx, endX, y) {
		endX--
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

	// Scan forward to find last unblocked position
	for endY < y && isCursorBlocked(ctx, x, endY) {
		endY++
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

	// Scan backward to find last unblocked position
	for endY > y && isCursorBlocked(ctx, x, endY) {
		endY--
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: x, EndY: endY,
		Type: RangeLine, Style: StyleInclusive,
		Valid: endY != y,
	}
}

// MotionColumnUp implements [, u - jump to first non-space above in same column
func MotionColumnUp(ctx *engine.GameContext, x, y, count int) MotionResult {
	return motionScanDirectional(ctx, x, y, count, 0, -1)
}

// MotionColumnDown implements ], o - jump to first non-space below in same column
func MotionColumnDown(ctx *engine.GameContext, x, y, count int) MotionResult {
	return motionScanDirectional(ctx, x, y, count, 0, 1)
}

// motionScanDirectional is consolidated directional scanning algorithm
func motionScanDirectional(ctx *engine.GameContext, x, y, count, dx, dy int) MotionResult {
	bounds := ctx.World.GetPingAbsoluteBounds()
	endX, endY := x, y

	glyphStore := ctx.World.Components.Glyph
	filter := func(e core.Entity) bool {
		return glyphStore.HasEntity(e)
	}

	for i := 0; i < count; i++ {
		// Use shared engine logic for consistency
		_, nextX, nextY, found := ctx.World.Positions.FindClosestEntityInDirection(endX, endY, dx, dy, bounds, filter)
		if !found {
			break
		}
		endX, endY = nextX, nextY
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: endX != x || endY != y,
	}
}

// MotionColoredGlyph finds first glyph of specified type (or any if glyphType < 0) in direction
// Uses bounds for visual mode
func MotionColoredGlyph(ctx *engine.GameContext, x, y, count int, motion input.MotionOp, glyphType component.GlyphType) MotionResult {
	bounds := ctx.World.GetPingAbsoluteBounds()

	var dx, dy int
	switch motion {
	case input.MotionColoredGlyphRight:
		dx, dy = 1, 0
	case input.MotionColoredGlyphLeft:
		dx, dy = -1, 0
	case input.MotionColoredGlyphUp:
		dx, dy = 0, -1
	case input.MotionColoredGlyphDown:
		dx, dy = 0, 1
	default:
		return MotionResult{StartX: x, StartY: y, EndX: x, EndY: y, Valid: false}
	}

	glyphStore := ctx.World.Components.Glyph

	filter := func(e core.Entity) bool {
		if !glyphStore.HasEntity(e) {
			return false
		}
		if glyphType < 0 {
			return true // Any glyph
		}
		glyph, ok := glyphStore.GetComponent(e)
		return ok && glyph.Type == glyphType
	}

	endX, endY := x, y
	found := false

	for i := 0; i < count; i++ {
		var nextX, nextY int
		var ok bool
		// Use the centralized engine logic
		_, nextX, nextY, ok = ctx.World.Positions.FindClosestEntityInDirection(endX, endY, dx, dy, bounds, filter)

		if !ok {
			// If not found in sequence, stop at last valid
			break
		}
		endX, endY = nextX, nextY
		found = true
	}

	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: endY,
		Type: RangeChar, Style: StyleInclusive,
		Valid: found,
	}
}