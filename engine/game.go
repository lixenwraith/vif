package engine

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/core"
)

// GameMode represents the current input mode
type GameMode int

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// ECS World
	World *World

	// Screen and buffer
	Screen tcell.Screen
	Buffer *core.Buffer

	// Screen dimensions
	Width, Height int

	// Game area (excluding UI elements)
	GameX, GameY          int
	GameWidth, GameHeight int
	LineNumWidth          int

	// Mode state
	Mode           GameMode
	SearchText     string
	LastSearchText string

	// Cursor state
	CursorX, CursorY int
	CursorVisible    bool
	CursorError      bool
	CursorErrorTime  time.Time
	CursorBlinkTime  time.Time

	// Motion command state
	MotionCount    int
	MotionCommand  string
	WaitingForF    bool
	CommandPrefix  rune
	StatusMessage  string
	DeleteOperator bool
	LastCommand    string // Last executed command for display

	// Score tracking
	Score            int
	ScoreIncrement   int
	ScoreBlinkActive bool
	ScoreBlinkColor  tcell.Color
	ScoreBlinkTime   time.Time

	// Trail state
	TrailEnabled bool
	TrailTimer   *time.Timer
	TrailEndTime time.Time

	// Ping coordinates feature
	PingActive    bool
	PingGridTimer float64 // Timer in seconds for ping grid (0 = inactive)
	PingStartTime time.Time
	PingRow       int
	PingCol       int

	// Heat tracking
	LastMoveKey      rune
	ConsecutiveCount int

	// Spawn tracking
	LastSpawn time.Time
	NextSeqID int
}

// NewGameContext creates a new game context with initialized ECS world
func NewGameContext(screen tcell.Screen) *GameContext {
	width, height := screen.Size()

	ctx := &GameContext{
		World:           NewWorld(),
		Screen:          screen,
		Width:           width,
		Height:          height,
		Mode:            ModeNormal,
		CursorVisible:   true,
		CursorBlinkTime: time.Now(),
		NextSeqID:       1,
		Score:           0,
		ScoreIncrement:  0,
		LastSpawn:       time.Now(),
	}

	ctx.updateGameArea()
	ctx.CursorX = ctx.GameWidth / 2
	ctx.CursorY = ctx.GameHeight / 2

	// Create buffer
	ctx.Buffer = core.NewBuffer(ctx.GameWidth, ctx.GameHeight)

	return ctx
}

// updateGameArea calculates the game area dimensions
func (g *GameContext) updateGameArea() {
	// Calculate line number width based on height
	// We need 1 line for heat meter at top, 2 lines for column indicator and status bar at bottom
	gameHeight := g.Height - 3
	if gameHeight < 1 {
		gameHeight = 1
	}

	lineNumWidth := len(formatNumber(gameHeight))
	if lineNumWidth < 1 {
		lineNumWidth = 1
	}

	g.LineNumWidth = lineNumWidth
	g.GameX = lineNumWidth + 1 // line number + 1 space
	g.GameY = 1                // Start at row 1 (row 0 is for heat meter)
	g.GameWidth = g.Width - g.GameX
	g.GameHeight = gameHeight

	if g.GameWidth < 1 {
		g.GameWidth = 1
	}
}

// formatNumber is a helper to format line numbers
func formatNumber(n int) string {
	// Simple implementation - just count digits
	if n == 0 {
		return "0"
	}
	digits := 0
	for n > 0 {
		digits++
		n /= 10
	}
	result := make([]byte, digits)
	return string(result)
}

// HandleResize handles terminal resize events
func (g *GameContext) HandleResize() {
	newWidth, newHeight := g.Screen.Size()
	if newWidth != g.Width || newHeight != g.Height {
		g.Width = newWidth
		g.Height = newHeight
		g.updateGameArea()

		// Resize buffer
		if g.Buffer != nil {
			g.Buffer.Resize(g.GameWidth, g.GameHeight)
		}

		// Clamp cursor position
		if g.CursorX >= g.GameWidth {
			g.CursorX = g.GameWidth - 1
		}
		if g.CursorY >= g.GameHeight {
			g.CursorY = g.GameHeight - 1
		}
		if g.CursorX < 0 {
			g.CursorX = 0
		}
		if g.CursorY < 0 {
			g.CursorY = 0
		}
	}
}

// IsInsertMode returns true if in insert mode
func (g *GameContext) IsInsertMode() bool {
	return g.Mode == ModeInsert
}

// IsSearchMode returns true if in search mode
func (g *GameContext) IsSearchMode() bool {
	return g.Mode == ModeSearch
}

// IsNormalMode returns true if in normal mode
func (g *GameContext) IsNormalMode() bool {
	return g.Mode == ModeNormal
}
