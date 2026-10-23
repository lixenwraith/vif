package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
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
