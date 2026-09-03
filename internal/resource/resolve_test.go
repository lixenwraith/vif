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

// TestResolutionPrecedence pins the whole ordering rule in one fixture: root
// ownership outranks layout generation, the categorized layout supersedes the flat
// one within a root, and the working directory is consulted last.
func TestResolutionPrecedence(t *testing.T) {
	base := t.TempDir()
	user, system, local := filepath.Join(base, "user"), filepath.Join(base, "system"), filepath.Join(base, "work")

	userFlat := filepath.Join(user, paths.GameConfigFile)
	writeFixture(t, userFlat)
	writeFixture(t, filepath.Join(system, paths.GameDirName, paths.GameConfigFile))
	writeFixture(t, filepath.Join(local, paths.GameConfigFile))

	r := resolver{roots: []string{user, system}, localRoot: local, external: true}
	if got := r.file(paths.GameDirName, paths.GameConfigFile, paths.GameConfigFile); got != userFlat {
		t.Fatalf("game path = %q, want the user root's flat file %q", got, userFlat)
	}

	userCategorized := filepath.Join(user, paths.GameDirName, paths.GameConfigFile)
	writeFixture(t, userCategorized)
	if got := r.file(paths.GameDirName, paths.GameConfigFile, paths.GameConfigFile); got != userCategorized {
		t.Fatalf("game path = %q, want the categorized file %q", got, userCategorized)
	}

	// Directories follow the same rule, with the deprecated working-directory name
	// as the last candidate.
	legacy := filepath.Join(local, paths.LegacyLocalContentDir)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.dir(paths.ContentDirName, paths.LegacyLocalContentDir); got != legacy {
		t.Fatalf("content dir = %q, want the legacy fallback %q", got, legacy)
	}
	preferred := filepath.Join(user, paths.ContentDirName)
	if err := os.MkdirAll(preferred, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.dir(paths.ContentDirName, paths.LegacyLocalContentDir); got != preferred {
		t.Fatalf("content dir = %q, want the user root %q", got, preferred)
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
