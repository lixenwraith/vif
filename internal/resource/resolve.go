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
// selects config-root discovery; Embedded selects the built-in scenario and
// corpus and rejects Scenario and Content.
type Options struct {
	// Dir is an optional root searched before the user and system roots, in the
	// categorized scenario/, input/, audio/, content/, image/ layout. Empty
	// selects platform discovery.
	Dir string

	// Scenario is a scenario name, a scenario.toml path, or a scenario directory.
	Scenario string

	// Content is a corpus directory or a single content file.
	Content string

	// Keymap, Music and Sounds are explicit TOML overrides.
	Keymap string
	Music  string
	Sounds string

	// Embedded forces the built-in scenario and corpus.
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
	if o.Embedded && (o.Scenario != "" || o.Content != "") {
		return errors.New("-d is mutually exclusive with -s and -f")
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

// ScenarioPath returns the scenario entry path. An empty path selects the
// embedded default.
func ScenarioPath(o Options) (string, error) {
	if o.Embedded {
		return "", nil
	}
	if o.Scenario != "" {
		info, err := os.Stat(o.Scenario)
		if err == nil {
			if info.IsDir() {
				p := filepath.Join(o.Scenario, paths.ScenarioFile)
				if !fileExists(p) {
					return "", fmt.Errorf("%s not found in %s", paths.ScenarioFile, o.Scenario)
				}
				return p, nil
			}
			return o.Scenario, nil // explicit file: entry filename override
		}
		if !errors.Is(err, os.ErrNotExist) || !isScenarioName(o.Scenario) {
			return "", err
		}
		if p := newResolver(o).scenario(o.Scenario); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("scenario %q not found as a path or in any configuration root", o.Scenario)
	}

	r := newResolver(o)
	return r.scenario(paths.MainScenarioName), nil
}

// isScenarioName distinguishes the installed shorthand from an explicit path.
// A path always wins when it exists; only a single clean path element falls
// back to scenario/<name>/scenario.toml under the configured roots.
func isScenarioName(name string) bool {
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

func (r resolver) scenario(name string) string {
	return r.file(filepath.Join(paths.ScenarioDirName, name), paths.ScenarioFile)
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
