package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigRootsPutOverrideAndUserBeforeSystem(t *testing.T) {
	if !isUnixLike() || runtime.GOOS == "android" {
		t.Skip("system roots are Unix-specific")
	}

	base := t.TempDir()
	override := filepath.Join(base, "override")
	user := filepath.Join(base, "user")
	systemA := filepath.Join(base, "system-a")
	systemB := filepath.Join(base, "system-b")
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(SystemRootsEnv, systemA+string(filepath.ListSeparator)+systemB)

	got := ConfigRoots(override)
	want := []string{override, filepath.Join(user, AppDirName), systemA, systemB}
	if len(got) != len(want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("root[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStateDirectoriesAreCategorized(t *testing.T) {
	if !isUnixLike() || runtime.GOOS == "android" {
		t.Skip("XDG state layout is Unix-specific")
	}

	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	if got, want := DefaultLogDir(), filepath.Join(base, AppDirName, LogDirName); got != want {
		t.Errorf("log dir = %q, want %q", got, want)
	}
	if got, want := DefaultJournalDir(), filepath.Join(base, AppDirName, JournalDirName); got != want {
		t.Errorf("journal dir = %q, want %q", got, want)
	}
}

// TestSettingsFileSetsOnlyWhatItNames: the first root's vif.toml wins, a key it
// omits keeps the embedded default, and paths resolve against the file.
func TestSettingsFileSetsOnlyWhatItNames(t *testing.T) {
	if !isUnixLike() || runtime.GOOS == "android" {
		t.Skip("system roots are Unix-specific")
	}
	base, home := t.TempDir(), t.TempDir()
	user, system := filepath.Join(base, "user"), filepath.Join(base, "system")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(SystemRootsEnv, filepath.Join(system, AppDirName))
	write := func(root, body string) {
		dir := filepath.Join(root, AppDirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(system, "[audio]\nbuffer_ms = 100\n")
	write(user, "[paths]\nlog = \"logs\"\nscenario = \"td\"\nkeymap = \"~/k.toml\"\n")

	s, path, err := LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	userRoot := filepath.Join(user, AppDirName)
	for _, c := range []struct{ name, got, want string }{
		{"file", path, filepath.Join(userRoot, SettingsFile)},
		{"log", s.Paths.Log, filepath.Join(userRoot, "logs")},
		{"scenario", s.Paths.Scenario, "td"},
		{"keymap", s.Paths.Keymap, filepath.Join(home, "k.toml")},
		{"journal", s.Paths.Journal, ""},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if s.Audio.BufferMs != 50 {
		t.Errorf("buffer_ms = %d, want the embedded 50: the system file is shadowed", s.Audio.BufferMs)
	}
}

func TestSettingsRefuseAnUnknownKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(SystemRootsEnv, t.TempDir())
	if err := os.WriteFile(filepath.Join(root, SettingsFile), []byte("[paths]\nlogs = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSettings(root); err == nil || !strings.Contains(err.Error(), "paths.logs") {
		t.Fatalf("error = %v, want one naming paths.logs", err)
	}
}

// TestSaveSettingsWritesThePlayTableAlone: a file not there yet starts as the
// embedded one; a file holding [play] has that table replaced, comments under it
// included, and every other line kept; one without it has it appended. What was
// written reads back.
func TestSaveSettingsWritesThePlayTableAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(SystemRootsEnv, "")
	dir := t.TempDir()
	file := filepath.Join(dir, SettingsFile)
	play := Play{Effects: true, Volume: 0.35, AutoFire: "main", Mouse: true, HUD: true}
	if err := SaveSettings(file, play); err != nil {
		t.Fatal(err)
	}
	s, path, err := LoadSettings(dir)
	if err != nil || path != file || s.Play != play || s.Audio.BufferMs != 50 {
		t.Fatalf("saved anew: %+v from %q, %v", s.Play, path, err)
	}

	text := "# mine\n[paths]\nlog = \"logs\"\n\n[play]\n# old\neffects = false\n\n[audio]\nbuffer_ms = 20\n"
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	play.Music = true
	if err := SaveSettings(file, play); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(got), "# mine\n[paths]\nlog = \"logs\"\n\n[play]\n") ||
		!strings.HasSuffix(string(got), "\n\n[audio]\nbuffer_ms = 20\n") || strings.Contains(string(got), "# old") {
		t.Fatalf("replaced table:\n%s", got)
	}
	if s, _, err = LoadSettings(dir); err != nil || s.Play != play || s.Audio.BufferMs != 20 || filepath.Base(s.Paths.Log) != "logs" {
		t.Fatalf("after the replace: %+v %+v, %v", s.Play, s.Paths, err)
	}

	if err := os.WriteFile(file, []byte("[audio]\nbuffer_ms = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(file, play); err != nil {
		t.Fatal(err)
	}
	if s, _, err = LoadSettings(dir); err != nil || s.Play != play || s.Audio.BufferMs != 30 {
		t.Fatalf("after the append: %+v, %v", s, err)
	}
}
