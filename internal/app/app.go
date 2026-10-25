package app

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/probe"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/system"
	"github.com/lixenwraith/vif/internal/vlog"
)

// fallbackColorMode is published when no terminal exists to detect against
const fallbackColorMode = terminal.ColorMode256

// restartRequest is what one run asks the next one to be. Loop returns it and
// Config.restarted builds the next run from it on the command line Run was given,
// so the operator's own flags still decide everything a restart does not name — a
// pinned -seed most of all.
type restartRequest struct {
	Notice string
	// Scenario replaces -s. Empty keeps the command line's, which is what a guest
	// rebuilding into a session that is about to re-offer it one wants.
	Scenario string

	// Host is the address to open hosting on once the next run's clock is running.
	// A coordinator changing scenario does not go back into a startup lobby: its
	// guests are already redialling, and the mid-run gate is the door they arrive
	// at — the same one a reconnect has always used.
	Host string

	// Rejoin says the next run follows this session rather than leads it, and Join
	// where to dial now that succession moved the door. Join alone is a :join target
	// that dialled holds already admitted; dialled alone is this run's own door,
	// redialled once its host went. With none the next run is solo, which is what an
	// authority with an empty roster has become.
	Rejoin  bool
	Join    string
	dialled *joinDial

	// press says the next run's player started its join, so its seat waits for
	// their press. provided is the scenario a join already holds, which a run
	// joining the same session again has no need to fetch twice.
	press    bool
	provided *resource.Scenario

	// Bots are the graphs of the seats this run held, which the next one seats again.
	Bots []string

	// Play is how this run was left, which the next one starts as.
	Play paths.Play

	// Journal opens a journal in the next run from its start: this run was
	// journaling, or asked to start, when it was replaced.
	Journal bool
}

// replaceWith latches req as the run to build next, and is the only thing that
// does. Its seats are read now rather than at the switch: what asks for a restart
// may be the session closing, which ends them before this run is replaced. The
// first request stands; a later one is refused.
func (a *App) replaceWith(req *restartRequest) bool {
	req.Bots = a.membership.seatGraphs()
	return a.restart.CompareAndSwap(nil, req)
}

// App owns the wired runtime: services, world, input, scheduler, and the selected
// presentation adapter.
type App struct {
	cfg Config

	hub *service.Hub
	log *vlog.Log
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

	scheduler      *engine.Scheduler
	frameReady     chan struct{}
	gameUpdateDone <-chan struct{}

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

	// glue is the correction glue over this run's world: what the authority protocol
	// reads and writes, and what a journal's written worlds and a join's install go
	// through. It reads the run through correctionRun.
	glue corrector

	// membership is this run's place in its session, and the protocol's view of it
	// beside the glue. It reads the run through membershipRun.
	membership membership

	// scribe keeps this run's journal and reads one back. It reads the run through
	// scribeRun.
	scribe scribe

	// The three halves of the authority protocol, built together because they hold
	// references to each other: corrections is what this instance publishes and
	// installs, authority whether it is allowed to, and reach the links a
	// succession needs. See internal/converge.
	corrections *converge.Corrections
	authority   *converge.Authority
	reach       *converge.Reach
}

// New wires the runtime, releasing anything already started on failure. Errors are
// returned rather than panicked: the map editor and the wasm entry need them.
func New(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	log := cfg.log
	if log == nil {
		// A driven run stamps its own ticks: a process may step several at once.
		log = vlog.Default()
		if cfg.Mode.Driven() {
			log = vlog.NewLog("")
		}
	}
	a := &App{
		cfg:  cfg,
		log:  log,
		hub:  service.NewHub(log),
		life: lifecycle.New(cfg.Lifetime),
	}
	a.membership.open(a, &a.cfg, &a.scenario, log, &a.glue, a.life)
	a.scribe.open(a, &a.cfg, &a.scenario, log, a.hub, &a.glue)
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
	a.log.Info("app", "msg", "init started", "mode", a.cfg.Mode.String())

	// Root RNG seed; resolved first, since services and systems both derive
	// from it. A drawn seed is logged so any run replays with -seed.
	if a.cfg.Seed == 0 {
		a.cfg.Seed = uint64(time.Now().UnixNano()) // [wall] once per process
	}
	a.log.Info("app", "msg", "seed resolved", "seed", a.cfg.Seed)

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
	// After the scenario, whose max_players bounds what the transport admits.
	if a.cfg.HostAddress != "" && a.cfg.networkConfig == nil {
		a.cfg.networkConfig = a.membership.hostNetworkConfig()
	}
	// A guest's own port takes joins beside its peer links: as the host once it
	// authors the session, a successor taking players where its session reaches it,
	// and as its -relay door would while its player relays for others.
	if c := a.cfg.networkConfig; c != nil && c.AcceptPeer != nil {
		c.AcceptSession, c.OnAdmit = a.membership.relayAcceptor(a.membership.admitHosted), a.membership.releaseRelayedJoiner
	}
	if err := a.initServices(); err != nil {
		return err
	}
	a.initWorld()
	if err := a.scribe.initJournal(); err != nil {
		return err
	}
	// After the journal opens, so a run that starts in a session notes it first.
	a.world.RunSafe(a.world.LatchSession)
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

	a.log.Info("app", "msg", "init completed",
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
	a.membership.world, a.scribe.world = a.world, a.world
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
		// A joiner is nobody until its seat names it (bindSeat), not the coordinator
		// a port with no identity otherwise stands for.
		if a.cfg.JoinAddress != "" && a.cfg.networkConfig != nil && a.cfg.networkConfig.ParticipantID == 0 {
			r.ParticipantID = 0
		}
		a.membership.bindSessionHooks(r)
		// A session endpoint exists, so this run is shared for its whole life whether
		// or not a peer is attached at a given tick. Latching here rather than
		// reading the port keeps the anchor, the D-14 verdict and the playout barrier
		// answering one question, which is what a reproduction adopts.
		a.world.MarkSessionShared()
	}

	// The terminal supplies color whenever one exists, but dimensions only when the
	// mode says so; a replay's come from the journal, via config
	width, height, colorMode, aspect := a.presentationGeometry(a.cfg.Width, a.cfg.Height)

	// GameContext initializes the remaining world resources.
	// A driven run uses the manual clock: game time is a pure function of ticks.
	if a.cfg.Mode.Driven() {
		a.ctx = engine.NewGameContextWithClock(a.world, width, height, engine.NewManualClock())
	} else {
		a.ctx = engine.NewGameContext(a.world, width, height)
	}
	a.membership.ctx, a.scribe.ctx = a.ctx, a.ctx
	a.ctx.BindLog(a.log)
	a.applyPlay()
	a.world.Resources.Config.ColorMode = colorMode
	a.world.Resources.Config.CellAspect = aspect

	// The floor applies before the latch and the boot script, which places the
	// first cursor from the map's size, or puts it on no cell at all
	cfg := a.world.Resources.Config
	cfg.MapMinWidth, cfg.MapMinHeight = a.scenario.MapMinWidth, a.scenario.MapMinHeight
	cfg.CursorsOffMap = a.scenario.CursorsOffMap
	if w, h := cfg.FloorMapSize(cfg.MapWidth, cfg.MapHeight); w != cfg.MapWidth || h != cfg.MapHeight {
		a.world.SetupLevel(w, h, false, cfg.CropOnResize, false)
	}
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
	content := service.MustGet[*service.ContentService](a.hub, "content")
	content.PublishStatus(a.world.Resources.Status)
	a.glue.open(a, a.world, a.log, content, &a.scenario)
	// The protocol is built here rather than before init because it reads the
	// telemetry cells and the status registry this world owns. Nothing can reach
	// it earlier: the transport hooks bound above answer nothing until the service
	// is started, which is after construction returns.
	a.corrections, a.authority, a.reach =
		converge.New(a.instance(), a.glue.telemetry, a.world.Resources.Status)
	a.membership.corrections, a.membership.authority, a.membership.reach = a.corrections, a.authority, a.reach
	if a.cfg.SlowPolicy != nil {
		a.authority.SetSlowPolicy(*a.cfg.SlowPolicy)
	}

	// Initial rate; ParseScale rejects "" so a bare run stays at real time
	if s, ok := engine.ParseScale(a.cfg.TimeScaleSpec); ok {
		a.ctx.StartScale = s
		a.ctx.TimeCtl.SetScale(s)
	}
	// The host can wait in the lobby much longer than a joiner. Freeze both clocks
	// before the FSM creates any game-time deadline, then release them only after
	// the common start gate. Otherwise the lobby wait ages every absolute deadline
	// before tick one.
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

// applyPlay starts the run as its [play] says; a purge returns to these values.
func (a *App) applyPlay() {
	p := a.cfg.Play
	if i := slices.Index(engine.AutoFireWords[:], p.AutoFire); i >= 0 {
		a.ctx.StartAutoFire = uint32(i)
	}
	a.ctx.StartMouseFree = p.FollowMouse
	a.ctx.AutoFire.Store(a.ctx.StartAutoFire)
	a.ctx.MouseFreeMode.Store(p.FollowMouse)
	a.ctx.MouseDisabled.Store(!p.Mouse)
	a.ctx.OverlayHUD.Store(p.HUD)
	a.ctx.Relaying.Store(p.Relay)
	a.ctx.WaitStart.Store(p.Wait && a.cfg.Mode.OwnsInput())
}

// livePlay is the run's [play] as it stands: what the next run starts from and
// what the settings file is kept at.
func (a *App) livePlay() paths.Play {
	p := a.cfg.Play
	p.AutoFire = engine.AutoFireWords[min(a.ctx.AutoFire.Load(), engine.AutoFireBoth)]
	p.Mouse = !a.ctx.MouseDisabled.Load()
	p.FollowMouse = a.ctx.MouseFreeMode.Load()
	p.HUD = a.ctx.OverlayHUD.Load()
	p.Relay = a.ctx.Relaying.Load()
	if a.cfg.Mode.OwnsInput() { // a run nobody plays never waits, whatever the setting
		p.Wait = a.ctx.WaitStart.Load()
	}
	a.audioPlay(&p)
	return p
}

// persistPlay writes back each [play] value the player changed; a write that
// fails is reported once and the settings left alone.
func (a *App) persistPlay() {
	if a.cfg.Settings == nil {
		return
	}
	p, was := a.livePlay(), a.cfg.Play
	if p == was {
		return
	}
	a.cfg.Play = p
	if err := paths.SavePlay(a.cfg.Settings, was, p); err != nil {
		a.settingsUnsaved(err)
	}
}

// settingsUnsaved reports a write back that failed, once: the run writes no more
func (a *App) settingsUnsaved(err error) {
	a.log.Warn("app", "msg", "settings not saved", "error", err.Error())
	a.ctx.SetStatusMessage("Settings not saved: "+err.Error(), parameter.StatusAlert)
	a.cfg.Settings = nil
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
	a.world.SetupLevel(a.cfg.MapWidth, a.cfg.MapHeight, false, a.cfg.CropOnResize, false)
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
		a.router.SetKeyReportApplier(a.keyReportApplier())
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
	a.glue.scheduler, a.membership.scheduler, a.scribe.scheduler = a.scheduler, a.scheduler, a.scheduler
	a.scheduler.SetJournalDigest(a.scribe.journalDigestLocked)

	if err := a.scheduler.LoadScenarioFromFS(
		a.scenario.FS(), a.scenario.Entry(), manifest.RegisterFSMComponents); err != nil {
		return fmt.Errorf("load scenario %s: %w", a.scenario.Name, err)
	}

	// MetaSystem needs the completed GameContext and therefore joins here. Its
	// profile remains in manifest.ContextSystems for the ordinary validation and
	// fingerprint checks.
	meta := system.NewMetaSystem(a.ctx)
	a.world.AddSystem(meta, manifest.ProfileFor(meta.Name()))

	// The declared dependency graph is checked once the set is complete; an
	// unknown name or a cycle is a startup error, not a runtime degradation
	if err := a.world.CheckSystemDependencies(); err != nil {
		return fmt.Errorf("system dependencies: %w", err)
	}

	for _, sys := range a.world.Systems() {
		if h, ok := sys.(event.Handler); ok {
			a.scheduler.RegisterEventHandler(h)
		}
	}
	return nil
}

// Close stops the scheduler before the services it depends on
// Safe on a partially constructed App
func (a *App) Close() {
	a.log.Info("app", "msg", "shutdown started")
	begin := time.Now()
	a.membership.closeSeats(false)
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	a.membership.leaveClaim()
	if a.membership.pendingJoin != nil {
		_ = a.membership.pendingJoin.Close()
	}
	if a.corrections != nil {
		a.corrections.Close()
	}
	a.reach.Close()
	a.closeProbe()
	a.membership.closeMidRunPort()
	a.glue.discardStagingWorld()
	a.hub.StopAll()
	a.scribe.closedAtExit, a.scribe.closedAtExitAs = a.scribe.closeJournal()

	a.log.Info("app", "msg", "shutdown completed", "us", time.Since(begin).Microseconds())
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
	if err := a.membership.playersFit(sc); err != nil {
		return err
	}
	a.scenario = sc
	a.log.Info("app", "msg", "scenario loaded", "name", sc.Name, "digest", sc.Short(), "files", sc.Files())
	return nil
}
