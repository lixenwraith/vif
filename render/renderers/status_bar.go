package renderers

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
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
	// FPS Calculation
	s.frameCount++
	now := time.Now()
	if now.Sub(s.lastFpsUpdate) >= time.Second {
		s.currentFps = s.frameCount
		s.frameCount = 0
		s.lastFpsUpdate = now
	}

	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)
	statusY := ctx.GameY + ctx.GameHeight + 1

	// Clear status bar
	for x := 0; x < ctx.Width; x++ {
		buf.Set(x, statusY, ' ', defaultStyle)
	}

	// Track current x position for status bar elements
	x := 0
	y := statusY

	// Audio mute indicator - always visible
	if s.gameCtx.AudioEngine != nil {
		var audioBgColor tcell.Color
		if s.gameCtx.AudioEngine.IsMuted() {
			audioBgColor = render.RgbAudioMuted // Bright red when muted
		} else {
			audioBgColor = render.RgbAudioUnmuted // Bright green when unmuted
		}
		audioStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(audioBgColor)
		for _, ch := range constants.AudioStr {
			buf.Set(x, y, ch, audioStyle)
			x++
		}
	}

	// Draw mode indicator
	var modeText string
	var modeBgColor tcell.Color
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
	modeStyle := defaultStyle.Foreground(render.RgbStatusText).Background(modeBgColor)
	for i, ch := range modeText {
		if x+i < ctx.Width {
			buf.Set(x+i, statusY, ch, modeStyle)
		}
	}
	x += len(modeText)

	// Draw last command indicator (if present)
	statusStartX := x
	if s.gameCtx.LastCommand != "" && !s.gameCtx.IsSearchMode() && !s.gameCtx.IsCommandMode() {
		statusStartX++
		lastCmdStyle := defaultStyle.Foreground(tcell.ColorYellow)
		for i, ch := range s.gameCtx.LastCommand {
			if statusStartX+i < ctx.Width {
				buf.Set(statusStartX+i, statusY, ch, lastCmdStyle)
			}
		}
		statusStartX += len(s.gameCtx.LastCommand) + 1
	} else {
		statusStartX++
	}

	// Draw search text, command text, or status message
	if s.gameCtx.IsSearchMode() {
		searchStyle := defaultStyle.Foreground(tcell.ColorWhite)
		cursorStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbCursorNormal)

		for i, ch := range s.gameCtx.SearchText {
			if statusStartX+i < ctx.Width {
				buf.Set(statusStartX+i, statusY, ch, searchStyle)
			}
		}

		cursorX := statusStartX + len(s.gameCtx.SearchText)
		if cursorX < ctx.Width {
			buf.Set(cursorX, statusY, ' ', cursorStyle)
		}
	} else if s.gameCtx.IsCommandMode() {
		commandStyle := defaultStyle.Foreground(tcell.ColorWhite)
		cursorStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbModeCommandBg)

		for i, ch := range s.gameCtx.CommandText {
			if statusStartX+i < ctx.Width {
				buf.Set(statusStartX+i, statusY, ch, commandStyle)
			}
		}

		cursorX := statusStartX + len(s.gameCtx.CommandText)
		if cursorX < ctx.Width {
			buf.Set(cursorX, statusY, ' ', cursorStyle)
		}
	} else {
		statusStyle := defaultStyle.Foreground(render.RgbStatusBar)
		for i, ch := range s.gameCtx.StatusMessage {
			if statusStartX+i < ctx.Width {
				buf.Set(statusStartX+i, statusY, ch, statusStyle)
			}
		}
	}

	// --- RIGHT SIDE METRICS ---

	// Prepare strings for all right-aligned components
	energyText := fmt.Sprintf(" Energy: %d ", s.gameCtx.State.GetEnergy())
	decaySnapshot := s.gameCtx.State.ReadDecayState(ctx.GameTime)
	decayText := fmt.Sprintf(" Decay: %.1fs ", decaySnapshot.TimeUntil)

	var boostText string
	if s.gameCtx.State.GetBoostEnabled() {
		remaining := s.gameCtx.State.GetBoostEndTime().Sub(s.gameCtx.PausableClock.Now()).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		boostText = fmt.Sprintf(" Boost: %.1fs ", remaining)
	}

	var gridText string
	if s.gameCtx.GetPingActive() {
		gridRemaining := s.gameCtx.GetPingGridTimer()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		gridText = fmt.Sprintf(" Grid: %.1fs ", gridRemaining)
	}

	// New Metrics
	fpsStr := fmt.Sprintf(" FPS: %d ", s.currentFps)
	gtStr := fmt.Sprintf(" GT: %d ", s.gameCtx.State.GetGameTicks())
	apmStr := fmt.Sprintf(" APM: %d ", s.gameCtx.State.GetAPM())

	// Calculate total width to determine start position
	// Order from Left to Right: [Boost] [Grid] [Decay] [Energy] [APM] [GT] [FPS]
	totalWidth := len(boostText) + len(gridText) + len(decayText) + len(energyText) + len(apmStr) + len(gtStr) + len(fpsStr)

	startX := ctx.Width - totalWidth
	// Clamp so we don't overwrite the left side if window is too small
	if startX < statusStartX {
		startX = statusStartX
	}

	clockNow := s.gameCtx.PausableClock.Now()

	// 1. Boost
	if s.gameCtx.State.GetBoostEnabled() {
		boostStyle := defaultStyle.Foreground(render.RgbStatusText).Background(render.RgbBoostBg)
		for i, ch := range boostText {
			if startX+i < ctx.Width {
				buf.Set(startX+i, statusY, ch, boostStyle)
			}
		}
		startX += len(boostText)
	}

	// 2. Grid
	if s.gameCtx.GetPingActive() {
		gridStyle := defaultStyle.Foreground(tcell.ColorWhite)
		for i, ch := range gridText {
			if startX+i < ctx.Width {
				buf.Set(startX+i, statusY, ch, gridStyle)
			}
		}
		startX += len(gridText)
	}

	// 3. Decay
	decayStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbDecayTimerBg)
	for i, ch := range decayText {
		if startX+i < ctx.Width {
			buf.Set(startX+i, statusY, ch, decayStyle)
		}
	}
	startX += len(decayText)

	// 4. Energy
	if s.gameCtx.State.GetEnergyBlinkActive() && clockNow.Sub(s.gameCtx.State.GetEnergyBlinkTime()).Milliseconds() < 200 {
		typeCode := s.gameCtx.State.GetEnergyBlinkType()
		var energyStyle tcell.Style

		if typeCode == 0 {
			energyStyle = defaultStyle.Foreground(render.RgbCursorError).Background(render.RgbBlack)
		} else {
			var blinkColor tcell.Color
			switch typeCode {
			case 1:
				blinkColor = render.RgbEnergyBlinkBlue // Blue
			case 2:
				blinkColor = render.RgbEnergyBlinkGreen // Green
			case 3:
				blinkColor = render.RgbEnergyBlinkRed // Red
			case 4:
				blinkColor = render.RgbSequenceGold // Gold
			default:
				blinkColor = render.RgbEnergyBlinkWhite
			}
			energyStyle = defaultStyle.Foreground(render.RgbBlack).Background(blinkColor)
		}
		for i, ch := range energyText {
			if startX+i < ctx.Width {
				buf.Set(startX+i, statusY, ch, energyStyle)
			}
		}
	} else {
		energyStyle := defaultStyle.Foreground(render.RgbBlack).Background(render.RgbEnergyBg)
		for i, ch := range energyText {
			if startX+i < ctx.Width {
				buf.Set(startX+i, statusY, ch, energyStyle)
			}
		}
	}
	startX += len(energyText)

	// 5. APM
	apmStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbApmBg)
	for i, ch := range apmStr {
		if startX+i < ctx.Width {
			buf.Set(startX+i, statusY, ch, apmStyle)
		}
	}
	startX += len(apmStr)

	// 6. GT
	gtStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbGtBg)
	for i, ch := range gtStr {
		if startX+i < ctx.Width {
			buf.Set(startX+i, statusY, ch, gtStyle)
		}
	}
	startX += len(gtStr)

	// 7. FPS
	fpsStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbFpsBg)
	for i, ch := range fpsStr {
		if startX+i < ctx.Width {
			buf.Set(startX+i, statusY, ch, fpsStyle)
		}
	}
}