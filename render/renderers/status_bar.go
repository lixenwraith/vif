package renderers

import (
	"fmt"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx *engine.GameContext

	// Cached metric pointers (zero-lock reads)
	statFPS        *atomic.Int64
	statAPM        *atomic.Int64
	statTicks      *atomic.Int64
	statDecayTimer *atomic.Int64
	statDecayPhase *atomic.Int64
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	reg := engine.MustGetResource[*status.Registry](gameCtx.World.Resources)
	return &StatusBarRenderer{
		gameCtx:        gameCtx,
		statFPS:        reg.Ints.Get("engine.fps"),
		statAPM:        reg.Ints.Get("engine.apm"),
		statTicks:      reg.Ints.Get("engine.ticks"),
		statDecayTimer: reg.Ints.Get("decay.timer"),
		statDecayPhase: reg.Ints.Get("decay.phase"),
	}
}

// Render implements SystemRenderer
func (s *StatusBarRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	// Get consistent snapshot of UI state
	uiSnapshot := s.gameCtx.GetUISnapshot()

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
	if s.gameCtx.AudioEngine != nil {
		var audioBgColor render.RGB
		if s.gameCtx.AudioEngine.IsMuted() {
			audioBgColor = render.RgbAudioMuted
		} else {
			audioBgColor = render.RgbAudioUnmuted
		}
		for _, ch := range constants.AudioStr {
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
	if s.gameCtx.IsSearchMode() {
		modeText = constants.ModeTextSearch
		modeBgColor = render.RgbModeSearchBg
	} else if s.gameCtx.IsCommandMode() {
		modeText = constants.ModeTextCommand
		modeBgColor = render.RgbModeCommandBg
	} else if s.gameCtx.IsInsertMode() {
		modeText = constants.ModeTextInsert
		modeBgColor = render.RgbModeInsertBg
	} else {
		modeText = constants.ModeTextNormal
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
	if uiSnapshot.LastCommand != "" && !s.gameCtx.IsSearchMode() && !s.gameCtx.IsCommandMode() {
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
	if s.gameCtx.IsSearchMode() {
		searchText := "/" + uiSnapshot.SearchText
		for _, ch := range searchText {
			if leftEndX >= ctx.Width {
				return
			}
			buf.SetWithBg(leftEndX, statusY, ch, render.RgbSearchInputText, render.RgbBackground)
			leftEndX++
		}
	} else if s.gameCtx.IsCommandMode() {
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

	// TODO: unused, just keeping it warm for now
	// clockNow := s.gameCtx.PausableClock.Now()

	// Priority 1: Decay timer (show during DecayWait phase)
	decayPhase := s.statDecayPhase.Load()
	if decayPhase == int64(engine.PhaseDecayWait) {
		decayRemaining := time.Duration(s.statDecayTimer.Load())
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Decay: %.1fs ", decayRemaining.Seconds()),
			fg:   render.RgbBlack,
			bg:   render.RgbDecayTimerBg,
		})
	}

	// Priority 2: Energy
	energyComp, _ := s.gameCtx.World.Energies.Get(s.gameCtx.CursorEntity)
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
	boost, boostOk := s.gameCtx.World.Boosts.Get(s.gameCtx.CursorEntity)

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
	if ping, ok := s.gameCtx.World.Pings.Get(s.gameCtx.CursorEntity); ok && ping.GridActive {
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
		text: fmt.Sprintf(" APM: %d ", s.statAPM.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbApmBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" GT: %d ", s.statTicks.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbGtBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" FPS: %d ", s.statFPS.Load()),
		fg:   render.RgbBlack,
		bg:   render.RgbFpsBg,
	})

	// Priority 8: Color Mode Indicator
	var colorModeStr string
	if s.gameCtx.Terminal.ColorMode() == terminal.ColorModeTrueColor {
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