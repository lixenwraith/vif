// Package paths defines Vi-Fighter's external filesystem layout. It owns
// platform discovery only; callers decide which resource names are required.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AppDirName = "vi-fighter"

	GameDirName    = "game"
	InputDirName   = "input"
	AudioDirName   = "audio"
	ContentDirName = "content"
	LogDirName     = "log"
	JournalDirName = "journal"

	GameConfigFile   = "game.toml"
	KeymapConfigFile = "keymap.toml"
	MusicConfigFile  = "music.toml"
	SoundConfigFile  = "sounds.toml"

	// LegacyLocalConfigDir and LegacyLocalContentDir are compatibility-only
	// working-directory fallbacks. New installations use ConfigRoots.
	LegacyLocalConfigDir  = "config"
	LegacyLocalContentDir = "data"
	LegacyLocalLogDir     = "log"
)

// ExternalFiles reports whether this target has a host filesystem available
// for discovery. Browser builds use only embedded assets unless an explicit
// virtual-filesystem path is supplied by an embedder.
func ExternalFiles() bool { return runtime.GOOS != "js" }

// ConfigRoots returns configuration roots in descending priority: an operator
// override, the per-user root, then XDG system roots. Duplicate roots are
// removed without changing order.
func ConfigRoots(override string) []string {
	if !ExternalFiles() {
		return nil
	}

	var roots []string
	add := func(root string) {
		if root == "" {
			return
		}
		root = filepath.Clean(root)
		for _, existing := range roots {
			if existing == root {
				return
			}
		}
		roots = append(roots, root)
	}

	add(override)
	if base, err := os.UserConfigDir(); err == nil {
		add(filepath.Join(base, AppDirName))
	}
	for _, base := range systemConfigBases() {
		add(filepath.Join(base, AppDirName))
	}
	return roots
}

// DefaultLogDir returns the writable session-log directory. The XDG state
// hierarchy is preferred; the historical ./log directory is the final
// fallback when no platform user directory can be established.
func DefaultLogDir() string { return stateDir(LogDirName) }

// DefaultJournalDir returns the writable replay-journal directory. Journals
// are kept apart from diagnostic logs when a platform state root is available.
func DefaultJournalDir() string { return stateDir(JournalDirName) }

func stateDir(kind string) string {
	if ExternalFiles() {
		if base := stateBase(); base != "" {
			return filepath.Join(base, AppDirName, kind)
		}
	}
	return filepath.Join(".", LegacyLocalLogDir)
}

func stateBase() string {
	if isUnixLike() {
		if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
			return base
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "state")
		}
	}
	if base, err := os.UserCacheDir(); err == nil {
		return base
	}
	return ""
}

func systemConfigBases() []string {
	if !isUnixLike() {
		return nil
	}
	spec := os.Getenv("XDG_CONFIG_DIRS")
	if spec == "" {
		spec = "/etc/xdg"
	}

	var out []string
	for _, base := range filepath.SplitList(spec) {
		base = strings.TrimSpace(base)
		if filepath.IsAbs(base) {
			out = append(out, filepath.Clean(base))
		}
	}
	return out
}

func isUnixLike() bool {
	switch runtime.GOOS {
	case "aix", "android", "dragonfly", "freebsd", "illumos", "linux",
		"netbsd", "openbsd", "solaris":
		return true
	default:
		return false
	}
}
