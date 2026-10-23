package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
)

// settleAuthority runs the succession to a conclusion. The election is driven from
// the correction loop, so a round of ApplyPendingCorrections plus one tick is one
// round of report and handoff. The session stands frozen meanwhile, so the bound is
// the window on the wall clock: past it the instances have fallen back to local
// continuation, which is a conclusion several of these tests are about.
func settleAuthority(t *testing.T, apps []*App, done func() bool) bool {
	t.Helper()
	window := parameter.NetworkSuccessionWindow + time.Second
	for deadline := time.Now().Add(window); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
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
// The successor has to be a function of the chain and who went rather than of who
// noticed the loss first, or two survivors would adopt two different authorities
// and the session would have forked while appearing not to.
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
		// The chain's first candidate, not the first to notice: both survivors
		// read the same list.
		if got.Authority != 2 {
			t.Fatalf("participant %d elected participant %d, want the first candidate",
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
		t.Fatal("the first candidate does not consider itself the authority")
	}
	if survivors[1].authority.State().Authoring() {
		t.Fatal("a participant that was not elected considers itself the authority")
	}
}

// TestASessionStandsStillWhileItsHostChanges: every survivor stops when it sees its
// host go and takes no tick until the successor resumes, which waits for the notice
// of a survivor slow to see the loss; the successor's first tick then runs the rest.
func TestASessionStandsStillWhileItsHostChanges(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 4, [][2]int{{1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}})
	localCursors(t, apps)
	primeRetention(t, apps)
	at := apps[1].Position().Tick
	closeParticipant(apps[0])
	quick, slow := apps[1:3], apps[3]
	if !settleAuthority(t, quick, func() bool { return authorityOf(quick[1]).Authority == 2 }) {
		t.Fatalf("participant 3 never followed participant 2: %+v", authorityOf(quick[1]))
	}
	for range 4 {
		tickAll(quick)
	}
	for i, a := range quick {
		if got := a.Position().Tick; got != at {
			t.Fatalf("participant %d ran to tick %d while participant 4 had not seen the loss", i+2, got)
		}
	}
	slow.ApplyPendingCorrections()
	for range 4 {
		tickAll(apps[1:])
	}
	for i, a := range apps[1:] {
		if a.Context().TimeCtl.Held() || a.Position().Tick <= at {
			t.Fatalf("participant %d: held %v at tick %d, want running past %d",
				i+2, a.Context().TimeCtl.Held(), a.Position().Tick, at)
		}
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

	// And it plays on with the host's place waiting as a Wraith for its player: the
	// predecessor's departure is the successor's first act under the term.
	for range 2*parameter.NetworkSuccessionTicks + 4 {
		guest.Tick(1)
		guest.ApplyPendingCorrections()
	}
	var own core.Entity
	guest.World().RunSafe(func() { own = guest.World().Resources.Player.Slot(1) })
	if own == 0 {
		t.Fatal("the successor dropped its own cursor")
	}
	if !wraithIn(guest, 0) {
		t.Fatal("the host's place does not wait as a Wraith")
	}
}

// TestAGuestNoCandidateReachesForks: the centre of a star goes and its guests share
// no link. The first candidate takes the term alone; the other guest walks the list
// without reaching it and forks once its turn is over, giving up its claim, so what
// it plays from there is not what its player brings back. The host keeps the
// place of each participant it lost as a Wraith, and its own claim.
func TestAGuestNoCandidateReachesForks(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}}, 2)
	localCursors(t, apps)
	primeRetention(t, apps)
	claims := make([]*claim, len(apps))
	for i, a := range apps {
		claims[i] = a.claims.issue(false)
		a.claim.Store(claims[i])
	}

	closeParticipant(apps[0])
	successor, cutOff := apps[1], apps[2]
	survivors := []*App{successor, cutOff}
	settleAuthority(t, survivors, func() bool {
		return authorityOf(successor).Authority == 2 && authorityOf(cutOff).Fork &&
			!successor.Context().TimeCtl.Held()
	})
	if got := authorityOf(successor); got.Authority != 2 || got.Fork || got.Term != network.FirstTerm+1 {
		t.Fatalf("the first candidate did not take the next term: %+v", got)
	}
	if boolOf(successor, "network.host_lost") {
		t.Fatal("the successor still reports the authority as lost")
	}
	if got := authorityOf(cutOff); !got.Fork || got.Term != network.FirstTerm || !boolOf(cutOff, "network.host_lost") {
		t.Fatalf("the guest no candidate reached did not fork where it stood, saying so: %+v", got)
	}

	for range 2*parameter.NetworkSuccessionTicks + 4 {
		tickAll(survivors)
	}
	for _, slot := range []uint8{0, 2} {
		if !wraithIn(successor, slot) {
			t.Fatalf("the host keeps no Wraith in slot %d", slot)
		}
	}
	var count int
	cutOff.World().RunSafe(func() { count = cutOff.World().Resources.Player.Count() })
	if count != 1 {
		t.Fatalf("the fork holds %d cursors, want only its own", count)
	}
	if cutOff.claim.Load() != nil || claimHeld(cutOff, claims[2]) {
		t.Fatal("the fork still holds the claim its player comes back under")
	}
	if successor.claim.Load() != claims[1] || !claimHeld(successor, claims[1]) {
		t.Fatal("the successor gave up the claim it carries its session on under")
	}
}

// TestAGuestWhoseLinkDroppedRedialsItsPlace: a person's run whose own link to its
// host dropped, the host still there, dials the host's door again under the claim
// its place waits under; the dial the host admits back into that place is what the
// run is replaced by.
func TestAGuestWhoseLinkDroppedRedialsItsPlace(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	stopTicks := tickInBackground(host)
	t.Cleanup(func() { stopTicks(); host.Close() })
	guest, _ := driveBot(t, Config{JoinAddress: host.HostAddr()}, "patrol", nil)
	waitForCursors(t, host, 2)
	c, id := guest.claim.Load(), guest.localParticipant()
	guest.World().RunSafe(func() { guest.cfg.Mode = ModePlay }) // the mode whose loop takes a rejoin on
	if c == nil || !(instance{host}).DropParticipant(id) {
		t.Fatal("the guest holds no claim, or its link would not drop")
	}
	var req *restartRequest
	for deadline := time.Now().Add(socketWait); req == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		req = guest.restart.Load()
	}
	if req == nil || req.dialled == nil || req.dialled.claim != c || uint32(req.dialled.offer.Assigned) != id {
		t.Fatalf("the guest was not admitted back into its own place: %+v", req)
	}
	req.dialled.abandon(errors.New("the test is over"))
}

// TestASuccessorTakesPlayersAsTheFirstHostDid: a guest's own port turns a join away
// while another participant hosts, and takes one once the guest took the session
// over, into the term it holds and under the -players its first host set. :host
// then opens the door it names, keeping the policy the session's players hold, and
// the run is its host. A survivor whose link to it drops redials it where the chain
// says, the first host's door long shut, and is admitted back into its own place.
func TestASuccessorTakesPlayersAsTheFirstHostDid(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	host.cfg.Participants = 5
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	stopTicks := tickInBackground(host)
	stop := sync.OnceFunc(func() { stopTicks(); host.Close() })
	t.Cleanup(stop)
	successor, _ := driveBot(t, Config{JoinAddress: host.HostAddr()}, "patrol", nil)
	guest, _ := driveBot(t, Config{JoinAddress: host.HostAddr()}, "patrol", nil)
	waitForCursors(t, host, 3)
	// The first candidate is whichever guest's port the host confirmed first.
	var chain network.SuccessionChain
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if chain = guest.authority.Chain(); len(chain) == 2 && len(successor.authority.Chain()) == 2 {
			break
		}
	}
	if len(chain) != 2 {
		t.Fatalf("the guests hold the chain %v; want both of them in it", chain)
	}
	if chain[0].ID == network.PeerID(guest.localParticipant()) {
		successor, guest = guest, successor
	}
	id := network.PeerID(successor.localParticipant())
	door, _ := chain.Lookup(id)
	if _, _, err := network.DialSession(door, network.DebugConfig(network.RolePeer, "")); !errors.Is(err, errNotHosting) ||
		!strings.Contains(err.Error(), errNotHosting.Reason) {
		t.Fatalf("a join at a guest's port while another participant hosts: %v; want %q", err, errNotHosting.Reason)
	}
	port, err := guest.socketPort()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(socketWait); !port.Connected(uint32(id)) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}

	stop()
	for _, a := range []*App{successor, guest} {
		for deadline := time.Now().Add(socketWait); (a.authority.Holder() != id || a.authority.Migrating()) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		}
		if a.authority.Holder() != id {
			t.Fatalf("participant %d follows %d; want the first candidate, %d", a.localParticipant(), a.authority.Holder(), id)
		}
	}
	// Dialled once the session plays again, as a player meets it: a join whose gate
	// reads the frozen world fails if the thaw lands mid-read.
	for deadline := time.Now().Add(socketWait); successor.Context().TimeCtl.Held() && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if got := successor.playerCapacity(); got != 5 {
		t.Fatalf("the successor holds the session to %d players; want its first host's -players, 5", got)
	}
	if line, want := successor.SessionSummary(), "Session hosting "+successor.reach.DeclaredAddr()+","; !strings.HasPrefix(line, want) {
		t.Fatalf(":session on the successor reads %q; want it to open %q", line, want)
	}
	player, _ := driveBot(t, Config{JoinAddress: door}, "patrol", nil)
	if got := authorityOf(player); got.Authority != id || got.Term != network.FirstTerm+1 {
		t.Fatalf("a player that joined the successor holds %+v; want participant %d's term, %d", got, id, network.FirstTerm+1)
	}
	waitForCursors(t, successor, 4) // the first host's Wraith among them

	at := freeAddress(t)
	var fixed, opened, again error
	var role sessionRole
	successor.World().RunSafe(func() {
		fixed, opened = successor.beginHostingLocked(at, AuthorityHost), successor.beginHostingLocked(at, "")
		again, role = successor.hostErrorLocked(), successor.sessionRoleLocked()
	})
	if fixed == nil || opened != nil || role != roleHost || again == nil || !strings.Contains(again.Error(), "already hosting") {
		t.Fatalf(":host on the successor: %v under the host policy, %v under its own, then role %d and %v; "+
			"want refused, opened, a host, already hosting", fixed, opened, role, again)
	}
	if line, want := successor.SessionSummary(), "Session hosting "+at+","; !strings.HasPrefix(line, want) {
		t.Fatalf(":session on the successor after :host reads %q; want it to open %q", line, want)
	}
	late, _ := driveBot(t, Config{JoinAddress: at}, "patrol", nil)
	if got := authorityOf(late); got.Authority != id || got.Term != network.FirstTerm+1 {
		t.Fatalf("a player that joined at the door :host opened holds %+v; want participant %d's term, %d", got, id, network.FirstTerm+1)
	}
	waitForCursors(t, successor, 5)

	c, gid := guest.claim.Load(), guest.localParticipant()
	guest.World().RunSafe(func() { guest.cfg.Mode = ModePlay }) // the mode whose loop takes a rejoin on
	if c == nil || !(instance{successor}).DropParticipant(gid) {
		t.Fatal("the guest holds no claim, or its link would not drop")
	}
	var req *restartRequest
	for deadline := time.Now().Add(socketWait); req == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		req = guest.restart.Load()
	}
	if req == nil || req.dialled == nil || req.dialled.claim != c || uint32(req.dialled.offer.Assigned) != gid ||
		req.dialled.offer.Host != id {
		t.Fatalf("the guest was not admitted back into its own place at the successor: %+v", req)
	}
	req.dialled.abandon(errors.New("the test is over"))
}

// TestASuccessorHostsAtItsOwnPort: :host on a successor naming the port its session
// reaches it at opens nothing more, where a second bind would only collide with it,
// and the run hosts there.
func TestASuccessorHostsAtItsOwnPort(t *testing.T) {
	// Not parallel: real sockets against wall-clock deadlines.
	host := mustHeadless(t, fixtureSeed, BotWidth, BotHeight)
	tickUntilCursor(t, host)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	stopTicks := tickInBackground(host)
	stop := sync.OnceFunc(func() { stopTicks(); host.Close() })
	t.Cleanup(stop)
	successor, _ := driveBot(t, Config{JoinAddress: host.HostAddr()}, "patrol", nil)
	waitForCursors(t, successor, 2)
	id := network.PeerID(successor.localParticipant())
	for deadline := time.Now().Add(socketWait); len(successor.authority.Chain()) != 1 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	stop()
	for deadline := time.Now().Add(socketWait); (successor.authority.Holder() != id || successor.authority.Migrating()) && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	port, err := successor.socketPort()
	if err != nil || successor.authority.Holder() != id {
		t.Fatalf("the guest did not take the session over on a port of its own: %v", err)
	}
	own := port.ListenAddr()
	var opened error
	var role sessionRole
	successor.World().RunSafe(func() {
		opened = successor.beginHostingLocked(own, "")
		role = successor.sessionRoleLocked()
	})
	if opened != nil || role != roleHost {
		t.Fatalf(":host %s on the successor: %v, then role %d; want it hosting there", own, opened, role)
	}
	if line, want := successor.SessionSummary(), "Session hosting "+own+","; !strings.HasPrefix(line, want) {
		t.Fatalf(":session on the successor reads %q; want it to open %q", line, want)
	}
}

// claimHeld reads whether a run of a's process is in its session under c.
func claimHeld(a *App, c *claim) bool {
	a.claims.mu.Lock()
	defer a.claims.mu.Unlock()
	return c.held
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
		cursors []uint64
	}
	read := func(a *App) membership {
		held := a.authority.State()
		m := membership{roster: held.Roster, anchor: held.Anchor.Anchor.ScenarioID}
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
		if got.anchor != want.anchor {
			t.Fatalf("survivor %d anchor moved: %q, was %q", i+2, got.anchor, want.anchor)
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
// mid-succession must not be half-admitted into a term about to end, holding a
// slot the next host's record does not carry from an authority that stopped
// publishing. The refusal is one the joiner retries, as an ordinary join the
// mid-run gate criteria pin.
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
	// whoever was authoring it: the succession opens on the real path, and this
	// instance's turn passes with nothing retained to author from, the walk open for
	// its window. What is being tested is the admission gate, which reads "is a
	// succession running" rather than "who went".
	host.openAuthority(network.SessionOffer{
		Anchor: host.JoinAnchor(), Host: 2, Assigned: engine.CoordinatorParticipant,
		Term:   network.FirstTerm,
		Roster: []network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}},
		Chain:  network.SuccessionChain{{ID: engine.CoordinatorParticipant, Addr: addr}},
	}, engine.CoordinatorParticipant)
	host.reportPeerLost(2)
	host.ApplyPendingCorrections()
	if !host.authority.Migrating() {
		t.Fatal("losing the authority opened no succession")
	}

	_, _, err := network.DialSession(addr, network.DebugConfig(network.RolePeer, ""))
	if err == nil {
		t.Fatal("a dial that landed mid-succession was admitted")
	}
	if !errors.Is(err, ErrSessionHandoff) {
		t.Fatalf("the refusal is not distinguishable: %v", err)
	}
	// A guest's own port, which takes joins once its guest hosts, refuses the same.
	if err := host.admitHosted(nil); !errors.Is(err, ErrSessionHandoff) {
		t.Fatalf("a guest's port mid-succession refuses with %v; want the handoff refusal", err)
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
		t.Fatal("the first candidate is not authoring")
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
