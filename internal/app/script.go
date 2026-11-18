package app

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/journal"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// ScriptPaceMax is the -speed token that removes wall pacing from a script run.
const ScriptPaceMax = "max"

// scriptPacing resolves whether a run's wall pace applies; a paced tick occupies the
// game interval at the rate the run's clock holds, which -speed sets alone and the
// host's in a session. An empty spec takes the default from the run rather than the
// flags — a script that starts in a session is paced from its first tick, a solo one
// runs flat out until it opens a session with :host. Bots seat in a session.
func scriptPacing(cfg Config) (paced bool, err error) {
	switch {
	case cfg.TimeScaleSpec == "":
		return cfg.Membership.HostAddress != "" || cfg.Membership.JoinAddress != "" || len(cfg.Membership.Bots) > 0, nil
	case cfg.TimeScaleSpec == ScriptPaceMax:
		return false, nil
	}
	if _, ok := engine.ParseScale(cfg.TimeScaleSpec); !ok {
		return false, fmt.Errorf(
			"-speed %q is not a ladder rate (1/8 1/4 1/2 1 2 4 8) or %q", cfg.TimeScaleSpec, ScriptPaceMax)
	}
	return true, nil
}

// RunScript loads an authored deterministic script and runs it. A host/join
// configuration uses the ordinary session handshake; ModeScript presents the run
// on the platform's Host, as Run does, and every other mode runs it headlessly.
func RunScript(cfg Config, host Host, path string) (journal.ScriptStats, error) {
	script, err := journal.LoadScript(path)
	if err != nil {
		return journal.ScriptStats{}, err
	}
	if script.Width != 0 {
		cfg.Width, cfg.Height = script.Width, script.Height
	}
	var driver *journal.ScriptDriver
	finished, err := runDriven(runConfig{Config: cfg, host: host}, "script", path, func(a *App) (pacedSource, error) {
		d, err := journal.NewScriptDriver(scriptTarget{a: a}, script)
		if err != nil {
			return nil, err
		}
		if cfg.Membership.HostAddress != "" || cfg.Membership.JoinAddress != "" {
			if err := d.Live(); err != nil {
				return nil, err
			}
		}
		driver = d
		return scriptSource{d}, nil
	})
	if driver == nil {
		return journal.ScriptStats{}, err
	}
	stats := driver.Stats()
	if finished {
		vlog.Info("app", "msg", "script completed",
			"path", path, "actions", stats.Executed, "ticks", stats.Ticks,
			"run", stats.End.Run, "tick", stats.End.Tick)
	}
	return stats, err
}

// RunBot plays this instance's own seat with a bot graph: a name or a path, which
// cfg.Resources and its scenario resolve, or none for the scenario's own bot. It is
// otherwise a script run, and presented, it keeps real time unless -speed says
// otherwise. It reports its counters however it stops, summed over the games a
// learning run played (BotRun).
func RunBot(cfg Config, host Host, spec string) (bot.Stats, error) {
	graph, games, err := botRun(cfg, spec)
	if err != nil {
		return bot.Stats{}, err
	}
	name := spec
	if cfg.Mode == ModeScript && cfg.TimeScaleSpec == "" {
		cfg.TimeScaleSpec = "1"
	}
	if graph != nil {
		name, cfg.Play = graph.Name, botPlay(cfg.Play, graph)
	}
	run := runConfig{Config: cfg, host: host}
	var (
		driver   *bot.Driver
		learners []*bot.Learner
		played   *App
		beside   *sideGames
	)
	quit, err := runDriven(run, "bot", name, func(a *App) (pacedSource, error) {
		if graph == nil {
			// The session joined carries the scenario, and so its bots
			var err error
			if graph, err = membership.LoadBotGraph(a.cfg.Resources, a.scenario, spec); err != nil {
				return nil, err
			}
			name = graph.Name
			a.world.RunSafe(func() { a.cfg.Play = botPlay(a.cfg.Play, graph) })
			a.applyPlay()
		}
		d, err := bot.NewDriver(a, a.ctx, graph, a.Seed(), a.membership.LocalParticipant())
		driver, played = d, a
		if err == nil {
			beside, err = a.playBeside(graph, games-1)
		}
		if err == nil {
			learners, err = a.learn(graph, append([]*bot.Driver{d}, beside.drivers...)...)
		}
		if err == nil {
			beside.start()
		}
		return botSource{d}, err
	})
	if driver == nil {
		return bot.Stats{}, err
	}
	// Where the graph ended the run, the games beside play to the tick its own
	// reached, so what the run kept follows from its seed
	end := uint64(0)
	if quit {
		end = driver.Stats().Ticks
	}
	driver.End(end)
	if beside != nil {
		beside.done.Wait()
	}
	for _, l := range learners {
		err = errors.Join(err, l.Close())
	}
	// Read once every game has stopped and the learners have closed, so the run
	// reports what its files hold
	stats := driver.Stats()
	stats.Games = games
	err = errors.Join(err, beside.close(&stats))
	reg := played.world.Resources.Status
	played.log.Info("app", "msg", "bot stopped", "name", name, "quit", quit, "games", games, "ticks", stats.Ticks,
		"injected", stats.Injected, "dropped", stats.Dropped, "fitness", stats.Fitness, "state", stats.State,
		"rounds", stats.Rounds, "score", stats.Score,
		"typed", reg.Ints.Get("typing.correct").Load(), "errors", reg.Ints.Get("typing.errors").Load())
	return stats, err
}

// BotRun names the graph RunBot plays for spec and how many games it plays it in
// at once; no name for a run that joins, whose session's scenario resolves it
func BotRun(cfg Config, spec string) (name string, games int, err error) {
	graph, games, err := botRun(cfg, spec)
	if graph != nil {
		name = graph.Name
	}
	return name, games, err
}

// botRun resolves the graph a bot's run plays against cfg's scenario, and its count
// of games. A joining run's graph is nil: the session it joins carries its
// scenario, which may be what names the bot, so it resolves once joined.
func botRun(cfg Config, spec string) (*bot.Graph, int, error) {
	var graph *bot.Graph
	if cfg.Membership.JoinAddress == "" {
		sc, err := resource.LoadScenario(cfg.Resources)
		if err != nil {
			return nil, 0, fmt.Errorf("scenario: %w", err)
		}
		if graph, err = membership.LoadBotGraph(cfg.Resources, sc, spec); err != nil {
			return nil, 0, err
		}
	}
	games, err := botGames(cfg, graph != nil && graph.Settings.Learn)
	return graph, games, err
}

// botGames is how many games a bot's run plays at once: one, but for a headless
// run alone and flat out whose graph learns, which plays cfg.Games, all teaching
// the graph's learners. Where it names none that is one more than the cores,
// as a game waiting its turn to close a round leaves its core to another.
func botGames(cfg Config, learns bool) (int, error) {
	paced, err := scriptPacing(cfg)
	if err != nil {
		return 0, err
	}
	m := cfg.Membership
	switch {
	case !learns || cfg.Mode == ModeScript || paced || m.HostAddress != "" || m.JoinAddress != "" || len(m.Bots) > 0:
		if cfg.Games > 1 {
			return 0, fmt.Errorf("-games %d: only a -headless bot alone and flat out whose graph learns plays more than one game", cfg.Games)
		}
		return 1, nil
	case cfg.Games > 0:
		return cfg.Games, nil
	}
	return runtime.GOMAXPROCS(0) + 1, nil
}

// sideGames are the games a learning run plays beside its own, each a headless run
// of its own on a seed drawn from the run's, stepped flat out on its own goroutine
// until the run's driver ends the learning or its own graph quits
type sideGames struct {
	apps    []*App
	drivers []*bot.Driver
	done    sync.WaitGroup
	mu      sync.Mutex
	err     error
}

// playBeside builds n further games of this run's graph, none yet started
func (a *App) playBeside(g *bot.Graph, n int) (*sideGames, error) {
	s := &sideGames{}
	for i := 1; i <= n; i++ {
		cfg := a.cfg.Config
		cfg.Seed = vmath.DeriveSeed(a.Seed(), "bot.game."+strconv.Itoa(i))
		cfg.Membership, cfg.Journal, cfg.JournalSink = Membership{}, false, nil
		cfg.StatTicks, cfg.RecTicks, cfg.Settings, cfg.Notice = -1, -1, nil, ""
		game, err := newDriven(runConfig{Config: cfg, wiring: wiring{log: vlog.NewLog("game " + strconv.Itoa(i+1)), scriptedSession: true}})
		if err != nil {
			return nil, errors.Join(err, s.close(nil))
		}
		s.apps = append(s.apps, game)
		d, err := bot.NewDriver(game, game.ctx, g, game.Seed(), game.membership.LocalParticipant())
		if err != nil {
			return nil, errors.Join(err, s.close(nil))
		}
		s.drivers = append(s.drivers, d)
	}
	return s, nil
}

// start steps each game for as long as its driver has more to play
func (s *sideGames) start() {
	for _, d := range s.drivers {
		s.done.Add(1)
		core.Go(func() {
			defer s.done.Done()
			for {
				if more, err := d.Step(); err != nil || !more {
					s.mu.Lock()
					s.err = errors.Join(s.err, err)
					s.mu.Unlock()
					return
				}
			}
		})
	}
}

// close waits for the games to stop, which the run's driver ending the learning
// has them do, closes them and adds what they played to st
func (s *sideGames) close(st *bot.Stats) error {
	if s == nil {
		return nil
	}
	s.done.Wait()
	for i, game := range s.apps {
		if i < len(s.drivers) && st != nil {
			played := s.drivers[i].Stats()
			st.Ticks, st.Injected, st.Dropped = st.Ticks+played.Ticks, st.Injected+played.Injected, st.Dropped+played.Dropped
			st.Fitness, st.Rounds, st.Score = st.Fitness+played.Fitness, st.Rounds+played.Rounds, st.Score+played.Score
		}
		game.Close()
	}
	return s.err
}

// botPlay is the [play] g's run starts on: the shipped pointer and the auto-fire
// g's [bot] names, else the shipped one, so what a graph scores is its own and not
// its player's settings'
func botPlay(play paths.Play, g *bot.Graph) paths.Play {
	shipped := paths.DefaultPlay()
	if play.AutoFire == "" {
		play = shipped
	}
	play.AutoFire, play.Mouse, play.FollowMouse = shipped.AutoFire, shipped.Mouse, shipped.FollowMouse
	if g.Settings.AutoFire != "" {
		play.AutoFire = g.Settings.AutoFire
	}
	return play
}

// learn has drivers, this run's own first, each play a game of what this player's
// runs have learned for g in this run's scenario, a population a network in the
// user root, and learn on while the run is in no session, whose rounds are not
// its bot's alone. None for a graph that does not learn.
func (a *App) learn(g *bot.Graph, drivers ...*bot.Driver) ([]*bot.Learner, error) {
	if !g.Settings.Learn {
		return nil, nil
	}
	var learners []*bot.Learner
	for _, network := range g.Networks() {
		var store bot.Store
		if file := resource.PopulationFile(g.Name, a.scenario.Shown(), network); file != "" {
			store = paths.PlaceFile{Path: file}
		}
		l, err := bot.NewLearner(g, network, a.Seed(), len(drivers), store)
		if err != nil {
			return nil, err
		}
		l.Saved = func(st bot.LearnStats) {
			a.log.Info("app", "msg", "bot population kept", "name", g.Name, "network", st.Network, "games", len(drivers),
				"rounds", st.Rounds, "recent", st.Recent, "step", st.Step, "fielded", st.Fielded, "fielded_rounds", st.FieldedRounds)
		}
		l.Lost = func(err error) {
			a.log.Warn("app", "msg", "bot population not kept", "name", g.Name, "error", err.Error())
			a.ctx.SetStatusMessage("Bot "+g.Name+": "+err.Error(), parameter.StatusAlert)
		}
		learners = append(learners, l)
	}
	// Games beside its own are played only by a run alone, which a headless one stays
	bot.Learn(func() bool { return len(drivers) > 1 || a.membership.SessionTransport() == nil }, learners, drivers...)
	// A run somebody watches says what each round came to as it closes
	if d := drivers[0]; a.cfg.Mode.Presents() {
		d.Round = func(n uint64, score float64) {
			a.ctx.SetStatusMessage(fmt.Sprintf("Round %d scored %.0f%s", n, score, learnLine(d.Stats())), parameter.StatusNotice)
		}
	}
	return learners, nil
}

// learnLine says how far each of a bot's learners is, a bar's part a network
func learnLine(st bot.Stats) (line string) {
	for _, l := range st.Learn {
		line += fmt.Sprintf(" | %s recent %.0f, fielding %.0f over %d", l.Network, l.Recent, l.Fielded, l.FieldedRounds)
	}
	return line
}

// runDriven plays one driven participant, a script or a bot, until it finishes or
// the process is signalled.
func runDriven(cfg runConfig, kind, name string, build func(*App) (pacedSource, error)) (finished bool, err error) {
	signals, stopSignals := notifySignals()
	defer stopSignals()
	return drive(cfg, kind, name, signals, nil, build)
}

// drive builds the App in the session cfg names, lets build put a driver on it, and
// steps that driver headless under the run's wall pace, or presented. finished is
// false when stop or a cancelled session ended the run first. While hold reports
// true, or a hold stands, the run stands still and re-anchors its pace after.
func drive(cfg runConfig, kind, name string, signals <-chan os.Signal, hold func() bool,
	build func(*App) (pacedSource, error)) (finished bool, err error) {
	if cfg.Mode != ModeScript {
		cfg.Mode = ModeHeadless
	}
	cfg.wiring.scriptedSession = true
	paced, err := scriptPacing(cfg.Config)
	if err != nil {
		return false, err
	}

	a, err := newScriptApp(cfg, signals)
	if err != nil {
		if errors.Is(err, membership.ErrSessionCanceled) {
			return false, nil
		}
		return false, err
	}
	defer func() {
		if r := recover(); r != nil {
			core.HandleCrash(r)
		}
		a.Close()
	}()

	src, err := build(a)
	if err != nil {
		return false, err
	}
	// cfg, not a.cfg: once build hands the App out, its config is read under its lock
	if cfg.Mode == ModeScript {
		return true, runPresented(a, src, kind, name, paced, signals)
	}
	// A solo run that opens a session, with :host or for its bots, gains a peer to
	// keep step with, so pacing engages at that moment. The clock is re-anchored there
	// rather than carried forward, or the ticks it ran flat out would be a debt the
	// pacing immediately spends. An explicit -speed max is honoured either way.
	autoPace := cfg.TimeScaleSpec == ""
	nextTick := time.Now() // [wall] pacing only
	for {
		select {
		case <-signals:
			return false, nil
		default:
		}
		// A headless bot left with no session plays for nobody; a watched one plays
		// on, as a person does, and a seat ends as its holder says (seatEnds).
		if a.membership.Dismissed() || kind == "bot" && a.cfg.wiring.membership.Holder == 0 && membership.ForkCell(a.world).Load() {
			return false, nil
		}
		// Between two steps, as a presenting run's loop serves it between two waits
		a.scribe.ServeJournal()
		// A held run stands still as its holder's pause does, but still drains its
		// link: the host's resume is the next epoch it sends.
		if held := a.ctx.TimeCtl.Held(); held || hold != nil && hold() {
			if held {
				a.ApplyPendingCorrections()
			}
			if !membership.WaitScriptTick(signals, parameter.PausedPollInterval) {
				return false, nil
			}
			nextTick = time.Now() // [wall]
			continue
		}

		more, err := src.Step()
		if err != nil {
			return false, err
		}
		if !more {
			return true, nil
		}
		if !paced && autoPace && a.membership.SessionTransport() != nil {
			paced, nextTick = true, time.Now() // [wall]
			a.log.Info("app", "msg", kind+" pacing engaged",
				"address", a.HostAddr(), "tick", a.Position().Tick)
		}
		if paced {
			trim, step := a.scheduler.TakePace()
			tick := a.ctx.TimeCtl.Scale().Wall(a.ctx.World.TickInterval())
			nextTick = nextTick.Add(engine.PacedInterval(tick, trim, step))
			if !membership.WaitScriptTick(signals, time.Until(nextTick)) {
				return false, nil
			}
		}
	}
}

// pacedSource is the driven stream a run advances. Each driver reports its own
// counters, because "how far through" means a different thing to a record stream,
// an action list and a graph.
type pacedSource interface {
	Step() (bool, error)
	progress() string
}

type scriptSource struct{ d *journal.ScriptDriver }

func (s scriptSource) Step() (bool, error) { return s.d.Step() }

func (s scriptSource) progress() string {
	st := s.d.Stats()
	return fmt.Sprintf("run %d tick %d | %d/%d act", st.End.Run, st.End.Tick, st.Executed, st.Actions)
}

type botSource struct{ d *bot.Driver }

func (s botSource) Step() (bool, error) { return s.d.Step() }

func (s botSource) progress() string {
	st := s.d.Stats()
	line := fmt.Sprintf("tick %d | %s | %d intents, %d dropped", st.Ticks, st.State, st.Injected, st.Dropped)
	if len(st.Learn) > 0 {
		line += fmt.Sprintf(" | round %d%s", st.Rounds+1, learnLine(st))
	}
	return line
}

// screen is a terminal's size in cells.
type screen struct{ w, h int }

// seatSource ends a seat once its session is gone for it (seatEnds): a holder left
// with no session takes its bots along. A seat follows its holder's screen as it is
// published, between two of its steps.
type seatSource struct {
	botSource
	ends   []*atomic.Bool
	screen *atomic.Pointer[screen]
	run    *App
	shown  screen
}

func (s *seatSource) Step() (bool, error) {
	for _, gone := range s.ends {
		if gone.Load() {
			return false, nil
		}
	}
	if sc := s.screen.Load(); sc != nil && *sc != s.shown {
		s.shown = *sc
		s.run.Resize(sc.w, sc.h)
	}
	return s.botSource.Step()
}

// newScriptApp starts the same tick-zero gate as interactive play, but leaves the
// scheduler caller-driven after the roster is closed.
func newScriptApp(cfg runConfig, signals <-chan os.Signal) (*App, error) {
	a, err := newSessionApp(cfg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*App, error) {
		a.Close()
		return nil, err
	}
	if a.membership.HasPendingJoin() {
		if err := a.pollTerminalEarly(); err != nil {
			return fail(err)
		}
		if err := a.membership.StartJoinSession(signals); err != nil {
			return fail(err)
		}
	}
	if err := a.hub.StartAll(); err != nil {
		return fail(err)
	}
	if a.cfg.Membership.HostAddress != "" {
		// Before the lobby, which the host's own bots may be all of.
		if err := a.membership.SeatBots(); err != nil {
			return fail(err)
		}
		if err := a.membership.StartHostSession(signals); err != nil {
			return fail(err)
		}
	}
	if a.cfg.Membership.HostAddress != "" || a.cfg.Membership.JoinAddress != "" {
		a.membership.ActivateNetworkSession()
		if err := a.membership.ResumeJoinedSession(); err != nil {
			return fail(err)
		}
		a.ctx.TimeCtl.SetPaused(false)
	}
	a.scheduler.Prepare()
	// A scripted host is a host: its lobby has closed, so a dial from here is a
	// mid-run join and a guest that dropped comes back through the same gate. The
	// clock its capture waits on is the script's own, advanced by the caller that is
	// about to start stepping. On a run that hosts nothing this is inert until a
	// later :host opens a session.
	a.membership.OpenMidRunJoins()
	if a.cfg.Membership.HostAddress == "" {
		if err := a.membership.SeatBots(); err != nil {
			return fail(err)
		}
	}
	return a, nil
}

type scriptTarget struct{ a *App }

func (t scriptTarget) Position() event.Stamp        { return t.a.Position() }
func (t scriptTarget) Declared() component.Declared { return t.a.behaviors.Declared() }
func (t scriptTarget) Tick(n int)                   { t.a.Tick(n) }
func (t scriptTarget) Inject(intents ...*input.Intent) bool {
	return t.a.Inject(intents...)
}
func (t scriptTarget) Emit(et event.EventType, payload any, domain core.Domain) {
	// A script's own hold would stand for good: its driven loop takes no step while
	// the run is held, so no action of its timeline could end it.
	if p, ok := payload.(*event.GamePausePayload); ok && et == event.EventGameHoldRequest && p.Paused {
		t.a.log.Warn("script", "msg", "a script cannot hold its own run; the hold request was refused")
		return
	}
	t.a.ctx.PushEventFull(et, payload, event.OriginDebug, domain)
	t.a.Settle()
}
