package system

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

// upgradeKind is a kind a scenario declares, LootCount the first of its kinds
var upgradeKind = component.LootKind{
	LootSpec: component.LootSpec{Name: "upgrade", Reward: component.RewardWeapon, Weapon: component.WeaponGun},
	Look:     component.LootLook{Rune: 'U', Color: color.RGB{R: 255, G: 255}, Glow: color.RGB{R: 100, G: 220, B: 100}},
}

// raiderLoot declares the upgrade and a table only a player's kill rolls, which
// drops one, named by the scenario's one program
func raiderLoot(w *engine.World) {
	w.Resources.Loot = component.LootSet{
		Kinds: []component.LootKind{upgradeKind},
		Tables: map[string]component.DropTable{"raider": {Killer: component.DropKillerPlayer, Tiers: []component.DropTier{
			{Entries: []component.DropEntry{{Loot: component.LootCount, BaseRate: 1, Count: 1}}},
		}}},
	}
	w.Resources.Behaviors = []component.BehaviorPrototype{{Name: "scarab", Species: component.SpeciesDataFirst, Drops: "raider"}}
}

func TestAScenarioTableNamedForACodeSpeciesReplacesItsCodeTable(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	w.Resources.Loot.Tables = map[string]component.DropTable{"drain": {Tiers: []component.DropTier{
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1, Count: 2}}},
	}}}
	loot := ready(NewLootSystem(w)).(*LootSystem)
	for range 20 {
		got := loot.rollDropTable(component.SpeciesDrain, cursor, 0)
		if len(got) != 1 || got[0] != (DropResult{Loot: component.LootEnergy, Count: 2}) {
			t.Fatalf("a drain dropped %v, want the scenario's two energy every kill", got)
		}
	}
	if got := loot.rollDropTable(component.SpeciesStorm, cursor, 0); len(got) == 0 {
		t.Fatal("a storm the scenario names no table for dropped nothing, want its code table")
	}
}

func TestAPlayerKillTableDropsOnlyForAKillACursorDealt(t *testing.T) {
	w, first, second := testCursorWorld(t)
	raiderLoot(w)
	loot := ready(NewLootSystem(w)).(*LootSystem)

	loot.onSpeciesKilled(&event.SpeciesKilledPayload{Species: component.SpeciesDataFirst, X: 10, Y: 10})
	if n := w.Components.Loot.CountEntities(); n != 0 {
		t.Fatalf("a raider nobody shot dropped %d, want none", n)
	}
	loot.onSpeciesKilled(&event.SpeciesKilledPayload{Species: component.SpeciesDataFirst, X: 10, Y: 10, KillerEntity: first})
	owners := map[core.Entity]int{}
	for _, e := range w.Components.Loot.GetAllEntities() {
		if c, ok := w.Components.Loot.GetComponent(e); ok && c.Type == component.LootCount {
			owners[c.Owner]++
		}
	}
	if owners[first] != 1 || owners[second] != 1 || len(owners) != 2 {
		t.Fatalf("a raider a cursor shot dropped upgrades to %v, want one to each local cursor", owners)
	}
}

func TestADropCarriesItsKindsLookAndGrantsItsReward(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	raiderLoot(w)
	loot := ready(NewLootSystem(w)).(*LootSystem)
	loot.spawnLootWithBurst(component.LootCount, 10, 10, 0, 0, cursor, component.LootComponent{})
	drops := w.Components.Loot.GetAllEntities()
	if len(drops) != 1 {
		t.Fatalf("%d drops, want one", len(drops))
	}
	if c, _ := w.Components.Loot.GetComponent(drops[0]); c.Look != upgradeKind.Look {
		t.Fatalf("the drop looks %+v, want its kind's %+v", c.Look, upgradeKind.Look)
	}

	w.Resources.Event.Queue.Consume()
	drop, _ := w.Components.Loot.GetPtr(drops[0])
	loot.collectLoot(cursor, drops[0], drop)
	var granted, flashed bool
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch p := ev.Payload.(type) {
		case *event.WeaponAddRequestPayload:
			granted = p.Entity == cursor && p.Weapon == component.WeaponGun
		case *event.FlashRequestPayload:
			flashed = p.Char == upgradeKind.Look.Rune
		}
	}
	if !granted || !flashed || w.Components.Loot.HasEntity(drops[0]) {
		t.Fatalf("collecting the upgrade granted a gun charge %t and flashed its rune %t, want both and the drop gone", granted, flashed)
	}
}

// Two species naming one table roll on its one streak, which a copy carries: a
// U at even odds, certain after a miss, never misses twice running between them
func TestEverySpeciesNamingATableRollsOnItsStreaks(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	raiderLoot(w)
	w.Resources.Loot.Tables["raider"] = component.DropTable{Tiers: []component.DropTier{
		{Entries: []component.DropEntry{{Loot: component.LootCount, BaseRate: 0.5, Count: 1}}},
	}}
	w.Resources.Behaviors = append(w.Resources.Behaviors, component.BehaviorPrototype{Name: "crab", Species: component.SpeciesDataFirst + 1, Drops: "raider"})
	loot := ready(NewLootSystem(w)).(*LootSystem)

	missed, misses := false, 0
	for i := range 200 {
		dropped := len(loot.rollDropTable(component.SpeciesDataFirst+component.SpeciesType(i%2), cursor, 0)) > 0
		if !dropped && missed {
			t.Fatalf("roll %d: a second miss in a row across the table's two species", i)
		}
		if missed = !dropped; missed {
			misses++
		}
	}
	if misses == 0 || len(loot.pity) != 1 {
		t.Fatalf("%d misses over %d streaks, want some on the table's one", misses, len(loot.pity))
	}

	for !missed {
		missed = len(loot.rollDropTable(component.SpeciesDataFirst, cursor, 0)) == 0
	}
	data, err := loot.SaveLocal()
	if err != nil {
		t.Fatal(err)
	}
	copied := ready(NewLootSystem(w)).(*LootSystem)
	if err := copied.LoadLocal(data); err != nil {
		t.Fatal(err)
	}
	if got := copied.rollDropTable(component.SpeciesDataFirst+1, cursor, 0); len(got) != 1 {
		t.Fatalf("a copy rolled %v after the streak's miss, want the U it makes certain", got)
	}
}

// A gun on a scenario's path is full at the path's last level, not the code gun's
// third charge: only then does a unique tier pass its U over, for its fallback
func TestAUniqueGunOnAPathIsFullAtItsLastLevel(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	raiderLoot(w)
	w.Resources.Loot.Tables["raider"] = component.DropTable{Tiers: []component.DropTier{
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootCount, BaseRate: 1, Count: 1, FallbackCount: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1, Count: 1}}},
	}}
	loot := ready(NewLootSystem(w)).(*LootSystem)
	roll := func(charges int) map[component.LootType]int {
		weapons, _ := w.Components.Weapon.GetPtr(cursor)
		weapons.GunLevels, weapons.Charges[component.WeaponGun] = 9, charges
		got := map[component.LootType]int{}
		for _, r := range loot.rollDropTable(component.SpeciesDataFirst, cursor, 0) {
			got[r.Loot] += r.Count
		}
		return got
	}
	if got := roll(component.WeaponSpecs[component.WeaponGun].MaxCharges); got[component.LootCount] != 1 || got[component.LootEnergy] != 1 {
		t.Fatalf("a gun three levels up its path of nine dropped %v, want a U and one energy", got)
	}
	if got := roll(9); len(got) != 1 || got[component.LootEnergy] != 2 {
		t.Fatalf("a gun at its path's last level dropped %v, want no U and two energy", got)
	}
}

// A unique tier that rolls and misses adds no fallback; only loot it skips does
func TestAUniqueTiersMissAddsNoFallback(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	w.Resources.Loot.Tables = map[string]component.DropTable{"drain": {Tiers: []component.DropTier{
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootRod, BaseRate: 0.01, Count: 1, FallbackCount: 2}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1, Count: 1}}},
	}}}
	loot := ready(NewLootSystem(w)).(*LootSystem)
	misses := 0
	for range 20 {
		got := map[component.LootType]int{}
		for _, r := range loot.rollDropTable(component.SpeciesDrain, cursor, 0) {
			got[r.Loot] += r.Count
		}
		if got[component.LootRod] == 0 {
			if misses++; got[component.LootEnergy] != 1 {
				t.Fatalf("a missed rod roll dropped %v, want the one energy alone", got)
			}
		}
	}
	if misses == 0 {
		t.Fatal("no rod roll missed at 1%")
	}
}
