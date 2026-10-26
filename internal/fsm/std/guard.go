package std

import (
	"errors"
	"fmt"
	"time"

	"github.com/lixenwraith/vif/internal/fsm"
)

// === Core Guards ===

func registerCoreGuards[T any](m *fsm.Machine[T]) {
	// StateTimeExceeds passes once the region has been in its state for 'ms'
	m.RegisterGuard("StateTimeExceeds", func(a *msArgs) (fsm.GuardFunc[T], error) {
		if a.Ms <= 0 {
			return nil, fmt.Errorf("'ms' must be positive, got %d", a.Ms)
		}
		duration := time.Duration(a.Ms) * time.Millisecond
		return func(_ T, region *fsm.RegionState, _ any) bool {
			return region.TimeInState >= duration
		}, nil
	})

	// RegionExists reports whether a region is spawned, paused or not
	m.RegisterGuard("RegionExists", func(a *regionArgs) (fsm.GuardFunc[T], error) {
		if a.Region == "" {
			return nil, errors.New("requires 'region'")
		}
		return func(T, *fsm.RegionState, any) bool { return m.HasRegion(a.Region) }, nil
	})

	// VarEquals compares an FSM variable against a literal
	m.RegisterGuard("VarEquals", func(a *varEqualsArgs) (fsm.GuardFunc[T], error) {
		if a.Var == "" {
			return nil, errors.New("requires 'var'")
		}
		return func(T, *fsm.RegionState, any) bool { return m.GetVar(a.Var) == a.Value }, nil
	})

	// VarCompare compares an FSM variable against a literal with an operator
	m.RegisterGuard("VarCompare", func(a *varCompareArgs) (fsm.GuardFunc[T], error) {
		if a.Var == "" {
			return nil, errors.New("requires 'var'")
		}
		return func(T, *fsm.RegionState, any) bool { return a.Op.Holds(m.GetVar(a.Var), a.Value) }, nil
	})

	// VarCompareVar compares two FSM variables
	m.RegisterGuard("VarCompareVar", func(a *varCompareVarArgs) (fsm.GuardFunc[T], error) {
		if a.VarA == "" || a.VarB == "" {
			return nil, errors.New("requires 'var_a' and 'var_b'")
		}
		return func(T, *fsm.RegionState, any) bool { return a.Op.Holds(m.GetVar(a.VarA), m.GetVar(a.VarB)) }, nil
	})
}

// === Static Guards ===

// static registers a guard that takes no args
func static[T any](m *fsm.Machine[T], name string, g fsm.GuardFunc[T]) {
	m.RegisterGuard(name, func(*struct{}) (fsm.GuardFunc[T], error) { return g, nil })
}

func registerStaticGuards[T any](m *fsm.Machine[T]) {
	static(m, "AlwaysTrue", func(T, *fsm.RegionState, any) bool { return true })
	static(m, "StateTimeExceeds10s", func(_ T, region *fsm.RegionState, _ any) bool {
		return region.TimeInState > 10*time.Second
	})
	static(m, "StateTimeExceeds2s", func(_ T, region *fsm.RegionState, _ any) bool {
		return region.TimeInState > 2*time.Second
	})
}

// === Status Guards ===

func registerStatusGuards[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterGuard("StatusBoolEquals", func(a *statusBoolArgs) (fsm.GuardFunc[T], error) {
		if a.Key == "" {
			return nil, errors.New("requires 'key'")
		}
		if h.StatusBool == nil {
			return alwaysFalse[T](), nil
		}
		expected := a.Value == nil || *a.Value
		return func(ctx T, _ *fsm.RegionState, _ any) bool {
			current, ok := h.StatusBool(ctx, a.Key)
			return ok && current == expected
		}, nil
	})

	// since names a variable StatusToVar filled, so a script counts from its own
	// mark while the status key keeps its owner's total
	m.RegisterGuard("StatusIntCompare", func(a *statusIntArgs) (fsm.GuardFunc[T], error) {
		if a.Key == "" {
			return nil, errors.New("requires 'key'")
		}
		if h.StatusInt == nil {
			return alwaysFalse[T](), nil
		}
		return func(ctx T, _ *fsm.RegionState, _ any) bool {
			current, ok := h.StatusInt(ctx, a.Key)
			if !ok {
				return false
			}
			if a.Since != "" {
				current -= m.GetVar(a.Since)
			}
			return a.Op.Holds(current, a.Value)
		}, nil
	})
}

// === Config Guards ===

func registerConfigGuards[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterGuard("ConfigIntCompare", func(a *fieldCompareArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" {
			return nil, errors.New("requires 'field'")
		}
		if h.ConfigInt == nil {
			return alwaysFalse[T](), nil
		}
		// accessor binds here
		accessor, ok := h.ConfigInt(a.Field)
		if !ok {
			return nil, fmt.Errorf("unknown field '%s'", a.Field)
		}
		return func(ctx T, _ *fsm.RegionState, _ any) bool { return a.Op.Holds(accessor(ctx), a.Value) }, nil
	})

	m.RegisterGuard("ConfigBoolCompare", func(a *fieldBoolArgs) (fsm.GuardFunc[T], error) {
		if a.Field == "" {
			return nil, errors.New("requires 'field'")
		}
		if h.ConfigBool == nil {
			return alwaysFalse[T](), nil
		}
		accessor, ok := h.ConfigBool(a.Field)
		if !ok {
			return nil, fmt.Errorf("unknown field '%s'", a.Field)
		}
		expected := a.Value == nil || *a.Value
		return func(ctx T, _ *fsm.RegionState, _ any) bool { return accessor(ctx) == expected }, nil
	})
}
