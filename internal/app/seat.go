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
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
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

// loadBotGraph resolves and parses one graph spec, a name or a path.
func loadBotGraph(o resource.Options, spec string) (*bot.Graph, error) {
	data, name, err := resource.BotGraph(o, spec)
	if err != nil {
		return nil, err
	}
	return bot.ParseGraph(name, data)
}

// waitScriptTick waits delay unless signals delivers first, and reports whether it
// waited it out: a driven run's paced tick, a seat's rejoin.
func waitScriptTick(signals <-chan os.Signal, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-signals:
		return false
	case <-timer.C:
		return true
	}
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
}

// seatPlay is a seat's run as its holder hands it to be played (memberRun.PlaySeat):
// the Config and wiring it joins with and its log, the graph it plays, the channel
// that halts it, the pause it stands still with, its holder's mode as it was seated
// and its number among its holder's seats.
type seatPlay struct {
	config Config
	wiring membershipWiring
	log    *vlog.Log
	graph  *bot.Graph
	stop   <-chan os.Signal
	hold   func() bool
	mode   Mode
	number uint64
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
func (m *membership) seatHalted(id uint32) bool {
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	i := slices.Index(m.dropped, id)
	if i >= 0 {
		m.dropped = slices.Delete(m.dropped, i, i+1)
	}
	return i >= 0
}

// seatBots seats cfg.Bots in the session this run is in, or in one it opens for them.
func (m *membership) seatBots() error {
	if len(m.cfg.Bots) == 0 {
		return nil
	}
	if m.sessionTransport() == nil {
		m.run.Settle() // Bind the boot cursor before opening the bots' session.
	}
	var err error
	m.world.RunSafe(func() {
		for _, spec := range m.cfg.Bots {
			if err = m.addSeatLocked(spec); err != nil {
				return
			}
		}
	})
	return err
}

// addSeatLocked seats one bot playing spec. The graph is parsed here, so a name that
// does not resolve is the caller's error; the join runs on the seat's goroutine.
// Caller MUST hold updateMutex.
func (m *membership) addSeatLocked(spec string) error {
	graph, err := loadBotGraph(m.run.Resources(), spec)
	if err != nil {
		return err
	}
	play, err := m.seatConfigLocked()
	if err != nil {
		return err
	}
	s := &seat{spec: spec, graph: graph, stop: make(chan os.Signal), done: make(chan struct{})}
	m.seatsMu.Lock()
	m.seatSerial++
	s.number = m.seatSerial
	m.seats = append(m.seats, s)
	m.seatsMu.Unlock()
	play.log = vlog.NewLog("seat " + strconv.FormatUint(s.number, 10))
	play.mode = m.run.Mode() // the seat's goroutine reads nothing of its holder's config
	play.number = s.number
	core.Go(func() { m.playSeat(s, play) })
	return nil
}

// seatConfigLocked is a new seat's run, which joins this run's session in-process; a
// run in none first opens one with no listener. It sends the session's name, which
// the handshake its holder serves checks. Caller holds updateMutex.
func (m *membership) seatConfigLocked() (seatPlay, error) {
	if m.sessionTransportLocked() == nil {
		if err := m.openSessionLocked(nil); err != nil {
			return seatPlay{}, err
		}
	}
	width, height := m.run.SeatScreenLocked()
	return seatPlay{
		config: Config{Mode: ModeHeadless, Resources: m.run.Resources(), Width: width, Height: height,
			StatTicks: -1, RecTicks: -1, Membership: Membership{NoAdvertise: true, JoinAddress: seatJoin,
				SessionName: m.cfg.SessionName}},
		wiring: membershipWiring{holder: link.PeerID(m.world.LocalParticipant()),
			relay: m.seatLink, claims: m.claims},
	}, nil
}

// seatLink opens a stream to this run for one of its bots and serves the join
// handshake on the near end: the coordinator's, or a still-guest's, which asks the
// authority on its own link for each answer.
func (m *membership) seatLink() (net.Conn, error) {
	var (
		port     *link.SocketPort
		accept   func(net.Conn) (link.PeerID, error)
		admitted func(link.PeerID)
	)
	m.world.RunSafe(func() {
		port, _ = m.sessionTransportLocked().(*link.SocketPort)
		if m.world.IsSessionCoordinator() {
			accept, admitted = m.sessionAcceptor(m.admissions.Admit), m.admitLateJoiner
		} else {
			accept, admitted = m.relayAcceptor(m.admissions.Admit), m.releaseRelayedJoiner
		}
	})
	if port == nil {
		return nil, errors.New("the holder is in no session")
	}
	near, far := link.Pipe()
	if err := port.ServeConn(near, accept, admitted); err != nil {
		_ = far.Close()
		return nil, err
	}
	return far, nil
}

// playSeat runs one seat until its holder halts it, its graph quits, or the session
// ends for it, standing still while its holder's pause does. A join refused while
// the session starts or hands off is retried, as the refusal asks; any other failure
// ends it.
func (m *membership) playSeat(s *seat, play seatPlay) {
	defer close(s.done)
	defer m.forgetSeat(s)
	play.graph, play.stop = s.graph, s.stop
	// A holder whose clock stops when paused stops its seats with it; a driven one
	// steps through a pause, and so do they. A hold reaches them as the crossing it
	// is, at the tick every participant stops at.
	if !play.mode.Driven() {
		play.hold = m.ctx.TimeCtl.OwnPause
	}
	deadline := time.Now().Add(parameter.SessionRejoinWindow) // [wall] a link bound
	for {
		var driver *bot.Driver
		finished, err := m.run.PlaySeat(play, func(run *membership, d *bot.Driver) int {
			driver = d
			// The offer rather than the world: the cursor binds a tick later.
			run.sessionMu.Lock()
			entry, _ := run.sessionOffer.Entry(run.sessionOffer.Assigned)
			run.sessionMu.Unlock()
			s.slot.Store(uint32(entry.Slot))
			s.id.Store(uint32(entry.ID))
			m.log.Info("app", "msg", "bot seated", "graph", s.graph.Name,
				"peer", s.id.Load(), "slot", s.slot.Load())
			return int(entry.Slot)
		})
		if driver == nil && err != nil && network.RetryOf(err) == network.RetrySoon && time.Now().Before(deadline) &&
			waitScriptTick(s.stop, parameter.SessionRejoinInterval) {
			continue
		}
		switch {
		case driver == nil && err != nil:
			m.log.Warn("app", "msg", "bot not seated", "graph", s.graph.Name, "error", err.Error())
			m.ctx.SetStatusMessage("Bot "+s.graph.Name+": "+err.Error(), parameter.StatusAlert)
		case driver != nil:
			st := driver.Stats()
			m.log.Info("app", "msg", "bot left", "graph", s.graph.Name, "peer", s.id.Load(),
				"finished", finished, "ticks", st.Ticks, "injected", st.Injected, "dropped", st.Dropped,
				"fitness", st.Fitness, "state", st.State, "error", fmt.Sprint(err))
		}
		return
	}
}

func (m *membership) forgetSeat(s *seat) {
	m.seatsMu.Lock()
	m.seats = slices.DeleteFunc(m.seats, func(o *seat) bool { return o == s })
	m.seatsMu.Unlock()
}

// haltSeats halts every seat without waiting and returns them. Each link closing is
// then the drop that was asked for, as it is for a seat dropped alone, unless the
// seats keep their places: then it is a lost link, whose cursor the session keeps
// as a Wraith.
func (m *membership) haltSeats(keep bool) []*seat {
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	seats := slices.Clone(m.seats)
	for _, s := range seats {
		if id := s.id.Load(); id != 0 && !s.halted() && !keep {
			m.dropped = append(m.dropped, id)
		}
		s.halt()
	}
	return seats
}

// closeSeats halts every seat, keeping their places or not, and waits for each to
// leave, so a holder's bots depart the session before it does.
func (m *membership) closeSeats(keep bool) {
	for _, s := range m.haltSeats(keep) {
		<-s.done
	}
}

// dropSeat halts the seat on slot; it leaves as any participant does.
func (m *membership) dropSeat(slot int) error {
	if m.stopSeat(func(s *seat) bool { return s.id.Load() != 0 && int(s.slot.Load()) == slot }) {
		return nil
	}
	return fmt.Errorf("no bot of this run's is on slot %X", slot)
}

func (m *membership) removeSeat(id uint64) error {
	if m.stopSeat(func(s *seat) bool { return s.number == id }) {
		return nil
	}
	return fmt.Errorf("this bot has already left")
}

func (m *membership) stopSeat(match func(*seat) bool) bool {
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	for _, s := range m.seats {
		if match(s) {
			if id := s.id.Load(); id != 0 {
				m.dropped = append(m.dropped, id)
			}
			s.halt()
			return true
		}
	}
	return false
}

// seatGraphs are the graphs of the seats this run holds, in the order they came; a
// seat told to leave is not held, even while it is still leaving.
func (m *membership) seatGraphs() []string {
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	specs := make([]string, 0, len(m.seats))
	for _, s := range m.seats {
		if !s.halted() {
			specs = append(specs, s.spec)
		}
	}
	return specs
}

func (m *membership) botSeats() []engine.BotSeat {
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	bots := make([]engine.BotSeat, 0, len(m.seats))
	for _, s := range m.seats {
		bots = append(bots, engine.BotSeat{ID: s.number, Joining: s.id.Load() == 0, Slot: uint8(s.slot.Load()),
			Graph: s.graph.Name, Stopping: s.halted()})
	}
	return bots
}

func (m *membership) seatsSummary() string {
	bots := m.botSeats()
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
func (m *membership) seatsOnly() bool {
	port, ok := m.sessionTransportLocked().(interface{ Peers() []uint32 })
	if !ok {
		return false
	}
	peers := port.Peers()
	m.seatsMu.Lock()
	defer m.seatsMu.Unlock()
	return !slices.ContainsFunc(peers, func(p uint32) bool {
		return !slices.ContainsFunc(m.seats, func(s *seat) bool { return s.id.Load() == p })
	})
}
