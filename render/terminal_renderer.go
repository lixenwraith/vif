package render

import (
	"fmt"
	"reflect"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

const (
	errorBlinkMs  = 500
	cursorBlinkMs = 500
)

// TerminalRenderer handles all terminal rendering
type TerminalRenderer struct {
	screen     tcell.Screen
	width      int
	height     int
	gameX      int
	gameY      int
	gameWidth  int
	gameHeight int
	lineNumWidth int
}

// NewTerminalRenderer creates a new terminal renderer
func NewTerminalRenderer(screen tcell.Screen, width, height, gameX, gameY, gameWidth, gameHeight, lineNumWidth int) *TerminalRenderer {
	return &TerminalRenderer{
		screen:       screen,
		width:        width,
		height:       height,
		gameX:        gameX,
		gameY:        gameY,
		gameWidth:    gameWidth,
		gameHeight:   gameHeight,
		lineNumWidth: lineNumWidth,
	}
}

// RenderFrame renders the entire game frame
func (r *TerminalRenderer) RenderFrame(ctx *engine.GameContext, decayAnimating bool, decayRow int) {
	r.screen.Clear()
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw heat meter
	r.drawHeatMeter(ctx.ScoreIncrement, defaultStyle)

	// Draw line numbers
	r.drawLineNumbers(ctx, defaultStyle)

	// Draw ping highlights (cursor row/column)
	pingColor := r.getPingColor(ctx.World, ctx.CursorX, ctx.CursorY)
	r.drawPingHighlights(ctx, pingColor, defaultStyle)

	// Draw characters
	r.drawCharacters(ctx.World, pingColor, defaultStyle)

	// Draw trails
	r.drawTrails(ctx.World, defaultStyle)

	// Draw decay animation if active
	if decayAnimating {
		r.drawDecayAnimation(ctx.World, decayRow, defaultStyle)
	}

	// Draw column indicators
	r.drawColumnIndicators(ctx, defaultStyle)

	// Draw status bar
	r.drawStatusBar(ctx, defaultStyle)

	// Draw cursor (if not in search mode)
	if !ctx.IsSearchMode() {
		r.drawCursor(ctx, defaultStyle)
	}

	r.screen.Show()
}

// drawHeatMeter draws the heat meter at the top
func (r *TerminalRenderer) drawHeatMeter(scoreIncrement int, defaultStyle tcell.Style) {
	indicatorWidth := 6
	heatBarWidth := r.width - indicatorWidth
	if heatBarWidth < 1 {
		heatBarWidth = 1
	}

	filledChars := scoreIncrement
	if filledChars > heatBarWidth {
		filledChars = heatBarWidth
	}

	// Draw the heat bar
	for x := 0; x < heatBarWidth; x++ {
		var style tcell.Style
		if x < filledChars {
			progress := float64(x+1) / float64(heatBarWidth)
			color := GetHeatMeterColor(progress)
			style = defaultStyle.Foreground(color)
		} else {
			style = defaultStyle.Foreground(tcell.NewRGBColor(0, 0, 0))
		}
		r.screen.SetContent(x, 0, '█', nil, style)
	}

	// Draw numeric indicator
	heatValue := scoreIncrement
	if heatValue > 9999 {
		heatValue = 9999
	}
	heatText := fmt.Sprintf("%4d", heatValue)
	heatNumStyle := defaultStyle.Foreground(tcell.NewRGBColor(0, 255, 255))
	startX := r.width - 4
	if startX < heatBarWidth+2 {
		startX = heatBarWidth + 2
	}
	for i, ch := range heatText {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, 0, ch, nil, heatNumStyle)
		}
	}

	// Draw spaces between bar and number
	for i := 0; i < 2 && heatBarWidth+i < startX; i++ {
		r.screen.SetContent(heatBarWidth+i, 0, ' ', nil, defaultStyle)
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
			if ctx.IsSearchMode() {
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

// getPingColor determines the ping highlight color based on cursor position
func (r *TerminalRenderer) getPingColor(world *engine.World, cursorX, cursorY int) tcell.Color {
	entity := world.GetEntityAtPosition(cursorX, cursorY)
	if entity != 0 {
		return tcell.NewRGBColor(5, 5, 5) // Almost black when on character
	}
	return RgbPingHighlight // Dark gray for whitespace
}

// drawPingHighlights draws the cursor row and column highlights
func (r *TerminalRenderer) drawPingHighlights(ctx *engine.GameContext, pingColor tcell.Color, defaultStyle tcell.Style) {
	pingStyle := defaultStyle.Background(pingColor)

	// Highlight the row
	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		screenY := r.gameY + ctx.CursorY
		if screenY >= 0 && screenY < r.gameHeight {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Highlight the column
	for y := 0; y < r.gameHeight; y++ {
		screenX := r.gameX + ctx.CursorX
		screenY := r.gameY + y
		if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
			r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Draw grid lines if ping is active
	if ctx.PingActive {
		r.drawPingGrid(ctx, pingStyle)
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals
func (r *TerminalRenderer) drawPingGrid(ctx *engine.GameContext, pingStyle tcell.Style) {
	// Vertical lines
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
				if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
		col = ctx.CursorX - offset
		if col >= 0 {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
	}

	// Horizontal lines
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
				if screenY >= 0 && screenY < r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
		row = ctx.CursorY - offset
		if row >= 0 {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= 0 && screenY < r.gameHeight {
					r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
				}
			}
		}
	}
}

// drawCharacters draws all character entities
func (r *TerminalRenderer) drawCharacters(world *engine.World, pingColor tcell.Color, defaultStyle tcell.Style) {
	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	entities := world.GetEntitiesWith(posType, charType)

	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		charComp, _ := world.GetComponent(entity, charType)
		char := charComp.(components.CharacterComponent)

		screenX := r.gameX + pos.X
		screenY := r.gameY + pos.Y

		if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
			style := char.Style
			// Add ping background if on cursor row/column
			// (We'd need cursor position from context for this - simplified for now)
			r.screen.SetContent(screenX, screenY, char.Rune, nil, style)
		}
	}
}

// drawTrails draws all trail particles
func (r *TerminalRenderer) drawTrails(world *engine.World, defaultStyle tcell.Style) {
	posType := reflect.TypeOf(components.PositionComponent{})
	trailType := reflect.TypeOf(components.TrailComponent{})

	entities := world.GetEntitiesWith(posType, trailType)

	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		trailComp, _ := world.GetComponent(entity, trailType)
		trail := trailComp.(components.TrailComponent)

		screenX := r.gameX + pos.X
		screenY := r.gameY + pos.Y

		if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
			intensity := int(trail.Intensity * 255)
			if intensity > 255 {
				intensity = 255
			}
			color := tcell.NewRGBColor(int32(intensity), int32(intensity), int32(intensity))
			r.screen.SetContent(screenX, screenY, '█', nil, defaultStyle.Foreground(color))
		}
	}
}

// drawDecayAnimation draws the decay animation row
func (r *TerminalRenderer) drawDecayAnimation(world *engine.World, decayRow int, defaultStyle tcell.Style) {
	if decayRow >= r.gameHeight {
		return
	}

	darkGrayBg := tcell.NewRGBColor(60, 60, 60)
	screenY := r.gameY + decayRow

	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})
	entities := world.GetEntitiesWith(posType, charType)

	// Build map of characters at this row
	charsAtRow := make(map[int]rune)
	stylesAtRow := make(map[int]tcell.Style)

	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		if pos.Y == decayRow {
			charComp, _ := world.GetComponent(entity, charType)
			char := charComp.(components.CharacterComponent)
			charsAtRow[pos.X] = char.Rune
			fg, _, _ := char.Style.Decompose()
			stylesAtRow[pos.X] = defaultStyle.Foreground(fg).Background(darkGrayBg)
		}
	}

	// Draw the decay row
	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		if screenX >= r.gameX && screenX < r.width && screenY >= 0 && screenY < r.gameHeight {
			if ch, ok := charsAtRow[x]; ok {
				r.screen.SetContent(screenX, screenY, ch, nil, stylesAtRow[x])
			} else {
				r.screen.SetContent(screenX, screenY, ' ', nil, defaultStyle.Background(darkGrayBg))
			}
		}
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
			if ctx.IsSearchMode() {
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
func (r *TerminalRenderer) drawStatusBar(ctx *engine.GameContext, defaultStyle tcell.Style) {
	statusY := r.gameY + r.gameHeight + 1

	// Clear status bar
	for x := 0; x < r.width; x++ {
		r.screen.SetContent(x, statusY, ' ', nil, defaultStyle)
	}

	// Draw mode indicator
	var modeText string
	var modeBgColor tcell.Color
	if ctx.IsSearchMode() {
		modeText = " SEARCH "
		modeBgColor = RgbCursorNormal
	} else if ctx.IsInsertMode() {
		modeText = " INSERT "
		modeBgColor = RgbModeInsertBg
	} else {
		modeText = " NORMAL "
		modeBgColor = RgbModeNormalBg
	}
	modeStyle := defaultStyle.Foreground(RgbStatusText).Background(modeBgColor)
	for i, ch := range modeText {
		if i < r.width {
			r.screen.SetContent(i, statusY, ch, nil, modeStyle)
		}
	}

	// Draw search text or status message
	statusStartX := len(modeText) + 1
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
	} else {
		statusStyle := defaultStyle.Foreground(RgbStatusBar)
		for i, ch := range ctx.StatusMessage {
			if statusStartX+i < r.width {
				r.screen.SetContent(statusStartX+i, statusY, ch, nil, statusStyle)
			}
		}
	}

	// Calculate positions and draw trail timer + score
	scoreText := fmt.Sprintf(" Score: %d ", ctx.Score)
	var trailText string
	var totalWidth int

	if ctx.TrailEnabled {
		remaining := ctx.TrailEndTime.Sub(time.Now()).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		trailText = fmt.Sprintf(" Trail: %.1fs ", remaining)
		totalWidth = len(trailText) + len(scoreText)
	} else {
		totalWidth = len(scoreText)
	}

	startX := r.width - totalWidth
	if startX < 0 {
		startX = 0
	}

	now := time.Now()

	// Draw trail timer if active (with purple background)
	if ctx.TrailEnabled {
		trailStyle := defaultStyle.Foreground(RgbStatusText).Background(RgbTrailBg)
		for i, ch := range trailText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, trailStyle)
			}
		}
		startX += len(trailText)
	}

	// Draw score (with yellow background, or blink color if active)
	if ctx.ScoreBlinkActive && now.Sub(ctx.ScoreBlinkTime).Milliseconds() < 200 {
		scoreStyle := defaultStyle.Foreground(RgbStatusText).Background(ctx.ScoreBlinkColor)
		for i, ch := range scoreText {
			if startX+i < r.width {
				r.screen.SetContent(startX+i, statusY, ch, nil, scoreStyle)
			}
		}
	} else {
		scoreStyle := defaultStyle.Foreground(RgbStatusText).Background(RgbScoreBg)
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

	if screenX < r.gameX || screenX >= r.width || screenY < 0 || screenY >= r.gameHeight {
		return
	}

	// Check for cursor error blink
	now := time.Now()
	if ctx.CursorError && now.Sub(ctx.CursorErrorTime).Milliseconds() > errorBlinkMs {
		ctx.CursorError = false
	}

	// Find character at cursor position
	entity := ctx.World.GetEntityAtPosition(ctx.CursorX, ctx.CursorY)
	var charAtCursor rune = ' '
	var charColor tcell.Color
	hasChar := false

	if entity != 0 {
		charType := reflect.TypeOf(components.CharacterComponent{})
		if charComp, ok := ctx.World.GetComponent(entity, charType); ok {
			char := charComp.(components.CharacterComponent)
			charAtCursor = char.Rune
			fg, _, _ := char.Style.Decompose()
			charColor = fg
			hasChar = true
		}
	}

	// Determine cursor colors
	var cursorBgColor tcell.Color
	var charFgColor tcell.Color

	if ctx.CursorError {
		cursorBgColor = RgbCursorError
		charFgColor = tcell.ColorBlack
	} else if hasChar {
		cursorBgColor = charColor
		charFgColor = tcell.ColorBlack
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
