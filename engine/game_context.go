package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/events"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// UISnapshot provides a consistent view of UI state for rendering
type UISnapshot struct {
	CommandText    string
	SearchText     string
	StatusMessage  string
	LastCommand    string
	OverlayActive  bool
	OverlayTitle   string
	OverlayContent []string
	OverlayScroll  int
}

// GameContext holds all game state including the ECS world
type GameContext struct {
	// Central game state (spawn/content/phase state management)
	State *GameState

	// ECS World
	World *World

	// Event queue for inter-system communication
	eventQueue *events.EventQueue

	// Terminal interface
	Terminal terminal.Terminal

	// Pausable Clock time provider
	PausableClock *PausableClock

	// Crash handling
	crashHandler func(any)

	// Screen dimensions
	Width, Height int

	// Game area (excluding UI elements)
	// TODO: review, usage and resize race
	GameX, GameY          int
	GameWidth, GameHeight int
	// TODO: review
	LineNumWidth int

	// Cursor entity (singleton)
	CursorEntity Entity

	// --- Thread-Safe State ---

	// Mode state (Atomic)
	mode atomic.Int32

	// UI State (Mutex Protected)
	ui struct {
		mu             sync.RWMutex
		commandText    string
		searchText     string
		statusMessage  string
		lastCommand    string
		overlayActive  bool
		overlayTitle   string
		overlayContent []string
		overlayScroll  int
	}

	// LastSearchText is kept public as it is internal to InputHandler state (no race with renderers)
	LastSearchText string

	// Find/Till motion state (for ; and , repeat commands)
	LastFindChar    rune // Character that was searched for
	LastFindForward bool // true for f/t (forward), false for F/T (backward)
	LastFindType    rune // Type of last find: 'f', 'F', 't', or 'T'

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
		eventQueue:    events.NewEventQueue(),
	}

	// Default crash handler (just panic again if not set)
	ctx.crashHandler = func(r any) {
		panic(r)
	}

	// Initialize atomic mode
	ctx.SetMode(core.ModeNormal)

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

	// 4. Game State
	// Create centralized game state with pausable time provider
	ctx.State = NewGameState(constants.MaxEntities, pausableClock.Now())

	// 5. Cursor Entity
	ctx.CreateCursorEntity()

	// Initialize pause state
	ctx.IsPaused.Store(false)

	return ctx
}

// CreateCursorEntity handles standard cursor entity creation and component attachment
// Centralizes logic shared between NewGameContext and :new command
func (ctx *GameContext) CreateCursorEntity() {
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

	// Add PingComponent to cursor (handles crosshair and grid state)
	ctx.World.Pings.Add(ctx.CursorEntity, components.PingComponent{
		ShowCrosshair:  true,
		CrosshairColor: components.ColorNormal,
		GridActive:     false,
		GridRemaining:  0,
		GridColor:      components.ColorNormal,
		ContextAware:   true,
	})

	// Add HeatComponent to cursor
	ctx.World.Heats.Add(ctx.CursorEntity, components.HeatComponent{})

	// Add EnergyComponent to cursor (tracks energy and blink state)
	ctx.World.Energies.Add(ctx.CursorEntity, components.EnergyComponent{})

	// Add ShieldComponent to cursor (initially invisible via GameState.ShieldActive)
	// This ensures the renderer and systems have the geometric data needed when the shield activates
	ctx.World.Shields.Add(ctx.CursorEntity, components.ShieldComponent{
		RadiusX:       constants.ShieldRadiusX,
		RadiusY:       constants.ShieldRadiusY,
		OverrideColor: components.ColorNone,
		MaxOpacity:    constants.ShieldMaxOpacity,
		LastDrainTime: ctx.PausableClock.Now(),
	})
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

// cleanupOutOfBoundsEntities tags entities that are outside the valid game area
// Actual destruction is handled by CullSystem in the next tick
func (ctx *GameContext) cleanupOutOfBoundsEntities(width, height int) {
	// Unified cleanup: single PositionStore iteration handles all entity types
	allEntities := ctx.World.Positions.All()
	for _, e := range allEntities {
		// Skip cursor entity (special case)
		if e == ctx.CursorEntity {
			continue
		}

		// Check protection flags before marking
		if prot, ok := ctx.World.Protections.Get(e); ok {
			if prot.Mask.Has(components.ProtectFromCull) || prot.Mask == components.ProtectAll {
				continue
			}
		}

		// Mark entities outside valid coordinate space [0, width) × [0, height)
		// We use OutOfBoundsComponent instead of immediate destruction to allow
		// systems (like GoldSystem) to detect the loss and update GameState
		pos, _ := ctx.World.Positions.Get(e)
		if pos.X >= width || pos.Y >= height || pos.X < 0 || pos.Y < 0 {
			ctx.World.MarkedForDeaths.Add(e, components.MarkedForDeathComponent{})
		}
	}

	// Resize spatial grid to match new dimensions
	ctx.World.Positions.ResizeGrid(width, height)
}

// ===== MODE ACCESSORS =====

// GetMode returns the current game mode
func (ctx *GameContext) GetMode() core.GameMode {
	return core.GameMode(ctx.mode.Load())
}

// SetMode sets the current game mode
func (ctx *GameContext) SetMode(m core.GameMode) {
	ctx.mode.Store(int32(m))
}

// IsInsertMode returns true if in insert mode
func (ctx *GameContext) IsInsertMode() bool {
	return ctx.GetMode() == core.ModeInsert
}

// IsSearchMode returns true if in search mode
func (ctx *GameContext) IsSearchMode() bool {
	return ctx.GetMode() == core.ModeSearch
}

// IsCommandMode returns true if in command mode
func (ctx *GameContext) IsCommandMode() bool {
	return ctx.GetMode() == core.ModeCommand
}

// IsOverlayMode returns true if in overlay mode
func (ctx *GameContext) IsOverlayMode() bool {
	return ctx.GetMode() == core.ModeOverlay
}

// ===== UI STATE ACCESSORS =====

// GetUISnapshot returns a thread-safe copy of all UI state for rendering
func (ctx *GameContext) GetUISnapshot() UISnapshot {
	ctx.ui.mu.RLock()
	defer ctx.ui.mu.RUnlock()

	// Content slice copy is shallow (backing array shared), but writer typically replaces
	// the whole slice, making this safe for concurrent read/replace usage.
	return UISnapshot{
		CommandText:    ctx.ui.commandText,
		SearchText:     ctx.ui.searchText,
		StatusMessage:  ctx.ui.statusMessage,
		LastCommand:    ctx.ui.lastCommand,
		OverlayActive:  ctx.ui.overlayActive,
		OverlayTitle:   ctx.ui.overlayTitle,
		OverlayContent: ctx.ui.overlayContent,
		OverlayScroll:  ctx.ui.overlayScroll,
	}
}

func (ctx *GameContext) SetCommandText(text string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.commandText = text
}

func (ctx *GameContext) AppendCommandText(text string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.commandText += text
}

func (ctx *GameContext) SetSearchText(text string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.searchText = text
}

func (ctx *GameContext) AppendSearchText(text string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.searchText += text
}

func (ctx *GameContext) SetStatusMessage(msg string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.statusMessage = msg
}

func (ctx *GameContext) SetLastCommand(cmd string) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.lastCommand = cmd
}

func (ctx *GameContext) SetOverlayState(active bool, title string, content []string, scroll int) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.overlayActive = active
	ctx.ui.overlayTitle = title
	ctx.ui.overlayContent = content
	ctx.ui.overlayScroll = scroll
}

func (ctx *GameContext) SetOverlayScroll(scroll int) {
	ctx.ui.mu.Lock()
	defer ctx.ui.mu.Unlock()
	ctx.ui.overlayScroll = scroll
}

func (ctx *GameContext) GetOverlayScroll() int {
	ctx.ui.mu.RLock()
	defer ctx.ui.mu.RUnlock()
	return ctx.ui.overlayScroll
}

func (ctx *GameContext) GetOverlayContentLen() int {
	ctx.ui.mu.RLock()
	defer ctx.ui.mu.RUnlock()
	return len(ctx.ui.overlayContent)
}

// === Frame Number Accessories ===
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
func (ctx *GameContext) PushEvent(eventType events.EventType, payload any, now time.Time) {
	event := events.GameEvent{
		Type:      eventType,
		Payload:   payload,
		Frame:     ctx.State.GetFrameNumber(),
		Timestamp: now,
	}
	ctx.eventQueue.Push(event)
}