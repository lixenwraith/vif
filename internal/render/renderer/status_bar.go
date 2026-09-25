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

// with returns the item's colours around text
func (s statusItem) with(text string) statusItem {
	s.text = text
	return s
}

// statusPalette holds the status bar's colours. TrueColor takes the theme's; a text console
// takes fixed entries, each apart from the items beside it and with the text that reads best on
// it, since a theme colour could land on any of eight backgrounds, black included.
type statusPalette struct {
	audio            [4]statusItem // by channel mask
	mode             [5]statusItem // normal, visual, insert, search, command
	net              [3]statusItem // by severity
	wait, speed      statusItem
	alarm            statusItem // a pending step, a breakpoint, the multiplier, a recording
	energy, negative statusItem
	blink            [5]statusItem // by blink type 1-4, then white
	energyError      color.RGB
	boost            statusItem
	apm, gt, fps     statusItem
	label            statusItem // the colour mode, text included
	phase            func(index, total int64) statusItem
}

func statusPaletteTrueColor() *statusPalette {
	on := func(bg color.RGB) statusItem { return statusItem{fg: visual.RgbBlack, bg: bg} }
	text := func(bg color.RGB) statusItem { return statusItem{fg: visual.RgbStatusText, bg: bg} }
	return &statusPalette{
		audio: [4]statusItem{
			on(visual.RgbAudioBothOff), on(visual.RgbAudioEffectsOnly), on(visual.RgbAudioMusicOnly), on(visual.RgbAudioBothOn),
		},
		mode: [5]statusItem{
			text(visual.RgbModeNormalBg), text(visual.RgbModeVisualBg), text(visual.RgbModeInsertBg),
			text(visual.RgbModeSearchBg), text(visual.RgbModeCommandBg),
		},
		net:         [3]statusItem{on(visual.RgbNetGoodBg), on(visual.RgbNetWarnBg), on(visual.RgbNetBadBg)},
		wait:        on(visual.RgbGtBg),
		speed:       on(visual.RgbGtBg),
		alarm:       on(visual.RgbCursorError),
		energy:      on(visual.RgbEnergyBg),
		negative:    statusItem{fg: visual.RgbEnergyBg, bg: visual.RgbBlack},
		energyError: visual.RgbCursorError,
		boost:       text(visual.RgbBoostBg),
		apm:         on(visual.RgbApmBg),
		gt:          on(visual.RgbGtBg),
		fps:         on(visual.RgbFpsBg),
		label:       on(visual.RgbColorModeIndicator).with(" TC "),
		blink: [5]statusItem{
			on(visual.RgbEnergyBlinkBlue), on(visual.RgbEnergyBlinkGreen), on(visual.RgbEnergyBlinkRed),
			on(visual.RgbGlyphGold), on(visual.RgbEnergyBlinkWhite),
		},
		phase: func(index, total int64) statusItem {
			return on(render.RainbowIndexColor(index, total, visual.RgbModeNormalBg))
		},
	}
}

// statusPalette256 names console entries; the audio letter is drawn on the bar's own black, so it
// needs no background apart from the mode beside it
func statusPalette256(c *render.Console) *statusPalette {
	on := func(bg uint8) statusItem { return statusItem{fg: c.Color(c.TextOn(bg)), bg: c.Color(bg)} }
	letter := func(fg uint8) statusItem { return statusItem{fg: c.Color(fg), bg: visual.RgbBackground} }
	phase := on(visual.ConBlue)
	return &statusPalette{
		audio: [4]statusItem{
			letter(visual.ConBrightRed), letter(visual.ConBrightGreen), letter(visual.ConBrightYellow), letter(visual.ConBrightCyan),
		},
		mode: [5]statusItem{
			on(visual.ConCyan), on(visual.ConYellow), on(visual.ConGreen), on(visual.ConWhite), on(visual.ConMagenta),
		},
		net:         [3]statusItem{on(visual.ConGreen), on(visual.ConYellow), on(visual.ConRed)},
		wait:        on(visual.ConYellow),
		speed:       on(visual.ConYellow),
		alarm:       on(visual.ConRed),
		energy:      on(visual.ConWhite),
		negative:    letter(visual.ConBrightWhite),
		energyError: c.Color(visual.ConRed),
		boost:       on(visual.ConMagenta),
		apm:         on(visual.ConGreen),
		gt:          on(visual.ConYellow),
		fps:         on(visual.ConCyan),
		label:       on(visual.ConWhite).with(" 256 "),
		blink: [5]statusItem{
			on(visual.ConBlue), on(visual.ConGreen), on(visual.ConRed), on(visual.ConYellow), on(visual.ConWhite),
		},
		phase: func(int64, int64) statusItem { return phase },
	}
}

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx *engine.GameContext
	pal     *statusPalette

	// Sound/Audio indicator
	statAudioMask *atomic.Int64

	// Cached metric pointers (zero-lock reads)
	statFPS   *atomic.Int64
	statAPM   *atomic.Int64
	statTicks *atomic.Int64

	// Time control telemetry
	statSpeed *status.AtomicString
	statStep  *atomic.Int64
	statBreak *status.AtomicString

	// The session, in the one badge networkItem renders. The rest of the
	// measurements behind it — jitter, cadence, keyframe interval, byte rate, the
	// map latch — are read in the status snapshot and in :session, not here.
	statNet         *status.AtomicString
	statStale       *atomic.Bool
	statLag         *atomic.Int64
	statRTT         *atomic.Int64
	statLoss        *atomic.Int64
	statHostLost    *atomic.Bool
	statMigrating   *atomic.Bool
	statCadence     *atomic.Int64
	statConstrained *atomic.Bool
	statFloor       *atomic.Bool
	statRejoin      *atomic.Int64

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

	// The session cell as last drawn, and when. See networkItem.
	netHeld   statusItem
	netHeldAt time.Time
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	statusReg := gameCtx.World.Resources.Status

	r := &StatusBarRenderer{
		gameCtx: gameCtx,
		pal:     statusPaletteTrueColor(),

		statAudioMask: statusReg.Ints.Get("audio.mask"),

		statFPS:   statusReg.Ints.Get("engine.fps"),
		statAPM:   statusReg.Ints.Get("engine.music_apm"), // the rate music follows, not the minute's sum
		statTicks: statusReg.Ints.Get("engine.ticks"),

		statSpeed:     statusReg.Strings.Get("engine.speed"),
		statStep:      statusReg.Ints.Get("engine.step"),
		statBreak:     statusReg.Strings.Get("engine.breakpoint"),
		statNet:       statusReg.Strings.Get("network.state"),
		statStale:     statusReg.Bools.Get("network.stale"),
		statLag:       statusReg.Ints.Get("network.lag_ticks"),
		statRTT:       statusReg.Ints.Get("network.link_rtt_us"),
		statLoss:      statusReg.Ints.Get("network.link_loss_pct"),
		statHostLost:  statusReg.Bools.Get("network.host_lost"),
		statMigrating: statusReg.Bools.Get("network.migrating"),

		statCadence:     statusReg.Ints.Get("snapshot.cadence_ticks"),
		statConstrained: statusReg.Bools.Get("snapshot.cadence_constrained"),
		statFloor:       statusReg.Bools.Get("snapshot.cadence_floor_breached"),
		statRejoin:      statusReg.Ints.Get("network.rejoin_attempts"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),

		statDamageMultiplier: statusReg.Ints.Get("energy.damage_multiplier"),
	}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.pal = statusPalette256(render.ConsoleFor(cfg.ConsolePalette))
	}
	return r
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

	// Priority 0: one cell for the whole session. See networkItem.
	if item, ok := r.networkItem(); ok {
		rightItems = append(rightItems, item)
	}
	// Time control is absent at real time with nothing pending.
	if item, ok := r.timeItem(); ok {
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

		rightItems = append(rightItems, r.pal.phase(phaseIdx, phaseTotal).with(fmt.Sprintf(" %s: %.1fs ", phaseName, timerVal)))
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
	energy := r.pal.energy
	if energyVal < 0 {
		energy = r.pal.negative
	}

	view, hasView := r.gameCtx.World.Components.CursorView.GetPtr(playerEntity)
	if hasView && view.BlinkActive && view.BlinkRemaining > 0 {
		switch typeCode := view.BlinkType; {
		case typeCode == 0:
			energy.fg = r.pal.energyError
		case typeCode > 0 && typeCode < len(r.pal.blink):
			energy = r.pal.blink[typeCode-1]
		default:
			energy = r.pal.blink[len(r.pal.blink)-1]
		}
	}
	rightItems = append(rightItems, energy.with(fmt.Sprintf(" Energy: %s ", status.FormatCount(energyVal))))

	// Priority 3: Damage Multiplier (cycle scaling)
	dmgMult := r.statDamageMultiplier.Load()
	if dmgMult > 1 {
		rightItems = append(rightItems, r.pal.alarm.with(fmt.Sprintf(" x%s ", status.FormatCount(dmgMult))))
	}

	// Priority 4: Boost (conditional)
	boost, boostOk := r.gameCtx.World.Components.Boost.GetPtr(playerEntity)
	if boostOk && boost.Active {
		remaining := boost.Remaining.Seconds()
		if remaining < 0 {
			remaining = 0
		}
		rightItems = append(rightItems, r.pal.boost.with(fmt.Sprintf(" Boost: %.1fs ", remaining)))
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
	rightItems = append(rightItems,
		r.pal.apm.with(fmt.Sprintf(" APM: %d ", r.statAPM.Load())),
		r.pal.gt.with(fmt.Sprintf(" GT: %d ", r.statTicks.Load())),
		r.pal.fps.with(fmt.Sprintf(" FPS: %d ", r.statFPS.Load())),
		r.pal.label,
	)

	// === RENDER LEFT-SIDE FIXED ELEMENTS ===
	x := 0

	// Audio state indicator; -1 = no audio resource
	if mv := r.statAudioMask.Load(); mv >= 0 {
		mask := uint8(mv) & parameter.AudioChanAll
		audio := r.pal.audio[mask]
		for _, ch := range parameter.AudioText[mask] {
			if x >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(x, statusY, ch, audio.fg, audio.bg)
			x++
		}
	}

	// Mode indicator
	var modeText string
	var mode statusItem
	if r.gameCtx.IsSearchMode() {
		modeText, mode = parameter.ModeTextSearch, r.pal.mode[3]
	} else if r.gameCtx.IsCommandMode() {
		modeText, mode = parameter.ModeTextCommand, r.pal.mode[4]
	} else if r.gameCtx.IsInsertMode() {
		modeText, mode = parameter.ModeTextInsert, r.pal.mode[2]
	} else if r.gameCtx.IsVisualMode() {
		modeText, mode = parameter.ModeTextVisual, r.pal.mode[1]
	} else {
		modeText, mode = parameter.ModeTextNormal, r.pal.mode[0]
	}
	for _, ch := range modeText {
		if x >= ctx.ScreenWidth {
			return
		}
		buf.SetWithBg(x, statusY, ch, mode.fg, mode.bg)
		x++
	}

	// Macro recording indicator
	if r.gameCtx.MacroRecording.Load() {
		label := r.gameCtx.MacroRecordingLabel.Load()
		recText := fmt.Sprintf("%s: %c ", parameter.ModeTextRecord, label)
		recX := x - len(modeText)
		for i, ch := range recText {
			if recX+i < ctx.ScreenWidth {
				buf.SetWithBg(recX+i, statusY, ch, r.pal.alarm.fg, r.pal.alarm.bg)
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

	// Input the player is typing takes the space it needs from the right-side
	// items, lowest priority first. A message takes only what they leave, so one
	// arriving never reflows the bar; the command it follows pushes it right.
	for isInputMode && fitCount > 0 && textNeeded > 0 {
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

// networkItem is the badge as drawn, held for StatusNetworkHoldDuration. Its
// inputs move on the correction cadence and its width reflows every item beside
// it, so an unheld cell repaints faster than it can be read.
func (r *StatusBarRenderer) networkItem() (statusItem, bool) {
	item, ok := r.networkBadge()
	now := r.gameCtx.TimeCtl.RealTime() // [wall] readability, not simulation
	switch {
	case !ok:
		r.netHeld = statusItem{}
		return statusItem{}, false
	case r.netHeld.text == "" || now.Sub(r.netHeldAt) >= parameter.StatusNetworkHoldDuration:
		r.netHeld, r.netHeldAt = item, now
	}
	return r.netHeld, true
}

// networkBadge is the session in one badge: the round trip, which is the reading
// a player already has from every other networked game, coloured by how well this
// instance is keeping up, and at most one qualifier. A worse fact hides a lesser
// one; the numbers behind them are in :session and the status snapshot.
func (r *StatusBarRenderer) networkBadge() (statusItem, bool) {
	// Losing the authority is a permanent change for this run and outranks
	// everything, including the link state that described the host that went.
	if r.statHostLost.Load() {
		return r.pal.net[severityFailing].with(" Host lost "), true
	}
	// Transient by construction: the badge is cleared a fixed number of ticks after
	// the handoff is adopted, so the two states a player reads are either side of it.
	if r.statMigrating.Load() {
		// The count is the reconnect walking the candidate list.
		text := " Migrating "
		if n := r.statRejoin.Load(); n > 0 {
			text = fmt.Sprintf(" Migrating %d ", n)
		}
		return r.pal.net[severityDegrading].with(text), true
	}
	state := r.statNet.Load()
	if state == "" || state == "off" {
		return statusItem{}, false
	}
	switch state {
	case "down":
		return r.pal.net[severityFailing].with(" Net: down "), true
	case "connected":
	default:
		return r.pal.wait.with(" Net: wait "), true
	}

	// slow!     no cadence delivers a whole world inside the guaranteed window;
	// desync n  this instance is n ticks behind, so its crossings land late;
	// loss n%   probes went unanswered often enough for the link to be the cause;
	// slow      the cadence backed off and prediction carries more.
	rtt := r.statRTT.Load()
	// Players rather than links: a guest holds one link whatever the session's size.
	text := fmt.Sprintf(" %dP %s", r.gameCtx.World.Resources.Player.Count(), status.FormatLatency(rtt))
	severity := latencySeverity(rtt)
	loss := r.statLoss.Load()
	switch {
	case r.statFloor.Load() && r.statCadence.Load() != 0:
		text, severity = text+" slow! ", severityFailing
	case r.statStale.Load():
		text, severity = fmt.Sprintf("%s desync %d ", text, r.statLag.Load()), max(severity, severityDegrading)
	case loss >= parameter.StatusNetLossWarnPct:
		text, severity = fmt.Sprintf("%s loss %d%% ", text, loss), max(severity, severityDegrading)
	case r.statConstrained.Load() && r.statCadence.Load() != 0:
		text, severity = text+" slow ", max(severity, severityDegrading)
	default:
		text += " "
	}
	return r.pal.net[severity].with(text), true
}

// Link health as a player reads it, worst wins; latencySeverity is the round trip
// alone, which colours a badge that has nothing else to say.
const (
	severitySettled = iota
	severityDegrading
	severityFailing
)

func latencySeverity(us int64) int {
	switch rtt := time.Duration(us) * time.Microsecond; {
	case rtt >= parameter.StatusNetLatencyBad:
		return severityFailing
	case rtt >= parameter.StatusNetLatencyWarn:
		return severityDegrading
	}
	return severitySettled
}

// timeItem builds the time control indicator, present only when the simulation is
// off real time or a step request is pending
func (r *StatusBarRenderer) timeItem() (statusItem, bool) {
	if step := r.statStep.Load(); step > 0 {
		return r.pal.alarm.with(fmt.Sprintf(" STEP %d ", step)), true
	}

	speed := r.statSpeed.Load()
	if brk := r.statBreak.Load(); brk != "" && brk != "-" {
		return r.pal.alarm.with(fmt.Sprintf(" %sx>%s ", speed, brk)), true
	}
	if speed != "" && speed != "1" {
		return r.pal.speed.with(fmt.Sprintf(" %sx ", speed)), true
	}
	return statusItem{}, false
}

// getActiveStatusMessage returns the status message while its wall-clock
// lifetime lasts, and clears it once past: every message has one, capped at
// StatusMessageMaxDuration, so none of them stays on the bar for the run.
func (r *StatusBarRenderer) getActiveStatusMessage(now time.Time) string {
	msg := r.gameCtx.GetStatusMessage()
	if msg == "" {
		return ""
	}
	if expiry := r.gameCtx.GetStatusMessageExpiry(); expiry > 0 && now.UnixNano() > expiry {
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
