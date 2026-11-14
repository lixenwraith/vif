package resource

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/paths"
)

func writeFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestResolutionPrecedence pins the ordering rule: an earlier root wins over a
// later one, for files and for directories alike.
func TestResolutionPrecedence(t *testing.T) {
	base := t.TempDir()
	user, system := filepath.Join(base, "user"), filepath.Join(base, "system")

	systemScenario := filepath.Join(system, paths.ScenarioDirName, paths.MainScenarioName, paths.ScenarioFile)
	writeFixture(t, systemScenario)
	r := resolver{roots: []string{user, system}}
	if got := r.scenario(paths.MainScenarioName); got != systemScenario {
		t.Fatalf("scenario path = %q, want the system root's file %q", got, systemScenario)
	}

	userScenario := filepath.Join(user, paths.ScenarioDirName, paths.MainScenarioName, paths.ScenarioFile)
	writeFixture(t, userScenario)
	if got := r.scenario(paths.MainScenarioName); got != userScenario {
		t.Fatalf("scenario path = %q, want the user root's file %q", got, userScenario)
	}

	systemContent := filepath.Join(system, paths.ContentDirName)
	if err := os.MkdirAll(systemContent, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.dir(paths.ContentDirName); got != systemContent {
		t.Fatalf("content dir = %q, want the system root %q", got, systemContent)
	}
	userContent := filepath.Join(user, paths.ContentDirName)
	if err := os.MkdirAll(userContent, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.dir(paths.ContentDirName); got != userContent {
		t.Fatalf("content dir = %q, want the user root %q", got, userContent)
	}
}

// TestARunWritesBackOnlyThePlayersOwnKeymap: a keymap an older build left is
// written back brought up to date, the status line naming the changes, when it is
// the player's own: in the user or -config-dir root, or named by -k. One in a system
// root however named, one a vif.toml not the player's own names, a special file such
// as /dev/null, and a file that is no keymap are left as they are.
func TestARunWritesBackOnlyThePlayersOwnKeymap(t *testing.T) {
	doc := "# mine\n[normal]\na = \"unknown_action\"\nh = \"motion_right\" # mine\n"
	dir, system, named, home := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "mine.toml"), t.TempDir()
	for _, name := range []string{"XDG_CONFIG_HOME", "HOME", "AppData", "home"} {
		t.Setenv(name, home) // every variable a platform finds the user root by
	}
	t.Setenv(paths.SystemRootsEnv, system)
	if rel, err := filepath.Rel(home, paths.UserRoot()); err != nil || !filepath.IsLocal(rel) || !slices.Contains(paths.ConfigRoots(""), system) {
		t.Skip("the user root lies elsewhere, or system roots are not read here")
	}
	user := filepath.Join(paths.UserRoot(), paths.InputDirName, paths.KeymapConfigFile)
	site := filepath.Join(system, paths.InputDirName, paths.KeymapConfigFile)
	for _, c := range []struct {
		o         Options
		user, own bool
	}{
		{Options{Dir: dir}, false, true},
		{Options{Keymap: named}, false, true},
		{Options{}, true, true},
		{Options{}, false, false},
		{Options{Dir: dir, ForeignDir: true}, false, false},
		{Options{Keymap: named, ForeignKeymap: true}, false, false},
		{Options{Dir: system}, true, false},
		{Options{Keymap: site}, false, false},
		{Options{Keymap: os.DevNull}, false, false},
	} {
		for _, root := range []string{dir, system} {
			writeKeymap(t, filepath.Join(root, paths.InputDirName, paths.KeymapConfigFile), doc)
		}
		writeKeymap(t, named, doc)
		if os.Remove(user); c.user {
			writeKeymap(t, user, doc)
		}
		path, own, err := keymap(c.o)
		if err != nil || own != c.own {
			t.Fatalf("%+v: %s is the player's own %v, %v", c.o, path, own, err)
		}
		before, _ := os.ReadFile(path)
		said := UpgradeKeymap(c.o)
		got, _ := os.ReadFile(path)
		switch {
		case !c.own && (said != "" || string(got) != string(before)):
			t.Errorf("%+v, not the player's: %q, written:\n%s", c.o, said, got)
		case c.own && (!strings.HasPrefix(said, `Keymap brought up to date: reset to the default: [normal] key "a"`) ||
			!strings.Contains(string(got), "# mine\n[normal]\n") || !strings.Contains(string(got), `h = "motion_right" # mine`) ||
			strings.Contains(string(got), "unknown_action")):
			t.Errorf("%+v, the player's own: %q, written:\n%s", c.o, said, got)
		}
	}
	settings := "schema = 1\n[map]\nwidth = 80\n"
	writeKeymap(t, named, settings)
	if said, err := UpgradeKeymap(Options{Keymap: named}), checkKeymap(Options{Keymap: named}, io.Discard); said != "" || err == nil {
		t.Errorf("a file that is no keymap: %q, -check %v", said, err)
	}
	if got, _ := os.ReadFile(named); string(got) != settings {
		t.Errorf("a file that is no keymap was written:\n%s", got)
	}
}

// TestApplySettingsFillsWhatNoFlagNamed: vif.toml's [paths] gives each of the root,
// scenario, content and keymap no flag named its value, scenario and content not
// under -d; a root or keymap that a vif.toml not the player's own names is foreign.
// Its downloads directory, or the default one, and switch are taken.
func TestApplySettingsFillsWhatNoFlagNamed(t *testing.T) {
	var s paths.Settings
	s.Paths.Root, s.Paths.Scenario, s.Paths.Content, s.Paths.Keymap, s.Paths.Downloads = "/r", "td", "/c", "/k.toml", "/dl"
	s.Session.SaveDownloads = true
	named := Options{Dir: "/r", Scenario: "td", Content: "/c", Keymap: "/k.toml", Downloads: "/dl", SaveDownloads: true}
	foreign := named
	foreign.ForeignDir, foreign.ForeignKeymap = true, true
	for _, c := range []struct {
		name        string
		flags, want Options
		own         bool
	}{
		{"own", Options{}, named, true},
		{"foreign", Options{}, foreign, false},
		{"flags", Options{Dir: "/f", Keymap: "/f.toml", Scenario: "s"},
			Options{Dir: "/f", Keymap: "/f.toml", Scenario: "s", Content: "/c", Downloads: "/dl", SaveDownloads: true}, false},
		{"embedded", Options{Embedded: true},
			Options{Embedded: true, Dir: "/r", Keymap: "/k.toml", Downloads: "/dl", SaveDownloads: true}, true},
	} {
		o := c.flags
		if o.ApplySettings(s, c.own); o != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, o, c.want)
		}
	}
	var o Options
	if o.ApplySettings(paths.Settings{}, true); o != (Options{Downloads: paths.DefaultDownloadsDir()}) {
		t.Errorf("an empty [paths]: %+v", o)
	}
}

// TestCheckSaysWhatARunDoesWithAKeymap: -check names what bringing a keymap up to
// date changes and whether a run writes it back, reads it so as another's, or reads
// it so as its file may not be written, leaving it as it is.
func TestCheckSaysWhatARunDoesWithAKeymap(t *testing.T) {
	doc := "[normal]\na = \"unknown_action\"\n"
	dir := t.TempDir()
	path := filepath.Join(dir, paths.InputDirName, paths.KeymapConfigFile)
	writeKeymap(t, path, doc)
	for _, c := range []struct {
		foreign bool
		mode    os.FileMode
		want    string
	}{
		{false, 0o644, "a run writes it back up to date"},
		{true, 0o644, "a run reads it up to date"},
		{false, 0o444, "a run reads it up to date, as " + path + " is read-only"},
	} {
		if err := os.Chmod(path, c.mode); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := checkKeymap(Options{Dir: dir, ForeignDir: c.foreign}, &out); err != nil {
			t.Fatal(err)
		}
		if line := "keymap " + path + ": " + c.want + `: reset to the default: [normal] key "a"`; !strings.HasPrefix(out.String(), line) {
			t.Errorf("-check said %q, want %q", out.String(), line)
		}
		if got, _ := os.ReadFile(path); string(got) != doc {
			t.Errorf("-check wrote:\n%s", got)
		}
	}
}

func writeKeymap(t *testing.T, path, doc string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCategorizedRootResolvesEveryResource covers the operator-root path end to
// end, and the strictness the explicit overrides apply.
func TestCategorizedRootResolvesEveryResource(t *testing.T) {
	root := t.TempDir()
	scenario := filepath.Join(root, paths.ScenarioDirName, paths.MainScenarioName, paths.ScenarioFile)
	keymap := filepath.Join(root, paths.InputDirName, paths.KeymapConfigFile)
	music := filepath.Join(root, paths.AudioDirName, paths.MusicConfigFile)
	sounds := filepath.Join(root, paths.AudioDirName, paths.SoundConfigFile)
	content := filepath.Join(root, paths.ContentDirName)
	graph := filepath.Join(root, paths.BotDirName, "roam.toml")
	for _, path := range []string{scenario, keymap, music, sounds, graph} {
		writeFixture(t, path)
	}
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}

	o := Options{Dir: root}
	if got, err := ScenarioPath(o); err != nil || got != scenario {
		t.Fatalf("scenario = %q, %v; want %q", got, err, scenario)
	}
	if got, err := Keymap(o); err != nil || got != keymap {
		t.Fatalf("keymap = %q, %v; want %q", got, err, keymap)
	}
	if got, err := Corpus(o, Scenario{}); err != nil || got.Dir != content {
		t.Fatalf("content = %+v, %v; want %q", got, err, content)
	}
	if got, err := Audio(o); err != nil || got.MusicPath != music || got.SoundPath != sounds {
		t.Fatalf("audio = %+v, %v; want %q and %q", got, err, music, sounds)
	}
	if roots := Files(o).Roots; len(roots) == 0 || roots[0] != root {
		t.Fatalf("file roots = %v, want operator root %q first", roots, root)
	}
	if data, name, err := BotGraph(o, "roam"); err != nil || string(data) != "fixture" || name != "roam" {
		t.Fatalf("bot graph = %q named %q, %v; want the root's fixture", data, name, err)
	}
	if data, _, err := BotGraph(o, "patrol"); err != nil || len(data) == 0 || string(data) == "fixture" {
		t.Fatalf("a graph the root lacks = %q, %v; want the embedded one", data, err)
	}
	if _, _, err := BotGraph(o, "missing"); err == nil {
		t.Fatal("a bot graph no root or binary holds was resolved")
	}
}

// TestCheckRefusesABrokenBotGraph: -check parses every graph a name could resolve
// to, so an installed one that would fail only when seated fails the check, whatever
// characters its root's path holds.
func TestCheckRefusesABrokenBotGraph(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	root := filepath.Join(t.TempDir(), "games[old]")
	bad := filepath.Join(root, paths.BotDirName, "bad.toml")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("[nope]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Check(Options{Dir: root}, &out); err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("check = %v, want a fault naming %s", err, bad)
	}
	if !strings.Contains(out.String(), "bot ok: embedded default") {
		t.Fatalf("the shipped graph was not reported ok beside the broken one:\n%s", out.String())
	}
}

// TestAScenarioNameResolvesAtAnyDepthAndAPathExactly: a bare name finds the
// shallowest directory of that name below scenario/, the first in path order among
// equals, never one inside another scenario or a hidden group; a path below
// scenario/ names its scenario whatever shadows its name.
func TestAScenarioNameResolvesAtAnyDepthAndAPathExactly(t *testing.T) {
	root := t.TempDir()
	entry := func(rel string) string {
		p := filepath.Join(root, paths.ScenarioDirName, filepath.FromSlash(rel), paths.ScenarioFile)
		writeFixture(t, p)
		return p
	}
	td, puzzle := entry("base/td"), entry("a/puzzle/td")
	entry("zz/td")
	entry("base/vanguard")
	entry("base/vanguard/behavior/inner")
	entry(".trash/old")

	for name, want := range map[string]string{"td": td, "a/puzzle/td": puzzle} {
		if got, err := ScenarioPath(Options{Dir: root, Scenario: name}); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"missing", "inner", "old", ".trash/old", "base/../zz/td"} {
		if got, err := ScenarioPath(Options{Dir: root, Scenario: name}); err == nil {
			t.Errorf("%s resolved to %q", name, got)
		}
	}
}

// TestOptionsRejectUnusableOverrides covers every refusal Validate and the
// explicit-file path make.
func TestOptionsRejectUnusableOverrides(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	writeFixture(t, file)

	if _, err := Keymap(Options{Keymap: base}); err == nil {
		t.Fatal("directory accepted as an explicit config file")
	}
	if _, err := Keymap(Options{Keymap: filepath.Join(base, "missing.toml")}); err == nil {
		t.Fatal("missing explicit config file accepted")
	}
	for _, tc := range []struct {
		name string
		o    Options
	}{
		{"missing root", Options{Dir: filepath.Join(base, "missing")}},
		{"root is a file", Options{Dir: file}},
		{"embedded with an override", Options{Embedded: true, Scenario: file}},
	} {
		if err := tc.o.Validate(); err == nil {
			t.Errorf("%s accepted", tc.name)
		}
	}
	if err := (Options{Dir: base}).Validate(); err != nil {
		t.Fatalf("valid config root rejected: %v", err)
	}
}

// TestScenariosListsMainThenEveryRootsScenario: main leads whether or not a root
// holds it, then every scenario of every group, sorted, each by the name -s reaches
// it by: its own, or its path where that name reaches another. A directory without
// an entry file, one a higher root holds at the same path, or -d lists nothing.
func TestScenariosListsMainThenEveryRootsScenario(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(paths.SystemRootsEnv, "")
	dir := t.TempDir()
	for _, rel := range []string{"base/td", "base/blank", paths.MainScenarioName, "test/td"} {
		writeFixture(t, filepath.Join(dir, paths.ScenarioDirName, filepath.FromSlash(rel), paths.ScenarioFile))
	}
	writeFixture(t, filepath.Join(user, paths.AppDirName, paths.ScenarioDirName, "base", "td", paths.ScenarioFile))
	if err := os.MkdirAll(filepath.Join(dir, paths.ScenarioDirName, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Scenarios(Options{Dir: dir})
	if want := []string{paths.MainScenarioName, "blank", "td", "test/td"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("scenarios = %v, want %v", got, want)
	}
	if got := Scenarios(Options{Dir: dir, Embedded: true}); len(got) != 1 || got[0] != paths.MainScenarioName {
		t.Fatalf("-d lists %v, want main alone", got)
	}
}
