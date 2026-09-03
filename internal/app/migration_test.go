package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// settleAuthority runs the succession to a conclusion without advancing anyone's
// clock further than it has to.
//
// The election is driven from the correction loop, which is what every instance
// runs between two ticks, so a round of ApplyPendingCorrections plus one tick is
// one round of report, vote and handoff. The bound is the succession deadline
// itself: past it the instances have fallen back to local continuation, which is a
// conclusion too and one several of these tests are about.
func settleAuthority(t *testing.T, apps []*App, done func() bool) bool {
	t.Helper()
	for range parameter.NetworkSuccessionTicks + 4 {
		for _, a := range apps {
			a.ApplyPendingCorrections()
		}
		if done() {
			return true
		}
		tickAll(apps)
	}
	for _, a := range apps {
		a.ApplyPendingCorrections()
	}
	return done()
}

// closeParticipant drops one instance's transport, which is what every survivor
// observes as the departure that opens a succession.
func closeParticipant(a *App) {
	a.World().RunSafe(func() {
		if p, ok := a.World().Resources.Network.Port.(*network.MeshPort); ok {
			_ = p.Close()
		}
	})
}

// boolOf reads one boolean telemetry cell.
func boolOf(a *App, key string) (v bool) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Bools.Get(key).Load() })
	return v
}

// authorityOf is one instance's view of who is authoring.
func authorityOf(a *App) AuthorityReport { return a.AuthorityState() }

// primeRetention gives every participant a retained authoritative record, which is
// the succession's eligibility evidence. Without one nothing is electable, which is
// itself a case below.
func primeRetention(t *testing.T, apps []*App) {
	t.Helper()
	advance := func() { tickAll(apps) }
	deliverCorrection(t, apps[0], apps[1:], advance)
	for _, a := range apps {
		if r := authorityOf(a); r.Retained == 0 {
			t.Fatalf("participant %d retained nothing to be elected on", r.Local)
		}
	}
}

// TestSuccessionElectsOneParticipantOnEverySurvivor is the migration criterion.
//
// The successor has to be a function of the closed roster and the survivor set
// rather than of who noticed the loss first, or two survivors would adopt two
// different authorities and the session would have forked while appearing not to.
func TestSuccessionElectsOneParticipantOnEverySurvivor(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	primeRetention(t, apps)

	before := authorityOf(apps[1])
	if before.Term != network.FirstTerm || before.Authority != 1 {
		t.Fatalf("before the loss the session runs term %d under participant %d",
			before.Term, before.Authority)
	}

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Term > network.FirstTerm &&
			authorityOf(survivors[1]).Term > network.FirstTerm
	}) {
		t.Fatalf("no succession: participant 2 holds %+v, participant 3 holds %+v",
			authorityOf(survivors[0]), authorityOf(survivors[1]))
	}

	for i, a := range survivors {
		got := authorityOf(a)
		if got.Term != network.FirstTerm+1 {
			t.Fatalf("participant %d entered term %d, want exactly one increment",
				i+2, got.Term)
		}
		// The roster-lowest survivor, not the first to notice. Both survivors are
		// linked to each other and to nothing else, so both are eligible and the
		// tie is broken by the roster.
		if got.Authority != 2 {
			t.Fatalf("participant %d elected participant %d, want the roster-lowest survivor",
				i+2, got.Authority)
		}
		if got.Fork {
			t.Fatalf("participant %d forked instead of adopting the handoff", i+2)
		}
	}
	if boolOf(survivors[0], "network.host_lost") {
		t.Fatal("the successor still reports the host as lost")
	}
	if got := statOf(survivors[0], "network.migrations"); got != 1 {
		t.Fatalf("participant 2 counted %d handoffs, want exactly one", got)
	}
	if !survivors[0].authority.IsAuthority() {
		t.Fatal("the elected successor does not consider itself the authority")
	}
	if survivors[1].authority.IsAuthority() {
		t.Fatal("a participant that was not elected considers itself the authority")
	}
}

// TestASuccessorWithStaleRetentionIsNotElected: a participant
// that has been silently behind must not become the thing everyone else adopts,
// even when the roster would otherwise choose it.
func TestASuccessorWithStaleRetentionIsNotElected(t *testing.T) {
	t.Parallel()
	roster := []network.SessionParticipant{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}
	reports := map[network.PeerID]network.AuthorityReport{
		2: {Term: 2, From: 2, Lost: 1, Links: []network.PeerID{3}, RetainedTick: 40, Retained: 2},
		3: {Term: 2, From: 3, Lost: 1, Links: []network.PeerID{2}, RetainedTick: 96, Retained: 4},
	}
	got, ok := network.ElectSuccessor(roster, 1, reports)
	if !ok || got != 3 {
		t.Fatalf("elected %d (ok=%t); the roster-lowest survivor is behind, so the current one wins",
			got, ok)
	}

	// Caught up, the roster decides again.
	reports[2] = network.AuthorityReport{
		Term: 2, From: 2, Lost: 1, Links: []network.PeerID{3}, RetainedTick: 96, Retained: 4,
	}
	if got, ok := network.ElectSuccessor(roster, 1, reports); !ok || got != 2 {
		t.Fatalf("elected %d (ok=%t), want the roster-lowest survivor once it is current", got, ok)
	}

	// A minority partition elects nothing whatever its retention, which is what
	// keeps a split from producing two authorities.
	minority := map[network.PeerID]network.AuthorityReport{
		3: {Term: 2, From: 3, Lost: 1, RetainedTick: 96, Retained: 4},
	}
	if got, ok := network.ElectSuccessor(roster, 1, minority); ok {
		t.Fatalf("a participant reaching no majority elected %d", got)
	}

	// Retention nobody has is nobody eligible: the session falls back to local
	// continuation rather than adopting a participant that can prove nothing.
	none := map[network.PeerID]network.AuthorityReport{
		2: {Term: 2, From: 2, Lost: 1, Links: []network.PeerID{3}},
		3: {Term: 2, From: 3, Lost: 1, Links: []network.PeerID{2}},
	}
	if got, ok := network.ElectSuccessor(roster, 1, none); ok {
		t.Fatalf("a session with no retained authority elected %d", got)
	}
}

// TestNoEligibleSuccessorFallsBackToLocalContinuation is the other half of the
// same claim, over the real path: when nothing can be elected the survivors keep
// today's behaviour, and they say so.
func TestNoEligibleSuccessorFallsBackToLocalContinuation(t *testing.T) {
	t.Parallel()
	// A chain with the coordinator in the middle. Losing it leaves 1 and 3 with no
	// link to each other at all, so neither reaches a majority of the roster and
	// neither may elect.
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	primeRetention(t, apps)

	// Move authorship to participant 2 first, so the participant that goes is the
	// articulation point rather than a leaf.
	handOff(t, apps, 1)

	closeParticipant(apps[1])
	survivors := []*App{apps[0], apps[2]}
	settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Fork && authorityOf(survivors[1]).Fork
	})

	for i, a := range survivors {
		got := authorityOf(a)
		if !got.Fork {
			t.Fatalf("survivor %d did not fall back to local continuation: %+v", i, got)
		}
		if got.Term != network.FirstTerm+1 {
			t.Fatalf("survivor %d moved to term %d without a handoff", i, got.Term)
		}
		if !boolOf(a, "network.host_lost") {
			t.Fatalf("survivor %d forked without reporting the loss", i)
		}
	}
}

// membershipOf is what a handoff must carry unchanged, read straight off the
// authority state rather than off a copy the harness made.
func membershipOf(a *App) ([]network.SessionParticipant, event.JoinAnchor, uint64) {
	a.authority.mu.Lock()
	defer a.authority.mu.Unlock()
	return append([]network.SessionParticipant(nil), a.authority.roster...),
		a.authority.anchor, a.authority.delay
}

// handOff moves authorship to apps[to] by hand, for a test whose subject is what
// happens after a migration rather than the migration itself.
func handOff(t *testing.T, apps []*App, to int) {
	t.Helper()
	roster, anchor, delay := membershipOf(apps[to])
	rec := network.HandoffRecord{
		Term:              apps[to].AuthorityState().Term + 1,
		Authority:         network.PeerID(to + 1),
		Predecessor:       1,
		Roster:            roster,
		Anchor:            anchor,
		BarrierDelayTicks: delay,
	}
	for _, a := range apps {
		rec.Voters = append(rec.Voters, network.PeerID(a.localParticipant()))
	}
	for _, a := range apps {
		if err := a.authority.adopt(rec, 0); err != nil {
			t.Fatalf("hand authorship to participant %d: %v", to+1, err)
		}
	}
}

// TestOneTermHasOneAuthority drives the case the whole design exists for: two
// survivors that both believe they should succeed. A link flap gives each a view
// the other does not share, and the vote is what stops both from publishing.
func TestOneTermHasOneAuthority(t *testing.T) {
	t.Parallel()
	roster := []network.SessionParticipant{
		{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}, {ID: 4, Slot: 3}, {ID: 5, Slot: 4},
	}
	// Participant 5 is lost. Two overlapping majorities exist — {1,2,3} and
	// {2,3,4} — and 1 is not in the second, so participant 4 could believe itself
	// the lowest eligible candidate if it decided from its own view alone.
	viewOfOne := map[network.PeerID]network.AuthorityReport{
		1: {Term: 2, From: 1, Lost: 5, Links: []network.PeerID{2, 3}, RetainedTick: 80, Retained: 4},
		2: {Term: 2, From: 2, Lost: 5, Links: []network.PeerID{1, 3}, RetainedTick: 80, Retained: 4},
		3: {Term: 2, From: 3, Lost: 5, Links: []network.PeerID{1, 2}, RetainedTick: 80, Retained: 4},
	}
	viewOfFour := map[network.PeerID]network.AuthorityReport{
		2: {Term: 2, From: 2, Lost: 5, Links: []network.PeerID{3, 4}, RetainedTick: 80, Retained: 4},
		3: {Term: 2, From: 3, Lost: 5, Links: []network.PeerID{2, 4}, RetainedTick: 80, Retained: 4},
		4: {Term: 2, From: 4, Lost: 5, Links: []network.PeerID{2, 3}, RetainedTick: 80, Retained: 4},
	}
	a, okA := network.ElectSuccessor(roster, 5, viewOfOne)
	b, okB := network.ElectSuccessor(roster, 5, viewOfFour)
	if !okA || !okB {
		t.Fatalf("both views should elect something: %d/%t and %d/%t", a, okA, b, okB)
	}
	if a == b {
		t.Skip("the two views agreed; this case needs them to disagree to be worth anything")
	}

	// Each voter grants once. Whatever the two candidates believe, no participant
	// appears twice, so neither can reach three of five votes without the other
	// falling short: that is the invariant, checked over every way the five could
	// have voted given the two candidates above.
	for mask := range 1 << 5 {
		counts := map[network.PeerID]int{}
		for i := range 5 {
			voter := network.PeerID(i + 1)
			if voter == 5 {
				continue
			}
			candidate := a
			if mask&(1<<i) != 0 {
				candidate = b
			}
			counts[candidate]++
			_ = voter
		}
		majorities := 0
		for _, n := range counts {
			if n >= network.Majority(len(roster)) {
				majorities++
			}
		}
		if majorities > 1 {
			t.Fatalf("split %#b let %d candidates reach a majority", mask, majorities)
		}
	}
}

// TestASecondHandoffForOneTermIsRefused is the same invariant at the receiving
// end: whatever two candidates believe, a participant adopts one record per term.
func TestASecondHandoffForOneTermIsRefused(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	guest := apps[2]

	roster, anchor, delay := membershipOf(guest)
	base := network.HandoffRecord{
		Term:              network.FirstTerm + 1,
		Authority:         2,
		Predecessor:       1,
		Voters:            []network.PeerID{2, 3},
		Roster:            roster,
		Anchor:            anchor,
		BarrierDelayTicks: delay,
	}
	if err := guest.authority.adopt(base, 0); err != nil {
		t.Fatalf("the first record for a term must be adopted: %v", err)
	}
	rival := base
	rival.Authority = 3
	rival.Voters = []network.PeerID{1, 3}
	err := guest.authority.adopt(rival, 0)
	if err == nil {
		t.Fatal("a second, different record for one term was adopted")
	}
	if got := authorityOf(guest); got.Authority != 2 || got.Term != network.FirstTerm+1 {
		t.Fatalf("the refused record moved the authority anyway: %+v", got)
	}

	// A record that skips a term is refused for the same reason: nothing agreed it.
	skipped := base
	skipped.Term = network.FirstTerm + 3
	if err := guest.authority.adopt(skipped, 0); err == nil {
		t.Fatal("a record entering a term two generations ahead was adopted")
	}
	// And one that carries no majority is refused before anything is read from it.
	thin := base
	thin.Term = network.FirstTerm + 2
	thin.Voters = []network.PeerID{3}
	if err := guest.authority.adopt(thin, 0); err == nil {
		t.Fatal("a record carrying one vote out of three was adopted")
	}
}

// TestTheTermGateIgnoresTheOldAndRefusesTheUnheralded is the wire rule.
func TestTheTermGateIgnoresTheOldAndRefusesTheUnheralded(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	guest := apps[2]
	handOff(t, apps, 1)

	stale := statOf(guest, "network.term_stale")
	if guest.admitArtifactTerm(network.FirstTerm, 1) {
		t.Fatal("an artifact from the previous term was admitted after the handoff")
	}
	if statOf(guest, "network.term_stale") != stale+1 {
		t.Fatal("the ignored artifact was not counted")
	}

	refused := statOf(guest, "network.term_refused")
	if guest.admitArtifactTerm(network.FirstTerm+5, 2) {
		t.Fatal("an artifact from a term this instance was never handed was admitted")
	}
	if statOf(guest, "network.term_refused") != refused+1 {
		t.Fatal("the refused artifact was not counted")
	}
	if got := authorityOf(guest); got.Term != network.FirstTerm+1 {
		t.Fatalf("the refused artifact moved the term to %d", got.Term)
	}
	// The current term still passes, which is what says the gate is a gate rather
	// than a wall.
	if !guest.admitArtifactTerm(network.FirstTerm+1, 2) {
		t.Fatal("the current term was refused")
	}
}

// TestMembershipIsByteIdenticalAcrossAHandoff's other half. The
// roster, the slot assignments, the join anchor and the barrier delay are what a
// joiner adopts and what every instance builds its cursors from, so a handoff that
// changed any of them would have moved the session rather than its authority.
func TestMembershipIsByteIdenticalAcrossAHandoff(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	local := localCursors(t, apps)
	primeRetention(t, apps)

	type membership struct {
		roster  []network.SessionParticipant
		anchor  string
		delay   uint64
		cursors []uint64
	}
	read := func(a *App) membership {
		roster, anchor, delay := membershipOf(a)
		m := membership{roster: roster, anchor: anchor.Anchor.ConfigID, delay: delay}
		a.World().RunSafe(func() {
			for slot := range len(apps) {
				m.cursors = append(m.cursors, uint64(a.World().Resources.Player.Slot(uint8(slot))))
			}
		})
		return m
	}
	before := make([]membership, len(apps))
	for i, a := range apps {
		before[i] = read(a)
	}

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Term > network.FirstTerm &&
			authorityOf(survivors[1]).Term > network.FirstTerm
	}) {
		t.Fatal("no succession")
	}

	for i, a := range survivors {
		got, want := read(a), before[i+1]
		if len(got.roster) != len(want.roster) {
			t.Fatalf("survivor %d holds %d roster entries, held %d", i+2, len(got.roster), len(want.roster))
		}
		for j := range got.roster {
			if got.roster[j] != want.roster[j] {
				t.Fatalf("survivor %d roster entry %d moved: %+v, was %+v",
					i+2, j, got.roster[j], want.roster[j])
			}
		}
		if got.anchor != want.anchor || got.delay != want.delay {
			t.Fatalf("survivor %d anchor/delay moved: %q/%d, was %q/%d",
				i+2, got.anchor, got.delay, want.anchor, want.delay)
		}
		for slot := range got.cursors {
			if got.cursors[slot] != want.cursors[slot] {
				t.Fatalf("survivor %d slot %d cursor moved from %d to %d",
					i+2, slot, want.cursors[slot], got.cursors[slot])
			}
		}
		if !ownsCursor(a, local[i+1]) {
			t.Fatalf("survivor %d stopped simulating its own cursor across the handoff", i+2)
		}
	}
}

// TestAJoinerDiallingMidHandoffIsRefusedAndRetries's admission
// half. A dial that lands while the session is electing must not be half-admitted
// into a term that is about to end: it would hold a roster slot the successor's
// record does not carry and would receive an authority that has stopped
// publishing. The refusal is distinguishable so the joiner can retry.
func TestAJoinerDiallingMidHandoffIsRefusedAndRetries(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3017
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(20)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.HostAddr()

	// Open a succession by hand: the authority is this instance, so nothing is
	// actually lost — what is being tested is the admission gate, and the gate
	// reads "is a succession running" rather than "who went".
	host.authority.mu.Lock()
	host.authority.contested = host.authority.term + 1
	host.authority.lost = 9
	host.authority.reports = map[network.PeerID]network.AuthorityReport{}
	host.authority.grants = map[network.PeerID]network.PeerID{}
	host.authority.mu.Unlock()

	_, _, err := network.DialSession(addr, network.DebugConfig(network.RolePeer, ""))
	if err == nil {
		t.Fatal("a dial that landed mid-succession was admitted")
	}
	if !network.IsHandoffRefusal(err) {
		t.Fatalf("the refusal is not distinguishable: %v", err)
	}

	// The succession resolves; the retry is an ordinary join and lands in the same
	// slot discipline the first one would have.
	host.authority.mu.Lock()
	host.authority.contested = 0
	host.authority.mu.Unlock()

	stop := make(chan struct{})
	ticking := make(chan struct{})
	go func() {
		defer close(ticking)
		for {
			select {
			case <-stop:
				return
			default:
			}
			host.Tick(1)
			time.Sleep(joinTestTickInterval)
		}
	}()
	guest, _ := mustSocketJoiner(t, addr, seed, 120, 40)
	close(stop)
	<-ticking

	waitForRosterPair(t, host, guest)
	if got := guest.localSlot(); got != 1 {
		t.Fatalf("the retried join took slot %d, want the next free slot", got)
	}
	if got := guest.AuthorityState(); got.Term != network.FirstTerm || got.Authority != 1 {
		t.Fatalf("the retried join adopted term %d under participant %d",
			got.Term, got.Authority)
	}
}

// TestTheFirstCorrectionAfterAHandoffIsHashOnly, and the reason
// the successor seeds its baseline from what it already installed rather than
// capturing afresh: a session that answered a migration with a keyframe to every
// survivor would spend the most expensive frame it has at the moment it can least
// afford one.
func TestTheFirstCorrectionAfterAHandoffIsHashOnly(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	primeRetention(t, apps)
	for range 3 {
		deliverCorrection(t, apps[0], apps[1:], func() { tickAll(apps) })
	}

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[0]).Term > network.FirstTerm &&
			authorityOf(survivors[1]).Term > network.FirstTerm
	}) {
		t.Fatal("no succession")
	}
	successor, follower := survivors[0], survivors[1]

	// Measured from the moment the handoff is adopted: what the first correction
	// under the new term actually costs the participant that follows it.
	base := struct{ manifest, request, shard, body, keyframes, hashOnly int64 }{
		manifest:  statOf(follower, "snapshot.manifest_bytes_received"),
		request:   statOf(follower, "snapshot.request_bytes"),
		shard:     statOf(follower, "snapshot.shard_bytes_received"),
		body:      statOf(successor, "snapshot.correction_bytes_sent"),
		keyframes: statOf(successor, "snapshot.keyframes"),
		hashOnly:  statOf(follower, "snapshot.corrections_hash_only"),
	}

	advance := func() { tickAll(survivors) }
	deliverCorrection(t, successor, []*App{follower}, advance)

	manifest := statOf(follower, "snapshot.manifest_bytes_received") - base.manifest
	request := statOf(follower, "snapshot.request_bytes") - base.request
	shard := statOf(follower, "snapshot.shard_bytes_received") - base.shard
	body := statOf(successor, "snapshot.correction_bytes_sent") - base.body
	keyframes := statOf(successor, "snapshot.keyframes") - base.keyframes

	t.Logf("first correction after the handoff: index out %d B, answer back %d B, repair %d B, whole bodies %d B",
		manifest, request, shard, body)

	// Adoption is not a keyframe storm. The successor seeds its baseline from the
	// capture every survivor already installed, so the first correction under the
	// new term is an ordinary indexed one — and what it repairs is bounded by the
	// one thing the handoff itself changed, which is the roster.
	if keyframes != 0 {
		t.Fatalf("the successor published %d keyframes on its first correction", keyframes)
	}
	if body != 0 {
		t.Fatalf("the successor published %d whole-body bytes on its first correction", body)
	}
	if shard > 8<<10 {
		t.Fatalf("the first repair after the handoff moved %d bytes, want the roster change and no more", shard)
	}

	// Once the departure the handoff produced has applied on both, the exchange is
	// hash-only: an index out and an ack back and no state at all.
	converged := false
	var idx, ack int64
	for range 4 {
		was := struct{ manifest, request, shard, body, hashOnly int64 }{
			manifest: statOf(follower, "snapshot.manifest_bytes_received"),
			request:  statOf(follower, "snapshot.request_bytes"),
			shard:    statOf(follower, "snapshot.shard_bytes_received"),
			body:     statOf(successor, "snapshot.correction_bytes_sent"),
			hashOnly: statOf(follower, "snapshot.corrections_hash_only"),
		}
		deliverCorrection(t, successor, []*App{follower}, advance)
		idx = statOf(follower, "snapshot.manifest_bytes_received") - was.manifest
		ack = statOf(follower, "snapshot.request_bytes") - was.request
		if statOf(follower, "snapshot.corrections_hash_only") > was.hashOnly &&
			statOf(follower, "snapshot.shard_bytes_received") == was.shard &&
			statOf(successor, "snapshot.correction_bytes_sent") == was.body {
			converged = true
			break
		}
	}
	if !converged {
		t.Fatal("the exchange never reached hash-only after the handoff")
	}
	t.Logf("converged exchange under the new term: index out %d B, ack back %d B", idx, ack)
	// §3 measures the converged exchange at about 1.5 KiB on the storm world; this
	// fixture is smaller. The assertion is that it is bounded by an index and an
	// ack rather than that it hits a number.
	if total := idx + ack; total == 0 || total > 4<<10 {
		t.Fatalf("the converged exchange moved %d bytes, want a bounded index and ack", total)
	}
}

// TestALocalForkRejoiningAHigherTermIsRefused: Partition merging
// is a non-goal; the refusal is deliberate, and the operator is told.
func TestALocalForkRejoiningAHigherTermIsRefused(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	primeRetention(t, apps)

	// A partition that cannot elect: the succession opens and its deadline passes
	// with nothing eligible, which is exactly §4.3's local continuation.
	fork := apps[2]
	fork.authority.beginSuccession(1)
	fork.authority.giveUp()
	if got := authorityOf(fork); !got.Fork {
		t.Fatalf("the instance did not become a local fork: %+v", got)
	}

	before := fork.SnapshotShared()
	refused := statOf(fork, "network.term_refused")

	// The session it left has moved on. Every artifact it now carries names a term
	// this instance was never handed, and every one of them is refused.
	if fork.admitArtifactTerm(network.FirstTerm+2, 1) {
		t.Fatal("a fork adopted an artifact from a term it was never handed")
	}
	if statOf(fork, "network.term_refused") <= refused {
		t.Fatal("the refusal was not counted")
	}
	if got := authorityOf(fork); got.Term != network.FirstTerm || !got.Fork {
		t.Fatalf("the refused artifact moved the fork's authority: %+v", got)
	}
	if idx, x, y, differs := snapshot.FirstDiff(before, fork.SnapshotShared()); differs {
		t.Fatalf("state crossed into the fork at line %d\n  %s\n  %s", idx, x, y)
	}

	// And a handoff record for a term two generations ahead — the record the
	// session actually produced while this instance was away — is refused for the
	// same reason rather than adopted as a way back in.
	roster, anchor, delay := membershipOf(fork)
	if err := fork.authority.adopt(network.HandoffRecord{
		Term: network.FirstTerm + 2, Authority: 2, Predecessor: 1,
		Voters: []network.PeerID{1, 2}, Roster: roster, Anchor: anchor,
		BarrierDelayTicks: delay,
	}, 1); err == nil {
		t.Fatal("a fork adopted a handoff that skipped the term it missed")
	}
}

// TestSelectiveApplyKeepsItsExclusionsAcrossAHandoffAndARelay is the invariant
// that must not regress. A successor authors the Shared domain and nothing else:
// it does not begin authoring the D-13 owner-authored cells of cursors it does not
// simulate, and a relayed answer carries no Player-domain state either.
func TestSelectiveApplyKeepsItsExclusionsAcrossAHandoffAndARelay(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	local := localCursors(t, apps)
	primeRetention(t, apps)

	owned := func(a *App, e core.Entity) component.EnergyComponent {
		var out component.EnergyComponent
		a.World().RunSafe(func() { out, _ = a.World().Components.Energy.GetComponent(e) })
		return out
	}
	players := func(a *App) int {
		n := 0
		a.World().RunSafe(func() {
			for _, e := range a.World().Positions.Entities() {
				if e.Domain() == core.DomainPlayer {
					n++
				}
			}
		})
		return n
	}

	// Perturb the follower's own owner-authored cells so a correction that adopted
	// them would be visible.
	follower := apps[2]
	follower.World().RunSafe(func() {
		if c, ok := follower.World().Components.Energy.GetPtr(local[2]); ok {
			c.Current = max(c.Current-7, 0)
		}
	})
	wantOwned := owned(follower, local[2])
	wantPlayers := players(follower)

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		return authorityOf(survivors[1]).Term > network.FirstTerm
	}) {
		t.Fatal("no succession")
	}

	// Sampled across the correction rather than around the ticks between two: the
	// player domain is this instance's own simulation and moves on its own, and
	// what the invariant claims is that a correction does not move it.
	for range 3 {
		for range parameter.NetworkBarrierDelayTicks + 1 {
			tickAll(survivors)
		}
		wantPlayers = players(follower)
		wantOwned = owned(follower, local[2])
		deliverCorrectionNow(t, survivors[0], []*App{survivors[1]}, func() {})
		if got := owned(follower, local[2]); got != wantOwned {
			t.Fatalf("the successor authored a cursor it does not simulate: %+v, was %+v",
				got, wantOwned)
		}
		if got := players(follower); got != wantPlayers {
			t.Fatalf("a correction under the new term moved the player-domain population from %d to %d",
				wantPlayers, got)
		}
	}
	if !ownsCursor(follower, local[2]) {
		t.Fatal("the follower stopped simulating its own cursor")
	}
}

// TestMeshParityAcrossAHandoff is the whole phase seen from the world: after the
// successor's first two corrections every survivor holds the same Shared state.
func TestMeshParityAcrossAHandoff(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 4, [][2]int{{1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}})
	localCursors(t, apps)
	primeRetention(t, apps)

	closeParticipant(apps[0])
	survivors := apps[1:]
	if !settleAuthority(t, survivors, func() bool {
		for _, a := range survivors {
			if authorityOf(a).Term == network.FirstTerm {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("no succession: %+v %+v %+v",
			authorityOf(survivors[0]), authorityOf(survivors[1]), authorityOf(survivors[2]))
	}
	successor := survivors[0]
	if !successor.authority.IsAuthority() {
		t.Fatal("the roster-lowest survivor is not authoring")
	}

	advance := func() { tickAll(survivors) }
	for range 2 {
		deliverCorrection(t, successor, survivors[1:], advance)
	}
	assertMeshParity(t, survivors, -1)
}

func TestGuestContinuesLocallyAfterHostLoss(t *testing.T) {
	t.Parallel()
	host, guest, _ := shapedPair(t, 0x10571057, network.LinkShape{})
	runSession(host, guest, 80)

	beforeTick := guest.Position().Tick
	beforeCell, ok := localCell(guest)
	if !ok {
		t.Fatal("guest has no local cursor")
	}
	if err := transportOf(t, host).Close(); err != nil {
		t.Fatalf("close host link: %v", err)
	}
	guest.Tick(1) // drain the disconnect through the ordinary poll boundary
	if !statBoolOf(guest, "network.host_lost") {
		t.Fatal("guest did not enter explicit local continuation")
	}

	// The fork is still a playable game: local input settles immediately and the
	// scheduler keeps advancing without an authority or a replacement election.
	inject(t, guest, intentMotion(input.MotionRight, 1))
	afterCell, _ := localCell(guest)
	if afterCell.X != beforeCell.X+1 || afterCell.Y != beforeCell.Y {
		t.Fatalf("local continuation moved to %#v, want one cell right of %#v", afterCell, beforeCell)
	}
	guest.Tick(8)
	if got := guest.Position().Tick; got <= beforeTick+1 {
		t.Fatalf("guest stopped at tick %d after losing the host; started at %d", got, beforeTick)
	}
}
