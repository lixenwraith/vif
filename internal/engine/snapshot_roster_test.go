package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestAWraithIsDrivenByNobody: an installed world whose cursor for this participant
// is its Wraith leaves that cursor remote, so nothing is predicted or synced for it
// before the arrival takes it back; the same cursor out of its wait is this one's.
func TestAWraithIsDrivenByNobody(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	own := w.CreateEntity(core.DomainShared)
	w.Components.Cursor.SetComponent(own, component.CursorComponent{Slot: 2, PeerID: 7})
	w.Components.Wraith.SetComponent(own, component.WraithComponent{X: -1, Y: -1})
	w.AdoptSession(SessionState{Participant: 7, Authority: 1})

	w.RebindCursorRoster(w.CaptureCursorControl())
	if w.SimulatesLocally(own) {
		t.Fatal("this participant drives its own Wraith before taking it back")
	}
	w.Components.Wraith.RemoveEntity(own)
	w.RebindCursorRoster(w.CaptureCursorControl())
	if !w.SimulatesLocally(own) {
		t.Fatal("this participant does not drive its own cursor once taken back")
	}
}

// TestASupersededCursorIsDrivenByNobody: while a run waits for its own arrival in a
// place whose cursor the run before it left live, that cursor is remote and input
// follows none, through a write too; the session record that begins or ends the
// wait rebinds it, which is all a replay has to go by.
func TestASupersededCursorIsDrivenByNobody(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	own := w.CreateEntity(core.DomainShared)
	w.Components.Cursor.SetComponent(own, component.CursorComponent{Slot: 2, PeerID: 7})
	w.AdoptSession(SessionState{Participant: 7, Authority: 1})
	w.RebindCursorRoster(w.CaptureCursorControl())
	record := func(superseding bool) {
		w.PushRecord(event.JournalRecord{Type: event.EventSessionState},
			&event.SessionStatePayload{Participant: 7, Authority: 1, Superseding: superseding})
	}
	driven := func() bool { return w.SimulatesLocally(own) && w.Resources.Player.LocalSlot() == 2 }

	record(true)
	if w.SimulatesLocally(own) || w.Resources.Player.LocalSlot() != parameter.NoPlayerSlot {
		t.Fatalf("the wait begun, the run drives its cursor %v and input follows slot %d; want neither",
			w.SimulatesLocally(own), w.Resources.Player.LocalSlot())
	}
	w.RebindCursorRoster(w.CaptureCursorControl())
	if w.SimulatesLocally(own) || w.Resources.Player.LocalSlot() != parameter.NoPlayerSlot {
		t.Fatal("a write under the wait gave the run its cursor")
	}
	record(false)
	if !driven() {
		t.Fatal("the wait ended, the run does not drive the cursor in its place")
	}
}
