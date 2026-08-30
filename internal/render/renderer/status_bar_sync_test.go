package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

func TestStatusBarSyncIndicatorUsesAlertAndRecoveryColors(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	r := NewStatusBarRenderer(ctx)

	r.statSync.Store("desync")
	item, ok := r.syncItem()
	if !ok || item.text != " DESYNC " || item.bg != visual.RgbOrange {
		t.Fatalf("desync item = %#v, %t", item, ok)
	}

	// Past the point where a disagreement could still resolve itself the indicator
	// escalates: nothing re-derives a missing artifact, so this is not a warning
	// about a moment, it is a statement about the rest of the session.
	r.statDiverged.Store(true)
	item, ok = r.syncItem()
	if !ok || item.text != " DIVERGED " || item.bg != visual.RgbCursorError {
		t.Fatalf("diverged item = %#v, %t", item, ok)
	}
	r.statDiverged.Store(false)

	r.statSync.Store("synced")
	item, ok = r.syncItem()
	if !ok || item.text != " SYNCED " || item.bg != visual.RgbGreen {
		t.Fatalf("synced item = %#v, %t", item, ok)
	}
}
