//go:build !vif_headless && !vif_noaudio && !wasm

package stapes

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// tagCache maps a type's toml keys to field indices: the command paths address
// the key space the struct tags define, by a reflection walk, as audio.checkKeys
// does, rather than a setter table that would restate it and drift. The editor
// is single-goroutine, so a plain map.
var tagCache = map[reflect.Type]map[string]int{}

func tagIndex(t reflect.Type) map[string]int {
	if m, ok := tagCache[t]; ok {
		return m
	}
	m := make(map[string]int, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported: not part of the key space
		}
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		m[name] = i
	}
	tagCache[t] = m
	return m
}

// keysOf enumerates a struct's toml keys, sorted. Drives both the "did you
// mean" tail of a resolve error and TUI completion.
func keysOf(t reflect.Type) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	m := tagIndex(t)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// resolve walks a dotted path from a struct pointer to the addressed value. create
// allocates nil pointers on the way down, so `set x.source.vibrato.rate` works on
// a spec with no vibrato table; a read walk stops at the first nil and says
// "unset", since show must not invent structure that save would then emit.
func resolve(v reflect.Value, path []string, create bool) (reflect.Value, error) {
	at := ""
	for i, seg := range path {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !create {
					return reflect.Value{}, fmt.Errorf("%s: unset", pathOr(at))
				}
				if !v.CanSet() {
					return reflect.Value{}, fmt.Errorf("%s: not addressable", pathOr(at))
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			idx, ok := tagIndex(v.Type())[seg]
			if !ok {
				return reflect.Value{}, fmt.Errorf("%s: no key %q; have %s",
					pathOr(at), seg, strings.Join(keysOf(v.Type()), " "))
			}
			v = v.Field(idx)
		case reflect.Slice, reflect.Array:
			n, err := strconv.Atoi(seg)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("%s: %q is not an index; want 0..%d",
					pathOr(at), seg, v.Len()-1)
			}
			if n < 0 || n >= v.Len() {
				return reflect.Value{}, fmt.Errorf("%s: index %d out of range, len %d",
					pathOr(at), n, v.Len())
			}
			v = v.Index(n)
		default:
			return reflect.Value{}, fmt.Errorf("%s: %s is a leaf, cannot descend to %q",
				pathOr(at), v.Kind(), seg)
		}
		at = strings.Join(path[:i+1], ".")
	}
	return v, nil
}

// setLeaf parses s against the target kind. Every domain bound — Nyquist,
// gain range, humanize [0,1] — belongs to ValidateSound / ValidatePattern and is
// checked at apply. This rejects only what reflection cannot represent: bad
// syntax, width overflow, non-finite floats.
func setLeaf(v reflect.Value, s string) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("want a bool, got %q", s)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("want an int%d, got %q", v.Type().Bits(), s)
		}
		v.SetInt(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("want a float, got %q", s)
		}
		// A NaN reaching a biquad is the one input ValidateSound's finite()
		// checks exist to stop; refuse it at the keyboard, not at apply.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("non-finite value %q", s)
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("%s is not a settable leaf", v.Kind())
	}
	return nil
}

// formatLeaf renders a value for show. Composites report shape rather than
// contents; `show` on a composite is a navigation aid, and the full dump is
// what MarshalSounds is for.
func formatLeaf(v reflect.Value) string {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "<unset>"
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String())
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())
	case reflect.Slice, reflect.Array:
		return fmt.Sprintf("<%d %s>", v.Len(), v.Type().Elem().Name())
	case reflect.Struct:
		return "{" + strings.Join(keysOf(v.Type()), " ") + "}"
	}
	return v.Kind().String()
}

// addAt inserts a zero element at index at of the addressed list, appending
// when at is negative. The zero value is what the loader produces for an empty
// table: immediately settable, and immediately invalid to render — the correct
// state for a half-authored layer.
func addAt(root any, path []string, at int) (int, error) {
	v, err := resolve(reflect.ValueOf(root), path, true)
	if err != nil {
		return 0, err
	}
	if v.Kind() != reflect.Slice {
		return 0, fmt.Errorf("%s: %s is not a list", pathOr(strings.Join(path, ".")), v.Kind())
	}
	if at < 0 {
		at = v.Len()
	}
	if at > v.Len() {
		return 0, fmt.Errorf("%s: index %d out of range, 0 to %d", pathOr(strings.Join(path, ".")), at, v.Len())
	}
	insertElem(v, at, reflect.Zero(v.Type().Elem()))
	return at, nil
}

// dupAt inserts a copy of the addressed element at index at of its list, right
// after it when at is negative; the copy shares no storage with it, so an edit
// of one leaves the other
func dupAt(root any, path []string, at int) (int, error) {
	v, n, err := elemAt(root, path)
	if err != nil {
		return 0, err
	}
	if at < 0 {
		at = n + 1
	}
	if at > v.Len() {
		return 0, fmt.Errorf("%s: index %d out of range, 0 to %d", pathOr(strings.Join(path[:len(path)-1], ".")), at, v.Len())
	}
	insertElem(v, at, deepCopy(v.Index(n)))
	return at, nil
}

// moveAt moves the addressed element to index to of its list, the elements
// between shifting one place toward where it was
func moveAt(root any, path []string, to int) error {
	v, n, err := elemAt(root, path)
	if err != nil {
		return err
	}
	if to < 0 || to >= v.Len() {
		return fmt.Errorf("%s: index %d out of range, 0 to %d", pathOr(strings.Join(path[:len(path)-1], ".")), to, v.Len()-1)
	}
	e := reflect.New(v.Type().Elem()).Elem()
	e.Set(v.Index(n))
	if n < to {
		reflect.Copy(v.Slice(n, to), v.Slice(n+1, to+1))
	} else {
		reflect.Copy(v.Slice(to+1, n+1), v.Slice(to, n))
	}
	v.Index(to).Set(e)
	return nil
}

func insertElem(v reflect.Value, at int, e reflect.Value) {
	v.Set(reflect.Append(v, e))
	reflect.Copy(v.Slice(at+1, v.Len()), v.Slice(at, v.Len()-1))
	v.Index(at).Set(e)
}

// deepCopy copies through pointers, structs and slices, the shapes a spec is
// built of
func deepCopy(v reflect.Value) reflect.Value {
	n := reflect.New(v.Type()).Elem()
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		n = reflect.New(v.Type().Elem())
		n.Elem().Set(deepCopy(v.Elem()))
	case reflect.Struct:
		n.Set(v)
		for i := range v.NumField() {
			if n.Field(i).CanSet() {
				n.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		n = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			n.Index(i).Set(deepCopy(v.Index(i)))
		}
	default:
		n.Set(v)
	}
	return n
}

// delAt removes one element; the final segment must be an index.
func delAt(root any, path []string) error {
	v, n, err := elemAt(root, path)
	if err != nil {
		return err
	}
	v.Set(reflect.AppendSlice(v.Slice(0, n), v.Slice(n+1, v.Len())))
	return nil
}

// elemAt resolves a path to a list element: the list and the element's index
func elemAt(root any, path []string) (reflect.Value, int, error) {
	if len(path) == 0 {
		return reflect.Value{}, 0, fmt.Errorf("path must address a list element")
	}
	n, err := strconv.Atoi(path[len(path)-1])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("%q is not an index", path[len(path)-1])
	}
	v, err := resolve(reflect.ValueOf(root), path[:len(path)-1], false)
	if err != nil {
		return reflect.Value{}, 0, err
	}
	if v.Kind() != reflect.Slice {
		return reflect.Value{}, 0, fmt.Errorf("%s: %s is not a list", pathOr(strings.Join(path[:len(path)-1], ".")), v.Kind())
	}
	if n < 0 || n >= v.Len() {
		return reflect.Value{}, 0, fmt.Errorf("%s: index %d out of range, len %d",
			pathOr(strings.Join(path[:len(path)-1], ".")), n, v.Len())
	}
	return v, n, nil
}

func pathOr(s string) string {
	if s == "" {
		return "root"
	}
	return s
}
