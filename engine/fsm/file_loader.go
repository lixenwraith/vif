package fsm

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/lixenwraith/toml"
)

// LoadConfigFromPath loads FSM config from an OS file path
// Region file includes resolve relative to the config file's directory
func LoadConfigFromPath[T any](m *Machine[T], configPath string) error {
	info, err := os.Stat(configPath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("config file not found: %s", configPath)
	}
	// OS path handling via filepath; include resolution via fs.FS
	fsys := os.DirFS(filepath.Dir(configPath))
	return LoadConfigFromFS(m, fsys, filepath.Base(configPath))
}

// LoadConfigFromFS loads FSM config from any fs.FS (os.DirFS, embed.FS)
func LoadConfigFromFS[T any](m *Machine[T], fsys fs.FS, entry string) error {
	visited := make(map[string]bool)
	merged, err := loadAndResolve(fsys, entry, visited)
	if err != nil {
		return fmt.Errorf("failed to load FSM config '%s': %w", entry, err)
	}
	return m.LoadConfigFromMap(merged)
}

// loadAndResolve recursively loads a TOML file and resolves region file includes
func loadAndResolve(fsys fs.FS, name string, visited map[string]bool) (map[string]any, error) {
	cleanName := path.Clean(name)
	if visited[cleanName] {
		return nil, fmt.Errorf("circular include detected: %s", cleanName)
	}
	visited[cleanName] = true

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

		regionMap, err := loadAndResolve(fsys, path.Join(baseDir, fileStr), visited)
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
