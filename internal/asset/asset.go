// Package asset embeds every file the binary must be able to play without a
// host filesystem: the fallback scenario and typing corpus, the default keymap and
// settings, the built-in sound and music banks, and the version a source archive
// carries. Each group is narrowed with fs.Sub so its runtime root is its category.
package asset

import (
	"embed"
	"io/fs"
	"runtime/debug"
	"strings"
)

//go:embed scenario/*.toml content/*.toml input/keymap.toml audio/*.toml vif.toml
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

// DefaultMusic is the built-in pattern bank, which a user music.toml overrides by name.
var DefaultMusic []byte

// DefaultSettings is the vif.toml whose values stand when no root holds one.
var DefaultSettings []byte

// A missing group is a broken build artifact, not a recoverable user error.
func init() {
	DefaultScenario = sub("scenario")
	DefaultContent = sub("content")
	DefaultSounds = sub("audio")
	DefaultKeymap = read("input/keymap.toml")
	DefaultMusic = read("audio/music.toml")
	DefaultSettings = read("vif.toml")
}

// versionStamp is "$Format:...$" in a checkout; git archive, GitHub's archives
// included, expands it to the tag and commit through .gitattributes export-subst.
//
//go:embed version.txt
var versionStamp string

// Version is the release and commit the binary was built from: a source archive's
// stamp first, since an unpacked archive may sit inside an unrelated repository,
// then the Go toolchain's VCS stamp, then "(devel)" and no commit.
func Version() (version, revision string) {
	if !strings.HasPrefix(versionStamp, "$Format") {
		switch fields := strings.Fields(versionStamp); len(fields) {
		case 2:
			return fields[0], fields[1]
		case 1:
			return "(devel)", fields[0]
		}
	}
	version = "(devel)"
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.Main.Version != "" {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				revision = s.Value
			}
		}
	}
	return version, revision
}

func read(name string) []byte {
	data, err := assetFS.ReadFile(name)
	if err != nil {
		panic("asset: embedded " + name + " missing")
	}
	return data
}

func sub(dir string) fs.FS {
	f, err := fs.Sub(assetFS, dir)
	if err != nil {
		panic("asset: embedded " + dir + " missing")
	}
	return f
}
