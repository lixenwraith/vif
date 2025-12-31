package render
// @lixen: #dev{feature[drain(render,system)]}

// SystemRenderer is implemented by systems with visual output
type SystemRenderer interface {
	Render(ctx RenderContext, buf *RenderBuffer)
}

// VisibilityToggle is optionally implemented for runtime enable/disable
type VisibilityToggle interface {
	IsVisible() bool
}