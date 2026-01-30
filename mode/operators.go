package mode

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// OpMove updates cursor position based on motion result
func OpMove(ctx *engine.GameContext, result MotionResult) {
	if !result.Valid {
		return
	}

	ctx.World.Positions.SetPosition(ctx.World.Resources.Player.Entity, component.PositionComponent{
		X: result.EndX,
		Y: result.EndY,
	})

	ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{
		X: result.EndX,
		Y: result.EndY,
	})
}

// OpDelete emits a deletion request event based on the motion result
func OpDelete(ctx *engine.GameContext, result MotionResult) {
	if !result.Valid {
		return
	}

	payload := &event.DeleteRequestPayload{}

	if result.Type == RangeLine {
		payload.RangeType = event.DeleteRangeLine
		payload.StartY = result.StartY
		payload.EndY = result.EndY
	} else {
		payload.RangeType = event.DeleteRangeChar

		// Normalize range: Start should be visually before End
		sx, sy := result.StartX, result.StartY
		ex, ey := result.EndX, result.EndY

		if sy > ey || (sy == ey && sx > ex) {
			// Swap to ensure Start is first
			sx, sy, ex, ey = ex, ey, sx, sy
		}

		// Adjust for exclusive motions (exclude the last character)
		// e.g. "dw" lands on start of next word, but we don't delete that character
		if result.Style == StyleExclusive {
			if ex > 0 {
				ex--
			} else {
				// Wrap back to previous line if at start of line
				if ey > 0 {
					ey--
					ex = ctx.World.Resources.Config.GameWidth - 1
				} else {
					// At 0,0 - effective range is empty if sx=0,sy=0
					// Check if range became invalid (End before Start)
					if sy > ey || (sy == ey && sx > ex) {
						return // Nothing to delete
					}
				}
			}
		}

		payload.StartX = sx
		payload.StartY = sy
		payload.EndX = ex
		payload.EndY = ey
	}

	ctx.PushEvent(event.EventDeleteRequest, payload)
}