package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/paths"
)

func writePathFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPathResolverPrefersRootsBeforeWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	user := filepath.Join(base, "user")
	system := filepath.Join(base, "system")
	local := filepath.Join(base, "work")

	// A legacy flat user file must still beat a categorized system file; root
	// ownership has higher priority than layout generation.
	userGame := filepath.Join(user, paths.GameConfigFile)
	systemGame := filepath.Join(system, paths.GameDirName, paths.GameConfigFile)
	localGame := filepath.Join(local, paths.GameConfigFile)
	writePathFixture(t, userGame)
	writePathFixture(t, systemGame)
	writePathFixture(t, localGame)

	r := pathResolver{roots: []string{user, system}, localRoot: local, external: true}
	if got := r.file(paths.GameDirName, paths.GameConfigFile, paths.GameConfigFile); got != userGame {
		t.Fatalf("game path = %q, want user path %q", got, userGame)
	}

	// Within one root the categorized layout supersedes its flat predecessor.
	userCategorized := filepath.Join(user, paths.GameDirName, paths.GameConfigFile)
	writePathFixture(t, userCategorized)
	if got := r.file(paths.GameDirName, paths.GameConfigFile, paths.GameConfigFile); got != userCategorized {
		t.Fatalf("game path = %q, want categorized path %q", got, userCategorized)
	}
}

func TestPathResolverContentLayoutAndLegacyFallback(t *testing.T) {
	base := t.TempDir()
	user := filepath.Join(base, "user")
	local := filepath.Join(base, "work")
	legacy := filepath.Join(local, paths.LegacyLocalContentDir)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	r := pathResolver{roots: []string{user}, localRoot: local, external: true}
	if got := r.dir(paths.ContentDirName, paths.LegacyLocalContentDir); got != legacy {
		t.Fatalf("content dir = %q, want legacy fallback %q", got, legacy)
	}

	preferred := filepath.Join(user, paths.ContentDirName)
	if err := os.MkdirAll(preferred, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.dir(paths.ContentDirName, paths.LegacyLocalContentDir); got != preferred {
		t.Fatalf("content dir = %q, want user path %q", got, preferred)
	}
}

func TestExplicitConfigFilesAreStrict(t *testing.T) {
	dir := t.TempDir()
	if _, err := explicitFile(dir); err == nil {
		t.Fatal("directory accepted as an explicit config file")
	}
	missing := filepath.Join(dir, "missing.toml")
	if _, err := explicitFile(missing); err == nil {
		t.Fatal("missing explicit config file accepted")
	}
}

func TestCategorizedConfigRootResolvesEveryResource(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, paths.GameDirName, paths.GameConfigFile)
	keymap := filepath.Join(root, paths.InputDirName, paths.KeymapConfigFile)
	music := filepath.Join(root, paths.AudioDirName, paths.MusicConfigFile)
	sounds := filepath.Join(root, paths.AudioDirName, paths.SoundConfigFile)
	content := filepath.Join(root, paths.ContentDirName)
	for _, path := range []string{game, keymap, music, sounds} {
		writePathFixture(t, path)
	}
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Config{ConfigDir: root}
	if got, err := ResolveGameConfig(cfg); err != nil || got != game {
		t.Fatalf("game = %q, %v; want %q", got, err, game)
	}
	if got, err := ResolveKeymap(cfg); err != nil || got != keymap {
		t.Fatalf("keymap = %q, %v; want %q", got, err, keymap)
	}
	if got, err := ResolveContent(cfg); err != nil || got.Dir != content {
		t.Fatalf("content = %+v, %v; want %q", got, err, content)
	}
	if got, err := ResolveAudioConfig(cfg); err != nil || got.MusicPath != music || got.SoundPath != sounds {
		t.Fatalf("audio = %+v, %v; want %q and %q", got, err, music, sounds)
	}
}

func TestConfigDirValidationIsStrict(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "missing")
	if err := (Config{ConfigDir: missing}).Validate(); err == nil {
		t.Fatal("missing config root accepted")
	}
	file := filepath.Join(base, "file")
	writePathFixture(t, file)
	if err := (Config{ConfigDir: file}).Validate(); err == nil {
		t.Fatal("config root file accepted as a directory")
	}
	if err := (Config{ConfigDir: base}).Validate(); err != nil {
		t.Fatalf("valid config root rejected: %v", err)
	}
}
