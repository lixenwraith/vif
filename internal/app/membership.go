package app

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

// membership is the run's place in its session: hosting and joining, the roster and
// the offer, the seats it holds, the claims it comes back with, the relay desk and
// the authority it follows or holds. It is the half of converge.Instance the
// protocol moves. It holds the session half of the run's Config (cfg) and of its
// wiring (wiring), and reads the rest of the run through membershipRun alone.
type membership struct {
	run membershipRun

	world      *engine.World
	ctx        *engine.GameContext
	cfg        *Membership
	wiring     *membershipWiring
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

	dismissed atomic.Bool // this run was dismissed from its session; the next one is what replaces it
	shed      atomic.Bool // the dismissal was its host keeping its tick
	dialling  atomic.Bool // a :join is being dialled while this run plays on
	leaving   atomic.Bool // a :leave or a fork is waiting for Loop, or being served by it

	pendingJoin *network.PendingJoin
	// landingHeld and landingStart are the hold the world a join installs lands in,
	// set around that install for the journal to record with it (landingHold).
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

	// returning is each place a dial took back with its claim until its arrival
	// crosses, the entry as it waited, for a join that fails to give back, and the
	// record its player brought; no wait runs out on its Wraith meanwhile.
	returning map[link.PeerID]returnPlace

	// claims is what this process comes back to its sessions with, and claim the one
	// this run is in its session under: nil before it is in one, and once it has left.
	claims *claimKeeper
	claim  atomic.Pointer[claim]

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
	// with the App rather than with the session because a run can open one later
	// with :host, and a budget that started when hosting did would be a budget
	// reset by whatever closed the last session.
	admissions *network.AdmissionLimiter

	hostResumed bool // a held guest's drain admitted the host's resume; world lock

	// seats are the bots this run holds. seatsMu is taken under the world lock and
	// never takes it.
	seatsMu    sync.Mutex
	seats      []*seat
	dropped    []uint32 // seats told to leave, until their links close
	seatSerial uint64

	// seatScreen is this run's terminal as its seats follow it, published on each
	// resize by a run whose terminal is its geometry; nil until the first.
	seatScreen atomic.Pointer[screen]
	relay      relayDesk
	// tunnelStop ends the door this run publishes through a router (publishDoor).
	tunnelMu   sync.Mutex
	tunnelStop context.CancelFunc

	// joinedThrough is the relay this run joined the session through, zero on a
	// dial to the authority; the barrier reaches the authority through it.
	joinedThrough link.PeerID
}

// membershipRun is what the membership reads of the run beyond its world and its
// session half: its mode, script and geometry and the files it may load, where it
// stands and how it is paced, the lobby it shows on, the journal and identity it
// writes a session into, the Config and wiring a join builds on, the vacancy a
// dedicated host resumes from, and the run that replaces this one.
type membershipRun interface {
	mode() Mode
	scripted() bool
	geometryDefaulted() bool
	resources() resource.Options
	joinBase() joinBase
	Position() event.Stamp
	Settle()
	Resize(width, height int)
	showStartupStatus(message string)
	showJoinStatus(message string)
	lobbyEvents() <-chan lobbyEvent
	lobbyEventCancels(ev lobbyEvent) bool
	buildAnchor() event.JournalAnchor
	sessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField
	journalLostLocked(j *event.Journal, err error)
	resumeVacant()
	replaceWith(req *restartRequest) bool
	replacing() bool
}

// joinBase is the run's Config and wiring as a join dialled from it builds on, copied
// where the dial starts so the dial reads nothing of the run's own: a session half is
// checked against it (validateWith) and a session's scenario prepared on it (prepare).
type joinBase interface {
	validateWith(s Membership) error
	prepare(s Membership, pending *network.PendingJoin, preview network.SessionOffer) (resource.Options, error)
}

// membershipWiring is the session half of a run's wiring, which the membership holds
// beside Membership: the seat a bot's run is, the claims the process comes back with,
// the ceiling of a session it joined, the doors Run bound for it, and the press its
// join waits for.
type membershipWiring struct {
	// holder is the participant whose bot this run is, zero for any other run.
	holder link.PeerID
	relay  func() (net.Conn, error) // a bot's stream to its holder; see seatLink

	// claims is the process's claim keeper, which every run Run builds and every seat
	// they hold shares; Normalize gives a run built another way one of its own.
	claims *claimKeeper

	// sessionPlayers is the -players of the session this run joined, which it holds
	// to should it take that session over; Participants is a host's own.
	sessionPlayers int

	// hostListeners are the first run's -host doors, bound before its terminal
	// started; the run serves them once its clock runs.
	hostListeners []net.Listener

	// joinPress says this run's join is one its player started, at launch or from a
	// :join, whose seat waits for their press (membership.joinWaits). A run dialled on
	// its own, or whose player already pressed, takes its seat once its world is built.
	joinPress bool
}

// joining is s pointed at the session a :join names, as -join would have built it.
// -players and -authority go: they set a session this run no longer hosts, and the
// one it joins has its own, which its offer carries.
func joining(s Membership, target string) (Membership, error) {
	e, err := link.ParseEndpoint(target)
	if err != nil {
		return s, err
	}
	s.HostAddress, s.Participants, s.FixedAuthority = "", 0, false
	s.JoinAddress, s.SessionName = e.Addr, e.Name
	return s, nil
}

// hostNetwork is the transport the doors s's run opens at addr are served with.
func hostNetwork(s Membership, addr string) *link.Config {
	cfg := network.DebugConfig(link.RoleHost, addr)
	cfg.TLS = s.TLS
	return &cfg.Config
}

// bindDoors binds every door addr names for s's run, none when one cannot be had.
func bindDoors(s Membership, addr string) ([]net.Listener, error) {
	doors, err := link.Doors(addr)
	if err != nil {
		return nil, err
	}
	var lns []net.Listener
	for _, d := range doors {
		ln, err := link.Listen(d.Addr, hostNetwork(s, d.Addr))
		if err != nil {
			closeDoors(lns)
			return nil, err
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

func closeDoors(lns []net.Listener) {
	for _, ln := range lns {
		_ = ln.Close()
	}
}

// open wires the membership over the run's configuration and scenario before the
// run builds anything; the world, the context, the scheduler, the network service
// and the protocol halves follow as the run gets each.
func (m *membership) open(run membershipRun, cfg *Membership, wire *membershipWiring, scenario *resource.Scenario, log *vlog.Log, glue *corrector.Corrector, life *lifecycle.Controller) {
	m.run, m.cfg, m.wiring, m.scenario, m.log, m.glue, m.life = run, cfg, wire, scenario, log, glue, life
	m.admissions = network.NewAdmissionLimiter()
	m.claims = wire.claims
}

// The session API stays the run's for whatever reads it through the run, and the
// barrier is its own.
func (a *App) ApplyPendingCorrections()                { a.membership.ApplyPendingCorrections() }
func (a *App) AttachTransport(port engine.NetworkPort) { a.membership.AttachTransport(port) }
func (a *App) BeginHosting(addr string) error          { return a.membership.BeginHosting(addr) }
func (a *App) HostAddr() string                        { return a.membership.HostAddr() }
func (a *App) HostSession(o network.SessionOffer) error {
	return a.membership.HostSession(o)
}
func (a *App) Join(j event.JoinAnchor) error   { return a.membership.Join(j) }
func (a *App) JoinAnchor() event.JoinAnchor    { return a.membership.JoinAnchor() }
func (a *App) JoinAt(j event.JoinAnchor) error { return a.membership.JoinAt(j) }
func (a *App) JoinSession(o network.SessionOffer) error {
	return a.membership.JoinSession(o)
}
func (a *App) JoinSessionAt(o network.SessionOffer, cap snapshot.SharedCapture, bytes int) error {
	return a.membership.JoinSessionAt(o, cap, bytes)
}
func (a *App) SessionSummary() string { return a.membership.SessionSummary() }
func (a *App) barrierLocked() bool    { return a.membership.barrierLocked() }

// The run's settings the membership reads, live: tests switch a running run's mode,
// and a join copies the whole Config and wiring where it dials (joinBase).
func (a *App) mode() Mode                  { return a.cfg.Mode }
func (a *App) scripted() bool              { return a.cfg.wiring.scriptedSession }
func (a *App) geometryDefaulted() bool     { return a.cfg.wiring.geometryDefaulted }
func (a *App) resources() resource.Options { return a.cfg.Resources }
func (a *App) joinBase() joinBase          { return a.cfg }

// replacing reports whether a run to replace this one is latched already.
func (a *App) replacing() bool { return a.restart.Load() != nil }

// forkCell is a run's own "no session left" flag: it lost its authority and nobody
// took the session over.
func forkCell(w *engine.World) *atomic.Bool { return w.Resources.Status.Bools.Get("network.fork") }
