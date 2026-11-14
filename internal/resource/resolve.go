// Package resource applies one precedence rule to every runtime resource: its flag,
// -config-dir, the user root, the checkout's wad/, the system roots, then the
// embedded copy. It reads a scenario whole into the digested form a session serves
// a joiner, and validates what the paths resolve to without starting a runtime.
package resource

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/service"
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

	// ForeignDir and ForeignKeymap mark Dir and Keymap as named by a vif.toml that is
	// not the player's own, a checkout's or a system root's: a keymap found through
	// one is read up to date, never written back.
	ForeignDir, ForeignKeymap bool

	// Embedded forces the built-in scenario and corpus.
	Embedded bool

	// Provided is a scenario already in hand rather than one to find: the bytes a
	// session coordinator sent because no root here held them. It wins over every
	// other selector, since a run holding one has already agreed to simulate
	// exactly those bytes.
	Provided *Scenario

	// Downloads is where scenarios sessions' hosts sent are found by digest, empty
	// for nowhere; SaveDownloads saves there one this run is sent.
	Downloads     string
	SaveDownloads bool
}

// Validate reports conflicts between the overrides themselves.
func (o Options) Validate() error {
	if err := paths.CheckRoot(o.Dir); err != nil {
		return err
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
// embedded main scenario.
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
		if !paths.ExternalFiles() {
			if o.Scenario == paths.MainScenarioName {
				return "", nil
			}
			return "", fmt.Errorf("scenario %q not found: a browser holds only the embedded %s", o.Scenario, paths.MainScenarioName)
		}
		switch {
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("scenario %q: %w", o.Scenario, err)
		case !paths.IsScenarioName(o.Scenario):
			return "", fmt.Errorf("scenario %q not found", o.Scenario)
		}
		if p := newResolver(o).scenario(o.Scenario); p != "" {
			return p, nil
		}
		if o.Scenario == paths.MainScenarioName {
			return "", nil
		}
		return "", fmt.Errorf("scenario %q not found as a path or in any configuration root", o.Scenario)
	}

	r := newResolver(o)
	return r.scenario(paths.MainScenarioName), nil
}

// Scenarios names every scenario a run with these options starts by name: the main
// one, which the embedded copy answers for when no root holds it, then every one the
// roots' scenario/ trees hold, sorted. Each goes by its directory's name, or by its
// path below scenario/ where that name reaches another first; one a higher root
// holds at the same path is not named, since no name reaches it.
func Scenarios(o Options) []string {
	names := []string{paths.MainScenarioName}
	if o.Embedded {
		return names
	}
	seen := map[string]bool{}
	for _, root := range newResolver(o).roots {
		paths.WalkScenarios(root, func(rel, _ string) bool {
			switch name := path.Base(rel); {
			case !seen[name] && name != paths.MainScenarioName:
				names = append(names, name)
			case seen[name] && !seen[rel]:
				names = append(names, rel)
			}
			seen[path.Base(rel)], seen[rel] = true, true
			return true
		})
	}
	slices.Sort(names[1:])
	return names
}

// Keymap returns the external keymap path, "" for the embedded default keymap.
func Keymap(o Options) (string, error) {
	path, _, err := keymap(o)
	return path, err
}

// keymap is Keymap's path and whether it is the player's own, which a run writes
// back brought up to date: one they name, or one in the -config-dir root or the
// user root, as vif.toml goes, unless a vif.toml not theirs named it.
func keymap(o Options) (string, bool, error) {
	path, err := paths.ConfigFile(newResolver(o).roots, o.Keymap, paths.InputDirName, paths.KeymapConfigFile)
	if err != nil || path == "" {
		return "", false, err
	}
	if o.Keymap != "" {
		return path, !o.ForeignKeymap, nil
	}
	dir := o.Dir
	if o.ForeignDir {
		dir = ""
	}
	return path, paths.OwnRoot(dir, filepath.Dir(filepath.Dir(path))), nil
}

// UpgradeKeymap brings the player's own keymap up to this build's in place
// (input.UpgradeKeymap), as paths.UpgradeStore does vif.toml, and returns the status
// line naming the changes, "" when there were none; another's is left as it is, and
// one that does not read is for the run to refuse.
func UpgradeKeymap(o Options) string {
	path, own, err := keymap(o)
	if err != nil || !own {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	out, notes, err := input.UpgradeKeymap(data)
	if err != nil || len(notes) == 0 {
		return ""
	}
	if err := (paths.FileStore{Path: path}).Store(out); err != nil {
		return "Keymap not brought up to date: " + err.Error()
	}
	return "Keymap brought up to date: " + strings.Join(notes, "; ")
}

// BotGraph resolves a bot graph by path or by name: an existing file wins, and a
// name finds bot/<name>.toml under the configured roots, then the embedded copy.
// A graph is a participant's own, like its keymap, so -d does not pin it. It
// returns the document and the name the graph goes by.
func BotGraph(o Options, spec string) ([]byte, string, error) {
	if info, err := os.Stat(spec); err == nil && !info.IsDir() {
		data, err := os.ReadFile(spec)
		return data, strings.TrimSuffix(filepath.Base(spec), ".toml"), err
	}
	if spec == "" || (!paths.IsScenarioName(spec) || filepath.Base(spec) != spec) && paths.ExternalFiles() {
		return nil, "", fmt.Errorf("bot graph %q not found", spec)
	}
	file := spec + ".toml"
	if p := paths.FindFile(newResolver(o).roots, paths.BotDirName, file); p != "" {
		data, err := os.ReadFile(p)
		return data, spec, err
	}
	// Preserve named overrides while old built-in names share the current policy.
	if spec == "roam" || spec == "patrol" {
		file, spec = "default.toml", "default"
	}
	data, err := fs.ReadFile(asset.DefaultBots, file)
	switch {
	case err == nil:
		return data, spec, nil
	case !paths.ExternalFiles():
		return nil, "", fmt.Errorf("bot graph %q not found: a browser holds only the embedded graphs", spec)
	}
	return nil, "", fmt.Errorf("bot graph %q not found as a path, in a configuration root or embedded", spec)
}

// Files supplies the ordered configuration roots to FileService. Resolution
// and host I/O stay in that service; this package remains the composition-time
// owner of root selection.
func Files(o Options) service.FileSource {
	return service.FileSource{Roots: paths.ConfigRoots(o.Dir)}
}

// Corpus locates the content corpus: the scenario's own when its entry names one,
// else the roots'. An empty source selects embedded content.
func Corpus(o Options, sc Scenario) (service.ContentSource, error) {
	if fsys, ok := sc.Content(); ok {
		return service.ContentSource{FS: fsys, Label: "scenario " + sc.Name, Explicit: true}, nil
	}
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
	roots := newResolver(o).roots
	music, err := paths.ConfigFile(roots, o.Music, paths.AudioDirName, paths.MusicConfigFile)
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("music config: %w", err)
	}
	sounds, err := paths.ConfigFile(roots, o.Sounds, paths.AudioDirName, paths.SoundConfigFile)
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("sound config: %w", err)
	}
	return service.AudioSource{MusicPath: music, SoundPath: sounds}, nil
}

// scenario finds name below each root's scenario/ in turn: a path exactly, a bare
// name as the first directory of that name WalkScenarios reaches.
func (r resolver) scenario(name string) string {
	bare := filepath.Base(name) == name
	for _, root := range r.roots {
		found := filepath.Join(root, paths.ScenarioDirName, name, paths.ScenarioFile)
		if fileExists(found) {
			return found
		}
		found = ""
		if bare {
			paths.WalkScenarios(root, func(rel, dir string) bool {
				if path.Base(rel) == name {
					found = filepath.Join(dir, paths.ScenarioFile)
				}
				return found == ""
			})
		}
		if found != "" {
			return found
		}
	}
	return ""
}

func (r resolver) dir(category string) string {
	for _, root := range r.roots {
		if candidate := filepath.Join(root, category); dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
