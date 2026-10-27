package resource

import (
	"bytes"
	"compress/flate"
	"fmt"
	"github.com/lixenwraith/vif/internal/parameter"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/fsm"
)

// writeScenario lays down a two-file scenario under dir and returns its root.
func writeScenario(t *testing.T, dir, regionBody string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "description = \"A portable test scenario.\"\n[regions.main]\nfile = \"main.toml\"\n"
	for name, body := range map[string]string{"scenario.toml": entry, "main.toml": regionBody} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const scenarioRegion = "[states.MainStart]\nparent = \"Root\"\n"

// TestDigestFollowsContentNotLocation is the rule the digest exists for: two roots
// holding the same bytes are the same scenario however they are named or reached,
// and one changed byte is a different one. It is what a join is refused on.
func TestDigestFollowsContentNotLocation(t *testing.T) {
	base := t.TempDir()
	here := writeScenario(t, filepath.Join(base, "one", "scenario", "main"), scenarioRegion)
	there := writeScenario(t, filepath.Join(base, "two", "scenario", "renamed"), scenarioRegion)
	edited := writeScenario(t, filepath.Join(base, "three", "scenario", "main"),
		scenarioRegion+"\n[states.MainOther]\nparent = \"Root\"\n")

	a, b, c := read(t, here), read(t, there), read(t, edited)
	if a.Digest() != b.Digest() {
		t.Errorf("same bytes under different names digest differently:\n  %s\n  %s", a.Digest(), b.Digest())
	}
	if a.Name != "main" || b.Name != "renamed" {
		t.Errorf("names = %q and %q; want the containing directory", a.Name, b.Name)
	}
	if a.Digest() == c.Digest() {
		t.Error("an edited scenario kept its digest")
	}
}

// TestCanonicalFormRoundTrips covers what a transfer will depend on: the bytes a
// digest is taken over rebuild the same scenario, and a corrupted body is refused
// rather than loaded as a shorter one.
func TestCanonicalFormRoundTrips(t *testing.T) {
	src := read(t, writeScenario(t, filepath.Join(t.TempDir(), "scenario", "td"), scenarioRegion))

	back, err := UnmarshalScenario(src.Name, src.Marshal())
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Digest() != src.Digest() || back.Entry() != src.Entry() || back.Files() != src.Files() ||
		back.Description != "A portable test scenario." {
		t.Fatalf("round trip lost identity: %+v", back)
	}
	body, err := fs.ReadFile(back.FS(), "main.toml")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(body), "MainStart") {
		t.Errorf("round-tripped region body = %q", body)
	}

	truncated := src.Marshal()
	if _, err := UnmarshalScenario(src.Name, truncated[:len(truncated)-4]); err == nil {
		t.Error("a truncated body was accepted")
	}
	if _, err := UnmarshalScenario(src.Name, append(src.Marshal(), 0)); err == nil {
		t.Error("a body with trailing bytes was accepted")
	}
}

// TestMacroFilesAreScenarioFiles: a library the regions expand against is part of
// what the digest covers and a transfer carries. Were it not, two peers holding
// different libraries would agree on a digest and simulate different graphs.
func TestMacroFilesAreScenarioFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scenario", "lib")
	writeScenario(t, dir, "[states]\nMainStart = { macro = \"Wait\", next = \"MainStart\" }\n")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("scenario.toml", "macro_files = [\"macros.toml\"]\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n")
	library := "[macros.Wait]\nparams = { next = \"string\" }\nbody = { transitions = [{ trigger = \"Tick\", target = \"${next}\" }] }\n"
	write("macros.toml", library)

	before := read(t, dir)
	if before.Files() != 3 {
		t.Fatalf("scenario holds %d files, want the entry, the region and the library", before.Files())
	}
	merged, err := fsm.ResolveScenario(before.FS(), before.Entry())
	if err != nil {
		t.Fatalf("resolve from the held copy: %v", err)
	}
	var doc fsm.ScenarioDoc
	if err := toml.Decode(merged, &doc); err != nil || doc.States["MainStart"].Transitions[0].Target != "MainStart" {
		t.Fatalf("expanded state = %+v, %v", doc.States["MainStart"], err)
	}

	write("macros.toml", strings.Replace(library, "Tick", "EventGameResetRequest", 1))
	if read(t, dir).Digest() == before.Digest() {
		t.Error("an edited macro file kept the scenario's digest")
	}
}

// TestABehaviorProgramIsAScenarioFile: a program file is carried and digested as a
// region file is, expands against the entry's macros, and reads as the same
// program written inline; the machine leaves the table to its embedder
func TestABehaviorProgramIsAScenarioFile(t *testing.T) {
	dir := writeScenario(t, filepath.Join(t.TempDir(), "scenario", "arena"), scenarioRegion)
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	program := "initial = \"Idle\"\n[states]\nIdle = { macro = \"Still\" }\n"
	write("behavior/warden.toml", program)
	write("scenario.toml", "[macros.Still]\nbody = { on_update = [{ action = \"Freeze\" }] }\n"+
		"[regions.main]\nfile = \"main.toml\"\n"+
		"[behaviors]\nwarden = { file = \"behavior/warden.toml\" }\n"+
		"[behaviors.inline]\n"+strings.Replace(program, "[states]", "[behaviors.inline.states]", 1))

	before := read(t, dir)
	if before.Files() != 3 {
		t.Fatalf("scenario holds %d files, want the entry, the region and the program", before.Files())
	}
	programs, err := before.Behaviors()
	if err != nil {
		t.Fatal(err)
	}
	states, _ := programs["warden"]["states"].(map[string]any)
	if _, expanded := states["Idle"].(map[string]any)["on_update"]; !expanded || !reflect.DeepEqual(programs["warden"], programs["inline"]) {
		t.Fatalf("programs %v", programs)
	}
	if err := fsm.LoadScenarioFromFS(fsm.NewMachine[int](), before.FS(), before.Entry()); err != nil {
		t.Fatalf("machine load: %v", err)
	}

	write("behavior/warden.toml", strings.Replace(program, "Idle", "Rest", 2))
	if read(t, dir).Digest() == before.Digest() {
		t.Error("an edited program kept the scenario's digest")
	}
}

// TestABehaviorTableNamesAFileOrIsTheProgram: a table naming a file holds nothing
// else, so no key beside file is silently dropped
func TestABehaviorTableNamesAFileOrIsTheProgram(t *testing.T) {
	for program, want := range map[string]string{
		`{ file = 3 }`:                          "behavior 'warden': file must be a string",
		`{ file = "w.toml", initial = "Roam" }`: "behavior 'warden': file must be the table's only key",
	} {
		entry := "[regions.main]\nfile = \"main.toml\"\n[behaviors]\nwarden = " + program + "\n"
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		if _, err := ReadScenario("arena", fsys, "scenario.toml"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want %q", program, err, want)
		}
	}
}

// wisp is the least program that compiles: one cell standing still
const wisp = "initial = \"Idle\"\n" +
	"[prototype]\nwidth = 1\nheight = 1\nheader = { x = 0, y = 0 }\nhp = 1\ncombat = \"swarm\"\nframes = [{ glyphs = [\"o\"] }]\n" +
	"[states.Idle]\non_update = [{ action = \"Freeze\" }]\n"

// writePrograms lays down a scenario whose entry names each program's file
func writePrograms(t *testing.T, programs map[string]string) string {
	t.Helper()
	dir := writeScenario(t, filepath.Join(t.TempDir(), "scenario", "arena"), scenarioRegion)
	entry := "[regions.main]\nfile = \"main.toml\"\n[behaviors]\n"
	for _, name := range slices.Sorted(maps.Keys(programs)) {
		path := filepath.Join(dir, "behavior", name+".toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(programs[name]), 0o600); err != nil {
			t.Fatal(err)
		}
		entry += name + " = { file = \"behavior/" + name + ".toml\" }\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.toml"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAProgramThatDoesNotCompileFailsTheScenario: validation compiles every program
// against the game's verbs, so -check and a scenario change refuse it, naming its
// place, before anything is torn down
func TestAProgramThatDoesNotCompileFailsTheScenario(t *testing.T) {
	over := map[string]string{}
	for i := range parameter.BehaviorProgramsMax + 1 {
		over[fmt.Sprintf("wisp%d", i)] = wisp
	}
	for name, tc := range map[string]struct {
		programs map[string]string
		want     string
	}{
		"misspelt verb": {map[string]string{"wisp": strings.Replace(wisp, "Freeze", "Freez", 1)},
			"scenario arena: program 'wisp': state 'Idle': on_update[0]: unknown sustained action 'Freez'"},
		"one program too many": {over, fmt.Sprintf("scenario arena: %d behavior programs, at most %d",
			parameter.BehaviorProgramsMax+1, parameter.BehaviorProgramsMax)},
	} {
		if _, err := ValidateScenario(Options{Scenario: writePrograms(t, tc.programs)}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
}

// TestCheckReportsTheProgramsItCompiled: a scenario declaring none says so
func TestCheckReportsTheProgramsItCompiled(t *testing.T) {
	for want, dir := range map[string]string{
		"behaviors ok: 2 (spark, wisp)\n": writePrograms(t, map[string]string{"spark": wisp, "wisp": wisp}),
		"behaviors ok: 0\n":               writeScenario(t, filepath.Join(t.TempDir(), "scenario", "plain"), scenarioRegion),
	} {
		var out strings.Builder
		if err := checkScenario(Options{Scenario: dir}, &out); err != nil || !strings.HasSuffix(out.String(), want) {
			t.Errorf("check printed %q, %v; want it to end %q", out.String(), err, want)
		}
	}
}

func read(t *testing.T, dir string) Scenario {
	t.Helper()
	s, err := LoadScenario(Options{Scenario: dir})
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return s
}

// TestATransferCarriesBytesByLengthNotByScanning is why the whole container is
// deflated as one stream rather than one per file: the framing is length-prefixed,
// so a file holding the container's own magic, a NUL or an unbalanced quote is
// taken by its declared length and cannot run into its neighbour. It also bounds
// the inflation, so a few compressed bytes cannot be made to allocate a megabyte.
func TestATransferCarriesBytesByLengthNotByScanning(t *testing.T) {
	adversarial := scenarioRegion +
		"\n# VIFSCEN\x00\x00\x01 unbalanced \" and a lone ' in a comment\n" +
		"[states.MainOther]\nparent = \"Root\"\n"
	src := read(t, writeScenario(t, filepath.Join(t.TempDir(), "scenario", "main"), scenarioRegion))
	// TOML refuses a NUL, so it cannot load from disk; a peer's body can still carry one.
	src.files[slices.IndexFunc(src.files, func(f scenarioFile) bool { return f.name == "main.toml" })].data = []byte(adversarial)
	src.digest = digestOf(src.Marshal())

	body, err := src.MarshalCompressed()
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	back, err := UnmarshalScenarioCompressed(src.Name, body)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	if back.Digest() != src.Digest() {
		t.Fatalf("round trip changed the digest")
	}
	region, err := fs.ReadFile(back.FS(), "main.toml")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(region) != adversarial {
		t.Errorf("region came back as %q", region)
	}
	entry, err := fs.ReadFile(back.FS(), "scenario.toml")
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if strings.Contains(string(entry), "MainOther") {
		t.Error("one file's bytes reached the next")
	}

	var bomb bytes.Buffer
	w, _ := flate.NewWriter(&bomb, flate.BestCompression)
	_, _ = w.Write(make([]byte, parameter.ScenarioMaxBytes*4))
	_ = w.Close()
	if _, err := UnmarshalScenarioCompressed("bomb", bomb.Bytes()); err == nil {
		t.Fatalf("a %d-byte body inflating to %d was accepted", bomb.Len(), parameter.ScenarioMaxBytes*4)
	}
}

// TestAMapPlacesCursorsUnlessItSaysNot: [map] cursors = false takes the cursors off
// the map, true or no key leaves them on it, and anything but a boolean is refused.
func TestAMapPlacesCursorsUnlessItSaysNot(t *testing.T) {
	for _, c := range []struct {
		table   string
		offMap  bool
		refused bool
	}{
		{"", false, false},
		{"[map]\ncursors = true\n", false, false},
		{"[map]\ncursors = false\n", true, false},
		{"[map]\ncursors = 0\n", false, true},
	} {
		entry := "[regions.main]\nfile = \"main.toml\"\n" + c.table
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("cursors", fsys, "scenario.toml")
		if c.refused {
			if err == nil {
				t.Errorf("%q was taken", c.table)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.table, err)
		}
		if s.CursorsOffMap != c.offMap {
			t.Errorf("%q: cursors off the map %t, want %t", c.table, s.CursorsOffMap, c.offMap)
		}
	}
}

// TestAScenarioDeclaresItsPlayerRange: min_players and max_players default to one
// and a full roster, min may be 0, and a range nobody could join or that is upside
// down or past the roster is refused.
func TestAScenarioDeclaresItsPlayerRange(t *testing.T) {
	for _, c := range []struct {
		preamble string
		min, max int
		refused  bool
	}{
		{"", 1, parameter.MaxPlayers, false},
		{"min_players = 0\nmax_players = 2\n", 0, 2, false},
		{"min_players = 3\n", 3, parameter.MaxPlayers, false},
		{"max_players = 0\n", 0, 0, true},
		{"min_players = 3\nmax_players = 2\n", 0, 0, true},
		{fmt.Sprintf("max_players = %d\n", parameter.MaxPlayers+1), 0, 0, true},
		{"min_players = \"1\"\n", 0, 0, true},
	} {
		entry := c.preamble + "[regions.main]\nfile = \"main.toml\"\n"
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("players", fsys, "scenario.toml")
		switch {
		case c.refused && err == nil:
			t.Errorf("%q was taken", c.preamble)
		case !c.refused && err != nil:
			t.Errorf("%q: %v", c.preamble, err)
		case !c.refused && (s.MinPlayers != c.min || s.MaxPlayers != c.max):
			t.Errorf("%q: players %d..%d, want %d..%d", c.preamble, s.MinPlayers, s.MaxPlayers, c.min, c.max)
		}
	}
}

// TestAnEntryKeyNothingReadsIsRefused: a misspelt top-level key fails the read
// naming it, where it once loaded as absent.
func TestAnEntryKeyNothingReadsIsRefused(t *testing.T) {
	entry := "descripton = \"A typo.\"\n[regions.main]\nfile = \"main.toml\"\n"
	fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
	_, err := ReadScenario("typo", fsys, "scenario.toml")
	if err == nil || !strings.Contains(err.Error(), `entry "scenario.toml": unknown key "descripton"`) {
		t.Fatalf("error %v", err)
	}
}
