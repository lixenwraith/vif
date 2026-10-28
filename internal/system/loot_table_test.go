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
