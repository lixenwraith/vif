package resource

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/content"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
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

// TestCheckReportsTheProgramsLootAndMapsItCompiled: a scenario declaring none says
// so, and a map kept in a file is a scenario file as a program's is
func TestCheckReportsTheProgramsLootAndMapsItCompiled(t *testing.T) {
	looted := writePrograms(t, map[string]string{"wisp": strings.Replace(wisp, "hp = 1", "hp = 1\ndrops = \"spark\"", 1)})
	entry, err := os.OpenFile(filepath.Join(looted, "scenario.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = entry.WriteString("[loot.ember]\nrune = \"e\"\ncolor = {}\nglow = {}\nreward = \"heat\"\ndelta = 1\n" +
		"[drops.spark]\ntiers = [{ entries = [{ loot = \"ember\", rate = 1.0 }] }]\n" +
		"[maps]\nrank = { file = \"rank.toml\" }\n")
	err = errors.Join(err, entry.Close(), os.WriteFile(filepath.Join(looted, "rank.toml"),
		[]byte("legend = { w = { program = \"wisp\" } }\nrows = [\"w w\"]\n"), 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if n := read(t, looted).Files(); n != 4 {
		t.Fatalf("the scenario holds %d files, want its map among the entry, the region and the program", n)
	}
	for want, dir := range map[string]string{
		"behaviors ok: 2 (spark, wisp)\nloot ok: 0\ndrops ok: 0\nmaps ok: 0\n":                 writePrograms(t, map[string]string{"spark": wisp, "wisp": wisp}),
		"behaviors ok: 0\nloot ok: 0\ndrops ok: 0\nmaps ok: 0\n":                               writeScenario(t, filepath.Join(t.TempDir(), "scenario", "plain"), scenarioRegion),
		"behaviors ok: 1 (wisp)\nloot ok: 1 (ember)\ndrops ok: 1 (spark)\nmaps ok: 1 (rank)\n": looted,
	} {
		var out strings.Builder
		if _, err := checkScenario(Options{Scenario: dir}, &out); err != nil || !strings.HasSuffix(out.String(), want) {
			t.Errorf("check printed %q, %v; want it to end %q", out.String(), err, want)
		}
	}
}

// TestASavedDownloadIsFoundByItsDigestUntilEdited: a scenario saved in the
// downloads, nested files and all, is found by its digest and by no other; once the
// player edits it, it answers no join, and a later save leaves the edit standing.
func TestASavedDownloadIsFoundByItsDigestUntilEdited(t *testing.T) {
	sc := read(t, writePrograms(t, map[string]string{"scout": "[prototype]\n"}))
	dir := filepath.Join(t.TempDir(), "downloads")
	at, err := SaveDownload(dir, sc)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := FindDownload(dir, sc.Digest()); !ok || got.Digest() != sc.Digest() || got.Files() != sc.Files() {
		t.Fatalf("the saved scenario was not found by its digest: %t, %d files", ok, got.Files())
	}
	if _, ok := FindDownload(dir, strings.Repeat("0", len(sc.Digest()))); ok {
		t.Fatal("a download answered another digest")
	}
	edited := filepath.Join(at, "main.toml")
	if err := os.WriteFile(edited, []byte(scenarioRegion+"# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := FindDownload(dir, sc.Digest()); ok {
		t.Fatal("an edited download answered the digest it was saved under")
	}
	if _, err := SaveDownload(dir, sc); err == nil {
		t.Fatal("a save over an edited download succeeded")
	}
	if body, _ := os.ReadFile(edited); !strings.Contains(string(body), "# mine") {
		t.Fatal("a save replaced the player's edit")
	}
}

// TestNoNameAHostSendsLeavesTheDownloads: a download is named for its scenario and
// digest whatever name its host sent, so none climbs out of the directory, hides,
// reads as a flag, or takes a name the player's own scenarios could have.
func TestNoNameAHostSendsLeavesTheDownloads(t *testing.T) {
	sc := read(t, writeScenario(t, filepath.Join(t.TempDir(), "td"), scenarioRegion))
	dir := t.TempDir()
	safe := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}-` + sc.Short() + `$`)
	for name, want := range map[string]string{
		"../../escape": "escape", "/etc/vif": "etc_vif", ".hidden": "hidden", "-s": "s",
		"日本語": "scenario", strings.Repeat("x", 300): strings.Repeat("x", 48),
		EmbeddedLabel: "main", "td-" + sc.Short(): "td",
	} {
		sc.Name = name
		at, err := SaveDownload(dir, sc)
		if err != nil || filepath.Dir(at) != dir || !safe.MatchString(filepath.Base(at)) ||
			filepath.Base(at) != want+"-"+sc.Short() {
			t.Errorf("%q saved as %q, %v; want %s-%s in %s", name, at, err, want, sc.Short(), dir)
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

// TestNavigationTakesTickCounts: [navigation] field_refresh_ticks and
// route_rebuild_ticks take a tick count from one to a minute's, unset reads zero,
// and anything else, or another key, is refused.
func TestNavigationTakesTickCounts(t *testing.T) {
	for _, c := range []struct {
		table            string
		refresh, rebuild int
		refused          bool
	}{
		{"", 0, 0, false},
		{"[navigation]\nfield_refresh_ticks = 4\nroute_rebuild_ticks = 40\n", 4, 40, false},
		{"[navigation]\nfield_refresh_ticks = 0\n", 0, 0, true},
		{fmt.Sprintf("[navigation]\nroute_rebuild_ticks = %d\n", parameter.NavTicksMax+1), 0, 0, true},
		{"[navigation]\nfield_refresh_ticks = 2.5\n", 0, 0, true},
		{"[navigation]\nfield_refresh = 4\n", 0, 0, true},
	} {
		entry := "[regions.main]\nfile = \"main.toml\"\n" + c.table
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("navigation", fsys, "scenario.toml")
		switch {
		case c.refused && err == nil:
			t.Errorf("%q was taken", c.table)
		case !c.refused && err != nil:
			t.Errorf("%q: %v", c.table, err)
		case !c.refused && (s.NavRefreshTicks != c.refresh || s.NavRebuildTicks != c.rebuild):
			t.Errorf("%q: refresh %d, rebuild %d, want %d and %d", c.table, s.NavRefreshTicks, s.NavRebuildTicks, c.refresh, c.rebuild)
		}
	}
}

// TestAScenarioKeymapBindsPlayKeysAlone: [keymap] takes the normal, operator and
// prefix_g sections and normal_keys, each key a play action or none. A system
// action, a special key the default keymap gives one, either of which could take
// the quit or the menu from a player who joined, and any other section are refused.
func TestAScenarioKeymapBindsPlayKeysAlone(t *testing.T) {
	appends, _ := input.ActionEntry("mode_append")
	redo, _ := input.ActionEntry("redo_change")
	keys, _ := terminal.KeyByName("ctrl_r")
	for _, c := range []struct {
		table   string
		refused bool
	}{
		{"[keymap.normal]\na = \"mode_append\"\np = \"none\"\n[keymap.normal_keys]\nctrl_r = \"redo_change\"\n", false},
		{"[keymap.normal]\nj = \"quit\"\n", true},
		{"[keymap.normal_keys]\nctrl_r = \"help\"\n", true},
		{"[keymap.normal_keys]\nescape = \"redo_change\"\n", true},
		{"[keymap.overlay]\nq = \"overlay_activate\"\n", true},
		{"[keymap.normal]\na = \"no_such_action\"\n", true},
		{"keymap = 1\n", true},
	} {
		entry := c.table + "[regions.main]\nfile = \"main.toml\"\n"
		fsys := fstest.MapFS{"scenario.toml": {Data: []byte(entry)}, "main.toml": {Data: []byte(scenarioRegion)}}
		s, err := ReadScenario("keys", fsys, "scenario.toml")
		switch {
		case c.refused && err == nil:
			t.Errorf("%q was taken", c.table)
		case !c.refused && err != nil:
			t.Errorf("%q: %v", c.table, err)
		case !c.refused && (s.Keys.NormalRunes['a'] != appends || s.Keys.NormalRunes['p'].Behavior != input.BehaviorNone ||
			s.Keys.SpecialKeys[keys] != redo):
			t.Errorf("%q read as %+v and %+v", c.table, s.Keys.NormalRunes, s.Keys.SpecialKeys)
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

// TestAScenarioCarriesItsBotsAsFilesOfItsOwn: the bot documents an entry's bots
// lists are carried and digested as a region file is and travel in the canonical
// form. A name finds the player's own bot, then the scenario's, then the embedded
// one, and no name the scenario's first, or default where it lists none.
func TestAScenarioCarriesItsBotsAsFilesOfItsOwn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	root := t.TempDir()
	dir := writeScenario(t, filepath.Join(root, "scenario", "flight"), scenarioRegion)
	write := func(name, body string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(bots string) {
		write("scenario/flight/scenario.toml", "bots = "+bots+"\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n")
	}
	entry(`["bot/pilot.toml", "bot/spare.toml"]`)
	write("scenario/flight/bot/pilot.toml", "# pilot")
	write("scenario/flight/bot/spare.toml", "# spare")
	write("scenario/flight/pilot.toml", "# another pilot")

	sc, o := read(t, dir), Options{Dir: root}
	copied, err := UnmarshalScenario("flight", sc.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Scenario{sc, copied} {
		if s.Files() != 4 || !slices.Equal(s.Bots(), []string{"pilot", "spare"}) {
			t.Fatalf("the scenario holds %d files and bots %v; want the entry, the region and its two bots in the entry's order", s.Files(), s.Bots())
		}
		for spec, want := range map[string]string{"": "pilot", "pilot": "pilot", "spare": "spare"} {
			if data, name, err := BotGraph(o, s, spec); err != nil || name != want || string(data) != "# "+want {
				t.Fatalf("bot %q = %q named %q, %v; want the scenario's %s", spec, data, name, err, want)
			}
		}
	}
	if data, name, err := BotGraph(o, Scenario{}, ""); err != nil || name != DefaultBot || len(data) == 0 {
		t.Fatalf("no name in a scenario that carries no bot = %q, %v; want the embedded %s", name, err, DefaultBot)
	}
	if _, _, err := BotGraph(o, Scenario{}, "spare"); err == nil {
		t.Fatal("another scenario's bot was found by its name")
	}
	write("bot/pilot.toml", "# mine")
	if data, _, err := BotGraph(o, sc, ""); err != nil || string(data) != "# mine" {
		t.Fatalf("with a bot of the player's own named as the scenario's = %q, %v; want the player's", data, err)
	}

	for fault, bots := range map[string]string{
		"a file the scenario lacks":   `["bot/none.toml"]`,
		"a name listed twice":         `["bot/pilot.toml", "pilot.toml"]`,
		"a name that is not one word": `["bot/my.pilot.toml"]`,
		"no list":                     `"bot/pilot.toml"`,
	} {
		write("scenario/flight/bot/my.pilot.toml", "# dotted")
		entry(bots)
		if _, err := LoadScenario(Options{Scenario: dir}); err == nil {
			t.Errorf("%s: the scenario loaded", fault)
		}
	}
}

// TestTheEmbeddedScenarioCarriesItsOwnBot: main, which a seat naming no graph plays
// there, where default is still had by its name
func TestTheEmbeddedScenarioCarriesItsOwnBot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	o := Options{Embedded: true}
	sc, err := LoadScenario(o)
	if err != nil || !slices.Equal(sc.Bots(), []string{"main"}) {
		t.Fatalf("the embedded scenario carries %v, %v; want main", sc.Bots(), err)
	}
	for spec, want := range map[string]string{"": "main", "main": "main", DefaultBot: DefaultBot} {
		if data, name, err := BotGraph(o, sc, spec); err != nil || name != want || len(data) == 0 {
			t.Fatalf("bot %q in the embedded scenario is %q, %v; want %s", spec, name, err, want)
		}
	}
}

// flyer is a bot of two networks, fly and kraken, each with the table given
func flyer(fly, kraken string) string {
	return "# a bot that flies\n[bot]\nlearn = true\ninputs = [\"column\"]\n\n[network.fly]\ninputs = [\"energy\", \"heat\"]\n" + fly +
		"\n[network.kraken]\ninputs = [\"row\"]\noutputs = [\"move\"]\n" + kraken +
		"\n[regions.play]\ninitial = \"A\"\n\n[states.A]\non_update = [{ action = \"Steer\", payload = { network = \"fly\" } }]\n"
}

// botFixture is a root holding the scenario flight, which carries the bots pilot
// and spare, an empty user root, and what the verbs of a run of flight are given
func botFixture(t *testing.T) (o Options, sc Scenario, user string, verb func(words ...string) (string, error)) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	root := t.TempDir()
	dir := writeScenario(t, filepath.Join(root, "scenario", "flight"), scenarioRegion)
	for name, body := range map[string]string{
		"scenario.toml":  "bots = [\"bot/pilot.toml\", \"bot/spare.toml\"]\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n",
		"bot/pilot.toml": flyer("x = { column = -0.5, energy = 0.25 } # by hand", "[network.kraken.y]\nrow = -1.0\n"),
		"bot/spare.toml": flyer("y = { heat = 0.75 }", ""),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	o, sc = Options{Dir: root}, read(t, dir)
	return o, sc, paths.PopulationDir(), func(words ...string) (string, error) {
		lines, err := BotFiles(o, sc, words)
		return strings.Join(lines, "\n"), err
	}
}

// weights reads the genomes the bot document at path writes, a network a name
func weights(t *testing.T, path string) bot.Weights {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := bot.ParseGraph("read", data)
	if err != nil {
		t.Fatal(err)
	}
	return g.Weights()
}

// TestABotIsCreatedFromItsScenariosAndTakesWeightsByNetwork: create writes a graph
// of the player's, the scenario's own with no weight or the one addressed, whole
// or with the weights of the network its address names, and import gives one
// that exists those. A graph is addressed as a seat names one, or by a
// scenario's name for its own.
func TestABotIsCreatedFromItsScenariosAndTakesWeightsByNetwork(t *testing.T) {
	o, sc, user, verb := botFixture(t)
	pilot, _ := sc.Bot("pilot")
	nonzero := func(genes []float64) bool { return slices.ContainsFunc(genes, func(w float64) bool { return w != 0 }) }
	at := func(name string) string { return filepath.Join(user, name+".toml") }

	if _, err := verb("create", "mine"); err != nil {
		t.Fatal(err)
	}
	if w := weights(t, at("mine")); len(w) != 2 || nonzero(w["fly"]) || nonzero(w["kraken"]) {
		t.Fatalf("a bot created alone writes %v; want its scenario's own two networks with no weight", w)
	}
	if _, err := verb("create", "whole", "import", "pilot"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(at("whole")); !bytes.Equal(got, pilot) {
		t.Fatalf("a bot created from a source is not its document:\n%s", got)
	}
	if _, err := verb("create", "part", "import", "pilot.kraken"); err != nil {
		t.Fatal(err)
	}
	want := weights(t, at("whole"))
	if w := weights(t, at("part")); nonzero(w["fly"]) || !slices.Equal(w["kraken"], want["kraken"]) {
		t.Fatalf("a bot created with kraken's weights alone writes %v", w)
	}
	for _, name := range []string{"mine", "list", "2", "a.b", "../out"} {
		if told, err := verb("create", name); err == nil {
			t.Errorf("create %s was taken: %s", name, told)
		}
	}

	// What mine learned in flight outranks the weights it is given until cleared
	if err := os.WriteFile(filepath.Join(user, "mine.flight.fly.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	told, err := verb("import", "pilot.fly", "mine")
	if w := weights(t, at("mine")); err != nil || !slices.Equal(w["fly"], want["fly"]) || nonzero(w["kraken"]) || !strings.Contains(told, "mine.flight.fly") {
		t.Fatalf("import of fly's weights wrote %v and told %q, %v; want fly's alone and the population that still plays", w, told, err)
	}
	for _, name := range []string{"none", "../bot/mine"} {
		if told, err := verb("import", "pilot", name); err == nil {
			t.Errorf("import wrote into %s, no bot of the player's by name: %s", name, told)
		}
	}
	for _, words := range [][]string{{"import", "pilot.raid", "mine"}, {"import", "pilot", "mine", "fly"}, {"create", "more", "import", "pilot", "fly"}} {
		if told, err := verb(words...); err == nil {
			t.Errorf("%v was taken, a network its graph lacks or a word too many: %s", words, told)
		}
	}

	// From a run of another scenario, flight's own bot is found by flight's name
	for source, whole := range map[string]bool{"flight": true, "flight.kraken": false} {
		name := "far" + strings.ReplaceAll(source, ".", "")
		lines, err := BotFiles(o, Scenario{}, []string{"create", name, "import", source})
		if err != nil {
			t.Fatalf("%s: %v, %v", source, lines, err)
		}
		if w := weights(t, at(name)); !slices.Equal(w["kraken"], want["kraken"]) || nonzero(w["fly"]) != whole {
			t.Errorf("a graph created from %s writes %v; want that scenario's own, whole or its kraken alone", source, w)
		}
	}
}

// TestWhatBotsLearnedIsListedAndClearedByAddress: list names every graph a seat
// can play, a player's first, clear removes what the graph addressed learned in
// the run's scenario, one network's or all, and no other, and export writes a
// graph no population proves as its document.
func TestWhatBotsLearnedIsListedAndClearedByAddress(t *testing.T) {
	_, sc, user, verb := botFixture(t)
	if _, err := verb("create", "mine"); err != nil {
		t.Fatal(err)
	}
	told, err := verb("list")
	names := regexp.MustCompile(`(?m)^(\S+) \(([^)]+)\)`).FindAllStringSubmatch(told, -1)
	if err != nil || len(names) != 4 || names[0][1] != "mine" || names[0][2] != "yours" || names[1][1] != "pilot" || names[3][2] != "shipped" {
		t.Fatalf("list tells %q, %v; want mine, the scenario's two and the shipped one", told, err)
	}

	kept := []string{"mine.flight.fly", "mine.flight.kraken", "mine.other.fly", "mine2.flight.fly"}
	for _, stem := range kept {
		if err := os.WriteFile(filepath.Join(user, stem+".json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if told, err := verb("clear", "mine.fly"); err != nil || !strings.HasPrefix(told, "Cleared mine.flight.fly;") {
		t.Fatalf("clear of one network told %q, %v", told, err)
	}
	if told, err := verb("clear", "mine"); err != nil || !strings.HasPrefix(told, "Cleared mine.flight.kraken;") {
		t.Fatalf("clear of the rest told %q, %v", told, err)
	}
	if left, _ := filepath.Glob(filepath.Join(user, "*.json")); len(left) != 2 {
		t.Fatalf("clear left %v; want another scenario's and another graph's", left)
	}
	if told, err := verb("clear", "mine"); err == nil {
		t.Errorf("clear of nothing kept told %q", told)
	}

	file := filepath.Join(t.TempDir(), "out", "pilot.toml")
	pilot, _ := sc.Bot("pilot")
	for _, address := range []string{"pilot", "pilot.kraken"} {
		if _, err := verb("export", address, file); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); !bytes.Equal(got, pilot) {
			t.Fatalf("%s, which nothing proved, was exported as:\n%s", address, got)
		}
	}
	if told, err := verb("export", "pilot.raid", file); err == nil {
		t.Errorf("export took a network the graph lacks: %s", told)
	}
}

// TestCheckHoldsAScenariosBotToItsRegionsAndStates: an InState of a graph a
// scenario carries names that scenario's regions and states, or -check faults
// it, since such a guard loads and never holds. A player's graph, which any
// scenario may play, is held to none.
func TestCheckHoldsAScenariosBotToItsRegionsAndStates(t *testing.T) {
	for asks, fault := range map[string]string{
		`region = "main"`:                          "",
		`region = "main", states = ["MainStart"]`:  "",
		`region = "boss"`:                          `region "boss"`,
		`region = "main", states = ["MainFinish"]`: `state "MainFinish"`,
	} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv(paths.SystemRootsEnv, "")
		root := t.TempDir()
		dir := writeScenario(t, filepath.Join(root, "scenario", "flight"), scenarioRegion)
		bound := flyer("", "while = { name = \"InState\", args = { "+asks+" } }")
		for name, body := range map[string]string{
			filepath.Join(dir, "scenario.toml"):    "bots = [\"bot/pilot.toml\"]\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n",
			filepath.Join(dir, "bot/pilot.toml"):   bound,
			filepath.Join(root, "bot/roamer.toml"): flyer("", "while = { name = \"InState\", args = { region = \"elsewhere\" } }"),
		} {
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var out strings.Builder
		err := checkBots(Options{Dir: root}, read(t, dir), &out)
		if fault == "" && (err != nil || !strings.Contains(out.String(), "bot ok: scenario flight pilot")) {
			t.Errorf("InState { %s }: check = %v, reporting:\n%s", asks, err, out.String())
		}
		if fault != "" && (err == nil || !strings.Contains(err.Error(), fault) || strings.Contains(err.Error(), "elsewhere")) {
			t.Errorf("InState { %s }: check = %v; want a fault naming the %s the scenario has not, and none of a player's graph", asks, err, fault)
		}
	}
}

// TestContentFilesAreScenarioFilesAndItsCorpus: the corpus files an entry's [content]
// names are carried and digested as a region file is, serve as the run's corpus in
// place of the roots', and travel in the canonical form; a name the scenario has no
// file for fails the load
func TestContentFilesAreScenarioFilesAndItsCorpus(t *testing.T) {
	dir := writeScenario(t, filepath.Join(t.TempDir(), "scenario", "tutorial"), scenarioRegion)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("scenario.toml", "[content]\nfiles = [\"lessons.toml\"]\nblocks = true\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n")
	write("lessons.toml", "schema = 1\n[[blocks]]\nid = \"one\"\nlines = [\"press w to jump a word\", \"press b to jump back\"]\n")

	sc := read(t, dir)
	if sc.Files() != 3 || !sc.ContentBlocks {
		t.Fatalf("scenario holds %d files, blocks %v; want the entry, the region and the corpus, laid in blocks", sc.Files(), sc.ContentBlocks)
	}
	copied, err := UnmarshalScenario("tutorial", sc.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Scenario{sc, copied} {
		fsys, ok := s.Content()
		if !ok {
			t.Fatal("the scenario carries no corpus")
		}
		corpus, err := content.Load(fsys, content.DefaultPolicy())
		if err != nil || corpus.BlockCount() != 1 || corpus.LineCount() != 2 {
			t.Fatalf("the carried corpus loads as %+v, %v; want its one block of two lines", corpus, err)
		}
	}
	src, err := Corpus(Options{Scenario: dir, Content: dir}, sc)
	if err != nil || src.FS == nil || !src.Explicit || src.Label != "scenario tutorial" {
		t.Fatalf("the run's corpus = %+v, %v; want the scenario's own over the -f root", src, err)
	}

	write("scenario.toml", "[content]\nfiles = [\"missing.toml\"]\n[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n")
	if _, err := LoadScenario(Options{Scenario: dir}); err == nil {
		t.Fatal("a content file the scenario lacks loaded")
	}
}
