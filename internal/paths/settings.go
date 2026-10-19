package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
)

// SettingsFile is the settings document at the top of a configuration root.
const SettingsFile = "vif.toml"

// Settings is vif.toml: the defaults of flags a player would otherwise repeat on
// every run, and the [play] table a run writes back. Paths arrive resolved, so
// each reads as its flag's value would.
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
	// Presentation.Window opens the desktop window by default, as -g does, and
	// Maximized or Fullscreen the state it opens in, as the flags of those names do
	Presentation struct {
		Window     bool `toml:"window"`
		Maximized  bool `toml:"maximized"`
		Fullscreen bool `toml:"fullscreen"`
	} `toml:"presentation"`
	Play Play `toml:"play"`
}

// Play is the [play] table: how a run starts, and what the configuration menu
// changes in it; a run with a settings file writes it back as it is left.
type Play struct {
	Effects     bool    `toml:"effects"`
	Music       bool    `toml:"music"`
	Volume      float64 `toml:"volume"`
	AutoFire    string  `toml:"auto_fire"`
	Mouse       bool    `toml:"mouse"`
	FollowMouse bool    `toml:"follow_mouse"`
	HUD         bool    `toml:"hud"`
}

func (p Play) check() error {
	switch p.AutoFire {
	case "off", "main", "on":
	default:
		return fmt.Errorf("play.auto_fire %q is not off, main or on", p.AutoFire)
	}
	if p.Volume < 0 || p.Volume > 1 {
		return fmt.Errorf("play.volume %g is not between 0 and 1", p.Volume)
	}
	return nil
}

// DefaultPlay is the embedded [play], what a Config naming none starts from.
var DefaultPlay = sync.OnceValue(func() Play {
	var s Settings
	if err := decodeSettings(asset.DefaultSettings, &s); err != nil {
		panic("embedded " + SettingsFile + ": " + err.Error())
	}
	return s.Play
})

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
	if err := toml.Decode(raw, s); err != nil {
		return err
	}
	return s.Play.check()
}

// SaveSettings writes play as the [play] table of the settings file at path and
// keeps every other line as it stands; a file not there yet starts as the embedded
// one, so its comments travel with the player's values.
func SaveSettings(path string, play Play) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = asset.DefaultSettings, nil
	}
	if err != nil {
		return err
	}
	body, err := toml.Marshal(play)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		switch t := strings.TrimSpace(l); {
		case start < 0:
			if t == "[play]" {
				start = i
			}
		case strings.HasPrefix(t, "["):
			end = i
		}
		if end < len(lines) {
			break
		}
	}
	var out strings.Builder
	if start < 0 {
		out.Write(data)
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
		start, end = len(lines), len(lines)
	} else {
		for _, l := range lines[:start] {
			out.WriteString(l)
		}
	}
	out.WriteString("[play]\n")
	out.Write(body)
	if end < len(lines) {
		out.WriteByte('\n')
		for _, l := range lines[end:] {
			out.WriteString(l)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
