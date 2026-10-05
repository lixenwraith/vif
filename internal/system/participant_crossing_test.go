package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

func TestCursorDefeatTransitionCrossesCombinedOwnerState(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	energy := NewEnergySystem(w).(*EnergySystem)
	heat := NewHeatSystem(w).(*HeatSystem)

	find := func() *event.CursorDefeatStatePayload {
		t.Helper()
		var found *event.CursorDefeatStatePayload
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type != event.EventCursorDefeatState {
				continue
			}
			if !event.OnWire(ev) {
				t.Fatalf("defeat transition is not on wire: %#v", ev)
			}
			found, _ = ev.Payload.(*event.CursorDefeatStatePayload)
		}
		return found
	}

	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest,
		Payload: &event.HeatSetRequestPayload{Entity: cursor, Value: 10}})
	if p := find(); p == nil || p.Entity != cursor || p.Defeated {
		t.Fatalf("initial live transition = %#v, want cursor %d live", p, cursor)
	}

	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest,
		Payload: &event.EnergySetPayload{Entity: cursor, Value: 100}})
	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest,
		Payload: &event.HeatSetRequestPayload{Entity: cursor, Value: 0}})
	w.Resources.Event.Queue.Consume()
	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest,
		Payload: &event.EnergySetPayload{Entity: cursor, Value: 0}})
	if p := find(); p == nil || p.Entity != cursor || !p.Defeated {
		t.Fatalf("terminal transition = %#v, want cursor %d defeated", p, cursor)
	}
}

func TestMetaDefeatGatesFollowRosteredOwnerCrossings(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := NewCursorSystem(w).(*CursorSystem)
	meta := NewMetaSystem(ctx).(*MetaSystem)
	for slot := range uint8(2) {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{Slot: slot, X: 5 + int(slot)*10, Y: 5,
				Control: uint8(component.ControlLocal)}})
		entity := w.Resources.Player.Slot(slot)
		meta.HandleEvent(event.GameEvent{Type: event.EventCursorSpawned,
			Payload: &event.CursorSpawnedPayload{Entity: entity, Slot: slot}})
	}
	w.Resources.Event.Queue.Consume()

	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: first, Defeated: true}})
	if !w.Resources.Status.Bools.Get("session.any_defeated").Load() || w.Resources.Status.Bools.Get("session.all_defeated").Load() {
		t.Fatal("one defeated cursor must set any_defeated without setting all_defeated")
	}
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: second, Defeated: true}})
	if !w.Resources.Status.Bools.Get("session.all_defeated").Load() {
		t.Fatal("all rostered cursors defeated did not close the session")
	}
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: first, Defeated: false}})
	if w.Resources.Status.Bools.Get("session.all_defeated").Load() {
		t.Fatal("revived cursor left the session defeated")
	}
	if !w.Resources.Status.Bools.Get("session.any_defeated").Load() {
		t.Fatal("reviving one cursor hid the remaining defeat")
	}
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: second, Defeated: false}})
	if w.Resources.Status.Bools.Get("session.any_defeated").Load() {
		t.Fatal("all revived cursors left a stale defeat")
	}
}

// A species strikes only the cursors this instance owns: a shield its members reach,
// with the impact crossing, and a bare cursor on a member's cell unless the species
// strikes shields alone.
func TestSpeciesContactStrikesOnlyOwnedCursors(t *testing.T) {
	w, local, remote := testCursorWorld(t)
	remoteCursor, _ := w.Components.Cursor.GetPtr(remote)
	remoteCursor.Control = component.ControlRemote
	w.Positions.SetPosition(remote, component.PositionComponent{X: 5, Y: 5})
	for _, cursor := range []core.Entity{local, remote} {
		shield, _ := w.Components.Shield.GetPtr(cursor)
		shield.Active, shield.InvRxSq, shield.InvRySq = true, 1, 1
	}

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 5, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 5, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeUnit,
		MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	damage := component.CursorDamage{EnergyDrain: 7, HeatDelta: -3}
	strike := func(bare bool) (impacts, drains, heats int) {
		strikeContacts(w, header, damage, bare)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.CombatAttackAreaRequestPayload:
				if !event.OnWire(ev) || p.OwnerEntity != local || p.TargetEntity != header ||
					len(p.HitEntities) != 1 || p.HitEntities[0] != member {
					t.Fatalf("shield crossing = %#v payload %#v", ev, p)
				}
				impacts++
			case *event.ShieldDrainRequestPayload:
				if p.Entity != local || p.Value != damage.EnergyDrain {
					t.Fatalf("shield drain = %#v, want local cursor %d", p, local)
				}
				drains++
			case *event.HeatAddRequestPayload:
				if p.Entity != local || p.Delta != damage.HeatDelta {
					t.Fatalf("heat = %#v, want local cursor %d", p, local)
				}
				heats++
			}
		}
		return impacts, drains, heats
	}
	if i, d, h := strike(true); i != 1 || d != 1 || h != 0 {
		t.Fatalf("shielded contact = (%d impacts, %d drains, %d heats), want (1, 1, 0)", i, d, h)
	}
	shield, _ := w.Components.Shield.GetPtr(local)
	shield.Active = false
	w.Positions.SetPosition(local, component.PositionComponent{X: 5, Y: 5})
	if i, d, h := strike(true); i != 0 || d != 0 || h != 1 {
		t.Fatalf("bare contact = (%d impacts, %d drains, %d heats), want (0, 0, 1)", i, d, h)
	}
	if i, d, h := strike(false); i+d+h != 0 {
		t.Fatalf("shield-only contact struck a bare cursor: (%d, %d, %d)", i, d, h)
	}
}

// The snake's head strikes only the cursor on its cell, and heats a bare cursor only
// while the body no longer shields it.
func TestSnakeHeadHeatsOnlyWhileUnshielded(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	head := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(head, component.PositionComponent{X: 5, Y: 5})
	snakes := NewSnakeSystem(w).(*SnakeSystem)
	strikes := func(shielded bool) (drains, heats int) {
		snakes.handleInteractions(&component.SnakeComponent{HeadEntity: head, IsShielded: shielded})
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Payload.(type) {
			case *event.ShieldDrainRequestPayload:
				drains++
			case *event.HeatAddRequestPayload:
				heats++
			}
		}
		return drains, heats
	}
	if d, h := strikes(true); d+h != 0 {
		t.Fatalf("shielded snake struck a bare cursor: (%d drains, %d heats)", d, h)
	}
	if d, h := strikes(false); d != 0 || h != 1 {
		t.Fatalf("unshielded snake = (%d drains, %d heats), want (0, 1)", d, h)
	}
	shield, _ := w.Components.Shield.GetPtr(local)
	shield.Active, shield.InvRxSq, shield.InvRySq = true, 1, 1
	if d, h := strikes(true); d != 1 || h != 0 {
		t.Fatalf("shielded cursor on the head = (%d drains, %d heats), want (1, 0)", d, h)
	}
}
