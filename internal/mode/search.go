package mode

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// searchQuery is a search n and N repeat: / finds its text anywhere, forward; * and
// # find the run of glyphs at the cursor, a word whole, the way each went
type searchQuery struct {
	text           string
	forward, whole bool
}

// PerformSearch moves the cursor to the next match of a text pattern from it, the
// map wrapping round; whole matches a word only where no word glyph adjoins it
func PerformSearch(ctx *engine.GameContext, searchText string, forward, whole bool) bool {
	pos, ok := ctx.World.LocalCursor()
	return ok && searchFrom(ctx, pos.X, pos.Y, searchText, forward, whole)
}

// searchFrom is PerformSearch from cell x, y rather than the cursor's
func searchFrom(ctx *engine.GameContext, x, y int, searchText string, forward, whole bool) bool {
	if searchText == "" {
		return false
	}
	grid := buildCharacterGrid(ctx)
	if forward {
		return searchForward(ctx, grid, []rune(searchText), x+1, y, whole)
	}
	return searchBackward(ctx, grid, []rune(searchText), x-1, y, whole)
}

// wordAt is what * and # search for, and the column its run starts at: the word
// under the cursor or else the first after it on its row, as in vim, then the
// punctuation run there; -1 on a row with neither
func wordAt(ctx *engine.GameContext, x, y int) (searchQuery, int) {
	maxX := ctx.World.Resources.Config.MapWidth - 1
	for _, kind := range [...]CharType{CharTypeWord, CharTypePunctuation} {
		start := x
		for start <= maxX && getCharacterTypeAt(ctx, start, y) != kind {
			start++
		}
		if start > maxX {
			continue
		}
		for start > 0 && getCharacterTypeAt(ctx, start-1, y) == kind {
			start--
		}
		var run []rune
		for i := start; i <= maxX && getCharacterTypeAt(ctx, i, y) == kind; i++ {
			run = append(run, getCharAt(ctx, i, y))
		}
		return searchQuery{text: string(run), whole: kind == CharTypeWord}, start
	}
	return searchQuery{}, -1
}

// buildCharacterGrid maps each cell to its top glyph's character, as it is drawn
func buildCharacterGrid(ctx *engine.GameContext) map[vmath.Point]rune {
	grid := make(map[vmath.Point]rune)
	ctx.World.Components.Glyph.Each(func(entity core.Entity, glyph *component.GlyphComponent) bool {
		pos, ok := ctx.World.Positions.GetPosition(entity)
		if top, _ := ctx.World.Positions.TopGlyphAt(pos.X, pos.Y); ok && top == entity {
			grid[vmath.Point{X: pos.X, Y: pos.Y}] = glyph.Rune
		}
		return true
	})
	return grid
}

// searchForward searches forward from the given position
func searchForward(ctx *engine.GameContext, grid map[vmath.Point]rune, pattern []rune, startX, startY int, whole bool) bool {
	// Search from start position to end of screen
	for y := startY; y < ctx.World.Resources.Config.MapHeight; y++ {
		xStart := 0
		if y == startY {
			xStart = startX
		}

		for x := xStart; x <= ctx.World.Resources.Config.MapWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern, whole) {
				OpJump(ctx, x, y)
				return true
			}
		}
	}

	// Wrap around to beginning
	for y := range startY {
		for x := 0; x <= ctx.World.Resources.Config.MapWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern, whole) {
				OpJump(ctx, x, y)
				return true
			}
		}
	}

	// Search remaining part of start line
	for x := range startX {
		if matchesPattern(grid, x, startY, pattern, whole) {
			OpJump(ctx, x, startY)
			return true
		}
	}

	return false
}

// searchBackward searches backward from the given position
func searchBackward(ctx *engine.GameContext, grid map[vmath.Point]rune, pattern []rune, startX, startY int, whole bool) bool {
	// Search from start position to beginning of screen
	for y := startY; y >= 0; y-- {
		xEnd := ctx.World.Resources.Config.MapWidth - len(pattern)
		if y == startY {
			xEnd = startX
		}

		for x := xEnd; x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern, whole) {
				OpJump(ctx, x, y)
				return true
			}
		}
	}

	// Wrap around to end
	for y := ctx.World.Resources.Config.MapHeight - 1; y > startY; y-- {
		for x := ctx.World.Resources.Config.MapWidth - len(pattern); x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern, whole) {
				OpJump(ctx, x, y)
				return true
			}
		}
	}

	// Search remaining part of start line
	for x := ctx.World.Resources.Config.MapWidth - len(pattern); x > startX; x-- {
		if matchesPattern(grid, x, startY, pattern, whole) {
			OpJump(ctx, x, startY)
			return true
		}
	}

	return false
}

// matchesPattern checks if the pattern matches at the given position, and when whole,
// that no word glyph adjoins it on either side
func matchesPattern(grid map[vmath.Point]rune, x, y int, pattern []rune, whole bool) bool {
	for i, r := range pattern {
		gridRune, exists := grid[vmath.Point{X: x + i, Y: y}]
		if !exists || gridRune != r {
			return false
		}
	}
	return !whole || !isWordChar(grid[vmath.Point{X: x - 1, Y: y}]) && !isWordChar(grid[vmath.Point{X: x + len(pattern), Y: y}])
}
