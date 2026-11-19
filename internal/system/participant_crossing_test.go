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
	energy := ready(NewEnergySystem(w)).(*EnergySystem)
	heat := ready(NewHeatSystem(w)).(*HeatSystem)

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
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
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

// A crush is a defeat every instance latches alike: the walls crush the same cursor
// on the same tick, so no owner crossing has to author it
func TestACrushedCursorIsDefeated(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{X: 5, Y: 5, Control: uint8(component.ControlLocal)}})
	w.Resources.Event.Queue.Consume()
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorCrushed,
		Payload: &event.CursorCrushedPayload{Entity: w.Resources.Player.Slot(0)}})
	if !w.Resources.Status.Bools.Get("session.any_defeated").Load() {
		t.Fatal("a crushed cursor is not defeated")
	}
}

// TestAWraithNeitherLosesNorHoldsUpADefeat: the players left decide a defeat, so a
// defeated player alone with a Wraith has lost, and a session of Wraiths has not.
func TestAWraithNeitherLosesNorHoldsUpADefeat(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	for slot := range uint8(2) {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{Slot: slot, X: 5 + int(slot)*10, Y: 5,
				Control: uint8(component.ControlLocal)}})
	}
	turn := func(slot uint8) {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorWraithRequest,
			Payload: &event.CursorWraithRequestPayload{Slot: slot}})
		for _, ev := range w.Resources.Event.Queue.Consume() {
			meta.HandleEvent(ev)
		}
	}
	all, anyLost := w.Resources.Status.Bools.Get("session.all_defeated"), w.Resources.Status.Bools.Get("session.any_defeated")
	turn(1)
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(0), Defeated: true}})
	if !all.Load() || !anyLost.Load() {
		t.Fatalf("a defeated player alone with a Wraith: all defeated %t, any %t; want both", all.Load(), anyLost.Load())
	}
	turn(0)
	if all.Load() || anyLost.Load() {
		t.Fatalf("a session of Wraiths: all defeated %t, any %t; want neither", all.Load(), anyLost.Load())
	}
}

// defeatFold is a world rostering one cursor a payload, in slot order as participants
// 1 on, with ask, which hands an event to the cursor system and to the meta system
// that folds, then what the first raised to the second, and the fold's two keys.
func defeatFold(t *testing.T, rostered ...event.CursorSpawnRequestPayload) (w *engine.World, ask func(event.EventType, any), keys func() (anyLost, all bool)) {
	t.Helper()
	w = engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	ask = func(et event.EventType, payload any) {
		ev := event.GameEvent{Type: et, Payload: payload}
		cursors.HandleEvent(ev)
		meta.HandleEvent(ev)
		for _, raised := range w.Resources.Event.Queue.Consume() {
			meta.HandleEvent(raised)
		}
	}
	for slot, p := range rostered {
		p.Slot, p.X, p.Y, p.PeerID, p.Control = uint8(slot), 5+slot*10, 5, uint32(slot)+1, uint8(component.ControlRemote)
		ask(event.EventCursorSpawnRequest, &p)
	}
	status := w.Resources.Status.Bools
	return w, ask, func() (bool, bool) {
		return status.Get("session.any_defeated").Load(), status.Get("session.all_defeated").Load()
	}
}

// TestABotsDefeatEndsNoGameBesideAPerson: a seat's defeat and its crush leave both
// keys unset while a person's cursor plays, and that person's defeat sets both.
func TestABotsDefeatEndsNoGameBesideAPerson(t *testing.T) {
	w, ask, keys := defeatFold(t, event.CursorSpawnRequestPayload{}, event.CursorSpawnRequestPayload{Holder: 1, Bot: true})
	person, seat := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: seat, Defeated: true})
	ask(event.EventCursorCrushed, &event.CursorCrushedPayload{Entity: seat})
	if anyLost, all := keys(); anyLost || all {
		t.Fatalf("a seat defeated and crushed beside a person: any defeated %t, all %t; want neither", anyLost, all)
	}
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: person, Defeated: true})
	if anyLost, all := keys(); !anyLost || !all {
		t.Fatalf("the one person in play defeated: any defeated %t, all %t; want both", anyLost, all)
	}
}

// TestABotsDefeatEndsTheGameWhereOnlyBotsPlay: with no person's cursor in play the
// bots' decide as persons' would, a bot's own run's and its seat's alike.
func TestABotsDefeatEndsTheGameWhereOnlyBotsPlay(t *testing.T) {
	w, ask, keys := defeatFold(t, event.CursorSpawnRequestPayload{Bot: true}, event.CursorSpawnRequestPayload{Holder: 1, Bot: true})
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(1), Defeated: true})
	if anyLost, all := keys(); !anyLost || all {
		t.Fatalf("one of two bots defeated with no person in play: any defeated %t, all %t; want any alone", anyLost, all)
	}
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(0), Defeated: true})
	if anyLost, all := keys(); !anyLost || !all {
		t.Fatalf("both bots defeated with no person in play: any defeated %t, all %t; want both", anyLost, all)
	}
}

// TestAPersonAwayAsAWraithIsNoPersonInPlay: a defeated bot ends the game as the
// person beside it turns Wraith, the bot alone in play, and no longer once that
// person has taken the Wraith back.
func TestAPersonAwayAsAWraithIsNoPersonInPlay(t *testing.T) {
	w, ask, keys := defeatFold(t, event.CursorSpawnRequestPayload{}, event.CursorSpawnRequestPayload{Bot: true})
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(1), Defeated: true})
	ask(event.EventCursorWraithRequest, &event.CursorWraithRequestPayload{Slot: 0})
	if anyLost, all := keys(); !anyLost || !all {
		t.Fatalf("a defeated bot beside a person's Wraith: any defeated %t, all %t; want both", anyLost, all)
	}
	ask(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{Slot: 0, PeerID: 1, Control: uint8(component.ControlRemote)})
	if anyLost, all := keys(); anyLost || all || w.Components.Wraith.CountEntities() != 0 {
		t.Fatalf("the person back beside the defeated bot: any defeated %t, all %t, %d Wraith(s); want neither and none",
			anyLost, all, w.Components.Wraith.CountEntities())
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
		shield.Active, shield.Radius = true, 1
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
	snakes := ready(NewSnakeSystem(w)).(*SnakeSystem)
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
	shield.Active, shield.Radius = true, 1
	if d, h := strikes(true); d != 1 || h != 0 {
		t.Fatalf("shielded cursor on the head = (%d drains, %d heats), want (1, 0)", d, h)
	}
}
