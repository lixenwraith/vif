// The seam between the controller's contract and the game's numbers.

package converge

import (
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/linkpace"
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

// TestAPlanStaysPricedOnTheRateTheLinkHeldAt: the rate a link delivered while it
// held the frames prices the plan after the link takes them again, so the plan that
// fits does not creep back and queue the link; a constrained plan is offered twice
// as much once SnapshotLinkCapacityTicks pass without a hold, and a hold reads the
// rate anew.
func TestAPlanStaysPricedOnTheRateTheLinkHeldAt(t *testing.T) {
	t.Parallel()
	p := &peerPublisher{}
	idle := linkpace.Metrics{Throughput: 20_000, Ready: true}
	held := linkpace.Metrics{Throughput: 6_000, Saturated: true, Ready: true}
	if got := p.priced(idle, 10); got.Saturated || p.capacity != 0 {
		t.Fatalf("a link never read as the limit was priced: %+v", got)
	}
	if got := p.priced(held, 20); !got.Saturated || got.Throughput != 6_000 {
		t.Fatalf("a held link was not priced on the rate it held at: %+v", got)
	}
	hold := uint64(20 + parameter.SnapshotLinkCapacityTicks)
	if got := p.priced(idle, hold); !got.Saturated || got.Throughput != 6_000 {
		t.Fatalf("the plan was not kept priced while the link took its frames: %+v", got)
	}
	if got := p.priced(idle, hold+1); got.Throughput != 6_000 {
		t.Fatalf("an unconstrained plan was offered more: %+v", got)
	}
	p.plan.Constrained = true
	if got := p.priced(idle, hold+1); !got.Saturated || got.Throughput != 12_000 {
		t.Fatalf("a constrained plan past the hold was not offered twice the rate: %+v", got)
	}
	if got := p.priced(held, hold+2); got.Throughput != 6_000 || p.capacityTick != hold+2 {
		t.Fatalf("a link held again was not read anew: %+v at %d", got, p.capacityTick)
	}
}
