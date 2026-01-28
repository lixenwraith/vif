package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// === Immutable After Init ===
	// Set once in NewGameContext, and pointers/values never modified, safe for concurrent read without sync

	World         *World         // ECS world; has internal lock
	State         *GameState     // Centralized game state; has internal lock
	PausableClock *PausableClock // Pausable time source; has internal sync

	// === Channels ===
	// Inherently thread-safe for send operations

	ResetChan chan<- struct{} // FSM reset signal; wired to ClockScheduler

	// === Atomic (Self-Synchronized) ===

	FrameNumber atomic.Int64 // Render frame counter; incremented by main loop
	mode        atomic.Int32 // Game mode (core.GameMode); set by Router
	IsPaused    atomic.Bool  // Pause flag; actual timing handled by PausableClock
	IsMuted     atomic.Bool  // Mute flag; keeps mute state

	// === Main-Loop Exclusive ===
	// Accessed only from main goroutine (input, resize, render)
	// No sync required

	Width, Height            int // Terminal dimensions
	GameXOffset, GameYOffset int // Game area offset from terminal origin

	// === Context Exclusive ===
	// No sync required
	lastFPSUpdate time.Time
	frameCountFPS int64

	// === Atomic States ===

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

	// Cached FPS state
	statFPS *atomic.Int64
}

// NewGameContext creates a GameContext using an existing ECS World
// Component must be registered before context creation
// width/height are initial terminal dimensions
func NewGameContext(world *World, width, height int) *GameContext {
	// Create pausable clock
	pausableClock := NewPausableClock()

	ctx := &GameContext{
		World:         world,
		PausableClock: pausableClock,
		Width:         width,
		Height:        height,
	}

	// Initialize atomic mode
	ctx.SetMode(core.ModeNormal)

	// Calculate game area
	gameWidth, gameHeight := ctx.updateGameArea()

	// -- Initialize Resources --

	// 1. Status Registry (before other resources that may use it)
	world.Resources.Status = status.NewRegistry()

	// 2. Config Resource
	world.Resources.Config = &ConfigResource{
		GameWidth:  gameWidth,
		GameHeight: gameHeight,
	}

	// 3. Time Resource (Initial state)
	world.Resources.Time = &TimeResource{
		GameTime:  pausableClock.Now(),
		RealTime:  pausableClock.RealTime(),
		DeltaTime: constant.GameUpdateInterval,
	}

	// 4. Event Queue Resource
	world.Resources.Event = &EventQueueResource{Queue: event.NewEventQueue()}

	// 5. Game GameState
	ctx.State = NewGameState()
	world.Resources.Game = &GameStateResource{State: ctx.State}

	// 6. Environment and Cursor Entity
	ctx.World.CreateEnvironment()
	ctx.World.CreateCursorEntity()

	// 7. Initialize atomic string pointers to empty strings
	empty := ""
	ctx.commandText.Store(&empty)
	ctx.searchText.Store(&empty)
	ctx.statusMessage.Store(&empty)
	ctx.lastCommand.Store(&empty)
	ctx.overlayTitle.Store(&empty)

	// 8. Initialize audio muted
	ctx.IsMuted.Store(true)

	// 9. Initialize pause state
	ctx.IsPaused.Store(false)

	// 10. Initialize FPS tracking
	ctx.statFPS = ctx.World.Resources.Status.Ints.Get("engine.fps")
	ctx.lastFPSUpdate = ctx.PausableClock.RealTime()

	return ctx
}

// === Screen ===

// updateGameArea calculates the game area dimensions
func (ctx *GameContext) updateGameArea() (gameWidth, gameHeight int) {
	// Calculate line number width based on height
	gameHeight = ctx.Height - constant.BottomMargin - constant.TopMargin
	if gameHeight < 1 {
		gameHeight = 1
	}

	ctx.GameXOffset = constant.LeftMargin
	ctx.GameYOffset = constant.TopMargin
	gameWidth = ctx.Width - ctx.GameXOffset

	if gameWidth < 1 {
		gameWidth = 1
	}

	return gameWidth, gameHeight
}

// HandleResize handles terminal resize events
func (ctx *GameContext) HandleResize() {
	// New Height and Width already set in context by main
	gameWidth, gameHeight := ctx.updateGameArea()

	ctx.World.RunSafe(func() {
		// Update existing ConfigResource in-place
		configRes := ctx.World.Resources.Config
		configRes.GameWidth = gameWidth
		configRes.GameHeight = gameHeight

		// TODO: Optional disable (world.crop)
		// Cleanup entities outside new bounds to prevent ghosting/resource usage
		// Uses GameWidth/Height as valid coordinate space for entities, resizes Spatial Grid
		ctx.cleanupOutOfBoundsEntities(gameWidth, gameHeight)

		cursorEntity := ctx.World.Resources.Cursor.Entity
		// Clamp cursor position
		if pos, ok := ctx.World.Positions.GetPosition(cursorEntity); ok {
			newX := max(0, min(pos.X, gameWidth-1))
			newY := max(0, min(pos.Y, gameHeight-1))

			if newX != pos.X || newY != pos.Y {
				pos.X = newX
				pos.Y = newY
				ctx.World.Positions.SetPosition(cursorEntity, pos)
				// Signal cursor movement if clamped due to resize
				ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: newX, Y: newY})
			}
		}
	})
}

// cleanupOutOfBoundsEntities tags entities that are outside the valid game area
func (ctx *GameContext) cleanupOutOfBoundsEntities(width, height int) {
	deathStore := ctx.World.Components.Death

	// Unified cleanup: single Positions iteration handles all entity types
	allEntities := ctx.World.Positions.AllEntities()
	for _, e := range allEntities {
		// Skip cursor entity (special case)
		if e == ctx.World.Resources.Cursor.Entity {
			continue
		}

		// Mark entities outside valid coordinate space [0, width) × [0, height)
		// Death systems informs respective systems of their entity destruction
		pos, _ := ctx.World.Positions.GetPosition(e)
		if pos.X >= width || pos.Y >= height || pos.X < 0 || pos.Y < 0 {
			deathStore.SetComponent(e, component.DeathComponent{})
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
	// FPS calculation (once per second)
	ctx.frameCountFPS++
	now := ctx.PausableClock.RealTime()
	if now.Sub(ctx.lastFPSUpdate) >= time.Second {
		ctx.statFPS.Store(ctx.frameCountFPS)
		ctx.frameCountFPS = 0
		ctx.lastFPSUpdate = now
	}

	return ctx.FrameNumber.Add(1)
}

// === EVENT QUEUE METHODS ===

// PushEvent adds an event to the event queue using the World's optimized dispatcher, ensuring consistent frame-stamping across game space and input sources
func (ctx *GameContext) PushEvent(eventType event.EventType, payload any) {
	ctx.World.PushEvent(eventType, payload)
}

// === MODE ACCESSORS ===

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

// IsNormalMode returns true if in normal mode
func (ctx *GameContext) IsNormalMode() bool {
	return ctx.GetMode() == core.ModeNormal
}

// IsVisualMode returns true if in visual mode
func (ctx *GameContext) IsVisualMode() bool {
	return ctx.GetMode() == core.ModeVisual
}

// PingBounds holds the boundaries of ping crosshair and normal/visual mode operations
type PingBounds struct {
	MinY, MaxY int
	MinX, MaxX int
	Active     bool // True if band is wider than single row
}

// GetPingBounds returns the boundaries for pings and operations, in normal mode or shield inactive, returns single-row/column bounds
func (ctx *GameContext) GetPingBounds() PingBounds {
	cursorEntity := ctx.World.Resources.Cursor.Entity
	pos, ok := ctx.World.Positions.GetPosition(cursorEntity)
	if !ok {
		return PingBounds{}
	}

	bounds := PingBounds{
		MinY:   pos.Y,
		MaxY:   pos.Y,
		MinX:   pos.X,
		MaxX:   pos.X,
		Active: false,
	}

	if !ctx.IsVisualMode() {
		return bounds
	}

	// Check shield for band dimensions
	shield, ok := ctx.World.Components.Shield.GetComponent(cursorEntity)
	if !ok || !shield.Active {
		return bounds
	}

	halfWidth := vmath.ToInt(shield.RadiusX) / constant.PingBoundFactor
	halfHeight := vmath.ToInt(shield.RadiusY) / constant.PingBoundFactor

	bounds.MinY = pos.Y - halfHeight
	bounds.MaxY = pos.Y + halfHeight
	bounds.MinX = pos.X - halfWidth
	bounds.MaxX = pos.X + halfWidth
	bounds.Active = true

	// Clamp to game area
	gameWidth := ctx.World.Resources.Config.GameWidth
	gameHeight := ctx.World.Resources.Config.GameHeight
	if bounds.MinY < 0 {
		bounds.MinY = 0
	}
	if bounds.MaxY >= gameHeight {
		bounds.MaxY = gameHeight - 1
	}
	if bounds.MinX < 0 {
		bounds.MinX = 0
	}
	if bounds.MaxX >= gameWidth {
		bounds.MaxX = gameWidth - 1
	}

	return bounds
}

// === STATUS BAR ACCESSORS ===

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

// === OVERLAY ACCESSORS ===

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

// === Audio ===

// GetAudioPlayer retrieves audio player from resources
// Returns nil if audio unavailable
func (ctx *GameContext) GetAudioPlayer() AudioPlayer {
	if ctx.World.Resources.Audio != nil {
		return ctx.World.Resources.Audio.Player
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
	newMuted := player.ToggleEffectMute()
	ctx.IsMuted.Store(newMuted)
	return newMuted
}

// === Pause ===

// SetPaused sets the pause state using the pausable clock
func (ctx *GameContext) SetPaused(paused bool) {
	wasPaused := ctx.IsPaused.Load()
	ctx.IsPaused.Store(paused)

	player := ctx.GetAudioPlayer()

	if paused && !wasPaused {
		ctx.PausableClock.Pause()
		// Capture pre-pause state, then mute for pause
		if player != nil && player.IsRunning() {
			ctx.IsMuted.Store(player.IsEffectMuted())
			if !player.IsEffectMuted() {
				player.ToggleEffectMute()
			}
		}
	} else if !paused && wasPaused {
		ctx.PausableClock.Resume()
		// Restore pre-pause state (respects user toggle during pause)
		if player != nil && player.IsRunning() {
			if !ctx.IsMuted.Load() && player.IsEffectMuted() {
				player.ToggleEffectMute()
			}
		}
	}
}