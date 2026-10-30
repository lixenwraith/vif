package engine

import (
	"github.com/lixenwraith/vif/pkg/vmath"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/prof"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/navigation"
	"github.com/lixenwraith/vif/pkg/position"
)

const (
	AutoFireOff uint32 = iota
	AutoFireMain
	AutoFireBoth
)

// AutoFireWords names the three settings as :auto and vif.toml spell them
var AutoFireWords = [...]string{"off", "main", "on"}

// NavigationDebugState is one runtime's operator view of navigation internals.
type NavigationDebugState struct {
	Flow                 *navigation.FlowFieldCache
	CompositeFlow        *navigation.FlowFieldCache
	CompositePassability *navigation.CompositePassability
	ShowFlow             bool
	ShowComposite        bool
	GroupID              uint8
}

// SessionController methods run under the world lock; taking it again deadlocks.
type SessionController interface {
	// Empty authority keeps the run's policy.
	BeginHosting(addr, authority string) error
	Join(target string) error
	RequestSession(site string, players int, scenario string) error
	HostError() error
	JoinError() error
	// Leave takes the run out of its session into one of its own, the world as it
	// stands; the run's loop serves it and the status line says how it went.
	Leave() error
	LeaveError() error
	// False means the current scenario can reset without replacing the run.
	ChangeScenario(name string) (bool, error)
	// Scenarios names what ChangeScenario starts by name, and the one running.
	Scenarios() (names []string, current string)
	SessionSummary() string
	// A solo run opens a session with no listener for its bots.
	AddBot(graph string) error
	DropBot(slot int) error
	RemoveBot(id uint64) error
	BotSummary() string
	Bots() []BotSeat
	Participants() []SessionParticipant
	DropPlayer(slot int) error
	RemovePlayer(entity core.Entity) error
	// StartJournal asks for a journal from where the run stands, this instance's
	// with slot -1 or else the bot seat's this run holds on slot, and StopJournal
	// for one to close; the run's loop serves either once the command has settled
	// and the status line says how it went. Journaling is whether this instance
	// records once its asks are served, and JournalSummary names its journal and seats'.
	StartJournal(slot int) error
	StopJournal(slot int) error
	JournalError() error
	Journaling() bool
	JournalSummary() string
}

type SessionParticipant struct {
	Entity core.Entity
	Slot   uint8
	Holder uint32
	Local  bool
	Away   bool // a Wraith waits in the slot for the participant's return
}

type BotSeat struct {
	ID       uint64 // Holder-local identity; roster slots and participant IDs can be reused
	Slot     uint8
	Graph    string
	Joining  bool
	Stopping bool
}

// GameContext holds all game state including the ECS world
type GameContext struct {
	// === Immutable After Init ===

	// Set once in NewGameContext, and pointers/values never modified, safe for concurrent read without sync

	World   *World       // ECS world; has internal lock
	State   *GameState   // Centralized game state; has internal lock
	TimeCtl *TimeControl // Sole time surface: reads, rate, pause, step; registry-bound

	// How the run started, which a purge returns to
	StartScale     TimeScale
	StartAutoFire  uint32
	StartMouseFree bool

	KeyTable        *input.KeyTable
	Log             *vlog.Log
	NavigationDebug NavigationDebugState

	// === Channels ===

	ResetChan chan<- struct{} // FSM reset signal; wired to Scheduler

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

	AutoFire atomic.Uint32 // AutoFireOff, AutoFireMain, or AutoFireBoth

	// Relaying carries any participant's artifacts toward the authority, not only
	// those that joined through this run; off unless its player turns it on
	Relaying atomic.Bool

	// WaitStart holds each new game this run starts until its player's first press;
	// only a run a person plays turns it on
	WaitStart atomic.Bool

	KeyCapture input.KeyCapture // Keys the scenario takes from play-mode parsing

	// Viewer marks the command line as a replay viewer's: the recording authors the
	// world, so a command may inspect it but not change it.
	Viewer atomic.Bool

	// ReplaySeek hands :replay to the player presenting this world: tick of the run
	// it shows, or the recording's start on restart. Nil outside a replay.
	ReplaySeek func(tick uint64, restart bool)

	// === Main-Loop Exclusive ===

	// Terminal geometry, written only by MetaSystem's EventScreenResize handler
	// under the world lock. Every reader holds the same lock: HandleResizeLocked,
	// SnapshotContext, and the RenderContext build in App.frame.

	Width, Height            int // Terminal dimensions
	GameXOffset, GameYOffset int // Game area offset from terminal origin

	// The surface overlays are placed on when it is not the simulated terminal;
	// zero when it is. Written by a replay's presenting loop, which runs its ticks.
	presentW, presentH int

	// === Context Exclusive ===

	// No sync required
	lastFPSUpdate time.Time
	frameCountFPS int64

	// === Atomic States ===

	// The command line or overlay open over the play mode GameState holds; ModeNormal for none
	operatorMode atomic.Int32

	// Status bar state (atomic pointers for lock-free access)
	commandText   atomic.Pointer[string]
	searchText    atomic.Pointer[string]
	statusMessage atomic.Pointer[statusLine] // nil when none was posted
	lastCommand   atomic.Pointer[string]
	// Command-mode cursor position (rune offset within command text)
	commandCursorPos atomic.Int32

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

	// Telemetry card filter; editing routes text input to the query
	overlayFilter        atomic.Pointer[string]
	overlayFilterEditing atomic.Bool

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
	return newGameContext(world, width, height, NewPausableClock(), vlog.Default())
}

// NewGameContextWithClock creates a GameContext on a caller-supplied time source.
// Headless and replay runs pass a ManualClock.
func NewGameContextWithClock(world *World, width, height int, clock Clock) *GameContext {
	return newGameContext(world, width, height, clock, vlog.NewLog(""))
}

func newGameContext(world *World, width, height int, clock Clock, log *vlog.Log) *GameContext {
	ctx := &GameContext{
		World:          world,
		Width:          width,
		Height:         height,
		Log:            log,
		StartScale:     ScaleNormal,
		StartAutoFire:  AutoFireBoth,
		StartMouseFree: parameter.DefaultMouseFreeMode,
	}

	// Calculate game area
	// gameWidth, gameHeight := ctx.updateGameArea()
	viewportWidth, viewportHeight := ctx.updateGameArea()

	// -- Initialize Resources --

	// 1. Status Registry (before other resources that may use it)
	world.Resources.Status = status.NewRegistry()
	world.Resources.Status.SetLog(log)
	world.Resources.Status.SetSnapshotInterval(parameter.StatSnapshotTicks)
	world.updateMutex.BindStatus(world.Resources.Status)
	world.Positions.BindTelemetry(world.Resources.Status)
	world.BindPredictionTelemetry(world.Resources.Status)
	world.Resources.Prof = prof.New(world.Resources.Status)
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
	// Initial: Map = Viewport, CropOnResize on until a level setup says otherwise
	world.Resources.Config = &ConfigResource{
		MapWidth:       viewportWidth,
		MapHeight:      viewportHeight,
		ViewportWidth:  viewportWidth,
		ViewportHeight: viewportHeight,
		CameraX:        0,
		CameraY:        0,
		CropOnResize:   true,
		CellAspect:     vmath.RefAspect,
	}
	world.Positions.Resize(viewportWidth, viewportHeight)

	// 5. Time Resource (Initial state). Boot FSM actions can create deadlines
	// before tick one, while a network lobby is paused. Seed their game clock at
	// the same tick-derived origin processTick uses, never at the pacing clock's
	// wall instant.
	world.Resources.Time = &TimeResource{}
	world.Resources.Time.Update(SimEpoch, ctx.TimeCtl.RealTime(), world.tick)

	// 6. Event Queue Resource
	world.Resources.Event = &EventQueueResource{Queue: event.NewEventQueue()}

	// 7. Game GameState
	ctx.State = NewGameState()
	world.Resources.Game = &GameStateResource{State: ctx.State}

	// 8. Transient Resource
	world.Resources.Transient = NewTransientResource()
	world.Resources.View = NewViewResource()
	world.Resources.KrakenShape = NewKrakenShapeResource()
	world.Resources.Drift = &DriftResource{}
	world.Resources.Cave = &CaveResource{}
	world.Resources.Entry = &EntryResource{}

	// 9. Cursor roster; the FSM spawns cursors, so the world starts with none
	world.Resources.Player = &PlayerResource{status: world.Resources.Status}

	// 10. Target Resource
	world.Resources.Target = &TargetResource{}

	// 11. Initialize atomic string pointers to empty strings
	empty := ""
	ctx.commandText.Store(&empty)
	ctx.searchText.Store(&empty)
	ctx.lastCommand.Store(&empty)
	ctx.overlayTitle.Store(&empty)

	// 12. Operator session state; see the session state contract above ResetSessionState
	ctx.recomputeOverlayGeometry()
	ctx.SetMode(core.ModeNormal)
	ctx.lastFPSUpdate = time.Now() // [wall] see IncrementFrameNumber

	// 13. Initial input state; a plain reset keeps it and a purge restores it
	ctx.MouseFreeMode.Store(ctx.StartMouseFree)
	ctx.AutoFire.Store(ctx.StartAutoFire)

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

	// The viewport is what the resize and reset paths write the map from, so it is
	// bounded here rather than at each of them: a terminal that reports a size the
	// grid cannot hold is presented at the largest map there is, instead of
	// producing bounds no cell exists for.
	return position.Clamp(gameWidth, gameHeight)
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
		config.MapWidth, config.MapHeight = config.FloorMapSize(viewportWidth, viewportHeight)
		ctx.cleanupOutOfBoundsEntities(config.MapWidth, config.MapHeight)
		// Reset camera
		config.CameraX = 0
		config.CameraY = 0
	} else {
		if config.CropOnResize {
			ctx.Log.Info("app", "msg", "map size locked",
				"map_w", config.MapWidth, "map_h", config.MapHeight,
				"viewport_w", viewportWidth, "viewport_h", viewportHeight)
		}
		// Map persists, clamp camera to valid range
		ctx.clampCamera(config)
	}

	// Grid tracks map dimensions (grow-only, no reallocation on shrink)
	ctx.World.Positions.Resize(config.MapWidth, config.MapHeight)

	if !cropped {
		// The map did not move, so no cursor needs reconciling. A same-cell move
		// would be a shared event from one terminal: EventCursorMoved dirties the
		// flow-field throttle, whose phase is shared state. The camera re-anchors.
		ctx.World.FollowLocalCursor()
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

// === Overlay ===

// OverlayGeometry returns the overlay window placement for the current terminal size
func (ctx *GameContext) OverlayGeometry() OverlayGeometry {
	if g := ctx.overlayGeom.Load(); g != nil {
		return *g
	}
	return OverlayGeometry{}
}

// SetPresentationSize places overlays on a surface other than the simulated
// terminal: a replay simulates the recorded screen and draws on the viewer's.
func (ctx *GameContext) SetPresentationSize(width, height int) {
	ctx.presentW, ctx.presentH = width, height
	ctx.recomputeOverlayGeometry()
}

// recomputeOverlayGeometry republishes window placement and screen telemetry
func (ctx *GameContext) recomputeOverlayGeometry() {
	w, h := ctx.Width, ctx.Height
	if ctx.presentW > 0 {
		w, h = ctx.presentW, ctx.presentH
	}
	g := ComputeOverlayGeometry(w, h)
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

// ToggleOverlayPin adds or removes a group key, preserving pin order; a new pin
// turns the HUD on. Copy-on-write: readers keep the snapshot they loaded.
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
		ctx.OverlayHUD.Store(true)
	}
	ctx.overlayPins.Store(&next)
}

// ClearOverlayPins removes every pin
func (ctx *GameContext) ClearOverlayPins() {
	var empty []string
	ctx.overlayPins.Store(&empty)
}

// OverlayFilter returns the telemetry card query, empty when unfiltered
func (ctx *GameContext) OverlayFilter() string {
	if p := ctx.overlayFilter.Load(); p != nil {
		return *p
	}
	return ""
}

func (ctx *GameContext) SetOverlayFilter(query string) {
	ctx.overlayFilter.Store(&query)
}

// IsOverlayFilterEditing reports whether text input edits the card query
func (ctx *GameContext) IsOverlayFilterEditing() bool {
	return ctx.overlayFilterEditing.Load()
}

func (ctx *GameContext) SetOverlayFilterEditing(editing bool) {
	ctx.overlayFilterEditing.Store(editing)
}

// Session state is operator-owned — mouse free mode, auto-fire, simulation rate,
// overlay HUD and pins, standing breakpoints, the mode with its lines and overlay —
// and survives EventGameResetRequest; anything else is world state. Logging is
// process configuration and is excluded; pending step requests name what a reset
// destroys and are cancelled.

// ResetSessionState returns the operator's state to its startup values: the HUD,
// pins, overlay content and standing breakpoints cleared, free mouse, auto-fire and
// the rate as the run began. Called only on the purge path, never by a plain reset.
func (ctx *GameContext) ResetSessionState() {
	ctx.OverlayHUD.Store(false)
	ctx.ClearOverlayPins()
	ctx.SetOverlayContent(nil)
	ctx.TimeCtl.ClearBreaks(nil)
	ctx.MouseFreeMode.Store(ctx.StartMouseFree)
	ctx.AutoFire.Store(ctx.StartAutoFire)
	// A live session's rate is its host's, so a purge there leaves it as well.
	if !ctx.World.Session().Live {
		ctx.TimeCtl.SetScale(ctx.StartScale)
	}
}

// SetTickInterval fixes the run's tick before its scheduler is built, the boot's
// game clock with it.
func (ctx *GameContext) SetTickInterval(d time.Duration) {
	ctx.World.tick = d
	ctx.World.Resources.Time.Update(SimEpoch, ctx.TimeCtl.RealTime(), d)
}

// FrameInterval is how often the run is drawn: a third of its tick, so each tick is
// drawn three times, and no faster than parameter.FrameMinInterval.
func (ctx *GameContext) FrameInterval() time.Duration {
	return max(ctx.World.tick/3, parameter.FrameMinInterval)
}

// === Viewport and Bounds ===

// clampCamera constrains camera position to valid range
// When Viewport >= Map on an axis, camera is 0 and ConfigResource.MapOffset
// centers that axis in the renderer and inverse input transform.
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

// IncrementFrameNumber advances the frame authority (called by Render Loop).
// The count is a metric rather than a log stamp: nothing logs from the render
// goroutine, and a headless run never calls this at all.
func (ctx *GameContext) IncrementFrameNumber() int64 {
	// [wall] Frames per wall second: a replay's manual clock stands still through a
	// pause or a step, which read as over a hundred frames once play resumed.
	ctx.frameCountFPS++
	now := time.Now()
	if now.Sub(ctx.lastFPSUpdate) >= time.Second {
		ctx.statFPS.Store(ctx.frameCountFPS)
		ctx.frameCountFPS = 0
		ctx.lastFPSUpdate = now
	}

	n := ctx.FrameNumber.Add(1)
	ctx.statFrame.Store(n)
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

// PushLocalOrigin stamps both an explicit producer and the player domain.
func (ctx *GameContext) PushLocalOrigin(eventType event.EventType, payload any, origin event.Origin) {
	ctx.World.PushLocalOrigin(eventType, payload, origin)
}

// WithOrigin scopes the ambient producer tag; caller MUST hold updateMutex.
// CI guard: rg 'WithOrigin' internal/ must show only locked call sites.
func (ctx *GameContext) WithOrigin(o event.Origin, fn func()) { ctx.World.WithOrigin(o, fn) }

// === MODE ACCESSORS ===

// GetMode returns the current game mode: the operator's, else the play mode
func (ctx *GameContext) GetMode() core.GameMode {
	if m := core.GameMode(ctx.operatorMode.Load()); m.Operator() {
		return m
	}
	return ctx.World.Resources.Game.State.GetMode()
}

// SetMode applies the mode and publishes context.mode; the applier path, no event.
// An operator mode opens over the play mode, and a play mode closes it.
func (ctx *GameContext) SetMode(m core.GameMode) {
	if m.Operator() {
		ctx.operatorMode.Store(int32(m))
		ctx.publishMode()
		return
	}
	ctx.operatorMode.Store(int32(core.ModeNormal))
	ctx.SetPlayMode(m)
}

// SetPlayMode applies a play mode under whatever the operator has open, as a
// replayed one lands under a viewer's own command line.
func (ctx *GameContext) SetPlayMode(m core.GameMode) {
	ctx.World.Resources.Game.State.SetMode(m)
	ctx.publishMode()
}

func (ctx *GameContext) publishMode() {
	if m := ctx.GetMode(); int(m) < len(core.ModeNames) {
		ctx.statMode.StoreIfChanged(core.ModeNames[m])
	}
}

// BindLog makes l the handle this context's world writes through, for a runtime
// whose log is not the one its clock implies. Call before anything logs.
func (ctx *GameContext) BindLog(l *vlog.Log) {
	ctx.Log = l
	ctx.World.Resources.Status.SetLog(l)
}

// RequestMode applies a mode change and announces a changed play mode, so the run's
// mode is a function of the event stream; the operator's stays out of it. Decision
// sites call this; MetaSystem's handler applies the play mode, so the emit is not
// folded into SetMode. Caller MUST hold updateMutex: UpdateBoundsRadius reads
// component stores.
func (ctx *GameContext) RequestMode(m core.GameMode) {
	played := ctx.World.Resources.Game.State.GetMode()
	ctx.SetMode(m)
	if ctx.Viewer.Load() || m.Operator() || m == played {
		return // a replay viewer borrows the play mode; the recording authors the world
	}
	ctx.World.UpdateBoundsRadius()
	ctx.PushLocal(event.EventModeChanged, &event.ModeChangedPayload{Mode: m})
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

// statusLine is one posted message: what it says, how it ranks, and when it was
// posted, in wall-clock Unix nano. One value, so a reader never sees half of a write.
type statusLine struct {
	text  string
	kind  parameter.StatusKind
	since int64
}

// SetStatusMessage posts msg, unless the message shown outranks it and has been up
// for less than its kind's Min (parameter.StatusKinds).
func (ctx *GameContext) SetStatusMessage(msg string, kind parameter.StatusKind) {
	now := ctx.TimeCtl.RealTime().UnixNano()
	next := &statusLine{text: msg, kind: kind, since: now}
	for {
		cur := ctx.statusMessage.Load()
		if cur != nil && cur.kind > kind && now < cur.since+parameter.StatusKinds[cur.kind].Min.Nanoseconds() {
			return
		}
		if ctx.statusMessage.CompareAndSwap(cur, next) {
			return
		}
	}
}

// GetStatusMessage returns the message last posted, whether or not it is still shown
func (ctx *GameContext) GetStatusMessage() string {
	if l := ctx.statusMessage.Load(); l != nil {
		return l.text
	}
	return ""
}

// ShownStatusMessage returns the message the bar draws at now (Unix nano): the one
// last posted, until its kind's Shown has passed.
func (ctx *GameContext) ShownStatusMessage(now int64) string {
	if l := ctx.statusMessage.Load(); l != nil && now < l.since+parameter.StatusKinds[l.kind].Shown.Nanoseconds() {
		return l.text
	}
	return ""
}

// ClearStatusMessage removes the message, whatever its kind
func (ctx *GameContext) ClearStatusMessage() { ctx.statusMessage.Store(nil) }

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
		// The filter belongs to one viewing; the next open starts unfiltered
		ctx.overlayFilter.Store(&empty)
		ctx.overlayFilterEditing.Store(false)
	}
	ctx.overlayScroll.Store(0)
	ctx.overlayContentH.Store(0)
	ctx.overlayCards.Store(nil)
	ctx.syncOverlaySelection(content)
}

// Rebuilds retain the selected key when it still exists.
func (ctx *GameContext) syncOverlaySelection(content *core.OverlayContent) {
	if content != nil && content.Layout == core.OverlayLayoutMenu && content.Menu != nil {
		ctx.overlaySelectable.Store(false)
		prev := ctx.GetOverlaySelection()
		for _, row := range content.Menu.Rows {
			if row.Key == prev {
				return
			}
		}
		key := ""
		if len(content.Menu.Rows) > 0 {
			key = content.Menu.Rows[0].Key
		}
		ctx.SetOverlaySelection(key)
		return
	}
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

// SetPaused requests a pause change. A replay viewer's command line holds playback
// itself and leaves the recorded clock alone.
func (ctx *GameContext) SetPaused(paused bool) {
	if ctx.Viewer.Load() {
		return
	}
	ctx.PushLocalOrigin(event.EventGamePauseRequest, &event.GamePausePayload{Paused: paused}, event.OriginDevice)
}

// Hold starts or ends this instance's hold and, when that changed, tells its systems
// what the clock now does; what the player is told is the caller's.
func (ctx *GameContext) Hold(held bool) bool {
	if !ctx.TimeCtl.SetHeld(held) {
		return false
	}
	ctx.PushLocalOrigin(event.EventGamePauseChanged, &event.GamePausePayload{Paused: ctx.TimeCtl.IsPaused()}, event.OriginDevice)
	return true
}

// HoldAs is Hold for a hold that waits for what kind does; see TimeControl.HoldAs.
func (ctx *GameContext) HoldAs(kind HoldKind) {
	if ctx.TimeCtl.HoldAs(kind) {
		ctx.PushLocalOrigin(event.EventGamePauseChanged, &event.GamePausePayload{Paused: true}, event.OriginDevice)
	}
}

// PauseForInput pauses a solo run while the operator types a command or reads an
// overlay. A live session keeps ticking for every participant, so there the request
// is not made: it would only earn the refusal an explicit pause does.
func (ctx *GameContext) PauseForInput() {
	if ctx.World.Session().Pausable() {
		ctx.SetPaused(true)
	}
}
