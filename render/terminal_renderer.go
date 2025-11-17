package render

import (
	"fmt"
	"reflect"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
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
func (r *TerminalRenderer) RenderFrame(ctx *engine.GameContext, decayAnimating bool, decayRow int, decayTimeRemaining float64) {
	r.screen.Clear()
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw heat meter
	r.drawHeatMeter(ctx.GetScoreIncrement(), defaultStyle)

	// Draw line numbers
	r.drawLineNumbers(ctx, defaultStyle)

	// Get ping color for later use
	pingColor := r.getPingColor(ctx.World, ctx.CursorX, ctx.CursorY, ctx)

	// Draw characters
	r.drawCharacters(ctx.World, pingColor, defaultStyle, ctx)

	// Draw decay animation if active
	if decayAnimating {
		r.drawDecayAnimation(ctx.World, decayRow, defaultStyle)
	}

	// Draw ping highlights (cursor row/column) and grid - AFTER characters/decay
	r.drawPingHighlights(ctx, pingColor, defaultStyle)

	// Draw column indicators
	r.drawColumnIndicators(ctx, defaultStyle)

	// Draw status bar
	r.drawStatusBar(ctx, defaultStyle, decayTimeRemaining)

	// Draw cursor (if not in search mode)
	if !ctx.IsSearchMode() {
		r.drawCursor(ctx, defaultStyle)
	}

	r.screen.Show()
}

// drawHeatMeter draws the heat meter at the top
func (r *TerminalRenderer) drawHeatMeter(scoreIncrement int, defaultStyle tcell.Style) {
	heatBarWidth := r.width - constants.HeatBarIndicatorWidth
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
	heatNumStyle := defaultStyle.Foreground(tcell.NewRGBColor(0, 255, 255)).Background(tcell.NewRGBColor(0, 0, 0))
	startX := r.width - 4
	if startX < heatBarWidth+2 {
		startX = heatBarWidth + 2
	}
	for i, ch := range heatText {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, 0, ch, nil, heatNumStyle)
		}
	}

	// Draw spaces between bar and number (with black background)
	blackBgStyle := defaultStyle.Background(tcell.NewRGBColor(0, 0, 0))
	for i := 0; i < 2 && heatBarWidth+i < startX; i++ {
		r.screen.SetContent(heatBarWidth+i, 0, ' ', nil, blackBgStyle)
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
	charType := reflect.TypeOf(components.CharacterComponent{})

	// Highlight the row
	for x := 0; x < r.gameWidth; x++ {
		screenX := r.gameX + x
		screenY := r.gameY + ctx.CursorY
		if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			// Check if there's a character entity at this position
			entity := ctx.World.GetEntityAtPosition(x, ctx.CursorY)
			hasEntity := false
			if entity != 0 {
				if _, ok := ctx.World.GetComponent(entity, charType); ok {
					hasEntity = true
				}
			}

			// Only draw space if there's no entity (entities already have ping background from their draw functions)
			if !hasEntity {
				r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
			}
		}
	}

	// Highlight the column
	for y := 0; y < r.gameHeight; y++ {
		screenX := r.gameX + ctx.CursorX
		screenY := r.gameY + y
		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
			// Check if there's a character entity at this position
			entity := ctx.World.GetEntityAtPosition(ctx.CursorX, y)
			hasEntity := false
			if entity != 0 {
				if _, ok := ctx.World.GetComponent(entity, charType); ok {
					hasEntity = true
				}
			}

			// Only draw space if there's no entity
			if !hasEntity {
				r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
			}
		}
	}

	// Draw grid lines if ping is active
	if ctx.GetPingActive() {
		r.drawPingGrid(ctx, pingStyle, charType)
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals
func (r *TerminalRenderer) drawPingGrid(ctx *engine.GameContext, pingStyle tcell.Style, charType reflect.Type) {
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
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					// Check if there's an entity at this position
					entity := ctx.World.GetEntityAtPosition(col, y)
					hasEntity := false
					if entity != 0 {
						if _, ok := ctx.World.GetComponent(entity, charType); ok {
							hasEntity = true
						}
					}

					// Only draw space if there's no entity
					if !hasEntity {
						r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
		}
		col = ctx.CursorX - offset
		if col >= 0 {
			for y := 0; y < r.gameHeight; y++ {
				screenX := r.gameX + col
				screenY := r.gameY + y
				if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					// Check if there's an entity at this position
					entity := ctx.World.GetEntityAtPosition(col, y)
					hasEntity := false
					if entity != 0 {
						if _, ok := ctx.World.GetComponent(entity, charType); ok {
							hasEntity = true
						}
					}

					// Only draw space if there's no entity
					if !hasEntity {
						r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
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
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					// Check if there's an entity at this position
					entity := ctx.World.GetEntityAtPosition(x, row)
					hasEntity := false
					if entity != 0 {
						if _, ok := ctx.World.GetComponent(entity, charType); ok {
							hasEntity = true
						}
					}

					// Only draw space if there's no entity
					if !hasEntity {
						r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
		}
		row = ctx.CursorY - offset
		if row >= 0 {
			for x := 0; x < r.gameWidth; x++ {
				screenX := r.gameX + x
				screenY := r.gameY + row
				if screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
					// Check if there's an entity at this position
					entity := ctx.World.GetEntityAtPosition(x, row)
					hasEntity := false
					if entity != 0 {
						if _, ok := ctx.World.GetComponent(entity, charType); ok {
							hasEntity = true
						}
					}

					// Only draw space if there's no entity
					if !hasEntity {
						r.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
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
		posComp, _ := world.GetComponent(entity, posType)
		pos := posComp.(components.PositionComponent)

		charComp, _ := world.GetComponent(entity, charType)
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

			r.screen.SetContent(screenX, screenY, char.Rune, nil, style)
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
		if screenX >= r.gameX && screenX < r.width && screenY >= r.gameY && screenY < r.gameY+r.gameHeight {
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
func (r *TerminalRenderer) drawStatusBar(ctx *engine.GameContext, defaultStyle tcell.Style, decayTimeRemaining float64) {
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

	// Draw last command indicator (if present)
	statusStartX := len(modeText)
	if ctx.LastCommand != "" && !ctx.IsSearchMode() {
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

	// Draw search text or status message
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

	// Calculate positions and draw timers + score (from right to left: Boost, Grid, Decay, Score)
	scoreText := fmt.Sprintf(" Score: %d ", ctx.GetScore())
	decayText := fmt.Sprintf(" Decay: %.1fs ", decayTimeRemaining)
	var boostText string
	var gridText string
	var totalWidth int

	if ctx.GetBoostEnabled() {
		remaining := ctx.GetBoostEndTime().Sub(time.Now()).Seconds()
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

	now := time.Now()

	// Draw boost timer if active (with pink background)
	if ctx.GetBoostEnabled() {
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

	// Draw decay timer (always visible, with red background and black text)
	decayStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(tcell.NewRGBColor(200, 50, 50))
	for i, ch := range decayText {
		if startX+i < r.width {
			r.screen.SetContent(startX+i, statusY, ch, nil, decayStyle)
		}
	}
	startX += len(decayText)

	// Draw score (with yellow background, or blink color if active)
	if ctx.GetScoreBlinkActive() && now.Sub(ctx.GetScoreBlinkTime()).Milliseconds() < 200 {
		scoreStyle := defaultStyle.Foreground(RgbStatusText).Background(ctx.GetScoreBlinkColor())
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

	if screenX < r.gameX || screenX >= r.width || screenY < r.gameY || screenY >= r.gameY+r.gameHeight {
		return
	}

	// Check for cursor error blink
	now := time.Now()
	if ctx.GetCursorError() && now.Sub(ctx.GetCursorErrorTime()).Milliseconds() > errorBlinkMs {
		ctx.SetCursorError(false)
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

	if ctx.GetCursorError() {
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
