package membership

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/app/internal/scribe"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/status"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Control is the membership's half of the operator's engine.SessionController:
// hosting, joining and leaving, the scenario, the bots this run holds and the
// players its host may drop. Its caller holds the world lock.
type Control struct{ m *Membership }

func (c Control) BeginHosting(addr, authority string) error {
	return c.m.beginHostingLocked(addr, authority)
}
func (c Control) Join(target string) error { return c.m.joinLocked(target) }
func (c Control) SessionSummary() string   { return c.m.sessionSummaryLocked() }
func (c Control) DropBot(slot int) error {
	if err := c.m.dropSeat(slot); err == nil {
		return nil
	}
	e := c.m.world.Resources.Player.Slot(uint8(slot))
	cursor, ok := c.m.world.Components.Cursor.GetComponent(e)
	if !ok || cursor.Holder == 0 {
		return fmt.Errorf("no bot on slot %X", slot)
	}
	return c.m.dropPlayerLocked(e)
}
func (c Control) RemoveBot(id uint64) error { return c.m.removeSeat(id) }
func (c Control) BotSummary() string        { return c.m.seatsSummary() }
func (c Control) Bots() []engine.BotSeat    { return c.m.botSeats() }
func (c Control) HostError() error          { return c.m.hostErrorLocked() }
func (c Control) JoinError() error          { return c.m.joinErrorLocked() }
func (c Control) Leave() error              { return c.m.leaveLocked() }
func (c Control) LeaveError() error         { return c.m.leaveErrorLocked() }

// SetPassphrase sets what this run's doors take and its dials show: the phrase
// given, or one made for "new" or none, returned to be shown once. A door derived
// already is derived again at its next open.
func (c Control) SetPassphrase(phrase string) (string, error) {
	if phrase == "" || phrase == "new" {
		phrase = network.GeneratePassphrase()
	}
	if err := network.CheckPassphrase(phrase); err != nil {
		return "", err
	}
	c.m.sessionMu.Lock()
	defer c.m.sessionMu.Unlock()
	c.m.cfg.Passphrase = phrase
	c.m.door.Close()
	c.m.door = nil
	return phrase, nil
}

// passphraseDoor is the credential this run's doors take: the session's verifier
// where it joined one (SessionOffer.Door), else one derived from its own passphrase
// at the first door's open, once; nil opens them. One that cannot be had leaves the
// doors gated rather than open, since what gates them stands.
func (m *Membership) passphraseDoor() *network.PassphraseDoor {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.passphraseDoorLocked()
}

// passphraseDoorLocked is passphraseDoor for a caller holding sessionMu.
func (m *Membership) passphraseDoorLocked() *network.PassphraseDoor {
	if m.door != nil {
		return m.door
	}
	var d *network.PassphraseDoor
	var err error
	switch {
	case m.sessionOffer.Door != nil:
		d, err = network.OpenDoor(m.sessionOffer.Door)
	case m.cfg.Passphrase != "":
		d, err = network.NewPassphraseDoor(m.cfg.Passphrase)
	default:
		return nil
	}
	if err != nil {
		m.log.Error("session", "msg", "passphrase door not opened", "error", err.Error())
		return nil
	}
	m.door = d
	return d
}

// gated reports a session whose doors take no bare key: this run's passphrase, or
// its host's, which the offer says (Coordinator.Gated).
func (m *Membership) gated() bool {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.gatedLocked()
}

// gatedLocked is gated for a caller holding sessionMu.
func (m *Membership) gatedLocked() bool {
	return m.cfg.Passphrase != "" || len(m.cfg.TicketKeys) != 0 || m.sessionOffer.Gated
}

// ticketHook is the door's check of the site's ticket, for a host whose doors take
// them (-admit ticket); nil for doors that take none. Every dial reaches such a
// host from the router's address, so the join budget is charged to the player the
// ticket names, the address or account the router budgeted, and not to the address.
func (m *Membership) ticketHook() func(*network.Signed, []string) (*network.TicketBody, error) {
	if len(m.cfg.TicketKeys) == 0 {
		return nil
	}
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	if m.tickets == nil {
		m.tickets = &network.TicketGate{Keys: m.cfg.TicketKeys, Session: m.cfg.SessionName}
	}
	return func(shown *network.Signed, keys []string) (*network.TicketBody, error) {
		body, err := m.tickets.Admit(shown, keys)
		if err != nil {
			return nil, err
		}
		if err := m.admissions.AdmitKey(body.Subject); err != nil {
			return nil, err
		}
		return body, nil
	}
}

// joinBudget is what a dial spends before anything is allocated for it: the
// dialer's address, or nothing at a door that takes tickets, which charges the
// ticket's subject instead once the dial shows one.
func (m *Membership) joinBudget() func(net.Addr) error {
	if len(m.cfg.TicketKeys) != 0 {
		return nil
	}
	return m.admissions.Admit
}

// doorVerifierLocked is the verifier an offer of this run's carries, the session's
// or its own door's; nil where its doors are open. Caller holds sessionMu.
func (m *Membership) doorVerifierLocked() *network.DoorVerifier {
	return m.passphraseDoorLocked().Verifier()
}

// Register, Login and Logout run a site's account exchange in the background: the
// KDF and the requests take seconds the world lock cannot wait, so the status line
// says how it went. A native run keeps the sign-in for the site's front door; a page
// asks for the cookie its browser keeps, and the router reads it on the upgrade.
func (c Control) Register(site, name, password string) error {
	site, err := accountSite(site)
	if err != nil {
		return err
	}
	go func() {
		if err := network.Register(site, name, password, parameter.NetworkSessionRequestTimeout); err != nil {
			c.m.ctx.SetStatusMessage("Registration at "+siteHost(site)+" failed: "+err.Error(), parameter.StatusAlert)
			return
		}
		c.m.ctx.SetStatusMessage("Registered "+name+" at "+siteHost(site)+"; :login signs in", parameter.StatusNotice)
	}()
	return nil
}

func (c Control) Login(site, name, password string) error {
	site, err := accountSite(site)
	if err != nil {
		return err
	}
	cookie := !paths.ExternalFiles()
	go func() {
		token, err := network.SignIn(site, name, password, deviceName(), cookie, parameter.NetworkSessionRequestTimeout)
		if err != nil {
			c.m.ctx.SetStatusMessage("Sign-in to "+siteHost(site)+" failed: "+err.Error(), parameter.StatusAlert)
			return
		}
		if !cookie {
			c.m.wiring.SignIns.Set(site, token)
		}
		c.m.ctx.SetStatusMessage("Signed in to "+siteHost(site)+" as "+name, parameter.StatusNotice)
	}()
	return nil
}

func (c Control) Logout(site string) error {
	site, err := accountSite(site)
	if err != nil {
		return err
	}
	token := c.m.wiring.SignIns.Forget(site)
	if token == "" && paths.ExternalFiles() {
		return errors.New("not signed in to " + siteHost(site))
	}
	go func() {
		if err := network.SignOut(site, token, parameter.NetworkSessionRequestTimeout); err != nil {
			c.m.ctx.SetStatusMessage("Sign-out from "+siteHost(site)+" failed: "+err.Error(), parameter.StatusAlert)
			return
		}
		c.m.ctx.SetStatusMessage("Signed out from "+siteHost(site), parameter.StatusNotice)
	}()
	return nil
}

// accountSite is the site an account command names, as an https origin: a bare
// host is one, and a link to one of its sessions names it too.
func accountSite(target string) (string, error) {
	e, err := link.ParseEndpoint(target)
	if err != nil {
		return "", err
	}
	if e.Scheme == link.SchemeSite {
		return e.Addr, nil
	}
	if host := siteHost(target); host != "" {
		return "https://" + host, nil
	}
	return "", errors.New("a site is an https:// origin or its host")
}

// deviceName is what a sign-in is listed under among the account's: this machine's.
func deviceName() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "vif"
}

func (c Control) DropPlayer(slot int) error {
	if slot < 0 || slot >= parameter.MaxPlayers {
		return errors.New("invalid player slot")
	}
	return c.m.dropPlayerLocked(c.m.world.Resources.Player.Slot(uint8(slot)))
}

func (c Control) RemovePlayer(entity core.Entity) error {
	return c.m.dropPlayerLocked(entity)
}

func (c Control) Participants() []engine.SessionParticipant {
	var out []engine.SessionParticipant
	for slot := range parameter.MaxPlayers {
		e := c.m.world.Resources.Player.Slot(uint8(slot))
		if p, ok := c.m.world.Components.Cursor.GetComponent(e); ok {
			out = append(out, engine.SessionParticipant{Entity: e, Slot: p.Slot, Holder: p.Holder, Name: p.Name,
				Local: c.m.world.SimulatesLocally(e), Away: c.m.world.Components.Wraith.HasEntity(e)})
		}
	}
	return out
}

// The cursor entity prevents an open menu from dropping a replacement in its slot.
func (m *Membership) dropPlayerLocked(entity core.Entity) error {
	if !m.world.IsSessionCoordinator() {
		return errors.New("only the host can drop other players or their bots")
	}
	p, ok := m.world.Components.Cursor.GetComponent(entity)
	if !ok {
		return errors.New("that player has already left")
	}
	if m.world.SimulatesLocally(entity) {
		return errors.New("use :leave to leave the session")
	}
	m.world.PushEventFull(event.EventParticipantDeparted,
		&event.ParticipantDepartedPayload{Participant: p.PeerID, Slot: p.Slot, Dismissed: true, Dropped: true},
		event.OriginSession, core.DomainPlayer)
	return nil
}

func (c Control) RequestSession(site string, players int, scenario string) error {
	e, err := link.ParseEndpoint(site)
	if err != nil {
		return err
	}
	if e.Scheme != link.SchemeSite {
		return errors.New("router site must be an http:// or https:// URL")
	}
	if players < 0 || players > parameter.MaxPlayers {
		return fmt.Errorf("players must be 0 (server default) or 1..%d", parameter.MaxPlayers)
	}
	return c.m.joinSessionLocked(site, players, scenario)
}

func (c Control) AddBot(graph string) error {
	if c.m.dialling.Load() {
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
		if err := c.m.addSeatLocked(spec); err != nil {
			return err
		}
	}
	return nil
}

func (c Control) ChangeScenario(name string) (bool, error) {
	return c.m.changeScenarioLocked(name)
}

// The embedded scenario answers to the main name, which is how a row restarts it,
// and one started by a path below scenario/ to that path, as the list names it.
func (c Control) Scenarios() ([]string, string) {
	o := c.m.run.Resources()
	names, current := resource.Scenarios(o), c.m.scenario.Name
	switch {
	case current == resource.EmbeddedLabel:
		current = paths.MainScenarioName
	case o.Provided == nil && slices.Contains(names, o.Scenario):
		current = o.Scenario
	}
	return names, current
}

// Validate before teardown; a different scenario needs a new run and metric set.
// Caller holds updateMutex.
func (m *Membership) changeScenarioLocked(name string) (bool, error) {
	// The session's rule first: it holds whatever shape this runtime is, and it is
	// the more useful of the two answers to a participant that asked.
	port := m.sessionTransportLocked()
	if port != nil && !m.world.IsSessionCoordinator() {
		return false, errors.New("only the host can change a live session's scenario")
	}
	if mode := m.run.Mode(); mode != config.ModePlay {
		return false, fmt.Errorf("%s mode has no restart loop", mode)
	}
	// A scenario received from a coordinator answers its own name and no other.
	o := m.run.Resources()
	o.Scenario, o.Embedded = name, false
	if o.Provided != nil && o.Provided.Name != name {
		o.Provided = nil
	}
	sc, err := resource.ValidateScenario(o)
	if err != nil {
		return false, err
	}
	if sc.Digest() == m.scenario.Digest() {
		return false, nil // already running these bytes; the caller resets instead
	}
	if err := m.PlayersFit(sc); err != nil {
		return false, err
	}
	req := &RestartAsk{Scenario: name}
	if port != nil {
		// Where this run's participants come back to. The address it already
		// listens on when it opened the session; the one it advertised when it
		// inherited the session from somebody else. A run with neither has no door
		// to offer, and saying so is better than sending its peers to an address
		// that stopped answering; its own bots need none, the next run reseats them.
		req.Host = m.cfg.HostAddress
		if req.Host == "" {
			req.Host = m.reach.DeclaredAddr()
		}
		if req.Host == "" && !m.seatsOnly() {
			return false, errors.New("this participant has no address its session can return to")
		}
	}
	if !m.run.ReplaceWith(req) {
		return false, scribe.ErrRunReplaced
	}
	if port != nil {
		// Said before this run's transport goes, because afterwards there is nobody
		// to say it to. All it carries is where to come back: the offer a
		// participant is given when it redials names what is being played.
		port.Broadcast(uint8(network.MsgSessionRestart), []byte(req.Host))
	}
	m.log.Info("app", "msg", "scenario change requested",
		"scenario", sc.Name, "digest", sc.Short(), "host", req.Host)
	return true, nil
}

// A restart follows the authority's current address, which may have migrated.
// Called under the world lock; the restart loop consumes the atomic request.
func (m *Membership) receiveSessionRestart(from uint32, addr string) {
	if m.run.Mode() != config.ModePlay || !m.sessionRoleLocked().joined() {
		return // a coordinator hears its own broadcast back on a mesh; a driven run has no loop
	}
	// A host's own doors are bind addresses with no host, and say the session stayed
	// where this run joined it; a successor's address names a host, and says where.
	if doors, err := link.Doors(addr); err == nil && doors[0].Hostless() {
		addr = ""
	}
	if m.run.ReplaceWith(&RestartAsk{Rejoin: true, Join: addr}) {
		m.ctx.SetStatusMessage("Session restarting; rejoining "+link.Shown(cmp.Or(addr, m.cfg.JoinAddress))+"...", parameter.StatusProgress)
		m.log.Info("session", "msg", "session restarting", "authority", from, "dial", addr)
	}
}

// joinLocked dials target while this run plays on, and replaces the run only once
// the host has admitted it: a refusal leaves the game as it was. A run left alone
// by its session may join another, and so may one whose session is its own bots',
// which the next run seats in the new one. Caller MUST hold updateMutex.
func (m *Membership) joinLocked(target string) error {
	return m.joinSessionLocked(target, 0, "")
}

// sessionRole is what a run is to its session, read from what the session wrote
// rather than kept beside it. A guest that took the term over hosts at its own
// port, which takes joins as the first host's did, until :host opens the door it
// names; from then on it is a host like the one that opened the session.
type sessionRole uint8

const (
	roleSolo      sessionRole = iota // in no session
	roleSeats                        // in one it opened for its own bots, with no door
	roleHost                         // coordinating one others can join
	roleServer                       // coordinating one it plays no part in
	roleGuest                        // in one it joined
	roleLeft                         // in one it joined, which has nobody left but its own bots
	roleSuccessor                    // coordinating one it joined, at its own port
)

const inSession = "this run is already in a session"

// sessionRoles is why each role refuses :join, :host and :leave, empty where it
// takes them.
var sessionRoles = [...]struct{ join, host, leave string }{
	roleSolo:      {"", "", "this run is in no session"},
	roleSeats:     {"", "", "this run's session holds only its own bots"},
	roleHost:      {inSession, "this run is already hosting", ""},
	roleServer:    {inSession, "this run is already hosting", "a server stays in the session it serves"},
	roleGuest:     {inSession, inSession, ""},
	roleLeft:      {"", inSession, ""},
	roleSuccessor: {inSession, "", ""},
}

func (r sessionRole) joined() bool { return r == roleGuest || r == roleLeft }

// sessionRoleLocked is the role this run holds now. Caller MUST hold updateMutex.
func (m *Membership) sessionRoleLocked() sessionRole {
	m.sessionMu.Lock()
	opened := m.midRunPort != nil
	m.sessionMu.Unlock()
	switch {
	case m.run.Mode().Serves():
		return roleServer
	case m.sessionTransportLocked() == nil:
		return roleSolo
	case m.cfg.JoinAddress != "" && m.world.IsSessionCoordinator():
		return roleSuccessor
	case m.cfg.JoinAddress != "" && m.seatsOnly():
		return roleLeft
	case m.cfg.JoinAddress != "":
		return roleGuest
	case opened && m.cfg.HostAddress == "":
		return roleSeats
	}
	return roleHost
}

func (m *Membership) joinErrorLocked() error {
	if mode := m.run.Mode(); mode != config.ModePlay {
		return fmt.Errorf("%s mode has no restart loop", mode)
	}
	if why := sessionRoles[m.sessionRoleLocked()].join; why != "" {
		return errors.New(why)
	}
	if m.dialling.Load() {
		return errors.New("a join is already being dialled")
	}
	return nil
}

func (m *Membership) joinSessionLocked(target string, players int, scenario string) error {
	if err := m.joinErrorLocked(); err != nil {
		return err
	}
	// A site is asked for a session off the lock, since it answers once the pod is up.
	site, err := link.ParseEndpoint(target)
	next, wire, base := *m.cfg, *m.wiring, m.run.JoinBase()
	if err == nil && site.Scheme != link.SchemeSite {
		if next, err = Joining(*m.cfg, target); err == nil {
			err = base.ValidateWith(next)
		}
	}
	if err != nil {
		return err
	}
	if !m.dialling.CompareAndSwap(false, true) {
		return errors.New("a join is already being dialled")
	}
	// Progress rather than a notice: a site answers once its session's pod is up.
	if site.Scheme == link.SchemeSite {
		m.ctx.SetStatusMessage("Requesting a session from "+site.Addr+"...", parameter.StatusProgress)
	} else {
		m.ctx.SetStatusMessage("Joining "+link.Shown(target)+"...", parameter.StatusProgress)
	}
	m.log.Info("session", "msg", "join requested", "target", target)
	core.Go(func() {
		defer m.dialling.Store(false)
		door, err := target, error(nil)
		if site.Scheme == link.SchemeSite {
			var fingerprint string
			if door, fingerprint, err = network.RequestSession(site.Addr, players, scenario, m.wiring.SignIns.Token(site.Addr)); err == nil {
				m.ctx.SetStatusMessage("Joining "+link.Shown(door)+"...", parameter.StatusProgress)
				if next, err = Joining(next, door); err == nil {
					err = base.ValidateWith(next)
				}
			}
			// A door the router made its own certificate for is pinned by the
			// fingerprint the site handed out with the link, where -ca named nothing.
			if err == nil && fingerprint != "" && next.Trust == nil {
				next.Trust, err = link.TrustTLS(fingerprint)
			}
		}
		var d *JoinDial
		if err == nil {
			d, err = DialJoin(next, wire, base)
		}
		switch {
		case err != nil:
			m.log.Warn("session", "msg", "join failed; playing on", "target", target, "error", err.Error())
			m.ctx.SetStatusMessage("Join failed: "+err.Error(), parameter.StatusAlert)
		case !m.run.ReplaceWith(&RestartAsk{Join: door, Dialled: d, Press: true}):
			d.Abandon(errors.New("the run that dialled is being replaced"))
		}
	})
	return nil
}

// The authority policies a host names: what losing the authoring participant
// does. Host ends the session there; migrate hands it down the succession list.
const (
	AuthorityHost    = "host"
	AuthorityMigrate = "migrate"
)

// BeginHosting opens a running instance to participants, for a caller that holds
// no lock. The operator command path reaches beginHostingLocked instead.
func (m *Membership) BeginHosting(addr string) error {
	m.run.Settle()
	var err error
	m.world.RunSafe(func() { err = m.beginHostingLocked(addr, "") })
	return err
}

func (m *Membership) hostErrorLocked() error {
	if !config.SocketNetwork {
		return config.ErrBrowserHosting
	}
	if m.dialling.Load() {
		return errors.New("host: a join is already being dialled")
	}
	// A session this run opened without a door holds only its bots: :host opens
	// that one to others rather than a second.
	if why := sessionRoles[m.sessionRoleLocked()].host; why != "" {
		return errors.New("host: " + why)
	}
	return nil
}

// The world lock keeps admission from reading a partly attached session.
func (m *Membership) beginHostingLocked(addr, authority string) error {
	if err := m.hostErrorLocked(); err != nil {
		return err
	}
	if addr == "" {
		return errors.New("host: no address")
	}
	doors, err := link.Doors(addr)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	addr = link.DoorList(doors)
	successor := m.sessionRoleLocked() == roleSuccessor
	switch authority {
	case "", AuthorityMigrate:
	case AuthorityHost:
		// A session that changed hands migrates, and every player in it holds that:
		// ending it with this run would have them read its loss in two ways.
		if successor {
			return errors.New("host: this session migrates, as its players already hold")
		}
	default:
		return fmt.Errorf("host: authority %q is not %q or %q", authority, AuthorityHost, AuthorityMigrate)
	}
	if successor {
		return m.hostTakenSessionLocked(addr)
	}
	fixed := m.cfg.FixedAuthority
	if authority != "" {
		m.cfg.FixedAuthority = authority == AuthorityHost
	}

	// Bound before anything is written, so a port this run cannot have leaves it as
	// it was; a startup -host bound them before the terminal started.
	lns := m.wiring.HostListeners
	m.wiring.HostListeners = nil
	if lns == nil {
		if lns, err = BindDoors(*m.cfg, addr); err != nil {
			m.cfg.FixedAuthority = fixed
			return fmt.Errorf("host %s: %w", addr, err)
		}
	}
	if m.sessionTransportLocked() == nil {
		if err := m.openSessionLocked(nil); err != nil {
			CloseDoors(lns)
			m.cfg.FixedAuthority = fixed
			return err
		}
	}
	m.sessionMu.Lock()
	port := m.midRunPort
	m.sessionMu.Unlock()

	for _, ln := range lns {
		if err := port.Serve(ln); err != nil {
			CloseDoors(lns)
			m.cfg.FixedAuthority = fixed
			return fmt.Errorf("host: %w", err)
		}
	}
	// A dial already in reads these only once it has the world lock this call holds.
	// The policy is the session's from here: only this run's bots were offered one.
	m.cfg.HostAddress = addr
	m.restartWaitsLocked()
	m.authority.Pin(m.cfg.FixedAuthority)
	bound := doorsText(port.Doors())
	m.log.Info("session", "msg", "hosting opened mid-run", "address", bound,
		"certificate", link.Fingerprint(m.cfg.TLS), "tick", m.run.Position().Tick, "capacity", m.SessionCapacity()+1)
	m.ctx.SetStatusMessage("Hosting on "+bound, parameter.StatusNotice)
	return nil
}

// openAwayLocked starts who is away from a session this run opens: whom it kept a
// place for, longest first. Who was on the way back to the session it left stays
// with that session, as does a place it asked a Wraith there to give up: an
// identity it hands out again is somebody new. Caller holds sessionMu and MUST hold
// updateMutex.
func (m *Membership) openAwayLocked(kept []network.RosterEntry) {
	m.awayOrder, m.yielding, m.returning = m.awayOrderLocked(kept), 0, nil
	m.dismissWraith.Store(0)
}

// doorsText names every door a port answers at, as a status line reads them.
func doorsText(doors []net.Addr) string {
	names := make([]string, len(doors))
	for i, d := range doors {
		names[i] = d.String()
	}
	return strings.Join(names, ", ")
}

// hostTakenSessionLocked is :host on a run that took its session over. Its own port
// takes joins already, so addr naming that port opens nothing more and any other
// opens a second door. From here it hosts as the run that opened the session did:
// the address it joined at names a run that is gone, and a restart reopens addr.
func (m *Membership) hostTakenSessionLocked(addr string) error {
	port, _ := m.sessionTransportLocked().(*link.SocketPort)
	if port == nil {
		return errors.New("host: this run's session has no socket port")
	}
	doors, _ := link.Doors(addr) // read by the caller
	var opened []net.Listener
	for _, d := range doors {
		if port.ListensOn(d.Addr) {
			continue
		}
		ln, err := link.Listen(d.Addr, hostNetwork(*m.cfg, d.Addr))
		if err != nil {
			CloseDoors(opened)
			return fmt.Errorf("host %s: %w", d.Addr, err)
		}
		opened = append(opened, ln)
	}
	for _, ln := range opened {
		if err := port.Serve(ln); err != nil {
			CloseDoors(opened)
			return fmt.Errorf("host: %w", err)
		}
	}
	bound := doorsText(port.Doors())
	m.cfg.HostAddress, m.cfg.JoinAddress = addr, ""
	m.log.Info("session", "msg", "hosting opened on a session taken over",
		"address", bound, "tick", m.run.Position().Tick, "capacity", m.SessionCapacity()+1)
	m.ctx.SetStatusMessage("Hosting on "+bound, parameter.StatusNotice)
	return nil
}

// openSessionLocked opens a session this run coordinates, with no door: its bots'
// streams it opens itself, and :host opens the door. A run that leaves its session
// opens its next one here too, with the places it kept, each away. Caller MUST hold
// updateMutex.
func (m *Membership) openSessionLocked(kept []network.RosterEntry) error {
	// Opening a session writes the world — owners, the latch, the lead — so a
	// journal records the write as it records a join's, and a replay makes it.
	journal := m.world.Resources.Event.Queue.Journal()
	place, mark := m.run.Position(), journal.Mark()
	before, err := m.glue.CaptureSharedLocked()
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}

	// A run goes on under the identity it already has: what the session it left
	// still holds here names it, and a run in none takes the session's first.
	self := network.RosterEntry{
		ID:   cmp.Or(link.PeerID(m.world.LocalParticipant()), engine.CoordinatorParticipant),
		Slot: m.world.Resources.Player.LocalSlot(),
	}

	// The roster ceiling is the port's from here: an unset -players already means
	// the whole roster, and a run that opens a session mid-game has no lobby for the
	// flag's other meaning.
	netCfg := m.HostNetworkConfig()
	netCfg.ParticipantID = self.ID
	port := link.NewSocketPort(netCfg)

	// Armed before any stream arrives: a run that opens a session mid-game has no
	// startup lobby, so every dial it ever sees is a mid-run join.
	m.lateJoins.Store(true)

	m.sessionMu.Lock()
	m.midRunPort = port
	self.Key = m.ownKey()
	m.sessionRoster = append([]network.RosterEntry{self}, kept...)
	m.sessionOffer = network.SessionOffer{}
	m.keepOut = nil
	m.openAwayLocked(kept)
	roster := slices.Clone(m.sessionRoster)
	m.sessionMu.Unlock()

	// The session's world is written between two ticks, so the run takes up settle
	// group 0 as every install leaves it, and as its replay's install of it does.
	m.world.Resources.Event.Queue.RebaseStamp(place.Run, place.Tick)

	// A lobby binds this through the roster it closes on; a session opened mid-run
	// has no lobby, and an unattributed cursor is one no successor can see leave.
	m.bindCursorOwnersLocked(roster, self.ID)

	// The run that opens a session authors its first term, after any it held: a
	// handoff or report from the session it left then names a stale one. Everything
	// downstream reads authorship from here rather than from the identity the
	// handshake assigns, which is what lets a later handoff move it.
	m.openAuthorityLocked(network.SessionOffer{
		Anchor: m.joinAnchorLocked(), Host: self.ID, Assigned: self.ID,
		Term: m.authorityTerm() + 1, Roster: roster, FixedAuthority: m.cfg.FixedAuthority,
	}, self.ID)

	// Attaching latches the world as shared (D-14) and installs the session hooks;
	// activating closes the pre-session crossing window so this instance's own
	// artifacts start taking the session's playout lead.
	m.attachTransportLocked(port)
	m.ActivateNetworkSessionLocked()

	_ = port.Start() // no listener, so starting only runs it

	// The authority's cadence starts with the session. Nothing is published while
	// no peer is connected — publish returns on an empty roster — so a host waiting
	// alone pays a ticker and no world reads.
	m.corrections.StartPump()

	// The run takes its write as an install takes it, journal or not, so what an
	// install derives from the world it writes (the spatial gauges, what carriers
	// publish as they load) stands here as it does on the replay that installs it.
	after, err := m.glue.CaptureSharedLocked()
	if err == nil {
		_, err = m.glue.WriteSharedLocked(after, &before, true, false)
	}
	if err == nil && journal != nil {
		err = m.glue.JournalWritten(journal, place, mark, m.world.LocalParticipant(), m.BarrierLocked(), before, after)
	}
	if err != nil {
		m.log.Error("session", "msg", "opened session not taken as written", "error", err.Error())
		m.run.JournalLostLocked(journal, err)
	}

	m.log.Info("session", "msg", "session opened", "tick", m.run.Position().Tick)
	return nil
}

// leaveErrorLocked says why this run cannot leave its session now. Caller MUST hold
// updateMutex.
func (m *Membership) leaveErrorLocked() error {
	if mode := m.run.Mode(); mode != config.ModePlay {
		return fmt.Errorf("%s mode has no loop to leave from", mode)
	}
	if why := sessionRoles[m.sessionRoleLocked()].leave; why != "" {
		return errors.New(why)
	}
	switch {
	case m.dialling.Load():
		return errors.New("a join is already being dialled")
	case m.leaving.Load():
		return errors.New("this run is already leaving its session")
	case m.dismissed.Load() || m.run.Replacing():
		return scribe.ErrRunReplaced
	}
	return nil
}

// leaveLocked asks Loop to take this run out of its session, which it does off the
// world lock: closing a port waits for its handshakes, and they take that lock.
// Caller MUST hold updateMutex.
func (m *Membership) leaveLocked() error {
	if err := m.leaveErrorLocked(); err != nil {
		return err
	}
	m.leaving.Store(true)
	m.ctx.SetStatusMessage("Leaving the session...", parameter.StatusNotice)
	m.log.Info("session", "msg", "leave requested", "tick", m.run.Position().Tick)
	return nil
}

// LeaveSession takes this run out of its session, on :leave or once the session gave
// it up (forked), into one of its own with the world as it stands. Its bots leave
// first, as when it quits, waiting as Wraiths, and each takes its place back once the
// door it had is shut. Runs on Loop, which holds no lock: the port it closes waits
// for handshakes that take the world lock.
func (m *Membership) LeaveSession() {
	lost := ForkCell(m.world).Load() // read before the reopen clears it
	bots := m.SeatGraphs()
	m.CloseSeats(true)
	var old engine.NetworkPort
	err := m.corrections.Leave(func() error {
		var err error
		m.world.RunSafe(func() { old, err = m.leaveSessionLocked(lost) })
		return err
	})
	if port, ok := old.(interface{ Close() error }); ok {
		_ = port.Close()
	}
	m.leaving.Store(false)
	m.world.RunSafe(func() {
		held := lost && m.thawLocked()
		if err != nil {
			m.log.Warn("session", "msg", "session not left", "host_lost", lost, "error", err.Error())
		} else {
			m.log.Info("session", "msg", "session left", "tick", m.run.Position().Tick,
				"bots", len(bots), "host_lost", lost, "held", held)
		}
		m.ctx.SetStatusMessage(leftNotice(err, lost, held, len(bots) > 0))
		// Into whichever session the run is in now: the one it could not leave, else its own.
		for _, spec := range bots {
			if err := m.addSeatLocked(spec); err != nil {
				m.log.Warn("app", "msg", "bot not seated", "graph", spec, "error", err.Error())
				m.ctx.SetStatusMessage("Bots: "+err.Error(), parameter.StatusAlert)
			}
		}
	})
}

// leftNotice is what a run says once it plays on in a session of its own: after
// :leave, or after its session gave it up, lost, which is an alert.
func leftNotice(err error, lost, held, bots bool) (string, parameter.StatusKind) {
	switch {
	case err != nil && lost:
		return "Host connection lost: " + err.Error(), parameter.StatusAlert
	case err != nil:
		return "Leave: " + err.Error(), parameter.StatusAlert
	case lost && held:
		return "Host connection lost; paused in a session of your own", parameter.StatusAlert
	case lost:
		return "Host connection lost; playing on in a session of your own", parameter.StatusAlert
	case bots:
		return "Left the session; playing on with your bots", parameter.StatusNotice
	}
	return "Left the session; playing on alone", parameter.StatusNotice
}

// leaveSessionLocked reopens the world in a session of this run's own and forgets
// the one it was in, whose transport it returns for the caller to close off the
// lock. Caller MUST hold updateMutex.
func (m *Membership) leaveSessionLocked(lost bool) (engine.NetworkPort, error) {
	// A run being replaced is not left: what replaces it decides where it goes.
	if m.dismissed.Load() || m.run.Replacing() {
		return nil, scribe.ErrRunReplaced
	}
	m.takeOwnPlaceLocked()
	old, roster := m.sessionTransportLocked(), m.worldRosterLocked()
	local := link.PeerID(m.world.LocalParticipant())
	m.sessionMu.Lock()
	m.door.Close()
	m.door = nil
	m.sessionMu.Unlock()
	// A leave keeps its bots' places, and a run its session gave up every place, for
	// whoever hosts next to give back. A bot of this run's goes on under a key of its
	// own over the record it left with, which stays the left session's.
	var kept []network.RosterEntry
	var carried []*place
	for _, p := range roster {
		if p.ID == local || !lost && p.Holder != local {
			continue
		}
		if p.Holder == local {
			if c := m.places.carry(p.Key); c != nil {
				p.Key = c.id()
				carried = append(carried, c)
			}
		}
		p.Away = true
		kept = append(kept, p)
	}
	// Nothing names the session left: not the address a restart would go back to,
	// nor the name the next one would answer to, nor the ceiling it was opened with.
	cfg, wire := *m.cfg, *m.wiring
	m.cfg.JoinAddress, m.cfg.HostAddress, m.cfg.SessionName, m.wiring.SessionPlayers = "", "", "", 0
	// The place the run left keeps its record as it stands, for a return to that
	// session; the one it opens issues its own.
	prior := m.place.Swap(nil)
	if err := m.openSessionLocked(kept); err != nil {
		m.place.Store(prior)
		*m.cfg, *m.wiring = cfg, wire
		for _, c := range carried {
			m.places.forget(c)
		}
		return nil, err
	}
	if prior != nil {
		m.places.release(prior)
	}
	for _, sys := range m.world.Systems() {
		if s, ok := sys.(interface{ LeaveSession() }); ok {
			s.LeaveSession()
		}
	}
	m.reach.Close()
	m.CloseDoor()
	// Every link the drops were for closes unread with the port.
	m.seatsMu.Lock()
	m.dropped = nil
	m.seatsMu.Unlock()
	// Gone from this world, or waiting in it, before an identity is handed out again:
	// one of theirs given to a bot would be the participant a departure still
	// pending removes.
	m.dropAbandonedLocked(roster, kept, local)
	m.scheduler.SettleLocked()
	return old, nil
}

// SessionTransport returns the attached endpoint, nil when this run has none. The
// question is whether a transport exists, not whether a peer is on it: a host
// waiting alone is in a session and a second :host would open a second one.
func (m *Membership) SessionTransport() engine.NetworkPort {
	var port engine.NetworkPort
	m.world.RunSafe(func() { port = m.sessionTransportLocked() })
	return port
}

// sessionTransportLocked is the same read for a caller that holds the world lock.
// Caller MUST hold updateMutex.
func (m *Membership) sessionTransportLocked() engine.NetworkPort {
	if r := m.world.Resources.Network; r != nil {
		return r.Port
	}
	return nil
}

// SessionSummary is a one-line description of what this run is part of.
func (m *Membership) SessionSummary() string {
	var out string
	m.world.RunSafe(func() { out = m.sessionSummaryLocked() })
	return out
}

// sessionSummaryLocked is the same for the operator command path.
// Caller MUST hold updateMutex.
func (m *Membership) sessionSummaryLocked() string {
	if m.sessionTransportLocked() == nil {
		if !config.SocketNetwork {
			return "Solo run; :join <link> joins a session"
		}
		return "Solo run; :host <addr> opens it to participants, :join <target> joins one"
	}
	peers := m.world.Resources.Status.Ints.Get("network.peers").Load()
	participant := m.world.LocalParticipant()
	// A guest that took the term over hosts at the address it declared, which its
	// session dials, not the one it dialled. A harness binds no address, so the word
	// stands alone rather than trailing a blank.
	where, addr := "hosting", cmp.Or(m.cfg.HostAddress, m.reach.DeclaredAddr())
	if m.sessionRoleLocked().joined() {
		where, addr = "joined", m.cfg.JoinAddress
	}
	if addr != "" {
		where += " " + addr
	}
	if fp := link.Fingerprint(m.cfg.TLS); fp != "" && !m.sessionRoleLocked().joined() {
		where += " (certificate " + fp + ")"
	}
	if m.cfg.Passphrase != "" && !m.sessionRoleLocked().joined() {
		where += " (passphrase " + m.cfg.Passphrase + ")"
	}
	slot := m.world.Resources.Player.LocalSlot()
	cursor := fmt.Sprintf("slot %d", slot)
	if slot == parameter.NoPlayerSlot {
		cursor = "no cursor"
	}
	reg := m.world.Resources.Status
	// The D-14 latch is a fact about the run rather than a thing to watch
	latch := "map open"
	if reg.Bools.Get("network.map_latched").Load() {
		latch = "map latched"
	}
	line := fmt.Sprintf("Session %s, peer %d (%s), %d peer link(s), tick %d, %s",
		where, participant, cursor, peers, m.run.Position().Tick, latch)
	if m.authority != nil {
		if s := m.authority.Summary(); s != "" {
			line += "; " + s
		}
	}
	// Reachability, which is what decides whether losing the authority moves the
	// session or forks it. The chain is the address book a survivor dials down, so
	// its size is how many peers can be reached at all.
	if n := reg.Ints.Get("network.chain").Load(); n > 0 {
		line += fmt.Sprintf(", %d confirmed reachable", n)
	}
	if doors := m.sessionChain().Doors(); len(doors) > 0 {
		line += ", relaying at " + strings.Join(doors, " ")
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
func (m *Membership) releaseMidRunJoiner(id link.PeerID) {
	// One at a time. The handshakes that reach here run concurrently, and this gate
	// waits on a ready count that is cumulative over the session: two of them at
	// once could not tell which joiner had confirmed, and the link measurement each
	// takes would be timed against the other's transfer.
	m.midRunGate.Lock()
	defer m.midRunGate.Unlock()

	m.sessionMu.Lock()
	port := m.midRunPort
	m.sessionMu.Unlock()
	if port == nil {
		// A dedicated host never opened a socket of its own: its endpoint is the
		// one NetworkService contributed for -serve.
		port, _ = m.socketPort()
	}
	if port == nil {
		return
	}
	if err := m.sendMidRunGate(port, id); err != nil {
		m.log.Warn("session", "msg", "mid-run join failed", "peer", id, "error", err.Error())
		// The stream is already a peer by the time this runs, so refusing the join
		// means dropping it: a participant holding a handshake it could not finish
		// would otherwise stay in the session receiving crossings for a world it
		// never installed. Dropping it runs the ordinary departure path, which is
		// what returns its identity to the pool.
		port.Disconnect(uint32(id))
		m.releaseParticipant(id)
	}
}

// sendMidRunGate sends one joiner the closed roster and the world it names, then
// crosses its arrival. The world is the cadence's keyframe rather than a read taken
// for this join, so two joins arriving together share one read.
func (m *Membership) sendMidRunGate(port *link.SocketPort, id link.PeerID) error {
	offer, err := m.midRunOffer(id)
	if err != nil {
		return err
	}
	tick, bytes, took, err := m.gateJoiner(port, offer, m.ctx.TimeCtl.Held(), m.corrections.KeyframeAt)
	if err != nil {
		return err
	}
	// Refused *after* the install rather than before it, because the install is
	// what completes the measurement. A participant refused here is dropped by the
	// caller and its identity returned to the pool, which is the same unwind a
	// join that could not finish its gate takes.
	if err := m.corrections.AdmitLink(port, id, bytes, took); err != nil {
		return err
	}

	assignment, _ := offer.Entry(id)
	m.crossParticipantArrival(id, assignment.Slot)
	m.log.Info("session", "msg", "mid-run participant admitted",
		"peer", id, "slot", assignment.Slot, "snapshot_tick", tick, "bytes", bytes)
	return nil
}

// gateJoiner releases the participant offer is addressed to into the world at a
// playout lead past the admission, where every epoch flushed before it has applied,
// waits for its install and says which world and how long its transfer took. A held
// session reaches no lead: the joiner is handed the held tick's world and what is
// scheduled past it, and holds there too. A new game under the gate refuses it.
func (m *Membership) gateJoiner(port *link.SocketPort, offer network.SessionOffer, held bool,
	world func(minTick uint64, deadline time.Time) ([]byte, uint64, error),
) (tick uint64, bytes int, took time.Duration, err error) {
	deadline := time.Now().Add(parameter.NetworkJoinReadyTimeout) // [wall] a link bound
	var body []byte
	var pending [][]byte
	run := offer.Anchor.Anchor.Run
	for again := true; ; again = false {
		at := m.run.Position()
		minTick := at.Tick + parameter.NetworkBarrierDelayTicks
		if held {
			minTick = at.Tick
		}
		body, tick, err = world(minTick, deadline)
		// The session left the game offer names: told now, its joiner dials the next.
		if m.run.Position().Run != run {
			port.Send(uint32(offer.Assigned), uint8(network.MsgJoinReply), network.RefusalReply(ErrSessionMoved))
			return 0, 0, 0, ErrSessionMoved
		}
		if err != nil {
			return 0, 0, 0, err
		}
		// A world short of the lead is the held tick's: a hold landed as it was read.
		if held = held || tick < minTick; !held {
			break
		}
		if pending, err = m.pendingAt(at.Run, tick); err == nil {
			break
		}
		if !again {
			return 0, 0, 0, err
		}
		held = false // the session resumed as its world was read
	}
	// A hold standing for a new game's start waits for its host's press, which the
	// joiner waits for as that, rather than as a pause.
	offer.Held, offer.Start = held, held && m.ctx.TimeCtl.Holds(engine.HoldStart)
	began, err := sendGate(port, offer, tick, body, offer.Assigned)
	for i := 0; err == nil && i < len(pending); i++ {
		if !port.Send(uint32(offer.Assigned), uint8(network.MsgEvent), pending[i]) {
			err = fmt.Errorf("could not send scheduled crossings to participant %d", offer.Assigned)
		}
	}
	if err == nil {
		err = m.awaitJoinerReady(port, offer.Assigned)
	}
	return tick, len(body), time.Since(began), err
}

// pendingAt encodes what a held session has scheduled past tick, as the epochs it
// went out in, which a joiner installed at that tick never heard. It is read only
// while the session still stands there: what a tick applies is in neither.
func (m *Membership) pendingAt(run, tick uint64) ([][]byte, error) {
	var batches []event.WireBatch
	stood := false
	m.world.RunSafe(func() {
		at := m.run.Position()
		if stood = at.Run == run && at.Tick == tick && m.ctx.TimeCtl.Held(); !stood {
			return
		}
		for _, sys := range m.world.Systems() {
			if p, ok := sys.(interface {
				PendingAfter(uint64) []event.WireBatch
			}); ok {
				batches = p.PendingAfter(tick)
			}
		}
	})
	if !stood {
		return nil, errors.New("the session resumed while its world was read")
	}
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

// sendGate releases each participant named into the world body encodes at tick: the
// roster addressed to it, then the world, one encoding shared by all. It returns when
// the last transfer started; a transfer is the measurement, timed from its first
// chunk to the joiner's confirmation, since nothing else on a fresh link has pushed
// enough bytes to say what it carries.
func sendGate(port *link.SocketPort, offer network.SessionOffer, tick uint64, body []byte, to ...link.PeerID) (time.Time, error) {
	offer.SnapshotTick, offer.SnapshotBytes = tick, len(body)
	chunks, err := link.EncodeSnapshotChunks(0, tick, body)
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
func (m *Membership) midRunOffer(id link.PeerID) (network.SessionOffer, error) {
	anchor := m.JoinAnchor()
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	offer := m.offerLocked(anchor, id)
	return offer, offer.Validate()
}

// awaitJoinerReady waits for the joiner to confirm it installed the world.
//
// The wait is bounded and the bound is the point: a participant that cannot install
// and answer within it is one whose crossings would arrive after the ticks they
// name, and admitting it would trade a failed join for a divergence.
func (m *Membership) awaitJoinerReady(port *link.SocketPort, id link.PeerID) error {
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
func (m *Membership) crossParticipantArrival(id link.PeerID, slot uint8) {
	m.world.RunSafe(func() { m.crossArrivalLocked(id, slot) })
}

// crossArrivalLocked is crossParticipantArrival under the world lock. A participant
// back in a place its key took back is no longer away from here, and brings the
// record it left with. Caller MUST hold updateMutex.
func (m *Membership) crossArrivalLocked(id link.PeerID, slot uint8) {
	m.sessionMu.Lock()
	i := slices.IndexFunc(m.sessionRoster, func(p network.RosterEntry) bool { return p.ID == id })
	if i < 0 {
		m.sessionMu.Unlock()
		return
	}
	p := &event.ParticipantJoinedPayload{Participant: uint32(id), Slot: slot, Holder: uint32(m.sessionRoster[i].Holder),
		Key: m.sessionRoster[i].Key, Name: m.sessionRoster[i].Name, Record: m.returning[id].record}
	delete(m.returning, id)
	m.sessionRoster[i].Away = false
	m.awayOrder = slices.DeleteFunc(m.awayOrder, func(away link.PeerID) bool { return away == id })
	m.sessionMu.Unlock()
	m.world.PushEventFull(event.EventParticipantJoined, p, event.OriginSession, core.DomainPlayer)
}

// ResumeJoinedSession hands the traffic the gate held to the port and closes the gap
// between the installed world and the tick the session has reached by simulating it.
// The gap is the transfer and the install, so it is a function of world size and
// link speed; left open it is permanent, and every crossing goes out k ticks late.
// Call after the transport takes the stream and before game time is released.
func (m *Membership) ResumeJoinedSession() error {
	if m.pendingJoin == nil {
		return nil
	}
	port, err := m.injectPort()
	if err != nil {
		return err
	}
	held := m.pendingJoin.Deferred()
	from := uint32(m.pendingJoin.Peer())
	for _, msg := range held {
		port.Inject(from, uint8(msg.Type), msg.Payload)
	}

	// A held session's joiner holds at the world it installed. Only the host's next
	// epoch ends that, and it may be among what the rounds below drain: then the
	// gap is closed as any other. A new game its host has yet to start holds at its
	// first tick, which is why this comes before the tick-zero case.
	if m.sessionOffer.Held {
		m.world.RunSafe(func() {
			switch {
			case m.sessionOffer.Start:
				m.ctx.HoldAs(engine.HoldStart)
			case m.ctx.Hold(true):
				m.ctx.SetStatusMessage("Paused by the host", parameter.StatusNotice)
			}
		})
	}
	// A tick-zero lobby has no gap by construction: the host is frozen at tick zero
	// until every participant is ready, so it has produced nothing, and a probe tick
	// here would put this instance one tick ahead of a session that has not started.
	if !m.sessionOffer.CarriesSnapshot() || m.sessionOffer.SnapshotTick == 0 {
		m.reportJoinLag(0)
		return nil
	}

	// The gap is only partly readable from what the gate held: epochs closed during
	// the install sat in the socket until the port started, and the barrier learns of
	// those only once something drains them. So it closes by rounds — catch up to the
	// newest tick known, let that draining reveal the next, stop when it stops moving.
	caught := uint64(0)
	for range joinCatchUpRounds {
		local := m.run.Position().Tick
		target := max(newestHeldEpoch(held), m.awaitSessionTick(local))
		if target <= local || m.ctx.TimeCtl.Held() {
			break
		}
		step := target - local
		if caught+step > parameter.NetworkJoinCatchUpTicks {
			return joinBehind("it is more than %d ticks ahead of the world it sent", parameter.NetworkJoinCatchUpTicks)
		}
		m.scheduler.RunTicks(int(step))
		caught += step
	}
	return m.finishCatchUp(held, caught)
}

// awaitSessionTick drains the transport until a peer epoch newer than local shows
// up, or the wait runs out. Every tick closes an epoch, so on a live session the
// wait is bounded by one tick interval; the ceiling is there for the session that
// has stopped producing, where the honest answer is "no newer tick" rather than a
// hang.
func (m *Membership) awaitSessionTick(local uint64) uint64 {
	deadline := time.Now().Add(joinEpochWait) // [wall] a link bound, not a game one
	for {
		m.world.RunSafe(func() {
			for _, sys := range m.world.Systems() {
				if d, ok := sys.(interface{ DrainPeers() }); ok {
					d.DrainPeers()
				}
			}
		})
		if observed := m.observedSessionTick(); observed > local {
			return observed
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(joinEpochPoll)
	}
}

// finishCatchUp reports what the join cost and refuses one it could not close.
func (m *Membership) finishCatchUp(held []*link.Message, caught uint64) error {
	remaining := m.sessionLagTicks()
	m.reportJoinLag(remaining)
	m.log.Info("session", "msg", "join caught up", "held_frames", len(held),
		"caught_up_ticks", caught, "tick", m.run.Position().Tick, "lag_ticks", remaining)
	m.glue.Telemetry().CatchUp.Store(int64(caught))
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
func (m *Membership) injectPort() (interface {
	Inject(peer uint32, msgType uint8, payload []byte)
}, error) {
	injector, ok := m.SessionTransport().(interface {
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
func (m *Membership) HostAddr() string {
	m.sessionMu.Lock()
	port := m.midRunPort
	m.sessionMu.Unlock()
	if port == nil {
		return ""
	}
	if bound := port.Addr(); bound != nil {
		return bound.String()
	}
	return m.cfg.HostAddress
}

// newestHeldEpoch is the highest production epoch among the frames the gate held.
// A frame that does not decode is skipped: the barrier will count it as a drop when
// it drains the same bytes, and guessing a tick from a broken one is worse than
// reading a smaller gap and measuring what is left.
func newestHeldEpoch(held []*link.Message) uint64 {
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
func (m *Membership) observedSessionTick() uint64 {
	var newest uint64
	m.world.RunSafe(func() {
		for _, sys := range m.world.Systems() {
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
func (m *Membership) sessionLagTicks() uint64 {
	newest := m.observedSessionTick()
	local := m.run.Position().Tick
	if newest <= local {
		return 0
	}
	return newest - local
}

// reportJoinLag publishes the measured lag beside the other transport counters.
func (m *Membership) reportJoinLag(ticks uint64) {
	m.world.RunSafe(func() {
		for _, sys := range m.world.Systems() {
			if r, ok := sys.(interface{ ReportJoinLag(uint64) }); ok {
				r.ReportJoinLag(ticks)
			}
		}
	})
}

// CloseMidRunPort releases a socket this run opened for itself.
func (m *Membership) CloseMidRunPort() {
	m.sessionMu.Lock()
	port := m.midRunPort
	m.midRunPort = nil
	m.sessionMu.Unlock()
	if port != nil {
		_ = port.Close()
	}
}
