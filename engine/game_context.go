package engine

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	eventQueue *event.EventQueue

	// // Terminal interface
	// Terminal terminal.Terminal

	// Pausable Clock time provider
	PausableClock *PausableClock

	// FSM Reset channel, wired by main to ClockScheduler
	ResetChan chan<- struct{}

	// Screen dimensions
	Width, Height int

	// Game area (excluding UI elements)
	// TODO: review, usage and resize race
	GameX, GameY          int
	GameWidth, GameHeight int

	// Cursor entity (singleton)
	CursorEntity core.Entity

	// --- Thread-Safe State ---

	// Mode state (mode package is the authority)
	mode atomic.Int32

	// Authority: Total number of render cycles elapsed
	FrameNumber atomic.Int64

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
}

// NewGameContext creates a GameContext using an existing ECS World
// Components must be registered before context creation
// width/height are initial terminal dimensions
func NewGameContext(world *World, width, height int) *GameContext {
	// Create pausable clock
	pausableClock := NewPausableClock()

	ctx := &GameContext{
		World:         world,
		PausableClock: pausableClock,
		Width:         width,
		Height:        height,
		eventQueue:    event.NewEventQueue(),
	}

	// Wire World to this Context's frame and event source
	world.SetEventMetadata(ctx.eventQueue, &ctx.FrameNumber)

	// Initialize atomic mode
	ctx.SetMode(core.ModeNormal)

	// Calculate game area
	ctx.updateGameArea()

	// -- Initialize Resources --

	// 0. Status Registry (before other resources that may use it)
	statusRegistry := status.NewRegistry()
	AddResource(ctx.World.Resources, statusRegistry)

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
		DeltaTime:   constant.GameUpdateInterval,
		FrameNumber: ctx.FrameNumber.Load(),
	}
	AddResource(ctx.World.Resources, timeRes)

	// 3. Event Queue Resource
	AddResource(ctx.World.Resources, &EventQueueResource{Queue: ctx.eventQueue})

	// 4. Game State
	ctx.State = NewGameState()
	AddResource(ctx.World.Resources, &GameStateResource{State: ctx.State})

	// 5. Cursor Entity
	ctx.CreateCursorEntity()

	// 6. Cursor Resource
	AddResource(ctx.World.Resources, &CursorResource{Entity: ctx.CursorEntity})

	// ZIndex resolver for entity interaction ordering
	zIndexResolver := NewZIndexResolver(ctx.World)
	AddResource(ctx.World.Resources, zIndexResolver)

	// Initialize pause state
	ctx.IsPaused.Store(false)

	return ctx
}

// CreateCursorEntity handles standard cursor entity creation and component attachment
// Centralizes logic shared between NewGameContext and :new command
func (ctx *GameContext) CreateCursorEntity() {
	// Create cursor entity at the center of the screen
	ctx.CursorEntity = ctx.World.CreateEntity()
	ctx.World.Positions.Add(ctx.CursorEntity, component.PositionComponent{
		X: ctx.GameWidth / 2,
		Y: ctx.GameHeight / 2,
	})

	GetStore[component.CursorComponent](ctx.World).Add(ctx.CursorEntity, component.CursorComponent{})

	// Make cursor indestructible
	GetStore[component.ProtectionComponent](ctx.World).Add(ctx.CursorEntity, component.ProtectionComponent{
		Mask:      component.ProtectAll,
		ExpiresAt: 0, // No expiry
	})

	// Add PingComponent to cursor (handles crosshair and grid state)
	GetStore[component.PingComponent](ctx.World).Add(ctx.CursorEntity, component.PingComponent{
		ShowCrosshair:  true,
		CrosshairColor: component.ColorNormal,
		GridActive:     false,
		GridRemaining:  0,
		GridColor:      component.ColorNormal,
		ContextAware:   true,
	})

	// Add HeatComponent to cursor
	GetStore[component.HeatComponent](ctx.World).Add(ctx.CursorEntity, component.HeatComponent{})

	// Add EnergyComponent to cursor
	GetStore[component.EnergyComponent](ctx.World).Add(ctx.CursorEntity, component.EnergyComponent{})

	// Add ShieldComponent to cursor (initially invisible via GameState.ShieldActive)
	GetStore[component.ShieldComponent](ctx.World).Add(ctx.CursorEntity, component.ShieldComponent{
		RadiusX:       vmath.FromFloat(constant.ShieldRadiusX),
		RadiusY:       vmath.FromFloat(constant.ShieldRadiusY),
		OverrideColor: component.ColorNone,
		MaxOpacity:    constant.ShieldMaxOpacity,
		LastDrainTime: ctx.PausableClock.Now(),
	})

	// Add BoostComponent to cursor
	GetStore[component.BoostComponent](ctx.World).Add(ctx.CursorEntity, component.BoostComponent{})
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

// GetAudioPlayer retrieves audio player from resources
// Returns nil if audio unavailable
func (ctx *GameContext) GetAudioPlayer() AudioPlayer {
	if res, ok := GetResource[*AudioResource](ctx.World.Resources); ok {
		return res.Player
	}
	return nil
}

// StopAudio mutes audio during pause (sounds complete naturally)
func (ctx *GameContext) StopAudio() {
	player := ctx.GetAudioPlayer()
	if player != nil && player.IsRunning() && !player.IsMuted() {
		player.ToggleMute()
	}
}

// ToggleAudioMute toggles the mute state of the audio engine
// Returns the new mute state (true if muted, false if unmuted)
func (ctx *GameContext) ToggleAudioMute() bool {
	player := ctx.GetAudioPlayer()
	if player != nil {
		return player.ToggleMute()
	}
	return false
}

// updateGameArea calculates the game area dimensions
func (ctx *GameContext) updateGameArea() {
	// Calculate line number width based on height
	gameHeight := ctx.Height - constant.BottomMargin - constant.TopMargin
	if gameHeight < 1 {
		gameHeight = 1
	}

	ctx.GameX = constant.LeftMargin
	ctx.GameY = constant.TopMargin
	ctx.GameWidth = ctx.Width - ctx.GameX
	ctx.GameHeight = gameHeight

	if ctx.GameWidth < 1 {
		ctx.GameWidth = 1
	}
}

// HandleResize handles terminal resize events
func (ctx *GameContext) HandleResize() {
	// New Height and Width already set in context by main
	ctx.updateGameArea()

	ctx.World.RunSafe(func() {
		// Update existing ConfigResource in-place
		configRes := MustGetResource[*ConfigResource](ctx.World.Resources)
		configRes.ScreenWidth = ctx.Width
		configRes.ScreenHeight = ctx.Height
		configRes.GameWidth = ctx.GameWidth
		configRes.GameHeight = ctx.GameHeight
		configRes.GameX = ctx.GameX
		configRes.GameY = ctx.GameY

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
	})
}

// cleanupOutOfBoundsEntities tags entities that are outside the valid game area
func (ctx *GameContext) cleanupOutOfBoundsEntities(width, height int) {
	deathStore := GetStore[component.DeathComponent](ctx.World)

	// Unified cleanup: single PositionStore iteration handles all entity types
	allEntities := ctx.World.Positions.All()
	for _, e := range allEntities {
		// Skip cursor entity (special case)
		if e == ctx.CursorEntity {
			continue
		}

		// Mark entities outside valid coordinate space [0, width) × [0, height)
		// Death system informs respective systems of their entity destruction
		pos, _ := ctx.World.Positions.Get(e)
		if pos.X >= width || pos.Y >= height || pos.X < 0 || pos.Y < 0 {
			deathStore.Add(e, component.DeathComponent{})
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

// GetFrameNumber returns the live render frame index
func (ctx *GameContext) GetFrameNumber() int64 {
	return ctx.FrameNumber.Load()
}

// IncrementFrameNumber advances the frame authority (called by Render Loop)
func (ctx *GameContext) IncrementFrameNumber() int64 {
	return ctx.FrameNumber.Add(1)
}

// ===== EVENT QUEUE METHODS =====

// PushEvent adds an event to the event queue using the World's optimized dispatcher
// This ensures consistent frame-stamping across simulation and input sources
func (ctx *GameContext) PushEvent(eventType event.EventType, payload any) {
	ctx.World.PushEvent(eventType, payload)
}