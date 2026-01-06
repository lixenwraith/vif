package fsm

import (
	"fmt"
	"os"
	"path"

	"github.com/lixenwraith/vi-fighter/toml"
)

// LoadConfigAuto tries external config from DefaultConfigPath, falls back to embedded
func LoadConfigAuto[T any](m *Machine[T], embeddedFallback string) error {
	if fileExists(DefaultConfigPath) {
		return LoadConfigFromDir(m, DefaultConfigDir)
	}
	return m.LoadConfig([]byte(embeddedFallback))
}

// LoadConfigFromDir loads game.toml from configDir and resolves all file includes
func LoadConfigFromDir[T any](m *Machine[T], configDir string) error {
	visited := make(map[string]bool)
	merged, err := loadAndResolve(configDir, DefaultConfigFile, visited)
	if err != nil {
		return fmt.Errorf("failed to load FSM config from %s: %w", configDir, err)
	}
	return m.LoadConfigFromMap(merged)
}

// loadAndResolve recursively loads a TOML file and resolves region file includes
func loadAndResolve(baseDir, filename string, visited map[string]bool) (map[string]any, error) {
	fullPath := path.Join(baseDir, filename)

	// Circular include detection
	if visited[fullPath] {
		return nil, fmt.Errorf("circular include detected: %s", fullPath)
	}
	visited[fullPath] = true

	// Read and parse file
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", fullPath, err)
	}

	p := toml.NewParser(data)
	parsed, err := p.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", fullPath, err)
	}

	// Ensure states map exists
	if parsed["states"] == nil {
		parsed["states"] = make(map[string]any)
	}

	// Process region file includes
	regionsRaw, hasRegions := parsed["regions"]
	if !hasRegions {
		return parsed, nil
	}

	regions, ok := regionsRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: regions must be a table", fullPath)
	}

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
			return nil, fmt.Errorf("%s: region '%s' file must be a string", fullPath, regionName)
		}

		// Recursively load the region file
		regionMap, err := loadAndResolve(baseDir, fileStr, visited)
		if err != nil {
			return nil, fmt.Errorf("region '%s': %w", regionName, err)
		}

		// Validate region file contains only allowed keys
		for key := range regionMap {
			if key != "states" {
				panic(fmt.Sprintf("region file '%s' contains unexpected key '%s'; only [states] allowed", fileStr, key))
			}
		}

		// Merge states from region file
		if regionStates, ok := regionMap["states"].(map[string]any); ok {
			if err := mergeStates(parsed["states"].(map[string]any), regionStates, fileStr); err != nil {
				return nil, err
			}
		}

		// Remove file key from region config (consumed during loading)
		delete(regionCfg, "file")
	}

	return parsed, nil
}

// mergeStates merges addition states into base, panics on collision
func mergeStates(base, addition map[string]any, sourceFile string) error {
	for stateName, stateConfig := range addition {
		if _, exists := base[stateName]; exists {
			panic(fmt.Sprintf("duplicate state '%s' from file '%s'", stateName, sourceFile))
		}
		base[stateName] = stateConfig
	}
	return nil
}

// fileExists checks if a file exists and is not a directory
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}