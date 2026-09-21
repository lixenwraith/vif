// Package asset embeds every file the binary must be able to play without a
// host filesystem: the fallback scenario and typing corpus, the default keymap,
// and the built-in sound bank. Each group is narrowed with fs.Sub so its runtime
// root is the category directory, not internal/asset.
package asset

import (
	"embed"
	"io/fs"
)

//go:embed scenario/*.toml content/*.toml input/keymap.toml audio/*.toml
var assetFS embed.FS

var (
	// DefaultScenario is the fallback scenario; DefaultScenarioEntry is its entry.
	DefaultScenario fs.FS
	// DefaultContent is the fallback typing corpus.
	DefaultContent fs.FS
	// DefaultSounds holds the built-in sound specs; DefaultSoundFiles is their
	// load order, a later file overriding an earlier one by name.
	DefaultSounds fs.FS
)

const DefaultScenarioEntry = "scenario.toml"

var DefaultSoundFiles = []string{"sfx.toml", "drums.toml"}

// DefaultKeymap is the keymap TOML the binary falls back to.
var DefaultKeymap []byte

// A missing group is a broken build artifact, not a recoverable user error.
func init() {
	DefaultScenario = sub("scenario")
	DefaultContent = sub("content")
	DefaultSounds = sub("audio")
	data, err := assetFS.ReadFile("input/keymap.toml")
	if err != nil {
		panic("asset: embedded keymap missing")
	}
	DefaultKeymap = data
}

func sub(dir string) fs.FS {
	f, err := fs.Sub(assetFS, dir)
	if err != nil {
		panic("asset: embedded " + dir + " missing")
	}
	return f
}
