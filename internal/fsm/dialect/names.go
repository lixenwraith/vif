package dialect

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/toml"
)

// Names holds the names a document spells enum types' values by. A field of a type
// it holds takes one of that type's names, a mask's a name or a list of them, and
// a variant's one of those its struct's key field picks; each resolves in the raw
// table before the table decodes, so a reordered enum renames nothing it reads.
// A Names is built, then only read.
type Names struct {
	enums map[reflect.Type]*enum
}

// enum is one type's names and the values they stand for, in listing order; a
// variant's live in keyed, by the value of its key
type enum struct {
	what    string // the type's name, for a list too long to print
	names   []string
	values  []int64
	mask    bool // a list of names stands for their union
	numbers bool // a number decodes as it is, while the type's documents migrate
	key     reflect.Type
	keyed   map[int64]*enum
}

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// listMax is the most names an error lists; past it, -schema has them
const listMax = 32

func NewNames() *Names { return &Names{enums: make(map[reflect.Type]*enum)} }

// Clone is n, to build on without changing n
func (n *Names) Clone() *Names { return &Names{enums: maps.Clone(n.enums)} }

// Enum names E's values from zero: names[i] is value i's, and "" names none
func (n *Names) Enum[E integer](names ...string) *Names {
	t := reflect.TypeFor[E]()
	n.enums[t] = dense(t, names)
	return n
}

// Mask names E's bits and the unions a document calls by name; a field takes one
// name or a list of them, which stands for their union
func (n *Names) Mask[E integer](names map[string]E) *Names {
	t := reflect.TypeFor[E]()
	e := &enum{what: t.String(), mask: true}
	for _, name := range slices.SortedFunc(maps.Keys(names), func(a, b string) int {
		return cmp.Or(cmp.Compare(names[a], names[b]), strings.Compare(a, b))
	}) {
		e.names = append(e.names, name)
		e.values = append(e.values, int64(names[name]))
	}
	n.enums[t] = e
	return n
}

// Variant names E's values where the field of type K its struct holds is key:
// there names[i] is value i's. A key with no names takes none.
func (n *Names) Variant[E, K integer](key K, names ...string) *Names {
	t := reflect.TypeFor[E]()
	e := &enum{what: t.String(), key: reflect.TypeFor[K]()}
	if old := n.enums[t]; old != nil {
		*e = *old
	}
	e.keyed = maps.Clone(e.keyed)
	if e.keyed == nil {
		e.keyed = make(map[int64]*enum)
	}
	e.keyed[int64(key)] = dense(t, names)
	n.enums[t] = e
	return n
}

// Numbers lets E's fields take a number as well, while documents move to its names
func (n *Names) Numbers[E integer]() *Names {
	t := reflect.TypeFor[E]()
	e := *n.enums[t]
	e.numbers = true
	n.enums[t] = &e
	return n
}

// Value is the value name stands for among E's names
func (n *Names) Value[E integer](name string) (E, bool) {
	if e := n.enums[reflect.TypeFor[E]()]; e != nil {
		if i := slices.Index(e.names, name); i >= 0 {
			return E(e.values[i]), true
		}
	}
	return 0, false
}

func dense(t reflect.Type, names []string) *enum {
	e := &enum{what: t.String()}
	for i, name := range names {
		if name != "" {
			e.names = append(e.names, name)
			e.values = append(e.values, int64(i))
		}
	}
	return e
}

// NameList is one type's names as -schema lists them: a mask's field takes a list
// of them too, and a variant's names are keyed by the name of its key's value
type NameList struct {
	Type  string              `json:"type"`
	Names []string            `json:"names,omitempty"`
	Mask  bool                `json:"mask,omitempty"`
	Key   string              `json:"key,omitempty"`
	Keyed map[string][]string `json:"keyed,omitempty"`
}

// List lists every type's names, by type
func (n *Names) List() []NameList {
	var out []NameList
	for t, e := range n.enums {
		l := NameList{Type: t.String(), Names: e.names, Mask: e.mask}
		if e.key != nil {
			l.Key = e.key.String()
			l.Keyed = make(map[string][]string, len(e.keyed))
			for k, v := range e.keyed {
				l.Keyed[n.nameOf(e.key, k)] = v.names
			}
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b NameList) int { return strings.Compare(a.Type, b.Type) })
	return out
}

// nameOf is the name value v of type t goes by, else its number
func (n *Names) nameOf(t reflect.Type, v int64) string {
	if e := n.enums[t]; e != nil {
		if i := slices.Index(e.values, v); i >= 0 {
			return e.names[i]
		}
	}
	return strconv.FormatInt(v, 10)
}

// Decode reads raw into a new A as DecodeInto does
func (n *Names) Decode[A any](raw any) (*A, error) {
	a := new(A)
	if err := n.DecodeInto(raw, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DecodeInto reads raw into v, a pointer to a struct, refusing a key no field takes
// and a value its field cannot hold, each named by its path; a field of a type n
// holds takes its names. A nil raw leaves v as it is.
func (n *Names) DecodeInto(raw, v any) error {
	if raw == nil {
		return nil
	}
	if keys := toml.Unmapped(raw, v); len(keys) > 0 {
		return toml.UnknownKeys(keys)
	}
	if t := reflect.TypeOf(v); t != nil && t.Kind() == reflect.Pointer {
		resolved, _, err := n.resolve(raw, t.Elem())
		if err != nil {
			return err
		}
		raw = resolved
	}
	return toml.Decode(raw, v)
}

// resolve is raw, decoding as t, with each name a field takes swapped for its
// value; a table or array it changes is copied, as the caller may hold raw. It
// walks what toml.Decode walks, which reports any mismatch it passes over.
func (n *Names) resolve(raw any, t reflect.Type) (any, bool, error) {
	if e := n.enums[t]; e != nil && e.key == nil {
		return e.resolve(raw, e.numbers)
	}
	switch t.Kind() {
	case reflect.Pointer:
		return n.resolve(raw, t.Elem())
	case reflect.Struct:
		if table, ok := raw.(map[string]any); ok {
			return n.resolveStruct(table, t)
		}
	case reflect.Slice, reflect.Array:
		switch list := raw.(type) {
		case []any:
			return resolveList(list, func(v any) (any, bool, error) { return n.resolve(v, t.Elem()) })
		case []map[string]any:
			return resolveList(list, func(v map[string]any) (map[string]any, bool, error) {
				r, changed, err := n.resolve(v, t.Elem())
				m, _ := r.(map[string]any)
				return m, changed, err
			})
		}
	case reflect.Map:
		if table, ok := raw.(map[string]any); ok && t.Key().Kind() == reflect.String {
			out, copied := table, false
			for k, v := range table {
				r, changed, err := n.resolve(v, t.Elem())
				if err != nil {
					return nil, false, at(k, err)
				}
				if changed && !copied {
					out, copied = maps.Clone(table), true
				}
				if changed {
					out[k] = r
				}
			}
			return out, copied, nil
		}
	}
	return raw, false, nil
}

// resolveStruct resolves each field table sets, a variant's once its key's value
// is known
func (n *Names) resolveStruct(table map[string]any, t reflect.Type) (any, bool, error) {
	out, copied := table, false
	set := func(key string, v any) {
		if !copied {
			out, copied = maps.Clone(table), true
		}
		out[key] = v
	}
	var variants []reflect.StructField
	for f := range t.Fields() {
		key := keyOf(f)
		raw, ok := table[key]
		if key == "" || !ok {
			continue
		}
		if e := n.enums[f.Type]; e != nil && e.key != nil {
			variants = append(variants, f)
			continue
		}
		v, changed, err := n.resolve(raw, f.Type)
		if err != nil {
			return nil, false, at(key, err)
		}
		if changed {
			set(key, v)
		}
	}
	for _, f := range variants {
		key, e := keyOf(f), n.enums[f.Type]
		var pick int64
		for kf := range t.Fields() {
			if kf.Type == e.key {
				pick, _ = integral(out[keyOf(kf)])
			}
		}
		sub := e.keyed[pick]
		if sub == nil {
			if e.numbers && isNumber(table[key]) {
				continue
			}
			return nil, false, at(key, fmt.Errorf("%s takes no names, got %s", n.nameOf(e.key, pick), quoted(table[key])))
		}
		v, changed, err := sub.resolve(table[key], e.numbers)
		if err != nil {
			return nil, false, at(key, err)
		}
		if changed {
			set(key, v)
		}
	}
	return out, copied, nil
}

// resolveList resolves each item of list, copying list once one changes
func resolveList[T any](list []T, resolve func(T) (T, bool, error)) (any, bool, error) {
	var out []T
	for i, item := range list {
		r, changed, err := resolve(item)
		if err != nil {
			return nil, false, at("["+strconv.Itoa(i)+"]", err)
		}
		if changed {
			if out == nil {
				out = slices.Clone(list)
			}
			out[i] = r
		}
	}
	if out == nil {
		return list, false, nil
	}
	return out, true, nil
}

// resolve is the value raw names, or raw as it is where a number may stand
func (e *enum) resolve(raw any, numbers bool) (any, bool, error) {
	switch v := raw.(type) {
	case string:
		if i := slices.Index(e.names, v); i >= 0 {
			return e.values[i], true, nil
		}
	case []any:
		if !e.mask {
			break
		}
		var union int64
		for i, item := range v {
			name, _ := item.(string)
			j := slices.Index(e.names, name)
			if j < 0 {
				return nil, false, at("["+strconv.Itoa(i)+"]", e.refuse(item))
			}
			union |= e.values[j]
		}
		return union, true, nil
	default:
		if numbers && isNumber(raw) {
			return raw, false, nil
		}
	}
	return nil, false, e.refuse(raw)
}

func (e *enum) refuse(raw any) error {
	list := strings.Join(e.names, ", ")
	if len(e.names) > listMax {
		list = fmt.Sprintf("the %d names of %s", len(e.names), e.what)
	}
	if name, ok := raw.(string); ok {
		return fmt.Errorf("%.32q is not one of %s", name, list)
	}
	if e.mask {
		return fmt.Errorf("want one of %s, or a list of them, got %T", list, raw)
	}
	return fmt.Errorf("want one of %s, got %T", list, raw)
}

func isNumber(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// integral is v as a whole number, if it is one
func integral(v any) (int64, bool) {
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(r.Uint()), r.Uint() <= math.MaxInt64
	case reflect.Float32, reflect.Float64:
		f := r.Float()
		return int64(f), f == math.Trunc(f) && f >= -0x1p63 && f < 0x1p63
	}
	return 0, false
}

func quoted(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%.32q", s)
	}
	return fmt.Sprintf("%T", v)
}

// pathError names where a name failed by the key path a document spells, as a
// decode's errors do: visual.box_style, rooms[0].type
type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.path + ": " + e.err.Error() }

func (e *pathError) Unwrap() error { return e.err }

func at(segment string, err error) error {
	p, ok := err.(*pathError)
	if !ok {
		return &pathError{segment, err}
	}
	if p.path[0] == '[' {
		p.path = segment + p.path
	} else {
		p.path = segment + "." + p.path
	}
	return p
}
