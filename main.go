// FILE: main.go
package main

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/generators"
	"github.com/gopxl/beep/speaker"
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

	// Cursor state
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

	// Audio
	audioInit bool
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
	}

	g.width, g.height = screen.Size()
	g.cursorX = g.width / 2
	g.cursorY = g.height / 2

	// Initialize audio
	if err := g.initAudio(); err != nil {
		// Non-fatal, game can run without sound
		log.Printf("Audio initialization failed: %v", err)
	}

	return g, nil
}

func (g *Game) initAudio() error {
	sampleRate := beep.SampleRate(44100)
	err := speaker.Init(sampleRate, sampleRate.N(time.Second/10))
	if err == nil {
		g.audioInit = true
	}
	return err
}

func (g *Game) playHitSound() {
	if !g.audioInit {
		return
	}

	sampleRate := beep.SampleRate(44100)
	duration := sampleRate.N(50 * time.Millisecond)
	sine, _ := generators.SineTone(sampleRate, 880)

	// Create a buffer and play
	buffer := beep.Take(duration, sine)
	speaker.Play(buffer)
}

func (g *Game) generateCharacter() Character {
	chars := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?/")

	// Avoid spawn near cursor
	var x, y int
	for {
		x = rand.Intn(g.width)
		y = rand.Intn(g.height)
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

		// Clamp cursor position
		if g.cursorX >= g.width {
			g.cursorX = g.width - 1
		}
		if g.cursorY >= g.height {
			g.cursorY = g.height - 1
		}

		// Remove out-of-bounds characters
		newChars := make([]Character, 0, len(g.characters))
		for _, ch := range g.characters {
			if ch.x < g.width && ch.y < g.height {
				newChars = append(newChars, ch)
			}
		}
		g.characters = newChars
	}
}

func (g *Game) draw() {
	g.screen.Clear()

	// Draw characters
	for _, ch := range g.characters {
		g.screen.SetContent(ch.x, ch.y, ch.rune, nil, ch.style)
	}

	// Draw trails
	for _, trail := range g.trails {
		if trail.x >= 0 && trail.x < g.width && trail.y >= 0 && trail.y < g.height {
			intensity := int(trail.intensity * 255)
			if intensity > 255 {
				intensity = 255
			}
			color := tcell.NewRGBColor(int32(intensity), int32(intensity), int32(intensity))
			g.screen.SetContent(trail.x, trail.y, '█', nil, tcell.StyleDefault.Foreground(color))
		}
	}

	// Draw cursor
	now := time.Now()

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
		g.screen.SetContent(g.cursorX, g.cursorY, ' ', nil, cursorStyle)
	}

	g.screen.Show()
}

func (g *Game) handleInput(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC ||
			(ev.Key() == tcell.KeyRune && ev.Rune() == 'q' && ev.Modifiers()&tcell.ModCtrl != 0) {
			return false
		}

		if ev.Key() == tcell.KeyRune {
			char := ev.Rune()

			// Check if character matches any on screen
			for i, ch := range g.characters {
				if ch.rune == char {
					// Move cursor with trail
					g.addTrail(g.cursorX, g.cursorY, ch.x, ch.y)
					g.cursorX = ch.x
					g.cursorY = ch.y

					// Remove character
					g.characters = append(g.characters[:i], g.characters[i+1:]...)

					// Play sound
					g.playHitSound()

					// Reset cursor blink
					g.cursorVisible = true
					g.cursorBlinkTime = time.Now()

					return true
				}
			}

			// Wrong key - flash red
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
	if g.audioInit {
		speaker.Close()
	}
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
