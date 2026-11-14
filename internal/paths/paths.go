// Package paths defines vif's external filesystem layout: the configuration roots
// in priority order, the categorized names under each, the user-state directories
// and vif.toml. It finds files and reads only vif.toml; callers decide which
// resources are required and read them.
package paths

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AppDirName = "vif"

	ScenarioDirName  = "scenario"
	MainScenarioName = "main"
	DownloadsDirName = "downloads"
	InputDirName     = "input"
	AudioDirName     = "audio"
	ContentDirName   = "content"
	ImageDirName     = "image"
	BotDirName       = "bot"
	LogDirName       = "log"
	JournalDirName   = "journal"
	MusicDirName     = "music"
	PlacesFileName   = "places.json"
	SignInsFileName  = "signins.json"

	ScenarioFile     = "scenario.toml"
	KeymapConfigFile = "keymap.toml"
	MusicConfigFile  = "music.toml"
	SoundConfigFile  = "sounds.toml"
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

// DefaultDownloadsDir is where a run saves the scenarios its sessions' hosts send,
// and finds them again: the user root's scenario/downloads, "" without one.
func DefaultDownloadsDir() string {
	user := UserRoot()
	if user == "" {
		return ""
	}
	return filepath.Join(user, ScenarioDirName, DownloadsDirName)
}

// executable names the running binary, which places the checkout's wad
var executable = os.Executable

// checkoutRoot is the wad/ beside the bin/ holding this binary, so a build plays
// its checkout's scenarios uninstalled; "" for a binary anywhere else.
func checkoutRoot() string {
	exe, err := executable()
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

// IsScenarioName reports whether name is one the roots resolve below scenario/
// rather than a path on its own: clean, relative and staying below, with no element
// hidden. A bare name is found at any depth, a longer one is that path exactly.
func IsScenarioName(name string) bool {
	slash := filepath.ToSlash(name)
	return filepath.IsLocal(name) && slash == path.Clean(slash) &&
		!strings.HasPrefix(slash, ".") && !strings.Contains(slash, "/.")
}

// WalkScenarios visits each scenario below root's scenario/, by its slash-separated
// path there and its directory, shallowest first and in path order among equals,
// until visit returns false. A directory holding scenario.toml is a scenario and
// what is below it its own; any other is a group, read at any depth unless it is a
// link. Names beginning with a dot are passed over.
func WalkScenarios(root string, visit func(rel, dir string) bool) {
	level := []string{""}
	for len(level) > 0 {
		var next []string
		for _, group := range level {
			entries, err := os.ReadDir(filepath.Join(root, ScenarioDirName, filepath.FromSlash(group)))
			if err != nil {
				continue
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".") {
					continue
				}
				rel := path.Join(group, e.Name())
				dir := filepath.Join(root, ScenarioDirName, filepath.FromSlash(rel))
				if info, err := os.Stat(filepath.Join(dir, ScenarioFile)); err == nil && !info.IsDir() {
					if !visit(rel, dir) {
						return
					}
				} else if e.IsDir() {
					next = append(next, rel)
				}
			}
		}
		level = next
	}
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

// DefaultLogDir returns the writable session-log directory. The XDG state
// hierarchy is preferred; ./vif/log is the final fallback when no platform user
// directory can be established.
func DefaultLogDir() string { return stateDir(LogDirName) }

// DefaultJournalDir returns the writable replay-journal directory. Journals
// are kept apart from diagnostic logs when a platform state root is available.
func DefaultJournalDir() string { return stateDir(JournalDirName) }

// DefaultMusicDir returns where a recorded run's music is written, beside its logs.
func DefaultMusicDir() string { return stateDir(MusicDirName) }

// PlacesFile is where a run keeps the places it comes back to its sessions with,
// the key each is held under, beside its logs; a file for the user alone.
func PlacesFile() string { return filepath.Join(stateDir(""), PlacesFileName) }

// SignInsFile is where a run keeps the sign-ins it holds, one a site, beside its
// places; a file for the user alone, since each is the account until it is revoked.
func SignInsFile() string { return filepath.Join(stateDir(""), SignInsFileName) }

// PlaceFile is the places document at Path: one not there yet reads as none, and a
// write replaces it whole under a mode the user alone reads, by rename.
type PlaceFile struct{ Path string }

func (f PlaceFile) Load() ([]byte, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func (f PlaceFile) Store(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

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
