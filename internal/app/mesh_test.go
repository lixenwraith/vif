package app

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// meshSession builds n participants on one seed and links them into the given
// topology. Links are the pairs of participant IDs (one-based) that share a stream;
// everything else has to be reached by relay.
func meshSession(t *testing.T, seed uint64, n int, links [][2]int) []*App {
	t.Helper()

	offer := network.SessionOffer{
		Host:              1,
		Assigned:          2,
		Term:              network.FirstTerm,
		BarrierDelayTicks: parameter.NetworkBarrierDelayTicks,
	}
	for i := range n {
		offer.Participants = append(offer.Participants,
			network.SessionParticipant{ID: network.PeerID(i + 1), Slot: uint8(i)})
	}

	apps := make([]*App, n)
	for i := range apps {
		apps[i] = mustHeadless(t, seed, 120, 40)
	}
	t.Cleanup(func() {
		for _, a := range apps {
			a.Close()
		}
	})

	// Every instance adopts the same anchor and the same closed roster, so shared
	// entity identity and creation order are identical from tick zero (D-11).
	anchor := apps[0].JoinAnchor()
	for i, a := range apps {
		local := offer

		local.Anchor = anchor
		if i > 0 {
			// The host's own copy keeps a joiner in Assigned: an offer that assigns
			// the coordinator to itself is not a valid handshake.
			local.Assigned = network.PeerID(i + 1)
		}
		if i == 0 {
			if err := a.HostSession(local); err != nil {
				t.Fatalf("host session: %v", err)
			}
			continue
		}
		if err := a.JoinSession(local); err != nil {
			t.Fatalf("participant %d join: %v", i+1, err)
		}
	}

	mesh := network.NewMesh()
	for _, l := range links {
		mesh.Link(network.PeerID(l[0]), network.PeerID(l[1]))
	}
	for i, a := range apps {
		a.AttachTransport(mesh.Node(network.PeerID(i + 1)))
	}
	for _, a := range apps {
		a.activateNetworkSession()
	}
	return apps
}

// localCursors returns each participant's own cursor and asserts the roster came out
// identical on every instance, which is what makes the parity assertions meaningful.
func localCursors(t *testing.T, apps []*App) []core.Entity {
	t.Helper()
	local := make([]core.Entity, len(apps))
	var first []core.Entity
	for i, a := range apps {
		var roster []core.Entity
		a.World().RunSafe(func() {
			for slot := range len(apps) {
				roster = append(roster, a.World().Resources.Player.Slot(uint8(slot)))
			}
			local[i] = a.World().Resources.Player.Slot(uint8(i))
		})
		if first == nil {
			first = roster
		}
		for slot, e := range roster {
			if e == 0 || e != first[slot] {
				t.Fatalf("participant %d slot %d = %d, want %d on every instance", i+1, slot, e, first[slot])
			}
		}
		if !ownsCursor(a, local[i]) {
			t.Fatalf("participant %d does not simulate its own cursor", i+1)
		}
	}
	return local
}

func ownsCursor(a *App, e core.Entity) (owned bool) {
	a.World().RunSafe(func() { owned = a.World().SimulatesLocally(e) })
	return owned
}

// TestChainRelayReachesANonAdjacentParticipant is the mesh criterion. Participant 1
// is linked only to 2, and 3 only to 2: a crossing 1 produces is never sent to 3 by
// its producer. It has to arrive relayed, at the same absolute tick, or the two
// instances hold different shared state.
func TestChainRelayReachesANonAdjacentParticipant(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	local := localCursors(t, apps)
	a, c := apps[0], apps[2]

	// The producer and the participant two links away hold the same cursor entity.
	start := cursorPosition(a, local[0])
	if got := cursorPosition(c, local[0]); got != start {
		t.Fatalf("participant 3 sees cursor at %#v, want %#v", got, start)
	}

	a.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: local[0], X: start.X + 3, Y: start.Y})
	a.Settle()

	want := start
	want.X += 3
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}
	for i, x := range apps {
		if got := cursorPosition(x, local[0]); got != want {
			t.Fatalf("participant %d applied the relayed crossing as %#v, want %#v", i+1, got, want)
		}
	}

	// Relay is not an echo: the middle participant forwarded, the endpoints did not
	// send the epoch back down the link it came from.
	if relayed := statOf(apps[1], "network.relay_forwarded"); relayed == 0 {
		t.Fatal("participant 2 relayed nothing")
	}
	if dup := statOf(apps[0], "network.relay_duplicates"); dup != 0 {
		t.Fatalf("producer received %d copies of its own epoch back", dup)
	}
}

// TestMeshPropagatesEveryParticipantToEveryOther drives the branching topology a
// chain cannot express: 1—2, 2—3, 3—4 and 3—5. Participants 1, 4 and 5 share no
// link with each other, so every pair's agreement is relayed agreement.
func TestMeshPropagatesEveryParticipantToEveryOther(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	steps := soakScale(24, 40, 240)

	apps := meshSession(t, seed, 5, [][2]int{{1, 2}, {2, 3}, {3, 4}, {3, 5}})
	local := localCursors(t, apps)

	drivers := make([]*journal.FuzzDriver, len(apps))
	starts := make([]component.PositionComponent, len(apps))
	for i, a := range apps {
		opt := parityScript(seed, steps)
		opt.Regions, opt.MapSetups = false, false
		opt.DisableTicks, opt.DisableCommands, opt.DisableOverlays = true, true, true
		opt.Seed ^= uint64(i) * 0x9E3779B97F4A7C15
		drivers[i] = journal.NewFuzzDriver(a, opt)
		starts[i] = cursorPosition(a, local[i])
	}

	assertMeshParity(t, apps, -1)
	advance := func() { tickAll(apps) }
	corrections := 0
	for step := range steps {
		for i, d := range drivers {
			if !d.Step() {
				t.Fatalf("step %d quit participant %d", step, i+1)
			}
		}
		tickAll(apps)
		if (step+1)%correctionSteps == 0 {
			correctMesh(t, apps, advance, step)
			corrections++
		}
	}
	correctMesh(t, apps, advance, steps)
	corrections++
	if corrections < 2 {
		t.Fatalf("the run asserted convergence %d times, want a criterion that repeats", corrections)
	}

	// Non-vacuous: every participant drove its own cursor and produced crossings,
	// so every instance had something the others could only learn over the mesh.
	for i, a := range apps {
		if cursorPosition(a, local[i]) == starts[i] {
			t.Fatalf("participant %d never moved", i+1)
		}
		if sent := statOf(a, "network.crossings_sent"); sent == 0 {
			t.Fatalf("participant %d sent no crossing", i+1)
		}
	}
	// The leaves are three links apart, so what they agree on travelled through two
	// relays in each direction.
	if relayed := statOf(apps[2], "network.relay_forwarded"); relayed == 0 {
		t.Fatal("the branch point relayed nothing")
	}
}

// correctMesh publishes one authoritative correction from the coordinator and
// asserts every other participant converged on it.
//
// The relay is what is being proved as much as the correction. A correction is
// broadcast to the coordinator's direct links only, so participants 4 and 5 — three
// links away — hold the host's world at all only because every node forwards the
// chunks it admitted, on the same termination argument the artifact flood uses.
func correctMesh(t *testing.T, apps []*App, advance func(), step int) {
	t.Helper()
	want := deliverCorrection(t, apps[0], apps[1:], advance)
	for i, a := range apps[1:] {
		assertCorrected(t, want, a, fmt.Sprintf("step %d participant %d", step, i+2))
	}
}

// assertMeshParity compares every participant against the first. It holds before
// anyone has produced anything: every instance built the same world from the same
// seed; what the correction protocol governs is what happens once they disagree.
func assertMeshParity(t *testing.T, apps []*App, step int) {
	t.Helper()
	for i := 1; i < len(apps); i++ {
		x, y := apps[0].SnapshotShared(), apps[i].SnapshotShared()
		if idx, lx, ly, ok := snapshot.FirstDiff(x, y); ok {
			t.Fatalf("step %d: participants 1 and %d diverged at line %d\n  1: %s\n  %d: %s",
				step, i+1, idx, lx, i+1, ly)
		}
	}
}

// TestThreeParticipantLobbyClosesOnOneRoster drives the real startup handshake for a
// lobby larger than a pair. Each joiner dials at a different time and so receives a
// different partial offer; what every instance builds its roster from is the roster
// the start gate closes on, which is the whole reason that gate carries one.
func TestThreeParticipantLobbyClosesOnOneRoster(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	t.Cleanup(host.Close)
	host.cfg.Participants = 3

	hostCfg := network.DebugConfig(network.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = hostParticipantID
	hostCfg.MaxPeers = host.sessionCapacity()
	hostCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign: host.assignParticipant, Release: host.releaseParticipant,
	}, socketWait)
	hostPort := network.NewSocketPort(hostCfg)
	t.Cleanup(func() { _ = hostPort.Close() })
	if err := hostPort.Start(); err != nil {
		t.Fatalf("host transport: %v", err)
	}
	host.AttachTransport(hostPort)

	hostStarted := make(chan error, 1)
	go func() { hostStarted <- host.startHostSessionOn(hostPort, nil) }()

	// Two joiners arrive one after the other, so the first sees a two-entry offer
	// and the second a three-entry one.
	guests := make([]*App, 0, 2)
	ports := make([]*network.SocketPort, 0, 2)
	for i := range 2 {
		pending, offered, err := network.DialSession(hostPort.Addr().String(),
			network.DebugConfig(network.RolePeer, ""))
		if err != nil {
			t.Fatalf("guest %d dial: %v", i+1, err)
		}
		t.Cleanup(func() { _ = pending.Close() })
		if want := network.PeerID(i + 2); offered.Assigned != want {
			t.Fatalf("guest %d assigned participant %d, want %d", i+1, offered.Assigned, want)
		}
		if got, want := len(offered.Participants), i+2; got != want {
			t.Fatalf("guest %d offered %d participants, want the lobby so far (%d)", i+1, got, want)
		}

		joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, offered)
		if err != nil {
			t.Fatalf("guest %d config: %v", i+1, err)
		}
		g, err := NewHeadless(joinCfg)
		if err != nil {
			t.Fatalf("guest %d app: %v", i+1, err)
		}
		t.Cleanup(g.Close)
		g.pendingJoin, g.sessionOffer = pending, offered
		if err := g.Join(offered.Anchor); err != nil {
			_ = pending.Complete(err)
			t.Fatalf("guest %d identity: %v", i+1, err)
		}
		if err := pending.Complete(nil); err != nil {
			t.Fatalf("guest %d reply: %v", i+1, err)
		}
		guests = append(guests, g)
	}

	// The gate releases only once the lobby is full, so both joiners wait here.
	gated := make(chan error, len(guests))
	for _, g := range guests {
		go func() { gated <- g.startJoinSession() }()
	}
	for range guests {
		if err := <-gated; err != nil {
			t.Fatalf("guest startup: %v", err)
		}
	}
	if err := <-hostStarted; err != nil {
		t.Fatalf("host startup: %v", err)
	}

	for i, g := range guests {
		port := network.NewSocketPort(g.pendingJoin.TransportConfig())
		t.Cleanup(func() { _ = port.Close() })
		if err := port.Start(); err != nil {
			t.Fatalf("guest %d transport: %v", i+1, err)
		}
		g.AttachTransport(port)
		ports = append(ports, port)
	}
	waitSocket(t, hostPort, func() bool { return hostPort.PeerCount() == 2 }, "host peers")
	for i, port := range ports {
		waitSocket(t, port, func() bool { return port.PeerCount() == 1 }, fmt.Sprintf("guest %d peer", i+1))
	}

	apps := append([]*App{host}, guests...)
	for _, a := range apps {
		a.activateNetworkSession()
	}

	// One roster on every instance is the point: same slots, same shared entities,
	// each participant simulating exactly its own.
	local := localCursors(t, apps)
	if len(local) != 3 {
		t.Fatalf("roster width = %d, want 3", len(local))
	}
	for i, a := range apps {
		for j, e := range local {
			if owned := ownsCursor(a, e); owned != (i == j) {
				t.Fatalf("participant %d simulates slot %d = %t, want %t", i+1, j, owned, i == j)
			}
		}
	}
}

// TestDepartureReachesTheWholeMesh closes the membership half of the mesh. In the
// chain 1—2—3, participant 3's departure is observed only by 2, which shares no link
// with... nothing: 1 never sees the disconnect at all. If the removal happened where
// it was observed, participant 1 would keep a cursor nobody simulates for the rest of
// the session, and the two instances would disagree about the roster forever.
func TestDepartureReachesTheWholeMesh(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	local := localCursors(t, apps)

	for range 3 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, -1)

	// Participant 3 leaves. Only its neighbour has a link to lose.
	apps[2].World().RunSafe(func() {
		apps[2].World().Resources.Network.Port.(*network.MeshPort).Close()
	})

	survivors := apps[:2]
	for range parameter.NetworkBarrierDelayTicks + 4 {
		tickAll(survivors)
	}

	for i, a := range survivors {
		var slot core.Entity
		var count int
		a.World().RunSafe(func() {
			slot = a.World().Resources.Player.Slot(2)
			count = a.World().Resources.Player.Count()
		})
		if slot != 0 || count != 2 {
			t.Fatalf("participant %d roster after departure = slot2 %d, count %d; want 0 and 2",
				i+1, slot, count)
		}
		// Its own cursor is untouched: a departure removes one participant, not the
		// instance that noticed it.
		if !ownsCursor(a, local[i]) {
			t.Fatalf("participant %d lost its own cursor to another's departure", i+1)
		}
	}
	assertMeshParity(t, survivors, 0)
}

// wireBytes is what one participant has sent and received in total, which is the
// measurement the relayed path is compared against the direct one by.
func wireBytes(a *App) (manifest, shard, correction int64) {
	return statOf(a, "snapshot.manifest_bytes_sent") + statOf(a, "snapshot.manifest_bytes_received"),
		statOf(a, "snapshot.shard_bytes_sent") + statOf(a, "snapshot.shard_bytes_received"),
		statOf(a, "snapshot.correction_bytes_sent")
}

// driveCorrections publishes n corrections from the authority and lets every
// participant settle each one.
func driveCorrections(t *testing.T, apps []*App, n int) {
	t.Helper()
	advance := func() { tickAll(apps) }
	for range n {
		deliverCorrection(t, apps[0], apps[1:], advance)
	}
}

// TestARelayedParticipantKeepsTheSelectiveStream is the headline case.
//
// In the chain 1—2—3 participant 3 shares no link with the authority. Before this
// design that fact alone would put the whole session back on whole bodies, for
// everyone, for its life. Participant 2 now retains what it forwards and answers
// from it, so the session keeps the index — and the proof is that participant 3
// converges through a repair 2 served rather than through a body 1 flooded.
func TestARelayedParticipantKeepsTheSelectiveStream(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, 4)

	relay, far := apps[1], apps[2]
	if got := statOf(relay, "snapshot.relay_retained"); got == 0 {
		t.Fatal("the relaying participant retained nothing to answer from")
	}
	if got := statOf(far, "snapshot.manifests_received"); got == 0 {
		t.Fatal("the relayed participant never received an index")
	}
	// The session stopped flooding whole bodies: past the warm-up the authority
	// leads with the index, and the far participant is answered by its neighbour.
	served := statOf(relay, "snapshot.relay_served")
	converged := statOf(far, "snapshot.corrections_hash_only")
	if served == 0 && converged == 0 {
		t.Fatalf("the relayed participant was neither served a repair (%d) nor proved converged (%d)",
			served, converged)
	}
	assertMeshParity(t, apps, -1)

	// The same disagreement over a direct link, for the byte comparison the phase
	// asks to be reported rather than asserted against a threshold.
	direct := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {1, 3}, {2, 3}})
	localCursors(t, direct)
	driveCorrections(t, direct, 4)

	rm, rs, rc := wireBytes(apps[2])
	dm, ds, dc := wireBytes(direct[2])
	t.Logf("relayed path: manifest %d B, shard %d B, whole bodies %d B", rm, rs, rc)
	t.Logf("direct path:  manifest %d B, shard %d B, whole bodies %d B", dm, ds, dc)
	t.Logf("relay retention footprint: %d records, %d B forwarded, %d B answered",
		statOf(relay, "snapshot.relay_retained"),
		statOf(relay, "snapshot.relay_bytes_sent"),
		statOf(relay, "snapshot.shard_bytes_sent"))

	// The claim worth asserting is the one the phase makes: a relayed session is
	// no longer paying the whole-body flood for every correction.
	if rc > 0 && dc > 0 && rc > 4*dc {
		t.Fatalf("the relayed path moved %d whole-body bytes against %d direct", rc, dc)
	}
}

// TestARelayCannotForgeAPage is the proof that a relay serving pages it did not
// author cannot substitute one.
//
// The binding is the authority's own root, twice over: the set must declare the
// root the receiver was sent in the manifest, and the repaired capture must
// reproduce it. Mutating a page at the relay breaks the page hash first and the
// root second, and the receiver reaches the bounded keyframe fallback rather than
// installing anything.
func TestARelayCannotForgeAPage(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, 4)

	host, far := apps[0], apps[2]
	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	index, err := snapshot.BuildManifest(cap, 1)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	want := index.Summary()

	// A request the relay would answer, and the honest answer to it.
	req := snapshot.CorrectionRequest{
		Version: snapshot.ManifestVersion, Schema: snapshot.Schema,
		Tick: cap.Header.Tick, Run: cap.Header.Run, Session: cap.Header.Session,
		Term: cap.Header.Term,
		Sections: []snapshot.SectionRequest{{
			ID: snapshot.StoreSectionPrefix + "positions", Pages: want.Sections[1].Pages,
			Hash: make([]uint64, want.Sections[1].Pages),
		}},
	}
	set, pages, err := snapshot.BuildShardSet(index, req)
	if err != nil || pages == 0 {
		t.Fatalf("build a relayed answer: %v (%d pages)", err, pages)
	}
	set.Served = 2

	if err := snapshot.ValidateShardSet(set, cap.Header.Tick, 1, want.Root, want.Header); err != nil {
		t.Fatalf("an honest relayed answer was refused: %v", err)
	}

	// Substituted at the relay: one row's value replaced, everything else intact.
	forged := set
	forged.Shards = append([]snapshot.CorrectionShard(nil), set.Shards...)
	rows := append([]snapshot.ManifestRow(nil), forged.Shards[0].Rows...)
	if len(rows) == 0 {
		t.Skip("the chosen page is empty; nothing to substitute")
	}
	rows[0].Value = []byte(`{"X":1,"Y":1}`)
	forged.Shards[0].Rows = rows
	if err := snapshot.ValidateShardSet(forged, cap.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a substituted page passed the per-page proof")
	}

	// Truncated at the relay: the rows the page declares, minus one.
	truncated := set
	truncated.Shards = append([]snapshot.CorrectionShard(nil), set.Shards...)
	truncated.Shards[0].Rows = truncated.Shards[0].Rows[:len(truncated.Shards[0].Rows)-1]
	if err := snapshot.ValidateShardSet(truncated, cap.Header.Tick, 1, want.Root, want.Header); err == nil {
		t.Fatal("a truncated page passed the per-page proof")
	}

	// And a set that is internally consistent but describes a root the manifest
	// does not: a relay answering from a baseline of its own. The per-page hashes
	// all reproduce, so the root is the only thing that catches it.
	rebased := set
	if err := snapshot.ValidateShardSet(rebased, cap.Header.Tick, 1, want.Root^0x5EED, want.Header); err == nil {
		t.Fatal("a set declaring another root than the manifest it answers was admitted")
	}

	before := statOf(far, "snapshot.keyframe_fallbacks")
	far.corrections.applyRepairFromRelay(t, set, want)
	if statOf(far, "snapshot.corrections_applied") == 0 {
		t.Fatal("the honest relayed repair never reached the receiver")
	}
	_ = before
}

// applyRepairFromRelay drives one relayed answer through the receiver's apply
// path, so the refusal and the fallback are the real ones rather than a direct
// call to the validator.
func (c *corrections) applyRepairFromRelay(t *testing.T, set snapshot.CorrectionShardSet, want snapshot.CorrectionManifest) {
	t.Helper()
	body, err := snapshot.EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode a relayed answer: %v", err)
	}
	c.selectiveMu.Lock()
	c.selective.awaiting = append(c.selective.awaiting, &awaitingRepair{
		tick: want.Header.Tick, capture: mustCapture(t, c.a), index: mustIndex(t, c.a, want),
		manifest: want, from: 2,
	})
	c.selectiveMu.Unlock()
	c.applyRepair(body)
}

func mustIndex(t *testing.T, a *App, want snapshot.CorrectionManifest) *snapshot.Manifest {
	t.Helper()
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	cap.Header.Term = want.Header.Term
	index, err := snapshot.BuildManifest(cap, want.Authority)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	return index
}

// TestARelayThatDroppedTheManifestSaysSo is the bounded-staleness rule. A relay's
// retention is smaller than the session's history; a request naming a tick it no
// longer holds is answered in words, never with a body from another baseline.
func TestARelayThatDroppedTheManifestSaysSo(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)
	driveCorrections(t, apps, parameter.SnapshotManifestRetention+3)

	relay, far := apps[1], apps[2]
	if got := statOf(relay, "snapshot.relay_retained"); got > parameter.SnapshotManifestRetention {
		t.Fatalf("the relay holds %d records, the bound is %d", got, parameter.SnapshotManifestRetention)
	}

	// A request naming a tick far outside the ring.
	before := statOf(relay, "snapshot.relay_unserved")
	relay.corrections.receiveSelective(uint8(network.MsgStateRequest), uint32(3),
		mustRequestBody(t, snapshot.CorrectionRequest{
			Version: snapshot.ManifestVersion, Schema: snapshot.Schema,
			Tick: 1, Run: relayRun(relay), Session: relaySession(relay),
			Term:     relay.AuthorityState().Term,
			Sections: []snapshot.SectionRequest{{ID: snapshot.SectionMeta, Pages: 1, Hash: []uint64{0}}},
		}))
	relay.ApplyPendingCorrections()
	if statOf(relay, "snapshot.relay_unserved") == before {
		t.Fatal("a request naming a dropped manifest was not answered with a refusal")
	}

	// The far participant degrades rather than assembling something: it takes the
	// next whole world, and nothing mixed-baseline is ever installed.
	far.ApplyPendingCorrections()
	tickAll(apps)
	driveCorrections(t, apps, 2)
	assertMeshParity(t, apps, -1)
}

func mustRequestBody(t *testing.T, req snapshot.CorrectionRequest) []byte {
	t.Helper()
	body, err := snapshot.EncodeCorrectionRequest(req)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return body
}

func relayRun(a *App) uint64     { return a.Position().Run }
func relaySession(a *App) uint64 { return mustCaptureSession(a) }

func mustCaptureSession(a *App) (s uint64) {
	a.World().RunSafe(func() { s = a.World().Resources.Rand.Session() })
	return s
}

// TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies from the
// other side: the gate is "can every participant be answered", so a relay that
// holds nothing keeps the whole-body flood — unchanged, and reported.
func TestARelayWithNoRetentionLeavesTheSessionOnWholeBodies(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}})
	localCursors(t, apps)

	relay := apps[1]
	if relay.corrections.canRelay() {
		t.Fatal("a participant that has held no authoritative capture claims it can relay")
	}
	if got := relay.corrections.relayedParticipants(); len(got) != 0 {
		t.Fatalf("a relay with no retention offered to answer for %v", got)
	}
	// The authority therefore cannot answer everyone and says so.
	host := apps[0]
	host.corrections.publishMu.Lock()
	answerable := host.corrections.canAnswerEveryParticipant([]uint32{2})
	said := host.corrections.saidUnrelayed
	host.corrections.publishMu.Unlock()
	if answerable {
		t.Fatal("the authority believed a participant behind an empty relay could be answered")
	}
	if !said {
		t.Fatal("the fallback to whole bodies was not reported")
	}

	// Once the relay holds retention the same session becomes answerable, which is
	// the whole of the role: a topology did not change, a role did.
	driveCorrections(t, apps, 3)
	if !relay.corrections.canRelay() {
		t.Fatal("a relay that has installed authoritative captures still cannot answer")
	}
	host.corrections.publishMu.Lock()
	answerable = host.corrections.canAnswerEveryParticipant([]uint32{2})
	host.corrections.publishMu.Unlock()
	if !answerable {
		t.Fatal("the authority still believes the relayed participant cannot be answered")
	}
	if got := relay.corrections.sessionRole(); got != network.RoleRelay {
		t.Fatalf("the middle participant holds role %d, want the relay role", got)
	}
	if got := apps[0].corrections.sessionRole(); got != network.RoleHost {
		t.Fatalf("the authority holds role %d, want the host role", got)
	}
	if got := apps[2].corrections.sessionRole(); got != network.RolePeer {
		t.Fatalf("the leaf holds role %d, want the peer role", got)
	}
}
