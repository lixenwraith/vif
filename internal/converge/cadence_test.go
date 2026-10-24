// The seam between the controller's contract and the game's numbers.

package converge

import (
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
)

// TestCadenceBoundsAreTheGameParameters: pkg may not see internal, so the envelope
// is assembled here and a parameter that drifts out of the controller's declared
// order is caught here.
func TestCadenceBoundsAreTheGameParameters(t *testing.T) {
	t.Parallel()
	b := cadenceBounds()
	if err := b.Validate(); err != nil {
		t.Fatalf("the shipped envelope does not validate: %v", err)
	}
	if b.NominalCadenceTicks != parameter.SnapshotCorrectionTicks {
		t.Fatalf("the nominal cadence is %d, the parameter says %d",
			b.NominalCadenceTicks, parameter.SnapshotCorrectionTicks)
	}
	if b.FloorKeyframeTicks != parameter.SnapshotFloorKeyframeTicks {
		t.Fatalf("the floor is %d ticks, the parameter says %d",
			b.FloorKeyframeTicks, parameter.SnapshotFloorKeyframeTicks)
	}
	// The nominal point must itself honour the floor, or a healthy session would
	// start out already reporting a constrained link.
	if got := b.NominalCadenceTicks * uint64(b.NominalKeyframe); got > b.FloorKeyframeTicks {
		t.Fatalf("the nominal schedule leaves %d ticks between whole worlds, floor is %d",
			got, b.FloorKeyframeTicks)
	}
}

// TestPeersOnDifferentCadencesShareRounds: a cadence is a power of two of ticks and
// a peer is due at multiples of it, so two peers on cadences 4 and 8 are due together
// every eighth tick and the host's rounds are its fastest peer's.
func TestPeersOnDifferentCadencesShareRounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cadence, limit, want uint64 }{{3, 60, 4}, {4, 60, 4}, {5, 60, 8}, {40, 60, 60}, {2, 60, 2}} {
		if got := alignedCadence(tc.cadence, tc.limit); got != tc.want {
			t.Fatalf("alignedCadence(%d, %d) = %d, want %d", tc.cadence, tc.limit, got, tc.want)
		}
	}
	quick, slow := &peerPublisher{}, &peerPublisher{}
	quick.plan.CadenceTicks, slow.plan.CadenceTicks = 4, 8
	shared := 0
	for tick := uint64(13); tick < 13+64; tick++ {
		q, s := quick.nextDue(tick), slow.nextDue(tick)
		if q%4 != 0 || q <= tick || s%8 != 0 || s <= tick {
			t.Fatalf("at tick %d the next due ticks are %d and %d", tick, q, s)
		}
		if q == s {
			shared++
		}
	}
	if shared != 32 {
		t.Fatalf("the slow peer shared %d of 64 rounds with the quick one, want every one of its 32", shared)
	}
}
