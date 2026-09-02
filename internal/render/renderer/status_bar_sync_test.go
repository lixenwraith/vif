package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

// TestStatusBarSyncIndicatorReportsStalenessAndCorrection pins what replaced the
// parity verdict.
//
// The indicator used to escalate: DESYNC while two instances disagreed, DIVERGED
// once the disagreement had persisted past the point where anything could resolve
// it. Both were statements about instances that re-derived one world from one
// artifact stream. A guest predicts and is corrected now, so a disagreement is the
// ordinary condition and there is no state left for a player to be warned they are
// stuck in. What is worth telling them is the link — this instance is far enough
// behind that its own crossings reach the host late — and how visibly the authority
// last disagreed with the prediction.
func TestStatusBarSyncIndicatorReportsStalenessAndCorrection(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	r := NewStatusBarRenderer(ctx)

	// A correction that moved nothing says nothing: an exact prediction is the
	// resting state and does not deserve a badge.
	if item, ok := r.syncItem(); ok {
		t.Fatalf("an idle session renders %#v", item)
	}

	r.statCorrection.Store(12)
	item, ok := r.syncItem()
	if !ok || item.text != " COR 12 " {
		t.Fatalf("correction item = %#v, %t", item, ok)
	}

	// Staleness outranks it. A correction magnitude describes the picture; being
	// behind the session describes whether this participant's own actions are still
	// landing on time, which is the one a player can act on.
	r.statStale.Store(true)
	r.statLag.Store(7)
	item, ok = r.syncItem()
	if !ok || item.text != " LAG 7 " || item.bg != visual.RgbOrange {
		t.Fatalf("stale item = %#v, %t", item, ok)
	}

	r.statStale.Store(false)
	r.statCorrection.Store(0)
	if item, ok := r.syncItem(); ok {
		t.Fatalf("a converged session renders %#v", item)
	}
}

// TestStatusBarLinkIndicatorSeparatesConstrainedFromUnrecoverable is the
// distinction Phase 5's status item exists to draw.
//
// A constrained link is the design working: the cadence slowed, prediction
// carries more, and the correction magnitude rises and stays bounded. A link
// below the convergence floor is not: no cadence the controller may choose
// delivers a whole authoritative world inside the guaranteed window. Rendering
// both as one badge would tell a player the two are the same problem, and they
// are not.
func TestStatusBarLinkIndicatorSeparatesConstrainedFromUnrecoverable(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	r := NewStatusBarRenderer(ctx)

	// A solo run has no cadence at all, and a healthy session has nothing to say.
	if item, ok := r.linkItem(); ok {
		t.Fatalf("a run with no session renders %#v", item)
	}
	r.statCadence.Store(4)
	r.statKeyframe.Store(10)
	if item, ok := r.linkItem(); ok {
		t.Fatalf("an unconstrained link renders %#v", item)
	}

	r.statConstrained.Store(true)
	r.statLinkRTT.Store(120)
	r.statLinkJitter.Store(14)
	r.statLinkBps.Store(32_000)
	r.statCadence.Store(8)
	r.statKeyframe.Store(7)
	item, ok := r.linkItem()
	if !ok || item.text != " LNK 120±14ms 8x7 32K " {
		t.Fatalf("constrained item = %#v, %t", item, ok)
	}
	if item.bg != visual.RgbOrange {
		t.Fatalf("a constrained link is rendered as %v, want the warning colour", item.bg)
	}

	// The floor outranks it, and it reads differently on purpose: this one is not
	// the system degrading, it is the system unable to keep its guarantee.
	r.statFloor.Store(true)
	item, ok = r.linkItem()
	if !ok || item.text != " LINK! 120±14ms 8x7 32K " {
		t.Fatalf("floor item = %#v, %t", item, ok)
	}
	if item.bg != visual.RgbCursorError {
		t.Fatalf("a link below the floor is rendered as %v, want the error colour", item.bg)
	}
}

func TestStatusBarByteRateFitsTheBar(t *testing.T) {
	for _, c := range []struct {
		bps  int64
		want string
	}{{0, "-"}, {-5, "-"}, {900, "900B"}, {32_000, "32K"}, {999_999, "999K"}, {2_500_000, "2.5M"}} {
		if got := byteRate(c.bps); got != c.want {
			t.Errorf("byteRate(%d) = %q, want %q", c.bps, got, c.want)
		}
	}
}
