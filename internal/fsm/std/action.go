package std

import (
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// === Variable Actions ===

func registerVariableActions[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterAction("SetVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		value := resolveVarValue(m, a.Value, a.SourceVar)
		value = clampValue(value, a.Min, a.Max)
		m.SetVar(a.Name, value)
	})

	m.RegisterAction("IncrementVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		delta := a.Delta
		if delta == 0 && a.SourceVar == "" {
			delta = 1
		}
		delta = resolveVarValue(m, delta, a.SourceVar)
		result := m.IncrementVar(a.Name, delta)
		if a.Min != nil || a.Max != nil {
			m.SetVar(a.Name, clampValue(result, a.Min, a.Max))
		}
	})

	m.RegisterAction("DecrementVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		delta := a.Delta
		if delta == 0 && a.SourceVar == "" {
			delta = 1
		}
		delta = resolveVarValue(m, delta, a.SourceVar)
		result := m.IncrementVar(a.Name, -delta)
		if a.Min != nil || a.Max != nil {
			m.SetVar(a.Name, clampValue(result, a.Min, a.Max))
		}
	})

	m.RegisterAction("MultiplyVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		multiplier := a.Delta
		if multiplier == 0 && a.SourceVar == "" {
			multiplier = 1
		}
		multiplier = resolveVarValue(m, multiplier, a.SourceVar)
		result := m.GetVar(a.Name) * multiplier
		result = clampValue(result, a.Min, a.Max)
		m.SetVar(a.Name, result)
	})

	m.RegisterAction("DivideVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		divisor := a.Delta
		if divisor == 0 && a.SourceVar == "" {
			divisor = 1
		}
		divisor = resolveVarValue(m, divisor, a.SourceVar)
		if divisor == 0 {
			return // No-op on division by zero
		}
		result := m.GetVar(a.Name) / divisor
		result = clampValue(result, a.Min, a.Max)
		m.SetVar(a.Name, result)
	})

	m.RegisterAction("ModuloVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		divisor := resolveVarValue(m, a.Delta, a.SourceVar)
		if divisor == 0 {
			return // No-op on modulo by zero
		}
		result := m.GetVar(a.Name) % divisor
		result = clampValue(result, a.Min, a.Max)
		m.SetVar(a.Name, result)
	})

	m.RegisterAction("ClampVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" {
			return
		}
		m.SetVar(a.Name, clampValue(m.GetVar(a.Name), a.Min, a.Max))
	})

	m.RegisterAction("CopyVar", fsm.Payload[VariableArgs], func(ctx T, a *VariableArgs) {
		if a.Name == "" || a.SourceVar == "" {
			return
		}
		value := clampValue(m.GetVar(a.SourceVar), a.Min, a.Max)
		m.SetVar(a.Name, value)
	})

	m.RegisterAction("RandomVar", compileRandomVar, func(ctx T, ra *RandomVarArgs) {
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
	compile := compileRegionControl(m)

	m.RegisterKeyedAction("SpawnRegion", spawnKeys, compile, func(ctx T, a *RegionControlArgs) {
		if err := m.SpawnRegion(ctx, a.RegionName, a.InitialID); err != nil {
			return
		}
		reconcileSystems(ctx, m, h)
	})

	m.RegisterKeyedAction("TerminateRegion", regionKeys, compile, func(ctx T, a *RegionControlArgs) {
		if m.TerminateRegion(ctx, a.RegionName) == nil {
			reconcileSystems(ctx, m, h)
		}
	})

	m.RegisterKeyedAction("PauseRegion", regionKeys, compile, func(ctx T, a *RegionControlArgs) {
		m.PauseRegion(a.RegionName)
		reconcileSystems(ctx, m, h)
	})

	m.RegisterKeyedAction("ResumeRegion", regionKeys, compile, func(ctx T, a *RegionControlArgs) {
		m.ResumeRegion(a.RegionName)
		reconcileSystems(ctx, m, h)
	})
}

// reconcileSystems sets the toggled systems to what the running regions want
func reconcileSystems[T any](ctx T, m *fsm.Machine[T], h Host[T]) {
	if h.ApplySystems != nil {
		h.ApplySystems(ctx, m.SystemWanted, m.SystemDecided)
	}
}

// === System Control Actions ===

func registerSystemActions[T any](m *fsm.Machine[T], h Host[T]) {
	if !refused(m, h, h.SetSystem == nil, "EnableSystem") {
		m.RegisterAction("EnableSystem", fsm.Payload[SystemControlArgs], func(ctx T, a *SystemControlArgs) {
			if a.SystemName != "" && h.SetSystem != nil {
				h.SetSystem(ctx, a.SystemName, true)
			}
		})
	}

	if !refused(m, h, h.SetSystem == nil, "DisableSystem") {
		m.RegisterAction("DisableSystem", fsm.Payload[SystemControlArgs], func(ctx T, a *SystemControlArgs) {
			if a.SystemName != "" && h.SetSystem != nil {
				h.SetSystem(ctx, a.SystemName, false)
			}
		})
	}

	// Invoked by name after init, reset and capture import, and after each
	// :region op, none of which passes through a region action
	if !refused(m, h, h.ApplySystems == nil, "ReconcileSystems") {
		m.RegisterAction("ReconcileSystems", fsm.Payload[struct{}], func(ctx T, _ *struct{}) {
			reconcileSystems(ctx, m, h)
		})
	}
}

// === Core Actions ===

// refused registers name as refused, reporting so, when its hook is missing and
// the host refuses inert actions
func refused[T any](m *fsm.Machine[T], h Host[T], unhooked bool, name string) bool {
	if unhooked && h.Refuse != "" {
		m.RefuseAction(name, h.Refuse)
		return true
	}
	return false
}

func registerCoreActions[T any](m *fsm.Machine[T], h Host[T]) {
	if refused(m, h, h.Emit == nil, "EmitEvent") {
		return
	}
	m.RegisterKeyedAction("EmitEvent", emitKeys, compileEmitEvent, func(ctx T, a *EmitEventArgs) {
		if h.Emit == nil {
			return
		}
		payload := a.Payload
		if len(a.PayloadVars) > 0 && payload != nil {
			payload = ApplyPayloadVars(m, payload, a.PayloadVars)
		}
		h.Emit(ctx, a.Type, payload)
	})
}

// === Status Actions ===

func registerStatusActions[T any](m *fsm.Machine[T], h Host[T]) {
	m.RegisterAction("StatusToVar", fsm.Payload[StatusToVarArgs], func(ctx T, a *StatusToVarArgs) {
		if a.Key == "" || a.Name == "" || h.StatusInt == nil {
			return
		}
		if v, ok := h.StatusInt(ctx, a.Key); ok {
			m.SetVar(a.Name, v)
		}
	})

	m.RegisterAction("ConfigToVar", compileConfigToVar(h), func(ctx T, a *ConfigToVarArgs[T]) {
		if a.Name == "" || a.Accessor == nil {
			return
		}
		m.SetVar(a.Name, a.Accessor(ctx))
	})
}
