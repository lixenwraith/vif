package renderers

import (
	"fmt"
	"time"

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

	statusY := ctx.GameY + ctx.GameHeight + 1

	// Clear status bar
	for x := 0; x < ctx.Width; x++ {
		buf.SetWithBg(x, statusY, ' ', render.RgbBackground, render.RgbBackground)
	}

	// Track current x position for status bar elements
	x := 0
	// y := statusY

	// Audio mute indicator - always visible
	if s.gameCtx.AudioEngine != nil {
		var audioBgColor render.RGB
		if s.gameCtx.AudioEngine.IsMuted() {
			audioBgColor = render.RgbAudioMuted // Bright red when muted
		} else {
			audioBgColor = render.RgbAudioUnmuted // Bright green when unmuted
		}
		for _, ch := range constants.AudioStr {
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
		if x < ctx.Width {
			buf.SetWithBg(x, statusY, ch, render.RgbStatusText, modeBgColor)
			x++
		}
	}

	// Draw last command indicator (if present)
	statusStartX := x
	if s.gameCtx.LastCommand != "" && !s.gameCtx.IsSearchMode() && !s.gameCtx.IsCommandMode() {
		statusStartX++
		for _, ch := range s.gameCtx.LastCommand {
			if statusStartX < ctx.Width {
				buf.SetWithBg(statusStartX, statusY, ch, render.RGB{255, 255, 0}, render.RgbBackground) // TODO: reference color var
				statusStartX++
			}
		}
		statusStartX++
	} else {
		statusStartX++
	}

	// Draw search text, command text, or status message
	// TODO: change to color var
	if s.gameCtx.IsSearchMode() {
		searchText := "/" + s.gameCtx.SearchText
		for _, ch := range searchText {
			if statusStartX < ctx.Width {
				buf.SetWithBg(statusStartX, statusY, ch, render.RGB{255, 255, 255}, render.RgbBackground)
				statusStartX++
			}
		}
	} else if s.gameCtx.IsCommandMode() {
		cmdText := ":" + s.gameCtx.CommandText
		for _, ch := range cmdText {
			if statusStartX < ctx.Width {
				buf.SetWithBg(statusStartX, statusY, ch, render.RGB{255, 255, 255}, render.RgbBackground)
				statusStartX++
			}
		}
	} else if s.gameCtx.StatusMessage != "" {
		for _, ch := range s.gameCtx.StatusMessage {
			if statusStartX < ctx.Width {
				buf.SetWithBg(statusStartX, statusY, ch, render.RGB{200, 200, 200}, render.RgbBackground)
				statusStartX++
			}
		}
	}

	// --- RIGHT SIDE METRICS ---
	clockNow := s.gameCtx.PausableClock.Now()

	// Prepare strings for all right-aligned components
	energyText := fmt.Sprintf(" Energy: %d ", s.gameCtx.State.GetEnergy())
	decaySnapshot := s.gameCtx.State.ReadDecayState(ctx.GameTime)
	decayText := fmt.Sprintf(" Decay: %.1fs ", decaySnapshot.TimeUntil)

	var boostText string
	if s.gameCtx.State.GetBoostEnabled() {
		remaining := s.gameCtx.State.GetBoostEndTime().Sub(clockNow).Seconds()
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

	// 1. Boost
	if s.gameCtx.State.GetBoostEnabled() {
		for _, ch := range boostText {
			if startX < ctx.Width {
				buf.SetWithBg(startX, statusY, ch, render.RgbStatusText, render.RgbBoostBg)
				startX++
			}
		}
	}

	// 2. Grid
	// TODO: var instead of color
	if s.gameCtx.GetPingActive() {
		for _, ch := range gridText {
			if startX < ctx.Width {
				buf.SetWithBg(startX, statusY, ch, render.RGB{255, 255, 255}, render.RgbBackground)
				startX++
			}
		}
	}

	// 3. Decay
	for _, ch := range decayText {
		if startX < ctx.Width {
			buf.SetWithBg(startX, statusY, ch, render.RgbBlack, render.RgbDecayTimerBg)
			startX++
		}
	}

	// 4. Energy
	// TODO: hardcode time to const
	if s.gameCtx.State.GetEnergyBlinkActive() && clockNow.Sub(s.gameCtx.State.GetEnergyBlinkTime()).Milliseconds() < 200 {
		typeCode := s.gameCtx.State.GetEnergyBlinkType()
		var energyFg, energyBg render.RGB

		if typeCode == 0 {
			energyFg = render.RgbCursorError
			energyBg = render.RgbBlack
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
			energyFg = render.RgbBlack
			energyBg = blinkColor
		}
		for _, ch := range energyText {
			if startX < ctx.Width {
				buf.SetWithBg(startX, statusY, ch, energyFg, energyBg)
				startX++
			}
		}
	} else {
		for _, ch := range energyText {
			if startX < ctx.Width {
				buf.SetWithBg(startX, statusY, ch, render.RgbBlack, render.RgbEnergyBg)
				startX++
			}
		}
	}

	// 5. APM
	for _, ch := range apmStr {
		if startX < ctx.Width {
			buf.SetWithBg(startX, statusY, ch, render.RgbBlack, render.RgbApmBg)
			startX++
		}
	}

	// 6. GT
	for _, ch := range gtStr {
		if startX < ctx.Width {
			buf.SetWithBg(startX, statusY, ch, render.RgbBlack, render.RgbGtBg)
			startX++
		}
	}

	// 7. FPS
	for _, ch := range fpsStr {
		if startX < ctx.Width {
			buf.SetWithBg(startX, statusY, ch, render.RgbBlack, render.RgbFpsBg)
			startX++
		}
	}
}