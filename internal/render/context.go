package render

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// RenderContext provides frame state for renderers, passed by value
type RenderContext struct {
	// Time state
	GameTime  time.Time
	DeltaTime float64
	IsPaused  bool

	// Cursor position (map coordinates); CursorValid is false when no local cursor exists
	CursorX     int
	CursorY     int
	CursorValid bool

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
func NewRenderContextFromGame(ctx *engine.GameContext, timeRes engine.TimeResource, cursorX, cursorY int, cursorValid bool) RenderContext {
	config := ctx.World.Resources.Config

	// Map centering offset when map < viewport; shared with the mouse transform,
	// which is its inverse
	mapOffsetX, mapOffsetY := config.MapOffset()

	return RenderContext{
		GameTime:  timeRes.GameTime,
		DeltaTime: timeRes.DeltaTime.Seconds(),
		IsPaused:  ctx.TimeCtl.IsPaused(),

		CursorX:     cursorX,
		CursorY:     cursorY,
		CursorValid: cursorValid,

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

// IsInMap reports whether a coordinate names a cell the simulation owns.
// A centred map leaves margin inside the viewport that projects like a map cell
// but belongs to no cell, so visibility answers are gated on this first.
func (rc *RenderContext) IsInMap(mapX, mapY int) bool {
	return mapX >= 0 && mapX < rc.MapWidth && mapY >= 0 && mapY < rc.MapHeight
}

// PlayfieldViewportRect returns the map's extent in viewport coordinates.
// It is the whole viewport when the camera crops a larger map, and the centred
// sub-rectangle when the map is smaller than the viewport.
func (rc *RenderContext) PlayfieldViewportRect() Rect {
	r := RectWH(rc.MapOffsetX, rc.MapOffsetY,
		min(rc.MapWidth, rc.ViewportWidth), min(rc.MapHeight, rc.ViewportHeight))
	return r.Intersect(RectWH(0, 0, rc.ViewportWidth, rc.ViewportHeight))
}

// PlayfieldRect returns the map's extent in screen coordinates: every cell a
// simulation coordinate can legitimately reach this frame. The compositor takes
// it as the clip for simulation layers.
func (rc *RenderContext) PlayfieldRect() Rect {
	return rc.PlayfieldViewportRect().Translate(rc.GameXOffset, rc.GameYOffset)
}

// GameAreaRect returns the viewport's extent in screen coordinates. The part of
// it outside PlayfieldRect is the non-playable margin.
func (rc *RenderContext) GameAreaRect() Rect {
	return RectWH(rc.GameXOffset, rc.GameYOffset, rc.ViewportWidth, rc.ViewportHeight)
}

// MapToViewport converts map coordinates to viewport-relative coordinates
// Returns (vx, vy, visible) where visible=false if the coordinate is outside the
// map or outside viewport bounds. The projection is returned either way, so a
// caller anchoring a shape on an off-map centre can still use it.
func (rc *RenderContext) MapToViewport(mapX, mapY int) (int, int, bool) {
	vx := mapX - rc.CameraX + rc.MapOffsetX
	vy := mapY - rc.CameraY + rc.MapOffsetY
	visible := rc.IsInMap(mapX, mapY) &&
		vx >= 0 && vx < rc.ViewportWidth && vy >= 0 && vy < rc.ViewportHeight
	return vx, vy, visible
}

// IsInViewport checks if map coordinate is a map cell within the visible viewport
func (rc *RenderContext) IsInViewport(mapX, mapY int) bool {
	_, _, visible := rc.MapToViewport(mapX, mapY)
	return visible
}

// ViewportToScreen converts viewport-relative coordinates to screen coordinates
func (rc *RenderContext) ViewportToScreen(vx, vy int) (int, int) {
	return vx + rc.GameXOffset, vy + rc.GameYOffset
}

// MapToScreen converts map coordinates directly to screen coordinates
// Returns (sx, sy, visible) where visible=false if the coordinate is outside the
// map or outside the viewport, which together are the cells PlayfieldRect covers
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
