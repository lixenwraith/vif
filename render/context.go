package render

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
)

// RenderContext provides frame state, passed by value
type RenderContext struct {
	GameTime     time.Time
	FrameNumber  int64
	DeltaTime    float64
	IsPaused     bool
	CursorX      int
	CursorY      int
	GameXOffset  int
	GameYOffset  int
	GameWidth    int
	GameHeight   int
	ScreenWidth  int
	ScreenHeight int
}

// NewRenderContextFromGame creates a RenderContext from engine.GameContext and TimeResource
func NewRenderContextFromGame(ctx *engine.GameContext, timeRes *engine.TimeResource, cursorX, cursorY int) RenderContext {
	return RenderContext{
		GameTime:     timeRes.GameTime,
		FrameNumber:  timeRes.FrameNumber,
		DeltaTime:    timeRes.DeltaTime.Seconds(),
		IsPaused:     ctx.IsPaused.Load(),
		CursorX:      cursorX,
		CursorY:      cursorY,
		GameXOffset:  ctx.GameXOffset,
		GameYOffset:  ctx.GameYOffset,
		GameWidth:    ctx.World.Resources.Config.GameWidth,
		GameHeight:   ctx.World.Resources.Config.GameHeight,
		ScreenWidth:  ctx.Width,
		ScreenHeight: ctx.Height,
	}
}