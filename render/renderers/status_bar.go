package renderers

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// StatusBarRenderer draws the status bar at the bottom
type StatusBarRenderer struct {
	gameCtx *engine.GameContext

	// FPS Tracking
	frameCount    int
	lastFpsUpdate time.Time
	currentFps    int
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	return &StatusBarRenderer{
		gameCtx:       gameCtx,
		lastFpsUpdate: time.Now(),
	}
}

// Render implements SystemRenderer
func (s *StatusBarRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	// Get consistent snapshot of UI state
	uiSnapshot := s.gameCtx.GetUISnapshot()
	// FPS Calculation
	s.frameCount++
	now := time.Now()
	if now.Sub(s.lastFpsUpdate) >= time.Second {
		s.currentFps = s.frameCount
		s.frameCount = 0
		s.lastFpsUpdate = now
	}

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

	// Draw mode indicator
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

	// Draw last command indicator (if present)
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

	// Draw search text, command text, or status message
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

	clockNow := s.gameCtx.PausableClock.Now()

	// Priority 1: Decay (always important)
	decaySnapshot := s.gameCtx.State.ReadDecayState(ctx.GameTime)
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" Decay: %.1fs ", decaySnapshot.TimeUntil),
		fg:   render.RgbBlack,
		bg:   render.RgbDecayTimerBg,
	})

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
	if s.gameCtx.State.GetBoostEnabled() {
		remaining := s.gameCtx.State.GetBoostEndTime().Sub(clockNow).Seconds()
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
		gridRemaining := ping.GridTimer
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		rightItems = append(rightItems, statusItem{
			text: fmt.Sprintf(" Grid: %.1fs ", gridRemaining),
			fg:   render.RgbGridTimerFg,
			bg:   render.RgbBackground,
		})
	}

	// Priority 5-7: Metrics (lowest priority, dropped first)
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" APM: %d ", s.gameCtx.State.GetAPM()),
		fg:   render.RgbBlack,
		bg:   render.RgbApmBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" GT: %d ", s.gameCtx.State.GetGameTicks()),
		fg:   render.RgbBlack,
		bg:   render.RgbGtBg,
	})
	rightItems = append(rightItems, statusItem{
		text: fmt.Sprintf(" FPS: %d ", s.currentFps),
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