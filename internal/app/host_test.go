package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/mode"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// joinTestTickInterval paces the host through a join in this harness. See the
// comment at its use.
const joinTestTickInterval = 10 * time.Millisecond

func TestMidRunHostUsesConfiguredCapOrMaximum(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	for _, tt := range []struct {
		name         string
		participants int
		want         int
	}{
		{name: "explicit", participants: 3, want: 3},
		{name: "unspecified", want: parameter.MaxPlayers},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := mustHeadless(t, 0x3016, 120, 40)
			defer host.Close()
			host.cfg.Participants = tt.participants
			if err := host.BeginHosting("127.0.0.1:0"); err != nil {
				t.Fatalf("begin hosting: %v", err)
			}
			if got := host.remoteParticipantCount() + 1; got != tt.want {
				t.Fatalf("lobby size = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun drives the mid-run join over
// a real socket at a tick that is not zero.
//
// The whole claim is in this one path. A run that was solo opens a
// socket without restarting. A participant dials it hundreds of ticks in, receives
// the world instead of re-deriving it, and installs it. The crossings the host
// produced while that was happening reach the joiner rather than falling into the
// gap, because the joiner was admitted before the world was read for it. The joiner
// closes the remaining tick gap by simulating it. Its cursor is created by a
// crossing, at one agreed tick, on both instances. And the two then hold the same
// shared world.
func TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3017
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(240)

	if got := host.SessionSummary(); !strings.Contains(got, "Solo") {
		t.Fatalf("before hosting the run reports %q", got)
	}
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.HostAddr()
	if addr == "" {
		t.Fatal("hosting run reports no address")
	}
	var latched bool
	host.World().RunSafe(func() { latched = host.World().SessionShared() })
	if !latched {
		t.Fatal("hosting run is not latched as a session")
	}

	// The host has to keep running through the join: the capture is read under the
	// world lock from the accept goroutine, and the gap the joiner then closes is
	// exactly the ticks the host completed while its world was in transit. A host
	// frozen for the transfer would prove the easy half of this.
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
			// Faster than the game interval on purpose. The join has to close a gap
			// the host opened while its world was in transit, and at one tick per
			// 50 ms a loopback transfer finishes inside a single tick and the gap
			// is never there to close. Compressing the host's pacing is the honest
			// stand-in for the slow link or the large world that produces one.
			time.Sleep(joinTestTickInterval)
		}
	}()

	guest, _ := mustSocketJoiner(t, addr, seed, 120, 40)
	close(stop)
	<-ticking

	// The joiner arrived at a tick the host had reached, not at tick zero, and it
	// closed the gap between the world it was sent and where the session had got to
	// rather than staying behind it.
	guestTick := guest.Position().Tick
	if guestTick < 240 {
		t.Fatalf("guest entered at tick %d; the host was past 240 before it dialled", guestTick)
	}
	reg := guest.World().Resources.Status
	installed := reg.Ints.Get("snapshot.install_tick").Load()
	caught := reg.Ints.Get("snapshot.catch_up_ticks").Load()
	if installed <= 0 {
		t.Fatal("the guest installed no capture")
	}
	if caught == 0 || int64(guestTick) < installed {
		t.Fatalf("guest installed at tick %d and stands at %d after %d catch-up ticks; "+
			"the session moved during the transfer and the join has to close that",
			installed, guestTick, caught)
	}
	t.Logf("installed at tick %d, entered at %d (%d catch-up ticks), stage %dus commit %dus",
		installed, guestTick, caught,
		reg.Ints.Get("snapshot.stage_us").Load(), reg.Ints.Get("snapshot.commit_us").Load())
	// The bound is an absolute offset, not a one-sided lag. A joiner that ends a
	// tick ahead of the last epoch it saw is not a problem — its own crossings then
	// carry the full playout lead — and the assertion is that neither side is
	// further away than that lead, in either direction.
	offset := int64(host.Position().Tick) - int64(guestTick)
	if offset < 0 {
		offset = -offset
	}
	if offset > int64(parameter.NetworkJoinLagTicks) {
		t.Fatalf("guest stands %d ticks from the host after catching up; the lead is %d",
			offset, parameter.NetworkJoinLagTicks)
	}

	// The authority's cadence is stopped for the rest of this test, and everything
	// it already sent is drained. The subject here is the join, and a correction is
	// a *clock* as much as a world — installing one pins this guest's tick to the
	// host's at the moment the capture was read — so one landing between two
	// assertions would move the very thing they compare. The correction criteria
	// elsewhere are where a correction is the subject.
	settleCorrections(t, host, guest)

	// Bring the two onto one tick, then hold them there. The residual offset is this
	// harness's wall pacing, not the join's: the guest caught up to the newest epoch
	// it had seen, and the host closed a few more while the goroutine was stopping.
	alignTicks(t, host, guest)
	assertSharedParity(t, host, guest, 0)

	// The arrival is a crossing, so the guest's cursor exists on both instances as
	// the same shared entity. It is not in the capture: the capture was read before
	// the participant had one.
	waitForRosterPair(t, host, guest)
	alignTicks(t, host, guest)
	assertControl(t, host, 1, component.ControlRemote)
	assertControl(t, guest, 1, component.ControlHuman)
	if got := guest.localSlot(); got != 1 {
		t.Fatalf("guest follows slot %d, want its own slot 1", got)
	}

	// Nothing was applied twice: the artifacts the host produced between admitting
	// the guest and reading the world for it are in the capture, and the barrier
	// recognised them rather than replaying them onto a world that already had them.
	assertSharedParity(t, host, guest, 1)
	if got := host.SessionSummary(); !strings.Contains(got, "host") {
		t.Fatalf("hosting run reports %q", got)
	}
}

// TestAReconnectIsTheSameJoin asserts there is no fourth join mechanism.
//
// A participant that drops leaves a departure crossing behind, and the coordinator
// returns its identity to the pool. What comes back is a new dial: the same
// acceptor, the same identity allocation, the same capture at whatever tick the
// host has now reached, the same install, the same arrival crossing. Nothing here
// is reconnect-specific, and that is the whole claim — so the test is the join test
// run twice against one host, with a disconnect in between, asserting the second
// arrival lands on a world the host has moved well past since the first.
func TestAReconnectIsTheSameJoin(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	const seed = 0x3019
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(120)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	addr := host.HostAddr()

	firstTick := joinAndLeave(t, host, addr, seed)

	// The host keeps playing between the two joins, so the second capture describes
	// a world the first participant never saw.
	pumpHost(t, host, 60)
	secondTick := joinAndLeave(t, host, addr, seed)

	if secondTick <= firstTick {
		t.Fatalf("the reconnect installed tick %d, the first join installed %d; "+
			"the host has to have moved on between them or this proves nothing",
			secondTick, firstTick)
	}
}

// joinAndLeave runs one full join against a live host, returns the tick the guest
// installed at, and then drops the link.
func joinAndLeave(t *testing.T, host *App, addr string, seed uint64) uint64 {
	t.Helper()
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

	guest, port := mustSocketJoiner(t, addr, seed, 120, 40)
	close(stop)
	<-ticking

	installed := uint64(guest.World().Resources.Status.Ints.Get("snapshot.install_tick").Load())
	waitForRosterPair(t, host, guest)

	// The departure is a crossing like any other, so the host applies it a playout
	// lead after it observes the lost link, not where the link was lost.
	_ = port.Close()
	guest.Close()
	waitForHostRoster(t, host, 1)
	return installed
}

// waitForHostRoster ticks the host until its roster falls to want, which is the
// departure crossing applying a playout lead after the link was seen to go.
func waitForHostRoster(t *testing.T, host *App, want int) {
	t.Helper()
	for range 80 {
		var roster int
		host.World().RunSafe(func() { roster = host.World().Resources.Player.Count() })
		if roster == want {
			return
		}
		host.Tick(1)
		time.Sleep(joinTestTickInterval / 2)
	}
	var roster int
	host.World().RunSafe(func() { roster = host.World().Resources.Player.Count() })
	t.Fatalf("host roster holds %d cursors after the guest left, want %d", roster, want)
}

// pumpHost advances a hosting instance at something like its wall pacing, so the
// transport's own goroutines see the ticks pass.
func pumpHost(t *testing.T, host *App, ticks int) {
	t.Helper()
	for range ticks {
		host.Tick(1)
		time.Sleep(joinTestTickInterval / 2)
	}
}

// TestHostCommandRunsUnderTheWorldLock is the regression for a deadlock, and it
// exists because the unit test that did not have it passed.
//
// The whole router path runs inside App.handleIntent's critical section — mode/
// must never acquire the world lock itself — so a SessionController method that
// took the lock wedges the instance at the moment the operator presses enter, with
// neither a tick nor a signal able to get it back. Calling BeginHosting directly
// cannot see that; only the real input path can, so this test takes it.
func TestHostCommandRunsUnderTheWorldLock(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	a := mustHeadless(t, 0x301A, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	injectExCommand(t, a, "host 127.0.0.1:0")
	a.Tick(1)
	if a.HostAddr() == "" {
		t.Fatalf("the command opened no socket; status bar says %q", a.Context().GetStatusMessage())
	}
	injectExCommand(t, a, "session")
	a.Tick(1)
	if got := a.Context().GetStatusMessage(); !strings.Contains(got, "host") {
		t.Fatalf(":session reports %q", got)
	}
}

// injectExCommand types one ex command through the intent pipeline, which is the
// only path that runs it where the runtime actually runs it.
func injectExCommand(t *testing.T, a *App, command string) {
	t.Helper()
	a.Inject(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1})
	for _, r := range command {
		a.Inject(&input.Intent{Type: input.IntentTextChar, Char: r, Count: 1})
	}
	a.Inject(&input.Intent{Type: input.IntentTextConfirm, Count: 1})
}

// TestBeginHostingRefusesASecondSession pins the one rule the command carries: a
// run is in one session or none.
func TestBeginHostingRefusesASecondSession(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	a := mustHeadless(t, 0x3018, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	if err := a.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}
	if err := a.BeginHosting("127.0.0.1:0"); err == nil {
		t.Fatal("a second :host was accepted")
	}
	if err := a.BeginHosting("not-an-address"); err == nil {
		t.Fatal("a malformed address was accepted")
	}
}

// mustSocketJoiner runs the whole guest side of a join against a live host: dial,
// identity, the start gate, the capture, the install and the catch-up. It is the
// production sequence, assembled here because Loop owns it in a run with a
// terminal and this harness owns its own ticks.
func mustSocketJoiner(t *testing.T, addr string, seed uint64, w, h int) (*App, *network.SocketPort) {
	t.Helper()
	pending, offered, err := network.DialSession(addr, network.DebugConfig(network.RolePeer, ""))
	if err != nil {
		t.Fatalf("dial session: %v", err)
	}
	t.Cleanup(func() { _ = pending.Close() })

	joinCfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: w, Height: h}, offered)
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	if joinCfg.Seed != seed {
		t.Fatalf("join config drew seed %#x, host runs %#x", joinCfg.Seed, seed)
	}
	guest, err := NewHeadless(joinCfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	t.Cleanup(guest.Close)
	guest.pendingJoin = pending
	guest.sessionOffer = offered
	if err := guest.JoinAt(offered.Anchor); err != nil {
		_ = pending.Complete(err)
		t.Fatalf("join identity: %v", err)
	}
	if err := pending.Complete(nil); err != nil {
		t.Fatalf("join reply: %v", err)
	}

	// The transport takes the stream only after the gate has read the world off it,
	// which is what startJoinSession does; the port then replays what the gate held.
	port := network.NewSocketPort(pending.TransportConfig())
	t.Cleanup(func() { _ = port.Close() })
	if err := guest.startJoinSession(); err != nil {
		t.Fatalf("join startup: %v", err)
	}
	if err := port.Start(); err != nil {
		t.Fatalf("guest transport: %v", err)
	}
	guest.AttachTransport(port)
	guest.activateNetworkSession()
	if err := guest.resumeJoinedSession(); err != nil {
		t.Fatalf("join catch-up: %v", err)
	}
	return guest, port
}

// alignTicks advances whichever instance is behind until the two stand on one tick,
// so a shared-surface comparison is comparing worlds rather than instants.
func alignTicks(t *testing.T, a, b *App) {
	t.Helper()
	for range 4 * parameter.NetworkJoinLagTicks {
		at, bt := a.Position().Tick, b.Position().Tick
		switch {
		case at == bt:
			return
		case at < bt:
			a.Tick(1)
		default:
			b.Tick(1)
		}
	}
	t.Fatalf("could not align: a at tick %d, b at tick %d", a.Position().Tick, b.Position().Tick)
}

// settleCorrections stops a host's publication cadence and drains whatever it has
// already put on the wire, so a test whose subject is not the correction can compare
// two instances without one of them being re-based mid-comparison.
func settleCorrections(t *testing.T, host, guest *App) {
	t.Helper()
	host.corrections.close()
	for range 8 {
		guest.Tick(1)
		host.Tick(1)
	}
	guest.ApplyPendingCorrections()
}

// waitForRosterPair ticks both instances until the arrival crossing has applied on
// each, and fails if the entity it created is not the same one.
func waitForRosterPair(t *testing.T, host, guest *App) {
	t.Helper()
	for range 4 * parameter.NetworkBarrierDelayTicks {
		var hostE, guestE core.Entity
		host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(1) })
		guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(1) })
		if hostE != 0 && hostE == guestE {
			return
		}
		host.Tick(1)
		guest.Tick(1)
	}
	var hostE, guestE core.Entity
	host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(1) })
	guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(1) })
	t.Fatalf("the arrival crossing left slot 1 as entity %d on the host and %d on the guest",
		hostE, guestE)
}

// TestSessionRosterStartsAndRestartsEveryParticipant captures the two places the
// monitor script used its single player_entity variable as if it described the
// whole roster: lobby admission and the gameplay-wide defeat reset.
func TestSessionRosterStartsAndRestartsEveryParticipant(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA2A2, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	assertArmed := func(phase string) {
		t.Helper()
		for i, a := range apps {
			var heat int
			var energy int64
			var count int
			a.World().RunSafe(func() {
				w := a.World()
				count = w.Resources.Player.Count()
				if c, ok := w.Components.Heat.GetComponent(local[i]); ok {
					heat = c.Current
				}
				if c, ok := w.Components.Energy.GetComponent(local[i]); ok {
					energy = c.Current
				}
			})
			if count != len(apps) || heat != 10 || energy != 100 {
				t.Fatalf("%s participant %d: roster=%d heat=%d energy=%d, want %d/10/100",
					phase, i+1, count, heat, energy, len(apps))
			}
		}
	}

	assertArmed("start")

	// The shared monitor guard is normally published by MetaSystem after every
	// owner reports defeat. Set the already-folded value identically here so this
	// test exercises the real MonitorGlobalReset transition without constructing
	// two complete defeat sequences.
	for _, a := range apps {
		a.World().Resources.Status.Bools.Get("session.all_defeated").Store(true)
	}
	for range 6 {
		tickAll(apps)
	}

	assertArmed("global reset")
	assertMeshParity(t, apps, 6)
}

// TestLiveSessionRefusesAnInstanceLocalPause pins the operator policy: entering a
// local overlay or command mode may inspect a live session, but it must not stop
// that participant's production clock while its peers continue.
func TestLiveSessionRefusesAnInstanceLocalPause(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA1A1, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	apps[0].Context().SetPaused(true)
	apps[0].Settle()

	for i, a := range apps {
		if a.Context().TimeCtl.IsPaused() {
			t.Fatalf("participant %d paused inside a live session", i+1)
		}
	}
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, 0)

	// A synchronous snapshot drains a second log sink while the world lock is
	// held. That may exceed the playout lead, so it is not a live inspection
	// operation even though the non-blocking debug overlay remains available.
	apps[0].Context().ClearStatusMessage()
	mode.ExecuteCommand(apps[0].Context(), "d save")
	if got := apps[0].Context().GetStatusMessage(); got != "Snapshot save unavailable in a live session" {
		t.Fatalf(":d save status=%q", got)
	}
}

// TestCoordinatorResetCrossesAndPreservesRoster reproduces :new as an operator
// injection on one instance. The session must restart at one agreed barrier tick,
// and the reset must rebuild the closed roster rather than the boot cursor alone.
func TestCoordinatorResetCrossesAndPreservesRoster(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA3A3, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	// The same command on a guest is operator-local refusal, not an artifact.
	mode.ExecuteCommand(apps[1].Context(), "n")
	apps[1].Settle()
	if got := apps[1].Position().Run; got != 0 {
		t.Fatalf("guest :new changed run to %d", got)
	}

	mode.ExecuteCommand(apps[0].Context(), "n")
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 8 {
		tickAll(apps)
	}

	for i, a := range apps {
		if got := a.Position().Run; got != 1 {
			t.Fatalf("participant %d reset run=%d, want 1", i+1, got)
		}
		var count int
		a.World().RunSafe(func() { count = a.World().Resources.Player.Count() })
		if count != len(apps) {
			t.Fatalf("participant %d roster=%d after reset, want %d", i+1, count, len(apps))
		}
	}
	assertMeshParity(t, apps, 0)
}

// TestOneSharedQuasarTriggerProducesOneSpawn models the old MainEscalate fan-out:
// the same shared decision asks every player-domain FuseSystem to act. It is one
// logical fusion and therefore must yield one shared spawn request, not N.
func TestOneSharedQuasarTriggerProducesOneSpawn(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA4A4, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	spawns := make([]int, len(apps))
	for i, a := range apps {
		i := i
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventQuasarSpawnRequest {
				spawns[i]++
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}
	// Participant 2 produces the tenth shared defeat. The crossing is delivered
	// to both FSMs, but its causal cursor elects only participant 2's fuse system.
	apps[1].Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: local[1]})
	apps[1].Settle()

	// Fusion waits 600ms; the driven clock advances 50ms per tick.
	for range 20 + parameter.NetworkBarrierDelayTicks {
		tickAll(apps)
	}
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// TestExplosionPresentationStaysWithItsProducer is the presentation half of the
// explosion split. The combat artifact reaches the peer; the smoke center does not.
func TestExplosionPresentationStaysWithItsProducer(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xA5A5, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	apps[0].Context().PushLocal(event.EventExplosionVisualRequest,
		&event.ExplosionVisualRequestPayload{X: 10, Y: 10, Radius: 4, Type: event.ExplosionTypeMissile})
	apps[0].Context().PushCrossing(event.EventExplosionRequest,
		&event.ExplosionRequestPayload{Entity: local[0], X: 10, Y: 10, Radius: 4})
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}

	for i, a := range apps {
		var centers int
		a.World().RunSafe(func() { centers = a.World().Resources.Transient.ExplosionCount })
		want := 0
		if i == 0 {
			want = 1
		}
		if centers != want {
			t.Fatalf("participant %d has %d missile visual centers, want %d", i+1, centers, want)
		}
	}
}

// TestRuntimeDigestIsADriftGaugeRatherThanAVerdict is what the divergence report
// became.
//
// An escalation — DESYNC after two disagreeing samples, DIVERGED after five,
// SYNCED once the state agreed again — states that two instances re-deriving one
// world have lost an artifact and will never get it back. That holds while both
// re-derive and does not hold for a guest that predicts and is corrected. The
// escalation is therefore gone and the measurement stayed:
// a mismatch is counted and the surface that disagrees is named, and neither is a
// failure state a session can be stuck in.
func TestRuntimeDigestIsADriftGaugeRatherThanAVerdict(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0xD165E57, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	var target core.Entity
	var original component.PositionComponent
	for range 8 {
		apps[0].World().RunSafe(func() {
			for _, e := range apps[0].World().Components.Header.Entities() {
				if e.Domain() == core.DomainShared {
					target = e
					original, _ = apps[0].World().Positions.GetPosition(e)
					break
				}
			}
		})
		if target != 0 {
			break
		}
		tickAll(apps)
	}
	if target == 0 {
		t.Fatal("no shared composite available for the drift probe")
	}

	apps[0].World().RunSafe(func() {
		p := original
		p.X++
		apps[0].World().Positions.SetPosition(target, p)
	})
	for range 3*parameter.NetworkDigestTicks + 2 {
		tickAll(apps)
	}

	for i, a := range apps {
		reg := a.World().Resources.Status
		if reg.Ints.Get("network.digest_mismatches").Load() == 0 {
			t.Fatalf("participant %d counted no mismatch against a corrupted position", i+1)
		}
		if got := reg.Strings.Get("network.drift_part").Load(); got != "positions" {
			t.Fatalf("participant %d named %q as the drifting surface, want positions", i+1, got)
		}
		if reg.Ints.Get("network.drift_tick").Load() == 0 {
			t.Fatalf("participant %d named no tick for the drift it reported", i+1)
		}
	}

	// The retired surface is retired, not renamed: a session cannot enter a state
	// the next correction does not leave, so there is nothing left to report one.
	for _, key := range []string{"network.sync_state", "network.sync_part", "network.sync_records"} {
		if apps[0].World().Resources.Status.Strings.Has(key) {
			t.Fatalf("%s is still registered; DESYNC and DIVERGED were retired", key)
		}
	}
	if apps[0].World().Resources.Status.Bools.Has("network.diverged") {
		t.Fatal("network.diverged is still registered; DESYNC and DIVERGED were retired")
	}

	// And the authority closes it. A correction is what repairs a disagreement now,
	// including one nothing in the simulation caused.
	advance := func() { tickAll(apps) }
	want := deliverCorrection(t, apps[0], apps[1:], advance)
	assertCorrected(t, want, apps[1], "guest after a corrupted position")
}

// TestSharedSnapshotExcludesLocalSchedulerTiming pins the distinction the live
// digest needs but the manual-clock harness cannot produce naturally: two real
// schedulers have different wall origins and can miss different deadlines even
// while they complete the same absolute simulation tick.
//
// The set is narrower than it was, twice over. Elapsed game time used to be here on
// the same argument, and that argument was wrong in a way that cost a session: it
// was true only because the simulation instant came from the pacing clock.
// engine.SimTime derives it from the tick instead, so it is tick * interval
// everywhere, and TestSharedSnapshotComparesElapsedGameTime below asserts it is
// compared. The gold sequence's remaining time was here for a different reason —
// a tick-zero joiner reached MainSpawnGold one tick before its host — and a joiner
// installs the host's world rather than reproducing it, so it is compared too;
// TestSnapshotJoinCarriesTheGoldDeadline holds it.
func TestSharedSnapshotExcludesLocalSchedulerTimingAndComparesGameTime(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0xD165E58, 120, 40)
	b := mustHeadless(t, 0xD165E58, 120, 40)
	defer a.Close()
	defer b.Close()
	tickUntilCursor(t, a)
	tickUntilCursor(t, b)

	a.World().Resources.Status.Ints.Get("engine.tick_slips").Store(3)
	assertSharedParity(t, a, b, 0)

	// The elapsed-game-time half is the regression for the 2026-08-31 kinetics
	// divergence. Game time was read from each process's pacing clock, so every
	// shared reader that measures now.Sub(stored) — the quasar's speed step is the
	// one that diverged — crossed its threshold on a different tick per instance,
	// and nothing compared the clock itself.
	elapsed := func(x *App) int64 {
		return x.World().Resources.Status.Ints.Get("time.game_elapsed_ms").Load()
	}
	if elapsed(a) != elapsed(b) {
		t.Fatalf("elapsed game time %d vs %d at the same tick", elapsed(a), elapsed(b))
	}
	if want := int64(a.World().Resources.Game.State.GetGameTicks()) *
		parameter.GameUpdateInterval.Milliseconds(); elapsed(a) != want {
		t.Fatalf("elapsed game time %d, want tick * interval = %d", elapsed(a), want)
	}

	// The key is inside the compared surface, so a clock that drifts back onto the
	// wall fails here rather than surfacing as a kinetics digest mismatch minutes in.
	a.World().Resources.Status.Ints.Get("time.game_elapsed_ms").Store(17_000)
	if !snapshot.SharedKey("time.game_elapsed_ms") {
		t.Fatal("time.game_elapsed_ms is excluded from the shared surface again")
	}
	if _, _, _, differs := snapshot.FirstDiff(a.SnapshotShared(), b.SnapshotShared()); !differs {
		t.Fatal("a forged elapsed game time did not move the shared snapshot")
	}
}
