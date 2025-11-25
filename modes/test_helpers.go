package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// getCursorX returns the cursor X position from ECS
func getCursorX(ctx *engine.GameContext) int {
	if pos, ok := ctx.World.Positions.Get(ctx.CursorEntity); ok {
		return pos.X
	}
	return 0
}

// getCursorY returns the cursor Y position from ECS
func getCursorY(ctx *engine.GameContext) int {
	if pos, ok := ctx.World.Positions.Get(ctx.CursorEntity); ok {
		return pos.Y
	}
	return 0
}

// setCursorPosition sets the cursor position in ECS
func setCursorPosition(ctx *engine.GameContext, x, y int) {
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: x, Y: y})
}
