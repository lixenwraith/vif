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
	path := FindFile(ConfigRoots(override), "", SettingsFile)
	if path == "" {
		s, err := ParseSettings(nil)
		return s, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, "", err
	}
	s, err := ParseSettings(data)
	if err != nil {
		return Settings{}, "", fmt.Errorf("%s: %w", path, err)
	}
	s.resolve(filepath.Dir(path))
	if err := CheckRoot(s.Paths.Root); err != nil {
		return Settings{}, "", fmt.Errorf("%s: paths.root: %w", path, err)
	}
	return s, path, nil
}

// ParseSettings reads a settings document over the embedded one, as LoadSettings
// reads a file; a path in it stays as it is written.
func ParseSettings(data []byte) (Settings, error) {
	var s Settings
	if err := decodeSettings(asset.DefaultSettings, &s); err != nil {
		return Settings{}, fmt.Errorf("embedded %s: %w", SettingsFile, err)
	}
	if len(data) > 0 {
		if err := decodeSettings(data, &s); err != nil {
			return Settings{}, err
		}
	}
	return s, nil
}

// resolve makes each path what it names from a file in dir; a bare scenario stays
// a name the roots resolve, not a file beside the one read.
func (s *Settings) resolve(dir string) {
	p := &s.Paths
	for _, v := range []*string{&p.Root, &p.Log, &p.Journal, &p.Music, &p.Content, &p.Keymap} {
		*v = settingsPath(dir, *v)
	}
	if filepath.Base(p.Scenario) != p.Scenario || strings.HasPrefix(p.Scenario, "~") {
		p.Scenario = settingsPath(dir, p.Scenario)
	}
}

// rebase makes the relative paths of a settings document read in dir absolute,
// so a copy elsewhere names the same files; "~/" stays as written.
func rebase(data []byte, dir string) ([]byte, error) {
	s, err := ParseSettings(data)
	if err != nil {
		return nil, err
	}
	was := reflect.ValueOf(s.Paths)
	s.resolve(dir)
	now, moved := reflect.ValueOf(s.Paths), make(map[string]string)
	for i := range now.NumField() {
		if w := was.Field(i).String(); w != now.Field(i).String() && !strings.HasPrefix(w, "~") {
			moved[now.Type().Field(i).Tag.Get("toml")] = now.Field(i).String()
		}
	}
	if len(moved) == 0 {
		return data, nil
	}
	body, err := toml.Marshal(moved)
	if err != nil {
		return nil, err
	}
	return setKeys(data, "paths", body), nil
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

// SettingsStore holds the settings document a run writes back for the next run to
// read: a vif.toml, or a browser page's storage. Load returns the document a write
// starts from, nil for an empty one; Store replaces it whole.
type SettingsStore interface {
	Load() ([]byte, error)
	Store([]byte) error
}

// SettingsFor is where a run that read the vif.toml named read ("" for the
// embedded one) writes its settings back: that file when it is the player's own,
// in the override root or the user root, else the user root's, created at the
// first change from the one read. A vif.toml in the checkout or a system root is
// never written; the user root's shadows it from then on. Nil without a user root.
func SettingsFor(override, read string) SettingsStore {
	user := UserRoot()
	if dir := filepath.Dir(read); read != "" && (dir == user || override != "" && dir == filepath.Clean(override)) {
		return FileStore{Path: read}
	}
	if user == "" {
		return nil
	}
	return FileStore{Path: filepath.Join(user, SettingsFile), Seed: read}
}

// FileStore is the vif.toml at Path. One not there yet starts as Seed, the file
// the run read, its relative paths made absolute so they name what they named
// there; with no Seed, as the embedded one, so its comments travel with the
// player's values.
type FileStore struct {
	Path string
	Seed string
}

func (f FileStore) Load() ([]byte, error) {
	data, err := os.ReadFile(f.Path)
	switch {
	case !errors.Is(err, fs.ErrNotExist):
		return data, err
	case f.Seed == "":
		return asset.DefaultSettings, nil
	}
	if data, err = os.ReadFile(f.Seed); err != nil {
		return nil, err
	}
	return rebase(data, filepath.Dir(f.Seed))
}

func (f FileStore) Store(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// saving keeps each write back one load and one store, whichever goroutine writes
var saving sync.Mutex

// SavePlay writes what changed from was to now into the [play] table of the
// store's document: each such key's line is replaced, or added after the table's
// last key. Every other line stays as it stands, comments included, so a value a
// flag set for one run and the player left alone is never written.
func SavePlay(s SettingsStore, was, now Play) error {
	changed := make(map[string]any)
	a, b := reflect.ValueOf(was), reflect.ValueOf(now)
	for i := range b.NumField() {
		if !a.Field(i).Equal(b.Field(i)) {
			changed[b.Type().Field(i).Tag.Get("toml")] = b.Field(i).Interface()
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return saveTable(s, "play", changed)
}

func saveTable(s SettingsStore, table string, v any) error {
	body, err := toml.Marshal(v)
	if err != nil {
		return err
	}
	saving.Lock()
	defer saving.Unlock()
	data, err := s.Load()
	if err != nil {
		return err
	}
	// A document this writer could not edit in place is refused, never stored broken
	out := setKeys(data, table, body)
	if _, err := ParseSettings(out); err != nil {
		return fmt.Errorf("[%s] not written: %w", table, err)
	}
	return s.Store(out)
}

// setKeys writes body's key = value lines into table: the line setting a key is
// replaced, a key the table lacks goes after its last key, or after its header,
// and a table the document lacks is appended. Every other line stays as it is.
func setKeys(doc []byte, table string, body []byte) []byte {
	set := make(map[string]string)
	var keys []string
	for l := range strings.Lines(string(body)) {
		if k, _, ok := strings.Cut(l, "="); ok {
			k = strings.TrimSpace(k)
			set[k], keys = l, append(keys, k)
		}
	}
	lines := slices.Collect(strings.Lines(string(doc)))
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines[n-1] += "\n"
	}
	at, in := -1, false // the table's last key or header; whether a line is inside it
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			header, _, _ := strings.Cut(t, "#")
			header = strings.TrimSpace(header)
			if in = !strings.HasPrefix(header, "[[") && strings.HasSuffix(header, "]") &&
				unquote(header[1:len(header)-1]) == table; in {
				at = i
			}
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if !in || !ok || strings.HasPrefix(t, "#") {
			continue
		}
		at, k = i, unquote(k)
		if line, ok := set[k]; ok {
			lines[i] = line
			delete(set, k)
		}
	}
	var missing []string
	for _, k := range keys {
		if line, ok := set[k]; ok {
			missing = append(missing, line)
		}
	}
	if at < 0 {
		missing = slices.Insert(missing, 0, "["+table+"]\n")
		if len(lines) > 0 {
			missing = slices.Insert(missing, 0, "\n")
		}
		at = len(lines) - 1
	}
	return []byte(strings.Join(slices.Insert(lines, at+1, missing...), ""))
}

// unquote is a bare or quoted key as its name: "play", 'play' and play are play
func unquote(k string) string {
	k = strings.TrimSpace(k)
	if n := len(k); n >= 2 && (k[0] == '"' || k[0] == '\'') && k[n-1] == k[0] {
		return k[1 : n-1]
	}
	return k
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
