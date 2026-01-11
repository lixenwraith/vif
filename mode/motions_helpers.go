package mode

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CharType represents the type of character at a position
type CharType int

const (
	CharTypeSpace       CharType = 0
	CharTypeWord        CharType = 1
	CharTypePunctuation CharType = 2
)

// getCharAt returns the character at the given position, or 0 if empty
func getCharAt(ctx *engine.GameContext, x, y int) rune {
	entities := ctx.World.Positions.GetAllEntityAt(x, y)
	glyphStore := ctx.World.Components.Glyph

	for _, entity := range entities {
		if entity == ctx.World.Resources.Cursor.Entity || entity == 0 {
			continue
		}
		glyph, ok := glyphStore.GetComponent(entity)
		if ok {
			if glyph.Rune == ' ' {
				return 0
			}
			return glyph.Rune
		}
	}
	return 0
}

// isWordChar returns true if the rune is a word character
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// getCharacterTypeAt returns the character type at the given position
func getCharacterTypeAt(ctx *engine.GameContext, x, y int) CharType {
	ch := getCharAt(ctx, x, y)
	if ch == 0 || ch == ' ' {
		return CharTypeSpace
	}
	if isWordChar(ch) {
		return CharTypeWord
	}
	return CharTypePunctuation
}

// validatePosition ensures the cursor position is within bounds
func validatePosition(ctx *engine.GameContext, x, y int) (validX, validY int) {
	validX, validY = x, y
	if validX < 0 {
		validX = 0
	} else if validX >= ctx.World.Resources.Config.GameWidth {
		validX = ctx.World.Resources.Config.GameWidth - 1
	}
	if validY < 0 {
		validY = 0
	} else if validY >= ctx.World.Resources.Config.GameHeight {
		validY = ctx.World.Resources.Config.GameHeight - 1
	}
	return validX, validY
}

// findCharInDirection finds the Nth occurrence of target in direction (forward=true)
// Returns (x position, found). If count exceeds matches, returns last match found
func findCharInDirection(ctx *engine.GameContext, startX, startY int, target rune, count int, forward bool) (int, bool) {
	occurrences := 0
	lastMatch := -1
	glyphStore := ctx.World.Components.Glyph

	if forward {
		for x := startX + 1; x < ctx.World.Resources.Config.GameWidth; x++ {
			entities := ctx.World.Positions.GetAllEntityAt(x, startY)
			for _, entity := range entities {
				if entity == 0 {
					continue
				}
				glyph, ok := glyphStore.GetComponent(entity)
				if ok && glyph.Rune == target {
					occurrences++
					lastMatch = x
					if occurrences == count {
						return x, true
					}
				}
			}
		}
	} else {
		for x := startX - 1; x >= 0; x-- {
			entities := ctx.World.Positions.GetAllEntityAt(x, startY)
			for _, entity := range entities {
				if entity == 0 {
					continue
				}
				glyph, ok := glyphStore.GetComponent(entity)
				if ok && glyph.Rune == target {
					occurrences++
					lastMatch = x
					if occurrences == count {
						return x, true
					}
				}
			}
		}
	}

	if lastMatch != -1 {
		return lastMatch, true
	}
	return -1, false
}

// --- Word Motion Helpers ---

func findNextWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	if currentType != CharTypeSpace {
		for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == currentType {
			x++
		}
	} else {
		x++
	}

	for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}
	if x == cursorX {
		x++
		if x >= ctx.World.Resources.Config.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findWordEndVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX + 1
	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}

	for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}
	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}

	currentType := getCharacterTypeAt(ctx, x, cursorY)
	for x < ctx.World.Resources.Config.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)
		if nextType == CharTypeSpace || nextType != currentType {
			break
		}
		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findPrevWordStartVim(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX - 1
	if x < 0 {
		return cursorX
	}

	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}
	if x < 0 {
		return cursorX
	}

	currentType := getCharacterTypeAt(ctx, x, cursorY)
	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)
		if prevType == CharTypeSpace || prevType != currentType {
			break
		}
		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findNextWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX
	currentType := getCharacterTypeAt(ctx, x, cursorY)

	if currentType != CharTypeSpace {
		for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			x++
		}
	} else {
		x++
	}

	for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}

	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}
	if x == cursorX {
		x++
		if x >= ctx.World.Resources.Config.GameWidth {
			return cursorX
		}
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findWORDEnd(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX + 1
	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}

	for x < ctx.World.Resources.Config.GameWidth && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x++
	}
	if x >= ctx.World.Resources.Config.GameWidth {
		return cursorX
	}

	for x < ctx.World.Resources.Config.GameWidth {
		nextType := getCharacterTypeAt(ctx, x+1, cursorY)
		if nextType == CharTypeSpace {
			break
		}
		x++
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

func findPrevWORDStart(ctx *engine.GameContext, cursorX, cursorY int) int {
	x := cursorX - 1
	if x < 0 {
		return cursorX
	}

	for x >= 0 && getCharacterTypeAt(ctx, x, cursorY) == CharTypeSpace {
		x--
	}
	if x < 0 {
		return cursorX
	}

	for x > 0 {
		prevType := getCharacterTypeAt(ctx, x-1, cursorY)
		if prevType == CharTypeSpace {
			break
		}
		x--
	}

	validX, _ := validatePosition(ctx, x, cursorY)
	return validX
}

// --- Line Helpers ---

// findLineEnd returns the X coordinate of the last interactable entity on the line
// Returns -1 if no interactable entities are found on the line
// Optimized for high entity counts using spatial grid traversal (O(Width) instead of O(N))
func findLineEnd(ctx *engine.GameContext, cursorY int) int {
	// Stack-allocated buffer for zero-alloc queries
	var buf [constant.MaxEntitiesPerCell]core.Entity

	glyphStore := ctx.World.Components.Glyph

	// Scan from right to left
	for x := ctx.World.Resources.Config.GameWidth - 1; x >= 0; x-- {
		// Zero-alloc query
		count := ctx.World.Positions.GetAllEntityAtInto(x, cursorY, buf[:])

		// Check entities at this cell
		for i := 0; i < count; i++ {
			if buf[i] != 0 && glyphStore.HasEntity(buf[i]) {
				// Found right-most interactable entity
				return x
			}
		}
	}

	// Line is empty (or no interactable entities)
	return -1
}

func findFirstNonWhitespace(ctx *engine.GameContext, cursorY int) int {
	for x := 0; x < ctx.World.Resources.Config.GameWidth; x++ {
		if getCharacterTypeAt(ctx, x, cursorY) != CharTypeSpace {
			validX, _ := validatePosition(ctx, x, cursorY)
			return validX
		}
	}
	return 0
}

// --- Paragraph Helpers ---

// findPrevEmptyLine finds the previous line with no interactable entities
// Optimized for high entity counts using spatial grid traversal
func findPrevEmptyLine(ctx *engine.GameContext, cursorY int) int {
	var buf [constant.MaxEntitiesPerCell]core.Entity

	glyphStore := ctx.World.Components.Glyph

	for y := cursorY - 1; y >= 0; y-- {
		rowEmpty := true
		// Scan row; stop early if any interactable entity is found
		for x := 0; x < ctx.World.Resources.Config.GameWidth && rowEmpty; x++ {
			count := ctx.World.Positions.GetAllEntityAtInto(x, y, buf[:])
			for i := 0; i < count; i++ {
				if buf[i] != 0 && glyphStore.HasEntity(buf[i]) {
					rowEmpty = false
					break
				}
			}
		}
		if rowEmpty {
			return y
		}
	}
	return 0
}

// findNextEmptyLine finds the next line with no interactable entities
// Optimized for high entity counts using spatial grid traversal
func findNextEmptyLine(ctx *engine.GameContext, cursorY int) int {
	var buf [constant.MaxEntitiesPerCell]core.Entity

	glyphStore := ctx.World.Components.Glyph

	for y := cursorY + 1; y < ctx.World.Resources.Config.GameHeight; y++ {
		rowEmpty := true
		// Scan row; stop early if any interactable entity is found
		for x := 0; x < ctx.World.Resources.Config.GameWidth && rowEmpty; x++ {
			count := ctx.World.Positions.GetAllEntityAtInto(x, y, buf[:])
			for i := 0; i < count; i++ {
				if buf[i] != 0 && glyphStore.HasEntity(buf[i]) {
					rowEmpty = false
					break
				}
			}
		}
		if rowEmpty {
			return y
		}
	}
	return ctx.World.Resources.Config.GameHeight - 1
}

// --- Bracket Helpers ---

func isBracket(r rune) bool {
	return r == '(' || r == ')' || r == '{' || r == '}' || r == '[' || r == ']' || r == '<' || r == '>'
}

func isOpeningBracket(r rune) bool {
	return r == '(' || r == '{' || r == '[' || r == '<'
}

func getMatchingBracket(r rune) rune {
	switch r {
	case '(':
		return ')'
	case ')':
		return '('
	case '{':
		return '}'
	case '}':
		return '{'
	case '[':
		return ']'
	case ']':
		return '['
	case '<':
		return '>'
	case '>':
		return '<'
	}
	return 0
}

func findMatchingBracket(ctx *engine.GameContext, cursorX, cursorY int) (int, int) {
	currentChar := getCharAt(ctx, cursorX, cursorY)
	if !isBracket(currentChar) {
		return -1, -1
	}

	matchingChar := getMatchingBracket(currentChar)
	if isOpeningBracket(currentChar) {
		return findMatchingBracketForward(ctx, cursorX, cursorY, currentChar, matchingChar)
	}
	return findMatchingBracketBackward(ctx, cursorX, cursorY, currentChar, matchingChar)
}

func findMatchingBracketForward(ctx *engine.GameContext, startX, startY int, openChar, closeChar rune) (int, int) {
	depth := 0
	x, y := startX+1, startY

	if x >= ctx.World.Resources.Config.GameWidth {
		x = 0
		y++
	}

	for y < ctx.World.Resources.Config.GameHeight {
		for x < ctx.World.Resources.Config.GameWidth {
			ch := getCharAt(ctx, x, y)
			if ch == openChar {
				depth++
			} else if ch == closeChar {
				if depth == 0 {
					return x, y
				}
				depth--
			}
			x++
		}
		x = 0
		y++
	}
	return -1, -1
}

func findMatchingBracketBackward(ctx *engine.GameContext, startX, startY int, closeChar, openChar rune) (int, int) {
	depth := 0
	x, y := startX-1, startY

	if x < 0 {
		y--
		if y >= 0 {
			x = ctx.World.Resources.Config.GameWidth - 1
		}
	}

	for y >= 0 {
		for x >= 0 {
			ch := getCharAt(ctx, x, y)
			if ch == closeChar {
				depth++
			} else if ch == openChar {
				if depth == 0 {
					return x, y
				}
				depth--
			}
			x--
		}
		y--
		if y >= 0 {
			x = ctx.World.Resources.Config.GameWidth - 1
		}
	}
	return -1, -1
}

// === Band-Aware Helpers (Visual Mode) ===

// findNextWordStartInBand finds next word start within band (column-first: left-to-right, top-to-bottom)
func findNextWordStartInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandForward(ctx, cursorX, cursorY, bounds, isWordStartAt)
}

// findPrevWordStartInBand finds previous word start within band (column-first backward)
func findPrevWordStartInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandBackward(ctx, cursorX, cursorY, bounds, isWordStartAt)
}

// findWordEndInBand finds word end within band (column-first: left-to-right, top-to-bottom)
func findWordEndInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandForward(ctx, cursorX, cursorY, bounds, isWordEndAt)
}

// findNextWORDStartInBand finds next WORD start within band (column-first)
func findNextWORDStartInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandForward(ctx, cursorX, cursorY, bounds, isWORDStartAt)
}

// findPrevWORDStartInBand finds previous WORD start within band (column-first backward)
func findPrevWORDStartInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandBackward(ctx, cursorX, cursorY, bounds, isWORDStartAt)
}

// findWORDEndInBand finds WORD end within band (column-first)
func findWORDEndInBand(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds) (int, int) {
	return scanBandForward(ctx, cursorX, cursorY, bounds, isWORDEndAt)
}

// findCharInBand finds target char within band, column-first order (left-to-right, top-to-bottom)
// Returns (x, y, found). Skips starting position.
func findCharInBand(ctx *engine.GameContext, startX, startY int, target rune, count int, forward bool, bounds engine.PingBounds) (int, int, bool) {
	glyphStore := ctx.World.Components.Glyph
	occurrences := 0
	lastMatchX, lastMatchY := -1, -1

	hasTargetAt := func(x, y int) bool {
		entities := ctx.World.Positions.GetAllEntityAt(x, y)
		for _, entity := range entities {
			if entity == 0 {
				continue
			}
			glyph, ok := glyphStore.GetComponent(entity)
			if ok && glyph.Rune == target {
				return true
			}
		}
		return false
	}

	if forward {
		for x := startX; x < ctx.World.Resources.Config.GameWidth; x++ {
			for y := bounds.MinY; y <= bounds.MaxY; y++ {
				if x == startX && y <= startY {
					continue
				}
				if hasTargetAt(x, y) {
					occurrences++
					lastMatchX, lastMatchY = x, y
					if occurrences == count {
						return x, y, true
					}
				}
			}
		}
	} else {
		for x := startX; x >= 0; x-- {
			for y := bounds.MaxY; y >= bounds.MinY; y-- {
				if x == startX && y >= startY {
					continue
				}
				if hasTargetAt(x, y) {
					occurrences++
					lastMatchX, lastMatchY = x, y
					if occurrences == count {
						return x, y, true
					}
				}
			}
		}
	}

	if lastMatchX != -1 {
		return lastMatchX, lastMatchY, true
	}
	return -1, -1, false
}

// findLineEndInBand returns rightmost entity X across all band rows
// Returns -1 if no entities found
func findLineEndInBand(ctx *engine.GameContext, bounds engine.PingBounds) int {
	var buf [constant.MaxEntitiesPerCell]core.Entity
	glyphStore := ctx.World.Components.Glyph
	rightmost := -1

	for y := bounds.MinY; y <= bounds.MaxY; y++ {
		for x := ctx.World.Resources.Config.GameWidth - 1; x >= 0; x-- {
			count := ctx.World.Positions.GetAllEntityAtInto(x, y, buf[:])
			for i := 0; i < count; i++ {
				if buf[i] != 0 && glyphStore.HasEntity(buf[i]) {
					if x > rightmost {
						rightmost = x
					}
					break
				}
			}
		}
	}

	return rightmost
}

// findFirstNonWhitespaceInBand returns leftmost non-whitespace position in band
func findFirstNonWhitespaceInBand(ctx *engine.GameContext, bounds engine.PingBounds) (int, int) {
	for y := bounds.MinY; y <= bounds.MaxY; y++ {
		for x := 0; x < ctx.World.Resources.Config.GameWidth; x++ {
			if getCharacterTypeAt(ctx, x, y) != CharTypeSpace {
				return x, y
			}
		}
	}
	return 0, bounds.MinY
}

// findColumnUpInBand finds first glyph above cursor within band's X range
// Searches from cursor row-1 to screen top, returns (x, y) of found character
// Y search is NOT constrained by band - band only defines X search width
func findColumnUpInBand(ctx *engine.GameContext, cursorX, startY int, bounds engine.PingBounds) (int, int) {
	glyphStore := ctx.World.Components.Glyph

	for y := startY - 1; y >= 0; y-- {
		for x := bounds.MinX; x <= bounds.MaxX; x++ {
			entities := ctx.World.Positions.GetAllEntityAt(x, y)
			for _, entity := range entities {
				if entity != 0 && glyphStore.HasEntity(entity) {
					return x, y
				}
			}
		}
	}
	return cursorX, startY
}

// findColumnDownInBand finds first glyph below cursor within band's X range
// Searches from cursor row+1 to screen bottom, returns (x, y) of found character
// Y search is NOT constrained by band - band only defines X search width
func findColumnDownInBand(ctx *engine.GameContext, cursorX, startY int, bounds engine.PingBounds, gameHeight int) (int, int) {
	glyphStore := ctx.World.Components.Glyph

	for y := startY + 1; y < gameHeight; y++ {
		for x := bounds.MinX; x <= bounds.MaxX; x++ {
			entities := ctx.World.Positions.GetAllEntityAt(x, y)
			for _, entity := range entities {
				if entity != 0 && glyphStore.HasEntity(entity) {
					return x, y
				}
			}
		}
	}
	return cursorX, startY
}

// === Band Scanning Primitives ===

// isWordStartAt returns true if position (x,y) is the start of a word
// Word start: non-space character where left neighbor (same row) is space or different type
func isWordStartAt(ctx *engine.GameContext, x, y int) bool {
	current := getCharacterTypeAt(ctx, x, y)
	if current == CharTypeSpace {
		return false
	}
	if x == 0 {
		return true
	}
	left := getCharacterTypeAt(ctx, x-1, y)
	return left == CharTypeSpace || left != current
}

// isWordEndAt returns true if position (x,y) is the end of a word
// Word end: non-space character where right neighbor (same row) is space or different type
func isWordEndAt(ctx *engine.GameContext, x, y int) bool {
	current := getCharacterTypeAt(ctx, x, y)
	if current == CharTypeSpace {
		return false
	}
	if x >= ctx.World.Resources.Config.GameWidth-1 {
		return true
	}
	right := getCharacterTypeAt(ctx, x+1, y)
	return right == CharTypeSpace || right != current
}

// isWORDStartAt returns true if position (x,y) is the start of a WORD
// WORD start: non-space character where left neighbor (same row) is space
func isWORDStartAt(ctx *engine.GameContext, x, y int) bool {
	current := getCharacterTypeAt(ctx, x, y)
	if current == CharTypeSpace {
		return false
	}
	if x == 0 {
		return true
	}
	return getCharacterTypeAt(ctx, x-1, y) == CharTypeSpace
}

// isWORDEndAt returns true if position (x,y) is the end of a WORD
// WORD end: non-space character where right neighbor (same row) is space
func isWORDEndAt(ctx *engine.GameContext, x, y int) bool {
	current := getCharacterTypeAt(ctx, x, y)
	if current == CharTypeSpace {
		return false
	}
	if x >= ctx.World.Resources.Config.GameWidth-1 {
		return true
	}
	return getCharacterTypeAt(ctx, x+1, y) == CharTypeSpace
}

// scanBandForward scans column-first (left-to-right, top-to-bottom) for predicate match
// Skips cursor position, returns first match or original position if none found
func scanBandForward(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds, predicate func(*engine.GameContext, int, int) bool) (int, int) {
	for x := cursorX; x < ctx.World.Resources.Config.GameWidth; x++ {
		for y := bounds.MinY; y <= bounds.MaxY; y++ {
			// Skip cursor position and anything before it in scan order
			if x == cursorX && y <= cursorY {
				continue
			}
			if predicate(ctx, x, y) {
				return x, y
			}
		}
	}
	return cursorX, cursorY
}

// scanBandBackward scans column-first backward (right-to-left, bottom-to-top) for predicate match
// Skips cursor position, returns first match or original position if none found
func scanBandBackward(ctx *engine.GameContext, cursorX, cursorY int, bounds engine.PingBounds, predicate func(*engine.GameContext, int, int) bool) (int, int) {
	for x := cursorX; x >= 0; x-- {
		for y := bounds.MaxY; y >= bounds.MinY; y-- {
			// Skip cursor position and anything after it in scan order
			if x == cursorX && y >= cursorY {
				continue
			}
			if predicate(ctx, x, y) {
				return x, y
			}
		}
	}
	return cursorX, cursorY
}