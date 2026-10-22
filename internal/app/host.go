package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/status"
)

// The router already holds the world lock when calling this adapter.
type sessionControl struct{ a *App }

func (c sessionControl) BeginHosting(addr, authority string) error {
	return c.a.beginHostingLocked(addr, authority)
}
func (c sessionControl) Join(target string) error { return c.a.joinLocked(target) }
func (c sessionControl) SessionSummary() string   { return c.a.sessionSummaryLocked() }
func (c sessionControl) DropBot(slot int) error {
	if err := c.a.dropSeat(slot); err == nil {
		return nil
	}
	e := c.a.world.Resources.Player.Slot(uint8(slot))
	cursor, ok := c.a.world.Components.Cursor.GetComponent(e)
	if !ok || cursor.Holder == 0 {
		return fmt.Errorf("no bot on slot %X", slot)
	}
	return c.a.dropPlayerLocked(e)
}
func (c sessionControl) RemoveBot(id uint64) error   { return c.a.removeSeat(id) }
func (c sessionControl) BotSummary() string          { return c.a.seatsSummary() }
func (c sessionControl) Bots() []engine.BotSeat      { return c.a.botSeats() }
func (c sessionControl) HostError() error            { return c.a.hostErrorLocked() }
func (c sessionControl) JoinError() error            { return c.a.joinErrorLocked() }
func (c sessionControl) StartJournal(slot int) error { return c.a.startJournalLocked(slot) }
func (c sessionControl) StopJournal(slot int) error  { return c.a.stopJournalLocked(slot) }
func (c sessionControl) JournalError() error         { return c.a.journalErrorLocked() }
func (c sessionControl) Journaling() bool            { return c.a.journaling() }
func (c sessionControl) JournalSummary() string      { return c.a.journalSummary() }

func (c sessionControl) DropPlayer(slot int) error {
	if slot < 0 || slot >= parameter.MaxPlayers {
		return errors.New("invalid player slot")
	}
	return c.a.dropPlayerLocked(c.a.world.Resources.Player.Slot(uint8(slot)))
}

func (c sessionControl) RemovePlayer(entity core.Entity) error { return c.a.dropPlayerLocked(entity) }

func (c sessionControl) Participants() []engine.SessionParticipant {
	var out []engine.SessionParticipant
	for slot := range parameter.MaxPlayers {
		e := c.a.world.Resources.Player.Slot(uint8(slot))
		if p, ok := c.a.world.Components.Cursor.GetComponent(e); ok {
			out = append(out, engine.SessionParticipant{Entity: e, Slot: p.Slot, Holder: p.Holder,
				Local: c.a.world.SimulatesLocally(e)})
		}
	}
	return out
}

// The cursor entity prevents an open menu from dropping a replacement in its slot.
func (a *App) dropPlayerLocked(entity core.Entity) error {
	if !a.world.IsSessionCoordinator() {
		return errors.New("only the host can drop other players or their bots")
	}
	p, ok := a.world.Components.Cursor.GetComponent(entity)
	if !ok {
		return errors.New("that player has already left")
	}
	if a.world.SimulatesLocally(entity) {
		return errors.New("use :q to leave your own session")
	}
	a.world.PushEventFull(event.EventParticipantDeparted,
		&event.ParticipantDepartedPayload{Participant: p.PeerID, Slot: p.Slot, Dismissed: true},
		event.OriginSession, core.DomainPlayer)
	return nil
}

func (c sessionControl) RequestSession(site string, players int, scenario string) error {
	e, err := network.ParseEndpoint(site)
	if err != nil {
		return err
	}
	if e.Scheme != network.SchemeSite {
		return errors.New("allocator site must be an http:// or https:// URL")
	}
	if players < 0 || players > parameter.MaxPlayers {
		return fmt.Errorf("players must be 0 (server default) or 1..%d", parameter.MaxPlayers)
	}
	return c.a.joinSessionLocked(site, players, scenario)
}

func (c sessionControl) AddBot(graph string) error {
	if c.a.dialling.Load() {
		return errors.New("a join is already being dialled")
	}
	specs, err := BotSpecs(cmp.Or(graph, DefaultBotGraph))
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New("no bots to add; name a count of 1 or more")
	}
	for _, spec := range specs {
		if err := c.a.addSeatLocked(spec); err != nil {
			return err
		}
	}
	return nil
}

func (c sessionControl) ChangeScenario(name string) (bool, error) {
	return c.a.changeScenarioLocked(name)
}

// The embedded scenario answers to the main name, which is how a row restarts it.
func (c sessionControl) Scenarios() ([]string, string) {
	current := c.a.scenario.Name
	if current == resource.EmbeddedLabel {
		current = paths.MainScenarioName
	}
	return resource.Scenarios(c.a.cfg.Resources), current
}

// Validate before teardown; a different scenario needs a new App and metric set.
// Caller holds updateMutex.
func (a *App) changeScenarioLocked(name string) (bool, error) {
	// The session's rule first: it holds whatever shape this runtime is, and it is
	// the more useful of the two answers to a participant that asked.
	port := a.sessionTransportLocked()
	if port != nil && !a.world.IsSessionCoordinator() {
		return false, errors.New("only the host can change a live session's scenario")
	}
	if a.cfg.Mode != ModePlay {
		return false, fmt.Errorf("%s mode has no restart loop", a.cfg.Mode)
	}
	// A scenario received from a coordinator answers its own name and no other.
	o := a.cfg.Resources
	o.Scenario, o.Embedded = name, false
	if o.Provided != nil && o.Provided.Name != name {
		o.Provided = nil
	}
	sc, err := resource.ValidateScenario(o)
	if err != nil {
		return false, err
	}
	if sc.Digest() == a.scenario.Digest() {
		return false, nil // already running these bytes; the caller resets instead
	}
	if err := a.playersFit(sc); err != nil {
		return false, err
	}
	req := &restartRequest{Scenario: name}
	if port != nil {
		// Where this run's participants come back to. The address it already
		// listens on when it opened the session; the one it advertised when it
		// inherited the session from somebody else. A run with neither has no door
		// to offer, and saying so is better than sending its peers to an address
		// that stopped answering; its own bots need none, the next run reseats them.
		req.Host = a.cfg.HostAddress
		if req.Host == "" {
			req.Host = a.reach.DeclaredAddr()
		}
		if req.Host == "" && !a.seatsOnly() {
			return false, errors.New("this participant has no address its session can return to")
		}
	}
	if !a.replaceWith(req) {
		return false, errRunReplaced
	}
	if port != nil {
		// Said before this run's transport goes, because afterwards there is nobody
		// to say it to. All it carries is where to come back: the offer a
		// participant is given when it redials names what is being played.
		port.Broadcast(uint8(network.MsgSessionRestart), []byte(req.Host))
	}
	a.log.Info("app", "msg", "scenario change requested",
		"scenario", sc.Name, "digest", sc.Short(), "host", req.Host)
	return true, nil
}

// A restart follows the authority's current address, which may have migrated.
// Called under the world lock; the restart loop consumes the atomic request.
func (a *App) receiveSessionRestart(from uint32, addr string) {
	if a.cfg.Mode != ModePlay || !a.sessionRoleLocked().joined() {
		return // a coordinator hears its own broadcast back on a mesh; a driven run has no loop
	}
	if a.replaceWith(&restartRequest{Rejoin: true, Join: addr}) {
		a.log.Info("session", "msg", "session restarting", "authority", from, "dial", addr)
	}
}

// joinLocked dials target while this run plays on, and replaces the run only once
// the host has admitted it: a refusal leaves the game as it was. A run left alone
// by its session may join another, and so may one whose session is its own bots',
// which the next run seats in the new one. Caller MUST hold updateMutex.
func (a *App) joinLocked(target string) error {
	return a.joinSessionLocked(target, 0, "")
}

// sessionRole is what a run is to its session, read from what the session wrote
// rather than kept beside it. Who authors is not part of it but the authority's,
// so a guest that inherited the term is a guest that authors.
type sessionRole uint8

const (
	roleSolo   sessionRole = iota // in no session
	roleSeats                     // in one it opened for its own bots, with no door
	roleHost                      // coordinating one others can join
	roleServer                    // coordinating one it plays no part in
	roleGuest                     // in one it joined
	roleLeft                      // in one it joined, which has nobody left but its own bots
)

const inSession = "this run is already in a session"

// sessionRoles is why each role refuses :join and :host, empty where it takes them.
var sessionRoles = [...]struct{ join, host string }{
	roleSolo:   {"", ""},
	roleSeats:  {"", ""},
	roleHost:   {inSession, "this run is already hosting"},
	roleServer: {inSession, "this run is already hosting"},
	roleGuest:  {inSession, inSession},
	roleLeft:   {"", inSession},
}

func (r sessionRole) joined() bool { return r == roleGuest || r == roleLeft }

// sessionRoleLocked is the role this run holds now. Caller MUST hold updateMutex.
func (a *App) sessionRoleLocked() sessionRole {
	a.sessionMu.Lock()
	opened := a.midRunPort != nil
	a.sessionMu.Unlock()
	switch {
	case a.cfg.Mode.Serves():
		return roleServer
	case a.sessionTransportLocked() == nil:
		return roleSolo
	case a.cfg.JoinAddress != "" && a.seatsOnly():
		return roleLeft
	case a.cfg.JoinAddress != "":
		return roleGuest
	case opened && a.cfg.HostAddress == "":
		return roleSeats
	}
	return roleHost
}

func (a *App) joinErrorLocked() error {
	if a.cfg.Mode != ModePlay {
		return fmt.Errorf("%s mode has no restart loop", a.cfg.Mode)
	}
	if why := sessionRoles[a.sessionRoleLocked()].join; why != "" {
		return errors.New(why)
	}
	if a.dialling.Load() {
		return errors.New("a join is already being dialled")
	}
	return nil
}

func (a *App) joinSessionLocked(target string, players int, scenario string) error {
	if err := a.joinErrorLocked(); err != nil {
		return err
	}
	// A site is asked for a session off the lock, since it answers once the pod is up.
	site, err := network.ParseEndpoint(target)
	next := a.cfg
	if err == nil && site.Scheme != network.SchemeSite {
		if next, err = a.cfg.joining(target); err == nil {
			err = next.Validate()
		}
	}
	if err != nil {
		return err
	}
	if !a.dialling.CompareAndSwap(false, true) {
		return errors.New("a join is already being dialled")
	}
	a.ctx.SetStatusMessage("Joining "+target+"...", parameter.StatusNotice)
	a.log.Info("session", "msg", "join requested", "target", target)
	core.Go(func() {
		defer a.dialling.Store(false)
		link, err := target, error(nil)
		if site.Scheme == network.SchemeSite {
			if link, err = network.RequestSession(site.Addr, players, scenario); err == nil {
				a.ctx.SetStatusMessage("Joining "+link+"...", parameter.StatusNotice)
				if next, err = next.joining(link); err == nil {
					err = next.Validate()
				}
			}
		}
		var d *joinDial
		if err == nil {
			d, err = dialJoin(next)
		}
		switch {
		case err != nil:
			a.log.Warn("session", "msg", "join failed; playing on", "target", target, "error", err.Error())
			a.ctx.SetStatusMessage("Join failed: "+err.Error(), parameter.StatusAlert)
		case !a.replaceWith(&restartRequest{Join: link, dialled: d}):
			d.abandon(errors.New("the run that dialled is being replaced"))
		}
	})
	return nil
}

// The authority policies a host names: what losing the authoring participant
// does. Host ends the session there; migrate hands it to the next survivor.
const (
	AuthorityHost    = "host"
	AuthorityMigrate = "migrate"
)

// BeginHosting opens a running instance to participants, for a caller that holds
// no lock. The operator command path reaches beginHostingLocked instead.
func (a *App) BeginHosting(addr string) error {
	a.Settle()
	var err error
	a.world.RunSafe(func() { err = a.beginHostingLocked(addr, "") })
	return err
}

func (a *App) hostErrorLocked() error {
	if !buildHasSocketNetwork {
		return errors.New("host: a browser can join a session but not host one")
	}
	if a.dialling.Load() {
		return errors.New("host: a join is already being dialled")
	}
	// A session this run opened without a door holds only its bots: :host opens
	// that one to others rather than a second.
	if why := sessionRoles[a.sessionRoleLocked()].host; why != "" {
		return errors.New("host: " + why)
	}
	return nil
}

// The world lock keeps admission from reading a partly attached session.
func (a *App) beginHostingLocked(addr, authority string) error {
	if err := a.hostErrorLocked(); err != nil {
		return err
	}
	if addr == "" {
		return errors.New("host: no address")
	}
	e, err := network.ParseEndpoint(addr)
	if err == nil {
		err = e.Listenable()
	}
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	addr = e.Addr
	fixed := a.cfg.FixedAuthority
	switch authority {
	case "":
	case AuthorityHost, AuthorityMigrate:
		a.cfg.FixedAuthority = authority == AuthorityHost
	default:
		return fmt.Errorf("host: authority %q is not %q or %q", authority, AuthorityHost, AuthorityMigrate)
	}

	// Bound before anything is written, so a port this run cannot have leaves it as
	// it was; a startup -host bound it before the terminal started.
	ln := a.cfg.hostListener
	a.cfg.hostListener = nil
	if ln == nil {
		if ln, err = network.Listen(addr, network.DebugConfig(network.RoleHost, addr)); err != nil {
			a.cfg.FixedAuthority = fixed
			return fmt.Errorf("host %s: %w", addr, err)
		}
	}
	if a.sessionTransportLocked() == nil {
		if err := a.openSessionLocked(); err != nil {
			_ = ln.Close()
			a.cfg.FixedAuthority = fixed
			return err
		}
	}
	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()

	if err := port.Serve(ln); err != nil {
		_ = ln.Close()
		a.cfg.FixedAuthority = fixed
		return fmt.Errorf("host: %w", err)
	}
	// A dial already in reads these only once it has the world lock this call holds.
	// The policy is the session's from here: only this run's bots were offered one.
	a.cfg.HostAddress = addr
	a.authority.Pin(a.cfg.FixedAuthority)
	a.log.Info("session", "msg", "hosting opened mid-run",
		"address", ln.Addr().String(), "tick", a.Position().Tick, "capacity", a.sessionCapacity()+1)
	a.ctx.SetStatusMessage("Hosting on "+ln.Addr().String(), parameter.StatusNotice)
	return nil
}

// openSessionLocked opens a session this run coordinates, with no door: its bots'
// streams it opens itself, and :host opens the door. Caller MUST hold updateMutex.
func (a *App) openSessionLocked() error {
	// Opening a session writes the world — owners, the latch, the lead — so a
	// journal records the write as it records a join's, and a replay makes it.
	journal := a.world.Resources.Event.Queue.Journal()
	place, mark := a.Position(), journal.Mark()
	var before snapshot.SharedCapture
	if journal != nil {
		var err error
		if before, err = a.captureSharedLocked(); err != nil {
			return fmt.Errorf("host: %w", err)
		}
	}

	// The roster ceiling is the port's from here: an unset -players already means
	// the whole roster, and a run that opens a session mid-game has no lobby for the
	// flag's other meaning.
	port := network.NewSocketPort(a.hostNetworkConfig())

	// Armed before any stream arrives: a run that opens a session mid-game has no
	// startup lobby, so every dial it ever sees is a mid-run join.
	a.lateJoins.Store(true)

	a.sessionMu.Lock()
	a.midRunPort = port
	a.sessionRoster = []network.RosterEntry{{ID: engine.CoordinatorParticipant, Slot: 0}}
	roster := slices.Clone(a.sessionRoster)
	a.sessionMu.Unlock()

	// The session's world is written between two ticks, so the run takes up settle
	// group 0 as every install leaves it, and as its replay's install of it does.
	a.world.Resources.Event.Queue.RebaseStamp(place.Run, place.Tick)

	// A lobby binds this through the roster it closes on; a session opened mid-run
	// has no lobby, and an unattributed cursor is one no successor can see leave.
	a.bindCursorOwnersLocked(roster, engine.CoordinatorParticipant)

	// The run that opens a session authors its first term. Everything downstream
	// reads authorship from here rather than from the identity the handshake
	// assigns, which is what lets a later handoff move it.
	a.openAuthorityLocked(network.SessionOffer{
		Anchor: a.joinAnchorLocked(), Host: engine.CoordinatorParticipant, Assigned: engine.CoordinatorParticipant,
		Term: network.FirstTerm, Roster: roster, FixedAuthority: a.cfg.FixedAuthority,
	}, engine.CoordinatorParticipant)

	// Attaching latches the world as shared (D-14) and installs the departure and
	// digest hooks; activating closes the pre-session crossing window so this
	// instance's own artifacts start taking the session's playout lead.
	a.attachTransportLocked(port)
	a.activateNetworkSessionLocked()

	_ = port.Start() // no listener, so starting only runs it

	// The authority's cadence starts with the session. Nothing is published while
	// no peer is connected — publish returns on an empty roster — so a host waiting
	// alone pays a ticker and no world reads.
	a.corrections.StartPump()
	if journal != nil {
		after, err := a.captureSharedLocked()
		if err == nil {
			err = a.journalWritten(journal, place, mark, a.world.LocalParticipant(), a.barrierLocked(), before, after)
		}
		if err != nil {
			a.journalLostLocked(journal, err)
		}
	}

	a.log.Info("session", "msg", "session opened", "tick", a.Position().Tick)
	return nil
}

// sessionTransport returns the attached endpoint, nil when this run has none. The
// question is whether a transport exists, not whether a peer is on it: a host
// waiting alone is in a session and a second :host would open a second one.
func (a *App) sessionTransport() engine.NetworkPort {
	var port engine.NetworkPort
	a.world.RunSafe(func() { port = a.sessionTransportLocked() })
	return port
}

// sessionTransportLocked is the same read for a caller that holds the world lock.
// Caller MUST hold updateMutex.
func (a *App) sessionTransportLocked() engine.NetworkPort {
	if r := a.world.Resources.Network; r != nil {
		return r.Port
	}
	return nil
}

// SessionSummary is a one-line description of what this run is part of.
func (a *App) SessionSummary() string {
	var out string
	a.world.RunSafe(func() { out = a.sessionSummaryLocked() })
	return out
}

// sessionSummaryLocked is the same for the operator command path.
// Caller MUST hold updateMutex.
func (a *App) sessionSummaryLocked() string {
	if a.sessionTransportLocked() == nil {
		if !buildHasSocketNetwork {
			return "Solo run; :join <link> joins a session"
		}
		return "Solo run; :host <addr> opens it to participants, :join <target> joins one"
	}
	peers := a.world.Resources.Status.Ints.Get("network.peers").Load()
	participant := a.world.LocalParticipant()
	// The role this run holds now: a guest that inherited the term hosts the session
	// at the address it declared, which its peers redial, not the one it dialled. A
	// harness binds no address, so the word stands alone rather than trailing a blank.
	where, addr := "hosting", a.cfg.HostAddress
	switch {
	case !a.sessionRoleLocked().joined():
	case a.world.IsSessionCoordinator():
		addr = a.reach.DeclaredAddr()
	default:
		where, addr = "joined", a.cfg.JoinAddress
	}
	if addr != "" {
		where += " " + addr
	}
	slot := a.world.Resources.Player.LocalSlot()
	cursor := fmt.Sprintf("slot %d", slot)
	if slot == parameter.NoPlayerSlot {
		cursor = "no cursor"
	}
	reg := a.world.Resources.Status
	// The D-14 latch is a fact about the run rather than a thing to watch
	latch := "map open"
	if reg.Bools.Get("network.map_latched").Load() {
		latch = "map latched"
	}
	line := fmt.Sprintf("Session %s, peer %d (%s), %d peer link(s), tick %d, %s",
		where, participant, cursor, peers, a.Position().Tick, latch)
	if a.authority != nil {
		if s := a.authority.Summary(); s != "" {
			line += "; " + s
		}
	}
	// Reachability, which is what decides whether losing the authority moves the
	// session or forks it. The chain is the address book a survivor dials down, so
	// its size is how many peers can be reached at all.
	if n := reg.Ints.Get("network.chain").Load(); n > 0 {
		line += fmt.Sprintf(", %d confirmed reachable", n)
	}
	if reg.Bools.Get("network.listening").Load() {
		line += ", listening"
	}
	if reg.Bools.Get("network.host_lost").Load() {
		return line + "; HOST LOST, continuing locally from the last authoritative state"
	}

	// The operating point, read from the published cells rather than from the
	// scheduler itself. This runs under the world lock and the scheduler takes it
	// on the other side of its own — a capture is a world read — so reading the
	// atomics is not a shortcut here, it is the only order that cannot deadlock.
	// With no peer link there is no operating point: the cells are cleared with it.
	if peers == 0 {
		return line
	}
	// The link line is what the badge collapses: the slowest link's round trip,
	// variation and delivery rate with the peer it belongs to, and the worst loss.
	if rtt := reg.Ints.Get("network.link_rtt_ms").Load(); rtt > 0 {
		line += fmt.Sprintf("; slowest link %s ±%dms, %sB/s (peer %d), loss up to %d%%",
			status.FormatLatency(rtt),
			reg.Ints.Get("network.link_jitter_ms").Load(),
			status.FormatCount(reg.Ints.Get("network.link_bps").Load()),
			reg.Ints.Get("network.link_peer").Load(),
			reg.Ints.Get("network.link_loss_pct").Load())
	}
	cadence := reg.Ints.Get("snapshot.cadence_ticks").Load()
	if cadence == 0 {
		return line
	}
	links := reg.Ints.Get("snapshot.cadence_links").Load()
	state := "nominal"
	switch {
	case reg.Bools.Get("snapshot.cadence_floor_breached").Load():
		state = "BELOW THE CONVERGENCE FLOOR"
	case reg.Bools.Get("snapshot.cadence_constrained").Load():
		state = fmt.Sprintf("constrained on %d of %d", reg.Ints.Get("snapshot.cadence_constrained_links").Load(), links)
	}
	// Keyframe period in ticks only: the floor it is checked against is stated in
	// ticks. Uplink and floor are totals over the links the plan covers.
	return line + fmt.Sprintf(
		"; cadence %d ticks, keyframe %d ticks, uplink %sB/s over %d link(s), floor %sB/s, %s",
		cadence,
		reg.Ints.Get("snapshot.cadence_keyframe_period_ticks").Load(),
		status.FormatCount(reg.Ints.Get("snapshot.cadence_uplink_bps").Load()),
		links,
		status.FormatCount(reg.Ints.Get("snapshot.cadence_floor_bps").Load()),
		state)
}

// releaseMidRunJoiner completes the gate for a participant the accept loop just
// admitted. It runs on the accept goroutine, so it must not assume the world lock is
// free and must not hold it longer than one capture. A tick-zero lobby releases
// everyone together and does not come through here.
func (a *App) releaseMidRunJoiner(id network.PeerID) {
	// One at a time. The handshakes that reach here run concurrently, and this gate
	// waits on a ready count that is cumulative over the session: two of them at
	// once could not tell which joiner had confirmed, and the link measurement each
	// takes would be timed against the other's transfer.
	a.midRunGate.Lock()
	defer a.midRunGate.Unlock()

	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()
	if port == nil {
		// A dedicated host never opened a socket of its own: its endpoint is the
		// one NetworkService contributed for -serve.
		port, _ = a.socketPort()
	}
	if port == nil {
		return
	}
	if err := a.sendMidRunGate(port, id); err != nil {
		a.log.Warn("session", "msg", "mid-run join failed", "peer", id, "error", err.Error())
		// The stream is already a peer by the time this runs, so refusing the join
		// means dropping it: a participant holding a handshake it could not finish
		// would otherwise stay in the session receiving crossings for a world it
		// never installed. Dropping it runs the ordinary departure path, which is
		// what returns its identity to the pool.
		port.Disconnect(uint32(id))
		a.releaseParticipant(id)
	}
}

// sendMidRunGate sends one joiner the closed roster and the world it names, then
// crosses its arrival. The world is the cadence's keyframe rather than a read taken
// for this join, so two joins arriving together share one read.
func (a *App) sendMidRunGate(port *network.SocketPort, id network.PeerID) error {
	offer, err := a.midRunOffer(id)
	if err != nil {
		return err
	}
	tick, bytes, took, err := a.gateJoiner(port, offer, a.corrections.KeyframeAt)
	if err != nil {
		return err
	}
	// Refused *after* the install rather than before it, because the install is
	// what completes the measurement. A participant refused here is dropped by the
	// caller and its identity returned to the pool, which is the same unwind a
	// join that could not finish its gate takes.
	if err := a.corrections.AdmitLink(port, id, bytes, took); err != nil {
		return err
	}

	assignment, _ := offer.Entry(id)
	a.crossParticipantArrival(id, assignment.Slot)
	a.log.Info("session", "msg", "mid-run participant admitted",
		"peer", id, "slot", assignment.Slot, "snapshot_tick", tick, "bytes", bytes)
	return nil
}

// gateJoiner releases the participant offer is addressed to into the world at a
// playout lead past the admission, waits for its install, and says which world and
// how long its transfer took. Fresh enough is that lead, not the current tick: an
// epoch flushed just before the admission reaches nobody and is in no capture taken
// at that tick.
func (a *App) gateJoiner(port *network.SocketPort, offer network.SessionOffer,
	world func(minTick uint64, deadline time.Time) ([]byte, uint64, error),
) (tick uint64, bytes int, took time.Duration, err error) {
	minTick := a.Position().Tick + parameter.NetworkBarrierDelayTicks
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound
	body, tick, err := world(minTick, deadline)
	if err != nil {
		return 0, 0, 0, err
	}
	began, err := sendGate(port, offer, tick, body, offer.Assigned)
	if err == nil {
		err = a.awaitJoinerReady(port, offer.Assigned)
	}
	return tick, len(body), time.Since(began), err
}

// sendGate releases each participant named into the world body encodes at tick: the
// roster addressed to it, then the world, one encoding shared by all. It returns when
// the last transfer started; a transfer is the measurement, timed from its first
// chunk to the joiner's confirmation, since nothing else on a fresh link has pushed
// enough bytes to say what it carries.
func sendGate(port *network.SocketPort, offer network.SessionOffer, tick uint64, body []byte, to ...network.PeerID) (time.Time, error) {
	offer.SnapshotTick, offer.SnapshotBytes = tick, len(body)
	chunks, err := network.EncodeSnapshotChunks(0, tick, body)
	if err != nil {
		return time.Time{}, err
	}
	var began time.Time
	for _, id := range to {
		offer.Assigned = id
		start, err := json.Marshal(offer)
		if err != nil {
			return time.Time{}, err
		}
		if !port.Send(uint32(id), uint8(network.MsgStart), start) {
			return time.Time{}, fmt.Errorf("could not release participant %d", id)
		}
		began = time.Now() // [wall] a link measurement, not a game clock
		for i, chunk := range chunks {
			if !port.Send(uint32(id), uint8(network.MsgStateSnapshot), chunk) {
				return time.Time{}, fmt.Errorf("could not send capture chunk %d/%d to participant %d", i+1, len(chunks), id)
			}
		}
	}
	return began, nil
}

// midRunOffer allocates nothing: the acceptor already assigned this participant, so
// the roster is closed as it stands and addressed to it.
func (a *App) midRunOffer(id network.PeerID) (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	offer := a.offerLocked(anchor, id)
	return offer, offer.Validate()
}

// awaitJoinerReady waits for the joiner to confirm it installed the world.
//
// The wait is bounded and the bound is the point: a participant that cannot install
// and answer within it is one whose crossings would arrive after the ticks they
// name, and admitting it would trade a failed join for a divergence.
func (a *App) awaitJoinerReady(port *network.SocketPort, id network.PeerID) error {
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound, not a game one
	for time.Now().Before(deadline) {
		if port.Confirmed(uint32(id)) {
			return nil
		}
		if !port.Connected(uint32(id)) {
			return fmt.Errorf("participant %d dropped during its join", id)
		}
		select {
		case <-port.Changes():
		case <-time.After(2 * time.Millisecond):
		}
	}
	return fmt.Errorf("participant %d did not confirm its install within %s",
		id, parameter.NetworkJoinReadyTimeout)
}

// crossParticipantArrival announces a mid-run arrival as a D-3 crossing rather than
// as a local reaction to a connect: the cursor it creates is a shared entity, so
// every instance must create it at one agreed tick or their creation order diverges
// (D-11). The coordinator is the only producer, as it is for a departure.
func (a *App) crossParticipantArrival(id network.PeerID, slot uint8) {
	a.world.RunSafe(func() { a.crossArrivalLocked(id, slot) })
}

// crossArrivalLocked is crossParticipantArrival under the world lock. Caller MUST
// hold updateMutex.
func (a *App) crossArrivalLocked(id network.PeerID, slot uint8) {
	a.sessionMu.Lock()
	i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id })
	if i < 0 {
		a.sessionMu.Unlock()
		return
	}
	holder := a.sessionRoster[i].Holder
	a.sessionMu.Unlock()
	a.world.PushEventFull(event.EventParticipantJoined,
		&event.ParticipantJoinedPayload{Participant: uint32(id), Slot: slot, Holder: uint32(holder)},
		event.OriginSession, core.DomainPlayer)
}

// resumeJoinedSession hands the traffic the gate held to the port and closes the gap
// between the installed world and the tick the session has reached by simulating it.
// The gap is the transfer and the install, so it is a function of world size and
// link speed; left open it is permanent, and every crossing goes out k ticks late.
// Call after the transport takes the stream and before game time is released.
func (a *App) resumeJoinedSession() error {
	if a.pendingJoin == nil {
		return nil
	}
	port, err := a.injectPort()
	if err != nil {
		return err
	}
	held := a.pendingJoin.Deferred()
	from := uint32(a.pendingJoin.Peer())
	for _, msg := range held {
		port.Inject(from, uint8(msg.Type), msg.Payload)
	}

	// A tick-zero lobby has no gap by construction: the host is frozen at tick zero
	// until every participant is ready, so it has produced nothing, and a probe tick
	// here would put this instance one tick ahead of a session that has not started.
	if !a.sessionOffer.CarriesSnapshot() || a.sessionOffer.SnapshotTick == 0 {
		a.reportJoinLag(0)
		return nil
	}

	// The gap is only partly readable from what the gate held: epochs closed during
	// the install sat in the socket until the port started, and the barrier learns of
	// those only once something drains them. So it closes by rounds — catch up to the
	// newest tick known, let that draining reveal the next, stop when it stops moving.
	caught := uint64(0)
	for range joinCatchUpRounds {
		local := a.Position().Tick
		target := max(newestHeldEpoch(held), a.awaitSessionTick(local))
		if target <= local {
			break
		}
		step := target - local
		if caught+step > parameter.NetworkJoinCatchUpTicks {
			return joinBehind("it is more than %d ticks ahead of the world it sent", parameter.NetworkJoinCatchUpTicks)
		}
		a.scheduler.RunTicks(int(step))
		caught += step
	}
	return a.finishCatchUp(held, caught)
}

// awaitSessionTick drains the transport until a peer epoch newer than local shows
// up, or the wait runs out. Every tick closes an epoch, so on a live session the
// wait is bounded by one tick interval; the ceiling is there for the session that
// has stopped producing, where the honest answer is "no newer tick" rather than a
// hang.
func (a *App) awaitSessionTick(local uint64) uint64 {
	deadline := time.Now().Add(joinEpochWait) // [wall] a link bound, not a game one
	for {
		a.world.RunSafe(func() {
			for _, sys := range a.world.Systems() {
				if d, ok := sys.(interface{ DrainPeers() }); ok {
					d.DrainPeers()
				}
			}
		})
		if observed := a.observedSessionTick(); observed > local {
			return observed
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(joinEpochPoll)
	}
}

// finishCatchUp reports what the join cost and refuses one it could not close.
func (a *App) finishCatchUp(held []*network.Message, caught uint64) error {
	remaining := a.sessionLagTicks()
	a.reportJoinLag(remaining)
	a.log.Info("session", "msg", "join caught up", "held_frames", len(held),
		"caught_up_ticks", caught, "tick", a.Position().Tick, "lag_ticks", remaining)
	a.telemetry.CatchUp.Store(int64(caught))
	if remaining > parameter.NetworkJoinLagTicks {
		return joinBehind("still %d ticks after catching up, lead is %d", remaining, parameter.NetworkJoinLagTicks)
	}
	return nil
}

// joinBehind refuses a join that could not close the gap to the session while it
// installed, a matter of load rather than of the session; a seat retries it.
func joinBehind(format string, args ...any) error {
	return &network.Refusal{Code: network.RefusalBehind, Reason: "join: behind the session: " + fmt.Sprintf(format, args...)}
}

// joinCatchUpRounds is how many times a joining participant re-reads the session's
// tick while closing the gap. Each round drains what the previous one revealed;
// three is one more than the two windows a join actually has — the frames the gate
// held, and the frames the socket held while the world was being installed.
const joinCatchUpRounds = 3

// joinEpochWait is how long one catch-up round waits for the session's next epoch,
// and joinEpochPoll how often it looks. The wait is two tick intervals: a live
// session closes an epoch every tick, so anything longer is a session that has
// stopped rather than one this instance has not heard from yet.
const (
	joinEpochWait = 2 * parameter.GameUpdateInterval
	joinEpochPoll = time.Millisecond
)

// injectPort returns the attached endpoint as a frame injector. It is read from the
// world rather than from the service, because a harness attaches its own.
func (a *App) injectPort() (interface {
	Inject(peer uint32, msgType uint8, payload []byte)
}, error) {
	injector, ok := a.sessionTransport().(interface {
		Inject(peer uint32, msgType uint8, payload []byte)
	})
	if !ok {
		return nil, errors.New("join: the attached endpoint cannot replay the gate's held frames")
	}
	return injector, nil
}

// HostAddr is the address this run is hosting on, empty when it listens on none it
// opened itself. The bound form is reported, so a run opened on port zero names the
// port it actually got.
func (a *App) HostAddr() string {
	a.sessionMu.Lock()
	port := a.midRunPort
	a.sessionMu.Unlock()
	if port == nil {
		return ""
	}
	if bound := port.Addr(); bound != nil {
		return bound.String()
	}
	return a.cfg.HostAddress
}

// newestHeldEpoch is the highest production epoch among the frames the gate held.
// A frame that does not decode is skipped: the barrier will count it as a drop when
// it drains the same bytes, and guessing a tick from a broken one is worse than
// reading a smaller gap and measuring what is left.
func newestHeldEpoch(held []*network.Message) uint64 {
	var newest uint64
	for _, msg := range held {
		if msg.Type != network.MsgEvent {
			continue
		}
		batch, err := event.DecodeWireBatch(msg.Payload)
		if err != nil {
			continue
		}
		if batch.ProducedTick > newest {
			newest = batch.ProducedTick
		}
	}
	return newest
}

// observedSessionTick is the newest production epoch any peer has been seen
// closing. Every tick closes one, empty or not, so it is the session's tick as far
// as this instance's barrier has observed it.
func (a *App) observedSessionTick() uint64 {
	var newest uint64
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if e, ok := sys.(interface{ NewestPeerEpoch() uint64 }); ok {
				if n := e.NewestPeerEpoch(); n > newest {
					newest = n
				}
			}
		}
	})
	return newest
}

// sessionLagTicks is how far behind the observed session tick this instance stands.
func (a *App) sessionLagTicks() uint64 {
	newest := a.observedSessionTick()
	local := a.Position().Tick
	if newest <= local {
		return 0
	}
	return newest - local
}

// reportJoinLag publishes the measured lag beside the other transport counters.
func (a *App) reportJoinLag(ticks uint64) {
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if r, ok := sys.(interface{ ReportJoinLag(uint64) }); ok {
				r.ReportJoinLag(ticks)
			}
		}
	})
}

// closeMidRunPort releases a socket this run opened for itself.
func (a *App) closeMidRunPort() {
	a.sessionMu.Lock()
	port := a.midRunPort
	a.midRunPort = nil
	a.sessionMu.Unlock()
	if port != nil {
		_ = port.Close()
	}
}
