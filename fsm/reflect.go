package fsm

import (
	"reflect"
	"strings"
	"sync"
)

// fieldIndexCache maps struct type -> lookup key -> field index
// Payload types are a fixed set known at init, so entries are written once
var fieldIndexCache sync.Map // reflect.Type -> map[string]int

func fieldIndex(t reflect.Type) map[string]int {
	if v, ok := fieldIndexCache.Load(t); ok {
		return v.(map[string]int)
	}

	n := t.NumField()
	idx := make(map[string]int, n*2)

	// Go names first, toml tags second: a tag always wins over a field name
	for i := range n {
		if f := t.Field(i); f.IsExported() {
			idx[f.Name] = i
		}
	}
	for i := range n {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		if c := strings.IndexByte(tag, ','); c >= 0 {
			tag = tag[:c]
		}
		if tag == "" {
			continue // ",omitempty" with no name
		}
		idx[tag] = i
	}

	actual, _ := fieldIndexCache.LoadOrStore(t, idx)
	return actual.(map[string]int)
}

// FieldByTag resolves a struct field by toml tag, falling back to the Go field
// name. Replaces per-call linear tag scans in both the machine and the bridge.
// Unlike reflect.Value.FieldByName it does not traverse embedded structs;
// event payloads use no embedding.
func FieldByTag(v reflect.Value, key string) reflect.Value {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	i, ok := fieldIndex(v.Type())[key]
	if !ok {
		return reflect.Value{}
	}
	return v.Field(i)
}

// StructValue dereferences a payload to an addressable-agnostic struct Value
// Returns an invalid Value for nil pointers and non-structs
func StructValue(payload any) reflect.Value {
	v := reflect.ValueOf(payload)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v
}
