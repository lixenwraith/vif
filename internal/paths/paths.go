// Package paths defines vif's external filesystem layout: the configuration roots
// in priority order, the categorized names under each, the user-state directories
// and vif.toml. It finds files and reads only vif.toml; callers decide which
// resources are required and read them.
package paths

import (
	"cmp"
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
	BotDirName       = "bot"
	LogDirName       = "log"
	JournalDirName   = "journal"
	MusicDirName     = "music"

	ScenarioFile     = "scenario.toml"
	KeymapConfigFile = "keymap.toml"
	MusicConfigFile  = "music.toml"
	SoundConfigFile  = "sounds.toml"
	RipConfigFile    = "rip.toml"
	RipModelFile     = "ripnet.bin"
)

// ExternalFiles reports whether this target has a host filesystem available
// for discovery. Browser builds use only embedded assets unless an explicit
// virtual-filesystem path is supplied by an embedder.
func ExternalFiles() bool { return runtime.GOOS != "js" }

// SystemRootsEnv names the system roots in place of the default list, separated
// as PATH is; set but empty, there are none.
const SystemRootsEnv = "VIF_SYSTEM_ROOTS"

// ConfigRoots returns configuration roots in descending priority: an operator
// override, the per-user root, the checkout this binary was built in, then the
// system roots. Duplicate roots are removed without changing order.
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
	add(UserRoot())
	add(checkoutRoot())
	for _, root := range systemRoots() {
		add(root)
	}
	return roots
}

// UserRoot is the player's own configuration root: os.UserConfigDir plus vif, ""
// where the platform names none or a build has no host filesystem.
func UserRoot() string {
	if !ExternalFiles() {
		return ""
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, AppDirName)
}

// checkoutRoot is the wad/ beside the bin/ holding this binary, so a build plays
// its checkout's scenarios uninstalled; "" for a binary anywhere else.
func checkoutRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	root := filepath.Join(filepath.Dir(filepath.Dir(exe)), "wad")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return ""
	}
	return root
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
// hierarchy is preferred; ./vif/log is the final fallback when no platform user
// directory can be established.
func DefaultLogDir() string { return stateDir(LogDirName) }

// DefaultJournalDir returns the writable replay-journal directory. Journals
// are kept apart from diagnostic logs when a platform state root is available.
func DefaultJournalDir() string { return stateDir(JournalDirName) }

// DefaultMusicDir returns where a recorded run's music is written, beside its logs.
func DefaultMusicDir() string { return stateDir(MusicDirName) }

func stateDir(kind string) string {
	base := "."
	if ExternalFiles() {
		base = cmp.Or(stateBase(), base)
	}
	return filepath.Join(base, AppDirName, kind)
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

func systemRoots() []string {
	if !isUnixLike() {
		return nil
	}
	spec, ok := os.LookupEnv(SystemRootsEnv)
	if !ok {
		// A source install's /usr/local shadows a distribution package's /etc.
		spec = "/usr/local/etc/vif:/etc/vif"
	}

	var out []string
	for _, root := range filepath.SplitList(spec) {
		root = strings.TrimSpace(root)
		if filepath.IsAbs(root) {
			out = append(out, filepath.Clean(root))
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
