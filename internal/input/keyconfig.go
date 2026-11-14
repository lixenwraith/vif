package input

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/parameter"
)

// Rune aliases for keys that can't be bare single-char TOML keys
var runeAliases = map[string]rune{
	"space":     ' ',
	"backslash": '\\',
	"zero":      '0', // TOML parser rejects quoted numeric keys
}

// TOML section names → KeyTable field mapping
var sectionDefs = []struct {
	name      string
	isSpecial bool // true = terminal.Key map, false = rune map
}{
	{"normal", false},
	{"normal_keys", true},
	{"operator", false},
	{"prefix_g", false},
	{"overlay", false},
	{"overlay_keys", true},
	{"text_keys", true},
}

// LoadKeyConfig parses TOML keymap data into a sparse override KeyTable
// Only sections/keys present in TOML are populated
// Returns error on unknown action names, invalid key names, or parse failure
func LoadKeyConfig(data []byte) (*KeyTable, error) {
	p := toml.NewParser(data)
	raw, err := p.Parse()
	if err != nil {
		return nil, fmt.Errorf("keymap parse: %w", err)
	}

	kt := &KeyTable{}

	for _, def := range sectionDefs {
		sectionData, ok := raw[def.name]
		if !ok {
			continue
		}

		sectionMap, ok := sectionData.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("section [%s]: expected table, got %T", def.name, sectionData)
		}

		if def.isSpecial {
			keyMap, err := parseSpecialKeySection(def.name, sectionMap)
			if err != nil {
				return nil, err
			}
			switch def.name {
			case "normal_keys":
				kt.SpecialKeys = keyMap
			case "overlay_keys":
				kt.OverlayKeys = keyMap
			case "text_keys":
				kt.TextNavKeys = keyMap
			}
		} else {
			runeMap, err := parseRuneSection(def.name, sectionMap)
			if err != nil {
				return nil, err
			}
			switch def.name {
			case "normal":
				kt.NormalRunes = runeMap
			case "operator":
				kt.OperatorMotions = runeMap
			case "prefix_g":
				kt.PrefixG = runeMap
			case "overlay":
				kt.OverlayRunes = runeMap
			}
		}
	}

	return kt, nil
}

// UpgradeKeymap brings a keymap document an earlier build left up to this build's,
// as paths.UpgradeSettings does vif.toml: a section this build does not read and a
// key it cannot name go with the comments that are theirs, and a binding it refuses
// too, so the embedded one stands; then each binding the embedded keymap has for a
// key the document binds in no spelling comes with its comments. It names each change.
func UpgradeKeymap(data []byte) ([]byte, []string, error) {
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return nil, nil, fmt.Errorf("keymap parse: %w", err)
	}
	shipped, err := toml.ParseDocument(asset.DefaultKeymap)
	if err != nil {
		return nil, nil, err
	}
	var unknown, reset []toml.Key
	var refused []string
	bound := make(map[string]map[any]string) // per section, the spelling each key is bound under
	tree := doc.Tree()
	for _, name := range slices.Sorted(maps.Keys(tree)) {
		special, known := sectionKind(name)
		table, ok := tree[name].(map[string]any)
		switch {
		case !known:
			unknown = append(unknown, toml.Key{name})
			continue
		case !ok:
			reset = append(reset, toml.Key{name})
			refused = append(refused, fmt.Sprintf("section [%s]: expected table, got %T", name, tree[name]))
			continue
		}
		bound[name] = make(map[any]string, len(table))
		for _, k := range slices.Sorted(maps.Keys(table)) {
			key, err := keyOf(special, k)
			if err != nil {
				unknown = append(unknown, toml.Key{name, k})
				continue
			}
			if _, err := bindingOf(table[k]); err != nil {
				reset = append(reset, toml.Key{name, k})
				refused = append(refused, fmt.Sprintf("[%s] key %q: %v", name, k, err))
				continue
			}
			bound[name][key] = k
		}
	}
	var spelt []toml.Key // the shipped bindings of keys the document binds in another spelling
	for name, v := range shipped.Tree() {
		special, _ := sectionKind(name)
		table, _ := v.(map[string]any)
		for k := range table {
			if key, err := keyOf(special, k); err == nil && bound[name][key] != "" && bound[name][key] != k {
				spelt = append(spelt, toml.Key{name, k})
			}
		}
	}
	for _, k := range slices.Concat(unknown, reset) {
		if _, err := doc.Delete(k); err != nil {
			return nil, nil, err
		}
	}
	for _, k := range spelt {
		if _, err := shipped.Delete(k); err != nil {
			return nil, nil, err
		}
	}
	added, err := doc.Fill(shipped)
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	if len(unknown) > 0 {
		notes = append(notes, "dropped "+bindings(unknown))
	}
	for _, why := range refused {
		notes = append(notes, "reset to the default: "+why)
	}
	added = slices.DeleteFunc(added, func(k toml.Key) bool {
		return slices.ContainsFunc(reset, func(r toml.Key) bool { return len(k) >= len(r) && slices.Equal(k[:len(r)], r) })
	})
	switch {
	case len(added) > parameter.UpgradeNamed:
		notes = append(notes, fmt.Sprintf("added %d bindings", len(added)))
	case len(added) > 0:
		notes = append(notes, "added "+bindings(added))
	}
	if _, err := LoadKeyConfig(doc.Bytes()); err != nil {
		return nil, nil, err
	}
	return doc.Bytes(), notes, nil
}

// bindings names keymap keys as a player reads them: [section] "key"
func bindings(keys []toml.Key) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = "[" + k[0] + "]"
		if len(k) > 1 {
			names[i] += fmt.Sprintf(" %q", k[1])
		}
	}
	return strings.Join(names, ", ")
}

// sectionKind reports whether this build reads the section name, and whether it
// binds special keys there rather than runes
func sectionKind(name string) (special, known bool) {
	for _, d := range sectionDefs {
		if d.name == name {
			return d.isSpecial, true
		}
	}
	return false, false
}

// keyOf is the key a section binds under the name k: a terminal.Key where it binds
// special keys, else a rune
func keyOf(special bool, k string) (any, error) {
	if !special {
		r, err := resolveRune(k)
		return r, err
	}
	key, ok := terminal.KeyByName(strings.ToLower(k))
	if !ok {
		return nil, fmt.Errorf("unknown key name: %q", k)
	}
	return key, nil
}

// bindingOf is the action a key's value names
func bindingOf(v any) (KeyEntry, error) {
	name, ok := v.(string)
	if !ok {
		return KeyEntry{}, fmt.Errorf("value must be string, got %T", v)
	}
	return resolveAction(name)
}

// ScenarioKeyTable reads a scenario's [keymap], which its run layers over the
// player's keymap: the normal, operator and prefix_g sections alone, each key naming
// an action that is no system one, or none. The special keys, which quit, leave a mode
// and open the menu and help, stay the player's.
func ScenarioKeyTable(raw map[string]any) (*KeyTable, error) {
	kt := &KeyTable{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		var into *map[rune]KeyEntry
		switch name {
		case "normal":
			into = &kt.NormalRunes
		case "operator":
			into = &kt.OperatorMotions
		case "prefix_g":
			into = &kt.PrefixG
		default:
			return nil, fmt.Errorf("[%s] is not a section a scenario binds: normal, operator or prefix_g", name)
		}
		table, ok := raw[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("[%s] must be a table", name)
		}
		runes, err := parseRuneSection(name, table)
		if err != nil {
			return nil, err
		}
		for _, r := range slices.Sorted(maps.Keys(runes)) {
			if runes[r].Behavior == BehaviorSystem {
				return nil, fmt.Errorf("[%s] key %q: a scenario binds no system action", name, string(r))
			}
		}
		*into = runes
	}
	return kt, nil
}

// parseRuneSection parses a TOML section of rune key → action name bindings
func parseRuneSection(section string, data map[string]any) (map[rune]KeyEntry, error) {
	result := make(map[rune]KeyEntry, len(data))

	for _, keyStr := range slices.Sorted(maps.Keys(data)) { // the spelling of a key that sorts last binds it
		val := data[keyStr]
		r, err := resolveRune(keyStr)
		if err != nil {
			return nil, fmt.Errorf("[%s] key %q: %w", section, keyStr, err)
		}

		entry, err := bindingOf(val)
		if err != nil {
			return nil, fmt.Errorf("[%s] key %q: %w", section, keyStr, err)
		}

		result[r] = entry
	}

	return result, nil
}

// parseSpecialKeySection parses a TOML section of terminal.Key name → action name bindings
func parseSpecialKeySection(section string, data map[string]any) (map[terminal.Key]KeyEntry, error) {
	result := make(map[terminal.Key]KeyEntry, len(data))

	for _, keyStr := range slices.Sorted(maps.Keys(data)) { // the spelling of a key that sorts last binds it
		val := data[keyStr]
		k, ok := terminal.KeyByName(strings.ToLower(keyStr))
		if !ok {
			return nil, fmt.Errorf("[%s] unknown key name: %q", section, keyStr)
		}

		entry, err := bindingOf(val)
		if err != nil {
			return nil, fmt.Errorf("[%s] key %q: %w", section, keyStr, err)
		}

		result[k] = entry
	}

	return result, nil
}

// resolveRune converts a TOML key string to a rune
// Accepts single characters and named aliases
func resolveRune(s string) (rune, error) {
	// Named alias
	if r, ok := runeAliases[strings.ToLower(s)]; ok {
		return r, nil
	}

	// Single character
	runes := []rune(s)
	if len(runes) == 1 {
		return runes[0], nil
	}

	return 0, fmt.Errorf("invalid rune key: %q (expected single character or alias)", s)
}

// resolveAction converts an action name string to a KeyEntry
func resolveAction(name string) (KeyEntry, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	// Installed keymaps retain retired names; normalize them before strict validation.
	if name == "append" {
		name = "toggle_auto_fire"
	}
	entry, ok := ActionEntry(name)
	if !ok {
		return KeyEntry{}, fmt.Errorf("unknown action: %q", name)
	}
	return entry, nil
}

// MergeKeyTable returns a new KeyTable with base values overridden by non-nil override maps
// Override entries with BehaviorNone ("none" action) delete the key from the result
func MergeKeyTable(base, override *KeyTable) *KeyTable {
	result := base.Clone()

	mergeRuneMap(result.NormalRunes, override.NormalRunes)
	mergeRuneMap(result.OperatorMotions, override.OperatorMotions)
	mergeRuneMap(result.PrefixG, override.PrefixG)
	mergeRuneMap(result.OverlayRunes, override.OverlayRunes)

	mergeKeyMap(result.SpecialKeys, override.SpecialKeys)
	mergeKeyMap(result.OverlayKeys, override.OverlayKeys)
	mergeKeyMap(result.TextNavKeys, override.TextNavKeys)

	return result
}

func mergeRuneMap(base, override map[rune]KeyEntry) {
	if override == nil {
		return
	}
	for k, v := range override {
		if v.Behavior == BehaviorNone {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
}

func mergeKeyMap(base, override map[terminal.Key]KeyEntry) {
	if override == nil {
		return
	}
	for k, v := range override {
		if v.Behavior == BehaviorNone {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
}
