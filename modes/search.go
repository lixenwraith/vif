package modes

import (
	"reflect"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Point represents a 2D coordinate
type Point struct {
	X, Y int
}

// PerformSearch searches for a text pattern and moves cursor to first match
func PerformSearch(ctx *engine.GameContext, searchText string, forward bool) bool {
	if searchText == "" {
		return false
	}

	searchRunes := []rune(searchText)

	// Build character grid from ECS
	grid := buildCharacterGrid(ctx)

	// Determine search start position
	startX, startY := ctx.CursorX, ctx.CursorY
	if forward {
		startX++ // Start after cursor
	} else {
		startX-- // Start before cursor
	}

	// Search from cursor position
	if forward {
		if searchForward(ctx, grid, searchRunes, startX, startY) {
			return true
		}
	} else {
		if searchBackward(ctx, grid, searchRunes, startX, startY) {
			return true
		}
	}

	return false
}

// RepeatSearch repeats the last search in the specified direction
func RepeatSearch(ctx *engine.GameContext, forward bool) bool {
	if ctx.LastSearchText == "" {
		return false
	}
	return PerformSearch(ctx, ctx.LastSearchText, forward)
}

// buildCharacterGrid builds a 2D map of characters from the ECS
func buildCharacterGrid(ctx *engine.GameContext) map[Point]rune {
	grid := make(map[Point]rune)

	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := ctx.World.GetEntitiesWith(posType, charType)
	for _, entity := range entities {
		posComp, _ := ctx.World.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		charComp, _ := ctx.World.GetComponent(entity, charType)
		char := charComp.(components.CharacterComponent)

		grid[Point{pos.X, pos.Y}] = char.Rune
	}

	return grid
}

// searchForward searches forward from the given position
func searchForward(ctx *engine.GameContext, grid map[Point]rune, pattern []rune, startX, startY int) bool {
	// Search from start position to end of screen
	for y := startY; y < ctx.GameHeight; y++ {
		xStart := 0
		if y == startY {
			xStart = startX
		}

		for x := xStart; x <= ctx.GameWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern) {
				ctx.CursorX = x
				ctx.CursorY = y
				// Sync cursor position to GameState for Drain and other systems
				ctx.State.SetCursorX(x)
				ctx.State.SetCursorY(y)
				return true
			}
		}
	}

	// Wrap around to beginning
	for y := 0; y < startY; y++ {
		for x := 0; x <= ctx.GameWidth-len(pattern); x++ {
			if matchesPattern(grid, x, y, pattern) {
				ctx.CursorX = x
				ctx.CursorY = y
				// Sync cursor position to GameState for Drain and other systems
				ctx.State.SetCursorX(x)
				ctx.State.SetCursorY(y)
				return true
			}
		}
	}

	// Search remaining part of start line
	for x := 0; x < startX; x++ {
		if matchesPattern(grid, x, startY, pattern) {
			ctx.CursorX = x
			ctx.CursorY = startY
			// Sync cursor position to GameState for Drain and other systems
			ctx.State.SetCursorX(x)
			ctx.State.SetCursorY(startY)
			return true
		}
	}

	return false
}

// searchBackward searches backward from the given position
func searchBackward(ctx *engine.GameContext, grid map[Point]rune, pattern []rune, startX, startY int) bool {
	// Search from start position to beginning of screen
	for y := startY; y >= 0; y-- {
		xEnd := ctx.GameWidth - len(pattern)
		if y == startY {
			xEnd = startX
		}

		for x := xEnd; x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern) {
				ctx.CursorX = x
				ctx.CursorY = y
				// Sync cursor position to GameState for Drain and other systems
				ctx.State.SetCursorX(x)
				ctx.State.SetCursorY(y)
				return true
			}
		}
	}

	// Wrap around to end
	for y := ctx.GameHeight - 1; y > startY; y-- {
		for x := ctx.GameWidth - len(pattern); x >= 0; x-- {
			if matchesPattern(grid, x, y, pattern) {
				ctx.CursorX = x
				ctx.CursorY = y
				// Sync cursor position to GameState for Drain and other systems
				ctx.State.SetCursorX(x)
				ctx.State.SetCursorY(y)
				return true
			}
		}
	}

	// Search remaining part of start line
	for x := ctx.GameWidth - len(pattern); x > startX; x-- {
		if matchesPattern(grid, x, startY, pattern) {
			ctx.CursorX = x
			ctx.CursorY = startY
			// Sync cursor position to GameState for Drain and other systems
			ctx.State.SetCursorX(x)
			ctx.State.SetCursorY(startY)
			return true
		}
	}

	return false
}

// matchesPattern checks if the pattern matches at the given position
func matchesPattern(grid map[Point]rune, x, y int, pattern []rune) bool {
	for i, r := range pattern {
		gridRune, exists := grid[Point{x + i, y}]
		if !exists || gridRune != r {
			return false
		}
	}
	return true
}
