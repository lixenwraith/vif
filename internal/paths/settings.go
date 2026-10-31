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
	"github.com/lixenwraith/vif/internal/parameter"
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
	// Maximized or Fullscreen the state it opens in, as the flags of those names do;
	// Cell is the zoom a run writes back, 0 for the screen's own
	Presentation struct {
		Window     bool    `toml:"window"`
		Maximized  bool    `toml:"maximized"`
		Fullscreen bool    `toml:"fullscreen"`
		Cell       float64 `toml:"cell"`
	} `toml:"presentation"`
	Play Play `toml:"play"`
}

// Play is the [play] table: how a run starts, and what the configuration menu
// changes in it; a run with a settings file writes it back as it is left.
type Play struct {
	Sfx         bool    `toml:"sfx"`
	Music       bool    `toml:"music"`
	SfxVolume   float64 `toml:"sfx_volume"`
	AutoFire    string  `toml:"auto_fire"`
	Mouse       bool    `toml:"mouse"`
	FollowMouse bool    `toml:"follow_mouse"`
	HUD         bool    `toml:"hud"`
	Relay       bool    `toml:"relay"`
	Wait        bool    `toml:"wait"`
	// MaxFPS caps the frames drawn a second; 0 is the presentation's own rate
	MaxFPS int `toml:"max_fps"`
}

func (p Play) check() error {
	switch p.AutoFire {
	case "off", "main", "on":
	default:
		return fmt.Errorf("play.auto_fire %q is not off, main or on", p.AutoFire)
	}
	if p.SfxVolume < 0 || p.SfxVolume > 1 {
		return fmt.Errorf("play.sfx_volume %g is not between 0 and 1", p.SfxVolume)
	}
	if p.MaxFPS != 0 && (p.MaxFPS < parameter.FrameRateMin || p.MaxFPS > parameter.FrameRateMax) {
		return fmt.Errorf("play.max_fps %d is not 0 or between %d and %d", p.MaxFPS, parameter.FrameRateMin, parameter.FrameRateMax)
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

// UnknownKeys names the keys a settings document holds that this build has no
// field for, every key it does know read by the time this is returned. A run drops
// them from the document it writes back to (DropUnknown); soundlab only names them.
type UnknownKeys = toml.UnknownKeys

// LoadSettings reads the first vif.toml the roots of override hold, over the
// embedded one, and names the file it read: "" when the embedded one stands alone.
// A file naming keys this build does not know fails with UnknownKeys, the settings
// and the file read in full alongside it.
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
	var unknown UnknownKeys
	if err != nil && !errors.As(err, &unknown) {
		return Settings{}, "", fmt.Errorf("%s: %w", path, err)
	}
	s.resolve(filepath.Dir(path))
	if err := CheckRoot(s.Paths.Root); err != nil {
		return Settings{}, "", fmt.Errorf("%s: paths.root: %w", path, err)
	}
	if unknown != nil {
		return s, path, fmt.Errorf("%s: %w", path, unknown)
	}
	return s, path, nil
}

// DropUnknown removes keys from the store's document, each line setting one and
// each table one names with the comments that are theirs (toml Document.Delete),
// every other line kept. It writes only a document that then reads whole, and
// returns the status line saying which it did.
func DropUnknown(s SettingsStore, keys UnknownKeys) string {
	err := errors.New("nowhere to write the settings back")
	if s != nil {
		err = dropUnknown(s, keys)
	}
	if err != nil {
		return "Ignored " + keys.Error() + ", not dropped: " + err.Error()
	}
	return "Dropped " + keys.Error()
}

func dropUnknown(s SettingsStore, keys UnknownKeys) error {
	saving.Lock()
	defer saving.Unlock()
	data, err := s.Load()
	if err != nil {
		return err
	}
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := doc.Delete(k); err != nil {
			return err
		}
	}
	if _, err := ParseSettings(doc.Bytes()); err != nil {
		return err
	}
	return s.Store(doc.Bytes())
}

// ParseSettings reads a settings document over the embedded one, as LoadSettings
// reads a file; a path in it stays as it is written. Unknown keys fail as
// UnknownKeys with the settings read in full.
func ParseSettings(data []byte) (Settings, error) {
	var s Settings
	if err := decodeSettings(asset.DefaultSettings, &s); err != nil {
		return Settings{}, fmt.Errorf("embedded %s: %w", SettingsFile, err)
	}
	if len(data) > 0 {
		if err := decodeSettings(data, &s); err != nil {
			if _, ok := err.(UnknownKeys); ok {
				return s, err
			}
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
	if err != nil && !errors.As(err, new(UnknownKeys)) {
		return nil, err
	}
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return nil, err
	}
	was := reflect.ValueOf(s.Paths)
	s.resolve(dir)
	now := reflect.ValueOf(s.Paths)
	for i := range now.NumField() {
		if w := was.Field(i).String(); w != now.Field(i).String() && !strings.HasPrefix(w, "~") {
			if err := doc.Set(toml.Key{"paths", now.Type().Field(i).Tag.Get("toml")}, now.Field(i).String()); err != nil {
				return nil, err
			}
		}
	}
	return doc.Bytes(), nil
}

// decodeSettings reads data over s; the decoder skips keys s has no field for, which
// are reported once everything known has been read and checked
func decodeSettings(data []byte, s *Settings) error {
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return err
	}
	if err := doc.Decode(s); err != nil {
		return err
	}
	if c := s.Presentation.Cell; c != 0 && (c < parameter.CellLeast || c > parameter.CellMost) {
		return fmt.Errorf("presentation.cell %g is not 0 or between %g and %g", c, parameter.CellLeast, parameter.CellMost)
	}
	if err := s.Play.check(); err != nil {
		return err
	}
	if keys := doc.Unmapped(s); len(keys) > 0 {
		return UnknownKeys(keys)
	}
	return nil
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
// store's document: each such key's value is replaced, the rest of its line kept,
// or the key is added after the table's last key. Every other line stays as it
// stands, comments included, so a value a flag set for one run and the player left
// alone is never written.
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

// SaveCell writes the zoom a host was left at into [presentation] as SavePlay
// writes a [play] value
func SaveCell(s SettingsStore, cell float64) error {
	return saveTable(s, "presentation", map[string]any{"cell": cell})
}

// saveTable sets each of v's keys in table, every other line of the document kept;
// a document that would not then read whole is refused, never stored broken
func saveTable(s SettingsStore, table string, v map[string]any) error {
	saving.Lock()
	defer saving.Unlock()
	data, err := s.Load()
	if err != nil {
		return err
	}
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return fmt.Errorf("[%s] not written: %w", table, err)
	}
	for _, k := range slices.Sorted(maps.Keys(v)) {
		if err := doc.Set(toml.Key{table, k}, v[k]); err != nil {
			return fmt.Errorf("[%s] not written: %w", table, err)
		}
	}
	if _, err := ParseSettings(doc.Bytes()); err != nil {
		return fmt.Errorf("[%s] not written: %w", table, err)
	}
	return s.Store(doc.Bytes())
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
