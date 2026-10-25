package app

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"

	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

var errSessionCanceled = errors.New("network session canceled")

// errSessionSignalled is a cancel the process was told to make: it quits even a
// run that a failed :join would otherwise replace with a solo one.
var errSessionSignalled = fmt.Errorf("%w by a signal", errSessionCanceled)

// ErrSessionHandoff refuses a join that arrived while the session was electing a
// new authority. A joiner retries against the authority that emerges: half-admitting
// it into a term that is about to end is the one outcome that would leave a
// participant in a session nobody owns.
var ErrSessionHandoff = &network.Refusal{Code: network.RefusalHandoff, Reason: "session authority is changing"}

// errNotHosting refuses a join at the port of a guest that does not author its
// session: only the participant holding the term admits anybody.
var errNotHosting = &network.Refusal{Code: network.RefusalUnknown, Reason: "this participant does not host its session"}

// ErrSessionStarting refuses a dial that lands in the window between the startup
// lobby closing and the mid-run gate opening, which a dialer retries for the same
// reason it retries ErrSessionHandoff.
var ErrSessionStarting = &network.Refusal{Code: network.RefusalStarting, Reason: "session is starting"}

// ErrSessionEnding is the kind of refusal a draining or expired session gives a
// dial. It is the opposite of ErrSessionStarting: this run is leaving, so retrying
// against it is exactly the wrong answer, and an allocator reading it should place
// the participant somewhere else.
var ErrSessionEnding = &network.Refusal{Code: network.RefusalEnding, Reason: "session is ending"}

// errSessionExpired ends a lobby whose allocated window closed before a guest
// reached it. It is a clean end rather than a failure — the session did what it was
// told to do with a slot nobody claimed — so the caller reports it and exits zero.
var errSessionExpired = errors.New("session lifetime expired")

// newSessionApp resolves the startup handshake before a joining App draws a
// seed. Interactive play and authored headless scripts share this construction.
func newSessionApp(cfg Config) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Before anything is built, so a graph that does not resolve fails in front of
	// the operator. Each seat parses its own copy when it is added.
	for _, spec := range cfg.Bots {
		if _, err := loadBotGraph(cfg.Resources, spec); err != nil {
			return nil, err
		}
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
	// The boot settles before the accept loop opens, as BeginHosting's does: a
	// scenario that sets up its own map (td) is offered on it, not on -size's.
	a.Settle()
	return a, nil
}

// hostNetworkConfig captures the App before the service starts its accept loop.
func (a *App) hostNetworkConfig() *link.Config {
	netCfg := a.cfg.hostNetwork(a.cfg.HostAddress)
	netCfg.ParticipantID = engine.CoordinatorParticipant
	netCfg.MaxPeers = a.sessionCapacity()
	netCfg.OnError = logSessionError
	netCfg.AcceptSession = a.sessionAcceptor(a.admissions.Admit)
	// Every host outlives its guests, so every host admits a dial after its lobby
	// closed: a dropped participant comes back through the one gate a reconnect has
	// ever had. The hook answers nothing until the run arms it, because until then
	// the gate is the startup lobby's own.
	netCfg.OnAdmit = a.admitLateJoiner
	return netCfg
}

// sessionAcceptor is the join handshake this run serves as its session's coordinator,
// on its listener and on its bots' in-process streams, which spend no join budget.
// admit takes or refuses a dial before anything is allocated for it.
func (a *App) sessionAcceptor(admit func(net.Addr) error) func(net.Conn) (link.PeerID, error) {
	return network.HostAcceptor(network.Coordinator{
		Preview:     a.previewParticipant,
		Assign:      a.assignParticipant,
		Release:     a.releaseParticipant,
		Admit:       admit,
		Report:      a.noteJoinerReport,
		Prepared:    a.showJoinerPrepared,
		Name:        a.cfg.SessionName,
		Scenario:    a.scenarioBody,
		PressWindow: parameter.NetworkJoinPressWindow,
	}, network.DefaultConfig().ConnectTimeout)
}

// showJoinerPrepared tells this run's player who is about to join: a dialer whose
// world is built and whose player has the ready period to press in. Nothing stands
// for it on the map, since its place is assigned at the press.
func (a *App) showJoinerPrepared(addr net.Addr) {
	a.ctx.SetStatusMessage("A player at "+addr.String()+" is ready to join; waiting for their press", parameter.StatusNotice)
}

// admitHosted is a guest's port admitting a dial: it does once this run holds the
// term, spending the budget a host's door does, and while the term moves it refuses
// as that door would.
func (a *App) admitHosted(addr net.Addr) error {
	switch st := a.authority.State(); {
	case st.Migrating:
		return ErrSessionHandoff
	case !st.Authoring():
		return errNotHosting
	}
	return a.admissions.Admit(addr)
}

// Serve only the immutable scenario this run holds; another scenario needs a new App.
func (a *App) scenarioBody(digest string) ([]byte, error) {
	if digest != a.scenario.Digest() {
		return nil, fmt.Errorf("this session plays %s (%s), not %s",
			a.scenario.Name, a.scenario.Short(), shortDigest(digest))
	}
	body, err := a.scenario.MarshalCompressed()
	if err != nil {
		return nil, err
	}
	a.log.Info("app", "msg", "scenario served",
		"scenario", a.scenario.Name, "digest", a.scenario.Short(), "bytes", len(body))
	return body, nil
}

// shortDigest trims a digest for a message; one from a peer may be anything at all.
func shortDigest(d string) string {
	switch {
	case d == "":
		return "nothing"
	case len(d) > 12:
		return d[:12]
	}
	return d
}

// admitLateJoiner gates a dial that arrives after the startup lobby closed.
func (a *App) admitLateJoiner(id link.PeerID) {
	if !a.lateJoins.Load() {
		return
	}
	// Before the gate, not after it: the gate waits for a capture a playout lead
	// ahead of the current tick, so a session parked for having nobody in it would
	// time out every dial that came to end that. A pause its own bots allowed ends
	// the same way, since the arrival is somebody the pause does not hold; a hold
	// does hold it, and its gate needs no lead.
	a.resumeVacant()
	if a.ctx.TimeCtl.OwnPause() {
		a.ctx.SetPaused(false)
		a.scheduler.DispatchEventsImmediately()
	}
	a.releaseMidRunJoiner(id)
}

// openMidRunJoins ends the lobby's closing window and arms the mid-run gate, in that
// order: a dial refused a moment ago retries into a gate that now exists. Every run
// that owns a frame loop calls it once its clock is running — the gate reads a
// capture a playout lead ahead, so arming it over a stopped clock times every dial
// out instead of admitting it.
func (a *App) openMidRunJoins() {
	a.lateJoins.Store(true)
	a.lobbyClosing.Store(false)
}

// sessionCapacity is how many guests this host will ever hold, excluding itself.
// `-players` and the scenario's max_players are ceilings and only ceilings, the
// lower one holding; unset, `-players` means the whole roster. What the lobby waits
// for is lobbyQuorum. The subtraction is the only difference between the shapes: an
// interactive host holds one of the cursors itself, a dedicated one holds a roster
// entry and no slot on the map.
func (a *App) sessionCapacity() int {
	n := a.playersSet()
	if n <= 0 {
		n = parameter.MaxPlayers
	}
	n = min(n, a.scenario.MaxPlayers)
	if a.cfg.Mode.Serves() {
		return n
	}
	return n - 1
}

// playersSet is the `-players` this run's session was opened with, zero for none:
// its own as that session's host, else the one the offer it joined on carried.
func (a *App) playersSet() int { return cmp.Or(a.cfg.Participants, a.cfg.sessionPlayers) }

// playerCapacity is the cursor ceiling `-players` names. sessionCapacity is its
// guest half; an interactive host drives the one that is left, a dedicated one
// drives none.
func (a *App) playerCapacity() int {
	if a.cfg.Mode.Serves() {
		return a.sessionCapacity()
	}
	return a.sessionCapacity() + 1
}

// guestCount is how many guests the roster currently holds. The coordinator of a
// dedicated host holds a roster entry and no cursor, so it is subtracted: a session
// consisting only of its coordinator is an empty one, which is the reading both the
// readiness probe and the lifetime policy need.
func (a *App) guestCount() int {
	guests, _ := a.rosterGuests()
	return guests
}

// rosterGuests is guestCount and, from the same read, how many of those guests are
// bots: entries a participant holds.
func (a *App) rosterGuests() (guests, bots int) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	for _, p := range a.sessionRoster {
		if p.Holder != 0 {
			bots++
		}
	}
	return max(len(a.sessionRoster)-1, 0), bots
}

// lobbyQuorum is how many guests the start gate waits for before it closes the lobby
// and releases tick zero: one, or as many as the scenario's min_players needs beside
// this host's own cursor, unless `-players` named a party on a host that is not
// dedicated. Every later guest arrives through the mid-run gate, as a reconnect does.
// A min_players of 0 waits for one: a session with nobody in it parks.
func (a *App) lobbyQuorum() int {
	switch {
	case a.cfg.Mode.Serves():
		return max(1, a.scenario.MinPlayers)
	case a.cfg.Participants <= 0:
		return max(1, a.scenario.MinPlayers-1)
	}
	return a.sessionCapacity()
}

// rosterPlayers is how many players, bots counted, this run's session holds: its
// guests, and its own cursor unless it is a dedicated host.
func (a *App) rosterPlayers() int {
	if a.cfg.Mode.Serves() {
		return a.guestCount()
	}
	return a.guestCount() + 1
}

// playersFit refuses a scenario this run cannot play: a `-players` ceiling under
// its min_players, or a session already holding more than its max_players.
func (a *App) playersFit(sc resource.Scenario) error {
	if n := a.playersSet(); n > 0 && n < sc.MinPlayers {
		return fmt.Errorf("-players %d is below scenario %s's min_players %d", n, sc.Name, sc.MinPlayers)
	}
	if n := a.rosterPlayers(); n > sc.MaxPlayers {
		return fmt.Errorf("scenario %s holds at most %d players; this session has %d", sc.Name, sc.MaxPlayers, n)
	}
	return nil
}

// Admission records ownership before a bot can enter the lobby or running roster,
// and the claim its participant comes back under.
func (a *App) noteJoinerReport(id link.PeerID, report network.JoinerReport) error {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if report.Holder != 0 {
		i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == report.Holder })
		if i < 0 || report.Holder == id || a.sessionRoster[i].Holder != 0 || report.Listen != "" {
			return errors.New("bot holder is absent, is itself a bot, or the bot advertised a listener")
		}
	}
	i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id })
	if i < 0 {
		return errors.New("participant left during admission")
	}
	// A participant taking its place back keeps the claim it came back under, and
	// comes back as what it left as.
	if r, back := a.returning[id]; back {
		if report.Holder != r.entry.Holder {
			return errors.New("a participant comes back with the holder it left with")
		}
		r.record = report.Record
		a.returning[id] = r
	} else if report.Claim != "" && !network.ValidClaim(report.Claim) {
		return errors.New("the joiner's claim is of another form")
	} else {
		a.sessionRoster[i].Claim = report.Claim
	}
	a.sessionRoster[i].Holder = report.Holder
	if report.Holder == 0 {
		a.personJoined = true
		if !a.firstPerson.Sized() && report.Sized() {
			a.firstPerson = report
		}
	}
	a.reach.NoteDeclared(id, report)
	a.log.Info("admit", "msg", "peer admitted", "peer", uint64(id), "holder", uint64(report.Holder),
		"remote", report.Remote, "declared", report.Listen)
	return nil
}

// adoptLobbyGeometry sizes a dedicated host's map from the first person to join, as a
// server has no terminal of its own and would serve Normalize's 80x24, or from a bot's
// screen when its own bots start its game. An operator's -size, or a scenario fixing
// its own map (crop off), wins. It runs before the roster closes, so these bounds are
// the ones the offer names and every later guest adopts.
func (a *App) adoptLobbyGeometry() {
	if !a.cfg.Mode.Serves() || !a.cfg.geometryDefaulted {
		return
	}
	a.sessionMu.Lock()
	report, person := a.firstPerson, a.personJoined
	a.sessionMu.Unlock()
	bots := len(a.cfg.Bots) > 0 || !person
	if bots {
		report = network.JoinerReport{Width: BotWidth, Height: BotHeight}
	}
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
	// same bounds the same way. Pushed as adoptMapLatch does, not through SetupLevel:
	// the guest on the link makes the session live, and this is no operator request.
	a.Resize(report.Width, report.Height)
	var vw, vh int
	a.world.RunSafe(func() {
		cfg := a.world.Resources.Config
		vw, vh = cfg.ViewportWidth, cfg.ViewportHeight
	})
	a.ctx.PushEventOrigin(event.EventLevelSetup,
		&event.LevelSetupPayload{Width: vw, Height: vh, CropOnResize: true}, event.OriginDebug)
	a.scheduler.Settle()
	by := "person"
	if bots {
		by = "bots"
	}
	a.log.Info("session", "msg", "session sized", "by", by,
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

// newJoiningApp builds the App on a dial of its session: the one Config carries
// from a :join or a redial, or one made now.
func newJoiningApp(cfg Config) (*App, error) {
	d := cfg.dialled
	if d == nil {
		var err error
		if d, err = dialJoin(cfg); err != nil {
			return nil, err
		}
	}
	return d.open(cfg)
}

// joinDial is a session this instance dialled and whose scenario it holds: everything
// a host can refuse before a world is built, settled. It holds no place there until
// its seat, which is the press of the player whose run it opens, or comes at once.
// It holds the handshake alone, so the run it opens is the one its caller built.
type joinDial struct {
	pending   *network.PendingJoin
	offer     network.SessionOffer // the preview, then the offer its seat assigned
	resources resource.Options     // the session's scenario, as this instance resolved it

	// claim is the one the offer gives a place back under, nil for a new place, held
	// from claims until the run built on this dial takes it.
	claims *claimKeeper
	claim  *claim
}

// dialJoin dials cfg's session and prepares on its preview: the scenario it plays.
// A peer running a different protocol or simulation is refused here, before it has
// built a world, and nothing is held for it: its place is assigned at its seat. It
// names every claim of its kind it keeps, so a place one of them waits under is the
// one its seat is offered.
func dialJoin(cfg Config) (*joinDial, error) {
	pending, preview, err := network.PreviewSession(cfg.JoinAddress, joinNetConfig(cfg))
	if err != nil {
		if cfg.relay != nil {
			return nil, fmt.Errorf("join through the holder: %w", err)
		}
		return nil, fmt.Errorf("join %s: %w", link.Shown(cfg.JoinAddress), err)
	}
	d := &joinDial{pending: pending, offer: preview, claims: cfg.claims}
	if cfg, err = ConfigForJoin(cfg, preview); err == nil {
		err = resolveJoinScenario(&cfg, pending, preview)
	}
	if err != nil {
		d.abandon(err)
		return nil, err
	}
	d.resources = cfg.Resources
	return d, nil
}

// joinNetConfig is the transport a dial of cfg's session runs on.
func joinNetConfig(cfg Config) *network.Config {
	netCfg := network.DebugConfig(link.RolePeer, cfg.JoinAddress)
	netCfg.OnError = logSessionError
	netCfg.Identity = buildIdentity()
	netCfg.SessionName = cfg.SessionName
	netCfg.Dial = cfg.relay
	netCfg.ConnectTimeout = parameter.NetworkJoinConnectTimeout
	netCfg.Claims = cfg.claims.tokens(cfg.holder != 0)
	netCfg.TLS = cfg.Trust
	netCfg.ConnectPeer = network.PeerDialer(netCfg.Identity, netCfg.ConnectTimeout)
	return netCfg
}

// seat takes this dial's place before a run is built on it, for a caller that has to
// know which place it is: a redial coming back to its own. Its claim is held from
// here, until the run built on it takes it.
func (d *joinDial) seat(bot bool) error {
	offer, err := d.pending.Seat()
	if err != nil {
		_ = d.pending.Close()
		return err
	}
	d.offer, d.claim = offer, d.claims.take(offer, bot)
	return nil
}

// seated reports whether this dial holds its place yet.
func (d *joinDial) seated() bool { return d.offer.Assigned != 0 }

// abandon refuses the offer, which releases the identity the host assigned, or
// gives the place back to its Wraith and the claim back to the keeper; before its
// seat, it ends a handshake that held nothing.
func (d *joinDial) abandon(cause error) {
	_ = d.pending.Complete(cause, network.JoinerReport{})
	_ = d.pending.Close()
	if d.claim != nil {
		d.claims.release(d.claim)
	}
}

// open constructs the joining App cfg describes, on the preview or the offer: the
// world is built and checked before the seat, which comes now unless this run's
// player presses for it (Loop). A dial already seated replies here.
func (d *joinDial) open(cfg Config) (*App, error) {
	pending, offer := d.pending, d.offer
	cfg.Resources, cfg.networkConfig = d.resources, pending.TransportConfig()
	cfg, err := ConfigForJoin(cfg, offer)
	if err != nil {
		d.abandon(err)
		return nil, err
	}

	// Bound before the reply that declares it (internal/converge/reach.go), and only
	// where authorship can move: elsewhere a guest's port is for nothing. A browser is
	// never dialled, and js/wasm's net would bind a port nobody reaches. A guest that
	// joined through a relay could not reach the host, so it is a leaf unless -listen
	// names an address the session can reach it at.
	var listener net.Listener
	var declared string
	gate := &converge.PeerLinkGate{}
	if !offer.FixedAuthority && !cfg.NoAdvertise && buildHasSocketNetwork && (offer.Relay == 0 || cfg.ListenAddress != "") {
		listener, declared = converge.BindAdvertised(cfg.ListenAddress, cfg.JoinAddress)
	}
	if listener != nil {
		// Installed before New, because New is what builds the port that serves
		// this listener. The gate holds the App rather than closing over it for the
		// same reason: it does not exist yet, nor does the identity it answers with.
		cfg.networkConfig.PreboundListener = listener
		cfg.networkConfig.AcceptPeer = network.PeerAcceptor(network.PeerGate{
			Local:    gate.Local,
			Identity: identityFromAnchor(offer.Anchor),
			Admit:    gate.Admit,
		}, cfg.networkConfig.ConnectTimeout)
	}
	a, err := New(cfg)
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		d.abandon(err)
		return nil, err
	}
	gate.Bind(a.reach)
	a.reach.AdoptListener(listener, declared)
	a.pendingJoin = pending
	a.sessionOffer = offer
	// Identity now, world and roster at the start gate: a mismatched joiner must be
	// refused before the host spends a seat or the rest of the lobby on it. The
	// position is deliberately not checked — the gate carries the host's world, so
	// what tick it has reached is no longer this instance's problem to reproduce.
	if err := a.JoinAt(offer.Anchor); err != nil {
		d.abandon(err)
		a.Close()
		return nil, err
	}
	switch {
	case d.seated():
		err = a.bindSeat(pending, offer, d.claim)
	case a.joinWaits():
		err = pending.Prepared()
	default:
		err = a.takeSeat(false, nil)
	}
	if err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// joinWaits says this run's seat waits for its player's press: a dial that player
// started, in a run a person plays, with the start wait on.
func (a *App) joinWaits() bool {
	return a.cfg.joinPress && a.cfg.Mode.OwnsInput() && a.ctx.WaitStart.Load()
}

// seated reports whether this joining run holds its place in its session yet.
func (a *App) seated() bool { return a.sessionOffer.Assigned != 0 }

// errSessionMoved refuses a seat in a session that started another game, or moved
// to another host, after this run built its world: a retry builds one on that game.
var errSessionMoved = &network.Refusal{Code: network.RefusalBehind,
	Reason: "the session started another game while this join prepared"}

// takeSeat seats this run in the session it prepared for. The host assigns its place
// now, which binds the identity this run's port and world were built without, and
// the run replies as it would have to an offer. Pressed for, a host that dropped the
// handshake while its player was away, or refused for a moment, is dialled again
// and seats at once, while the game is the one this world was built on, saying so
// and reading the player's Ctrl-C between dials; a seat that comes at once leaves a
// retry to its caller, as a refused dial does.
func (a *App) takeSeat(pressed bool, signals <-chan os.Signal) error {
	pending := a.pendingJoin
	offer, err := pending.Seat()
	if err != nil {
		_ = pending.Close()
	}
	var refusal *network.Refusal
	redial := pressed && err != nil && (!errors.As(err, &refusal) || network.RetryOf(err) == network.RetrySoon)
	for deadline := time.Now().Add(parameter.SessionRejoinWindow); redial && time.Now().Before(deadline); { // [wall] a link bound
		a.log.Info("session", "msg", "seat dialled again", "error", err.Error())
		a.showJoinStatus("Joining " + link.Shown(a.cfg.JoinAddress) + "... (" + err.Error() + ")")
		pending, offer, err = a.redialSeat()
		if redial = err != nil && network.RetryOf(err) == network.RetrySoon && !errors.Is(err, errSessionMoved); redial {
			select {
			case <-signals:
				return errSessionSignalled
			case ev := <-a.lobbyEvents():
				if a.lobbyEventCancels(ev) {
					return errSessionCanceled
				}
			case <-time.After(parameter.SessionRejoinInterval):
			}
		}
	}
	if err == nil && !a.preparedFor(offer) {
		err = errSessionMoved
		_ = pending.Complete(err, network.JoinerReport{})
	}
	if err != nil {
		return err
	}
	return a.bindSeat(pending, offer, a.claims.take(offer, a.cfg.holder != 0))
}

// redialSeat dials this run's session again and seats at once, while the game there
// is the one this run's world was built on. What it dialled is closed unless it
// seated.
func (a *App) redialSeat() (*network.PendingJoin, network.SessionOffer, error) {
	pending, preview, err := network.PreviewSession(a.cfg.JoinAddress, joinNetConfig(a.cfg))
	if err != nil {
		return nil, network.SessionOffer{}, err
	}
	if !a.preparedFor(preview) {
		_ = pending.Complete(errSessionMoved, network.JoinerReport{})
		return nil, network.SessionOffer{}, errSessionMoved
	}
	offer, err := pending.Seat()
	if err != nil {
		_ = pending.Close()
		return nil, network.SessionOffer{}, err
	}
	return pending, offer, nil
}

// preparedFor reports whether o is the session this run's world was built on: the
// game a join's anchor check names, at whatever tick it has reached, reached the way
// this run's listener was chosen for. A map the host has resized since is the gate's
// to adopt, as it is for any joiner.
func (a *App) preparedFor(o network.SessionOffer) bool {
	was, now := a.sessionOffer.Anchor.Anchor, o.Anchor.Anchor
	return was.Seed == now.Seed && was.Session == now.Session && was.ScenarioDigest == now.ScenarioDigest &&
		o.Relay == a.sessionOffer.Relay && o.FixedAuthority == a.sessionOffer.FixedAuthority
}

// bindSeat makes this run the participant offer assigns: its port takes the join's
// stream and identity, its world the identity, and it holds c, the claim the place
// was given back under, or a new one for a new place, from here. Then it replies,
// reporting what it turned out to be. Reported after construction, which is the
// whole reason the acceptance carries it rather than the dial: only now does this
// instance know the terminal it got. A host with no geometry of its own uses it to
// size the session.
func (a *App) bindSeat(pending *network.PendingJoin, offer network.SessionOffer, c *claim) error {
	port, err := a.socketPort()
	if err == nil {
		err = pending.Hand(port)
	}
	if err != nil {
		_ = pending.Complete(err, network.JoinerReport{})
		if c != nil {
			a.claims.release(c)
		}
		return err
	}
	if c == nil {
		c = a.claims.issue(a.cfg.holder != 0)
	}
	// The latch reads it when the first tick opens, as it would have read it from New.
	a.world.RunSafe(func() { a.world.Resources.Network.ParticipantID = uint32(offer.Assigned) })
	a.sessionMu.Lock()
	a.pendingJoin, a.sessionOffer = pending, offer
	a.sessionMu.Unlock()
	a.claim.Store(c)
	if err := pending.Complete(nil, a.joinerReport()); err != nil {
		return fmt.Errorf("join reply: %w", err)
	}
	return nil
}

// resolveJoinScenario makes this run hold the exact scenario the coordinator is
// playing, before it builds a world the coordinator would refuse. A root that
// already has those bytes is used as it stands, whatever it calls them; otherwise
// the coordinator is asked for them and they are held in memory for the life of the
// run and never written to disk.
func resolveJoinScenario(cfg *Config, pending *network.PendingJoin, o network.SessionOffer) error {
	want := o.Anchor.Anchor.ScenarioDigest
	if want == "" {
		return nil // nothing to match against; the identity check is the verdict
	}
	if local, err := resource.LoadScenario(cfg.Resources); err == nil && local.Digest() == want {
		return nil
	}
	body, err := pending.RequestScenario(want)
	if err != nil {
		return fmt.Errorf("scenario transfer: %w", err)
	}
	sc, err := resource.UnmarshalScenarioCompressed(o.Anchor.Anchor.ScenarioID, body)
	if err != nil {
		return err
	}
	if sc.Digest() != want {
		return fmt.Errorf("scenario transfer: arrived as %s, the offer named %s",
			sc.Short(), shortDigest(want))
	}
	cfg.Resources.Provided = &sc
	vlog.Info("app", "msg", "scenario received",
		"scenario", sc.Name, "digest", sc.Short(), "files", sc.Files(), "bytes", len(body))
	return nil
}

// previewParticipant is what a dial is shown while it prepares: the offer its seat
// would carry, naming nobody. It refuses what the seat would refuse with nothing
// allocated, and a full session only when no claim of the dial's names a place in
// it and no player who left holds one: whether a place is taken back or given up is
// the seat's to settle, so a dial that never seats moves nobody.
func (a *App) previewParticipant(claims []string) (network.SessionOffer, error) {
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if err := a.refusedLocked(); err != nil {
		return network.SessionOffer{}, err
	}
	// Counted as the seat counts it, the coordinator's own entry among them, with no
	// roster opened for a dial that may never seat (openRosterLocked).
	if max(len(a.sessionRoster), 1) > a.sessionCapacity() && !a.namesPlaceLocked(claims) && !a.mayYieldLocked() {
		return network.SessionOffer{}, a.fullLocked()
	}
	return a.offerLocked(anchor, 0), nil
}

// assignParticipant allocates the next identity, or the place a claim the dialer
// named waits under, and returns the offer carrying it. One call per accepted
// connection: the roster is the lobby so far, and the roster a participant actually
// builds from arrives later, at the start gate.
func (a *App) assignParticipant(claims []string) (network.SessionOffer, error) {
	o, err := a.assignFor(a.JoinAnchor(), 0, claims)
	a.closeStaleLink(err, a.sessionTransport())
	return o, err
}

// returnPlace is a place a dial took back with its claim, until its arrival
// crosses: the entry as it waited, and what its player brings back.
type returnPlace struct {
	entry  network.RosterEntry
	record *event.CursorRecord
}

// staleLink refuses a dial whose claim names a place still linked: whoever names the
// token is its player, and the link one it left that the host has not yet seen go.
type staleLink struct {
	id link.PeerID
	*network.Refusal
}

func (s *staleLink) Unwrap() error { return s.Refusal }

// closeStaleLink closes the link a refusal found a returning player's place held
// on. Its departure crosses as any lost link's does, and the dial's retry takes the
// place back; a place held through a relay waits for that relay to see its link go.
func (a *App) closeStaleLink(err error, port engine.NetworkPort) {
	var s *staleLink
	if p, ok := port.(engine.PeerDroppingPort); ok && errors.As(err, &s) && p.Disconnect(uint32(s.id)) {
		a.log.Info("session", "msg", "stale link closed for a returning player", "peer", uint64(s.id))
	}
}

// assignFor is the allocation itself. holder is the participant a bot joins through,
// recorded now because the bot never reports to this instance. A claim among the
// tokens named takes back the place its Wraith keeps, a full session's too. Caller
// must not hold sessionMu.
func (a *App) assignFor(anchor event.JoinAnchor, holder link.PeerID, claims []string) (network.SessionOffer, error) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if err := a.refusedLocked(); err != nil {
		return network.SessionOffer{}, err
	}

	limit := a.sessionCapacity() + 1
	a.openRosterLocked()
	if holder != 0 {
		i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == holder })
		if i < 0 || a.sessionRoster[i].Holder != 0 {
			return network.SessionOffer{}, errors.New("a bot's holder must be a participant that is not itself a bot")
		}
	}
	if entry, err := a.claimedLocked(claims, holder); err != nil || entry.ID != 0 {
		if err != nil {
			return network.SessionOffer{}, err
		}
		a.sessionOffer = a.offerLocked(anchor, entry.ID)
		return a.sessionOffer, a.sessionOffer.Validate()
	}
	if len(a.sessionRoster) >= limit {
		if a.yieldLocked() {
			return network.SessionOffer{}, &network.Refusal{Code: network.RefusalYielding,
				Reason: "session is full, and a player who left is giving up a place"}
		}
		return network.SessionOffer{}, a.fullLocked()
	}
	assigned := a.nextParticipantLocked()
	assigned.Holder = holder
	a.sessionRoster = append(a.sessionRoster, assigned)

	a.sessionOffer = a.offerLocked(anchor, assigned.ID)
	return a.sessionOffer, a.sessionOffer.Validate()
}

// refusedLocked is why this session takes no dial now, whoever dials: what a preview
// and a seat both refuse before anything is allocated. Caller holds sessionMu.
func (a *App) refusedLocked() error {
	// A dial mid-succession is refused: the offer would name a term that is about to
	// end, so the participant would hold a roster slot the successor's record does
	// not carry. The refusal is distinguishable, so the joiner retries against
	// whatever authority emerges.
	if a.authority != nil && a.authority.Migrating() {
		return ErrSessionHandoff
	}
	// The same refusal for the same reason, one window earlier. Between the lobby
	// closing on its roster and the mid-run gate being armed there is no gate that
	// can serve a dial: the lobby's has already sent its offers, and the mid-run
	// one waits for a capture a clock that has not started never reaches. A dialer
	// admitted there would hold an identity and wait for a start it is not in.
	if a.lobbyClosing.Load() {
		return ErrSessionStarting
	}
	// A dial while this run leaves its session would be offered a term that ends with
	// it, on a door about to shut; its bots go back in once the run has left.
	if a.leaving.Load() {
		return &network.Refusal{Code: network.RefusalEnding, Reason: "session is ending: its host is leaving it"}
	}
	// A draining or expired session refuses before it allocates anything. The
	// roster slot, the identity and the capture that would follow are all work
	// spent on a participant this process is about to stop authoring for, and the
	// refusal is what makes a drain a drain rather than a slower shutdown.
	if st := a.life.State(time.Now()); !st.Admit {
		return &network.Refusal{Code: network.RefusalEnding, Reason: "session is ending: " + st.Reason}
	}
	return nil
}

// fullLocked is the refusal of a dial a full session has no place for. Caller holds
// sessionMu.
func (a *App) fullLocked() error {
	return &network.Refusal{Code: network.RefusalFull,
		Reason: fmt.Sprintf("session is full at %d participant(s)", a.playerCapacity())}
}

// namesPlaceLocked reports whether one of the tokens a dial named is a place's in
// this session, which its seat may take back. Caller holds sessionMu.
func (a *App) namesPlaceLocked(tokens []string) bool {
	return slices.ContainsFunc(tokens, func(t string) bool {
		digest := network.ClaimDigest(t)
		return slices.ContainsFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.Claim == digest })
	})
}

// claimedLocked is the place one of the tokens a dial named takes back: an away
// entry its Wraith keeps, held for the dial until its arrival crosses. A bot comes
// back through its own holder. A claimed place still linked, or coming back on
// another dial, refuses this one for a retry. Caller holds sessionMu.
func (a *App) claimedLocked(tokens []string, holder link.PeerID) (network.RosterEntry, error) {
	var refused error
	for _, t := range tokens {
		digest := network.ClaimDigest(t)
		i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.Claim == digest })
		if i < 0 {
			continue
		}
		p := a.sessionRoster[i]
		_, back := a.returning[p.ID]
		switch {
		case p.ID == a.authorityID() || p.ID == a.yielding || p.Holder != 0 && p.Holder != cmp.Or(holder, a.authorityID()):
			continue
		case back:
			refused = &network.Refusal{Code: network.RefusalReturning, Reason: "this player's place is being taken back by another join"}
		case !p.Away:
			refused = &staleLink{id: p.ID, Refusal: &network.Refusal{Code: network.RefusalReturning,
				Reason: "this player's place is still linked to the run it left"}}
		default:
			if a.returning == nil {
				a.returning = make(map[link.PeerID]returnPlace)
			}
			a.returning[p.ID] = returnPlace{entry: p}
			if holder != 0 {
				a.sessionRoster[i].Holder = holder
			}
			return a.sessionRoster[i], nil
		}
	}
	return network.RosterEntry{}, refused
}

// giveBack returns a place a dial took back to its Wraith, for a join that did not
// complete, and reports whether id was one.
func (a *App) giveBack(id link.PeerID) bool {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	r, ok := a.returning[id]
	if !ok {
		return false
	}
	delete(a.returning, id)
	if i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id }); i >= 0 {
		a.sessionRoster[i] = r.entry
	}
	return true
}

// openRosterLocked starts the roster with this run's own entry, which carries the
// claim it comes back under should a successor take its session. Caller holds
// sessionMu.
func (a *App) openRosterLocked() {
	if len(a.sessionRoster) == 0 {
		a.sessionRoster = []network.RosterEntry{{ID: engine.CoordinatorParticipant, Slot: a.hostSlot(), Claim: a.ownClaim()}}
	}
}

// ownClaim is the digest of the claim this run is in its session under, issued when
// a session it opens first names it; a dedicated host has no place to come back to.
func (a *App) ownClaim() string {
	if a.cfg.Mode.Serves() {
		return ""
	}
	c := a.claim.Load()
	if c == nil {
		c = a.claims.issue(a.cfg.holder != 0)
		a.claim.Store(c)
	}
	return network.ClaimDigest(c.Token)
}

// nextParticipantLocked takes the lowest free identity and the lowest free slot, so
// a lobby that loses a joiner reuses its place rather than exhausting the roster.
func (a *App) nextParticipantLocked() network.RosterEntry {
	taken := func(pick func(network.RosterEntry) int, want int) bool {
		return slices.ContainsFunc(a.sessionRoster, func(p network.RosterEntry) bool {
			return pick(p) == want
		})
	}
	var out network.RosterEntry
	for id := 1; id <= parameter.MaxPlayers+1; id++ {
		if !taken(func(p network.RosterEntry) int { return int(p.ID) }, id) {
			out.ID = link.PeerID(id)
			break
		}
	}
	for slot := range parameter.MaxPlayers {
		if !taken(func(p network.RosterEntry) int { return int(p.Slot) }, slot) {
			out.Slot = uint8(slot)
			break
		}
	}
	return out
}

// participantDeparted is the departure hook the network resource calls when a
// participant leaves.
func (a *App) participantDeparted(id uint32, final bool) { a.leaveRoster(link.PeerID(id), final) }

// releaseParticipant returns an identity whose handshake did not complete, or whose
// participant has left for good; a place taken back goes back to its Wraith.
func (a *App) releaseParticipant(id link.PeerID) {
	if !a.giveBack(id) {
		a.leaveRoster(id, true)
	}
}

// leaveRoster forgets the links a departed participant held. A final departure
// returns its identity and slot to the pool, with its bots'; any other keeps both
// for the Wraith that waits in the slot, and marks the entry away.
func (a *App) leaveRoster(id link.PeerID, final bool) {
	if id == 0 || id == a.authorityID() {
		return
	}
	a.reach.Forget(id)
	if a.authority != nil {
		a.authority.ForgetReachable(id)
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if final {
		gone := func(p network.RosterEntry) bool { return p.ID == id || p.Holder == id }
		for _, p := range a.sessionRoster {
			if gone(p) {
				a.awayOrder = slices.DeleteFunc(a.awayOrder, func(away link.PeerID) bool { return away == p.ID })
				delete(a.returning, p.ID)
				if a.yielding == p.ID {
					a.yielding = 0
				}
			}
		}
		a.sessionRoster = slices.DeleteFunc(a.sessionRoster, gone)
		return
	}
	for i := range a.sessionRoster {
		if a.sessionRoster[i].ID == id {
			a.sessionRoster[i].Away = true
		}
	}
	if !slices.Contains(a.awayOrder, id) {
		a.awayOrder = append(a.awayOrder, id)
	}
}

// yieldLocked asks the authority to dismiss the participant away longest, for a dial
// a full session cannot otherwise admit, and reports whether a place is on its way.
// It asks once until that dismissal applies; a bot whose holder is away waits for
// its holder's, and a place coming back is not away. Caller holds sessionMu.
func (a *App) yieldLocked() bool {
	if _, ok := a.awayLocked(a.yielding); ok {
		return true
	}
	a.yielding = a.yieldableLocked()
	if a.yielding == 0 {
		return false
	}
	a.dismissWraith.Store(uint32(a.yielding))
	return true
}

// mayYieldLocked reports whether yieldLocked would find a place on its way, asking
// nothing. Caller holds sessionMu.
func (a *App) mayYieldLocked() bool {
	_, ok := a.awayLocked(a.yielding)
	return ok || a.yieldableLocked() != 0
}

// yieldableLocked is the participant away longest whose place a full session may ask
// for: not one coming back, nor a bot whose holder is away too. Zero when nobody's
// is. Caller holds sessionMu.
func (a *App) yieldableLocked() link.PeerID {
	for _, id := range a.awayOrder {
		p, ok := a.awayLocked(id)
		if _, holderAway := a.awayLocked(p.Holder); ok && (p.Holder == 0 || !holderAway) {
			return id
		}
	}
	return 0
}

// awayLocked is id's roster entry while it is away and not coming back. Caller holds
// sessionMu.
func (a *App) awayLocked(id link.PeerID) (network.RosterEntry, bool) {
	i := slices.IndexFunc(a.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id })
	if _, back := a.returning[id]; i < 0 || !a.sessionRoster[i].Away || back {
		return network.RosterEntry{}, false
	}
	return a.sessionRoster[i], true
}

// crossPredecessorDeparture removes the authority that was lost from the roster. A
// departure is a shared entity's destruction, so exactly one instance may produce it
// (D-11) and that instance is the authority — which is the one that went. The
// successor is the first instance that may, so it does, under the new term.
func (a *App) crossPredecessorDeparture(rec network.HandoffRecord) {
	if rec.Predecessor == 0 {
		return
	}
	i := slices.IndexFunc(rec.Roster, func(p network.RosterEntry) bool {
		return p.ID == rec.Predecessor
	})
	slot := uint8(parameter.NoPlayerSlot)
	if i >= 0 {
		slot = rec.Roster[i].Slot
	}
	a.crossDeparture(rec.Predecessor, slot)
}

// dropAbandonedCursors removes the participants an instance left alone will never
// hear from again: every cursor it does not simulate belongs to someone nothing will
// move again. Having no link is what makes the removal local rather than a crossing
// — a departure is produced once at one agreed tick because two instances must
// destroy a shared entity together, and here there is no second instance.
func (a *App) dropAbandonedCursors(roster []network.RosterEntry, local link.PeerID) {
	a.world.RunSafe(func() { a.dropAbandonedLocked(roster, nil, local) })
}

// dropAbandonedLocked is dropAbandonedCursors under the world lock, which a run
// leaving its session holds. A place it kept is not removed but waits as a Wraith,
// as it would for any departure. Caller MUST hold updateMutex.
func (a *App) dropAbandonedLocked(roster, kept []network.RosterEntry, local link.PeerID) {
	if p := a.sessionTransportLocked(); p != nil && p.IsRunning() && p.PeerCount() > 0 {
		return
	}
	for _, p := range roster {
		keep := slices.ContainsFunc(kept, func(k network.RosterEntry) bool { return k.ID == p.ID })
		if p.ID == local || p.Slot == parameter.NoPlayerSlot || keep && p.Away {
			continue
		}
		a.pushDepartureLocked(event.ParticipantDepartedPayload{Participant: uint32(p.ID), Slot: p.Slot, Dismissed: !keep},
			core.DomainShared)
	}
}

// crossDeparture produces one participant's departure as the D-11 crossing it is.
func (a *App) crossDeparture(id link.PeerID, slot uint8) {
	a.pushDeparture(event.ParticipantDepartedPayload{Participant: uint32(id), Slot: slot}, core.DomainPlayer)
}

// pushDeparture emits one participant's departure: player puts the artifact on the
// wire for every instance to apply at one agreed tick, shared keeps it here, and
// both are recorded. The identity returns to the pool when a dismissal applies
// (OnDeparture), never here: a dial assigned it in between would be the
// participant the crossing removes.
func (a *App) pushDeparture(p event.ParticipantDepartedPayload, domain core.Domain) {
	a.world.RunSafe(func() { a.pushDepartureLocked(p, domain) })
}

// pushDepartureLocked is pushDeparture under the world lock. Caller MUST hold
// updateMutex.
func (a *App) pushDepartureLocked(p event.ParticipantDepartedPayload, domain core.Domain) {
	a.world.PushEventFull(event.EventParticipantDeparted, &p, event.OriginSession, domain)
}

// offerLocked builds the offer addressed to one participant. Caller holds sessionMu,
// which is why the anchor is passed in rather than read here: JoinAnchor takes the
// world lock, and a departure released from under that lock takes sessionMu.
func (a *App) offerLocked(anchor event.JoinAnchor, assigned link.PeerID) network.SessionOffer {
	term := a.authorityTerm()
	if term == 0 {
		term = network.FirstTerm
	}
	return network.SessionOffer{
		Anchor:   anchor,
		Host:     a.authorityID(),
		Assigned: assigned,
		Term:     term,
		Roster:   slices.Clone(a.sessionRoster),
		// A joiner adopts the chain whole: candidate list and address book in one.
		Chain:          a.sessionChain(),
		FixedAuthority: a.cfg.FixedAuthority,
		Players:        a.playersSet(),
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
	// Read before sessionMu: it takes the world lock, and a departure released from
	// under that lock takes sessionMu.
	anchor := a.JoinAnchor()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if len(a.sessionRoster) == 0 {
		// No joiner ever arrived; describe the two-participant lobby this host opened.
		// Through the allocator rather than beside it: a dedicated host holds no slot,
		// so its guest takes slot zero and a second rule here would disagree.
		a.openRosterLocked()
		a.sessionRoster = append(a.sessionRoster, a.nextParticipantLocked())
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

// startHostSessionOn runs the production gate against the supplied endpoint. The
// lobby closes on a quorum rather than a full roster, but what it closes *on* is
// whoever is in the roster at that moment, because that is what every instance
// builds its cursors from. From the roster read until the caller opens the mid-run
// gate, dials are refused: no gate can serve one in that window.
func (a *App) startHostSessionOn(port *link.SocketPort, signals <-chan os.Signal) error {
	quorum, capacity := a.lobbyQuorum(), a.sessionCapacity()
	addr := a.cfg.HostAddress
	if bound := port.Addr(); bound != nil {
		addr = bound.String()
	}
	a.log.Info("session", "msg", "host waiting",
		"address", addr, "quorum", quorum, "capacity", capacity)
	a.showStartupStatus(fmt.Sprintf("Hosting on %s; waiting for %d of up to %d participant(s) (Ctrl-C cancels)",
		addr, quorum, capacity))

	// The lobby is the one wait the first-guest window covers, so it is the one
	// wait that carries its deadline. Zero on an unbounded policy, and zero again
	// on the ready gate below — see the comment there.
	if err := a.waitForStartup(port, signals,
		a.life.State(time.Now()).Deadline,
		func(now time.Time) error {
			a.life.State(now) // settles the deadline so the reason is recorded once
			return errSessionExpired
		},
		func() bool { return port.PeerCount() >= quorum }); err != nil {
		return err
	}

	// Before the roster closes, so the bounds this produces are the ones the offer
	// names and the tick-zero capture carries.
	a.adoptLobbyGeometry()

	a.lobbyClosing.Store(true)
	a.settleLobbyHandshakes(port)
	offer, err := a.hostOffer()
	if err != nil {
		return err
	}
	// The window closes here rather than a second later, when the serve loop takes
	// its first reading. The roster the lobby closed on is what satisfies it, and
	// everything between this point and the loop — the capture, the sends, the
	// ready gate — happens while the deadline would otherwise still be running.
	a.life.Observe(len(offer.Roster)-1, time.Now())
	// Whoever the roster closed on, not whoever was counted a moment ago: an
	// accepted dial can complete between the quorum being met and the roster being
	// read, and that participant is in the session.
	admitted := len(offer.Roster) - 1
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
	body, tick, err := a.corrections.KeyframeAt(0, time.Now().Add(parameter.NetworkJoinReadyTimeout))
	if err != nil {
		return err
	}
	// Every guest receives the same roster and the same capture, which is what makes
	// shared creation order identical.
	guests := make([]link.PeerID, 0, admitted)
	for _, participant := range offer.Roster {
		if participant.ID != offer.Host {
			guests = append(guests, participant.ID)
		}
	}
	if _, err := sendGate(port, offer, tick, body, guests...); err != nil {
		return err
	}
	// Bound the install separately from first arrival; closed links count as abandoned.
	abandoned := make(map[link.PeerID]bool, admitted)
	confirmedGuests := func() int {
		n := 0
		for _, id := range guests {
			if port.Confirmed(uint32(id)) {
				n++
			}
		}
		return n
	}
	// Whoever is still on the link, not the roster the lobby closed on. A peer that
	// drops in this window cannot redial — lobbyClosing refuses it — so waiting for
	// it is waiting for nobody, and ending the session on its behalf hands a
	// stranger the power to end one.
	if err := a.waitForStartup(port, signals,
		time.Now().Add(parameter.NetworkJoinReadyTimeout),
		func(now time.Time) error {
			for _, id := range guests {
				if port.Confirmed(uint32(id)) {
					continue
				}
				abandoned[id] = true
				port.Disconnect(uint32(id))
				a.log.Warn("session", "msg", "participant did not confirm the start gate",
					"peer", id, "within", parameter.NetworkJoinReadyTimeout.String())
			}
			return nil
		},
		func() bool {
			for _, id := range guests {
				if abandoned[id] {
					continue
				}
				if port.Connected(uint32(id)) && !port.Confirmed(uint32(id)) {
					return false
				}
			}
			return true
		}); err != nil {
		return err
	}

	// The lobby's links have been up for the whole wait, so the convergence floor is
	// decided per link. A participant that cannot carry a whole world per floor
	// window is dropped like one that never confirmed: it leaves through the ordinary
	// departure the loop is about to drain, and the others keep their match.
	for _, id := range guests {
		if !port.Connected(uint32(id)) {
			continue
		}
		if err := a.corrections.AdmitMeasuredLink(port, id); err != nil {
			port.Disconnect(uint32(id))
			a.log.Warn("session", "msg", "participant refused at the start gate",
				"peer", id, "error", err.Error())
		}
	}

	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participant(s), %d of %d guest(s) confirmed",
		offer.ParticipantCount(), confirmedGuests(), admitted))
	a.corrections.StartPump()
	return nil
}

// settleLobbyHandshakes waits until every guest the roster names is on the link. A
// dial is rostered when it is assigned, before its handshake ends, and the gate sends
// to every roster entry; a handshake that fails releases its entry, so this ends
// within one handshake's bound.
func (a *App) settleLobbyHandshakes(port *link.SocketPort) {
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound
	for a.guestCount() > port.PeerCount() && time.Now().Before(deadline) {
		select {
		case <-port.Changes():
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// startJoinSession completes the tick-zero gate before the socket port owns the
// stream. The roster arrives with the gate, not with the offer: a joiner that
// dialled early saw only the participants ahead of it.
func (a *App) startJoinSession(signals <-chan os.Signal) error {
	if a.pendingJoin == nil {
		return errors.New("join session has no pending stream")
	}
	a.showJoinStatus("Join accepted; waiting for host start gate")
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
	a.showJoinStatus(fmt.Sprintf("Installing the session world at tick %d (%d bytes)",
		cap.Header.Tick, len(body)))
	if err := a.JoinSessionAt(final, cap, len(body)); err != nil {
		return fmt.Errorf("join roster: %w", err)
	}
	if err := a.pendingJoin.Ready(); err != nil {
		return fmt.Errorf("join ready gate: %w", err)
	}
	a.showStartupStatus(fmt.Sprintf("Network session ready: %d participant(s)",
		final.ParticipantCount()))
	return nil
}

// awaitStartGate reads the host's start record without freezing this instance. The
// gate carries no deadline — it is a human-paced wait — so the read runs on a
// goroutine while the terminal is polled here, and a join stays answerable to keys
// and signals. Cancelling closes the stream, which turns the read in flight into an
// error rather than a goroutine outliving the run.
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
	cancel := func(why error) (network.SessionOffer, error) {
		_ = a.pendingJoin.Close()
		<-done
		return network.SessionOffer{}, why
	}

	for {
		select {
		case g := <-done:
			if g.err != nil {
				return network.SessionOffer{}, fmt.Errorf("join start gate: %w", g.err)
			}
			return g.offer, nil
		case <-signals:
			return cancel(errSessionSignalled)
		case ev := <-a.lobbyEvents():
			if a.lobbyEventCancels(ev) {
				return cancel(errSessionCanceled)
			}
		}
	}
}

// Rejected handshakes are recoverable until the caller's lobby or install deadline.
func (a *App) waitForStartup(port *link.SocketPort, signals <-chan os.Signal,
	deadline time.Time, expire func(time.Time) error, ready func() bool) error {
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
			expiry = nil
			if err := expire(now); err != nil {
				return err
			}
		case ev := <-a.lobbyEvents():
			if a.lobbyEventCancels(ev) {
				return errSessionCanceled
			}
		case err := <-port.Errors():
			logSessionError(err)
			a.showStartupStatus("Join rejected: " + err.Error() + "; still waiting")
		case <-port.Changes():
		}
	}
	return nil
}

// socketPort returns the concrete startup endpoint contributed by NetworkService.
func (a *App) socketPort() (*link.SocketPort, error) {
	if a.networkSvc == nil || a.networkSvc.Port() == nil {
		return nil, errors.New("network session has no socket port")
	}
	return a.networkSvc.Port(), nil
}

// activateNetworkSession closes the crossing window before terminal input is read,
// and opens the relay address once the session it serves exists.
func (a *App) activateNetworkSession() {
	a.world.RunSafe(a.activateNetworkSessionLocked)
	a.serveRelay()
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

// barrierLocked reports whether this run's crossing barrier owns its crossings.
// Caller MUST hold updateMutex.
func (a *App) barrierLocked() bool {
	for _, sys := range a.world.Systems() {
		if b, ok := sys.(interface{ BarrierActive() bool }); ok {
			return b.BarrierActive()
		}
	}
	return false
}

func logSessionError(err error) {
	if err != nil {
		vlog.Warn("session", "msg", "session not run", "error", err.Error())
	}
}
