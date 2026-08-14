package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// GameContext holds all game state including the ECS world
type GameContext struct {
	// === Immutable After Init ===

	// Set once in NewGameContext, and pointers/values never modified, safe for concurrent read without sync

	World         *World         // ECS world; has internal lock
	State         *GameState     // Centralized game state; has internal lock
	PausableClock *PausableClock // Pausable time source; has internal sync
	TimeCtl       *TimeControl   // Rate control and scheduler wake; registry-bound

	// === Channels ===

	ResetChan chan<- struct{} // FSM reset signal; wired to ClockScheduler

	// === Atomic (Self-Synchronized) ===

	FrameNumber atomic.Int64 // Render frame counter; incremented by main loop

	IsPaused atomic.Bool // Pause flag; actual timing handled by PausableClock

	MacroRecording      atomic.Bool  // True when macro is recording
	MacroRecordingLabel atomic.Int32 // Current recording label (rune), 0 if not recording
	MacroPlaying        atomic.Bool  // True when any macro is playing
	MacroClearFlag      atomic.Bool  // Set by :new to signal macro reset

	MouseFreeMode atomic.Bool // Free cursor movement (motion tracking)
	MouseDisabled atomic.Bool // All mouse input ignored

	AutoFire atomic.Bool // Auto-fire (continuous weapon/special fire)

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
	// Command-mode cursor position (rune offset within command text)
	commandCursorPos atomic.Int32
	// Status message expiry (Unix nano timestamp, 0 = no expiry)
	statusMessageExpiry atomic.Int64

	// Overlay state (atomic for lock-free access)
	overlayActive  atomic.Bool
	overlayTitle   atomic.Pointer[string]
	overlayScroll  atomic.Int32
	overlayContent atomic.Pointer[core.OverlayContent]

	// Overlay geometry, recomputed on resize; content height published by the renderer
	overlayGeom     atomic.Pointer[OverlayGeometry]
	overlayContentH atomic.Int32

	// Card selection and pinning; snapshots are immutable once published
	overlaySelectable atomic.Bool
	overlaySelKey     atomic.Pointer[string]
	overlayCards      atomic.Pointer[[]OverlayCardRef]
	overlayPins       atomic.Pointer[[]string]

	// OverlayHUD draws pinned metric groups over the game area
	OverlayHUD atomic.Bool

	// Cached status pointers
	statFPS     *atomic.Int64
	statFrame   *atomic.Int64
	statScreenW *atomic.Int64
	statScreenH *atomic.Int64
	statMode    *status.AtomicString
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
	world.Resources.Status.SetSnapshotInterval(parameter.StatSnapshotTicks)

	// 2. Context metrics; registered before Freeze, written by their owners
	reg := world.Resources.Status
	ctx.statFPS = reg.Ints.Get("engine.fps")
	ctx.statFrame = reg.Ints.Get("context.frame")
	ctx.statScreenW = reg.Ints.Get("context.screen_w")
	ctx.statScreenH = reg.Ints.Get("context.screen_h")
	ctx.statMode = reg.Strings.Get("context.mode")

	// 3. Time control; registers its metrics before Freeze
	ctx.TimeCtl = NewTimeControl(pausableClock, reg)

	// 4. Config Resource
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

	// 5. Time Resource (Initial state)
	world.Resources.Time = &TimeResource{}
	world.Resources.Time.Update(
		pausableClock.Now(),
		pausableClock.RealTime(),
		parameter.GameUpdateInterval,
	)

	// 6. Event Queue Resource
	world.Resources.Event = &EventQueueResource{Queue: event.NewEventQueue()}

	// 7. Game GameState
	ctx.State = NewGameState()
	world.Resources.Game = &GameStateResource{State: ctx.State}

	// 8. Transient Resource
	world.Resources.Transient = NewTransientResource()

	// 9. Cursor Entity
	ctx.World.CreateCursorEntity()

	// 10. Target Resource
	world.Resources.Target = &TargetResource{}

	// 11. Initialize atomic string pointers to empty strings
	empty := ""
	ctx.commandText.Store(&empty)
	ctx.searchText.Store(&empty)
	ctx.statusMessage.Store(&empty)
	ctx.lastCommand.Store(&empty)
	ctx.overlayTitle.Store(&empty)

	// 12. Initialize pause state
	ctx.IsPaused.Store(false)

	// 13. Overlay geometry and mode for the initial terminal size
	ctx.recomputeOverlayGeometry()
	ctx.SetMode(core.ModeNormal)
	ctx.lastFPSUpdate = ctx.PausableClock.RealTime()

	// 14. Initial input state - Not restored by EventGameReset: user-owned for the session
	ctx.MouseFreeMode.Store(parameter.DefaultMouseFreeMode)
	ctx.AutoFire.Store(parameter.DefaultAutoFire)

	// 15. Initialize FPS tracking
	ctx.statFPS = ctx.World.Resources.Status.Ints.Get("engine.fps")
	ctx.lastFPSUpdate = ctx.PausableClock.RealTime()

	return ctx
}

// === Screen ===

// updateGameArea calculates the game area dimensions
func (ctx *GameContext) updateGameArea() (gameWidth, gameHeight int) {
	ctx.GameXOffset = parameter.LeftMargin
	ctx.GameYOffset = parameter.TopMargin

	// Calculate line number width based on height
	gameHeight = max(ctx.Height-parameter.BottomMargin-parameter.TopMargin, 1)
	gameWidth = max(ctx.Width-ctx.GameXOffset, 1)

	return gameWidth, gameHeight
}

// HandleResize handles terminal resize events (acquires world lock)
// Direct-call path: main loop terminal.EventResize
func (ctx *GameContext) HandleResize() {
	ctx.World.RunSafe(ctx.HandleResizeLocked)
}

// HandleResizeLocked is the resize body; caller MUST hold updateMutex
// Router path: Handle runs under the lock, so IntentResize calls this
func (ctx *GameContext) HandleResizeLocked() {
	// New Height and Width already set in context by main
	viewportWidth, viewportHeight := ctx.updateGameArea()
	ctx.recomputeOverlayGeometry()

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

	// Grid tracks map dimensions (grow-only, no reallocation on shrink)
	ctx.World.Positions.ResizeGrid(config.MapWidth, config.MapHeight)

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
}

// === Overlay ===

// OverlayGeometry returns the overlay window placement for the current terminal size
func (ctx *GameContext) OverlayGeometry() OverlayGeometry {
	if g := ctx.overlayGeom.Load(); g != nil {
		return *g
	}
	return OverlayGeometry{}
}

// recomputeOverlayGeometry republishes window placement and screen telemetry
func (ctx *GameContext) recomputeOverlayGeometry() {
	g := ComputeOverlayGeometry(ctx.Width, ctx.Height)
	ctx.overlayGeom.Store(&g)
	ctx.statScreenW.Store(int64(ctx.Width))
	ctx.statScreenH.Store(int64(ctx.Height))
}

// GetOverlayContentH returns the laid-out overlay content height in rows
func (ctx *GameContext) GetOverlayContentH() int {
	return int(ctx.overlayContentH.Load())
}

// SetOverlayContentH publishes the laid-out content height; renderer-owned
func (ctx *GameContext) SetOverlayContentH(h int) {
	ctx.overlayContentH.Store(int32(h))
}

// OverlayCardRef locates one laid-out overlay card in content coordinates,
// published by the renderer so input can resolve selection without geometry
type OverlayCardRef struct {
	Key        string
	X, Y, W, H int
}

// IsOverlaySelectable reports whether the active overlay supports card selection
func (ctx *GameContext) IsOverlaySelectable() bool {
	return ctx.overlaySelectable.Load()
}

// GetOverlaySelection returns the selected card key, empty when none
func (ctx *GameContext) GetOverlaySelection() string {
	if p := ctx.overlaySelKey.Load(); p != nil {
		return *p
	}
	return ""
}

// SetOverlaySelection selects a card by key
func (ctx *GameContext) SetOverlaySelection(key string) {
	ctx.overlaySelKey.Store(&key)
}

// OverlayCards returns the published card index; the slice is immutable
func (ctx *GameContext) OverlayCards() []OverlayCardRef {
	if p := ctx.overlayCards.Load(); p != nil {
		return *p
	}
	return nil
}

// SetOverlayCards publishes a fresh card index; renderer-owned
func (ctx *GameContext) SetOverlayCards(refs []OverlayCardRef) {
	ctx.overlayCards.Store(&refs)
}

// OverlayPins returns the pinned group keys in pin order; the slice is immutable
func (ctx *GameContext) OverlayPins() []string {
	if p := ctx.overlayPins.Load(); p != nil {
		return *p
	}
	return nil
}

// OverlayPinsRef returns the pin snapshot pointer, for consumers that rebind on change
func (ctx *GameContext) OverlayPinsRef() *[]string {
	return ctx.overlayPins.Load()
}

// ToggleOverlayPin adds or removes a group key, preserving pin order.
// Copy-on-write: readers keep the snapshot they loaded.
func (ctx *GameContext) ToggleOverlayPin(key string) {
	cur := ctx.OverlayPins()
	next := make([]string, 0, len(cur)+1)
	found := false
	for _, k := range cur {
		if k == key {
			found = true
			continue
		}
		next = append(next, k)
	}
	if !found {
		next = append(next, key)
	}
	ctx.overlayPins.Store(&next)
}

// ClearOverlayPins removes every pin
func (ctx *GameContext) ClearOverlayPins() {
	var empty []string
	ctx.overlayPins.Store(&empty)
}

// === Viewport and Bounds ===

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

	n := ctx.FrameNumber.Add(1)
	ctx.statFrame.Store(n)
	vlog.SetFrame(uint64(n))
	return n
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
	if int(m) < len(core.ModeNames) {
		ctx.statMode.StoreIfChanged(core.ModeNames[m])
	}
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

func (ctx *GameContext) GetCommandCursorPos() int {
	return int(ctx.commandCursorPos.Load())
}

func (ctx *GameContext) SetCommandCursorPos(pos int) {
	ctx.commandCursorPos.Store(int32(pos))
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
	ctx.overlayContentH.Store(0)
	ctx.overlayCards.Store(nil)
	ctx.syncOverlaySelection(content)
}

// syncOverlaySelection keeps the selected card across a rebuild, falling back
// to the first card when the previous key is gone
func (ctx *GameContext) syncOverlaySelection(content *core.OverlayContent) {
	if content == nil || content.Layout != core.OverlayLayoutCards {
		ctx.overlaySelectable.Store(false)
		ctx.SetOverlaySelection("")
		return
	}

	prev := ctx.GetOverlaySelection()
	first, keep := "", false
	for _, item := range content.Items {
		card, ok := item.(core.OverlayCard)
		if !ok {
			continue
		}
		if first == "" {
			first = card.Key
		}
		if card.Key == prev && prev != "" {
			keep = true
			break
		}
	}

	ctx.overlaySelectable.Store(first != "")
	if !keep {
		ctx.SetOverlaySelection(first)
	}
}

// === Pause ===

func (ctx *GameContext) SetPaused(paused bool) {
	ctx.PushEvent(event.EventGamePauseRequest, &event.GamePausePayload{Paused: paused})
}
