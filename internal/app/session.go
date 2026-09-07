package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// hostParticipantID is the coordinator. It allocates every other identity, so it
// takes the first one itself and hands out the rest in arrival order.
const hostParticipantID network.PeerID = 1

var errSessionCanceled = errors.New("network session canceled")

// ErrSessionStarting refuses a dial that lands in the window between the startup
// lobby closing and the mid-run gate opening. It is distinguishable for the same
// reason ErrSessionHandoff is: the dialer's answer is to retry, not to give up.
var ErrSessionStarting = errors.New("session is starting; retry")

// ErrSessionEnding refuses a dial to a session that is draining or has expired. It
// is the opposite of ErrSessionStarting and must not be confused with it: this run
// is leaving, so retrying against it is exactly the wrong answer. An allocator
// reading it should place the participant somewhere else.
var ErrSessionEnding = errors.New("session is ending")

// errSessionExpired ends a lobby whose allocated window closed before a guest
// reached it. It is a clean end rather than a failure — the session did what it was
// told to do with a slot nobody claimed — so the caller reports it and exits zero.
var errSessionExpired = errors.New("session lifetime expired")

// errLobbyAbandoned ends a startup gate whose guest left before confirming it
// installed the world. On an interactive host that is a failure to report to the
// person who started it. On a dedicated one it is not: nobody is watching, the
// session has no participants and never had any, and the honest end is the same
// clean exit an unclaimed window takes.
var errLobbyAbandoned = errors.New("participant disconnected during startup")

// newSessionApp resolves the startup handshake before a joining App draws a
// seed. Interactive play and authored headless scripts share this construction.
func newSessionApp(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.JoinAddress != "" {
		return newJoiningApp(cfg)
	}
	if cfg.HostAddress != "" {
		return newHostingApp(cfg)
	}
	return New(cfg)
}

// newHostingApp installs a tick-zero acceptor before the service is initialized.
// The map latch is engaged for the whole run: the anchor a joiner adopts names
// these bounds, and a crop landing between that offer and the start gate would move
// them under a participant that has already built its world on them (D-14).
func newHostingApp(cfg Config) (*App, error) {
	cfg.LockMap = true
	a, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// hostNetworkConfig captures the App before the service starts its accept loop.
func (a *App) hostNetworkConfig() *network.Config {
	netCfg := network.DebugConfig(network.RoleHost, a.cfg.HostAddress)
	netCfg.ParticipantID = hostParticipantID
	netCfg.MaxPeers = a.sessionCapacity()
	netCfg.OnError = logSessionError
	netCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign:  a.assignParticipant,
		Release: a.releaseParticipant,
		Admit:   a.admissions.admit,
		Report:  a.noteJoinerReport,
	}, netCfg.ConnectTimeout)
	// Every host outlives its guests, so every host admits a dial after its lobby
	// has closed: a dropped participant comes back into the slot its departure
	// released, through the one gate a reconnect has ever had. It used to be a
	// dedicated host's alone, which left every other shape accepting the stream,
	// sending no start record, and holding the dialer in a read it could not leave.
	// The hook answers nothing until the run arms it, because until then the gate
	// is the startup lobby's own.
	netCfg.OnAdmit = a.admitLateJoiner
	return netCfg
}

// admitLateJoiner gates a dial that arrives after the startup lobby closed.
func (a *App) admitLateJoiner(id network.PeerID) {
	if !a.lateJoins.Load() {
		return
	}
	// Before the gate, not after it: the gate waits for a capture a playout lead
	// ahead of the current tick, so a session parked for having nobody in it would
	// time out every dial that came to end that.
	a.resumeVacant()
	a.releaseMidRunJoiner(id)
}

// openMidRunJoins ends the lobby's closing window and arms the mid-run gate, in
// that order: a dial refused a moment ago retries into a gate that now exists.
//
// Every run that owns a frame loop calls it once its clock is running — interactive
// play, a dedicated host, and an authored script alike. The gate reads a capture a
// playout lead ahead of the current tick, so arming it over a clock that has not
// started would time out every dial it was meant to admit.
func (a *App) openMidRunJoins() {
	a.lateJoins.Store(true)
	a.lobbyClosing.Store(false)
}

// sessionCapacity is how many guests this host will ever hold, excluding itself.
//
// A dedicated host is excluded by construction rather than by subtraction: it
// holds no cursor, so the number is the number of guests and the roster is exactly
// those guests plus one cursorless coordinator. There -players is a ceiling and
// nothing else — a server with none named holds the whole roster, because a fleet
// host that had to be told how many people were coming would be a host that only
// ever served the number it was told.
//
// An interactive -host is the other shape: its lobby is a fixed party that starts
// together, so there the ceiling and the number it waits for are the same value.
func (a *App) sessionCapacity() int {
	n := a.cfg.Participants
	if a.cfg.Mode.Serves() {
		if n <= 0 {
			return parameter.MaxPlayers
		}
		return min(n, parameter.MaxPlayers)
	}
	if n < 2 {
		n = 2
	}
	if n > parameter.MaxPlayers {
		n = parameter.MaxPlayers
	}
	return n - 1
}

// guestCount is how many guests the roster currently holds. The coordinator of a
// dedicated host holds a roster entry and no cursor, so it is subtracted: a session
// consisting only of its coordinator is an empty one, which is the reading both the
// readiness probe and the lifetime policy need.
func (a *App) guestCount() int {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	n := len(a.sessionRoster)
	if n > 0 {
		n--
	}
	return n
}

// lobbyQuorum is how many guests the start gate waits for before it closes the
// lobby and releases tick zero.
//
// One, on a dedicated host. The session exists to be joined rather than to be
// assembled, so the first guest is what there is to wait for and every guest after
// it arrives through the mid-run gate — which is the same path a guest that
// dropped comes back through, and is therefore already the path that has to work.
// Waiting for a full roster instead would make the pod's readiness a function of
// how many people happened to want to play.
func (a *App) lobbyQuorum() int {
	if a.cfg.Mode.Serves() {
		return 1
	}
	return a.sessionCapacity()
}

// noteJoinerReport keeps the first geometry a guest reported. First rather than
// smallest, and the difference is the mid-run gate: guests arrive throughout the
// run, so "smallest" would mean shrinking the map under participants already
// playing on it — which D-14 forbids for the same reason a terminal may not crop a
// shared map. First is a number the session can commit to before it starts.
func (a *App) noteJoinerReport(_ network.PeerID, report network.JoinerReport) {
	if !report.Sized() {
		return
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if !a.firstJoiner.Sized() {
		a.firstJoiner = report
	}
}

// adoptLobbyGeometry sizes a dedicated host's map from its first guest.
//
// A server has no terminal, so without this it serves whatever Config.Normalize
// defaulted to — 80x24, which is a 77x21 map that every guest then adopts however
// large its own terminal is. The operator's -size still wins where it was given;
// this is only for the case where nobody said.
//
// It runs before the roster closes, so the bounds it produces are the ones the
// offer names, the tick-zero capture contains, and every later guest adopts. A
// scenario that fixes its own map (crop off) is left alone: those bounds are the
// scenario's statement, not a stand-in for a terminal nobody has.
func (a *App) adoptLobbyGeometry() {
	if !a.cfg.Mode.Serves() || !a.cfg.geometryDefaulted {
		return
	}
	a.sessionMu.Lock()
	report := a.firstJoiner
	a.sessionMu.Unlock()
	if !report.Sized() || !engine.ViewportFits(report.Width, report.Height) {
		return
	}
	if report.Width == a.ctx.Width && report.Height == a.ctx.Height {
		return
	}

	var crop bool
	a.world.RunSafe(func() { crop = a.world.Resources.Config.CropOnResize })
	if !crop {
		return
	}

	// Through the two authorities rather than by writing Config: the resize is what
	// moves this instance's viewport, and the level setup is what moves the shared
	// map (D-14). Both are recorded events, so a replay of this run reaches the
	// same bounds the same way.
	a.Resize(report.Width, report.Height)
	var vw, vh int
	a.world.RunSafe(func() {
		cfg := a.world.Resources.Config
		vw, vh = cfg.ViewportWidth, cfg.ViewportHeight
	})
	a.SetupLevel(vw, vh, false, true)
	vlog.Info("app", "msg", "session sized from its first guest",
		"terminal_w", report.Width, "terminal_h", report.Height, "map_w", vw, "map_h", vh)
}

// hostSlot is the roster slot this instance takes for itself: the first one on an
// ordinary host, and none at all on a dedicated one.
func (a *App) hostSlot() uint8 {
	if a.cfg.Mode.Serves() {
		return parameter.NoPlayerSlot
	}
	return 0
}

// newJoiningApp receives the host anchor, adopts it, then constructs the App.
func newJoiningApp(cfg Config) (*App, error) {
	netCfg := network.DebugConfig(network.RolePeer, cfg.JoinAddress)
	netCfg.OnError = logSessionError
	// Before the world: a peer running a different protocol or a different
	// simulation is refused by the dial rather than after it has built one.
	netCfg.Identity = buildIdentity()
	pending, offer, err := network.DialSession(cfg.JoinAddress, netCfg)
	if err != nil {
		return nil, fmt.Errorf("join %s: %w", cfg.JoinAddress, err)
	}
	reject := func(cause error) (*App, error) {
		_ = pending.Complete(cause, network.JoinerReport{})
		_ = pending.Close()
		return nil, cause
	}

	cfg.networkConfig = pending.TransportConfig()
	cfg, err = ConfigForJoin(cfg, offer)
	if err != nil {
		return reject(err)
	}
	a, err := New(cfg)
	if err != nil {
		return reject(err)
	}
	a.pendingJoin = pending
	a.sessionOffer = offer
	// Identity now, world and roster at the start gate: a mismatched joiner must be
	// refused before the host spends the rest of the lobby waiting for it. The
	// position is deliberately not checked — the gate carries the host's world, so
	// what tick it has reached is no longer this instance's problem to reproduce.
	if err := a.JoinAt(offer.Anchor); err != nil {
		_ = pending.Complete(err, network.JoinerReport{})
		a.Close()
		return nil, err
	}
	// Reported after construction, which is the whole reason the acceptance carries
	// it rather than the dial: only now does this instance know the terminal it
	// got. A host with no geometry of its own uses it to size the session.
	if err := pending.Complete(nil, a.joinerReport()); err != nil {
		a.Close()
		return nil, fmt.Errorf("join reply: %w", err)
	}
	return a, nil
}

// assignParticipant allocates the next identity and returns the offer carrying it.
// One call per accepted connection: the roster is the lobby so far, and the roster a
// participant actually builds from arrives later, at the start gate.
func (a *App) assignParticipant() (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	// A dial that lands mid-succession is refused rather than admitted. The offer
	// this call would write names a term that is about to end, and a participant
	// admitted under one is in a session nobody owns: it holds a roster slot the
	// successor's record does not carry and receives an authority that has
	// already stopped publishing. The refusal is distinguishable so the joiner can
	// retry against whatever authority emerges.
	if a.authority != nil && a.authority.Migrating() {
		return network.SessionOffer{}, ErrSessionHandoff
	}
	// The same refusal for the same reason, one window earlier. Between the lobby
	// closing on its roster and the mid-run gate being armed there is no gate that
	// can serve a dial: the lobby's has already sent its offers, and the mid-run
	// one waits for a capture a clock that has not started never reaches. A dialer
	// admitted there would hold an identity and wait for a start it is not in.
	if a.lobbyClosing.Load() {
		return network.SessionOffer{}, ErrSessionStarting
	}
	// A draining or expired session refuses before it allocates anything. The
	// roster slot, the identity and the capture that would follow are all work
	// spent on a participant this process is about to stop authoring for, and the
	// refusal is what makes a drain a drain rather than a slower shutdown.
	if st := a.life.State(time.Now()); !st.Admit {
		return network.SessionOffer{}, fmt.Errorf("%w: %s", ErrSessionEnding, st.Reason)
	}

	limit := a.sessionCapacity() + 1
	if len(a.sessionRoster) == 0 {
		a.sessionRoster = []network.SessionParticipant{{ID: hostParticipantID, Slot: a.hostSlot()}}
	}
	if len(a.sessionRoster) >= limit {
		return network.SessionOffer{}, fmt.Errorf("session is full at %d participants", limit)
	}
	assigned := a.nextParticipantLocked()
	a.sessionRoster = append(a.sessionRoster, assigned)

	a.sessionOffer = a.offerLocked(anchor, assigned.ID)
	return a.sessionOffer, a.sessionOffer.Validate()
}

// nextParticipantLocked takes the lowest free identity and the lowest free slot, so
// a lobby that loses a joiner reuses its place rather than exhausting the roster.
func (a *App) nextParticipantLocked() network.SessionParticipant {
	taken := func(pick func(network.SessionParticipant) int, want int) bool {
		return slices.ContainsFunc(a.sessionRoster, func(p network.SessionParticipant) bool {
			return pick(p) == want
		})
	}
	var out network.SessionParticipant
	for id := 1; id <= parameter.MaxPlayers+1; id++ {
		if !taken(func(p network.SessionParticipant) int { return int(p.ID) }, id) {
			out.ID = network.PeerID(id)
			break
		}
	}
	for slot := range parameter.MaxPlayers {
		if !taken(func(p network.SessionParticipant) int { return int(p.Slot) }, slot) {
			out.Slot = uint8(slot)
			break
		}
	}
	return out
}

// releaseParticipant32 is the departure hook the network resource calls when a
// participant leaves, returning its identity so a later connection can take it.
func (a *App) releaseParticipant32(id uint32) { a.releaseParticipant(network.PeerID(id)) }

// releaseParticipant returns an identity whose handshake did not complete, or whose
// participant has left.
func (a *App) releaseParticipant(id network.PeerID) {
	if id == 0 || id == hostParticipantID {
		return
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessionRoster = slices.DeleteFunc(a.sessionRoster,
		func(p network.SessionParticipant) bool { return p.ID == id })
}

// offerLocked builds the offer addressed to one participant. Caller holds sessionMu,
// which is why the anchor is passed in rather than read here: JoinAnchor takes the
// world lock, and a departure released from under that lock takes sessionMu.
func (a *App) offerLocked(anchor event.JoinAnchor, assigned network.PeerID) network.SessionOffer {
	term := a.authorityTerm()
	if term == 0 {
		term = network.FirstTerm
	}
	return network.SessionOffer{
		Anchor:            anchor,
		Host:              a.authorityID(),
		Assigned:          assigned,
		Term:              term,
		Participants:      slices.Clone(a.sessionRoster),
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
		// Derived from the anchor this offer carries rather than read again, so
		// what the coordinator later compares a joiner's report against is exactly
		// what it offered — a reset between the two cannot turn a valid join into a
		// mismatch or the reverse.
		Identity: identityFromAnchor(anchor),
	}
}

// hostOffer closes the lobby and returns the roster every participant builds from.
// Addressed to the host itself; each joiner receives the same roster with Assigned
// set to its own identity.
func (a *App) hostOffer() (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if len(a.sessionRoster) == 0 {
		// No joiner ever arrived; describe the two-participant lobby this host opened.
		a.sessionRoster = []network.SessionParticipant{
			{ID: hostParticipantID, Slot: a.hostSlot()}, {ID: 2, Slot: 1},
		}
	}
	assigned := a.authorityID()
	for _, p := range a.sessionRoster {
		if p.ID != a.authorityID() {
			assigned = p.ID
			break
		}
	}
	a.sessionOffer = a.offerLocked(anchor, assigned)
	return a.sessionOffer, a.sessionOffer.Validate()
}

// startHostSession holds tick zero until every offered remote participant is ready.
func (a *App) startHostSession(signals <-chan os.Signal) error {
	port, err := a.socketPort()
	if err != nil {
		return err
	}
	return a.startHostSessionOn(port, signals)
}

// startHostSessionOn runs the production gate against the supplied endpoint.
//
// The lobby closes on a quorum rather than on a full roster: an interactive -host
// is a party that starts together and its quorum is its capacity, and a dedicated
// host starts on its first guest and takes the rest through the mid-run gate. What
// it closes *on* is the same either way — whoever is actually in the roster at
// that moment — because that roster is what every instance builds its cursors
// from, and a count agreed in advance is not the same thing as the participants
// that arrived.
//
// From the moment the roster is read until the caller opens the mid-run gate,
// dials are refused: a dialer admitted in that window would hold an identity and
// wait for a start gate no longer being sent.
func (a *App) startHostSessionOn(port *network.SocketPort, signals <-chan os.Signal) error {
	quorum, capacity := a.lobbyQuorum(), a.sessionCapacity()
	addr := a.cfg.HostAddress
	if bound := port.Addr(); bound != nil {
		addr = bound.String()
	}
	vlog.Info("app", "msg", "network host waiting",
		"address", addr, "quorum", quorum, "capacity", capacity)
	a.showStartupStatus(fmt.Sprintf("Hosting on %s; waiting for %d of up to %d participant(s) (Ctrl-C cancels)",
		addr, quorum, capacity))

	// The lobby is the one wait the first-guest window covers, so it is the one
	// wait that carries its deadline. Zero on an unbounded policy, and zero again
	// on the ready gate below — see the comment there.
	if err := a.waitForStartup(port, signals, quorum, false,
		a.life.State(time.Now()).Deadline,
		func() bool { return port.PeerCount() >= quorum }); err != nil {
		return err
	}

	// Before the roster closes, so the bounds this produces are the ones the offer
	// names and the tick-zero capture carries.
	a.adoptLobbyGeometry()

	a.lobbyClosing.Store(true)
	offer, err := a.hostOffer()
	if err != nil {
		return err
	}
	// The window closes here rather than a second later, when the serve loop takes
	// its first reading. The roster the lobby closed on is what satisfies it, and
	// everything between this point and the loop — the capture, the sends, the
	// ready gate — happens while the deadline would otherwise still be running.
	a.life.Observe(len(offer.Participants)-1, time.Now())
	// Whoever the roster closed on, not whoever was counted a moment ago: an
	// accepted dial can complete between the quorum being met and the roster being
	// read, and that participant is in the session.
	admitted := len(offer.Participants) - 1
	if admitted < quorum || admitted > capacity {
		return fmt.Errorf("host closed a lobby of %d guests, outside %d..%d",
			admitted, quorum, capacity)
	}
	if err := a.HostSession(offer); err != nil {
		return err
	}

	// The capture is taken after the roster closes and before the gate opens. Both
	// halves matter: the world a joiner installs has to already contain every cursor
	// the roster names, and it has to describe a tick no participant has moved past.
	// The tick-zero gate's capture is a keyframe like any other, and taking it
	// through the same path is what makes it the baseline the first delta names.
	body, tick, err := a.corrections.keyframeAt(0, time.Now().Add(parameter.NetworkJoinReadyTimeout))
	if err != nil {
		return err
	}
	offer.SnapshotTick, offer.SnapshotBytes = tick, len(body)
	chunks, err := network.EncodeSnapshotChunks(tick, body)
	if err != nil {
		return err
	}

	// Each joiner receives the closed roster addressed to itself, then the world it
	// names. Sending the same participant list and the same capture to everyone is
	// what makes shared creation order identical.
	for _, participant := range offer.Participants {
		if participant.ID == offer.Host {
			continue
		}
		addressed := offer
		addressed.Assigned = participant.ID
		start, err := json.Marshal(addressed)
		if err != nil {
			return err
		}
		if !port.Send(uint32(participant.ID), uint8(network.MsgStart), start) {
			return fmt.Errorf("host could not release participant %d", participant.ID)
		}
		for i, chunk := range chunks {
			if !port.Send(uint32(participant.ID), uint8(network.MsgStateSnapshot), chunk) {
				return fmt.Errorf("host could not send capture chunk %d/%d to participant %d",
					i+1, len(chunks), participant.ID)
			}
		}
	}
	// No deadline. The first-guest window was satisfied by the roster this gate is
	// waiting on, and re-arming it here would end a session that has its guest
	// because installing the world took the last second of it. A participant that
	// connects and then never confirms holds this gate open; that is a startup-gate
	// bound this does not have, recorded as a blocker in doc/kubernetes-fleet.md.
	if err := a.waitForStartup(port, signals, admitted, true, time.Time{}, func() bool {
		return port.PeerCount() >= admitted && port.ReadyCount() >= admitted
	}); err != nil {
		return err
	}

	// The lobby's links have been up for the whole wait, so several round trips
	// have completed on each and the convergence floor can be decided per link
	// rather than from the gate's aggregate transfer. A participant that cannot
	// carry a whole world per floor window is refused here for the same reason a
	// mid-run join is: it would play, it would drift, and nothing would be
	// scheduled that repairs it.
	for _, participant := range offer.Participants {
		if participant.ID == offer.Host {
			continue
		}
		if err := a.admitMeasuredLink(port, participant.ID); err != nil {
			return fmt.Errorf("session start: %w", err)
		}
	}

	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participants", len(offer.Participants)))
	a.corrections.startPump()
	return nil
}

// startJoinSession completes the tick-zero gate before the socket port owns the
// stream. The roster arrives with the gate, not with the offer: a joiner that
// dialled early saw only the participants ahead of it.
func (a *App) startJoinSession(signals <-chan os.Signal) error {
	if a.pendingJoin == nil {
		return errors.New("join session has no pending stream")
	}
	a.showStartupStatus("Join accepted; waiting for host start gate")
	final, err := a.awaitStartGate(signals)
	if err != nil {
		return err
	}
	a.sessionOffer = final
	if !final.CarriesSnapshot() {
		return fmt.Errorf("join start gate carries no capture; host is running an older build")
	}
	_, body, err := a.pendingJoin.ReceiveSnapshot()
	if err != nil {
		return err
	}
	cap, err := snapshot.DecodeCapture(body)
	if err != nil {
		return err
	}
	a.showStartupStatus(fmt.Sprintf("Installing the session world at tick %d (%d bytes)",
		cap.Header.Tick, len(body)))
	if err := a.JoinSessionAt(final, cap); err != nil {
		return fmt.Errorf("join roster: %w", err)
	}
	if err := a.pendingJoin.Ready(); err != nil {
		return fmt.Errorf("join ready gate: %w", err)
	}
	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participants", len(final.Participants)))
	return nil
}

// awaitStartGate reads the host's start record without freezing this instance.
//
// The gate carries no deadline, and that is right: it is the host waiting for the
// rest of its lobby, a human-paced wait with no bound worth guessing at. What was
// wrong is that it was also a wait nobody could leave — the whole of a join runs
// before the frame loop exists, so a blocking read here answered no key and no
// signal, and a host that never sent the record froze the game, quit included.
//
// So the read runs on a goroutine while the terminal is polled here, as the host's
// own lobby already does. Cancelling closes the stream, which turns the read in
// flight into an error rather than a goroutine outliving the run; waiting for it to
// return is what keeps the App single-threaded either way.
func (a *App) awaitStartGate(signals <-chan os.Signal) (network.SessionOffer, error) {
	type gate struct {
		offer network.SessionOffer
		err   error
	}
	done := make(chan gate, 1)
	go func() {
		offer, err := a.pendingJoin.WaitStart()
		done <- gate{offer, err}
	}()
	cancel := func() (network.SessionOffer, error) {
		_ = a.pendingJoin.Close()
		<-done
		return network.SessionOffer{}, errSessionCanceled
	}

	var events <-chan terminal.Event
	if a.termSvc != nil {
		events = a.termSvc.Events()
	}
	for {
		select {
		case g := <-done:
			if g.err != nil {
				return network.SessionOffer{}, fmt.Errorf("join start gate: %w", g.err)
			}
			return g.offer, nil
		case <-signals:
			return cancel()
		case ev := <-events:
			switch ev.Type {
			case terminal.EventClosed, terminal.EventError:
				return cancel()
			case terminal.EventResize:
				a.handleResize(ev.Width, ev.Height)
			case terminal.EventKey:
				if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ {
					return cancel()
				}
			}
		}
	}
}

// waitForStartup treats rejected handshakes as recoverable while no peer was admitted.
//
// deadline, when non-zero, ends the wait with errSessionExpired. It is supplied by
// the caller rather than read from the lifetime policy here, because only one of
// the two gates this serves is inside the window that deadline belongs to.
func (a *App) waitForStartup(port *network.SocketPort, signals <-chan os.Signal,
	expectedPeers int, failOnDisconnect bool, deadline time.Time, ready func() bool) error {
	var events <-chan terminal.Event
	if a.termSvc != nil {
		events = a.termSvc.Events()
	}
	// A pod nobody dialled is precisely the case the first-guest window exists for,
	// and it is also the case this gate would otherwise wait in forever. A run with
	// no bounded policy is given no deadline and waits as it always has.
	var expiry <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expiry = timer.C
	}
	for !ready() {
		select {
		case <-signals:
			return errSessionCanceled
		case now := <-expiry:
			a.life.State(now) // settles the deadline so the reason is recorded once
			return errSessionExpired
		case ev := <-events:
			switch ev.Type {
			case terminal.EventClosed, terminal.EventError:
				return errSessionCanceled
			case terminal.EventResize:
				a.handleResize(ev.Width, ev.Height)
			case terminal.EventKey:
				if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyCtrlQ {
					return errSessionCanceled
				}
			}
		case err := <-port.Errors():
			logSessionError(err)
			a.showStartupStatus("Join rejected: " + err.Error() + "; still waiting")
		case <-port.Changes():
			if failOnDisconnect && port.PeerCount() < expectedPeers {
				return errLobbyAbandoned
			}
		}
	}
	return nil
}

// pollTerminalEarly starts the terminal poll ahead of the rest of the hub, so a
// gate that blocks on a peer still has keys and signals to end on. A service is
// started once: the StartAll that follows finds this one already running, and a run
// with no terminal has nothing to start.
func (a *App) pollTerminalEarly() error {
	if a.termSvc == nil {
		return nil
	}
	return a.termSvc.Start()
}

// socketPort returns the concrete startup endpoint contributed by NetworkService.
func (a *App) socketPort() (*network.SocketPort, error) {
	if a.networkSvc == nil || a.networkSvc.Port() == nil {
		return nil, errors.New("network session has no socket port")
	}
	return a.networkSvc.Port(), nil
}

// activateNetworkSession closes the crossing window before terminal input is read.
func (a *App) activateNetworkSession() {
	a.world.RunSafe(a.activateNetworkSessionLocked)
}

// activateNetworkSessionLocked is the same for a caller that already holds the
// world lock. Caller MUST hold updateMutex.
func (a *App) activateNetworkSessionLocked() {
	for _, sys := range a.world.Systems() {
		if activator, ok := sys.(interface{ ActivateSession() }); ok {
			activator.ActivateSession()
		}
	}
}

// showStartupStatus renders a frozen tick-zero lobby message.
func (a *App) showStartupStatus(message string) {
	a.ctx.SetStatusMessage(message, 0, false)
	if a.orchestrator != nil {
		a.frame()
	}
}

func logSessionError(err error) {
	if err != nil {
		vlog.Warn("app", "msg", "network session", "error", err.Error())
	}
}
