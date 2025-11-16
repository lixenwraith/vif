// FILE: main.go
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	trailLength      = 8
	trailDecayMs     = 50
	errorBlinkMs     = 500
	cursorBlinkMs    = 500
	characterSpawnMs = 2000
)

// Color palette - custom RGB colors for consistent theming
const (
	// Character colors
	colorCharLowercase = tcell.Color(16) // Will be set via RGB
	colorCharUppercase = tcell.Color(17)
	colorCharDigit     = tcell.Color(18)
	colorCharSpecial   = tcell.Color(19)

	// UI element colors
	colorLineNumbers   = tcell.Color(20)
	colorStatusBar     = tcell.Color(21)
	colorColumnIndicator = tcell.Color(22)

	// Highlight colors
	colorPingHighlight = tcell.Color(23)
	colorCursorNormal  = tcell.Color(24)
	colorCursorError   = tcell.Color(25)
	colorTrailGray     = tcell.Color(26)
)

var (
	// RGB color definitions for sequences - dark/normal/bright levels
	rgbSequenceGreenDark     = tcell.NewRGBColor(0, 130, 0)      // Dark Green
	rgbSequenceGreenNormal   = tcell.NewRGBColor(0, 200, 0)      // Normal Green
	rgbSequenceGreenBright   = tcell.NewRGBColor(50, 255, 50)    // Bright Green

	rgbSequenceRedDark       = tcell.NewRGBColor(180, 50, 50)    // Dark Red
	rgbSequenceRedNormal     = tcell.NewRGBColor(255, 80, 80)    // Normal Red
	rgbSequenceRedBright     = tcell.NewRGBColor(255, 120, 120)  // Bright Red

	rgbSequenceBlueDark      = tcell.NewRGBColor(60, 100, 200)   // Dark Blue
	rgbSequenceBlueNormal    = tcell.NewRGBColor(100, 150, 255)  // Normal Blue
	rgbSequenceBlueBright    = tcell.NewRGBColor(140, 190, 255)  // Bright Blue

	rgbLineNumbers           = tcell.NewRGBColor(180, 180, 180)  // Brighter gray
	rgbStatusBar             = tcell.NewRGBColor(255, 255, 255)  // White
	rgbColumnIndicator       = tcell.NewRGBColor(180, 180, 180)  // Brighter gray
	rgbBackground            = tcell.NewRGBColor(26, 27, 38)     // Tokyo Night background

	rgbPingHighlight         = tcell.NewRGBColor(50, 50, 50)     // Very dark gray for ping
	rgbPingOrange            = tcell.NewRGBColor(60, 40, 0)      // Very dark orange for ping on whitespace
	rgbPingGreen             = tcell.NewRGBColor(0, 40, 0)       // Very dark green for ping on green char
	rgbPingRed               = tcell.NewRGBColor(50, 15, 15)     // Very dark red for ping on red char
	rgbPingBlue              = tcell.NewRGBColor(15, 25, 50)     // Very dark blue for ping on blue char
	rgbCursorNormal          = tcell.NewRGBColor(255, 165, 0)    // Orange for normal mode
	rgbCursorInsert          = tcell.NewRGBColor(255, 255, 255)  // Bright white for insert mode
	rgbCursorError           = tcell.NewRGBColor(255, 80, 80)    // Red
	rgbTrailGray             = tcell.NewRGBColor(200, 200, 200)  // Light gray base

	// Status bar backgrounds
	rgbModeNormalBg          = tcell.NewRGBColor(135, 206, 250)  // Light sky blue
	rgbModeInsertBg          = tcell.NewRGBColor(144, 238, 144)  // Light grass green
	rgbScoreBg               = tcell.NewRGBColor(255, 223, 100)  // Light golden yellow
	rgbStatusText            = tcell.NewRGBColor(0, 0, 0)        // Dark text for status
)

type SequenceType int

const (
	SequenceGreen SequenceType = iota // Positive scoring
	SequenceRed                        // Negative scoring
	SequenceBlue                       // Positive scoring + trail effect
)

type SequenceLevel int

const (
	LevelDark   SequenceLevel = iota // x1 multiplier
	LevelNormal                      // x2 multiplier
	LevelBright                      // x3 multiplier
)

type Character struct {
	rune         rune
	x, y         int
	style        tcell.Style
	sequenceID   int           // All chars in same sequence have same ID
	seqIndex     int           // Position in the sequence (0-based)
	sequenceType SequenceType  // Type of sequence (Green, Red, Blue)
	level        SequenceLevel // Sequence level (Dark, Normal, Bright)
}

type Trail struct {
	x, y      int
	intensity float64
	timestamp time.Time
}

type Game struct {
	screen        tcell.Screen
	width, height int

	// Game area (excluding line numbers and status bars)
	gameX, gameY           int
	gameWidth, gameHeight  int
	lineNumWidth           int

	// Cursor state (in game area coordinates)
	cursorX, cursorY int
	cursorVisible    bool
	cursorError      bool
	cursorErrorTime  time.Time
	cursorBlinkTime  time.Time

	// Mode state (normal/insert/search)
	insertMode       bool
	searchMode       bool
	searchText       string

	// Trail effect
	trails        []Trail
	trailEnabled  bool
	trailTimer    *time.Timer
	trailEndTime  time.Time

	// Characters on screen
	characters []Character
	lastSpawn  time.Time
	nextSeqID  int // Counter for unique sequence IDs

	// Score tracking
	score            int
	scoreIncrement   int // Current score multiplier (increments with each correct char, resets on wrong)
	scoreBlinkActive bool
	scoreBlinkColor  tcell.Color
	scoreBlinkTime   time.Time

	// Vi-motion state
	motionCount    int
	motionCommand  string
	waitingForF    bool
	commandPrefix  rune  // For multi-key commands like 'g'
	statusMessage  string

	// Ping coordinates feature
	pingActive    bool
	pingStartTime time.Time
	pingRow       int
	pingCol       int
}

func NewGame() (*Game, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}

	if err := screen.Init(); err != nil {
		return nil, err
	}

	g := &Game{
		screen:          screen,
		trails:          make([]Trail, 0, trailLength*2),
		characters:      make([]Character, 0),
		cursorVisible:   true,
		lastSpawn:       time.Now(),
		cursorBlinkTime: time.Now(),
		nextSeqID:       1,
		score:           0,
		scoreIncrement:  0,
		motionCount:     0,
		motionCommand:   "",
		waitingForF:     false,
		commandPrefix:   0,
		statusMessage:   "",
	}

	g.width, g.height = screen.Size()
	g.updateGameArea()
	g.cursorX = g.gameWidth / 2
	g.cursorY = g.gameHeight / 2

	return g, nil
}

func (g *Game) updateGameArea() {
	// Calculate line number width based on height
	// We need 2 lines for column indicator and status bar at bottom
	gameHeight := g.height - 2
	if gameHeight < 1 {
		gameHeight = 1
	}

	lineNumWidth := len(fmt.Sprintf("%d", gameHeight))
	if lineNumWidth < 1 {
		lineNumWidth = 1
	}

	g.lineNumWidth = lineNumWidth
	g.gameX = lineNumWidth + 1 // line number + 1 space
	g.gameY = 0
	g.gameWidth = g.width - g.gameX
	g.gameHeight = gameHeight

	if g.gameWidth < 1 {
		g.gameWidth = 1
	}
}

func (g *Game) generateCharacterSequence() []Character {
	chars := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?/")

	// Generate sequence length (1-10 characters)
	seqLength := rand.Intn(10) + 1

	// Generate the sequence of runes
	sequence := make([]rune, seqLength)
	for i := 0; i < seqLength; i++ {
		sequence[i] = chars[rand.Intn(len(chars))]
	}

	// Randomly assign sequence type (Green, Red, or Blue)
	seqType := SequenceType(rand.Intn(3))

	// Randomly assign sequence level (Dark, Normal, or Bright)
	seqLevel := SequenceLevel(rand.Intn(3))

	// Pick color based on sequence type and level
	var style tcell.Style
	baseStyle := tcell.StyleDefault.Background(rgbBackground)
	switch seqType {
	case SequenceGreen:
		switch seqLevel {
		case LevelDark:
			style = baseStyle.Foreground(rgbSequenceGreenDark)
		case LevelNormal:
			style = baseStyle.Foreground(rgbSequenceGreenNormal)
		case LevelBright:
			style = baseStyle.Foreground(rgbSequenceGreenBright)
		}
	case SequenceRed:
		switch seqLevel {
		case LevelDark:
			style = baseStyle.Foreground(rgbSequenceRedDark)
		case LevelNormal:
			style = baseStyle.Foreground(rgbSequenceRedNormal)
		case LevelBright:
			style = baseStyle.Foreground(rgbSequenceRedBright)
		}
	case SequenceBlue:
		switch seqLevel {
		case LevelDark:
			style = baseStyle.Foreground(rgbSequenceBlueDark)
		case LevelNormal:
			style = baseStyle.Foreground(rgbSequenceBlueNormal)
		case LevelBright:
			style = baseStyle.Foreground(rgbSequenceBlueBright)
		}
	}

	// Find a position where the sequence fits without overlapping
	// Try up to 100 times to find a valid position
	var x, y int
	maxAttempts := 100
	foundValidPosition := false
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Random position, avoiding cursor
		x = rand.Intn(g.gameWidth)
		y = rand.Intn(g.gameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-g.cursorX)) <= 5 && math.Abs(float64(y-g.cursorY)) <= 3 {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > g.gameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			for _, ch := range g.characters {
				if ch.x == x+i && ch.y == y {
					overlaps = true
					break
				}
			}
			if overlaps {
				break
			}
		}

		if !overlaps {
			// Found a valid position
			foundValidPosition = true
			break
		}
	}

	// If no valid position was found, return empty slice
	if !foundValidPosition {
		return []Character{}
	}

	// Create the sequence
	sequenceID := g.nextSeqID
	g.nextSeqID++

	result := make([]Character, seqLength)
	for i := 0; i < seqLength; i++ {
		result[i] = Character{
			rune:         sequence[i],
			x:            x + i,
			y:            y,
			style:        style,
			sequenceID:   sequenceID,
			seqIndex:     i,
			sequenceType: seqType,
			level:        seqLevel,
		}
	}

	return result
}

func (g *Game) addTrail(fromX, fromY, toX, toY int) {
	steps := trailLength
	dx := float64(toX - fromX)
	dy := float64(toY - fromY)

	for i := 1; i <= steps; i++ {
		progress := float64(i) / float64(steps)
		x := fromX + int(dx*progress)
		y := fromY + int(dy*progress)

		g.trails = append(g.trails, Trail{
			x:         x,
			y:         y,
			intensity: 1.0 - progress*0.8,
			timestamp: time.Now().Add(time.Duration(i) * trailDecayMs * time.Millisecond),
		})
	}
}

func (g *Game) updateTrails() {
	now := time.Now()
	newTrails := make([]Trail, 0, len(g.trails))

	for _, trail := range g.trails {
		elapsed := now.Sub(trail.timestamp).Seconds()
		if elapsed < 0 {
			// Future trail point
			newTrails = append(newTrails, trail)
		} else if elapsed < 0.5 {
			// Decay intensity
			trail.intensity *= (1.0 - elapsed*2)
			if trail.intensity > 0.05 {
				newTrails = append(newTrails, trail)
			}
		}
	}

	g.trails = newTrails
}

func (g *Game) handleResize() {
	newWidth, newHeight := g.screen.Size()
	if newWidth != g.width || newHeight != g.height {
		g.width = newWidth
		g.height = newHeight
		g.updateGameArea()

		// Clamp cursor position to game area
		if g.cursorX >= g.gameWidth {
			g.cursorX = g.gameWidth - 1
		}
		if g.cursorY >= g.gameHeight {
			g.cursorY = g.gameHeight - 1
		}
		if g.cursorX < 0 {
			g.cursorX = 0
		}
		if g.cursorY < 0 {
			g.cursorY = 0
		}

		// Remove out-of-bounds characters
		newChars := make([]Character, 0, len(g.characters))
		for _, ch := range g.characters {
			if ch.x < g.gameWidth && ch.y < g.gameHeight && ch.x >= 0 && ch.y >= 0 {
				newChars = append(newChars, ch)
			}
		}
		g.characters = newChars
	}
}

func (g *Game) draw() {
	g.screen.Clear()

	// Set default background to Tokyo Night color
	defaultStyle := tcell.StyleDefault.Background(rgbBackground)
	lineNumStyle := defaultStyle.Foreground(rgbLineNumbers)

	// Draw relative line numbers (like vim's set number relativenumber)
	for y := 0; y < g.gameHeight; y++ {
		var lineNum string
		relativeNum := y - g.cursorY
		if relativeNum < 0 {
			relativeNum = -relativeNum
		}
		lineNum = fmt.Sprintf("%*d", g.lineNumWidth, relativeNum)
		for i, ch := range lineNum {
			g.screen.SetContent(i, y, ch, nil, lineNumStyle)
		}
	}

	// Permanent ping state: always highlight cursor's row and column
	// Determine ping color based on character under cursor
	pingColor := rgbPingOrange // Default: very dark orange (whitespace)
	for _, ch := range g.characters {
		if ch.x == g.cursorX && ch.y == g.cursorY {
			// Cursor is on a character - use color based on sequence type
			switch ch.sequenceType {
			case SequenceGreen:
				pingColor = rgbPingGreen
			case SequenceRed:
				pingColor = rgbPingRed
			case SequenceBlue:
				pingColor = rgbPingBlue
			}
			break
		}
	}
	pingStyle := defaultStyle.Background(pingColor)
	// Highlight the row
	for x := 0; x < g.gameWidth; x++ {
		screenX := g.gameX + x
		screenY := g.gameY + g.cursorY
		if screenY >= 0 && screenY < g.gameHeight {
			g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}
	// Highlight the column
	for y := 0; y < g.gameHeight; y++ {
		screenX := g.gameX + g.cursorX
		screenY := g.gameY + y
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
		}
	}

	// Coordinate ping: draw grid lines at (+/-)5*n intervals if active
	if g.pingActive {
		// Draw vertical lines at (+/-)5*n columns from cursor
		for n := 1; ; n++ {
			offset := 5 * n
			// Positive direction
			col := g.cursorX + offset
			if col >= g.gameWidth && g.cursorX-offset < 0 {
				break
			}
			if col < g.gameWidth {
				for y := 0; y < g.gameHeight; y++ {
					screenX := g.gameX + col
					screenY := g.gameY + y
					if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
						g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
			// Negative direction
			col = g.cursorX - offset
			if col >= 0 {
				for y := 0; y < g.gameHeight; y++ {
					screenX := g.gameX + col
					screenY := g.gameY + y
					if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
						g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
		}
		// Draw horizontal lines at (+/-)5*n rows from cursor
		for n := 1; ; n++ {
			offset := 5 * n
			// Positive direction
			row := g.cursorY + offset
			if row >= g.gameHeight && g.cursorY-offset < 0 {
				break
			}
			if row < g.gameHeight {
				for x := 0; x < g.gameWidth; x++ {
					screenX := g.gameX + x
					screenY := g.gameY + row
					if screenY >= 0 && screenY < g.gameHeight {
						g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
			// Negative direction
			row = g.cursorY - offset
			if row >= 0 {
				for x := 0; x < g.gameWidth; x++ {
					screenX := g.gameX + x
					screenY := g.gameY + row
					if screenY >= 0 && screenY < g.gameHeight {
						g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
					}
				}
			}
		}
	}

	// Draw characters (translate game coords to screen coords)
	for _, ch := range g.characters {
		screenX := g.gameX + ch.x
		screenY := g.gameY + ch.y
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			style := ch.style
			// Add ping background if on cursor's row or column
			if ch.y == g.cursorY || ch.x == g.cursorX {
				style = style.Background(pingColor)
			}
			g.screen.SetContent(screenX, screenY, ch.rune, nil, style)
		}
	}

	// Draw trails (translate game coords to screen coords)
	for _, trail := range g.trails {
		screenX := g.gameX + trail.x
		screenY := g.gameY + trail.y
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			intensity := int(trail.intensity * 255)
			if intensity > 255 {
				intensity = 255
			}
			color := tcell.NewRGBColor(int32(intensity), int32(intensity), int32(intensity))
			g.screen.SetContent(screenX, screenY, '█', nil, defaultStyle.Foreground(color))
		}
	}

	// Draw column indicators at bottom (row gameHeight) - relative to cursor
	indicatorY := g.gameHeight
	indicatorStyle := defaultStyle.Foreground(rgbColumnIndicator)
	for x := 0; x < g.gameWidth; x++ {
		screenX := g.gameX + x
		relativeCol := x - g.cursorX
		var ch rune
		if relativeCol == 0 {
			// Cursor column: show 0
			ch = '0'
		} else {
			absRelative := relativeCol
			if absRelative < 0 {
				absRelative = -absRelative
			}
			if absRelative%10 == 0 {
				// Every 10th column: show the tens digit
				ch = rune('0' + (absRelative / 10 % 10))
			} else if absRelative%5 == 0 {
				// Every 5th column: show |
				ch = '|'
			} else {
				ch = ' '
			}
		}
		g.screen.SetContent(screenX, indicatorY, ch, nil, indicatorStyle)
	}
	// Clear line number area for indicator row
	for i := 0; i < g.gameX; i++ {
		g.screen.SetContent(i, indicatorY, ' ', nil, defaultStyle)
	}

	// Draw status bar (row gameHeight + 1)
	statusY := g.gameHeight + 1
	// Clear the status bar first
	for x := 0; x < g.width; x++ {
		g.screen.SetContent(x, statusY, ' ', nil, defaultStyle)
	}

	// Draw mode indicator on the left with colored background
	var modeText string
	var modeBgColor tcell.Color
	if g.searchMode {
		// In search mode, show SEARCH with orange background
		modeText = " SEARCH "
		modeBgColor = rgbCursorNormal // Orange background
	} else if g.insertMode {
		modeText = " INSERT "
		modeBgColor = rgbModeInsertBg
	} else {
		modeText = " NORMAL "
		modeBgColor = rgbModeNormalBg
	}
	modeStyle := defaultStyle.Foreground(rgbStatusText).Background(modeBgColor)
	for i, ch := range modeText {
		if i < g.width {
			g.screen.SetContent(i, statusY, ch, nil, modeStyle)
		}
	}

	// Draw search mode UI or status message after mode indicator
	statusStartX := len(modeText) + 1
	if g.searchMode {
		// Draw search text with orange block cursor
		searchStyle := defaultStyle.Foreground(tcell.ColorWhite)
		cursorStyle := defaultStyle.Foreground(tcell.ColorBlack).Background(rgbCursorNormal)

		// Draw the search text
		for i, ch := range g.searchText {
			if statusStartX+i < g.width {
				g.screen.SetContent(statusStartX+i, statusY, ch, nil, searchStyle)
			}
		}

		// Draw block cursor at the end of search text
		cursorX := statusStartX + len(g.searchText)
		if cursorX < g.width {
			g.screen.SetContent(cursorX, statusY, ' ', nil, cursorStyle)
		}
	} else {
		// Draw status message (count/motion) after mode indicator
		statusStyle := defaultStyle.Foreground(rgbStatusBar)
		for i, ch := range g.statusMessage {
			if statusStartX+i < g.width {
				g.screen.SetContent(statusStartX+i, statusY, ch, nil, statusStyle)
			}
		}
	}

	// Draw score at bottom right with colored background
	scoreText := fmt.Sprintf(" Score: %d ", g.score)
	scoreStartX := g.width - len(scoreText)
	if scoreStartX < 0 {
		scoreStartX = 0
	}

	// Get current time for blinking effects
	now := time.Now()

	// Check if score blink is active
	if g.scoreBlinkActive && now.Sub(g.scoreBlinkTime).Milliseconds() < 200 {
		// Blink with character color
		scoreStyle := defaultStyle.Foreground(rgbStatusText).Background(g.scoreBlinkColor)
		for i, ch := range scoreText {
			if scoreStartX+i < g.width {
				g.screen.SetContent(scoreStartX+i, statusY, ch, nil, scoreStyle)
			}
		}
	} else {
		// Normal score display with golden yellow background
		g.scoreBlinkActive = false
		scoreStyle := defaultStyle.Foreground(rgbStatusText).Background(rgbScoreBg)
		for i, ch := range scoreText {
			if scoreStartX+i < g.width {
				g.screen.SetContent(scoreStartX+i, statusY, ch, nil, scoreStyle)
			}
		}
	}

	// Draw cursor (translate game coords to screen coords) - non-blinking

	// Handle error blink
	if g.cursorError && now.Sub(g.cursorErrorTime).Milliseconds() > errorBlinkMs {
		g.cursorError = false
	}

	// Draw cursor as full box (always visible, non-blinking)
	// In search mode, cursor is not visually shown (appears normal)
	if !g.searchMode {
		screenX := g.gameX + g.cursorX
		screenY := g.gameY + g.cursorY
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			// Check if there's a character at cursor position
			var charAtCursor rune = ' '
			var charColor tcell.Color
			hasChar := false
			for _, ch := range g.characters {
				if ch.x == g.cursorX && ch.y == g.cursorY {
					charAtCursor = ch.rune
					fg, _, _ := ch.style.Decompose()
					charColor = fg
					hasChar = true
					break
				}
			}

			// Determine cursor color based on mode and character
			var cursorBgColor tcell.Color
			var charFgColor tcell.Color
			if g.cursorError {
				cursorBgColor = rgbCursorError
				charFgColor = tcell.ColorBlack
			} else if hasChar {
				// Cursor on a character: cursor bg = black, char fg = character color
				cursorBgColor = tcell.NewRGBColor(10, 10, 10) // Very dark (almost black)
				charFgColor = charColor
			} else {
				// Cursor on empty space: orange (normal) or white (insert)
				if g.insertMode {
					cursorBgColor = rgbCursorInsert
				} else {
					cursorBgColor = rgbCursorNormal
				}
				charFgColor = tcell.ColorBlack
			}

			// Draw cursor with character (or space)
			cursorStyle := defaultStyle.Foreground(charFgColor).Background(cursorBgColor)
			g.screen.SetContent(screenX, screenY, charAtCursor, nil, cursorStyle)
		}
	}

	g.screen.Show()
}

func (g *Game) moveCursor(dx, dy int) {
	oldX, oldY := g.cursorX, g.cursorY

	g.cursorX += dx
	g.cursorY += dy

	// Clamp to game area
	if g.cursorX < 0 {
		g.cursorX = 0
	}
	if g.cursorX >= g.gameWidth {
		g.cursorX = g.gameWidth - 1
	}
	if g.cursorY < 0 {
		g.cursorY = 0
	}
	if g.cursorY >= g.gameHeight {
		g.cursorY = g.gameHeight - 1
	}

	// Add trail if cursor moved and trail is enabled
	if oldX != g.cursorX || oldY != g.cursorY {
		if g.trailEnabled {
			g.addTrail(oldX, oldY, g.cursorX, g.cursorY)
		}
	}
}

// handleCharacterTyping processes character input in insert mode
func (g *Game) handleCharacterTyping(typedChar rune) {
	// Find character at cursor position
	hitCharIndex := -1
	var hitChar Character
	for i, ch := range g.characters {
		if ch.x == g.cursorX && ch.y == g.cursorY {
			hitCharIndex = i
			hitChar = ch
			break
		}
	}

	// Check if typed character matches
	if hitCharIndex >= 0 && hitChar.rune == typedChar {
		// Increment score multiplier (first correct char = 1, second = 2, etc.)
		g.scoreIncrement++

		// Calculate level multiplier based on character level
		levelMultiplier := 1
		switch hitChar.level {
		case LevelDark:
			levelMultiplier = 1
		case LevelNormal:
			levelMultiplier = 2
		case LevelBright:
			levelMultiplier = 3
		}

		// Calculate points (score increment × level multiplier)
		points := g.scoreIncrement * levelMultiplier

		// Apply negative for red sequences
		if hitChar.sequenceType == SequenceRed {
			points = -points
		}

		// Add to score
		g.score += points

		// Activate score blink with character color
		g.scoreBlinkActive = true
		g.scoreBlinkTime = time.Now()
		// Extract the foreground color from the character's style
		fg, _, _ := hitChar.style.Decompose()
		g.scoreBlinkColor = fg

		// If this is a blue character, enable trail for 5 seconds
		if hitChar.sequenceType == SequenceBlue {
			g.enableTrailFor5Seconds()
		}

		// Remove character
		g.characters = append(g.characters[:hitCharIndex], g.characters[hitCharIndex+1:]...)

		// Move cursor right if possible
		if g.cursorX < g.gameWidth-1 {
			g.moveCursor(1, 0)
		}
	} else {
		// Wrong character - flash error and reset score increment
		g.cursorError = true
		g.cursorErrorTime = time.Now()
		g.scoreIncrement = 0
	}
}

// Helper to check if there's a character at position (x, y)
func (g *Game) hasCharAt(x, y int) bool {
	for _, ch := range g.characters {
		if ch.x == x && ch.y == y {
			return true
		}
	}
	return false
}

// checkPathForCharacters checks if all positions between (x1, y1) and (x2, y2) contain characters
// Returns true if the path is clear (all positions have characters), false if there's a gap
func (g *Game) checkPathForCharacters(x1, y1, x2, y2 int) bool {
	// For horizontal movement on same line
	if y1 == y2 {
		minX := x1
		maxX := x2
		if x1 > x2 {
			minX = x2
			maxX = x1
		}
		// Check all positions between (exclusive of endpoints)
		for x := minX + 1; x < maxX; x++ {
			if !g.hasCharAt(x, y1) {
				return false // Found a gap
			}
		}
		return true
	}
	// For vertical or diagonal movement, just check if adjacent
	xDiff := x2 - x1
	yDiff := y2 - y1
	if xDiff < 0 {
		xDiff = -xDiff
	}
	if yDiff < 0 {
		yDiff = -yDiff
	}
	// Only consider adjacent if within 1 step
	return xDiff <= 1 && yDiff <= 1
}

// enableTrailFor5Seconds enables the cursor trail effect for 5 seconds
func (g *Game) enableTrailFor5Seconds() {
	g.trailEnabled = true
	g.trailEndTime = time.Now().Add(5 * time.Second)

	// Cancel existing timer if any
	if g.trailTimer != nil {
		g.trailTimer.Stop()
	}

	// Set new timer
	g.trailTimer = time.AfterFunc(5*time.Second, func() {
		g.trailEnabled = false
	})
}

// moveToNextWordStart implements 'w' motion
// Moves to the first character after spaces on the right
func (g *Game) moveToNextWordStart() {
	y := g.cursorY
	startX := g.cursorX + 1

	// Phase 1: Skip any characters (if we're in a word)
	x := startX
	for x < g.gameWidth && g.hasCharAt(x, y) {
		x++
	}

	// Phase 2: Skip spaces
	for x < g.gameWidth && !g.hasCharAt(x, y) {
		x++
	}

	// Phase 3: Found first character after spaces (or reached end)
	if x < g.gameWidth && g.hasCharAt(x, y) {
		g.moveCursor(x-g.cursorX, 0)
	} else {
		// No word found - flash error
		g.cursorError = true
		g.cursorErrorTime = time.Now()
	}
}

// moveToNextWordEnd implements 'e' motion
// Moves to the last character of the first word on the right
func (g *Game) moveToNextWordEnd() {
	y := g.cursorY
	startX := g.cursorX

	// If we're on a character, skip to end of current word first
	x := startX
	if g.hasCharAt(x, y) {
		x++
		for x < g.gameWidth && g.hasCharAt(x, y) {
			x++
		}
	} else {
		x++
	}

	// Skip spaces
	for x < g.gameWidth && !g.hasCharAt(x, y) {
		x++
	}

	// Now find the end of the next word
	if x < g.gameWidth && g.hasCharAt(x, y) {
		// Found start of word, now find its end
		for x+1 < g.gameWidth && g.hasCharAt(x+1, y) {
			x++
		}
		g.moveCursor(x-g.cursorX, 0)
	} else {
		// No word found - flash error
		g.cursorError = true
		g.cursorErrorTime = time.Now()
	}
}

// moveToPrevWordStart implements 'b' motion
// Moves to the beginning of the word on the left
func (g *Game) moveToPrevWordStart() {
	y := g.cursorY
	startX := g.cursorX

	// Start from position to the left
	x := startX - 1
	if x < 0 {
		// Already at beginning - flash error
		g.cursorError = true
		g.cursorErrorTime = time.Now()
		return
	}

	// Phase 1: Skip any spaces
	for x >= 0 && !g.hasCharAt(x, y) {
		x--
	}

	// Phase 2: If we found a character, find the beginning of that word
	if x >= 0 && g.hasCharAt(x, y) {
		// We're on a character, move to the beginning of this word
		for x-1 >= 0 && g.hasCharAt(x-1, y) {
			x--
		}
		g.moveCursor(x-g.cursorX, 0)
	} else {
		// No word found - flash error
		g.cursorError = true
		g.cursorErrorTime = time.Now()
	}
}

func (g *Game) executeMotion(command rune, count int) {
	if count == 0 {
		count = 1
	}

	switch command {
	case 'h': // left
		g.moveCursor(-count, 0)
	case 'j': // down
		g.moveCursor(0, count)
	case 'k': // up
		g.moveCursor(0, -count)
	case 'l': // right
		g.moveCursor(count, 0)
	case '0': // beginning of line
		g.moveCursor(-g.cursorX, 0)
	case '$': // end of line (rightmost non-space character)
		// Find rightmost character on current line
		rightmost := -1
		for _, ch := range g.characters {
			if ch.y == g.cursorY {
				if rightmost == -1 || ch.x > rightmost {
					rightmost = ch.x
				}
			}
		}
		// Only move if we found a character on the line
		if rightmost >= 0 && rightmost != g.cursorX {
			g.moveCursor(rightmost-g.cursorX, 0)
		} else if rightmost < 0 {
			// No characters on line - flash error
			g.cursorError = true
			g.cursorErrorTime = time.Now()
		}
	case 'G': // bottom row, same column
		dy := (g.gameHeight - 1) - g.cursorY
		g.moveCursor(0, dy)
	case 'w': // next word start (first character after spaces)
		for i := 0; i < count; i++ {
			g.moveToNextWordStart()
		}
	case 'e': // next word end (last character before space)
		for i := 0; i < count; i++ {
			g.moveToNextWordEnd()
		}
	case 'b': // previous word start (beginning of word on left)
		for i := 0; i < count; i++ {
			g.moveToPrevWordStart()
		}
	}

	// Clear motion state
	g.motionCount = 0
	g.motionCommand = ""
	g.commandPrefix = 0
	g.statusMessage = ""
}

// executeCompoundMotion handles multi-key commands like 'gg' or 'go'
func (g *Game) executeCompoundMotion(prefix rune, command rune) {
	switch prefix {
	case 'g':
		switch command {
		case 'g': // gg - go to top row, same column
			dy := -g.cursorY
			g.moveCursor(0, dy)
		case 'o': // go - go to top-left corner
			dx := -g.cursorX
			dy := -g.cursorY
			g.moveCursor(dx, dy)
		default:
			// Unknown compound command - flash error
			g.cursorError = true
			g.cursorErrorTime = time.Now()
		}
	}

	// Clear motion state
	g.motionCount = 0
	g.motionCommand = ""
	g.commandPrefix = 0
	g.statusMessage = ""
}

func (g *Game) findCharOnLine(target rune) {
	// Search from current position to right on current line
	for x := g.cursorX + 1; x < g.gameWidth; x++ {
		// Check if there's a character at this position
		for _, ch := range g.characters {
			if ch.y == g.cursorY && ch.x == x && ch.rune == target {
				g.moveCursor(x-g.cursorX, 0)
				g.waitingForF = false
				g.motionCount = 0
				g.motionCommand = ""
				g.commandPrefix = 0
				g.statusMessage = ""
				return
			}
		}
	}

	// Character not found - flash error
	g.cursorError = true
	g.cursorErrorTime = time.Now()
	g.waitingForF = false
	g.motionCount = 0
	g.motionCommand = ""
	g.commandPrefix = 0
	g.statusMessage = ""
}

// performSearch searches for the given text starting from current cursor position
// Returns true if found and moves cursor to the beginning of the match
func (g *Game) performSearch(searchStr string) bool {
	if searchStr == "" {
		// Empty search string - do nothing
		return true
	}

	searchRunes := []rune(searchStr)
	searchLen := len(searchRunes)

	// Helper function to check if sequence at (startX, y) matches search string
	matchesAt := func(startX, y int) bool {
		for i := 0; i < searchLen; i++ {
			found := false
			for _, ch := range g.characters {
				if ch.x == startX+i && ch.y == y && ch.rune == searchRunes[i] {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	// Search from current position forward
	// Start from next position after cursor
	startY := g.cursorY
	startX := g.cursorX + 1

	// Search from current row to end
	for y := startY; y < g.gameHeight; y++ {
		xStart := 0
		if y == startY {
			xStart = startX
		}
		for x := xStart; x <= g.gameWidth-searchLen; x++ {
			if matchesAt(x, y) {
				// Found match - move cursor
				dx := x - g.cursorX
				dy := y - g.cursorY
				g.moveCursor(dx, dy)
				return true
			}
		}
	}

	// If not found, search from beginning (rollover)
	for y := 0; y < startY; y++ {
		for x := 0; x <= g.gameWidth-searchLen; x++ {
			if matchesAt(x, y) {
				// Found match - move cursor
				dx := x - g.cursorX
				dy := y - g.cursorY
				g.moveCursor(dx, dy)
				return true
			}
		}
	}

	// Search remaining part of starting row (before cursor)
	for x := 0; x < startX && x <= g.gameWidth-searchLen; x++ {
		if matchesAt(x, startY) {
			// Found match - move cursor
			dx := x - g.cursorX
			g.moveCursor(dx, 0)
			return true
		}
	}

	// Not found
	return false
}

func (g *Game) handleInput(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		// Handle Ctrl+Q to exit (works in both normal and insert modes)
		if ev.Key() == tcell.KeyCtrlQ {
			return false
		}

		// Handle Escape key
		if ev.Key() == tcell.KeyEscape {
			if g.searchMode {
				// Exit search mode
				g.searchMode = false
				g.searchText = ""
				return true
			}
			if g.insertMode {
				// Exit insert mode
				g.insertMode = false
				// Reset score increment when exiting insert mode
				g.scoreIncrement = 0
				return true
			}
			// In normal mode, ESC does nothing
			return true
		}

		// Handle Ctrl+C to exit
		if ev.Key() == tcell.KeyCtrlC {
			return false
		}

		// Handle Enter key
		if ev.Key() == tcell.KeyEnter {
			if g.searchMode {
				// In search mode: execute search
				if g.searchText == "" {
					// Empty search text - exit search mode
					g.searchMode = false
					return true
				} else {
					// Execute search
					if !g.performSearch(g.searchText) {
						// Search failed - flash error
						g.cursorError = true
						g.cursorErrorTime = time.Now()
					}
					// Exit search mode
					g.searchMode = false
					g.searchText = ""
					return true
				}
			} else if !g.insertMode {
				// In normal mode: toggle coordinate ping
				g.pingActive = !g.pingActive
				return true
			}
		}

		// Handle Backspace in search mode
		if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
			if g.searchMode && len(g.searchText) > 0 {
				// Remove last character from search text
				g.searchText = g.searchText[:len(g.searchText)-1]
				return true
			}
		}

		// Handle arrow keys in insert mode
		if g.insertMode {
			switch ev.Key() {
			case tcell.KeyUp:
				g.moveCursor(0, -1)
				return true
			case tcell.KeyDown:
				g.moveCursor(0, 1)
				return true
			case tcell.KeyLeft:
				g.moveCursor(-1, 0)
				return true
			case tcell.KeyRight:
				g.moveCursor(1, 0)
				return true
			}
		}

		// Handle Home key in both modes (behaves like '0' - go to beginning of line)
		if ev.Key() == tcell.KeyHome {
			g.moveCursor(-g.cursorX, 0)
			return true
		}

		// Handle End key in both modes (behaves like '$' - go to rightmost character)
		if ev.Key() == tcell.KeyEnd {
			// Find rightmost character on current line
			rightmost := -1
			for _, ch := range g.characters {
				if ch.y == g.cursorY {
					if rightmost == -1 || ch.x > rightmost {
						rightmost = ch.x
					}
				}
			}
			// Only move if we found a character on the line
			if rightmost >= 0 && rightmost != g.cursorX {
				g.moveCursor(rightmost-g.cursorX, 0)
			}
			return true
		}

		if ev.Key() == tcell.KeyRune {
			char := ev.Rune()

			// Handle search mode - type characters into search text
			if g.searchMode {
				g.searchText += string(char)
				return true
			}

			// Handle insert mode - type characters
			if g.insertMode {
				g.handleCharacterTyping(char)
				return true
			}

			// Normal mode commands below

			// Handle '/' to enter search mode
			if char == '/' {
				g.searchMode = true
				g.searchText = ""
				return true
			}

			// Handle 'i' to enter insert mode
			if char == 'i' {
				g.insertMode = true
				return true
			}

			// If waiting for f{char}, handle it
			if g.waitingForF {
				g.findCharOnLine(char)
				return true
			}

			// If we have a command prefix (like 'g'), handle the next character
			if g.commandPrefix != 0 {
				g.motionCommand += string(char)
				g.statusMessage = g.motionCommand
				g.executeCompoundMotion(g.commandPrefix, char)
				return true
			}

			// Handle digits for motion count (including '0' when already in count mode)
			if char >= '0' && char <= '9' {
				// '0' is beginning-of-line command only when NOT in count mode
				if char == '0' && g.motionCount == 0 {
					// Execute '0' command (beginning of line)
					g.motionCommand = "0"
					g.statusMessage = g.motionCommand
					g.executeMotion('0', 0)
					return true
				}
				// Otherwise, '0' is part of count (e.g., "10k")
				g.motionCount = g.motionCount*10 + int(char-'0')
				g.motionCommand += string(char)
				g.statusMessage = g.motionCommand
				return true
			}

			// Handle movement commands
			if char == 'h' || char == 'j' || char == 'k' || char == 'l' {
				g.motionCommand += string(char)
				g.statusMessage = g.motionCommand
				g.executeMotion(char, g.motionCount)
				return true
			}

			// Handle 'w' command (next word start)
			if char == 'w' {
				g.motionCommand += "w"
				g.statusMessage = g.motionCommand
				g.executeMotion('w', g.motionCount)
				return true
			}

			// Handle 'e' command (next word end)
			if char == 'e' {
				g.motionCommand += "e"
				g.statusMessage = g.motionCommand
				g.executeMotion('e', g.motionCount)
				return true
			}

			// Handle 'b' command (previous word start)
			if char == 'b' {
				g.motionCommand += "b"
				g.statusMessage = g.motionCommand
				g.executeMotion('b', g.motionCount)
				return true
			}

			// Handle 'G' command (bottom of screen)
			if char == 'G' {
				g.motionCommand = "G"
				g.statusMessage = g.motionCommand
				g.executeMotion('G', 0)
				return true
			}

			// Handle 'g' command prefix
			if char == 'g' {
				g.commandPrefix = 'g'
				g.motionCommand = "g"
				g.statusMessage = g.motionCommand
				return true
			}

			// Handle '$' command
			if char == '$' {
				g.motionCommand = "$"
				g.statusMessage = g.motionCommand
				g.executeMotion('$', 0)
				return true
			}

			// Handle 'f' command
			if char == 'f' {
				g.waitingForF = true
				g.motionCommand += "f"
				g.statusMessage = g.motionCommand
				return true
			}

			// Unknown command - clear state and flash error
			g.motionCount = 0
			g.motionCommand = ""
			g.commandPrefix = 0
			g.statusMessage = ""
			g.cursorError = true
			g.cursorErrorTime = time.Now()
		}

	case *tcell.EventResize:
		g.handleResize()
	}

	return true
}

func (g *Game) run() {
	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	eventChan := make(chan tcell.Event, 100)
	go func() {
		for {
			eventChan <- g.screen.PollEvent()
		}
	}()

	for {
		select {
		case ev := <-eventChan:
			if !g.handleInput(ev) {
				return
			}

		case <-ticker.C:
			// Calculate screen fill percentage
			totalCells := g.gameWidth * g.gameHeight
			filledCells := len(g.characters)
			fillPercentage := float64(filledCells) / float64(totalCells)

			// Adjust spawn rate based on fill percentage
			var spawnDelay int64
			if fillPercentage < 0.30 {
				// Less than 30% filled - accelerate spawning (2x faster)
				spawnDelay = characterSpawnMs / 2
			} else if fillPercentage > 0.70 {
				// More than 70% filled - decelerate spawning (2x slower)
				spawnDelay = characterSpawnMs * 2
			} else {
				// Normal spawn rate
				spawnDelay = characterSpawnMs
			}

			// Spawn new character sequences
			if time.Since(g.lastSpawn).Milliseconds() > spawnDelay {
				if len(g.characters) < 200 { // Max characters on screen (increased for sequences)
					newSeq := g.generateCharacterSequence()
					// Only add if sequence was successfully placed (not all zeros for position)
					if len(newSeq) > 0 {
						g.characters = append(g.characters, newSeq...)
					}
					g.lastSpawn = time.Now()
				}
			}

			g.updateTrails()
			g.draw()
		}
	}
}

func (g *Game) cleanup() {
	g.screen.Fini()
}

func main() {
	rand.Seed(time.Now().UnixNano())

	game, err := NewGame()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize: %v\n", err)
		os.Exit(1)
	}
	defer game.cleanup()

	game.run()
}
