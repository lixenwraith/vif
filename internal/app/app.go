package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/probe"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/system"
	"github.com/lixenwraith/vif/internal/vlog"
)

// fallbackColorMode is published when no terminal exists to detect against
const fallbackColorMode = terminal.ColorMode256

// restartRequest is what one run asks the next one to be. Loop returns it and Run
// applies it to the command line it was given, so the operator's own flags still
// decide everything a restart does not name — a pinned -seed most of all.
type restartRequest struct {
	// Scenario replaces -s. Empty keeps the command line's, which is what a guest
	// rebuilding into a session that is about to re-offer it one wants.
	Scenario string

	// Host is the address to open hosting on once the next run's clock is running.
	// A coordinator changing scenario does not go back into a startup lobby: its
	// guests are already redialling, and the mid-run gate is the door they arrive
	// at — the same one a reconnect has always used.
	Host string

	// Rejoin says the next run follows this session rather than leads it, and Join
	// where to dial when the command line's address is not where the session is
	// now — succession moves the door. Join alone is a :join target, dialled once as
	// -join is. With neither the next run is solo, which is what an authority with
	// an empty roster has actually become.
	Rejoin bool
	Join   string
}

// App owns the wired runtime: services, world, input, scheduler, and the selected
// presentation adapter.
type App struct {
	cfg Config

	hub *service.Hub
	presentationState
	networkSvc *service.NetworkService

	world    *engine.World
	ctx      *engine.GameContext
	scenario resource.Scenario

	// restart is what this run is to be replaced by, latched either by the operator
	// command surface or by the authority's notice on the tick goroutine, and read
	// by Loop between two waits. Nil means this run ends when the player quits.
	restart      atomic.Pointer[restartRequest]
	inputMachine *input.Machine
	router       *mode.Router
	recorder     *journal.Recorder

	scheduler      *engine.Scheduler
	frameReady     chan struct{}
	gameUpdateDone <-chan struct{}

	pendingJoin  *network.PendingJoin
	sessionMu    sync.Mutex
	sessionOffer network.SessionOffer
	// sessionRoster is the lobby the coordinator has admitted so far. It grows one
	// entry per accepted connection and closes into the offer the start gate sends.
	sessionRoster []network.RosterEntry

	// midRunPort is the socket a solo run opened for itself with :host. A run
	// started with -host takes its endpoint from NetworkService instead, which the
	// hub owns and closes; this one is owned here because nothing else knows it
	// exists.
	midRunPort *network.SocketPort

	// lateJoins arms the mid-run gate on a host whose startup lobby has closed. It
	// is a flag rather than a late assignment because the accept goroutine reads
	// the hook: the gate exists from construction and answers nothing until the
	// lobby's own gate is done, which is what keeps the two from racing.
	lateJoins atomic.Bool

	// lobbyClosing refuses dials for the window between the startup lobby reading
	// its roster and lateJoins arming. Neither gate can serve a dial there — the
	// lobby's offers are already out, and the mid-run gate waits on a capture a
	// clock that has not started never produces — so the honest answer is a
	// refusal the dialer can retry rather than an identity and a silent wait.
	lobbyClosing atomic.Bool

	// midRunGate serialises the mid-run admission. The handshakes that reach it run
	// concurrently, but the gate they call reads a whole world and then waits on a
	// session-cumulative ready count, so two of them at once cannot tell whose
	// joiner confirmed.
	midRunGate sync.Mutex

	// firstJoiner is the geometry the first guest reported, guarded by sessionMu.
	// A dedicated host with no -size takes its map from it: it is the only real
	// terminal the session ever sees.
	firstJoiner network.JoinerReport

	// probe is the supervised run's endpoint, nil when none was configured.
	// probeTick and probeAt are its stall detector: the last tick a probe read and
	// when it read it, so a stall is measured across reads rather than inside one.
	probe     *probe.Server
	probeMu   sync.Mutex
	probeTick uint64
	probeAt   time.Time

	// life is the allocated session's lifetime policy: the first-guest window, the
	// empty-roster grace, and the drain a termination request opens. It exists on
	// every run and governs only the ones whose Config asked it to, so the dial and
	// probe paths can consult it without knowing which shape this run is.
	life *lifecycle.Controller

	// parked marks a dedicated host whose clock this run stopped because nobody is
	// in it, and vacantReset that the parked world has already been restarted for
	// this vacancy. They are separate from TimeControl's own paused flag because
	// only what parked a session may unpark it: an operator pause is refused in a
	// live session, and the lobby's pause is released by the start gate.
	parked      atomic.Bool
	vacantReset atomic.Bool

	// admissions bounds how often one dialling host may be admitted. It is built
	// with the App rather than with the session because a run can open one later
	// with :host, and a budget that started when hosting did would be a budget
	// reset by whatever closed the last session.
	admissions *network.AdmissionLimiter

	// telemetry is reserved during construction so a capture or an install can
	// publish its cost into a registry that is frozen by then.
	telemetry snapshot.Telemetry

	// The three halves of the authority protocol, built together because they hold
	// references to each other: corrections is what this instance publishes and
	// installs, authority whether it is allowed to, and reach the links a
	// succession needs. See internal/converge.
	corrections *converge.Corrections
	authority   *converge.Authority
	reach       *converge.Reach

	// staging is the second world a capture resolves into before it is written into
	// this one, built on first use and re-used for the life of the run: building one
	// per install costs 9 to 31 ms, which a correction five times a second cannot
	// afford.
	stageMu   sync.Mutex
	staging   *App
	stagedFor stagingKey
}

// New wires the runtime, releasing anything already started on failure. Errors are
// returned rather than panicked: the map editor and the wasm entry need them.
func New(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	a := &App{
		cfg:        cfg,
		hub:        service.NewHub(),
		admissions: network.NewAdmissionLimiter(),
		life:       lifecycle.New(cfg.Lifetime),
	}
	if a.cfg.HostAddress != "" && a.cfg.networkConfig == nil {
		a.cfg.networkConfig = a.hostNetworkConfig()
	}
	if a.cfg.JoinAddress != "" && a.cfg.networkConfig == nil {
		return nil, errors.New("join sessions must be started with app.Run")
	}
	if err := a.init(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// init wires the runtime in dependency order; a headless run skips presentation
func (a *App) init() error {
	vlog.Info("app", "msg", "init begin", "mode", a.cfg.Mode.String())

	// Root RNG seed; resolved first, since services and systems both derive
	// from it. A drawn seed is logged so any run replays with -seed.
	if a.cfg.Seed == 0 {
		a.cfg.Seed = uint64(time.Now().UnixNano()) // [wall] once per process
	}
	vlog.Info("app", "msg", "seed", "seed", a.cfg.Seed)

	// Embedders never call vlog.Configure, so the scope is applied here.
	// The CLI reaches this too and applies it twice; both resolve the spec
	// against ScopeAll, so the second application is a no-op.
	if a.cfg.LogScope != "" {
		if s, err := vlog.ParseScopes(a.cfg.LogScope, vlog.ScopeAll); err == nil {
			vlog.SetScopes(s)
		}
	}

	// Before the services, so a scenario that does not resolve fails in front of
	// the operator rather than after the terminal has been taken, and before the
	// journal anchor, which names what this run turned out to be reading.
	if err := a.readScenario(); err != nil {
		return err
	}
	if err := a.initServices(); err != nil {
		return err
	}
	a.initWorld()
	if err := a.initJournal(); err != nil {
		return err
	}
	if a.cfg.Mode.Presents() {
		a.initPresentation()
	}
	if err := a.initInput(); err != nil {
		return err
	}
	if err := a.initScheduler(); err != nil {
		return err
	}
	a.ctx.SessionCtl = sessionControl{a}

	vlog.Info("app", "msg", "init complete",
		"width", a.ctx.Width,
		"height", a.ctx.Height,
		"systems", len(a.world.Systems()))
	return nil
}

// initServices registers and initializes the I/O boundary. A driven run registers
// only what its mode presents, so no goroutine exists that the caller does not own.
func (a *App) initServices() error {
	// Event registry backs FSM trigger resolution and :emit; precedes FSM load
	event.EnsureRegistry()

	if err := a.initPresentationService(); err != nil {
		return err
	}
	if err := a.initNetworkService(); err != nil {
		return err
	}
	if err := a.initAudioService(); err != nil {
		return err
	}
	_ = a.hub.Register(service.NewFileService(resource.Files(a.cfg.Resources)))

	contentSrc, err := resource.Corpus(a.cfg.Resources)
	if err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	_ = a.hub.Register(service.NewContentService(contentSrc, a.cfg.Seed))

	// Init in dependency order; rolls back internally on failure
	return a.hub.InitAll()
}

// initWorld builds the ECS world, the game context and the system set
func (a *App) initWorld() {
	// Services take no world argument, so placement relative to InitAll is free
	a.world = engine.NewWorld()
	a.world.Resources.Rand = engine.NewRandResource(a.cfg.Seed)
	// A run reproducing a session draws that session's streams, and a session's
	// streams are drawn one below the number it reports: construction draws them and
	// then advances, so the game a host calls session N ran on the streams of N-1.
	// A host that has restarted is several games in, so a joiner counting from zero
	// would build a different world and be refused on the identity check for it.
	if a.cfg.Session > 1 {
		a.world.Resources.Rand.SetSession(a.cfg.Session - 1)
	}

	// Service resources bridged into the ECS
	a.hub.BindResources(a.world.Resources)
	if r := a.world.Resources.Network; r != nil {
		r.OnDeparture = a.releaseParticipant32
		r.SharedDigest = a.sharedDigestLocked
		r.OnCorrection = a.receiveCorrection
		r.OnSelective = a.receiveSelective
		r.OnTickClosed = a.tickClosed
		r.OnAuthority = a.receiveAuthorityFrame
		r.OnPeerLost = a.reportPeerLost
		r.OnSessionRestart = a.receiveSessionRestart
		// A session endpoint exists, so this run is shared for its whole life whether
		// or not a peer is attached at a given tick. Latching here rather than
		// reading the port keeps the anchor, the D-14 verdict and the playout barrier
		// answering one question, which is what a reproduction adopts.
		a.world.MarkSessionShared()
	}

	// The terminal supplies color whenever one exists, but dimensions only when the
	// mode says so; a replay's come from the journal, via config
	width, height, colorMode := a.presentationGeometry(a.cfg.Width, a.cfg.Height)

	// GameContext initializes the remaining world resources.
	// A driven run uses the manual clock: game time is a pure function of ticks.
	if a.cfg.Mode.Driven() {
		a.ctx = engine.NewGameContextWithClock(a.world, width, height, engine.NewManualClock())
	} else {
		a.ctx = engine.NewGameContext(a.world, width, height)
	}
	a.world.Resources.Config.ColorMode = colorMode

	a.applyMapLatch()

	if n := a.cfg.StatTicks; n != 0 {
		if n < 0 {
			n = 0 // explicit disable
		}
		a.world.Resources.Status.SetSnapshotInterval(uint64(n))
	}

	// Recorder is laid out at Freeze; enabling it here only sizes the ring.
	// Opt-in: an explicit depth always wins, the default applies only when a
	// log session is configured. A bare run installs no recorder, so no
	// trigger can write a file the operator did not ask for.
	recDepth := a.cfg.RecTicks
	if recDepth == 0 && vlog.Enabled() {
		recDepth = parameter.RecorderDepthTicks
	}
	a.world.Resources.Status.EnableRecorder(recDepth)

	// Corpus telemetry needs the registry NewGameContext creates
	service.MustGet[*service.ContentService](a.hub, "content").
		PublishStatus(a.world.Resources.Status)
	a.telemetry = snapshot.NewTelemetry(a.world.Resources.Status)
	// The protocol is built here rather than before init because it reads the
	// telemetry cells and the status registry this world owns. Nothing can reach
	// it earlier: the transport hooks bound above answer nothing until the service
	// is started, which is after construction returns.
	a.corrections, a.authority, a.reach =
		converge.New(instance{a}, a.telemetry, a.world.Resources.Status)
	if a.cfg.SlowPolicy != nil {
		a.authority.SetSlowPolicy(*a.cfg.SlowPolicy)
	}

	// Initial rate; ParseScale rejects "" so a bare run stays at real time
	if s, ok := engine.ParseScale(a.cfg.TimeScaleSpec); ok {
		a.ctx.TimeCtl.SetScale(s)
	}
	// The host can wait in the lobby much longer than a joiner. Freeze both clocks
	// before the FSM creates any game-time deadline, then release them only after
	// the common start gate. Otherwise the lobby wait ages the host's gold timer and
	// every other absolute deadline before tick one.
	if a.cfg.HostAddress != "" || a.cfg.JoinAddress != "" {
		a.ctx.TimeCtl.SetPaused(true)
	}

	// Systems; AddSystem sorts by Priority(), manifest order breaks ties
	for _, sys := range manifest.BuildSystems(a.world) {
		a.world.AddSystem(sys, manifest.ProfileFor(sys.Name()))
	}
	// This game's streams are drawn; advance so the next game differs
	a.world.Resources.Rand.NextSession()
}

// applyMapLatch installs this run's D-14 position before any system is built and
// before the FSM boot script spawns cursor slot zero at the centre of the map.
// LockMap alone closes the crop path and engages the playout barrier; a run
// reproducing a session also carries the bounds it must start on.
func (a *App) applyMapLatch() {
	if a.cfg.LockMap {
		a.world.MarkSessionShared()
	}
	cfg := a.world.Resources.Config
	if a.cfg.MapWidth <= 0 || a.cfg.MapHeight <= 0 {
		return
	}
	if a.cfg.MapWidth == cfg.MapWidth && a.cfg.MapHeight == cfg.MapHeight &&
		a.cfg.CropOnResize == cfg.CropOnResize {
		return
	}
	a.world.SetupLevel(a.cfg.MapWidth, a.cfg.MapHeight, false, a.cfg.CropOnResize)
}

// initInput builds the intent pipeline. Kept headless because intents are the
// injection path; only the terminal mouse sink is skipped.
func (a *App) initInput() error {
	a.inputMachine = input.NewMachine()
	if err := a.loadKeymap(); err != nil {
		return err
	}
	a.router = mode.NewRouter(a.ctx, a.inputMachine)
	if a.cfg.Mode.OwnsInput() {
		a.router.SetMouseModeApplier(a.applyMouseMode)
	}
	return nil
}

// handleIntent tags the producer and keeps the whole router path under the world
// lock; mode must never acquire that lock itself.
func (a *App) handleIntent(intent *input.Intent) bool {
	origin := event.OriginInput
	if intent.MacroPlayback {
		origin = event.OriginMacro
	}
	cont := true
	a.world.RunSafe(func() {
		a.world.WithOrigin(origin, func() {
			cont = a.router.Handle(intent)
		})
	})
	return cont
}

// processInputTick takes the lock here because no input event wraps timer-driven
// reads of cursor state.
func (a *App) processInputTick() bool {
	emitted := false
	a.world.RunSafe(func() {
		emitted = a.router.ProcessInputTick()
	})
	return emitted
}

// initScheduler wires the scheduler, loads the FSM, and registers the
// systems that handle events
func (a *App) initScheduler() error {
	a.frameReady = make(chan struct{}, 1)
	var resetChan chan<- struct{}
	a.scheduler, a.gameUpdateDone, resetChan = engine.NewScheduler(
		a.world,
		a.ctx.TimeCtl,
		parameter.GameUpdateInterval,
		a.frameReady,
	)
	a.ctx.ResetChan = resetChan

	if err := a.scheduler.LoadScenarioFromFS(
		a.scenario.FS(), a.scenario.Entry(), manifest.RegisterFSMComponents); err != nil {
		return fmt.Errorf("load scenario %s: %w", a.scenario.Name, err)
	}

	// MetaSystem needs the completed GameContext and therefore joins here. Its
	// profile remains in manifest.ContextSystems for the ordinary validation and
	// fingerprint checks.
	meta := system.NewMetaSystem(a.ctx)
	a.world.AddSystem(meta, manifest.ProfileFor(meta.Name()))

	// The declared dependency graph is resolved once the set is complete; an
	// unknown name or a cycle is a startup error, not a runtime degradation
	order, err := a.world.SystemInitOrder()
	if err != nil {
		return fmt.Errorf("system dependencies: %w", err)
	}
	vlog.Debug("app", "msg", "system init order", "systems", strings.Join(order, ","))

	for _, sys := range a.world.Systems() {
		if h, ok := sys.(event.Handler); ok {
			a.scheduler.RegisterEventHandler(h)
		}
	}
	return nil
}

// initJournal opens the replay journal and installs it on the event queue.
// Opt-in: it records every non-system event for the life of the run.
func (a *App) initJournal() error {
	if !a.cfg.Journal {
		return nil
	}

	r, err := journal.Start(a.world.Resources.Event.Queue, a.buildAnchor(), a.cfg.JournalSink)
	if err != nil {
		return err
	}
	a.recorder = r
	vlog.Info("app", "msg", "journal open", "path", r.Path())
	return nil
}

// buildAnchor describes what a replay must reproduce. Reads the telemetry
// ContentService published during initWorld, so anchor and verification share a source.
func (a *App) buildAnchor() event.JournalAnchor {
	reg := a.world.Resources.Status
	cfg := a.world.Resources.Config
	return event.JournalAnchor{
		Speed:          a.ctx.TimeCtl.Scale().String(),
		ScenarioID:     a.scenario.Name,
		ScenarioDigest: a.scenario.Digest(),
		ContentID:      reg.Strings.Get("content.source").Load(),
		ContentPin:     service.MustGet[*service.ContentService](a.hub, "content").Pin(),
		ContentFiles:   uint64(reg.Ints.Get("content.files").Load()),
		ContentBlocks:  uint64(reg.Ints.Get("content.blocks").Load()),
		ContentLines:   uint64(reg.Ints.Get("content.lines").Load()),
		Seed:           a.world.Resources.Rand.Root(),
		Session:        a.world.Resources.Rand.Session(),
		TickInterval:   int64(parameter.GameUpdateInterval),
		Width:          a.ctx.Width,
		Height:         a.ctx.Height,
		MapWidth:       cfg.MapWidth,
		MapHeight:      cfg.MapHeight,
		CropOnResize:   cfg.CropOnResize,
		SessionShared:  a.world.SessionShared(),
	}
}

// Close stops the scheduler before the services it depends on
// Safe on a partially constructed App
func (a *App) Close() {
	vlog.Info("app", "msg", "shutdown begin")
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	if a.pendingJoin != nil {
		_ = a.pendingJoin.Close()
	}
	if a.corrections != nil {
		a.corrections.Close()
	}
	a.reach.Close()
	a.closeProbe()
	a.closeMidRunPort()
	a.discardStagingWorld()
	a.hub.StopAll()

	if a.recorder != nil {
		stats, err := a.recorder.Close()
		vlog.Info("app", "msg", "journal close",
			"path", stats.Path, "records", stats.Emitted, "encode_failed", stats.EncodeFailed)
		if err != nil {
			vlog.Error("app", "msg", "journal close", "error", err.Error())
		}
	}

	vlog.Info("app", "msg", "shutdown complete")
}

// loadKeymap merges an external key table over the embedded default document.
func (a *App) loadKeymap() error {
	base := input.DefaultKeyTable()
	path, err := resource.Keymap(a.cfg.Resources)
	if err != nil {
		return fmt.Errorf("keymap path: %w", err)
	}
	if path == "" {
		a.inputMachine.SetKeyTable(base)
		a.ctx.KeyTable = base
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("keymap load: %w", err)
	}

	override, err := input.LoadKeyConfig(data)
	if err != nil {
		return fmt.Errorf("keymap config %s: %w", path, err)
	}
	kt := input.MergeKeyTable(base, override)
	a.inputMachine.SetKeyTable(kt)
	a.ctx.KeyTable = kt
	return nil
}

// readScenario reads this run's scenario whole. Reading it rather than pointing the
// loader at a directory is what gives the run a digest to be identified by, and the
// one seam a scenario that arrived from a peer enters through.
func (a *App) readScenario() error {
	sc, err := resource.LoadScenario(a.cfg.Resources)
	if err != nil {
		return fmt.Errorf("scenario: %w", err)
	}
	a.scenario = sc
	vlog.Info("app", "msg", "scenario", "name", sc.Name, "digest", sc.Short(), "files", sc.Files())
	return nil
}
