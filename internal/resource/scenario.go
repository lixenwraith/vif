package resource

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/asset"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
)

// EmbeddedLabel is the name recorded for a built-in asset, scenario or corpus.
const EmbeddedLabel = "embedded"

// Bounds on a scenario in any form. They apply to one read from disk as much as
// to one that arrived from a peer: the cost of the second is what they are for,
// and a scenario nobody can transfer is not one worth loading either.
const (
	ScenarioMaxFiles     = 64
	ScenarioMaxFileBytes = 256 << 10
	ScenarioMaxBytes     = 1 << 20
)

// scenarioMagic identifies the canonical form. scenarioFormat versions the layout
// after it; a reader that does not know a version refuses rather than guesses.
const (
	scenarioMagic  = "VIFSCEN\x00"
	scenarioFormat = uint16(1)
)

// Scenario is one playable scenario as a portable artifact: the entry file, the
// region files it includes, and the digest of the canonical form.
//
// Name and digest answer different questions. The name is what a person and a
// config root call it; the digest is whether two participants hold the same
// bytes, which is the only thing a session can be refused on — two roots may
// install the same scenario under different paths, and the same name may cover
// an edit.
type Scenario struct {
	Name string

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
		return ReadScenario(EmbeddedLabel, asset.DefaultScenario, asset.DefaultScenarioEntry)
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

// MarshalCompressed is the canonical form deflated, which is what a transfer
// carries. The whole container is one stream rather than one per file, because the
// files are near-identical TOML and a shared window is most of the saving.
//
// That is safe to do because the container is length-prefixed, not delimited: a
// reader takes exactly the bytes each file declares and never scans for a
// terminator, so a file that is truncated, malformed or deliberately unterminated
// cannot run into the next one. Compression changes the bytes on the wire and
// nothing about the framing, and the digest stays over what Marshal produced, so
// what a receiver verifies is what a sender hashed.
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
	plain, err := io.ReadAll(io.LimitReader(r, ScenarioMaxBytes+1))
	if err != nil {
		return Scenario{}, fmt.Errorf("scenario %s: %w", name, err)
	}
	if len(plain) > ScenarioMaxBytes {
		return Scenario{}, fmt.Errorf("scenario %s: inflates past the %d-byte ceiling",
			name, ScenarioMaxBytes)
	}
	return UnmarshalScenario(name, plain)
}

// UnmarshalScenario reads a canonical form back, under the same bounds a read from
// disk is held to. The name travels beside the body rather than inside it, because
// it is what a root calls this scenario and not part of what the digest answers.
func UnmarshalScenario(name string, body []byte) (Scenario, error) {
	if len(body) > ScenarioMaxBytes {
		return Scenario{}, fmt.Errorf("scenario %s: %d bytes exceeds the %d-byte ceiling",
			name, len(body), ScenarioMaxBytes)
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
	if count > ScenarioMaxFiles {
		return Scenario{}, fmt.Errorf("scenario %s: %d files exceeds the %d-file ceiling",
			name, count, ScenarioMaxFiles)
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

// validate holds every form of a scenario to one rule set: bounded, sorted,
// .toml-only, and naming nothing outside itself.
func (s *Scenario) validate() error {
	switch {
	case len(s.files) == 0:
		return errors.New("no files")
	case len(s.files) > ScenarioMaxFiles:
		return fmt.Errorf("%d files exceeds the %d-file ceiling", len(s.files), ScenarioMaxFiles)
	}
	total := 0
	for i, f := range s.files {
		if !validScenarioName(f.name) {
			return fmt.Errorf("file %q is not a scenario file name", f.name)
		}
		if i > 0 && s.files[i-1].name >= f.name {
			return fmt.Errorf("file %q is out of order or duplicated", f.name)
		}
		if len(f.data) > ScenarioMaxFileBytes {
			return fmt.Errorf("file %q is %d bytes, over the %d-byte ceiling",
				f.name, len(f.data), ScenarioMaxFileBytes)
		}
		total += len(f.data)
	}
	if total > ScenarioMaxBytes {
		return fmt.Errorf("%d bytes exceeds the %d-byte ceiling", total, ScenarioMaxBytes)
	}
	if !slices.ContainsFunc(s.files, func(f scenarioFile) bool { return f.name == s.entry }) {
		return fmt.Errorf("entry %q is not one of the files", s.entry)
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
