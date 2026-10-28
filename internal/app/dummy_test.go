package app

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/resource"
)

// TestTheTargetDummyTakesItsHitPointsFromThePads plays the shipped dummy scenario on
// its smallest map: the range lies still and comes back after a pickup, each pad
// press moves the standing dummy's hit points at once and again after its
// countdown, a dummy shot down rises on its spot once the cursor has stood there
// and walked back out of its shield's reach, and energy at 0 brings an energy drop
// homing from the dummy's centre.
func TestTheTargetDummyTakesItsHitPointsFromThePads(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "dummy"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("dummy scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 80, Height: 30, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	w := a.World()
	state := func(region string) string { return w.Resources.Status.Strings.Get("fsm." + region + ".state").Load() }
	await := func(region, want string) {
		t.Helper()
		for range 200 {
			if state(region) == want {
				return
			}
			a.Tick(1)
		}
		t.Fatalf("%s stayed in %s, never reached %s", region, state(region), want)
	}
	// Each standing pylon's member hit points
	dummies := func() (hp [][]int) {
		w.RunSafe(func() {
			for _, header := range w.Components.Pylon.Entities() {
				var members []int
				for _, m := range pylonMembers(w, header) {
					c, _ := w.Components.Combat.GetComponent(m)
					members = append(members, c.HitPoints)
				}
				hp = append(hp, members)
			}
		})
		return hp
	}
	// One dummy stands with every member at want hit points within a countdown's span
	awaitDummy := func(want int) {
		t.Helper()
		var hp [][]int
		for range 100 {
			if hp = dummies(); len(hp) == 1 && len(hp[0]) > 0 && !slices.ContainsFunc(hp[0], func(v int) bool { return v != want }) {
				return
			}
			a.Tick(1)
		}
		t.Fatalf("the dummies stand at %v, want one at %d hit points", hp, want)
	}
	var cursor core.Entity
	move := func(x, y int) {
		t.Helper()
		a.Context().PushEventOrigin(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: cursor, X: x, Y: y}, event.OriginDebug)
		a.Settle()
		a.Tick(1)
	}
	drops := func() (still, homing []component.LootComponent) {
		w.RunSafe(func() {
			w.Components.Loot.Each(func(_ core.Entity, l *component.LootComponent) bool {
				if l.Still {
					still = append(still, *l)
				} else {
					homing = append(homing, *l)
				}
				return true
			})
		})
		return still, homing
	}

	// The map's centre is (40, 15): the range left, the dummy at (58, 15), the pads
	// at (75, 8) and (75, 22)
	await("dummy", "DummyUp")
	awaitDummy(1)
	cursor = w.Resources.Player.Slot(0)
	if still, homing := drops(); len(still) != 8 || len(homing) != 0 {
		t.Fatalf("the range laid %d still drops and %d homing, want 8 and 0", len(still), len(homing))
	}
	if n := w.Components.Wall.CountEntities(); n != 26 {
		t.Fatalf("the pads stand on %d wall cells, want two frames of 12 and their signs", n)
	}

	// A rod charge from the range, its drop back three seconds on
	move(14, 12)
	if wc, _ := w.Components.Weapon.GetComponent(cursor); wc.Charges[component.WeaponRod] != 1 {
		t.Fatalf("the rod drop gave %d charges", wc.Charges[component.WeaponRod])
	}
	move(40, 15)
	a.Tick(60)
	if still, _ := drops(); len(still) != 8 || slices.ContainsFunc(still, func(l component.LootComponent) bool { return l.Away > 0 }) {
		t.Fatal("the rod drop did not lie back on the range")
	}

	// + presses as the cursor arrives and again after its countdown, - once
	move(75, 8)
	awaitDummy(2)
	awaitDummy(3)
	move(75, 22)
	awaitDummy(2)

	// Shot down, the dummy waits for the cursor on its spot and rises as it leaves
	w.RunSafe(func() {
		for _, header := range w.Components.Pylon.Entities() {
			for _, m := range pylonMembers(w, header) {
				if c, ok := w.Components.Combat.GetPtr(m); ok {
					c.HitPoints = 0
				}
			}
		}
	})
	await("dummy", "DummyDown")
	move(58, 15)
	await("dummy", "DummyArmed")
	move(50, 15)
	if state("dummy") != "DummyArmed" || len(dummies()) != 0 {
		t.Fatal("the dummy rose with the cursor's shield still over its spot")
	}
	move(40, 15)
	await("dummy", "DummyUp")
	awaitDummy(2)

	// Energy at 0 brings one drop from the dummy's centre, homing on the cursor
	a.Context().PushEventOrigin(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: cursor}, event.OriginDebug)
	a.Settle()
	await("player", "PlayerRecharge")
	if _, homing := drops(); len(homing) != 1 || homing[0].Type != component.LootEnergy {
		t.Fatalf("energy at 0 brought %d homing drops", len(homing))
	}
	await("player", "PlayerActive")
	if e, _ := w.Components.Energy.GetComponent(cursor); e.Current <= 0 {
		t.Fatalf("the energy drop left the cursor at %d energy", e.Current)
	}
}

// pylonMembers lists a pylon's member entities
func pylonMembers(w *engine.World, header core.Entity) (members []core.Entity) {
	h, _ := w.Components.Header.GetComponent(header)
	for _, m := range h.MemberEntries {
		if m.Entity != 0 {
			members = append(members, m.Entity)
		}
	}
	return members
}
