package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
)

func TestPersonalNuggetUsesPlayerDomainAndLocalCursor(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	nuggets := NewNuggetSystem(w).(*NuggetSystem)

	nuggets.spawnNugget()
	spawned := nuggets.activeNuggetEntity
	if spawned == 0 || spawned.Domain() != core.DomainPlayer {
		t.Fatalf("spawned nugget = %d in %s, want player domain", spawned, spawned.Domain())
	}
	w.DestroyEntity(spawned)
	nuggets.activeNuggetEntity = 0
	w.Resources.Event.Queue.Consume()

	remoteHeat, _ := w.Components.Heat.GetPtr(remote)
	remoteHeat.EmberActive = true
	if got := nuggets.collectionCursor(25, 5); got != 0 {
		t.Fatalf("remote cursor claimed personal nugget: got %d, want 0", got)
	}

	w.Positions.SetPosition(local, component.PositionComponent{X: 25, Y: 5})
	if got := nuggets.collectionCursor(25, 5); got != local {
		t.Fatalf("local cursor claim = %d, want %d", got, local)
	}
}

func TestPersonalNuggetJumpCrossesOnlyCursorMove(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	nuggets := NewNuggetSystem(w).(*NuggetSystem)

	nugget := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(nugget, component.PositionComponent{X: 12, Y: 7})
	w.Components.Nugget.SetComponent(nugget, component.NuggetComponent{})
	nuggets.activeNuggetEntity = nugget

	nuggets.HandleEvent(event.GameEvent{
		Type:    event.EventNuggetJumpRequest,
		Payload: &event.NuggetJumpRequestPayload{Entity: remote},
	})
	if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
		t.Fatalf("remote nugget jump produced events: %#v", events)
	}

	nuggets.HandleEvent(event.GameEvent{
		Type:    event.EventNuggetJumpRequest,
		Payload: &event.NuggetJumpRequestPayload{Entity: local},
	})
	events := w.Resources.Event.Queue.Consume()
	if len(events) != 4 {
		t.Fatalf("local nugget jump events = %#v, want move, energy, sound and heat", events)
	}
	move, ok := events[0].Payload.(*event.CursorMoveRequestPayload)
	if !ok || events[0].Type != event.EventCursorMoveRequest || !event.OnWire(events[0]) {
		t.Fatalf("jump crossing = %#v, want one on-wire cursor move", events[0])
	}
	if move.Entity != local || move.X != 12 || move.Y != 7 {
		t.Fatalf("jump move = %#v, want cursor %d at (12,7)", move, local)
	}
	for _, ev := range events[1:] {
		if event.OnWire(ev) {
			t.Fatalf("personal nugget consequence reached the wire: %#v", ev)
		}
	}
}

// TestPersonalNuggetCollectsAtThePredictedCell: the collection area is the one the
// renderer draws the shield and the ember at. Nothing in absorption crosses — the
// heat it pays is owner-authored — so resolving it from the announced placement
// delayed a wholly local reward by a playout lead.
func TestPersonalNuggetCollectsAtThePredictedCell(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	nuggets := NewNuggetSystem(w).(*NuggetSystem)

	w.PushCursorMove(local, 20, 9)
	if pos, _ := w.Positions.GetPosition(local); pos.X == 20 && pos.Y == 9 {
		t.Fatal("store cell already moved; the check is vacuous")
	}
	if got := nuggets.collectionCursor(20, 9); got != local {
		t.Fatalf("collection cursor = %d, want %d at the predicted cell", got, local)
	}
}
