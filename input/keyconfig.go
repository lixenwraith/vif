package input

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/toml"
)

// Rune aliases for keys that can't be bare single-char TOML keys
var runeAliases = map[string]rune{
	"space":     ' ',
	"backslash": '\\',
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

// parseRuneSection parses a TOML section of rune key → action name bindings
func parseRuneSection(section string, data map[string]any) (map[rune]KeyEntry, error) {
	result := make(map[rune]KeyEntry, len(data))

	for keyStr, val := range data {
		r, err := resolveRune(keyStr)
		if err != nil {
			return nil, fmt.Errorf("[%s] key %q: %w", section, keyStr, err)
		}

		actionName, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("[%s] key %q: value must be string, got %T", section, keyStr, val)
		}

		entry, err := resolveAction(actionName)
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

	for keyStr, val := range data {
		k, ok := terminal.KeyByName(strings.ToLower(keyStr))
		if !ok {
			return nil, fmt.Errorf("[%s] unknown key name: %q", section, keyStr)
		}

		actionName, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("[%s] key %q: value must be string, got %T", section, keyStr, val)
		}

		entry, err := resolveAction(actionName)
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