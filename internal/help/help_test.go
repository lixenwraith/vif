package help

import (
	"testing"

	"github.com/lixenwraith/vif/internal/input"
)

// documentedByLiteral lists actions whose keys appear inside a literal entry,
// because their meaning depends on what follows them
var documentedByLiteral = map[string]string{
	"operator_delete":     "EDITING d{motion}",
	"prefix_g":            "MOVEMENT gg and the g-prefixed motions",
	"prefix_macro_play":   "MACROS @{a-z}",
	"macro_record_toggle": "MACROS q{a-z}",
}

// TestBindingsDocumented fails when a binding lands without a help entry.
// SectionOperator is excluded: its motions repeat NORMAL and are covered by
// d{motion}. SectionText is excluded: its keys are documented per mode.
func TestBindingsDocumented(t *testing.T) {
	kt := input.DefaultKeyTable()

	for _, section := range []input.KeySection{input.SectionNormal, input.SectionPrefixG, input.SectionOverlay} {
		for _, action := range kt.Actions(section) {
			if _, ok := documentedByLiteral[action]; ok {
				continue
			}
			if !documents(section, action) {
				t.Errorf("section %d: action %q has no help entry", section, action)
			}
		}
	}
}

// TestEntriesResolve fails on a typo or on documentation left behind by an
// unbound action
func TestEntriesResolve(t *testing.T) {
	kt := input.DefaultKeyTable()

	for i := range topics {
		for j := range topics[i].Entries {
			e := &topics[i].Entries[j]
			if len(e.Actions) == 0 {
				if e.Keys == "" {
					t.Errorf("%s: entry %q has neither actions nor literal keys", topics[i].Title, e.Desc)
				}
				continue
			}
			for _, a := range e.Actions {
				if !input.IsActionName(a) {
					t.Errorf("%s: unknown action %q", topics[i].Title, a)
					continue
				}
				if len(kt.Bindings(e.Scope, a)) == 0 {
					t.Errorf("%s: action %q is unbound in section %d", topics[i].Title, a, e.Scope)
				}
			}
			if e.PrefixAction != "" && primaryKey(kt, input.SectionNormal, e.PrefixAction) == "" {
				t.Errorf("%s: prefix action %q is unbound", topics[i].Title, e.PrefixAction)
			}
		}
	}
}

// TestAnEntryShowsEveryKeyOfOneActionAndTheFirstOfSeveral: a player's own keys
// join the key column of an entry documenting their action alone; an entry
// documenting several (h j k l) keeps to the first key of each
func TestAnEntryShowsEveryKeyOfOneActionAndTheFirstOfSeveral(t *testing.T) {
	override, err := input.LoadKeyConfig([]byte("[normal]\ny = \"motion_left\"\n[normal_keys]\nf2 = \"config_menu\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	kt := input.MergeKeyTable(input.DefaultKeyTable(), override)
	for _, tt := range []struct {
		entry entryDef
		want  string
	}{
		{entryDef{Actions: []string{"config_menu"}}, "Ctrl+G/F10/F2"},
		{entryDef{Actions: []string{"motion_left", "motion_down"}}, "h/j"},
	} {
		if got, _ := tt.entry.resolve(kt); got != tt.want {
			t.Errorf("%v shows %q, want %q", tt.entry.Actions, got, tt.want)
		}
	}
}

// TestTopicsNonEmpty asserts the default keymap resolves every section
func TestTopicsNonEmpty(t *testing.T) {
	got := Topics(input.DefaultKeyTable())
	if len(got) != len(topics) {
		t.Fatalf("resolved %d topics, want %d", len(got), len(topics))
	}
	for _, tp := range got {
		if tp.Key == "" || tp.Title == "" || len(tp.Entries) == 0 {
			t.Errorf("topic %q resolved empty", tp.Title)
		}
	}
}

// documents reports whether any entry claims an action in a section
func documents(section input.KeySection, action string) bool {
	for i := range topics {
		for j := range topics[i].Entries {
			e := &topics[i].Entries[j]
			if e.Scope != section {
				continue
			}
			for _, a := range e.Actions {
				if a == action {
					return true
				}
			}
		}
	}
	return false
}
