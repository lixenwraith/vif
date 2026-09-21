package fsm

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/lixenwraith/toml"
)

// LoadScenarioFromPath loads a scenario from an OS entry path
// Region file includes resolve relative to the entry file's directory
func LoadScenarioFromPath[T any](m *Machine[T], entryPath string) error {
	info, err := os.Stat(entryPath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("scenario entry not found: %s", entryPath)
	}
	// OS path handling via filepath; include resolution via fs.FS
	fsys := os.DirFS(filepath.Dir(entryPath))
	return LoadScenarioFromFS(m, fsys, filepath.Base(entryPath))
}

// LoadScenarioFromFS loads a scenario from any fs.FS (os.DirFS, embed.FS)
func LoadScenarioFromFS[T any](m *Machine[T], fsys fs.FS, entry string) error {
	// 'stack' tracks the in-progress include chain, not every file seen
	stack := make(map[string]bool)
	merged, err := loadAndResolve(fsys, entry, stack, nil)
	if err != nil {
		return fmt.Errorf("failed to load scenario '%s': %w", entry, err)
	}
	return m.LoadScenarioFromMap(merged)
}

// ResolveScenario merges an entry file and its region includes into the map
// LoadScenarioFromMap consumes, without building a machine. It is the seam a
// checker reads: a rule about what the shipped configuration may declare has to
// see the declarations, and decoding ScenarioDoc from this map is the only way to
// reach a transition's trigger without running the game.
func ResolveScenario(fsys fs.FS, entry string) (map[string]any, error) {
	merged, err := loadAndResolve(fsys, entry, make(map[string]bool), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load scenario '%s': %w", entry, err)
	}
	return merged, nil
}

// ScenarioFiles names every file a scenario is made of — its entry and the region
// files that entry includes — sorted and deduplicated. A portable copy is read
// through this, so it carries exactly the files the loader would have read and
// nothing that happens to sit beside them.
func ScenarioFiles(fsys fs.FS, entry string) ([]string, error) {
	var visited []string
	if _, err := loadAndResolve(fsys, entry, make(map[string]bool), &visited); err != nil {
		return nil, fmt.Errorf("failed to load scenario '%s': %w", entry, err)
	}
	slices.Sort(visited)
	return slices.Compact(visited), nil
}

// loadAndResolve recursively loads a TOML file and resolves region file includes
// stack holds the ancestors currently being resolved; entries are popped on return
// visited, when non-nil, collects every file read, in no particular order
func loadAndResolve(fsys fs.FS, name string, stack map[string]bool, visited *[]string) (map[string]any, error) {
	cleanName := path.Clean(name)
	if stack[cleanName] {
		return nil, fmt.Errorf("circular include detected: %s", cleanName)
	}
	stack[cleanName] = true
	defer delete(stack, cleanName) // pop on return; siblings may re-include
	if visited != nil {
		*visited = append(*visited, cleanName)
	}

	data, err := fs.ReadFile(fsys, cleanName)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", cleanName, err)
	}

	p := toml.NewParser(data)
	parsed, err := p.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", cleanName, err)
	}

	if parsed["states"] == nil {
		parsed["states"] = make(map[string]any)
	}

	regionsRaw, hasRegions := parsed["regions"]
	if !hasRegions {
		return parsed, nil
	}
	regions, ok := regionsRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: regions must be a table", cleanName)
	}

	baseDir := path.Dir(cleanName)

	for regionName, regionRaw := range regions {
		regionCfg, ok := regionRaw.(map[string]any)
		if !ok {
			continue
		}
		fileRef, hasFile := regionCfg["file"]
		if !hasFile {
			continue
		}
		fileStr, ok := fileRef.(string)
		if !ok {
			return nil, fmt.Errorf("%s: region '%s' file must be a string", cleanName, regionName)
		}

		regionMap, err := loadAndResolve(fsys, path.Join(baseDir, fileStr), stack, visited)
		if err != nil {
			return nil, fmt.Errorf("region '%s': %w", regionName, err)
		}

		for key := range regionMap {
			if key != "states" {
				return nil, fmt.Errorf("region file '%s' contains unexpected key '%s'; only [states] allowed", fileStr, key)
			}
		}

		if regionStates, ok := regionMap["states"].(map[string]any); ok {
			if err := mergeStates(parsed["states"].(map[string]any), regionStates, fileStr); err != nil {
				return nil, err
			}
		}

		delete(regionCfg, "file")
	}

	return parsed, nil
}

// mergeStates merges addition states into base, panics on collision
func mergeStates(base, addition map[string]any, sourceFile string) error {
	for stateName, stateConfig := range addition {
		if _, exists := base[stateName]; exists {
			return fmt.Errorf("duplicate state '%s' from file '%s'", stateName, sourceFile)
		}
		base[stateName] = stateConfig
	}
	return nil
}
