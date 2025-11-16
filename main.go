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
	// RGB color definitions for sequences
	rgbSequenceGreen   = tcell.NewRGBColor(0, 200, 0)      // Green
	rgbSequenceRed     = tcell.NewRGBColor(255, 80, 80)    // Red
	rgbSequenceBlue    = tcell.NewRGBColor(100, 150, 255)  // Blue

	rgbLineNumbers     = tcell.NewRGBColor(100, 100, 100)  // Dark gray
	rgbStatusBar       = tcell.NewRGBColor(255, 255, 255)  // White
	rgbColumnIndicator = tcell.NewRGBColor(100, 100, 100)  // Dark gray

	rgbPingHighlight   = tcell.NewRGBColor(50, 50, 50)     // Very dark gray for ping
	rgbCursorNormal    = tcell.NewRGBColor(255, 255, 255)  // White
	rgbCursorError     = tcell.NewRGBColor(255, 80, 80)    // Red
	rgbTrailGray       = tcell.NewRGBColor(200, 200, 200)  // Light gray base
)

type SequenceType int

const (
	SequenceGreen SequenceType = iota // Positive scoring
	SequenceRed                        // Negative scoring
	SequenceBlue                       // Positive scoring + trail effect
)

type Character struct {
	rune         rune
	x, y         int
	style        tcell.Style
	sequenceID   int          // All chars in same sequence have same ID
	seqIndex     int          // Position in the sequence (0-based)
	sequenceType SequenceType // Type of sequence (Green, Red, Blue)
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
	lastCharX        int // Last character position hit (-1 if none or space)
	lastCharY        int
	hitSequencePos   int // Current position in hit sequence (1-based)

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
		lastCharX:       -1,
		lastCharY:       -1,
		hitSequencePos:  0,
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

	// Pick color based on sequence type
	var style tcell.Style
	switch seqType {
	case SequenceGreen:
		style = tcell.StyleDefault.Foreground(rgbSequenceGreen)
	case SequenceRed:
		style = tcell.StyleDefault.Foreground(rgbSequenceRed)
	case SequenceBlue:
		style = tcell.StyleDefault.Foreground(rgbSequenceBlue)
	}

	// Find a position where the sequence fits without overlapping
	// Try up to 100 times to find a valid position
	var x, y int
	maxAttempts := 100
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
			break
		}
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

	lineNumStyle := tcell.StyleDefault.Foreground(rgbLineNumbers)

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

	// Check if ping should still be active
	now := time.Now()
	if g.pingActive && now.Sub(g.pingStartTime).Milliseconds() > 1000 {
		g.pingActive = false
	}

	// Draw ping highlights (background only)
	if g.pingActive {
		pingStyle := tcell.StyleDefault.Background(rgbPingHighlight)
		// Highlight the row
		for x := 0; x < g.gameWidth; x++ {
			screenX := g.gameX + x
			screenY := g.gameY + g.pingRow
			if screenY >= 0 && screenY < g.gameHeight {
				g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
			}
		}
		// Highlight the column
		for y := 0; y < g.gameHeight; y++ {
			screenX := g.gameX + g.pingCol
			screenY := g.gameY + y
			if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
				g.screen.SetContent(screenX, screenY, ' ', nil, pingStyle)
			}
		}
	}

	// Draw characters (translate game coords to screen coords)
	for _, ch := range g.characters {
		screenX := g.gameX + ch.x
		screenY := g.gameY + ch.y
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			style := ch.style
			// Add gray background if on ping row or column
			if g.pingActive && (ch.y == g.pingRow || ch.x == g.pingCol) {
				style = style.Background(rgbPingHighlight)
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
			g.screen.SetContent(screenX, screenY, '█', nil, tcell.StyleDefault.Foreground(color))
		}
	}

	// Draw column indicators at bottom (row gameHeight) - relative to cursor
	indicatorY := g.gameHeight
	indicatorStyle := tcell.StyleDefault.Foreground(rgbColumnIndicator)
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
		g.screen.SetContent(i, indicatorY, ' ', nil, tcell.StyleDefault)
	}

	// Draw status bar (row gameHeight + 1)
	statusY := g.gameHeight + 1
	statusStyle := tcell.StyleDefault.Foreground(rgbStatusBar)
	// Clear the status bar first
	for x := 0; x < g.width; x++ {
		g.screen.SetContent(x, statusY, ' ', nil, tcell.StyleDefault)
	}
	// Draw status message
	for i, ch := range g.statusMessage {
		if i < g.width {
			g.screen.SetContent(i, statusY, ch, nil, statusStyle)
		}
	}
	// Draw score at bottom right
	scoreText := fmt.Sprintf("Score: %d", g.score)
	scoreStartX := g.width - len(scoreText)
	if scoreStartX < 0 {
		scoreStartX = 0
	}
	for i, ch := range scoreText {
		if scoreStartX+i < g.width {
			g.screen.SetContent(scoreStartX+i, statusY, ch, nil, statusStyle)
		}
	}

	// Draw cursor (translate game coords to screen coords)
	now = time.Now()

	// Handle error blink
	if g.cursorError && now.Sub(g.cursorErrorTime).Milliseconds() > errorBlinkMs {
		g.cursorError = false
	}

	// Handle cursor blink
	if now.Sub(g.cursorBlinkTime).Milliseconds() > cursorBlinkMs {
		g.cursorVisible = !g.cursorVisible
		g.cursorBlinkTime = now
	}

	if g.cursorVisible {
		var cursorStyle tcell.Style
		if g.cursorError {
			cursorStyle = tcell.StyleDefault.Foreground(rgbCursorError).Reverse(true)
		} else {
			cursorStyle = tcell.StyleDefault.Foreground(rgbCursorNormal).Reverse(true)
		}
		screenX := g.gameX + g.cursorX
		screenY := g.gameY + g.cursorY
		if screenX >= g.gameX && screenX < g.width && screenY >= 0 && screenY < g.gameHeight {
			g.screen.SetContent(screenX, screenY, ' ', nil, cursorStyle)
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

		// Clear ping on cursor movement
		g.pingActive = false

		// Check if we hit a character at the new position
		hitCharIndex := -1
		var hitChar Character
		for i, ch := range g.characters {
			if ch.x == g.cursorX && ch.y == g.cursorY {
				hitCharIndex = i
				hitChar = ch
				break
			}
		}

		if hitCharIndex >= 0 {
			// We hit a character - update score
			wasOnSpace := g.lastCharX == -1

			// Calculate score multiplier based on sequence type
			scoreMultiplier := 1
			if hitChar.sequenceType == SequenceRed {
				scoreMultiplier = -1
			}

			if wasOnSpace {
				// Moving from space to character
				g.score += 1 * scoreMultiplier
				g.hitSequencePos = 1
			} else {
				// Moving from character to character - check if continuing sequence
				// Find the last character we hit
				var lastHitChar *Character
				for i := range g.characters {
					if g.characters[i].x == g.lastCharX && g.characters[i].y == g.lastCharY {
						lastHitChar = &g.characters[i]
						break
					}
				}

				continuing := false
				if lastHitChar != nil {
					// Check if there's a clear path between last char and current char
					// (no spaces in between)
					pathClear := g.checkPathForCharacters(lastHitChar.x, lastHitChar.y, hitChar.x, hitChar.y)
					if pathClear {
						continuing = true
					}
				}

				if continuing {
					// Continuing sequence
					g.hitSequencePos++
					g.score += g.hitSequencePos * scoreMultiplier
				} else {
					// New sequence
					g.hitSequencePos = 1
					g.score += 1 * scoreMultiplier
				}
			}

			// If this is a blue character, enable trail for 5 seconds
			if hitChar.sequenceType == SequenceBlue {
				g.enableTrailFor5Seconds()
			}

			// Update last character position
			g.lastCharX = hitChar.x
			g.lastCharY = hitChar.y

			// Remove character
			g.characters = append(g.characters[:hitCharIndex], g.characters[hitCharIndex+1:]...)
		} else {
			// No character at new position - reset to space
			g.lastCharX = -1
			g.lastCharY = -1
			g.hitSequencePos = 0
		}
	}

	// Reset cursor blink
	g.cursorVisible = true
	g.cursorBlinkTime = time.Now()
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

func (g *Game) handleInput(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC ||
			(ev.Key() == tcell.KeyRune && ev.Rune() == 'q' && ev.Modifiers()&tcell.ModCtrl != 0) {
			return false
		}

		// Handle Enter key for ping coordinates
		if ev.Key() == tcell.KeyEnter {
			g.pingActive = true
			g.pingStartTime = time.Now()
			g.pingRow = g.cursorY
			g.pingCol = g.cursorX
			return true
		}

		if ev.Key() == tcell.KeyRune {
			char := ev.Rune()

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
			// Spawn new character sequences
			if time.Since(g.lastSpawn).Milliseconds() > characterSpawnMs {
				if len(g.characters) < 50 { // Max characters on screen (increased for sequences)
					newSeq := g.generateCharacterSequence()
					g.characters = append(g.characters, newSeq...)
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
