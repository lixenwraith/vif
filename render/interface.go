package render

import "github.com/lixenwraith/vi-fighter/engine"

// SystemRenderer is implemented by systems with visual output
type SystemRenderer interface {
	Render(ctx RenderContext, world *engine.World, buf *RenderBuffer)
}

// VisibilityToggle is optionally implemented for runtime enable/disable
type VisibilityToggle interface {
	IsVisible() bool
}