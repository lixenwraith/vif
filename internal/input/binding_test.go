package input

import "testing"

// TestActionEntriesUnique guards the KeyEntry inversion: two action names
// sharing one entry would make the reverse lookup arbitrary
func TestActionEntriesUnique(t *testing.T) {
	seen := make(map[KeyEntry]string, len(actionRegistry))
	for name, entry := range actionRegistry {
		if entry.Behavior == BehaviorNone {
			continue
		}
		if prev, dup := seen[entry]; dup {
			t.Errorf("actions %q and %q share a KeyEntry", prev, name)
			continue
		}
		seen[entry] = name
	}
}

// TestBindingsResolve asserts every default binding maps back to an action
func TestBindingsResolve(t *testing.T) {
	kt := DefaultKeyTable()
	sections := []KeySection{SectionNormal, SectionOperator, SectionPrefixG, SectionOverlay, SectionText}

	for _, s := range sections {
		runes, keys := kt.sectionMaps(s)
		for r, e := range runes {
			if _, ok := EntryAction(e); !ok {
				t.Errorf("section %d: rune %q has no action name", s, string(r))
			}
		}
		for k, e := range keys {
			if _, ok := EntryAction(e); !ok {
				t.Errorf("section %d: key %s has no action name", s, SpecialKeyName(k))
			}
		}
	}
}
