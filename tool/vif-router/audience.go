package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lixenwraith/vif/internal/network"
	link "github.com/lixenwraith/vif/pkg/network"
)

const (
	// audienceLinger is how long a session's feed is kept once its last spectator
	// left: a page that reloads finds it, and its pod is not asked for a world again.
	audienceLinger = 15 * time.Second
	// audienceOpens bounds a dial's wait on the subscription another's is opening:
	// the lookup, the pod's connect and its handshake, each at its worst.
	audienceOpens = 3 * routeTimeout
	// feedSubject is whom the router's own ticket names, which its pod budgets.
	feedSubject = "router:feed"
)

// errFeedOver refuses a dial whose session's feed ended while it was answered: the
// next dial opens the feed anew, or is told why it cannot.
var errFeedOver = &network.Refusal{Code: network.RefusalStarting, Reason: "the session's feed is starting again"}

// audiences are the sessions watched through this router. Each is one subscription
// to its pod's feed, which a hub here repeats to every spectator, so a spectator
// costs the router a copy and its pod nothing. The front door and the browser
// route share them; see doc/kubernetes-fleet.md §9.
type audiences struct {
	max        int // spectators one session is shown to at once
	perAddress int // of them from one address; 0 leaves it to max
	linger     time.Duration

	mu sync.Mutex
	m  map[string]*audience
}

// audience is one session's: its subscription and the hub repeating it, with the
// session as a dial is shown it and the scenario it plays, both the pod's word.
type audience struct {
	id    string
	ready chan struct{} // closed once the subscription is open, or failed with err
	done  chan struct{} // closed once it has ended
	err   error

	feed     net.Conn
	hub      *link.FeedHub
	digest   string
	scenario []byte

	mu    sync.Mutex
	shown network.SessionOffer

	// Guarded by audiences.mu.
	viewers int            // dials being answered and spectators being fed
	from    map[string]int // of them, by address
	idle    *time.Timer
	over    bool
}

// newAudiences shows each session to max spectators at once, perAddress of them
// from one address; nil, for a max of none, shows no session.
func newAudiences(max, perAddress int) *audiences {
	if max <= 0 {
		return nil
	}
	return &audiences{max: max, perAddress: perAddress, linger: audienceLinger, m: make(map[string]*audience)}
}

// enter counts a dial into id's audience, which open begins for the first, and
// returns what gives its place back; or the refusal of a session, or an address,
// that has every spectator it takes. A dial still being answered holds a place.
func (s *audiences) enter(id, from string, open func(*audience)) (*audience, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.m[id]
	switch {
	case a == nil:
		a = &audience{id: id, ready: make(chan struct{}), done: make(chan struct{}), from: make(map[string]int)}
		s.m[id] = a
		go open(a)
	case a.viewers >= s.max:
		return nil, nil, &network.Refusal{Code: network.RefusalAudience,
			Reason: fmt.Sprintf("session has the %d spectator(s) it takes", s.max)}
	case s.perAddress > 0 && from != "" && a.from[from] >= s.perAddress:
		return nil, nil, &network.Refusal{Code: network.RefusalAudience,
			Reason: fmt.Sprintf("this address has the %d spectator(s) it may in one session", s.perAddress)}
	}
	if a.idle != nil {
		a.idle.Stop()
		a.idle = nil
	}
	a.viewers++
	if from != "" {
		a.from[from]++
	}
	var once sync.Once
	return a, func() { once.Do(func() { s.leave(a, from) }) }, nil
}

// leave gives a place back, and starts the linger of an audience left empty.
func (s *audiences) leave(a *audience, from string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.viewers--
	if from != "" {
		if a.from[from]--; a.from[from] <= 0 {
			delete(a.from, from)
		}
	}
	if a.viewers == 0 && !a.over {
		a.idle = time.AfterFunc(s.linger, func() { s.unwatched(a) })
	}
}

// unwatched closes the subscription of an audience still empty after its linger,
// which ends its repeating; the next dial opens the session's feed anew.
func (s *audiences) unwatched(a *audience) {
	s.mu.Lock()
	empty := a.viewers == 0 && !a.over
	if empty {
		s.forget(a)
	}
	s.mu.Unlock()
	if empty {
		if <-a.ready; a.feed != nil {
			_ = a.feed.Close()
		}
	}
}

// end takes an audience whose subscription failed or ended out of the sessions
// watched, so the next dial opens its own.
func (s *audiences) end(a *audience) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forget(a)
}

// forget is end under the caller's lock.
func (s *audiences) forget(a *audience) {
	a.over = true
	if s.m[a.id] == a {
		delete(s.m, a.id)
	}
	if a.idle != nil {
		a.idle.Stop()
		a.idle = nil
	}
}

// watching is how many watch id through this router now; a nil s shows nobody.
func (s *audiences) watching(id string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.m[id]; a != nil {
		return a.viewers
	}
	return 0
}

// session is the session as a watching dial is shown it: what the feed's newest
// starting point says, so the world the dial builds is the one its feed begins from.
func (a *audience) session(network.Admission) (network.SessionOffer, error) {
	select {
	case <-a.done:
		return network.SessionOffer{}, errFeedOver
	default:
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.shown, nil
}

// show reads the session off each starting point as it passes to the spectators.
func (a *audience) show(rec []byte) {
	if rec[0] != link.FeedSync {
		return
	}
	head, err := network.DecodeWatchHead(link.FeedBody(rec))
	if err != nil {
		return // the spectators' own runs refuse it, and say why
	}
	// A hold is the starting point's to say, as it is at the session's own door.
	head.Offer.Held, head.Offer.Start = false, false
	a.mu.Lock()
	a.shown = head.Offer
	a.mu.Unlock()
}

// scenarioBody answers a dial that lacks the session's scenario with the pod's copy.
func (a *audience) scenarioBody(digest string) ([]byte, error) {
	if digest != a.digest || len(a.scenario) == 0 {
		return nil, errors.New("this session plays another scenario than the one asked for")
	}
	return a.scenario, nil
}

// spectator is a watching dial's stream, which gives its place in the audience back
// as it is closed: by the hub that feeds it, or by the handshake that failed.
type spectator struct {
	net.Conn
	leave func()
}

func (c *spectator) Close() error {
	c.leave()
	return c.Conn.Close()
}

// watch serves a dial that only watches a fleet session as the session's own door
// would, from the copy of its feed this router keeps, which the first to ask opens.
// The dial holds a place in the audience from here, and no connection to the pod.
func (r *tcpRouter) watch(client net.Conn, from net.Addr, id string) {
	a, leave, err := r.audiences.enter(id, network.AdmissionKey(from), r.subscribe)
	if err == nil {
		select {
		case <-a.ready:
			err = a.err
		case <-time.After(audienceOpens):
			err = errSessionUnroutable
		}
		if err != nil {
			leave()
		}
	}
	if err != nil {
		network.RefuseJoin(client, err, routeTimeout)
		_ = client.Close()
		return
	}
	conn := &spectator{Conn: client, leave: leave}
	door := network.Coordinator{Name: id, Watch: a.session, Scenario: a.scenarioBody,
		Feed: func(conn net.Conn, _ network.Admission) error { return a.hub.Subscribe(conn) }}
	if err := network.ServeWatch(conn, door, routeTimeout); err != nil {
		r.log.Info("watch dial not served", "session", id, "error", err)
		_ = conn.Close()
		return
	}
	r.log.Info("session watched", "session", id, "spectators", r.audiences.watching(id))
}

// subscribe opens a's subscription and repeats it to a's spectators until the pod
// ends the feed, the link to it is lost, or nobody has watched for the linger.
func (r *tcpRouter) subscribe(a *audience) {
	a.err = r.open(a)
	close(a.ready)
	if a.err != nil {
		r.audiences.end(a)
		close(a.done)
		return
	}
	r.log.Info("session feed opened", "session", a.id)
	started := time.Now()
	err := a.hub.Repeat(a.feed, a.show)
	_ = a.feed.Close()
	r.audiences.end(a)
	close(a.done)
	totals := a.hub.Totals()
	r.log.Info("session feed closed", "session", a.id, "reason", err.Error(),
		"bytes", totals.Stream+totals.Heads, "seconds", int64(time.Since(started)/time.Second))
}

// open dials a's pod as a dial that only watches, under a ticket of the router's
// own where it signs them, and takes the session as shown, its scenario and its
// feed. What fails is what a's dials are told: the pod's refusal, or no more than
// that the pod is out of reach.
func (r *tcpRouter) open(a *audience) error {
	podIP, err := r.lookup(a.id, network.AdmitWatch)
	if err != nil {
		return err
	}
	fail := func(step string, err error) error {
		if refused := new(*network.Refusal); errors.As(err, refused) {
			return *refused
		}
		r.log.Warn("session feed not opened", "session", a.id, "step", step, "error", err)
		return errSessionUnroutable
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(podIP, r.game), podDialTimeout)
	if err != nil {
		return fail("dial", err)
	}
	cfg := network.DefaultConfig()
	cfg.SessionName, cfg.ConnectTimeout, cfg.Keys = a.id, routeTimeout, []network.Key{network.NewKey()}
	if r.ticket.Valid() {
		body := network.TicketBody{Session: a.id, Kind: network.AdmitWatch, Subject: feedSubject, Key: cfg.Keys[0].Public()}
		if cfg.Ticket, err = network.SignTicket(r.ticket, body, time.Now()); err != nil {
			_ = conn.Close()
			return fail("ticket", err)
		}
	}
	pending, shown, err := network.WatchOver(conn, cfg)
	if err != nil {
		return fail("watch", err)
	}
	if digest := shown.Anchor.Anchor.ScenarioDigest; digest != "" {
		if a.scenario, err = pending.RequestScenario(digest); err != nil {
			_ = pending.Close()
			return fail("scenario", err)
		}
		a.digest = digest
	}
	if a.feed, err = pending.Watch(); err != nil {
		_ = pending.Close()
		return fail("feed", err)
	}
	a.shown, a.hub = shown, link.NewFeedHub(network.FeedConfig(r.audiences.max))
	return nil
}
