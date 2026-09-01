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
