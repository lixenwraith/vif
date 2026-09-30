package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/vlog"
)

// DefaultBotGraph is the graph a bot seat plays when none is named.
const DefaultBotGraph = "roam"

// seatLoopback is where a run in no session opens one for its bots.
const seatLoopback = "127.0.0.1:0"

// seat is one bot this run holds: a headless instance of its own that joins the
// run's session over an ordinary link, as any participant does, and plays a graph.
// The session sees a participant; only the holder knows it is a bot.
type seat struct {
	spec  string
	graph *bot.Graph
	stop  chan os.Signal // closed to halt it; drive reads a closed channel as a signal
	done  chan struct{}
	once  sync.Once
	id    atomic.Uint32 // the participant it was admitted as, zero until then
	slot  atomic.Uint32
}

func (s *seat) halt() { s.once.Do(func() { close(s.stop) }) }

// seatBots seats cfg.Bots in the session this run is in, or in one it hosts on
// loopback for them.
func (a *App) seatBots() error {
	if len(a.cfg.Bots) == 0 {
		return nil
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
	// steps through a pause, and so do they.
	var hold func() bool
	if !a.cfg.Mode.Driven() {
		hold = a.ctx.TimeCtl.IsPaused
	}
	s := &seat{spec: spec, graph: graph, stop: make(chan os.Signal), done: make(chan struct{})}
	a.seatsMu.Lock()
	a.seats = append(a.seats, s)
	a.seatsMu.Unlock()
	core.Go(func() { a.playSeat(s, cfg, hold) })
	return nil
}

// seatConfigLocked is a new seat's run: headless at the fleet's terminal size, never
// advertised, recording nothing process-wide, and joined to the session this run
// joined, or to its own listener over loopback, which a run in none opens first.
// Caller MUST hold updateMutex.
func (a *App) seatConfigLocked() (Config, error) {
	cfg := Config{Mode: ModeHeadless, Resources: a.cfg.Resources, Width: BotWidth, Height: BotHeight,
		NoAdvertise: true, StatTicks: -1, RecTicks: -1}
	if a.cfg.JoinAddress != "" {
		cfg.JoinAddress, cfg.SessionName = a.cfg.JoinAddress, a.cfg.SessionName
		return cfg, nil
	}
	if a.sessionTransportLocked() == nil {
		if err := a.beginHostingLocked(seatLoopback, ""); err != nil {
			return cfg, err
		}
	}
	bound := a.ownListener()
	if bound == "" {
		return cfg, errors.New("this run's session has no listener to seat a bot on")
	}
	target := loopbackOf(bound)
	if a.cfg.SessionName != "" {
		target += "/" + a.cfg.SessionName
	}
	return cfg.joining(target)
}

// ownListener is the address this run's session listener bound, "" when it has none.
func (a *App) ownListener() string {
	if addr := a.HostAddr(); addr != "" {
		return addr
	}
	if port, err := a.socketPort(); err == nil && a.cfg.HostAddress != "" && port.Addr() != nil {
		return port.Addr().String()
	}
	return ""
}

// loopbackOf names a bound listener as a dial from this machine: an unspecified host
// becomes the loopback of its family, which the admission budget does not count.
func loopbackOf(bound string) string {
	scheme, hostport := "", bound
	if s, rest, ok := strings.Cut(bound, "://"); ok {
		scheme, hostport = s+"://", rest
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return bound
	}
	if ip := net.ParseIP(host); host == "" || ip.IsUnspecified() {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	return scheme + net.JoinHostPort(host, port)
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
		var driver *bot.Driver
		finished, err := drive(cfg, "bot", s.graph.Name, s.stop, hold,
			func(b *App) (pacedSource, error) {
				d, err := bot.NewDriver(b, b.ctx, s.graph, b.Seed(), b.localParticipant())
				if err != nil {
					return nil, err
				}
				driver = d
				// The offer rather than the world: the cursor binds a tick later.
				b.sessionMu.Lock()
				entry, _ := b.sessionOffer.Entry(b.sessionOffer.Assigned)
				b.sessionMu.Unlock()
				s.id.Store(uint32(entry.ID))
				s.slot.Store(uint32(entry.Slot))
				vlog.Info("app", "msg", "bot seated", "graph", s.graph.Name,
					"participant", s.id.Load(), "slot", s.slot.Load(), "joined", cfg.JoinAddress)
				return seatSource{botSource{d}, forkCell(b), forkCell(a)}, nil
			})
		if driver == nil && err != nil && retryableJoin(err) && time.Now().Before(deadline) &&
			waitScriptTick(s.stop, parameter.SessionRejoinInterval) {
			continue
		}
		switch {
		case driver == nil && err != nil:
			vlog.Warn("app", "msg", "bot not seated", "graph", s.graph.Name, "error", err.Error())
			a.ctx.SetStatusMessage("Bot "+s.graph.Name+": "+err.Error(), parameter.StatusMessageMaxDuration, true)
		case driver != nil:
			st := driver.Stats()
			vlog.Info("app", "msg", "bot left", "graph", s.graph.Name, "participant", s.id.Load(),
				"finished", finished, "ticks", st.Ticks, "injected", st.Injected, "error", fmt.Sprint(err))
		}
		return
	}
}

// retryableJoin reports a refusal that asks the dialer to retry: the host is between
// its lobby and its mid-run gate, or electing an authority.
func retryableJoin(err error) bool {
	return network.IsHandoffRefusal(err) || strings.Contains(err.Error(), ErrSessionStarting.Error())
}

// forkCell is an instance's own "no session left" flag: it lost its authority and
// nobody took the session over.
func forkCell(a *App) *atomic.Bool { return a.world.Resources.Status.Bools.Get("network.fork") }

// seatSource ends a seat once its session has, for it or for its holder. A seat left
// alone plays for nobody, and a holder that left its session takes its bots along.
type seatSource struct {
	botSource
	own, holder *atomic.Bool
}

func (s seatSource) Step() (bool, error) {
	if s.own.Load() || s.holder.Load() {
		return false, nil
	}
	return s.botSource.Step()
}

func (a *App) forgetSeat(s *seat) {
	a.seatsMu.Lock()
	a.seats = slices.DeleteFunc(a.seats, func(o *seat) bool { return o == s })
	a.seatsMu.Unlock()
}

// haltSeats halts every seat without waiting and returns them.
func (a *App) haltSeats() []*seat {
	a.seatsMu.Lock()
	seats := slices.Clone(a.seats)
	a.seatsMu.Unlock()
	for _, s := range seats {
		s.halt()
	}
	return seats
}

// closeSeats halts every seat and waits for each to leave, so a holder's bots depart
// the session before it does.
func (a *App) closeSeats() {
	for _, s := range a.haltSeats() {
		<-s.done
	}
}

// dropSeat halts the seat on slot; it leaves as any participant does.
func (a *App) dropSeat(slot int) error {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	for _, s := range a.seats {
		if s.id.Load() != 0 && int(s.slot.Load()) == slot {
			s.halt()
			return nil
		}
	}
	return fmt.Errorf("no bot of this run's is on slot %d", slot)
}

// seatGraphs are the graphs of the seats this run holds, in the order they came.
func (a *App) seatGraphs() []string {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	specs := make([]string, 0, len(a.seats))
	for _, s := range a.seats {
		specs = append(specs, s.spec)
	}
	return specs
}

// seatsSummary is :bot's report of the seats this run holds.
func (a *App) seatsSummary() string {
	a.seatsMu.Lock()
	defer a.seatsMu.Unlock()
	if len(a.seats) == 0 {
		return "No bots; :bot add [graph] seats one"
	}
	parts := make([]string, 0, len(a.seats))
	for _, s := range a.seats {
		if s.id.Load() == 0 {
			parts = append(parts, s.graph.Name+" joining")
			continue
		}
		parts = append(parts, fmt.Sprintf("slot %d %s", s.slot.Load(), s.graph.Name))
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
