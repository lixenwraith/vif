package toml

import (
	"fmt"
	"reflect"
	"strings"
)

// Unmarshal parses TOML data and stores the result in the value pointed to by v.
func Unmarshal(data []byte, v any) error {
	p := NewParser(data)
	parsedMap, err := p.Parse()
	if err != nil {
		return err
	}
	return Decode(parsedMap, v)
}

// Decode maps a generic map[string]any to a struct/slice/etc using reflection.
// It prioritizes `toml` tags and falls back to field names.
func Decode(data any, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}

	return decodeValue(data, val.Elem())
}

func decodeValue(data any, val reflect.Value) error {
	if data == nil {
		return nil
	}

	switch val.Kind() {
	case reflect.Ptr:
		// Allocate new value of the pointed-to type, decode into it, set pointer
		elemType := val.Type().Elem()
		newVal := reflect.New(elemType)
		if err := decodeValue(data, newVal.Elem()); err != nil {
			return err
		}
		val.Set(newVal)

	case reflect.Struct:
		dataMap, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for struct, got %T", data)
		}
		return decodeStruct(dataMap, val)

	case reflect.Slice:
		dataSlice, ok := data.([]any)
		// TOML parser returns []any for arrays.
		// If it was an array of tables, the parser constructs it as []map[string]any inside the interface{}.
		// We handle the conversion if the underlying type is []map...
		if !ok {
			if mapSlice, ok := data.([]map[string]any); ok {
				// Convert []map[string]any to []any for uniform handling
				dataSlice = make([]any, len(mapSlice))
				for i, m := range mapSlice {
					dataSlice[i] = m
				}
			} else {
				return fmt.Errorf("expected slice, got %T", data)
			}
		}

		newSlice := reflect.MakeSlice(val.Type(), len(dataSlice), len(dataSlice))
		for i := 0; i < len(dataSlice); i++ {
			if err := decodeValue(dataSlice[i], newSlice.Index(i)); err != nil {
				return err
			}
		}
		val.Set(newSlice)

	case reflect.Map:
		// Map[string]T
		if val.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("only map[string]T is supported")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map, got %T", data)
		}

		newMap := reflect.MakeMap(val.Type())
		elemType := val.Type().Elem()

		for k, vData := range dataMap {
			newVal := reflect.New(elemType).Elem()
			if err := decodeValue(vData, newVal); err != nil {
				return fmt.Errorf("map key %s: %w", k, err)
			}
			newMap.SetMapIndex(reflect.ValueOf(k), newVal)
		}
		val.Set(newMap)

	case reflect.Interface:
		// Assign directly if type matches, or minimal conversion
		// This handles 'any' fields (e.g., dynamic payloads)
		val.Set(reflect.ValueOf(data))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Convert generic int or float to specific int
		f, ok := toFloat(data)
		if ok {
			val.SetInt(int64(f))
		} else {
			return fmt.Errorf("cannot convert %T to int", data)
		}

	case reflect.Float32, reflect.Float64:
		f, ok := toFloat(data)
		if ok {
			val.SetFloat(f)
		} else {
			return fmt.Errorf("cannot convert %T to float", data)
		}

	case reflect.String:
		if s, ok := data.(string); ok {
			val.SetString(s)
		} else {
			return fmt.Errorf("cannot convert %T to string", data)
		}

	case reflect.Bool:
		if b, ok := data.(bool); ok {
			val.SetBool(b)
		} else {
			return fmt.Errorf("cannot convert %T to bool", data)
		}
	}

	return nil
}

func decodeStruct(data map[string]any, val reflect.Value) error {
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Determine key name
		key := fieldType.Name
		if tag := fieldType.Tag.Get("toml"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] == "-" {
				continue
			}
			key = parts[0]
		}

		// Look up in data map (case sensitive)
		if vData, ok := data[key]; ok {
			if err := decodeValue(vData, field); err != nil {
				return fmt.Errorf("%s.%s: %w", typ.Name(), fieldType.Name, err)
			}
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch i := v.(type) {
	case int:
		return float64(i), true
	case int8:
		return float64(i), true
	case int16:
		return float64(i), true
	case int32:
		return float64(i), true
	case int64:
		return float64(i), true
	case uint:
		return float64(i), true
	case uint8:
		return float64(i), true
	case uint16:
		return float64(i), true
	case uint32:
		return float64(i), true
	case uint64:
		return float64(i), true
	case float64:
		return i, true
	}
	return 0, false
}