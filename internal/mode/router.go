package mode

import (
	"slices"
	"time"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/vlog"
)

const undoStackSize = 256

type undoPosition struct {
	x, y int
}

const cmdHistorySize = 256

// MouseModeApplier is terminal-side sink for mouse reporting state
type MouseModeApplier func(enabled, motion bool)

// mouseModeState caches the last applied reporting state
// applied=false forces the first reconcile (lazy init), prevent access before context exists
type mouseModeState struct {
	applied bool
	enabled bool
	motion  bool
}

// KeyReportApplier asks the presentation for key releases from its protocol, and
// from a console keyboard too when console is set; the answer comes to KeyReported
type KeyReportApplier func(console bool)

// keyAskState is the last ask for releases and whether keys were captured since
type keyAskState struct {
	applied  bool
	console  bool
	captured bool
}

// heldInput is a press whose release has not crossed yet, and the slot it pressed from
type heldInput struct {
	key    rune
	source event.InputSource
	slot   uint8
}

// Router interprets Intents and executes game logic
// Authoritative owner of game mode state
type Router struct {
	ctx        *engine.GameContext
	machine    *input.Machine
	configForm *configForm

	macro *MacroManager

	undoRing  [undoStackSize]undoPosition
	undoHead  int // next write index
	undoCount int // valid entry count

	// Input state (find/search repeat)
	lastSearchText  string // Preserved for n/N repeat
	lastFindChar    rune   // Target character for f/F/t/T
	lastFindForward bool   // true for f/t, false for F/T
	lastFindType    rune   // Motion type: 'f', 'F', 't', or 'T'

	// Command history ring buffer
	cmdHistory    [cmdHistorySize]string
	cmdHistHead   int    // next write index
	cmdHistCount  int    // valid entry count
	cmdHistBrowse int    // -1 = live input, 0..count-1 = offset from newest
	cmdHistSaved  string // preserves in-progress input during browsing

	// Mouse hold state for repeat firing
	mouseLeftHeld  bool
	mouseRightHeld bool

	// The last left press that fired main, for the second on its cell that fires special
	leftPressAt   time.Time // [wall]
	leftPressCell [2]int

	// menuHold is a left press held on the status bar: it summons the menu once it
	// has lasted MenuHoldDuration without a release or a drift past MenuHoldSlack
	menuHold   bool
	menuHoldAt [2]int
	menuSince  time.Time // [wall]

	// overlayBar is the overlay's scrollbar thumb a left press holds until its release
	overlayBar tui.ScrollDrag

	// holds are the captured keys and buttons this instance's presses hold down; a
	// button's release goes to the button pressed, wherever the pointer lets go
	holds []heldInput

	// Fire anchors, shared by auto-fire and held-button preventing stacked requests
	lastFireMain time.Time
	lastFireSpec time.Time

	// Terminal mouse reporting, reconciled on the input tick
	applyMouseMode MouseModeApplier
	mouseMode      mouseModeState

	// Key releases, asked for on the input tick: releases is the latest answer
	applyKeyReport KeyReportApplier
	keyAsk         keyAskState
	releases       bool

	// Look-up tables: OpCode → Function
	motionLUT map[input.MotionOp]MotionFunc
	charLUT   map[input.MotionOp]CharMotionFunc
}

// NewRouter creates a router with LUTs initialized
func NewRouter(ctx *engine.GameContext, machine *input.Machine) *Router {
	r := &Router{
		ctx:           ctx,
		machine:       machine,
		macro:         NewMacroManager(),
		cmdHistBrowse: -1,
	}
	machine.SetCapture(&ctx.KeyCapture)

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

// Handle processes an Intent and returns false if game should exit.
// SYNC: caller MUST hold World.updateMutex (App.handleIntent wraps this in
// World.RunSafe); the mutex is non-reentrant, so nothing reachable from here may
// call RunSafe, World.Lock or Positions.Lock. CI guard:
// rg 'RunSafe|World\.Lock|Positions\.Lock' mode/ must return zero hits.
func (r *Router) Handle(intent *input.Intent) bool {
	if intent == nil {
		return true
	}

	if r.ctx.Log.On("input", vlog.LevelDebug) {
		r.ctx.Log.Debug("input", "msg", "intent",
			"type", intent.Type.String(),
			"motion", intent.Motion.String(),
			"operator", intent.Operator.String(),
			"special", intent.Special.String(),
			"mode_target", intent.ModeTarget.String(),
			"scroll", intent.ScrollDir.String(),
			"count", intent.Count,
			"char", int32(intent.Char),
			"x", intent.X,
			"y", intent.Y,
			"cmd", intent.Command,
			"macro", intent.MacroPlayback)
	}

	// Macro reset check (triggered by :new)
	if r.ctx.MacroClearFlag.CompareAndSwap(true, false) {
		r.macro.Reset()
		r.ctx.MacroRecording.Store(false)
		r.ctx.MacroPlaying.Store(false)
	}

	// Pointer admission gate. Dropped input is not an action. The status bar keeps
	// its hold with the mouse disabled and through a hold: a phone has no key for
	// the menu, which is where the mouse is enabled again and a hold ends
	menu := r.menuPointer(intent)
	if isMouseIntent(intent.Type) && (r.inputSuspended() && !(menu && r.heldInPlay()) || r.ctx.MouseDisabled.Load() && !menu) {
		switch intent.Type {
		case input.IntentMouseLeftDown, input.IntentMouseLeftUp:
			r.mouseLeftHeld, r.menuHold = false, false
			r.overlayBar.Release()
			r.releasePointer(false, intent.Type == input.IntentMouseLeftDown)
		case input.IntentMouseRightDown, input.IntentMouseRightUp:
			r.mouseRightHeld = false
			r.releasePointer(true, intent.Type == input.IntentMouseRightDown)
		}
		return true
	}

	if r.ctx.TimeCtl.Held() && !r.heldAdmits(intent) {
		return true
	}

	if r.configForm != nil && configMenu(r.ctx) != nil && r.handleConfigForm(intent) {
		return true
	}

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

	// A recording keeps what playback can replay: not macro control, not playback's
	// own intents, and nothing that enters or drives a mode playback waits in.
	if r.macro.IsRecording() && !isMacroControlIntent(intent.Type) && !intent.MacroPlayback &&
		!r.suspendsPlayback(intent) {
		r.macro.Record(*intent)
	}

	switch intent.Type {
	// System
	case input.IntentQuit:
		return false
	case input.IntentEscape:
		return r.handleEscape()
	case input.IntentToggleAudioCycle:
		return r.handleToggleAudioCycle()
	case input.IntentConfigMenu:
		return r.handleConfigMenu()
	case input.IntentHelp:
		return r.handleHelp()

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
	case input.IntentKeyPress:
		return r.handleKeyPress(intent)
	case input.IntentKeyRelease:
		return r.handleKeyRelease(intent)

	// Mode switching
	case input.IntentModeSwitch:
		return r.handleModeSwitch(intent)
	case input.IntentToggleAutoFire:
		return handleAutoCommand(r.ctx, nil).Continue

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
	case input.IntentMacroPlay:
		return r.handleMacroPlay(intent)
	case input.IntentMacroPlayInfinite:
		return r.handleMacroPlayInfinite(intent)
	case input.IntentMacroPlayAll:
		return r.handleMacroPlayAll()
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
	case input.IntentOverlayFilter:
		return r.handleOverlayFilter()
	case input.IntentOverlayPoint:
		return r.handleOverlayPoint(intent)
	case input.IntentOverlayDrag:
		return r.handleOverlayDrag(intent)
	case input.IntentOverlayWheel:
		return r.handleOverlayWheel(intent)

	// Mouse
	case input.IntentMouseLeftDown:
		return r.handleMouseLeftDown(intent)
	case input.IntentMouseLeftUp:
		return r.handleMouseLeftUp()
	case input.IntentMouseRightDown:
		return r.handleMouseRightDown(intent)
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
		r.ctx.SetCommandCursorPos(0)
		r.resetCommandHistoryBrowse()
		r.ctx.SetPaused(false)
	case core.ModeOverlay:
		if configMenu(r.ctx) != nil {
			return r.configMenuBack()
		}
		if r.ctx.IsOverlayFilterEditing() {
			r.endOverlayFilter()
			r.setOverlayFilter("")
			return true
		}
		r.ctx.SetPaused(false)
	case core.ModeNormal:
		// ESC in Normal mode triggers ping grid, no mode change
		r.ctx.PushLocal(event.EventPingGridRequest, &event.PingGridRequestPayload{
			Entity:   r.ctx.World.Resources.Player.Entity,
			Duration: parameter.PingGridDuration,
		})
		return true
	}

	// Return to Normal mode via centralized transition
	r.transitionMode(core.ModeNormal)

	return true
}

// handleToggleAudioCycle asks AudioSystem to advance parameter.AudioMaskCycle. The
// mute is this machine's, so it is neither journaled nor counted as an action.
func (r *Router) handleToggleAudioCycle() bool {
	r.ctx.PushLocalOrigin(event.EventAudioMuteToggle, nil, event.OriginDevice)
	return true
}

// --- Motion Handlers ---

// applyOperator hands a resolved range to the intent's operator. One arm today;
// this is where a second operator attaches, and it is why the three call sites do
// not each carry their own switch.
func (r *Router) applyOperator(intent *input.Intent, result MotionResult) {
	switch intent.Operator {
	case input.OperatorDelete:
		OpDelete(r.ctx, result)
	}
}

// rememberFind stores what ';' and ',' repeat. Both the plain and the operator
// char-motion handler need it and neither owns it.
func (r *Router) rememberFind(intent *input.Intent, result MotionResult) {
	if !result.Valid {
		return
	}
	r.lastFindChar = intent.Char
	r.lastFindType = motionOpToRune(intent.Motion)
	r.lastFindForward = intent.Motion == input.MotionFindForward ||
		intent.Motion == input.MotionTillForward
}

// charCells is the inclusive character range from (x,y) to (endX,y).
func charCells(x, y, endX int) MotionResult {
	return MotionResult{
		StartX: x, StartY: y,
		EndX: endX, EndY: y,
		Type: RangeChar, Style: StyleInclusive,
		Valid: true,
	}
}

func (r *Router) handleMotion(intent *input.Intent) bool {
	motionFn, ok := r.motionLUT[intent.Motion]
	if !ok {
		return true
	}

	r.captureForUndo()

	if pos, ok := r.ctx.World.LocalCursor(); ok {
		OpMove(r.ctx, motionFn(r.ctx, pos.X, pos.Y, intent.Count))
	}
	return true
}

func (r *Router) handleCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	r.captureForUndo()

	if pos, ok := r.ctx.World.LocalCursor(); ok {
		result := charFn(r.ctx, pos.X, pos.Y, intent.Count, intent.Char)
		OpMove(r.ctx, result)
		r.rememberFind(intent, result)
	}
	return true
}

func (r *Router) handleMotionMarkerShow(intent *input.Intent) bool {
	// Emit event for MotionMarkerSystem to show colored markers
	dir := r.motionToDirection(intent.Motion)
	r.ctx.PushLocal(event.EventMotionMarkerShowColored, &event.MotionMarkerShowPayload{
		DirectionX: dir[0],
		DirectionY: dir[1],
	})
	return true
}

func (r *Router) handleMotionMarkerJump(intent *input.Intent) bool {
	// Clear colored markers
	r.ctx.PushLocal(event.EventMotionMarkerClearColored, nil)

	r.captureForUndo()

	if pos, ok := r.ctx.World.LocalCursor(); ok {
		var glyphType component.GlyphType = -1 // -1 = any
		switch intent.Char {
		case 'r':
			glyphType = component.GlyphRed
		case 'g':
			glyphType = component.GlyphGreen
		case 'b':
			glyphType = component.GlyphBlue
		}

		OpMove(r.ctx, MotionColoredGlyph(r.ctx, pos.X, pos.Y, intent.Count, intent.Motion, glyphType))
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

	if pos, ok := r.ctx.World.LocalCursor(); ok {
		r.applyOperator(intent, motionFn(r.ctx, pos.X, pos.Y, intent.Count))
	}
	return true
}

func (r *Router) handleOperatorLine(intent *input.Intent) bool {
	if pos, ok := r.ctx.World.LocalCursor(); ok {
		cfg := r.ctx.World.Resources.Config
		r.applyOperator(intent, MotionResult{
			StartX: 0, StartY: pos.Y,
			EndX: cfg.MapWidth - 1, EndY: min(pos.Y+intent.Count-1, cfg.MapHeight-1),
			Type: RangeLine, Style: StyleInclusive,
			Valid: true,
		})
	}
	return true
}

func (r *Router) handleOperatorCharMotion(intent *input.Intent) bool {
	charFn, ok := r.charLUT[intent.Motion]
	if !ok {
		return true
	}

	if pos, ok := r.ctx.World.LocalCursor(); ok {
		result := charFn(r.ctx, pos.X, pos.Y, intent.Count, intent.Char)
		r.applyOperator(intent, result)
		r.rememberFind(intent, result)
	}
	return true
}

// --- Special Command Handlers ---

func (r *Router) handleSpecial(intent *input.Intent) bool {
	if pos, ok := r.ctx.World.LocalCursor(); ok {
		switch intent.Special {
		case input.SpecialDeleteChar:
			// x = delete chars forward
			OpDelete(r.ctx, charCells(pos.X, pos.Y,
				min(pos.X+intent.Count-1, r.ctx.World.Resources.Config.MapWidth-1)))

		case input.SpecialDeleteToEnd:
			// D = d$
			OpDelete(r.ctx, MotionLineEnd(r.ctx, pos.X, pos.Y, 1))

		case input.SpecialSearchNext:
			RepeatSearch(r.ctx, r.lastSearchText, true)

		case input.SpecialSearchPrev:
			RepeatSearch(r.ctx, r.lastSearchText, false)

		case input.SpecialRepeatFind:
			r.executeRepeatFind(false)

		case input.SpecialRepeatFindRev:
			r.executeRepeatFind(true)
		}
	}
	return true
}

func (r *Router) handleNuggetJump() bool {
	r.captureForUndo()
	r.ctx.PushLocal(event.EventNuggetJumpRequest, &event.NuggetJumpRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	return true
}

func (r *Router) handleGoldJump() bool {
	r.captureForUndo()
	r.ctx.PushCrossing(event.EventGoldJumpRequest, &event.GoldJumpRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	return true
}

func (r *Router) handleFireMain() bool {
	r.ctx.PushLocal(event.EventWeaponFireRequest, &event.WeaponFireRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	return true
}

func (r *Router) handleFireSpecial() bool {
	r.ctx.PushLocal(event.EventFireSpecialRequest, &event.FireSpecialRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	return true
}

// handleKeyPress crosses a captured key: the scenario reading it is shared
func (r *Router) handleKeyPress(intent *input.Intent) bool {
	r.pressInput(intent, intent.Char, event.InputKey)
	return true
}

// handleKeyRelease crosses the release of a captured key this instance holds; a
// macro's is none, as its presses are none
func (r *Router) handleKeyRelease(intent *input.Intent) bool {
	if !intent.MacroPlayback {
		r.release(intent.Char, event.InputKey, intent.Lost)
	}
	return true
}

// pressInput crosses a press of a captured key, typed or clicked on its button, from
// this instance's roster slot, and holds it until its release. The capture and the
// modes the parser captures in gate every source, bots and scripts included. A
// macro replays none: a recorded round is the wrong answer to the next.
func (r *Router) pressInput(intent *input.Intent, key rune, source event.InputSource) {
	playing := r.ctx.IsNormalMode() || r.ctx.IsVisualMode() || r.ctx.IsInsertMode()
	seated := int(r.ctx.World.Resources.Player.LocalSlot()) < parameter.MaxPlayers
	if intent.MacroPlayback || !playing || !seated || r.inputSuspended() || !r.ctx.KeyCapture.Has(key) {
		return
	}
	r.release(key, source, true) // a hold whose release never came ends as the key presses again
	in := r.input(key, source)
	r.ctx.World.PushInputPress(in)
	r.holds = append(r.holds, heldInput{key: key, source: source, slot: in.Slot})
	// A keyboard reporting presses alone taps: each press lets go as it is made
	if source == event.InputKey && !r.releases {
		r.release(key, source, true)
	}
}

// releasePointer crosses the release of the button a left or right pointer press
// holds, lost when that pointer button never let go of it
func (r *Router) releasePointer(right, lost bool) {
	i := slices.IndexFunc(r.holds, func(h heldInput) bool {
		if right {
			return h.source == event.InputPointerRight
		}
		return h.source == event.InputPointer || h.source == event.InputTouch
	})
	if i >= 0 {
		r.release(r.holds[i].key, r.holds[i].source, lost)
	}
}

// buttonUnder is the key of the button under a pointer intent's cell, zero for none
func (r *Router) buttonUnder(intent *input.Intent) rune {
	if x, y, ok := r.pointerCell(intent); ok {
		return buttonAt(r.ctx, x, y)
	}
	return 0
}

// release crosses the release of the hold of key from source, from the slot that
// pressed it; lost when no player let go of it
func (r *Router) release(key rune, source event.InputSource, lost bool) {
	i := slices.IndexFunc(r.holds, func(h heldInput) bool { return h.key == key && h.source == source })
	if i < 0 {
		return
	}
	h := r.holds[i]
	r.holds = slices.Delete(r.holds, i, i+1)
	in := r.input(key, source)
	in.Slot, in.Lost = h.slot, lost
	r.ctx.World.PushInputRelease(in)
}

// endHolds ends, as lost, each hold play no longer allows: one whose key the capture
// gave back, and every one once play stops for a mode that captures nothing or input
// is suspended
func (r *Router) endHolds() {
	playing := (r.ctx.IsNormalMode() || r.ctx.IsVisualMode() || r.ctx.IsInsertMode()) && !r.inputSuspended()
	for i := len(r.holds) - 1; i >= 0; i-- {
		if h := r.holds[i]; !playing || !r.ctx.KeyCapture.Has(h.key) {
			r.release(h.key, h.source, true)
		}
	}
}

// input is an input of key made now from this instance's roster slot
func (r *Router) input(key rune, source event.InputSource) *event.InputPayload {
	world := r.ctx.World
	return &event.InputPayload{
		Slot:   world.Resources.Player.LocalSlot(),
		Key:    string(key),
		Source: source,
		Tick:   world.Resources.Game.State.GetGameTicks(),
	}
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
		r.ctx.SetCommandCursorPos(0)
		r.resetCommandHistoryBrowse()
		r.ctx.PauseForInput()
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

// transitionMode handles all mode changes with consistent side-effects
func (r *Router) transitionMode(newMode core.GameMode) {
	// Update game mode, ping bounds, emit event
	r.ctx.RequestMode(newMode)

	// Sync input machine
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

// openBreak puts the command line over a run a breakpoint paused, so the operator
// inspects it there and leaving the line resumes it. An overlay keeps the screen.
func (r *Router) openBreak() {
	if !r.ctx.TimeCtl.TakeHit() {
		return
	}
	if m := r.ctx.GetMode(); m == core.ModeCommand || m == core.ModeOverlay {
		return
	}
	r.ctx.SetCommandText("")
	r.ctx.SetCommandCursorPos(0)
	r.resetCommandHistoryBrowse()
	r.transitionMode(core.ModeCommand)
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
	case core.ModeOverlay:
		r.setOverlayFilter(r.ctx.OverlayFilter() + string(intent.Char))
	}

	return true
}

func (r *Router) handleInsertChar(char rune) {
	var posX, posY int
	if pos, ok := r.ctx.World.LocalCursor(); ok {
		posX, posY = pos.X, pos.Y
	}

	payload := event.CharacterTypedPayloadPool.Get().(*event.CharacterTypedPayload)
	payload.Entity = r.ctx.World.Resources.Player.Entity
	payload.Char = char
	payload.X = posX
	payload.Y = posY
	r.ctx.PushLocal(event.EventCharacterTyped, payload)
}

func (r *Router) handleSearchChar(char rune) {
	searchText := r.ctx.GetSearchText()
	r.ctx.SetSearchText(searchText + string(char))
}

func (r *Router) handleCommandChar(char rune) {
	text := []rune(r.ctx.GetCommandText())
	pos := min(r.ctx.GetCommandCursorPos(), len(text))

	newText := make([]rune, 0, len(text)+1)
	newText = append(newText, text[:pos]...)
	newText = append(newText, char)
	newText = append(newText, text[pos:]...)

	r.ctx.SetCommandText(string(newText))
	r.ctx.SetCommandCursorPos(pos + 1)
}

func (r *Router) handleTextBackspace() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		if q := []rune(r.ctx.GetSearchText()); len(q) > 0 {
			r.ctx.SetSearchText(string(q[:len(q)-1]))
		}
	case core.ModeCommand:
		text := []rune(r.ctx.GetCommandText())
		pos := r.ctx.GetCommandCursorPos()
		if pos > 0 && pos <= len(text) {
			newText := append(text[:pos-1], text[pos:]...)
			r.ctx.SetCommandText(string(newText))
			r.ctx.SetCommandCursorPos(pos - 1)
		}
	case core.ModeInsert:
		return r.handleInsertDeleteBack()
	case core.ModeOverlay:
		if q := []rune(r.ctx.OverlayFilter()); len(q) > 0 {
			r.setOverlayFilter(string(q[:len(q)-1]))
		}
	}

	return true
}

func (r *Router) handleTextConfirm() bool {
	currentMode := r.ctx.GetMode()

	switch currentMode {
	case core.ModeSearch:
		searchText := r.ctx.GetSearchText()
		if searchText != "" {
			if PerformSearch(r.ctx, searchText, true) {
				r.lastSearchText = searchText
			}
		}
		r.ctx.SetSearchText("")
		r.transitionMode(core.ModeNormal)

	case core.ModeCommand:
		commandText := r.ctx.GetCommandText()

		// Push to history before execution
		r.pushCommandHistory(commandText)
		r.resetCommandHistoryBrowse()

		// Unpause ahead of execution: the command's own events are queued
		// before this intent's dispatch, so a trailing unpause is applied too
		// late and every EventSoundRequest the command produces is gated. A
		// command that wants the pause requests it again from its own handler.
		r.ctx.SetPaused(false)

		var result CommandResult
		r.ctx.WithOrigin(event.OriginCommand, func() {
			result = ExecuteCommand(r.ctx, commandText)
		})

		r.ctx.SetCommandText("")
		r.ctx.SetCommandCursorPos(0)

		if r.ctx.GetMode() != core.ModeOverlay {
			r.transitionMode(core.ModeNormal)
		} else {
			r.machine.SetMode(input.ModeOverlay)
		}

		return result.Continue

	case core.ModeOverlay:
		r.endOverlayFilter()
	}

	return true
}

func (r *Router) handleTextNav(intent *input.Intent) bool {
	// Command mode: cursor movement + history navigation
	if r.ctx.GetMode() == core.ModeCommand {
		text := r.ctx.GetCommandText()
		pos := r.ctx.GetCommandCursorPos()
		textLen := len([]rune(text))

		switch intent.Motion {
		case input.MotionLeft:
			if pos > 0 {
				r.ctx.SetCommandCursorPos(pos - 1)
			}
		case input.MotionRight:
			if pos < textLen {
				r.ctx.SetCommandCursorPos(pos + 1)
			}
		case input.MotionLineStart:
			r.ctx.SetCommandCursorPos(0)
		case input.MotionLineEnd:
			r.ctx.SetCommandCursorPos(textLen)
		case input.MotionUp:
			r.commandHistoryUp()
		case input.MotionDown:
			r.commandHistoryDown()
		}
		return true
	}

	// Navigation in Insert mode moves cursor
	if r.ctx.GetMode() == core.ModeInsert {
		motionFn, ok := r.motionLUT[intent.Motion]
		if !ok {
			return true
		}

		r.captureForUndo()

		if pos, ok := r.ctx.World.LocalCursor(); ok {
			OpMove(r.ctx, motionFn(r.ctx, pos.X, pos.Y, intent.Count))
		}
	}

	return true
}

func (r *Router) handleInsertDeleteCurrent() bool {
	if pos, ok := r.ctx.World.LocalCursor(); ok {
		OpDelete(r.ctx, charCells(pos.X, pos.Y, pos.X))
	}
	return true
}

func (r *Router) handleInsertDeleteForward() bool {
	pos, ok := r.ctx.World.LocalCursor()
	if !ok {
		return true
	}
	OpDelete(r.ctx, charCells(pos.X, pos.Y, pos.X))
	OpMove(r.ctx, MotionRight(r.ctx, pos.X, pos.Y, 1))
	return true
}

func (r *Router) handleInsertDeleteBack() bool {
	pos, ok := r.ctx.World.LocalCursor()
	if !ok || pos.X == 0 {
		return true
	}
	OpDelete(r.ctx, charCells(pos.X-1, pos.Y, pos.X-1))
	OpMove(r.ctx, MotionLeft(r.ctx, pos.X, pos.Y, 1))
	return true
}

// --- Undo ---

// captureForUndo records current cursor position before movement
func (r *Router) captureForUndo() {
	pos, ok := r.ctx.World.LocalCursor()
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

	n := min(max(intent.Count, 1), r.undoCount)

	var x, y int
	for range n {
		r.undoHead = (r.undoHead - 1 + undoStackSize) % undoStackSize
		r.undoCount--
		x, y = r.undoRing[r.undoHead].x, r.undoRing[r.undoHead].y
	}

	OpJump(r.ctx, x, y)
	return true
}

// --- Command History ---

// pushCommandHistory appends a command to the history ring buffer
// Deduplicates against most recent entry
func (r *Router) pushCommandHistory(cmd string) {
	if cmd == "" {
		return
	}
	if r.cmdHistCount > 0 {
		prev := (r.cmdHistHead - 1 + cmdHistorySize) % cmdHistorySize
		if r.cmdHistory[prev] == cmd {
			return
		}
	}
	r.cmdHistory[r.cmdHistHead] = cmd
	r.cmdHistHead = (r.cmdHistHead + 1) % cmdHistorySize
	if r.cmdHistCount < cmdHistorySize {
		r.cmdHistCount++
	}
}

// commandHistoryUp navigates to older history entry
func (r *Router) commandHistoryUp() {
	if r.cmdHistCount == 0 {
		return
	}
	next := r.cmdHistBrowse + 1
	if next >= r.cmdHistCount {
		return
	}
	if r.cmdHistBrowse == -1 {
		r.cmdHistSaved = r.ctx.GetCommandText()
	}
	r.cmdHistBrowse = next
	ringIdx := (r.cmdHistHead - 1 - next + cmdHistorySize) % cmdHistorySize
	text := r.cmdHistory[ringIdx]
	r.ctx.SetCommandText(text)
	r.ctx.SetCommandCursorPos(len([]rune(text)))
}

// commandHistoryDown navigates to newer history entry or restores live input
func (r *Router) commandHistoryDown() {
	if r.cmdHistBrowse < 0 {
		return
	}
	r.cmdHistBrowse--
	if r.cmdHistBrowse < 0 {
		r.ctx.SetCommandText(r.cmdHistSaved)
		r.ctx.SetCommandCursorPos(len([]rune(r.cmdHistSaved)))
		r.cmdHistSaved = ""
		return
	}
	ringIdx := (r.cmdHistHead - 1 - r.cmdHistBrowse + cmdHistorySize) % cmdHistorySize
	text := r.cmdHistory[ringIdx]
	r.ctx.SetCommandText(text)
	r.ctx.SetCommandCursorPos(len([]rune(text)))
}

// resetCommandHistoryBrowse exits history browsing mode
func (r *Router) resetCommandHistoryBrowse() {
	r.cmdHistBrowse = -1
	r.cmdHistSaved = ""
}

// --- Overlay Handlers ---

func (r *Router) handleOverlayClose() bool {
	if configMenu(r.ctx) != nil {
		return r.configMenuBack()
	}
	return r.closeOverlay()
}

func (r *Router) closeOverlay() bool {
	r.configForm = nil
	r.overlayBar.Release()
	r.ctx.SetOverlayContent(nil)
	r.ctx.SetPaused(false)
	r.transitionMode(core.ModeNormal)
	return true
}

// handleHelp opens help over any mode or overlay, and closes it where it is shown
func (r *Router) handleHelp() bool {
	if r.ctx.GetOverlayTitle() == core.OverlayHelpTitle {
		return r.closeOverlay()
	}
	r.enterOverlay()
	handleHelpCommand(r.ctx)
	return true
}

// enterOverlay leaves the mode a key opens an overlay over: pointer holds, the
// command and search lines and a menu form are dropped, and keys parse as the overlay's
func (r *Router) enterOverlay() {
	r.configForm = nil
	r.mouseLeftHeld, r.mouseRightHeld = false, false
	r.overlayBar.Release()
	r.ctx.SetCommandText("")
	r.ctx.SetSearchText("")
	r.ctx.SetCommandCursorPos(0)
	r.resetCommandHistoryBrowse()
	r.machine.SetMode(input.ModeOverlay)
}

// --- Mouse ---

func (r *Router) handleMouseLeftDown(intent *input.Intent) bool {
	r.releasePointer(false, true)
	// The status bar is no map cell: a press there starts the hold that summons the
	// menu, and neither moves the cursor nor fires
	if r.onStatusBar(intent) {
		r.menuHold, r.menuHoldAt, r.menuSince = true, [2]int{intent.X, intent.Y}, time.Now() // [wall]
		return true
	}
	// A press on a button is its key's input whether or not that key is taken now, so
	// it never moves the cursor onto the button or fires
	if key := r.buttonUnder(intent); key != 0 {
		source := event.InputPointer
		if intent.Touch {
			source = event.InputTouch
		}
		r.pressInput(intent, key, source)
		return true
	}
	r.moveMouseCursor(intent)
	r.ctx.PushLocal(event.EventWeaponFireRequest, &event.WeaponFireRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	r.mouseLeftHeld = true
	r.lastFireMain = r.ctx.TimeCtl.Now()
	// A second press near the first within the window is the right button a finger
	// has not; both presses fire main, and a third starts over
	now, cell := time.Now(), [2]int{intent.X, intent.Y} // [wall]
	slack := max(1, r.ctx.Width*parameter.MouseDoubleClickSlackPercent/100)
	near := max(cell[0]-r.leftPressCell[0], r.leftPressCell[0]-cell[0]) <= slack &&
		max(cell[1]-r.leftPressCell[1], r.leftPressCell[1]-cell[1]) <= slack
	if near && now.Sub(r.leftPressAt) <= parameter.MouseDoubleClickWindow {
		r.leftPressAt = time.Time{}
		r.fireSpecialHeld()
		r.mouseRightHeld = false
	} else {
		r.leftPressAt, r.leftPressCell = now, cell
	}
	return true
}

func (r *Router) handleMouseLeftUp() bool {
	r.mouseLeftHeld, r.menuHold = false, false
	r.overlayBar.Release()
	r.releasePointer(false, false)
	return true
}

// handleMouseRightDown presses a button's key as the right button, as the left does,
// and anywhere else fires special
func (r *Router) handleMouseRightDown(intent *input.Intent) bool {
	r.releasePointer(true, true)
	if key := r.buttonUnder(intent); key != 0 {
		r.pressInput(intent, key, event.InputPointerRight)
		return true
	}
	return r.fireSpecialHeld()
}

// fireSpecialHeld fires special at the cursor without moving it and holds it to repeat
func (r *Router) fireSpecialHeld() bool {
	r.ctx.PushLocal(event.EventFireSpecialRequest, &event.FireSpecialRequestPayload{
		Entity: r.ctx.World.Resources.Player.Entity,
	})
	r.mouseRightHeld = true
	r.lastFireSpec = r.ctx.TimeCtl.Now()
	return true
}

func (r *Router) handleMouseRightUp() bool {
	r.mouseRightHeld = false
	r.releasePointer(true, false)
	return true
}

func (r *Router) handleMouseDrag(intent *input.Intent) bool {
	if r.menuHold {
		dx, dy := intent.X-r.menuHoldAt[0], intent.Y-r.menuHoldAt[1]
		r.menuHold = max(dx, -dx) <= parameter.MenuHoldSlack && max(dy, -dy) <= parameter.MenuHoldSlack
	}
	if r.mouseLeftHeld {
		r.moveMouseCursor(intent)
	}
	return true
}

// onStatusBar reports whether a pointer intent names a cell of the rows below the
// viewport, where the status bar is drawn and no map cell can be
func (r *Router) onStatusBar(intent *input.Intent) bool {
	return !intent.MapCell && intent.Y >= r.ctx.GameYOffset+r.ctx.World.Resources.Config.ViewportHeight
}

// menuPointer is a press that may open the menu, or the release that ends one
func (r *Router) menuPointer(intent *input.Intent) bool {
	return intent.Type == input.IntentMouseLeftUp || intent.Type == input.IntentMouseLeftDown && r.onStatusBar(intent)
}

// handleMouseWheelMove moves the cursor, or over a button presses its key for one
// notch, released as it presses since a wheel has no release
func (r *Router) handleMouseWheelMove(intent *input.Intent) bool {
	if key := r.buttonUnder(intent); key != 0 {
		source := event.InputWheelUp
		if intent.ScrollDir == input.ScrollDown {
			source = event.InputWheelDown
		}
		r.pressInput(intent, key, source)
		r.release(key, source, true)
		return true
	}
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

// inputSuspended reports whether machine-driven and pointer input must be withheld
func (r *Router) inputSuspended() bool {
	return r.ctx.TimeCtl.IsPaused() || r.ctx.IsCommandMode() || r.ctx.IsOverlayMode()
}

// heldInPlay reports a hold over play rather than over the command line or a menu
func (r *Router) heldInPlay() bool {
	return r.ctx.TimeCtl.Held() && !r.ctx.IsCommandMode() && !r.ctx.IsOverlayMode()
}

// heldAdmits reports whether an intent acts during a hold, which takes no play: the
// command line and the overlays take everything, and play only what opens them,
// quits, mutes, lets go of a key, or the status bar press the pointer gate kept.
func (r *Router) heldAdmits(intent *input.Intent) bool {
	if !r.heldInPlay() || isMouseIntent(intent.Type) {
		return true
	}
	switch intent.Type {
	case input.IntentQuit, input.IntentEscape, input.IntentToggleAudioCycle, input.IntentConfigMenu,
		input.IntentHelp, input.IntentKeyRelease:
		return true
	case input.IntentModeSwitch:
		return intent.ModeTarget == input.ModeTargetCommand
	}
	return false
}

// TakeStart starts a waiting game at its player's press, which does nothing else: a
// key the held game would ignore, nil when the keymap binds none or waits for more,
// or a pointer press off the status bar, whose press opens the menu. Whatever a held
// game takes keeps its meaning; false leaves the press to Handle.
func (r *Router) TakeStart(intent *input.Intent) bool {
	if !r.ctx.TimeCtl.Holds(engine.HoldStart) || !r.heldInPlay() || holdRefused(r.ctx, false) != "" {
		return false
	}
	switch {
	case intent == nil:
	case isMouseIntent(intent.Type):
		if r.ctx.MouseDisabled.Load() || r.onStatusBar(intent) {
			return false
		}
	case r.heldAdmits(intent):
		return false
	}
	r.machine.Reset()
	r.ctx.PushLocal(event.EventGameHoldRequest, &event.GamePausePayload{Paused: false})
	return true
}

// SetMouseModeApplier installs the terminal reporting sink. Optional: a nil
func (r *Router) SetMouseModeApplier(fn MouseModeApplier) {
	r.applyMouseMode = fn
}

// SetKeyReportApplier installs the sink for key release asks: the presentation's,
// or a bot's, which lets go of each key it presses. Without one, keys report
// presses alone.
func (r *Router) SetKeyReportApplier(fn KeyReportApplier) {
	r.applyKeyReport = fn
}

// syncKeyReport asks for releases from the first input tick on, a console keyboard's
// only while the capture asks for them, since a raw console costs some of its own
// keys and its mouse. Keys coming to be captured cross this slot's report; an ask
// crosses its answer.
func (r *Router) syncKeyReport() {
	captured, console := r.ctx.KeyCapture.Any(), r.ctx.KeyCapture.Releases()
	started := captured && !r.keyAsk.captured
	r.keyAsk.captured = captured
	if r.keyAsk.applied && r.keyAsk.console == console {
		if started {
			r.crossReport()
		}
		return
	}
	r.keyAsk.applied, r.keyAsk.console = true, console
	if r.applyKeyReport == nil {
		r.KeyReported(false)
		return
	}
	r.applyKeyReport(console)
}

// KeyReported takes the answer to an ask: whether keys release now.
// While keys are captured, it crosses for this instance's slot.
func (r *Router) KeyReported(releases bool) {
	r.releases = releases
	if r.keyAsk.captured {
		r.crossReport()
	}
}

// crossReport tells every instance whether this slot's keys release or tap
func (r *Router) crossReport() {
	slot := r.ctx.World.Resources.Player.LocalSlot()
	if int(slot) >= parameter.MaxPlayers {
		return
	}
	r.ctx.World.PushCrossing(event.EventInputReport, &event.InputReportPayload{Slot: slot, Releases: r.releases})
}

// syncMouseMode pushes reporting state to the host when it diverges from the last applied value
// First call always applies — post context availability lazy init
// Runs regardless of pause: reporting tracks the flags even while the simulation is frozen
func (r *Router) syncMouseMode() {
	if r.applyMouseMode == nil {
		return
	}
	// An overlay takes the pointer whatever the setting: a tap is all a phone has
	enabled := !r.ctx.MouseDisabled.Load() || r.ctx.IsOverlayMode()
	motion := enabled && r.ctx.MouseFreeMode.Load()

	if r.mouseMode.applied && r.mouseMode.enabled == enabled && r.mouseMode.motion == motion {
		return
	}
	// Reporting off, no release can follow a press held now
	if !enabled {
		r.mouseLeftHeld, r.mouseRightHeld = false, false
		r.releasePointer(false, true)
		r.releasePointer(true, true)
	}
	r.mouseMode = mouseModeState{applied: true, enabled: enabled, motion: motion}
	r.applyMouseMode(enabled, motion)
}

// ProcessInputTick checks mouse reports and key release asks, ends the holds play no
// longer allows, summons the menu a hold on the status bar has earned, and emits
// repeat fire requests for auto-fire and held buttons
func (r *Router) ProcessInputTick() bool {
	r.syncMouseMode()
	r.syncKeyReport()
	r.endHolds()
	r.openBreak()

	if r.menuHold && time.Since(r.menuSince) >= parameter.MenuHoldDuration { // [wall]
		r.menuHold = false
		r.handleConfigMenu()
		return false
	}
	if r.inputSuspended() {
		return false
	}

	now := r.ctx.TimeCtl.Now()
	auto := r.ctx.AutoFire.Load()
	mouse := !r.ctx.MouseDisabled.Load()
	player := r.ctx.World.Resources.Player.Entity

	emitted := false
	if o, due := r.fireDue(now, &r.lastFireMain, auto != engine.AutoFireOff, mouse && r.mouseLeftHeld); due {
		r.ctx.PushEventFull(event.EventWeaponFireRequest,
			&event.WeaponFireRequestPayload{Entity: player}, o, core.DomainPlayer)
		emitted = true
	}
	if o, due := r.fireDue(now, &r.lastFireSpec, auto == engine.AutoFireBoth, mouse && r.mouseRightHeld); due {
		r.ctx.PushEventFull(event.EventFireSpecialRequest,
			&event.FireSpecialRequestPayload{Entity: player}, o, core.DomainPlayer)
		emitted = true
	}
	return emitted
}

// fireDue reports whether a repeat request is due for a weapon slot and advances
// its anchor. Auto-fire and held-button repeat carry independent intervals; when
// both are due the held button wins, since a held button is input and auto-fire
// replays as a macro.
func (r *Router) fireDue(now time.Time, last *time.Time, auto, held bool) (event.Origin, bool) {
	elapsed := now.Sub(*last)
	switch {
	case held && elapsed >= parameter.MouseRepeatInterval:
		*last = now
		return event.OriginInput, true
	case auto && elapsed >= parameter.AutoFireInterval:
		*last = now
		return event.OriginMacro, true
	}
	return event.OriginSystem, false
}

// moveMouseCursor moves the cursor to the pointer's cell, reporting whether it may.
// A map cell counts only where the viewport shows it, which is all a mouse reaches;
// a terminal cell goes through ViewportToMap, which undoes the centring of a map
// smaller than the viewport so the cell under the pointer is the one jumped to.
// pointerCell is the shown map cell a pointer intent names: a terminal cell, or a map
// cell from a bot or script
func (r *Router) pointerCell(intent *input.Intent) (x, y int, ok bool) {
	config := r.ctx.World.Resources.Config
	if intent.MapCell {
		_, _, ok = config.MapToViewport(intent.X, intent.Y)
		return intent.X, intent.Y, ok
	}
	return config.ViewportToMap(intent.X-r.ctx.GameXOffset, intent.Y-r.ctx.GameYOffset)
}

func (r *Router) moveMouseCursor(intent *input.Intent) bool {
	gameX, gameY, ok := r.pointerCell(intent)
	if !ok {
		return false
	}

	player := r.ctx.World.Resources.Player.Entity

	// A report on the cell the cursor is already bound for is no move and no action.
	// That cell is the D-18 prediction: the store lags it by a settle or a playout
	// lead, so testing the store re-sent every repeated report as a new move.
	if cur, ok := r.ctx.World.CursorCell(player); ok && cur.X == gameX && cur.Y == gameY {
		return true
	}

	// Block check
	if isCursorBlocked(r.ctx, gameX, gameY) {
		return false
	}

	r.ctx.World.PushPointerMove(player, gameX, gameY)
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

func (r *Router) handleMacroPlay(intent *input.Intent) bool {
	now := r.ctx.TimeCtl.Now()
	if r.macro.StartPlayback(intent.Char, intent.Count, now) {
		r.ctx.MacroPlaying.Store(true)
	}
	return true
}

func (r *Router) handleMacroPlayInfinite(intent *input.Intent) bool {
	now := r.ctx.TimeCtl.Now()
	if r.macro.StartPlayback(intent.Char, 0, now) { // 0 = infinite
		r.ctx.MacroPlaying.Store(true)
	}
	return true
}

func (r *Router) handleMacroPlayAll() bool {
	now := r.ctx.TimeCtl.Now()
	if r.macro.StartAllPlayback(now) > 0 {
		r.ctx.MacroPlaying.Store(true)
	}
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
	if r.inputSuspended() {
		return nil
	}
	now := r.ctx.TimeCtl.Now()
	intents := r.macro.Tick(now)
	r.updateMacroPlayingState()
	return intents
}

// --- Helper Methods ---

func isMacroControlIntent(t input.IntentType) bool {
	switch t {
	case input.IntentMacroRecordStart, input.IntentMacroPlay,
		input.IntentMacroPlayInfinite, input.IntentMacroStopAll,
		input.IntentMacroRecordToggle:
		return true
	}
	return false
}

// suspendsPlayback reports an intent that enters or drives Command or Overlay mode.
// Playback waits in both, so a recording that held one would stall at it: a macro
// replays Normal, Insert, Visual and Search mode actions only.
func (r *Router) suspendsPlayback(intent *input.Intent) bool {
	if r.ctx.IsCommandMode() || r.ctx.IsOverlayMode() {
		return true
	}
	switch intent.Type {
	case input.IntentConfigMenu, input.IntentHelp, input.IntentOverlayPoint, input.IntentOverlayDrag, input.IntentOverlayWheel:
		return true
	}
	return intent.Type == input.IntentModeSwitch && intent.ModeTarget == input.ModeTargetCommand
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
