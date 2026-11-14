package membership

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// audience is everyone watching the session this run hosts: one feed, written as the
// authority commits and copied to each viewer by its hub. stop ends its publisher
// (serveAudience), and done is closed once that has returned.
type audience struct {
	hub        *link.FeedHub
	writer     *link.FeedWriter
	stop, done chan struct{}
}

// watchAcceptor is the handshake this run's doors serve a dial that only watches,
// which takes no pass: a viewer comes back to no place. A dial it turns away is the
// log's alone, since nobody playing is told who came to watch.
func (m *Membership) watchAcceptor() func(net.Conn) error {
	serve := network.WatchAcceptor(network.Coordinator{
		Name:       m.cfg.SessionName,
		Admit:      m.joinBudget(),
		Watch:      m.watchParticipant,
		Feed:       m.feedTo,
		Scenario:   m.scenarioBody,
		Binding:    network.LeafBinding(m.cfg.TLS),
		Passphrase: m.passphraseDoor,
		Ticket:     m.ticketHook(network.AdmitWatch),
		Gated:      m.gated,
	}, network.DefaultConfig().ConnectTimeout)
	return func(conn net.Conn) error {
		if err := serve(conn); err != nil {
			m.log.Info("session", "msg", "watch dial not served", "from", conn.RemoteAddr().String(), "error", err.Error())
			_ = conn.Close()
		}
		return nil
	}
}

// watchParticipant is the session as a dial that only watches is shown it, or why
// it is not fed: what refuses any dial, a session this run does not author or has
// not started, and an audience at its ceiling. Nothing is held for it, and the
// keep-out list does not apply: that keeps a place from being taken, and it takes none.
func (m *Membership) watchParticipant(network.Admission) (network.SessionOffer, error) {
	var anchor event.JoinAnchor
	hosting := false
	m.world.RunSafe(func() { anchor, hosting = m.joinAnchorLocked(), m.world.IsSessionCoordinator() })
	ceiling := m.audienceCeiling()
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	err := m.refusedLocked()
	switch {
	case m.unwatched != nil:
		err = m.unwatched
	case err != nil:
	case !hosting:
		err = errNotHosting
	case !m.lateJoins.Load():
		err = errSessionStarting
	case ceiling == 0:
		err = &network.Refusal{Code: network.RefusalUnknown, Reason: "this session takes no viewers"}
	case m.audience != nil && m.audience.hub.Subscribers() >= ceiling:
		err = &network.Refusal{Code: network.RefusalAudience,
			Reason: fmt.Sprintf("session has the %d viewer(s) it takes", ceiling)}
	}
	if err != nil {
		return network.SessionOffer{}, err
	}
	return m.offerLocked(anchor, 0).Watched(), nil
}

// audienceCeiling is how many viewers this run's feed takes at once.
func (m *Membership) audienceCeiling() int {
	return max(cmp.Or(m.cfg.Spectators, parameter.FeedAudience), 0)
}

// feedTo subscribes a watching dial's stream to this session's feed, which its
// first viewer opens. The hub owns conn from here on.
func (m *Membership) feedTo(conn net.Conn, _ network.Admission) error {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if m.unwatched != nil {
		return m.unwatched
	}
	a := m.audience
	if a == nil {
		hub := link.NewFeedHub(network.FeedConfig(m.audienceCeiling()))
		a = &audience{hub: hub, writer: link.NewFeedWriter(hub), stop: make(chan struct{}), done: make(chan struct{})}
		m.audience = a
		core.Go(func() { m.serveAudience(a) })
	}
	if err := a.hub.Subscribe(conn); err != nil {
		return err
	}
	m.log.Info("session", "msg", "viewer watching", "viewers", a.hub.Subscribers())
	return nil
}

// serveAudience keeps a's feed whole while anyone watches: a starting point when a
// viewer waits for one, when the game changed and each parameter.FeedSyncTicks, and
// the ticks a stopped clock closed and left unsent. With nobody watching the
// authority copies nothing, and a session another run authors ends the feed.
func (m *Membership) serveAudience(a *audience) {
	defer close(a.done)
	poll := time.NewTicker(parameter.FeedPollInterval) // [wall] the feed's cadence, not the game's
	defer poll.Stop()
	var synced, seen event.Stamp
	attached := false
	for {
		select {
		case <-a.stop:
			return
		case <-a.hub.Wanted():
		case <-poll.C:
		}
		if a.hub.Idle() {
			if attached {
				m.world.RunSafe(func() { m.detachFeedLocked(a) })
				attached = false
			}
			continue
		}
		at := m.run.Position()
		switch {
		case !attached || a.hub.Waiting() || at.Run != synced.Run || at.Tick >= synced.Tick+parameter.FeedSyncTicks:
			now, hosting, published := m.syncAudience(a)
			if !hosting {
				m.dropAudience(a, "The session moved to another host")
				return
			}
			attached = true
			if published {
				synced = now
			}
		case at == seen:
			a.writer.Flush()
		}
		seen = at
	}
}

// syncAudience writes a's feed a starting point at the tick the session stands at.
// The feed is cut, the world captured and what is committed past it read under one
// lock, so the stream after the cut continues what the starting point holds; the
// rest is encoded off it. published is false where a later cut replaced this one.
func (m *Membership) syncAudience(a *audience) (at event.Stamp, hosting, published bool) {
	var (
		head           network.WatchHead
		cut            uint64
		epochs, states []event.WireBatch
	)
	began := time.Now() // [wall] measures the stall the capture is
	cap, err := m.glue.CaptureSharedWith(func() {
		r := m.world.Resources.Network
		if hosting = r != nil && m.world.IsSessionCoordinator(); !hosting {
			return
		}
		at = m.run.Position()
		r.Feed, cut = a.writer, a.writer.Cut(at.Tick)
		for _, sys := range m.world.Systems() {
			if p, ok := sys.(interface {
				PendingAfter(uint64) []event.WireBatch
				PendingStates(uint64) []event.WireBatch
			}); ok {
				epochs, states = p.PendingAfter(at.Tick), p.PendingStates(at.Tick)
			}
		}
		anchor, held := m.joinAnchorLocked(), m.ctx.TimeCtl.Held()
		m.sessionMu.Lock()
		head.Offer = m.offerLocked(anchor, 0).Watched()
		m.sessionMu.Unlock()
		head.Offer.Held, head.Offer.Start = held, held && m.ctx.TimeCtl.Holds(engine.HoldStart)
	})
	captured := time.Since(began)
	if err == nil && !hosting {
		return at, false, false
	}
	var body []byte
	if err == nil {
		head.World, err = m.glue.Encoder().EncodeCapture(cap)
	}
	if err == nil {
		head.Epochs, err = encodeBatches(epochs)
	}
	if err == nil {
		head.States, err = encodeBatches(states)
	}
	if err == nil {
		body, err = head.Encode()
	}
	if err != nil {
		m.log.Warn("session", "msg", "feed starting point not written", "error", err.Error())
		return at, true, false
	}
	published = a.writer.Sync(cut, body)
	m.log.Debug("session", "msg", "feed starting point", "tick", at.Tick, "run", at.Run, "bytes", len(body),
		"capture_us", captured.Microseconds(), "encode_us", (time.Since(began) - captured).Microseconds(),
		"viewers", a.hub.Subscribers(), "published", published)
	return at, true, published
}

// encodeBatches is each batch as a frame carries it.
func encodeBatches(batches []event.WireBatch) ([][]byte, error) {
	bodies := make([][]byte, 0, len(batches))
	for _, b := range batches {
		body, err := event.EncodeWireBatch(b)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

// closeAudience ends this run's feed, telling its viewers why, in the words a
// person's run shows, and whether it is served again where they found it. after is
// what a watching dial is refused with from then on, nil where a later one may open
// the feed anew. The caller MUST NOT hold updateMutex: its publisher takes it.
func (m *Membership) closeAudience(reason string, again bool, after error) {
	m.sessionMu.Lock()
	a := m.audience
	m.audience, m.unwatched = nil, cmp.Or(m.unwatched, after)
	m.sessionMu.Unlock()
	if a == nil {
		return
	}
	a.hub.End(reason, again)
	close(a.stop)
	<-a.done
	m.world.RunSafe(func() { m.detachFeedLocked(a) })
	m.log.Info("session", "msg", "feed ended", "reason", reason, "again", again)
}

// dropAudience is closeAudience from a's own publisher, which nothing waits for.
func (m *Membership) dropAudience(a *audience, reason string) {
	m.sessionMu.Lock()
	if m.audience == a {
		m.audience = nil
	}
	m.sessionMu.Unlock()
	a.hub.End(reason, false)
	m.world.RunSafe(func() { m.detachFeedLocked(a) })
	m.log.Info("session", "msg", "feed ended", "reason", reason, "again", false)
}

// detachFeedLocked stops the authority copying what it commits to a's feed. Caller
// MUST hold updateMutex.
func (m *Membership) detachFeedLocked(a *audience) {
	if r := m.world.Resources.Network; r != nil && r.Feed == engine.FeedSink(a.writer) {
		r.Feed = nil
	}
}

// CloseFeed ends this run's part in a feed as the run closes: the one it serves,
// whose viewers watch again where the run replacing this one reopens its door, and
// the one a viewer reads.
func (m *Membership) CloseFeed(restarting bool) {
	if m.world == nil {
		return
	}
	if restarting {
		m.closeAudience("The session is restarting", true, errSessionStarting)
	} else {
		m.closeAudience("The host closed the session", false, sessionEnding("its host closed it"))
	}
	if port, ok := m.SessionTransport().(*link.FeedPort); ok {
		_ = port.Close()
	}
}

// Audience is how many viewers this run's feed has now, and the feed's bytes, each
// counted once however many it is copied to.
func (m *Membership) Audience() (viewers int, totals link.FeedTotals) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if m.audience == nil {
		return 0, link.FeedTotals{}
	}
	return m.audience.hub.Subscribers(), m.audience.hub.Totals()
}

// startWatchSession takes this run's dial through to its feed and begins from the
// first starting point it carries: the session as its viewers are shown it, its
// host's world, and what the host had committed past that world. Another game's
// world is ErrSessionMoved, as at a join's gate.
func (m *Membership) startWatchSession(signals <-chan os.Signal) error {
	shown := link.Shown(m.cfg.JoinAddress)
	m.run.ShowJoinStatus("Watching " + shown + "; waiting for the session's world")
	conn, err := m.pendingJoin.Watch()
	if err != nil {
		return err
	}
	port := network.NewFeedPort(conn)
	fail := func(err error) error {
		_ = port.Close()
		return err
	}
	body, err := m.awaitHead(port, signals)
	if err != nil {
		return fail(err)
	}
	head, err := network.DecodeWatchHead(body)
	if err != nil {
		return fail(err)
	}
	cap, err := snapshot.DecodeCapture(head.World)
	if err != nil {
		return fail(err)
	}
	if cap.Header.Session != m.world.Resources.Rand.Session() {
		return fail(ErrSessionMoved)
	}
	m.run.ShowJoinStatus(fmt.Sprintf("Installing the session world at tick %d (%d bytes)", cap.Header.Tick, len(head.World)))
	if err := m.watchSessionAt(port, head, cap); err != nil {
		return fail(fmt.Errorf("watch: %w", err))
	}
	m.run.ShowStartupStatus(fmt.Sprintf("Watching %s: %d participant(s)", shown, head.Offer.ParticipantCount()))
	return nil
}

// awaitHead waits for the feed's first starting point, which its host writes once a
// viewer has none to begin from, answering the terminal and signals as a join's
// start gate does.
func (m *Membership) awaitHead(port *link.FeedPort, signals <-chan os.Signal) ([]byte, error) {
	timeout := time.NewTimer(parameter.FeedHeadTimeout) // [wall] a link bound
	defer timeout.Stop()
	for {
		select {
		case <-port.Heads():
			if body := port.TakeHead(); body != nil {
				return body, nil
			}
		case <-port.Done():
			return nil, fmt.Errorf("watch: the feed ended before its first world: %w", port.Err())
		case <-timeout.C:
			return nil, fmt.Errorf("watch: the session sent no world within %s", parameter.FeedHeadTimeout)
		case <-signals:
			return nil, ErrSessionSignalled
		case ev := <-m.run.LobbyEvents():
			if m.run.LobbyEventCancels(ev) {
				return nil, ErrSessionCanceled
			}
		}
	}
}

// watchSessionAt installs the world a starting point holds and attaches its feed as
// this run's transport, under no identity: the run is nobody in the session it
// watches. What the host had committed past that world is in hand before a tick runs.
func (m *Membership) watchSessionAt(port *link.FeedPort, head network.WatchHead, cap snapshot.SharedCapture) error {
	o := head.Offer
	if err := m.JoinAt(o.Anchor); err != nil {
		return err
	}
	m.openAuthority(o, 0)
	m.scheduler.Settle()
	staged, err := m.glue.StageShared(cap)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if err := staged.Commit(); err != nil {
		return err
	}
	m.adoptCorrectionBaseline(cap, len(head.World))

	port.Attribute(uint32(o.Host))
	for _, b := range head.Epochs {
		port.Inject(0, uint8(network.MsgEvent), b)
	}
	for _, b := range head.States {
		port.Inject(0, uint8(network.MsgStateSync), b)
	}
	m.world.RunSafe(func() {
		r := engine.NewNetworkResource(port)
		r.ParticipantID = 0
		m.attachResourceLocked(r)
	})
	m.sessionMu.Lock()
	m.sessionOffer = o
	m.sessionMu.Unlock()
	m.log.Info("session", "msg", "watching the session", "tick", cap.Header.Tick, "run", cap.Header.Run,
		"bytes", len(head.World), "epochs", len(head.Epochs), "states", len(head.States))
	return nil
}

// resumeWatchedSession lands a viewer in the hold its starting point was read in,
// or runs it up to the ticks its feed has closed since, bar its slack: a starting
// point is up to a period old when a viewer begins from it.
func (m *Membership) resumeWatchedSession() error {
	port, ok := m.SessionTransport().(*link.FeedPort)
	if !ok {
		return errors.New("watch: this run reads no feed")
	}
	m.landHeld()
	caught := 0
	for !m.ctx.TimeCtl.Held() && port.Backlog() > parameter.FeedSlackTicks {
		at := m.run.Position()
		if m.scheduler.RunTicks(1); m.run.Position() == at {
			break
		}
		caught++
	}
	m.log.Info("session", "msg", "viewer caught up", "caught_up_ticks", caught,
		"tick", m.run.Position().Tick, "backlog", port.Backlog())
	m.glue.Telemetry().CatchUp.Store(int64(caught))
	return nil
}

// followFeed takes what a viewer's feed carries beside its ticks: a later starting
// point, whose world the viewer checks its own against as a correction, and the
// feed's end. Caller MUST NOT hold updateMutex.
func (m *Membership) followFeed(port *link.FeedPort) {
	if body := port.TakeHead(); body != nil {
		head, err := network.DecodeWatchHead(body)
		switch {
		case err != nil:
			m.log.Warn("session", "msg", "feed starting point not read", "error", err.Error())
		case m.corrections != nil:
			m.corrections.Receive(head.World)
		}
	}
	select {
	case <-port.Done():
		m.feedEnded(port.Err())
	default:
	}
}

// feedEnded takes a viewer out of a session whose feed is over, once. A person's
// run watches again where the host is to serve the session anew, or only the link
// went, and otherwise starts a game of its own; a driven run has no loop to do
// either from, and ends.
func (m *Membership) feedEnded(err error) {
	if !m.feedOver.CompareAndSwap(false, true) {
		return
	}
	var ended link.FeedEnded
	over := errors.As(err, &ended) && !ended.Again
	m.log.Info("session", "msg", "viewer's feed ended", "error", fmt.Sprint(err), "over", over, "tick", m.run.Position().Tick)
	switch {
	case m.run.Mode() != config.ModePlay:
		m.dismissed.Store(true)
	case over:
		m.run.ReplaceWith(&RestartAsk{Notice: cmp.Or(ended.Reason, "The session ended") + "; started a local game"})
	case ended.Again:
		m.ctx.SetStatusMessage("Session restarting; watching again...", parameter.StatusProgress)
		m.run.ReplaceWith(&RestartAsk{Rejoin: true})
	default:
		m.ctx.SetStatusMessage("The session's feed was lost; watching again...", parameter.StatusProgress)
		m.run.ReplaceWith(&RestartAsk{Rejoin: true})
	}
}
