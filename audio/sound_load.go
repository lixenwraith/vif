package audio

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"strings"

	"github.com/lixenwraith/toml"
)

// LoadSoundsTOML parses one document. Include directives are rejected here —
// resolving them needs a filesystem capability, so they go through
// LoadSoundsFS. The returned slice holds every sound that validated; err is a
// join of the ones that did not, so one bad spec does not discard the file.
func LoadSoundsTOML(data []byte) ([]*SoundDef, error) {
	f, err := decodeSpecFile(data)
	if err != nil {
		return nil, err
	}
	if len(f.Include) > 0 {
		return nil, errors.New("sound spec: include requires LoadSoundsFS")
	}
	return partitionValid(f.Sound)
}

// LoadSoundsFS loads documents from a filesystem capability, resolving nested
// includes. Includes are loaded depth-first and before the including file's own
// definitions, so an including file overrides what it includes.
func LoadSoundsFS(fsys fs.FS, names ...string) ([]*SoundDef, error) {
	l := &specLoader{fsys: fsys, seen: make(map[string]bool)}
	for _, n := range names {
		if err := l.load(path.Clean(n), 0); err != nil {
			return nil, err
		}
	}
	return partitionValid(l.out)
}

// MarshalSoundsFile re-emits a document with its include directives intact.
// MarshalSounds drops them, which flattens a multi-file spec set on first save.
func MarshalSoundsFile(include []string, defs []*SoundDef) ([]byte, error) {
	return toml.Marshal(SoundSpecFile{Include: include, Sound: defs})
}

// MarshalSounds re-emits specs as TOML. Arrays keep their order; scalar keys
// are sorted. Reloading the output yields identical audio.
func MarshalSounds(defs []*SoundDef) ([]byte, error) {
	return toml.Marshal(SoundSpecFile{Sound: defs})
}

type specLoader struct {
	fsys fs.FS
	seen map[string]bool
	n    int
	out  []*SoundDef
}

func (l *specLoader) load(name string, depth int) error {
	if depth > MaxIncludeDepth {
		return fmt.Errorf("sound spec %q: include depth exceeds %d", name, MaxIncludeDepth)
	}
	if !fs.ValidPath(name) {
		return fmt.Errorf("sound spec: invalid path %q", name)
	}
	if l.seen[name] {
		return fmt.Errorf("sound spec: include cycle at %q", name)
	}
	if l.n++; l.n > MaxSoundIncludes {
		return fmt.Errorf("sound spec: more than %d files", MaxSoundIncludes)
	}
	l.seen[name] = true

	data, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return fmt.Errorf("sound spec %q: %w", name, err)
	}
	f, err := decodeSpecFile(data)
	if err != nil {
		return fmt.Errorf("%q: %w", name, err)
	}
	for _, inc := range f.Include {
		ref, err := resolveInclude(name, inc)
		if err != nil {
			return err
		}
		if err := l.load(ref, depth+1); err != nil {
			return err
		}
	}
	l.out = append(l.out, f.Sound...)
	return nil
}

// resolveInclude keeps every reference inside the supplied fs.FS root.
func resolveInclude(base, ref string) (string, error) {
	if path.IsAbs(ref) || strings.HasPrefix(ref, "..") {
		return "", fmt.Errorf("sound spec %q: include %q escapes root", base, ref)
	}
	p := path.Clean(path.Join(path.Dir(base), ref))
	if !fs.ValidPath(p) {
		return "", fmt.Errorf("sound spec %q: include %q escapes root", base, ref)
	}
	return p, nil
}

// decodeSpecFile parses once to map[string]any, rejects unknown keys, then
// decodes. The decoder ignores unknown keys, which would turn a typo into a
// silent zero value; the strict pass reflects over the struct tags so there is
// no schema to keep in sync.
func decodeSpecFile(data []byte) (*SoundSpecFile, error) {
	raw, err := toml.NewParser(data).Parse()
	if err != nil {
		return nil, fmt.Errorf("sound spec: %w", err)
	}
	if err := checkKeys("", raw, reflect.TypeOf(SoundSpecFile{})); err != nil {
		return nil, fmt.Errorf("sound spec: %w", err)
	}
	var f SoundSpecFile
	if err := toml.Decode(raw, &f); err != nil {
		return nil, fmt.Errorf("sound spec: %w", err)
	}
	return &f, nil
}

func checkKeys(at string, v any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		fields := make(map[string]reflect.Type, t.NumField())
		for f := range t.Fields() {
			if f.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		for k, sub := range m {
			ft, ok := fields[k]
			if !ok {
				return fmt.Errorf("%s: unknown key %q", pathOr(at, "document"), k)
			}
			if err := checkKeys(at+"."+k, sub, ft); err != nil {
				return err
			}
		}
	case reflect.Slice:
		switch a := v.(type) {
		case []any:
			for i, e := range a {
				if err := checkKeys(fmt.Sprintf("%s[%d]", at, i), e, t.Elem()); err != nil {
					return err
				}
			}
		case []map[string]any:
			for i := range a {
				if err := checkKeys(fmt.Sprintf("%s[%d]", at, i), a[i], t.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func pathOr(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return strings.TrimPrefix(s, ".")
}

func partitionValid(in []*SoundDef) ([]*SoundDef, error) {
	out := make([]*SoundDef, 0, len(in))
	var errs []error
	for _, d := range in {
		if d == nil {
			continue
		}
		if err := ValidateSound(d); err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, d)
	}
	return out, errors.Join(errs...)
}
