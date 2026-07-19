package std

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/fsm"
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

		setPayloadInt(field, varValue)
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

// setPayloadInt writes an FSM variable value into an int-compatible field
func setPayloadInt(field reflect.Value, value int64) {
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
	}
}

// === Payload Guard Factories ===

func registerPayloadGuards[T any](m *fsm.Machine[T]) {
	m.RegisterGuardFactory("PayloadIntCompare", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("PayloadIntCompare requires 'field'")
		}
		op, _ := args["op"].(string)
		value := ParseIntArg(args, "value")

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			fieldVal, ok := ExtractIntField(payload, field)
			if !ok {
				return false
			}
			return CompareInt(fieldVal, op, value)
		}, nil
	})

	m.RegisterGuardFactory("PayloadBoolEquals", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("PayloadBoolEquals requires 'field'")
		}
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			fieldVal, ok := ExtractBoolField(payload, field)
			if !ok {
				return false
			}
			return fieldVal == expected
		}, nil
	})

	m.RegisterGuardFactory("PayloadStringEquals", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("PayloadStringEquals requires 'field'")
		}
		expected, _ := args["value"].(string)

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			fieldVal, ok := ExtractStringField(payload, field)
			if !ok {
				return false
			}
			return fieldVal == expected
		}, nil
	})

	// PayloadExists reports whether the event carried a payload (useful inside Or)
	m.RegisterGuardFactory("PayloadExists", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return payload != nil
		}, nil
	})
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
