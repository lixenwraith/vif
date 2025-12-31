package engine
// @lixen: #dev{feature[drain(render,system)],feature[quasar(render,system)]}

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// ===== Immutable After Init =====
	// Set once during NewGameContext. Pointers/values never modified.
	// Safe for concurrent read without synchronization.

	World         *World            // ECS world; has internal locking for component access
	State         *GameState        // Centralized game state; has internal atomics/mutex
	eventQueue    *event.EventQueue // Lock-free MPSC queue
	PausableClock *PausableClock    // Pausable time source; has internal sync
	CursorEntity  core.Entity       // Singleton cursor entity ID; recreated only on :new

	// ===== Channels =====
	// Inherently thread-safe for send operations.

	ResetChan chan<- struct{} // FSM reset signal; wired to ClockScheduler

	// ===== Atomic (Self-Synchronized) =====
	// Safe for concurrent access via atomic operations.

	FrameNumber atomic.Int64 // Render frame counter; incremented by main loop
	mode        atomic.Int32 // Game mode (core.GameMode); set by Router
	IsPaused    atomic.Bool  // Pause flag; actual timing handled by PausableClock
	IsMuted     atomic.Bool  // Mute flag; keeps mute state

	// ===== Main-Loop Exclusive =====
	// Accessed only from main goroutine (input, resize, render).
	// No synchronization required.

	Width, Height         int // Terminal dimensions
	GameX, GameY          int // Game area offset from terminal origin
	GameWidth, GameHeight int // Game area dimensions (excluding margins)

	// ===== Mutex-Protected (ui.mu) =====
	// All access requires holding ui.mu (RLock for read, Lock for write).

	// Status bar state (atomic pointers for lock-free access)
	commandText   atomic.Pointer[string]
	searchText    atomic.Pointer[string]
	statusMessage atomic.Pointer[string]
	lastCommand   atomic.Pointer[string]

	// Overlay state (atomic for lock-free access)
	overlayActive  atomic.Bool
	overlayTitle   atomic.Pointer[string]
	overlayScroll  atomic.Int32
	overlayContent atomic.Pointer[core.OverlayContent]
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
	SetResource(ctx.World.Resources, statusRegistry)

	// 1. Config Resource
	configRes := &ConfigResource{
		ScreenWidth:  ctx.Width,
		ScreenHeight: ctx.Height,
		GameWidth:    ctx.GameWidth,
		GameHeight:   ctx.GameHeight,
		GameX:        ctx.GameX,
		GameY:        ctx.GameY,
	}
	SetResource(ctx.World.Resources, configRes)

	// 2. Time Resource (Initial state)
	timeRes := &TimeResource{
		GameTime:    pausableClock.Now(),
		RealTime:    pausableClock.RealTime(),
		DeltaTime:   constant.GameUpdateInterval,
		FrameNumber: ctx.FrameNumber.Load(),
	}
	SetResource(ctx.World.Resources, timeRes)

	// 3. Event Queue Resource
	SetResource(ctx.World.Resources, &EventQueueResource{Queue: ctx.eventQueue})

	// 4. Game State
	ctx.State = NewGameState()
	SetResource(ctx.World.Resources, &GameStateResource{State: ctx.State})

	// 5. Cursor Entity
	ctx.CreateCursorEntity()

	// 6. Cursor Resource
	SetResource(ctx.World.Resources, &CursorResource{Entity: ctx.CursorEntity})

	// ZIndex resolver for entity interaction ordering
	zIndexResolver := NewZIndexResolver(ctx.World)
	SetResource(ctx.World.Resources, zIndexResolver)

	// Initialize atomic string pointers to empty strings
	empty := ""
	ctx.commandText.Store(&empty)
	ctx.searchText.Store(&empty)
	ctx.statusMessage.Store(&empty)
	ctx.lastCommand.Store(&empty)
	ctx.overlayTitle.Store(&empty)

	// Initialize audio muted
	ctx.IsMuted.Store(true)

	// Initialize pause state
	ctx.IsPaused.Store(false)

	return ctx
}

// ===== Screen =====

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
			newX := max(0, min(pos.X, ctx.GameWidth-1))
			newY := max(0, min(pos.Y, ctx.GameHeight-1))

			if newX != pos.X || newY != pos.Y {
				pos.X = newX
				pos.Y = newY
				ctx.World.Positions.Set(ctx.CursorEntity, pos)
				// Signal cursor movement if clamped due to resize
				ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: newX, Y: newY})
			}
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
			deathStore.Set(e, component.DeathComponent{})
		}
	}

	// Resize spatial grid to match new dimensions
	ctx.World.Positions.ResizeGrid(width, height)
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

// PushEvent adds an event to the event queue using the World's optimized dispatcher, ensuring consistent frame-stamping across game space and input sources
func (ctx *GameContext) PushEvent(eventType event.EventType, payload any) {
	ctx.World.PushEvent(eventType, payload)
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

// ===== STATUS BAR ACCESSORS =====

func (ctx *GameContext) GetCommandText() string {
	if p := ctx.commandText.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) SetCommandText(text string) {
	ctx.commandText.Store(&text)
}

func (ctx *GameContext) GetSearchText() string {
	if p := ctx.searchText.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) SetSearchText(text string) {
	ctx.searchText.Store(&text)
}

func (ctx *GameContext) GetStatusMessage() string {
	if p := ctx.statusMessage.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) SetStatusMessage(msg string) {
	ctx.statusMessage.Store(&msg)
}

func (ctx *GameContext) GetLastCommand() string {
	if p := ctx.lastCommand.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) SetLastCommand(cmd string) {
	ctx.lastCommand.Store(&cmd)
}

// ===== OVERLAY ACCESSORS =====

func (ctx *GameContext) IsOverlayActive() bool {
	return ctx.overlayActive.Load()
}

func (ctx *GameContext) GetOverlayTitle() string {
	if p := ctx.overlayTitle.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) GetOverlayScroll() int {
	return int(ctx.overlayScroll.Load())
}

func (ctx *GameContext) SetOverlayScroll(scroll int) {
	ctx.overlayScroll.Store(int32(scroll))
}

func (ctx *GameContext) GetOverlayContent() *core.OverlayContent {
	return ctx.overlayContent.Load()
}

func (ctx *GameContext) SetOverlayState(active bool, title string, scroll int) {
	ctx.overlayContent.Store(nil)
	ctx.overlayActive.Store(active)
	ctx.overlayTitle.Store(&title)
	ctx.overlayScroll.Store(int32(scroll))
}

func (ctx *GameContext) SetOverlayContent(content *core.OverlayContent) {
	ctx.overlayContent.Store(content)
	if content != nil {
		ctx.overlayTitle.Store(&content.Title)
		ctx.overlayActive.Store(true)
	} else {
		ctx.overlayActive.Store(false)
		empty := ""
		ctx.overlayTitle.Store(&empty)
	}
	ctx.overlayScroll.Store(0)
}

// ===== Cursor Entity =====

// CreateCursorEntity handles standard cursor entity creation and component attachment
func (ctx *GameContext) CreateCursorEntity() {
	// Create cursor entity at the center of the screen
	ctx.CursorEntity = ctx.World.CreateEntity()
	ctx.World.Positions.Set(ctx.CursorEntity, component.PositionComponent{
		X: ctx.GameWidth / 2,
		Y: ctx.GameHeight / 2,
	})

	GetStore[component.CursorComponent](ctx.World).Set(ctx.CursorEntity, component.CursorComponent{})

	// Make cursor indestructible
	GetStore[component.ProtectionComponent](ctx.World).Set(ctx.CursorEntity, component.ProtectionComponent{
		Mask:      component.ProtectAll,
		ExpiresAt: 0, // No expiry
	})

	// Set PingComponent to cursor (handles crosshair and grid state)
	GetStore[component.PingComponent](ctx.World).Set(ctx.CursorEntity, component.PingComponent{
		ShowCrosshair: true,
		GridActive:    false,
		GridRemaining: 0,
		ContextAware:  true,
	})

	// Set HeatComponent to cursor
	GetStore[component.HeatComponent](ctx.World).Set(ctx.CursorEntity, component.HeatComponent{})

	// Set EnergyComponent to cursor
	GetStore[component.EnergyComponent](ctx.World).Set(ctx.CursorEntity, component.EnergyComponent{})

	// Set ShieldComponent to cursor (initially invisible via GameState.ShieldActive)
	GetStore[component.ShieldComponent](ctx.World).Set(ctx.CursorEntity, component.ShieldComponent{
		RadiusX:       vmath.FromFloat(constant.ShieldRadiusX),
		RadiusY:       vmath.FromFloat(constant.ShieldRadiusY),
		MaxOpacity:    constant.ShieldMaxOpacity,
		LastDrainTime: ctx.PausableClock.Now(),
	})

	// Set BoostComponent to cursor
	GetStore[component.BoostComponent](ctx.World).Set(ctx.CursorEntity, component.BoostComponent{})
}

// ===== Audio =====

// GetAudioPlayer retrieves audio player from resources
// Returns nil if audio unavailable
func (ctx *GameContext) GetAudioPlayer() AudioPlayer {
	if res, ok := GetResource[*AudioResource](ctx.World.Resources); ok {
		return res.Player
	}
	return nil
}

// ToggleAudioMute toggles the mute state of the audio engine
// Returns the new mute state (true if muted, false if unmuted)
func (ctx *GameContext) ToggleAudioMute() bool {
	player := ctx.GetAudioPlayer()
	if player == nil {
		return ctx.IsMuted.Load()
	}
	newMuted := player.ToggleMute()
	ctx.IsMuted.Store(newMuted)
	return newMuted
}

// ===== Pause =====

// SetPaused sets the pause state using the pausable clock
func (ctx *GameContext) SetPaused(paused bool) {
	wasPaused := ctx.IsPaused.Load()
	ctx.IsPaused.Store(paused)

	player := ctx.GetAudioPlayer()

	if paused && !wasPaused {
		ctx.PausableClock.Pause()
		// Capture pre-pause state, then mute for pause
		if player != nil && player.IsRunning() {
			ctx.IsMuted.Store(player.IsMuted())
			if !player.IsMuted() {
				player.ToggleMute()
			}
		}
	} else if !paused && wasPaused {
		ctx.PausableClock.Resume()
		// Restore pre-pause state (respects user toggle during pause)
		if player != nil && player.IsRunning() {
			if !ctx.IsMuted.Load() && player.IsMuted() {
				player.ToggleMute()
			}
		}
	}
}