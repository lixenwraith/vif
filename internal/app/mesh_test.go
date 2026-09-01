package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// meshSession builds n participants on one seed and links them into the given
// topology. Links are the pairs of participant IDs (one-based) that share a stream;
// everything else has to be reached by relay.
func meshSession(t *testing.T, seed uint64, n int, links [][2]int) []*App {
	t.Helper()

	offer := network.SessionOffer{
		Host:              1,
		Assigned:          2,
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

// tickAll advances every participant by one tick, which is the paired boundary the
// barrier's fixed playout lead is measured against.
func tickAll(apps []*App) {
	for _, a := range apps {
		a.Tick(1)
	}
}

// TestChainRelayReachesANonAdjacentParticipant is the mesh criterion. Participant 1
// is linked only to 2, and 3 only to 2: a crossing 1 produces is never sent to 3 by
// its producer. It has to arrive relayed, at the same absolute tick, or the two
// instances hold different shared state.
func TestChainRelayReachesANonAdjacentParticipant(t *testing.T) {
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
	const seed = 0x5EEDBEEF
	steps := soakScale(40, 100, 240)

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
	for step := range steps {
		for i, d := range drivers {
			if !d.Step() {
				t.Fatalf("step %d quit participant %d", step, i+1)
			}
		}
		tickAll(apps)
		assertMeshParity(t, apps, step)
	}
	for i := range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
		assertMeshParity(t, apps, steps+i)
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

// assertMeshParity compares every participant against the first: shared state is
// re-derived, so any two instances that disagree have applied different artifacts.
func assertMeshParity(t *testing.T, apps []*App, step int) {
	t.Helper()
	for i := 1; i < len(apps); i++ {
		x, y := apps[0].SnapshotShared(), apps[i].SnapshotShared()
		if idx, lx, ly, ok := FirstDiff(x, y); ok {
			t.Fatalf("step %d: participants 1 and %d diverged at line %d\n  1: %s\n  %d: %s",
				step, i+1, idx, lx, i+1, ly)
		}
	}
}

func statOf(a *App, key string) (v int64) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Ints.Get(key).Load() })
	return v
}

// TestThreeParticipantLobbyClosesOnOneRoster drives the real startup handshake for a
// lobby larger than a pair. Each joiner dials at a different time and so receives a
// different partial offer; what every instance builds its roster from is the roster
// the start gate closes on, which is the whole reason that gate carries one.
func TestThreeParticipantLobbyClosesOnOneRoster(t *testing.T) {
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	t.Cleanup(host.Close)
	host.cfg.Participants = 3

	hostCfg := network.DebugConfig(network.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = hostParticipantID
	hostCfg.MaxPeers = host.remoteParticipantCount()
	hostCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign: host.assignParticipant, Release: host.releaseParticipant,
	}, 5*time.Second)
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
