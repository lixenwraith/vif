package renderer

import (
	"fmt"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx *engine.GameContext

	// Color mode (persist throughout runtime)
	colorMode terminal.ColorMode

	// Cached metric pointers (zero-lock reads)
	statFPS        *atomic.Int64
	statAPM        *atomic.Int64
	statTicks      *atomic.Int64
	statPhase      *atomic.Int64
	statDecayTimer *atomic.Int64

	// FSM telemetry
	statFSMName    *status.AtomicString
	statFSMElapsed *atomic.Int64
	statFSMMaxDur  *atomic.Int64
	statFSMIndex   *atomic.Int64
	statFSMTotal   *atomic.Int64

	// Cursor blink state
	cursorBlinkOn   bool
	lastBlinkToggle time.Time
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	statusReg := gameCtx.World.Resources.Status

	return &StatusBarRenderer{
		gameCtx: gameCtx,

		colorMode: gameCtx.World.Resources.Config.ColorMode,

		statFPS:        statusReg.Ints.Get("engine.fps"),
		statAPM:        statusReg.Ints.Get("engine.apm"),
		statTicks:      statusReg.Ints.Get("engine.ticks"),
		statPhase:      statusReg.Ints.Get("engine.phase"),
		statDecayTimer: statusReg.Ints.Get("decay.timer"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),
	}
}

// Render implements SystemRenderer
func (r *StatusBarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)
	statusY := ctx.GameYOffset + ctx.ViewportHeight + 1

	// Bounds check: skip if status row outside screen
	if statusY >= ctx.ScreenHeight {
		return
	}

	// Clear status bar
	for x := 0; x < ctx.ScreenWidth; x++ {
		buf.SetWithBg(x, statusY, ' ', visual.RgbBackground, visual.RgbBackground)
	}

	// Update cursor blink state (250ms cycle, uses real time - continues during pause)
	realNow := r.gameCtx.PausableClock.RealTime()
	if realNow.Sub(r.lastBlinkToggle) >= parameter.StatusCursorBlinkDuration {
		r.cursorBlinkOn = !r.cursorBlinkOn
		r.lastBlinkToggle = realNow
	}

	// === BUILD RIGHT-SIDE ITEMS ===
	type statusItem struct {
		text string
		fg   terminal.RGB
		bg   terminal.RGB
	}
	var rightItems []statusItem

	// Priority 1: FSM Phase
	phaseName := r.statFSMName.Load()
	if phaseName != "" {
		elapsed := time.Duration(r.statFSMElapsed.Load())
		maxDur := time.Duration(r.statFSMMaxDur.Load())
		phaseIdx := r.statFSMIndex.Load()
		phaseTotal := r.statFSMTotal.Load()

		var timerVal float64
		if maxDur > 0 {
			remaining := maxDur - elapsed
			if remaining < 0 {
				remaining = 0
			}
			timerVal = remaining.Seconds()
		} else {
			timerVal = elapsed.Seconds()
		}

		phaseBg := render.RainbowIndexColor(phaseIdx, phaseTotal, visual.RgbModeNormalBg)
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" %s: %.1fs ", phaseName, timerVal),
			fg:   visual.RgbBlack,
			bg:   phaseBg,
		})
	}

	// Priority 2: Energy
	energyComp, _ := r.gameCtx.World.Components.Energy.GetComponent(r.gameCtx.World.Resources.Player.Entity)
	energyVal := energyComp.Current
	energyText := fmt.Sprintf(" Energy: %d ", energyVal)

	var energyFg, energyBg terminal.RGB
	if energyVal < 0 {
		energyFg, energyBg = visual.RgbEnergyBg, visual.RgbBlack
	} else {
		energyFg, energyBg = visual.RgbBlack, visual.RgbEnergyBg
	}

	blinkRemaining := energyComp.BlinkRemaining
	if energyComp.BlinkActive && blinkRemaining > 0 {
		typeCode := energyComp.BlinkType
		if typeCode == 0 {
			energyFg = visual.RgbCursorError
		} else {
			var blinkColor terminal.RGB
			switch typeCode {
			case 1:
				blinkColor = visual.RgbEnergyBlinkBlue
			case 2:
				blinkColor = visual.RgbEnergyBlinkGreen
			case 3:
				blinkColor = visual.RgbEnergyBlinkRed
			case 4:
				blinkColor = visual.RgbGlyphGold
			default:
				blinkColor = visual.RgbEnergyBlinkWhite
			}
			energyFg, energyBg = visual.RgbBlack, blinkColor
		}
	}
	rightItems = append(rightItems, statusItem{text: energyText, fg: energyFg, bg: energyBg})

	// Priority 3: Boost (conditional)
	boost, boostOk := r.gameCtx.World.Components.Boost.GetComponent(r.gameCtx.World.Resources.Player.Entity)
	if boostOk && boost.Active {
		remaining := boost.Remaining.Seconds()
		if remaining < 0 {
			remaining = 0
		}
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Boost: %.1fs ", remaining),
			fg:   visual.RgbStatusText,
			bg:   visual.RgbBoostBg,
		})
	}

	// Priority 4: Grid (conditional)
	if ping, ok := r.gameCtx.World.Components.Ping.GetComponent(r.gameCtx.World.Resources.Player.Entity); ok && ping.GridActive {
		gridRemaining := ping.GridRemaining.Seconds()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Grid: %.1fs ", gridRemaining),
			fg:   visual.RgbGridTimerFg,
			bg:   visual.RgbBackground,
		})
	}

	// Priority 5-8: Metrics (lowest priority, dropped first)
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" APM: %d ", r.statAPM.Load()),
		fg:   visual.RgbBlack,
		bg:   visual.RgbApmBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" GT: %d ", r.statTicks.Load()),
		fg:   visual.RgbBlack,
		bg:   visual.RgbGtBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" FPS: %d ", r.statFPS.Load()),
		fg:   visual.RgbBlack,
		bg:   visual.RgbFpsBg,
	})

	var colorModeStr string
	if r.colorMode == terminal.ColorModeTrueColor {
		colorModeStr = " TC "
	} else {
		colorModeStr = " 256 "
	}
	rightItems = append(rightItems, statusItem{
		text: colorModeStr,
		fg:   visual.RgbBlack,
		bg:   visual.RgbColorModeIndicator,
	})

	// === RENDER LEFT-SIDE FIXED ELEMENTS ===
	x := 0

	// Audio state indicator
	if player := r.gameCtx.GetAudioPlayer(); player != nil {
		effectMuted := player.IsEffectMuted()
		musicMuted := player.IsMusicMuted()

		var audioBgColor terminal.RGB
		switch {
		case effectMuted && musicMuted:
			audioBgColor = visual.RgbAudioBothOff
		case effectMuted && !musicMuted:
			audioBgColor = visual.RgbAudioMusicOnly
		case !effectMuted && musicMuted:
			audioBgColor = visual.RgbAudioEffectsOnly
		default:
			audioBgColor = visual.RgbAudioBothOn
		}
		for _, ch := range parameter.AudioStr {
			if x >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(x, statusY, ch, visual.RgbBlack, audioBgColor)
			x++
		}
	}

	// Mode indicator
	var modeText string
	var modeBgColor terminal.RGB
	if r.gameCtx.IsSearchMode() {
		modeText = parameter.ModeTextSearch
		modeBgColor = visual.RgbModeSearchBg
	} else if r.gameCtx.IsCommandMode() {
		modeText = parameter.ModeTextCommand
		modeBgColor = visual.RgbModeCommandBg
	} else if r.gameCtx.IsInsertMode() {
		modeText = parameter.ModeTextInsert
		modeBgColor = visual.RgbModeInsertBg
	} else if r.gameCtx.IsVisualMode() {
		modeText = parameter.ModeTextVisual
		modeBgColor = visual.RgbModeVisualBg
	} else {
		modeText = parameter.ModeTextNormal
		modeBgColor = visual.RgbModeNormalBg
	}
	for _, ch := range modeText {
		if x >= ctx.ScreenWidth {
			return
		}
		buf.SetWithBg(x, statusY, ch, visual.RgbStatusText, modeBgColor)
		x++
	}

	// Macro recording indicator
	if r.gameCtx.MacroRecording.Load() {
		label := r.gameCtx.MacroRecordingLabel.Load()
		recText := fmt.Sprintf("%s: %c ", parameter.ModeTextRecord, label)
		recX := x - len(modeText)
		for i, ch := range recText {
			if recX+i < ctx.ScreenWidth {
				buf.SetWithBg(recX+i, statusY, ch, visual.RgbBlack, visual.RgbCursorError)
			}
		}
	}

	// Last command indicator (only in normal/visual/insert modes)
	leftEndX := x + 1 // 1 char gap after mode indicator
	lastCommand := r.gameCtx.GetLastCommand()
	if lastCommand != "" && !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode() {
		for _, ch := range lastCommand {
			if leftEndX >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, visual.RgbLastCommandText, visual.RgbBackground)
			leftEndX++
		}
		leftEndX++ // gap after last command
	}

	// === DETERMINE TEXT CONTENT AND NEEDED WIDTH ===
	var textContent string
	var textFg terminal.RGB
	var isInputMode bool // search or command mode (needs cursor)

	if r.gameCtx.IsSearchMode() {
		textContent = "/" + r.gameCtx.GetSearchText()
		textFg = visual.RgbSearchInputText
		isInputMode = true
	} else if r.gameCtx.IsCommandMode() {
		textContent = ":" + r.gameCtx.GetCommandText()
		textFg = visual.RgbCommandInputText
		isInputMode = true
	} else {
		textContent = r.getActiveStatusMessage(realNow)
		textFg = visual.RgbStatusMessageText
		isInputMode = false
	}

	textNeeded := utf8.RuneCountInString(textContent)
	if isInputMode && !r.gameCtx.IsOverlayActive() {
		textNeeded++ // Reserve space for cursor
	}

	// === DYNAMIC RIGHT-SIDE ALLOCATION ===
	// Calculate widths for all right items
	itemWidths := make([]int, len(rightItems))
	for i, item := range rightItems {
		itemWidths[i] = utf8.RuneCountInString(item.text)
	}

	availableTotal := ctx.ScreenWidth - leftEndX

	// Start with max items that could fit (ignoring text needs)
	fitCount := 0
	rightFitWidth := 0
	for i, w := range itemWidths {
		if rightFitWidth+w <= availableTotal {
			rightFitWidth += w
			fitCount = i + 1
		} else {
			break
		}
	}

	// Drop items from end (lowest priority) until text fits
	for fitCount > 0 && textNeeded > 0 {
		textAvailable := availableTotal - rightFitWidth
		if textAvailable >= textNeeded {
			break
		}
		// Drop last item
		fitCount--
		rightFitWidth = 0
		for i := 0; i < fitCount; i++ {
			rightFitWidth += itemWidths[i]
		}
	}

	textAvailableWidth := availableTotal - rightFitWidth
	if textAvailableWidth < 0 {
		textAvailableWidth = 0
	}

	// === RENDER TEXT CONTENT ===
	var textEndX int
	if isInputMode {
		textEndX = r.renderInputText(buf, statusY, leftEndX, textAvailableWidth, textContent, textFg)
	} else if textContent != "" {
		r.renderStatusMessage(buf, statusY, leftEndX, textAvailableWidth, textContent)
		textEndX = leftEndX + min(utf8.RuneCountInString(textContent), textAvailableWidth)
	}

	// === RENDER CURSOR (search/command modes only, not during overlay) ===
	if isInputMode && !r.gameCtx.IsOverlayActive() && r.cursorBlinkOn {
		cursorX := textEndX
		if cursorX < ctx.ScreenWidth-rightFitWidth {
			buf.SetWithBg(cursorX, statusY, parameter.StatusCursorChar, visual.RgbStatusCursor, visual.RgbStatusCursorBg)
		}
	}

	// === RENDER RIGHT-SIDE ITEMS ===
	if fitCount > 0 {
		startX := ctx.ScreenWidth - rightFitWidth
		for i := 0; i < fitCount; i++ {
			item := rightItems[i]
			for _, ch := range item.text {
				buf.SetWithBg(startX, statusY, ch, item.fg, item.bg)
				startX++
			}
		}
	}
}

// getActiveStatusMessage returns status message if not expired
func (r *StatusBarRenderer) getActiveStatusMessage(now time.Time) string {
	msg := r.gameCtx.GetStatusMessage()
	if msg == "" {
		return ""
	}

	expiry := r.gameCtx.GetStatusMessageExpiry()
	if expiry > 0 && now.UnixNano() > expiry {
		// Expired - clear it
		r.gameCtx.ClearStatusMessage()
		return ""
	}

	return msg
}

// renderInputText renders search/command input with left-truncation (shows end of text)
// Returns X position after last rendered character (for cursor placement)
func (r *StatusBarRenderer) renderInputText(buf *render.RenderBuffer, y, startX, maxWidth int, text string, fg terminal.RGB) int {
	if maxWidth <= 0 {
		return startX
	}

	runes := []rune(text)
	textLen := len(runes)

	if textLen <= maxWidth {
		for i, ch := range runes {
			buf.SetWithBg(startX+i, y, ch, fg, visual.RgbBackground)
		}
		return startX + textLen
	}

	// Truncate from left: show '<' + end of text
	if maxWidth == 1 {
		buf.SetWithBg(startX, y, '<', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
		return startX + 1
	}

	buf.SetWithBg(startX, y, '<', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
	visibleStart := textLen - (maxWidth - 1)
	for i := 0; i < maxWidth-1; i++ {
		buf.SetWithBg(startX+1+i, y, runes[visibleStart+i], fg, visual.RgbBackground)
	}
	return startX + maxWidth
}

// renderStatusMessage renders status message with right-truncation (shows start of text)
func (r *StatusBarRenderer) renderStatusMessage(buf *render.RenderBuffer, y, startX, maxWidth int, text string) {
	if maxWidth <= 0 {
		return
	}

	runes := []rune(text)
	textLen := len(runes)

	if textLen <= maxWidth {
		for i, ch := range runes {
			buf.SetWithBg(startX+i, y, ch, visual.RgbStatusMessageText, visual.RgbBackground)
		}
		return
	}

	if maxWidth == 1 {
		buf.SetWithBg(startX, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
		return
	}

	for i := 0; i < maxWidth-1; i++ {
		buf.SetWithBg(startX+i, y, runes[i], visual.RgbStatusMessageText, visual.RgbBackground)
	}
	buf.SetWithBg(startX+maxWidth-1, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
}