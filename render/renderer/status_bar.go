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
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	statusReg := gameCtx.World.Resources.Status

	return &StatusBarRenderer{
		gameCtx: gameCtx,

		colorMode: gameCtx.World.Resources.Render.ColorMode,

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
	statusY := ctx.GameYOffset + ctx.GameHeight + 1

	// Bounds check: skip if status row outside screen
	if statusY >= ctx.ScreenHeight {
		return
	}

	// Clear status bar
	for x := 0; x < ctx.ScreenWidth; x++ {
		buf.SetWithBg(x, statusY, ' ', visual.RgbBackground, visual.RgbBackground)
	}

	// Track current x position for status bar elements
	x := 0

	// Audio state indicator - combined effects + music state
	if player := r.gameCtx.GetAudioPlayer(); player != nil {
		effectMuted := player.IsEffectMuted()
		musicMuted := player.IsMusicMuted()

		var audioBgColor terminal.RGB
		switch {
		case effectMuted && musicMuted:
			audioBgColor = visual.RgbAudioBothOff // Red
		case effectMuted && !musicMuted:
			audioBgColor = visual.RgbAudioMusicOnly // Yellow
		case !effectMuted && musicMuted:
			audioBgColor = visual.RgbAudioEffectsOnly // Green
		default:
			audioBgColor = visual.RgbAudioBothOn // Blue
		}
		for _, ch := range parameter.AudioStr {
			if x >= ctx.ScreenWidth {
				return // No space left
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

	// Last command indicator
	lastCommand := r.gameCtx.GetLastCommand()
	leftEndX := x
	if lastCommand != "" && !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode() {
		leftEndX++
		for _, ch := range lastCommand {
			if leftEndX >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, visual.RgbLastCommandText, visual.RgbBackground)
			leftEndX++
		}
		leftEndX++
	} else {
		leftEndX++
	}

	// Search, command, or status text
	searchText := r.gameCtx.GetSearchText()
	if r.gameCtx.IsSearchMode() {
		searchText = "/" + searchText
		for _, ch := range searchText {
			if leftEndX >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, visual.RgbSearchInputText, visual.RgbBackground)
			leftEndX++
		}
	} else if r.gameCtx.IsCommandMode() {
		commandText := r.gameCtx.GetCommandText()
		commandText = ":" + commandText
		for _, ch := range commandText {
			if leftEndX >= ctx.ScreenWidth {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, visual.RgbCommandInputText, visual.RgbBackground)
			leftEndX++
		}
	} else {
		statusMessage := r.gameCtx.GetStatusMessage()
		if statusMessage != "" {
			for _, ch := range statusMessage {
				if leftEndX >= ctx.ScreenWidth {
					return
				}
				buf.SetWithBg(leftEndX, statusY, ch, visual.RgbStatusMessageText, visual.RgbBackground)
				leftEndX++
			}
		}
	}

	// --- RIGHT SIDE METRICS ---
	// Build items in priority order (highest priority first)
	// Items are dropped from right (lowest priority) when space is limited

	type statusItem struct {
		text string
		fg   terminal.RGB
		bg   terminal.RGB
	}
	var rightItems []statusItem

	// Priority 1: FSM Phase (replaces Decay timer)
	phaseName := r.statFSMName.Load()
	if phaseName != "" {
		elapsed := time.Duration(r.statFSMElapsed.Load())
		maxDur := time.Duration(r.statFSMMaxDur.Load())
		phaseIdx := r.statFSMIndex.Load()
		phaseTotal := r.statFSMTotal.Load()

		// Calculate timer text
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

		// Calculate phase color from rainbow gradient
		phaseBg := render.RainbowIndexColor(phaseIdx, phaseTotal, visual.RgbModeNormalBg)

		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" %s: %.1fs ", phaseName, timerVal),
			fg:   visual.RgbBlack,
			bg:   phaseBg,
		})
	}

	// Priority 2: Energy
	energyComp, _ := r.gameCtx.World.Components.Energy.GetComponent(r.gameCtx.World.Resources.Cursor.Entity)
	energyVal := energyComp.Current
	energyText := fmt.Sprintf(" Energy: %d ", energyVal)

	// Base colors based on energy polarity
	var energyFg, energyBg terminal.RGB
	if energyVal < 0 {
		energyFg, energyBg = visual.RgbEnergyBg, visual.RgbBlack // white fg, black bg
	} else {
		energyFg, energyBg = visual.RgbBlack, visual.RgbEnergyBg // black fg, white bg
	}

	blinkRemaining := energyComp.BlinkRemaining
	if energyComp.BlinkActive && blinkRemaining > 0 {
		typeCode := energyComp.BlinkType
		if typeCode == 0 {
			// Error: red text, keep polarity background
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
	boost, boostOk := r.gameCtx.World.Components.Boost.GetComponent(r.gameCtx.World.Resources.Cursor.Entity)

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
	if ping, ok := r.gameCtx.World.Components.Ping.GetComponent(r.gameCtx.World.Resources.Cursor.Entity); ok && ping.GridActive {
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

	// Priority 5-7: Metrics from registry (direct atomic reads)
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

	// Priority 8: Color Mode Indicator
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

	// Calculate which items fit, dropping from end (lowest priority)
	availableWidth := ctx.ScreenWidth - leftEndX
	totalWidth := 0
	fitCount := 0
	for _, item := range rightItems {
		// utf8.RuneCountInString() for correct width calculation versus len()
		// e.g. "Energy: 100" vs "♫"
		itemWidth := utf8.RuneCountInString(item.text)
		if totalWidth+itemWidth <= availableWidth {
			totalWidth += itemWidth
			fitCount++
		} else {
			break
		}
	}

	// Render items that fit, right-aligned
	if fitCount > 0 {
		startX := ctx.ScreenWidth - totalWidth
		for i := 0; i < fitCount; i++ {
			item := rightItems[i]
			for _, ch := range item.text {
				buf.SetWithBg(startX, statusY, ch, item.fg, item.bg)
				startX++
			}
		}
	}
}