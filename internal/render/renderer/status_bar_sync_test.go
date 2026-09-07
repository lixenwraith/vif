package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

func newStatusBar(t *testing.T) *StatusBarRenderer {
	t.Helper()
	w := engine.NewWorld()
	return NewStatusBarRenderer(engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock()))
}

// TestStatusBarNetworkBadgeIsOneCellChosenBySeverity pins the collapse.
//
// The session used to render as up to four items at once — an authority badge, a
// link panel with five numbers in it, a staleness or correction badge, and a
// connection badge carrying a map latch that is on for every session and off for
// every solo run. Together they could take the whole right half of the bar and
// push the player's own resources off the end, and two of the readings they
// produced were actively wrong: a correction count outliving the link it came
// from, and DOWN printed beside a latch as though the two were related.
//
// One badge, and a worse fact hides a lesser one rather than sitting beside it.
func TestStatusBarNetworkBadgeIsOneCellChosenBySeverity(t *testing.T) {
	r := newStatusBar(t)

	if item, ok := r.networkItem(); ok {
		t.Fatalf("a solo run renders %#v", item)
	}

	r.statNet.Store("waiting")
	if item, ok := r.networkItem(); !ok || item.text != " Net: wait " {
		t.Fatalf("waiting item = %#v, %t", item, ok)
	}

	r.statNet.Store("connected")
	r.statPeers.Store(2)
	if item, ok := r.networkItem(); !ok || item.text != " Net: 2 " {
		t.Fatalf("converged item = %#v, %t", item, ok)
	}

	// How visibly the authority disagreed with the prediction, and absent when it
	// was exact — which at rest it usually is.
	r.statCorrection.Store(12)
	if item, ok := r.networkItem(); !ok || item.text != " Net: 2 ~12 " {
		t.Fatalf("correction item = %#v, %t", item, ok)
	}

	// A constrained link is the system working on a small link; the numbers behind
	// the word are in the status snapshot and in :session.
	r.statCadence.Store(8)
	r.statConstrained.Store(true)
	item, ok := r.networkItem()
	if !ok || item.text != " Net: 2 slow " || item.bg != visual.RgbOrange {
		t.Fatalf("constrained item = %#v, %t", item, ok)
	}

	// Being behind the session outranks it: it says whether this participant's own
	// actions are still landing on time, which is the one a player can act on.
	r.statStale.Store(true)
	r.statLag.Store(7)
	item, ok = r.networkItem()
	if !ok || item.text != " Net: 2 lag 7 " || item.bg != visual.RgbOrange {
		t.Fatalf("stale item = %#v, %t", item, ok)
	}

	// The floor outranks both, and reads differently on purpose: not the system
	// degrading, the system unable to keep its guarantee.
	r.statFloor.Store(true)
	item, ok = r.networkItem()
	if !ok || item.text != " Net: 2 slow! " || item.bg != visual.RgbCursorError {
		t.Fatalf("floor item = %#v, %t", item, ok)
	}
}

// TestStatusBarAuthorityLossHidesTheLinkItDescribed is the reading the collapse
// exists to fix: after the host has gone, a correction count and a peer count
// describe a session this instance is no longer in.
func TestStatusBarAuthorityLossHidesTheLinkItDescribed(t *testing.T) {
	r := newStatusBar(t)
	r.statNet.Store("connected")
	r.statPeers.Store(1)
	r.statCorrection.Store(4)
	r.statCadence.Store(4)
	r.statFloor.Store(true)

	r.statMigrating.Store(true)
	if item, ok := r.networkItem(); !ok || item.text != " Migrating " || item.bg != visual.RgbOrange {
		t.Fatalf("migrating item = %#v, %t", item, ok)
	}

	// Permanent for this run, so it outranks even the handoff badge.
	r.statHostLost.Store(true)
	item, ok := r.networkItem()
	if !ok || item.text != " Host lost " || item.bg != visual.RgbCursorError {
		t.Fatalf("host-loss item = %#v, %t", item, ok)
	}

	// And a link that has gone says so rather than reporting the peers it had.
	r.statHostLost.Store(false)
	r.statMigrating.Store(false)
	r.statNet.Store("down")
	item, ok = r.networkItem()
	if !ok || item.text != " Net: down " || item.bg != visual.RgbCursorError {
		t.Fatalf("down item = %#v, %t", item, ok)
	}
}
