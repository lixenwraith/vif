package mode

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/parameter"
)

const undoStackSize = 256

type undoPosition struct {
	x, y int
}

// Router interprets Intents and executes game logic
// Authoritative owner of game mode state
type Router struct {
	ctx     *engine.GameContext
	machine *input.Machine

	macro *MacroManager

	undoRing  [undoStackSize]undoPosition
	undoHead  int // next write index
	undoCount int // valid entry count

	// Input state (find/search repeat)
	lastSearchText  string // Preserved for n/N repeat
	lastFindChar    rune   // Target character for f/F/t/T
	lastFindForward bool   // true for f/t, false for F/T
	lastFindType    rune   // Motion type: 'f', 'F', 't', or 'T'

	// Mouse hold state for repeat firing
	mouseLeftHeld     bool
	mouseRightHeld    bool
	mouseLastFireMain time.Time
	mouseLastFireSpec time.Time

	// Look-up tables: OpCode → Function
	motionLUT map[input.MotionOp]MotionFunc
	charLUT   map[input.MotionOp]CharMotionFunc
}

// NewRouter creates a router with LUTs initialized
func NewRouter(ctx *engine.GameContext, machine *input.Machine) *Router {
	r := &Router{
		ctx:     ctx,
		machine: machine,
		macro:   NewMacroManager(),
	}

	r.motionLUT = map[input.MotionOp]MotionFunc{
		input.MotionLeft:                MotionLeft,
		input.MotionRight:               MotionRight,
		input.MotionUp:                  MotionUp,
		input.MotionDown:                MotionDown,
		input.MotionWordForward:         MotionWordForward,
		input.MotionWORDForward:         MotionWORDForward,
		input.MotionWordBack:            MotionWordBack,
		input.MotionWORDBack:            MotionWORDBack,
		input.MotionWordEnd:             MotionWordEnd,
		input.MotionWORDEnd:             MotionWORDEnd,
		input.MotionLineStart:           MotionLineStart,
		input.MotionLineEnd:             MotionLineEnd,
		input.MotionFirstNonWS:          MotionFirstNonWS,
		input.MotionScreenVerticalMid:   MotionScreenVerticalMid,
		input.MotionScreenHorizontalMid: MotionScreenHorizontalMid,
		input.MotionScreenTop:           MotionScreenTop,
		input.MotionScreenBottom:        MotionScreenBottom,
		input.MotionParaBack:            MotionParaBack,
		input.MotionParaForward:         MotionParaForward,
		input.MotionMatchBracket:        MotionMatchBracket,
		input.MotionOrigin:              MotionOrigin,
		input.MotionEnd:                 MotionEnd,
		input.MotionCenter:              MotionCenter,
		input.MotionHalfPageLeft:        MotionHalfPageLeft,
		input.MotionHalfPageRight:       MotionHalfPageRight,
		input.MotionHalfPageUp:          MotionHalfPageUp,
		input.MotionHalfPageDown:        MotionHalfPageDown,
		input.MotionColumnUp:            MotionColumnUp,
		input.MotionColumnDown:          MotionColumnDown,
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

	// Macro reset check (triggered by :new)
	if r.ctx.MacroClearFlag.CompareAndSwap(true, false) {
		r.macro.Reset()
		r.ctx.MacroRecording.Store(false)
		r.ctx.MacroPlaying.Store(false)
	}

	// Clear status message on any action
	if r.ctx.GetStatusMessage() != "" {
		r.ctx.SetStatusMessage("", 0, false)
	}
	r.ctx.State.RecordAction()

	// === Macro Context Interception ===

	if intent.Type == input.IntentMacroRecordToggle {
		if r.macro.IsRecording() {
			// q while recording -> stop recording
			r.macro.StopRecording()
			r.ctx.MacroRecording.Store(false)
			r.ctx.MacroRecordingLabel.Store(0)
			return true
		}
		// q while idle OR playing -> transition to record-await
		// Recording takes priority; specific label's playback stops in handleMacroRecordStart
		r.machine.SetState(input.StateMacroRecordAwait)
		return true
	}

	// Record intent if recording (exclude macro control intents and playback-originated)
	if r.macro.IsRecording() && !isMacroControlIntent(intent.Type) && !intent.MacroPlayback {
		r.macro.Record(*intent)
	}

	// Skip mouse input when disabled
	if r.ctx.MouseDisabled.Load() && isMouseIntent(intent.Type) {
		return true
	}

	switch intent.Type {
	// System
	case input.IntentQuit:
		return false
	case input.IntentEscape:
		return r.handleEscape()
	case input.IntentToggleEffectMute:
		return r.handleToggleEffectMute()
	case input.IntentToggleMusicMute:
		return r.handleToggleMusicMute()
	case input.IntentResize:
		r.ctx.HandleResize()
		return true

	// Normal mode navigation
	case input.IntentMotion:
		return r.handleMotion(intent)
	case input.IntentCharMotion:
		return r.handleCharMotion(intent)
	case input.IntentMotionMarkerShow:
		return r.handleMotionMarkerShow(intent)
	case input.IntentMotionMarkerJump:
		return r.handleMotionMarkerJump(intent)

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
	case input.IntentGoldJump:
		return r.handleGoldJump()
	case input.IntentFireMain:
		return r.handleFireMain()
	case input.IntentFireSpecial:
		return r.handleFireSpecial()

	// Mode switching
	case input.IntentModeSwitch:
		return r.handleModeSwitch(intent)
	case input.IntentAppend:
		return r.handleAppend()

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

		// Undo
	case input.IntentUndo:
		return r.handleUndo(intent)

	// Macro control
	case input.IntentMacroRecordStart:
		return r.handleMacroRecordStart(intent)
	case input.IntentMacroRecordStop:
		return r.handleMacroRecordStop()
	case input.IntentMacroPlay:
		return r.handleMacroPlay(intent)
	case input.IntentMacroPlayInfinite:
		return r.handleMacroPlayInfinite(intent)
	case input.IntentMacroPlayAll:
		return r.handleMacroPlayAll()
	case input.IntentMacroStopOne:
		return r.handleMacroStopOne(intent)
	case input.IntentMacroStopAll:
		return r.handleMacroStopAll()

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

	// Mouse
	case input.IntentMouseLeftDown:
		return r.handleMouseLeftDown(intent)
	case input.IntentMouseLeftUp:
		return r.handleMouseLeftUp()
	case input.IntentMouseRightDown:
		return r.handleMouseRightDown()
	case input.IntentMouseRightUp:
		return r.handleMouseRightUp()
	case input.IntentMouseDrag:
		return r.handleMouseDrag(intent)
	case input.IntentMouseWheelMove:
		return r.handleMouseWheelMove(intent)
	case input.IntentMouseMove:
		return r.handleMouseMove(intent)
	}

	return true
}

// --- System Handlers ---

func (r *Router) handleEscape() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		r.ctx.SetSearchText("")
	case core.ModeCommand:
		r.ctx.SetCommandText("")
		r.ctx.SetPaused(false)
	case core.ModeOverlay:
		r.ctx.SetPaused(false)
	case core.ModeNormal:
		// ESC in Normal mode triggers ping grid, no mode change
		r.ctx.PushEvent(event.EventPingGridRequest, &event.PingGridRequestPayload{
			Duration: parameter.PingGridDuration,
		})
		return true
	}

	// Return to Normal mode via centralized transition
	r.transitionMode(core.ModeNormal)

	return true
}

func (r *Router) handleToggleEffectMute() bool {
	if player := r.ctx.GetAudioPlayer(); player != nil {
		_ = player.ToggleEffectMute()
	}
	return true
}

func (r *Router) handleToggleMusicMute() bool {
	player := r.ctx.GetAudioPlayer()
	if player == nil {
		return true
	}

	// TODO: Move this to music system, should not be handled here
	musicEnabled := player.ToggleMusicMute()
	if musicEnabled {
		bpm := parameter.APMToBPM(r.ctx.State.GetAPM())
		player.SetMusicBPM(bpm)
		player.SetBeatPattern(core.PatternBeatBasic, 0, false)
		player.StartMusic()
	}
	return true
}

// --- Motion Handlers ---

func (r *Router) handleMotion(intent *input.Intent) bool {
	motionFn, ok := r.motionLUT[intent.Motion]
	if !ok {
		return true
	}

	r.captureForUndo()

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
		if !ok {
			return
		}

		result := motionFn(r.ctx, pos.X, pos.Y, intent.Count)
		OpMove(r.ctx, result)
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

func (r *Router) handleCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	r.captureForUndo()

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
		if !ok {
			return
		}

		result := charFn(r.ctx, pos.X, pos.Y, intent.Count, intent.Char)
		OpMove(r.ctx, result)

		// Track for ; and , repeat
		if result.Valid {
			r.lastFindChar = intent.Char
			r.lastFindType = motionOpToRune(intent.Motion)
			r.lastFindForward = intent.Motion == input.MotionFindForward || intent.Motion == input.MotionTillForward
		}
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

func (r *Router) handleMotionMarkerShow(intent *input.Intent) bool {
	// Emit event for MotionMarkerSystem to show colored markers
	dir := r.motionToDirection(intent.Motion)
	r.ctx.PushEvent(event.EventMotionMarkerShowColored, &event.MotionMarkerShowPayload{
		DirectionX: dir[0],
		DirectionY: dir[1],
	})
	return true
}

func (r *Router) handleMotionMarkerJump(intent *input.Intent) bool {
	// Clear colored markers
	r.ctx.PushEvent(event.EventMotionMarkerClearColored, nil)

	r.captureForUndo()

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
		if !ok {
			return
		}

		var glyphType component.GlyphType = -1 // -1 = any
		switch intent.Char {
		case 'r':
			glyphType = component.GlyphRed
		case 'g':
			glyphType = component.GlyphGreen
		case 'b':
			glyphType = component.GlyphBlue
		}

		result := MotionColoredGlyph(r.ctx, pos.X, pos.Y, intent.Count, intent.Motion, glyphType)
		OpMove(r.ctx, result)
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}
	return true
}

func (r *Router) motionToDirection(motion input.MotionOp) [2]int {
	switch motion {
	case input.MotionColoredGlyphRight:
		return [2]int{1, 0}
	case input.MotionColoredGlyphLeft:
		return [2]int{-1, 0}
	case input.MotionColoredGlyphUp:
		return [2]int{0, -1}
	case input.MotionColoredGlyphDown:
		return [2]int{0, 1}
	}
	return [2]int{0, 0}
}

// --- Operator Handlers ---

func (r *Router) handleOperatorMotion(intent *input.Intent) bool {
	motionFn, ok := r.motionLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

func (r *Router) handleOperatorLine(intent *input.Intent) bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
		if !ok {
			return
		}

		endY := pos.Y + intent.Count - 1
		if endY >= r.ctx.World.Resources.Config.MapHeight {
			endY = r.ctx.World.Resources.Config.MapHeight - 1
		}

		result := MotionResult{
			StartX: 0, StartY: pos.Y,
			EndX: r.ctx.World.Resources.Config.MapWidth - 1, EndY: endY,
			Type: RangeLine, Style: StyleInclusive,
			Valid: true,
		}

		switch intent.Operator {
		case input.OperatorDelete:
			OpDelete(r.ctx, result)
		}
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

func (r *Router) handleOperatorCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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
			r.lastFindChar = intent.Char
			r.lastFindType = motionOpToRune(intent.Motion)
			r.lastFindForward = (intent.Motion == input.MotionFindForward || intent.Motion == input.MotionTillForward)
		}
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

// --- Special Command Handlers ---

func (r *Router) handleSpecial(intent *input.Intent) bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
		if !ok {
			return
		}

		switch intent.Special {
		case input.SpecialDeleteChar:
			// x = delete chars forward
			endX := pos.X + intent.Count - 1
			if endX >= r.ctx.World.Resources.Config.MapWidth {
				endX = r.ctx.World.Resources.Config.MapWidth - 1
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
			RepeatSearch(r.ctx, r.lastSearchText, true)

		case input.SpecialSearchPrev:
			RepeatSearch(r.ctx, r.lastSearchText, false)

		case input.SpecialRepeatFind:
			r.executeRepeatFind(false)

		case input.SpecialRepeatFindRev:
			r.executeRepeatFind(true)
		}
	})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

func (r *Router) handleNuggetJump() bool {
	r.captureForUndo()
	r.ctx.PushEvent(event.EventNuggetJumpRequest, nil)
	return true
}

func (r *Router) handleGoldJump() bool {
	r.captureForUndo()
	r.ctx.PushEvent(event.EventGoldJumpRequest, nil)
	return true
}

func (r *Router) handleFireMain() bool {
	r.ctx.PushEvent(event.EventWeaponFireRequest, nil)
	return true
}

func (r *Router) handleFireSpecial() bool {
	r.ctx.PushEvent(event.EventFireSpecialRequest, nil)
	return true
}

// --- Mode Switch Handler ---

func (r *Router) handleModeSwitch(intent *input.Intent) bool {
	var newMode core.GameMode

	switch intent.ModeTarget {
	case input.ModeTargetInsert:
		newMode = core.ModeInsert
	case input.ModeTargetSearch:
		newMode = core.ModeSearch
		r.ctx.SetSearchText("")
	case input.ModeTargetCommand:
		newMode = core.ModeCommand
		r.ctx.SetCommandText("")
		r.ctx.SetPaused(true)
	case input.ModeTargetVisual:
		if r.ctx.IsVisualMode() {
			newMode = core.ModeNormal // Toggle off
		} else {
			newMode = core.ModeVisual
		}
	case input.ModeTargetNormal:
		newMode = core.ModeNormal
	default:
		return true
	}

	r.transitionMode(newMode)
	return true
}

func (r *Router) handleAppend() bool {
	r.captureForUndo()

	// 1. Move cursor right
	r.ctx.World.RunSafe(func() {
		if pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity); ok {
			result := MotionRight(r.ctx, pos.X, pos.Y, 1)
			OpMove(r.ctx, result)
		}
	})

	// 2. Switch to Insert mode via centralized transition
	r.transitionMode(core.ModeInsert)

	return true
}

// transitionMode handles all mode changes with consistent side-effects
func (r *Router) transitionMode(newMode core.GameMode) {
	// 1. Update game mode
	r.ctx.SetMode(newMode)

	// 2. Update ping bounds (recomputes based on new mode)
	r.ctx.World.UpdateBoundsRadius()

	// 3. Emit mode change event
	r.ctx.PushEvent(event.EventModeChangeNotification, &event.ModeChangeNotificationPayload{Mode: newMode})

	// 4. Sync input machine
	var inputMode input.InputMode
	switch newMode {
	case core.ModeNormal:
		inputMode = input.ModeNormal
	case core.ModeVisual:
		inputMode = input.ModeVisual
	case core.ModeInsert:
		inputMode = input.ModeInsert
	case core.ModeSearch:
		inputMode = input.ModeSearch
	case core.ModeCommand:
		inputMode = input.ModeCommand
	case core.ModeOverlay:
		inputMode = input.ModeOverlay
	}
	r.machine.SetMode(inputMode)
}

// --- Text Entry Handlers ---

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
	if pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity); ok {
		posX, posY = pos.X, pos.Y
	}

	payload := event.CharacterTypedPayloadPool.Get().(*event.CharacterTypedPayload)
	payload.Char = char
	payload.X = posX
	payload.Y = posY
	r.ctx.PushEvent(event.EventCharacterTyped, payload)
}

func (r *Router) handleSearchChar(char rune) {
	searchText := r.ctx.GetSearchText()
	r.ctx.SetSearchText(searchText + string(char))
}

func (r *Router) handleCommandChar(char rune) {
	commandText := r.ctx.GetCommandText()
	r.ctx.SetCommandText(commandText + string(char))
}

func (r *Router) handleTextBackspace() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		searchText := r.ctx.GetSearchText()
		if len(searchText) > 0 {
			r.ctx.SetSearchText(searchText[:len(searchText)-1])
		}
	case core.ModeCommand:
		commandText := r.ctx.GetCommandText()
		if len(commandText) > 0 {
			r.ctx.SetCommandText(commandText[:len(commandText)-1])
		}
	case core.ModeInsert:
		// Backspace in Insert mode is move left and delete character
		return r.handleInsertDeleteBack()
	}

	return true
}

func (r *Router) handleTextConfirm() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		searchText := r.ctx.GetSearchText()
		if searchText != "" {
			r.ctx.World.RunSafe(func() {
				if PerformSearch(r.ctx, searchText, true) {
					r.lastSearchText = searchText
				}
			})
		}
		r.ctx.SetSearchText("")
		r.ctx.SetMode(core.ModeNormal)
		r.machine.SetMode(input.ModeNormal)

	case core.ModeCommand:
		commandText := r.ctx.GetCommandText()

		var result CommandResult
		r.ctx.World.RunSafe(func() {
			result = ExecuteCommand(r.ctx, commandText)
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

		r.captureForUndo()

		r.ctx.World.RunSafe(func() {
			pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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
		// Move cursor right
		moveResult := MotionRight(r.ctx, pos.X, pos.Y, 1)
		OpMove(r.ctx, moveResult)
	})
	return true
}

func (r *Router) handleInsertDeleteBack() bool {
	r.ctx.World.RunSafe(func() {
		pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
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

// --- Undo ---

// captureForUndo records current cursor position before movement
func (r *Router) captureForUndo() {
	pos, ok := r.ctx.World.Positions.GetPosition(r.ctx.World.Resources.Player.Entity)
	if !ok {
		return
	}

	// Deduplicate consecutive identical positions
	if r.undoCount > 0 {
		topIdx := (r.undoHead - 1 + undoStackSize) % undoStackSize
		if r.undoRing[topIdx].x == pos.X && r.undoRing[topIdx].y == pos.Y {
			return
		}
	}

	r.undoRing[r.undoHead] = undoPosition{x: pos.X, y: pos.Y}
	r.undoHead = (r.undoHead + 1) % undoStackSize
	if r.undoCount < undoStackSize {
		r.undoCount++
	}
}

func (r *Router) handleUndo(intent *input.Intent) bool {
	if r.undoCount == 0 {
		return true
	}

	n := intent.Count
	if n < 1 {
		n = 1
	}
	if n > r.undoCount {
		n = r.undoCount
	}

	var x, y int
	for i := 0; i < n; i++ {
		r.undoHead = (r.undoHead - 1 + undoStackSize) % undoStackSize
		r.undoCount--
		x, y = r.undoRing[r.undoHead].x, r.undoRing[r.undoHead].y
	}

	r.ctx.World.RunSafe(func() {
		r.ctx.World.Positions.SetPosition(r.ctx.World.Resources.Player.Entity, component.PositionComponent{
			X: x,
			Y: y,
		})
	})

	r.ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: x, Y: y})

	if intent.Command != "" {
		r.ctx.SetLastCommand(intent.Command)
	}

	return true
}

// --- Overlay Handlers ---

func (r *Router) handleOverlayClose() bool {
	r.ctx.SetOverlayContent(nil)
	r.ctx.SetPaused(false)
	r.transitionMode(core.ModeNormal)
	return true
}

// TODO: future implementation
func (r *Router) handleOverlayActivate() bool {
	// Stub: future section toggle/expand functionality
	return true
}

func (r *Router) handleOverlayPageScroll(direction int) bool {
	// Calculate visible height based on overlay dimensions
	overlayH := int(float64(r.ctx.Height) * parameter.OverlayHeightPercent)

	// Subtract border (2) + padding (2) + hints row (1)
	visibleH := overlayH - 2 - (2 * parameter.OverlayPaddingY) - 1
	pageSize := visibleH / 2
	if pageSize < 1 {
		pageSize = 1
	}

	newScroll := r.ctx.GetOverlayScroll() + (direction * pageSize)
	if newScroll < 0 {
		newScroll = 0
	}

	r.ctx.SetOverlayScroll(newScroll)
	return true
}

func (r *Router) handleOverlayScroll(intent *input.Intent) bool {
	newScroll := r.ctx.GetOverlayScroll() + int(intent.ScrollDir)

	if newScroll < 0 {
		newScroll = 0
	}

	r.ctx.SetOverlayScroll(newScroll)
	return true
}

// --- Mouse ---

func (r *Router) handleMouseLeftDown(intent *input.Intent) bool {
	r.moveMouseCursor(intent)
	r.ctx.PushEvent(event.EventWeaponFireRequest, nil)
	r.mouseLeftHeld = true
	r.mouseLastFireMain = r.ctx.PausableClock.Now()
	return true
}

func (r *Router) handleMouseLeftUp() bool {
	r.mouseLeftHeld = false
	return true
}

func (r *Router) handleMouseRightDown() bool {
	// Fire special at current cursor position, no movement
	r.ctx.PushEvent(event.EventFireSpecialRequest, nil)
	r.mouseRightHeld = true
	r.mouseLastFireSpec = r.ctx.PausableClock.Now()
	return true
}

func (r *Router) handleMouseRightUp() bool {
	r.mouseRightHeld = false
	return true
}

func (r *Router) handleMouseDrag(intent *input.Intent) bool {
	if r.mouseLeftHeld {
		r.moveMouseCursor(intent)
	}
	return true
}

func (r *Router) handleMouseWheelMove(intent *input.Intent) bool {
	r.moveMouseCursor(intent)
	return true
}

func (r *Router) handleMouseMove(intent *input.Intent) bool {
	if !r.ctx.MouseFreeMode.Load() {
		return true
	}
	r.moveMouseCursor(intent)
	return true
}

// ProcessMouseTick handles repeat firing for held mouse buttons
// Called from main loop each frame
func (r *Router) ProcessMouseTick() {
	if r.ctx.MouseDisabled.Load() {
		return
	}

	now := r.ctx.PausableClock.Now()
	autoMode := r.ctx.MouseAutoMode.Load()

	if (r.mouseLeftHeld || autoMode) && now.Sub(r.mouseLastFireMain) >= parameter.MouseRepeatInterval {
		r.ctx.PushEvent(event.EventWeaponFireRequest, nil)
		r.mouseLastFireMain = now
	}

	if (r.mouseRightHeld || autoMode) && now.Sub(r.mouseLastFireSpec) >= parameter.MouseRepeatInterval {
		r.ctx.PushEvent(event.EventFireSpecialRequest, nil)
		r.mouseLastFireSpec = now
	}
}

// moveMouseCursor handles coordinate conversion, bounds check, and cursor movement
// Returns true if cursor was moved successfully
func (r *Router) moveMouseCursor(intent *input.Intent) bool {
	termX := intent.Count
	termY := int(intent.Char)

	// Convert terminal coords to viewport coords
	viewportX := termX - r.ctx.GameXOffset
	viewportY := termY - r.ctx.GameYOffset

	// Viewport bounds check
	config := r.ctx.World.Resources.Config
	if viewportX < 0 || viewportX >= config.ViewportWidth || viewportY < 0 || viewportY >= config.ViewportHeight {
		return false
	}

	// Convert viewport coords to map coords
	gameX := viewportX + config.CameraX
	gameY := viewportY + config.CameraY

	// Map bounds check (defensive, should not exceed given viewport clamp)
	if gameX < 0 || gameX >= config.MapWidth || gameY < 0 || gameY >= config.MapHeight {
		return false
	}

	// Block check
	if isCursorBlocked(r.ctx, gameX, gameY) {
		return false
	}

	// Move cursor
	r.ctx.World.Positions.SetPosition(r.ctx.World.Resources.Player.Entity, component.PositionComponent{
		X: gameX,
		Y: gameY,
	})
	r.ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: gameX, Y: gameY})

	return true
}

// --- Macro ---

func (r *Router) handleMacroRecordStart(intent *input.Intent) bool {
	label := intent.Char

	// If this label is playing, stop it first
	if r.macro.IsLabelPlaying(label) {
		r.macro.StopPlayback(label)
		r.updateMacroPlayingState()
		// Continue to start recording
	}

	r.macro.StartRecording(label)
	r.ctx.MacroRecording.Store(true)
	r.ctx.MacroRecordingLabel.Store(int32(label))
	return true
}

func (r *Router) handleMacroRecordStop() bool {
	r.macro.StopRecording()
	r.ctx.MacroRecording.Store(false)
	r.ctx.MacroRecordingLabel.Store(0)
	return true
}

func (r *Router) handleMacroPlay(intent *input.Intent) bool {
	now := r.ctx.PausableClock.Now()
	if r.macro.StartPlayback(intent.Char, intent.Count, now) {
		r.ctx.MacroPlaying.Store(true)
	}
	return true
}

func (r *Router) handleMacroPlayInfinite(intent *input.Intent) bool {
	now := r.ctx.PausableClock.Now()
	if r.macro.StartPlayback(intent.Char, 0, now) { // 0 = infinite
		r.ctx.MacroPlaying.Store(true)
	}
	return true
}

func (r *Router) handleMacroPlayAll() bool {
	now := r.ctx.PausableClock.Now()
	if r.macro.StartAllPlayback(now) > 0 {
		r.ctx.MacroPlaying.Store(true)
	}
	return true
}

func (r *Router) handleMacroStopOne(intent *input.Intent) bool {
	r.macro.StopPlayback(intent.Char)
	r.updateMacroPlayingState()
	return true
}

func (r *Router) handleMacroStopAll() bool {
	r.macro.StopAllPlayback()
	r.ctx.MacroPlaying.Store(false)
	return true
}

func (r *Router) updateMacroPlayingState() {
	r.ctx.MacroPlaying.Store(r.macro.IsPlaying())
}

func (r *Router) ProcessMacroTick() []*input.Intent {
	if r.ctx.IsPaused.Load() || r.ctx.IsCommandMode() {
		return nil
	}
	now := r.ctx.PausableClock.Now()
	intents := r.macro.Tick(now)
	r.updateMacroPlayingState()
	return intents
}

// --- Helper Methods ---

func isMacroControlIntent(t input.IntentType) bool {
	switch t {
	case input.IntentMacroRecordStart, input.IntentMacroRecordStop,
		input.IntentMacroPlay, input.IntentMacroPlayInfinite,
		input.IntentMacroStopOne, input.IntentMacroStopAll,
		input.IntentMacroRecordToggle:
		return true
	}
	return false
}

func isMouseIntent(t input.IntentType) bool {
	switch t {
	case input.IntentMouseLeftDown, input.IntentMouseLeftUp,
		input.IntentMouseRightDown, input.IntentMouseRightUp,
		input.IntentMouseDrag, input.IntentMouseWheelMove,
		input.IntentMouseMove:
		return true
	}
	return false
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
	case input.MotionScreenVerticalMid:
		return 'M'
	case input.MotionScreenHorizontalMid:
		return 'm'
	case input.MotionHalfPageLeft:
		return 'H'
	case input.MotionHalfPageRight:
		return 'L'
	case input.MotionHalfPageDown:
		return 'J'
	case input.MotionHalfPageUp:
		return 'K'
	case input.MotionScreenTop:
		return 'g'
	case input.MotionScreenBottom:
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