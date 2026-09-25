// Package paths defines vif's external filesystem layout. It owns
// platform discovery only; callers decide which resource names are required.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AppDirName = "vif"

	ScenarioDirName  = "scenario"
	MainScenarioName = "main"
	InputDirName     = "input"
	AudioDirName     = "audio"
	ContentDirName   = "content"
	ImageDirName     = "image"
	LogDirName       = "log"
	JournalDirName   = "journal"

	ScenarioFile     = "scenario.toml"
	KeymapConfigFile = "keymap.toml"
	MusicConfigFile  = "music.toml"
	SoundConfigFile  = "sounds.toml"

	// FallbackLogDir is used only when no platform state or cache root can be
	// established; every normal target resolves one.
	FallbackLogDir = "log"
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

// CheckRoot rejects a -config-dir that is not an existing directory; empty is none.
func CheckRoot(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("-config-dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("-config-dir %q is not a directory", dir)
	}
	return nil
}

// FindFile returns category/name under the first root holding it, or "".
func FindFile(roots []string, category, name string) string {
	for _, root := range roots {
		candidate := filepath.Join(root, category, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ConfigFile resolves one optional document: an explicit path wins and must be a
// file; otherwise the first root holding category/name, or "" for the embedded one.
func ConfigFile(roots []string, explicit, category, name string) (string, error) {
	if explicit == "" {
		return FindFile(roots, category, name), nil
	}
	info, err := os.Stat(explicit)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", explicit)
	}
	return explicit, nil
}

// ConfigTarget is where a document must be written for the next run with these
// roots to read it first: the explicit path, else category/name under the first
// root. A system root further down stays untouched and is shadowed.
func ConfigTarget(roots []string, explicit, category, name string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if len(roots) == 0 {
		return "", errors.New("no configuration root; name a file")
	}
	return filepath.Join(roots[0], category, name), nil
}

// DefaultLogDir returns the writable session-log directory. The XDG state
// hierarchy is preferred; ./log is the final fallback when no platform user
// directory can be established.
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
	return filepath.Join(".", FallbackLogDir)
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
		if runtime.GOOS == "freebsd" {
			// Ports install under /usr/local, which the XDG default does not name.
			spec = "/usr/local/etc/xdg:/etc/xdg"
		}
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
