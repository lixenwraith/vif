package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

// TestAppsScopeOperatorState keeps view, help and log state on the App that owns it.
func TestAppsScopeOperatorState(t *testing.T) {
	a := mustHeadless(t, 0xA11CE, 120, 40)
	b := mustHeadless(t, 0xA11CE, 120, 40)
	defer a.Close()
	defer b.Close()

	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
	}

	a.Resize(140, 44)
	b.Resize(90, 28)
	a.Context().PushLocal(event.EventDebugFlowToggle, nil)
	a.Settle()
	if !a.Context().NavigationDebug.ShowFlow {
		t.Fatal("instance a did not enable its flow overlay")
	}
	if b.Context().NavigationDebug.ShowFlow {
		t.Fatal("instance b inherited instance a's flow overlay")
	}
	if a.Context().NavigationDebug.CompositePassability == b.Context().NavigationDebug.CompositePassability {
		t.Fatal("navigation debug state is shared between Apps")
	}

	a.Context().KeyTable = &input.KeyTable{}
	a.Context().PushLocal(event.EventMetaHelpRequest, nil)
	a.Settle()
	b.Context().PushLocal(event.EventMetaHelpRequest, nil)
	b.Settle()
	gotA := a.Context().GetOverlayContent()
	gotB := b.Context().GetOverlayContent()
	if gotA == nil {
		t.Fatal("instance a help produced no content")
	}
	if gotB == nil || len(gotB.Items) == 0 {
		t.Fatal("instance b help inherited instance a's empty key table")
	}
	if len(gotA.Items) >= len(gotB.Items) {
		t.Fatalf("help item counts = (%d, %d), want instance a's empty bindings scoped", len(gotA.Items), len(gotB.Items))
	}

	a.Tick(2)
	b.Tick(1)
	_, tickA, _ := a.Context().Correlation.Stamp()
	_, tickB, _ := b.Context().Correlation.Stamp()
	if tickA == tickB {
		t.Fatalf("correlation ticks = (%d, %d), want independent values", tickA, tickB)
	}
}
