// Package resource applies one precedence rule to every discovered runtime
// resource: operator root, user root, system roots, then the caller's embedded
// fallback. It also validates what those paths resolve to, without starting a
// runtime.
package resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/internal/paths"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// Options names the resource overrides a run was started with. An empty field
// selects config-root discovery; Embedded selects the built-in FSM config and
// corpus and rejects Game and Content.
type Options struct {
	// Dir is an optional root searched before the user and system roots, in the
	// categorized game/, input/, audio/, content/, image/ layout. Empty selects
	// platform discovery.
	Dir string

	// Game is a game name, a game.toml path, or a map directory.
	Game string

	// Content is a corpus directory or a single content file.
	Content string

	// Keymap, Music and Sounds are explicit TOML overrides.
	Keymap string
	Music  string
	Sounds string

	// Embedded forces the built-in FSM config and corpus.
	Embedded bool
}

// Validate reports conflicts between the overrides themselves.
func (o Options) Validate() error {
	if o.Dir != "" {
		info, err := os.Stat(o.Dir)
		if err != nil {
			return fmt.Errorf("-config-dir: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("-config-dir %q is not a directory", o.Dir)
		}
	}
	if o.Embedded && (o.Game != "" || o.Content != "") {
		return errors.New("-d is mutually exclusive with -g and -f")
	}
	return nil
}

// resolver holds the roots one Options resolves against.
type resolver struct {
	roots []string
}

func newResolver(o Options) resolver {
	return resolver{roots: paths.ConfigRoots(o.Dir)}
}

// GameConfig returns the FSM entry config path. An empty path selects the
// embedded default.
func GameConfig(o Options) (string, error) {
	if o.Embedded {
		return "", nil
	}
	if o.Game != "" {
		info, err := os.Stat(o.Game)
		if err == nil {
			if info.IsDir() {
				p := filepath.Join(o.Game, paths.GameConfigFile)
				if !fileExists(p) {
					return "", fmt.Errorf("%s not found in %s", paths.GameConfigFile, o.Game)
				}
				return p, nil
			}
			return o.Game, nil // explicit file: entry filename override
		}
		if !errors.Is(err, os.ErrNotExist) || !isGameName(o.Game) {
			return "", err
		}
		if p := newResolver(o).game(o.Game); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("game %q not found as a path or in any configuration root", o.Game)
	}

	r := newResolver(o)
	return r.game(paths.MainGameName), nil
}

// isGameName distinguishes the installed shorthand from an explicit path.
// A path always wins when it exists; only a single clean path element falls
// back to game/<name>/game.toml under the configured roots.
func isGameName(name string) bool {
	return name != "." && name != ".." && filepath.Base(name) == name
}

// Keymap returns the external keymap path. An empty path selects the embedded
// default keymap.
func Keymap(o Options) (string, error) {
	if o.Keymap != "" {
		return explicitFile(o.Keymap)
	}
	r := newResolver(o)
	return r.file(paths.InputDirName, paths.KeymapConfigFile), nil
}

// Files supplies the ordered configuration roots to FileService. Resolution
// and host I/O stay in that service; this package remains the composition-time
// owner of root selection.
func Files(o Options) service.FileSource {
	return service.FileSource{Roots: paths.ConfigRoots(o.Dir)}
}

// Corpus locates the content corpus. An empty source selects embedded content.
func Corpus(o Options) (service.ContentSource, error) {
	if o.Embedded {
		return service.ContentSource{}, nil
	}
	if p := o.Content; p != "" {
		info, err := os.Stat(p)
		if err != nil {
			return service.ContentSource{}, err
		}
		if info.IsDir() {
			return service.ContentSource{Dir: p, Explicit: true}, nil
		}
		return service.ContentSource{
			Dir:      filepath.Dir(p),
			Pin:      filepath.Base(p),
			Explicit: true,
		}, nil
	}

	r := newResolver(o)
	if p := r.dir(paths.ContentDirName); p != "" {
		return service.ContentSource{Dir: p}, nil
	}
	return service.ContentSource{}, nil
}

// Audio resolves optional music and sound override documents. Empty paths leave
// the shipped sound bank and built-in patterns in place.
func Audio(o Options) (service.AudioSource, error) {
	r := newResolver(o)
	music, err := r.optionalFile(o.Music, paths.AudioDirName, paths.MusicConfigFile)
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("music config: %w", err)
	}
	sounds, err := r.optionalFile(o.Sounds, paths.AudioDirName, paths.SoundConfigFile)
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("sound config: %w", err)
	}
	return service.AudioSource{MusicPath: music, SoundPath: sounds}, nil
}

func (r resolver) optionalFile(explicit, category, name string) (string, error) {
	if explicit != "" {
		return explicitFile(explicit)
	}
	return r.file(category, name), nil
}

// Roots are already in priority order, so the first hit wins.
func (r resolver) file(category, name string) string {
	for _, root := range r.roots {
		if candidate := filepath.Join(root, category, name); fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func (r resolver) game(name string) string {
	return r.file(filepath.Join(paths.GameDirName, name), paths.GameConfigFile)
}

func (r resolver) dir(category string) string {
	for _, root := range r.roots {
		if candidate := filepath.Join(root, category); dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

func explicitFile(p string) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", p)
	}
	return p, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
