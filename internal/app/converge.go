package app

import (
	"errors"
	"slices"
	"syscall"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// instance is the App as the authority protocol sees it: the correction glue over
// its world, and beside it the run's membership, which the protocol moves. Every
// method takes the world lock itself, which is what keeps converge.Instance's
// contract — "never called while the caller holds it" — a property of this file
// rather than of every call site.
type instance struct {
	*corrector
	a *App
}

// instance is the run as converge reads it.
func (a *App) instance() instance { return instance{&a.glue, a} }

func (i instance) Position() event.Stamp { return i.a.Position() }
func (i instance) Driven() bool          { return i.a.cfg.Mode.Driven() }

func (i instance) LocalParticipant() uint32 { return i.a.localParticipant() }

// WorldRoster reads the roster off the cursors rather than off the lobby the offer
// described: arrivals and departures are barrier-bound crossings, so this is the
// same list on every instance.
func (i instance) WorldRoster() []network.RosterEntry {
	var out []network.RosterEntry
	i.a.world.RunSafe(func() { out = i.a.worldRosterLocked() })
	return out
}

// worldRosterLocked is WorldRoster for a caller that holds the world lock.
// Caller MUST hold updateMutex.
func (a *App) worldRosterLocked() []network.RosterEntry {
	var out []network.RosterEntry
	w := a.world
	for slot := range parameter.MaxPlayers {
		e := w.Resources.Player.Slot(uint8(slot))
		if e == 0 {
			continue
		}
		c, ok := w.Components.Cursor.GetComponent(e)
		if !ok || c.PeerID == 0 {
			continue
		}
		out = append(out, network.RosterEntry{
			ID: link.PeerID(c.PeerID), Slot: uint8(slot), Holder: link.PeerID(c.Holder),
			Away: w.Components.Wraith.HasEntity(e), Claim: c.Claim,
		})
	}
	slices.SortFunc(out, func(a, b network.RosterEntry) int { return int(a.ID) - int(b.ID) })
	return out
}

func (i instance) Transport() engine.NetworkPort {
	if i.a.dismissed.Load() {
		return nil
	}
	return i.a.sessionTransport()
}

// Downstream reads the barrier's view of who is behind this instance.
func (i instance) Downstream() (out []uint32) {
	i.a.world.RunSafe(func() {
		for _, sys := range i.a.world.Systems() {
			if d, ok := sys.(interface{ Downstream() []uint32 }); ok {
				out = d.Downstream()
				return
			}
		}
	})
	return out
}

// relayLinks is the network system as the succession asks it about paths: whether
// this run reaches its authority, and whose artifacts it carries there.
type relayLinks interface {
	Reaches() bool
	Carries(id uint32) bool
	Carry(id uint32)
}

// links runs fn on the network system under the world lock; a run without one
// has nothing to ask.
func (i instance) links(fn func(relayLinks)) {
	i.a.world.RunSafe(func() {
		for _, sys := range i.a.world.Systems() {
			if l, ok := sys.(relayLinks); ok {
				fn(l)
				return
			}
		}
	})
}

// Reaches reports a path to the authority; a run with no network system has
// nothing that could lose one.
func (i instance) Reaches() bool {
	reaches := true
	i.links(func(l relayLinks) { reaches = l.Reaches() })
	return reaches
}

func (i instance) Carries(id uint32) (carries bool) {
	i.links(func(l relayLinks) { carries = l.Carries(id) })
	return carries
}

func (i instance) Carry(id uint32) { i.links(func(l relayLinks) { l.Carry(id) }) }

// DrainOffTick translates whatever the endpoint holds without advancing a tick, so
// a manifest, a request or a repair is acted on the moment it lands, and a resume
// once the drain that carried it is done.
func (i instance) DrainOffTick() {
	i.a.world.RunSafe(func() {
		for _, sys := range i.a.world.Systems() {
			if d, ok := sys.(interface{ DrainOffTick() }); ok {
				d.DrainOffTick()
				break
			}
		}
		if i.a.hostResumed {
			i.a.hostResumed = false
			i.a.releaseHold("Resumed by the host")
		}
	})
}

// CaptureSharedLocked is the read without its seal, for a comparison under the lock.
func (c *corrector) CaptureSharedLocked() (snapshot.SharedCapture, error) {
	return c.captureSharedLocked()
}

// IndexSharedLocked indexes the live world under its lock against the authority
// and term a manifest names, copying nothing but the index's own.
func (c *corrector) IndexSharedLocked(authority uint32, term network.AuthorityTerm) (*snapshot.Manifest, snapshot.SharedCapture, error) {
	light, err := c.captureLightLocked()
	if err != nil {
		return nil, snapshot.SharedCapture{}, err
	}
	light.Header.Term = term
	index, err := c.encoder.BuildLive(c.world, light, authority)
	return index, light, err
}

// InstallCapture resolves a capture against the staging world, projects it to this
// instance's tick and commits it between two ticks, reporting how far this instance
// had drifted from the projection.
func (c *corrector) InstallCapture(cap snapshot.SharedCapture) (engine.WorldDifference, error) {
	staged, err := c.stageProved(cap)
	if err != nil {
		return engine.WorldDifference{}, err
	}
	if err := staged.Commit(); err != nil {
		return engine.WorldDifference{}, err
	}
	return staged.Difference(), nil
}

func (c *corrector) Encoder() *snapshot.Encoder { return &c.encoder }

// AdoptAuthority takes a header whose world this instance already holds: the
// barrier's fences move to it and the ledger is settled against this world, which
// the authority has just proved is its own.
func (c *corrector) AdoptAuthority(h snapshot.CaptureHeader) {
	c.world.RunSafe(func() { c.adoptSnapshotBarrierLocked(h) })
	c.confirmPredictions(h.Tick, c.world)
	c.world.RunSafe(func() { c.telemetry.InstallTick.Store(int64(h.Tick)) })
}

// DropParticipant closes one participant's link; a direct neighbour observes the
// loss and the authority turns it into one departure at one agreed tick.
func (i instance) DropParticipant(id uint32) bool {
	p, ok := i.a.sessionTransport().(engine.PeerDroppingPort)
	return ok && p.Disconnect(id)
}

func (i instance) CommitLate(id uint32) (n uint64) {
	i.a.world.RunSafe(func() {
		if r := i.a.world.Resources.Network; r != nil && int(id) < len(r.CommitLate) {
			n = r.CommitLate[id].Load()
		}
	})
	return n
}

func (i instance) Ingress(id uint32) (n uint64) {
	i.a.world.RunSafe(func() {
		if r := i.a.world.Resources.Network; r != nil && int(id) < len(r.Ingress) {
			n = r.Ingress[id].Load()
		}
	})
	return n
}

func (i instance) AuthorityChanged(rec network.HandoffRecord, mine bool, via link.PeerID) {
	i.a.applyAuthorityChange(rec, mine, via)
}

// CrossDepartures keeps the places of the participants a new host's group does not
// hold: each departs as a Wraith, as any departure does.
func (i instance) CrossDepartures(away []network.RosterEntry) {
	for _, p := range away {
		i.a.crossDeparture(p.ID, p.Slot)
	}
}

func (i instance) Redial(lost link.PeerID) bool { return i.a.redial(lost) }

func (i instance) Forked(roster []network.RosterEntry, local link.PeerID) {
	i.a.forked(roster, local)
}

func (i instance) Freeze() { i.a.freeze() }
func (i instance) Thaw()   { i.a.thaw() }

func (i instance) SetStatusMessage(msg string, kind parameter.StatusKind) {
	i.a.ctx.SetStatusMessage(msg, kind)
}

// === the run's side ===

// receiveCorrection queues one reassembled correction. It is the seam every
// transport binds to, so a correction reaches the same queue however this run came
// to be in a session. Caller holds the world lock: take the bytes and nothing else.
func (a *App) receiveCorrection(_ uint64, body []byte) {
	if a.corrections != nil {
		a.corrections.Receive(body)
	}
}

// receiveSelective queues one manifest, request or repair. Same rule as
// receiveCorrection: the caller holds the world lock, so this takes the bytes and
// decides nothing.
func (a *App) receiveSelective(kind uint8, from uint32, body []byte) {
	if a.corrections != nil {
		a.corrections.ReceiveSelective(kind, from, body)
	}
}

// tickClosed runs under the world lock after every tick; see Corrections.TickClosed.
func (a *App) tickClosed(tick uint64) {
	if a.corrections != nil {
		a.corrections.TickClosed(tick)
	}
}

// receiveAuthorityFrame queues one succession frame, under the same rule: the
// decode and the decision happen between two ticks, in the correction loop's drain.
func (a *App) receiveAuthorityFrame(kind uint8, from uint32, body []byte) {
	if a.corrections != nil {
		a.corrections.ReceiveAuthorityFrame(kind, from, body)
	}
}

// reportPeerLost hands a departure to the succession and says whether this run
// asked for it by dropping the seat. Caller holds the world lock.
func (a *App) reportPeerLost(id uint32) bool {
	if a.corrections != nil {
		a.corrections.QueuePeerLost(id)
	}
	return a.seatHalted(id)
}

// ApplyPendingCorrections installs whatever authority has arrived, between two
// ticks. A caller-driven run reaches it from Tick; an interactive one has a
// goroutine of its own, because nothing on its side calls Tick.
func (a *App) ApplyPendingCorrections() {
	if a.corrections == nil {
		return
	}
	a.corrections.Apply()
}

// adoptCorrectionBaseline records the capture a join installed, so the deltas that
// follow it have the keyframe they name. It is the same object from both ends: the
// host sent a keyframe and this is the instance that installed it.
func (a *App) adoptCorrectionBaseline(cap snapshot.SharedCapture, bytes int) {
	if a.corrections != nil {
		a.corrections.SetBaseline(cap, bytes)
	}
}

// === authority ===

// authorityStamp is the term and participant a capture read now is authoritative
// under. Zero on a solo run, which is what makes a capture saved from one carry no
// authority claim at all.
func (a *App) authorityStamp() (network.AuthorityTerm, uint32) {
	if a.authority == nil {
		return 0, 0
	}
	return a.authority.Term(), uint32(a.authority.Holder())
}

// authorityTerm is the generation this instance is part of.
func (a *App) authorityTerm() network.AuthorityTerm {
	if a.authority == nil {
		return 0
	}
	return a.authority.Term()
}

// authorityID is the participant currently authoring, which every admission
// artifact names. It falls back to the session's first identity so a run that has
// not opened a session yet still offers a valid one.
func (a *App) authorityID() link.PeerID {
	if a.authority == nil {
		return engine.CoordinatorParticipant
	}
	if id := a.authority.Holder(); id != 0 {
		return id
	}
	return engine.CoordinatorParticipant
}

// openAuthority records the term and membership this run enters a session under,
// and the relay it joined through, which is its path to the authority.
func (a *App) openAuthority(o network.SessionOffer, local link.PeerID) {
	if a.authority == nil {
		return
	}
	a.joinedThrough = o.Relay
	a.authority.Open(o, local)
	a.publishAuthorityResource()
}

// openAuthorityLocked is openAuthority for a caller that already holds the world
// lock, which the operator `:host` path does.
// Caller MUST hold updateMutex.
func (a *App) openAuthorityLocked(o network.SessionOffer, local link.PeerID) {
	if a.authority == nil {
		return
	}
	a.joinedThrough = o.Relay
	a.authority.Open(o, local)
	a.publishAuthorityResourceLocked()
}

// applyAuthorityChange adopts the membership a handoff carries exactly, which keeps
// roster, slots and anchor identical on every survivor, and switches this
// instance's role. Who is away is ordered by their Wraiths' waits, and a place a
// dial was taking back is the predecessor's to have finished. via is the link this
// run follows the new host through, zero for a link of its own.
func (a *App) applyAuthorityChange(rec network.HandoffRecord, mine bool, via link.PeerID) {
	var away []link.PeerID
	a.world.RunSafe(func() { away = a.awayOrderLocked(rec.Roster) })
	a.sessionMu.Lock()
	a.sessionRoster = slices.Clone(rec.Roster)
	a.awayOrder, a.yielding, a.returning = away, 0, nil
	a.sessionOffer.Host = rec.Authority
	a.sessionOffer.Term = rec.Term
	a.sessionOffer.Anchor = rec.Anchor
	a.sessionOffer.Roster = slices.Clone(rec.Roster)
	a.sessionMu.Unlock()

	a.publishAuthorityResource()
	if a.corrections == nil {
		return
	}
	// What the lost authority never returned committed goes to its successor,
	// this instance's own when it is the successor, by the path the record came.
	a.world.RunSafe(func() {
		for _, sys := range a.world.Systems() {
			if r, ok := sys.(interface {
				FollowThrough(id uint32)
				ResendUncommitted()
			}); ok {
				r.FollowThrough(uint32(via))
				r.ResendUncommitted()
			}
		}
	})
	if mine {
		a.corrections.BecomeAuthority(rec)
		// Live scripts and bots advance their clock, but need the same publication
		// pump as their original host; only harnesses publish corrections by hand.
		if a.cfg.scriptedSession {
			a.corrections.StartPump()
		}
		a.crossPredecessorDeparture(rec)
		return
	}
	a.corrections.FollowAuthority(rec)
}

// awayOrderLocked is who of roster is away, longest first, as their Wraiths wait.
// Caller MUST hold updateMutex.
func (a *App) awayOrderLocked(roster []network.RosterEntry) []link.PeerID {
	since := make(map[link.PeerID]time.Time)
	var out []link.PeerID
	for _, p := range roster {
		if !p.Away || p.Slot == parameter.NoPlayerSlot {
			continue
		}
		if w, ok := a.world.Components.Wraith.GetComponent(a.world.Resources.Player.Slot(p.Slot)); ok {
			since[p.ID] = w.Since
			out = append(out, p.ID)
		}
	}
	slices.SortStableFunc(out, func(x, y link.PeerID) int { return since[x].Compare(since[y]) })
	return out
}

// freeze stops this run while its session's host changes, through the request the
// session makes, settled at once as releaseHold settles its end, so a replay stands
// still in the pass the run did. A hold standing already stays the host's.
func (a *App) freeze() {
	a.world.RunSafe(func() {
		if !a.ctx.TimeCtl.Held() {
			a.ctx.PushLocalOrigin(event.EventGameHoldRequest, &event.GamePausePayload{Paused: true}, event.OriginSession)
			a.scheduler.SettleLocked()
		}
		a.ctx.SetStatusMessage("Host lost; waiting for the next host", parameter.StatusAlert)
	})
}

// thaw resumes the session this run took over.
func (a *App) thaw() {
	a.world.RunSafe(func() {
		if a.thawLocked() {
			a.ctx.SetStatusMessage("Paused; this run hosts the session now", parameter.StatusNotice)
		}
	})
}

// thawLocked ends a freeze, and reports whether a hold the lost host set still
// stands: it stays for this run's own :pause, unless a script or bot drives the run
// or it hosts no session, and nobody here can end it. Caller MUST hold updateMutex,
// between two ticks.
func (a *App) thawLocked() (held bool) {
	if a.ctx.TimeCtl.Holds(engine.HoldFreeze) || a.cfg.scriptedSession || !a.world.IsSessionCoordinator() {
		a.releaseHold("Resumed")
		return false
	}
	return a.ctx.TimeCtl.Held()
}

// forked goes on with a run its session gave up. The claim it was in that session
// under keeps its record as it stood, whatever links it still holds: what a fork
// plays is not what its player brings back. A play run's loop takes it into a
// session of its own (leaveSession), keeping every other player's place for
// whoever hosts next; a run with no loop drops them and plays on as the fork.
func (a *App) forked(roster []network.RosterEntry, local link.PeerID) {
	if a.cfg.Mode == ModePlay {
		// Leaving before the claim goes, as a redial holds its claim again before it
		// reads leaving: whichever of the two runs second lets the claim go.
		a.leaving.Store(true)
		a.leaveClaim()
		return
	}
	a.leaveClaim()
	a.dropAbandonedCursors(roster, local)
	a.world.RunSafe(func() { a.thawLocked() })
}

// errRedialEnded turns down an admission the redialling run no longer takes.
var errRedialEnded = errors.New("the run went on without this host")

// redial takes this run back to the host it lost, which it had a link of its own
// to: a link that dropped on this side alone leaves a place that host's Wraith gives
// back, under the claim the run joined with. The chain names where each host but the
// first is dialled; the first is at the door this run came in by. Only a person's run
// redials. The claim is let go for the dial to name, its record as the drop left it.
func (a *App) redial(lost link.PeerID) bool {
	var cfg Config
	door := false
	a.world.RunSafe(func() {
		cfg = a.cfg
		door = cfg.Mode == ModePlay && cfg.holder == 0 && cfg.JoinAddress != "" &&
			!a.leaving.Load() && a.restart.Load() == nil
	})
	if !door {
		return false
	}
	addr, ok := a.authority.Chain().Lookup(lost)
	if !ok {
		addr = cfg.JoinAddress
	}
	c := a.claim.Swap(nil)
	if c == nil {
		return false
	}
	a.claims.release(c)
	a.log.Info("session", "msg", "redialling the lost host", "authority", uint64(lost),
		"dial", link.Shown(addr))
	core.Go(func() { a.redialDoor(cfg, addr, lost, c) })
	return true
}

// redialDoor dials the lost host at addr while this run waits on it. Admitted back
// into its own place, the run is replaced by the one admitted; a refusal no retry
// helps, a door nothing answers at, or a place that is not this run's begins the
// walk. A run that stays in its session holds its claim again.
func (a *App) redialDoor(cfg Config, addr string, lost link.PeerID, c *claim) {
	dial := cfg
	dial.JoinAddress = addr
	for a.authority.Redialing(lost) {
		d, err := dialJoin(dial)
		if err == nil {
			// Seated at once: which place the host offers is what the redial asks.
			err = d.seat(cfg.holder != 0)
		}
		if err != nil {
			if network.RetryOf(err) == network.RetryNever || errors.Is(err, syscall.ECONNREFUSED) {
				a.log.Info("session", "msg", "lost host refused the redial", "error", err.Error())
				a.authority.RedialRefused(lost)
			} else {
				time.Sleep(parameter.NetworkRejoinPassInterval) // [wall] a link wait, not a game one
			}
			continue
		}
		back := &restartRequest{Notice: "Host connection restored", dialled: d}
		if d.claim == c && a.authority.RedialAdmitted(lost) && a.replaceWith(back) {
			a.log.Info("session", "msg", "lost host admitted this run back", "authority", uint64(lost))
			return
		}
		d.abandon(errRedialEnded)
		if d.claim != c {
			a.log.Info("session", "msg", "lost host holds no place for this run")
			a.authority.RedialRefused(lost)
		}
	}
	a.world.RunSafe(func() {
		if a.cfg.JoinAddress != cfg.JoinAddress || !a.claims.hold(c) {
			return
		}
		// Held before leaving is read, the other way round from forked.
		if !a.claim.CompareAndSwap(nil, c) ||
			(a.leaving.Load() || a.restart.Load() != nil) && a.claim.CompareAndSwap(c, nil) {
			a.claims.release(c)
		}
	})
}

// publishAuthorityResource hands the transport the two cells the barrier reads:
// which participant may produce a roster crossing, and under which generation.
func (a *App) publishAuthorityResource() {
	a.world.RunSafe(a.publishAuthorityResourceLocked)
}

// publishAuthorityResourceLocked is the same write for a caller that holds the
// world lock. Caller MUST hold updateMutex.
func (a *App) publishAuthorityResourceLocked() {
	term, holder := a.authorityStamp()
	if r := a.world.Resources.Network; r != nil {
		r.Authority.Store(holder)
		r.Term.Store(uint64(term))
		r.Relay = uint32(a.joinedThrough)
	}
	a.world.LatchSession()
}

// sessionChain is what an offer carries, read through the authority because a
// guest holds one too.
func (a *App) sessionChain() network.SuccessionChain {
	if a.authority == nil {
		return nil
	}
	return a.authority.Chain()
}
