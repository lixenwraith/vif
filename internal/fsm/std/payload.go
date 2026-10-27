package std

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/internal/fsm"
)

// === Payload Variable Injection ===

// ApplyPayloadVars injects FSM variable values into payload fields
// Supports dot-path keys for nested struct and slice access: "rooms.0.center_x"
// Keys resolve against toml struct tags first, then Go field names, at each level
// Returns a modified copy; the original payload is unchanged
func ApplyPayloadVars[T any](m *fsm.Machine[T], payload any, vars map[string]string) any {
	if payload == nil || len(vars) == 0 {
		return payload
	}

	pv := reflect.ValueOf(payload)
	if pv.Kind() != reflect.Ptr || pv.IsNil() {
		return payload
	}

	elem := pv.Elem()
	if elem.Kind() != reflect.Struct {
		return payload
	}

	copied := reflect.New(elem.Type()).Elem()
	copied.Set(elem)

	// Track deep-copied slices by path prefix to avoid redundant copies
	copiedSlices := make(map[string]bool)

	for key, varName := range vars {
		varValue := m.GetVar(varName)
		segments := strings.Split(key, ".")

		parent := walkPayloadPath(copied, segments[:len(segments)-1], copiedSlices)
		if !parent.IsValid() {
			continue
		}

		field := fsm.FieldByTag(parent, segments[len(segments)-1])
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		setPayloadVar(field, varValue)
	}

	return copied.Addr().Interface()
}

// walkPayloadPath navigates nested structs and slices along dot-path segments
// Deep-copies slice fields on first encounter to isolate mutations from the original
func walkPayloadPath(current reflect.Value, segments []string, copiedSlices map[string]bool) reflect.Value {
	for i, seg := range segments {
		for current.Kind() == reflect.Ptr {
			if current.IsNil() {
				return reflect.Value{}
			}
			current = current.Elem()
		}

		switch current.Kind() {
		case reflect.Struct:
			field := fsm.FieldByTag(current, seg)
			if !field.IsValid() {
				return reflect.Value{}
			}
			// Deep-copy slice fields before descent to avoid mutating the original backing array
			if field.Kind() == reflect.Slice && field.Len() > 0 {
				pathKey := strings.Join(segments[:i+1], ".")
				if !copiedSlices[pathKey] {
					copiedSlices[pathKey] = true
					newSlice := reflect.MakeSlice(field.Type(), field.Len(), field.Len())
					reflect.Copy(newSlice, field)
					field.Set(newSlice)
				}
			}
			current = field

		case reflect.Slice:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= current.Len() {
				return reflect.Value{}
			}
			current = current.Index(idx)

		default:
			return reflect.Value{}
		}
	}
	return current
}

// setPayloadVar writes an FSM variable value into a field; a string takes its decimal
// form in place of each "{}", so a message can carry a count
func setPayloadVar(field reflect.Value, value int64) {
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value >= 0 {
			field.SetUint(uint64(value))
		}
	case reflect.Float32, reflect.Float64:
		field.SetFloat(float64(value))
	case reflect.Bool:
		field.SetBool(value != 0)
	case reflect.String:
		field.SetString(strings.ReplaceAll(field.String(), "{}", strconv.FormatInt(value, 10)))
	}
}

// === Payload Guards ===

func registerPayloadGuards[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterGuard("PayloadIntCompare", func(a *fieldCompareArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" {
			return nil, errors.New("requires 'field'")
		}
		return func(_ T, _ *fsm.RegionState, payload any) bool {
			v, ok := ExtractIntField(payload, a.Field)
			return ok && a.Op.Holds(v, a.Value)
		}, nil
	})

	// PayloadIntCompareVar compares a payload field with an FSM variable, as an input's
	// tick with the marks a window took
	m.RegisterGuard("PayloadIntCompareVar", func(a *fieldCompareVarArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" || a.Var == "" {
			return nil, errors.New("requires 'field' and 'var'")
		}
		return func(_ T, _ *fsm.RegionState, payload any) bool {
			v, ok := ExtractIntField(payload, a.Field)
			return ok && a.Op.Holds(v, m.GetVar(a.Var))
		}, nil
	})

	m.RegisterGuard("PayloadBoolEquals", func(a *fieldBoolArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" {
			return nil, errors.New("requires 'field'")
		}
		expected := a.Value == nil || *a.Value
		return func(_ T, _ *fsm.RegionState, payload any) bool {
			v, ok := ExtractBoolField(payload, a.Field)
			return ok && v == expected
		}, nil
	})

	m.RegisterGuard("PayloadStringEquals", func(a *fieldStringArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" {
			return nil, errors.New("requires 'field'")
		}
		return func(_ T, _ *fsm.RegionState, payload any) bool {
			v, ok := ExtractStringField(payload, a.Field)
			return ok && v == a.Value
		}, nil
	})

	// PayloadSpeciesIn passes when a payload's species is one of names, resolved at
	// load, so a program that sorts before another, which renumbers it, retargets
	// nothing; a name no species holds fails the load
	m.RegisterGuard("PayloadSpeciesIn", func(a *fieldSpeciesArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" || len(a.Names) == 0 {
			return nil, errors.New("requires 'field' and 'names'")
		}
		if h.Species == nil {
			return alwaysFalse[T](), nil
		}
		ids := make([]int64, len(a.Names))
		for i, name := range a.Names {
			id, ok := h.Species(name)
			if !ok {
				return nil, fmt.Errorf("'%s' is neither a code species nor a behavior program", name)
			}
			ids[i] = id
		}
		return func(_ T, _ *fsm.RegionState, payload any) bool {
			v, ok := ExtractIntField(payload, a.Field)
			return ok && slices.Contains(ids, v)
		}, nil
	})

	// PayloadExists reports whether the event carried a payload (useful inside Or)
	static(m, "PayloadExists", func(_ T, _ *fsm.RegionState, payload any) bool { return payload != nil })
}

// === Payload Field Extraction ===

// ExtractIntField reads an int-compatible payload field by toml tag or Go name
func ExtractIntField(payload any, fieldName string) (int64, bool) {
	v := fsm.StructValue(payload)
	if !v.IsValid() {
		return 0, false
	}

	field := fsm.FieldByTag(v, fieldName)
	if !field.IsValid() {
		return 0, false
	}

	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(field.Uint()), true
	case reflect.Float32, reflect.Float64:
		return int64(field.Float()), true
	default:
		return 0, false
	}
}

// ExtractBoolField reads a bool payload field by toml tag or Go name
func ExtractBoolField(payload any, fieldName string) (bool, bool) {
	v := fsm.StructValue(payload)
	if !v.IsValid() {
		return false, false
	}

	field := fsm.FieldByTag(v, fieldName)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, false
	}
	return field.Bool(), true
}

// ExtractStringField reads a string payload field by toml tag or Go name
func ExtractStringField(payload any, fieldName string) (string, bool) {
	v := fsm.StructValue(payload)
	if !v.IsValid() {
		return "", false
	}

	field := fsm.FieldByTag(v, fieldName)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	return field.String(), true
}
