package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/service"
)

// ResolveGameConfig returns the FSM entry config path
// "" selects the embedded default
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

// ResolveContent locates the corpus: explicit flag > ./data > user config.
// A zero Dir selects the embedded corpus.
func ResolveContent(cfg Config) (service.ContentSource, error) {
	if cfg.ForceDefault {
		return service.ContentSource{}, nil
	}

	if p := cfg.ContentPath; p != "" {
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

	candidates := []string{parameter.ContentDataDir}
	if base, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(base, parameter.AppConfigDirName, parameter.ContentDataDir))
	}
	for _, p := range candidates {
		if dirExists(p) {
			return service.ContentSource{Dir: p}, nil
		}
	}
	return service.ContentSource{}, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
