package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/parameter"
)

// ResolveGameConfig returns the FSM entry config path
// "" selects the embedded default
// CHANGED: the ForceDefault branch was duplicated between main() and
// runConfigCheck(), which could disagree
func ResolveGameConfig(cfg Config) (string, error) {
	if cfg.ForceDefault {
		return "", nil
	}

	if cfg.GameScript != "" {
		info, err := os.Stat(cfg.GameScript)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			p := filepath.Join(cfg.GameScript, parameter.GameConfigFile)
			if !fileExists(p) {
				return "", fmt.Errorf("%s not found in %s", parameter.GameConfigFile, cfg.GameScript)
			}
			return p, nil
		}
		return cfg.GameScript, nil // explicit file: entry filename override
	}

	candidates := []string{
		parameter.GameConfigFile, // ./game.toml
		filepath.Join(parameter.LocalConfigDir, parameter.GameConfigFile), // ./config/game.toml
	}
	if base, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(base, parameter.AppConfigDirName, parameter.GameConfigFile))
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p, nil
		}
	}
	return "", nil
}

// ResolveKeymap returns the keymap path: explicit > ./keymap.toml > user config
// "" selects the embedded default key table
func ResolveKeymap(cfg Config) string {
	if cfg.KeymapPath != "" {
		return cfg.KeymapPath
	}
	if fileExists(parameter.KeymapConfigFile) {
		return parameter.KeymapConfigFile
	}
	if base, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(base, parameter.AppConfigDirName, parameter.KeymapConfigFile)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
