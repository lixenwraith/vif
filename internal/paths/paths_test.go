package paths

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/parameter"
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
	write(user, "[paths]\nlog = \"logs\"\nscenario = \"base/td\"\nkeymap = \"~/k.toml\"\n")

	s, path, _, err := LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	userRoot := filepath.Join(user, AppDirName)
	for _, c := range []struct{ name, got, want string }{
		{"file", path, filepath.Join(userRoot, SettingsFile)},
		{"log", s.Paths.Log, filepath.Join(userRoot, "logs")},
		{"scenario", s.Paths.Scenario, "base/td"},
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

// TestSettingsReadAsThisBuildBringsThemUpToDate: a file naming keys this build
// does not know, or a value it refuses, reads every other key it holds, the refused
// one at its default, and names each change bringing it up to date makes, so a run
// can go on with an older or newer vif.toml; the file is left as it is.
func TestSettingsReadAsThisBuildBringsThemUpToDate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(SystemRootsEnv, t.TempDir())
	doc := "[paths]\nlogs = \"x\"\n[audio]\nbuffer_ms = 20\n[play]\nauto_fire = true\nmouse = false\n[later]\nkey = 1\n"
	file := filepath.Join(root, SettingsFile)
	if err := os.WriteFile(file, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, path, notes, err := LoadSettings(root)
	if err != nil || path != file || s.Audio.BufferMs != 20 || s.Play.Mouse || s.Play.AutoFire != DefaultPlay().AutoFire {
		t.Fatalf("read %+v from %q, %v", s, path, err)
	}
	if len(notes) != 3 || notes[0] != `dropped unknown keys "later", "paths.logs"` || !strings.Contains(notes[1], "play.auto_fire") {
		t.Errorf("notes %q", notes)
	}
	if got, _ := os.ReadFile(file); string(got) != doc {
		t.Errorf("reading the file wrote it:\n%s", got)
	}
}

// TestSettingsTakePlayNumbersInRange: [play] max_fps is 0 for the presentation's
// own rate or a rate between parameter.FrameRateMin and FrameRateMax, and
// camera_edge a percent from 0 to parameter.CameraEdgeMost; any other fails.
func TestSettingsTakePlayNumbersInRange(t *testing.T) {
	for _, c := range []struct {
		key  string
		read func(Play) int
		ok   map[int]bool
	}{
		{"max_fps", func(p Play) int { return p.MaxFPS }, map[int]bool{
			0: true, parameter.FrameRateMin: true, 24: true, parameter.FrameRateMax: true,
			parameter.FrameRateMin - 1: false, parameter.FrameRateMax + 1: false, -24: false,
		}},
		{"camera_edge", func(p Play) int { return p.CameraEdge }, map[int]bool{
			0: true, 15: true, parameter.CameraEdgeMost: true, -1: false, parameter.CameraEdgeMost + 1: false,
		}},
	} {
		for v, ok := range c.ok {
			s, err := ParseSettings(fmt.Appendf(nil, "[play]\n%s = %d\n", c.key, v))
			if (err == nil) != ok || ok && c.read(s.Play) != v {
				t.Errorf("%s = %d: read %d, error %v", c.key, v, c.read(s.Play), err)
			}
		}
	}
}

// TestARunWritesBackOnlyThePlayersOwnSettings: the vif.toml a run read is written
// back brought up to date when it is the player's own, and is up to date then; a
// system root's is left as it is with no copy made, and the copy its first change
// makes in the user root starts up to date.
func TestARunWritesBackOnlyThePlayersOwnSettings(t *testing.T) {
	doc := "# mine\n[play]\nmouse = false # mine\n# the old name\neffects = false\n"
	want, _, err := UpgradeSettings([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	user, system := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv(SystemRootsEnv, system)
	mine := filepath.Join(user, AppDirName, SettingsFile)
	for _, root := range []string{filepath.Dir(mine), system} {
		os.RemoveAll(filepath.Dir(mine))
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, SettingsFile), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		_, read, notes, err := LoadSettings("")
		if err != nil || len(notes) == 0 {
			t.Fatalf("%s read with %q, %v", read, notes, err)
		}
		said := UpgradeStore(SettingsFor("", read))
		got, _ := os.ReadFile(read)
		if root == system {
			if _, err := os.Stat(mine); said != "" || string(got) != doc || err == nil {
				t.Fatalf("a system root's file: %q, wrote %v:\n%s", said, err == nil, got)
			}
			continue
		}
		if !strings.HasPrefix(said, `Settings brought up to date: dropped unknown key "play.effects"`) || string(got) != string(want) {
			t.Fatalf("the player's own: %q:\n%s", said, got)
		}
		if again := UpgradeStore(SettingsFor("", read)); again != "" {
			t.Fatalf("an up-to-date file upgraded again: %q", again)
		}
	}
	s, read, _, err := LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	now := s.Play
	now.HUD = !now.HUD
	if err := SavePlay(SettingsFor("", read), s.Play, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mine); strings.Contains(string(got), "effects") || UpgradeStore(FileStore{Path: mine}) != "" {
		t.Fatalf("the copy the first change made is not up to date:\n%s", got)
	}
}

// TestUpgradingSettingsKeepsThePlayersLines: an older file loses the keys this
// build does not take, tables they head included, and the values it refuses, each
// with its own comments, and gains what the embedded document sets that it lacks,
// comments included; every other line stays. The result, like the embedded
// document, upgrades to itself.
func TestUpgradingSettingsKeepsThePlayersLines(t *testing.T) {
	old := "# my settings\n\n[paths]\nlog = \"~/logs\" # mine\n\n[play]\nsfx = false\n# the old name\neffects = false\nauto_fire = true\nmouse = false\n" +
		"\n[later] # a newer build's\n# about later\nkey = 1\n[later.sub]\ndeep = 2\n"
	out, notes, err := UpgradeSettings([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	gone := []string{"# the old name", "effects = false", "auto_fire = true", "[later] # a newer build's", "# about later", "key = 1", "[later.sub]", "deep = 2"}
	for _, line := range strings.Split(strings.TrimSpace(old), "\n") {
		if kept := slices.Contains(strings.Split(string(out), "\n"), line); kept == slices.Contains(gone, line) {
			t.Errorf("line %q kept %v", line, kept)
		}
	}
	if _, session, _ := strings.Cut(string(asset.DefaultSettings), "\n\n# Whether a run keeps"); !strings.Contains(string(out), "# Whether a run keeps"+session) {
		t.Errorf("[session] came without its comments:\n%s", out)
	}
	if len(notes) != 3 || !strings.HasPrefix(notes[0], `dropped unknown keys "later", "play.effects"`) ||
		!strings.Contains(notes[1], "play.auto_fire") || !strings.HasPrefix(notes[2], "added ") || !strings.HasSuffix(notes[2], " keys") {
		t.Errorf("notes %q", notes)
	}
	s, err := ParseSettings(out)
	if err != nil || s.Play.Sfx || s.Play.Mouse || s.Play.AutoFire != "on" || s.Paths.Log != "~/logs" {
		t.Errorf("reads %+v, %v", s, err)
	}
	for _, doc := range [][]byte{out, asset.DefaultSettings} {
		if again, notes, err := UpgradeSettings(doc); err != nil || len(notes) > 0 || string(again) != string(doc) {
			t.Errorf("an up-to-date document changed, %q, %v:\n%s", notes, err, again)
		}
	}
}

// FuzzUpgradingSettingsReadsAsBefore: whatever settings document parses, its
// upgrade reads whole and is up to date, and one whose values this build takes
// reads as it did.
func FuzzUpgradingSettingsReadsAsBefore(f *testing.F) {
	f.Add(string(asset.DefaultSettings))
	f.Add("# mine\n[play]\nmouse = false # mine\neffects = 1\n\n[later.sub]\nx = 1\n")
	f.Add("play.hud = true\n[audio]\nbuffer_ms = \"x\"\n")
	f.Add("[[play]]\nsfx = true\n[presentation]\ncell = { a = 1 }\n")
	f.Fuzz(func(t *testing.T, doc string) {
		out, _, err := UpgradeSettings([]byte(doc))
		if err != nil {
			return
		}
		after, err := ParseSettings(out)
		if err != nil {
			t.Fatalf("the upgrade does not read: %v\n%s", err, out)
		}
		if again, notes, err := UpgradeSettings(out); err != nil || len(notes) > 0 || string(again) != string(out) {
			t.Fatalf("the upgrade is not up to date, %q, %v:\n%s", notes, err, again)
		}
		if before, err := ParseSettings([]byte(doc)); (err == nil || errors.As(err, new(UnknownKeys))) && before != after {
			t.Fatalf("read %+v after the upgrade, %+v before:\n%s", after, before, out)
		}
	})
}

// TestAStoreKeepsTheFileThePlayerKeeps: a store writes through a link to the file
// it names, which keeps its mode, refuses a file no one may write, and leaves no
// temporary file behind.
func TestAStoreKeepsTheFileThePlayerKeeps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a link needs a privilege there")
	}
	dir := t.TempDir()
	target, link := filepath.Join(dir, "dotfiles", SettingsFile), filepath.Join(dir, "root", SettingsFile)
	for _, d := range []string{filepath.Dir(target), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := (FileStore{Path: link}).Store([]byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); info.Mode()&os.ModeSymlink == 0 || string(got) != "new" || st.Mode().Perm() != 0o600 {
		t.Errorf("link %v, target %q under %v", info.Mode(), got, st.Mode())
	}
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := (FileStore{Path: link}).Store([]byte("newer")); err == nil {
		t.Error("a read-only file was written")
	}
	for _, d := range []string{filepath.Dir(target), filepath.Dir(link)} {
		if left, _ := filepath.Glob(filepath.Join(d, "*.tmp")); len(left) > 0 {
			t.Errorf("left %v", left)
		}
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
	s, path, _, err := LoadSettings(dir)
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
	want := "# mine\n[paths]\nlog = \"logs\"\n\n[play]\n# kept\nsfx = false\nhud = false # old\n" +
		"mouse = false\nsfx_volume = 0.25\n\n# about audio\n[audio]\nbuffer_ms = 20"
	if got, _ := os.ReadFile(file); string(got) != want {
		t.Fatalf("written keys:\n%s\nwant:\n%s", got, want)
	}
	if s, _, _, err = LoadSettings(dir); err != nil || s.Play.Mouse || s.Play.SfxVolume != 0.25 || s.Audio.BufferMs != 20 || filepath.Base(s.Paths.Log) != "logs" {
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
	s, read, _, err := LoadSettings("")
	if err != nil || read != site {
		t.Fatalf("read %q, %v", read, err)
	}
	now := s.Play
	now.HUD = true
	if err := SavePlay(SettingsFor("", read), s.Play, now); err != nil {
		t.Fatal(err)
	}
	s, read, _, err = LoadSettings("")
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
