// Package membership keeps a run's place in its session: it hosts and joins, holds
// the lobby and the start gates, admits and relays, keeps the roster, the keys a
// player comes back under and the Wraiths of whoever is away, seats the run's bots,
// and answers the half of converge.Instance the authority protocol moves. It reads
// the run through Run alone, which also plays its seats and replaces it.
package membership

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Membership is the run's place in its session: hosting and joining, the roster and
// the offer, the seats it holds, the keys it comes back with, the relay desk and
// the authority it follows or holds. It is the half of converge.Instance the
// protocol moves. It holds the session half of the run's Config (cfg) and of its
// wiring (wiring), and reads the rest of the run through Run alone.
type Membership struct {
	run Run

	world      *engine.World
	ctx        *engine.GameContext
	cfg        *config.Membership
	wiring     *Wiring
	log        *vlog.Log
	scheduler  *engine.Scheduler // wired when the run builds its own, after the world
	scenario   *resource.Scenario
	glue       *corrector.Corrector
	life       *lifecycle.Controller
	networkSvc *service.NetworkService

	// The three halves of the authority protocol, built together because they hold
	// references to each other: corrections is what this instance publishes and
	// installs, authority whether it is allowed to, and reach the links a
	// succession needs. See internal/converge.
	corrections *converge.Corrections
	authority   *converge.Authority
	reach       *converge.Reach

	dismissed  atomic.Bool // this run was dismissed from its session; the next one is what replaces it
	shed       atomic.Bool // the dismissal was its host keeping its tick
	wasDropped atomic.Bool // the dismissal was the host's drop, which keeps this run's key out
	dialling   atomic.Bool // a :join is being dialled while this run plays on
	leaving    atomic.Bool // a :leave or a fork is waiting for Loop, or being served by it

	pendingJoin *network.PendingJoin
	// landingHeld and landingStart are the hold the world a join installs lands in,
	// set around that install for the journal to record with it (LandingHold).
	landingHeld, landingStart bool
	sessionMu                 sync.Mutex
	sessionOffer              network.SessionOffer

	// sessionRoster is the lobby the coordinator has admitted so far. It grows one
	// entry per accepted connection and closes into the offer the start gate sends.
	sessionRoster []network.RosterEntry

	// awayOrder is who of the roster is away, in the order they left: a full session
	// gives a newcomer the place of the first. yielding is that one while its dismissal
	// is on its way, and dismissWraith hands it to the authority's next tick.
	awayOrder     []link.PeerID
	yielding      link.PeerID
	dismissWraith atomic.Uint32

	// returning is each place a dial took back with its key until its arrival
	// crosses, the entry as it waited, for a join that fails to give back, and the
	// record its player brought; no wait runs out on its Wraith meanwhile.
	returning map[link.PeerID]returnPlace

	// places is what this process comes back to its sessions with, and place the one
	// this run is in its session under: nil before it is in one, and once it has left.
	places *PlaceKeeper
	place  atomic.Pointer[place]

	// relayNonces are the challenges this coordinator issued to its relays' doors,
	// each good for one dial's proofs; guarded by sessionMu.
	relayNonces map[string]relayNonce

	// keepOut is the keys and accounts the session keeps out, oldest first and
	// parameter.KeepOutKept at most: the offer's or the record's, then each drop of
	// the host's as its departure applies here, so every door holds the same list.
	// keptOutSlots is the slot each key this run saw dropped held, for the refusal.
	keepOut      []string
	keptOutSlots map[string]uint8

	// door is the credential this run's doors take a passphrase against, derived
	// from cfg.Passphrase when the first door opens; nil opens them. tickets is the
	// gate a fleet pod's doors take the site's tickets at; nil takes none.
	door    *network.PassphraseDoor
	tickets *network.TicketGate

	// midRunPort is the socket a solo run opened for itself, with :host or with no
	// listener for its bots. A run started with -host takes its endpoint from
	// NetworkService instead, which the hub owns and closes; this one is owned here
	// because nothing else knows it exists.
	midRunPort *link.SocketPort

	// lateJoins arms the mid-run gate on a host whose startup lobby has closed. It
	// is a flag rather than a late assignment because the accept goroutine reads
	// the hook: the gate exists from construction and answers nothing until the
	// lobby's own gate is done, which is what keeps the two from racing.
	lateJoins atomic.Bool

	// lobbyClosing refuses dials for the window between the startup lobby reading
	// its roster and lateJoins arming. Neither gate can serve a dial there — the
	// lobby's offers are already out, and the mid-run gate waits on a capture a
	// clock that has not started never produces — so the honest answer is a
	// refusal the dialer can retry rather than an identity and a silent wait.
	lobbyClosing atomic.Bool

	// midRunGate serialises the mid-run admission. The handshakes that reach it run
	// concurrently, but the gate they call reads a whole world and then waits on a
	// session-cumulative ready count, so two of them at once cannot tell whose
	// joiner confirmed.
	midRunGate sync.Mutex

	// firstPerson is the geometry the first person to join reported, and personJoined
	// whether one has, both guarded by sessionMu. A dedicated host with no -size takes
	// its map from that report, the only real terminal the session ever sees: a seat
	// reports its holder's screen or a bot's default, and sizes nothing.
	firstPerson  network.JoinerReport
	personJoined bool

	// admissions bounds how often one dialling host may be admitted. It is built
	// with the run (Open) rather than with the session because a run can open one
	// later with :host, and a budget that started when hosting did would be a
	// budget reset by whatever closed the last session.
	admissions *network.AdmissionLimiter

	hostResumed bool // a held guest's drain admitted the host's resume; world lock

	// seats are the bots this run holds. seatsMu is taken under the world lock and
	// never takes it.
	seatsMu    sync.Mutex
	seats      []*seat
	dropped    []uint32 // seats told to leave, until their links close
	seatSerial uint64

	relay relayDesk
	// tunnelStop ends the door this run publishes through a router (publishDoor).
	tunnelMu   sync.Mutex
	tunnelStop context.CancelFunc

	// joinedThrough is the relay this run joined the session through, zero on a
	// dial to the authority; the barrier reaches the authority through it.
	joinedThrough link.PeerID
}

// Run is what the membership reads of the run beyond its world and its session
// half: its mode, script and geometry and the files it may load, where it stands and
// how it is paced, the lobby it shows on, the journal and identity it writes a
// session into, the Config and wiring a join builds on, the vacancy a dedicated host
// resumes from, the run that replaces this one, and the screen and runs it plays its
// seats in.
type Run interface {
	Mode() config.Mode
	Scripted() bool
	GeometryDefaulted() bool
	Resources() resource.Options
	JoinBase() JoinBase
	Position() event.Stamp
	Settle()
	Resize(width, height int)
	ShowStartupStatus(message string)
	ShowJoinStatus(message string)
	LobbyEvents() <-chan terminal.Event
	LobbyEventCancels(ev terminal.Event) bool
	BuildAnchor() event.JournalAnchor
	SessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField
	JournalLostLocked(j *event.Journal, err error)
	ResumeVacant()
	ReplaceWith(ask *RestartAsk) bool
	Replacing() bool
	SeatScreenLocked() (width, height int)
	PlaySeat(p SeatPlay, seated func(run *Membership, d *bot.Driver) (slot int)) (finished bool, err error)
}

// RestartAsk is what the membership asks the run that replaces this one to be
// (Run.ReplaceWith), which the run's own request carries.
type RestartAsk struct {
	Notice string
	// Scenario replaces -s. Empty keeps the command line's, which is what a guest
	// rebuilding into a session that is about to re-offer it one wants.
	Scenario string

	// Host is the address to open hosting on once the next run's clock is running.
	// A coordinator changing scenario does not go back into a startup lobby: its
	// guests are already redialling, and the mid-run gate is the door they arrive
	// at — the same one a reconnect has always used.
	Host string

	// Rejoin says the next run follows this session rather than leads it, and Join
	// where to dial now that succession moved the door. Join alone is a :join target
	// that Dialled holds already admitted; Dialled alone is this run's own door,
	// redialled once its host went. With none the next run is solo, which is what an
	// authority with an empty roster has become.
	Rejoin  bool
	Join    string
	Dialled *JoinDial

	// Press says the next run's player started its join, so its seat waits for
	// their press.
	Press bool
}

// JoinBase is the run's Config and wiring as a join dialled from it builds on, copied
// where the dial starts so the dial reads nothing of the run's own: a session half is
// checked against it (ValidateWith) and a session's scenario prepared on it (Prepare).
type JoinBase interface {
	ValidateWith(s config.Membership) error
	Prepare(s config.Membership, pending *network.PendingJoin, preview network.SessionOffer) (resource.Options, error)
}

// Wiring is the session half of a run's wiring, which the membership holds
// beside Membership: the seat a bot's run is, the places the process comes back
// with, the ceiling of a session it joined, the doors Run bound for it, and the
// press its join waits for.
type Wiring struct {
	// Holder is the participant whose bot this run is, zero for any other run.
	Holder link.PeerID
	Relay  func() (net.Conn, error) // a bot's stream to its holder; see seatLink

	// Places is the process's place keeper, which every run Run builds and every seat
	// they hold shares; Normalize gives a run built another way one of its own.
	Places *PlaceKeeper

	// SignIns is the process's sign-ins by site, which a dial to a site's front door
	// shows over TLS; every run of the process shares one.
	SignIns *SignInKeeper

	// Links signs this run's peer links and checks the far end's against the roster,
	// bound to the run's reach once it is built (converge.PeerLinkGate).
	Links network.PeerKeys

	// SessionPlayers is the -players of the session this run joined, which it holds
	// to should it take that session over; Participants is a host's own.
	SessionPlayers int

	// HostListeners are the first run's -host doors, bound before its terminal
	// started; the run serves them once its clock runs.
	HostListeners []net.Listener

	// JoinPress says this run's join is one its player started, at launch or from a
	// :join, whose seat waits for their press (joinWaits). A run dialled on
	// its own, or whose player already pressed, takes its seat once its world is built.
	JoinPress bool
}

// Joining is s pointed at the session a :join names, as -join would have built it.
// -players and -authority go: they set a session this run no longer hosts, and the
// one it joins has its own, which its offer carries.
func Joining(s config.Membership, target string) (config.Membership, error) {
	e, err := link.ParseEndpoint(target)
	if err != nil {
		return s, err
	}
	s.HostAddress, s.Participants, s.FixedAuthority = "", 0, false
	s.JoinAddress, s.SessionName = e.Dialable(), e.Name
	return s, nil
}

// hostNetwork is the transport the doors s's run opens at addr are served with.
func hostNetwork(s config.Membership, addr string) *link.Config {
	cfg := network.DebugConfig(link.RoleHost, addr)
	cfg.TLS = s.TLS
	return &cfg.Config
}

// BindDoors binds every door addr names for s's run, none when one cannot be had.
func BindDoors(s config.Membership, addr string) ([]net.Listener, error) {
	doors, err := link.Doors(addr)
	if err != nil {
		return nil, err
	}
	var lns []net.Listener
	for _, d := range doors {
		ln, err := link.Listen(d.Addr, hostNetwork(s, d.Addr))
		if err != nil {
			CloseDoors(lns)
			return nil, err
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

// CloseDoors closes every door in lns.
func CloseDoors(lns []net.Listener) {
	for _, ln := range lns {
		_ = ln.Close()
	}
}

// Open wires the membership over the run's configuration and scenario before the
// run builds anything; the world, the context, the scheduler, the network service
// and the protocol halves follow as the run gets each.
func (m *Membership) Open(run Run, cfg *config.Membership, wire *Wiring, scenario *resource.Scenario, log *vlog.Log, glue *corrector.Corrector, life *lifecycle.Controller) {
	m.run, m.cfg, m.wiring, m.scenario, m.log, m.glue, m.life = run, cfg, wire, scenario, log, glue, life
	m.admissions = network.NewAdmissionLimiter()
	m.places = wire.Places
}

// Bind wires the run's world and its game context once both exist.
func (m *Membership) Bind(w *engine.World, ctx *engine.GameContext) { m.world, m.ctx = w, ctx }

// SetConverge wires the protocol's three halves, which the run builds over its world.
func (m *Membership) SetConverge(corrections *converge.Corrections, authority *converge.Authority, reach *converge.Reach) {
	m.corrections, m.authority, m.reach = corrections, authority, reach
}

// SetScheduler wires the run's scheduler, which it builds after the world.
func (m *Membership) SetScheduler(s *engine.Scheduler) { m.scheduler = s }

// SetNetworkService wires the service that owns the endpoint of a run started in a
// session (-host, -join) or one that may open one.
func (m *Membership) SetNetworkService(s *service.NetworkService) { m.networkSvc = s }

// GuestDoor is the join handshake a guest's own port serves beside its peer links,
// and the gate that completes one it admitted: the relay's, admitting as admitHosted
// says.
func (m *Membership) GuestDoor() (accept func(net.Conn) (link.PeerID, error), admitted func(link.PeerID)) {
	return m.relayAcceptor(m.admitHosted), m.releaseRelayedJoiner
}

// Control is the membership's half of the operator's session surface.
func (m *Membership) Control() Control { return Control{m} }

// Dismissed reports whether this run was dismissed from its session, Shed whether
// its host did so to keep its tick, and Dropped whether the host dropped it, which
// keeps its key out: the run goes on alone, as after :leave.
func (m *Membership) Dismissed() bool { return m.dismissed.Load() }
func (m *Membership) Shed() bool      { return m.shed.Load() }
func (m *Membership) Dropped() bool   { return m.wasDropped.Load() }

// Leaving reports whether a :leave or a fork waits for the run's loop (LeaveSession).
func (m *Membership) Leaving() bool { return m.leaving.Load() }

// LobbyClosing reports whether a dial lands between the startup lobby reading its
// roster and the mid-run gate arming, which refuses it.
func (m *Membership) LobbyClosing() bool { return m.lobbyClosing.Load() }

// HasPendingJoin reports whether this run was built on a join's handshake (TakeDial),
// which StartJoinSession finishes and ClosePendingJoin closes.
func (m *Membership) HasPendingJoin() bool { return m.pendingJoin != nil }

// ClosePendingJoin releases the join's stream if it never reached the run's port.
func (m *Membership) ClosePendingJoin() {
	if m.pendingJoin != nil {
		_ = m.pendingJoin.Close()
	}
}

// ForkCell is a run's own "no session left" flag: it lost its authority and nobody
// took the session over.
func ForkCell(w *engine.World) *atomic.Bool { return w.Resources.Status.Bools.Get("network.fork") }
