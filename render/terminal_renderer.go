package render

import (
	"fmt"
	"reflect"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

const (
	errorBlinkMs = 500
)

// CleanerSnapshot represents a thread-safe snapshot of cleaner data for rendering
type CleanerSnapshot struct {
	Row            int
	XPosition      float64
	TrailPositions []float64
	Char           rune
}

// CleanerSystemInterface provides the interface needed by the renderer
type CleanerSystemInterface interface {
	GetCleanerSnapshots() []CleanerSnapshot
}

// TerminalRenderer handles all terminal rendering
type TerminalRenderer struct {
	screen               tcell.Screen
	width                int
	height               int
	gameX                int
	gameY                int
	gameWidth            int
	gameHeight           int
	lineNumWidth         int
	cleanerSystem        CleanerSystemInterface
	cleanerSnapshots     []CleanerSnapshot
	cleanerSnapshotFrame int64 // Frame counter for cache invalidation
}

// NewTerminalRenderer creates a new terminal renderer
func NewTerminalRenderer(screen tcell.Screen, width, height, gameX, gameY, gameWidth, gameHeight, lineNumWidth int, cleanerSystem CleanerSystemInterface) *TerminalRenderer {
	return &TerminalRenderer{
		screen:               screen,
		width:                width,
		height:               height,
		gameX:                gameX,
		gameY:                gameY,
		gameWidth:            gameWidth,
		gameHeight:           gameHeight,
		lineNumWidth:         lineNumWidth,
		cleanerSystem:        cleanerSystem,
		cleanerSnapshots:     nil,
		cleanerSnapshotFrame: -1, // Initialize to -1 to force first update
	}
}

// RenderFrame renders the entire game frame
func (r *TerminalRenderer) RenderFrame(ctx *engine.GameContext, decayAnimating bool, decayRow int, decayTimeRemaining float64) {
	// Increment frame counter and get frame number
	frameNum := ctx.IncrementFrameNumber()

	// Update cleaner snapshot cache once per frame
	if r.cleanerSystem != nil && r.cleanerSnapshotFrame != frameNum {
		r.cleanerSnapshots = r.cleanerSystem.GetCleanerSnapshots()
		r.cleanerSnapshotFrame = frameNum
	}

	r.screen.Clear()
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw heat meter
	r.drawHeatMeter(ctx.State.GetHeat(), defaultStyle)

	// Draw line numbers
	r.drawLineNumbers(ctx, defaultStyle)

	// Get ping color for later use
	pingColor := r.getPingColor(ctx.World, ctx.CursorX, ctx.CursorY, ctx)

	// Draw ping highlights (cursor row/column) and grid - BEFORE characters
	r.drawPingHighlights(ctx, pingColor, defaultStyle)

	// Draw characters - will render over grid
	r.drawCharacters(ctx.World, pingColor, defaultStyle, ctx)

	// Draw falling decay animation if active - AFTER ping highlights
	if decayAnimating {
		r.drawFallingDecay(ctx.World, defaultStyle)
	}

	// Draw cleaners if active - AFTER decay animation
	r.drawCleaners(defaultStyle)

	// Draw removal flash effects - AFTER cleaners
	r.drawRemovalFlashes(ctx.World, ctx, defaultStyle)

	// Draw drain entity - AFTER removal flashes, BEFORE cursor
	r.drawDrain(ctx, defaultStyle)

	// Draw column indicators
	r.drawColumnIndicators(ctx, defaultStyle)

	// Draw status bar
	r.drawStatusBar(ctx, defaultStyle, decayTimeRemaining)

	// Draw cursor (if not in search or command mode)
	if !ctx.IsSearchMode() && !ctx.IsCommandMode() {
		r.drawCursor(ctx, defaultStyle)
	}

	r.screen.Show()
}

// drawHeatMeter draws the heat meter at the top as a 10-segment display
func (r *TerminalRenderer) drawHeatMeter(heat int, defaultStyle tcell.Style) {
	heatBarWidth := r.width
	if heatBarWidth < 1 {
		heatBarWidth = 1
	}

	// Calculate display heat: map 0-heatBarWidth to 0-10 segments
	displayHeat := int(float64(heat) / float64(heatBarWidth) * 10.0)
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
			style = defaultStyle.Foreground(tcell.NewRGBColor(0, 0, 0))
		}

		// Draw all characters in this segment
		for x := segmentStart; x < segmentEnd && x < r.width; x++ {
			r.screen.SetContent(x, 0, '█', nil, style)
		}
	}
}

// drawLineNumbers draws relative line numbers
func (r *TerminalRenderer) drawLineNumbers(ctx *engine.GameContext, defaultStyle tcell.Style) {
	lineNumStyle := defaultStyle.Foreground(RgbLineNumbers)

	for y := 0; y < r.gameHeight; y++ {
		relativeNum := y - ctx.CursorY
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
	return tcell.NewRGBColor(5, 5, 5) // Almost black for NORMAL and SEARCH modes
}

// drawPingHighlights draws the cursor row and column highlights
func (r *TerminalRenderer) drawPingHighlights(ctx *engine.GameContext, pingColor tcell.Color, defaultStyle tcell.Style) {
	pingStyle := defaultStyle.Background(pingColor)

	// Highlight the row - draw unconditionally (characters will render on top)
	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		screenY := r.gameY + ctx.CursorY
		if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Highlight the column - draw unconditionally (characters will render on top)
	for y := 0; y < r.gameHeight; y++ {
		screenX := r.gameX + ctx.CursorX
		screenY := r.gameY + y
		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Draw grid lines if ping is active
	if ctx.GetPingActive() {
		r.drawPingGrid(ctx, pingStyle)
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals
func (r *TerminalRenderer) drawPingGrid(ctx *engine.GameContext, pingStyle tcell.Style) {
	// Vertical lines - draw unconditionally (characters will render on top)
	for n := 1; ; n++ {
		offset := 5 * n
		col := ctx.CursorX + offset
		if col >= r.gameWidth && ctx.CursorX-offset < 0 {
			break
		}
		if col < r.gameWidth {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
		col = ctx.CursorX - offset
		if col >= 0 {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
	}

	// Horizontal lines - draw unconditionally (characters will render on top)
	for n := 1; ; n++ {
		offset := 5 * n
		row := ctx.CursorY + offset
		if row >= r.gameHeight && ctx.CursorY-offset < 0 {
			break
		}
		if row < r.gameHeight {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
		row = ctx.CursorY - offset
		if row >= 0 {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
	}
}

// drawCharacters draws all character entities
func (r *TerminalRenderer) drawCharacters(world *engine.World, pingColor tcell.Color, defaultStyle tcell.Style, ctx *engine.GameContext) {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := world.GetEntitiesWith(posType, charType)

	for _, entity := range entities {
		// Defensive: Check if entity still exists and has components
		// Entity could be destroyed between GetEntitiesWith and GetComponent calls
		posComp, ok := world.GetComponent(entity, posType)
		if !ok {
			continue // Entity was destroyed or component removed
		}
		pos := posComp.(components.PositionComponent)

		charComp, ok := world.GetComponent(entity, charType)
		if !ok {
			continue // Entity was destroyed or component removed
		}
		char := charComp.(components.CharacterComponent)

		screenX := r.gameX + pos.X
		screenY := r.gameY + pos.Y

		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			style := char.Style

			// Check if character is on a ping line (cursor row or column)
			onPingLine := (pos.Y == ctx.CursorY) || (pos.X == ctx.CursorX)

			// Also check if on ping grid lines when ping is active
			if !onPingLine && ctx.GetPingActive() {
				// Check if on vertical grid line (columns at ±5, ±10, ±15, etc.)
				deltaX := pos.X - ctx.CursorX
				if deltaX%5 == 0 && deltaX != 0 {
					onPingLine = true
				}
				// Check if on horizontal grid line (rows at ±5, ±10, ±15, etc.)
				deltaY := pos.Y - ctx.CursorY
				if deltaY%5 == 0 && deltaY != 0 {
					onPingLine = true
				}
			}

			// If on ping line, use ping background color
			if onPingLine {
				fg, _, _ := style.Decompose()
				style = defaultStyle.Foreground(fg).Background(pingColor)
			}

			// Apply dimming effect when paused
			if ctx.IsPaused.Load() {
				fg, bg, attrs := style.Decompose()
				// Extract RGB components and dim by 0.7
				r, g, b := fg.RGB()
				dimmedR := int32(float64(r) * 0.7)
				dimmedG := int32(float64(g) * 0.7)
				dimmedB := int32(float64(b) * 0.7)
				dimmedFg := tcell.NewRGBColor(dimmedR, dimmedG, dimmedB)
				style = tcell.StyleDefault.Foreground(dimmedFg).Background(bg).Attributes(attrs)
			}

			r.screen.SetContent(screenX, screenY, char.Rune, nil, style)
		}
	}
}

// drawFallingDecay draws the falling decay characters
func (r *TerminalRenderer) drawFallingDecay(world *engine.World, defaultStyle tcell.Style) {
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	entities := world.GetEntitiesWith(fallingType)

	// Style for falling characters: dark cyan foreground, default background
	fallingStyle := defaultStyle.Foreground(RgbDecayFalling)

	for _, entity := range entities {
		// Defensive: Check if entity still exists
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			continue // Entity was destroyed between GetEntitiesWith and GetComponent
		}
		fall := fallComp.(components.FallingDecayComponent)

		// Calculate screen position
		y := int(fall.YPosition)
		if y < 0 || y >= r.gameHeight {
			continue
		}

		screenX := r.gameX + fall.Column
		screenY := r.gameY + y

		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			// Draw the falling character with no background (preserves existing background)
			r.screen.SetContent(screenX, screenY, fall.Char, nil, fallingStyle)
		}
	}
}

// drawCleaners draws the cleaner animation with trail effects using pre-calculated gradients
// Uses cached snapshots from RenderFrame to ensure single snapshot per frame
func (r *TerminalRenderer) drawCleaners(defaultStyle tcell.Style) {
	// Skip if no cleaner system configured
	if r.cleanerSystem == nil {
		return
	}

	// Use cached snapshots from RenderFrame (already fetched once per frame)
	snapshots := r.cleanerSnapshots

	for _, cleaner := range snapshots {
		// Bounds check for cleaner row
		if cleaner.Row < 0 || cleaner.Row >= r.gameHeight {
			continue
		}
		screenY := r.gameY + cleaner.Row

		// Draw trail with fade effect using pre-calculated gradient (from oldest to newest)
		trailLen := len(cleaner.TrailPositions)
		for i := trailLen - 1; i >= 0; i-- {
			trailX := cleaner.TrailPositions[i]
			x := int(trailX + 0.5) // Round to nearest integer

			// Skip if out of bounds
			if x < 0 || x >= r.gameWidth {
				continue
			}

			screenX := r.gameX + x

			// Use pre-calculated gradient table for trail colors
			// Index 0 = newest/brightest, higher indices = older/fainter
			gradientIndex := i
			if gradientIndex >= len(CleanerTrailGradient) {
				gradientIndex = len(CleanerTrailGradient) - 1
			}

			// Only draw if color has sufficient opacity
			if gradientIndex < len(CleanerTrailGradient) {
				trailColor := CleanerTrailGradient[gradientIndex]
				trailStyle := defaultStyle.Foreground(trailColor)
				r.screen.SetContent(screenX, screenY, cleaner.Char, nil, trailStyle)
			}
		}

		// Draw the main cleaner block (bright yellow, on top of trail)
		x := int(cleaner.XPosition + 0.5) // Round to nearest integer
		if x >= 0 && x < r.gameWidth {
			screenX := r.gameX + x
			cleanerStyle := defaultStyle.Foreground(RgbSequenceGold)
			r.screen.SetContent(screenX, screenY, cleaner.Char, nil, cleanerStyle)
		}
	}
}

// drawDrain draws the drain entity with transparent background
func (r *TerminalRenderer) drawDrain(ctx *engine.GameContext, defaultStyle tcell.Style) {
	// Check if drain is active
	if !ctx.State.GetDrainActive() {
		return
	}

	// Get drain position from GameState atomics
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()

	// Calculate screen position
	screenX := r.gameX + drainX
	screenY := r.gameY + drainY

	// Bounds check
	if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
		return
	}

	// Get the current background at this position to inherit it
	// We read the cell content to preserve the background
	mainc, _, style, _ := r.screen.GetContent(screenX, screenY)
	_, bg, _ := style.Decompose()

	// Use drain character with light cyan foreground, inheriting background
	drainStyle := defaultStyle.Foreground(RgbDrain).Background(bg)

	// If there's no existing background (e.g., just been cleared), use default background
	if bg == tcell.ColorDefault {
		_, defaultBg, _ := defaultStyle.Decompose()
		drainStyle = defaultStyle.Foreground(RgbDrain).Background(defaultBg)
	}

	// Preserve the underlying character if it exists, otherwise use the drain character
	drainChar := constants.DrainChar
	if mainc != 0 && mainc != ' ' {
		// There's an underlying character, overlay drain on top
		drainChar = constants.DrainChar // Still use drain character to clearly show drain position
	}

	r.screen.SetContent(screenX, screenY, drainChar, nil, drainStyle)
}

// drawRemovalFlashes draws the brief flash effects when red characters are removed
func (r *TerminalRenderer) drawRemovalFlashes(world *engine.World, ctx *engine.GameContext, defaultStyle tcell.Style) {
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	entities := world.GetEntitiesWith(flashType)

	for _, entity := range entities {
		// Defensive: Check if entity still exists
		flashComp, ok := world.GetComponent(entity, flashType)
		if !ok || flashComp == nil {
			continue // Entity was destroyed between GetEntitiesWith and GetComponent
		}
		flash, ok := flashComp.(components.RemovalFlashComponent)
		if !ok {
			continue // Type assertion failed
		}

		// Check if position is in bounds
		if flash.Y < 0 || flash.Y >= r.gameHeight || flash.X < 0 || flash.X >= r.gameWidth {
			continue
		}

		// Calculate elapsed time for fade effect
		now := ctx.TimeProvider.Now()
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

		// Interpolate flash color from bright yellow-white to transparent
		// Start: RGB(255, 255, 200) -> End: RGB(255, 255, 0)
		red := int32(255)
		green := int32(255)
		blue := int32(200 * opacity) // Fade blue component for yellow transition

		flashColor := tcell.NewRGBColor(red, green, blue)
		flashStyle := defaultStyle.Foreground(flashColor)

		screenX := r.gameX + flash.X
		screenY := r.gameY + flash.Y

		r.screen.SetContent(screenX, screenY, flash.Char, nil, flashStyle)
	}
}

// drawColumnIndicators draws column position indicators
func (r *TerminalRenderer) drawColumnIndicators(ctx *engine.GameContext, defaultStyle tcell.Style) {
	indicatorY := r.gameY + r.gameHeight
	indicatorStyle := defaultStyle.Foreground(RgbColumnIndicator)

	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		relativeCol := x - ctx.CursorX
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

	// Audio mute indicator - always visible with bell character
	if ctx.AudioEngine != nil {
		bellStr := "🔔 "
		var bellBgColor tcell.Color
		if ctx.AudioEngine.IsMuted() {
			bellBgColor = tcell.NewRGBColor(255, 0, 0) // Bright red when muted
		} else {
			bellBgColor = tcell.NewRGBColor(100, 255, 100) // Bright green when unmuted
		}
		bellStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(bellBgColor)
		for i, ch := range bellStr {
			r.screen.SetContent(x+i, y, ch, nil, bellStyle)
		}
		x += len(bellStr)
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

	// Calculate positions and draw timers + score (from right to left: Boost, Grid, Decay, Score)
	scoreText := fmt.Sprintf(" Score: %d ", ctx.State.GetScore())
	decayText := fmt.Sprintf(" Decay: %.1fs ", decayTimeRemaining)
	var boostText string
	var gridText string
	var totalWidth int

	if ctx.State.GetBoostEnabled() {
		remaining := ctx.State.GetBoostEndTime().Sub(ctx.TimeProvider.Now()).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		boostText = fmt.Sprintf(" Boost: %.1fs ", remaining)
	}

	if ctx.GetPingActive() {
		gridRemaining := ctx.GetPingGridTimer()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		gridText = fmt.Sprintf(" Grid: %.1fs ", gridRemaining)
	}

	totalWidth = len(boostText) + len(gridText) + len(decayText) + len(scoreText)

	startX := r.width - totalWidth
	if startX < 0 {
		startX = 0
	}

	now := ctx.TimeProvider.Now()

	// Draw boost timer if active (with pink background)
	if ctx.State.GetBoostEnabled() {
		boostStyle := defaultStyle.Foreground(RgbStatusText).Background(RgbBoostBg)
		for i, ch := range boostText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, boostStyle)
			}
		}
		startX += len(boostText)
	}

	// Draw grid timer if active (with default background and white text)
	if ctx.GetPingActive() {
		gridStyle := defaultStyle.Foreground(tcell.ColorWhite)
		for i, ch := range gridText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, gridStyle)
			}
		}
		startX += len(gridText)
	}

	// Draw decay timer (always visible)
	decayStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(RgbDecayTimerBg)
	for i, ch := range decayText {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, decayStyle)
		}
	}
	startX += len(decayText)

	// Draw score (with white background, or blink color if active)
	if ctx.State.GetScoreBlinkActive() && now.Sub(ctx.State.GetScoreBlinkTime()).Milliseconds() < 200 {
		// Get score blink type and generate color
		typeCode := ctx.State.GetScoreBlinkType()
		var blinkColor tcell.Color
		// Map back to sequence type and generate bright background color
		switch typeCode {
		case 0: // Error state
			blinkColor = tcell.NewRGBColor(0, 0, 0) // Black for error
		case 1: // Blue
			blinkColor = tcell.NewRGBColor(160, 210, 255) // Brighter blue for background
		case 2: // Green
			blinkColor = tcell.NewRGBColor(120, 255, 120) // Brighter green for background
		case 3: // Red
			blinkColor = tcell.NewRGBColor(255, 140, 140) // Brighter red for background
		case 4: // Gold
			blinkColor = tcell.NewRGBColor(255, 255, 0) // Bright yellow for gold
		default:
			blinkColor = tcell.NewRGBColor(255, 255, 255) // Default to white
		}
		var scoreStyle tcell.Style
		if blinkColor == 0 {
			// Error state: black background with bright red text
			scoreStyle = defaultStyle.Foreground(tcell.NewRGBColor(255, 100, 100)).Background(tcell.NewRGBColor(0, 0, 0))
		} else {
			// Success state: character color background with black text
			scoreStyle = defaultStyle.Foreground(tcell.NewRGBColor(0, 0, 0)).Background(blinkColor)
		}
		for i, ch := range scoreText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, scoreStyle)
			}
		}
	} else {
		// Default: white background with black text
		scoreStyle := defaultStyle.Foreground(tcell.NewRGBColor(0, 0, 0)).Background(RgbScoreBg)
		for i, ch := range scoreText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, scoreStyle)
			}
		}
	}
}

// drawCursor draws the cursor
func (r *TerminalRenderer) drawCursor(ctx *engine.GameContext, defaultStyle tcell.Style) {
	screenX := r.gameX + ctx.CursorX
	screenY := r.gameY + ctx.CursorY

	if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
		return
	}

	// Check for cursor error blink
	now := ctx.TimeProvider.Now()
	if ctx.State.GetCursorError() && now.Sub(ctx.State.GetCursorErrorTime()).Milliseconds() > errorBlinkMs {
		ctx.State.SetCursorError(false)
	}

	// Find character at cursor position
	entity := ctx.World.GetEntityAtPosition(ctx.CursorX, ctx.CursorY)
	var charAtCursor rune = ' '
	var charColor tcell.Color
	hasChar := false
	isNugget := false

	if entity != 0 {
		charType := reflect.TypeOf(components.CharacterComponent{})
		if charComp, ok := ctx.World.GetComponent(entity, charType); ok {
			char := charComp.(components.CharacterComponent)
			charAtCursor = char.Rune
			fg, _, _ := char.Style.Decompose()
			charColor = fg
			hasChar = true
		}

		// Check if entity is a nugget
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		if _, ok := ctx.World.GetComponent(entity, nuggetType); ok {
			isNugget = true
		}
	}

	// Determine cursor colors
	var cursorBgColor tcell.Color
	var charFgColor tcell.Color

	if ctx.State.GetCursorError() {
		cursorBgColor = RgbCursorError
		charFgColor = tcell.ColorBlack
	} else if hasChar {
		cursorBgColor = charColor
		// Use dark foreground for nuggets to provide contrast with orange cursor
		if isNugget {
			charFgColor = RgbNuggetDark
		} else {
			charFgColor = tcell.ColorBlack
		}
	} else {
		if ctx.IsInsertMode() {
			cursorBgColor = RgbCursorInsert
		} else {
			cursorBgColor = RgbCursorNormal
		}
		charFgColor = tcell.ColorBlack
	}

	cursorStyle := defaultStyle.Foreground(charFgColor).Background(cursorBgColor)
	r.screen.SetContent(screenX, screenY, charAtCursor, nil, cursorStyle)
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