package resource

import (
	"os"
	"path/filepath"
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
	if got, err := Corpus(o); err != nil || got.Dir != content {
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

func TestGameNameResolvesInsideConfigurationRoots(t *testing.T) {
	root := t.TempDir()
	td := filepath.Join(root, paths.ScenarioDirName, "td", paths.ScenarioFile)
	writeFixture(t, td)

	got, err := ScenarioPath(Options{Dir: root, Scenario: "td"})
	if err != nil || got != td {
		t.Fatalf("named scenario = %q, %v; want %q", got, err, td)
	}
	if _, err := ScenarioPath(Options{Dir: root, Scenario: "missing"}); err == nil {
		t.Fatal("missing named scenario accepted")
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
// holds it, the roots' scenario directories follow sorted and named once, and a
// directory without an entry file or -d lists nothing beyond main.
func TestScenariosListsMainThenEveryRootsScenario(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(paths.SystemRootsEnv, "")
	dir := t.TempDir()
	for _, name := range []string{"td", "blank", paths.MainScenarioName} {
		writeFixture(t, filepath.Join(dir, paths.ScenarioDirName, name, paths.ScenarioFile))
	}
	if err := os.MkdirAll(filepath.Join(dir, paths.ScenarioDirName, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Scenarios(Options{Dir: dir})
	if want := []string{paths.MainScenarioName, "blank", "td"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("scenarios = %v, want %v", got, want)
	}
	if got := Scenarios(Options{Dir: dir, Embedded: true}); len(got) != 1 || got[0] != paths.MainScenarioName {
		t.Fatalf("-d lists %v, want main alone", got)
	}
}
