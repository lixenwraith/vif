package paths

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
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

// TestSettingsNameEveryUnknownKey: the game refuses a file naming keys it does not
// know, and the error names each one while the keys it knows read in full, so
// soundlab can go on with an older or newer vif.toml.
func TestSettingsNameEveryUnknownKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(SystemRootsEnv, t.TempDir())
	doc := "[paths]\nlogs = \"x\"\n[audio]\nbuffer_ms = 20\n[later]\nkey = 1\n"
	if err := os.WriteFile(filepath.Join(root, SettingsFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, path, err := LoadSettings(root)
	var unknown UnknownKeys
	if !errors.As(err, &unknown) || !slices.Equal(unknown, UnknownKeys{"later", "paths.logs"}) {
		t.Fatalf("error = %v, want UnknownKeys naming later and paths.logs", err)
	}
	if s.Audio.BufferMs != 20 || path != filepath.Join(root, SettingsFile) {
		t.Errorf("read %q with buffer_ms %d, want the file's 20 read alongside", path, s.Audio.BufferMs)
	}
}

// TestSavePlayWritesTheChangedKeysAlone: a file not there yet starts as the
// embedded one; in a file holding [play] the line of each key that changed is
// replaced and a key it lacks goes after its last, every other line kept, comments
// included; a file without the table has it appended. What was written reads back.
func TestSavePlayWritesTheChangedKeysAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(SystemRootsEnv, "")
	dir := t.TempDir()
	file := filepath.Join(dir, SettingsFile)
	store := FileStore{Path: file}
	was := DefaultPlay()
	now := was
	now.Sfx, now.SfxVolume, now.AutoFire, now.HUD = false, 0.35, "main", true
	if err := SavePlay(store, was, now); err != nil {
		t.Fatal(err)
	}
	s, path, err := LoadSettings(dir)
	if err != nil || path != file || s.Play != now || s.Audio.BufferMs != 50 {
		t.Fatalf("saved anew: %+v from %q, %v", s.Play, path, err)
	}
	if got, _ := os.ReadFile(file); !strings.HasPrefix(string(got), "# vif.toml") {
		t.Fatalf("a new file did not start as the embedded one:\n%s", got)
	}

	text := "# mine\n[paths]\nlog = \"logs\"\n\n[play]\n# kept\nsfx = false\nhud = false # old\n\n# about audio\n[audio]\nbuffer_ms = 20"
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	was = now
	now.HUD, now.Mouse, now.SfxVolume = false, false, 0.25
	if err := SavePlay(store, was, now); err != nil {
		t.Fatal(err)
	}
	want := "# mine\n[paths]\nlog = \"logs\"\n\n[play]\n# kept\nsfx = false\nhud = false\n" +
		"mouse = false\nsfx_volume = 0.25\n\n# about audio\n[audio]\nbuffer_ms = 20\n"
	if got, _ := os.ReadFile(file); string(got) != want {
		t.Fatalf("written keys:\n%s\nwant:\n%s", got, want)
	}
	if s, _, err = LoadSettings(dir); err != nil || s.Play.Mouse || s.Play.SfxVolume != 0.25 || s.Audio.BufferMs != 20 || filepath.Base(s.Paths.Log) != "logs" {
		t.Fatalf("after the write: %+v %+v, %v", s.Play, s.Paths, err)
	}
	if err := SavePlay(store, now, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != want {
		t.Fatalf("nothing changed, yet the file did:\n%s", got)
	}

	if err := os.WriteFile(file, []byte("[audio]\nbuffer_ms = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SavePlay(store, was, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "[audio]\nbuffer_ms = 30\n\n[play]\nhud = false\nmouse = false\nsfx_volume = 0.25\n" {
		t.Fatalf("appended table:\n%s", got)
	}
}

// TestSettingsForWritesThePlayersOwnFile: the file read is written when it is in
// the override or the user root; one read further down, or none, makes the user
// root's, which starts as the file read with its relative paths made absolute and
// shadows it from then on. Without a user root nothing is written.
func TestSettingsForWritesThePlayersOwnFile(t *testing.T) {
	if !isUnixLike() || runtime.GOOS == "android" {
		t.Skip("system roots are Unix-specific")
	}
	base, home := t.TempDir(), t.TempDir()
	user, system, override := filepath.Join(base, "user"), filepath.Join(base, "system"), filepath.Join(base, "override")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(SystemRootsEnv, system)
	mine := filepath.Join(user, AppDirName, SettingsFile)
	site := filepath.Join(system, SettingsFile)
	for _, c := range []struct {
		name, override, read string
		want                 FileStore
	}{
		{"user root", "", mine, FileStore{Path: mine}},
		{"override", override, filepath.Join(override, SettingsFile), FileStore{Path: filepath.Join(override, SettingsFile)}},
		{"system root", override, site, FileStore{Path: mine, Seed: site}},
		{"embedded", override, "", FileStore{Path: mine}},
	} {
		if got := SettingsFor(c.override, c.read); got != SettingsStore(c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}

	if err := os.MkdirAll(system, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "[paths]\ncontent = \"words\" # site\nscenario = \"td\"\nkeymap = \"~/k.toml\"\n\n[audio]\nbuffer_ms = 100\n"
	if err := os.WriteFile(site, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	s, read, err := LoadSettings("")
	if err != nil || read != site {
		t.Fatalf("read %q, %v", read, err)
	}
	now := s.Play
	now.HUD = true
	if err := SavePlay(SettingsFor("", read), s.Play, now); err != nil {
		t.Fatal(err)
	}
	s, read, err = LoadSettings("")
	if err != nil || read != mine || !s.Play.HUD || s.Audio.BufferMs != 100 ||
		s.Paths.Content != filepath.Join(system, "words") || s.Paths.Scenario != "td" || s.Paths.Keymap != filepath.Join(home, "k.toml") {
		t.Fatalf("the user root's copy: %+v from %q, %v", s, read, err)
	}
	if got, _ := os.ReadFile(site); string(got) != text {
		t.Fatalf("the system root's file was written:\n%s", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if got := SettingsFor("", site); got != nil {
		t.Fatalf("no user root, yet a store: %+v", got)
	}
}

// memStore is a settings document held in memory, as a page holds one
type memStore struct{ doc []byte }

func (m *memStore) Load() ([]byte, error) { return m.doc, nil }
func (m *memStore) Store(doc []byte) error {
	m.doc = doc
	return nil
}

// FuzzSavePlayKeepsTheDocument: whatever valid settings document a store holds, a
// write either leaves it valid, reading back the values written and every other
// setting as it was, or is refused and leaves it alone.
func FuzzSavePlayKeepsTheDocument(f *testing.F) {
	f.Add(string(asset.DefaultSettings), true, 0.25)
	f.Add("", false, 1.0)
	f.Add("[play]\nhud = true\n", false, 0.0)
	f.Add("[audio]\nbuffer_ms = 20\n[ \"play\" ] # mine\n'sfx'=true", true, 0.5)
	f.Add("play.hud = true\n", false, 0.5)
	f.Fuzz(func(t *testing.T, doc string, hud bool, volume float64) {
		before, err := ParseSettings([]byte(doc))
		if err != nil || math.IsNaN(volume) || volume < 0 || volume > 1 {
			t.Skip()
		}
		now := before.Play
		now.HUD, now.SfxVolume = hud, volume
		store := &memStore{doc: []byte(doc)}
		if err := SavePlay(store, before.Play, now); err != nil {
			if string(store.doc) != doc {
				t.Fatalf("a refused write changed the document: %v", err)
			}
			return
		}
		after, err := ParseSettings(store.doc)
		if err != nil {
			t.Fatalf("the written document does not read: %v\n%s", err, store.doc)
		}
		before.Play = now
		if after != before {
			t.Fatalf("read back %+v, want %+v\n%s", after, before, store.doc)
		}
	})
}
