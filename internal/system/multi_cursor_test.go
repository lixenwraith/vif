package system

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
)

func TestSnakePursuitKeepsItsRouteUntilAReachableTargetIsMuchCloser(t *testing.T) {
	w, first, second := testCursorWorld(t)
	w.SetupLevel(80, 40, false, false, false)
	w.Positions.SetPosition(first, component.PositionComponent{X: 10, Y: 10})
	w.Positions.SetPosition(second, component.PositionComponent{X: 70, Y: 10})
	for y := range 30 {
		spawnWall(w, 30, y)
		spawnWall(w, 50, y)
	}
	head := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(head, component.PositionComponent{X: 40, Y: 10})
	w.Components.SnakeHead.SetComponent(head, component.SnakeHeadComponent{})
	w.Components.Navigation.SetComponent(head, component.NavigationComponent{
		Width: parameter.SnakeHeadWidth, Height: parameter.SnakeHeadHeight,
		FlowLookahead: parameter.NavFlowLookaheadDefault,
	})
	motion := component.KineticComponent{}
	motion.PreciseX, motion.PreciseY = 40.5, 10.5
	w.Components.Kinetic.SetComponent(head, motion)
	s := ready(NewNavigationSystem(w)).(*NavigationSystem)
	s.Update()
	nav, _ := w.Components.Navigation.GetPtr(head)
	if nav.LockedTarget == 0 || nav.HasDirectPath {
		t.Fatalf("expected a routed pursuit behind walls: %+v", nav)
	}
	locked := nav.LockedTarget
	for tick := range 12 {
		x := 39 + tick%3
		w.Positions.SetPosition(head, component.PositionComponent{X: x, Y: 10})
		kinetic, _ := w.Components.Kinetic.GetPtr(head)
		kinetic.PreciseX = float64(x) + 0.5
		s.Update()
		if nav.LockedTarget != locked {
			t.Fatal("small distance changes switched the snake between hidden cursors")
		}
		field := s.groups[s.getEntityGroup(head)].compositeFlowCache
		fx, fy := s.getCompositeFlowDirection(kinetic.PreciseX, kinetic.PreciseY, field)
		if (fx == 0 && fy == 0) || nav.FlowX != fx || nav.FlowY != fy {
			t.Fatal("snake steering disagrees with its locked target's route")
		}
	}
	other := first
	if locked == first {
		other = second
	}
	w.Positions.SetPosition(other, component.PositionComponent{X: 43, Y: 10})
	s.Update()
	if nav.LockedTarget != other || !nav.HasDirectPath {
		t.Fatal("a substantially closer reachable cursor did not replace the pursuit")
	}
	state := w.CaptureSharedWorld()
	phase, err := s.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	ready(NewCursorSystem(w)).(*CursorSystem).despawn(&event.CursorDespawnRequestPayload{Entity: other})
	s.Update()
	if nav.LockedTarget != locked {
		t.Fatal("departed cursor remained the pursuit target")
	}
	w.InstallSharedWorld(state)
	if err := s.LoadShared(phase); err != nil {
		t.Fatal(err)
	}
	nav, _ = w.Components.Navigation.GetPtr(head)
	back, err := s.SaveShared()
	if err != nil || !bytes.Equal(phase, back) || nav.LockedTarget != other {
		t.Fatal("snapshot lost pursuit identity or routing phase")
	}
}

// ready initialises a system constructed outside the engine, as World.InitSystems
// does for the registered set at startup and on reset
func ready[S engine.System](s S) S {
	s.Init()
	return s
}

func testCursorWorld(t *testing.T) (*engine.World, core.Entity, core.Entity) {
	t.Helper()

	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 5, Y: 5, Slot: 0, Control: uint8(component.ControlLocal),
		},
	})
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: 15, Y: 5, Slot: 1, Control: uint8(component.ControlLocal),
		},
	})

	first := w.Resources.Player.Slot(0)
	second := w.Resources.Player.Slot(1)
	if first == 0 || second == 0 || first == second {
		t.Fatalf("cursor roster = (%d, %d), want two distinct entities", first, second)
	}
	w.Resources.Event.Queue.Consume()
	return w, first, second
}

// spawnRemoteCursor adds one peer-owned cursor to an existing roster, so the remote
// lifecycle is verified against two local cursors without a socket.
func spawnRemoteCursor(t *testing.T, w *engine.World, slot uint8, x, y int, peer uint32) core.Entity {
	t.Helper()

	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: x, Y: y, Slot: slot, Control: uint8(component.ControlRemote), PeerID: peer,
		},
	})
	w.Resources.Event.Queue.Consume()

	e := w.Resources.Player.Slot(slot)
	if e == 0 {
		t.Fatalf("remote cursor did not occupy slot %d", slot)
	}
	c, ok := w.Components.Cursor.GetComponent(e)
	if !ok || c.Control != component.ControlRemote || c.PeerID != peer {
		t.Fatalf("remote cursor component = %#v, want remote control for peer %d", c, peer)
	}
	return e
}

// TestRemoteCursorJoinsTheRosterWithoutBecomingLocal asserts the roster half of
// item 3: a remote cursor is a full shared entity that contested mechanics see,
// and it never becomes this instance's binding.
func TestRemoteCursorJoinsTheRosterWithoutBecomingLocal(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)

	roster := w.Resources.Player
	if roster.Count() != 3 {
		t.Fatalf("roster count = %d, want 3", roster.Count())
	}
	if roster.Entity != first || roster.LocalSlot() != 0 {
		t.Fatalf("local binding = (entity %d, slot %d), want (%d, 0)", roster.Entity, roster.LocalSlot(), first)
	}
	if roster.IsLocal(remote) {
		t.Fatalf("remote cursor %d reports as local", remote)
	}

	// Contested mechanics resolve over the whole roster, remote included
	if got, _, _, ok := ClosestCursor(w, 26, 5); !ok || got != remote {
		t.Fatalf("closest cursor = (%d, %t), want remote %d", got, ok, remote)
	}

	// Ping is pure local view: only the binding's bounds are recomputed (D-13)
	if ping, ok := w.Components.Ping.GetComponent(remote); !ok || ping.BoundsActive {
		t.Fatalf("remote ping = %#v, want present and inactive", ping)
	}
}

// TestADepartedCursorWaitsAsAWraith: a Wraith keeps its entity, slot, owner and the
// cell it stood on, and holds nothing the world reaches a cursor through, so no
// contested mechanic finds it. Turning it is announced as the cursor leaving play,
// once: its wait starts when it turns.
func TestADepartedCursorWaitsAsAWraith(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	turn := func() []event.GameEvent {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorWraithRequest,
			Payload: &event.CursorWraithRequestPayload{Slot: 2}})
		return w.Resources.Event.Queue.Consume()
	}

	since := w.Resources.Time.GameTime
	events := turn()
	if got := w.Resources.Player.Slot(2); got != remote {
		t.Fatalf("slot 2 holds %d after its cursor turned Wraith, want the same entity %d", got, remote)
	}
	if c, ok := w.Components.Cursor.GetComponent(remote); !ok || c.PeerID != 7 || c.Control != component.ControlRemote {
		t.Fatalf("the Wraith's cursor = %+v, want peer 7 driving it remotely", c)
	}
	wraith, ok := w.Components.Wraith.GetComponent(remote)
	if !ok || wraith.X != 25 || wraith.Y != 5 || !wraith.Since.Equal(since) {
		t.Fatalf("Wraith = %+v (held %t), want cell 25,5 since %v", wraith, ok, since)
	}
	if mask, _ := w.GetComponentMask(remote); mask != engine.CursorBit|engine.ProtectionBit|engine.WraithBit {
		t.Fatalf("the Wraith holds components %b, want its cursor, protection and Wraith alone", mask)
	}
	if got, _, _, ok := ClosestCursor(w, 25, 5); ok && got == remote {
		t.Fatal("a contested mechanic found the Wraith at the cell it holds")
	}
	if len(events) != 1 || events[0].Type != event.EventCursorDespawned {
		t.Fatalf("turning Wraith announced %v, want one cursor leaving play", events)
	}
	if p, _ := events[0].Payload.(*event.CursorDespawnedPayload); p == nil || p.Entity != remote || p.Slot != 2 {
		t.Fatalf("the announcement names %+v, want entity %d in slot 2", events[0].Payload, remote)
	}

	w.Resources.Time.GameTime = since.Add(time.Minute)
	if events := turn(); len(events) != 0 {
		t.Fatalf("a second request announced %v, want nothing", events)
	}
	if wraith, _ := w.Components.Wraith.GetComponent(remote); !wraith.Since.Equal(since) {
		t.Fatalf("a second request restarted the wait at %v, want %v", wraith.Since, since)
	}
}

// TestAnArrivalIntoItsOwnWraithTakesItBack: a participant's spawn into the slot its
// Wraith keeps is that cursor again, at the cell it held and with what its player
// brought back over the template; another participant's finds the slot taken.
func TestAnArrivalIntoItsOwnWraithTakesItBack(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{Type: event.EventCursorWraithRequest,
		Payload: &event.CursorWraithRequestPayload{Slot: 2}})
	w.Resources.Event.Queue.Consume()
	record := event.CursorRecord{Charges: []int{0, 3}, Energy: 4321, Heat: 9, HitPoints: 37}
	arrive := func(peer uint32) {
		cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest, Payload: &event.CursorSpawnRequestPayload{
			Slot: 2, Center: true, Control: uint8(component.ControlRemote), PeerID: peer, Energy: 100, Record: &record}})
		w.Resources.Event.Queue.Consume()
	}

	arrive(8)
	if !w.Components.Wraith.HasEntity(remote) || w.Resources.Player.Slot(2) != remote {
		t.Fatal("another participant's arrival took the Wraith's place")
	}
	arrive(7)
	if w.Resources.Player.Slot(2) != remote || w.Components.Wraith.HasEntity(remote) {
		t.Fatalf("slot 2 holds %d, a Wraith %t; want entity %d taken back", w.Resources.Player.Slot(2),
			w.Components.Wraith.HasEntity(remote), remote)
	}
	if pos, ok := w.Positions.GetPosition(remote); !ok || pos.X != 25 || pos.Y != 5 {
		t.Fatalf("the cursor taken back stands at %+v, want the cell its Wraith held, 25,5", pos)
	}
	energy, _ := w.Components.Energy.GetComponent(remote)
	heat, _ := w.Components.Heat.GetComponent(remote)
	combat, _ := w.Components.Combat.GetComponent(remote)
	weapon, _ := w.Components.Weapon.GetComponent(remote)
	if energy.Current != record.Energy || heat.Current != record.Heat || combat.HitPoints != record.HitPoints ||
		weapon.Charges[1] != record.Charges[1] {
		t.Fatalf("taken back with energy %d heat %d health %d charges %v; want the record %+v",
			energy.Current, heat.Current, combat.HitPoints, weapon.Charges, record)
	}
}

// TestRemoteCursorRejectsOwnerAuthoredWrites is the D-2 admission check: every
// owner-authored writer refuses a cursor this instance does not simulate, so a
// transported value is the single authority for that cell (D-13).
func TestRemoteCursorRejectsOwnerAuthoredWrites(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)

	if !w.SimulatesLocally(local) || w.SimulatesLocally(remote) {
		t.Fatalf("SimulatesLocally = (%t, %t), want (true, false)", w.SimulatesLocally(local), w.SimulatesLocally(remote))
	}
	if got := w.ResolveOwnedCursor(remote); got != 0 {
		t.Fatalf("ResolveOwnedCursor(remote) = %d, want 0", got)
	}

	energy := ready(NewEnergySystem(w)).(*EnergySystem)
	heat := ready(NewHeatSystem(w)).(*HeatSystem)
	shield := ready(NewShieldSystem(w)).(*ShieldSystem)
	boost := ready(NewBoostSystem(w)).(*BoostSystem)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)

	for _, ev := range []event.GameEvent{
		{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Entity: remote, Value: 42}},
		{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Entity: remote, Value: 37}},
		{Type: event.EventShieldActivate, Payload: &event.ShieldActivatePayload{Entity: remote}},
		{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Entity: remote, Duration: time.Second}},
		{Type: event.EventWeaponAddRequest, Payload: &event.WeaponAddRequestPayload{Entity: remote, Weapon: component.WeaponRod}},
	} {
		energy.HandleEvent(ev)
		heat.HandleEvent(ev)
		shield.HandleEvent(ev)
		boost.HandleEvent(ev)
		weapon.HandleEvent(ev)
	}

	remoteEnergy, _ := w.Components.Energy.GetComponent(remote)
	remoteHeat, _ := w.Components.Heat.GetComponent(remote)
	remoteShield, _ := w.Components.Shield.GetComponent(remote)
	remoteBoost, _ := w.Components.Boost.GetComponent(remote)
	remoteWeapon, _ := w.Components.Weapon.GetComponent(remote)
	if remoteEnergy.Current != 0 || remoteHeat.Current != 0 || remoteShield.Active ||
		remoteBoost.Active || remoteWeapon.Charges[component.WeaponRod] != 0 {
		t.Fatalf("remote cursor was written locally: energy %d heat %d shield %t boost %t rod %d",
			remoteEnergy.Current, remoteHeat.Current, remoteShield.Active,
			remoteBoost.Active, remoteWeapon.Charges[component.WeaponRod])
	}

	// Non-vacuous: the same commands land on a cursor this instance owns
	for _, ev := range []event.GameEvent{
		{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Entity: local, Value: 42}},
		{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Entity: local, Value: 37}},
		{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Entity: local, Duration: time.Second}},
	} {
		energy.HandleEvent(ev)
		heat.HandleEvent(ev)
		boost.HandleEvent(ev)
	}
	localEnergy, _ := w.Components.Energy.GetComponent(local)
	localHeat, _ := w.Components.Heat.GetComponent(local)
	localBoost, _ := w.Components.Boost.GetComponent(local)
	if localEnergy.Current != 42 || localHeat.Current != 37 || !localBoost.Active {
		t.Fatalf("owned cursor state = (energy %d, heat %d, boost %t), want (42, 37, true)",
			localEnergy.Current, localHeat.Current, localBoost.Active)
	}
}

// TestRemoteCursorStateDoesNotAgeLocally covers the other half of the admission
// check: the per-tick loops that would otherwise decay a transported value.
func TestRemoteCursorStateDoesNotAgeLocally(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)

	boost := ready(NewBoostSystem(w)).(*BoostSystem)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	shield := ready(NewShieldSystem(w)).(*ShieldSystem)

	// A transported snapshot: both cursors arrive at the same values
	for _, e := range []core.Entity{local, remote} {
		b, _ := w.Components.Boost.GetPtr(e)
		b.Active, b.Remaining, b.TotalDuration = true, time.Second, time.Second
		wp, _ := w.Components.Weapon.GetPtr(e)
		wp.MainFireCooldown = time.Second
		sh, _ := w.Components.Shield.GetPtr(e)
		sh.Active = true
		sh.LastDrainTime = w.Resources.Time.GameTime.Add(-time.Hour)
	}

	w.Resources.Time.DeltaTime = 100 * time.Millisecond
	boost.Update()
	weapon.Update()
	shield.Update()

	remoteBoost, _ := w.Components.Boost.GetComponent(remote)
	remoteWeapon, _ := w.Components.Weapon.GetComponent(remote)
	if remoteBoost.Remaining != time.Second || remoteWeapon.MainFireCooldown != time.Second {
		t.Fatalf("remote cursor aged locally: boost %v cooldown %v",
			remoteBoost.Remaining, remoteWeapon.MainFireCooldown)
	}

	localBoost, _ := w.Components.Boost.GetComponent(local)
	localWeapon, _ := w.Components.Weapon.GetComponent(local)
	if localBoost.Remaining != 900*time.Millisecond || localWeapon.MainFireCooldown != 900*time.Millisecond {
		t.Fatalf("owned cursor did not age: boost %v cooldown %v",
			localBoost.Remaining, localWeapon.MainFireCooldown)
	}

	// The shield drain is an energy command; exactly one cursor may produce it
	drains := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.EnergyAddPayload); ok && ev.Type == event.EventEnergyAddRequest {
			if p.Entity == remote {
				t.Fatalf("shield drain produced for remote cursor %d", remote)
			}
			drains++
		}
	}
	if drains != 1 {
		t.Fatalf("shield drain requests = %d, want 1", drains)
	}
}

func TestClosestCursorUsesRoster(t *testing.T) {
	w, first, second := testCursorWorld(t)

	got, _, _, ok := ClosestCursor(w, 14, 5)
	if !ok || got != second {
		t.Fatalf("closest cursor = (%d, %t), want (%d, true)", got, ok, second)
	}

	got, _, _, ok = ClosestCursor(w, 10, 5)
	if !ok || got != first {
		t.Fatalf("tie cursor = (%d, %t), want slot-zero entity %d", got, ok, first)
	}
}

func TestCursorOverlapIncludesEveryTouchingPlayer(t *testing.T) {
	w, first, second := testCursorWorld(t)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorMoveRequest,
		Payload: &event.CursorMoveRequestPayload{
			Entity: second,
			X:      5,
			Y:      5,
		},
	})
	w.Resources.Event.Queue.Consume()

	entity := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(entity, component.PositionComponent{X: 5, Y: 5})

	overlaps := CheckCursorOverlaps(w, entity)
	if overlaps.Count != 2 {
		t.Fatalf("overlap count = %d, want 2", overlaps.Count)
	}
	if overlaps.Entries[0].Cursor != first || overlaps.Entries[1].Cursor != second {
		t.Fatalf("overlap cursors = (%d, %d), want (%d, %d)", overlaps.Entries[0].Cursor, overlaps.Entries[1].Cursor, first, second)
	}
	if !overlaps.Entries[0].OnCursor || !overlaps.Entries[1].OnCursor {
		t.Fatalf("overlap flags = (%t, %t), want both true", overlaps.Entries[0].OnCursor, overlaps.Entries[1].OnCursor)
	}
}

func TestCursorMoveRejectsZeroEntity(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)

	cursors.HandleEvent(event.GameEvent{
		Type:    event.EventCursorMoveRequest,
		Payload: &event.CursorMoveRequestPayload{X: 9, Y: 8},
	})

	pos, ok := w.Positions.GetPosition(first)
	if !ok || pos.X != 5 || pos.Y != 5 {
		t.Fatalf("local cursor moved to %#v from a zero-entity command", pos)
	}
}

func TestNavigationGroupZeroPublishesRoster(t *testing.T) {
	w, first, second := testCursorWorld(t)
	navigation := ready(NewNavigationSystem(w)).(*NavigationSystem)
	navigation.resolveGroupTargets()

	state := w.Resources.Target.GetGroup(0)
	if !state.Valid || state.Count != 2 {
		t.Fatalf("group zero = %#v, want two valid targets", state)
	}
	if state.Targets[0].Entity != first || state.Targets[1].Entity != second {
		t.Fatalf("group-zero entities = (%d, %d), want (%d, %d)", state.Targets[0].Entity, state.Targets[1].Entity, first, second)
	}

	empty := engine.NewWorld()
	engine.NewGameContextWithClock(empty, 40, 24, engine.NewManualClock())
	emptyNavigation := ready(NewNavigationSystem(empty)).(*NavigationSystem)
	emptyNavigation.resolveGroupTargets()
	if state := empty.Resources.Target.GetGroup(0); state.Valid || state.Count != 0 {
		t.Fatalf("empty group zero = %#v, want invalid", state)
	}
}

// TestShotContactStrikesOnlyTheTouchedOwnedCursor: a hosted shot addresses the
// cursor it touched, shield before cell, and a remote cursor's hit is its owner's.
func TestShotContactStrikesOnlyTheTouchedOwnedCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 9)
	damage := component.CursorDamage{EnergyDrain: -7, HeatDelta: -11}

	if hit := CursorContactAt(w, 15, 5); hit != second {
		t.Fatalf("contact at slot-one cell = %d, want %d", hit, second)
	}
	strikeCursor(w, second, damage)
	events := w.Resources.Event.Queue.Consume()
	heat, ok := events[0].Payload.(*event.HeatAddRequestPayload)
	if len(events) != 1 || !ok || heat.Entity != second || heat.Delta != damage.HeatDelta {
		t.Fatalf("direct-hit events = %#v, want one heat command for %d", events, second)
	}

	shield, _ := w.Components.Shield.GetComponent(first)
	shield.Active, shield.InvRxSq, shield.InvRySq = true, 1, 1
	w.Components.Shield.SetComponent(first, shield)
	if hit := CursorContactAt(w, 5, 5); hit != first {
		t.Fatalf("contact at shielded cell = %d, want %d", hit, first)
	}
	strikeCursor(w, first, damage)
	events = w.Resources.Event.Queue.Consume()
	drain, ok := events[0].Payload.(*event.ShieldDrainRequestPayload)
	if len(events) != 1 || !ok || drain.Entity != first || drain.Value != damage.EnergyDrain {
		t.Fatalf("shield-hit events = %#v, want one shield command for %d", events, first)
	}

	if hit := CursorContactAt(w, 25, 5); hit != remote {
		t.Fatalf("contact at remote cell = %d, want %d", hit, remote)
	}
	strikeCursor(w, remote, damage)
	if events = w.Resources.Event.Queue.Consume(); len(events) != 0 {
		t.Fatalf("remote-hit events = %#v, want none: its owner applies it", events)
	}
}

func TestCombatQueriesExcludeEveryCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	if HasCombatTargetAt(w, 15, 5, engine.ScopeBoth, 0, first) {
		t.Fatalf("slot-one cursor %d was treated as a combat target", second)
	}

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 16, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        1,
	})
	targets := FindNearestTargets(w, 15, 5, 1, engine.ScopeBoth, first)
	if len(targets) != 1 || targets[0].Target != target {
		t.Fatalf("nearest targets = %#v, want only combat target %d", targets, target)
	}
}

func TestPlayerCommandsMutateOnlyAddressedCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	energy := ready(NewEnergySystem(w)).(*EnergySystem)
	heat := ready(NewHeatSystem(w)).(*HeatSystem)
	shield := ready(NewShieldSystem(w)).(*ShieldSystem)
	boost := ready(NewBoostSystem(w)).(*BoostSystem)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)

	// Zero is invalid rather than an alias for the local cursor.
	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Value: 99}})
	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Value: 88}})
	shield.HandleEvent(event.GameEvent{Type: event.EventShieldActivate, Payload: &event.ShieldActivatePayload{}})
	boost.HandleEvent(event.GameEvent{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Duration: 2 * time.Second}})
	weapon.HandleEvent(event.GameEvent{Type: event.EventWeaponAddRequest, Payload: &event.WeaponAddRequestPayload{Weapon: component.WeaponRod}})

	energy.HandleEvent(event.GameEvent{Type: event.EventEnergySetRequest, Payload: &event.EnergySetPayload{Entity: second, Value: 42}})
	heat.HandleEvent(event.GameEvent{Type: event.EventHeatSetRequest, Payload: &event.HeatSetRequestPayload{Entity: second, Value: 37}})
	shield.HandleEvent(event.GameEvent{Type: event.EventShieldActivate, Payload: &event.ShieldActivatePayload{Entity: second}})
	boost.HandleEvent(event.GameEvent{Type: event.EventBoostActivate, Payload: &event.BoostActivatePayload{Entity: second, Duration: time.Second}})
	weapon.HandleEvent(event.GameEvent{Type: event.EventWeaponAddRequest, Payload: &event.WeaponAddRequestPayload{Entity: second, Weapon: component.WeaponRod}})

	firstEnergy, _ := w.Components.Energy.GetComponent(first)
	secondEnergy, _ := w.Components.Energy.GetComponent(second)
	if firstEnergy.Current != 0 || secondEnergy.Current != 42 {
		t.Fatalf("energy = (%d, %d), want (0, 42)", firstEnergy.Current, secondEnergy.Current)
	}

	firstHeat, _ := w.Components.Heat.GetComponent(first)
	secondHeat, _ := w.Components.Heat.GetComponent(second)
	if firstHeat.Current != 0 || secondHeat.Current != 37 {
		t.Fatalf("heat = (%d, %d), want (0, 37)", firstHeat.Current, secondHeat.Current)
	}

	firstShield, _ := w.Components.Shield.GetComponent(first)
	secondShield, _ := w.Components.Shield.GetComponent(second)
	if firstShield.Active || !secondShield.Active {
		t.Fatalf("shield active = (%t, %t), want (false, true)", firstShield.Active, secondShield.Active)
	}

	firstBoost, _ := w.Components.Boost.GetComponent(first)
	secondBoost, _ := w.Components.Boost.GetComponent(second)
	if firstBoost.Active || !secondBoost.Active || secondBoost.Remaining != time.Second {
		t.Fatalf("boost = (%#v, %#v), want only slot one active", firstBoost, secondBoost)
	}

	firstWeapon, _ := w.Components.Weapon.GetComponent(first)
	secondWeapon, _ := w.Components.Weapon.GetComponent(second)
	if firstWeapon.Charges[component.WeaponRod] != 0 || secondWeapon.Charges[component.WeaponRod] != 1 {
		t.Fatalf("rod charges = (%d, %d), want (0, 1)", firstWeapon.Charges[component.WeaponRod], secondWeapon.Charges[component.WeaponRod])
	}
}

func TestSpeciesKillBoostRewardsOnlyCreditedCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	boost := ready(NewBoostSystem(w)).(*BoostSystem)

	boost.HandleEvent(event.GameEvent{
		Type: event.EventSpeciesKilled,
		Payload: &event.SpeciesKilledPayload{
			KillerEntity: second,
			Species:      component.SpeciesDrain,
		},
	})

	firstBoost, _ := w.Components.Boost.GetComponent(first)
	secondBoost, _ := w.Components.Boost.GetComponent(second)
	if firstBoost.Active || !secondBoost.Active || secondBoost.Remaining != parameter.BoostBaseDuration {
		t.Fatalf("boost after first kill = (%#v, %#v), want only credited cursor activated", firstBoost, secondBoost)
	}

	boost.HandleEvent(event.GameEvent{
		Type: event.EventSpeciesKilled,
		Payload: &event.SpeciesKilledPayload{
			KillerEntity: second,
			Species:      component.SpeciesSwarm,
		},
	})
	boost.HandleEvent(event.GameEvent{
		Type:    event.EventSpeciesKilled,
		Payload: &event.SpeciesKilledPayload{Species: component.SpeciesEye},
	})

	secondBoost, _ = w.Components.Boost.GetComponent(second)
	wantRemaining := parameter.BoostBaseDuration + parameter.BoostExtensionDuration
	if secondBoost.Remaining != wantRemaining || secondBoost.TotalDuration != wantRemaining {
		t.Fatalf("boost after second kill = %#v, want remaining and total %v", secondBoost, wantRemaining)
	}
}

func TestCombatRecordsCursorDamageCreditOnUnitAndAblativeHeader(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)

	unit := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(unit, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(unit, component.CombatComponent{
		OwnerEntity:      unit,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageTracer,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: unit, HitEntity: unit,
		AttackType: component.CombatAttackTracer,
	})
	unitCombat, _ := w.Components.Combat.GetComponent(unit)
	if unitCombat.HitPoints != 0 || unitCombat.LastDamagedBy != cursor {
		t.Fatalf("unit combat = %#v, want fatal credit for cursor %d", unitCombat, cursor)
	}

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 10, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 10, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeAblative,
		MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntityPylon,
	})
	w.Components.Combat.SetComponent(member, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntityPylon,
		HitPoints:        parameter.CombatDamageTracer,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: header, HitEntity: member,
		AttackType: component.CombatAttackTracer,
	})

	headerCombat, _ := w.Components.Combat.GetComponent(header)
	memberCombat, _ := w.Components.Combat.GetComponent(member)
	if memberCombat.HitPoints != 0 || memberCombat.LastDamagedBy != cursor || headerCombat.LastDamagedBy != cursor {
		t.Fatalf("ablative combat = header %#v member %#v, want fatal credit for cursor %d", headerCombat, memberCombat, cursor)
	}

	areaTarget := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(areaTarget, component.PositionComponent{X: 12, Y: 5})
	w.Components.Combat.SetComponent(areaTarget, component.CombatComponent{
		OwnerEntity:      areaTarget,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageExplosion,
	})
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: areaTarget, HitEntities: []core.Entity{areaTarget},
		AttackType: component.CombatAttackExplosion,
	})
	areaCombat, _ := w.Components.Combat.GetComponent(areaTarget)
	if areaCombat.HitPoints != 0 || areaCombat.LastDamagedBy != cursor {
		t.Fatalf("area combat = %#v, want fatal credit for cursor %d", areaCombat, cursor)
	}
}

func TestCombatTelemetryAttributesDamageAbsorptionAndChains(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatDamageTracer * 3,
	})
	payload := &event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackTracer,
	}
	combat.applyHitDirect(payload)

	targetCombat, _ := w.Components.Combat.GetPtr(target)
	targetCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
	combat.applyHitDirect(payload)

	reg := w.Resources.Status
	for key, want := range map[string]int64{
		"combat.damage_dealt":             parameter.CombatDamageTracer,
		"combat.damage_attacker_cursor":   parameter.CombatDamageTracer,
		"combat.damage_defender_drain":    parameter.CombatDamageTracer,
		"combat.absorbed_attacker_cursor": parameter.CombatDamageTracer,
		"combat.absorbed_defender_drain":  parameter.CombatDamageTracer,
		"combat.chain_followups":          2,
		"combat.chain_depth_total":        2,
		"combat.chain_depth_max":          1,
		"combat.hits_direct":              2,
	} {
		if got := reg.Ints.Get(key).Load(); got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
}

func TestCombatClearsStaleCursorCreditWhenSpeciesDealsFatalDamage(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)

	target := w.CreateEntity(core.DomainShared)
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntitySwarm,
		HitPoints:        parameter.CombatDamageTracer + parameter.CombatDamageEyeSelfDestruct,
	})
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackTracer,
	})

	targetCombat, _ := w.Components.Combat.GetComponent(target)
	targetCombat.RemainingDamageImmunity = 0
	w.Components.Combat.SetComponent(target, targetCombat)

	eye := w.CreateEntity(core.DomainShared)
	w.Components.Combat.SetComponent(eye, component.CombatComponent{
		OwnerEntity:      eye,
		CombatEntityType: component.CombatEntityEye,
	})
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: eye, OriginEntity: eye,
		TargetEntity: target, HitEntities: []core.Entity{target},
		AttackType: component.CombatAttackSelfDestruct,
	})

	targetCombat, _ = w.Components.Combat.GetComponent(target)
	if targetCombat.HitPoints != 0 || targetCombat.LastDamagedBy != 0 {
		t.Fatalf("target combat = %#v, want fatal non-cursor damage with no player credit", targetCombat)
	}
}

func TestATowersLossNamesNoKiller(t *testing.T) {
	w, _, killer := testCursorWorld(t)
	towers := ready(NewTowerSystem(w)).(*TowerSystem)

	header := w.CreateEntity(core.DomainShared)
	w.Components.Tower.SetComponent(header, component.TowerComponent{
		SpawnX: 7,
		SpawnY: 9,
		Type:   component.TowerCyan,
	})
	w.Components.Combat.SetComponent(header, component.CombatComponent{LastDamagedBy: killer})
	towers.handleTowerDeath(header)

	kills := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventSpeciesKilled {
			continue
		}
		payload, ok := ev.Payload.(*event.SpeciesKilledPayload)
		if !ok || payload.Entity != header || payload.KillerEntity != 0 ||
			payload.Species != component.SpeciesTower || payload.X != 7 || payload.Y != 9 {
			t.Fatalf("tower kill payload = %#v, want the region's loss with nobody credited", ev.Payload)
		}
		kills++
	}
	if kills != 1 {
		t.Fatalf("tower kill events = %d, want 1", kills)
	}
}

func TestNoPlayersWeaponTargetsTheTower(t *testing.T) {
	w, first, second := testCursorWorld(t)
	towers := ready(NewTowerSystem(w)).(*TowerSystem)
	towers.HandleEvent(event.GameEvent{
		Type:    event.EventTowerSpawnRequest,
		Payload: &event.TowerSpawnRequestPayload{X: 25, Y: 12, RadiusX: 2, RadiusY: 2},
	})
	headers := w.Components.Tower.Entities()
	if len(headers) != 1 {
		t.Fatalf("towers spawned = %d, want 1 with no cursor named", len(headers))
	}
	headerComp, _ := w.Components.Header.GetComponent(headers[0])
	member, _ := w.Positions.GetPosition(headerComp.MemberEntries[0].Entity)

	for _, cursor := range []core.Entity{first, second} {
		if HasCombatTargetAt(w, member.X, member.Y, engine.ScopeBoth, 0, cursor) {
			t.Fatalf("cursor %d targets the tower at (%d, %d)", cursor, member.X, member.Y)
		}
		if got := FindNearestTargets(w, float64(member.X), float64(member.Y), 1, engine.ScopeBoth, cursor); len(got) != 0 {
			t.Fatalf("cursor %d finds %#v next to the tower", cursor, got)
		}
	}
}

func TestATowersDeathIsNobodysKill(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{X: 5, Y: 5, Control: uint8(component.ControlLocal)}})
	cursor := w.Resources.Player.Slot(0)
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	boost := ready(NewBoostSystem(w)).(*BoostSystem)

	ev := event.GameEvent{Type: event.EventSpeciesKilled,
		Payload: &event.SpeciesKilledPayload{Species: component.SpeciesTower, KillerEntity: cursor}}
	meta.HandleEvent(ev)
	boost.HandleEvent(ev)

	kills := w.Resources.Status.Ints.Get("kills.tower").Load()
	total := w.Resources.Status.Ints.Get("kills.total").Load()
	active, _ := w.Components.Boost.GetComponent(cursor)
	if kills != 0 || total != 0 || active.Active {
		t.Fatalf("tower death counted %d (total %d), boost %v; want nothing for the players' own loss",
			kills, total, active.Active)
	}
}

func TestBoostRewardSurvivesStaleTypingDecision(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	boost := ready(NewBoostSystem(w)).(*BoostSystem)
	typing := ready(NewTypingSystem(w)).(*TypingSystem)

	// Pass one: the keystroke is dispatched ahead of the kills it shares a batch with.
	typing.applyUniversalRewards(cursor)
	for range 3 {
		boost.HandleEvent(event.GameEvent{
			Type:    event.EventSpeciesKilled,
			Payload: &event.SpeciesKilledPayload{KillerEntity: cursor, Species: component.SpeciesDrain},
		})
	}

	want := parameter.BoostBaseDuration + 2*parameter.BoostExtensionDuration
	got, _ := w.Components.Boost.GetComponent(cursor)
	if got.Remaining != want {
		t.Fatalf("boost after three kills = %v, want %v", got.Remaining, want)
	}

	// Pass two: the typing reward must extend the live boost, never truncate it.
	for _, ev := range w.Resources.Event.Queue.Consume() {
		boost.HandleEvent(ev)
	}
	want += parameter.BoostExtensionDuration
	got, _ = w.Components.Boost.GetComponent(cursor)
	if got.Remaining != want {
		t.Fatalf("boost after typing reward = %v, want %v", got.Remaining, want)
	}
}

func TestDirectDamageAppliesExactlyOnce(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatInitialHPDrain,
	})

	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackTracer,
	})

	got, _ := w.Components.Combat.GetComponent(target)
	want := parameter.CombatInitialHPDrain - parameter.CombatDamageTracer
	if got.HitPoints != want {
		t.Fatalf("hit points = %d, want %d (one application of %d)",
			got.HitPoints, want, parameter.CombatDamageTracer)
	}
	if n := combat.statDamage.Load(); n != int64(parameter.CombatDamageTracer) {
		t.Fatalf("combat.damage_dealt = %d, want %d", n, parameter.CombatDamageTracer)
	}
}

// TestDamageImmunityBudgetIsPerAttacker: the window belongs to the target, its
// budget to each attacker. One shared window divides a target's damage between the
// roster, and with the receive lead a guest's hits land inside the host's shadow.
func TestDamageImmunityBudgetIsPerAttacker(t *testing.T) {
	w, first, second := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity:      target,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        parameter.CombatInitialHPDrain,
	})

	hit := func(owner core.Entity) {
		combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
			OwnerEntity: owner, OriginEntity: owner,
			TargetEntity: target, HitEntity: target,
			AttackType: component.CombatAttackTracer,
		})
	}

	// Two cursors, one hit each, then a second round inside the same window.
	hit(first)
	hit(second)
	hit(first)
	hit(second)

	got, _ := w.Components.Combat.GetComponent(target)
	want := parameter.CombatInitialHPDrain - 2*parameter.CombatDamageTracer
	if got.HitPoints != want {
		t.Fatalf("hit points = %d, want %d: one hit per cursor per window", got.HitPoints, want)
	}
	if got.RemainingDamageImmunity != parameter.CombatDamageImmunityDuration {
		t.Fatalf("window = %v, want %v: a later attacker extended it",
			got.RemainingDamageImmunity, parameter.CombatDamageImmunityDuration)
	}
}

func TestKnockbackAndStunImmunityCoverAllPlayersAndWeapons(t *testing.T) {
	w, first, second := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Kinetic.SetComponent(target, component.KineticComponent{})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 1 << 20,
	})
	direct := func(owner core.Entity) {
		combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
			OwnerEntity: owner, OriginEntity: owner, TargetEntity: target, HitEntity: target,
			OriginVelX: 40, OriginVelY: 8, HasVelocity: true, AttackType: component.CombatAttackTracer,
		})
	}
	area := func(owner core.Entity, attack component.CombatAttackType) {
		combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
			OwnerEntity: owner, OriginEntity: owner, TargetEntity: target,
			HitEntities: []core.Entity{target}, AttackType: attack,
		})
	}
	direct(first)
	motion, _ := w.Components.Kinetic.GetComponent(target)
	if motion.VelX == 0 && motion.VelY == 0 {
		t.Fatal("first knockback did not land")
	}
	state, _ := w.Components.Combat.GetPtr(target)
	state.RemainingKineticImmunity /= 2
	// The strongest impulse these profiles draw: every later hit is weaker
	state.KineticStrength = parameter.CollisionKineticImpulseMax * profile.MassTracer / profile.MassSwarm
	remaining := state.RemainingKineticImmunity
	for _, owner := range []core.Entity{first, second} {
		direct(owner)
		area(owner, component.CombatAttackExplosion)
		got, _ := w.Components.Kinetic.GetComponent(target)
		if got != motion || state.RemainingKineticImmunity != remaining {
			t.Fatal("another weapon or player bypassed/refreshed knockback immunity")
		}
	}
	area(first, component.CombatAttackPulse)
	if state.Status[component.StatusStun].Remaining <= 0 {
		t.Fatal("first stun did not land")
	}
	state.Status[component.StatusStun].Remaining /= 2
	stun := state.Status[component.StatusStun].Remaining
	area(second, component.CombatAttackPulse)
	if state.Status[component.StatusStun].Remaining != stun {
		t.Fatal("another player's pulse refreshed stun immunity")
	}
	w.Resources.Time.DeltaTime = max(remaining, stun)
	combat.Update()
	area(second, component.CombatAttackExplosion)
	got, _ := w.Components.Kinetic.GetComponent(target)
	if got == motion || state.RemainingKineticImmunity == 0 {
		t.Fatal("knockback immunity did not reopen after expiry")
	}
}

// TestPassiveDrainSurvivesATransportedStamp covers the other way a per-cursor
// shield can stop draining: LastDrainTime is an absolute simulation instant and
// the component is captured, so a cursor materialised from an authority further
// along carries that authority's tick. Game time is a pure function of the tick,
// so the deadline is unreachable and the drain would be silent for the session.
func TestPassiveDrainSurvivesATransportedStamp(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	shield := ready(NewShieldSystem(w)).(*ShieldSystem)

	sh, _ := w.Components.Shield.GetPtr(local)
	sh.Active = true
	sh.LastDrainTime = w.Resources.Time.GameTime.Add(time.Hour)

	drains := func() int {
		n := 0
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if ev.Type == event.EventEnergyAddRequest {
				n++
			}
		}
		return n
	}

	shield.Update()
	if n := drains(); n != 0 {
		t.Fatalf("drains on the tick that adopted the stamp = %d, want 0", n)
	}
	w.Resources.Time.GameTime = w.Resources.Time.GameTime.Add(parameter.ShieldPassiveDrainInterval)
	shield.Update()
	if n := drains(); n != 1 {
		t.Fatalf("drains one interval later = %d, want 1", n)
	}
}

// TestLootDropsOnlyForLocallySimulatedCursors is the D-2 read of a personal reward
// on a shared kill. Every instance sees the species die and each rolls the drop
// table for its own cursors only: rolling for a cursor it does not simulate would
// double that player's reward, because the instance that does simulate it rolls the
// same slot.
func TestLootDropsOnlyForLocallySimulatedCursors(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 25, 5, 7)

	loot := ready(NewLootSystem(w)).(*LootSystem)
	for range 200 {
		loot.onSpeciesKilled(&event.SpeciesKilledPayload{
			Species: component.SpeciesDrain, X: 10, Y: 10, KillerEntity: remote,
		})
	}

	owners := map[core.Entity]int{}
	for _, e := range w.Components.Loot.GetAllEntities() {
		if c, ok := w.Components.Loot.GetComponent(e); ok {
			owners[c.Owner]++
		}
	}
	if owners[remote] != 0 {
		t.Errorf("%d drops landed on a cursor this instance does not simulate",
			owners[remote])
	}
	if owners[local] == 0 {
		t.Fatal("200 kills dropped nothing for the local cursor; the check is vacuous")
	}
}

// TestCameraAnchorsOnThePredictedCell is the view half of D-18: the camera follows
// the cell this participant's input selected, so an announcement arriving a playout
// lead later cannot walk it back to where the shared store still was.
func TestCameraAnchorsOnThePredictedCell(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)
	camera := ready(NewCameraSystem(w)).(*CameraSystem)

	config := w.Resources.Config
	config.MapWidth, config.MapHeight = 200, 100
	w.Positions.ResizeGrid(config.MapWidth, config.MapHeight)

	w.PushCursorMove(local, 150, 60)
	if config.CameraX == 0 || config.CameraY == 0 {
		t.Fatalf("camera = (%d, %d), want a scroll toward the predicted cell", config.CameraX, config.CameraY)
	}
	if pos, _ := w.Positions.GetPosition(local); pos.X != 5 || pos.Y != 5 {
		t.Fatalf("store cell = (%d, %d), want the crossing still pending", pos.X, pos.Y)
	}

	w.PushCursorMove(local, 0, 0)
	if config.CameraX != 0 || config.CameraY != 0 {
		t.Fatalf("camera = (%d, %d), want the origin the newest prediction selected", config.CameraX, config.CameraY)
	}

	// Both placements announce in order; the first is the older absolute cell
	for range 4 {
		events := w.Resources.Event.Queue.Consume()
		if len(events) == 0 {
			break
		}
		for _, ev := range events {
			cursors.HandleEvent(ev)
			camera.HandleEvent(ev)
		}
	}
	if config.CameraX != 0 || config.CameraY != 0 {
		t.Fatalf("camera = (%d, %d) after the announcements, want the origin", config.CameraX, config.CameraY)
	}
}

// TestAPointerHeldStillLeavesTheMapStill: a pointer names a screen cell, so a view
// that scrolls under it moves the cursor on every report until the map edge. Inside
// the screen it does not scroll; at the edge each report scrolls a bounded step.
func TestAPointerHeldStillLeavesTheMapStill(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	config := w.Resources.Config
	config.MapWidth, config.MapHeight = 400, 100
	w.Positions.ResizeGrid(config.MapWidth, config.MapHeight)
	w.PushCursorMove(local, 300, 50) // a key move scrolls the view to the right side

	report := func(vx, vy int) (int, int) {
		x, y, ok := config.ViewportToMap(vx, vy)
		if !ok {
			t.Fatalf("viewport cell (%d,%d) is off the map", vx, vy)
		}
		w.PushPointerMove(local, x, y)
		return config.CameraX, config.CameraY
	}
	quarter := config.ViewportWidth / 4
	camX, camY := report(quarter, config.ViewportHeight/2)
	for range 5 {
		if x, y := report(quarter, config.ViewportHeight/2); x != camX || y != camY {
			t.Fatalf("a still pointer moved the view from (%d,%d) to (%d,%d)", camX, camY, x, y)
		}
	}
	for range 5 {
		before := config.CameraX
		if x, _ := report(0, config.ViewportHeight/2); before-x > parameter.CameraPointerMarginX {
			t.Fatalf("one report at the edge scrolled %d cells", before-x)
		}
	}
	if config.CameraX >= camX {
		t.Fatal("a pointer at the edge did not scroll the view")
	}
}

// TestOwnPlacementsInFlightNeverWalkTheViewBack is the D-18 rule a fast sweep —
// a macro, a counted motion — depends on: it predicts more cells than the ring holds
// within one playout lead, and this instance's own placements land in the order they
// were produced, so none of them, nor a foreign placement between them, may move the
// view off the newest cell the player's input selected.
func TestOwnPlacementsInFlightNeverWalkTheViewBack(t *testing.T) {
	w, local, _ := testCursorWorld(t)
	cursors := ready(NewCursorSystem(w)).(*CursorSystem)

	cell := func(i int) (int, int) { return 1 + i%30, 1 + i/30 }
	sweep := parameter.MaxPredictedCursorCells + 20
	for i := range sweep {
		x, y := cell(i)
		w.PushCursorMove(local, x, y)
	}
	lastX, lastY := cell(sweep - 1)
	var inFlight []event.GameEvent
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type == event.EventCursorMoveRequest {
			// What the barrier stamps on this instance's own crossing at dispatch
			ev.CrossingSeq = uint64(len(inFlight) + 1)
			inFlight = append(inFlight, ev)
		}
	}
	if len(inFlight) != sweep {
		t.Fatalf("%d placements in flight, want the sweep's %d", len(inFlight), sweep)
	}
	viewAt := func(stage string, x, y int) {
		t.Helper()
		if pos, _ := w.CursorCell(local); pos.X != x || pos.Y != y {
			t.Fatalf("%s: view on (%d,%d), want (%d,%d)", stage, pos.X, pos.Y, x, y)
		}
	}

	half := len(inFlight) / 2
	for i, ev := range inFlight[:half] {
		cursors.HandleEvent(ev)
		viewAt(fmt.Sprintf("own placement %d of %d landed", i+1, len(inFlight)), lastX, lastY)
	}

	// A foreign placement snaps the view, and a newer prediction survives the own
	// placements that were already in flight when it came.
	bottom := w.Resources.Config.MapHeight - 1
	cursors.HandleEvent(event.GameEvent{Type: event.EventCursorMoveRequest,
		Payload: &event.CursorMoveRequestPayload{Entity: local, X: 35, Y: bottom}})
	viewAt("foreign placement", 35, bottom)
	w.PushCursorMove(local, 36, bottom)
	w.Resources.Event.Queue.Consume()
	for _, ev := range inFlight[half:] {
		cursors.HandleEvent(ev)
		viewAt("an older own placement landed after a newer prediction", 36, bottom)
	}
}
