package app

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app/internal/config"
	"github.com/lixenwraith/vif/internal/app/internal/corrector"
	"github.com/lixenwraith/vif/internal/app/internal/membership"
	"github.com/lixenwraith/vif/internal/app/internal/scribe"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// The authority policies a host names and the graph a bot seat plays when none is
// named, the membership's, for the callers that build a Config.
const (
	AuthorityHost    = membership.AuthorityHost
	AuthorityMigrate = membership.AuthorityMigrate
	DefaultBotGraph  = membership.DefaultBotGraph
)

// BotSpecs is shared by CLI flags and :bot add; empty or a zero count means no request.
func BotSpecs(spec string) ([]string, error) { return membership.BotSpecs(spec) }

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
func (a *App) HoldKey(k network.Key) { a.membership.HoldKey(k) }
func (a *App) JoinSessionAt(o network.SessionOffer, cap snapshot.SharedCapture, bytes int) error {
	return a.membership.JoinSessionAt(o, cap, bytes)
}
func (a *App) SessionSummary() string { return a.membership.SessionSummary() }
func (a *App) barrierLocked() bool    { return a.membership.BarrierLocked() }

// membershipRun is the run as its membership reads it: the membership.Run seam,
// answered by its configuration, presentation and scribe, and the runs it plays for
// its seats (PlaySeat).
type membershipRun struct{ a *App }

// The run's settings, read live: tests switch a running run's mode, and a join
// copies the whole Config and wiring where it dials (JoinBase).
func (r membershipRun) Mode() Mode                    { return r.a.cfg.Mode }
func (r membershipRun) Scripted() bool                { return r.a.cfg.wiring.scriptedSession }
func (r membershipRun) GeometryDefaulted() bool       { return r.a.cfg.wiring.geometryDefaulted }
func (r membershipRun) Resources() resource.Options   { return r.a.cfg.Resources }
func (r membershipRun) JoinBase() membership.JoinBase { return r.a.cfg }

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
func (r membershipRun) ReplaceWith(ask *membership.RestartAsk) bool {
	return r.a.replaceWith(&restartRequest{RestartAsk: *ask})
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
func (r membershipRun) PlaySeat(p membership.SeatPlay, seated func(run *membership.Membership, d *bot.Driver) (slot int)) (finished bool, err error) {
	var played *seatRun
	cfg := runConfig{Config: p.Config, wiring: wiring{log: p.Log, membership: p.Wiring}}
	finished, err = drive(cfg, "bot", p.Graph.Name, p.Stop, p.Hold, func(b *App) (pacedSource, error) {
		d, err := bot.NewDriver(b, b.ctx, p.Graph, b.Seed(), b.membership.LocalParticipant())
		if err != nil {
			return nil, err
		}
		played = &seatRun{run: b, slot: seated(&b.membership, d), stop: p.Stop, number: p.Number}
		r.a.addSeatRun(played)
		return &seatSource{botSource: botSource{d}, ends: seatEnds(p.Mode, r.a.world, b.world), screen: &r.a.seatScreen,
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
	return []*atomic.Bool{membership.ForkCell(seat), membership.ForkCell(holder)}
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

// newSessionApp resolves the startup handshake before a joining App draws a
// seed. Interactive play and authored headless scripts share this construction.
func newSessionApp(cfg runConfig) (*App, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Before anything is built, so a graph that does not resolve fails in front of
	// the operator. Each seat parses its own copy when it is added.
	for _, spec := range cfg.Membership.Bots {
		if _, err := membership.LoadBotGraph(cfg.Resources, spec); err != nil {
			return nil, err
		}
	}
	if cfg.Membership.JoinAddress != "" {
		return newJoiningApp(cfg)
	}
	if cfg.Membership.HostAddress != "" {
		return newHostingApp(cfg)
	}
	return newApp(cfg)
}

// newHostingApp installs a tick-zero acceptor before the service is initialized.
// The map latch is engaged for the whole run: the anchor a joiner adopts names
// these bounds, and a crop landing between that offer and the start gate would move
// them under a participant that has already built its world on them (D-14).
func newHostingApp(cfg runConfig) (*App, error) {
	cfg.LockMap = true
	a, err := newApp(cfg)
	if err != nil {
		return nil, err
	}
	// The boot settles before the accept loop opens, as BeginHosting's does: a
	// scenario that sets up its own map (td) is offered on it, not on -size's.
	a.Settle()
	return a, nil
}

// newJoiningApp builds the App on a dial of its session: the one its wiring carries
// from a :join or a redial, or one made now.
func newJoiningApp(cfg runConfig) (*App, error) {
	d := cfg.wiring.dialled
	if d == nil {
		var err error
		if d, err = membership.DialJoin(cfg.Membership, cfg.wiring.membership, cfg); err != nil {
			return nil, err
		}
	}
	return openDial(d, cfg)
}

// openDial constructs the joining App cfg describes on d, on its preview or its
// offer: the world is built, then the run takes the dial (TakeDial).
func openDial(d *membership.JoinDial, cfg runConfig) (*App, error) {
	offer := d.Offer()
	cfg.Resources, cfg.wiring.networkConfig = d.Resources(), d.TransportConfig()
	cfg.Notice = JoinNotice(cfg.Notice, d.Notice())
	cfg, err := configForJoin(cfg, offer)
	if err != nil {
		d.Abandon(err)
		return nil, err
	}

	// Bound before the reply that declares it (internal/converge/reach.go), only
	// where authorship can move and this run's player lets it move here: the session
	// shares what is declared with every player. A browser is never dialled. A guest
	// that joined through a relay could not reach the host, so it is a leaf unless
	// -listen names an address the session can reach it at.
	var listener net.Listener
	var declared string
	gate, _ := cfg.wiring.membership.Links.(*converge.PeerLinkGate)
	if gate == nil {
		gate = &converge.PeerLinkGate{}
		cfg.wiring.membership.Links = gate
	}
	// On this run's gate, whichever run dialled: a :join or a redial hands its dial
	// to the run built on it, whose reach is the one that holds the roster's keys.
	cfg.wiring.networkConfig.ConnectPeer = network.PeerDialer(membership.IdentityFromAnchor(offer.Anchor),
		gate, cfg.wiring.networkConfig.ConnectTimeout)
	pinned := cfg.Membership.ListenAddress != ""
	if !offer.FixedAuthority && (cfg.Play.Takeover || pinned) && config.SocketNetwork && (offer.Relay == 0 || pinned) {
		listener, declared = converge.BindAdvertised(cfg.Membership.ListenAddress, cfg.Membership.JoinAddress)
	}
	if listener != nil {
		// Installed before newApp, because newApp is what builds the port that serves
		// this listener. The gate holds the App rather than closing over it for the
		// same reason: it does not exist yet, nor does the identity it answers with.
		cfg.wiring.networkConfig.PreboundListener = listener
		cfg.wiring.networkConfig.AcceptPeer = network.PeerAcceptor(network.PeerGate{
			Local:    gate.Local,
			Identity: membership.IdentityFromAnchor(offer.Anchor),
			Admit:    gate.Admit,
			Keys:     gate,
		}, cfg.wiring.networkConfig.ConnectTimeout)
	}
	a, err := newApp(cfg)
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		d.Abandon(err)
		return nil, err
	}
	gate.Bind(a.reach)
	a.reach.AdoptListener(listener, declared)
	if err := a.membership.TakeDial(d); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// sessionControl is the operator's session surface: the membership's half (Control)
// and the journal's, which the scribe answers. The router holds the world lock
// calling it.
type sessionControl struct {
	membership.Control
	a *App
}

func (c sessionControl) StartJournal(slot int) error { return c.a.scribe.StartJournalLocked(slot) }
func (c sessionControl) StopJournal(slot int) error  { return c.a.scribe.StopJournalLocked(slot) }
func (c sessionControl) JournalError() error         { return c.a.scribe.JournalErrorLocked() }
func (c sessionControl) Journaling() bool            { return c.a.scribe.Journaling() }
func (c sessionControl) JournalSummary() string      { return c.a.scribe.JournalSummary() }

// instance is the App as the authority protocol sees it: the correction glue over
// its world, and beside it the run's membership, which the protocol moves. Every
// method takes the world lock itself, which is what keeps converge.Instance's
// contract — "never called while the caller holds it" — a property of these two
// types rather than of every call site.
type instance struct {
	*corrector.Corrector
	*membership.Membership
}

// instance is the run as converge reads it.
func (a *App) instance() instance { return instance{&a.glue, &a.membership} }
