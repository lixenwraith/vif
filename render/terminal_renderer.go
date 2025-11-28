package render

import (
	"fmt"
	"math"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TerminalRenderer handles all terminal rendering
type TerminalRenderer struct {
	screen              tcell.Screen
	width               int
	height              int
	gameX               int
	gameY               int
	gameWidth           int
	gameHeight          int
	lineNumWidth        int
	cleanerGradient     []tcell.Color
	materializeGradient []tcell.Color

	// FPS Tracking
	frameCount    int
	lastFpsUpdate time.Time
	currentFps    int
}

// NewTerminalRenderer creates a new terminal renderer
func NewTerminalRenderer(screen tcell.Screen, width, height, gameX, gameY, gameWidth, gameHeight, lineNumWidth int) *TerminalRenderer {
	r := &TerminalRenderer{
		screen:        screen,
		width:         width,
		height:        height,
		gameX:         gameX,
		gameY:         gameY,
		gameWidth:     gameWidth,
		gameHeight:    gameHeight,
		lineNumWidth:  lineNumWidth,
		lastFpsUpdate: time.Now(),
	}

	// Initialize gradient internally
	r.buildCleanerGradient()
	r.buildMaterializeGradient()

	return r
}

// buildCleanerGradient internal method to build gradient
func (r *TerminalRenderer) buildCleanerGradient() {
	length := constants.CleanerTrailLength

	r.cleanerGradient = make([]tcell.Color, length)
	red, green, blue := RgbCleanerBase.RGB()

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}

		rVal := int32(float64(red) * opacity)
		gVal := int32(float64(green) * opacity)
		bVal := int32(float64(blue) * opacity)

		r.cleanerGradient[i] = tcell.NewRGBColor(rVal, gVal, bVal)
	}
}

// buildMaterializeGradient internal method to build materialize gradient
func (r *TerminalRenderer) buildMaterializeGradient() {
	length := constants.MaterializeTrailLength

	r.materializeGradient = make([]tcell.Color, length)
	red, green, blue := RgbMaterialize.RGB()

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}

		rVal := int32(float64(red) * opacity)
		gVal := int32(float64(green) * opacity)
		bVal := int32(float64(blue) * opacity)

		r.materializeGradient[i] = tcell.NewRGBColor(rVal, gVal, bVal)
	}
}

// RenderFrame renders the entire game frame
func (r *TerminalRenderer) RenderFrame(ctx *engine.GameContext, decayAnimating bool, decayTimeRemaining float64) {
	// FPS Calculation
	r.frameCount++
	now := time.Now()
	if now.Sub(r.lastFpsUpdate) >= time.Second {
		r.currentFps = r.frameCount
		r.frameCount = 0
		r.lastFpsUpdate = now
	}

	// Increment frame counter and get frame number
	ctx.IncrementFrameNumber()

	r.screen.Clear()
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw heat meter
	r.drawHeatMeter(ctx.State.GetHeat(), defaultStyle)

	// Read cursor position
	cursorPos, ok := ctx.World.Positions.Get(ctx.CursorEntity)
	if !ok {
		// return
		panic(fmt.Errorf("cursor destroyed"))
	}

	// Draw line numbers
	r.drawLineNumbers(cursorPos.Y, ctx, defaultStyle)

	// Get ping color for later use
	pingColor := r.getPingColor(ctx.World, cursorPos.X, cursorPos.Y, ctx)

	// Draw ping highlights (cursor row/column) and grid - BEFORE characters
	r.drawPingHighlights(cursorPos.X, cursorPos.Y, ctx, pingColor, defaultStyle)

	// Draw Shields (blends with background/ping) - BEFORE characters
	r.drawShields(ctx.World)

	// Draw characters - will render over grid
	r.drawCharacters(ctx.World, cursorPos.X, cursorPos.Y, pingColor, defaultStyle, ctx)

	// Draw decay animation if active - AFTER ping highlights
	if decayAnimating {
		r.drawDecay(ctx.World, defaultStyle)
	}

	// Draw cleaners if active - AFTER decay animation
	r.drawCleaners(ctx.World, defaultStyle)

	// Draw removal flash effects - AFTER cleaners
	r.drawRemovalFlashes(ctx.World, ctx, defaultStyle)

	// Draw materialize animation if active - BEFORE drain
	r.drawMaterializers(ctx.World, defaultStyle)

	// Draw drain entity - AFTER removal flashes, BEFORE cursor
	r.drawDrain(ctx.World, defaultStyle)

	// Draw column indicators
	r.drawColumnIndicators(cursorPos.X, ctx, defaultStyle)

	// Draw status bar
	r.drawStatusBar(ctx, defaultStyle, decayTimeRemaining)

	// Draw cursor (if not in search or command mode)
	if !ctx.IsSearchMode() && !ctx.IsCommandMode() {
		r.drawCursor(cursorPos.X, cursorPos.Y, ctx, defaultStyle)
	}

	// Draw overlay on top of everything if active
	if ctx.IsOverlayMode() && ctx.OverlayActive {
		r.drawOverlay(ctx, defaultStyle)
	}

	r.screen.Show()
}

// drawHeatMeter draws the heat meter at the top as a 10-segment display
func (r *TerminalRenderer) drawHeatMeter(heat int, defaultStyle tcell.Style) {
	// Calculate display heat: map 0-MaxHeat to 0-10 segments
	displayHeat := int(float64(heat) / float64(constants.MaxHeat) * 10.0)
	if displayHeat > 10 {
		displayHeat = 10
	}
	if displayHeat < 0 {
		displayHeat = 0
	}

	// Draw 10-segment heat bar across full terminal width
	segmentWidth := float64(r.width) / 10.0
	for segment := 0; segment < 10; segment++ {
		// Calculate start and end positions for this segment
		segmentStart := int(float64(segment) * segmentWidth)
		segmentEnd := int(float64(segment+1) * segmentWidth)

		// Determine if this segment is filled
		isFilled := segment < displayHeat

		var style tcell.Style
		if isFilled {
			// Calculate progress for color gradient (0.0 to 1.0)
			progress := float64(segment+1) / 10.0
			color := GetHeatMeterColor(progress)
			style = defaultStyle.Foreground(color)
		} else {
			// Empty segment: black foreground
			style = defaultStyle.Foreground(RgbBlack)
		}

		// Draw all characters in this segment
		for x := segmentStart; x < segmentEnd && x < r.width; x++ {
			r.screen.SetContent(x, 0, '█', nil, style)
		}
	}
}

// drawLineNumbers draws relative line numbers
func (r *TerminalRenderer) drawLineNumbers(cursorY int, ctx *engine.GameContext, defaultStyle tcell.Style) {
	lineNumStyle := defaultStyle.Foreground(RgbLineNumbers)

	for y := 0; y < r.gameHeight; y++ {
		relativeNum := y - cursorY
		if relativeNum < 0 {
			relativeNum = -relativeNum
		}
		lineNum := fmt.Sprintf("%*d", r.lineNumWidth, relativeNum)

		var numStyle tcell.Style
		if relativeNum == 0 {
			if ctx.IsSearchMode() || ctx.IsCommandMode() {
				numStyle = defaultStyle.Foreground(RgbCursorNormal)
			} else {
				numStyle = defaultStyle.Foreground(tcell.ColorBlack).Background(RgbCursorNormal)
			}
		} else {
			numStyle = lineNumStyle
		}

		screenY := r.gameY + y
		for i, ch := range lineNum {
			r.screen.SetContent(i, screenY, ch, nil, numStyle)
		}
	}
}

// getPingColor determines the ping highlight color based on game mode
func (r *TerminalRenderer) getPingColor(world *engine.World, cursorX, cursorY int, ctx *engine.GameContext) tcell.Color {
	// INSERT mode: use whitespace color (dark gray)
	// NORMAL/SEARCH mode: use character color (almost black)
	if ctx.IsInsertMode() {
		return RgbPingHighlight // Dark gray (50,50,50)
	}
	return RgbPingNormal // Almost black for NORMAL and SEARCH modes
}

// drawPingHighlights draws the cursor row and column highlights
// Draws ONLY on cells with default/black background to avoid overwriting shield
func (r *TerminalRenderer) drawPingHighlights(cursorX, cursorY int, ctx *engine.GameContext, pingColor tcell.Color, defaultStyle tcell.Style) {
	pingStyle := defaultStyle.Background(pingColor)

	// Helper to draw ping only if cell has default background
	drawPingCell := func(screenX, screenY int) {
		_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := existingStyle.Decompose()
		// Only draw ping if background is default/black (don't overwrite shield)
		if bg == tcell.ColorDefault || bg == RgbBackground {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Highlight the row
	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		screenY := r.gameY + cursorY
		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			drawPingCell(screenX, screenY)
		}
	}

	// Highlight the column
	for y := 0; y < r.gameHeight; y++ {
		screenX := r.gameX + cursorX
		screenY := r.gameY + y
		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			drawPingCell(screenX, screenY)
		}
	}

	// Draw grid lines if ping is active
	if ctx.GetPingActive() {
		r.drawPingGrid(cursorX, cursorY, defaultStyle)
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals
// Only draws on cells with default background
func (r *TerminalRenderer) drawPingGrid(cursorX, cursorY int, pingStyle tcell.Style) {

	// Helper to draw ping only if cell has default background
	drawPingCell := func(screenX, screenY int) {
		_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := existingStyle.Decompose()
		if bg == tcell.ColorDefault || bg == RgbBackground {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Vertical lines
	for n := 1; ; n++ {
		offset := 5 * n
		col := cursorX + offset
		if col >= r.gameWidth && cursorX-offset < 0 {
			break
		}
		if col < r.gameWidth {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
		col = cursorX - offset
		if col >= 0 {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
	}

	// Horizontal lines
	for n := 1; ; n++ {
		offset := 5 * n
		row := cursorY + offset
		if row >= r.gameHeight && cursorY-offset < 0 {
			break
		}
		if row < r.gameHeight {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
		row = cursorY - offset
		if row >= 0 {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
	}
}

// drawCharacters draws all character entities
func (r *TerminalRenderer) drawCharacters(world *engine.World, cursorX, cursorY int, pingColor tcell.Color, defaultStyle tcell.Style, ctx *engine.GameContext) {
	// Query entities with both position and character
	entities := world.Query().
		With(world.Positions).
		With(world.Characters).
		Execute()

	// Direct iteration
	for _, entity := range entities {
		pos, okP := world.Positions.Get(entity)
		char, okC := world.Characters.Get(entity)
		if !okP || !okC {
			continue // Entity destroyed mid-iteration
		}

		screenX := r.gameX + pos.X
		screenY := r.gameY + pos.Y

		if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
			continue
		}

		// Get existing content to preserve background (e.g., Shield color)
		_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := existingStyle.Decompose()

		// Handle default background case
		if bg == tcell.ColorDefault {
			bg = RgbBackground
		}

		// Extract foreground from character's defined style
		fg, _, attrs := char.Style.Decompose()

		// Check if character is on a ping line (cursor row or column)
		onPingLine := (pos.Y == cursorY) || (pos.X == cursorX)

		// Also check if on ping grid lines when ping is active
		if !onPingLine && ctx.GetPingActive() {
			// Check if on vertical grid line (columns at ±5, ±10, ±15, etc.)
			deltaX := pos.X - cursorX
			if deltaX%5 == 0 && deltaX != 0 {
				onPingLine = true
			}
			// Check if on horizontal grid line (rows at ±5, ±10, ±15, etc.)
			deltaY := pos.Y - cursorY
			if deltaY%5 == 0 && deltaY != 0 {
				onPingLine = true
			}
		}

		// Logic to determine final background:
		// 1. Existing background takes priority (Shield, etc.)
		// 2. If existing is default black AND on ping line -> Ping Color
		// 3. Otherwise -> Preserve existing background
		finalBg := bg
		if onPingLine && bg == RgbBackground {
			finalBg = pingColor
		}

		finalStyle := defaultStyle.Foreground(fg).Background(finalBg).Attributes(attrs)

		// Apply dimming effect when paused
		if ctx.IsPaused.Load() {
			red, green, blue := fg.RGB()
			dimmedR := int32(float64(red) * 0.7)
			dimmedG := int32(float64(green) * 0.7)
			dimmedB := int32(float64(blue) * 0.7)
			dimmedFg := tcell.NewRGBColor(dimmedR, dimmedG, dimmedB)
			finalStyle = tcell.StyleDefault.Foreground(dimmedFg).Background(finalBg).Attributes(attrs)
		}

		r.screen.SetContent(screenX, screenY, char.Rune, nil, finalStyle)
	}
}

// drawDecay draws the falling decay characters
func (r *TerminalRenderer) drawDecay(world *engine.World, defaultStyle tcell.Style) {
	// Direct store access - single component query
	decayEntities := world.Decays.All()

	fgColor := RgbDecay

	for _, decayEntity := range decayEntities {
		decay, exists := world.Decays.Get(decayEntity)
		if !exists {
			continue
		}

		// Calculate screen position
		y := int(decay.YPosition)
		if y < 0 || y >= r.gameHeight {
			continue
		}

		screenX := r.gameX + decay.Column
		screenY := r.gameY + y

		if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
			continue
		}

		// Preserve existing background (e.g., Shield)
		_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := existingStyle.Decompose()

		if bg == tcell.ColorDefault {
			bg = RgbBackground
		}

		// Combine decay foreground with existing background
		decayStyle := defaultStyle.Foreground(fgColor).Background(bg)

		r.screen.SetContent(screenX, screenY, decay.Char, nil, decayStyle)
	}
}

// drawCleaners draws the cleaner animation using the trail of grid points.
// Cleaners are opaque and render ON TOP of everything (occlude shield).
func (r *TerminalRenderer) drawCleaners(world *engine.World, defaultStyle tcell.Style) {
	cleanerEntities := world.Cleaners.All()

	// Calculate gradient length
	gradientLen := len(r.cleanerGradient)
	maxGradientIdx := gradientLen - 1

	for _, cleanerEntity := range cleanerEntities {
		cleaner, ok := world.Cleaners.Get(cleanerEntity)
		if !ok {
			continue
		}

		// Deep copy trail to avoid race conditions during rendering
		trailCopy := make([]core.Point, len(cleaner.Trail))
		copy(trailCopy, cleaner.Trail)

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i, point := range trailCopy {
			// Bounds check both X and Y
			if point.X < 0 || point.X >= r.gameWidth || point.Y < 0 || point.Y >= r.gameHeight {
				continue
			}

			screenX := r.gameX + point.X
			screenY := r.gameY + point.Y

			// Use gradient based on index (clamped to valid range)
			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Apply color from gradient - cleaners are OPAQUE (solid background)
			color := r.cleanerGradient[gradientIndex]
			style := defaultStyle.Foreground(color).Background(RgbBackground)

			r.screen.SetContent(screenX, screenY, cleaner.Char, nil, style)
		}
	}
}

// drawMaterializers draws the materialize animation using the trail of grid points.
func (r *TerminalRenderer) drawMaterializers(world *engine.World, defaultStyle tcell.Style) {
	entities := world.Materializers.All()
	if len(entities) == 0 {
		return
	}

	// Pre-calculate gradient length outside loop for performance
	gradientLen := len(r.materializeGradient)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		// Deep copy trail to avoid race conditions during rendering
		trailCopy := make([]core.Point, len(mat.Trail))
		copy(trailCopy, mat.Trail)

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i, point := range trailCopy {
			// Skip if out of bounds
			if point.X < 0 || point.X >= r.gameWidth || point.Y < 0 || point.Y >= r.gameHeight {
				continue
			}

			screenX := r.gameX + point.X
			screenY := r.gameY + point.Y

			// Use pre-calculated gradient based on index (clamped to valid range)
			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Apply color from gradient
			color := r.materializeGradient[gradientIndex]

			// Preserve existing background (e.g., Shield color)
			_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
			_, bg, _ := existingStyle.Decompose()

			if bg == tcell.ColorDefault {
				bg = RgbBackground
			}

			style := defaultStyle.Foreground(color).Background(bg)
			r.screen.SetContent(screenX, screenY, mat.Char, nil, style)
		}
	}
}

// drawDrain draws the drain entity with transparent background
func (r *TerminalRenderer) drawDrain(world *engine.World, defaultStyle tcell.Style) {
	// Get all drains
	drainEntities := world.Drains.All()
	if len(drainEntities) == 0 {
		return
	}

	// Iterate on all drains
	for _, drainEntity := range drainEntities {
		// Get current position
		drainPos, ok := world.Positions.Get(drainEntity)
		if !ok {
			panic(fmt.Errorf("drain destroyed"))
		}

		// Calculate screen position
		screenX := r.gameX + drainPos.X
		screenY := r.gameY + drainPos.Y

		// Bounds check
		if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
			continue
		}

		// Draw the drain character with transparent background
		_, _, style, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := style.Decompose()

		if bg == tcell.ColorDefault {
			bg = RgbBackground
		}

		drainStyle := defaultStyle.Foreground(RgbDrain).Background(bg)
		r.screen.SetContent(screenX, screenY, constants.DrainChar, nil, drainStyle)
	}
}

// drawRemovalFlashes draws the brief flash effects when red characters are removed
func (r *TerminalRenderer) drawRemovalFlashes(world *engine.World, ctx *engine.GameContext, defaultStyle tcell.Style) {
	// Use world for direct store access
	entities := world.Flashes.All()

	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		// Check if position is in bounds
		if flash.Y < 0 || flash.Y >= r.gameHeight || flash.X < 0 || flash.X >= r.gameWidth {
			continue
		}

		// Calculate elapsed time for fade effect
		now := ctx.PausableClock.Now()
		elapsed := now.Sub(flash.StartTime).Milliseconds()

		// Skip if flash has expired (cleanup will handle removal)
		if elapsed >= int64(flash.Duration) {
			continue
		}

		// Calculate opacity based on elapsed time (fade from bright to transparent)
		opacity := 1.0 - (float64(elapsed) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Flash color: bright yellow-white fading to yellow
		red := int32(255)
		green := int32(255)
		blue := int32(200 * opacity)

		flashColor := tcell.NewRGBColor(red, green, blue)

		screenX := r.gameX + flash.X
		screenY := r.gameY + flash.Y

		// Preserve existing background
		_, _, existingStyle, _ := r.screen.GetContent(screenX, screenY)
		_, bg, _ := existingStyle.Decompose()

		if bg == tcell.ColorDefault {
			bg = RgbBackground
		}

		flashStyle := defaultStyle.Foreground(flashColor).Background(bg)
		r.screen.SetContent(screenX, screenY, flash.Char, nil, flashStyle)
	}
}

// drawColumnIndicators draws column position indicators
func (r *TerminalRenderer) drawColumnIndicators(cursorX int, ctx *engine.GameContext, defaultStyle tcell.Style) {
	indicatorY := r.gameY + r.gameHeight
	indicatorStyle := defaultStyle.Foreground(RgbColumnIndicator)

	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		relativeCol := x - cursorX
		var ch rune
		var colStyle tcell.Style

		if relativeCol == 0 {
			ch = '0'
			if ctx.IsSearchMode() || ctx.IsCommandMode() {
				colStyle = defaultStyle.Foreground(RgbCursorNormal)
			} else {
				colStyle = defaultStyle.Foreground(tcell.ColorBlack).Background(RgbCursorNormal)
			}
		} else {
			absRelative := relativeCol
			if absRelative < 0 {
				absRelative = -absRelative
			}
			if absRelative%10 == 0 {
				ch = rune('0' + (absRelative / 10 % 10))
			} else if absRelative%5 == 0 {
				ch = '|'
			} else {
				ch = ' '
			}
			colStyle = indicatorStyle
		}
		r.screen.SetContent(screenX, indicatorY, ch, nil, colStyle)
	}

	// Clear line number area for indicator row
	for i := 0; i < r.gameX; i++ {
		r.screen.SetContent(i, indicatorY, ' ', nil, defaultStyle)
	}
}

// drawStatusBar draws the status bar
func (r *TerminalRenderer) drawStatusBar(ctx *engine.GameContext, defaultStyle tcell.Style, decayTimeRemaining float64) {
	statusY := r.gameY + r.gameHeight + 1

	// Clear status bar
	for x := 0; x < r.width; x++ {
		r.screen.SetContent(x, statusY, ' ', nil, defaultStyle)
	}

	// Track current x position for status bar elements
	x := 0
	y := statusY

	// Audio mute indicator - always visible
	if ctx.AudioEngine != nil {
		var audioBgColor tcell.Color
		if ctx.AudioEngine.IsMuted() {
			audioBgColor = RgbAudioMuted // Bright red when muted
		} else {
			audioBgColor = RgbAudioUnmuted // Bright green when unmuted
		}
		audioStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(audioBgColor)
		for _, ch := range constants.AudioStr {
			r.screen.SetContent(x, y, ch, nil, audioStyle)
			x++
		}
	}

	// Draw mode indicator
	var modeText string
	var modeBgColor tcell.Color
	if ctx.IsSearchMode() {
		modeText = constants.ModeTextSearch
		modeBgColor = RgbModeSearchBg
	} else if ctx.IsCommandMode() {
		modeText = constants.ModeTextCommand
		modeBgColor = RgbModeCommandBg
	} else if ctx.IsInsertMode() {
		modeText = constants.ModeTextInsert
		modeBgColor = RgbModeInsertBg
	} else {
		modeText = constants.ModeTextNormal
		modeBgColor = RgbModeNormalBg
	}
	modeStyle := defaultStyle.Foreground(RgbStatusText).Background(modeBgColor)
	for i, ch := range modeText {
		if x+i < r.width {
			r.screen.SetContent(x+i, statusY, ch, nil, modeStyle)
		}
	}
	x += len(modeText)

	// Draw last command indicator (if present)
	statusStartX := x
	if ctx.LastCommand != "" && !ctx.IsSearchMode() && !ctx.IsCommandMode() {
		statusStartX++
		lastCmdStyle := defaultStyle.Foreground(tcell.ColorYellow)
		for i, ch := range ctx.LastCommand {
			if statusStartX+i < r.width {
				r.screen.SetContent(statusStartX+i, statusY, ch, nil, lastCmdStyle)
			}
		}
		statusStartX += len(ctx.LastCommand) + 1
	} else {
		statusStartX++
	}

	// Draw search text, command text, or status message
	if ctx.IsSearchMode() {
		searchStyle := defaultStyle.Foreground(tcell.ColorWhite)
		cursorStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbCursorNormal)

		for i, ch := range ctx.SearchText {
			if statusStartX+i < r.width {
				r.screen.SetContent(statusStartX+i, statusY, ch, nil, searchStyle)
			}
		}

		cursorX := statusStartX + len(ctx.SearchText)
		if cursorX < r.width {
			r.screen.SetContent(cursorX, statusY, ' ', nil, cursorStyle)
		}
	} else if ctx.IsCommandMode() {
		commandStyle := defaultStyle.Foreground(tcell.ColorWhite)
		cursorStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbModeCommandBg)

		for i, ch := range ctx.CommandText {
			if statusStartX+i < r.width {
				r.screen.SetContent(statusStartX+i, statusY, ch, nil, commandStyle)
			}
		}

		cursorX := statusStartX + len(ctx.CommandText)
		if cursorX < r.width {
			r.screen.SetContent(cursorX, statusY, ' ', nil, cursorStyle)
		}
	} else {
		statusStyle := defaultStyle.Foreground(RgbStatusBar)
		for i, ch := range ctx.StatusMessage {
			if statusStartX+i < r.width {
				r.screen.SetContent(statusStartX+i, statusY, ch, nil, statusStyle)
			}
		}
	}

	// --- RIGHT SIDE METRICS ---

	// Prepare strings for all right-aligned components
	energyText := fmt.Sprintf(" Energy: %d ", ctx.State.GetEnergy())
	decayText := fmt.Sprintf(" Decay: %.1fs ", decayTimeRemaining)

	var boostText string
	if ctx.State.GetBoostEnabled() {
		remaining := ctx.State.GetBoostEndTime().Sub(ctx.PausableClock.Now()).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		boostText = fmt.Sprintf(" Boost: %.1fs ", remaining)
	}

	var gridText string
	if ctx.GetPingActive() {
		gridRemaining := ctx.GetPingGridTimer()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		gridText = fmt.Sprintf(" Grid: %.1fs ", gridRemaining)
	}

	// New Metrics
	fpsStr := fmt.Sprintf(" FPS: %d ", r.currentFps)
	gtStr := fmt.Sprintf(" GT: %d ", ctx.State.GetGameTicks())
	apmStr := fmt.Sprintf(" APM: %d ", ctx.State.GetAPM())

	// Calculate total width to determine start position
	// Order from Left to Right: [Boost] [Grid] [Decay] [Energy] [APM] [GT] [FPS]
	totalWidth := len(boostText) + len(gridText) + len(decayText) + len(energyText) + len(apmStr) + len(gtStr) + len(fpsStr)

	startX := r.width - totalWidth
	// Clamp so we don't overwrite the left side if window is too small
	if startX < statusStartX {
		startX = statusStartX
	}

	now := ctx.PausableClock.Now()

	// 1. Boost
	if ctx.State.GetBoostEnabled() {
		boostStyle := defaultStyle.Foreground(RgbStatusText).Background(RgbBoostBg)
		for i, ch := range boostText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, boostStyle)
			}
		}
		startX += len(boostText)
	}

	// 2. Grid
	if ctx.GetPingActive() {
		gridStyle := defaultStyle.Foreground(tcell.ColorWhite)
		for i, ch := range gridText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, gridStyle)
			}
		}
		startX += len(gridText)
	}

	// 3. Decay
	decayStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbDecayTimerBg)
	for i, ch := range decayText {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, decayStyle)
		}
	}
	startX += len(decayText)

	// 4. Energy
	if ctx.State.GetEnergyBlinkActive() && now.Sub(ctx.State.GetEnergyBlinkTime()).Milliseconds() < 200 {
		typeCode := ctx.State.GetEnergyBlinkType()
		var energyStyle tcell.Style

		if typeCode == 0 {
			energyStyle = defaultStyle.Foreground(RgbCursorError).Background(RgbBlack)
		} else {
			var blinkColor tcell.Color
			switch typeCode {
			case 1:
				blinkColor = RgbEnergyBlinkBlue // Blue
			case 2:
				blinkColor = RgbEnergyBlinkGreen // Green
			case 3:
				blinkColor = RgbEnergyBlinkRed // Red
			case 4:
				blinkColor = RgbSequenceGold // Gold
			default:
				blinkColor = RgbEnergyBlinkWhite
			}
			energyStyle = defaultStyle.Foreground(RgbBlack).Background(blinkColor)
		}
		for i, ch := range energyText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, energyStyle)
			}
		}
	} else {
		energyStyle := defaultStyle.Foreground(RgbBlack).Background(RgbEnergyBg)
		for i, ch := range energyText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, energyStyle)
			}
		}
	}
	startX += len(energyText)

	// 5. APM
	apmStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbApmBg)
	for i, ch := range apmStr {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, apmStyle)
		}
	}
	startX += len(apmStr)

	// 6. GT
	gtStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbGtBg)
	for i, ch := range gtStr {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, gtStyle)
		}
	}
	startX += len(gtStr)

	// 7. FPS
	fpsStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbFpsBg)
	for i, ch := range fpsStr {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, fpsStyle)
		}
	}
}

// drawCursor draws the cursor handling complex overlaps with masked entities
func (r *TerminalRenderer) drawCursor(cursorX, cursorY int, ctx *engine.GameContext, defaultStyle tcell.Style) {
	screenX := r.gameX + cursorX
	screenY := r.gameY + cursorY

	// Bounds check
	if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
		return
	}

	// 1. Determine Default State (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor tcell.Color

	// Default background based on mode
	if ctx.IsInsertMode() {
		cursorBgColor = RgbCursorInsert
	} else {
		cursorBgColor = RgbCursorNormal
	}

	var charFgColor tcell.Color = tcell.ColorBlack

	// 2. Scan for Overlapping Entities

	// Stack allocation of buffer (size 15), NO GC overhead
	var entityBuf [engine.MaxEntitiesPerCell]engine.Entity

	// Copy data into stack buffer
	count := ctx.World.Positions.GetAllAtInto(cursorX, cursorY, entityBuf[:])

	// Create a slice view of our valid data
	entitiesAtCursor := entityBuf[:count]

	isDrain := false
	hasChar := false
	isNugget := false
	var charStyle tcell.Style

	for _, e := range entitiesAtCursor {
		if e == ctx.CursorEntity {
			continue
		}

		// Priority 1: Drain
		// Drain masks everything else
		if ctx.World.Drains.Has(e) {
			isDrain = true
			break
		}

		// Priority 2: Characters (Spawned, Gold, Nugget)
		if !hasChar {
			if charComp, ok := ctx.World.Characters.Get(e); ok {
				hasChar = true
				charAtCursor = charComp.Rune
				charStyle = charComp.Style
				if ctx.World.Nuggets.Has(e) {
					isNugget = true
				}
				// Do not break here; a Drain might still be in the list
			}
		}
	}

	// Priority 3: Decay (Lowest Priority)
	// Only checked if no standard character or drain is present
	// We scan manually because Decay entities are not fully integrated into PositionStore
	// Because of sub-pixel precision requirement of position
	// TODO: find a clever way around it for uniformity
	hasDecay := false
	if !isDrain && !hasChar {
		decayEntities := ctx.World.Decays.All()
		for _, e := range decayEntities {
			decay, ok := ctx.World.Decays.Get(e)
			if ok && decay.Column == cursorX && int(decay.YPosition) == cursorY {
				charAtCursor = decay.Char
				hasDecay = true
				break
			}
		}
	}

	// 3. Resolve Final Visuals based on priority
	if isDrain {
		// Drain overrides everything
		charAtCursor = constants.DrainChar
		cursorBgColor = RgbDrain
		charFgColor = tcell.ColorBlack
	} else if hasChar {
		// Character found (Blue/Green/Red/Gold/Nugget)
		// Inherit background from character's foreground color
		fg, _, _ := charStyle.Decompose()
		cursorBgColor = fg

		if isNugget {
			charFgColor = RgbNuggetDark
		} else {
			charFgColor = tcell.ColorBlack
		}
	} else if hasDecay {
		// Decay found on empty space
		cursorBgColor = RgbDecay
		charFgColor = tcell.ColorBlack
	}

	// 4. Error Flash Overlay (Absolute Highest Priority for Background)
	// Reads component directly to ensure flash works during pause
	cursorComp, ok := ctx.World.Cursors.Get(ctx.CursorEntity)
	if ok && cursorComp.ErrorFlashEnd > 0 {
		if ctx.PausableClock.Now().UnixNano() < cursorComp.ErrorFlashEnd {
			cursorBgColor = RgbCursorError
			charFgColor = tcell.ColorBlack
		}
	}

	// 5. Render
	style := defaultStyle.Foreground(charFgColor).Background(cursorBgColor)
	r.screen.SetContent(screenX, screenY, charAtCursor, nil, style)
}

// UpdateDimensions updates the renderer dimensions
func (r *TerminalRenderer) UpdateDimensions(width, height, gameX, gameY, gameWidth, gameHeight, lineNumWidth int) {
	r.width = width
	r.height = height
	r.gameX = gameX
	r.gameY = gameY
	r.gameWidth = gameWidth
	r.gameHeight = gameHeight
	r.lineNumWidth = lineNumWidth
}

// drawOverlay draws the modal overlay window with borders
func (r *TerminalRenderer) drawOverlay(ctx *engine.GameContext, defaultStyle tcell.Style) {
	// Calculate overlay dimensions (80% of screen)
	overlayWidth := int(float64(r.width) * constants.OverlayWidthPercent)
	overlayHeight := int(float64(r.height) * constants.OverlayHeightPercent)

	// Ensure minimum dimensions
	if overlayWidth < 20 {
		overlayWidth = 20
	}
	if overlayHeight < 10 {
		overlayHeight = 10
	}

	// Ensure it fits on screen
	if overlayWidth > r.width-4 {
		overlayWidth = r.width - 4
	}
	if overlayHeight > r.height-4 {
		overlayHeight = r.height - 4
	}

	// Calculate centered position
	startX := (r.width - overlayWidth) / 2
	startY := (r.height - overlayHeight) / 2

	// Define styles
	borderStyle := defaultStyle.Foreground(RgbOverlayBorder).Background(RgbOverlayBg)
	bgStyle := defaultStyle.Foreground(RgbOverlayText).Background(RgbOverlayBg)
	titleStyle := defaultStyle.Foreground(RgbOverlayTitle).Background(RgbOverlayBg)

	// Draw top border with title
	r.screen.SetContent(startX, startY, '╔', nil, borderStyle)
	for x := 1; x < overlayWidth-1; x++ {
		r.screen.SetContent(startX+x, startY, '═', nil, borderStyle)
	}
	r.screen.SetContent(startX+overlayWidth-1, startY, '╗', nil, borderStyle)

	// Draw title centered on top border
	if ctx.OverlayTitle != "" {
		titleX := startX + (overlayWidth-len(ctx.OverlayTitle))/2
		if titleX > startX {
			for i, ch := range ctx.OverlayTitle {
				if titleX+i < startX+overlayWidth-1 {
					r.screen.SetContent(titleX+i, startY, ch, nil, titleStyle)
				}
			}
		}
	}

	// Draw content area and side borders
	contentHeight := overlayHeight - 2
	contentWidth := overlayWidth - 2

	for y := 1; y < overlayHeight-1; y++ {
		// Left border
		r.screen.SetContent(startX, startY+y, '║', nil, borderStyle)

		// Fill background
		for x := 1; x < overlayWidth-1; x++ {
			r.screen.SetContent(startX+x, startY+y, ' ', nil, bgStyle)
		}

		// Right border
		r.screen.SetContent(startX+overlayWidth-1, startY+y, '║', nil, borderStyle)
	}

	// Draw bottom border
	r.screen.SetContent(startX, startY+overlayHeight-1, '╚', nil, borderStyle)
	for x := 1; x < overlayWidth-1; x++ {
		r.screen.SetContent(startX+x, startY+overlayHeight-1, '═', nil, borderStyle)
	}
	r.screen.SetContent(startX+overlayWidth-1, startY+overlayHeight-1, '╝', nil, borderStyle)

	// Draw content lines
	contentStartY := startY + 1 + constants.OverlayPaddingY
	contentStartX := startX + constants.OverlayPaddingX
	maxContentLines := contentHeight - 2*constants.OverlayPaddingY

	// Calculate visible range based on scroll
	startLine := ctx.OverlayScroll
	endLine := startLine + maxContentLines
	if endLine > len(ctx.OverlayContent) {
		endLine = len(ctx.OverlayContent)
	}

	// Draw visible content lines
	lineY := contentStartY
	for i := startLine; i < endLine && lineY < startY+overlayHeight-1-constants.OverlayPaddingY; i++ {
		line := ctx.OverlayContent[i]
		maxLineWidth := contentWidth - 2*constants.OverlayPaddingX

		// Truncate line if too long
		displayLine := line
		if len(line) > maxLineWidth {
			displayLine = line[:maxLineWidth]
		}

		// Draw the line
		for j, ch := range displayLine {
			if contentStartX+j < startX+overlayWidth-1-constants.OverlayPaddingX {
				r.screen.SetContent(contentStartX+j, lineY, ch, nil, bgStyle)
			}
		}
		lineY++
	}

	// Draw scroll indicator if content is scrollable
	if len(ctx.OverlayContent) > maxContentLines {
		scrollInfo := fmt.Sprintf("[%d/%d]", ctx.OverlayScroll+1, len(ctx.OverlayContent)-maxContentLines+1)
		scrollX := startX + overlayWidth - len(scrollInfo) - 2
		scrollY := startY + overlayHeight - 1
		for i, ch := range scrollInfo {
			r.screen.SetContent(scrollX+i, scrollY, ch, nil, borderStyle)
		}
	}
}

// blendColors blends two colors based on alpha.
// alpha is 0.0 (fully background) to 1.0 (fully foreground).
func (r *TerminalRenderer) blendColors(bg, fg tcell.Color, alpha float64) tcell.Color {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}

	// Safeguard: treat ColorDefault as RgbBackground to prevent negative RGB math
	if bg == tcell.ColorDefault {
		bg = RgbBackground
	}
	if fg == tcell.ColorDefault {
		fg = RgbBackground
	}

	r1, g1, b1 := bg.RGB()
	r2, g2, b2 := fg.RGB()

	rOut := int32(float64(r1)*(1.0-alpha) + float64(r2)*alpha)
	gOut := int32(float64(g1)*(1.0-alpha) + float64(g2)*alpha)
	bOut := int32(float64(b1)*(1.0-alpha) + float64(b2)*alpha)

	return tcell.NewRGBColor(rOut, gOut, bOut)
}

// drawShields renders active shields by blending their color with the existing background.
// It uses a geometric field function to calculate opacity per cell.
func (r *TerminalRenderer) drawShields(world *engine.World) {
	// DEBUG MODE: Temporarily bypasses blending for visual tuning.
	const useBlending = false // Toggle for debugging

	shields := world.Shields.All()

	for _, entity := range shields {
		shield, okS := world.Shields.Get(entity)
		pos, okP := world.Positions.Get(entity)

		if !okS || !okP || !shield.Active {
			continue
		}

		// Bounding box
		startX := int(float64(pos.X) - shield.RadiusX)
		endX := int(float64(pos.X) + shield.RadiusX)
		startY := int(float64(pos.Y) - shield.RadiusY)
		endY := int(float64(pos.Y) + shield.RadiusY)

		// Clamp to screen bounds
		if startX < 0 {
			startX = 0
		}
		if endX >= r.gameWidth {
			endX = r.gameWidth - 1
		}
		if startY < 0 {
			startY = 0
		}
		if endY >= r.gameHeight {
			endY = r.gameHeight - 1
		}

		// Pre-extract shield base color components
		shieldR, shieldG, shieldB := shield.Color.RGB()

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + y

				dx := float64(x - pos.X)
				dy := float64(y - pos.Y)

				// Elliptical distance: (dx/rx)^2 + (dy/ry)^2
				normalizedDistSq := (dx*dx)/(shield.RadiusX*shield.RadiusX) + (dy*dy)/(shield.RadiusY*shield.RadiusY)

				if normalizedDistSq > 1.0 {
					continue // Outside shield
				}

				dist := math.Sqrt(normalizedDistSq)

				// Alpha: 1.0 at center, 0.0 at edge, scaled by MaxOpacity
				alpha := (1.0 - dist) * shield.MaxOpacity

				// Get existing content
				mainc, combc, style, _ := r.screen.GetContent(screenX, screenY)
				fg, bg, attrs := style.Decompose()

				// Normalize ColorDefault
				if bg == tcell.ColorDefault {
					bg = RgbBackground
				}

				var newBg tcell.Color

				if useBlending {
					// Production: blend shield with existing background
					newBg = r.blendColors(bg, shield.Color, alpha)
				} else {
					// Debug: direct alpha-scaled shield color (no blending)
					// This shows the pure shield gradient without interference
					newBg = tcell.NewRGBColor(
						int32(float64(shieldR)*alpha),
						int32(float64(shieldG)*alpha),
						int32(float64(shieldB)*alpha),
					)
				}

				newStyle := tcell.StyleDefault.Foreground(fg).Background(newBg).Attributes(attrs)
				r.screen.SetContent(screenX, screenY, mainc, combc, newStyle)
			}
		}
	}
}

// dimColor reduces color intensity by factor (0.0-1.0)
// TODO: to be integrated with all component draws in pause, currently only characters dim
func (r *TerminalRenderer) dimColor(c tcell.Color, factor float64) tcell.Color {
	red, green, blue := c.RGB()
	return tcell.NewRGBColor(
		int32(float64(red)*factor),
		int32(float64(green)*factor),
		int32(float64(blue)*factor),
	)
}