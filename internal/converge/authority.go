package converge

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Authority is this instance's view of who authors and under which generation,
// plus the succession it runs when that instance goes.
type Authority struct {
	inst Instance
	tel  snapshot.Telemetry
	log  *vlog.Log

	// slow is the eviction policy and watch the window each participant is being
	// judged over; see driveEviction. ingress is each link's budget per participant
	// it carries and metered its window; see driveIngress. Touched only by the
	// correction loop.
	slow      SlowPolicy
	watch     map[link.PeerID]slowWatch
	ingress   float64
	metered   map[link.PeerID]slowWatch
	statEvict *atomic.Int64

	// corrections is what authorship produces, and reach the links it needs to
	// move: a successor seeds its cadence from what it installed, and a survivor
	// dials each candidate on the list whose turn has come.
	corrections *Corrections
	reach       *Reach

	mu     sync.Mutex
	term   network.AuthorityTerm
	holder link.PeerID
	local  link.PeerID
	roster []network.RosterEntry
	anchor event.JoinAnchor

	// chain is the succession candidate list: every participant that declared a
	// port, in join order, adopted whole from an offer, a handoff or MsgPeerList.
	// See internal/network/reach.go. chainDue marks a participant appended since
	// the chain last went out, which drive publishes.
	chain    network.SuccessionChain
	chainDue bool

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

	// direct marks a link of this instance's own to its authority rather than a path
	// through a relay: a survivor that had one saw the loss itself, and redials it.
	direct bool

	// The succession in progress, if any. contested is the term the lost authority
	// held, zero when no walk is open; lost is that authority, and opened when this
	// instance saw it go. walkFrom is when the walk down the candidates began, zero
	// while it waits on this instance's redial of the lost authority (redialing).
	contested network.AuthorityTerm
	lost      link.PeerID
	opened    time.Time // [wall] the session stands frozen, so no tick counts it
	walkFrom  time.Time // [wall]
	redialing bool
	published bool

	// reports is each notice and membership report seen since the authority of term
	// reportsFor went, by sender and named term: what ends the flood and tells a host
	// who joined it. It outlives the handoff; resumeBy is when that host resumes, zero
	// once it has.
	reports    map[reportKey]struct{}
	reportsFor network.AuthorityTerm
	resumeBy   time.Time // [wall]

	// cutSince is when this instance lost every path to its authority, zero while it
	// has one; see driveCutOff.
	cutSince time.Time // [wall]

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
		watch:          make(map[link.PeerID]slowWatch),
		ingress:        parameter.NetworkIngressBytesPerSecond,
		metered:        make(map[link.PeerID]slowWatch),
		statEvict:      reg.Ints.Get("network.evicted"),
		slow: SlowPolicy{
			Window:         parameter.NetworkSlowWindow,
			LatePerSecond:  parameter.NetworkSlowLatePerSecond,
			BytesPerSecond: parameter.NetworkSlowBytesPerSecond,
		},
	}
}

// SlowPolicy is when the authority evicts a participant that cannot keep up: over
// Window, its epochs reached the authority late at least LatePerSecond, and its
// link carried at least BytesPerSecond. Zero drops a criterion; both zero, or no
// window, disables eviction.
type SlowPolicy struct {
	Window         time.Duration
	LatePerSecond  float64
	BytesPerSecond float64
}

func (p SlowPolicy) enabled() bool {
	return p.Window > 0 && (p.LatePerSecond > 0 || p.BytesPerSecond > 0)
}

// slowWatch is one participant's current window: where it started, the count it
// started from, and whether it is the grace window after the participant arrived.
type slowWatch struct {
	since time.Time
	from  uint64
	grace bool
}

// SetSlowPolicy replaces the eviction policy. Call before the session runs.
func (u *Authority) SetSlowPolicy(p SlowPolicy) { u.slow = p }

// driveEviction judges each participant's last window on the authority. Only a
// participant's own late crossings count, as the authority received them from its
// link, so a slow participant's actions relayed to the others never mark them; the
// first window after one arrives is grace, because a join catches up.
func (u *Authority) driveEviction(now time.Time) {
	if !u.slow.enabled() || !u.isAuthority() {
		clear(u.watch)
		return
	}
	meter, _ := u.inst.Transport().(engine.LinkMeasuringPort)
	local := u.inst.LocalParticipant()
	seen := make(map[link.PeerID]bool, len(u.watch))
	for _, e := range u.currentRoster() {
		if uint32(e.ID) == local {
			continue
		}
		seen[e.ID] = true
		late := u.inst.CommitLate(uint32(e.ID))
		w, ok := u.watch[e.ID]
		if !ok || late < w.from {
			u.watch[e.ID] = slowWatch{since: now, from: late, grace: true}
			continue
		}
		elapsed := now.Sub(w.since)
		if elapsed < u.slow.Window {
			continue
		}
		u.watch[e.ID] = slowWatch{since: now, from: late}
		rate := float64(late-w.from) / elapsed.Seconds()
		var bps float64
		if meter != nil {
			bps = meter.LinkMetric(uint32(e.ID)).Throughput
		}
		if w.grace || rate < u.slow.LatePerSecond || bps < u.slow.BytesPerSecond {
			continue
		}
		if u.inst.DropParticipant(uint32(e.ID)) {
			u.statEvict.Add(1)
			u.log.Warn("session", "msg", "participant evicted as too slow",
				"peer", e.ID, "late_per_s", rate, "bytes_per_s", bps,
				"window", u.slow.Window.String())
		}
	}
	for id := range u.watch {
		if !seen[id] {
			delete(u.watch, id)
		}
	}
}

// driveIngress budgets each link's ingress per participant it carries, itself and
// the bots it holds, so a relay that floods leaves with its subtree and stalls
// nobody else. A held bot is judged through its holder's link; a person on none of
// this authority's links may reach it through any, so every link carries them too.
func (u *Authority) driveIngress(now time.Time) {
	meter, ok := u.inst.Transport().(engine.LinkMeasuringPort)
	if u.slow.Window <= 0 || u.ingress <= 0 || !u.isAuthority() || !ok {
		clear(u.metered)
		return
	}
	roster, local, peers := u.currentRoster(), link.PeerID(u.inst.LocalParticipant()), meter.Peers()
	unlinked := func(e network.RosterEntry) bool {
		return e.ID != local && e.Holder == 0 && !slices.Contains(peers, uint32(e.ID))
	}
	behind := 0
	for _, e := range roster {
		if unlinked(e) {
			behind++
		}
	}
	seen := make(map[link.PeerID]bool, len(roster))
	for _, e := range roster {
		if e.ID == local || e.Holder != 0 || unlinked(e) {
			continue
		}
		seen[e.ID] = true
		in := u.inst.Ingress(uint32(e.ID))
		w, ok := u.metered[e.ID]
		if !ok || in < w.from {
			u.metered[e.ID] = slowWatch{since: now, from: in}
			continue
		}
		elapsed := now.Sub(w.since)
		if elapsed < u.slow.Window {
			continue
		}
		u.metered[e.ID] = slowWatch{since: now, from: in}
		carried := 1 + behind
		for _, o := range roster {
			if o.Holder == e.ID {
				carried++
			}
		}
		rate := float64(in-w.from) / elapsed.Seconds()
		if rate > u.ingress*float64(carried) && u.inst.DropParticipant(uint32(e.ID)) {
			u.statEvict.Add(1)
			u.log.Warn("session", "msg", "participant evicted for what its link sends",
				"peer", e.ID, "bytes_per_s", rate, "carried", carried, "window", u.slow.Window.String())
		}
	}
	for id := range u.metered {
		if !seen[id] {
			delete(u.metered, id)
		}
	}
}

// Open records the term and membership this instance enters a session under. It
// is the same call from every door — a tick-zero lobby, a mid-run join, a `:host`
// that opens a solo run, and a run reopening its world after `:leave` — because
// they differ in how the offer was obtained and not in what it says. Whatever a
// session left here, its succession included, ends with it.
func (u *Authority) Open(o network.SessionOffer, local link.PeerID) {
	u.mu.Lock()
	u.term = max(o.Term, network.FirstTerm)
	u.holder = o.Host
	u.local = local
	u.roster = slices.Clone(o.Roster)
	u.anchor = o.Anchor
	u.chain = slices.Clone(o.Chain)
	u.fixed = o.FixedAuthority
	u.direct = o.Relay == 0 && o.Host != local
	u.fork = false
	u.record = network.HandoffRecord{}
	clear(u.accepted)
	u.contested, u.lost, u.opened, u.walkFrom = 0, 0, time.Time{}, time.Time{}
	u.redialing, u.published, u.cutSince = false, false, time.Time{}
	u.reports, u.reportsFor, u.resumeBy = nil, u.term, time.Time{}
	u.reconcileAt, u.badgeUntil = 0, 0
	u.mu.Unlock()
	u.statMigrating.Store(false)
	u.statHostLost.Store(false)
	u.publish()
}

// Pin sets whether authorship stays with the participant that opened the session.
// A session opened for its holder's own bots takes the policy it opens to others on.
func (u *Authority) Pin(fixed bool) {
	u.mu.Lock()
	u.fixed = fixed
	u.mu.Unlock()
}

// Term, Holder and Local are the three identities the rest of the session reads.
func (u *Authority) Term() network.AuthorityTerm {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.term
}

// Holder is the participant currently authoring.
func (u *Authority) Holder() link.PeerID {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.holder
}

// Local is this instance's own identity in the session.
func (u *Authority) Local() link.PeerID {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.local
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
			"This instance is a local fork; the session has elected a new authority and cannot be rejoined", parameter.StatusAlert)
	}
	return false
}

// refuse records and reports one artifact turned away by the term gate.
func (u *Authority) refuse(from uint32, term network.AuthorityTerm, why string) {
	u.statRefused.Add(1)
	u.log.Warn("session", "msg", "authoritative artifact refused",
		"peer", from, "term", uint64(term), "held", uint64(u.Term()), "reason", why)
}

// === succession ===

// reportKey is one report: who sent it, and the term it names.
type reportKey struct {
	from link.PeerID
	term network.AuthorityTerm
}

// peerLost is the transport's report that a direct neighbour has gone. It starts
// a succession only for the participant that was authoring; any other departure
// is the authority's to cross.
func (u *Authority) peerLost(id uint32) {
	u.mu.Lock()
	start := u.term > 0 && link.PeerID(id) == u.holder && u.local != u.holder && u.contested == 0
	u.mu.Unlock()
	if !start {
		return
	}
	u.beginSuccession(link.PeerID(id))
}

// beginSuccession opens the walk down the candidates for the authority that went and
// floods this survivor's notice. One with a link of its own to that authority
// redials it first, and its walk begins when the redial ends; any other walks at
// once.
func (u *Authority) beginSuccession(lost link.PeerID) {
	u.mu.Lock()
	if u.contested != 0 || u.term == 0 || u.fork {
		u.mu.Unlock()
		return
	}
	now := time.Now() // [wall]
	u.contested, u.lost, u.opened = u.term, lost, now
	u.walkFrom, u.redialing, u.published = time.Time{}, false, false
	u.reportsFor = u.term
	maps.DeleteFunc(u.reports, func(k reportKey, _ struct{}) bool { return k.term < u.term })
	held, local, fixed, direct := u.contested, u.local, u.fixed, u.direct
	u.mu.Unlock()

	redial := direct && u.inst.Redial(lost)
	u.mu.Lock()
	if u.contested == held {
		u.redialing = redial
		if !redial {
			u.walkFrom = now
		}
	}
	u.mu.Unlock()
	// Nobody plays on in a world the next host is about to replace, so the session
	// stands still until it resumes; a pinned one forks once its redial ends.
	if !fixed || redial {
		u.inst.Freeze()
	}
	u.statMigrating.Store(true)
	u.log.Warn("session", "msg", "authority lost; walking the succession list",
		"peer", uint64(lost), "term", uint64(held), "local", uint64(local), "redial", redial)
	u.sendReport()
	u.drive()
}

// Redialing reports that this instance still waits on its redial of lost: nothing
// ended the wait and its window has not run out.
func (u *Authority) Redialing(lost link.PeerID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.redialOpenLocked(lost, time.Now()) // [wall]
}

// RedialAdmitted reports whether the run may go back to lost through the dial that
// authority admitted, which it may until the walk begins. On true this instance
// takes no turn and follows no record: the run it is replaced by is in its place.
func (u *Authority) RedialAdmitted(lost link.PeerID) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.redialOpenLocked(lost, time.Now()) { // [wall]
		return false
	}
	u.contested, u.redialing, u.fork = 0, false, true
	return true
}

// RedialRefused ends the wait on this instance's redial of lost: that authority
// refused it, nothing answered at its door, or it holds no place for this instance.
// The walk begins.
func (u *Authority) RedialRefused(lost link.PeerID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if now := time.Now(); u.redialOpenLocked(lost, now) { // [wall]
		u.walkFrom = now
	}
}

// redialOpenLocked is Redialing at now. Caller holds mu.
func (u *Authority) redialOpenLocked(lost link.PeerID, now time.Time) bool {
	return u.contested != 0 && u.lost == lost && u.redialing && u.walkFrom.IsZero() &&
		now.Sub(u.opened) < parameter.NetworkSuccessionWindow
}

// sendReport floods this survivor's notice that its authority went, again on a link
// opened during the walk: the far end may host, or carry this instance to a host,
// and the record it took was flooded before the link existed.
func (u *Authority) sendReport() {
	u.mu.Lock()
	rep := network.AuthorityReport{Term: u.contested, From: u.local, Lost: u.lost}
	u.mu.Unlock()
	u.report(rep)
}

// report records and floods one report of this instance's own.
func (u *Authority) report(rep network.AuthorityReport) {
	if rep.Term == 0 || rep.From == 0 {
		return
	}
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

// seat reports whether this instance is a bot some participant holds.
func (u *Authority) seat() bool {
	local, roster := link.PeerID(u.inst.LocalParticipant()), u.currentRoster()
	i := slices.IndexFunc(roster, func(e network.RosterEntry) bool { return e.ID == local })
	return i >= 0 && roster[i].Holder != 0
}

// recordReport notes one report, reporting whether it was new. The set is what ends
// the flood: a report says one thing and says it once. One naming a term before the
// lost authority's is history.
func (u *Authority) recordReport(rep network.AuthorityReport) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	k := reportKey{from: rep.From, term: rep.Term}
	if u.term == 0 || rep.From == 0 || rep.Term < u.reportsFor {
		return false
	}
	if _, seen := u.reports[k]; seen {
		return false
	}
	if u.reports == nil {
		u.reports = make(map[reportKey]struct{}, 2*len(u.roster))
	}
	u.reports[k] = struct{}{}
	return true
}

// reportedLocked reports a membership id sent at term. Caller holds mu.
func (u *Authority) reportedLocked(id link.PeerID, term network.AuthorityTerm) bool {
	_, ok := u.reports[reportKey{from: id, term: term}]
	return ok
}

// drive advances the succession. It is called from the correction loop, which runs
// between two ticks on every instance whichever half of the protocol it is, and
// from each succession frame that arrives — so the walk proceeds on evidence as
// well as on its deadlines.
func (u *Authority) drive() {
	u.mu.Lock()
	contested := u.contested
	badge, reconcile := u.badgeUntil, u.reconcileAt
	due := u.chainDue
	u.chainDue = false
	u.mu.Unlock()

	if due {
		u.PublishChain()
	}
	tick := u.inst.Position().Tick
	if badge != 0 && tick >= badge {
		u.mu.Lock()
		u.badgeUntil = 0
		u.mu.Unlock()
		u.statMigrating.Store(false)
	}
	if reconcile != 0 && tick >= reconcile {
		u.reconcile()
	}
	now := time.Now() // [wall] the walk and the policies below run over links, not the game
	cut := contested == 0 && u.reach.cutOff()
	// The reachability work runs on the same loop and for the same reason: it is
	// between two ticks, on every instance, whichever half of the protocol it is.
	u.reach.drive(contested != 0 || cut)
	u.driveEviction(now)
	u.driveIngress(now)
	if contested != 0 {
		u.driveWalk(now)
	}
	u.driveCutOff(cut, now)
	u.driveResume()
}

// driveWalk takes this instance's turn on the list when it comes, and gives the
// session up past the last one, or at once in a pinned session, which has nobody
// to walk to. A redial nothing ended runs one window.
func (u *Authority) driveWalk(now time.Time) {
	roster := u.currentRoster()
	u.mu.Lock()
	if u.walkFrom.IsZero() && now.Sub(u.opened) >= parameter.NetworkSuccessionWindow {
		u.walkFrom = u.opened.Add(parameter.NetworkSuccessionWindow)
	}
	contested, from, fixed := u.contested, u.walkFrom, u.fixed
	turns := len(network.SuccessionCandidates(roster, u.lost, u.chain))
	u.mu.Unlock()
	if from.IsZero() {
		return
	}
	if !fixed {
		u.trySucceed(now)
	}
	u.mu.Lock()
	stillOpen := u.contested == contested
	u.mu.Unlock()
	switch {
	case !stillOpen:
	case fixed:
		u.giveUp("the session pinned its authority")
	case now.Sub(from) >= time.Duration(turns)*parameter.NetworkSuccessionWindow:
		u.giveUp("no candidate's record arrived")
	}
}

// driveCutOff gives up a session this instance has had no path to for as long as a
// walk down the whole list takes: no candidate it reached carries it.
func (u *Authority) driveCutOff(cut bool, now time.Time) {
	u.mu.Lock()
	if !cut {
		u.cutSince = time.Time{}
		u.mu.Unlock()
		return
	}
	if u.cutSince.IsZero() {
		u.cutSince = now
	}
	since, holder, chain := u.cutSince, u.holder, slices.Clone(u.chain)
	u.mu.Unlock()
	turns := len(network.SuccessionCandidates(u.currentRoster(), holder, chain)) + 1
	if now.Sub(since) >= time.Duration(turns)*parameter.NetworkSuccessionWindow {
		u.giveUp("no participant carries this instance to its authority")
	}
}

// driveResume ends the host's window: its group resumes once every survivor's
// membership is in, or once the window runs out for whoever is missing. The lost
// authority's own bots went with it, and nothing reports for a Wraith.
func (u *Authority) driveResume() {
	u.mu.Lock()
	by, lost := u.resumeBy, u.record.Predecessor
	u.mu.Unlock()
	if by.IsZero() {
		return
	}
	roster := u.currentRoster()
	u.mu.Lock()
	missing := slices.ContainsFunc(roster, func(p network.RosterEntry) bool {
		return p.ID != lost && p.Holder != lost && !p.Away && !u.reportedLocked(p.ID, u.term)
	})
	if u.resumeBy.IsZero() || missing && time.Now().Before(by) { // [wall]
		u.mu.Unlock()
		return
	}
	u.resumeBy = time.Time{}
	u.mu.Unlock()
	u.inst.Thaw()
}

// reconcile keeps the place of every participant the host's group does not hold as
// a Wraith, a floor after play resumed: a survivor that followed another host, or
// none, reported no membership at this term. Its bots wait with it, and one away
// waits already. The lost authority's departure crossed as the term was taken.
func (u *Authority) reconcile() {
	roster := u.currentRoster()
	u.mu.Lock()
	u.reconcileAt = 0
	var away []network.RosterEntry
	for _, p := range roster {
		if p.ID != u.local && p.ID != u.record.Predecessor && p.Holder == 0 && !p.Away &&
			!u.reportedLocked(p.ID, u.term) {
			away = append(away, p)
		}
	}
	u.mu.Unlock()
	if len(away) > 0 {
		u.inst.CrossDepartures(away)
	}
}

// restartRun moves the pending deadlines onto a run that restarted the tick count
// at tick: each starts again there, which can only lengthen it, where a deadline
// from the run before would not fall due until the new run passed it.
func (u *Authority) restartRun(tick uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.badgeUntil != 0 {
		u.badgeUntil = tick + parameter.NetworkMigrationBadgeTicks
	}
	if u.reconcileAt != 0 {
		u.reconcileAt = tick + parameter.NetworkSuccessionTicks
	}
}

// trySucceed takes a term when this instance's turn on the list has come and no
// record reached it first. Candidate k takes the term k past the lost one's once
// k-1 turns of a window each passed, so no two hosts' records share a term and an
// earlier candidate's record wins wherever it arrives in time. The one self-check is
// retention: without a baseline it lets its turn pass.
func (u *Authority) trySucceed(now time.Time) {
	roster := u.currentRoster()
	u.mu.Lock()
	if u.contested == 0 || u.published || u.local == 0 || u.walkFrom.IsZero() {
		u.mu.Unlock()
		return
	}
	held, lost, local, from := u.contested, u.lost, u.local, u.walkFrom
	k := slices.Index(network.SuccessionCandidates(roster, lost, u.chain), local) + 1
	u.mu.Unlock()
	if k == 0 || now.Sub(from) < time.Duration(k-1)*parameter.NetworkSuccessionWindow {
		return
	}
	evidenceTick, retained := u.corrections.retentionEvidence()
	if retained == 0 {
		return
	}

	u.mu.Lock()
	if u.contested != held || u.published {
		u.mu.Unlock()
		return
	}
	u.published = true
	rec := network.HandoffRecord{
		Term:        held + network.AuthorityTerm(k),
		Authority:   local,
		Predecessor: lost,
		Roster:      roster,
		Anchor:      u.anchor,
		// The predecessor's identity returns to the pool, so its entry describes
		// nobody now.
		Chain:        u.chain.Without(lost),
		EvidenceTick: evidenceTick,
	}
	u.mu.Unlock()

	if err := u.adopt(rec, 0); err != nil {
		u.log.Error("session", "msg", "succession could not adopt its own record", "error", err.Error())
		return
	}
	// The roster taken over names everyone, the group or not. Reconciled a floor
	// after play resumes rather than now, because a survivor a relay away is still
	// arriving and keeping it away here would take a cursor it still simulates.
	u.mu.Lock()
	u.reconcileAt = u.inst.Position().Tick + parameter.NetworkSuccessionTicks
	u.resumeBy = now.Add(parameter.NetworkSuccessionWindow)
	u.mu.Unlock()
}

// giveUp ends a walk no record answered, or a path no participant restored: this
// instance's turn never came or it had nothing retained to author from, or the
// session pinned its authority. What is left is the local continuation, said
// plainly: this instance continues its own game from the last authoritative state.
func (u *Authority) giveUp(why string) {
	// Read before the state is cleared: the run goes on with the roster it held when
	// its authority went.
	roster := u.currentRoster()
	u.mu.Lock()
	if u.fork || u.term == 0 {
		u.mu.Unlock()
		return
	}
	held, holder, local := u.term, u.holder, u.local
	u.contested, u.published, u.redialing = 0, false, false
	u.walkFrom, u.cutSince = time.Time{}, time.Time{}
	u.fork = true
	u.mu.Unlock()

	u.statMigrating.Store(false)
	u.statHostLost.Store(true)
	u.publish()
	u.log.Warn("session", "msg", "continuing locally", "held_term", uint64(held),
		"authority", uint64(holder), "reason", why)
	u.inst.SetStatusMessage(
		"Host connection lost; continuing locally from the last authoritative state", parameter.StatusAlert)
	u.inst.Forked(roster, local)
}

// adopt installs a handoff record: the term, the authority, and the membership
// that moves with them.
//
// from is the link the record arrived on, or zero when this instance produced it.
func (u *Authority) adopt(rec network.HandoffRecord, from uint32) error {
	roster := u.currentRoster()
	direct := from == 0 || u.linked(rec.Authority)
	u.mu.Lock()
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
	// One at or below the term held is history, and one for a loss this instance
	// followed another host through, or none, is a group it did not join: past its
	// window a survivor follows only the host it joined.
	if rec.Term <= u.term || u.fork || rec.Predecessor != u.holder {
		u.mu.Unlock()
		return nil
	}
	held := u.term
	if err := rec.Validate(roster, u.chain, held); err != nil {
		u.mu.Unlock()
		return err
	}
	u.term, u.holder = rec.Term, rec.Authority
	u.roster = slices.Clone(rec.Roster)
	u.anchor = rec.Anchor
	u.chain = slices.Clone(rec.Chain)
	u.record = rec
	u.accepted[rec.Term] = rec
	// An instance the record reaches before any notice sees its authority go only now.
	learned := u.contested == 0
	if learned {
		u.lost, u.reportsFor = rec.Predecessor, held
		maps.DeleteFunc(u.reports, func(k reportKey, _ struct{}) bool { return k.term < held })
	}
	u.contested, u.published, u.redialing = 0, false, false
	u.walkFrom, u.direct = time.Time{}, direct
	u.badgeUntil = u.inst.Position().Tick + parameter.NetworkMigrationBadgeTicks
	local, mine := u.local, u.local == rec.Authority
	u.mu.Unlock()

	// A record from a link other than its host's is followed through that link.
	var via link.PeerID
	if !direct {
		via = link.PeerID(from)
	}
	if learned && !mine {
		u.inst.Freeze()
	}
	u.statMigrations.Add(1)
	u.statMigrating.Store(true)
	u.statHostLost.Store(false)
	u.publish()
	u.inst.AuthorityChanged(rec, mine, via)

	u.log.Warn("session", "msg", "authority handed off",
		"term", uint64(rec.Term), "authority", uint64(rec.Authority),
		"predecessor", uint64(rec.Predecessor), "roster", len(rec.Roster),
		"evidence_tick", rec.EvidenceTick, "local", mine, "via", uint64(via))
	u.inst.SetStatusMessage(
		fmt.Sprintf("Authority moved to participant %d (term %d)", rec.Authority, rec.Term), parameter.StatusAlert)

	// The host floods its record; a survivor passes it on only to the links it
	// carries, since one it does not carry could not follow the host through it.
	if body, err := network.EncodeHandoff(rec); err == nil {
		if mine {
			u.flood(network.MsgAuthorityHandoff, 0, body)
		} else {
			u.forward(network.MsgAuthorityHandoff, from, body)
		}
	}
	// Every member reports the term it joined, which is how its host counts its group.
	u.report(network.AuthorityReport{Term: rec.Term, From: local, Lost: rec.Predecessor, Via: via})
	return nil
}

// === reachability ===

// Chain is the candidate list, which is also the address book peers dial from.
func (u *Authority) Chain() network.SuccessionChain {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.chain)
}

// followed is the authority this instance follows through another participant's
// link rather than one of its own: a record that came through a relay, or a join
// at one. Reach dials it for a link of its own.
func (u *Authority) followed() (link.PeerID, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.term == 0 || u.fork || u.contested != 0 || u.direct || u.holder == u.local {
		return 0, false
	}
	return u.holder, true
}

// reachedHost notes a link of this instance's own to the authority it followed
// through another participant: its artifacts go there now (NetworkSystem.up), the
// relay lets it go as its window closes, and it redials that authority on a loss
// as one that had a link does.
func (u *Authority) reachedHost(host link.PeerID) {
	u.mu.Lock()
	reached := !u.direct && u.contested == 0 && u.holder == host && host != u.local
	if reached {
		u.direct = true
	}
	term := u.term
	u.mu.Unlock()
	if reached {
		u.log.Info("session", "msg", "authority reached on a link of this instance's own",
			"authority", uint64(host), "term", uint64(term))
	}
}

// successor is the first candidate this instance would walk to if the authority
// went now, exposed so a survivor holds a link to it before it needs one.
func (u *Authority) successor() (link.PeerID, bool) {
	roster := u.currentRoster()
	u.mu.Lock()
	defer u.mu.Unlock()
	list := network.SuccessionCandidates(roster, u.holder, u.chain)
	if len(list) == 0 {
		return 0, false
	}
	return list[0], true
}

// walkTargets is whom this instance dials: during a walk each candidate whose turn
// has begun, the first while it redials; cut off, every candidate, for one that
// carries it. Never itself.
func (u *Authority) walkTargets(now time.Time) []link.PeerID {
	roster := u.currentRoster()
	u.mu.Lock()
	defer u.mu.Unlock()
	list := network.SuccessionCandidates(roster, u.holder, u.chain)
	if u.contested != 0 {
		turns := 1
		if !u.walkFrom.IsZero() {
			turns += int(now.Sub(u.walkFrom) / parameter.NetworkSuccessionWindow)
		}
		list = list[:min(turns, len(list))]
	}
	return slices.DeleteFunc(list, func(id link.PeerID) bool { return id == u.local })
}

// appendChain adds one participant that declared a port, which only the
// coordinator does, and leaves the chain to drive to publish. A joiner is noted
// under the session lock, or the world lock when it came through a relay, and a
// flood reads the transport under the world lock.
func (u *Authority) appendChain(id link.PeerID, addr string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if next := u.chain.Append(id, addr); !slices.Equal(u.chain, next) {
		u.chain, u.chainDue = next, true
	}
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
	u.log.Info("session", "msg", "succession chain published",
		"term", uint64(term), "candidates", len(chain))
}

// ForgetReachable drops a departed participant. Local only: the departure crossing
// reaches every instance, so each drops the same entry without a broadcast — and
// this runs under the world lock, which flooding would deadlock on.
func (u *Authority) ForgetReachable(id link.PeerID) {
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
	switch link.MessageType(kind) {
	case network.MsgAuthorityReport:
		u.onReport(from, body)
	case network.MsgAuthorityHandoff:
		u.onHandoff(from, body)
	case network.MsgPeerList:
		u.onPeerList(from, body)
	}
}

// onReport notes one report and acts on it. A notice says its sender lost the
// authority of the term it names: a survivor two links from that authority learns
// of the loss here, and a host, or a survivor that carries the sender, answers with
// the record it follows. A membership report names the term its sender joined.
func (u *Authority) onReport(from uint32, body []byte) {
	rep, err := network.DecodeAuthorityReport(body)
	if err != nil || rep.From == 0 {
		return
	}
	// A survivor that follows its host through this instance names it, and the
	// host's artifacts flow on down to it from here.
	if rep.Via != 0 && rep.Via == u.Local() && from == uint32(rep.From) {
		u.inst.Carry(from)
	}
	// A sender that lost an authority still linked here was cut off, closed or
	// dismissed alone, which is no news here.
	if u.linked(rep.Lost) {
		return
	}
	u.mu.Lock()
	held, contested, holder, local, base := u.term, u.contested, u.holder, u.local, u.reportsFor
	resolved := contested == 0 && u.record.Term != 0 && u.record.Predecessor == rep.Lost
	u.mu.Unlock()
	switch {
	case rep.Lost == local || rep.Lost != holder && !resolved:
		return // a loss this instance is no survivor of
	case contested == 0 && rep.Lost == holder && rep.Term == held:
		u.beginSuccession(rep.Lost)
	case resolved && rep.Term == base:
		// Answered before the duplicate test: a survivor sends its notice again on
		// each link it opens, and a new one may be how it reaches a host.
		u.answer(from)
	}
	if !u.recordReport(rep) {
		return
	}
	u.flood(network.MsgAuthorityReport, from, body)
	u.drive()
}

// answer re-sends the record this instance follows to a survivor still walking: the
// host answers every one, any other survivor one it carries, which then follows the
// host through it.
func (u *Authority) answer(to uint32) {
	u.mu.Lock()
	rec, host := u.record, u.holder == u.local
	u.mu.Unlock()
	if rec.Term == 0 || to == 0 || !host && !u.inst.Carries(to) {
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

// onHandoff adopts, or refuses, one record. One whose predecessor is still linked
// here is another group's, this one's host being alive; should that link go, this
// instance's notice is answered with it.
func (u *Authority) onHandoff(from uint32, body []byte) {
	rec, err := network.DecodeHandoff(body)
	if err != nil || u.linked(rec.Predecessor) {
		return
	}
	if err := u.adopt(rec, from); err != nil {
		u.statRefused.Add(1)
		u.log.Warn("session", "msg", "handoff refused",
			"peer", from, "term", uint64(rec.Term), "authority", uint64(rec.Authority),
			"error", err.Error())
		u.inst.SetStatusMessage("Refused a conflicting authority handoff: "+err.Error(), parameter.StatusAlert)
	}
}

// forward passes one succession frame on to the links this instance carries but the
// one it arrived on.
func (u *Authority) forward(kind link.MessageType, exclude uint32, body []byte) {
	port := u.inst.Transport()
	p, ok := port.(interface{ Peers() []uint32 })
	if !ok || !port.IsRunning() {
		return
	}
	for _, id := range p.Peers() {
		if id != exclude && u.inst.Carries(id) {
			port.Send(id, uint8(kind), body)
		}
	}
}

// linked reports a direct link to id.
func (u *Authority) linked(id link.PeerID) bool {
	p, ok := u.inst.Transport().(interface{ Peers() []uint32 })
	return ok && id != 0 && slices.Contains(p.Peers(), uint32(id))
}

// flood forwards one succession frame to every direct neighbour but the link it
// arrived on. Deduplication is by term and participant rather than by a hop count:
// a report says one thing once, and a handoff is adopted once.
func (u *Authority) flood(kind link.MessageType, exclude uint32, body []byte) {
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
		line = fmt.Sprintf("term %d, following peer %d", term, holder)
	}
	if migrations > 0 {
		line += fmt.Sprintf(", %d handoff(s)", migrations)
	}
	if contested != 0 {
		line += "; host lost, walking the succession list"
	}
	if fork {
		line += "; LOCAL FORK — this instance is no longer part of the session"
	}
	return line
}
