package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
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

// defeatFold is a world of the given tick rostering one cursor a payload, in slot
// order as participants 1 on, with ask, which hands an event to the cursor system
// and to the meta system that folds, then what the first raised to the second, the
// fold's two keys, and pass, which plays game ticks: the meta system's update and
// what it asks for.
func defeatFold(t *testing.T, tick time.Duration, rostered ...event.CursorSpawnRequestPayload) (w *engine.World, ask func(event.EventType, any), keys func() (anyLost, all bool), pass func(ticks uint64)) {
	t.Helper()
	w = engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	ctx.SetTickInterval(tick)
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
	keys = func() (bool, bool) {
		return status.Get("session.any_defeated").Load(), status.Get("session.all_defeated").Load()
	}
	return w, ask, keys, func(ticks uint64) {
		for range ticks {
			w.Resources.Game.State.IncrementGameTicks()
			meta.Update()
			for _, raised := range w.Resources.Event.Queue.Consume() {
				ask(raised.Type, raised.Payload)
			}
		}
	}
}

// TestTheDefeatKeysFoldOnlyTheCursorsWhoseDefeatDecides: a bot's defeat and its
// crush set neither key while a person holds a place, in play or away as a Wraith,
// and that person's own defeat sets both. With no person on the roster the bots'
// decide as persons' would, a bot's own run's and its seat's alike.
func TestTheDefeatKeysFoldOnlyTheCursorsWhoseDefeatDecides(t *testing.T) {
	w, ask, keys, _ := defeatFold(t, parameter.GameUpdateInterval,
		event.CursorSpawnRequestPayload{}, event.CursorSpawnRequestPayload{Bot: true})
	person, bot := w.Resources.Player.Slot(0), w.Resources.Player.Slot(1)
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: bot, Defeated: true})
	ask(event.EventCursorCrushed, &event.CursorCrushedPayload{Entity: bot})
	if anyLost, all := keys(); anyLost || all {
		t.Fatalf("a bot defeated and crushed beside a person: any defeated %t, all %t; want neither", anyLost, all)
	}
	ask(event.EventCursorWraithRequest, &event.CursorWraithRequestPayload{Slot: 0})
	if anyLost, all := keys(); anyLost || all {
		t.Fatalf("a defeated bot beside a person's Wraith: any defeated %t, all %t; want neither", anyLost, all)
	}
	ask(event.EventCursorSpawnRequest, &event.CursorSpawnRequestPayload{Slot: 0, PeerID: 1, Control: uint8(component.ControlRemote)})
	if anyLost, all := keys(); anyLost || all {
		t.Fatalf("the person back beside the defeated bot: any defeated %t, all %t; want neither", anyLost, all)
	}
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: person, Defeated: true})
	if anyLost, all := keys(); !anyLost || !all {
		t.Fatalf("the one person, back in play and defeated: any defeated %t, all %t; want both", anyLost, all)
	}

	w, ask, keys, _ = defeatFold(t, parameter.GameUpdateInterval,
		event.CursorSpawnRequestPayload{Bot: true}, event.CursorSpawnRequestPayload{Holder: 1, Bot: true})
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(1), Defeated: true})
	if anyLost, all := keys(); !anyLost || all {
		t.Fatalf("one of two bots defeated with no person on the roster: any defeated %t, all %t; want any alone", anyLost, all)
	}
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(0), Defeated: true})
	if anyLost, all := keys(); !anyLost || !all {
		t.Fatalf("both bots defeated with no person on the roster: any defeated %t, all %t; want both", anyLost, all)
	}
}

// TestABotDownBesideAPersonComesBack: a seat defeated or crushed beside a person is
// equipped again BotRespawn of game time later, counted in the run's tick, as a new
// game makes it: at the template's cell with its heat and energy, its defeat lifted.
// A person's cursor is left as it fell, and so is a bot's where no person holds a
// place, whose defeat decides.
func TestABotDownBesideAPersonComesBack(t *testing.T) {
	start := event.CursorSpawnRequestPayload{Heat: 10, Energy: 100}
	seated := event.CursorSpawnRequestPayload{Heat: 10, Energy: 100, Holder: 1, Bot: true}
	// down drains a cursor where it has wandered to, as a defeat or a crush leaves it
	down := func(w *engine.World, e core.Entity) {
		w.Components.Energy.SetComponent(e, component.EnergyComponent{})
		w.Components.Heat.SetComponent(e, component.HeatComponent{})
		w.Positions.SetPosition(e, component.PositionComponent{X: 30, Y: 20})
	}
	whole := func(w *engine.World, e core.Entity) bool {
		energy, _ := w.Components.Energy.GetComponent(e)
		heat, _ := w.Components.Heat.GetComponent(e)
		pos, _ := w.Positions.GetPosition(e)
		return energy.Current == 100 && heat.Current == 10 && pos.X == 5 && pos.Y == 5
	}
	falls := map[string]func(ask func(event.EventType, any), e core.Entity){
		"defeated": func(ask func(event.EventType, any), e core.Entity) {
			ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: e, Defeated: true})
		},
		"crushed": func(ask func(event.EventType, any), e core.Entity) {
			ask(event.EventCursorCrushed, &event.CursorCrushedPayload{Entity: e})
		},
	}

	for _, tick := range []time.Duration{parameter.GameUpdateInterval, 20 * time.Millisecond} {
		due := uint64(parameter.BotRespawn / tick)
		for name, fall := range falls {
			w, ask, keys, pass := defeatFold(t, tick, start, seated)
			seat := w.Resources.Player.Slot(1)
			down(w, seat)
			fall(ask, seat)
			pass(due - 1)
			if whole(w, seat) {
				t.Fatalf("%s at a %s tick: the seat is back before its %d ticks are up", name, tick, due)
			}
			pass(1)
			if !whole(w, seat) {
				pos, _ := w.Positions.GetPosition(seat)
				energy, _ := w.Components.Energy.GetComponent(seat)
				t.Fatalf("%s at a %s tick: after %d ticks the seat stands at %d,%d with energy %d; want it equipped again at 5,5 with 100",
					name, tick, due, pos.X, pos.Y, energy.Current)
			}
			// Its defeat has lifted: a seat still down would end the game as the person goes
			ask(event.EventCursorDespawnRequest, &event.CursorDespawnRequestPayload{Slot: 0})
			if anyLost, all := keys(); anyLost || all {
				t.Fatalf("%s at a %s tick: the person gone, the seat that came back reads any defeated %t, all %t; want neither",
					name, tick, anyLost, all)
			}
		}
	}

	wait := 3 * uint64(parameter.BotRespawn/parameter.GameUpdateInterval)
	w, ask, _, pass := defeatFold(t, parameter.GameUpdateInterval, start, start)
	person := w.Resources.Player.Slot(1)
	down(w, person)
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: person, Defeated: true})
	pass(wait)
	if whole(w, person) {
		t.Fatal("a person's cursor was equipped again; only a bot's is")
	}

	seated.Holder = 0
	w, ask, keys, pass := defeatFold(t, parameter.GameUpdateInterval, seated, seated)
	alone := w.Resources.Player.Slot(1)
	down(w, alone)
	ask(event.EventCursorDefeatState, &event.CursorDefeatStatePayload{Entity: alone, Defeated: true})
	pass(wait)
	if anyLost, _ := keys(); whole(w, alone) || !anyLost {
		t.Fatalf("with no person on the roster a bot down was equipped again %t and reads any defeated %t; want it left and its defeat to decide", whole(w, alone), anyLost)
	}
}

// TestABotsTimeDownIsCarriedByACapture: an instance that takes the meta system's
// record mid-wait asks for the bot back at the tick the others do.
func TestABotsTimeDownIsCarriedByACapture(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	for slot, bot := range []bool{false, true} {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest, Payload: &event.CursorSpawnRequestPayload{
			Slot: uint8(slot), X: 5, Y: 5, Heat: 10, Energy: 100, PeerID: uint32(slot) + 1, Control: uint8(component.ControlRemote), Bot: bot}})
	}
	w.Resources.Event.Queue.Consume()
	state, due := w.Resources.Game.State, uint64(parameter.BotRespawn/w.TickInterval())
	state.SetGameTicks(100)
	meta.HandleEvent(event.GameEvent{Type: event.EventCursorDefeatState,
		Payload: &event.CursorDefeatStatePayload{Entity: w.Resources.Player.Slot(1), Defeated: true, ProducedTick: 100}})
	record, err := meta.SaveShared()
	if err != nil {
		t.Fatal(err)
	}

	taken := ready(NewMetaSystem(ctx)).(*MetaSystem)
	if err := taken.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	asked := func() int {
		n := 0
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.CursorRespawnRequestPayload); ok && p.Slot == 1 {
				n++
			}
		}
		return n
	}
	state.SetGameTicks(100 + due - 1)
	taken.Update()
	if n := asked(); n != 0 {
		t.Fatalf("a tick early the record's taker asked for the bot back %d times", n)
	}
	state.SetGameTicks(100 + due)
	taken.Update()
	if n := asked(); n != 1 {
		t.Fatalf("at its tick the record's taker asked for the bot back %d times, want once", n)
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
