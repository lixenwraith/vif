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

// TestABotsDefeatDecidesOnlyWhereNoPersonHoldsAPlace: a person's played cursor
// decides, and a bot's only while no person holds a place, in play or away. A
// Wraith's own and a cursor off the roster decide nothing.
func TestABotsDefeatDecidesOnlyWhereNoPersonHoldsAPlace(t *testing.T) {
	// roster is a letter a slot, the first the cursor asked of: p a person's, b a
	// bot's, w a person's Wraith, - a cursor bound to no slot that names the next
	for name, c := range map[string]struct {
		roster  string
		decides bool
	}{
		"a person alone":                 {"p", true},
		"a bot alone":                    {"b", true},
		"a bot beside a bot":             {"bb", true},
		"a person beside a bot":          {"pb", true},
		"a bot beside a person":          {"bp", false},
		"a bot beside a person's Wraith": {"bw", false},
		"a Wraith":                       {"w", false},
		"a cursor off the roster":        {"-p", false},
	} {
		w := NewWorld()
		NewGameContextWithClock(w, 40, 24, NewManualClock())
		var asked core.Entity
		for slot, kind := range c.roster {
			e := w.CreateEntity(core.DomainShared)
			cursor := component.CursorComponent{Slot: uint8(slot), Bot: kind == 'b'}
			switch kind {
			case 'w':
				w.Components.Wraith.SetComponent(e, component.WraithComponent{X: -1, Y: -1})
			case '-':
				cursor.Slot++
			}
			w.Components.Cursor.SetComponent(e, cursor)
			if kind != '-' {
				w.Resources.Player.Bind(uint8(slot), e)
			}
			if slot == 0 {
				asked = e
			}
		}
		if got := w.DefeatDecides(asked); got != c.decides {
			t.Errorf("%s: its defeat decides %t, want %t", name, got, c.decides)
		}
	}
}

// TestAViewerLeavesAWraithOnlyForOneWhoPlays: with nobody playing, a viewer stays on
// the Wraith it follows rather than hop to the next each tick; a cursor somebody
// plays takes its view, and so does any cursor once its own slot is empty.
func TestAViewerLeavesAWraithOnlyForOneWhoPlays(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 40, 24, NewManualClock())
	w.Watch()
	var cursors [3]core.Entity
	for slot := range cursors {
		cursors[slot] = w.CreateEntity(core.DomainShared)
		w.Resources.Player.Bind(uint8(slot), cursors[slot])
		w.Components.Wraith.SetComponent(cursors[slot], component.WraithComponent{X: -1, Y: -1})
	}
	w.Follow(0, 0)
	kept := func(want uint8, when string) {
		t.Helper()
		for range 3 {
			w.KeepView()
			if got := w.Resources.Player.ViewSlot(); got != want {
				t.Fatalf("%s the viewer looks from slot %d, want %d", when, got, want)
			}
		}
	}
	kept(0, "among three Wraiths")
	w.Components.Wraith.RemoveEntity(cursors[2])
	kept(2, "with slot 2 played")
	w.Components.Wraith.SetComponent(cursors[2], component.WraithComponent{X: -1, Y: -1})
	w.Resources.Player.Unbind(2)
	kept(0, "with its slot emptied and Wraiths left")
}
