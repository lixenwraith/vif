package converge

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// Authority is this instance's view of who authors and under which generation,
// plus the succession it runs when that instance goes.
type Authority struct {
	inst Instance
	tel  snapshot.Telemetry

	// corrections is what authorship produces, and reach the links it needs to
	// move: a successor seeds its cadence from what it installed, and a survivor
	// with no link to the participant taking over dials down the chain.
	corrections *Corrections
	reach       *Reach

	mu     sync.Mutex
	term   network.AuthorityTerm
	holder network.PeerID
	local  network.PeerID
	roster []network.RosterEntry
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

// newAuthority builds the authority half of a session. Like the correction half
// it starts nothing: a run becomes part of a session when a transport is attached
// and an offer or a handoff names its term.
func newAuthority(inst Instance, tel snapshot.Telemetry, reg *status.Registry) *Authority {
	return &Authority{
		inst:           inst,
		tel:            tel,
		accepted:       make(map[network.AuthorityTerm]network.HandoffRecord, 4),
		statTerm:       reg.Ints.Get("network.term"),
		statHolder:     reg.Ints.Get("network.authority"),
		statMigrations: reg.Ints.Get("network.migrations"),
		statRefused:    reg.Ints.Get("network.term_refused"),
		statFork:       reg.Bools.Get("network.fork"),
		statMigrating:  reg.Bools.Get("network.migrating"),
		statHostLost:   reg.Bools.Get("network.host_lost"),
	}
}

// Open records the term and membership this instance enters a session under. It
// is the same call from all three doors — a tick-zero lobby, a mid-run join, and
// a `:host` that opens a solo run — because the three differ in how the offer was
// obtained and not in what it says.
func (u *Authority) Open(o network.SessionOffer, local network.PeerID) {
	u.mu.Lock()
	u.term = max(o.Term, network.FirstTerm)
	u.holder = o.Host
	u.local = local
	u.roster = slices.Clone(o.Roster)
	u.anchor = o.Anchor
	u.delay = o.BarrierDelayTicks
	u.chain = slices.Clone(o.Chain)
	u.fixed = o.FixedAuthority
	u.fork = false
	u.mu.Unlock()
	u.publish()
}

// Term, Holder and Local are the three identities the rest of the session reads.
func (u *Authority) Term() network.AuthorityTerm {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.term
}

// Holder is the participant currently authoring.
func (u *Authority) Holder() network.PeerID {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.holder
}

// isAuthority reports whether this instance is the one authoring.
func (u *Authority) isAuthority() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.term > 0 && u.local != 0 && u.local == u.holder
}

// Migrating reports whether a succession is in progress, which is what refuses a
// join rather than admitting it into a term that is about to end.
func (u *Authority) Migrating() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.contested != 0
}

// admit is the wire gate: whether an artifact produced under term may be acted on.
// Older is ignored, equal acted on, newer refused rather than adopted — only a
// handoff record may raise this instance's term, so an artifact under a term nobody
// handed it is a fork or a skipped succession. Both are reported, neither followed.
func (u *Authority) admit(term network.AuthorityTerm, from uint32) bool {
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
		u.tel.StaleTerm.Add(1)
		return false
	case term == held:
		return true
	}
	u.refuse(from, term, "names a term this instance was never handed")
	if fork {
		u.inst.SetStatusMessage(
			"This instance is a local fork; the session has elected a new authority and cannot be rejoined",
			4*parameter.StatusMessageDefaultTimeout, true)
	}
	return false
}

// refuse records and reports one artifact turned away by the term gate.
func (u *Authority) refuse(from uint32, term network.AuthorityTerm, why string) {
	u.statRefused.Add(1)
	vlog.Warn("app", "msg", "authoritative artifact refused",
		"peer", from, "term", uint64(term), "held", uint64(u.Term()), "reason", why)
}

// === succession ===

// peerLost is the transport's report that a direct neighbour has gone. It starts
// a succession only for the participant that was authoring; every other departure
// changes this instance's reach, which the next report it sends will carry.
func (u *Authority) peerLost(id uint32) {
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
func (u *Authority) beginSuccession(lost network.PeerID) {
	tick := u.inst.Position().Tick
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
		"peer", uint64(lost), "term", uint64(term), "local", uint64(local))
	u.sendReport()
	u.drive()
}

// sendReport floods the news that the authority is gone. It decides nothing — the
// successor is a function of the roster — but only a direct neighbour sees the link
// drop, and the crossing that would carry the news is produced by the participant
// that is gone, so the notice travels instead.
func (u *Authority) sendReport() {
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

// currentRoster is the closed roster as the world holds it rather than as the offer
// that admitted this instance described it. A mid-run joiner's offer names the lobby
// at the moment it dialled, so two participants would hold two lists; arrivals and
// departures are barrier-bound crossings, so the cursor roster is the same list
// everywhere. The offer stays as the fallback before the cursors exist.
func (u *Authority) currentRoster() []network.RosterEntry {
	out := u.inst.WorldRoster()
	if len(out) == 0 {
		u.mu.Lock()
		out = slices.Clone(u.roster)
		u.mu.Unlock()
	}
	return out
}

// recordReport notes one survivor's loss notice, reporting whether it was new.
// The set is what terminates the flood: a notice says one thing and says it once.
func (u *Authority) recordReport(rep network.AuthorityReport) bool {
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
func (u *Authority) drive() {
	u.mu.Lock()
	contested, since := u.contested, u.since
	badge, reconcile := u.badgeUntil, u.reconcileAt
	u.mu.Unlock()

	tick := u.inst.Position().Tick
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
		u.inst.DropAbandonedCursors(roster, local)
	}
	// The reachability work runs on the same loop and for the same reason: it is
	// between two ticks, on every instance, whichever half of the protocol it is.
	u.reach.drive(contested != 0)
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
// DesignatedSuccessor is a pure function of the closed roster and the participant
// that went, so exactly one instance reaches the publish below and reaches it as
// soon as it notices — a quorum round would add a trip a star cannot complete. The
// one self-check is retention: without a baseline the successor stands down.
func (u *Authority) trySucceed() {
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
	evidenceTick, retained := u.corrections.retentionEvidence()
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
	u.reconcileAt = u.inst.Position().Tick + parameter.NetworkSuccessionTicks
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
func (u *Authority) giveUp() {
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
	u.inst.DropAbandonedCursors(roster, local)
	why := "no successor was reachable"
	if fixed {
		why = "the session pinned its authority"
	}
	vlog.Warn("app", "msg", "continuing locally", "held_term", uint64(u.Term()),
		"contested_term", uint64(term), "lost", uint64(lost), "reason", why)
	u.inst.SetStatusMessage(
		"Host connection lost; continuing locally from the last authoritative state",
		4*parameter.StatusMessageDefaultTimeout, true)
}

// adopt installs a handoff record: the term, the authority, and the membership
// that moves with them.
//
// from is the link the record arrived on, or zero when this instance produced it.
func (u *Authority) adopt(rec network.HandoffRecord, from uint32) error {
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
	u.badgeUntil = u.inst.Position().Tick + parameter.NetworkMigrationBadgeTicks
	mine := u.local == rec.Authority
	u.mu.Unlock()

	u.statMigrations.Add(1)
	u.statMigrating.Store(true)
	u.statHostLost.Store(false)
	u.publish()
	u.inst.AuthorityChanged(rec, mine)

	vlog.Warn("app", "msg", "authority handed off",
		"term", uint64(rec.Term), "authority", uint64(rec.Authority),
		"predecessor", uint64(rec.Predecessor), "roster", len(rec.Roster),
		"evidence_tick", rec.EvidenceTick, "local", mine)
	u.inst.SetStatusMessage(
		fmt.Sprintf("Authority moved to participant %d (term %d)", rec.Authority, rec.Term),
		2*parameter.StatusMessageDefaultTimeout, false)

	if body, err := network.EncodeHandoff(rec); err == nil {
		u.flood(network.MsgAuthorityHandoff, from, body)
	}
	return nil
}

// === reachability ===

// Chain is the candidate list, which is also the address book peers dial from.
func (u *Authority) Chain() network.SuccessionChain {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.chain)
}

// successor is the participant this instance would follow if the authority went
// now. It is the same pure function succession runs, exposed so a survivor can
// dial the right address before it needs one.
func (u *Authority) successor() (network.PeerID, bool) {
	roster := u.currentRoster()
	u.mu.Lock()
	holder, chain := u.holder, slices.Clone(u.chain)
	u.mu.Unlock()
	return network.DesignatedSuccessor(roster, holder, chain)
}

// successionOrder is the succession list: every survivor of the authority's loss,
// in the order the rule would elect them. It is what a survivor with no link
// retries down, so a reconnect walks the same order the election does rather than
// an order of its own.
func (u *Authority) successionOrder() []network.PeerID {
	roster := u.currentRoster()
	u.mu.Lock()
	holder, local, chain := u.holder, u.local, slices.Clone(u.chain)
	u.mu.Unlock()
	alive := func(id network.PeerID) bool {
		return id != 0 && id != holder && id != local &&
			slices.ContainsFunc(roster, func(p network.RosterEntry) bool { return p.ID == id })
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
func (u *Authority) appendChain(id network.PeerID, addr string) bool {
	u.mu.Lock()
	next := u.chain.Append(id, addr)
	if slices.Equal(u.chain, next) {
		u.mu.Unlock()
		return false
	}
	u.chain = next
	u.mu.Unlock()
	u.PublishChain()
	return true
}

// PublishChain floods the chain whole. The coordinator also runs it as the session
// opens: each offer handed out during the lobby named the chain as it stood at that
// moment, so a participant admitted early holds a prefix until it is told the rest.
func (u *Authority) PublishChain() {
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

// ForgetReachable drops a departed participant. Local only: the departure crossing
// reaches every instance, so each drops the same entry without a broadcast — and
// this runs under the world lock, which flooding would deadlock on.
func (u *Authority) ForgetReachable(id network.PeerID) {
	u.mu.Lock()
	u.chain = u.chain.Without(id)
	u.mu.Unlock()
}

// onPeerList adopts one chain broadcast, refused below the term this instance
// holds so a stale one cannot resurrect a departed peer.
func (u *Authority) onPeerList(from uint32, body []byte) {
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

// Receive takes one succession frame. It runs between two ticks, from the
// correction loop's drain, so it may decode and decide.
func (u *Authority) Receive(kind uint8, from uint32, body []byte) {
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
func (u *Authority) onReport(from uint32, body []byte) {
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
func (u *Authority) answerElection(to uint32) {
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
	if port := u.inst.Transport(); port != nil {
		port.Send(to, uint8(network.MsgAuthorityHandoff), body)
	}
}

// onHandoff adopts, or refuses, one record.
func (u *Authority) onHandoff(from uint32, body []byte) {
	rec, err := network.DecodeHandoff(body)
	if err != nil {
		return
	}
	if err := u.adopt(rec, from); err != nil {
		u.statRefused.Add(1)
		vlog.Warn("app", "msg", "handoff refused",
			"peer", from, "term", uint64(rec.Term), "authority", uint64(rec.Authority),
			"error", err.Error())
		u.inst.SetStatusMessage("Refused a conflicting authority handoff: "+err.Error(),
			4*parameter.StatusMessageDefaultTimeout, true)
	}
}

// flood forwards one succession frame to every direct neighbour but the link it
// arrived on. Deduplication is by term and participant rather than by a hop count:
// a report says one thing once, and a handoff is adopted once.
func (u *Authority) flood(kind network.MessageType, exclude uint32, body []byte) {
	port := u.inst.Transport()
	if port == nil || !port.IsRunning() || port.PeerCount() == 0 {
		return
	}
	port.BroadcastExcept(exclude, uint8(kind), body)
}

// publish writes the operator surface. Six cells in one card: which generation is
// authoring and who, how many handoffs this session has run, how many artifacts
// the term gate turned away, whether this instance is a fork, and whether a
// handoff is in progress.
func (u *Authority) publish() {
	u.mu.Lock()
	term, holder, fork := u.term, u.holder, u.fork
	u.mu.Unlock()
	u.statTerm.Store(int64(term))
	u.statHolder.Store(int64(holder))
	u.statFork.Store(fork)
}

// Summary is `:session`'s authority line.
func (u *Authority) Summary() string {
	u.mu.Lock()
	term, holder, local, fork, contested := u.term, u.holder, u.local, u.fork, u.contested
	migrations := u.statMigrations.Load()
	u.mu.Unlock()
	if term == 0 {
		return ""
	}
	// The holder is named only when it is somebody else: the session line has
	// already printed this participant's own identity, and a handoff count of zero
	// is what every session that has never migrated reports.
	line := fmt.Sprintf("term %d, authoring", term)
	if local != holder {
		line = fmt.Sprintf("term %d, following participant %d", term, holder)
	}
	if migrations > 0 {
		line += fmt.Sprintf(", %d handoff(s)", migrations)
	}
	if contested != 0 {
		line += fmt.Sprintf("; electing term %d", contested)
	}
	if fork {
		line += "; LOCAL FORK — this instance is no longer part of the session"
	}
	return line
}
