package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// === Immutable After Init ===

	// Set once in NewGameContext, and pointers/values never modified, safe for concurrent read without sync

	World         *World         // ECS world; has internal lock
	State         *GameState     // Centralized game state; has internal lock
	PausableClock *PausableClock // Pausable time source; has internal sync

	// === Channels ===

	ResetChan chan<- struct{} // FSM reset signal; wired to ClockScheduler

	// === Atomic (Self-Synchronized) ===

	FrameNumber atomic.Int64 // Render frame counter; incremented by main loop

	IsPaused atomic.Bool // Pause flag; actual timing handled by PausableClock
	IsMuted  atomic.Bool // Mute flag; keeps mute state

	MacroRecording      atomic.Bool  // True when macro is recording
	MacroRecordingLabel atomic.Int32 // Current recording label (rune), 0 if not recording
	MacroPlaying        atomic.Bool  // True when any macro is playing
	MacroClearFlag      atomic.Bool  // Set by :new to signal macro reset

	// === Main-Loop Exclusive ===

	// Accessed only from main goroutine (input, resize, render), no sync required

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
	// Status message expiry (Unix nano timestamp, 0 = no expiry)
	statusMessageExpiry atomic.Int64

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

	// Calculate game area
	// gameWidth, gameHeight := ctx.updateGameArea()
	viewportWidth, viewportHeight := ctx.updateGameArea()

	// -- Initialize Resources --

	// 1. Status Registry (before other resources that may use it)
	world.Resources.Status = status.NewRegistry()

	// 2. Config Resource
	// Initial: Map = Viewport, CropOnResize enabled for backward compat
	world.Resources.Config = &ConfigResource{
		MapWidth:       viewportWidth,
		MapHeight:      viewportHeight,
		ViewportWidth:  viewportWidth,
		ViewportHeight: viewportHeight,
		CameraX:        0,
		CameraY:        0,
		CropOnResize:   true,
	}

	// 3. Time Resource (Initial state)
	world.Resources.Time = &TimeResource{
		GameTime:  pausableClock.Now(),
		RealTime:  pausableClock.RealTime(),
		DeltaTime: parameter.GameUpdateInterval,
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
	gameHeight = ctx.Height - parameter.BottomMargin - parameter.TopMargin
	if gameHeight < 1 {
		gameHeight = 1
	}

	ctx.GameXOffset = parameter.LeftMargin
	ctx.GameYOffset = parameter.TopMargin
	gameWidth = ctx.Width - ctx.GameXOffset

	if gameWidth < 1 {
		gameWidth = 1
	}

	return gameWidth, gameHeight
}

// HandleResize handles terminal resize events
func (ctx *GameContext) HandleResize() {
	// New Height and Width already set in context by main
	viewportWidth, viewportHeight := ctx.updateGameArea()

	ctx.World.RunSafe(func() {
		config := ctx.World.Resources.Config
		config.ViewportWidth = viewportWidth
		config.ViewportHeight = viewportHeight

		if config.CropOnResize {
			// Resize map to match viewport, cleanup OOB entities
			config.MapWidth = viewportWidth
			config.MapHeight = viewportHeight
			ctx.cleanupOutOfBoundsEntities(config.MapWidth, config.MapHeight)
			// Reset camera
			config.CameraX = 0
			config.CameraY = 0
		} else {
			// Map persists, clamp camera to valid range
			ctx.clampCamera(config)
		}

		// Clamp cursor to Map bounds
		cursorEntity := ctx.World.Resources.Player.Entity
		if pos, ok := ctx.World.Positions.GetPosition(cursorEntity); ok {
			newX := max(0, min(pos.X, config.MapWidth-1))
			newY := max(0, min(pos.Y, config.MapHeight-1))

			if newX != pos.X || newY != pos.Y {
				pos.X = newX
				pos.Y = newY
				ctx.World.Positions.SetPosition(cursorEntity, pos)
			}

			// Always emit cursor moved after resize to trigger camera adjustment
			ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: pos.X, Y: pos.Y})
		}

		// Free cursor if blocked
		if newX, newY, moved := ctx.World.PushEntityFromBlocked(cursorEntity, component.WallBlockCursor); moved {
			ctx.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{X: newX, Y: newY})
		}
	})

	// TODO: re-evaluate grid resize. do we need very large grids?
	// Resize spatial grid to match new dimensions
	// ctx.World.Positions.ResizeGrid(width, height)
}

// clampCamera constrains camera position to valid range
// TODO: renderer handling viewport larger than map
// When Viewport >= Map on an axis, camera is 0 (renderer handles centering)
func (ctx *GameContext) clampCamera(config *ConfigResource) {
	maxCameraX := config.MapWidth - config.ViewportWidth
	maxCameraY := config.MapHeight - config.ViewportHeight

	if maxCameraX <= 0 {
		config.CameraX = 0
	} else {
		config.CameraX = max(0, min(config.CameraX, maxCameraX))
	}

	if maxCameraY <= 0 {
		config.CameraY = 0
	} else {
		config.CameraY = max(0, min(config.CameraY, maxCameraY))
	}
}

// cleanupOutOfBoundsEntities tags entities outside valid map area for destruction
func (ctx *GameContext) cleanupOutOfBoundsEntities(width, height int) {
	deathStore := ctx.World.Components.Death

	// Unified cleanup: single Positions iteration handles all entity types
	allEntities := ctx.World.Positions.AllEntities()
	for _, e := range allEntities {
		// Skip cursor entity (special case)
		if e == ctx.World.Resources.Player.Entity {
			continue
		}

		// Mark entities outside valid coordinate space [0, width) × [0, height)
		// Death systems informs respective systems of their entity destruction
		pos, _ := ctx.World.Positions.GetPosition(e)
		if pos.X >= width || pos.Y >= height || pos.X < 0 || pos.Y < 0 {
			deathStore.SetComponent(e, component.DeathComponent{})
		}
	}
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
	return ctx.World.Resources.Game.State.GetMode()
}

// SetMode sets the current game mode
func (ctx *GameContext) SetMode(m core.GameMode) {
	ctx.World.Resources.Game.State.SetMode(m)
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

// SetStatusMessage sets status message with optional duration and override
func (ctx *GameContext) SetStatusMessage(msg string, duration time.Duration, override bool) {
	now := ctx.PausableClock.RealTime().UnixNano()
	currentExpiry := ctx.statusMessageExpiry.Load()

	// Reject write if current message has unexpired duration and no override
	if !override && currentExpiry > 0 && currentExpiry > now && msg != "" {
		return
	}

	ctx.statusMessage.Store(&msg)
	if duration > 0 {
		ctx.statusMessageExpiry.Store(now + duration.Nanoseconds())
	} else {
		ctx.statusMessageExpiry.Store(0)
	}
}

// GetStatusMessage returns current status message
func (ctx *GameContext) GetStatusMessage() string {
	if p := ctx.statusMessage.Load(); p != nil {
		return *p
	}
	return ""
}

// GetStatusMessageExpiry returns the expiry timestamp (Unix nano), 0 if none
func (ctx *GameContext) GetStatusMessageExpiry() int64 {
	return ctx.statusMessageExpiry.Load()
}

// ClearStatusMessage forcibly clears the status message and expiry
func (ctx *GameContext) ClearStatusMessage() {
	empty := ""
	ctx.statusMessage.Store(&empty)
	ctx.statusMessageExpiry.Store(0)
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
		// Pause audio engine (freezes sequencer, stops SFX)
		if player != nil && player.IsRunning() {
			player.SetPaused(true)
		}
	} else if !paused && wasPaused {
		ctx.PausableClock.Resume()
		// Resume audio engine
		if player != nil && player.IsRunning() {
			player.SetPaused(false)
		}
	}
}