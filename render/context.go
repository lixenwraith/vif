// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package render

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
)

// RenderContext provides frame state, passed by value
type RenderContext struct {
	GameTime    time.Time
	FrameNumber int64
	DeltaTime   float64
	IsPaused    bool
	CursorX     int
	CursorY     int
	GameX       int
	GameY       int
	GameWidth   int
	GameHeight  int
	Width       int
	Height      int
}

// NewRenderContextFromGame creates a RenderContext from engine.GameContext and TimeResource
func NewRenderContextFromGame(ctx *engine.GameContext, timeRes *engine.TimeResource, cursorX, cursorY int) RenderContext {
	return RenderContext{
		GameTime:    timeRes.GameTime,
		FrameNumber: timeRes.FrameNumber,
		DeltaTime:   timeRes.DeltaTime.Seconds(),
		IsPaused:    ctx.IsPaused.Load(),
		CursorX:     cursorX,
		CursorY:     cursorY,
		GameX:       ctx.GameX,
		GameY:       ctx.GameY,
		GameWidth:   ctx.GameWidth,
		GameHeight:  ctx.GameHeight,
		Width:       ctx.Width,
		Height:      ctx.Height,
	}
}