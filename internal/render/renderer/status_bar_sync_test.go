package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

func newStatusBar(t *testing.T) (*StatusBarRenderer, *engine.ManualClock) {
	t.Helper()
	clock := engine.NewManualClock()
	return NewStatusBarRenderer(engine.NewGameContextWithClock(engine.NewWorld(), 80, 24, clock)), clock
}

// TestStatusBarNetworkBadgeIsOneCellChosenBySeverity pins the collapse.
//
// One badge for the whole session, and a worse fact hides a lesser one rather than
// sitting beside it. The measurements behind it are in :session and the status
// snapshot; a row of numbers on the bar is a diagnostic panel, not a glance.
func TestStatusBarNetworkBadgeIsOneCellChosenBySeverity(t *testing.T) {
	r, _ := newStatusBar(t)

	if item, ok := r.networkBadge(); ok {
		t.Fatalf("a solo run renders %#v", item)
	}

	r.statNet.Store("waiting")
	if item, ok := r.networkBadge(); !ok || item.text != " Net: wait " {
		t.Fatalf("waiting item = %#v, %t", item, ok)
	}

	r.statNet.Store("connected")
	r.statPeers.Store(2)
	if item, ok := r.networkBadge(); !ok || item.text != " Net: 2 " {
		t.Fatalf("converged item = %#v, %t", item, ok)
	}

	// A constrained link is the system working on a small link; the numbers behind
	// the word are in the status snapshot and in :session.
	r.statCadence.Store(8)
	r.statConstrained.Store(true)
	item, ok := r.networkBadge()
	if !ok || item.text != " Net: 2 slow " || item.bg != visual.RgbOrange {
		t.Fatalf("constrained item = %#v, %t", item, ok)
	}

	// Being behind the session outranks it: it says whether this participant's own
	// actions are still landing on time, which is the one a player can act on.
	r.statStale.Store(true)
	r.statLag.Store(7)
	item, ok = r.networkBadge()
	if !ok || item.text != " Net: 2 lag 7 " || item.bg != visual.RgbOrange {
		t.Fatalf("stale item = %#v, %t", item, ok)
	}

	// The floor outranks both, and reads differently on purpose: not the system
	// degrading, the system unable to keep its guarantee.
	r.statFloor.Store(true)
	item, ok = r.networkBadge()
	if !ok || item.text != " Net: 2 slow! " || item.bg != visual.RgbCursorError {
		t.Fatalf("floor item = %#v, %t", item, ok)
	}
}

// TestStatusBarAuthorityLossHidesTheLinkItDescribed is the reading the severity
// order exists for: after the host has gone, a peer count describes a session this
// instance is no longer in.
func TestStatusBarAuthorityLossHidesTheLinkItDescribed(t *testing.T) {
	r, _ := newStatusBar(t)
	r.statNet.Store("connected")
	r.statPeers.Store(1)
	r.statCadence.Store(4)
	r.statFloor.Store(true)

	r.statMigrating.Store(true)
	if item, ok := r.networkBadge(); !ok || item.text != " Migrating " || item.bg != visual.RgbOrange {
		t.Fatalf("migrating item = %#v, %t", item, ok)
	}

	// Permanent for this run, so it outranks even the handoff badge.
	r.statHostLost.Store(true)
	item, ok := r.networkBadge()
	if !ok || item.text != " Host lost " || item.bg != visual.RgbCursorError {
		t.Fatalf("host-loss item = %#v, %t", item, ok)
	}

	// And a link that has gone says so rather than reporting the peers it had.
	r.statHostLost.Store(false)
	r.statMigrating.Store(false)
	r.statNet.Store("down")
	item, ok = r.networkBadge()
	if !ok || item.text != " Net: down " || item.bg != visual.RgbCursorError {
		t.Fatalf("down item = %#v, %t", item, ok)
	}
}

// TestStatusBarNetworkCellHoldsItsWidth is why the cell is held at all: its inputs
// move on the correction cadence, and its width reflows every item beside it, so an
// unheld cell repaints faster than it can be read.
func TestStatusBarNetworkCellHoldsItsWidth(t *testing.T) {
	r, clock := newStatusBar(t)
	r.statNet.Store("connected")
	r.statPeers.Store(2)
	first, _ := r.networkItem()

	r.statStale.Store(true)
	r.statLag.Store(7)
	if item, _ := r.networkItem(); item.text != first.text {
		t.Fatalf("the cell changed to %q inside its hold", item.text)
	}

	clock.Step(parameter.StatusNetworkHoldDuration)
	if item, _ := r.networkItem(); item.text != " Net: 2 lag 7 " {
		t.Fatalf("the cell is %q after its hold, want the current reading", item.text)
	}
}
