package app

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/vlog"
)

// DefaultBotGraph is the graph a bot seat plays when none is named.
const DefaultBotGraph = "default"

// BotSpecs is shared by CLI flags and :bot add; empty or a zero count means no request.
func BotSpecs(spec string) ([]string, error) {
	if spec == "" {
		return nil, nil
	}
	count, graph, counted := strings.Cut(spec, ":")
	n, err := strconv.Atoi(count)
	if err != nil && !counted {
		return []string{spec}, nil
	}
	if err != nil || n < 0 || n > parameter.MaxPlayers {
		return nil, fmt.Errorf("bots %q: use a graph or N[:graph], with N at most %d", spec, parameter.MaxPlayers)
	}
	return slices.Repeat([]string{cmp.Or(graph, DefaultBotGraph)}, n), nil
}

// seatJoin is every seat's join address, which makes its run a joining one. It is
// never dialled: the seat's stream is one its holder opens in-process (seatLink).
const seatJoin = "holder:0"

// seat is one bot this run holds: a headless instance of its own that joins the
// run's session as any participant does, over an in-process stream, and plays a
// graph. The session carries holder identity; graph and lifecycle stay with this
// process.
type seat struct {
	number uint64
	spec   string
	graph  *bot.Graph
	stop   chan os.Signal // closed to halt it; drive reads a closed channel as a signal
	done   chan struct{}
	once   sync.Once
	id     atomic.Uint32 // the participant it was admitted as, zero until then
	slot   atomic.Uint32
	app    atomic.Pointer[App] // the seat's own run, nil until seated and once it leaves
}

func (s *seat) halt() { s.once.Do(func() { close(s.stop) }) }

// halted reports whether the seat was told to leave
func (s *seat) halted() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// seatHalted reports whether participant id is a seat this run told to leave, so
// its link closing is the drop that was asked for. A seat forgets itself before
// its link closes, so the drop is remembered by identity until then.
func (a *App) seatHalted(id uint32) bool {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	i := slices.Index(a.dropped, id)
	if i >= 0 {
		a.dropped = slices.Delete(a.dropped, i, i+1)
	}
	return i >= 0
}

// seatBots seats cfg.Bots in the session this run is in, or in one it opens for them.
func (a *App) seatBots() error {
	if len(a.cfg.Bots) == 0 {
		return nil
	}
	if a.sessionTransport() == nil {
		a.Settle() // Bind the boot cursor before opening the bots' session.
	}
	var err error
	a.world.RunSafe(func() {
		for _, spec := range a.cfg.Bots {
			if err = a.addSeatLocked(spec); err != nil {
				return
			}
		}
	})
	return err
}

// addSeatLocked seats one bot playing spec. The graph is parsed here, so a name that
// does not resolve is the caller's error; the join runs on the seat's goroutine.
// Caller MUST hold updateMutex.
func (a *App) addSeatLocked(spec string) error {
	graph, err := loadBotGraph(a.cfg.Resources, spec)
	if err != nil {
		return err
	}
	cfg, err := a.seatConfigLocked()
	if err != nil {
		return err
	}
	// A holder whose clock stops when paused stops its seats with it; a driven one
	// steps through a pause, and so do they. A hold reaches them as the crossing it
	// is, at the tick every participant stops at.
	var hold func() bool
	if !a.cfg.Mode.Driven() {
		hold = a.ctx.TimeCtl.OwnPause
	}
	s := &seat{spec: spec, graph: graph, stop: make(chan os.Signal), done: make(chan struct{})}
	a.seatsMu.Lock()
	a.seatSerial++
	s.number = a.seatSerial
	a.seats = append(a.seats, s)
	a.seatsMu.Unlock()
	cfg.log = vlog.NewLog("seat " + strconv.FormatUint(s.number, 10))
	core.Go(func() { a.playSeat(s, cfg, hold) })
	return nil
}

// seatConfigLocked is a new seat's run, which joins this run's session in-process; a
// run in none first opens one with no listener. It sends the session's name, which
// the handshake its holder serves checks. Caller holds updateMutex.
func (a *App) seatConfigLocked() (Config, error) {
	if a.sessionTransportLocked() == nil {
		if err := a.openSessionLocked(nil); err != nil {
			return Config{}, err
		}
	}
	width, height := a.seatScreenLocked()
	return Config{Mode: ModeHeadless, Resources: a.cfg.Resources, Width: width, Height: height,
		NoAdvertise: true, StatTicks: -1, RecTicks: -1, JoinAddress: seatJoin, SessionName: a.cfg.SessionName,
		holder: network.PeerID(a.world.LocalParticipant()), relay: a.seatLink, claims: a.claims}, nil
}

// seatLink opens a stream to this run for one of its bots and serves the join
// handshake on the near end: the coordinator's, or a still-guest's, which asks the
// authority on its own link for each answer.
func (a *App) seatLink() (net.Conn, error) {
	var (
		port     *network.SocketPort
		accept   func(net.Conn) (network.PeerID, error)
		admitted func(network.PeerID)
	)
	a.world.RunSafe(func() {
		port, _ = a.sessionTransportLocked().(*network.SocketPort)
		if a.world.IsSessionCoordinator() {
			accept, admitted = a.sessionAcceptor(), a.admitLateJoiner
		} else {
			accept, admitted = a.relayAcceptor(network.PeerID(a.world.LocalParticipant())), a.releaseRelayedJoiner
		}
	})
	if port == nil {
		return nil, errors.New("the holder is in no session")
	}
	near, far := network.Pipe()
	if err := port.ServeConn(near, accept, admitted); err != nil {
		_ = far.Close()
		return nil, err
	}
	return far, nil
}

// seatScreenLocked is the screen a new seat plays in: its holder's, so a person's bots
// see what that person sees, or a bot's own for a holder that names none, a server or
// headless run given no -size. Caller holds updateMutex.
func (a *App) seatScreenLocked() (int, int) {
	if a.cfg.geometryDefaulted || !engine.ViewportFits(a.ctx.Width, a.ctx.Height) {
		return BotWidth, BotHeight
	}
	return a.ctx.Width, a.ctx.Height
}

// playSeat runs one seat until its holder halts it, its graph quits, or the session
// ends for it, standing still while hold reports true. A join refused while the
// session starts or hands off is retried, as the refusal asks; any other failure
// ends it.
func (a *App) playSeat(s *seat, cfg Config, hold func() bool) {
	defer close(s.done)
	defer a.forgetSeat(s)
	deadline := time.Now().Add(parameter.SessionRejoinWindow) // [wall] a link bound
	for {
		var (
			driver *bot.Driver
			run    *App
		)
		finished, err := drive(cfg, "bot", s.graph.Name, s.stop, hold,
			func(b *App) (pacedSource, error) {
				run = b
				d, err := bot.NewDriver(b, b.ctx, s.graph, b.Seed(), b.localParticipant())
				if err != nil {
					return nil, err
				}
				driver = d
				// The offer rather than the world: the cursor binds a tick later.
				b.sessionMu.Lock()
				entry, _ := b.sessionOffer.Entry(b.sessionOffer.Assigned)
				b.sessionMu.Unlock()
				s.slot.Store(uint32(entry.Slot))
				s.id.Store(uint32(entry.ID))
				s.app.Store(b)
				a.log.Info("app", "msg", "bot seated", "graph", s.graph.Name,
					"peer", s.id.Load(), "slot", s.slot.Load())
				return &seatSource{botSource: botSource{d}, ends: a.seatEnds(b), screen: &a.seatScreen,
					run: b, shown: screen{cfg.Width, cfg.Height}}, nil
			})
		// Under seatsMu, so an ask lands before this or finds no run
		a.seatsMu.Lock()
		s.app.Store(nil)
		a.seatsMu.Unlock()
		if run != nil {
			a.seatJournalLeft(int(s.slot.Load()), run)
		}
		if driver == nil && err != nil && network.RetryOf(err) == network.RetrySoon && time.Now().Before(deadline) &&
			waitScriptTick(s.stop, parameter.SessionRejoinInterval) {
			continue
		}
		switch {
		case driver == nil && err != nil:
			a.log.Warn("app", "msg", "bot not seated", "graph", s.graph.Name, "error", err.Error())
			a.ctx.SetStatusMessage("Bot "+s.graph.Name+": "+err.Error(), parameter.StatusAlert)
		case driver != nil:
			st := driver.Stats()
			a.log.Info("app", "msg", "bot left", "graph", s.graph.Name, "peer", s.id.Load(),
				"finished", finished, "ticks", st.Ticks, "injected", st.Injected, "dropped", st.Dropped,
				"fitness", st.Fitness, "state", st.State, "error", fmt.Sprint(err))
		}
		return
	}
}

// forkCell is an instance's own "no session left" flag: it lost its authority and
// nobody took the session over.
func forkCell(a *App) *atomic.Bool { return a.world.Resources.Status.Bools.Get("network.fork") }

// seatEnds is what ends seat, one of this run's, besides this run halting it: the
// seat's fork or this run's. A play run's loop takes its fork into a session of its
// own and seats its bots again there (forked), so nothing but the run ends its seats.
func (a *App) seatEnds(seat *App) []*atomic.Bool {
	if a.cfg.Mode == ModePlay {
		return nil
	}
	return []*atomic.Bool{forkCell(seat), forkCell(a)}
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

func (a *App) forgetSeat(s *seat) {
	a.seatsMu.Lock()
	a.seats = slices.DeleteFunc(a.seats, func(o *seat) bool { return o == s })
	a.seatsMu.Unlock()
}

// haltSeats halts every seat without waiting and returns them. Each link closing is
// then the drop that was asked for, as it is for a seat dropped alone, unless the
// seats keep their places: then it is a lost link, whose cursor the session keeps
// as a Wraith.
func (a *App) haltSeats(keep bool) []*seat {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	seats := slices.Clone(a.seats)
	for _, s := range seats {
		if id := s.id.Load(); id != 0 && !s.halted() && !keep {
			a.dropped = append(a.dropped, id)
		}
		s.halt()
	}
	return seats
}

// closeSeats halts every seat, keeping their places or not, and waits for each to
// leave, so a holder's bots depart the session before it does.
func (a *App) closeSeats(keep bool) {
	for _, s := range a.haltSeats(keep) {
		<-s.done
	}
}

// dropSeat halts the seat on slot; it leaves as any participant does.
func (a *App) dropSeat(slot int) error {
	if a.stopSeat(func(s *seat) bool { return s.id.Load() != 0 && int(s.slot.Load()) == slot }) {
		return nil
	}
	return fmt.Errorf("no bot of this run's is on slot %X", slot)
}

func (a *App) removeSeat(id uint64) error {
	if a.stopSeat(func(s *seat) bool { return s.number == id }) {
		return nil
	}
	return fmt.Errorf("this bot has already left")
}

func (a *App) stopSeat(match func(*seat) bool) bool {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	for _, s := range a.seats {
		if match(s) {
			if id := s.id.Load(); id != 0 {
				a.dropped = append(a.dropped, id)
			}
			s.halt()
			return true
		}
	}
	return false
}

// askSeatJournal asks the seat on slot to open or close its journal, which its own
// loop serves between two of its steps; the outcome reaches this run's status line.
// A seat is a run of its own, so it journals its own player domain, which no other
// instance can: another participant's never leaves its machine.
func (a *App) askSeatJournal(slot int, start bool) error {
	// Held to the ask, so a seat leaving takes it up or this finds no run
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	var b *App
	leaving := false
	for _, s := range a.seats {
		if run := s.app.Load(); run != nil && int(s.slot.Load()) == slot {
			// A leaving seat's loop serves no more asks, and a live one may share its slot
			if !s.halted() {
				b = run
				break
			}
			leaving = true
		}
	}
	switch {
	case b == nil && leaving && start:
		return fmt.Errorf("the bot on slot %X is leaving", slot)
	case b == nil && leaving:
		return fmt.Errorf("the bot on slot %X is leaving; its journal closes with it", slot)
	case b == nil:
		return fmt.Errorf("no bot of this run's is on slot %X", slot)
	case start && b.recorder.Load() != nil && b.journalAsk.Load() == nil:
		return fmt.Errorf("slot %X already journals to %s", slot, b.recorder.Load().Path())
	case !start && b.cancelJournalStart():
		return nil
	case !start && b.recorder.Load() == nil:
		return fmt.Errorf("slot %X keeps no journal", slot)
	}
	report := func(path string, err error) { a.reportSeatJournal(slot, start, path, err) }
	if !b.queueJournalAsk(&journalAsk{start: start, report: report}) {
		return fmt.Errorf("slot %X is already opening or closing a journal", slot)
	}
	return nil
}

// seatJournalLeft tells this run's player what became of a seat's journal as its run
// closed, which only the seat's Close saw: the journal it shut, or a start its loop
// never served. Called on the seat's goroutine once its run has closed.
func (a *App) seatJournalLeft(slot int, run *App) {
	if ask := run.journalAsk.Swap(nil).opening(); ask != nil {
		ask.report("", errors.New("the bot left before it opened"))
	}
	switch {
	case run.closedAtExitAs != nil:
		a.log.Warn("journal", "msg", "seat journal not closed", "slot", slot, "error", run.closedAtExitAs.Error())
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X left; its journal %s", slot, run.closedAtExitAs), parameter.StatusAlert)
	case run.closedAtExit != "":
		a.log.Info("journal", "msg", "seat journal closed", "slot", slot, "path", run.closedAtExit, "reason", "bot left")
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X left; its journal closed: %s", slot, run.closedAtExit), parameter.StatusNotice)
	}
}

// reportSeatJournal tells this run's player how a seat's journal start or stop went.
// It runs on the seat's goroutine, or the holder's for a start a stop cancelled; the
// status line and the log take either.
func (a *App) reportSeatJournal(slot int, start bool, path string, err error) {
	switch {
	case err != nil:
		a.log.Warn("journal", "msg", "seat journal not changed", "slot", slot, "start", start, "error", err.Error())
		a.ctx.SetStatusMessage(fmt.Sprintf("Journal on slot %X: %s", slot, err), parameter.StatusAlert)
	case start:
		a.log.Info("journal", "msg", "seat journal opened", "slot", slot, "path", path)
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X journaling to %s", slot, path), parameter.StatusNotice)
	default:
		a.log.Info("journal", "msg", "seat journal closed", "slot", slot, "path", path)
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X journal closed: %s", slot, path), parameter.StatusNotice)
	}
}

// seatJournalNotices names each journal a seat of this run keeps, which closes with it.
func (a *App) seatJournalNotices() []string {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	var out []string
	for _, s := range a.seats {
		if b := s.app.Load(); b != nil {
			if r := b.recorder.Load(); r != nil {
				out = append(out, fmt.Sprintf("slot %X journal closed: %s", s.slot.Load(), r.Path()))
			}
		}
	}
	return out
}

// seatGraphs are the graphs of the seats this run holds, in the order they came; a
// seat told to leave is not held, even while it is still leaving.
func (a *App) seatGraphs() []string {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	specs := make([]string, 0, len(a.seats))
	for _, s := range a.seats {
		if !s.halted() {
			specs = append(specs, s.spec)
		}
	}
	return specs
}

func (a *App) botSeats() []engine.BotSeat {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	bots := make([]engine.BotSeat, 0, len(a.seats))
	for _, s := range a.seats {
		bots = append(bots, engine.BotSeat{ID: s.number, Joining: s.id.Load() == 0, Slot: uint8(s.slot.Load()),
			Graph: s.graph.Name, Stopping: s.halted()})
	}
	return bots
}

func (a *App) seatsSummary() string {
	bots := a.botSeats()
	if len(bots) == 0 {
		return "No bots; :bot add [N[:graph]|graph] seats bots"
	}
	parts := make([]string, 0, len(bots))
	for _, b := range bots {
		if b.Joining {
			parts = append(parts, b.Graph+" joining")
			continue
		}
		label := fmt.Sprintf("slot %X %s", b.Slot, b.Graph)
		if b.Stopping {
			label += " leaving"
		}
		parts = append(parts, label)
	}
	return "Bots: " + strings.Join(parts, ", ") + "; :bot drop <slot> drops one"
}

// seatsOnly reports whether every participant linked to this run is a seat it
// holds, which is what lets an authority pause: its seats stand still with it.
// Called under the world lock.
func (a *App) seatsOnly() bool {
	port, ok := a.sessionTransportLocked().(interface{ Peers() []uint32 })
	if !ok {
		return false
	}
	peers := port.Peers()
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	return !slices.ContainsFunc(peers, func(p uint32) bool {
		return !slices.ContainsFunc(a.seats, func(s *seat) bool { return s.id.Load() == p })
	})
}
