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
		Root      string `toml:"root"`
		Log       string `toml:"log"`
		Journal   string `toml:"journal"`
		Music     string `toml:"music"`
		Scenario  string `toml:"scenario"`
		Content   string `toml:"content"`
		Keymap    string `toml:"keymap"`
		Downloads string `toml:"downloads"`
	} `toml:"paths"`
	Audio struct {
		BufferMs int `toml:"buffer_ms"`
	} `toml:"audio"`
	// Presentation.Window opens the desktop window by default, as -g does, and
	// Maximized or Fullscreen the state it opens in, as the flags of those names do;
	// Cell is the zoom a run writes back, 0 for the screen's own; TmuxFrameCap draws
	// the terminal no faster than tmux or screen keep up with
	Presentation struct {
		Window       bool    `toml:"window"`
		Maximized    bool    `toml:"maximized"`
		Fullscreen   bool    `toml:"fullscreen"`
		Cell         float64 `toml:"cell"`
		TmuxFrameCap bool    `toml:"tmux_frame_cap"`
	} `toml:"presentation"`
	Play    Play    `toml:"play"`
	Session Session `toml:"session"`
}

// Session is the [session] table: what a run keeps of its sessions past the
// process. ResumeAfterRestart keeps the keys its places are held under in the
// user's state directory (PlacesFile), so a restart, a crash or a reboot comes back
// to the place its Wraith kept; off, they last the run. SaveDownloads keeps a
// scenario a host sends in [paths] downloads; off, it lasts the run.
type Session struct {
	ResumeAfterRestart bool `toml:"resume_after_restart"`
	SaveDownloads      bool `toml:"save_downloads"`
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
	Takeover    bool    `toml:"takeover"`
	Lan         bool    `toml:"lan"`
	Wait        bool    `toml:"wait"`
	// MaxFPS caps the frames drawn a second; 0 is the presentation's own rate
	MaxFPS int `toml:"max_fps"`
	// PauseMenu and PauseCommand pause a game that may pause while the menu or
	// another window, or the command line, is open
	PauseMenu    bool `toml:"pause_menu"`
	PauseCommand bool `toml:"pause_command"`
	// CameraEdge is the percent of the screen's size the view keeps the cursor from
	// each side (engine ConfigResource.EdgeCells)
	CameraEdge int `toml:"camera_edge"`
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
	if p.CameraEdge < 0 || p.CameraEdge > parameter.CameraEdgeMost {
		return fmt.Errorf("play.camera_edge %d is not between 0 and %d", p.CameraEdge, parameter.CameraEdgeMost)
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
// field for, every key it does know read by the time ParseSettings returns it;
// UpgradeSettings drops them.
type UnknownKeys = toml.UnknownKeys

// LoadSettings reads the first vif.toml the roots of override hold over the
// embedded one, as this build brings it up to date (UpgradeSettings), and names
// the file it read, "" when the embedded one stands alone, and each change bringing
// it up to date makes; the file stays as it is, for UpgradeStore to write.
func LoadSettings(override string) (Settings, string, []string, error) {
	path := FindFile(ConfigRoots(override), "", SettingsFile)
	if path == "" {
		s, err := ParseSettings(nil)
		return s, "", nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, "", nil, err
	}
	data, notes, err := UpgradeSettings(data)
	if err != nil {
		return Settings{}, "", nil, fmt.Errorf("%s: %w", path, err)
	}
	s, err := ParseSettings(data)
	if err != nil {
		return Settings{}, "", nil, fmt.Errorf("%s: %w", path, err)
	}
	s.resolve(filepath.Dir(path))
	if err := CheckRoot(s.Paths.Root); err != nil {
		return Settings{}, "", nil, fmt.Errorf("%s: paths.root: %w", path, err)
	}
	return s, path, notes, nil
}

// UpgradeStore brings the document the store holds up to this build's
// (UpgradeSettings), as a run starts by doing, and stores it when that changed it.
// A FileStore whose file is not there yet holds the one it starts from, which is up
// to date, and makes the file at the first change. It returns the status line
// naming each change, "" when there was none.
func UpgradeStore(s SettingsStore) string {
	notes, err := upgradeStore(s)
	switch {
	case err != nil:
		return "Settings not brought up to date: " + err.Error()
	case len(notes) == 0:
		return ""
	}
	return "Settings brought up to date: " + strings.Join(notes, "; ")
}

func upgradeStore(s SettingsStore) ([]string, error) {
	if s == nil {
		return nil, errors.New("nowhere to write them back")
	}
	saving.Lock()
	defer saving.Unlock()
	data, err := s.Load()
	if err != nil {
		return nil, err
	}
	out, notes, err := UpgradeSettings(data)
	if err != nil || len(notes) == 0 {
		return nil, err
	}
	return notes, s.Store(out)
}

// drop removes keys from doc, each with the comments that are its own; one inside
// an inline value goes as that value is written again without it
func drop(doc *toml.Document, keys []toml.Key) error {
	for _, k := range keys {
		if _, err := doc.Delete(k); err != nil && !dropInside(doc, k) {
			return err
		}
	}
	return nil
}

// dropInside writes again, without key, the inline value holding it: that of the
// key above it the document sets. It reports whether one held it.
func dropInside(doc *toml.Document, key toml.Key) bool {
	for i := len(key) - 1; i > 0; i-- {
		if v, ok := valueAt(doc.Tree(), key[:i]); ok && doc.Set(key[:i], without(v, key[i:])) == nil {
			return true
		}
	}
	return false
}

// valueAt is the value tree holds at key
func valueAt(tree map[string]any, key toml.Key) (any, bool) {
	var v any = tree
	for _, k := range key {
		table, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = table[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

// without is a copy of the table v less the key rest names in it
func without(v any, rest toml.Key) any {
	table, ok := v.(map[string]any)
	if !ok {
		return v
	}
	table = maps.Clone(table)
	if len(rest) == 1 {
		delete(table, rest[0])
	} else if sub, ok := table[rest[0]]; ok {
		table[rest[0]] = without(sub, rest[1:])
	}
	return table
}

// UpgradeSettings brings a settings document an earlier build left up to this
// build's: each key this build does not take, or whose value it refuses, goes with
// the comments that are its own, then each key the embedded document sets that it
// lacks comes with its comments where that document puts it (toml Document.Fill).
// Every other line and value stays. It names the changes, counting many adds.
func UpgradeSettings(data []byte) ([]byte, []string, error) {
	doc, err := toml.ParseDocument(data)
	if err != nil {
		return nil, nil, err
	}
	shipped, err := toml.ParseDocument(asset.DefaultSettings)
	if err != nil {
		return nil, nil, err
	}
	var base Settings
	if err := shipped.Decode(&base); err != nil {
		return nil, nil, err
	}
	var notes []string
	if unknown := UnknownKeys(doc.Unmapped(&base)); len(unknown) > 0 {
		if err := drop(doc, unknown); err != nil {
			return nil, nil, err
		}
		notes = append(notes, "dropped "+unknown.Error())
	}
	reset := make(map[string]bool)
	// the embedded document's tables say where a value is a table to walk
	var refused func(tree, shape map[string]any, at toml.Key) error
	refused = func(tree, shape map[string]any, at toml.Key) error {
		for _, k := range slices.Sorted(maps.Keys(tree)) {
			key := append(slices.Clip(at), k)
			sub, table := tree[k].(map[string]any)
			if want, ok := shape[k].(map[string]any); table && ok {
				if err := refused(sub, want, key); err != nil {
					return err
				}
			} else if why := base.refuses(key, tree[k]); why != nil {
				if err := drop(doc, []toml.Key{key}); err != nil {
					return err
				}
				reset[key.String()] = true
				notes = append(notes, "reset to the default: "+why.Error())
			}
		}
		return nil
	}
	if err := refused(doc.Tree(), shipped.Tree(), nil); err != nil {
		return nil, nil, err
	}
	added, err := doc.Fill(shipped)
	if err != nil {
		return nil, nil, err
	}
	switch added = slices.DeleteFunc(added, func(k toml.Key) bool { return reset[k.String()] }); {
	case len(added) > parameter.UpgradeNamed:
		notes = append(notes, fmt.Sprintf("added %d keys", len(added)))
	case len(added) > 0:
		notes = append(notes, "added "+strings.TrimPrefix(UnknownKeys(added).Error(), "unknown "))
	}
	if _, err := ParseSettings(doc.Bytes()); err != nil {
		return nil, nil, err
	}
	return doc.Bytes(), notes, nil
}

// refuses reports why s would refuse value at key, read alone over s
func (s Settings) refuses(key toml.Key, value any) error {
	tree := map[string]any{key[len(key)-1]: value}
	for i := len(key) - 2; i >= 0; i-- {
		tree = map[string]any{key[i]: tree}
	}
	if err := toml.Decode(tree, &s); err != nil {
		return err
	}
	return s.check()
}

// ParseSettings reads a settings document over the embedded one as it is written,
// not brought up to date: a path in it stays as written, a refused value fails, and
// unknown keys fail as UnknownKeys with the settings read in full.
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

// resolve makes each path what it names from a file in dir; a scenario name, bare or
// a path below scenario/, stays a name the roots resolve, as -s takes it.
func (s *Settings) resolve(dir string) {
	p := &s.Paths
	for _, v := range []*string{&p.Root, &p.Log, &p.Journal, &p.Music, &p.Content, &p.Keymap, &p.Downloads} {
		*v = settingsPath(dir, *v)
	}
	if !IsScenarioName(p.Scenario) || strings.HasPrefix(p.Scenario, "~") {
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
	if err := s.check(); err != nil {
		return err
	}
	if keys := doc.Unmapped(s); len(keys) > 0 {
		return UnknownKeys(keys)
	}
	return nil
}

// check refuses a value outside what its key takes
func (s *Settings) check() error {
	if c := s.Presentation.Cell; c != 0 && (c < parameter.CellLeast || c > parameter.CellMost) {
		return fmt.Errorf("presentation.cell %g is not 0 or between %g and %g", c, parameter.CellLeast, parameter.CellMost)
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
	if OwnSettings(override, read) {
		return FileStore{Path: read}
	}
	user := UserRoot()
	if user == "" {
		return nil
	}
	return FileStore{Path: filepath.Join(user, SettingsFile), Seed: read}
}

// OwnRoot reports whether root is one of the player's own, whose files a run writes
// back: the override root or the user root.
func OwnRoot(override, root string) bool {
	return root == UserRoot() || override != "" && root == filepath.Clean(override)
}

// OwnSettings reports whether the vif.toml read ("" for the embedded one) is the
// player's own, at the top of one of their roots, which a run writes back
func OwnSettings(override, read string) bool {
	return read != "" && OwnRoot(override, filepath.Dir(read))
}

// FileStore is the vif.toml at Path, or a keymap a run stores whole. One not there
// yet starts as Seed, the file the run read, brought up to date with its relative
// paths made absolute so they name what they named there; with no Seed, as the
// embedded one, so its comments travel with the player's values.
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
	if data, _, err = UpgradeSettings(data); err != nil {
		return nil, err
	}
	return rebase(data, filepath.Dir(f.Seed))
}

// Store replaces the file whole by rename, through a link to the file it names: a
// file there keeps its mode and, as far as the writer may, its owner, a new one is
// made 0644, and one no one may write is refused. Each write has a temporary file
// of its own, so runs that start together never tear one another's.
func (f FileStore) Store(data []byte) error {
	path, mode := f.Path, fs.FileMode(0o644)
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if path, err = filepath.EvalSymlinks(path); err == nil {
			info, err = os.Stat(path)
		}
		if err != nil {
			return err // a link to nothing stays as it is
		}
	}
	switch {
	case err == nil && info.Mode().Perm()&0o222 == 0:
		return fmt.Errorf("%s is read-only", path)
	case err == nil:
		mode = info.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // nothing once renamed
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Chmod(mode), tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	if info != nil {
		keepOwner(tmp.Name(), info)
	}
	return os.Rename(tmp.Name(), path)
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
