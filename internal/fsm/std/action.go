package std

import (
	"errors"

	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// === Variable Actions ===

func registerVariableActions[T any](m *fsm.Machine[T], h Host[T]) {
	for _, name := range []string{
		"SetVar", "IncrementVar", "DecrementVar", "MultiplyVar",
		"DivideVar", "ModuloVar", "ClampVar", "CopyVar",
	} {
		m.RegisterActionArgs(name, decodePayload[T, VariableArgs])
	}

	m.RegisterAction("SetVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		value := resolveVarValue(m, varArgs.Value, varArgs.SourceVar)
		value = clampValue(value, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, value)
	})

	m.RegisterAction("IncrementVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		delta := varArgs.Delta
		if delta == 0 && varArgs.SourceVar == "" {
			delta = 1
		}
		delta = resolveVarValue(m, delta, varArgs.SourceVar)
		result := m.IncrementVar(varArgs.Name, delta)
		if varArgs.Min != nil || varArgs.Max != nil {
			m.SetVar(varArgs.Name, clampValue(result, varArgs.Min, varArgs.Max))
		}
	})

	m.RegisterAction("DecrementVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		delta := varArgs.Delta
		if delta == 0 && varArgs.SourceVar == "" {
			delta = 1
		}
		delta = resolveVarValue(m, delta, varArgs.SourceVar)
		result := m.IncrementVar(varArgs.Name, -delta)
		if varArgs.Min != nil || varArgs.Max != nil {
			m.SetVar(varArgs.Name, clampValue(result, varArgs.Min, varArgs.Max))
		}
	})

	m.RegisterAction("MultiplyVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		multiplier := varArgs.Delta
		if multiplier == 0 && varArgs.SourceVar == "" {
			multiplier = 1
		}
		multiplier = resolveVarValue(m, multiplier, varArgs.SourceVar)
		result := m.GetVar(varArgs.Name) * multiplier
		result = clampValue(result, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, result)
	})

	m.RegisterAction("DivideVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		divisor := varArgs.Delta
		if divisor == 0 && varArgs.SourceVar == "" {
			divisor = 1
		}
		divisor = resolveVarValue(m, divisor, varArgs.SourceVar)
		if divisor == 0 {
			return // No-op on division by zero
		}
		result := m.GetVar(varArgs.Name) / divisor
		result = clampValue(result, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, result)
	})

	m.RegisterAction("ModuloVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		divisor := resolveVarValue(m, varArgs.Delta, varArgs.SourceVar)
		if divisor == 0 {
			return // No-op on modulo by zero
		}
		result := m.GetVar(varArgs.Name) % divisor
		result = clampValue(result, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, result)
	})

	m.RegisterAction("ClampVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		m.SetVar(varArgs.Name, clampValue(m.GetVar(varArgs.Name), varArgs.Min, varArgs.Max))
	})

	m.RegisterAction("CopyVar", func(ctx T, args any) {
		varArgs, ok := args.(*VariableArgs)
		if !ok || varArgs.Name == "" || varArgs.SourceVar == "" {
			return
		}
		value := clampValue(m.GetVar(varArgs.SourceVar), varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, value)
	})

	m.RegisterActionArgs("RandomVar", compileRandomVar[T])
	m.RegisterAction("RandomVar", func(ctx T, args any) {
		ra, ok := args.(*RandomVarArgs)
		if !ok {
			return
		}
		var root uint64
		if h.Seed != nil {
			root = h.Seed(ctx)
		}
		var seed int64
		if ra.SeedVar != "" {
			seed = m.GetVar(ra.SeedVar)
		}
		m.SetVar(ra.Name, drawInRange(root, seed, m.GetVar(ra.IndexVar), ra.Min, ra.Max))
	})
}

// drawInRange hashes (root, seed, index) into [lo, hi]. The span is unsigned, so it
// wraps to zero only for the whole int64 range, and the sum wraps back inside it.
func drawInRange(root uint64, seed, index, lo, hi int64) int64 {
	x := vmath.MixChain(root, uint64(seed), uint64(index))
	span := uint64(hi) - uint64(lo) + 1
	if span == 0 {
		return int64(x)
	}
	return int64(uint64(lo) + x%span)
}

// resolveVarValue returns the source var value when sourceVar is set, else the literal
func resolveVarValue[T any](m *fsm.Machine[T], literal int64, sourceVar string) int64 {
	if sourceVar != "" {
		return m.GetVar(sourceVar)
	}
	return literal
}

// clampValue applies optional min/max bounds
func clampValue(value int64, minVal, maxVal *int64) int64 {
	if minVal != nil && value < *minVal {
		value = *minVal
	}
	if maxVal != nil && value > *maxVal {
		value = *maxVal
	}
	return value
}

// === Region Control Actions ===

func registerRegionActions[T any](m *fsm.Machine[T], h Host[T]) {
	for _, name := range []string{"SpawnRegion", "TerminateRegion", "PauseRegion", "ResumeRegion"} {
		m.RegisterActionArgs(name, compileRegionControl[T])
	}

	m.RegisterAction("SpawnRegion", func(ctx T, args any) {
		rcArgs, ok := args.(*RegionControlArgs)
		if !ok {
			return
		}
		if err := m.SpawnRegion(ctx, rcArgs.RegionName, rcArgs.InitialID); err != nil {
			return
		}
		applyRegionSystemConfig(ctx, m, h, rcArgs.RegionName)
	})

	m.RegisterAction("TerminateRegion", func(ctx T, args any) {
		rcArgs, ok := args.(*RegionControlArgs)
		if !ok {
			return
		}
		_ = m.TerminateRegion(ctx, rcArgs.RegionName)
	})

	m.RegisterAction("PauseRegion", func(ctx T, args any) {
		rcArgs, ok := args.(*RegionControlArgs)
		if !ok {
			return
		}
		m.PauseRegion(rcArgs.RegionName)
	})

	m.RegisterAction("ResumeRegion", func(ctx T, args any) {
		rcArgs, ok := args.(*RegionControlArgs)
		if !ok {
			return
		}
		m.ResumeRegion(rcArgs.RegionName)

		// Re-apply the region's declared system toggles, as SpawnRegion does
		applyRegionSystemConfig(ctx, m, h, rcArgs.RegionName)
	})
}

// applyRegionSystemConfig reconciles systems declared in the region config
func applyRegionSystemConfig[T any](ctx T, m *fsm.Machine[T], h Host[T], regionName string) {
	if h.SetSystem == nil {
		return
	}
	cfg := m.GetRegionConfig(regionName)
	if cfg == nil {
		return
	}
	for _, name := range cfg.DisabledSystems {
		h.SetSystem(ctx, name, false)
	}
	for _, name := range cfg.EnabledSystems {
		h.SetSystem(ctx, name, true)
	}
}

// === System Control Actions ===

func registerSystemActions[T any](m *fsm.Machine[T], h Host[T]) {
	unhooked := h.SetSystem == nil
	hooked(m, h, unhooked, "EnableSystem", decodePayload[T, SystemControlArgs])
	hooked(m, h, unhooked, "DisableSystem", decodePayload[T, SystemControlArgs])
	hooked(m, h, unhooked, "ApplyGlobalSystemConfig", nil)
	hooked(m, h, unhooked, "ApplyRegionSystemConfigs", nil)

	m.RegisterAction("EnableSystem", func(ctx T, args any) {
		sysArgs, ok := args.(*SystemControlArgs)
		if !ok || sysArgs.SystemName == "" || h.SetSystem == nil {
			return
		}
		h.SetSystem(ctx, sysArgs.SystemName, true)
	})

	m.RegisterAction("DisableSystem", func(ctx T, args any) {
		sysArgs, ok := args.(*SystemControlArgs)
		if !ok || sysArgs.SystemName == "" || h.SetSystem == nil {
			return
		}
		h.SetSystem(ctx, sysArgs.SystemName, false)
	})

	// ApplyGlobalSystemConfig applies the root [systems] toggles. Invoked by
	// name after init, reset and capture import, before the per-region pass.
	m.RegisterAction("ApplyGlobalSystemConfig", func(ctx T, args any) {
		if h.SetSystem == nil {
			return
		}
		cfg := m.GetSystemsConfig()
		if cfg == nil {
			return
		}
		for _, name := range cfg.DisabledSystems {
			h.SetSystem(ctx, name, false)
		}
		if cfg.OnlySystems == nil || h.Systems == nil {
			return
		}
		for _, name := range h.Systems(ctx) {
			if !cfg.Enabled(name) {
				h.SetSystem(ctx, name, false)
			}
		}
	})

	// ApplyRegionSystemConfigs reconciles declared toggles for every active
	// region. Invoked by name after init, reset and capture import, following the
	// global pass so per-region declarations win, and after each :region op that
	// changes regions; none of these passes through the SpawnRegion action.
	m.RegisterAction("ApplyRegionSystemConfigs", func(ctx T, args any) {
		if h.SetSystem == nil {
			return
		}
		for _, name := range m.ActiveRegions() {
			applyRegionSystemConfig(ctx, m, h, name)
		}
	})
}

// === Core Actions ===

// hooked registers name's compiler, or one refusing the load when the action's hook
// is missing and the host refuses inert actions
func hooked[T any](m *fsm.Machine[T], h Host[T], unhooked bool, name string, compile fsm.ArgCompiler[T]) {
	if unhooked && h.Refuse != "" {
		compile = func(*fsm.Machine[T], fsm.ActionConfig, fsm.StateResolver) (any, error) {
			return nil, errors.New(h.Refuse)
		}
	}
	if compile != nil {
		m.RegisterActionArgs(name, compile)
	}
}

func registerCoreActions[T any](m *fsm.Machine[T], h Host[T]) {
	hooked(m, h, h.Emit == nil, "EmitEvent", compileEmitEvent[T])
	m.RegisterAction("EmitEvent", func(ctx T, args any) {
		emitArgs, ok := args.(*EmitEventArgs)
		if !ok || h.Emit == nil {
			return
		}
		payload := emitArgs.Payload
		if len(emitArgs.PayloadVars) > 0 && payload != nil {
			payload = ApplyPayloadVars(m, payload, emitArgs.PayloadVars)
		}
		h.Emit(ctx, emitArgs.Type, payload)
	})
}

// === Status Actions ===

func registerStatusActions[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterActionArgs("StatusToVar", decodePayload[T, StatusToVarArgs])
	m.RegisterActionArgs("ConfigToVar", compileConfigToVar(h))

	m.RegisterAction("StatusToVar", func(ctx T, args any) {
		sa, ok := args.(*StatusToVarArgs)
		if !ok || sa.Key == "" || sa.Name == "" || h.StatusInt == nil {
			return
		}
		if v, ok := h.StatusInt(ctx, sa.Key); ok {
			m.SetVar(sa.Name, v)
		}
	})

	m.RegisterAction("ConfigToVar", func(ctx T, args any) {
		ca, ok := args.(*ConfigToVarArgs[T])
		if !ok || ca.Name == "" || ca.Accessor == nil {
			return
		}
		m.SetVar(ca.Name, ca.Accessor(ctx))
	})
}
