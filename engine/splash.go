// FILE: engine/splash.go
package engine

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TriggerSplashChar activates splash with a single character
func TriggerSplashChar(ctx *GameContext, char rune, color terminal.RGB) {
	cursorPos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	anchorX, anchorY := calculateSplashAnchor(ctx, cursorPos.X, cursorPos.Y, 1)

	splash := components.SplashComponent{
		Length:    1,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		StartNano: ctx.PausableClock.Now().UnixNano(),
	}
	splash.Content[0] = char

	ctx.World.Splashes.Add(ctx.SplashEntity, splash)
}

// TriggerSplashString activates splash with a string (up to 8 chars)
func TriggerSplashString(ctx *GameContext, text string, color terminal.RGB) {
	if len(text) == 0 {
		return
	}

	cursorPos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	runes := []rune(text)
	length := len(runes)
	if length > constants.SplashMaxLength {
		length = constants.SplashMaxLength
	}

	anchorX, anchorY := calculateSplashAnchor(ctx, cursorPos.X, cursorPos.Y, length)

	splash := components.SplashComponent{
		Length:    length,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		StartNano: ctx.PausableClock.Now().UnixNano(),
	}

	for i := 0; i < length; i++ {
		splash.Content[i] = runes[i]
	}

	ctx.World.Splashes.Add(ctx.SplashEntity, splash)
}

// calculateSplashAnchor computes top-left anchor for splash positioning
func calculateSplashAnchor(ctx *GameContext, cursorX, cursorY, charCount int) (int, int) {
	// Determine cursor quadrant and select opposite
	quadrant := 0
	if cursorX >= ctx.GameWidth/2 {
		quadrant |= 1
	}
	if cursorY >= ctx.GameHeight/2 {
		quadrant |= 2
	}
	opposite := quadrant ^ 0b11

	// Calculate quadrant center
	var centerX, centerY int
	if opposite&1 != 0 {
		centerX = ctx.GameWidth * 3 / 4
	} else {
		centerX = ctx.GameWidth / 4
	}
	if opposite&2 != 0 {
		centerY = ctx.GameHeight * 3 / 4
	} else {
		centerY = ctx.GameHeight / 4
	}

	// Calculate total splash dimensions
	totalWidth := charCount*constants.SplashCharWidth + (charCount-1)*constants.SplashCharSpacing
	totalHeight := constants.SplashCharHeight

	// Center splash in quadrant
	anchorX := centerX - totalWidth/2
	anchorY := centerY - totalHeight/2

	// Clamp to left/top boundaries only
	if anchorX < 0 {
		anchorX = 0
	}
	if anchorY < 0 {
		anchorY = 0
	}

	return anchorX, anchorY
}
