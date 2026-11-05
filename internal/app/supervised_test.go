package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	link "github.com/lixenwraith/vif/pkg/network"
)

// supervisedServer is a dedicated host with a probe and, optionally, the lifetime
// bounds an allocated session carries. The zero policy is the host an operator
// starts by hand: supervised, but ended by the person who started it.
func supervisedServer(t *testing.T, players int, p lifecycle.Policy) *App {
	t.Helper()
	a, err := New(Config{
		Mode: ModeServer, Membership: Membership{HostAddress: "127.0.0.1:0", Participants: players},
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Seed: 0x11FE, Lifetime: p,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

// supervisedLobby is supervisedServer serving its lobby, as Serve does once its
// probe is up: guests dial the address returned.
func supervisedLobby(t *testing.T, players int, p lifecycle.Policy) (*App, string) {
	t.Helper()
	a, addr, _ := lobbyHost(t, Config{
		Mode: ModeServer, Membership: Membership{Participants: players},
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Seed: 0x11FE, Lifetime: p,
	})
	return a, addr
}

// seatedDial is a dial its host has seated and that has not answered its offer, as
// a joiner building its world has not: its place is in the roster until it answers,
// goes, or the handshake's bound runs out, and no lobby closes on it meanwhile.
func seatedDial(t *testing.T, addr string) (*network.PendingJoin, network.SessionOffer) {
	t.Helper()
	pending, offered := dialSession(t, addr)
	t.Cleanup(func() { _ = pending.Close() })
	return pending, offered
}

// leaveLobby closes the dials named and waits for their host to have released their
// places, guests left in its roster.
func leaveLobby(t *testing.T, a *App, left int, dials ...*network.PendingJoin) {
	t.Helper()
	for _, d := range dials {
		_ = d.Close()
	}
	waitUntil(t, func() bool { return a.membership.GuestCount() == left }, "the dials' places to be released")
}

// TestAnUnclaimedSessionEndsItsOwnLobby is the whole reason the first-guest window
// exists: a session the player never reaches must cost the fleet one window rather
// than one pod. It ends cleanly rather than as a failure — the run did what it was
// told to do with a slot nobody claimed, and a supervisor reading a non-zero exit
// would restart it.
func TestAnUnclaimedSessionEndsItsOwnLobby(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Mode: ModeServer, Membership: Membership{HostAddress: "127.0.0.1:0"},
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Seed: 0x11FF, Lifetime: lifecycle.Policy{FirstJoin: 150 * time.Millisecond},
	}
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- RunServer(cfg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an expired lobby reported a failure: %v", err)
		}
		if waited := time.Since(started); waited < 150*time.Millisecond {
			t.Fatalf("the lobby ended after %s, before its own window closed", waited)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a lobby nobody dialled never ended itself")
	}
}

// TestAnUnboundedLobbyKeepsWaiting is the other half: an interactively started
// host is supervised by the person who started it, and nothing here may end it.
func TestAnUnboundedLobbyKeepsWaiting(t *testing.T) {
	t.Parallel()
	a := supervisedServer(t, 2, lifecycle.Policy{})
	a.life.Start(time.Now())

	st := a.life.State(time.Now().Add(72 * time.Hour))
	if st.Expired {
		t.Fatalf("an unbounded lobby expired: %q", st.Reason)
	}
	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("an unbounded lobby is not ready: %s", snap.Reason)
	}
	if snap := a.probeSnapshot(); snap.Detail["phase"] != "waiting" {
		t.Fatalf("probe phase = %q, want waiting", snap.Detail["phase"])
	}
}

// TestTheProbeNamesItsBuildAsARefusalWould: the session list read from the probe
// says which build each session runs in the words a refusal of another build uses,
// so a page can be told before it dials.
func TestTheProbeNamesItsBuildAsARefusalWould(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 2, lifecycle.Policy{})
	// The offer is what a dial is checked against, and refused in the terms of.
	_, offered := seatedDial(t, addr)
	if got, want := a.probeSnapshot().Detail["build"], offered.Identity.Build; got == "" || got != want {
		t.Fatalf("the probe names build %q; a refusal names %q", got, want)
	}
}

// TestWhatTheProbeReportsAboutAdmission pins the one field that is not the status
// code. Readiness is "would a dial be admitted", which is neither liveness nor "the
// session has started". The refusals stay distinguishable: a full session and a
// draining one are both unready and mean opposite things, and an allocator reading
// them the same way would keep choosing a session on its way out.
func TestWhatTheProbeReportsAboutAdmission(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 2, lifecycle.Policy{Empty: time.Minute, Drain: time.Minute})

	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("an empty lobby is not ready: %s", snap.Reason)
	}
	first, offered := seatedDial(t, addr)
	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("a lobby with room is not ready: %s", snap.Reason)
	}

	second, _ := seatedDial(t, addr)
	full := a.probeSnapshot()
	if full.Ready || full.Reason != "session at capacity" {
		t.Fatalf("a full session reported ready=%v reason=%q", full.Ready, full.Reason)
	}
	if !full.Live {
		t.Fatal("a full session reported itself dead; capacity is not a fault")
	}
	if _, _, err := network.DialSession(addr, network.DebugConfig(link.RolePeer, "")); !errors.Is(err, &network.Refusal{Code: network.RefusalFull}) {
		t.Fatalf("a full session answered a dial with %v, want the refusal a full one gives", err)
	}

	// The closing window is a third refusal: neither gate can serve a dial there. A
	// server's lobby closes on its first guest to answer.
	leaveLobby(t, a, 1, second)
	if err := first.Complete(nil, network.JoinerReport{Identity: offered.Identity}); err != nil {
		t.Fatalf("the first guest's reply: %v", err)
	}
	waitUntil(t, func() bool { return a.probeSnapshot().Reason == "lobby closing" }, "the lobby to close")
	if snap := a.probeSnapshot(); snap.Ready {
		t.Fatalf("the closing lobby reported ready=%v reason=%q", snap.Ready, snap.Reason)
	}

	// And a session on its way out outranks all of them.
	a.interrupt(time.Now(), "signal terminated")
	draining := a.probeSnapshot()
	if draining.Ready || draining.Reason == "session at capacity" || draining.Reason == "lobby closing" {
		t.Fatalf("a draining session reported ready=%v reason=%q", draining.Ready, draining.Reason)
	}
}

// TestTheProbeReportsTheMapOnceTheRunTicks: the fleet lists each session's map,
// which only the run knows once its scenario, or its first guest, has sized it.
func TestTheProbeReportsTheMapOnceTheRunTicks(t *testing.T) {
	t.Parallel()
	a := supervisedServer(t, 2, lifecycle.Policy{})
	if m, ok := a.probeSnapshot().Detail["map"]; ok {
		t.Fatalf("a run that has not ticked reported map %q", m)
	}
	a.Tick(1)
	w, h := mapSize(a)
	if got, want := a.probeSnapshot().Detail["map"], fmt.Sprintf("%dx%d", w, h); got != want {
		t.Fatalf("probe reported map %q, the run holds %s", got, want)
	}
}

// TestDrainingRefusesDialsAndReadiness is what makes a drain a drain rather than a
// slower shutdown: the session stops being routed to and stops allocating, while
// the guests it already holds keep playing.
func TestDrainingRefusesDialsAndReadiness(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 4, lifecycle.Policy{Empty: time.Minute, Drain: time.Minute})
	seatedDial(t, addr)

	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("a session with room is not ready: %s", snap.Reason)
	}

	st := a.interrupt(time.Now(), "signal terminated")
	if st.Phase != lifecycle.PhaseDraining {
		t.Fatalf("phase = %s after one signal, want draining", st.Phase)
	}

	snap := a.probeSnapshot()
	if snap.Ready {
		t.Fatal("a draining session reported itself ready")
	}
	if !snap.Live {
		t.Fatal("a draining session reported itself dead; a drain is not a fault")
	}
	if snap.Detail["phase"] != "draining" {
		t.Fatalf("probe phase = %q, want draining", snap.Detail["phase"])
	}
	if snap.Detail["expires_in"] == "" {
		t.Fatal("a draining session published no deadline for its allocator to read")
	}

	// The refusal has to be distinguishable from the starting one: this session is
	// leaving, so retrying against it is exactly the wrong answer.
	_, _, err := network.DialSession(addr, network.DebugConfig(link.RolePeer, ""))
	if !errors.Is(err, &network.Refusal{Code: network.RefusalEnding}) {
		t.Fatalf("a draining session answered a dial with %v, want the refusal of a session ending", err)
	}
	if errors.Is(err, &network.Refusal{Code: network.RefusalStarting}) {
		t.Fatal("a draining session told the dialer to retry")
	}
}

// TestASignalReadsTheRosterItArrivesWith covers the stale-observation hazard: the
// loop folds the roster in once a second, so a signal landing just after a guest
// connected must read that guest rather than the loop's last empty reading — and a
// drain that read the stale one would end a session somebody had only just joined.
func TestASignalReadsTheRosterItArrivesWith(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: time.Minute, Drain: time.Minute})
	now := time.Now()
	a.life.Observe(a.membership.GuestCount(), now) // the loop's last tick: nobody had arrived

	seatedDial(t, addr) // the guest connects between two ticks
	st := a.interrupt(now.Add(100*time.Millisecond), "signal terminated")
	if st.Phase != lifecycle.PhaseDraining {
		t.Fatalf("phase = %s, want draining; the signal read a stale empty roster", st.Phase)
	}
	if st.Guests != 1 {
		t.Fatalf("the drain saw %d guests, want the one that had just joined", st.Guests)
	}
}

// TestAGuestThatLeavesBeforeConfirmingDoesNotEndTheSession is the abuse bound on an
// open port: the gate waits on whoever is still linked, so a peer that dials and
// drops costs the session its lobby rather than its life.
func TestAGuestThatLeavesBeforeConfirmingDoesNotEndTheSession(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr, started := lobbyHost(t, Config{
		Mode: ModeServer, Membership: Membership{Participants: 4},
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Seed: 0x11FE, Lifetime: lifecycle.Policy{FirstJoin: 30 * time.Second, Empty: time.Minute},
	})

	// A guest up to the point of the abuse: the coordinator verifies what a joiner
	// turned out to be, so only one that answers as having built its world gets into
	// a roster.
	pending, offered := rawJoiner(t, addr, network.JoinerReport{Width: 120, Height: 40})
	// The gate is open once the start arrives; leaving without confirming is the
	// whole of the abuse.
	if _, err := pending.WaitStart(); err != nil {
		t.Fatalf("the host never released the lobby: %v", err)
	}
	_ = pending.Close()

	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("the abandoned start gate ended the session with %v", err)
		}
	case <-time.After(socketWait):
		t.Fatalf("the start gate is still waiting for participant %d", offered.Assigned)
	}
	if st := a.life.State(time.Now()); st.Expired {
		t.Fatalf("the session expired on an abandoned lobby: %s", st.Reason)
	}
}

// TestGuestCountExcludesTheCoordinator pins the reading both the probe and the
// lifetime policy take: a roster holding only its cursorless coordinator is empty,
// and a grace period that counted it would never elapse.
func TestGuestCountExcludesTheCoordinator(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 4, lifecycle.Policy{Empty: time.Minute})

	if got := a.membership.GuestCount(); got != 0 {
		t.Fatalf("an unopened roster holds %d guests, want 0", got)
	}
	// A dial opens the roster on the coordinator's own entry, which its leaving
	// leaves behind.
	gone, _ := seatedDial(t, addr)
	leaveLobby(t, a, 0, gone)
	for range 3 {
		seatedDial(t, addr)
	}
	if got := a.membership.GuestCount(); got != 3 {
		t.Fatalf("a roster of three guests holds %d", got)
	}
}

// TestVacancyGraceEndsAnEmptiedSession runs the observation path the serve loop
// runs, at the resolution the policy is written in rather than in real seconds.
func TestVacancyGraceEndsAnEmptiedSession(t *testing.T) {
	// Not parallel: this binds a real socket.
	a, addr := supervisedLobby(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: 90 * time.Second})
	now := time.Now()

	one, _ := seatedDial(t, addr)
	two, _ := seatedDial(t, addr)
	if st := a.life.Observe(a.membership.GuestCount(), now.Add(time.Second)); st.Phase != lifecycle.PhaseOccupied {
		t.Fatalf("phase = %s with two guests, want occupied", st.Phase)
	}

	left := now.Add(10 * time.Minute)
	leaveLobby(t, a, 0, one, two)
	st := a.life.Observe(a.membership.GuestCount(), left)
	if st.Phase != lifecycle.PhaseVacant {
		t.Fatalf("phase = %s after the last guest left, want vacant", st.Phase)
	}
	// Still ready: a session inside its grace is exactly the one a returning
	// participant dials back into.
	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("a session inside its grace is not ready: %s", snap.Reason)
	}
	if st = a.life.Observe(0, left.Add(89*time.Second)); st.Expired {
		t.Fatal("the session ended inside its own grace")
	}
	if st = a.life.Observe(0, left.Add(90*time.Second)); !st.Expired {
		t.Fatalf("phase = %s at the end of the grace, want expired", st.Phase)
	}
	if snap := a.probeSnapshot(); snap.Ready {
		t.Fatal("an expired session reported itself ready")
	}
}

// TestSupervisorFlagsAreRefusedWhereNothingWouldHonourThem keeps -probe and the
// lifetime bounds honest: a mode with no supervisor to answer and nothing to end it
// refuses them rather than accepting a flag that silently does nothing — and, for
// the bounds, one that was meant to end a process.
func TestSupervisorFlagsAreRefusedWhereNothingWouldHonourThem(t *testing.T) {
	t.Parallel()
	for name, mode := range map[string]Mode{
		"play":     ModePlay,
		"headless": ModeHeadless,
		"script":   ModeScript,
	} {
		cfg := Config{
			Mode: mode, Width: 120, Height: 40,
			Resources: resource.Options{Embedded: true},
			Lifetime:  lifecycle.Policy{Empty: 90 * time.Second},
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("%s: a session lifetime bound was accepted", name)
		}
	}
	negative := Config{
		Mode: ModeServer, Membership: Membership{HostAddress: ":7777"}, Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true},
		Lifetime:  lifecycle.Policy{FirstJoin: -time.Second},
	}
	if err := negative.Validate(); err == nil {
		t.Fatal("a negative lifetime bound was accepted")
	}

	probeless := Config{Mode: ModeHeadless, Width: 80, Height: 24, ProbeAddress: "127.0.0.1:0"}
	if err := probeless.Validate(); err == nil {
		t.Fatal("a driven run accepted a probe address it would never bind")
	}
}

// TestAStagingWorldIsNotAnAllocatedSession covers the config a correction builds
// its second world from. It inherits this instance's configuration, and a probe
// address or a lifetime bound carried into a non-serving mode is refused outright —
// which would make a dedicated host unable to stage a capture at all.
func TestAStagingWorldIsNotAnAllocatedSession(t *testing.T) {
	t.Parallel()
	a := supervisedServer(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: time.Minute})
	a.cfg.ProbeAddress = "127.0.0.1:0"

	tickUntilCursor(t, a)
	cap, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	stage, err := a.newStagingApp(cap)
	if err != nil {
		t.Fatalf("a dedicated host could not stage its own capture: %v", err)
	}
	defer stage.Close()

	if stage.cfg.Lifetime.Bounded() {
		t.Fatal("the staging world inherited a lifetime that could end the live run")
	}
	if stage.cfg.ProbeAddress != "" {
		t.Fatal("the staging world inherited a probe address")
	}
}

// TestLivenessDistinguishesAStalledClockFromOneThatHasNotStarted is the whole of
// what the stall detector is for. A lobby has not released tick zero, and
// restarting a pod for that would restart it forever.
func TestLivenessDistinguishesAStalledClockFromOneThatHasNotStarted(t *testing.T) {
	t.Parallel()
	a := supervisedServer(t, 2, lifecycle.Policy{})
	now := time.Now()

	live, clock := a.observeTick(0, now, false, false)
	if !live || clock != "stopped" {
		t.Fatalf("an unstarted clock reported live=%v %q", live, clock)
	}

	// Running and moving.
	if live, _ := a.observeTick(10, now, true, false); !live {
		t.Fatal("an advancing clock reported dead")
	}
	// Running, not moving, but not yet past the threshold.
	if live, _ := a.observeTick(10, now.Add(parameter.ProbeStallInterval/2), true, false); !live {
		t.Fatal("a clock reported dead before the stall threshold")
	}
	// Past it.
	live, clock = a.observeTick(10, now.Add(2*parameter.ProbeStallInterval), true, false)
	if live || clock != "stalled" {
		t.Fatalf("a stalled clock reported live=%v %q", live, clock)
	}
	// Moving again clears it, so one slow moment does not condemn the run.
	if live, _ := a.observeTick(11, now.Add(3*parameter.ProbeStallInterval), true, false); !live {
		t.Fatal("a clock that resumed stayed dead")
	}
}

// TestAPausedClockIsNotAStall: pause is an operator state, not a fault, and the
// stall clock must not accumulate under one or the run would be condemned for
// having been paused long enough.
func TestAPausedClockIsNotAStall(t *testing.T) {
	t.Parallel()
	a := supervisedServer(t, 2, lifecycle.Policy{})
	now := time.Now()

	a.observeTick(5, now, true, false)
	for i := range 5 {
		at := now.Add(time.Duration(i+1) * parameter.ProbeStallInterval)
		if live, reason := a.observeTick(5, at, true, true); !live || reason != "paused" {
			t.Fatalf("a paused clock reported live=%v %q", live, reason)
		}
	}
	// And the pause left no debt behind: the first unpaused read starts its own
	// window rather than inheriting the one the pause spanned.
	if live, _ := a.observeTick(5, now.Add(6*parameter.ProbeStallInterval), true, false); !live {
		t.Fatal("the run was condemned for time it spent paused")
	}
}
