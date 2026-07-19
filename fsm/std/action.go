package std

import "github.com/lixenwraith/vi-fighter/fsm"

// === Variable Actions ===

func registerVariableActions[T any](m *fsm.Machine[T]) {
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

		// Re-apply declared system toggles: Pause/Resume bypassed the
		// reconciliation SpawnRegion performs, leaving a paused region's
		// systems stuck past resume
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
	m.RegisterActionArgs("EnableSystem", decodePayload[T, SystemControlArgs])
	m.RegisterActionArgs("DisableSystem", decodePayload[T, SystemControlArgs])

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

	// ApplyRegionSystemConfigs reconciles declared toggles for every
	// active region. Invoked by name after Init and after Reset, where regions
	// come up without passing through SpawnRegion.
	// Runs after ApplyGlobalSystemConfig so per-region declarations win.
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

func registerCoreActions[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterActionArgs("EmitEvent", compileEmitEvent[T])
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
	m.RegisterActionArgs("SetStatusInt", decodePayload[T, StatusIntArgs])
	m.RegisterActionArgs("ResetStatusInt", decodePayload[T, StatusIntArgs])
	m.RegisterActionArgs("ConfigToVar", compileConfigToVar(h))

	m.RegisterAction("SetStatusInt", func(ctx T, args any) {
		sa, ok := args.(*StatusIntArgs)
		if !ok || sa.Key == "" || h.SetStatusInt == nil {
			return
		}
		h.SetStatusInt(ctx, sa.Key, sa.Value)
	})

	m.RegisterAction("ResetStatusInt", func(ctx T, args any) {
		sa, ok := args.(*StatusIntArgs)
		if !ok || sa.Key == "" || h.SetStatusInt == nil {
			return
		}
		h.SetStatusInt(ctx, sa.Key, 0)
	})

	m.RegisterAction("ConfigToVar", func(ctx T, args any) {
		ca, ok := args.(*ConfigToVarArgs[T])
		if !ok || ca.Name == "" || ca.Accessor == nil {
			return
		}
		m.SetVar(ca.Name, ca.Accessor(ctx))
	})
}
