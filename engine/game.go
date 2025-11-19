package engine

import (
	"sync/atomic"
	"time"
	"unsafe"

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
	// Central game state (spawn/content/phase state management)
	State *GameState

	// ECS World
	World *World

	// Screen and buffer
	Screen tcell.Screen
	Buffer *core.Buffer

	// Time provider (monotonic clock for animations)
	TimeProvider TimeProvider

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

	// Cursor state (local to input handling)
	CursorX, CursorY int // These will be synced to State.CursorX/Y
	CursorVisible    bool
	CursorBlinkTime  time.Time

	// Motion command state (input parsing - not game mechanics)
	MotionCount         int
	MotionCommand       string
	WaitingForF         bool
	WaitingForFBackward bool
	PendingCount        int // Preserved count for multi-keystroke commands (e.g., 2fa, 3Fb)
	CommandPrefix       rune
	StatusMessage       string
	DeleteOperator      bool
	LastCommand         string // Last executed command for display

	// Atomic ping coordinates feature (local to input handling)
	pingActive    atomic.Bool
	pingGridTimer atomic.Uint64 // float64 bits for seconds
	PingRow       int
	PingCol       int

	// Heat tracking (for consecutive move penalty - input specific)
	LastMoveKey      rune
	ConsecutiveCount int
}

// NewGameContext creates a new game context with initialized ECS world
func NewGameContext(screen tcell.Screen) *GameContext {
	width, height := screen.Size()
	timeProvider := NewMonotonicTimeProvider()

	ctx := &GameContext{
		World:           NewWorld(),
		Screen:          screen,
		TimeProvider:    timeProvider,
		Width:           width,
		Height:          height,
		Mode:            ModeNormal,
		CursorVisible:   true,
		CursorBlinkTime: timeProvider.Now(),
	}

	// Calculate game area first
	ctx.updateGameArea()

	// Create centralized game state
	ctx.State = NewGameState(ctx.GameWidth, ctx.GameHeight, ctx.Width, timeProvider)

	// Initialize local cursor position
	ctx.CursorX = ctx.GameWidth / 2
	ctx.CursorY = ctx.GameHeight / 2

	// Sync cursor to game state
	ctx.State.SetCursorX(ctx.CursorX)
	ctx.State.SetCursorY(ctx.CursorY)

	// Initialize ping atomic values (still local to input handling)
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)

	// Create buffer
	ctx.Buffer = core.NewBuffer(ctx.GameWidth, ctx.GameHeight)

	return ctx
}

// ===== INPUT-SPECIFIC METHODS =====
// These methods are specific to GameContext and not delegated to GameState

// Atomic accessor methods for PingActive
func (g *GameContext) GetPingActive() bool {
	return g.pingActive.Load()
}

func (g *GameContext) SetPingActive(active bool) {
	g.pingActive.Store(active)
}

// Atomic accessor methods for PingGridTimer
func (g *GameContext) GetPingGridTimer() float64 {
	bits := g.pingGridTimer.Load()
	return *(*float64)(unsafe.Pointer(&bits))
}

func (g *GameContext) SetPingGridTimer(seconds float64) {
	bits := *(*uint64)(unsafe.Pointer(&seconds))
	g.pingGridTimer.Store(bits)
}

func (g *GameContext) AddPingGridTimer(delta float64) {
	for {
		oldBits := g.pingGridTimer.Load()
		oldValue := *(*float64)(unsafe.Pointer(&oldBits))
		newValue := oldValue + delta
		newBits := *(*uint64)(unsafe.Pointer(&newValue))
		if g.pingGridTimer.CompareAndSwap(oldBits, newBits) {
			break
		}
	}
}

// UpdatePingGridTimerAtomic atomically decrements the ping timer and returns true if it expired
// This method handles the check-then-set atomically to avoid race conditions
func (g *GameContext) UpdatePingGridTimerAtomic(delta float64) bool {
	for {
		oldBits := g.pingGridTimer.Load()
		oldValue := *(*float64)(unsafe.Pointer(&oldBits))

		if oldValue <= 0 {
			// Timer already expired or not active
			return false
		}

		newValue := oldValue - delta
		var newBits uint64

		if newValue <= 0 {
			// Timer will expire, set to 0
			newBits = 0
		} else {
			// Timer still active
			newBits = *(*uint64)(unsafe.Pointer(&newValue))
		}

		if g.pingGridTimer.CompareAndSwap(oldBits, newBits) {
			// Successfully updated, return true if timer expired
			return newValue <= 0
		}
	}
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

// GetFrameNumber returns the current frame number
func (g *GameContext) GetFrameNumber() int64 {
	return g.State.GetFrameNumber()
}

// IncrementFrameNumber increments and returns the frame number
func (g *GameContext) IncrementFrameNumber() int64 {
	return g.State.IncrementFrameNumber()
}