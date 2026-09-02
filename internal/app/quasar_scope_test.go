package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// The quasar is fused from one cursor's drains, and its two standing effects — the
// grayout and the drain pause — belong to that cursor.
//
// The region that raises them does not: it is shared, so every instance runs the
// same machine, enters QuasarFuse and executes the same on_enter actions. Before
// the scope payload that made one participant's quasar darken every participant's
// screen and stop every participant's drains. The unit tests in internal/system pin
// the two handlers; this one drives the whole path — a shared drain defeat, the
// MainEscalate capture, the spawned region, the emitted effects, the region's end —
// across two linked instances, which is the only place the fan-out was visible.

// TestAQuasarsEffectsReachOnlyTheCursorItWasFusedFrom is the reported defect.
func TestAQuasarsEffectsReachOnlyTheCursorItWasFusedFrom(t *testing.T) {
	apps := meshSession(t, 0xA6A6, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	spawns := make([]int, len(apps))
	for i, a := range apps {
		if grayedOut(a) || drainsPaused(a) {
			t.Fatalf("participant %d starts greyed out or paused", i+1)
		}
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventQuasarSpawnRequest {
				spawns[i]++
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}

	// Participant 2's cursor takes the tenth shared drain. The crossing reaches
	// both FSMs and both take MainEscalate, which captures the causal cursor as
	// fuse_owner and spawns the quasar region from it.
	apps[1].Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: local[1]})
	apps[1].Settle()

	// Far enough in that the barrier has delivered the crossing to the peer and
	// both machines are inside the region, and short of the gold cycle ending it.
	for range 12 {
		tickAll(apps)
	}
	for i, a := range apps {
		if quasarState(a) == "-" {
			t.Fatalf("the quasar region is not running on participant %d", i+1)
		}
		// Exactly the participant the region names, on both halves of the effect.
		want := i == 1
		if got := grayedOut(a); got != want {
			t.Fatalf("participant %d grayout = %v, want %v", i+1, got, want)
		}
		if got := drainsPaused(a); got != want {
			t.Fatalf("participant %d drain pause = %v, want %v", i+1, got, want)
		}
	}

	// The region ends, and the owner's effects end with it: a scoped hold that
	// nothing released would stop that participant's drains for the rest of the run.
	for range 24 {
		tickAll(apps)
	}
	for i, a := range apps {
		if s := quasarState(a); s != "-" {
			t.Fatalf("participant %d is still in the quasar region (%s)", i+1, s)
		}
		if grayedOut(a) || drainsPaused(a) {
			t.Fatalf("participant %d kept the quasar's effects after it ended: grayout=%v paused=%v",
				i+1, grayedOut(a), drainsPaused(a))
		}
	}

	// The shared half is unchanged: one logical fusion producing one spawn request
	// on each instance, not one per participant.
	//
	// Full snapshot parity is not the assertion here. Both machines run the region
	// and both leave it, but they enter it a barrier apart, so the states they hold
	// afterwards differ by that lead in elapsed time — a property of the delivery
	// lead rather than of the scope this test is about.
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// quasarState is the region's current state name, "-" while it is not running.
func quasarState(a *App) (state string) {
	a.World().RunSafe(func() {
		state = a.World().Resources.Status.Strings.Get("fsm.quasar.state").Load()
	})
	return state
}

// grayedOut reads the overlay resource the transient system owns, rather than the
// telemetry key beside it, so the assertion is on the effect and not its report.
func grayedOut(a *App) (active bool) {
	a.World().RunSafe(func() { active = a.World().Resources.View.Grayout.Active })
	return active
}

// drainsPaused reads the drain system's published hold. The system itself is not
// reachable from here, and the key is what an operator watching a stalled session
// reads too.
func drainsPaused(a *App) (paused bool) {
	a.World().RunSafe(func() {
		paused = a.World().Resources.Status.Bools.Get("drain.paused").Load()
	})
	return paused
}
