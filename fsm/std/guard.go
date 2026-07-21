package std

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/fsm"
)

// === Core Guard Factories ===

func registerCoreGuards[T any](m *fsm.Machine[T]) {
	// StateTimeExceeds passes once the region has been in its state for 'ms'
	m.RegisterGuardFactory("StateTimeExceeds", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		if args == nil {
			return nil, fmt.Errorf("StateTimeExceeds requires 'ms'")
		}
		raw, present := args["ms"]
		if !present {
			return nil, fmt.Errorf("StateTimeExceeds requires 'ms'")
		}
		ms := ParseIntArg(args, "ms")
		if ms <= 0 {
			return nil, fmt.Errorf("StateTimeExceeds: 'ms' must be a positive integer, got %v", raw)
		}
		duration := time.Duration(ms) * time.Millisecond

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return region.TimeInState >= duration
		}, nil
	})

	// RegionExists reports whether a region is currently active
	m.RegisterGuardFactory("RegionExists", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		regionName, _ := args["region"].(string)
		if regionName == "" {
			return nil, fmt.Errorf("RegionExists requires 'region'")
		}
		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return machine.HasRegion(regionName)
		}, nil
	})

	// VarEquals compares an FSM variable against a literal
	m.RegisterGuardFactory("VarEquals", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		varName, _ := args["var"].(string)
		if varName == "" {
			return nil, fmt.Errorf("VarEquals requires 'var'")
		}
		value := ParseIntArg(args, "value")

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return machine.GetVar(varName) == value
		}, nil
	})

	// VarCompare compares an FSM variable against a literal with an operator
	m.RegisterGuardFactory("VarCompare", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		varName, _ := args["var"].(string)
		if varName == "" {
			return nil, fmt.Errorf("VarCompare requires 'var'")
		}
		op, _ := args["op"].(string)
		value := ParseIntArg(args, "value")

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return CompareInt(machine.GetVar(varName), op, value)
		}, nil
	})

	// VarCompareVar compares two FSM variables
	m.RegisterGuardFactory("VarCompareVar", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		varA, _ := args["var_a"].(string)
		varB, _ := args["var_b"].(string)
		if varA == "" || varB == "" {
			return nil, fmt.Errorf("VarCompareVar requires 'var_a' and 'var_b'")
		}
		op, _ := args["op"].(string)

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return CompareInt(machine.GetVar(varA), op, machine.GetVar(varB))
		}, nil
	})
}

// === Compound Guard Factories ===

func registerCompoundGuards[T any](m *fsm.Machine[T]) {
	m.RegisterGuardFactory("And", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		childGuards, err := resolveChildGuards(machine, args)
		if err != nil {
			return nil, err
		}
		if len(childGuards) == 0 {
			return func(T, *fsm.RegionState, any) bool { return true }, nil
		}
		return func(ctx T, region *fsm.RegionState, payload any) bool {
			for _, g := range childGuards {
				if !g(ctx, region, payload) {
					return false
				}
			}
			return true
		}, nil
	})

	m.RegisterGuardFactory("Or", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		childGuards, err := resolveChildGuards(machine, args)
		if err != nil {
			return nil, err
		}
		if len(childGuards) == 0 {
			return func(T, *fsm.RegionState, any) bool { return true }, nil
		}
		return func(ctx T, region *fsm.RegionState, payload any) bool {
			for _, g := range childGuards {
				if g(ctx, region, payload) {
					return true
				}
			}
			return false
		}, nil
	})
}

// === Static Guards ===

func registerStaticGuards[T any](m *fsm.Machine[T]) {
	m.RegisterGuard("AlwaysTrue", func(ctx T, region *fsm.RegionState, payload any) bool {
		return true
	})

	m.RegisterGuard("StateTimeExceeds10s", func(ctx T, region *fsm.RegionState, payload any) bool {
		return region.TimeInState > 10*time.Second
	})

	m.RegisterGuard("StateTimeExceeds2s", func(ctx T, region *fsm.RegionState, payload any) bool {
		return region.TimeInState > 2*time.Second
	})
}

// === Guard Composition Helpers ===

// resolveChildGuards compiles nested guard definitions from a compound guard's args
func resolveChildGuards[T any](m *fsm.Machine[T], args map[string]any) ([]fsm.GuardFunc[T], error) {
	guardsRaw, ok := args["guards"]
	if !ok {
		return nil, nil
	}

	guardsList, ok := guardsRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("compound guard: 'guards' must be an array")
	}

	result := make([]fsm.GuardFunc[T], 0, len(guardsList))
	for i, item := range guardsList {
		guardDef, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("compound guard: entry %d is not a table", i)
		}

		name, _ := guardDef["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("compound guard: entry %d missing 'name'", i)
		}

		var childArgs map[string]any
		if argsRaw, ok := guardDef["args"]; ok {
			childArgs, _ = argsRaw.(map[string]any)
		}

		g, err := ResolveGuard(m, name, childArgs)
		if err != nil {
			return nil, fmt.Errorf("compound guard '%s': %w", name, err)
		}

		result = append(result, g)
	}
	return result, nil
}

// ResolveGuard builds a guard from a registered factory or static guard by name
// Exported for embedders composing guards outside the standard library
func ResolveGuard[T any](m *fsm.Machine[T], name string, args map[string]any) (fsm.GuardFunc[T], error) {
	if factory, ok := m.GetGuardFactory(name); ok {
		return factory(m, args)
	}
	if guard, ok := m.GetGuard(name); ok {
		return guard, nil
	}
	return nil, fmt.Errorf("unknown guard '%s'", name)
}

// === Arg Helpers ===

// ParseIntArg extracts an int64 from a guard args map
// Handles the numeric types a TOML parser may produce, plus the string "0"
func ParseIntArg(args map[string]any, key string) int64 {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case string:
		if val == "0" {
			return 0
		}
	}
	return 0
}

// CompareInt applies a named comparison operator; an unknown op means equality
func CompareInt(current int64, op string, value int64) bool {
	switch op {
	case "eq":
		return current == value
	case "neq":
		return current != value
	case "gt":
		return current > value
	case "gte":
		return current >= value
	case "lt":
		return current < value
	case "lte":
		return current <= value
	default:
		return current == value
	}
}

// === Status Guards ===

func registerStatusGuards[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterGuardFactory("StatusBoolEquals", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		key, _ := args["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("StatusBoolEquals requires 'key'")
		}
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}
		if h.StatusBool == nil {
			return alwaysFalse[T](), nil
		}

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			current, ok := h.StatusBool(ctx, key)
			if !ok {
				return false
			}
			return current == expected
		}, nil
	})

	m.RegisterGuardFactory("StatusIntCompare", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		key, _ := args["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("StatusIntCompare requires 'key'")
		}
		op, _ := args["op"].(string)
		value := ParseIntArg(args, "value")
		if h.StatusInt == nil {
			return alwaysFalse[T](), nil
		}

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			current, ok := h.StatusInt(ctx, key)
			if !ok {
				return false
			}
			return CompareInt(current, op, value)
		}, nil
	})
}

// === Config Guards ===

func registerConfigGuards[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterGuardFactory("ConfigIntCompare", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("ConfigIntCompare requires 'field'")
		}
		op, _ := args["op"].(string)
		value := ParseIntArg(args, "value")

		if h.ConfigInt == nil {
			return alwaysFalse[T](), nil
		}
		// accessor binds here
		accessor, ok := h.ConfigInt(field)
		if !ok {
			return nil, fmt.Errorf("ConfigIntCompare: unknown field '%s'", field)
		}

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return CompareInt(accessor(ctx), op, value)
		}, nil
	})

	m.RegisterGuardFactory("ConfigBoolCompare", func(machine *fsm.Machine[T], args map[string]any) (fsm.GuardFunc[T], error) {
		field, _ := args["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("ConfigBoolCompare requires 'field'")
		}
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		if h.ConfigBool == nil {
			return alwaysFalse[T](), nil
		}
		accessor, ok := h.ConfigBool(field)
		if !ok {
			return nil, fmt.Errorf("ConfigBoolCompare: unknown field '%s'", field)
		}

		return func(ctx T, region *fsm.RegionState, payload any) bool {
			return accessor(ctx) == expected
		}, nil
	})
}
