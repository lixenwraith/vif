package render

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
)

// RenderContext provides frame state for renderers, passed by value
type RenderContext struct {
	// Time state
	GameTime  time.Time
	DeltaTime float64
	IsPaused  bool

	// Cursor position (map coordinates)
	CursorX int
	CursorY int

	// Screen margins (game area offset from terminal origin)
	GameXOffset int
	GameYOffset int

	// Viewport dimensions (visible game area, terminal-derived)
	ViewportWidth  int
	ViewportHeight int

	// Camera position (top-left of viewport in map coordinates)
	// When map <= viewport: 0
	// When map > viewport: [0, MapDim - ViewportDim]
	CameraX int
	CameraY int

	// Map centering offset (viewport coordinates)
	// Non-zero when map dimension < viewport dimension
	MapOffsetX int
	MapOffsetY int

	// Map dimensions (simulation bounds)
	MapWidth  int
	MapHeight int

	// Screen dimensions (terminal size)
	ScreenWidth  int
	ScreenHeight int
}

// NewRenderContextFromGame creates a RenderContext from engine.GameContext and TimeResource
func NewRenderContextFromGame(ctx *engine.GameContext, timeRes *engine.TimeResource, cursorX, cursorY int) RenderContext {
	config := ctx.World.Resources.Config

	// Compute map centering offset when map < viewport
	mapOffsetX := 0
	mapOffsetY := 0
	if config.MapWidth < config.ViewportWidth {
		mapOffsetX = (config.ViewportWidth - config.MapWidth) / 2
	}
	if config.MapHeight < config.ViewportHeight {
		mapOffsetY = (config.ViewportHeight - config.MapHeight) / 2
	}

	return RenderContext{
		GameTime:  timeRes.GameTime,
		DeltaTime: timeRes.DeltaTime.Seconds(),
		IsPaused:  ctx.IsPaused.Load(),

		CursorX: cursorX,
		CursorY: cursorY,

		GameXOffset: ctx.GameXOffset,
		GameYOffset: ctx.GameYOffset,

		ViewportWidth:  config.ViewportWidth,
		ViewportHeight: config.ViewportHeight,

		CameraX: config.CameraX,
		CameraY: config.CameraY,

		MapOffsetX: mapOffsetX,
		MapOffsetY: mapOffsetY,

		MapWidth:  config.MapWidth,
		MapHeight: config.MapHeight,

		ScreenWidth:  ctx.Width,
		ScreenHeight: ctx.Height,
	}
}

// MapToViewport converts map coordinates to viewport-relative coordinates
// Returns (vx, vy, visible) where visible=false if outside viewport bounds
func (rc *RenderContext) MapToViewport(mapX, mapY int) (int, int, bool) {
	vx := mapX - rc.CameraX + rc.MapOffsetX
	vy := mapY - rc.CameraY + rc.MapOffsetY
	visible := vx >= 0 && vx < rc.ViewportWidth && vy >= 0 && vy < rc.ViewportHeight
	return vx, vy, visible
}

// IsInViewport checks if map coordinate is within visible viewport
func (rc *RenderContext) IsInViewport(mapX, mapY int) bool {
	vx := mapX - rc.CameraX + rc.MapOffsetX
	vy := mapY - rc.CameraY + rc.MapOffsetY
	return vx >= 0 && vx < rc.ViewportWidth && vy >= 0 && vy < rc.ViewportHeight
}

// ViewportToScreen converts viewport-relative coordinates to screen coordinates
func (rc *RenderContext) ViewportToScreen(vx, vy int) (int, int) {
	return vx + rc.GameXOffset, vy + rc.GameYOffset
}

// MapToScreen converts map coordinates directly to screen coordinates
// Returns (sx, sy, visible) where visible=false if outside viewport
func (rc *RenderContext) MapToScreen(mapX, mapY int) (int, int, bool) {
	vx, vy, visible := rc.MapToViewport(mapX, mapY)
	if !visible {
		return 0, 0, false
	}
	return vx + rc.GameXOffset, vy + rc.GameYOffset, true
}

// VisibleMapBounds returns the map coordinate range currently visible in viewport
// Returns (minX, minY, maxX, maxY) clamped to map bounds
func (rc *RenderContext) VisibleMapBounds() (int, int, int, int) {
	minX := rc.CameraX
	minY := rc.CameraY
	maxX := rc.CameraX + rc.ViewportWidth - 1 - rc.MapOffsetX*2
	maxY := rc.CameraY + rc.ViewportHeight - 1 - rc.MapOffsetY*2

	// Clamp to map bounds
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= rc.MapWidth {
		maxX = rc.MapWidth - 1
	}
	if maxY >= rc.MapHeight {
		maxY = rc.MapHeight - 1
	}

	return minX, minY, maxX, maxY
}

// CursorViewportPos returns cursor position in viewport coordinates
func (rc *RenderContext) CursorViewportPos() (int, int) {
	return rc.CursorX - rc.CameraX + rc.MapOffsetX, rc.CursorY - rc.CameraY + rc.MapOffsetY
}