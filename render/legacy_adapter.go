package render

import "github.com/lixenwraith/vi-fighter/engine"

// LegacyRenderer interface for the existing TerminalRenderer.
// Phase 2 will add RenderFrameToScreen method to TerminalRenderer.
type LegacyRenderer interface {
	RenderFrameToScreen(ctx *engine.GameContext, screen *BufferScreen)
}

// LegacyAdapter wraps a LegacyRenderer as a SystemRenderer.
type LegacyAdapter struct {
	renderer LegacyRenderer
	gameCtx  *engine.GameContext
	screen   *BufferScreen
}

// NewLegacyAdapter creates an adapter bridging legacy renderer to new pipeline.
func NewLegacyAdapter(renderer LegacyRenderer, gameCtx *engine.GameContext) *LegacyAdapter {
	return &LegacyAdapter{
		renderer: renderer,
		gameCtx:  gameCtx,
	}
}

// Render implements SystemRenderer. Creates/updates BufferScreen and delegates to legacy.
func (l *LegacyAdapter) Render(ctx RenderContext, world *engine.World, buf *RenderBuffer) {
	if l.screen == nil || l.screen.buf != buf {
		l.screen = NewBufferScreen(buf)
	}
	l.renderer.RenderFrameToScreen(l.gameCtx, l.screen)
}