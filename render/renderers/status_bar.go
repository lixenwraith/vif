package renderers

import (
	"fmt"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx     *engine.GameContext
	pingStore   *engine.Store[component.PingComponent]
	energyStore *engine.Store[component.EnergyComponent]
	boostStore  *engine.Store[component.BoostComponent]

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
	reg := engine.MustGetResource[*status.Registry](gameCtx.World.Resources)
	cfg := engine.MustGetResource[*engine.RenderConfig](gameCtx.World.Resources)
	return &StatusBarRenderer{
		gameCtx:     gameCtx,
		pingStore:   engine.GetStore[component.PingComponent](gameCtx.World),
		energyStore: engine.GetStore[component.EnergyComponent](gameCtx.World),
		boostStore:  engine.GetStore[component.BoostComponent](gameCtx.World),

		colorMode: cfg.ColorMode,

		statFPS:        reg.Ints.Get("engine.fps"),
		statAPM:        reg.Ints.Get("engine.apm"),
		statTicks:      reg.Ints.Get("engine.ticks"),
		statPhase:      reg.Ints.Get("engine.phase"),
		statDecayTimer: reg.Ints.Get("decay.timer"),

		statFSMName:    reg.Strings.Get("fsm.state"),
		statFSMElapsed: reg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  reg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   reg.Ints.Get("fsm.state_index"),
		statFSMTotal:   reg.Ints.Get("fsm.state_count"),
	}
}

// Render implements SystemRenderer
func (r *StatusBarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	// Get consistent snapshot of UI state
	uiSnapshot := r.gameCtx.GetUISnapshot()

	statusY := ctx.GameY + ctx.GameHeight + 1

	// Bounds check: skip if status row outside screen
	if statusY >= ctx.Height {
		return
	}

	// Clear status bar
	for x := 0; x < ctx.Width; x++ {
		buf.SetWithBg(x, statusY, ' ', render.RgbBackground, render.RgbBackground)
	}

	// Track current x position for status bar elements
	x := 0

	// Audio mute indicator - always visible
	if player := r.gameCtx.GetAudioPlayer(); player != nil {
		var audioBgColor render.RGB
		if player.IsMuted() {
			audioBgColor = render.RgbAudioMuted
		} else {
			audioBgColor = render.RgbAudioUnmuted
		}
		for _, ch := range constant.AudioStr {
			if x >= ctx.Width {
				return // No space left
			}
			buf.SetWithBg(x, statusY, ch, render.RgbBlack, audioBgColor)
			x++
		}
	}

	// Mode indicator
	var modeText string
	var modeBgColor render.RGB
	if r.gameCtx.IsSearchMode() {
		modeText = constant.ModeTextSearch
		modeBgColor = render.RgbModeSearchBg
	} else if r.gameCtx.IsCommandMode() {
		modeText = constant.ModeTextCommand
		modeBgColor = render.RgbModeCommandBg
	} else if r.gameCtx.IsInsertMode() {
		modeText = constant.ModeTextInsert
		modeBgColor = render.RgbModeInsertBg
	} else {
		modeText = constant.ModeTextNormal
		modeBgColor = render.RgbModeNormalBg
	}
	for _, ch := range modeText {
		if x >= ctx.Width {
			return
		}
		buf.SetWithBg(x, statusY, ch, render.RgbStatusText, modeBgColor)
		x++
	}

	// Last command indicator
	leftEndX := x
	if uiSnapshot.LastCommand != "" && !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode() {
		leftEndX++
		for _, ch := range uiSnapshot.LastCommand {
			if leftEndX >= ctx.Width {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, render.RgbLastCommandText, render.RgbBackground)
			leftEndX++
		}
		leftEndX++
	} else {
		leftEndX++
	}

	// Search, command, or status text
	if r.gameCtx.IsSearchMode() {
		searchText := "/" + uiSnapshot.SearchText
		for _, ch := range searchText {
			if leftEndX >= ctx.Width {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, render.RgbSearchInputText, render.RgbBackground)
			leftEndX++
		}
	} else if r.gameCtx.IsCommandMode() {
		cmdText := ":" + uiSnapshot.CommandText
		for _, ch := range cmdText {
			if leftEndX >= ctx.Width {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, render.RgbCommandInputText, render.RgbBackground)
			leftEndX++
		}
	} else if uiSnapshot.StatusMessage != "" {
		for _, ch := range uiSnapshot.StatusMessage {
			if leftEndX >= ctx.Width {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, render.RgbStatusMessageText, render.RgbBackground)
			leftEndX++
		}
	}

	// --- RIGHT SIDE METRICS ---
	// Build items in priority order (highest priority first)
	// Items are dropped from right (lowest priority) when space is limited

	type statusItem struct {
		text string
		fg   render.RGB
		bg   render.RGB
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
		phaseBg := phaseColor(phaseIdx, phaseTotal)

		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" %s: %.1fs ", phaseName, timerVal),
			fg:   render.RgbBlack,
			bg:   phaseBg,
		})
	}

	// Priority 2: Energy
	energyComp, _ := r.energyStore.Get(r.gameCtx.CursorEntity)
	energyVal := energyComp.Current.Load()
	energyText := fmt.Sprintf(" Energy: %d ", energyVal)
	energyFg, energyBg := render.RgbBlack, render.RgbEnergyBg
	blinkRemaining := energyComp.BlinkRemaining.Load()
	if energyComp.BlinkActive.Load() && blinkRemaining > 0 {
		typeCode := energyComp.BlinkType.Load()
		if typeCode == 0 {
			energyFg, energyBg = render.RgbCursorError, render.RgbBlack
		} else {
			var blinkColor render.RGB
			switch typeCode {
			case 1:
				blinkColor = render.RgbEnergyBlinkBlue
			case 2:
				blinkColor = render.RgbEnergyBlinkGreen
			case 3:
				blinkColor = render.RgbEnergyBlinkRed
			case 4:
				blinkColor = render.RgbSequenceGold
			default:
				blinkColor = render.RgbEnergyBlinkWhite
			}
			energyFg, energyBg = render.RgbBlack, blinkColor
		}
	}
	rightItems = append(rightItems, statusItem{text: energyText, fg: energyFg, bg: energyBg})

	// Priority 3: Boost (conditional)
	boost, boostOk := r.boostStore.Get(r.gameCtx.CursorEntity)

	if boostOk && boost.Active {
		remaining := boost.Remaining.Seconds()
		if remaining < 0 {
			remaining = 0
		}
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Boost: %.1fs ", remaining),
			fg:   render.RgbStatusText,
			bg:   render.RgbBoostBg,
		})
	}

	// Priority 4: Grid (conditional)
	if ping, ok := r.pingStore.Get(r.gameCtx.CursorEntity); ok && ping.GridActive {
		gridRemaining := ping.GridRemaining.Seconds()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Grid: %.1fs ", gridRemaining),
			fg:   render.RgbGridTimerFg,
			bg:   render.RgbBackground,
		})
	}

	// Priority 5-7: Metrics from registry (direct atomic reads)
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" APM: %d ", r.statAPM.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbApmBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" GT: %d ", r.statTicks.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbGtBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" FPS: %d ", r.statFPS.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbFpsBg,
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
		fg:   render.RgbBlack,
		bg:   render.RgbColorModeIndicator,
	})

	// Calculate which items fit, dropping from end (lowest priority)
	availableWidth := ctx.Width - leftEndX
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
		startX := ctx.Width - totalWidth
		for i := 0; i < fitCount; i++ {
			item := rightItems[i]
			for _, ch := range item.text {
				buf.SetWithBg(startX, statusY, ch, item.fg, item.bg)
				startX++
			}
		}
	}
}

// phaseColor returns rainbow-interpolated color for phase index
// Uses offset range (40-220) to avoid dark extremes for text readability
func phaseColor(index, total int64) render.RGB {
	if total <= 1 {
		return render.RgbModeNormalBg
	}
	// Map to 40-220 range to skip dark red/purple extremes
	// TODO: may be expensive
	lutIdx := 40 + int((index*180)/(total-1))
	if lutIdx > 220 {
		lutIdx = 220
	}
	return render.HeatGradientLUT[lutIdx]
}