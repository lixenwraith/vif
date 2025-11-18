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

	// Cursor state
	CursorX, CursorY int
	CursorVisible    bool
	CursorBlinkTime  time.Time

	// Atomic cursor error state
	cursorError     atomic.Bool
	cursorErrorTime atomic.Int64 // UnixNano

	// Motion command state
	MotionCount    int
	MotionCommand  string
	WaitingForF    bool
	PendingCount   int  // Preserved count for multi-keystroke commands (e.g., 2fa)
	CommandPrefix  rune
	StatusMessage  string
	DeleteOperator bool
	LastCommand    string // Last executed command for display

	// Atomic score tracking
	score            atomic.Int64
	scoreIncrement   atomic.Int64
	scoreBlinkActive atomic.Bool
	scoreBlinkColor  atomic.Uint32 // tcell.Color is uint32
	scoreBlinkTime   atomic.Int64  // UnixNano

	// Atomic boost state (heat multiplier mechanic)
	boostEnabled      atomic.Bool
	boostEndTime      atomic.Int64 // UnixNano
	boostSequenceColor atomic.Int32 // 0=None, 1=Blue, 2=Green

	// Atomic ping coordinates feature
	pingActive    atomic.Bool
	pingGridTimer atomic.Uint64 // float64 bits for seconds
	PingRow       int
	PingCol       int

	// Heat tracking
	LastMoveKey      rune
	ConsecutiveCount int

	// Spawn tracking
	NextSeqID int
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
		NextSeqID:       1,
	}

	// Initialize atomic values
	ctx.score.Store(0)
	ctx.scoreIncrement.Store(0)
	ctx.scoreBlinkActive.Store(false)
	ctx.scoreBlinkColor.Store(0)
	ctx.scoreBlinkTime.Store(0)
	ctx.boostEnabled.Store(false)
	ctx.boostEndTime.Store(0)
	ctx.boostSequenceColor.Store(0) // 0 = None
	ctx.cursorError.Store(false)
	ctx.cursorErrorTime.Store(0)
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)

	ctx.updateGameArea()
	ctx.CursorX = ctx.GameWidth / 2
	ctx.CursorY = ctx.GameHeight / 2

	// Create buffer
	ctx.Buffer = core.NewBuffer(ctx.GameWidth, ctx.GameHeight)

	return ctx
}

// Atomic accessor methods for Score
func (g *GameContext) GetScore() int {
	return int(g.score.Load())
}

func (g *GameContext) SetScore(score int) {
	g.score.Store(int64(score))
}

func (g *GameContext) AddScore(delta int) {
	g.score.Add(int64(delta))
}

// Atomic accessor methods for ScoreIncrement
func (g *GameContext) GetScoreIncrement() int {
	return int(g.scoreIncrement.Load())
}

func (g *GameContext) SetScoreIncrement(increment int) {
	g.scoreIncrement.Store(int64(increment))
}

func (g *GameContext) AddScoreIncrement(delta int) {
	g.scoreIncrement.Add(int64(delta))
}

// Atomic accessor methods for ScoreBlinkActive
func (g *GameContext) GetScoreBlinkActive() bool {
	return g.scoreBlinkActive.Load()
}

func (g *GameContext) SetScoreBlinkActive(active bool) {
	g.scoreBlinkActive.Store(active)
}

// Atomic accessor methods for ScoreBlinkColor
func (g *GameContext) GetScoreBlinkColor() tcell.Color {
	return tcell.Color(g.scoreBlinkColor.Load())
}

func (g *GameContext) SetScoreBlinkColor(color tcell.Color) {
	g.scoreBlinkColor.Store(uint32(color))
}

// Atomic accessor methods for ScoreBlinkTime
func (g *GameContext) GetScoreBlinkTime() time.Time {
	nano := g.scoreBlinkTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (g *GameContext) SetScoreBlinkTime(t time.Time) {
	g.scoreBlinkTime.Store(t.UnixNano())
}

// Atomic accessor methods for BoostEnabled
func (g *GameContext) GetBoostEnabled() bool {
	return g.boostEnabled.Load()
}

func (g *GameContext) SetBoostEnabled(enabled bool) {
	g.boostEnabled.Store(enabled)
}

// Atomic accessor methods for BoostEndTime
func (g *GameContext) GetBoostEndTime() time.Time {
	nano := g.boostEndTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (g *GameContext) SetBoostEndTime(t time.Time) {
	g.boostEndTime.Store(t.UnixNano())
}

// Atomic accessor methods for BoostSequenceColor
// Color values: 0=None, 1=Blue, 2=Green
func (g *GameContext) GetBoostSequenceColor() int32 {
	return g.boostSequenceColor.Load()
}

func (g *GameContext) SetBoostSequenceColor(color int32) {
	g.boostSequenceColor.Store(color)
}

// UpdateBoostTimerAtomic atomically checks if boost should expire and disables it
// Returns true if boost was disabled due to expiration
// Uses CAS pattern to avoid race conditions in check-then-set
func (g *GameContext) UpdateBoostTimerAtomic() bool {
	// Check if boost is currently enabled
	if !g.boostEnabled.Load() {
		return false
	}

	// Get current time
	now := g.TimeProvider.Now()

	// Get boost end time
	endTimeNano := g.boostEndTime.Load()
	if endTimeNano == 0 {
		return false
	}
	endTime := time.Unix(0, endTimeNano)

	// Check if expired
	if now.After(endTime) {
		// Try to atomically disable boost using CAS
		// Only disable if it's still enabled (another goroutine might have disabled it)
		if g.boostEnabled.CompareAndSwap(true, false) {
			// Reset boost sequence color when boost expires
			g.boostSequenceColor.Store(0) // 0 = None
			return true
		}
	}

	return false
}

// Atomic accessor methods for CursorError
func (g *GameContext) GetCursorError() bool {
	return g.cursorError.Load()
}

func (g *GameContext) SetCursorError(error bool) {
	g.cursorError.Store(error)
}

// Atomic accessor methods for CursorErrorTime
func (g *GameContext) GetCursorErrorTime() time.Time {
	nano := g.cursorErrorTime.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (g *GameContext) SetCursorErrorTime(t time.Time) {
	g.cursorErrorTime.Store(t.UnixNano())
}

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
