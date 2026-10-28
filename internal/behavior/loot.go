package behavior

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/parameter"
)

// lootDoc is the loot a scenario's entry declares: kinds a drop may be, and the
// drop tables kills roll
type lootDoc struct {
	Loot  map[string]*kindDoc  `toml:"loot"`
	Drops map[string]*tableDoc `toml:"drops"`
}

// kindDoc is one kind: its look, and its reward, a weapon's charge or delta heat
// or energy
type kindDoc struct {
	Rune   string     `toml:"rune"`
	Color  *color.RGB `toml:"color"`
	Glow   *color.RGB `toml:"glow"`
	Reward string     `toml:"reward"`
	Weapon string     `toml:"weapon"`
	Delta  int        `toml:"delta"`
}

// tableDoc is one table: whose kill rolls it, and its tiers in roll order
type tableDoc struct {
	Killer string    `toml:"killer"`
	Tiers  []tierDoc `toml:"tiers"`
}

type tierDoc struct {
	Unique  bool       `toml:"unique"`
	Entries []entryDoc `toml:"entries"`
}

// entryDoc is one kind a tier rolls; count defaults to one drop
type entryDoc struct {
	Loot     string  `toml:"loot"`
	Rate     float64 `toml:"rate"`
	Count    *int    `toml:"count"`
	Fallback int     `toml:"fallback"`
}

// compileLoot builds the loot an entry's [loot] and [drops] declare, each fault
// naming its kind or table and place: the kinds LootCount onward in sorted-name
// order, and the tables by name, none taking the name of one of programs, which
// name their tables by drops. No other key of entry is read.
func compileLoot(entry map[string]any, programs []string) (component.LootSet, []error) {
	var set component.LootSet
	raw := map[string]any{}
	for _, key := range []string{"loot", "drops"} {
		if v, ok := entry[key]; ok {
			raw[key] = v
		}
	}
	if len(raw) == 0 {
		return set, nil
	}
	doc, err := dialect.Decode[lootDoc](raw)
	if err != nil {
		return set, []error{err}
	}
	var errs []error
	failAt := func(what, name string, err error) { errs = append(errs, fmt.Errorf("%s '%s': %w", what, name, err)) }

	names := slices.Sorted(maps.Keys(doc.Loot))
	if len(names) > parameter.LootKindsMax {
		errs = append(errs, fmt.Errorf("%d loot kinds, at most %d", len(names), parameter.LootKindsMax))
		names = names[:parameter.LootKindsMax]
	}
	kinds := make(map[string]component.LootType, len(component.LootSpecs)+len(names))
	for i, s := range component.LootSpecs {
		kinds[s.Name] = component.LootType(i)
	}
	for _, name := range names {
		if err := checkLootName("loot", name); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, code := kinds[name]; code {
			errs = append(errs, fmt.Errorf("loot name %q is a code kind", name))
			continue
		}
		kinds[name] = component.LootCount + component.LootType(len(set.Kinds))
		k, err := doc.Loot[name].kind(name)
		if err != nil {
			failAt("loot", name, err)
		}
		set.Kinds = append(set.Kinds, k)
	}

	set.Tables = make(map[string]component.DropTable, len(doc.Drops))
	for _, name := range slices.Sorted(maps.Keys(doc.Drops)) {
		switch err := checkLootName("drop table", name); {
		case err != nil:
			errs = append(errs, err)
		case name == component.SpeciesNames[component.SpeciesNone]:
			errs = append(errs, fmt.Errorf("drop table name %q names no species", name))
		case slices.Contains(programs, name):
			errs = append(errs, fmt.Errorf("drop table name %q is a program's, which names its table by drops", name))
		}
		t, tableErrs := doc.Drops[name].table(kinds)
		for _, e := range tableErrs {
			failAt("drops", name, e)
		}
		set.Tables[name] = t
	}
	return set, errs
}

// checkLootName holds a kind's or table's name, what names it, to what a payload
// and a program's drops spell it by
func checkLootName(what, name string) error {
	ok := len(name) > 0 && len(name) <= parameter.LootNameMax && name[0] >= 'a' && name[0] <= 'z'
	for i := 1; ok && i < len(name); i++ {
		c := name[i]
		ok = c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
	}
	if !ok {
		return fmt.Errorf("%s name %.32q is not [a-z][a-z0-9_]* of at most %d bytes", what, name, parameter.LootNameMax)
	}
	return nil
}

// kind validates d and makes it the kind's plain data
func (d *kindDoc) kind(name string) (component.LootKind, error) {
	k := component.LootKind{LootSpec: component.LootSpec{Name: name, Delta: d.Delta}}
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	r, size := utf8.DecodeRuneInString(d.Rune)
	if size != len(d.Rune) || r == utf8.RuneError || !unicode.IsGraphic(r) || unicode.IsSpace(r) {
		fail("rune %q is not one glyph", d.Rune)
	}
	k.Look.Rune = r
	if d.Color == nil || d.Glow == nil {
		fail("color and glow are required, the rune's and its shield's")
	} else {
		k.Look.Color, k.Look.Glow = *d.Color, *d.Glow
	}

	i := slices.Index(component.RewardNames[:], d.Reward)
	if i <= int(component.RewardNone) {
		fail("reward %q is not one of %s", d.Reward, strings.Join(component.RewardNames[1:], ", "))
		return k, errors.Join(errs...)
	}
	k.Reward = component.RewardType(i)
	if k.Reward == component.RewardWeapon {
		w := slices.IndexFunc(component.WeaponSpecs[:], func(s component.WeaponSpec) bool { return s.Name == d.Weapon })
		switch {
		case w < 0:
			names := make([]string, len(component.WeaponSpecs))
			for i, s := range component.WeaponSpecs {
				names[i] = s.Name
			}
			fail("weapon %q is not one of %s", d.Weapon, strings.Join(names, ", "))
		case d.Delta != 0:
			fail("delta %d: a weapon reward is one charge", d.Delta)
		default:
			k.Weapon = component.WeaponType(w)
		}
	} else {
		switch {
		case d.Weapon != "":
			fail("weapon %q: %s charges no weapon", d.Weapon, d.Reward)
		case d.Delta <= 0:
			fail("delta %d: %s adds a positive amount", d.Delta, d.Reward)
		}
	}
	return k, errors.Join(errs...)
}

// table validates d against the kinds by name and makes it the table's plain data
func (d *tableDoc) table(kinds map[string]component.LootType) (component.DropTable, []error) {
	var t component.DropTable
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if d.Killer != "" {
		if i := slices.Index(component.DropKillerNames[:], d.Killer); i >= 0 {
			t.Killer = component.DropKiller(i)
		} else {
			fail("killer %q is not one of %s", d.Killer, strings.Join(component.DropKillerNames[:], ", "))
		}
	}
	if len(d.Tiers) > parameter.LootTiersMax {
		fail("%d tiers, at most %d", len(d.Tiers), parameter.LootTiersMax)
	}
	for i, tier := range d.Tiers {
		where := fmt.Sprintf("tiers[%d]", i)
		if len(tier.Entries) == 0 {
			fail("%s: no entry", where)
		}
		out := component.DropTier{Unique: tier.Unique}
		for j, e := range tier.Entries {
			at := fmt.Sprintf("%s: entries[%d]", where, j)
			loot, ok := kinds[e.Loot]
			if !ok {
				fail("%s: loot %q names no kind", at, e.Loot)
			} else if slices.ContainsFunc(out.Entries, func(o component.DropEntry) bool { return o.Loot == loot }) {
				fail("%s: loot %q is in the tier already", at, e.Loot)
			}
			if !(e.Rate > 0 && e.Rate <= 1) {
				fail("%s: rate %v is outside (0, 1]", at, e.Rate)
			}
			count := 1
			if e.Count != nil {
				count = *e.Count
			}
			if count < 1 || count > parameter.LootDropCountMax {
				fail("%s: count %d is outside 1..%d", at, count, parameter.LootDropCountMax)
			}
			switch {
			case e.Fallback < 0 || e.Fallback > parameter.LootDropCountMax:
				fail("%s: fallback %d is outside 0..%d", at, e.Fallback, parameter.LootDropCountMax)
			case e.Fallback > 0 && !tier.Unique:
				fail("%s: fallback %d adds only when a unique tier skips it", at, e.Fallback)
			}
			out.Entries = append(out.Entries, component.DropEntry{Loot: loot, BaseRate: e.Rate, Count: count, FallbackCount: e.Fallback})
		}
		t.Tiers = append(t.Tiers, out)
	}
	return t, errs
}
