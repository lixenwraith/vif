package app

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/lifecycle"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

// lifetimeServer is probeServer with a lifetime policy: a dedicated host that has
// been allocated on somebody's behalf rather than started by hand.
func lifetimeServer(t *testing.T, players int, p lifecycle.Policy) *App {
	t.Helper()
	a, err := New(Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0", Participants: players,
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true},
		Seed: 0x11FE, Lifetime: p,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

// seatGuests replaces the roster with the coordinator and n guests, the way an
// accepted handshake would have.
func seatGuests(a *App, n int) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessionRoster = []network.SessionParticipant{
		{ID: hostParticipantID, Slot: parameter.NoPlayerSlot},
	}
	for i := range n {
		a.sessionRoster = append(a.sessionRoster,
			network.SessionParticipant{ID: network.PeerID(i + 2), Slot: uint8(i)})
	}
}

// TestAnUnclaimedSessionEndsItsOwnLobby is the whole reason the first-guest window
// exists. A session created on a player's behalf that the player never reaches must
// cost the fleet one window rather than one pod: nothing else is watching, and the
// lobby is a wait with no other end.
//
// It ends cleanly rather than as a failure. The run did what it was told to do with
// a slot nobody claimed, and a supervisor reading a non-zero exit would restart it.
func TestAnUnclaimedSessionEndsItsOwnLobby(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0",
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
	a := lifetimeServer(t, 2, lifecycle.Policy{})
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

// TestDrainingRefusesDialsAndReadiness is what makes a drain a drain rather than a
// slower shutdown: the session stops being routed to and stops allocating, while
// the guests it already holds keep playing.
func TestDrainingRefusesDialsAndReadiness(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{Empty: time.Minute, Drain: time.Minute})
	a.life.Start(time.Now())
	seatGuests(a, 1)

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
	_, err := a.assignParticipant()
	if !errors.Is(err, ErrSessionEnding) {
		t.Fatalf("a draining session answered a dial with %v, want ErrSessionEnding", err)
	}
	if errors.Is(err, ErrSessionStarting) {
		t.Fatal("a draining session told the dialer to retry")
	}
}

// TestCapacityAndDrainAreDistinctRefusals keeps a full session from reading as a
// draining one. A Service that confused them would put a draining pod back into
// rotation as soon as a guest left.
func TestCapacityAndDrainAreDistinctRefusals(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 1, lifecycle.Policy{Empty: time.Minute, Drain: time.Minute})
	a.life.Start(time.Now())
	seatGuests(a, 1)

	full := a.probeSnapshot()
	if full.Ready || full.Reason != "session at capacity" {
		t.Fatalf("a full session reported ready=%v reason=%q", full.Ready, full.Reason)
	}
	if _, err := a.assignParticipant(); errors.Is(err, ErrSessionEnding) {
		t.Fatal("a full session refused as if it were ending")
	}

	a.interrupt(time.Now(), "signal terminated")
	draining := a.probeSnapshot()
	if draining.Reason == "session at capacity" {
		t.Fatal("a draining session still reports itself merely full")
	}
}

// TestASignalReadsTheRosterItArrivesWith covers the stale-observation hazard: the
// loop folds the roster in once a second, so a signal landing just after a guest
// connected must read that guest rather than the loop's last empty reading — and a
// drain that read the stale one would end a session somebody had only just joined.
func TestASignalReadsTheRosterItArrivesWith(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: time.Minute, Drain: time.Minute})
	now := time.Now()
	a.life.Start(now)
	a.life.Observe(a.guestCount(), now) // the loop's last tick: nobody had arrived

	seatGuests(a, 1) // the guest connects between two ticks
	st := a.interrupt(now.Add(100*time.Millisecond), "signal terminated")
	if st.Phase != lifecycle.PhaseDraining {
		t.Fatalf("phase = %s, want draining; the signal read a stale empty roster", st.Phase)
	}
	if st.Guests != 1 {
		t.Fatalf("the drain saw %d guests, want the one that had just joined", st.Guests)
	}
}

// TestOnlyTheLobbyWaitCarriesTheFirstGuestDeadline is the boundary between the two
// gates a session start runs.
//
// The lobby is inside the first-guest window and must end when it closes. The ready
// gate that follows is not: its guest has already arrived, and re-arming the window
// there would end a session because installing the world took the last second of
// it. The deadline is therefore the caller's argument rather than a policy read
// inside the wait.
func TestOnlyTheLobbyWaitCarriesTheFirstGuestDeadline(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{FirstJoin: 80 * time.Millisecond})
	a.life.Start(time.Now())

	port := network.NewSocketPort(network.DebugConfig(network.RoleHost, "127.0.0.1:0"))
	defer port.Close()

	// The lobby's gate: a deadline that passes ends it, cleanly.
	never := func() bool { return false }
	err := a.waitForStartup(port, nil, 1, false, a.life.State(time.Now()).Deadline, never)
	if !errors.Is(err, errSessionExpired) {
		t.Fatalf("the lobby wait returned %v, want errSessionExpired", err)
	}

	// The ready gate: no deadline, so the same expired policy does not end it. It
	// is left waiting on its own readiness condition and ended by a signal, which
	// is the only thing that should be able to end it here.
	signals := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- a.waitForStartup(port, signals, 1, false, time.Time{}, never) }()

	// Well past the first-guest window, which the ready gate must not inherit.
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("the ready gate ended on its own with %v; it inherited a deadline", err)
	default:
	}

	signals <- os.Interrupt
	select {
	case err := <-done:
		if !errors.Is(err, errSessionCanceled) {
			t.Fatalf("the ready gate ended with %v, want errSessionCanceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the ready gate ignored its signal")
	}
}

// TestGuestCountExcludesTheCoordinator pins the reading both the probe and the
// lifetime policy take: a roster holding only its cursorless coordinator is empty,
// and a grace period that counted it would never elapse.
func TestGuestCountExcludesTheCoordinator(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{Empty: time.Minute})

	if got := a.guestCount(); got != 0 {
		t.Fatalf("an unopened roster holds %d guests, want 0", got)
	}
	seatGuests(a, 0)
	if got := a.guestCount(); got != 0 {
		t.Fatalf("a coordinator-only roster holds %d guests, want 0", got)
	}
	seatGuests(a, 3)
	if got := a.guestCount(); got != 3 {
		t.Fatalf("a roster of three guests holds %d", got)
	}
}

// TestVacancyGraceEndsAnEmptiedSession runs the observation path the serve loop
// runs, at the resolution the policy is written in rather than in real seconds.
func TestVacancyGraceEndsAnEmptiedSession(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: 90 * time.Second})
	now := time.Now()
	a.life.Start(now)

	seatGuests(a, 2)
	if st := a.life.Observe(a.guestCount(), now.Add(time.Second)); st.Phase != lifecycle.PhaseOccupied {
		t.Fatalf("phase = %s with two guests, want occupied", st.Phase)
	}

	left := now.Add(10 * time.Minute)
	seatGuests(a, 0)
	st := a.life.Observe(a.guestCount(), left)
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

// TestLifetimeBoundsAreRefusedOutsideADedicatedHost keeps a flag that ends a
// process from silently doing nothing in a mode that has no allocator.
func TestLifetimeBoundsAreRefusedOutsideADedicatedHost(t *testing.T) {
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
		Mode: ModeServer, HostAddress: ":7777", Width: 120, Height: 40,
		Resources: resource.Options{Embedded: true},
		Lifetime:  lifecycle.Policy{FirstJoin: -time.Second},
	}
	if err := negative.Validate(); err == nil {
		t.Fatal("a negative lifetime bound was accepted")
	}
}

// TestAStagingWorldIsNotAnAllocatedSession covers the config a correction builds
// its second world from. It inherits this instance's configuration, and a probe
// address or a lifetime bound carried into a non-serving mode is refused outright —
// which would make a dedicated host unable to stage a capture at all.
func TestAStagingWorldIsNotAnAllocatedSession(t *testing.T) {
	t.Parallel()
	a := lifetimeServer(t, 4, lifecycle.Policy{FirstJoin: time.Minute, Empty: time.Minute})
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
