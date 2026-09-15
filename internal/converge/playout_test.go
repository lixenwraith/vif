// The correction playout buffer: when an arriving authority is installed.

package converge

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// receiver is a guest of a two-participant session, which is the side that
// installs.
func receiver(t *testing.T) *run {
	t.Helper()
	return session(t, 2, [][2]int{{1, 2}})[1]
}

// deliverBody hands one whole correction to a receiver the way a reassembled
// chunk stream does.
func deliverBody(t *testing.T, r *run, cap snapshot.SharedCapture) {
	t.Helper()
	body, err := snapshot.EncodeCorrection(cap)
	if err != nil {
		t.Fatalf("correction encode: %v", err)
	}
	r.c.Receive(body)
	r.c.Apply()
}

// TestACorrectionWaitsForTheTickItDescribes is the buffer's whole rule. Installing
// adopts the authority tick, so a correction ahead of this instance's clock would
// step it forward and the next one behind it would step it back — which is a
// shared actor moving that many ticks in each direction. It waits instead.
func TestACorrectionWaitsForTheTickItDescribes(t *testing.T) {
	t.Parallel()
	guest := receiver(t)
	ahead := capture(4, 0)

	deliverBody(t, guest, ahead)
	if n := guest.world.installs(); n != 0 {
		t.Fatalf("a correction three ticks ahead of the clock installed %d times", n)
	}
	if got := guest.stat("snapshot.corrections_held"); got != 1 {
		t.Fatalf("the buffer reported %d deferrals, want 1", got)
	}

	guest.world.advance(3)
	guest.c.Apply()
	if n := guest.world.installs(); n != 1 {
		t.Fatalf("the clock reached tick 4 and the correction installed %d times", n)
	}
	if got := guest.world.Position().Tick; got != 4 {
		t.Fatalf("the installed world is at tick %d, want the authority's 4", got)
	}
}

// TestASecondWaitingCorrectionTakesTheStep bounds the buffer. A second correction
// arriving while the first is still ahead says the clock is not catching up, so
// continuing to wait would defer every correction for the rest of the session.
func TestASecondWaitingCorrectionTakesTheStep(t *testing.T) {
	t.Parallel()
	guest := receiver(t)

	deliverBody(t, guest, capture(4, 0))
	if n := guest.world.installs(); n != 0 {
		t.Fatalf("the first correction installed %d times before its tick", n)
	}
	deliverBody(t, guest, capture(5, 0))
	if n := guest.world.installs(); n != 1 {
		t.Fatalf("the second correction left %d installs, want the step taken", n)
	}
	if got := guest.world.Position().Tick; got != 5 {
		t.Fatalf("the step left the clock at tick %d, want the newer authority's 5", got)
	}
}
