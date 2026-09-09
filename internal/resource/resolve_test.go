package resource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/paths"
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

	systemGame := filepath.Join(system, paths.GameDirName, paths.GameConfigFile)
	writeFixture(t, systemGame)
	r := resolver{roots: []string{user, system}}
	if got := r.file(paths.GameDirName, paths.GameConfigFile); got != systemGame {
		t.Fatalf("game path = %q, want the system root's file %q", got, systemGame)
	}

	userGame := filepath.Join(user, paths.GameDirName, paths.GameConfigFile)
	writeFixture(t, userGame)
	if got := r.file(paths.GameDirName, paths.GameConfigFile); got != userGame {
		t.Fatalf("game path = %q, want the user root's file %q", got, userGame)
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
	game := filepath.Join(root, paths.GameDirName, paths.GameConfigFile)
	keymap := filepath.Join(root, paths.InputDirName, paths.KeymapConfigFile)
	music := filepath.Join(root, paths.AudioDirName, paths.MusicConfigFile)
	sounds := filepath.Join(root, paths.AudioDirName, paths.SoundConfigFile)
	content := filepath.Join(root, paths.ContentDirName)
	for _, path := range []string{game, keymap, music, sounds} {
		writeFixture(t, path)
	}
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}

	o := Options{Dir: root}
	if got, err := GameConfig(o); err != nil || got != game {
		t.Fatalf("game = %q, %v; want %q", got, err, game)
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
}

// TestOptionsRejectUnusableOverrides covers every refusal Validate and the
// explicit-file path make.
func TestOptionsRejectUnusableOverrides(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	writeFixture(t, file)

	if _, err := explicitFile(base); err == nil {
		t.Fatal("directory accepted as an explicit config file")
	}
	if _, err := explicitFile(filepath.Join(base, "missing.toml")); err == nil {
		t.Fatal("missing explicit config file accepted")
	}
	for _, tc := range []struct {
		name string
		o    Options
	}{
		{"missing root", Options{Dir: filepath.Join(base, "missing")}},
		{"root is a file", Options{Dir: file}},
		{"embedded with an override", Options{Embedded: true, Game: file}},
	} {
		if err := tc.o.Validate(); err == nil {
			t.Errorf("%s accepted", tc.name)
		}
	}
	if err := (Options{Dir: base}).Validate(); err != nil {
		t.Fatalf("valid config root rejected: %v", err)
	}
}
