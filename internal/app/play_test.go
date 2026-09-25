//go:build !vif_headless

package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/input"
)

// TestReplayViewStopsAtTheMapEdges is the scroll rule: a map the viewer's view holds
// is centred and does not move, a larger one is shown from the recorded view and
// scrolls no further than its edges, and pan pushed past an edge is not banked.
func TestReplayViewStopsAtTheMapEdges(t *testing.T) {
	t.Parallel()

	if cam, off, kept := viewAxis(0, 80, 100, 60, 7); cam != 0 || off != 20 || kept != 0 {
		t.Fatalf("a map the view holds: camera %d offset %d pan %d, want 0 20 0", cam, off, kept)
	}
	if cam, off, _ := viewAxis(50, 100, 100, 200, 0); cam != 50 || off != 0 {
		t.Fatalf("an unpanned view the recorded size: camera %d offset %d, want the recorded 50 0", cam, off)
	}
	// A 60-cell view on a 100-cell recording at camera 50 starts from its centre, 70.
	cam, _, kept := viewAxis(50, 100, 60, 200, 1000)
	if cam != 140 || kept != 70 {
		t.Fatalf("panned past the far edge: camera %d pan %d, want 140 70", cam, kept)
	}
	if cam, _, _ = viewAxis(50, 100, 60, 200, kept-panStep); cam != 140-panStep {
		t.Fatalf("one step back from the far edge: camera %d, want %d", cam, 140-panStep)
	}
	if cam, _, kept = viewAxis(50, 100, 60, 200, -1000); cam != 0 || kept != -70 {
		t.Fatalf("panned past the near edge: camera %d pan %d, want 0 -70", cam, kept)
	}
}

// TestReplayViewerCommandLineReturnsTheRecordedState is the rule the viewer's command
// line keeps: it inspects the recording and changes none of it, and the mode and pause
// it borrowed are the recording's again once it closes.
func TestReplayViewerCommandLineReturnsTheRecordedState(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	if !a.Inject(intentModeSwitch(input.ModeTargetInsert)) {
		t.Fatal("insert quit the game")
	}

	p := &player{a: a}
	press := func(ev terminal.Event) {
		t.Helper()
		ev.Type = terminal.EventKey
		if !p.key(ev) {
			t.Fatalf("key %+v quit the replay", ev)
		}
	}
	energy := func() (v int64) {
		a.World().RunSafe(func() {
			if e, ok := a.World().Components.Energy.GetPtr(a.World().Resources.Player.Entity); ok {
				v = e.Current
			}
		})
		return v
	}

	press(terminal.Event{Key: terminal.KeyRune, Rune: ':'})
	if p.cmd == nil || !a.Context().IsCommandMode() || !a.Context().TimeCtl.IsPaused() {
		t.Fatal("':' did not open a paused command line for the viewer")
	}
	before := energy()
	for _, r := range "energy 12345" {
		press(terminal.Event{Key: terminal.KeyRune, Rune: r})
	}
	press(terminal.Event{Key: terminal.KeyEnter})
	if energy() != before || !strings.Contains(a.Context().GetStatusMessage(), "replay") {
		t.Fatalf("a viewer's :energy ran: energy %d, was %d; bar %q", energy(), before, a.Context().GetStatusMessage())
	}
	if p.cmd != nil || a.Context().Viewer.Load() {
		t.Fatal("the command line stayed the viewer's after it closed")
	}
	if a.Context().GetMode() != core.ModeInsert || a.Context().TimeCtl.IsPaused() {
		t.Fatalf("closed into mode %d paused %t, want the recorded INSERT, running",
			a.Context().GetMode(), a.Context().TimeCtl.IsPaused())
	}
}
