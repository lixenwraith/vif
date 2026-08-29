package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
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

func TestMetaDefeatGateRequiresEveryRosteredCursor(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := NewCursorSystem(w).(*CursorSystem)
	meta := NewMetaSystem(ctx).(*MetaSystem)
	for slot := range uint8(2) {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{Slot: slot, X: 5 + int(slot)*10, Y: 5,
				Control: uint8(component.ControlHuman)}})
		entity := w.Resources.Player.Slot(slot)
		meta.HandleEvent(event.GameEvent{Type: event.EventCursorSpawned,
			Payload: &event.CursorSpawnedPayload{Entity: entity, Slot: slot}})
	}
	w.Resources.Event.Queue.Consume()

	first, second := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: first, Defeated: true}})
	if w.Resources.Status.Bools.Get("session.all_defeated").Load() {
		t.Fatal("one defeated cursor ended a two-participant session")
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
}

func TestSharedSpeciesCrossesOnlyOwnedShieldImpact(t *testing.T) {
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

	NewQuasarSystem(w).(*QuasarSystem).handleInteractions(header)
	impacts, drains := 0, 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch ev.Type {
		case event.EventCombatAttackAreaCrossingRequest:
			p, _ := ev.Payload.(*event.CombatAttackAreaRequestPayload)
			if !event.OnWire(ev) || p == nil || p.OwnerEntity != local || p.TargetEntity != header ||
				len(p.HitEntities) != 1 || p.HitEntities[0] != member {
				t.Fatalf("shield crossing = %#v payload %#v", ev, p)
			}
			impacts++
		case event.EventShieldDrainRequest:
			p, _ := ev.Payload.(*event.ShieldDrainRequestPayload)
			if p == nil || p.Entity != local {
				t.Fatalf("shield drain = %#v, want local cursor %d", p, local)
			}
			drains++
		}
	}
	if impacts != 1 || drains != 1 {
		t.Fatalf("owner-resolved interactions = (%d impacts, %d drains), want (1, 1)", impacts, drains)
	}
}
