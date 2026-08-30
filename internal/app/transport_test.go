package app

import (
	"errors"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// pair builds two joined participants on one seed, linked by an in-process
// transport. Each spawns its own cursor in its own slot and mirrors the other's as
// a remote, which is the roster a real join produces.
func pair(t *testing.T, seed uint64, steps int) (*App, *App) {
	t.Helper()

	a := mustHeadless(t, seed, 120, 40)
	b := mustHeadless(t, seed, 120, 40)
	t.Cleanup(func() { a.Close(); b.Close() })

	an := a.JoinAnchor()
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

// mirrorCursors splits ownership of a two-slot roster. Both instances run the
// same spawn requests, so the two rosters hold the same shared entities in the same
// slots (D-11); what differs is Control and the local binding, which is the whole
// of what D-2 keys on. a drives slot 0 and mirrors slot 1; b is its inverse.
func mirrorCursors(t *testing.T, a, b *App) (localA, remoteA core.Entity) {
	t.Helper()

	for _, x := range []*App{a, b} {
		x.Context().PushEventOrigin(event.EventCursorSpawnRequest,
			&event.CursorSpawnRequestPayload{
				Slot: 1, X: 20, Y: 10,
				Control: uint8(component.ControlRemote), PeerID: 1,
			}, event.OriginDebug)
		x.Settle()
	}

	// b owns the second cursor and mirrors the first
	b.World().RunSafe(func() {
		w := b.World()
		if c, ok := w.Components.Cursor.GetPtr(w.Resources.Player.Slot(0)); ok {
			c.Control, c.PeerID = component.ControlRemote, 1
		}
		if c, ok := w.Components.Cursor.GetPtr(w.Resources.Player.Slot(1)); ok {
			c.Control, c.PeerID = component.ControlHuman, 0
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

// TestTransportSyncsOwnerAuthoredCursorState is Phase 7 item 4: the owner writes,
// the peer receives, and the peer's own systems never author the same cell.
func TestTransportSyncsOwnerAuthoredCursorState(t *testing.T) {
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

// TestTransportCarriesCrossingsWithoutEcho is Phase 7 item 5: a D-3 artifact
// reaches the peer, and the peer does not send it back.
func TestTransportCarriesCrossingsWithoutEcho(t *testing.T) {
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
	assertPosition(a, false)
	assertPosition(b, false)
	for range parameter.NetworkBarrierDelayTicks {
		a.Tick(1)
		b.Tick(1)
		assertPosition(a, false)
		assertPosition(b, false)
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

// TestObserverSharedStateTracksTheLiveParticipant proves the barrier first with
// one-way traffic: local and peer artifacts apply at the same future tick boundary.
func TestObserverSharedStateTracksTheLiveParticipant(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := 1200
	if testing.Short() {
		steps = 120
	}

	live, observer := pair(t, seed, steps)
	observeOnly(t, observer)

	assertSharedParity(t, live, observer, -1)

	// Only the live participant is scripted, so the two operator actions that
	// rewrite shared state on the instance that runs them — an FSM region op and a
	// level setup — have no counterpart on the observer.
	opt := parityScript(seed, steps)
	opt.Regions = false
	opt.MapSetups = false
	d := NewScriptDriver(live, opt)
	for i := range steps {
		before := live.Position().Tick
		if !d.Step() {
			t.Fatalf("step %d quit the live participant", i)
		}
		// The observer runs the same clock, then drains what the step produced
		if n := int(live.Position().Tick - before); n > 0 {
			observer.Tick(n)
		}
		live.Tick(1)
		observer.Tick(1)
		assertSharedParity(t, live, observer, i)
	}
}

// TestTwoLiveParticipantsStayInLockstep is Phase 8's headless exit criterion.
func TestTwoLiveParticipantsStayInLockstep(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := 1200
	if testing.Short() {
		steps = 120
	}

	a, b := pair(t, seed, steps)
	localA, _ := mirrorCursors(t, a, b)
	var localB core.Entity
	b.World().RunSafe(func() { localB = b.World().Resources.Player.Slot(1) })
	proveTwoLive(t, a, b, localA, localB, seed, steps, func() {
		a.Tick(1)
		b.Tick(1)
	})
}

// TestActivatedSessionDefersCrossingBeforeFirstTick closes the lobby/input gap.
func TestActivatedSessionDefersCrossingBeforeFirstTick(t *testing.T) {
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
	if got := cursorPosition(a, target); got != start {
		t.Fatalf("host applied pre-tick crossing immediately: %#v", got)
	}
	if got := cursorPosition(b, target); got != start {
		t.Fatalf("joiner applied pre-tick crossing immediately: %#v", got)
	}

	for range parameter.NetworkBarrierDelayTicks + 1 {
		a.Tick(1)
		b.Tick(1)
	}
	want := start
	want.X++
	if got := cursorPosition(a, target); got != want {
		t.Fatalf("host crossing position = %#v, want %#v", got, want)
	}
	if got := cursorPosition(b, target); got != want {
		t.Fatalf("joiner crossing position = %#v, want %#v", got, want)
	}
}

// TestTwoLiveParticipantsStayInLockstepOverTCP proves the same session through
// stream framing, the anchor handshake and canonical socket participant IDs.
func TestTwoLiveParticipantsStayInLockstepOverTCP(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := 1200
	if testing.Short() {
		steps = 120
	}

	a := mustHeadless(t, seed, 120, 40)
	t.Cleanup(a.Close)
	offer, err := a.hostOffer()
	if err != nil {
		t.Fatalf("host offer: %v", err)
	}
	hostCfg := network.DebugConfig(network.RoleHost, "127.0.0.1:0")
	hostCfg.ParticipantID = offer.Host
	hostCfg.AcceptSession = network.HostAcceptor(a.hostOffer, time.Second)
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
	if err := b.JoinSession(offered); err != nil {
		_ = pending.Complete(err)
		t.Fatalf("join session: %v", err)
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
	proveTwoLive(t, a, b, localA, localB, seed, steps, func() {
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
	a.Tick(1)
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
	if roster != 1 || !host.IsRunning() || state != "down" || peers != 0 || latched {
		t.Fatalf("host after disconnect = roster %d running %t state %q peers %d latch %t",
			roster, host.IsRunning(), state, peers, latched)
	}

	retry, retryOffer, err := network.DialSession(host.Addr().String(), network.DebugConfig(network.RolePeer, ""))
	if err != nil {
		t.Fatalf("retry dial: %v", err)
	}
	defer retry.Close()
	retryCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, retryOffer)
	if err != nil {
		t.Fatalf("retry config: %v", err)
	}
	retryApp, err := NewHeadless(retryCfg)
	if err != nil {
		t.Fatalf("retry app: %v", err)
	}
	defer retryApp.Close()
	joinErr := retryApp.JoinSession(retryOffer)
	if !errors.Is(joinErr, ErrJoinMidRun) {
		t.Fatalf("retry JoinSession() error = %v, want ErrJoinMidRun", joinErr)
	}
	if err := retry.Complete(joinErr); !errors.Is(err, ErrJoinMidRun) {
		t.Fatalf("retry Complete() error = %v, want unchanged ErrJoinMidRun", err)
	}
	select {
	case err := <-host.Errors():
		if err.Error() != joinErr.Error() {
			t.Fatalf("host retry error = %v, want %v", err, joinErr)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not report rejected mid-run join")
	}
	if !host.IsRunning() || host.PeerCount() != 0 {
		t.Fatalf("host after rejected retry = running %t peers %d", host.IsRunning(), host.PeerCount())
	}
}

func proveTwoLive(t *testing.T, a, b *App, localA, localB core.Entity, seed uint64, steps int, tickPair func()) {
	t.Helper()
	assertSharedParity(t, a, b, -1)

	// The harness owns the clock and holds non-transported operator mutations fixed.
	optA := parityScript(seed, steps)
	optA.Regions, optA.MapSetups = false, false
	optA.DisableTicks, optA.DisableCommands, optA.DisableOverlays = true, true, true
	optB := optA
	optB.Seed ^= 0x9E3779B97F4A7C15
	da, db := NewScriptDriver(a, optA), NewScriptDriver(b, optB)

	startA, startB := cursorPosition(a, localA), cursorPosition(b, localB)
	movedA, movedB := false, false
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
		assertSharedParity(t, a, b, i)
	}
	for i := range parameter.NetworkBarrierDelayTicks + 1 {
		tickPair()
		assertSharedParity(t, a, b, steps+i)
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

func waitSocket(t *testing.T, port *network.SocketPort, ready func() bool, what string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
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

// cursorPosition reads one roster entity under the world lock.
func cursorPosition(a *App, e core.Entity) (pos component.PositionComponent) {
	a.World().RunSafe(func() { pos, _ = a.World().Positions.GetPosition(e) })
	return pos
}

// observeOnly marks every cursor on an instance remote, so it simulates none and
// its shared state is whatever the wire delivers
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
	})
	if owned == 0 {
		t.Fatal("observer has no cursor to mirror")
	}
}

// TestWireSetExcludesDerivedAndShared asserts the predicate rather than the pipe:
// a Shared event is re-derived on both sides and must never travel, a chain
// follow-up is derived from the root that did (D-5), and an arriving artifact is
// never echoed.
func TestWireSetExcludesDerivedAndShared(t *testing.T) {
	event.EnsureRegistry()

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

// TestWireFrameRoundTrips asserts the codec preserves what the receiver pushes:
// the event type, the producer's domain, and every payload field.
func TestWireFrameRoundTrips(t *testing.T) {
	event.EnsureRegistry()

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

// TestCursorStatePayloadRoundTrips covers the D-13 value transfer through the same
// codec, including the slices the fixed-size weapon arrays flatten into.
func TestCursorStatePayloadRoundTrips(t *testing.T) {
	event.EnsureRegistry()

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

// disableSystem turns one system off on an instance, through the same command the
// operator surface uses
func disableSystem(t *testing.T, a *App, name string) {
	t.Helper()
	a.Context().PushEventOrigin(event.EventMetaSystemCommandRequest,
		&event.MetaSystemCommandPayload{SystemName: name, Enabled: false}, event.OriginDebug)
	a.Settle()
}
