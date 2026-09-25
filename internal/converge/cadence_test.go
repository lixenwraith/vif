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
