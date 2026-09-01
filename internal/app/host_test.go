package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// joinTestTickInterval paces the host through a join in this harness. See the
// comment at its use.
const joinTestTickInterval = 10 * time.Millisecond

// TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun is Phase 3's criterion, over a
// real socket and at a tick that is not zero.
//
// Everything the phase claims is in this one path. A run that was solo opens a
// socket without restarting. A participant dials it hundreds of ticks in, receives
// the world instead of re-deriving it, and installs it. The crossings the host
// produced while that was happening reach the joiner rather than falling into the
// gap, because the joiner was admitted before the world was read for it. The joiner
// closes the remaining tick gap by simulating it. Its cursor is created by a
// crossing, at one agreed tick, on both instances. And the two then hold the same
// shared world.
func TestSoloRunBecomesAHostAndAdmitsAParticipantMidRun(t *testing.T) {
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
	// assertions would move the very thing they compare. Phase 4's own criteria are
	// where a correction is the subject.
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

// TestAReconnectIsTheSameJoin covers Phase 3's fourth requirement, which is a
// requirement about there being no fourth mechanism.
//
// A participant that drops leaves a departure crossing behind, and the coordinator
// returns its identity to the pool. What comes back is a new dial: the same
// acceptor, the same identity allocation, the same capture at whatever tick the
// host has now reached, the same install, the same arrival crossing. Nothing here
// is reconnect-specific, and that is the whole claim — so the test is the join test
// run twice against one host, with a disconnect in between, asserting the second
// arrival lands on a world the host has moved well past since the first.
func TestAReconnectIsTheSameJoin(t *testing.T) {
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
