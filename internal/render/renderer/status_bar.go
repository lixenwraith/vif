package renderer

import (
	"fmt"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// statusItem is one right-aligned run of status bar cells
type statusItem struct {
	text string
	fg   color.RGB
	bg   color.RGB
}

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx *engine.GameContext

	// Color mode (persist throughout runtime)
	colorMode terminal.ColorMode

	// Sound/Audio indicator
	statAudioMask *atomic.Int64

	// Cached metric pointers (zero-lock reads)
	statFPS   *atomic.Int64
	statAPM   *atomic.Int64
	statTicks *atomic.Int64

	// Time control telemetry
	statSpeed      *status.AtomicString
	statStep       *atomic.Int64
	statBreak      *status.AtomicString
	statNet        *status.AtomicString
	statStale      *atomic.Bool
	statLag        *atomic.Int64
	statCorrection *atomic.Int64
	statPeers      *atomic.Int64
	statLatch      *atomic.Bool

	// The operating point. Phase 5 made the correction cadence a function of the
	// link, and a player whose picture has gone coarse needs to be told which of
	// the two it is — a constrained link, or a link that cannot converge at all.
	statCadence     *atomic.Int64
	statKeyframe    *atomic.Int64
	statLinkRTT     *atomic.Int64
	statLinkJitter  *atomic.Int64
	statLinkBps     *atomic.Int64
	statConstrained *atomic.Bool
	statFloor       *atomic.Bool

	// FSM telemetry
	statFSMName    *status.AtomicString
	statFSMElapsed *atomic.Int64
	statFSMMaxDur  *atomic.Int64
	statFSMIndex   *atomic.Int64
	statFSMTotal   *atomic.Int64

	// Energy telemetry
	statDamageMultiplier *atomic.Int64

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

		statAudioMask: statusReg.Ints.Get("audio.mask"),

		statFPS:   statusReg.Ints.Get("engine.fps"),
		statAPM:   statusReg.Ints.Get("engine.apm"),
		statTicks: statusReg.Ints.Get("engine.ticks"),

		statSpeed:      statusReg.Strings.Get("engine.speed"),
		statStep:       statusReg.Ints.Get("engine.step"),
		statBreak:      statusReg.Strings.Get("engine.breakpoint"),
		statNet:        statusReg.Strings.Get("network.state"),
		statStale:      statusReg.Bools.Get("network.stale"),
		statLag:        statusReg.Ints.Get("network.lag_ticks"),
		statCorrection: statusReg.Ints.Get("snapshot.correction_entities"),
		statPeers:      statusReg.Ints.Get("network.peers"),
		statLatch:      statusReg.Bools.Get("network.map_latched"),

		statCadence:     statusReg.Ints.Get("snapshot.cadence_ticks"),
		statKeyframe:    statusReg.Ints.Get("snapshot.cadence_keyframe_interval"),
		statLinkRTT:     statusReg.Ints.Get("network.link_rtt_ms"),
		statLinkJitter:  statusReg.Ints.Get("network.link_jitter_ms"),
		statLinkBps:     statusReg.Ints.Get("network.link_bps"),
		statConstrained: statusReg.Bools.Get("snapshot.cadence_constrained"),
		statFloor:       statusReg.Bools.Get("snapshot.cadence_floor_breached"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),

		statDamageMultiplier: statusReg.Ints.Get("energy.damage_multiplier"),
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
	for x := range ctx.ScreenWidth {
		buf.SetWithBg(x, statusY, ' ', visual.RgbBackground, visual.RgbBackground)
	}

	// Cursor blink runs on wall time: it must continue while the world is paused
	realNow := r.gameCtx.TimeCtl.RealTime()
	if realNow.Sub(r.lastBlinkToggle) >= parameter.StatusCursorBlinkDuration {
		r.cursorBlinkOn = !r.cursorBlinkOn
		r.lastBlinkToggle = realNow
	}

	// === BUILD RIGHT-SIDE ITEMS ===

	var rightItems []statusItem

	// Priority 0: a link that cannot converge, and the staleness beside it, must
	// survive even the narrowest useful status bar.
	if item, ok := r.linkItem(); ok {
		rightItems = append(rightItems, item)
	}
	if item, ok := r.syncItem(); ok {
		rightItems = append(rightItems, item)
	}
	// Time control is absent at real time with nothing pending.
	if item, ok := r.timeItem(); ok {
		rightItems = append(rightItems, item)
	}
	if item, ok := r.networkItem(); ok {
		rightItems = append(rightItems, item)
	}

	// Priority 1: FSM Phase
	phaseName := r.statFSMName.Load()
	if phaseName != "" {
		elapsed := time.Duration(r.statFSMElapsed.Load())
		maxDur := time.Duration(r.statFSMMaxDur.Load())
		phaseIdx := r.statFSMIndex.Load()
		phaseTotal := r.statFSMTotal.Load()

		var timerVal float64
		if maxDur > 0 {
			remaining := max(maxDur-elapsed, 0)
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
	var playerEntity core.Entity
	if r.gameCtx.World.Resources.Player.Valid() {
		playerEntity = r.gameCtx.World.Resources.Player.Entity
	}
	energyComp, hasEnergy := r.gameCtx.World.Components.Energy.GetPtr(playerEntity)
	var energyVal int64
	if hasEnergy {
		energyVal = energyComp.Current
	}
	energyText := fmt.Sprintf(" Energy: %d ", energyVal)

	var energyFg, energyBg color.RGB
	if energyVal < 0 {
		energyFg, energyBg = visual.RgbEnergyBg, visual.RgbBlack
	} else {
		energyFg, energyBg = visual.RgbBlack, visual.RgbEnergyBg
	}

	view, hasView := r.gameCtx.World.Components.CursorView.GetPtr(playerEntity)
	if hasView && view.BlinkActive && view.BlinkRemaining > 0 {
		typeCode := view.BlinkType
		if typeCode == 0 {
			energyFg = visual.RgbCursorError
		} else {
			var blinkColor color.RGB
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

	// Priority 3: Damage Multiplier (cycle scaling)
	dmgMult := r.statDamageMultiplier.Load()
	if dmgMult > 1 {
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" x%d ", dmgMult),
			fg:   visual.RgbBlack,
			bg:   visual.RgbCursorError, // Red background
		})
	}

	// Priority 4: Boost (conditional)
	boost, boostOk := r.gameCtx.World.Components.Boost.GetPtr(playerEntity)
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

	// Priority 5: Grid (conditional)
	if ping, ok := r.gameCtx.World.Components.Ping.GetPtr(playerEntity); ok && ping.GridActive {
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

	// Priority 6-9: Metrics (lowest priority, dropped first)
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

	// Audio state indicator; -1 = no audio resource
	if mv := r.statAudioMask.Load(); mv >= 0 {
		var audioBgColor color.RGB
		switch uint8(mv) {
		case parameter.AudioChanNone:
			audioBgColor = visual.RgbAudioBothOff
		case parameter.AudioChanMusic:
			audioBgColor = visual.RgbAudioMusicOnly
		case parameter.AudioChanEffects:
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
	var modeBgColor color.RGB
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
	var textFg color.RGB
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
		textContent = r.getActiveStatusMessage(r.gameCtx.TimeCtl.Now())
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
		for i := range fitCount {
			rightFitWidth += itemWidths[i]
		}
	}

	textAvailableWidth := max(availableTotal-rightFitWidth, 0)

	// === RENDER TEXT CONTENT ===
	var textEndX int
	if isInputMode {
		cursorPos := utf8.RuneCountInString(textContent) // search: cursor at end
		if r.gameCtx.IsCommandMode() {
			cursorPos = r.gameCtx.GetCommandCursorPos() + 1 // +1 for ':' prefix
		}
		textEndX = r.renderInputText(buf, statusY, leftEndX, textAvailableWidth, textContent, textFg, cursorPos)
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
		for i := range fitCount {
			item := rightItems[i]
			for _, ch := range item.text {
				buf.SetWithBg(startX, statusY, ch, item.fg, item.bg)
				startX++
			}
		}
	}
}

// syncItem renders what a participant needs to know about its own picture, which
// Phase 4 changed from a verdict into two measurements.
//
// It used to say DESYNC and then DIVERGED: two instances re-derived the shared
// world from one artifact stream, so a disagreement meant one of them had lost an
// artifact and nothing would ever re-derive it — the second state was a statement
// about the rest of the session rather than about a moment. Under an authority
// neither is true. A guest predicts between corrections and is expected to differ;
// the host's next snapshot replaces whatever it drifted to; there is no state a
// session can enter that the next correction does not leave.
//
// What is worth showing instead is the two things that are not automatically fine.
// LAG says this instance is far enough behind the session that its own crossings
// are reaching the host after the ticks they name — the link, not the game. COR is
// the size of the last correction in shared entities, which is how visibly the
// authority disagreed with the prediction; it is absent when the prediction was
// exact, which at rest it usually is.
func (r *StatusBarRenderer) syncItem() (statusItem, bool) {
	if r.statStale.Load() {
		return statusItem{
			text: fmt.Sprintf(" LAG %d ", r.statLag.Load()),
			fg:   visual.RgbBlack, bg: visual.RgbOrange,
		}, true
	}
	if n := r.statCorrection.Load(); n > 0 {
		return statusItem{
			text: fmt.Sprintf(" COR %d ", n),
			fg:   visual.RgbBlack, bg: visual.RgbGtBg,
		}, true
	}
	return statusItem{}, false
}

// linkItem reports the operating point the link put this session at, and it is
// absent while there is nothing to say.
//
// A player whose picture has gone coarse has two very different problems and
// deserves to be told which. A *constrained* link is the system working: the
// cadence has slowed, prediction is carrying more, and the correction magnitude
// is rising and bounded — the game is fine and the link is small. A link *below
// the convergence floor* is not: no cadence the controller may choose delivers a
// whole authoritative world inside the guaranteed window, so this instance may
// stop converging, and the plan's boundary is that this is said rather than
// silently adapted past.
//
// The item carries all of it because the operating point is not one number: the
// round trip and its variation say what the link is, the cadence and keyframe
// interval say what was chosen, and the rate says how much of the link that
// choice is spending. It is only ever on screen when the link is constrained, so
// the width it costs is width a healthy session never pays.
func (r *StatusBarRenderer) linkItem() (statusItem, bool) {
	cadence := r.statCadence.Load()
	if cadence == 0 {
		return statusItem{}, false
	}
	breached := r.statFloor.Load()
	if !breached && !r.statConstrained.Load() {
		return statusItem{}, false
	}
	label := "LNK"
	bg := visual.RgbOrange
	if breached {
		label, bg = "LINK!", visual.RgbCursorError
	}
	return statusItem{
		text: fmt.Sprintf(" %s %d±%dms %dx%d %s ", label,
			r.statLinkRTT.Load(), r.statLinkJitter.Load(),
			cadence, r.statKeyframe.Load(), byteRate(r.statLinkBps.Load())),
		fg: visual.RgbBlack, bg: bg,
	}, true
}

// byteRate renders a bandwidth estimate in the width a status bar has, which is
// three or four characters rather than the nine a byte count wants.
func byteRate(bps int64) string {
	switch {
	case bps <= 0:
		return "-"
	case bps < 1000:
		return fmt.Sprintf("%dB", bps)
	case bps < 1000*1000:
		return fmt.Sprintf("%dK", bps/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(bps)/1e6)
	}
}

// networkItem reports connection, peer count and the D-14 map latch.
func (r *StatusBarRenderer) networkItem() (statusItem, bool) {
	state := r.statNet.Load()
	if state == "" || state == "off" {
		return statusItem{}, false
	}
	label := "WAIT"
	bg := visual.RgbGtBg
	switch state {
	case "connected":
		label = fmt.Sprintf("%dP", r.statPeers.Load())
		bg = visual.RgbBoostBg
	case "down":
		label = "DOWN"
		bg = visual.RgbCursorError
	}
	latch := "OPEN"
	if r.statLatch.Load() {
		latch = "LOCK"
	}
	return statusItem{text: fmt.Sprintf(" NET:%s/%s ", label, latch), fg: visual.RgbBlack, bg: bg}, true
}

// timeItem builds the time control indicator, present only when the simulation is
// off real time or a step request is pending
func (r *StatusBarRenderer) timeItem() (statusItem, bool) {
	if step := r.statStep.Load(); step > 0 {
		return statusItem{
			text: fmt.Sprintf(" STEP %d ", step),
			fg:   visual.RgbBlack,
			bg:   visual.RgbCursorError,
		}, true
	}

	speed := r.statSpeed.Load()
	if brk := r.statBreak.Load(); brk != "" && brk != "-" {
		return statusItem{
			text: fmt.Sprintf(" %sx>%s ", speed, brk),
			fg:   visual.RgbBlack,
			bg:   visual.RgbCursorError,
		}, true
	}
	if speed != "" && speed != "1" {
		return statusItem{
			text: fmt.Sprintf(" %sx ", speed),
			fg:   visual.RgbBlack,
			bg:   visual.RgbGtBg,
		}, true
	}
	return statusItem{}, false
}

// getActiveStatusMessage returns the status message if its game-time expiry has not passed
func (r *StatusBarRenderer) getActiveStatusMessage(gameNow time.Time) string {
	msg := r.gameCtx.GetStatusMessage()
	if msg == "" {
		return ""
	}

	expiry := r.gameCtx.GetStatusMessageExpiry()
	if expiry > 0 && gameNow.UnixNano() > expiry {
		// Expired - clear it
		r.gameCtx.ClearStatusMessage()
		return ""
	}

	return msg
}

// renderInputText renders search/command input with scrolling window around cursor
// Returns screen X position where cursor should be drawn
func (r *StatusBarRenderer) renderInputText(buf *render.RenderBuffer, y, startX, maxWidth int, text string, fg color.RGB, cursorPos int) int {
	if maxWidth <= 0 {
		return startX
	}

	runes := []rune(text)
	textLen := len(runes)
	if cursorPos > textLen {
		cursorPos = textLen
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	// No overflow: render all, return cursor screen position
	if textLen < maxWidth {
		for i, ch := range runes {
			buf.SetWithBg(startX+i, y, ch, fg, visual.RgbBackground)
		}
		return startX + cursorPos
	}

	// Overflow: compute scrolling window
	winStart, contentSlots, leftTrunc, rightTrunc := computeInputWindow(textLen, cursorPos, maxWidth)

	// Render indicators and content
	screenX := startX
	if leftTrunc {
		buf.SetWithBg(screenX, y, '<', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
		screenX++
	}
	winEnd := min(winStart+contentSlots, textLen)
	for i := winStart; i < winEnd; i++ {
		buf.SetWithBg(screenX, y, runes[i], fg, visual.RgbBackground)
		screenX++
	}
	if rightTrunc {
		buf.SetWithBg(screenX, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
	}

	li := 0
	if leftTrunc {
		li = 1
	}
	return startX + li + (cursorPos - winStart)
}

// computeInputWindow determines the visible rune range for scrolled input text
// Returns window start index, content slot count, and truncation flags
func computeInputWindow(textLen, cursorPos, maxWidth int) (winStart, contentSlots int, leftTrunc, rightTrunc bool) {
	// Effective display length: cursor at end of text occupies one extra cell
	displayLen := textLen
	if cursorPos == textLen {
		displayLen++
	}

	// Initial placement: cursor at ~1/3 from left for typing comfort
	winStart = max(0, cursorPos-maxWidth/3)

	// Iterative fit: converge indicators and cursor visibility (max 2 passes)
	for range 3 {
		leftTrunc = winStart > 0
		li := 0
		if leftTrunc {
			li = 1
		}
		contentSlots = maxWidth - li
		winEnd := winStart + contentSlots

		rightTrunc = winEnd < displayLen
		if rightTrunc {
			contentSlots--
			winEnd = winStart + contentSlots
		}

		// Cursor must be in [winStart, winStart+contentSlots)
		if cursorPos < winStart {
			winStart = cursorPos
			continue
		}
		if cursorPos >= winStart+contentSlots {
			winStart = max(cursorPos-contentSlots+1, 0)
			continue
		}

		// Fill trailing slack: pull winStart back to maximize visible content
		visible := contentSlots
		if winStart+visible > displayLen {
			visible = displayLen - winStart
		}
		if visible < contentSlots && winStart > 0 {
			winStart -= min(contentSlots-visible, winStart)
			continue
		}

		break
	}

	return
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

	for i := range maxWidth - 1 {
		buf.SetWithBg(startX+i, y, runes[i], visual.RgbStatusMessageText, visual.RgbBackground)
	}
	buf.SetWithBg(startX+maxWidth-1, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
}
