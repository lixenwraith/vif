package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/app/internal/scribe"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
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
// wiring (wiring), and reads the rest of the run through memberRun alone.
type membership struct {
	run memberRun

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

	relay relayDesk
	// tunnelStop ends the door this run publishes through a router (publishDoor).
	tunnelMu   sync.Mutex
	tunnelStop context.CancelFunc

	// joinedThrough is the relay this run joined the session through, zero on a
	// dial to the authority; the barrier reaches the authority through it.
	joinedThrough link.PeerID
}

// memberRun is what the membership reads of the run beyond its world and its
// session half: its mode, script and geometry and the files it may load, where it
// stands and how it is paced, the lobby it shows on, the journal and identity it
// writes a session into, the Config and wiring a join builds on, the vacancy a
// dedicated host resumes from, the run that replaces this one, and the screen and
// runs it plays its seats in.
type memberRun interface {
	Mode() Mode
	Scripted() bool
	GeometryDefaulted() bool
	Resources() resource.Options
	JoinBase() joinBase
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
	ReplaceWith(ask *restartAsk) bool
	Replacing() bool
	SeatScreenLocked() (width, height int)
	PlaySeat(p seatPlay, seated func(run *membership, d *bot.Driver) (slot int)) (finished bool, err error)
}

// restartAsk is what the membership asks the run that replaces this one to be
// (memberRun.ReplaceWith), which the run's own request carries (restartRequest).
type restartAsk struct {
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
	// that dialled holds already admitted; dialled alone is this run's own door,
	// redialled once its host went. With none the next run is solo, which is what an
	// authority with an empty roster has become.
	Rejoin  bool
	Join    string
	dialled *joinDial

	// press says the next run's player started its join, so its seat waits for
	// their press.
	press bool
}

// joinBase is the run's Config and wiring as a join dialled from it builds on, copied
// where the dial starts so the dial reads nothing of the run's own: a session half is
// checked against it (ValidateWith) and a session's scenario prepared on it (Prepare).
type joinBase interface {
	ValidateWith(s Membership) error
	Prepare(s Membership, pending *network.PendingJoin, preview network.SessionOffer) (resource.Options, error)
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
func (m *membership) open(run memberRun, cfg *Membership, wire *membershipWiring, scenario *resource.Scenario, log *vlog.Log, glue *corrector.Corrector, life *lifecycle.Controller) {
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

// membershipRun is the run as its membership reads it: the memberRun seam, answered
// by its configuration, presentation and scribe, and the runs it plays for its seats
// (PlaySeat).
type membershipRun struct{ a *App }

// The run's settings, read live: tests switch a running run's mode, and a join
// copies the whole Config and wiring where it dials (JoinBase).
func (r membershipRun) Mode() Mode                  { return r.a.cfg.Mode }
func (r membershipRun) Scripted() bool              { return r.a.cfg.wiring.scriptedSession }
func (r membershipRun) GeometryDefaulted() bool     { return r.a.cfg.wiring.geometryDefaulted }
func (r membershipRun) Resources() resource.Options { return r.a.cfg.Resources }
func (r membershipRun) JoinBase() joinBase          { return r.a.cfg }

func (r membershipRun) Position() event.Stamp              { return r.a.Position() }
func (r membershipRun) Settle()                            { r.a.Settle() }
func (r membershipRun) Resize(width, height int)           { r.a.Resize(width, height) }
func (r membershipRun) ShowStartupStatus(message string)   { r.a.showStartupStatus(message) }
func (r membershipRun) ShowJoinStatus(message string)      { r.a.showJoinStatus(message) }
func (r membershipRun) LobbyEvents() <-chan terminal.Event { return r.a.lobbyEvents() }
func (r membershipRun) LobbyEventCancels(ev terminal.Event) bool {
	return r.a.lobbyEventCancels(ev)
}
func (r membershipRun) BuildAnchor() event.JournalAnchor { return r.a.buildAnchor() }
func (r membershipRun) SessionAnchorFields(an event.JournalAnchor) []corrector.AnchorField {
	return r.a.sessionAnchorFields(an)
}
func (r membershipRun) JournalLostLocked(j *event.Journal, err error) { r.a.journalLostLocked(j, err) }
func (r membershipRun) ResumeVacant()                                 { r.a.resumeVacant() }
func (r membershipRun) Replacing() bool                               { return r.a.replacing() }

// ReplaceWith latches the membership's ask as this run's request (replaceWith).
func (r membershipRun) ReplaceWith(ask *restartAsk) bool {
	return r.a.replaceWith(&restartRequest{restartAsk: *ask})
}

// SeatScreenLocked is the screen a new seat plays in: this run's, so a person's bots
// see what that person sees, or a bot's own for a run that names none, a server or
// headless run given no -size. Caller holds updateMutex.
func (r membershipRun) SeatScreenLocked() (int, int) {
	if r.a.cfg.wiring.geometryDefaulted || !engine.ViewportFits(r.a.ctx.Width, r.a.ctx.Height) {
		return BotWidth, BotHeight
	}
	return r.a.ctx.Width, r.a.ctx.Height
}

// PlaySeat drives a seat's run as drive does a bot's, until it ends: seated is told
// of the run's membership and its driver before the first step and says the slot it
// was seated on, and the seat follows this run's screen (seatScreen). While it plays
// the run is one of this run's seatRuns.
func (r membershipRun) PlaySeat(p seatPlay, seated func(run *membership, d *bot.Driver) (slot int)) (finished bool, err error) {
	var played *seatRun
	cfg := runConfig{Config: p.config, wiring: wiring{log: p.log, membership: p.wiring}}
	finished, err = drive(cfg, "bot", p.graph.Name, p.stop, p.hold, func(b *App) (pacedSource, error) {
		d, err := bot.NewDriver(b, b.ctx, p.graph, b.Seed(), b.membership.localParticipant())
		if err != nil {
			return nil, err
		}
		played = &seatRun{run: b, slot: seated(&b.membership, d), stop: p.stop, number: p.number}
		r.a.addSeatRun(played)
		return &seatSource{botSource: botSource{d}, ends: seatEnds(p.mode, r.a.world, b.world), screen: &r.a.seatScreen,
			run: b, shown: screen{cfg.Width, cfg.Height}}, nil
	})
	if played != nil {
		// Under seatRunsMu, so an ask lands before this or finds no run
		r.a.seatRunsMu.Lock()
		r.a.seatRuns = slices.DeleteFunc(r.a.seatRuns, func(s *seatRun) bool { return s == played })
		r.a.seatRunsMu.Unlock()
		r.a.seatJournalLeft(played)
	}
	return finished, err
}

// seatEnds is what ends a seat this run plays besides its halt: the seat's fork or
// this run's (holder). A play run's loop takes its fork into a session of its own and
// seats its bots again there (forked), so nothing but the run ends its seats.
func seatEnds(mode Mode, holder, seat *engine.World) []*atomic.Bool {
	if mode == ModePlay {
		return nil
	}
	return []*atomic.Bool{forkCell(seat), forkCell(holder)}
}

// seatRun is a run this App plays for one of its seats: the slot its session gave it,
// the channel its membership halts it with and the seat's number.
type seatRun struct {
	run    *App
	slot   int
	stop   <-chan os.Signal
	number uint64
}

// addSeatRun keeps s among this run's seatRuns, in the order their seats came, which
// a later seat may beat to its place.
func (a *App) addSeatRun(s *seatRun) {
	a.seatRunsMu.Lock()
	defer a.seatRunsMu.Unlock()
	i, _ := slices.BinarySearchFunc(a.seatRuns, s.number, func(o *seatRun, n uint64) int { return cmp.Compare(o.number, n) })
	a.seatRuns = slices.Insert(a.seatRuns, i, s)
}

// halted reports whether the seat was told to leave.
func (s *seatRun) halted() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// askSeatJournal asks the seat on slot to open or close its journal, which its own
// loop serves between two of its steps; the outcome reaches this run's status line.
// A seat is a run of its own, so it journals its own player domain, which no other
// instance can: another participant's never leaves its machine.
func (a *App) askSeatJournal(slot int, start bool) error {
	// Held to the ask, so a seat leaving takes it up or this finds no run
	a.seatRunsMu.Lock()
	defer a.seatRunsMu.Unlock()
	var b *scribe.Scribe
	leaving := false
	for _, s := range a.seatRuns {
		if s.slot == slot {
			// A leaving seat's loop serves no more asks, and a live one may share its slot
			if !s.halted() {
				b = &s.run.scribe
				break
			}
			leaving = true
		}
	}
	switch {
	case b == nil && leaving && start:
		return fmt.Errorf("the bot on slot %X is leaving", slot)
	case b == nil && leaving:
		return fmt.Errorf("the bot on slot %X is leaving; its journal closes with it", slot)
	case b == nil:
		return fmt.Errorf("no bot of this run's is on slot %X", slot)
	case start && b.Recorder() != nil && !b.AskPending():
		return fmt.Errorf("slot %X already journals to %s", slot, b.Recorder().Path())
	case !start && b.CancelJournalStart():
		return nil
	case !start && b.Recorder() == nil:
		return fmt.Errorf("slot %X keeps no journal", slot)
	}
	report := func(path string, err error) { a.reportSeatJournal(slot, start, path, err) }
	if !b.QueueJournalAsk(start, report) {
		return fmt.Errorf("slot %X is already opening or closing a journal", slot)
	}
	return nil
}

// seatJournalLeft tells this run's player what became of a seat's journal as its run
// closed, which only the seat's Close saw: the journal it shut, or a start its loop
// never served. Called on the seat's goroutine once its run has closed.
func (a *App) seatJournalLeft(s *seatRun) {
	sc := &s.run.scribe
	sc.AbandonAsks(errors.New("the bot left before it opened"))
	switch path, err := sc.ClosedAtExit(); {
	case err != nil:
		a.log.Warn("journal", "msg", "seat journal not closed", "slot", s.slot, "error", err.Error())
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X left; its journal %s", s.slot, err), parameter.StatusAlert)
	case path != "":
		a.log.Info("journal", "msg", "seat journal closed", "slot", s.slot, "path", path, "reason", "bot left")
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X left; its journal closed: %s", s.slot, path), parameter.StatusNotice)
	}
}

// reportSeatJournal tells this run's player how a seat's journal start or stop went.
// It runs on the seat's goroutine, or the holder's for a start a stop cancelled; the
// status line and the log take either.
func (a *App) reportSeatJournal(slot int, start bool, path string, err error) {
	switch {
	case err != nil:
		a.log.Warn("journal", "msg", "seat journal not changed", "slot", slot, "start", start, "error", err.Error())
		a.ctx.SetStatusMessage(fmt.Sprintf("Journal on slot %X: %s", slot, err), parameter.StatusAlert)
	case start:
		a.log.Info("journal", "msg", "seat journal opened", "slot", slot, "path", path)
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X journaling to %s", slot, path), parameter.StatusNotice)
	default:
		a.log.Info("journal", "msg", "seat journal closed", "slot", slot, "path", path)
		a.ctx.SetStatusMessage(fmt.Sprintf("Slot %X journal closed: %s", slot, path), parameter.StatusNotice)
	}
}

// seatJournalSummaries names the journal each seated bot keeps, by slot.
func (a *App) seatJournalSummaries() []string {
	var parts []string
	a.seatRunsMu.Lock()
	for _, s := range a.seatRuns {
		if p := s.run.scribe.Recorder().Path(); p != "" {
			parts = append(parts, fmt.Sprintf("slot %X to %s", s.slot, p))
		}
	}
	a.seatRunsMu.Unlock()
	return parts
}

// seatJournalNotices names each journal a seat of this run keeps, which closes with it.
func (a *App) seatJournalNotices() []string {
	a.seatRunsMu.Lock()
	defer a.seatRunsMu.Unlock()
	var out []string
	for _, s := range a.seatRuns {
		if r := s.run.scribe.Recorder(); r != nil {
			out = append(out, fmt.Sprintf("slot %X journal closed: %s", s.slot, r.Path()))
		}
	}
	return out
}

// replacing reports whether a run to replace this one is latched already.
func (a *App) replacing() bool { return a.restart.Load() != nil }

// forkCell is a run's own "no session left" flag: it lost its authority and nobody
// took the session over.
func forkCell(w *engine.World) *atomic.Bool { return w.Resources.Status.Bools.Get("network.fork") }
