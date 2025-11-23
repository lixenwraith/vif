package engine

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
)

// GameMode represents the current input mode
type GameMode int

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
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
	TimeProvider  TimeProvider   // Pausable game time
	PausableClock *PausableClock // Direct access for pause control

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
	CommandText    string

	// Cursor state (local to input handling)
	CursorX, CursorY int // These will be synced to State.CursorX/Y
	// CursorVisible    bool
	// CursorBlinkTime  time.Time

	// Motion command state (input parsing - not game mechanics)
	MotionCount         int
	MotionCommand       string
	WaitingForF         bool
	WaitingForFBackward bool
	WaitingForT         bool
	WaitingForTBackward bool
	PendingCount        int // Preserved count for multi-keystroke commands (e.g., 2fa, 3Fb)
	CommandPrefix       rune
	StatusMessage       string
	DeleteOperator      bool
	LastCommand         string // Last executed command for display

	// Find/Till motion state (for ; and , repeat commands)
	LastFindChar    rune // Character that was searched for
	LastFindForward bool // true for f/t (forward), false for F/T (backward)
	LastFindType    rune // Type of last find: 'f', 'F', 't', or 'T'

	// Atomic ping coordinates feature (local to input handling)
	pingActive    atomic.Bool
	pingGridTimer atomic.Uint64 // float64 bits for seconds
	PingRow       int
	PingCol       int

	// Pause state management (simplified - actual pause handled by PausableClock)
	IsPaused atomic.Bool

	// Audio engine (nil if audio disabled or initialization failed)
	AudioEngine interface {
		SendRealTime(cmd audio.AudioCommand) bool
		SendState(cmd audio.AudioCommand) bool
		StopCurrentSound()
		DrainQueues()
		IsRunning() bool
		ToggleMute() bool
		IsMuted() bool
	}

	// Heat tracking (for consecutive move penalty - input specific)
	LastMoveKey      rune
	ConsecutiveCount int
}

// NewGameContext creates a new game context with initialized ECS world
func NewGameContext(screen tcell.Screen) *GameContext {
	width, height := screen.Size()

	// Create pausable clock
	pausableClock := NewPausableClock()

	ctx := &GameContext{
		World:         NewWorld(),
		Screen:        screen,
		TimeProvider:  pausableClock, // Use pausable clock as TimeProvider
		PausableClock: pausableClock, // Direct reference for pause control
		Width:         width,
		Height:        height,
		Mode:          ModeNormal,
		// CursorVisible:   true,
		// CursorBlinkTime: pausableClock.RealTime(), // UI uses real time
	}

	// Calculate game area first
	ctx.updateGameArea()

	// Create centralized game state with pausable time provider
	ctx.State = NewGameState(ctx.GameWidth, ctx.GameHeight, ctx.Width, pausableClock)

	// Initialize local cursor position
	ctx.CursorX = ctx.GameWidth / 2
	ctx.CursorY = ctx.GameHeight / 2

	// Sync cursor to game state
	ctx.State.SetCursorX(ctx.CursorX)
	ctx.State.SetCursorY(ctx.CursorY)

	// Initialize ping atomic values (still local to input handling)
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)

	// Initialize pause state
	ctx.IsPaused.Store(false)

	// Create buffer
	ctx.Buffer = core.NewBuffer(ctx.GameWidth, ctx.GameHeight)

	return ctx
}

// ===== INPUT-SPECIFIC METHODS =====

// GetPingActive returns the current ping active state
func (g *GameContext) GetPingActive() bool {
	return g.pingActive.Load()
}

// SetPingActive sets the ping active state
func (g *GameContext) SetPingActive(active bool) {
	g.pingActive.Store(active)
}

// GetPingGridTimer returns the current ping grid timer value in seconds
func (g *GameContext) GetPingGridTimer() float64 {
	bits := g.pingGridTimer.Load()
	return math.Float64frombits(bits)
}

// SetPingGridTimer sets the ping grid timer to the specified seconds
func (g *GameContext) SetPingGridTimer(seconds float64) {
	bits := math.Float64bits(seconds)
	g.pingGridTimer.Store(bits)
}

// UpdatePingGridTimerAtomic atomically decrements the ping timer and returns true if it expired
// This method handles the check-then-set atomically to avoid race conditions
func (g *GameContext) UpdatePingGridTimerAtomic(delta float64) bool {
	for {
		oldBits := g.pingGridTimer.Load()
		oldValue := math.Float64frombits(oldBits)

		if oldValue <= 0 {
			// Timer already expired or not active
			return false
		}

		newValue := oldValue - delta
		var newBits uint64

		if newValue <= 0 {
			// Timer will expire, set to 0
			newBits = 0 // 0.0 float is 0 uint64
		} else {
			// Timer still active
			newBits = math.Float64bits(newValue)
		}

		if g.pingGridTimer.CompareAndSwap(oldBits, newBits) {
			// Successfully updated, return true if timer expired
			return newValue <= 0
		}
	}
}

// SetPaused sets the pause state using the pausable clock
func (g *GameContext) SetPaused(paused bool) {
	wasPaused := g.IsPaused.Load()
	g.IsPaused.Store(paused)

	if paused && !wasPaused {
		// Starting pause
		g.PausableClock.Pause()
		g.StopAudio()
	} else if !paused && wasPaused {
		// Ending pause
		g.PausableClock.Resume()
	}
}

// GetPauseDuration returns the current pause duration
func (g *GameContext) GetPauseDuration() time.Duration {
	return g.PausableClock.GetCurrentPauseDuration()
}

// GetTotalPauseDuration returns the cumulative pause time
func (g *GameContext) GetTotalPauseDuration() time.Duration {
	return g.PausableClock.GetTotalPauseDuration()
}

// GetRealTime returns wall clock time for UI elements
func (g *GameContext) GetRealTime() time.Time {
	return g.PausableClock.RealTime()
}

// StopAudio stops the current audio and drains queues if audio engine is available
func (g *GameContext) StopAudio() {
	if g.AudioEngine != nil && g.AudioEngine.IsRunning() {
		g.AudioEngine.StopCurrentSound()
		g.AudioEngine.DrainQueues()
	}
}

// ToggleAudioMute toggles the mute state of the audio engine
// Returns the new mute state (true if muted, false if unmuted)
func (g *GameContext) ToggleAudioMute() bool {
	if g.AudioEngine != nil {
		return g.AudioEngine.ToggleMute()
	}
	return false
}

// updateGameArea calculates the game area dimensions
func (g *GameContext) updateGameArea() {
	// Calculate line number width based on height
	gameHeight := g.Height - constants.BottomMargin - constants.TopMargin
	if gameHeight < 1 {
		gameHeight = 1
	}

	lineNumWidth := len(formatNumber(gameHeight))
	if lineNumWidth < 1 {
		lineNumWidth = 1
	}

	g.LineNumWidth = lineNumWidth
	g.GameX = lineNumWidth + 1    // line number + 1 space
	g.GameY = constants.TopMargin // Start at row 1 (row 0 is for heat meter)
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

// TODO: Systems don't handle resize
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

// IsCommandMode returns true if in command mode
func (g *GameContext) IsCommandMode() bool {
	return g.Mode == ModeCommand
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