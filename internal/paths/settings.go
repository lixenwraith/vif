package paths

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
)

// SettingsFile is the settings document at the top of a configuration root.
const SettingsFile = "vif.toml"

// Settings is vif.toml: the defaults of flags a player would otherwise repeat on
// every run. Paths arrive resolved, so each reads as its flag's value would.
type Settings struct {
	Paths struct {
		Root     string `toml:"root"`
		Log      string `toml:"log"`
		Journal  string `toml:"journal"`
		Music    string `toml:"music"`
		Scenario string `toml:"scenario"`
		Content  string `toml:"content"`
		Keymap   string `toml:"keymap"`
	} `toml:"paths"`
	Audio struct {
		BufferMs int `toml:"buffer_ms"`
	} `toml:"audio"`
}

// LoadSettings reads the first vif.toml the roots of override hold, over the
// embedded one, and names the file it read: "" when the embedded one stands alone.
func LoadSettings(override string) (Settings, string, error) {
	var s Settings
	if err := decodeSettings(asset.DefaultSettings, &s); err != nil {
		return Settings{}, "", fmt.Errorf("embedded %s: %w", SettingsFile, err)
	}
	path := FindFile(ConfigRoots(override), "", SettingsFile)
	if path == "" {
		return s, "", nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		err = decodeSettings(data, &s)
	}
	if err != nil {
		return Settings{}, "", fmt.Errorf("%s: %w", path, err)
	}
	dir, p := filepath.Dir(path), &s.Paths
	for _, v := range []*string{&p.Root, &p.Log, &p.Journal, &p.Music, &p.Content, &p.Keymap} {
		*v = settingsPath(dir, *v)
	}
	// A bare scenario is a name the roots resolve, not a file beside this one.
	if filepath.Base(p.Scenario) != p.Scenario || strings.HasPrefix(p.Scenario, "~") {
		p.Scenario = settingsPath(dir, p.Scenario)
	}
	if err := CheckRoot(p.Root); err != nil {
		return Settings{}, "", fmt.Errorf("%s: paths.root: %w", path, err)
	}
	return s, path, nil
}

func decodeSettings(data []byte, s *Settings) error {
	raw, err := toml.NewParser(data).Parse()
	if err != nil {
		return err
	}
	if key := unknownKey(raw, reflect.TypeFor[Settings]()); key != "" {
		return fmt.Errorf("unknown key %q", key)
	}
	return toml.Decode(raw, s)
}

// unknownKey names the first key raw holds that t has no field for, so a misspelt
// setting is refused rather than left silently at its default.
func unknownKey(raw map[string]any, t reflect.Type) string {
	fields := make(map[string]reflect.Type, t.NumField())
	for i := range t.NumField() {
		fields[t.Field(i).Tag.Get("toml")] = t.Field(i).Type
	}
	for _, k := range slices.Sorted(maps.Keys(raw)) {
		ft, ok := fields[k]
		if !ok {
			return k
		}
		if table, ok := raw[k].(map[string]any); ok && ft.Kind() == reflect.Struct {
			if bad := unknownKey(table, ft); bad != "" {
				return k + "." + bad
			}
		}
	}
	return ""
}

// settingsPath resolves one path value: "~/" is the home directory and a relative
// path is relative to the directory holding the file, never the working directory.
func settingsPath(dir, v string) string {
	switch {
	case v == "" || filepath.IsAbs(v):
		return v
	case v == "~" || strings.HasPrefix(v, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, v[1:])
		}
	}
	return filepath.Join(dir, v)
}
