package input

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
)

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

// TestUpgradingAKeymapKeepsThePlayersBindings: an older keymap loses the sections
// this build does not read, the keys it cannot name and the bindings it refuses,
// each with its own comments, and gains each embedded binding of a key it binds in
// no spelling; every other line stays, unbindings included. The result, like the
// embedded keymap, upgrades to itself.
func TestUpgradingAKeymapKeepsThePlayersBindings(t *testing.T) {
	old := "# mine\n[normal]\nh = \"motion_right\" # mine\nSPACE = \"fire_main\"\nv = \"none\"\n# a typo\nq = \"macro_record\"\n" +
		"xy = \"undo\"\n\n# gone\n[later]\nkey = \"undo\"\n\n[normal_keys]\nhyper_q = \"quit\"\n\n[text_keys]\nShift_Tab = \"help\"\nctrl_q = 3\n"
	out, notes, err := UpgradeKeymap([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(out), "\n")
	gone := []string{"# a typo", `q = "macro_record"`, `xy = "undo"`, "# gone", "[later]", `key = "undo"`, `hyper_q = "quit"`, "ctrl_q = 3"}
	for _, line := range strings.Split(old, "\n") {
		if kept := slices.Contains(lines, line); kept == slices.Contains(gone, line) {
			t.Errorf("line %q kept %v", line, kept)
		}
	}
	kt, err := LoadKeyConfig(out)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := ActionEntry("macro_record_toggle")
	fire, _ := ActionEntry("fire_main")
	quit, _ := ActionEntry("quit")
	help, _ := ActionEntry("help")
	if kt.NormalRunes['q'] != record || kt.NormalRunes[' '] != fire || slices.Contains(lines, `space = "fire_special"`) ||
		kt.NormalRunes['v'].Behavior != BehaviorNone || kt.TextNavKeys[terminal.KeyCtrlQ] != quit || kt.TextNavKeys[terminal.KeyBacktab] != help {
		t.Errorf("bindings after the upgrade:\n%s", out)
	}
	if len(notes) != 4 || notes[0] != `dropped [later], [normal] "xy", [normal_keys] "hyper_q"` ||
		notes[1] != `reset to the default: [normal] key "q": unknown action: "macro_record"` ||
		notes[2] != `reset to the default: [text_keys] key "ctrl_q": value must be string, got int64` || !strings.HasSuffix(notes[3], " bindings") {
		t.Errorf("notes %q", notes)
	}
	for _, doc := range [][]byte{out, asset.DefaultKeymap} {
		if again, notes, err := UpgradeKeymap(doc); err != nil || len(notes) > 0 || string(again) != string(doc) {
			t.Errorf("an up-to-date keymap changed, %q, %v:\n%s", notes, err, again)
		}
	}
}

// TestAnUpgradePutsBackEachShippedBindingAsShipped: the embedded keymap lacking any
// one binding or section upgrades to itself byte for byte, so what a build adds
// reaches an older keymap where and as the embedded one has it, each binding named.
func TestAnUpgradePutsBackEachShippedBindingAsShipped(t *testing.T) {
	shipped, err := toml.ParseDocument(asset.DefaultKeymap)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range slices.Sorted(maps.Keys(shipped.Tree())) {
		keys := []toml.Key{{section}}
		for _, k := range slices.Sorted(maps.Keys(shipped.Tree()[section].(map[string]any))) {
			keys = append(keys, toml.Key{section, k})
		}
		for _, key := range keys {
			doc, _ := toml.ParseDocument(asset.DefaultKeymap)
			if _, err := doc.Delete(key); err != nil {
				t.Fatal(err)
			}
			out, notes, err := UpgradeKeymap(doc.Bytes())
			if err != nil || string(out) != string(asset.DefaultKeymap) {
				t.Errorf("without %s, %q, %v:\n%s", key, notes, err, out)
			}
			if len(key) == 2 && !slices.Equal(notes, []string{fmt.Sprintf("added [%s] %q", key[0], key[1])}) {
				t.Errorf("without %s: notes %q", key, notes)
			}
		}
	}
}

// TestUpgradingAKeymapEditsInlineSections: a key to drop or a binding it refuses in
// a section written inline goes as that section is written again without it, its
// other bindings kept.
func TestUpgradingAKeymapEditsInlineSections(t *testing.T) {
	out, notes, err := UpgradeKeymap([]byte("normal = { h = \"undo\", zz = \"x\", q = \"bogus\" } # mine\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "normal = { h = \"undo\" } # mine\n") || len(notes) != 3 || notes[0] != `dropped [normal] "zz"` ||
		notes[1] != `reset to the default: [normal] key "q": unknown action: "bogus"` {
		t.Errorf("notes %q:\n%s", notes, out)
	}
	undo, _ := ActionEntry("undo")
	if kt, err := LoadKeyConfig(out); err != nil || kt.NormalRunes['h'] != undo {
		t.Errorf("binds %+v, %v", kt, err)
	}
}

// TestAKeySpeltTwoWaysBindsAsTheSpellingSortingLast: a section binding one key under
// two spellings binds it as the one that sorts last binds it, before an upgrade and
// after, which keeps both lines.
func TestAKeySpeltTwoWaysBindsAsTheSpellingSortingLast(t *testing.T) {
	doc := []byte("[normal]\nSPACE = \"fire_main\"\nspace = \"none\"\n[normal_keys]\nESCAPE = \"quit\"\nescape = \"help\"\n")
	out, _, err := UpgradeKeymap(doc)
	if err != nil {
		t.Fatal(err)
	}
	help, _ := ActionEntry("help")
	for _, d := range [][]byte{doc, out} {
		kt, err := LoadKeyConfig(d)
		if err != nil || kt.NormalRunes[' '].Behavior != BehaviorNone || kt.SpecialKeys[terminal.KeyEscape] != help {
			t.Errorf("binds space %+v, escape %+v, %v:\n%s", kt.NormalRunes[' '], kt.SpecialKeys[terminal.KeyEscape], err, d)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(doc)), "\n") {
		if !strings.Contains(string(out), line+"\n") {
			t.Errorf("line %q gone:\n%s", line, out)
		}
	}
}

// TestKeymapUpgradeNotesNameEachChangeOnce: a refused binding whose key the
// embedded keymap spells otherwise is reset, not also added; one another spelling
// binds is dropped, not reset; a value outside any section is named as a key.
func TestKeymapUpgradeNotesNameEachChangeOnce(t *testing.T) {
	shipped, line := string(asset.DefaultKeymap), "\nspace = \"fire_special\"\n"
	if !strings.Contains(shipped, line) {
		t.Fatalf("the embedded keymap has no %q", line)
	}
	for doc, want := range map[string]string{
		strings.Replace(shipped, line, "\nSPACE = \"bogus\"\n", 1):                        `reset to the default: [normal] key "SPACE": unknown action: "bogus"`,
		strings.Replace(shipped, line, "\nSPACE = \"fire_main\"\nspace = \"bogus\"\n", 1): `dropped [normal] key "space": unknown action: "bogus"`,
		"version = 3\n" + shipped: `dropped "version"`,
	} {
		if _, notes, err := UpgradeKeymap([]byte(doc)); err != nil || !slices.Equal(notes, []string{want}) {
			t.Errorf("%s: notes %q, %v", want, notes, err)
		}
	}
}

// FuzzUpgradingAKeymapBindsAsBefore: every keymap document that parses upgrades,
// its upgrade reads whole and is up to date, and one this build reads binds every key
// as it did.
func FuzzUpgradingAKeymapBindsAsBefore(f *testing.F) {
	f.Add(string(asset.DefaultKeymap))
	f.Add("# mine\n[normal]\nh = \"motion_right\" # mine\nSPACE = \"none\"\n\n[later]\nx = 1\n")
	f.Add("normal.h = \"undo\"\n[text_keys]\nShift_Tab = \"help\"\nctrl_q = 3\n")
	f.Add("[[normal]]\nh = \"undo\"\n[overlay]\nq = { a = 1 }\n")
	f.Add("normal = { h = \"undo\", zz = \"x\", q = \"bogus\" }\n[normal_keys]\nEscape = \"none\"\nescape = \"quit\"\n")
	f.Fuzz(func(t *testing.T, doc string) {
		out, _, err := UpgradeKeymap([]byte(doc))
		if _, parse := toml.ParseDocument([]byte(doc)); parse != nil {
			return
		} else if err != nil {
			t.Fatalf("a keymap that parses does not upgrade: %v\n%s", err, doc)
		}
		after, err := LoadKeyConfig(out)
		if err != nil {
			t.Fatalf("the upgrade does not read: %v\n%s", err, out)
		}
		if again, notes, err := UpgradeKeymap(out); err != nil || len(notes) > 0 || string(again) != string(out) {
			t.Fatalf("the upgrade is not up to date, %q, %v:\n%s", notes, err, again)
		}
		before, err := LoadKeyConfig([]byte(doc))
		if err != nil {
			return
		}
		if def := DefaultKeyTable(); !reflect.DeepEqual(MergeKeyTable(def, before), MergeKeyTable(def, after)) {
			t.Fatalf("the upgrade binds otherwise:\n%s", out)
		}
	})
}
