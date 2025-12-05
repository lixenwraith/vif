package engine

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TriggerSplashChar creates a new transient splash entity with a single character
func TriggerSplashChar(ctx *GameContext, char rune, color terminal.RGB) {
	cursorPos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		return
	}

	anchorX, anchorY := CalculateSplashAnchor(ctx, cursorPos.X, cursorPos.Y, 1)

	splash := components.SplashComponent{
		Length:    1,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		StartNano: ctx.PausableClock.Now().UnixNano(),
		Duration:  constants.SplashDuration.Nanoseconds(),
		Mode:      components.SplashModeTransient,
	}
	splash.Content[0] = char

	entity := ctx.World.CreateEntity()
	ctx.World.Splashes.Add(entity, splash)
}

// TriggerSplashString creates a new transient splash entity with a string
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

	anchorX, anchorY := CalculateSplashAnchor(ctx, cursorPos.X, cursorPos.Y, length)

	splash := components.SplashComponent{
		Length:    length,
		Color:     color,
		AnchorX:   anchorX,
		AnchorY:   anchorY,
		StartNano: ctx.PausableClock.Now().UnixNano(),
		Duration:  constants.SplashDuration.Nanoseconds(),
		Mode:      components.SplashModeTransient,
	}

	for i := 0; i < length; i++ {
		splash.Content[i] = runes[i]
	}

	entity := ctx.World.CreateEntity()
	ctx.World.Splashes.Add(entity, splash)
}

// CalculateSplashAnchor computes top-left anchor for splash positioning
// Avoids cursor and existing splashes using quadrant checking
func CalculateSplashAnchor(ctx *GameContext, cursorX, cursorY, charCount int) (int, int) {
	// Divide screen into 4 quadrants relative to game center
	// Q0: Top-Left, Q1: Top-Right, Q2: Bottom-Left, Q3: Bottom-Right
	centerX := ctx.GameWidth / 2
	centerY := ctx.GameHeight / 2

	// Determine cursor quadrant
	cursorQuadrant := 0
	if cursorX >= centerX {
		cursorQuadrant |= 1 // Right
	}
	if cursorY >= centerY {
		cursorQuadrant |= 2 // Bottom
	}

	// Identify quadrants occupied by existing splashes
	occupiedMask := 0
	splashes := ctx.World.Splashes.All()
	for _, e := range splashes {
		s, ok := ctx.World.Splashes.Get(e)
		if !ok {
			continue
		}

		// Map splash position to quadrant
		// Use splash center for determination
		splashCenterX := s.AnchorX + (s.Length*(constants.SplashCharWidth+constants.SplashCharSpacing))/2
		splashCenterY := s.AnchorY + constants.SplashCharHeight/2

		quad := 0
		if splashCenterX >= centerX {
			quad |= 1
		}
		if splashCenterY >= centerY {
			quad |= 2
		}
		occupiedMask |= (1 << quad)
	}

	// Mark cursor quadrant as occupied
	occupiedMask |= (1 << cursorQuadrant)

	// Find best available quadrant
	// Priority: Opposite to cursor -> Any free -> Opposite to cursor (overlap fallback)
	targetQuadrant := -1
	oppositeQuadrant := cursorQuadrant ^ 0b11

	if occupiedMask&(1<<oppositeQuadrant) == 0 {
		targetQuadrant = oppositeQuadrant
	} else {
		// Check other quadrants (0 to 3)
		for q := 0; q < 4; q++ {
			if occupiedMask&(1<<q) == 0 {
				targetQuadrant = q
				break
			}
		}
	}

	// If all occupied, fallback to opposite
	if targetQuadrant == -1 {
		targetQuadrant = oppositeQuadrant
	}

	// Calculate center coords for target quadrant
	var quadCenterX, quadCenterY int
	if targetQuadrant&1 != 0 {
		quadCenterX = ctx.GameWidth * 3 / 4 // Right
	} else {
		quadCenterX = ctx.GameWidth / 4 // Left
	}
	if targetQuadrant&2 != 0 {
		quadCenterY = ctx.GameHeight * 3 / 4 // Bottom
	} else {
		quadCenterY = ctx.GameHeight / 4 // Top
	}

	// Calculate anchor
	totalWidth := charCount*constants.SplashCharWidth + (charCount-1)*constants.SplashCharSpacing
	totalHeight := constants.SplashCharHeight

	anchorX := quadCenterX - totalWidth/2
	anchorY := quadCenterY - totalHeight/2

	// Clamp bounds
	if anchorX < 0 {
		anchorX = 0
	} else if anchorX+totalWidth > ctx.GameWidth {
		anchorX = ctx.GameWidth - totalWidth
	}

	if anchorY < 0 {
		anchorY = 0
	} else if anchorY+totalHeight > ctx.GameHeight {
		anchorY = ctx.GameHeight - totalHeight
	}

	return anchorX, anchorY
}