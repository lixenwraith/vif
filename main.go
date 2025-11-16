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

type Character struct {
	rune  rune
	x, y  int
	style tcell.Style
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
	trails []Trail

	// Characters on screen
	characters []Character
	lastSpawn  time.Time

	// Vi-motion state
	motionCount   int
	motionCommand string
	waitingForF   bool
	statusMessage string

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
		motionCount:     0,
		motionCommand:   "",
		waitingForF:     false,
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

func (g *Game) generateCharacter() Character {
	chars := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?/")

	// Avoid spawn near cursor (coordinates are in game area)
	var x, y int
	for {
		x = rand.Intn(g.gameWidth)
		y = rand.Intn(g.gameHeight)
		if math.Abs(float64(x-g.cursorX)) > 5 || math.Abs(float64(y-g.cursorY)) > 3 {
			break
		}
	}

	char := chars[rand.Intn(len(chars))]

	// Color based on character type
	var style tcell.Style
	switch {
	case char >= 'a' && char <= 'z':
		style = tcell.StyleDefault.Foreground(tcell.ColorGreen)
	case char >= 'A' && char <= 'Z':
		style = tcell.StyleDefault.Foreground(tcell.ColorBlue)
	case char >= '0' && char <= '9':
		style = tcell.StyleDefault.Foreground(tcell.ColorYellow)
	default:
		style = tcell.StyleDefault.Foreground(tcell.ColorPurple)
	}

	return Character{
		rune:  char,
		x:     x,
		y:     y,
		style: style,
	}
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

	lineNumStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)

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
		pingStyle := tcell.StyleDefault.Background(tcell.ColorDarkGray)
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
				style = style.Background(tcell.ColorDarkGray)
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
	indicatorStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
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
	statusStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
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
			cursorStyle = tcell.StyleDefault.Foreground(tcell.ColorRed).Reverse(true)
		} else {
			cursorStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Reverse(true)
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

	// Add trail if cursor moved
	if oldX != g.cursorX || oldY != g.cursorY {
		g.addTrail(oldX, oldY, g.cursorX, g.cursorY)

		// Clear ping on cursor movement
		g.pingActive = false

		// Check if we hit a character at the new position
		for i, ch := range g.characters {
			if ch.x == g.cursorX && ch.y == g.cursorY {
				// Remove character
				g.characters = append(g.characters[:i], g.characters[i+1:]...)
				break
			}
		}
	}

	// Reset cursor blink
	g.cursorVisible = true
	g.cursorBlinkTime = time.Now()
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
	}

	// Clear motion state
	g.motionCount = 0
	g.motionCommand = ""
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
				g.motionCommand = ""
				g.statusMessage = ""
				return
			}
		}
	}

	// Character not found - flash error
	g.cursorError = true
	g.cursorErrorTime = time.Now()
	g.waitingForF = false
	g.motionCommand = ""
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

			// Handle digits for motion count
			if char >= '1' && char <= '9' {
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

			// Handle special commands
			if char == '0' {
				g.motionCommand = "0"
				g.statusMessage = g.motionCommand
				g.executeMotion('0', 0)
				return true
			}

			if char == '$' {
				g.motionCommand = "$"
				g.statusMessage = g.motionCommand
				g.executeMotion('$', 0)
				return true
			}

			if char == 'f' {
				g.waitingForF = true
				g.motionCommand += "f"
				g.statusMessage = g.motionCommand
				return true
			}

			// Unknown command - clear state and flash error
			g.motionCount = 0
			g.motionCommand = ""
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
			// Spawn new characters
			if time.Since(g.lastSpawn).Milliseconds() > characterSpawnMs {
				if len(g.characters) < 20 { // Max characters on screen
					g.characters = append(g.characters, g.generateCharacter())
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
