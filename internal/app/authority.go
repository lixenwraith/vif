// Package app: who authors, and what happens when that instance goes.
//
// Phase 6 left exactly one instance able to author the Shared world and to answer
// a selective request: the coordinator. Losing it ended the session's shared
// identity — the survivors kept ticking, separately, with no roster authority and
// no way to admit anyone. That behaviour is honest and it is still the fallback.
// What this file adds is the other outcome: a coordinated handoff that moves
// authorship without ever letting two instances claim it at once.
//
// The unit is the authority term (see network/authority.go). Everything here is
// one of three things:
//
//   - **The gate.** Every authoritative artifact carries the term it was produced
//     under. An artifact from an older term is ignored, one from the current term
//     is acted on, and one from a term this instance has never been handed is
//     refused and reported — that is the split-brain case, not a fast successor.
//
//   - **The succession.** Report, vote, handoff. A survivor floods what it can
//     reach and how current its retention is; once its view covers a strict
//     majority it votes, once, for the lowest eligible candidate; a candidate
//     holding a strict majority of votes publishes the record it authors under.
//     One vote per participant per term is what makes two authorities in one term
//     impossible rather than unlikely, and it is why none of this needs a timer to
//     break a tie.
//
//   - **The transfer.** A handoff carries the membership with it — roster, slot
//     assignments, anchor and barrier delay — so adopting it is one decision
//     rather than a term change followed by a roster negotiation. A joiner that
//     dials while it is running is refused with a distinguishable error rather
//     than half-admitted into a term that is about to end.
//
// What a successor may author is unchanged: the Shared domain and nothing else.
// It does not begin authoring the D-13 owner-authored cells of cursors it does not
// simulate, its correction index keeps the same two exclusions, and no
// Player-domain state crosses as part of the transfer. SimulatesLocally and
// ResolveOwnedCursor remain the only admission checks.
package app

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
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

	// accepted is the handoff record adopted for each term, so a second record for
	// a term already adopted is recognised as the split-brain attempt it is rather
	// than applied over the first.
	accepted map[network.AuthorityTerm]network.HandoffRecord

	// fork marks a local continuation: this instance lost the authority, no
	// succession was possible, and what it is running is its own game from the last
	// authoritative state. It is what makes a later encounter with a higher term a
	// refusal to report rather than a merge to attempt.
	fork bool

	// The succession in progress, if any. contested is the term being elected for,
	// which is always the held term plus one: a successor that skipped a term would
	// be adopting authorship over state nobody agreed it had.
	contested network.AuthorityTerm
	lost      network.PeerID
	since     uint64
	reports   map[network.PeerID]network.AuthorityReport
	vote      network.PeerID
	grants    map[network.PeerID]network.PeerID
	published bool

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
	u.reports = make(map[network.PeerID]network.AuthorityReport, len(u.roster))
	u.grants = make(map[network.PeerID]network.PeerID, len(u.roster))
	u.vote, u.published = 0, false
	term, local := u.contested, u.local
	u.mu.Unlock()

	u.statMigrating.Store(true)
	vlog.Warn("app", "msg", "authority lost; succession opened",
		"lost", uint64(lost), "term", uint64(term), "participant", uint64(local))
	u.sendReport()
	u.drive()
}

// sendReport floods this survivor's reach and retention. It is information rather
// than a commitment, so it may be sent again as links change; the vote below is
// what cannot be revised.
func (u *authority) sendReport() {
	u.mu.Lock()
	term, local, lost := u.contested, u.local, u.lost
	roster := slices.Clone(u.roster)
	u.mu.Unlock()
	if term == 0 || local == 0 {
		return
	}
	tick, records := u.a.corrections.retentionEvidence()
	rep := network.AuthorityReport{
		Term: term, From: local, Lost: lost,
		Links: u.linkedRoster(roster, lost), RetainedTick: tick, Retained: records,
	}
	u.recordReport(rep)
	body, err := network.EncodeAuthorityReport(rep)
	if err != nil {
		return
	}
	u.flood(network.MsgAuthorityReport, 0, body)
}

// linkedRoster is the roster members this instance is directly linked to, which is
// requirement (a)'s input: a candidate must reach a strict majority of the closed
// roster over links of its own, not through the participant that has gone.
func (u *authority) linkedRoster(roster []network.SessionParticipant, lost network.PeerID) []network.PeerID {
	link, ok := u.a.sessionTransport().(engine.LinkMeasuringPort)
	if !ok {
		return nil
	}
	out := make([]network.PeerID, 0, len(roster))
	for _, id := range link.Peers() {
		p := network.PeerID(id)
		if p == lost {
			continue
		}
		if slices.ContainsFunc(roster, func(r network.SessionParticipant) bool { return r.ID == p }) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// recordReport stores one survivor's input, reporting whether it was new.
func (u *authority) recordReport(rep network.AuthorityReport) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if rep.Term != u.contested || rep.From == 0 {
		return false
	}
	prior, had := u.reports[rep.From]
	u.reports[rep.From] = rep
	return !had || changedReport(prior, rep)
}

// changedReport reports whether a re-sent survey says anything new. A report is
// idempotent and re-sent as links change, so the flood terminates on content
// rather than on a hop count.
func changedReport(a, b network.AuthorityReport) bool {
	return a.Lost != b.Lost || a.RetainedTick != b.RetainedTick ||
		a.Retained != b.Retained || !slices.Equal(a.Links, b.Links)
}

// drive advances the succession. It is called from the correction loop, which runs
// between two ticks on every instance whichever half of the protocol it is, and
// from each succession frame that arrives — so the election proceeds on evidence
// rather than on a schedule.
func (u *authority) drive() {
	u.mu.Lock()
	contested, since := u.contested, u.since
	badge := u.badgeUntil
	u.mu.Unlock()

	tick := u.a.Position().Tick
	if badge != 0 && tick >= badge {
		u.mu.Lock()
		u.badgeUntil = 0
		u.mu.Unlock()
		u.statMigrating.Store(false)
	}
	if contested == 0 {
		return
	}
	u.tryVote()
	u.tryPublish()

	u.mu.Lock()
	stillOpen := u.contested == contested
	u.mu.Unlock()
	if stillOpen && tick > since+parameter.NetworkSuccessionTicks {
		u.giveUp()
	}
}

// tryVote casts this instance's single vote for the contested term, once its view
// is as complete as its own links allow.
//
// Two conditions, and they are different. A view narrower than a strict majority
// cannot elect anything, because eligibility is measured against the closed
// roster. A view missing a survivor this instance is *directly linked to* is a
// view that is still arriving, and voting from it would split the vote for no
// reason — so the wait is on evidence rather than on a clock, and it ends when the
// links this instance has have all answered.
func (u *authority) tryVote() {
	u.mu.Lock()
	if u.contested == 0 || u.vote != 0 {
		u.mu.Unlock()
		return
	}
	term, lost, local := u.contested, u.lost, u.local
	roster := slices.Clone(u.roster)
	reports := make(map[network.PeerID]network.AuthorityReport, len(u.reports))
	for k, v := range u.reports {
		reports[k] = v
	}
	u.mu.Unlock()

	if len(reports) < network.Majority(len(roster)) {
		return
	}
	for _, id := range u.linkedRoster(roster, lost) {
		if _, ok := reports[id]; !ok {
			return
		}
	}
	candidate, ok := network.ElectSuccessor(roster, lost, reports)
	if !ok {
		return // nothing eligible; the deadline turns this into local continuation
	}

	u.mu.Lock()
	if u.contested != term || u.vote != 0 {
		u.mu.Unlock()
		return
	}
	u.vote = candidate
	u.grants[local] = candidate
	u.mu.Unlock()

	body, err := network.EncodeAuthorityVote(network.AuthorityVote{
		Term: term, Voter: local, Candidate: candidate,
	})
	if err != nil {
		return
	}
	vlog.Info("app", "msg", "succession vote cast",
		"term", uint64(term), "voter", uint64(local), "candidate", uint64(candidate))
	u.flood(network.MsgAuthorityVote, 0, body)
}

// tryPublish turns a strict majority of votes into the record this instance
// authors under. It is the only place a term is entered by its own holder, and the
// majority is what makes that safe: every participant grants one vote per term, so
// two candidates cannot both reach one.
func (u *authority) tryPublish() {
	u.mu.Lock()
	if u.contested == 0 || u.published || u.local == 0 || u.vote != u.local {
		u.mu.Unlock()
		return
	}
	voters := make([]network.PeerID, 0, len(u.grants))
	for voter, candidate := range u.grants {
		if candidate == u.local {
			voters = append(voters, voter)
		}
	}
	if len(voters) < network.Majority(len(u.roster)) {
		u.mu.Unlock()
		return
	}
	slices.Sort(voters)
	u.published = true
	rec := network.HandoffRecord{
		Term:              u.contested,
		Authority:         u.local,
		Predecessor:       u.lost,
		Voters:            voters,
		Roster:            slices.Clone(u.roster),
		Anchor:            u.anchor,
		BarrierDelayTicks: u.delay,
	}
	u.mu.Unlock()

	rec.EvidenceTick, _ = u.a.corrections.retentionEvidence()
	if err := u.adopt(rec, 0); err != nil {
		vlog.Error("app", "msg", "succession could not adopt its own record", "error", err.Error())
		return
	}
	body, err := network.EncodeHandoff(rec)
	if err != nil {
		return
	}
	u.flood(network.MsgAuthorityHandoff, 0, body)
}

// giveUp ends a succession that found no eligible successor, or whose votes never
// reached a majority. What is left is exactly §4.3's behaviour, said plainly: this
// instance continues its own game from the last authoritative state.
func (u *authority) giveUp() {
	u.mu.Lock()
	if u.contested == 0 {
		u.mu.Unlock()
		return
	}
	term, lost := u.contested, u.lost
	u.contested, u.reports, u.grants, u.vote, u.published = 0, nil, nil, 0, false
	u.fork = true
	u.mu.Unlock()

	u.statMigrating.Store(false)
	u.statHostLost.Store(true)
	u.publish()
	vlog.Warn("app", "msg", "no succession possible; continuing locally",
		"term", uint64(term), "lost", uint64(lost))
	u.a.ctx.SetStatusMessage(
		"Host connection lost; continuing locally from the last authoritative state",
		4*parameter.StatusMessageDefaultTimeout, true)
}

// adopt installs a handoff record: the term, the authority, and the membership
// that moves with them.
//
// from is the link the record arrived on, or zero when this instance produced it.
func (u *authority) adopt(rec network.HandoffRecord, from uint32) error {
	u.mu.Lock()
	if err := rec.Validate(u.roster); err != nil {
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
	u.accepted[rec.Term] = rec
	u.contested, u.reports, u.grants, u.vote, u.published = 0, nil, nil, 0, false
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
		"predecessor", uint64(rec.Predecessor), "voters", len(rec.Voters),
		"evidence_tick", rec.EvidenceTick, "local", mine)
	u.a.ctx.SetStatusMessage(
		fmt.Sprintf("Authority moved to participant %d (term %d)", rec.Authority, rec.Term),
		2*parameter.StatusMessageDefaultTimeout, false)

	if body, err := network.EncodeHandoff(rec); err == nil {
		u.flood(network.MsgAuthorityHandoff, from, body)
	}
	return nil
}

// === inbound ===

// receive takes one succession frame. It runs between two ticks, from the
// correction loop's drain, so it may decode and decide.
func (u *authority) receive(kind uint8, from uint32, body []byte) {
	switch network.MessageType(kind) {
	case network.MsgAuthorityReport:
		u.onReport(from, body)
	case network.MsgAuthorityVote:
		u.onVote(from, body)
	case network.MsgAuthorityHandoff:
		u.onHandoff(from, body)
	}
}

// onReport records a survivor's input and joins the succession it announces. A
// participant that never saw the disconnect itself — one two links from the lost
// authority — learns of it here, which is why the reports are flooded: the
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
		return // a succession this instance has already resolved
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

// onVote records one participant's choice for the contested term.
func (u *authority) onVote(from uint32, body []byte) {
	v, err := network.DecodeAuthorityVote(body)
	if err != nil || v.Voter == 0 || v.Candidate == 0 {
		return
	}
	u.mu.Lock()
	if v.Term != u.contested {
		u.mu.Unlock()
		return
	}
	if prior, ok := u.grants[v.Voter]; ok {
		u.mu.Unlock()
		if prior != v.Candidate {
			u.statRefused.Add(1)
			vlog.Warn("app", "msg", "participant voted twice in one term",
				"term", uint64(v.Term), "voter", uint64(v.Voter),
				"first", uint64(prior), "second", uint64(v.Candidate))
		}
		return
	}
	u.grants[v.Voter] = v.Candidate
	u.mu.Unlock()

	u.flood(network.MsgAuthorityVote, from, body)
	u.drive()
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
// a report is idempotent, a vote is immutable, and a handoff is adopted once.
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

// ensureAuthorityCells registers the Phase 7 surface before the registry freezes.
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
	slot, ok := uint8(0), false
	for _, p := range rec.Roster {
		if p.ID == rec.Predecessor {
			slot, ok = p.Slot, true
			break
		}
	}
	if !ok {
		return
	}
	a.world.RunSafe(func() {
		a.world.PushEventFull(event.EventParticipantDeparted,
			&event.ParticipantDepartedPayload{Participant: uint32(rec.Predecessor), Slot: slot},
			event.OriginSession, core.DomainPlayer)
	})
	a.releaseParticipant(rec.Predecessor)
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
