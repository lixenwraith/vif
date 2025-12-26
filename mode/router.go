package mode

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/input"
)

// Router interprets Intents and executes game logic
// Authoritative owner of game mode state
type Router struct {
	ctx     *engine.GameContext
	machine *input.Machine

	// Look-up tables: OpCode → Function
	motionLUT map[input.MotionOp]MotionFunc
	charLUT   map[input.MotionOp]CharMotionFunc
}

// NewRouter creates a router with LUTs initialized
func NewRouter(ctx *engine.GameContext, machine *input.Machine) *Router {
	r := &Router{
		ctx:     ctx,
		machine: machine,
	}

	r.motionLUT = map[input.MotionOp]MotionFunc{
		input.MotionLeft:         MotionLeft,
		input.MotionRight:        MotionRight,
		input.MotionUp:           MotionUp,
		input.MotionDown:         MotionDown,
		input.MotionWordForward:  MotionWordForward,
		input.MotionWORDForward:  MotionWORDForward,
		input.MotionWordBack:     MotionWordBack,
		input.MotionWORDBack:     MotionWORDBack,
		input.MotionWordEnd:      MotionWordEnd,
		input.MotionWORDEnd:      MotionWORDEnd,
		input.MotionLineStart:    MotionLineStart,
		input.MotionLineEnd:      MotionLineEnd,
		input.MotionFirstNonWS:   MotionFirstNonWS,
		input.MotionScreenTop:    MotionScreenTop,
		input.MotionScreenMid:    MotionScreenMid,
		input.MotionScreenBot:    MotionScreenBot,
		input.MotionFileStart:    MotionFileStart,
		input.MotionFileEnd:      MotionFileEnd,
		input.MotionParaBack:     MotionParaBack,
		input.MotionParaForward:  MotionParaForward,
		input.MotionMatchBracket: MotionMatchBracket,
		input.MotionOrigin:       MotionOrigin,
		input.MotionHalfPageUp:   MotionHalfPageUp,
		input.MotionHalfPageDown: MotionHalfPageDown,
		input.MotionColumnUp:     MotionColumnUp,
		input.MotionColumnDown:   MotionColumnDown,
	}

	r.charLUT = map[input.MotionOp]CharMotionFunc{
		input.MotionFindForward: MotionFindForward,
		input.MotionFindBack:    MotionFindBack,
		input.MotionTillForward: MotionTillForward,
		input.MotionTillBack:    MotionTillBack,
	}

	return r
}

// Handle processes an Intent and returns false if game should exit
func (r *Router) Handle(intent *input.Intent) bool {
	if intent == nil {
		return true
	}

	// Clear status message on any action
	if r.ctx.GetUISnapshot().StatusMessage != "" {
		r.ctx.SetStatusMessage("")
	}
	r.ctx.State.RecordAction()

	switch intent.Type {
	// System
	case input.IntentQuit:
		return false
	case input.IntentEscape:
		return r.handleEscape()
	case input.IntentToggleMute:
		return r.handleToggleMute()
	case input.IntentResize:
		r.ctx.HandleResize()
		return true

	// Normal mode navigation
	case input.IntentMotion:
		return r.handleMotion(intent)
	case input.IntentCharMotion:
		return r.handleCharMotion(intent)

	// Normal mode operators
	case input.IntentOperatorMotion:
		return r.handleOperatorMotion(intent)
	case input.IntentOperatorLine:
		return r.handleOperatorLine(intent)
	case input.IntentOperatorCharMotion:
		return r.handleOperatorCharMotion(intent)

	// Normal mode special
	case input.IntentSpecial:
		return r.handleSpecial(intent)
	case input.IntentNuggetJump:
		return r.handleNuggetJump()
	case input.IntentFireCleaner:
		return r.handleFireCleaner()

	// Mode switching
	case input.IntentModeSwitch:
		return r.handleModeSwitch(intent)

	// Text entry
	case input.IntentTextChar:
		return r.handleTextChar(intent)
	case input.IntentTextBackspace:
		return r.handleTextBackspace()
	case input.IntentTextConfirm:
		return r.handleTextConfirm()
	case input.IntentTextNav:
		return r.handleTextNav(intent)
	case input.IntentInsertDeleteCurrent:
		return r.handleInsertDeleteCurrent()
	case input.IntentInsertDeleteForward:
		return r.handleInsertDeleteForward()
	case input.IntentInsertDeleteBack:
		return r.handleInsertDeleteBack()

	// Overlay
	case input.IntentOverlayScroll:
		return r.handleOverlayScroll(intent)
	case input.IntentOverlayActivate:
		return r.handleOverlayActivate()
	case input.IntentOverlayPageUp:
		return r.handleOverlayPageScroll(-1)
	case input.IntentOverlayPageDown:
		return r.handleOverlayPageScroll(1)
	case input.IntentOverlayClose:
		return r.handleOverlayClose()
	}

	return true
}

// ========== System Handlers ==========

func (r *Router) handleEscape() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		r.ctx.SetSearchText("")
	case core.ModeCommand:
		r.ctx.SetCommandText("")
		r.ctx.SetPaused(false)
	case core.ModeOverlay:
		r.ctx.SetOverlayState(false, "", nil, 0)
		r.ctx.SetPaused(false)
	case core.ModeInsert:
		// Nothing to clear
	case core.ModeNormal:
		// Trigger ping grid
		r.ctx.PushEvent(event.EventPingGridRequest, &event.PingGridRequestPayload{
			Duration: constant.PingGridDuration,
		})
		return true // Stay in Normal mode
	}

	// Return to Normal mode
	r.ctx.SetMode(core.ModeNormal)
	r.machine.SetMode(input.ModeNormal)

	return true
}

func (r *Router) handleToggleMute() bool {
	if player := r.ctx.GetAudioPlayer(); player != nil {
		_ = player.ToggleMute()
	}
	return true
}

// ========== Motion Handlers ==========

func (r *Router) handleMotion(intent *input.Intent) bool {
	motionFn, ok := r.motionLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		result := motionFn(r.ctx, pos.X, pos.Y, intent.Count)
		OpMove(r.ctx, result)
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

func (r *Router) handleCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		result := charFn(r.ctx, pos.X, pos.Y, intent.Count, intent.Char)
		OpMove(r.ctx, result)

		// Track for ; and , repeat
		if result.Valid {
			r.ctx.LastFindChar = intent.Char
			r.ctx.LastFindType = motionOpToRune(intent.Motion)
			r.ctx.LastFindForward = intent.Motion == input.MotionFindForward || intent.Motion == input.MotionTillForward
		}
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

// ========== Operator Handlers ==========

func (r *Router) handleOperatorMotion(intent *input.Intent) bool {
	motionFn, ok := r.motionLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		result := motionFn(r.ctx, pos.X, pos.Y, intent.Count)

		switch intent.Operator {
		case input.OperatorDelete:
			OpDelete(r.ctx, result)
		}
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

func (r *Router) handleOperatorLine(intent *input.Intent) bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		endY := pos.Y + intent.Count - 1
		if endY >= r.ctx.GameHeight {
			endY = r.ctx.GameHeight - 1
		}

		result := MotionResult{
			StartX: 0, StartY: pos.Y,
			EndX: r.ctx.GameWidth - 1, EndY: endY,
			Type: RangeLine, Style: StyleInclusive,
			Valid: true,
		}

		switch intent.Operator {
		case input.OperatorDelete:
			OpDelete(r.ctx, result)
		}
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

func (r *Router) handleOperatorCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		result := charFn(r.ctx, pos.X, pos.Y, intent.Count, intent.Char)

		switch intent.Operator {
		case input.OperatorDelete:
			OpDelete(r.ctx, result)
		}

		// Track for ; and , repeat
		if result.Valid {
			r.ctx.LastFindChar = intent.Char
			r.ctx.LastFindType = motionOpToRune(intent.Motion)
			r.ctx.LastFindForward = (intent.Motion == input.MotionFindForward || intent.Motion == input.MotionTillForward)
		}
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

// ========== Special Command Handlers ==========

func (r *Router) handleSpecial(intent *input.Intent) bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}

		switch intent.Special {
		case input.SpecialDeleteChar:
			// x = delete chars forward
			endX := pos.X + intent.Count - 1
			if endX >= r.ctx.GameWidth {
				endX = r.ctx.GameWidth - 1
			}
			result := MotionResult{
				StartX: pos.X, StartY: pos.Y,
				EndX: endX, EndY: pos.Y,
				Type: RangeChar, Style: StyleInclusive,
				Valid: true,
			}
			OpDelete(r.ctx, result)

		case input.SpecialDeleteToEnd:
			// D = d$
			result := MotionLineEnd(r.ctx, pos.X, pos.Y, 1)
			OpDelete(r.ctx, result)

		case input.SpecialSearchNext:
			RepeatSearch(r.ctx, true)

		case input.SpecialSearchPrev:
			RepeatSearch(r.ctx, false)

		case input.SpecialRepeatFind:
			executeRepeatFind(r.ctx, false)

		case input.SpecialRepeatFindRev:
			executeRepeatFind(r.ctx, true)
		}
	})

	if intent.Command != "" {
		r.setLastCommandAndSplash(intent.Command)
	}

	return true
}

func (r *Router) handleNuggetJump() bool {
	r.ctx.PushEvent(event.EventNuggetJumpRequest, nil)
	return true
}

func (r *Router) handleFireCleaner() bool {
	// Get cursor position for cleaner origin
	var originX, originY int
	if pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity); ok {
		originX, originY = pos.X, pos.Y
	}

	r.ctx.PushEvent(event.EventDirectionalCleanerRequest, &event.DirectionalCleanerPayload{
		OriginX: originX,
		OriginY: originY,
	})

	return true
}

// ========== Mode Switch Handler ==========

func (r *Router) handleModeSwitch(intent *input.Intent) bool {
	var newMode core.GameMode
	var inputMode input.InputMode

	switch intent.ModeTarget {
	case input.ModeTargetInsert:
		newMode = core.ModeInsert
		inputMode = input.ModeInsert
	case input.ModeTargetSearch:
		newMode = core.ModeSearch
		inputMode = input.ModeSearch
		r.ctx.SetSearchText("")
	case input.ModeTargetCommand:
		newMode = core.ModeCommand
		inputMode = input.ModeCommand
		r.ctx.SetCommandText("")
		r.ctx.SetPaused(true)
	default:
		return true
	}

	// Update GameContext (authoritative)
	r.ctx.SetMode(newMode)

	// Sync input.Machine
	r.machine.SetMode(inputMode)

	return true
}

// ========== Text Entry Handlers ==========

func (r *Router) handleTextChar(intent *input.Intent) bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeInsert:
		r.handleInsertChar(intent.Char)
	case core.ModeSearch:
		r.handleSearchChar(intent.Char)
	case core.ModeCommand:
		r.handleCommandChar(intent.Char)
	}

	return true
}

func (r *Router) handleInsertChar(char rune) {
	var posX, posY int
	if pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity); ok {
		posX, posY = pos.X, pos.Y
	}

	payload := event.CharacterTypedPayloadPool.Get().(*event.CharacterTypedPayload)
	payload.Char = char
	payload.X = posX
	payload.Y = posY
	r.ctx.PushEvent(event.EventCharacterTyped, payload)
}

func (r *Router) handleSearchChar(char rune) {
	snapshot := r.ctx.GetUISnapshot()
	r.ctx.SetSearchText(snapshot.SearchText + string(char))
}

func (r *Router) handleCommandChar(char rune) {
	snapshot := r.ctx.GetUISnapshot()
	r.ctx.SetCommandText(snapshot.CommandText + string(char))
}

func (r *Router) handleTextBackspace() bool {
	currentMode := r.ctx.GetMode()
	snapshot := r.ctx.GetUISnapshot()

	switch currentMode {
	case core.ModeSearch:
		if len(snapshot.SearchText) > 0 {
			r.ctx.SetSearchText(snapshot.SearchText[:len(snapshot.SearchText)-1])
		}
	case core.ModeCommand:
		if len(snapshot.CommandText) > 0 {
			r.ctx.SetCommandText(snapshot.CommandText[:len(snapshot.CommandText)-1])
		}
	case core.ModeInsert:
		// Backspace in Insert mode is move left and delete character
		return r.handleInsertDeleteBack()
	}

	return true
}

func (r *Router) handleTextConfirm() bool {
	currentMode := r.ctx.GetMode()
	snapshot := r.ctx.GetUISnapshot()

	switch currentMode {
	case core.ModeSearch:
		if snapshot.SearchText != "" {
			r.ctx.World.RunSafe(func() {
				if PerformSearch(r.ctx, snapshot.SearchText, true) {
					r.ctx.LastSearchText = snapshot.SearchText
				}
			})
		}
		r.ctx.SetSearchText("")
		r.ctx.SetMode(core.ModeNormal)
		r.machine.SetMode(input.ModeNormal)

	case core.ModeCommand:
		command := snapshot.CommandText

		var result CommandResult
		r.ctx.World.RunSafe(func() {
			result = ExecuteCommand(r.ctx, command)
		})

		r.ctx.SetCommandText("")

		// Check if command switched to Overlay mode
		if r.ctx.GetMode() != core.ModeOverlay {
			r.ctx.SetMode(core.ModeNormal)
			r.machine.SetMode(input.ModeNormal)
			if !result.KeepPaused {
				r.ctx.SetPaused(false)
			}
		} else {
			r.machine.SetMode(input.ModeOverlay)
		}

		return result.Continue
	}

	return true
}

func (r *Router) handleTextNav(intent *input.Intent) bool {
	// Navigation in Insert mode moves cursor
	if r.ctx.GetMode() == core.ModeInsert {
		motionFn, ok := r.motionLUT[intent.Motion]
		if !ok {
			return true
		}

		r.ctx.World.RunSafe(func() {
			pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
			if !ok {
				return
			}
			result := motionFn(r.ctx, pos.X, pos.Y, intent.Count)
			OpMove(r.ctx, result)
		})
	}
	// Search/Command modes ignore arrow navigation (text is single-line, no cursor)

	return true
}

func (r *Router) handleInsertDeleteCurrent() bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}
		result := MotionResult{
			StartX: pos.X, StartY: pos.Y,
			EndX: pos.X, EndY: pos.Y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: true,
		}
		OpDelete(r.ctx, result)
	})
	return true
}

func (r *Router) handleInsertDeleteForward() bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok {
			return
		}
		// Delete at current position
		result := MotionResult{
			StartX: pos.X, StartY: pos.Y,
			EndX: pos.X, EndY: pos.Y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: true,
		}
		OpDelete(r.ctx, result)
		// Move right
		moveResult := MotionRight(r.ctx, pos.X, pos.Y, 1)
		OpMove(r.ctx, moveResult)
	})
	return true
}

func (r *Router) handleInsertDeleteBack() bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity)
		if !ok || pos.X == 0 {
			return
		}
		// Delete at pos-1
		result := MotionResult{
			StartX: pos.X - 1, StartY: pos.Y,
			EndX: pos.X - 1, EndY: pos.Y,
			Type: RangeChar, Style: StyleInclusive,
			Valid: true,
		}
		OpDelete(r.ctx, result)
		// Move to pos-1
		moveResult := MotionLeft(r.ctx, pos.X, pos.Y, 1)
		OpMove(r.ctx, moveResult)
	})
	return true
}

// ========== Overlay Handlers ==========

func (r *Router) handleOverlayScroll(intent *input.Intent) bool {
	snapshot := r.ctx.GetUISnapshot()
	newScroll := snapshot.OverlayScroll + int(intent.ScrollDir)

	if newScroll < 0 {
		newScroll = 0
	}
	if newScroll >= len(snapshot.OverlayContent) {
		newScroll = len(snapshot.OverlayContent) - 1
	}
	if newScroll < 0 {
		newScroll = 0
	}

	r.ctx.SetOverlayScroll(newScroll)
	return true
}

func (r *Router) handleOverlayClose() bool {
	r.ctx.SetOverlayState(false, "", nil, 0)
	r.ctx.SetMode(core.ModeNormal)
	r.machine.SetMode(input.ModeNormal)
	r.ctx.SetPaused(false)
	return true
}

// TODO: future implementation
func (r *Router) handleOverlayActivate() bool {
	// Stub: future section toggle/expand functionality
	return true
}

func (r *Router) handleOverlayPageScroll(direction int) bool {
	snapshot := r.ctx.GetUISnapshot()
	// Page scroll by half visible height (estimate 20 lines visible)
	pageSize := 10
	newScroll := snapshot.OverlayScroll + (direction * pageSize)

	if newScroll < 0 {
		newScroll = 0
	}
	// Upper bound handled by renderer based on content

	r.ctx.SetOverlayScroll(newScroll)
	return true
}

// ========== Helper Methods ==========

func (r *Router) setLastCommandAndSplash(cmd string) {
	r.ctx.SetLastCommand(cmd)

	// Get cursor position for splash origin
	var originX, originY int
	if pos, ok := r.ctx.World.Positions.Get(r.ctx.CursorEntity); ok {
		originX, originY = pos.X, pos.Y
	}

	r.ctx.PushEvent(event.EventSplashRequest, &event.SplashRequestPayload{
		Text:    cmd,
		Color:   component.SplashColorNormal,
		OriginX: originX,
		OriginY: originY,
	})
}

// motionOpToRune converts MotionOp to the canonical rune for tracking
func motionOpToRune(op input.MotionOp) rune {
	switch op {
	case input.MotionLeft:
		return 'h'
	case input.MotionRight:
		return 'l'
	case input.MotionUp:
		return 'k'
	case input.MotionDown:
		return 'j'
	case input.MotionWordForward:
		return 'w'
	case input.MotionWORDForward:
		return 'W'
	case input.MotionWordBack:
		return 'b'
	case input.MotionWORDBack:
		return 'B'
	case input.MotionWordEnd:
		return 'e'
	case input.MotionWORDEnd:
		return 'E'
	case input.MotionLineStart:
		return '0'
	case input.MotionLineEnd:
		return '$'
	case input.MotionFirstNonWS:
		return '^'
	case input.MotionScreenTop:
		return 'H'
	case input.MotionScreenMid:
		return 'M'
	case input.MotionScreenBot:
		return 'L'
	case input.MotionFileStart:
		return 'g'
	case input.MotionFileEnd:
		return 'G'
	case input.MotionParaBack:
		return '{'
	case input.MotionParaForward:
		return '}'
	case input.MotionMatchBracket:
		return '%'
	case input.MotionOrigin:
		return 'o'
	case input.MotionFindForward:
		return 'f'
	case input.MotionFindBack:
		return 'F'
	case input.MotionTillForward:
		return 't'
	case input.MotionTillBack:
		return 'T'
	}
	return 0
}