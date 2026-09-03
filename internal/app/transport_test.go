package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/journal"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// pair builds two joined participants on one seed, linked by an in-process
// transport. Each spawns its own cursor in its own slot and mirrors the other's as
// a remote, which is the roster a real join produces.
//
// The two terminals are deliberately unequal. Two participants of one size share
// every viewport-derived value by accident, so a criterion built on them cannot see
// a shared value that was derived from the local terminal — which is exactly the
// divergence a second window, a tmux pane or a resize produces.
func pair(t *testing.T, seed uint64, steps int) (*App, *App) {
	t.Helper()

	a := mustHeadless(t, seed, 120, 40)
	an := a.JoinAnchor()
	b := mustJoiner(t, seed, 84, 26, an)
	t.Cleanup(func() { a.Close(); b.Close() })

	if err := b.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	// The host runs the same level setup its guest adopted, so the two see one
	// event sequence; a join that only moved the guest would be the divergence.
	a.adoptMapLatch(an.Anchor)

	pa, pb := network.NewLoopbackPair(1, 2)
	a.AttachTransport(pa)
	b.AttachTransport(pb)

	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
		x.Tick(1)
	}
	return a, b
}

// liveScript is the action set a two-participant criterion drives. The harness owns
// the clock and holds the operator mutations no artifact carries fixed: FSM regions,
// the programmatic level setup, commands and the overlay round trip.
//
// Resizes and viewport-relative motions are deliberately not among them. Each
// participant drives its own terminal and its own camera, so a resize has to reflow
// this instance's view without touching shared state and a screen-relative motion
// has to resolve locally and cross as the absolute cell it selected — which is
// exactly what they failed to do, and what no parity criterion could see while every
// one of them held both fixed.
func liveScript(seed uint64, steps int) journal.FuzzOptions {
	opt := parityScript(seed, steps)
	opt.Regions, opt.MapSetups = false, false
	opt.DisableTicks, opt.DisableCommands, opt.DisableOverlays = true, true, true
	opt.Resizes, opt.MapMotionsOnly = true, false
	return opt
}

// mirrorCursors splits ownership of a two-slot roster. Both instances run the
// same spawn requests, so the two rosters hold the same shared entities in the same
// slots (D-11); what differs is Control and the local binding, which is the whole
// of what D-2 keys on. a drives slot 0 and mirrors slot 1; b is its inverse.
//
// Both cursors are stamped with the participant identity that owns them — pair
// links the two through NewLoopbackPair(1, 2), so slot 0 is participant 1 and slot
// 1 is participant 2. That is not decoration. An install re-derives control from
// the identity rather than adopting the capture's answer (D-13), so a roster whose
// PeerIDs name nobody leaves *every* cursor ControlRemote on the guest at its first
// correction: the guest stops simulating its own cursor, and every criterion that
// drives it past a correction quietly proves nothing after the first one.
func mirrorCursors(t *testing.T, a, b *App) (localA, remoteA core.Entity) {
	t.Helper()

	for _, x := range []*App{a, b} {
		x.Context().PushEventOrigin(event.EventCursorSpawnRequest,
			&event.CursorSpawnRequestPayload{
				Slot: 1, X: 20, Y: 10,
				Control: uint8(component.ControlRemote), PeerID: 2,
			}, event.OriginDebug)
		x.Settle()
	}
	a.World().RunSafe(func() {
		w := a.World()
		if c, ok := w.Components.Cursor.GetPtr(w.Resources.Player.Slot(0)); ok {
			c.Control, c.PeerID = component.ControlHuman, 1
		}
	})

	// b owns the second cursor and mirrors the first
	b.World().RunSafe(func() {
		w := b.World()
		if c, ok := w.Components.Cursor.GetPtr(w.Resources.Player.Slot(0)); ok {
			c.Control, c.PeerID = component.ControlRemote, 1
		}
		if c, ok := w.Components.Cursor.GetPtr(w.Resources.Player.Slot(1)); ok {
			c.Control, c.PeerID = component.ControlHuman, 2
		}
	})
	b.Context().PushEventOrigin(event.EventCursorSetLocalRequest,
		&event.CursorSetLocalPayload{Slot: 1}, event.OriginDebug)
	b.Settle()

	a.World().RunSafe(func() {
		localA = a.World().Resources.Player.Slot(0)
		remoteA = a.World().Resources.Player.Slot(1)
	})
	if localA == 0 || remoteA == 0 {
		t.Fatalf("instance a roster = (%d, %d), want a local and a remote cursor", localA, remoteA)
	}
	return localA, remoteA
}

// TestTransportSyncsOwnerAuthoredCursorState is the D-13 transport: the owner writes,
// the peer receives, and the peer's own systems never author the same cell.
func TestTransportSyncsOwnerAuthoredCursorState(t *testing.T) {
	t.Parallel()
	a, b := pair(t, 0x5EEDBEEF, 0)
	_, remoteOnA := mirrorCursors(t, a, b)

	// Give b's own cursor a value only b can produce. The ember decay clock is
	// pushed out so the owner holds the value still while a drains it: this asserts
	// the transport, not the sync interval.
	var ownedOnB core.Entity
	b.World().RunSafe(func() {
		ownedOnB = b.World().Resources.Player.Slot(1)
		h, _ := b.World().Components.Heat.GetPtr(ownedOnB)
		h.Current, h.EmberActive = 55, true
		h.EmberDecayTime = b.World().Resources.Time.GameTime.Add(time.Hour)
		sh, _ := b.World().Components.Shield.GetPtr(ownedOnB)
		sh.Active, sh.InvRxSq, sh.InvRySq = true, 0.25, 1.0
	})

	b.Tick(parameter.NetworkSyncTicks)
	a.Tick(parameter.NetworkSyncTicks)

	var mirrored, owned component.HeatComponent
	var mirrorShield, ownedShield component.ShieldComponent
	a.World().RunSafe(func() {
		mirrored, _ = a.World().Components.Heat.GetComponent(remoteOnA)
		mirrorShield, _ = a.World().Components.Shield.GetComponent(remoteOnA)
	})
	b.World().RunSafe(func() {
		owned, _ = b.World().Components.Heat.GetComponent(ownedOnB)
		ownedShield, _ = b.World().Components.Shield.GetComponent(ownedOnB)
	})

	// Heat and the shield ellipse are mirrored for the remote cursor's presentation
	// and owner-local interactions; they no longer determine a shared outcome.
	if mirrored.Current != owned.Current || mirrored.Current != 55 || !mirrored.EmberActive {
		t.Fatalf("remote heat on a = %d, owner holds %d; ember %t",
			mirrored.Current, owned.Current, mirrored.EmberActive)
	}
	if mirrorShield.InvRxSq != 0.25 || mirrorShield.InvRySq != 1.0 ||
		mirrorShield.Active != ownedShield.Active {
		t.Fatalf("remote shield on a = %#v, owner holds %#v", mirrorShield, ownedShield)
	}

	// The receiving instance applied at least one owner-authored snapshot.
	var applied int64
	a.World().RunSafe(func() {
		applied = a.World().Resources.Status.Ints.Get("network.state_applied").Load()
	})
	if applied == 0 {
		t.Fatal("instance a applied no cursor state")
	}
}

// TestTransportCarriesCrossingsWithoutEcho is the D-3 wire rule: a D-3 artifact
// reaches the peer, and the peer does not send it back.
func TestTransportCarriesCrossingsWithoutEcho(t *testing.T) {
	t.Parallel()
	a, b := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, a, b)

	// A crossing with no local producer needed: the post-typing cursor advance
	var target core.Entity
	a.World().RunSafe(func() { target = a.World().Resources.Player.Slot(0) })
	a.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: target, X: 31, Y: 12})
	a.Settle()
	assertPosition := func(x *App, want bool) {
		t.Helper()
		var pos component.PositionComponent
		x.World().RunSafe(func() { pos, _ = x.World().Positions.GetPosition(target) })
		if got := pos.X == 31 && pos.Y == 12; got != want {
			t.Fatalf("cursor on participant = (%d, %d), target applied = %t, want %t", pos.X, pos.Y, got, want)
		}
	}
	// The producer applies its own artifact at once; the peer keeps
	// the playout lead, which is the interpolation buffer for remote action.
	assertPosition(a, true)
	assertPosition(b, false)
	for range parameter.NetworkBarrierDelayTicks {
		a.Tick(1)
		b.Tick(1)
		assertPosition(a, true)
	}
	a.Tick(1)
	b.Tick(1)

	var sentA, recvB, sentB int64
	a.World().RunSafe(func() {
		reg := a.World().Resources.Status
		sentA = reg.Ints.Get("network.crossings_sent").Load()
	})
	b.World().RunSafe(func() {
		reg := b.World().Resources.Status
		recvB = reg.Ints.Get("network.crossings_received").Load()
		sentB = reg.Ints.Get("network.crossings_sent").Load()
	})
	if sentA == 0 || recvB == 0 {
		t.Fatalf("crossings sent by a = %d, received by b = %d, want both non-zero", sentA, recvB)
	}
	if sentB != 0 {
		t.Fatalf("instance b sent %d crossings, want 0: a received artifact must not echo", sentB)
	}

	var pos component.PositionComponent
	b.World().RunSafe(func() { pos, _ = b.World().Positions.GetPosition(target) })
	if pos.X != 31 || pos.Y != 12 {
		t.Fatalf("cursor on b = (%d, %d), want the crossing's (31, 12)", pos.X, pos.Y)
	}
}

func TestBarrierIsNoOpWithoutPeer(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)

	var cursor core.Entity
	a.World().RunSafe(func() { cursor = a.World().Resources.Player.Slot(0) })
	a.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: cursor, X: 17, Y: 9})
	a.Settle()

	var pos component.PositionComponent
	var deferred, lag int64
	a.World().RunSafe(func() {
		pos, _ = a.World().Positions.GetPosition(cursor)
		reg := a.World().Resources.Status
		deferred = reg.Ints.Get("network.barrier_deferred").Load()
		lag = reg.Ints.Get("network.barrier_peer_lag_ticks").Load()
	})
	if pos.X != 17 || pos.Y != 9 {
		t.Fatalf("no-peer crossing remained deferred at (%d, %d)", pos.X, pos.Y)
	}
	if deferred != 0 || lag != 0 {
		t.Fatalf("no-peer barrier telemetry = (deferred %d, lag %d), want zero", deferred, lag)
	}
}

// TestObserverSharedStateTracksTheLiveParticipant proves the correction with
// one-way traffic: only one participant produces anything, so everything the
// observer holds arrived from the authority — its own artifacts a playout lead
// after the producer applied them, and its whole world at every correction.
func TestObserverSharedStateTracksTheLiveParticipant(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	steps := soakScale(60, 120, 1200)

	live, observer := pair(t, seed, steps)
	observeOnly(t, observer)

	assertSharedParity(t, live, observer, -1)

	// Only the live participant is scripted, so the two operator actions that
	// rewrite shared state on the instance that runs them — an FSM region op and a
	// level setup — have no counterpart on the observer.
	opt := parityScript(seed, steps)
	opt.Regions = false
	opt.MapSetups = false
	d := journal.NewFuzzDriver(live, opt)
	corrections := 0
	step := func() {
		live.Tick(1)
		observer.Tick(1)
	}
	for i := range steps {
		before := live.Position().Tick
		if !d.Step() {
			t.Fatalf("step %d quit the live participant", i)
		}
		// The observer runs the same clock, then drains what the step produced
		if n := int(live.Position().Tick - before); n > 0 {
			observer.Tick(n)
		}
		step()
		if (i+1)%correctionSteps == 0 {
			assertCorrected(t, deliverCorrection(t, live, []*App{observer}, step), observer,
				fmt.Sprintf("step %d observer", i))
			corrections++
		}
	}
	if corrections == 0 {
		t.Fatal("the run asserted convergence never")
	}
}

// TestTwoLiveParticipantsConvergeOnCorrections is the headless two-participant
// criterion. Weakened D-11 says two
// participants no longer agree at every tick, because each applies its own
// artifacts a playout lead before the other sees them, and what is asserted instead
// is that every correction closes the gap exactly.
func TestTwoLiveParticipantsConvergeOnCorrections(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	steps := soakScale(60, 120, 1200)

	a, b := pair(t, seed, steps)
	localA, _ := mirrorCursors(t, a, b)
	var localB core.Entity
	b.World().RunSafe(func() { localB = b.World().Resources.Player.Slot(1) })
	proveTwoLive(t, a, b, localA, localB, liveScript(seed, steps), func() {
		a.Tick(1)
		b.Tick(1)
	})
}

// TestActivatedSessionDefersCrossingBeforeFirstTick closes the lobby/input gap: an
// artifact produced after the session is activated and before the first tick still
// reaches its peer at the agreed apply tick, rather than falling into the window
// between the two. The producer applies its own copy at once; the gap this test
// exists for is the peer's.
func TestActivatedSessionDefersCrossingBeforeFirstTick(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	a := mustHeadless(t, seed, 120, 40)
	b := mustHeadless(t, seed, 120, 40)
	t.Cleanup(func() { a.Close(); b.Close() })

	offer, err := a.hostOffer()
	if err != nil {
		t.Fatalf("host offer: %v", err)
	}
	if err := b.JoinSession(offer); err != nil {
		t.Fatalf("join roster: %v", err)
	}
	if err := a.HostSession(offer); err != nil {
		t.Fatalf("host roster: %v", err)
	}
	pa, pb := network.NewLoopbackPair(offer.Host, offer.Assigned)
	a.AttachTransport(pa)
	b.AttachTransport(pb)
	a.activateNetworkSession()
	b.activateNetworkSession()

	var target core.Entity
	a.World().RunSafe(func() { target = a.World().Resources.Player.Slot(0) })
	start := cursorPosition(a, target)
	a.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: target, X: start.X + 1, Y: start.Y})
	a.Settle()
	want := start
	want.X++
	if got := cursorPosition(a, target); got != want {
		t.Fatalf("host held its own pre-tick crossing behind the lead: %#v", got)
	}
	if got := cursorPosition(b, target); got != start {
		t.Fatalf("joiner applied a pre-tick crossing before its apply tick: %#v", got)
	}

	for range parameter.NetworkBarrierDelayTicks + 1 {
		a.Tick(1)
		b.Tick(1)
	}
	if got := cursorPosition(a, target); got != want {
		t.Fatalf("host crossing position = %#v, want %#v", got, want)
	}
	if got := cursorPosition(b, target); got != want {
		t.Fatalf("joiner crossing position = %#v, want %#v", got, want)
	}
}

// TestTwoLiveParticipantsConvergeOverTCP proves the same session through stream
// framing, the anchor handshake and canonical socket participant IDs — including
// the correction, which is chunked and reassembled off a real socket rather than
// handed across in one piece.
//
// It no longer ends in a mid-run join. That leg exercised the retired
// replay-the-session-from-tick-zero path; the authoritative snapshot join that
// replaces it is the next implementation's to prove.
func TestTwoLiveParticipantsConvergeOverTCP(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x5EEDBEEF
	// The socket leg re-proves the same criterion through framing and a real
	// handshake, neither of which needs the long run the in-process one takes.
	steps := soakScale(60, 120, 800)

	a, err := NewHeadless(Config{
		Seed: seed, Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
	})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	t.Cleanup(a.Close)
	// The lobby is not closed here: hostOffer closes it, and the coordinator has to
	// allocate this run's guest identity from an open one.
	hostCfg := network.DebugConfig(network.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = hostParticipantID
	hostCfg.AcceptSession = network.HostAcceptor(network.Coordinator{
		Assign: a.assignParticipant, Release: a.releaseParticipant,
	}, socketWait)
	host := network.NewSocketPort(hostCfg)
	t.Cleanup(func() { _ = host.Close() })
	if err := host.Start(); err != nil {
		t.Fatalf("host transport: %v", err)
	}
	a.AttachTransport(host)
	hostStarted := make(chan error, 1)
	go func() { hostStarted <- a.startHostSessionOn(host, nil) }()

	pending, offered, err := network.DialSession(host.Addr().String(), network.DebugConfig(network.RolePeer, ""))
	if err != nil {
		t.Fatalf("dial session: %v", err)
	}
	t.Cleanup(func() { _ = pending.Close() })
	joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, offered)
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	b, err := NewHeadless(joinCfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	t.Cleanup(b.Close)
	b.pendingJoin = pending
	b.sessionOffer = offered
	if err := b.Join(offered.Anchor); err != nil {
		_ = pending.Complete(err)
		t.Fatalf("join identity: %v", err)
	}
	if err := pending.Complete(nil); err != nil {
		t.Fatalf("join reply: %v", err)
	}
	if err := b.startJoinSession(); err != nil {
		t.Fatalf("join startup: %v", err)
	}

	guest := network.NewSocketPort(pending.TransportConfig())
	t.Cleanup(func() { _ = guest.Close() })
	if err := guest.Start(); err != nil {
		t.Fatalf("guest transport: %v", err)
	}
	b.AttachTransport(guest)
	waitSocket(t, guest, func() bool { return guest.PeerCount() == 1 }, "guest peer")
	if err := <-hostStarted; err != nil {
		t.Fatalf("host startup: %v", err)
	}
	a.activateNetworkSession()
	b.activateNetworkSession()

	var localA, localB core.Entity
	a.World().RunSafe(func() { localA = a.World().Resources.Player.Slot(0) })
	b.World().RunSafe(func() { localB = b.World().Resources.Player.Slot(1) })
	proveTwoLive(t, a, b, localA, localB, liveScript(seed, steps), func() {
		recvA, recvB := host.Received(), guest.Received()
		a.Tick(1)
		b.Tick(1)
		waitSocket(t, host, func() bool { return host.Received() > recvA }, "host epoch")
		waitSocket(t, guest, func() bool { return guest.Received() > recvB }, "guest epoch")
	})

	if err := guest.Close(); err != nil {
		t.Fatalf("guest disconnect: %v", err)
	}
	waitSocket(t, host, func() bool { return host.PeerCount() == 0 }, "host disconnect")
	// A departure is a crossing like any other, so it lands at the playout lead the
	// session runs on rather than where the lost link was observed. The barrier owns
	// it even though there is no peer left to send it to: the tick it applies at is
	// what a reproduction of this run has to reach.
	a.Tick(parameter.NetworkBarrierDelayTicks + 1)
	var roster int
	var state string
	var peers int64
	var latched bool
	a.World().RunSafe(func() {
		roster = a.World().Resources.Player.Count()
		reg := a.World().Resources.Status
		state = reg.Strings.Get("network.state").Load()
		peers = reg.Ints.Get("network.peers").Load()
		latched = reg.Bools.Get("network.map_latched").Load()
	})
	// The latch survives the disconnect: a run that opened a session keeps the bounds
	// its participants adopted, so a returning one replays onto the same map (D-14).
	if roster != 1 || !host.IsRunning() || state != "down" || peers != 0 || !latched {
		t.Fatalf("host after disconnect = roster %d running %t state %q peers %d latch %t",
			roster, host.IsRunning(), state, peers, latched)
	}

}

// proveTwoLive drives two live participants and asserts the criterion that
// replaced lockstep with: the guest is equal to the host as of every correction.
//
// Between corrections the two are *expected* to disagree — each applies its own
// artifacts a playout lead before the other does — so asserting parity per tick
// would now be asserting the thing this phase removed. What has to stay true is
// that each participant is really driving something, which the moved/sent/apm
// checks below are for, and that the disagreement is closed rather than tolerated.
func proveTwoLive(t *testing.T, a, b *App, localA, localB core.Entity, optA journal.FuzzOptions, tickPair func()) {
	t.Helper()
	steps := optA.Steps
	assertSharedParity(t, a, b, -1)

	optB := optA
	optB.Seed ^= 0x9E3779B97F4A7C15
	da, db := journal.NewFuzzDriver(a, optA), journal.NewFuzzDriver(b, optB)

	startA, startB := cursorPosition(a, localA), cursorPosition(b, localB)
	movedA, movedB := false, false
	corrections := 0
	for i := range steps {
		if !da.Step() {
			t.Fatalf("step %d quit participant a", i)
		}
		if !db.Step() {
			t.Fatalf("step %d quit participant b", i)
		}
		tickPair()
		movedA = movedA || cursorPosition(a, localA) != startA
		movedB = movedB || cursorPosition(b, localB) != startB
		if (i+1)%correctionSteps == 0 {
			assertCorrected(t, deliverCorrection(t, a, []*App{b}, tickPair), b, fmt.Sprintf("step %d guest", i))
			corrections++
		}
	}
	assertCorrected(t, deliverCorrection(t, a, []*App{b}, tickPair), b, "final guest")
	corrections++
	if corrections < 2 {
		t.Fatalf("the run asserted convergence %d times, want a criterion that repeats", corrections)
	}

	var sentA, sentB int64
	var apmA, apmB uint64
	a.World().RunSafe(func() {
		sentA = a.World().Resources.Status.Ints.Get("network.crossings_sent").Load()
		apmA = a.World().Resources.Game.State.GetAPM()
	})
	b.World().RunSafe(func() {
		sentB = b.World().Resources.Status.Ints.Get("network.crossings_sent").Load()
		apmB = b.World().Resources.Game.State.GetAPM()
	})
	if !movedA || !movedB || sentA == 0 || sentB == 0 || apmA == 0 || apmB == 0 {
		t.Fatalf("live proof = moved(%t,%t) sent(%d,%d) apm(%d,%d), want both active",
			movedA, movedB, sentA, sentB, apmA, apmB)
	}
}

// socketWait is how long a socket handshake step may take. It is generous on
// purpose: the suite runs its parallel tests on every core, so a loopback round
// trip competes with the race detector rather than with the network.
const socketWait = 15 * time.Second

func waitSocket(t *testing.T, port *network.SocketPort, ready func() bool, what string) {
	t.Helper()
	deadline := time.NewTimer(socketWait)
	defer deadline.Stop()
	for !ready() {
		select {
		case err := <-port.Errors():
			t.Fatalf("%s: %v", what, err)
		case <-port.Changes():
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// observeOnly marks every cursor on an instance remote and removes player-domain
// work already spawned during boot, so it produces no crossings of its own and its
// shared state is whatever the wire delivers.
func observeOnly(t *testing.T, a *App) {
	t.Helper()
	var owned int
	a.World().RunSafe(func() {
		w := a.World()
		w.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
			c.Control, c.PeerID = component.ControlRemote, 1
			owned++
			return true
		})
		var player []core.Entity
		for _, e := range w.Positions.Entities() {
			if e.Domain() == core.DomainPlayer {
				player = append(player, e)
			}
		}
		w.DestroyEntitiesBatch(player)
		w.Resources.Player.Entity = 0
	})
	if owned == 0 {
		t.Fatal("observer has no cursor to mirror")
	}
}

// TestWireEncoding covers the wire boundary without a session: which events travel
// at all, and whether the codec preserves what the receiver pushes.
func TestWireEncoding(t *testing.T) {
	t.Parallel()
	event.EnsureRegistry()
	t.Run("set", testWireSet)
	t.Run("frame", testWireFrameRoundTrip)
	t.Run("cursor state", testCursorStateRoundTrip)
}

// testWireSet asserts the predicate rather than the pipe: a Shared event is
// re-derived on both sides and must never travel, a chain follow-up is derived from
// the root that did (D-5), and an arriving artifact is never echoed.
func testWireSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   event.GameEvent
		want bool
	}{
		{"bus crossing", event.GameEvent{
			Type: event.EventCursorMoveRequest, Domain: core.DomainPlayer,
			Payload: &event.CursorMoveRequestPayload{}}, true},
		{"shared is compared, not sent", event.GameEvent{
			Type: event.EventCursorMoved, Domain: core.DomainShared,
			Payload: &event.CursorMovedPayload{}}, false},
		{"local", event.GameEvent{
			Type: event.EventEnergyAddRequest, Domain: core.DomainPlayer,
			Payload: &event.EnergyAddPayload{}}, false},
		{"stamped crossing at a shared target", event.GameEvent{
			Type: event.EventCombatAttackDirectRequest, Domain: core.DomainShared,
			Payload: &event.CombatAttackDirectRequestPayload{}}, true},
		{"stamped hit on a player target", event.GameEvent{
			Type: event.EventCombatAttackDirectRequest, Domain: core.DomainPlayer,
			Payload: &event.CombatAttackDirectRequestPayload{}}, false},
		{"derived chain follow-up", event.GameEvent{
			Type: event.EventCombatAttackDirectRequest, Domain: core.DomainShared,
			Payload: &event.CombatAttackDirectRequestPayload{ChainDepth: 1}}, false},
		{"stamped death batch is re-derived", event.GameEvent{
			Type: event.EventDeathBatch, Domain: core.DomainShared}, false},
		{"an arriving artifact is not echoed", event.GameEvent{
			Type: event.EventCursorMoveRequest, Domain: core.DomainPlayer,
			Origin: event.OriginNetwork, Payload: &event.CursorMoveRequestPayload{}}, false},
	} {
		if got := event.OnWire(tc.ev); got != tc.want {
			t.Errorf("OnWire(%s) = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// testWireFrameRoundTrip asserts the codec preserves the event type, the
// producer's domain, and every payload field.
func testWireFrameRoundTrip(t *testing.T) {
	want := &event.CombatAttackDirectRequestPayload{
		OwnerEntity:  core.MakeEntity(core.DomainShared, 3),
		TargetEntity: core.MakeEntity(core.DomainShared, 9),
		HitEntity:    core.MakeEntity(core.DomainShared, 9),
		OriginX:      4, OriginY: 7, HasOrigin: true,
	}
	frame, encErr := event.NewWireFrame(event.GameEvent{
		Type: event.EventCombatAttackDirectRequest, Domain: core.DomainShared, Payload: want, Seq: 11,
	})
	if encErr != "" {
		t.Fatalf("encode: %s", encErr)
	}

	body, err := event.EncodeFrames([]event.WireFrame{frame})
	if err != nil {
		t.Fatalf("encode frames: %v", err)
	}
	frames, err := event.DecodeFrames(body)
	if err != nil || len(frames) != 1 {
		t.Fatalf("decode frames = (%d, %v), want one frame", len(frames), err)
	}

	et, payload, domain, err := frames[0].Decode()
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if et != event.EventCombatAttackDirectRequest || domain != core.DomainShared {
		t.Fatalf("decoded = (%v, %v), want the direct attack in the shared domain", et, domain)
	}
	got, ok := payload.(*event.CombatAttackDirectRequestPayload)
	if !ok || *got != *want {
		t.Fatalf("payload round trip = %#v, want %#v", payload, want)
	}
}

// testCursorStateRoundTrip covers the D-13 value transfer through the same codec,
// including the slices the fixed-size weapon arrays flatten into.
func testCursorStateRoundTrip(t *testing.T) {
	want := &event.CursorStatePayload{
		Entity: core.MakeEntity(core.DomainShared, 5), Slot: 1, Seq: 3,
		Energy: -12, Heat: 55, Overheat: 2, EmberActive: true,
		ShieldActive: true, ShieldRadiusX: 9, ShieldInvRxSq: 0.25, ShieldInvRySq: 1,
		BoostActive: true, BoostRemaining: 1234, BoostTotal: 5678,
		WeaponCharges: []int{1, 0, 2}, WeaponCooldown: []int64{7, 0, 9},
		HitPoints: 88, BlinkActive: true, BlinkType: 3, BlinkLevel: 2,
	}
	frame, encErr := event.NewWireFrame(event.GameEvent{
		Type: event.EventCursorStateSync, Domain: core.DomainShared, Payload: want,
	})
	if encErr != "" {
		t.Fatalf("encode: %s", encErr)
	}
	_, payload, _, err := frame.Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, ok := payload.(*event.CursorStatePayload)
	if !ok {
		t.Fatalf("decoded payload = %T, want *event.CursorStatePayload", payload)
	}
	if got.Energy != want.Energy || got.Heat != want.Heat || !got.EmberActive ||
		got.ShieldInvRxSq != want.ShieldInvRxSq || got.BoostRemaining != want.BoostRemaining ||
		len(got.WeaponCharges) != 3 || got.WeaponCharges[2] != 2 ||
		len(got.WeaponCooldown) != 3 || got.WeaponCooldown[2] != 9 {
		t.Fatalf("cursor state round trip = %#v, want %#v", got, want)
	}
}

// The local-input goal is an equality: a session's local
// cursor and typing must respond exactly as a solo run does. Every test here
// therefore measures the same probe twice — once solo, once on the producing
// instance of a live two-participant session — and asserts the two agree.
//
// The session figures these replaced are recorded in
// doc/multi-player-enhancement.md §3: one keypress reaching the store only after
// the playout lead, one cell of five, and five typing errors out of six correct
// keystrokes. D-18's prediction is what closes the gap; the barrier below it is
// deliberately unchanged, and the first test asserts that too.

// soloInstance is one participant with a cursor and no session.
func soloInstance(t *testing.T, seed uint64) *App {
	t.Helper()
	a := mustHeadless(t, seed, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Tick(1)
	return a
}

// liveInstance is the producing instance of a live two-participant session, past
// the first ticks so the barrier owns its crossings.
func liveInstance(t *testing.T, seed uint64) (*App, []*App) {
	t.Helper()
	apps := meshSession(t, seed, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	for range 3 {
		tickAll(apps)
	}
	return apps[0], apps
}

// localCell reads what this instance's own input and view resolve against.
func localCell(a *App) (pos component.PositionComponent, ok bool) {
	a.World().RunSafe(func() { pos, ok = a.World().LocalCursor() })
	return pos, ok
}

// localCursorEntity returns the shared cursor this instance drives.
func localCursorEntity(a *App) (e core.Entity) {
	a.World().RunSafe(func() { e = a.World().Resources.Player.Entity })
	return e
}

func inject(t *testing.T, a *App, intents ...*input.Intent) {
	t.Helper()
	if !a.Inject(intents...) {
		t.Fatal("intent quit the game")
	}
}

// TestOneKeypressMovesTheLocalCursorWithoutATick is §3's first row. Solo, a press
// lands before any tick; in a session it used to take the whole playout lead.
func TestOneKeypressMovesTheLocalCursorWithoutATick(t *testing.T) {
	t.Parallel()
	press := func(a *App) (before, after component.PositionComponent) {
		t.Helper()
		before, _ = localCell(a)
		inject(t, a, intentMotion(input.MotionRight, 1))
		after, _ = localCell(a)
		return before, after
	}

	solo := soloInstance(t, 0x10CA1)
	soloBefore, soloAfter := press(solo)
	if soloAfter.X != soloBefore.X+1 || soloAfter.Y != soloBefore.Y {
		t.Fatalf("solo press moved the cursor to %#v, want one cell right of %#v", soloAfter, soloBefore)
	}

	live, apps := liveInstance(t, 0x10CA1)
	liveBefore, liveAfter := press(live)
	if liveAfter.X != liveBefore.X+1 || liveAfter.Y != liveBefore.Y {
		t.Fatalf("session press moved the cursor to %#v, want the solo answer: one cell right of %#v",
			liveAfter, liveBefore)
	}

	// Prediction gives the cell this participant reads; the local path drops the playout
	// lead off the shared store as well, so the producer's own crossing is applied
	// in the tick that produced it and the prediction and the store agree at once.
	// The peers keep the lead, which is where a remote participant's motion is
	// interpolated from.
	local := localCursorEntity(live)
	want := liveBefore
	want.X++
	if got := cursorPosition(live, local); got != want {
		t.Fatalf("the producer's own crossing landed at %#v, want %#v with no lead", got, want)
	}
	if got := cursorPosition(apps[1], local); got != liveBefore {
		t.Fatalf("a peer applied the crossing at %#v before its apply tick", got)
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := cursorPosition(a, local); got != want {
			t.Fatalf("participant %d applied the crossing as %#v, want %#v", i+1, got, want)
		}
	}
}

// TestFiveKeypressesBetweenTicksReachFiveCells is §3's second row. Every press
// resolves its motion from the cell the previous one selected, so five presses
// select five cells; a session that re-read the shared store selected one, four
// times over.
func TestFiveKeypressesBetweenTicksReachFiveCells(t *testing.T) {
	t.Parallel()
	const presses = 5

	cells := func(a *App, drain func()) (int, component.PositionComponent) {
		t.Helper()
		local := localCursorEntity(a)
		seen := map[component.PositionComponent]bool{}
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type != event.EventCursorMoved {
				return
			}
			if p, ok := ev.Payload.(*event.CursorMovedPayload); ok && p.Entity == local {
				seen[component.PositionComponent{X: p.X, Y: p.Y}] = true
			}
		})
		defer a.SetDispatchTap(nil)

		for range presses {
			inject(t, a, intentMotion(input.MotionRight, 1))
		}
		drain()
		pos, _ := localCell(a)
		return len(seen), pos
	}

	solo := soloInstance(t, 0x5CE115)
	soloStart, _ := localCell(solo)
	soloCells, soloEnd := cells(solo, func() { solo.Tick(1) })
	if soloCells != presses {
		t.Fatalf("solo placed the cursor on %d cells, want %d", soloCells, presses)
	}
	if soloEnd.X != soloStart.X+presses {
		t.Fatalf("solo cursor ended at %#v, want %d cells right of %#v", soloEnd, presses, soloStart)
	}

	live, apps := liveInstance(t, 0x5CE115)
	liveStart, _ := localCell(live)
	liveCells, liveEnd := cells(live, func() {
		for range parameter.NetworkBarrierDelayTicks + 1 {
			tickAll(apps)
		}
	})
	if liveCells != soloCells {
		t.Fatalf("the session placed the cursor on %d cells, want the solo %d", liveCells, soloCells)
	}
	if liveEnd.X != liveStart.X+presses {
		t.Fatalf("session cursor ended at %#v, want %d cells right of %#v", liveEnd, presses, liveStart)
	}
}

// glyphRun writes runes into the cells the local cursor stands on and to its right,
// so a keystroke that lands on its own cell finds its own character there.
//
// The run is player-domain, which is what a corpus glyph is (§4: every shared glyph
// is a gold composite member). Whatever the corpus already put on those cells is
// destroyed first, because the typing path answers with the first glyph it finds in
// the cell; a shared one would make the probe measure a composite instead, and the
// test says so rather than quietly measuring something else.
func glyphRun(t *testing.T, a *App, runes string) {
	t.Helper()
	pos, ok := localCell(a)
	if !ok {
		t.Fatal("no local cursor")
	}

	var shared []component.PositionComponent
	a.World().RunSafe(func() {
		w := a.World()
		var buf [parameter.MaxEntitiesPerCell]core.Entity
		for i, r := range runes {
			cell := component.PositionComponent{X: pos.X + i, Y: pos.Y}
			n := w.Positions.GetAllEntitiesAtInto(cell.X, cell.Y, buf[:])
			for _, e := range buf[:n] {
				if !w.Components.Glyph.HasEntity(e) {
					continue
				}
				if e.Domain() == core.DomainShared {
					shared = append(shared, cell)
					continue
				}
				w.DestroyEntity(e)
			}
			g := w.CreateEntity(core.DomainPlayer)
			w.Positions.SetPosition(g, cell)
			w.Components.Glyph.SetComponent(g, component.GlyphComponent{
				Rune: r, Type: component.GlyphRed, Level: 1,
			})
		}
	})
	if len(shared) > 0 {
		t.Fatalf("a shared glyph occupies %v; the run would answer with it instead", shared)
	}
}

// TestFastTypingOverAGlyphRunScoresNoErrors is §3's third row, and the one that
// matters most: in a typing game, keystrokes issued faster than the playout lead
// were not merely dropped, they were scored against the player, because each one
// resolved against a cell whose glyph the previous keystroke had already consumed.
func TestFastTypingOverAGlyphRunScoresNoErrors(t *testing.T) {
	t.Parallel()
	const run = "abcdef"

	typed := func(a *App) (correct, errors int64) {
		t.Helper()
		glyphRun(t, a, run)
		inject(t, a, intentModeSwitch(input.ModeTargetInsert))
		for _, r := range run {
			inject(t, a, intentTextChar(r))
		}
		a.World().RunSafe(func() {
			reg := a.World().Resources.Status
			correct = reg.Ints.Get("typing.correct").Load()
			errors = reg.Ints.Get("typing.errors").Load()
		})
		return correct, errors
	}

	solo := soloInstance(t, 0x7791AB)
	soloCorrect, soloErrors := typed(solo)
	if soloCorrect != int64(len(run)) || soloErrors != 0 {
		t.Fatalf("solo typed %d correct, %d errors; want %d and 0", soloCorrect, soloErrors, len(run))
	}

	live, _ := liveInstance(t, 0x7791AB)
	liveCorrect, liveErrors := typed(live)
	if liveCorrect != soloCorrect || liveErrors != soloErrors {
		t.Fatalf("the session typed %d correct, %d errors; want the solo %d and %d",
			liveCorrect, liveErrors, soloCorrect, soloErrors)
	}
}

// TestTypedGoldMembersDisappearWithoutATick pins the shared half of local input
// prediction. Gold is composite and shared, but the producing peer must still see
// each correct member leave the screen before the next terminal frame; the same
// crossing reaches the other peers on their playout schedule and corrections
// remain free to reconcile the provisional result.
func TestTypedGoldMembersDisappearWithoutATick(t *testing.T) {
	t.Parallel()
	type member struct {
		entity core.Entity
		cell   component.PositionComponent
		rune   rune
	}

	live, _ := liveInstance(t, 0x601D)
	live.Context().PushEventOrigin(event.EventGoldSpawnRequest, nil, event.OriginDebug)
	live.Settle()
	live.Tick(2)

	var run []member
	live.World().RunSafe(func() {
		w := live.World()
		for _, headerEntity := range w.Components.Header.GetAllEntities() {
			header, ok := w.Components.Header.GetComponent(headerEntity)
			if !ok || header.Behavior != component.BehaviorGold {
				continue
			}
			for _, entry := range header.MemberEntries {
				glyph, glyphOK := w.Components.Glyph.GetComponent(entry.Entity)
				cell, cellOK := w.Positions.GetPosition(entry.Entity)
				if glyphOK && cellOK {
					run = append(run, member{entity: entry.Entity, cell: cell, rune: glyph.Rune})
				}
			}
			break
		}
		slices.SortFunc(run, func(a, b member) int { return cmp.Compare(a.cell.X, b.cell.X) })
		if len(run) > 0 {
			cursor := w.Resources.Player.Entity
			w.Positions.SetPosition(cursor, run[0].cell)
			w.Resources.Player.DropPrediction()
		}
	})
	if len(run) != parameter.GoldSequenceLength {
		t.Fatalf("gold run has %d members, want %d", len(run), parameter.GoldSequenceLength)
	}

	inject(t, live, intentModeSwitch(input.ModeTargetInsert))
	startTick := live.Position().Tick
	for i, m := range run {
		inject(t, live, intentTextChar(m.rune))
		if got := live.Position().Tick; got != startTick {
			t.Fatalf("typing member %d advanced tick %d to %d", i, startTick, got)
		}
		live.World().RunSafe(func() {
			if live.World().Components.Glyph.HasEntity(m.entity) {
				t.Fatalf("typed gold member %d remains renderable before a tick", i)
			}
		})
		if got, _ := sharedGlyphs(live); got != len(run)-i-1 {
			t.Fatalf("after member %d, %d shared glyphs remain; want %d", i, got, len(run)-i-1)
		}
	}
}

// TestPredictedLocalCursorReconcilesAndSnaps is D-18's reconcile half. A placement
// this participant did not request is the authority, and the prediction it disagrees
// with is discarded rather than merged: the queue is emptied and the local cell
// falls back to the store.
func TestPredictedLocalCursorReconcilesAndSnaps(t *testing.T) {
	t.Parallel()
	live, apps := liveInstance(t, 0xD18ADD)
	local := localCursorEntity(live)
	start, _ := localCell(live)

	// Two of this participant's own placements, outstanding behind the barrier.
	inject(t, live, intentMotion(input.MotionRight, 1))
	inject(t, live, intentMotion(input.MotionRight, 1))
	predicted := start
	predicted.X += 2
	if got, _ := localCell(live); got != predicted {
		t.Fatalf("two presses predicted %#v, want %#v", got, predicted)
	}

	// A placement the prediction did not produce. Stamped shared, so it is not a
	// crossing and CursorSystem applies it at once — a level setup, a wall push-out
	// and a reset all reach the local cursor this way.
	snap := start
	snap.X -= 4
	live.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: local, X: snap.X, Y: snap.Y}, event.OriginDebug)
	live.Settle()

	if got, _ := localCell(live); got != snap {
		t.Fatalf("local cell after an unpredicted placement = %#v, want the authoritative %#v", got, snap)
	}
	if got := cursorPosition(live, local); got != snap {
		t.Fatalf("store after an unpredicted placement = %#v, want %#v", got, snap)
	}

	// Discarded, not merged, and nothing comes back to un-discard it. Dropping
	// the playout lead off the local path, so the two crossings the prediction
	// described had already applied on this instance before the authoritative
	// placement replaced them; what is still in flight is the peers' copies, which
	// land at the agreed tick and are then corrected by the host like any other
	// disagreement.
	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	if got, _ := localCell(live); got != snap {
		t.Fatalf("local cell after the lead drained = %#v, want the authoritative %#v", got, snap)
	}
	if got := cursorPosition(live, local); got != snap {
		t.Fatalf("store after the lead drained = %#v, want %#v", got, snap)
	}
	if got := cursorPosition(apps[1], local); got != predicted {
		t.Fatalf("the peer applied the outstanding crossings as %#v, want %#v", got, predicted)
	}
}

// parityScript builds the option set two instances step in lockstep. Resizes and
// resets both re-derive map bounds from this instance's terminal, so both are
// excluded; motions are restricted to the map-relative set, since a screen- or
// page-relative motion resolves against a viewport the instances do not share.
func parityScript(seed uint64, steps int) journal.FuzzOptions {
	opt := journal.DefaultFuzz(seed, steps)
	opt.Resizes = false
	opt.Resets = false
	opt.MapMotionsOnly = true
	return opt
}

// TestSharedSnapshotParityAcrossTerminalSizes is the D-11 criterion: two
// instances of one seed on different terminals agree on every shared record.
//
// Both are constructed at one size and diverge only after SetupLevel decouples the
// map from the viewport with crop off. Constructing them at different sizes instead
// would bake different map bounds into the FSM's entry actions, which run inside New,
// before any Tick.
func TestSharedSnapshotParityAcrossTerminalSizes(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	steps := soakScale(48, 96, 400)

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()
	b := mustHeadless(t, seed, 120, 40)
	defer b.Close()

	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
		x.Tick(1)
	}
	assertSharedParity(t, a, b, -2)

	// Only b's terminal changes; the map is now the FSM's, not the terminal's
	b.Resize(180, 56)
	b.Tick(1)
	a.Tick(1)
	assertSharedParity(t, a, b, -1)

	opt := parityScript(seed, steps)
	da, db := journal.NewFuzzDriver(a, opt), journal.NewFuzzDriver(b, opt)
	for i := range steps {
		if !da.Step() {
			t.Fatalf("step %d quit instance a", i)
		}
		if !db.Step() {
			t.Fatalf("step %d quit instance b", i)
		}
		assertSharedParity(t, a, b, i)
	}
}

// assertSharedParity fails with the first divergent record and its neighbours
func assertSharedParity(t *testing.T, a, b *App, step int) {
	t.Helper()
	x, y := a.SnapshotShared(), b.SnapshotShared()
	idx, lx, ly, ok := snapshot.FirstDiff(x, y)
	if !ok {
		return
	}
	t.Fatalf("step %d: shared snapshot diverged at line %d\n  a: %s\n  b: %s\n%s\n%s",
		step, idx, lx, ly, strings.Join(snapshot.Diff(x, y, 8), "\n"), strings.Join(diffSharedWorld(a, b, 8), "\n"))
}

// diffSharedWorld names the entities behind a world-digest mismatch.
func diffSharedWorld(a, b *App, maxDiff int) []string {
	type state struct {
		position  component.PositionComponent
		kinetic   component.KineticComponent
		combat    component.CombatComponent
		hasPos    bool
		hasKin    bool
		hasCombat bool
	}
	read := func(x *App) map[core.Entity]state {
		out := make(map[core.Entity]state)
		x.World().RunSafe(func() {
			w := x.World()
			visit := func(e core.Entity) {
				if e.Domain() != core.DomainShared {
					return
				}
				s := out[e]
				s.position, s.hasPos = w.Positions.GetPosition(e)
				s.kinetic, s.hasKin = w.Components.Kinetic.GetComponent(e)
				s.combat, s.hasCombat = w.Components.Combat.GetComponent(e)
				if s.hasPos || s.hasKin || s.hasCombat {
					out[e] = s
				}
			}
			for _, e := range w.Positions.Entities() {
				visit(e)
			}
			for _, e := range w.Components.Kinetic.Entities() {
				visit(e)
			}
			for _, e := range w.Components.Combat.Entities() {
				visit(e)
			}
		})
		return out
	}

	x, y := read(a), read(b)
	entities := make([]core.Entity, 0, len(x)+len(y))
	for e := range x {
		entities = append(entities, e)
	}
	for e := range y {
		if _, ok := x[e]; !ok {
			entities = append(entities, e)
		}
	}
	slices.Sort(entities)

	out := make([]string, 0, maxDiff)
	for _, e := range entities {
		sx, okx := x[e]
		sy, oky := y[e]
		if okx == oky && sx == sy {
			continue
		}
		out = append(out, fmt.Sprintf("  entity %d: a=(%+v,%t) b=(%+v,%t)", e, sx, okx, sy, oky))
		if len(out) == maxDiff {
			break
		}
	}
	if len(out) == 0 {
		return []string{"  compared shared world state agrees"}
	}
	return out
}
