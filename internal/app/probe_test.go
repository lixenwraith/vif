package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

func probeServer(t *testing.T, players int) *App {
	t.Helper()
	a, err := New(Config{
		Mode: ModeServer, HostAddress: "127.0.0.1:0", Participants: players,
		Width: 120, Height: 40, Resources: resource.Options{Embedded: true}, Seed: 0xB0BE,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

// TestReadinessTracksWhetherADialWouldBeAdmitted pins what readiness means, which
// is not what liveness means and not "the session has started".
//
// A lobby waiting for its first guest is ready: being dialled is what it is
// waiting for. A session at capacity is not, because a Service that kept routing
// to it would be sending participants to a roster with no room — which they would
// discover only after the connect and a slot allocation.
func TestReadinessTracksWhetherADialWouldBeAdmitted(t *testing.T) {
	t.Parallel()
	a := probeServer(t, 2)

	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("an empty lobby is not ready: %s", snap.Reason)
	}

	a.sessionMu.Lock()
	a.sessionRoster = []network.SessionParticipant{
		{ID: hostParticipantID, Slot: parameter.NoPlayerSlot},
		{ID: 2, Slot: 0},
	}
	a.sessionMu.Unlock()
	if snap := a.probeSnapshot(); !snap.Ready {
		t.Fatalf("a lobby with room is not ready: %s", snap.Reason)
	}

	a.sessionMu.Lock()
	a.sessionRoster = append(a.sessionRoster, network.SessionParticipant{ID: 3, Slot: 1})
	a.sessionMu.Unlock()
	snap := a.probeSnapshot()
	if snap.Ready {
		t.Fatal("a session at capacity reported itself ready")
	}
	if !snap.Live {
		t.Fatal("a full session reported itself dead; capacity is not a fault")
	}

	// The closing window is the other refusal: neither gate can serve a dial
	// there, so nothing may be routed into it.
	a.sessionRoster = a.sessionRoster[:2]
	a.lobbyClosing.Store(true)
	if snap := a.probeSnapshot(); snap.Ready {
		t.Fatal("the lobby reported itself ready while closing")
	}
}

// TestLivenessDistinguishesAStalledClockFromOneThatHasNotStarted is the whole of
// what the stall detector is for. A lobby has not released tick zero, and
// restarting a pod for that would restart it forever.
func TestLivenessDistinguishesAStalledClockFromOneThatHasNotStarted(t *testing.T) {
	t.Parallel()
	a := probeServer(t, 2)
	now := time.Now()

	live, reason := a.observeTick(0, now, false, false)
	if !live || reason != "clock not running" {
		t.Fatalf("an unstarted clock reported live=%v %q", live, reason)
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
	live, reason = a.observeTick(10, now.Add(2*parameter.ProbeStallInterval), true, false)
	if live || reason != "tick stalled" {
		t.Fatalf("a stalled clock reported live=%v %q", live, reason)
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
	a := probeServer(t, 2)
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

// TestAProbeAddressIsRefusedWhereNothingWouldAnswer keeps the flag honest: the
// codebase refuses a setting a mode cannot honour rather than accepting one that
// silently does nothing.
func TestAProbeAddressIsRefusedWhereNothingWouldAnswer(t *testing.T) {
	t.Parallel()
	cfg := Config{Mode: ModeHeadless, Width: 80, Height: 24, ProbeAddress: "127.0.0.1:0"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a driven run accepted a probe address it would never bind")
	}
}
