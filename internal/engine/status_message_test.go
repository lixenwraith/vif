package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
)

func newMessageContext(t *testing.T) (*GameContext, *ManualClock) {
	t.Helper()
	clock := NewManualClock()
	return NewGameContextWithClock(NewWorld(), 80, 24, clock), clock
}

// TestAStatusMessageYieldsOnlyToItsRankOrItsMinimum: a message the one shown
// outranks is refused until that one has been up for its kind's minimum, one that
// ranks as high replaces it at once, and each leaves the bar after its kind's time.
func TestAStatusMessageYieldsOnlyToItsRankOrItsMinimum(t *testing.T) {
	ctx, clock := newMessageContext(t)

	ctx.SetStatusMessage("Host connection lost", parameter.StatusAlert)
	ctx.SetStatusMessage("Auto: on", parameter.StatusNotice)
	if got := ctx.GetStatusMessage(); got != "Host connection lost" {
		t.Fatalf("the bar reads %q inside the alert's minimum", got)
	}
	ctx.SetStatusMessage("Authority moved to participant 2", parameter.StatusAlert)
	if got := ctx.GetStatusMessage(); got != "Authority moved to participant 2" {
		t.Fatalf("the bar reads %q after an alert of the same rank", got)
	}

	clock.Step(parameter.StatusKinds[parameter.StatusAlert].Min)
	ctx.SetStatusMessage("TOWER DESTROYED", parameter.StatusNotice)
	if got := ctx.GetStatusMessage(); got != "TOWER DESTROYED" {
		t.Fatalf("the bar reads %q once the alert's minimum has passed", got)
	}
	shown := clock.Now().Add(parameter.StatusKinds[parameter.StatusNotice].Shown)
	if ctx.ShownStatusMessage(shown.UnixNano()-1) == "" || ctx.ShownStatusMessage(shown.UnixNano()) != "" {
		t.Fatal("the notice is not shown for exactly its kind's time")
	}
}
