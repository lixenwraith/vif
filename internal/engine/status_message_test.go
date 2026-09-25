package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

func newMessageContext(t *testing.T) (*GameContext, *ManualClock) {
	t.Helper()
	clock := NewManualClock()
	return NewGameContextWithClock(NewWorld(), 80, 24, clock), clock
}

// TestStatusMessageExpiresAtItsCap is the rule a message posted by a script
// relied on not existing: without a duration it used to sit on the bar for the
// rest of the run, and a long one outstayed the event it described.
func TestStatusMessageExpiresAtItsCap(t *testing.T) {
	ctx, clock := newMessageContext(t)

	ctx.SetStatusMessage("SPECIES DAMAGE INCREASED", 0, false)
	want := clock.Now().Add(parameter.StatusMessageMaxDuration).UnixNano()
	if got := ctx.GetStatusMessageExpiry(); got != want {
		t.Fatalf("a message with no duration expires at %d, want the cap at %d", got, want)
	}

	ctx.SetStatusMessage("Refused a conflicting authority handoff", 4*parameter.StatusMessageMaxDuration, true)
	if got := ctx.GetStatusMessageExpiry(); got != want {
		t.Fatalf("a message asking for four caps expires at %d, want %d", got, want)
	}

	clock.Step(parameter.StatusMessageMaxDuration + time.Millisecond)
	if ctx.GetStatusMessageExpiry() >= clock.Now().UnixNano() {
		t.Fatal("the message outlived its cap")
	}
}

// TestOnlyADurationHoldsTheStatusBar separates the two things a duration says.
// A caller that named one is asking to be read before it is replaced; one that
// named none is a notice, and the next notice takes the space.
func TestOnlyADurationHoldsTheStatusBar(t *testing.T) {
	ctx, clock := newMessageContext(t)

	ctx.SetStatusMessage("QUASAR FORMING", 0, false)
	ctx.SetStatusMessage("TOWER DESTROYED", 0, false)
	if got := ctx.GetStatusMessage(); got != "TOWER DESTROYED" {
		t.Fatalf("the bar reads %q, want the newer notice", got)
	}

	ctx.SetStatusMessage("Link recovered", parameter.StatusMessageDefaultTimeout, false)
	ctx.SetStatusMessage("TOWER DEFENSE CLEARED", 0, false)
	if got := ctx.GetStatusMessage(); got != "Link recovered" {
		t.Fatalf("the bar reads %q inside the held message's duration", got)
	}

	clock.Step(parameter.StatusMessageDefaultTimeout)
	ctx.SetStatusMessage("TOWER DEFENSE CLEARED", 0, false)
	if got := ctx.GetStatusMessage(); got != "TOWER DEFENSE CLEARED" {
		t.Fatalf("the bar reads %q once the hold has passed", got)
	}
}
