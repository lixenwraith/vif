// Who authors, and what happens when that instance goes.
//
// Losing the authority without a successor ends the session's shared identity: the
// survivors keep ticking separately, with no roster authority and no way to admit
// anyone. That is the fallback; the succession here is the other outcome.
//
// Notice, handoff. A survivor floods the news that the authority is gone, so a
// participant two links away learns of a loss only its neighbour observed; the
// participant the roster names as successor — its lowest surviving identity, which
// is the first guest admitted — publishes the record it authors under, at once and
// without asking anyone. network.DesignatedSuccessor is why there is no vote here:
// it is a function of a roster every survivor already holds, so at most one
// instance can conclude that it is the successor. A quorum could not serve the
// shape a session actually has — see that file's header.
//
// The record carries roster, slot assignments, anchor and barrier delay, so adopting
// it is one decision rather than a term change followed by a roster negotiation, and
// a joiner dialling mid-handoff is refused with a distinguishable error rather than
// half-admitted into a term about to end.
//
// What a successor may author is unchanged: the Shared domain and nothing else. It
// does not begin authoring the D-13 owner-authored cells of cursors it does not
// simulate, its correction index keeps the same two exclusions, and no Player-domain
// state crosses as part of the transfer.

package app

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// authority is this instance's view of who authors and under which generation,
// plus the succession it runs when that instance goes.
type authority struct {
	a *App

	mu     sync.Mutex
	term   network.AuthorityTerm
	holder network.PeerID
	local  network.PeerID
	roster []network.SessionParticipant
	anchor event.JoinAnchor
	delay  uint64

	// chain is the succession candidate list: every participant that declared a
	// port, in join order, adopted whole from an offer, a handoff or MsgPeerList.
	// See internal/network/reach.go.
	chain network.SuccessionChain

	// record is the handoff this instance is authoring under, kept so a survivor
	// that links up after the record was flooded can be told who took the term.
	record network.HandoffRecord

	// accepted is the handoff record adopted for each term, so a second record for
	// a term already adopted is recognised as the split-brain attempt it is rather
	// than applied over the first.
	accepted map[network.AuthorityTerm]network.HandoffRecord

	// fork marks a local continuation: this instance lost the authority, no
	// succession was possible, and what it is running is its own game from the last
	// authoritative state. It is what makes a later encounter with a higher term a
	// refusal to report rather than a merge to attempt.
	fork bool

	// fixed is the session's answer to losing its authority, adopted from the offer
	// rather than from this instance's own configuration: with it set the term
	// never moves, and every survivor forks the moment the authority goes.
	fixed bool

	// The succession in progress, if any. contested is the term being taken over,
	// which is always the held term plus one: a successor that skipped a term would
	// be adopting authorship over state nobody agreed it had. reports is the set of
	// survivors whose loss notice has been seen, which is what terminates the flood.
	contested network.AuthorityTerm
	lost      network.PeerID
	since     uint64
	reports   map[network.PeerID]bool
	published bool

	// reconcileAt is the tick at which a successor that took the term reconciles
	// its roster with what it can actually reach. Zero when nothing is pending.
	reconcileAt uint64

	badgeUntil uint64

	statTerm       *atomic.Int64
	statHolder     *atomic.Int64
	statMigrations *atomic.Int64
	statRefused    *atomic.Int64
	statFork       *atomic.Bool
	statMigrating  *atomic.Bool
	statHostLost   *atomic.Bool
}

// ErrSessionHandoff refuses a join that arrived while the session was electing a
// new authority. It is distinguishable on purpose: a joiner may retry against the
// authority that emerges, and half-admitting it into a term that is about to end
// is the one outcome that would leave a participant in a session nobody owns.
var ErrSessionHandoff = errors.New(
	"session authority is changing (" + network.HandoffRefusalTag + "); retry")

// newAuthority builds the authority half of a session. Like the correction half
// it starts nothing: a run becomes part of a session when a transport is attached
// and an offer or a handoff names its term.
func newAuthority(a *App) *authority {
	reg := a.world.Resources.Status
	u := &authority{
		a:              a,
		accepted:       make(map[network.AuthorityTerm]network.HandoffRecord, 4),
		statTerm:       reg.Ints.Get("network.term"),
		statHolder:     reg.Ints.Get("network.authority"),
		statMigrations: reg.Ints.Get("network.migrations"),
		statRefused:    reg.Ints.Get("network.term_refused"),
		statFork:       reg.Bools.Get("network.fork"),
		statMigrating:  reg.Bools.Get("network.migrating"),
		statHostLost:   reg.Bools.Get("network.host_lost"),
	}
	return u
}

// open records the term and membership this instance enters a session under. It
// is the same call from all three doors — a tick-zero lobby, a mid-run join, and
// a `:host` that opens a solo run — because the three differ in how the offer was
// obtained and not in what it says.
func (u *authority) open(o network.SessionOffer, local network.PeerID) {
	u.mu.Lock()
	u.term = max(o.Term, network.FirstTerm)
	u.holder = o.Host
	u.local = local
	u.roster = slices.Clone(o.Participants)
	u.anchor = o.Anchor
	u.delay = o.BarrierDelayTicks
	u.chain = slices.Clone(o.Chain)
	u.fixed = o.FixedAuthority
	u.fork = false
	u.mu.Unlock()
	u.publish()
}

// Term, Holder and Local are the three identities the rest of the session reads.
func (u *authority) Term() network.AuthorityTerm {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.term
}

// Holder is the participant currently authoring.
func (u *authority) Holder() network.PeerID {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.holder
}

// IsAuthority reports whether this instance is the one authoring.
func (u *authority) IsAuthority() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.term > 0 && u.local != 0 && u.local == u.holder
}

// Migrating reports whether a succession is in progress, which is what refuses a
// join rather than admitting it into a term that is about to end.
func (u *authority) Migrating() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.contested != 0
}

// Fork reports whether this instance is a local continuation rather than part of
// a session.
func (u *authority) Fork() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.fork
}

// admit is the wire gate: whether an artifact produced under term may be acted on.
//
// The three answers are the three rules. Older is ignored, because the session has
// moved past it. Equal is acted on. Newer is *refused* — not adopted — because the
// only thing that may raise this instance's term is a handoff record, and an
// artifact arriving under a term nobody handed it is either a fork that has been
// running separately or an instance that has skipped a succession. Both are
// reported; neither is followed.
func (u *authority) admit(term network.AuthorityTerm, from uint32) bool {
	u.mu.Lock()
	held, fork := u.term, u.fork
	u.mu.Unlock()
	switch {
	case held == 0:
		return true // this run is not in a session: nothing to be authoritative over
	case term == 0:
		u.refuse(from, term, "carries no authority term")
		return false
	case term < held:
		u.a.snapshotTelemetry.staleTerm.Add(1)
		return false
	case term == held:
		return true
	}
	u.refuse(from, term, "names a term this instance was never handed")
	if fork {
		u.a.ctx.SetStatusMessage(
			"This instance is a local fork; the session has elected a new authority and cannot be rejoined",
			4*parameter.StatusMessageDefaultTimeout, true)
	}
	return false
}

// refuse records and reports one artifact turned away by the term gate.
func (u *authority) refuse(from uint32, term network.AuthorityTerm, why string) {
	u.statRefused.Add(1)
	vlog.Warn("app", "msg", "authoritative artifact refused",
		"peer", from, "term", uint64(term), "held", uint64(u.Term()), "reason", why)
}

// === succession ===

// peerLost is the transport's report that a direct neighbour has gone. It starts
// a succession only for the participant that was authoring; every other departure
// changes this instance's reach, which the next report it sends will carry.
func (u *authority) peerLost(id uint32) {
	u.mu.Lock()
	start := u.term > 0 && network.PeerID(id) == u.holder && u.local != u.holder && u.contested == 0
	u.mu.Unlock()
	if !start {
		return
	}
	u.beginSuccession(network.PeerID(id))
}

// beginSuccession opens the election for the next term and floods this survivor's
// input to it.
func (u *authority) beginSuccession(lost network.PeerID) {
	tick := u.a.Position().Tick
	u.mu.Lock()
	if u.contested != 0 || u.term == 0 {
		u.mu.Unlock()
		return
	}
	u.contested = u.term + 1
	u.lost = lost
	u.since = tick
	u.reports = make(map[network.PeerID]bool, len(u.roster))
	u.published = false
	term, local := u.contested, u.local
	u.mu.Unlock()

	u.statMigrating.Store(true)
	vlog.Warn("app", "msg", "authority lost; succession opened",
		"lost", uint64(lost), "term", uint64(term), "participant", uint64(local))
	u.sendReport()
	u.drive()
}

// sendReport floods the news that the authority is gone.
//
// It decides nothing — the successor is a function of the roster — but only a
// direct neighbour of the authority sees the link drop, and the departure crossing
// that used to carry that news is produced by the participant that is gone. So the
// notice travels instead, and a survivor two links away opens the same succession
// from it.
func (u *authority) sendReport() {
	u.mu.Lock()
	term, local, lost := u.contested, u.local, u.lost
	u.mu.Unlock()
	if term == 0 || local == 0 {
		return
	}
	rep := network.AuthorityReport{Term: term, From: local, Lost: lost}
	u.recordReport(rep)
	body, err := network.EncodeAuthorityReport(rep)
	if err != nil {
		return
	}
	u.flood(network.MsgAuthorityReport, 0, body)
}

// currentRoster is the closed roster as the *world* holds it rather than as the
// offer that admitted this instance described it.
//
// The difference matters for a session that grew after this participant arrived. A
// mid-run joiner's offer names the lobby at the moment it dialled, so two
// participants admitted a minute apart hold two different lists — and a succession
// computed over them would use two different majorities. The cursor roster does
// not have that problem: an arrival and a departure are barrier-bound crossings
// that every instance applies at one agreed tick (D-11), so what the world holds is
// the same list everywhere. The stored offer stays as the fallback for a run whose
// world has not built its cursors yet.
func (u *authority) currentRoster() []network.SessionParticipant {
	var out []network.SessionParticipant
	u.a.world.RunSafe(func() {
		w := u.a.world
		for slot := range parameter.MaxPlayers {
			e := w.Resources.Player.Slot(uint8(slot))
			if e == 0 {
				continue
			}
			c, ok := w.Components.Cursor.GetComponent(e)
			if !ok || c.PeerID == 0 {
				continue
			}
			out = append(out, network.SessionParticipant{
				ID: network.PeerID(c.PeerID), Slot: uint8(slot),
			})
		}
	})
	slices.SortFunc(out, func(a, b network.SessionParticipant) int { return int(a.ID) - int(b.ID) })
	if len(out) == 0 {
		u.mu.Lock()
		out = slices.Clone(u.roster)
		u.mu.Unlock()
	}
	return out
}

// recordReport notes one survivor's loss notice, reporting whether it was new.
// The set is what terminates the flood: a notice says one thing and says it once.
func (u *authority) recordReport(rep network.AuthorityReport) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if rep.Term != u.contested || rep.From == 0 || u.reports[rep.From] {
		return false
	}
	u.reports[rep.From] = true
	return true
}

// drive advances the succession. It is called from the correction loop, which runs
// between two ticks on every instance whichever half of the protocol it is, and
// from each succession frame that arrives — so the election proceeds on evidence
// rather than on a schedule.
func (u *authority) drive() {
	u.mu.Lock()
	contested, since := u.contested, u.since
	badge, reconcile := u.badgeUntil, u.reconcileAt
	u.mu.Unlock()

	tick := u.a.Position().Tick
	if badge != 0 && tick >= badge {
		u.mu.Lock()
		u.badgeUntil = 0
		u.mu.Unlock()
		u.statMigrating.Store(false)
	}
	if reconcile != 0 && tick >= reconcile {
		roster := u.currentRoster()
		u.mu.Lock()
		u.reconcileAt = 0
		local := u.local
		u.mu.Unlock()
		u.a.dropAbandonedCursors(roster, local)
	}
	// The reachability work runs on the same loop and for the same reason: it is
	// between two ticks, on every instance, whichever half of the protocol it is.
	u.a.reach.drive(contested != 0)
	if contested == 0 {
		return
	}
	u.mu.Lock()
	fixed := u.fixed
	u.mu.Unlock()
	// A session that pinned its authorship has nothing to elect and nothing to wait
	// for. The succession still *opens*, because opening it is what floods the loss
	// to survivors a relay away, and it ends in the same place a fruitless one does
	// — one window earlier, because no record is coming.
	if !fixed {
		u.trySucceed()
	}

	u.mu.Lock()
	stillOpen := u.contested == contested
	u.mu.Unlock()
	if stillOpen && (fixed || tick > since+parameter.NetworkSuccessionTicks) {
		u.giveUp()
	}
}

// trySucceed takes the term, when this instance is the one the roster names.
//
// There is nothing to collect and nobody to ask. DesignatedSuccessor is a pure
// function of the closed roster and the participant that went, both of which every
// survivor already holds, so exactly one instance reaches the publish below and it
// reaches it as soon as it notices the loss. That immediacy is the point: the
// session is stalled from the moment the authority goes until somebody authors, and
// a quorum round would add a round trip that a star cannot complete at all.
//
// The one self-check is retention. A successor with no retained authoritative
// record has no baseline for a delta to name, so it would answer the first manifest
// with a whole world for every survivor at once; without one it stands down and the
// succession window turns this into a local fork.
func (u *authority) trySucceed() {
	roster := u.currentRoster()
	u.mu.Lock()
	if u.contested == 0 || u.published || u.local == 0 {
		u.mu.Unlock()
		return
	}
	term, lost, local := u.contested, u.lost, u.local
	u.mu.Unlock()

	u.mu.Lock()
	chain := slices.Clone(u.chain)
	u.mu.Unlock()
	if want, ok := network.DesignatedSuccessor(roster, lost, chain); !ok || want != local {
		return // not this instance's term to take; the record or the window decides
	}
	evidenceTick, retained := u.a.corrections.retentionEvidence()
	if retained == 0 {
		return
	}

	u.mu.Lock()
	if u.contested != term || u.published {
		u.mu.Unlock()
		return
	}
	u.published = true
	rec := network.HandoffRecord{
		Term:              term,
		Authority:         local,
		Predecessor:       lost,
		Roster:            roster,
		Anchor:            u.anchor,
		BarrierDelayTicks: u.delay,
		// The predecessor's identity returns to the pool, so its entry describes
		// nobody now.
		Chain:        u.chain.Without(lost),
		EvidenceTick: evidenceTick,
	}
	u.mu.Unlock()

	if err := u.adopt(rec, 0); err != nil {
		vlog.Error("app", "msg", "succession could not adopt its own record", "error", err.Error())
		return
	}
	// The roster the successor took over names participants it may have no path to
	// — in a star, every guest but itself. Reconciled after the succession window
	// rather than now, because a survivor that is merely a relay hop away is still
	// arriving and dropping it here would destroy a cursor it still simulates.
	u.mu.Lock()
	u.reconcileAt = u.a.Position().Tick + parameter.NetworkSuccessionTicks
	u.mu.Unlock()

	if body, err := network.EncodeHandoff(rec); err == nil {
		u.flood(network.MsgAuthorityHandoff, 0, body)
	}
}

// giveUp ends a succession no record ever answered: either this instance is not the
// successor the roster names and cannot reach the one that is, or it is and had
// nothing retained to author from. What is left is the local-continuation fallback,
// said plainly: this instance continues its own game from the last authoritative
// state.
func (u *authority) giveUp() {
	// Read before the state is cleared: what this fork keeps is the roster it can
	// still reach, and both halves of that answer are gone once lost is.
	roster := u.currentRoster()
	u.mu.Lock()
	if u.contested == 0 {
		u.mu.Unlock()
		return
	}
	term, lost, local, fixed := u.contested, u.lost, u.local, u.fixed
	u.contested, u.reports, u.published = 0, nil, false
	u.fork = true
	u.mu.Unlock()

	u.statMigrating.Store(false)
	u.statHostLost.Store(true)
	u.publish()
	u.a.dropAbandonedCursors(roster, local)
	why := "no successor was reachable"
	if fixed {
		why = "the session pinned its authority"
	}
	vlog.Warn("app", "msg", "continuing locally", "held_term", uint64(u.Term()),
		"contested_term", uint64(term), "lost", uint64(lost), "reason", why)
	u.a.ctx.SetStatusMessage(
		"Host connection lost; continuing locally from the last authoritative state",
		4*parameter.StatusMessageDefaultTimeout, true)
}

// adopt installs a handoff record: the term, the authority, and the membership
// that moves with them.
//
// from is the link the record arrived on, or zero when this instance produced it.
func (u *authority) adopt(rec network.HandoffRecord, from uint32) error {
	roster := u.currentRoster()
	u.mu.Lock()
	if err := rec.Validate(roster, u.chain); err != nil {
		u.mu.Unlock()
		return err
	}
	// The conflict check comes first, and the order is the invariant rather than a
	// preference: a rival record for a term this instance has already adopted is
	// the split-brain case, and reaching the staleness test before it would
	// silently drop the very thing that has to be reported.
	if prior, ok := u.accepted[rec.Term]; ok {
		u.mu.Unlock()
		if prior.Authority != rec.Authority {
			return fmt.Errorf("term %d was already handed to participant %d; participant %d also claims it",
				rec.Term, prior.Authority, rec.Authority)
		}
		return nil // the same record arriving by a second path
	}
	if rec.Term <= u.term {
		u.mu.Unlock()
		return nil // the session has already moved past this record
	}
	if rec.Term != u.term+1 {
		held := u.term
		u.mu.Unlock()
		return fmt.Errorf("handoff enters term %d from term %d; a term is never skipped",
			rec.Term, held)
	}
	u.term, u.holder = rec.Term, rec.Authority
	u.roster = slices.Clone(rec.Roster)
	u.anchor, u.delay = rec.Anchor, rec.BarrierDelayTicks
	u.chain = slices.Clone(rec.Chain)
	u.record = rec
	u.accepted[rec.Term] = rec
	u.contested, u.reports, u.published = 0, nil, false
	u.fork = false
	u.badgeUntil = u.a.Position().Tick + parameter.NetworkMigrationBadgeTicks
	mine := u.local == rec.Authority
	u.mu.Unlock()

	u.statMigrations.Add(1)
	u.statMigrating.Store(true)
	u.statHostLost.Store(false)
	u.publish()
	u.a.applyAuthorityChange(rec, mine)

	vlog.Warn("app", "msg", "authority handed off",
		"term", uint64(rec.Term), "authority", uint64(rec.Authority),
		"predecessor", uint64(rec.Predecessor), "roster", len(rec.Roster),
		"evidence_tick", rec.EvidenceTick, "local", mine)
	u.a.ctx.SetStatusMessage(
		fmt.Sprintf("Authority moved to participant %d (term %d)", rec.Authority, rec.Term),
		2*parameter.StatusMessageDefaultTimeout, false)

	if body, err := network.EncodeHandoff(rec); err == nil {
		u.flood(network.MsgAuthorityHandoff, from, body)
	}
	return nil
}

// === reachability ===

// Chain is the candidate list, which is also the address book peers dial from.
func (u *authority) Chain() network.SuccessionChain {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.chain)
}

// Successor is the participant this instance would follow if the authority went
// now. It is the same pure function succession runs, exposed so a survivor can
// dial the right address before it needs one.
func (u *authority) Successor() (network.PeerID, bool) {
	roster := u.currentRoster()
	u.mu.Lock()
	holder, chain := u.holder, slices.Clone(u.chain)
	u.mu.Unlock()
	return network.DesignatedSuccessor(roster, holder, chain)
}

// SuccessionOrder is the succession list: every survivor of the authority's loss,
// in the order the rule would elect them. It is what a survivor with no link
// retries down, so a reconnect walks the same order the election does rather than
// an order of its own.
func (u *authority) SuccessionOrder() []network.PeerID {
	roster := u.currentRoster()
	u.mu.Lock()
	holder, local, chain := u.holder, u.local, slices.Clone(u.chain)
	u.mu.Unlock()
	alive := func(id network.PeerID) bool {
		return id != 0 && id != holder && id != local &&
			slices.ContainsFunc(roster, func(p network.SessionParticipant) bool { return p.ID == id })
	}
	var out []network.PeerID
	for _, e := range chain {
		if alive(e.ID) {
			out = append(out, e.ID)
		}
	}
	var rest []network.PeerID
	for _, p := range roster {
		if alive(p.ID) && !slices.Contains(out, p.ID) {
			rest = append(rest, p.ID)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// appendChain adds one participant that declared a port and publishes the whole
// chain, which only the coordinator does. Reports whether anything changed.
func (u *authority) appendChain(id network.PeerID, addr string) bool {
	u.mu.Lock()
	next := u.chain.Append(id, addr)
	if slices.Equal(u.chain, next) {
		u.mu.Unlock()
		return false
	}
	u.chain = next
	u.mu.Unlock()
	u.publishChain()
	return true
}

// publishChain floods the chain whole. The coordinator also runs it as the session
// opens: each offer handed out during the lobby named the chain as it stood at that
// moment, so a participant admitted early holds a prefix until it is told the rest.
func (u *authority) publishChain() {
	u.mu.Lock()
	chain, holder := slices.Clone(u.chain), u.holder
	term := max(u.term, network.FirstTerm)
	u.mu.Unlock()
	if len(chain) == 0 {
		return
	}
	body, err := network.EncodePeerList(network.PeerListRecord{
		Term: term, Authority: holder, Chain: chain,
	})
	if err != nil {
		return
	}
	u.flood(network.MsgPeerList, 0, body)
	vlog.Info("app", "msg", "succession chain published",
		"term", uint64(term), "candidates", len(chain))
}

// forgetReachable drops a departed participant. Local only: the departure crossing
// reaches every instance, so each drops the same entry without a broadcast — and
// this runs under the world lock, which flooding would deadlock on.
func (u *authority) forgetReachable(id network.PeerID) {
	u.mu.Lock()
	u.chain = u.chain.Without(id)
	u.mu.Unlock()
}

// onPeerList adopts one chain broadcast, refused below the term this instance
// holds so a stale one cannot resurrect a departed peer.
func (u *authority) onPeerList(from uint32, body []byte) {
	rec, err := network.DecodePeerList(body)
	if err != nil {
		return
	}
	u.mu.Lock()
	held, holder := u.term, u.holder
	u.mu.Unlock()
	if rec.Term < held || (held != 0 && rec.Authority != holder) {
		u.statRefused.Add(1)
		return
	}
	u.mu.Lock()
	changed := !slices.Equal(u.chain, rec.Chain)
	if changed {
		u.chain = rec.Chain
	}
	u.mu.Unlock()
	if changed {
		u.flood(network.MsgPeerList, from, body)
	}
}

// === inbound ===

// receive takes one succession frame. It runs between two ticks, from the
// correction loop's drain, so it may decode and decide.
func (u *authority) receive(kind uint8, from uint32, body []byte) {
	switch network.MessageType(kind) {
	case network.MsgAuthorityReport:
		u.onReport(from, body)
	case network.MsgAuthorityHandoff:
		u.onHandoff(from, body)
	case network.MsgPeerList:
		u.onPeerList(from, body)
	}
}

// onReport notes a survivor's loss notice and joins the succession it announces. A
// participant that never saw the disconnect itself — one two links from the lost
// authority — learns of it here, which is why the notices are flooded: the
// departure crossing that used to carry that news is produced by the authority.
func (u *authority) onReport(from uint32, body []byte) {
	rep, err := network.DecodeAuthorityReport(body)
	if err != nil || rep.From == 0 {
		return
	}
	u.mu.Lock()
	held, contested, holder, local := u.term, u.contested, u.holder, u.local
	u.mu.Unlock()
	if rep.Term <= held {
		// A succession this instance has already resolved. The record that named
		// the authority was flooded before this link existed, so the survivor still
		// electing is told on it rather than left to time the election out.
		u.answerElection(from)
		return
	}
	if contested == 0 {
		if rep.Lost != holder || local == holder {
			return
		}
		u.beginSuccession(rep.Lost)
	}
	if !u.recordReport(rep) {
		return
	}
	u.flood(network.MsgAuthorityReport, from, body)
	u.drive()
}

// answerElection re-sends this instance's record to one survivor, when this
// instance is the authority that record named.
func (u *authority) answerElection(to uint32) {
	u.mu.Lock()
	rec, holder, local := u.record, u.holder, u.local
	u.mu.Unlock()
	if rec.Term == 0 || holder != local || to == 0 {
		return
	}
	body, err := network.EncodeHandoff(rec)
	if err != nil {
		return
	}
	if port := u.a.sessionTransport(); port != nil {
		port.Send(to, uint8(network.MsgAuthorityHandoff), body)
	}
}

// onHandoff adopts, or refuses, one record.
func (u *authority) onHandoff(from uint32, body []byte) {
	rec, err := network.DecodeHandoff(body)
	if err != nil {
		return
	}
	if err := u.adopt(rec, from); err != nil {
		u.statRefused.Add(1)
		vlog.Warn("app", "msg", "handoff refused",
			"peer", from, "term", uint64(rec.Term), "authority", uint64(rec.Authority),
			"error", err.Error())
		u.a.ctx.SetStatusMessage("Refused a conflicting authority handoff: "+err.Error(),
			4*parameter.StatusMessageDefaultTimeout, true)
	}
}

// flood forwards one succession frame to every direct neighbour but the link it
// arrived on. Deduplication is by term and participant rather than by a hop count:
// a report says one thing once, and a handoff is adopted once.
func (u *authority) flood(kind network.MessageType, exclude uint32, body []byte) {
	port := u.a.sessionTransport()
	if port == nil || !port.IsRunning() || port.PeerCount() == 0 {
		return
	}
	port.BroadcastExcept(exclude, uint8(kind), body)
}

// publish writes the operator surface. Six cells in one card: which generation is
// authoring and who, how many handoffs this session has run, how many artifacts
// the term gate turned away, whether this instance is a fork, and whether a
// handoff is in progress.
func (u *authority) publish() {
	u.mu.Lock()
	term, holder, fork := u.term, u.holder, u.fork
	u.mu.Unlock()
	u.statTerm.Store(int64(term))
	u.statHolder.Store(int64(holder))
	u.statFork.Store(fork)
}

// summary is `:session`'s authority line.
func (u *authority) summary() string {
	u.mu.Lock()
	term, holder, local, fork, contested := u.term, u.holder, u.local, u.fork, u.contested
	migrations := u.statMigrations.Load()
	u.mu.Unlock()
	if term == 0 {
		return ""
	}
	role := "following"
	if local == holder {
		role = "authoring"
	}
	line := fmt.Sprintf("term %d, authority participant %d (%s), %d handoff(s)",
		term, holder, role, migrations)
	if contested != 0 {
		line += fmt.Sprintf("; electing term %d", contested)
	}
	if fork {
		line += "; LOCAL FORK — this instance is no longer part of the session"
	}
	return line
}

// ensureAuthorityCells registers the succession surface before the registry freezes.
// It is called from construction rather than lazily for the same reason every
// other counter is: a cell created after the freeze is counted late and never
// displayed.
func ensureAuthorityCells(reg *status.Registry) {
	reg.Ints.Get("network.term")
	reg.Ints.Get("network.authority")
	reg.Ints.Get("network.migrations")
	reg.Ints.Get("network.term_refused")
	reg.Bools.Get("network.fork")
	reg.Bools.Get("network.migrating")
}

// === App surface ===

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

// authoring reports whether this instance is the one publishing the world.
func (a *App) authoring() bool {
	return a.authority != nil && a.authority.IsAuthority()
}

// authorityID is the participant currently authoring, which every admission
// artifact names. It falls back to the session's first identity so a run that has
// not opened a session yet still offers a valid one.
func (a *App) authorityID() network.PeerID {
	if a.authority == nil {
		return hostParticipantID
	}
	if id := a.authority.Holder(); id != 0 {
		return id
	}
	return hostParticipantID
}

// admitArtifactTerm is the wire gate as the correction path calls it.
func (a *App) admitArtifactTerm(term network.AuthorityTerm, from uint32) bool {
	if a.authority == nil {
		return true
	}
	return a.authority.admit(term, from)
}

// openAuthority records the term and membership this run enters a session under.
func (a *App) openAuthority(o network.SessionOffer, local network.PeerID) {
	if a.authority == nil {
		return
	}
	a.authority.open(o, local)
	a.publishAuthorityResource()
}

// openAuthorityLocked is openAuthority for a caller that already holds the world
// lock, which the operator `:host` path does.
// Caller MUST hold updateMutex.
func (a *App) openAuthorityLocked(o network.SessionOffer, local network.PeerID) {
	if a.authority == nil {
		return
	}
	a.authority.open(o, local)
	a.publishAuthorityResourceLocked()
}

// applyAuthorityChange moves the membership a handoff carries into the places the
// session actually reads it from, and switches this instance's role.
//
// Nothing here re-derives anything: the roster, the slot assignments, the anchor
// and the barrier delay are adopted exactly as the record carries them, which is
// what makes them byte-identical on every survivor. What changes is which
// participant the admission surface names and which half of the correction
// protocol this run is.
func (a *App) applyAuthorityChange(rec network.HandoffRecord, mine bool) {
	a.sessionMu.Lock()
	a.sessionRoster = slices.Clone(rec.Roster)
	a.sessionOffer.Host = rec.Authority
	a.sessionOffer.Term = rec.Term
	a.sessionOffer.Anchor = rec.Anchor
	a.sessionOffer.BarrierDelayTicks = rec.BarrierDelayTicks
	a.sessionOffer.Participants = slices.Clone(rec.Roster)
	a.sessionMu.Unlock()

	a.publishAuthorityResource()
	if a.corrections == nil {
		return
	}
	if mine {
		a.corrections.becomeAuthority(rec)
		a.crossPredecessorDeparture(rec)
		return
	}
	a.corrections.followAuthority(rec)
}

// crossPredecessorDeparture removes the authority that was lost from the roster.
//
// A departure is a shared entity's destruction, so it may be produced by exactly
// one instance at exactly one tick (D-11) — and the instance the protocol names is
// the authority. That is precisely what was missing when the authority itself was
// what went: the neighbour that saw the link drop floods a notice, and the
// participant that would have turned it into a crossing is the one that is gone.
// The successor is the first instance that may, so it does, as its first act under
// the new term.
func (a *App) crossPredecessorDeparture(rec network.HandoffRecord) {
	if rec.Predecessor == 0 {
		return
	}
	i := slices.IndexFunc(rec.Roster, func(p network.SessionParticipant) bool {
		return p.ID == rec.Predecessor
	})
	if i < 0 {
		return
	}
	a.crossDeparture(rec.Predecessor, rec.Roster[i].Slot)
}

// dropAbandonedCursors removes the participants an instance left alone will never
// hear from again.
//
// Both outcomes of a lost authority reach it. A successor of a star takes the term
// and finds itself the only participant it can reach; a survivor that could not
// reach that successor continues as an explicit local fork. Either way each cursor
// this instance does not simulate belongs to a participant nothing will ever move
// again — the authority that went, and behind it the guests only ever reachable
// through it. Left there they are players that cannot be played and cannot leave.
//
// Having no link is also what makes the removal local rather than a crossing, and
// that is exact rather than convenient: a departure is produced once at one agreed
// tick (D-11) because two instances must destroy the same shared entity together or
// their allocators diverge from there on, and here there is no second instance. An
// instance that still holds links is left alone for the mirror of that reason,
// which is the partition case doc/multi-player-enhancement.md §8 records as
// unfinished.
func (a *App) dropAbandonedCursors(roster []network.SessionParticipant, local network.PeerID) {
	if p := a.sessionTransport(); p != nil && p.IsRunning() && p.PeerCount() > 0 {
		return
	}
	for _, p := range roster {
		if p.ID == local || p.Slot == parameter.NoPlayerSlot {
			continue
		}
		a.pushDeparture(p.ID, p.Slot, core.DomainShared)
	}
}

// crossDeparture produces one participant's departure as the D-11 crossing it is.
func (a *App) crossDeparture(id network.PeerID, slot uint8) {
	a.pushDeparture(id, slot, core.DomainPlayer)
}

// pushDeparture emits one participant's removal and returns its identity to the
// pool this instance allocates from.
//
// The domain is the whole of the difference between the two producers above. Player
// puts the artifact on the wire, where every instance applies it at one agreed tick;
// shared keeps it here, which is this instance re-deriving its own roster because
// there is nobody left to agree with. Both are recorded, so a replay of either run
// reaches the same world the same way.
func (a *App) pushDeparture(id network.PeerID, slot uint8, domain core.Domain) {
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventParticipantDeparted,
			&event.ParticipantDepartedPayload{Participant: uint32(id), Slot: slot},
			event.OriginSession, domain)
	})
	a.releaseParticipant(id)
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
	}
}

// receiveAuthorityFrame queues one succession frame. Caller holds the world lock,
// so it takes the bytes and decides nothing — the decode and the decision happen
// between two ticks, in the correction loop's drain.
func (a *App) receiveAuthorityFrame(kind uint8, from uint32, body []byte) {
	if a.corrections == nil {
		return
	}
	a.corrections.receiveAuthorityFrame(kind, from, body)
}

// reportPeerLost hands a departure to the succession. Caller holds the world lock.
func (a *App) reportPeerLost(id uint32) {
	if a.corrections == nil {
		return
	}
	a.corrections.queuePeerLost(id)
}

// AuthorityReport is what `:session` and the tests read about who is authoring.
type AuthorityReport struct {
	Term       network.AuthorityTerm
	Authority  network.PeerID
	Local      network.PeerID
	Migrations int64
	Migrating  bool
	Fork       bool
	Retained   int
	RetainedAt uint64
}

// AuthorityState describes this instance's place in the session's authority.
func (a *App) AuthorityState() AuthorityReport {
	if a.authority == nil {
		return AuthorityReport{}
	}
	u := a.authority
	u.mu.Lock()
	out := AuthorityReport{
		Term: u.term, Authority: u.holder, Local: u.local,
		Migrations: u.statMigrations.Load(),
		Migrating:  u.contested != 0, Fork: u.fork,
	}
	u.mu.Unlock()
	if a.corrections != nil {
		out.RetainedAt, out.Retained = a.corrections.retentionEvidence()
	}
	return out
}
