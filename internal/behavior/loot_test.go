package behavior

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
)

// raidLoot declares two kinds, a table a player's kill rolls naming code and
// declared kinds, and an override that leaves the drain dropping nothing
const raidLoot = `
[loot.upgrade]
rune = "U"
color = { r = 255, g = 255, b = 0 }
glow = { r = 100, g = 220, b = 100 }
reward = "weapon"
weapon = "gun"

[loot.beacon]
rune = "*"
color = { r = 255, g = 255, b = 255 }
glow = { r = 60, g = 60, b = 200 }
reward = "energy"
delta = 500

[drops.raider]
killer = "player"
tiers = [
    { unique = true, entries = [{ loot = "rod", rate = 0.5, fallback = 1 }, { loot = "beacon", rate = 0.25 }] },
    { entries = [{ loot = "upgrade", rate = 1.0, count = 2 }] },
]

[drops.drain]
tiers = []
`

func TestDeclaredKindsFollowTheCodesInSortedNameOrder(t *testing.T) {
	set, err := Compile(testVocabulary(), nil, parse(t, raidLoot))
	if err != nil {
		t.Fatal(err)
	}
	beacon, upgrade := component.LootCount, component.LootCount+1
	want := component.LootSet{
		Kinds: []component.LootKind{
			{LootSpec: component.LootSpec{Name: "beacon", Reward: component.RewardEnergy, Delta: 500},
				Look: component.LootLook{Rune: '*', Color: color.RGB{R: 255, G: 255, B: 255}, Glow: color.RGB{R: 60, G: 60, B: 200}}},
			{LootSpec: component.LootSpec{Name: "upgrade", Reward: component.RewardWeapon, Weapon: component.WeaponGun},
				Look: component.LootLook{Rune: 'U', Color: color.RGB{R: 255, G: 255}, Glow: color.RGB{R: 100, G: 220, B: 100}}},
		},
		Tables: map[string]component.DropTable{
			"raider": {Killer: component.DropKillerPlayer, Tiers: []component.DropTier{
				{Unique: true, Entries: []component.DropEntry{{Loot: component.LootRod, BaseRate: 0.5, Count: 1, FallbackCount: 1}, {Loot: beacon, BaseRate: 0.25, Count: 1}}},
				{Entries: []component.DropEntry{{Loot: upgrade, BaseRate: 1.0, Count: 2}}},
			}},
			"drain": {},
		},
	}
	if got := *set.Loot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("loot %+v\nwant %+v", got, want)
	}
	if got := set.Declared().Loot; !reflect.DeepEqual(got, []string{"beacon", "upgrade"}) {
		t.Fatalf("declared kinds %v, want beacon then upgrade", got)
	}
}

func TestALootCompileErrorNamesItsKindOrTableAndPlace(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"two runes", `rune = "U"`, `rune = "UU"`, `loot 'upgrade': rune "UU" is not one glyph`},
		{"blank rune", `rune = "U"`, `rune = " "`, `loot 'upgrade': rune " " is not one glyph`},
		{"no glow", `glow = { r = 100, g = 220, b = 100 }`, ``, `loot 'upgrade': color and glow are required`},
		{"reward", `reward = "weapon"`, `reward = "charge"`, `loot 'upgrade': reward "charge" is not one of weapon, heat, energy`},
		{"weapon", `weapon = "gun"`, `weapon = "laser"`, `loot 'upgrade': weapon "laser" is not one of rod, launcher`},
		{"weapon delta", `weapon = "gun"`, "weapon = \"gun\"\ndelta = 3", `loot 'upgrade': delta 3: a weapon reward is one charge`},
		{"delta", `delta = 500`, `delta = 0`, `loot 'beacon': delta 0: energy adds a positive amount`},
		{"energy weapon", `delta = 500`, "delta = 500\nweapon = \"gun\"", `loot 'beacon': weapon "gun": energy charges no weapon`},
		{"killer", `killer = "player"`, `killer = "anyone"`, `drops 'raider': killer "anyone" is not one of any, player`},
		{"no entry", `{ entries = [{ loot = "upgrade", rate = 1.0, count = 2 }] }`, `{ entries = [] }`, `drops 'raider': tiers[1]: no entry`},
		{"kind", `loot = "upgrade"`, `loot = "upgrde"`, `drops 'raider': tiers[1]: entries[0]: loot "upgrde" names no kind`},
		{"twice", `{ loot = "beacon", rate = 0.25 }`, `{ loot = "rod", rate = 0.25 }`, `drops 'raider': tiers[0]: entries[1]: loot "rod" is in the tier already`},
		{"rate", `rate = 0.25`, `rate = 0.0`, `drops 'raider': tiers[0]: entries[1]: rate 0 is outside (0, 1]`},
		{"count", `count = 2`, `count = 0`, fmt.Sprintf(`drops 'raider': tiers[1]: entries[0]: count 0 is outside 1..%d`, parameter.LootDropCountMax)},
		{"fallback", `fallback = 1`, `fallback = 99`, fmt.Sprintf(`drops 'raider': tiers[0]: entries[0]: fallback 99 is outside 0..%d`, parameter.LootDropCountMax)},
		{"fallback outside unique", `count = 2`, `count = 2, fallback = 1`, `drops 'raider': tiers[1]: entries[0]: fallback 1 adds only when a unique tier skips it`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(raidLoot, tc.from) {
				t.Fatalf("%q is not in the loot", tc.from)
			}
			_, err := Compile(testVocabulary(), nil, parse(t, strings.Replace(raidLoot, tc.from, tc.to, 1)))
			if err == nil {
				t.Fatal("compiled")
			}
			if lines := strings.Split(err.Error(), "\n"); !strings.Contains(err.Error(), tc.want) || len(lines) != 1 {
				t.Fatalf("error %q\nwant one line holding %q", err, tc.want)
			}
		})
	}
}

func TestALootNameKeepsToTheRules(t *testing.T) {
	var many strings.Builder
	for i := range parameter.LootKindsMax + 1 {
		fmt.Fprintf(&many, "[loot.k%d]\nrune = \"k\"\ncolor = {}\nglow = {}\nreward = \"heat\"\ndelta = 1\n", i)
	}
	for _, tc := range []struct {
		programs map[string]map[string]any
		loot     string
		want     string
	}{
		{nil, many.String(), fmt.Sprintf("%d loot kinds, at most %d", parameter.LootKindsMax+1, parameter.LootKindsMax)},
		{nil, strings.Replace(raidLoot, "[loot.beacon]", "[loot.Beacon]", 1), `loot name "Beacon" is not [a-z][a-z0-9_]*`},
		{nil, strings.Replace(raidLoot, "[loot.beacon]", "[loot.energy]", 1), `loot name "energy" is a code kind`},
		{nil, strings.Replace(raidLoot, "[drops.drain]", "[drops.none]", 1), `drop table name "none" names no species`},
		{map[string]map[string]any{"raider": parse(t, warden)}, raidLoot, `drop table name "raider" is a program's, which names its table by drops`},
	} {
		if _, err := Compile(testVocabulary(), tc.programs, parse(t, tc.loot)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error %v, want %q", err, tc.want)
		}
	}
}

func TestAProgramsDropsNameADeclaredTableOrACodeSpecies(t *testing.T) {
	compile := func(drops string) (*Set[*testCtx], error) {
		return Compile(testVocabulary(), map[string]map[string]any{
			"warden": parse(t, strings.Replace(warden, `drops = "swarm"`, `drops = "`+drops+`"`, 1)),
		}, parse(t, raidLoot))
	}
	for _, drops := range []string{"raider", "swarm"} {
		set, err := compile(drops)
		if err != nil {
			t.Fatalf("drops %q: %v", drops, err)
		}
		if got := set.Prototypes()[0].Drops; got != drops {
			t.Fatalf("drops %q compiled to %q", drops, got)
		}
	}
	for drops, want := range map[string]string{
		"raidr": `program 'warden': prototype: drops "raidr" names no drop table`,
		"drain": `program 'warden': prototype: drops "drain": that table drops nothing`,
	} {
		if _, err := compile(drops); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("drops %q: error %v, want %q", drops, err, want)
		}
	}
}
