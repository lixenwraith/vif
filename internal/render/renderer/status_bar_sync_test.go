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
	if !ok || item.text != " DESYNC " || item.bg != visual.RgbCursorError {
		t.Fatalf("desync item = %#v, %t", item, ok)
	}

	r.statSync.Store("synced")
	item, ok = r.syncItem()
	if !ok || item.text != " SYNCED " || item.bg != visual.RgbGreen {
		t.Fatalf("synced item = %#v, %t", item, ok)
	}
}
