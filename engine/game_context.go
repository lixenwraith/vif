package engine

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// GameMode represents the current input mode
type GameMode int

const (
	ModeNormal GameMode = iota
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// Central game state (spawn/content/phase state management)
	State *GameState

	// ECS World
	World *World

	// Event queue for inter-system communication
	eventQueue *EventQueue

	// Terminal interface
	Terminal terminal.Terminal

	// Pausable Clock time provider
	PausableClock *PausableClock

	// Crash handling
	crashHandler func(any)

	// Screen dimensions
	Width, Height int

	// Game area (excluding UI elements)
	GameX, GameY          int
	GameWidth, GameHeight int
	LineNumWidth          int

	// Cursor entity (singleton)
	CursorEntity Entity

	// Mode state
	Mode           GameMode
	SearchText     string
	LastSearchText string
	CommandText    string

	// Overlay state
	OverlayActive  bool
	OverlayTitle   string
	OverlayContent []string
	OverlayScroll  int

	// Motion command state (input parsing - not game mechanics)
	StatusMessage string
	LastCommand   string // Last executed command for display

	// Find/Till motion state (for ; and , repeat commands)
	LastFindChar    rune // Character that was searched for
	LastFindForward bool // true for f/t (forward), false for F/T (backward)
	LastFindType    rune // Type of last find: 'f', 'F', 't', or 'T'

	// Atomic ping coordinates feature (local to input handling)
	pingActive    atomic.Bool
	pingGridTimer atomic.Uint64 // float64 bits for seconds

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
}

// NewGameContext creates a new game context with initialized ECS world
func NewGameContext(term terminal.Terminal) *GameContext {
	// Get terminal size
	width, height := term.Size()

	// Create pausable clock
	pausableClock := NewPausableClock()

	ctx := &GameContext{
		World:         NewWorld(),
		Terminal:      term,
		PausableClock: pausableClock,
		Width:         width,
		Height:        height,
		Mode:          ModeNormal,
		eventQueue:    NewEventQueue(),
	}

	// Default crash handler (just panic again if not set)
	ctx.crashHandler = func(r any) {
		panic(r)
	}

	// Calculate game area first
	ctx.updateGameArea()

	// -- Initialize Core Resources --

	// 1. Config Resource
	configRes := &ConfigResource{
		ScreenWidth:  ctx.Width,
		ScreenHeight: ctx.Height,
		GameWidth:    ctx.GameWidth,
		GameHeight:   ctx.GameHeight,
		GameX:        ctx.GameX,
		GameY:        ctx.GameY,
	}
	AddResource(ctx.World.Resources, configRes)

	// 2. Time Resource (Initial state)
	timeRes := &TimeResource{
		GameTime:    pausableClock.Now(),
		RealTime:    pausableClock.RealTime(),
		DeltaTime:   0,
		FrameNumber: 0,
	}
	AddResource(ctx.World.Resources, timeRes)

	// 3. Input Resource (Initial state)
	inputRes := &InputResource{
		GameMode: ResourceModeNormal,
		IsPaused: false,
	}
	AddResource(ctx.World.Resources, inputRes)

	// Create centralized game state with pausable time provider
	ctx.State = NewGameState(constants.MaxEntities, pausableClock.Now())

	// Create cursor entity at the center of the screen
	ctx.CursorEntity = ctx.World.CreateEntity()
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: ctx.GameWidth / 2,
		Y: ctx.GameHeight / 2,
	})
	ctx.World.Cursors.Add(ctx.CursorEntity, components.CursorComponent{})

	// Make cursor indestructible
	ctx.World.Protections.Add(ctx.CursorEntity, components.ProtectionComponent{
		Mask:      components.ProtectAll,
		ExpiresAt: 0, // Permanent
	})

	// Add ShieldComponent to cursor (initially invisible via GameState.ShieldActive)
	// This ensures the renderer and systems have the geometric data needed when the shield activates
	ctx.World.Shields.Add(ctx.CursorEntity, components.ShieldComponent{
		Sources:       0, // Field deprecated/unused in event-driven system
		RadiusX:       constants.ShieldRadiusX,
		RadiusY:       constants.ShieldRadiusY,
		OverrideColor: components.ColorNone,
		MaxOpacity:    constants.ShieldMaxOpacity,
		LastDrainTime: pausableClock.Now(),
	})

	// Initialize ping atomic values (still local to input handling)
	ctx.pingActive.Store(false)
	ctx.pingGridTimer.Store(0)

	// Initialize pause state
	ctx.IsPaused.Store(false)

	return ctx
}

// ===== Crash Handling and Panic Management =====

// SetCrashHandler sets the function called when a background goroutine panics
func (ctx *GameContext) SetCrashHandler(handler func(any)) {
	ctx.crashHandler = handler
}

// Go runs a function in a new goroutine with panic recovery
// Use this instead of 'go func()' for game systems to ensure terminal cleanup
func (ctx *GameContext) Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ctx.crashHandler(r)
			}
		}()
		fn()
	}()
}

// ===== INPUT-SPECIFIC METHODS =====

// GetPingActive returns the current ping active state
func (ctx *GameContext) GetPingActive() bool {
	return ctx.pingActive.Load()
}

// SetPingActive sets the ping active state
func (ctx *GameContext) SetPingActive(active bool) {
	ctx.pingActive.Store(active)
}

// GetPingGridTimer returns the current ping grid timer value in seconds
func (ctx *GameContext) GetPingGridTimer() float64 {
	bits := ctx.pingGridTimer.Load()
	return math.Float64frombits(bits)
}

// SetPingGridTimer sets the ping grid timer to the specified seconds
func (ctx *GameContext) SetPingGridTimer(seconds float64) {
	bits := math.Float64bits(seconds)
	ctx.pingGridTimer.Store(bits)
}

// UpdatePingGridTimerAtomic atomically decrements the ping timer and returns true if it expired
// This method handles the check-then-set atomically to avoid race conditions
func (ctx *GameContext) UpdatePingGridTimerAtomic(delta float64) bool {
	for {
		oldBits := ctx.pingGridTimer.Load()
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

		if ctx.pingGridTimer.CompareAndSwap(oldBits, newBits) {
			// Successfully updated, return true if timer expired
			return newValue <= 0
		}
	}
}

// SetPaused sets the pause state using the pausable clock
func (ctx *GameContext) SetPaused(paused bool) {
	wasPaused := ctx.IsPaused.Load()
	ctx.IsPaused.Store(paused)

	if paused && !wasPaused {
		// Starting pause
		ctx.PausableClock.Pause()
		ctx.StopAudio()
	} else if !paused && wasPaused {
		// Ending pause
		ctx.PausableClock.Resume()
	}
}

// GetPauseDuration returns the current pause duration
func (ctx *GameContext) GetPauseDuration() time.Duration {
	return ctx.PausableClock.GetCurrentPauseDuration()
}

// GetTotalPauseDuration returns the cumulative pause time
func (ctx *GameContext) GetTotalPauseDuration() time.Duration {
	return ctx.PausableClock.GetTotalPauseDuration()
}

// GetRealTime returns wall clock time for UI elements
func (ctx *GameContext) GetRealTime() time.Time {
	return ctx.PausableClock.RealTime()
}

// StopAudio stops the current audio and drains queues if audio engine is available
func (ctx *GameContext) StopAudio() {
	if ctx.AudioEngine != nil && ctx.AudioEngine.IsRunning() {
		ctx.AudioEngine.StopCurrentSound()
		ctx.AudioEngine.DrainQueues()
	}
}

// ToggleAudioMute toggles the mute state of the audio engine
// Returns the new mute state (true if muted, false if unmuted)
func (ctx *GameContext) ToggleAudioMute() bool {
	if ctx.AudioEngine != nil {
		return ctx.AudioEngine.ToggleMute()
	}
	return false
}

// updateGameArea calculates the game area dimensions
func (ctx *GameContext) updateGameArea() {
	// Calculate line number width based on height
	gameHeight := ctx.Height - constants.BottomMargin - constants.TopMargin
	if gameHeight < 1 {
		gameHeight = 1
	}

	lineNumWidth := formatNumber(gameHeight)
	if lineNumWidth < 1 {
		lineNumWidth = 1
	}

	ctx.LineNumWidth = lineNumWidth
	ctx.GameX = lineNumWidth + 1    // line number + 1 space
	ctx.GameY = constants.TopMargin // Start at row 1 (row 0 is for heat meter)
	ctx.GameWidth = ctx.Width - ctx.GameX
	ctx.GameHeight = gameHeight

	if ctx.GameWidth < 1 {
		ctx.GameWidth = 1
	}
}

// formatNumber returns the number of digits needed to display n
func formatNumber(n int) int {
	if n == 0 {
		return 1
	}
	digits := 0
	for n > 0 {
		digits++
		n /= 10
	}
	return digits
}

// HandleResize handles terminal resize events
func (ctx *GameContext) HandleResize() {
	newWidth, newHeight := ctx.Terminal.Size()
	if newWidth != ctx.Width || newHeight != ctx.Height {
		ctx.Width = newWidth
		ctx.Height = newHeight
		ctx.updateGameArea()

		// Update ConfigResource
		configRes := &ConfigResource{
			ScreenWidth:  ctx.Width,
			ScreenHeight: ctx.Height,
			GameWidth:    ctx.GameWidth,
			GameHeight:   ctx.GameHeight,
			GameX:        ctx.GameX,
			GameY:        ctx.GameY,
		}
		AddResource(ctx.World.Resources, configRes)

		// TODO: Optional disable (world.crop)
		// Cleanup entities outside new bounds to prevent ghosting/resource usage
		// Uses GameWidth/Height as valid coordinate space for entities, resizes Spatial Grid
		ctx.cleanupOutOfBoundsEntities(ctx.GameWidth, ctx.GameHeight)

		// Clamp cursor position
		if pos, ok := ctx.World.Positions.Get(ctx.CursorEntity); ok {
			pos.X = max(0, min(pos.X, ctx.GameWidth-1))
			pos.Y = max(0, min(pos.Y, ctx.GameHeight-1))
			ctx.World.Positions.Add(ctx.CursorEntity, pos)
		}
	}
}

// cleanupOutOfBoundsEntities removes entities that are outside the valid game area
// and resizes the spatial grid to match new dimensions
func (ctx *GameContext) cleanupOutOfBoundsEntities(width, height int) {
	// Unified cleanup: single PositionStore iteration handles all entity types
	allEntities := ctx.World.Positions.All()
	for _, e := range allEntities {
		// Skip cursor entity (special case)
		if e == ctx.CursorEntity {
			continue
		}

		// Check protection flags before culling
		if prot, ok := ctx.World.Protections.Get(e); ok {
			if prot.Mask.Has(components.ProtectFromCull) || prot.Mask == components.ProtectAll {
				continue
			}
		}

		// Destroy entities outside valid coordinate space [0, width) × [0, height)
		pos, _ := ctx.World.Positions.Get(e)
		if pos.X >= width || pos.Y >= height || pos.X < 0 || pos.Y < 0 {
			ctx.World.DestroyEntity(e)
		}
	}

	// Resize spatial grid to match new dimensions
	ctx.World.Positions.ResizeGrid(width, height)
}

// IsInsertMode returns true if in insert mode
func (ctx *GameContext) IsInsertMode() bool {
	return ctx.Mode == ModeInsert
}

// IsSearchMode returns true if in search mode
func (ctx *GameContext) IsSearchMode() bool {
	return ctx.Mode == ModeSearch
}

// IsCommandMode returns true if in command mode
func (ctx *GameContext) IsCommandMode() bool {
	return ctx.Mode == ModeCommand
}

// IsNormalMode returns true if in normal mode
func (ctx *GameContext) IsNormalMode() bool {
	return ctx.Mode == ModeNormal
}

// IsOverlayMode returns true if in overlay mode
func (ctx *GameContext) IsOverlayMode() bool {
	return ctx.Mode == ModeOverlay
}

// GetFrameNumber returns the current frame number
func (ctx *GameContext) GetFrameNumber() int64 {
	return ctx.State.GetFrameNumber()
}

// IncrementFrameNumber increments and returns the frame number
func (ctx *GameContext) IncrementFrameNumber() int64 {
	return ctx.State.IncrementFrameNumber()
}

// ===== EVENT QUEUE METHODS =====

// PushEvent adds an event to the event queue with current frame number and provided timestamp
func (ctx *GameContext) PushEvent(eventType EventType, payload any, now time.Time) {
	event := GameEvent{
		Type:      eventType,
		Payload:   payload,
		Frame:     ctx.State.GetFrameNumber(),
		Timestamp: now,
	}
	ctx.eventQueue.Push(event)
}

// ResetEventQueue clears all pending events from the queue
// Must be called inside a RunSafe block or during initialization
func (ctx *GameContext) ResetEventQueue() {
	ctx.eventQueue.Reset()
}