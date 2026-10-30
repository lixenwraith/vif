package resource

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lixenwraith/vif/internal/parameter"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/pkg/position"
)

// EmbeddedLabel is the name recorded for a built-in asset, scenario or corpus.
const EmbeddedLabel = "embedded"

// scenarioMagic identifies the canonical form. scenarioFormat versions the layout
// after it; a reader that does not know a version refuses rather than guesses.
const (
	scenarioMagic  = "VIFSCEN\x00"
	scenarioFormat = uint16(1)
)

// A scenario carries its files and content digest independently of its display name.
type Scenario struct {
	Name        string
	Description string
	// MinPlayers and MaxPlayers are min_players and max_players, how many players,
	// bots counted, the scenario runs with: one to a full roster when undeclared
	MinPlayers, MaxPlayers int
	// MapMinWidth and MapMinHeight are the [map] floor under the map's size, zero
	// when the scenario sets none
	MapMinWidth, MapMinHeight int
	// MapFixedWidth and MapFixedHeight are [map] fixed, the size the scenario says
	// its map holds throughout, for front-ends; its level setups are what hold it
	MapFixedWidth, MapFixedHeight int
	// CursorsOffMap is [map] cursors = false: a player holds a roster slot with no
	// cursor on the map, and plays through the keys and buttons the scenario takes
	CursorsOffMap bool
	// NavRefreshTicks and NavRebuildTicks are [navigation] field_refresh_ticks and
	// route_rebuild_ticks, zero when the scenario sets none
	NavRefreshTicks, NavRebuildTicks int

	entry  string
	files  []scenarioFile
	digest string
}

type scenarioFile struct {
	name string
	data []byte
}

// LoadScenario resolves this run's scenario and reads it whole.
func LoadScenario(o Options) (Scenario, error) {
	if o.Provided != nil {
		return *o.Provided, nil
	}
	entry, err := ScenarioPath(o)
	if err != nil {
		return Scenario{}, err
	}
	if entry == "" {
		return ReadScenario(EmbeddedLabel, asset.MainScenario, asset.MainScenarioEntry)
	}
	dir := filepath.Dir(entry)
	return ReadScenario(filepath.Base(dir), os.DirFS(dir), filepath.Base(entry))
}

// ReadScenario reads a scenario from any filesystem. The file set is the loader's
// own, so a copy holds what the loader would read and nothing that sits beside it.
func ReadScenario(name string, fsys fs.FS, entry string) (Scenario, error) {
	names, err := fsm.ScenarioFiles(fsys, entry)
	if err != nil {
		return Scenario{}, err
	}
	s := Scenario{Name: name, entry: path.Clean(entry), files: make([]scenarioFile, 0, len(names))}
	for _, n := range names {
		data, err := fs.ReadFile(fsys, n)
		if err != nil {
			return Scenario{}, fmt.Errorf("scenario %s: %w", name, err)
		}
		s.files = append(s.files, scenarioFile{name: n, data: data})
	}
	if err := s.validate(); err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", name, err)
	}
	s.digest = digestOf(s.Marshal())
	return s, nil
}

// Entry is the file the FSM loader starts from, relative to FS.
func (s Scenario) Entry() string { return s.entry }

// Digest is the hex SHA-256 of the canonical form. Empty on the zero value.
func (s Scenario) Digest() string { return s.digest }

// Short is the digest prefix a status line or a log record carries.
func (s Scenario) Short() string {
	if len(s.digest) < 12 {
		return s.digest
	}
	return s.digest[:12]
}

// Behaviors resolves the entry and returns its behavior programs by name, each
// file a program names read in place of its table
func (s Scenario) Behaviors() (map[string]map[string]any, error) {
	doc, err := fsm.ResolveScenario(s.FS(), s.entry)
	if err != nil {
		return nil, err
	}
	programs, _ := doc["behaviors"].(map[string]any) // the resolve holds it to a table of tables
	out := make(map[string]map[string]any, len(programs))
	for name, program := range programs {
		out[name] = program.(map[string]any)
	}
	return out, nil
}

// Loot resolves the entry and returns its [loot] kinds and [drops] tables, which
// its programs' compile reads beside them
func (s Scenario) Loot() (map[string]any, error) {
	doc, err := fsm.ResolveScenario(s.FS(), s.entry)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, key := range []string{"loot", "drops"} {
		if v, ok := doc[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}

// Loaded reports whether this is a scenario rather than the zero value.
func (s Scenario) Loaded() bool { return s.digest != "" }

// Files is how many files the scenario is made of.
func (s Scenario) Files() int { return len(s.files) }

// FS serves the scenario's files to the loader without touching a host path.
func (s Scenario) FS() fs.FS { return scenarioFS{files: s.files} }

// Marshal writes the canonical form: the one the digest is taken over and the one
// a transfer carries. One format rather than two, so a copy that verifies is by
// construction the copy that was hashed. Compression belongs to the transport.
func (s Scenario) Marshal() []byte {
	size := len(scenarioMagic) + 2 + 2 + len(s.entry) + 2
	for _, f := range s.files {
		size += 2 + len(f.name) + 4 + len(f.data)
	}
	out := make([]byte, 0, size)
	out = append(out, scenarioMagic...)
	out = binary.BigEndian.AppendUint16(out, scenarioFormat)
	out = binary.BigEndian.AppendUint16(out, uint16(len(s.entry)))
	out = append(out, s.entry...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(s.files)))
	for _, f := range s.files {
		out = binary.BigEndian.AppendUint16(out, uint16(len(f.name)))
		out = append(out, f.name...)
		out = binary.BigEndian.AppendUint32(out, uint32(len(f.data)))
		out = append(out, f.data...)
	}
	return out
}

// Transfers deflate the length-prefixed canonical form; the digest stays over its bytes.
func (s Scenario) MarshalCompressed() ([]byte, error) {
	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(s.Marshal()); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// UnmarshalScenarioCompressed inflates a transferred body and reads it. The inflate
// is bounded at the same ceiling the uncompressed form is held to, so a few
// kilobytes on the wire cannot be made to allocate a megabyte here, and a stream
// that claims to be larger is refused before anything parses it.
func UnmarshalScenarioCompressed(name string, body []byte) (Scenario, error) {
	r := flate.NewReader(bytes.NewReader(body))
	defer r.Close()
	plain, err := io.ReadAll(io.LimitReader(r, parameter.ScenarioMaxBytes+1))
	if err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", name, err)
	}
	if len(plain) > parameter.ScenarioMaxBytes {
		return Scenario{}, fmt.Errorf("scenario %s: inflates past the %d-byte ceiling",
			name, parameter.ScenarioMaxBytes)
	}
	return UnmarshalScenario(name, plain)
}

// UnmarshalScenario reads a canonical form back, under the same bounds a read from
// disk is held to. The name travels beside the body rather than inside it, because
// it is what a root calls this scenario and not part of what the digest answers.
func UnmarshalScenario(name string, body []byte) (Scenario, error) {
	if len(body) > parameter.ScenarioMaxBytes {
		return Scenario{}, fmt.Errorf("scenario %s: %d bytes exceeds the %d-byte ceiling",
			name, len(body), parameter.ScenarioMaxBytes)
	}
	r := reader{buf: body}
	if got := r.take(len(scenarioMagic)); string(got) != scenarioMagic {
		return Scenario{}, fmt.Errorf("scenario %s: not a scenario", name)
	}
	if v := r.u16(); v != scenarioFormat {
		return Scenario{}, fmt.Errorf("scenario %s: format %d, this build reads %d", name, v, scenarioFormat)
	}
	s := Scenario{Name: name}
	s.entry = string(r.take(int(r.u16())))
	count := int(r.u16())
	if count > parameter.ScenarioMaxFiles {
		return Scenario{}, fmt.Errorf("scenario %s: %d files exceeds the %d-file ceiling",
			name, count, parameter.ScenarioMaxFiles)
	}
	s.files = make([]scenarioFile, 0, count)
	for range count {
		f := scenarioFile{}
		f.name = string(r.take(int(r.u16())))
		f.data = r.take(int(r.u32()))
		s.files = append(s.files, f)
	}
	if r.err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", name, r.err)
	}
	if r.left() != 0 {
		return Scenario{}, fmt.Errorf("scenario %s: %d trailing bytes", name, r.left())
	}
	if err := s.validate(); err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", name, err)
	}
	s.digest = digestOf(s.Marshal())
	return s, nil
}

// entryKeys are the top-level keys a scenario entry may hold
var entryKeys = []string{"behaviors", "description", "drops", "loot", "macro_files", "macros", "map",
	"max_players", "min_players", "navigation", "regions", "states", "systems"}

// validate holds every form of a scenario to one rule set: bounded, sorted,
// .toml-only, naming nothing outside itself, its entry holding only entryKeys.
func (s *Scenario) validate() error {
	switch {
	case len(s.files) == 0:
		return errors.New("no files")
	case len(s.files) > parameter.ScenarioMaxFiles:
		return fmt.Errorf("%d files exceeds the %d-file ceiling", len(s.files), parameter.ScenarioMaxFiles)
	}
	total := 0
	for i, f := range s.files {
		if !validScenarioName(f.name) {
			return fmt.Errorf("file %q is not a scenario file name", f.name)
		}
		if i > 0 && s.files[i-1].name >= f.name {
			return fmt.Errorf("file %q is out of order or duplicated", f.name)
		}
		if len(f.data) > parameter.ScenarioMaxFileBytes {
			return fmt.Errorf("file %q is %d bytes, over the %d-byte ceiling",
				f.name, len(f.data), parameter.ScenarioMaxFileBytes)
		}
		total += len(f.data)
	}
	if total > parameter.ScenarioMaxBytes {
		return fmt.Errorf("%d bytes exceeds the %d-byte ceiling", total, parameter.ScenarioMaxBytes)
	}
	if !slices.ContainsFunc(s.files, func(f scenarioFile) bool { return f.name == s.entry }) {
		return fmt.Errorf("entry %q is not one of the files", s.entry)
	}
	data, _ := fs.ReadFile(s.FS(), s.entry)
	doc, err := toml.NewParser(data).Parse()
	if err != nil {
		return fmt.Errorf("entry %q: %w", s.entry, err)
	}
	var unknown toml.UnknownKeys
	for _, key := range slices.Sorted(maps.Keys(doc)) {
		if !slices.Contains(entryKeys, key) {
			unknown = append(unknown, toml.Key{key})
		}
	}
	if unknown != nil {
		return fmt.Errorf("entry %q: %w", s.entry, unknown)
	}
	if description, exists := doc["description"]; exists {
		var ok bool
		s.Description, ok = description.(string)
		if !ok {
			return errors.New("description must be a string")
		}
	}
	if err := s.readPlayers(doc); err != nil {
		return err
	}
	if table, exists := doc["map"]; exists {
		if err := s.readMap(table); err != nil {
			return err
		}
	}
	if table, exists := doc["navigation"]; exists {
		return s.readNavigation(table)
	}
	return nil
}

// readNavigation reads [navigation] field_refresh_ticks and route_rebuild_ticks,
// each a tick count from one to parameter.NavTicksMax
func (s *Scenario) readNavigation(v any) error {
	table, ok := v.(map[string]any)
	if !ok {
		return errors.New("navigation must be a table")
	}
	for key, value := range table {
		var to *int
		switch key {
		case "field_refresh_ticks":
			to = &s.NavRefreshTicks
		case "route_rebuild_ticks":
			to = &s.NavRebuildTicks
		default:
			return fmt.Errorf("navigation.%s is not a navigation setting", key)
		}
		n, ok := value.(int64)
		if !ok || n < 1 || n > parameter.NavTicksMax {
			return fmt.Errorf("navigation.%s must be an integer from 1 to %d", key, parameter.NavTicksMax)
		}
		*to = int(n)
	}
	return nil
}

// readPlayers reads min_players and max_players. A min of 0 declares a game that
// runs with nobody in it; a max of 0 would be one nobody could join.
func (s *Scenario) readPlayers(doc map[string]any) error {
	s.MinPlayers, s.MaxPlayers = 1, parameter.MaxPlayers
	for _, f := range []struct {
		key string
		to  *int
	}{{"min_players", &s.MinPlayers}, {"max_players", &s.MaxPlayers}} {
		value, exists := doc[f.key]
		if !exists {
			continue
		}
		n, ok := value.(int64)
		if !ok || n < 0 || n > parameter.MaxPlayers {
			return fmt.Errorf("%s must be an integer from 0 to %d", f.key, parameter.MaxPlayers)
		}
		*f.to = int(n)
	}
	switch {
	case s.MaxPlayers < 1:
		return errors.New("max_players must be at least 1")
	case s.MaxPlayers < s.MinPlayers:
		return fmt.Errorf("max_players %d is below min_players %d", s.MaxPlayers, s.MinPlayers)
	}
	return nil
}

// readMap reads [map] min_width, min_height, fixed and cursors. A size the grid
// cannot hold is refused rather than clamped: it names what the scenario's layout
// needs, and it arrives from a peer as readily as from disk.
func (s *Scenario) readMap(v any) error {
	table, ok := v.(map[string]any)
	if !ok {
		return errors.New("map must be a table")
	}
	for key, value := range table {
		if key == "cursors" {
			cursors, ok := value.(bool)
			if !ok {
				return errors.New("map.cursors must be true or false")
			}
			s.CursorsOffMap = !cursors
			continue
		}
		if key == "fixed" {
			text, _ := value.(string)
			if _, err := fmt.Sscanf(text, "%dx%d", &s.MapFixedWidth, &s.MapFixedHeight); err != nil ||
				fmt.Sprintf("%dx%d", s.MapFixedWidth, s.MapFixedHeight) != text || s.MapFixedWidth < 1 || s.MapFixedHeight < 1 {
				return errors.New(`map.fixed must be "WxH", positive integers`)
			}
			continue
		}
		n, ok := value.(int64)
		switch {
		case key != "min_width" && key != "min_height":
			return fmt.Errorf("map.%s is not a map setting", key)
		case !ok || n < 1 || n > math.MaxInt32:
			return fmt.Errorf("map.%s must be a positive integer", key)
		case key == "min_width":
			s.MapMinWidth = int(n)
		default:
			s.MapMinHeight = int(n)
		}
	}
	if w, h := position.Clamp(max(s.MapMinWidth, 1), max(s.MapMinHeight, 1)); w < s.MapMinWidth || h < s.MapMinHeight {
		return fmt.Errorf("map floor %dx%d is larger than a map can be", s.MapMinWidth, s.MapMinHeight)
	}
	if s.MapFixedWidth > 0 {
		if w, h := position.Clamp(s.MapFixedWidth, s.MapFixedHeight); w != s.MapFixedWidth || h != s.MapFixedHeight ||
			w < s.MapMinWidth || h < s.MapMinHeight {
			return fmt.Errorf("map.fixed %dx%d is not a map this scenario can have",
				s.MapFixedWidth, s.MapFixedHeight)
		}
	}
	return nil
}

// validScenarioName admits a relative, clean, non-escaping TOML path. Clean
// removes every interior "..", so a cleaned name that still escapes begins with one.
func validScenarioName(name string) bool {
	return name != "" && name == path.Clean(name) && !path.IsAbs(name) &&
		name != ".." && !strings.HasPrefix(name, "../") && path.Ext(name) == ".toml"
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// reader consumes the canonical form, latching the first overrun so each read
// does not have to be checked.
type reader struct {
	buf []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.buf) {
		r.err = io.ErrUnexpectedEOF
		return nil
	}
	out := r.buf[:n:n]
	r.buf = r.buf[n:]
	return out
}

func (r *reader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *reader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *reader) left() int { return len(r.buf) }

// scenarioFS serves a held scenario. ReadFile is the path the loader takes;
// Open exists so this is an fs.FS and behaves if something else reaches for one.
type scenarioFS struct{ files []scenarioFile }

func (f scenarioFS) ReadFile(name string) ([]byte, error) {
	for _, file := range f.files {
		if file.name == name {
			return bytes.Clone(file.data), nil
		}
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

func (f scenarioFS) Open(name string) (fs.File, error) {
	data, err := f.ReadFile(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &scenarioOpenFile{name: name, Reader: bytes.NewReader(data)}, nil
}

type scenarioOpenFile struct {
	*bytes.Reader
	name string
}

func (f *scenarioOpenFile) Stat() (fs.FileInfo, error) { return f, nil }
func (f *scenarioOpenFile) Close() error               { return nil }
func (f *scenarioOpenFile) Name() string               { return path.Base(f.name) }
func (f *scenarioOpenFile) Size() int64                { return f.Reader.Size() }
func (f *scenarioOpenFile) Mode() fs.FileMode          { return 0o444 }
func (f *scenarioOpenFile) ModTime() time.Time         { return time.Time{} }
func (f *scenarioOpenFile) IsDir() bool                { return false }
func (f *scenarioOpenFile) Sys() any                   { return nil }
