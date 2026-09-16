package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/converge"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// settleAuthority runs the succession to a conclusion without advancing anyone's
// clock further than it has to. The election is driven from the correction loop, so
// a round of ApplyPendingCorrections plus one tick is one round of report, vote and
// handoff. The bound is the succession deadline: past it the instances have fallen
// back to local continuation, which is a conclusion several of these tests are about.
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
func authorityOf(a *App) converge.AuthorityReport { return a.authority.State() }

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
	if !survivors[0].authority.State().Authoring() {
		t.Fatal("the elected successor does not consider itself the authority")
	}
	if survivors[1].authority.State().Authoring() {
		t.Fatal("a participant that was not elected considers itself the authority")
	}
}

// TestTheSuccessorIsTheRostersLowestSurvivor pins the succession rule itself: no
// reports, no links, no votes. When the centre of a star goes every survivor is
// alone, so a quorum rule elects nobody in the one shape the CLI builds; a function
// of the roster elects the same participant on every survivor without any of them
// exchanging anything.
func TestTheSuccessorIsTheRostersLowestSurvivor(t *testing.T) {
	t.Parallel()
	roster := []network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}

	// The host goes: the first guest the coordinator admitted takes over, because
	// identities are handed out lowest-free-first in arrival order.
	if got, ok := network.DesignatedSuccessor(roster, 1, nil); !ok || got != 2 {
		t.Fatalf("successor to the host = %d (ok=%t), want the first guest", got, ok)
	}
	// A later loss skips whoever is gone and nothing else.
	if got, ok := network.DesignatedSuccessor(roster, 2, nil); !ok || got != 1 {
		t.Fatalf("successor to participant 2 = %d (ok=%t), want 1", got, ok)
	}
	// A cursorless coordinator is not in the world's roster at all, so the
	// participant it lost is simply not among the candidates.
	guests := []network.RosterEntry{{ID: 2, Slot: 0}, {ID: 3, Slot: 1}}
	if got, ok := network.DesignatedSuccessor(guests, 1, nil); !ok || got != 2 {
		t.Fatalf("successor on a dedicated host's roster = %d (ok=%t), want 2", got, ok)
	}
	// The two-participant session, which a quorum could never serve: one survivor
	// of a roster of two is not a majority of two, and it is the whole session.
	if got, ok := network.DesignatedSuccessor(
		[]network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}}, 1, nil); !ok || got != 2 {
		t.Fatalf("the sole survivor of a pair = %d (ok=%t), want it to take the term", got, ok)
	}
	// Nobody left is nothing to continue.
	if got, ok := network.DesignatedSuccessor(
		[]network.RosterEntry{{ID: 1, Slot: 0}}, 1, nil); ok {
		t.Fatalf("a roster with no survivor designated %d", got)
	}
}

// TestOnlyTheDesignatedSuccessorMayHoldATerm is the receiving end of the same
// rule, and the whole of what replaced the vote: a receiver refuses a record
// naming anyone but the successor its own roster designates, so a survivor that
// decided from its own view alone cannot make itself the authority.
func TestOnlyTheDesignatedSuccessorMayHoldATerm(t *testing.T) {
	t.Parallel()
	roster := []network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2}}
	base := network.HandoffRecord{
		Term: network.FirstTerm + 1, Authority: 2, Predecessor: 1,
		Roster: roster, BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
	if err := base.Validate(roster, nil); err != nil {
		t.Fatalf("the designated successor's own record was refused: %v", err)
	}
	rival := base
	rival.Authority = 3
	if err := rival.Validate(roster, nil); err == nil {
		t.Fatal("a record naming a participant the roster does not designate was accepted")
	}
}

// TestTheFirstGuestSucceedsAHostThatLeaves is the case every real session is, and the
// one a quorum rule could never serve: one survivor out of a roster of two is a
// majority of nothing, so under a vote it could only fork — a guest playing on with a
// host cursor nobody would move again. The roster rule names it, and it continues the
// session as its own host.
func TestTheFirstGuestSucceedsAHostThatLeaves(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	primeRetention(t, apps)
	host, guest := apps[0], apps[1]

	closeParticipant(host)
	settleAuthority(t, []*App{guest}, func() bool { return authorityOf(guest).Authority == 2 })

	got := authorityOf(guest)
	if got.Authority != 2 || got.Local != 2 {
		t.Fatalf("the sole survivor did not take the term: %+v", got)
	}
	if got.Fork {
		t.Fatal("the successor reports itself as a local fork")
	}
	if got.Term != network.FirstTerm+1 {
		t.Fatalf("the successor entered term %d, want exactly one increment", got.Term)
	}
	if !guest.authority.State().Authoring() {
		t.Fatal("the successor does not consider itself the authority")
	}
	if boolOf(guest, "network.host_lost") {
		t.Fatal("the successor still reports the host as lost")
	}

	// And it authors a roster of one: the predecessor's cursor goes with the term,
	// which is the successor's first act under it.
	for range 2*parameter.NetworkSuccessionTicks + 4 {
		guest.Tick(1)
		guest.ApplyPendingCorrections()
	}
	var count int
	var own core.Entity
	guest.World().RunSafe(func() {
		count = guest.World().Resources.Player.Count()
		own = guest.World().Resources.Player.Slot(1)
	})
	if own == 0 {
		t.Fatal("the successor dropped its own cursor")
	}
	if count != 1 {
		t.Fatalf("the successor holds %d cursors, want only its own", count)
	}
}

// TestAnUnreachableSuccessorLeavesTheRestForking is the rule's cost and its benefit
// in one run. A chain with the authority in the middle: losing it leaves 1 and 3 with
// no link at all, so 1 takes the term alone and 3 computes the same successor, cannot
// hear it, and forks explicitly. Both then drop the participants they will never hear
// from again, because with no link left there is no destruction tick to agree.
func TestAnUnreachableSuccessorLeavesTheRestForking(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	primeRetention(t, apps)

	// Move authorship to participant 2 first, so the participant that goes is the
	// articulation point rather than a leaf.
	handOff(t, apps, 1)

	closeParticipant(apps[1])
	successor, cutOff := apps[0], apps[2]
	survivors := []*App{successor, cutOff}
	settleAuthority(t, survivors, func() bool {
		return authorityOf(successor).Authority == 1 && authorityOf(cutOff).Fork
	})

	if got := authorityOf(successor); got.Authority != 1 || got.Fork {
		t.Fatalf("the roster's successor did not take the term: %+v", got)
	}
	if got := authorityOf(successor).Term; got != network.FirstTerm+2 {
		t.Fatalf("the successor entered term %d, want one increment past the handoff", got)
	}
	if boolOf(successor, "network.host_lost") {
		t.Fatal("the successor still reports the authority as lost")
	}
	if got := authorityOf(cutOff); !got.Fork {
		t.Fatalf("the cut-off survivor did not fall back to local continuation: %+v", got)
	}
	if got := authorityOf(cutOff).Term; got != network.FirstTerm+1 {
		t.Fatalf("the cut-off survivor moved to term %d without hearing a handoff", got)
	}
	if !boolOf(cutOff, "network.host_lost") {
		t.Fatal("the cut-off survivor forked without reporting the loss")
	}

	// Each continues with the cursor it simulates and no others. The participants
	// on the far side of the loss — the authority that went, and behind it the one
	// that was only ever reachable through it — are cursors nothing will move
	// again, and no departure either instance can observe describes them. Left
	// there they are players that cannot be played and cannot leave.
	for range 2*parameter.NetworkSuccessionTicks + 4 {
		tickAll(survivors)
	}
	for i, a := range survivors {
		slot := uint8(2 * i) // participants 1 and 3, in slots 0 and 2
		var count int
		var own core.Entity
		a.World().RunSafe(func() {
			count = a.World().Resources.Player.Count()
			own = a.World().Resources.Player.Slot(slot)
		})
		if own == 0 {
			t.Fatalf("survivor %d dropped its own cursor in slot %d", i, slot)
		}
		if count != 1 {
			t.Fatalf("survivor %d holds %d cursors after continuing alone, want only its own",
				i, count)
		}
	}
}

// handOff moves authorship to apps[to], for a test whose subject is what happens
// after a migration rather than the migration itself. The record travels the way a
// real one does — in through the transport seam, adopted between two ticks — so
// what the session ends up holding is what a succession would have left it.
func handOff(t *testing.T, apps []*App, to int) {
	t.Helper()
	held := apps[to].authority.State()
	body, err := network.EncodeHandoff(network.HandoffRecord{
		Term:              held.Term + 1,
		Authority:         network.PeerID(to + 1),
		Predecessor:       1,
		Roster:            held.Roster,
		Anchor:            held.Anchor,
		BarrierDelayTicks: held.Delay,
	})
	if err != nil {
		t.Fatalf("encode the handoff: %v", err)
	}
	for _, a := range apps {
		a.receiveAuthorityFrame(uint8(network.MsgAuthorityHandoff), 0, body)
		a.ApplyPendingCorrections()
		if got := a.authority.State(); got.Term != held.Term+1 {
			t.Fatalf("participant %d holds term %d after the handoff, want %d",
				a.localParticipant(), got.Term, held.Term+1)
		}
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
		roster  []network.RosterEntry
		anchor  string
		delay   uint64
		cursors []uint64
	}
	read := func(a *App) membership {
		held := a.authority.State()
		m := membership{roster: held.Roster, anchor: held.Anchor.Anchor.ConfigID, delay: held.Delay}
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

// TestAJoinerDiallingMidHandoffIsRefused is the admission half. A dial that lands
// while the session is electing must not be half-admitted into a term that is about
// to end: it would hold a roster slot the successor's record does not carry and
// would receive an authority that has stopped publishing. The refusal is
// distinguishable so the joiner can retry — the retry itself is an ordinary join,
// pinned by the mid-run gate criteria.
func TestAJoinerDiallingMidHandoffIsRefused(t *testing.T) {
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

	// A session this instance follows rather than authors, and then the loss of
	// whoever was authoring it: the succession opens on the real path, and stands
	// down at once because this instance has retained nothing to author from. What
	// is being tested is the admission gate, which reads "is a succession running"
	// rather than "who went".
	host.openAuthority(network.SessionOffer{
		Anchor: host.JoinAnchor(), Host: 2, Assigned: hostParticipantID,
		Term: network.FirstTerm, BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
		Roster: []network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}},
	}, hostParticipantID)
	host.reportPeerLost(2)
	host.ApplyPendingCorrections()
	if !host.authority.Migrating() {
		t.Fatal("losing the authority opened no succession")
	}

	_, _, err := network.DialSession(addr, network.DebugConfig(network.RolePeer, ""))
	if err == nil {
		t.Fatal("a dial that landed mid-succession was admitted")
	}
	if !network.IsHandoffRefusal(err) {
		t.Fatalf("the refusal is not distinguishable: %v", err)
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
	// The cost gate measures the converged exchange at about 1.5 KiB on the storm world; this
	// fixture is smaller. The assertion is that it is bounded by an index and an
	// ack rather than that it hits a number.
	if total := idx + ack; total == 0 || total > 4<<10 {
		t.Fatalf("the converged exchange moved %d bytes, want a bounded index and ack", total)
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
	if !successor.authority.State().Authoring() {
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
	mirrorCursors(t, host, guest)
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
