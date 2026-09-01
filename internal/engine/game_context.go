package engine

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
	"github.com/lixenwraith/vi-fighter/pkg/navigation"
)

// NavigationDebugState is one runtime's operator view of navigation internals.
type NavigationDebugState struct {
	Flow                 *navigation.FlowFieldCache
	CompositeFlow        *navigation.FlowFieldCache
	CompositePassability *navigation.CompositePassability
	ShowFlow             bool
	ShowComposite        bool
	GroupID              uint8
}

// GameContext holds all game state including the ECS world
// SessionController is what an operator command may ask of the session. Kept to
// the two questions the command surface actually has: start hosting this run, and
// describe what it is part of now.
type SessionController interface {
	// BeginHosting opens this running instance to participants at addr. It returns
	// an error rather than reporting one, because the operator typed the address.
	BeginHosting(addr string) error
	// SessionSummary is a one-line description of the session for the status bar.
	SessionSummary() string
}

type GameContext struct {
	// === Immutable After Init ===

	// Set once in NewGameContext, and pointers/values never modified, safe for concurrent read without sync

	World   *World       // ECS world; has internal lock
	State   *GameState   // Centralized game state; has internal lock
	TimeCtl *TimeControl // Sole time surface: reads, rate, pause, step; registry-bound

	KeyTable        *input.KeyTable
	Correlation     *vlog.Correlation
	NavigationDebug NavigationDebugState

	// === Channels ===

	ResetChan chan<- struct{} // FSM reset signal; wired to ClockScheduler

	// SessionCtl is the App-level session lifecycle the operator surface reaches
	// through. It is nil in a harness that builds no transport, so every caller
	// checks. The context cannot own this itself: opening a session binds a socket
	// and starts goroutines, which belong to the runtime rather than to the world.
	SessionCtl SessionController

	// === Atomic (Self-Synchronized) ===

	FrameNumber atomic.Int64 // Render frame counter; incremented by main loop

	MacroRecording      atomic.Bool  // True when macro is recording
	MacroRecordingLabel atomic.Int32 // Current recording label (rune), 0 if not recording
	MacroPlaying        atomic.Bool  // True when any macro is playing
	MacroClearFlag      atomic.Bool  // Set by :new to signal macro reset

	MouseFreeMode atomic.Bool // Free cursor movement (motion tracking)
	MouseDisabled atomic.Bool // All mouse input ignored

	AutoFire atomic.Bool // Auto-fire (continuous weapon/special fire)

	// === Main-Loop Exclusive ===

	// Terminal geometry, written only by MetaSystem's EventScreenResize handler
	// under the world lock. Every reader holds the same lock: HandleResizeLocked,
	// SnapshotContext, and the RenderContext build in App.frame.

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
	statFPS       *atomic.Int64
	statFrame     *atomic.Int64
	statScreenW   *atomic.Int64
	statScreenH   *atomic.Int64
	statMapLocked *atomic.Bool
	statMode      *status.AtomicString
}

// NewGameContext creates a GameContext on the interactive clock
func NewGameContext(world *World, width, height int) *GameContext {
	return newGameContext(world, width, height, NewPausableClock(), vlog.DefaultCorrelation())
}

// NewGameContextWithClock creates a GameContext on a caller-supplied time source.
// Headless and replay runs pass a ManualClock.
func NewGameContextWithClock(world *World, width, height int, clock Clock) *GameContext {
	return newGameContext(world, width, height, clock, vlog.NewCorrelation())
}

func newGameContext(world *World, width, height int, clock Clock, corr *vlog.Correlation) *GameContext {
	ctx := &GameContext{
		World:       world,
		Width:       width,
		Height:      height,
		Correlation: corr,
	}

	// Calculate game area
	// gameWidth, gameHeight := ctx.updateGameArea()
	viewportWidth, viewportHeight := ctx.updateGameArea()

	// -- Initialize Resources --

	// 1. Status Registry (before other resources that may use it)
	world.Resources.Status = status.NewRegistry()
	world.Resources.Status.SetCorrelation(corr)
	world.Resources.Status.SetSnapshotInterval(parameter.StatSnapshotTicks)
	world.updateMutex.BindStatus(world.Resources.Status)
	world.Positions.BindTelemetry(world.Resources.Status)
	world.Resources.NavigationDebug = &ctx.NavigationDebug

	// 2. Context metrics; registered before Freeze, written by their owners
	reg := world.Resources.Status
	ctx.statFPS = reg.Ints.Get("engine.fps")
	ctx.statFrame = reg.Ints.Get("context.frame")
	ctx.statScreenW = reg.Ints.Get("context.screen_w")
	ctx.statScreenH = reg.Ints.Get("context.screen_h")
	ctx.statMapLocked = reg.Bools.Get("context.map_locked")
	ctx.statMode = reg.Strings.Get("context.mode")

	// 3. Time control; registers its metrics before Freeze
	ctx.TimeCtl = NewTimeControl(clock, reg)

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
		ctx.TimeCtl.Now(),
		ctx.TimeCtl.RealTime(),
		parameter.GameUpdateInterval,
	)

	// 6. Event Queue Resource
	world.Resources.Event = &EventQueueResource{Queue: event.NewEventQueue()}

	// 7. Game GameState
	ctx.State = NewGameState()
	world.Resources.Game = &GameStateResource{State: ctx.State}

	// 8. Transient Resource
	world.Resources.Transient = NewTransientResource()
	world.Resources.View = NewViewResource()

	// 9. Cursor roster; the FSM spawns cursors, so the world starts with none
	world.Resources.Player = &PlayerResource{}

	// 10. Target Resource
	world.Resources.Target = &TargetResource{}

	// 11. Initialize atomic string pointers to empty strings
	empty := ""
	ctx.commandText.Store(&empty)
	ctx.searchText.Store(&empty)
	ctx.statusMessage.Store(&empty)
	ctx.lastCommand.Store(&empty)
	ctx.overlayTitle.Store(&empty)

	// 12. Operator session state; see the session state contract above ResetSessionState
	ctx.recomputeOverlayGeometry()
	ctx.SetMode(core.ModeNormal)
	ctx.lastFPSUpdate = ctx.TimeCtl.RealTime()

	// 13. Initial input state - Not restored by EventGameResetRequest: user-owned for the session
	ctx.MouseFreeMode.Store(parameter.DefaultMouseFreeMode)
	ctx.AutoFire.Store(parameter.DefaultAutoFire)

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

// ScreenSize inverts updateGameArea: terminal dimensions recovered from the viewport
// the world already carries, for world-scoped consumers holding no GameContext — the
// journal anchor. Exact for every size the resize path admits, which rejects the
// degenerate ones updateGameArea would clamp to 1.
func ScreenSize(cfg *ConfigResource) (width, height int) {
	return cfg.ViewportWidth + parameter.LeftMargin,
		cfg.ViewportHeight + parameter.TopMargin + parameter.BottomMargin
}

// ViewportFits reports whether terminal dimensions leave a game area of at least one
// cell, which is exactly the range over which ScreenSize inverts updateGameArea
func ViewportFits(width, height int) bool {
	return width > parameter.LeftMargin && height > parameter.TopMargin+parameter.BottomMargin
}

// mapSizeLocal reports whether this instance may derive map bounds from its own terminal.
// Crop rewrites shared simulation state, so it is admissible only while nobody else shares the world.
// When more than one player is present, crop is disabled and map size is locked.
func (ctx *GameContext) mapSizeLocal() bool { return ctx.World.MapSizeLocal() }

// PublishMapLock republishes context.map_locked from current state. The flag is
// a derivation of CropOnResize and mapSizeLocal (D-14), so every writer of
// either republishes it or the last resize's verdict outlives its inputs.
func (ctx *GameContext) PublishMapLock() {
	ctx.statMapLocked.Store(ctx.World.Resources.Config.CropOnResize && !ctx.mapSizeLocal())
}

// HandleResizeLocked applies terminal dimensions already written to the context and
// reflows viewport, map, grid, camera and cursor. Caller MUST hold updateMutex;
// MetaSystem's EventScreenResize handler is the only entry point.
func (ctx *GameContext) HandleResizeLocked() {
	// New Height and Width already set in context by main
	viewportWidth, viewportHeight := ctx.updateGameArea()
	ctx.recomputeOverlayGeometry()

	config := ctx.World.Resources.Config
	config.ViewportWidth = viewportWidth
	config.ViewportHeight = viewportHeight

	cropped := config.CropOnResize && ctx.mapSizeLocal()
	ctx.PublishMapLock()
	if cropped {
		// Resize map to match viewport, cleanup OOB entities
		config.MapWidth = viewportWidth
		config.MapHeight = viewportHeight
		ctx.cleanupOutOfBoundsEntities(config.MapWidth, config.MapHeight)
		// Reset camera
		config.CameraX = 0
		config.CameraY = 0
	} else {
		if config.CropOnResize {
			vlog.Info("app", "msg", "map size locked",
				"map_w", config.MapWidth, "map_h", config.MapHeight,
				"viewport_w", viewportWidth, "viewport_h", viewportHeight)
		}
		// Map persists, clamp camera to valid range
		ctx.clampCamera(config)
	}

	// Grid tracks map dimensions (grow-only, no reallocation on shrink)
	ctx.World.Positions.ResizeGrid(config.MapWidth, config.MapHeight)

	if !cropped {
		// The map did not move, so no cursor is out of bounds and there is nothing
		// to reconcile. Announcing a same-cell move anyway would be a shared event
		// produced by one instance's terminal: EventCursorMoved dirties the
		// flow-field throttle, whose phase is shared state, so the two instances
		// would recompute their fields on different ticks and steer shared species
		// along fields of different ages. The view still has to follow, and that is
		// this instance's own business — the camera re-anchors directly.
		ctx.followLocalCursorCamera(config)
		return
	}

	// Clamp every cursor into the new map bounds and free it if the reflow blocked it;
	// CursorSystem applies the move, which is what re-anchors the camera
	ctx.World.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		pos, ok := ctx.World.Positions.GetPosition(e)
		if !ok {
			return true
		}
		x, y, _ := ctx.World.ResolveFreeCell(
			max(0, min(pos.X, config.MapWidth-1)),
			max(0, min(pos.Y, config.MapHeight-1)),
			component.WallBlockCursor)
		ctx.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y})
		return true
	})
}

// followLocalCursorCamera re-anchors the view on this instance's own cursor after a
// reflow that moved nothing in the world. Pure local view state: the camera is not
// compared across instances, and the cursor it follows is this instance's binding.
func (ctx *GameContext) followLocalCursorCamera(config *ConfigResource) {
	if !parameter.CameraEnabled {
		return
	}
	pos, ok := ctx.World.LocalCursor()
	if !ok {
		return
	}
	config.FollowCamera(pos.X, pos.Y)
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

// Session state is operator-owned: it describes how the game is being observed and
// driven, not the game itself, so it survives EventGameResetRequest.
// The list below is its definition; anything not named is world state and is rebuilt by reset.
//
//	MouseFreeMode, AutoFire   input preferences
//	TimeCtl scale             simulation rate
//	OverlayHUD, overlay pins  debug overlay layout
//
// Logging is excluded because target, level, scope and recorder depth are process
// configuration, and clearing them mid-session would close the log being read.
// Pending step requests are excluded in the other direction: they name an FSM region
// or event stream that reset destroys, so they are cancelled rather than kept.

// ResetSessionState restores every operator toggle to its startup value.
// Called only on the purge path, never by a plain reset.
func (ctx *GameContext) ResetSessionState() {
	ctx.MouseFreeMode.Store(false)
	ctx.AutoFire.Store(false)
	ctx.OverlayHUD.Store(false)
	ctx.ClearOverlayPins()
	ctx.SetOverlayContent(nil)
	ctx.TimeCtl.SetScale(ScaleNormal)
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
		// Protected entities survive the crop; the cursor is one of them
		if prot, ok := ctx.World.Components.Protection.GetComponent(e); ok &&
			prot.Mask&component.ProtectFromDeath != 0 {
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
	now := ctx.TimeCtl.RealTime()
	if now.Sub(ctx.lastFPSUpdate) >= time.Second {
		ctx.statFPS.Store(ctx.frameCountFPS)
		ctx.frameCountFPS = 0
		ctx.lastFPSUpdate = now
	}

	n := ctx.FrameNumber.Add(1)
	ctx.statFrame.Store(n)
	ctx.Correlation.SetFrame(uint64(n))
	return n
}

// === EVENT QUEUE METHODS ===

// PushEvent adds an event to the queue carrying the ambient producer tag,
// so a call site inside a WithOrigin scope needs no change
func (ctx *GameContext) PushEvent(eventType event.EventType, payload any) {
	ctx.World.PushEvent(eventType, payload)
}

// PushLocal stamps the player domain, for producers whose effect is this
// instance's alone: operator commands and per-instance input (D-10)
func (ctx *GameContext) PushLocal(eventType event.EventType, payload any) {
	ctx.World.PushLocal(eventType, payload)
}

// PushCrossing emits a D-3 crossing from a producer outside the world lock; see
// World.PushCrossing
func (ctx *GameContext) PushCrossing(eventType event.EventType, payload any) {
	ctx.World.PushCrossing(eventType, payload)
}

// PushCursorMove requests a cursor placement from a producer outside the world
// lock, advancing the D-18 prediction with it; see World.PushCursorMove
func (ctx *GameContext) PushCursorMove(e core.Entity, x, y int) {
	ctx.World.PushCursorMove(e, x, y)
}

// PushEventFull emits with explicit origin and domain tags, for replay and
// transport, which restore both from a record rather than from the ambient tags
func (ctx *GameContext) PushEventFull(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) {
	ctx.World.PushEventFull(eventType, payload, origin, domain)
}

// PushEventDomain emits with an explicit domain tag, for producers that run
// outside the world lock and therefore outside any WithDomain scope
func (ctx *GameContext) PushEventDomain(eventType event.EventType, payload any, domain core.Domain) {
	ctx.World.PushEventDomain(eventType, payload, domain)
}

// PushEventOrigin emits with an explicit producer tag, for producers that run
// outside the world lock and therefore outside any WithOrigin scope
func (ctx *GameContext) PushEventOrigin(eventType event.EventType, payload any, origin event.Origin) {
	ctx.World.PushEventOrigin(eventType, payload, origin)
}

// WithOrigin scopes the ambient producer tag; caller MUST hold updateMutex.
// CI guard: rg 'WithOrigin' internal/ must show only locked call sites.
func (ctx *GameContext) WithOrigin(o event.Origin, fn func()) { ctx.World.WithOrigin(o, fn) }

// === MODE ACCESSORS ===

// GetMode returns the current game mode
func (ctx *GameContext) GetMode() core.GameMode {
	return ctx.World.Resources.Game.State.GetMode()
}

// SetMode applies the mode and publishes context.mode; the applier path, no event
func (ctx *GameContext) SetMode(m core.GameMode) {
	ctx.World.Resources.Game.State.SetMode(m)
	if int(m) < len(core.ModeNames) {
		ctx.statMode.StoreIfChanged(core.ModeNames[m])
	}
}

// RequestMode applies a mode change and announces it, so the transition is a
// function of the event stream. Decision sites call this; MetaSystem's handler
// calls SetMode, which is why the emit is not folded into it.
// Caller MUST hold updateMutex: UpdateBoundsRadius reads component stores.
func (ctx *GameContext) RequestMode(m core.GameMode) {
	ctx.SetMode(m)
	ctx.World.UpdateBoundsRadius()
	ctx.PushEvent(event.EventModeChanged, &event.ModeChangedPayload{Mode: m})
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

// SetStatusMessage sets status message with optional duration and override.
// Expiry is game time: a message dilates with the rate and holds while paused.
func (ctx *GameContext) SetStatusMessage(msg string, duration time.Duration, override bool) {
	now := ctx.TimeCtl.Now().UnixNano()
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
