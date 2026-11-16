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

// search moves the cursor to the count-th match of q from cell (x, y), each sought
// from the one before it, the map wrapping round; it reports whether any was found
func search(ctx *engine.GameContext, x, y int, q searchQuery, count int) bool {
	if q.text == "" {
		return false
	}
	grid, pattern := buildCharacterGrid(ctx), []rune(q.text)
	var first vmath.Point
	found := 0
	for found < count {
		at, ok := searchBackward(ctx, grid, pattern, x-1, y, q.whole)
		if q.forward {
			at, ok = searchForward(ctx, grid, pattern, x+1, y, q.whole)
		}
		if !ok {
			break
		}
		x, y, found = at.X, at.Y, found+1
		// Back on the first match, the matches are a ring: its whole turns are skipped
		if found == 1 {
			first = at
		} else if at == first {
			count = found + (count-found)%(found-1)
		}
	}
	if found > 0 {
		OpJump(ctx, x, y)
	}
	return found > 0
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

// runAt is what * and # search for in Visual, and the column it starts at: the run
// of glyphs adjoining the cursor's on its row within the bounds' columns, matched
// anywhere; from an empty cell, wordAt's
func runAt(ctx *engine.GameContext, x, y int, bounds engine.PingAbsoluteBounds) (searchQuery, int) {
	if getCharAt(ctx, x, y) == 0 {
		return wordAt(ctx, x, y)
	}
	start := x
	for start > bounds.MinX && getCharAt(ctx, start-1, y) != 0 {
		start--
	}
	var run []rune
	for i := start; i <= bounds.MaxX && getCharAt(ctx, i, y) != 0; i++ {
		run = append(run, getCharAt(ctx, i, y))
	}
	return searchQuery{text: string(run)}, start
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

// searchForward returns the first match from the given position on, wrapping round
func searchForward(ctx *engine.GameContext, grid map[vmath.Point]rune, pattern []rune, startX, startY int, whole bool) (vmath.Point, bool) {
	// Search from start position to end of screen
	for y := startY; y < ctx.World.Resources.Config.MapHeight; y++ {
		xStart := 0
		if y == startY {
			xStart = startX
		}

		for x := xStart; x <= ctx.World.Resources.Config.MapWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern, whole) {
				return vmath.Point{X: x, Y: y}, true
			}
		}
	}

	// Wrap around to beginning
	for y := range startY {
		for x := 0; x <= ctx.World.Resources.Config.MapWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern, whole) {
				return vmath.Point{X: x, Y: y}, true
			}
		}
	}

	// Search remaining part of start line
	for x := range startX {
		if matchesPattern(grid, x, startY, pattern, whole) {
			return vmath.Point{X: x, Y: startY}, true
		}
	}

	return vmath.Point{}, false
}

// searchBackward returns the first match from the given position back, wrapping round
func searchBackward(ctx *engine.GameContext, grid map[vmath.Point]rune, pattern []rune, startX, startY int, whole bool) (vmath.Point, bool) {
	// Search from start position to beginning of screen
	for y := startY; y >= 0; y-- {
		xEnd := ctx.World.Resources.Config.MapWidth - len(pattern)
		if y == startY {
			xEnd = startX
		}

		for x := xEnd; x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern, whole) {
				return vmath.Point{X: x, Y: y}, true
			}
		}
	}

	// Wrap around to end
	for y := ctx.World.Resources.Config.MapHeight - 1; y > startY; y-- {
		for x := ctx.World.Resources.Config.MapWidth - len(pattern); x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern, whole) {
				return vmath.Point{X: x, Y: y}, true
			}
		}
	}

	// Search remaining part of start line
	for x := ctx.World.Resources.Config.MapWidth - len(pattern); x > startX; x-- {
		if matchesPattern(grid, x, startY, pattern, whole) {
			return vmath.Point{X: x, Y: startY}, true
		}
	}

	return vmath.Point{}, false
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
