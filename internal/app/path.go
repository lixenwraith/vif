package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/internal/paths"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

// pathResolver applies one precedence rule to every discovered resource:
// operator root, user root, system roots, deprecated working-directory paths,
// then the caller's embedded fallback.
type pathResolver struct {
	roots     []string
	localRoot string
	external  bool
}

func newPathResolver(cfg Config) pathResolver {
	return pathResolver{
		roots:     paths.ConfigRoots(cfg.ConfigDir),
		localRoot: ".",
		external:  paths.ExternalFiles(),
	}
}

// ResolveGameConfig returns the FSM entry config path. An empty path selects
// the embedded default.
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
			p := filepath.Join(cfg.GameScript, paths.GameConfigFile)
			if !fileExists(p) {
				return "", fmt.Errorf("%s not found in %s", paths.GameConfigFile, cfg.GameScript)
			}
			return p, nil
		}
		return cfg.GameScript, nil // explicit file: entry filename override
	}

	r := newPathResolver(cfg)
	return r.file(paths.GameDirName, paths.GameConfigFile,
		paths.GameConfigFile,
		filepath.Join(paths.LegacyLocalConfigDir, paths.GameConfigFile)), nil
}

// ResolveKeymap returns the external keymap path. An empty path selects the
// embedded default keymap.
func ResolveKeymap(cfg Config) (string, error) {
	if cfg.KeymapPath != "" {
		return explicitFile(cfg.KeymapPath)
	}
	r := newPathResolver(cfg)
	return r.file(paths.InputDirName, paths.KeymapConfigFile,
		paths.KeymapConfigFile,
		filepath.Join(paths.LegacyLocalConfigDir, paths.KeymapConfigFile)), nil
}

// ResolveContent locates the corpus. An empty source selects embedded content.
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

	r := newPathResolver(cfg)
	if p := r.dir(paths.ContentDirName, paths.LegacyLocalContentDir); p != "" {
		return service.ContentSource{Dir: p}, nil
	}
	return service.ContentSource{}, nil
}

// ResolveAudioConfig resolves optional music and sound override documents.
// Empty paths leave pkg/audio's embedded definitions in place.
func ResolveAudioConfig(cfg Config) (service.AudioSource, error) {
	r := newPathResolver(cfg)
	music, err := r.optionalFile(cfg.MusicPath, paths.AudioDirName, paths.MusicConfigFile,
		paths.MusicConfigFile,
		filepath.Join(paths.LegacyLocalConfigDir, paths.MusicConfigFile))
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("music config: %w", err)
	}
	sounds, err := r.optionalFile(cfg.SoundPath, paths.AudioDirName, paths.SoundConfigFile,
		paths.SoundConfigFile,
		filepath.Join(paths.LegacyLocalConfigDir, paths.SoundConfigFile))
	if err != nil {
		return service.AudioSource{}, fmt.Errorf("sound config: %w", err)
	}
	return service.AudioSource{MusicPath: music, SoundPath: sounds}, nil
}

func (r pathResolver) optionalFile(explicit, category, name string, local ...string) (string, error) {
	if explicit != "" {
		return explicitFile(explicit)
	}
	return r.file(category, name, local...), nil
}

// file checks the categorized path before the legacy flat path within each
// root. Only after every configured root does it inspect working-directory
// compatibility paths.
func (r pathResolver) file(category, name string, local ...string) string {
	for _, root := range r.roots {
		for _, candidate := range []string{
			filepath.Join(root, category, name),
			filepath.Join(root, name),
		} {
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	if !r.external {
		return ""
	}
	for _, candidate := range local {
		candidate = filepath.Join(r.localRoot, candidate)
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func (r pathResolver) dir(category, legacy string) string {
	for _, root := range r.roots {
		for _, candidate := range []string{
			filepath.Join(root, category),
			filepath.Join(root, legacy),
		} {
			if dirExists(candidate) {
				return candidate
			}
		}
	}
	if r.external {
		candidate := filepath.Join(r.localRoot, legacy)
		if dirExists(candidate) {
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
