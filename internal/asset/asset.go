// Package asset embeds every file the binary must be able to play without a
// host filesystem: the fallback FSM bundle and typing corpus, the default
// keymap, and the built-in sound bank. Each group is narrowed with fs.Sub so
// its runtime root is the category directory, not internal/asset.
package asset

import (
	"embed"
	"io/fs"
)

//go:embed config/*.toml content/*.toml input/keymap.toml audio/*.toml
var assetFS embed.FS

var (
	// DefaultFSMConfig is the fallback FSM bundle; DefaultFSMEntry is its entry.
	DefaultFSMConfig fs.FS
	// DefaultContent is the fallback typing corpus.
	DefaultContent fs.FS
	// DefaultSounds holds the built-in sound specs; DefaultSoundFiles is their
	// load order, a later file overriding an earlier one by name.
	DefaultSounds fs.FS
)

const DefaultFSMEntry = "game.toml"

var DefaultSoundFiles = []string{"sfx.toml", "drums.toml"}

// DefaultKeymap is the keymap TOML the binary falls back to.
var DefaultKeymap []byte

// A missing group is a broken build artifact, not a recoverable user error.
func init() {
	DefaultFSMConfig = sub("config")
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
