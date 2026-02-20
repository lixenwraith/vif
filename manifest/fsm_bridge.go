package manifest

import (
	"fmt"
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/fsm"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// RegisterFSMComponents registers all game-specific actions and guards with the FSM
func RegisterFSMComponents(m *fsm.Machine[*engine.World]) {
	registerCoreActions(m)
	registerRegionActions(m)
	registerVariableActions(m)
	registerSystemActions(m)
	registerGuardFactories(m)
	registerStaticGuards(m)
}

// === Core Actions ===

func registerCoreActions(m *fsm.Machine[*engine.World]) {
	m.RegisterAction("EmitEvent", func(world *engine.World, args any) {
		emitArgs, ok := args.(*fsm.EmitEventArgs)
		if !ok {
			return
		}

		payload := emitArgs.Payload

		// Apply variable injection if configured
		if len(emitArgs.PayloadVars) > 0 && payload != nil {
			payload = applyPayloadVars(m, payload, emitArgs.PayloadVars)
		}

		world.PushEvent(emitArgs.Type, payload)
	})
}

// applyPayloadVars injects FSM variable values into payload fields
// Returns a modified copy of the payload (original unchanged)
func applyPayloadVars(m *fsm.Machine[*engine.World], payload any, vars map[string]string) any {
	if payload == nil || len(vars) == 0 {
		return payload
	}

	// Get reflect value, dereference pointer
	pv := reflect.ValueOf(payload)
	if pv.Kind() != reflect.Ptr || pv.IsNil() {
		return payload
	}

	elem := pv.Elem()
	if elem.Kind() != reflect.Struct {
		return payload
	}

	// Create a copy of the struct
	copied := reflect.New(elem.Type()).Elem()
	copied.Set(elem)

	// Apply variable values to specified fields
	for fieldName, varName := range vars {
		field := copied.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		varValue := m.GetVar(varName)

		// Set field based on type
		switch field.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(varValue)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if varValue >= 0 {
				field.SetUint(uint64(varValue))
			}
		case reflect.Float32, reflect.Float64:
			field.SetFloat(float64(varValue))
		}
	}

	return copied.Addr().Interface()
}

// === Region Control Actions ===

func registerRegionActions(m *fsm.Machine[*engine.World]) {
	m.RegisterAction("SpawnRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		initialID, ok := m.GetStateID(rcArgs.InitialState)
		if !ok {
			return
		}
		if err := m.SpawnRegion(world, rcArgs.RegionName, initialID); err != nil {
			return
		}

		// Apply region-specific system toggles
		applyRegionSystemConfig(world, m, rcArgs.RegionName)
	})

	m.RegisterAction("TerminateRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		_ = m.TerminateRegion(world, rcArgs.RegionName)
	})

	m.RegisterAction("PauseRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		m.PauseRegion(rcArgs.RegionName)
	})

	m.RegisterAction("ResumeRegion", func(world *engine.World, args any) {
		rcArgs, ok := args.(*fsm.RegionControlArgs)
		if !ok {
			return
		}
		m.ResumeRegion(rcArgs.RegionName)
	})
}

// applyRegionSystemConfig enables/disables systems based on region config
func applyRegionSystemConfig(world *engine.World, m *fsm.Machine[*engine.World], regionName string) {
	cfg := m.GetRegionConfig(regionName)
	if cfg == nil {
		return
	}

	for _, sysName := range cfg.DisabledSystems {
		world.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
			SystemName: sysName,
			Enabled:    false,
		})
	}

	for _, sysName := range cfg.EnabledSystems {
		world.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
			SystemName: sysName,
			Enabled:    true,
		})
	}
}

// === Variable Actions ===

func registerVariableActions(m *fsm.Machine[*engine.World]) {
	m.RegisterAction("SetVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		value := resolveVarValue(m, varArgs.Value, varArgs.SourceVar)
		value = clampValue(value, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, value)
	})

	m.RegisterAction("IncrementVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
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

	m.RegisterAction("DecrementVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
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

	m.RegisterAction("MultiplyVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
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

	m.RegisterAction("DivideVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
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

	m.RegisterAction("ModuloVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		divisor := varArgs.Delta
		divisor = resolveVarValue(m, divisor, varArgs.SourceVar)
		if divisor == 0 {
			return // No-op on modulo by zero
		}
		result := m.GetVar(varArgs.Name) % divisor
		result = clampValue(result, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, result)
	})

	m.RegisterAction("ClampVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
		if !ok || varArgs.Name == "" {
			return
		}
		result := clampValue(m.GetVar(varArgs.Name), varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, result)
	})

	m.RegisterAction("CopyVar", func(world *engine.World, args any) {
		varArgs, ok := args.(*fsm.VariableArgs)
		if !ok || varArgs.Name == "" || varArgs.SourceVar == "" {
			return
		}
		value := m.GetVar(varArgs.SourceVar)
		value = clampValue(value, varArgs.Min, varArgs.Max)
		m.SetVar(varArgs.Name, value)
	})
}

// resolveVarValue returns source var value if sourceVar is set, otherwise returns literal
func resolveVarValue(m *fsm.Machine[*engine.World], literal int64, sourceVar string) int64 {
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

// === System Control Actions ===

func registerSystemActions(m *fsm.Machine[*engine.World]) {
	m.RegisterAction("EnableSystem", func(world *engine.World, args any) {
		sysArgs, ok := args.(*fsm.SystemControlArgs)
		if !ok || sysArgs.SystemName == "" {
			return
		}
		world.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
			SystemName: sysArgs.SystemName,
			Enabled:    true,
		})
	})

	m.RegisterAction("DisableSystem", func(world *engine.World, args any) {
		sysArgs, ok := args.(*fsm.SystemControlArgs)
		if !ok || sysArgs.SystemName == "" {
			return
		}
		world.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
			SystemName: sysArgs.SystemName,
			Enabled:    false,
		})
	})

	// ApplyGlobalSystemConfig applies root-level [systems] config
	// Called externally during FSM initialization
	m.RegisterAction("ApplyGlobalSystemConfig", func(world *engine.World, args any) {
		cfg := m.GetSystemsConfig()
		if cfg == nil {
			return
		}

		for _, sysName := range cfg.DisabledSystems {
			world.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
				SystemName: sysName,
				Enabled:    false,
			})
		}
	})

	m.RegisterAction("SetStatusInt", func(world *engine.World, args any) {
		statusArgs, ok := args.(*fsm.StatusIntArgs)
		if !ok || statusArgs.Key == "" {
			return
		}
		world.Resources.Status.Ints.Get(statusArgs.Key).Store(statusArgs.Value)
	})

	m.RegisterAction("ResetStatusInt", func(world *engine.World, args any) {
		statusArgs, ok := args.(*fsm.StatusIntArgs)
		if !ok || statusArgs.Key == "" {
			return
		}
		world.Resources.Status.Ints.Get(statusArgs.Key).Store(0)
	})

	m.RegisterAction("ResetKillVars", func(world *engine.World, args any) {
		world.Resources.Status.Ints.Get("kills.drain").Store(0)
		world.Resources.Status.Ints.Get("kills.swarm").Store(0)
		world.Resources.Status.Ints.Get("kills.quasar").Store(0)
		world.Resources.Status.Ints.Get("kills.storm").Store(0)
	})
}

// === Guard Factories ===

func registerGuardFactories(m *fsm.Machine[*engine.World]) {
	// StateTimeExceeds - checks if time in current state exceeds threshold
	m.RegisterGuardFactory("StateTimeExceeds", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		duration := parameter.GameUpdateInterval
		if args != nil {
			if v, ok := args["ms"]; ok {
				switch val := v.(type) {
				case float64:
					duration = time.Duration(val) * time.Millisecond
				case int:
					duration = time.Duration(val) * time.Millisecond
				case int64:
					duration = time.Duration(val) * time.Millisecond
				}
			}
		}
		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return region.TimeInState >= duration
		}
	})

	// StatusBoolEquals - checks status registry bool value
	m.RegisterGuardFactory("StatusBoolEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		key, _ := args["key"].(string)
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			if !world.Resources.Status.Bools.Has(key) {
				return false
			}
			return world.Resources.Status.Bools.Get(key).Load() == expected
		}
	})

	// StatusIntCompare - compares status registry int value
	m.RegisterGuardFactory("StatusIntCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		key, _ := args["key"].(string)
		op, _ := args["op"].(string)
		value := parseIntArg(args, "value")

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			if !world.Resources.Status.Ints.Has(key) {
				return false
			}
			current := world.Resources.Status.Ints.Get(key).Load()
			return compareInt(current, op, value)
		}
	})

	// RegionExists - checks if a region is currently active
	m.RegisterGuardFactory("RegionExists", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		regionName, _ := args["region"].(string)
		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return machine.HasRegion(regionName)
		}
	})

	// VarEquals - checks if FSM variable equals value
	m.RegisterGuardFactory("VarEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		varName, _ := args["var"].(string)
		value := parseIntArg(args, "value")

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return machine.GetVar(varName) == value
		}
	})

	// VarCompare - compares FSM variable with operators
	m.RegisterGuardFactory("VarCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		varName, _ := args["var"].(string)
		op, _ := args["op"].(string)
		value := parseIntArg(args, "value")

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return compareInt(machine.GetVar(varName), op, value)
		}
	})

	// VarCompareVar - compares two FSM variables
	m.RegisterGuardFactory("VarCompareVar", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		varA, _ := args["var_a"].(string)
		varB, _ := args["var_b"].(string)
		op, _ := args["op"].(string)

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return compareInt(machine.GetVar(varA), op, machine.GetVar(varB))
		}
	})

	// ConfigIntCompare - compares ConfigResource int fields
	m.RegisterGuardFactory("ConfigIntCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		field, _ := args["field"].(string)
		op, _ := args["op"].(string)
		value := parseIntArg(args, "value")

		// Validate field at registration time
		validFields := map[string]bool{
			"map_width": true, "map_height": true,
			"viewport_width": true, "viewport_height": true,
			"camera_x": true, "camera_y": true,
			"color_mode": true,
		}
		if !validFields[field] {
			panic(fmt.Sprintf("ConfigIntCompare: unknown field '%s'", field))
		}

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			cfg := world.Resources.Config
			var current int64
			switch field {
			case "map_width":
				current = int64(cfg.MapWidth)
			case "map_height":
				current = int64(cfg.MapHeight)
			case "viewport_width":
				current = int64(cfg.ViewportWidth)
			case "viewport_height":
				current = int64(cfg.ViewportHeight)
			case "camera_x":
				current = int64(cfg.CameraX)
			case "camera_y":
				current = int64(cfg.CameraY)
			case "color_mode":
				current = int64(cfg.ColorMode)
			}
			return compareInt(current, op, value)
		}
	})

	// ConfigBoolCompare - compares ConfigResource bool fields
	m.RegisterGuardFactory("ConfigBoolCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		field, _ := args["field"].(string)
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		// Validate field at registration time
		validFields := map[string]bool{
			"crop_on_resize": true,
		}
		if !validFields[field] {
			panic(fmt.Sprintf("ConfigBoolCompare: unknown field '%s'", field))
		}

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			cfg := world.Resources.Config
			var current bool
			switch field {
			case "crop_on_resize":
				current = cfg.CropOnResize
			}
			return current == expected
		}
	})

	// And - all child guards must pass
	m.RegisterGuardFactory("And", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		childGuards := resolveChildGuards(machine, args)
		if len(childGuards) == 0 {
			return func(world *engine.World, region *fsm.RegionState, payload any) bool { return true }
		}
		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			for _, g := range childGuards {
				if !g(world, region, payload) {
					return false
				}
			}
			return true
		}
	})

	// Or - at least one child guard must pass
	m.RegisterGuardFactory("Or", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		childGuards := resolveChildGuards(machine, args)
		if len(childGuards) == 0 {
			return func(world *engine.World, region *fsm.RegionState, payload any) bool { return true }
		}
		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			for _, g := range childGuards {
				if g(world, region, payload) {
					return true
				}
			}
			return false
		}
	})

	// PayloadIntCompare - compares int field in event payload
	m.RegisterGuardFactory("PayloadIntCompare", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		field, _ := args["field"].(string)
		op, _ := args["op"].(string)
		value := parseIntArg(args, "value")

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			if payload == nil {
				return false
			}
			fieldVal, ok := extractIntField(payload, field)
			if !ok {
				return false
			}
			return compareInt(fieldVal, op, value)
		}
	})

	// PayloadBoolEquals - checks bool field in event payload
	m.RegisterGuardFactory("PayloadBoolEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		field, _ := args["field"].(string)
		expected := true
		if v, ok := args["value"].(bool); ok {
			expected = v
		}

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			if payload == nil {
				return false
			}
			fieldVal, ok := extractBoolField(payload, field)
			if !ok {
				return false
			}
			return fieldVal == expected
		}
	})

	// PayloadStringEquals - checks string field in event payload
	m.RegisterGuardFactory("PayloadStringEquals", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		field, _ := args["field"].(string)
		expected, _ := args["value"].(string)

		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			if payload == nil {
				return false
			}
			fieldVal, ok := extractStringField(payload, field)
			if !ok {
				return false
			}
			return fieldVal == expected
		}
	})

	// PayloadExists - checks if payload is non-nil (useful in Or guards)
	m.RegisterGuardFactory("PayloadExists", func(machine *fsm.Machine[*engine.World], args map[string]any) fsm.GuardFunc[*engine.World] {
		return func(world *engine.World, region *fsm.RegionState, payload any) bool {
			return payload != nil
		}
	})
}

// === Static Guards ===

func registerStaticGuards(m *fsm.Machine[*engine.World]) {
	m.RegisterGuard("AlwaysTrue", func(world *engine.World, region *fsm.RegionState, payload any) bool {
		return true
	})

	m.RegisterGuard("StateTimeExceeds10s", func(world *engine.World, region *fsm.RegionState, payload any) bool {
		return region.TimeInState > 10*time.Second
	})

	m.RegisterGuard("StateTimeExceeds2s", func(world *engine.World, region *fsm.RegionState, payload any) bool {
		return region.TimeInState > 2*time.Second
	})
}

// --- Helpers ---

// parseIntArg extracts int64 value from guard args map
// Handles TOML numeric types: float64, int64, int, and string "0"
func parseIntArg(args map[string]any, key string) int64 {
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

// compareInt performs comparison operation between two int64 values
func compareInt(current int64, op string, value int64) bool {
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

// resolveChildGuards parses nested guard definitions and returns compiled guard functions
func resolveChildGuards(m *fsm.Machine[*engine.World], args map[string]any) []fsm.GuardFunc[*engine.World] {
	guardsRaw, ok := args["guards"]
	if !ok {
		return nil
	}

	guardsList, ok := guardsRaw.([]any)
	if !ok {
		return nil
	}

	result := make([]fsm.GuardFunc[*engine.World], 0, len(guardsList))
	for _, item := range guardsList {
		guardDef, ok := item.(map[string]any)
		if !ok {
			continue
		}

		name, _ := guardDef["name"].(string)
		if name == "" {
			continue
		}

		var childArgs map[string]any
		if argsRaw, ok := guardDef["args"]; ok {
			childArgs, _ = argsRaw.(map[string]any)
		}

		guard := resolveGuard(m, name, childArgs)
		if guard != nil {
			result = append(result, guard)
		}
	}
	return result
}

// resolveGuard creates a guard function from name and args using registered factories/guards
func resolveGuard(m *fsm.Machine[*engine.World], name string, args map[string]any) fsm.GuardFunc[*engine.World] {
	// Check factory first (for parameterized guards)
	if factory, ok := m.GetGuardFactory(name); ok {
		return factory(m, args)
	}
	// Fall back to static guard
	if guard, ok := m.GetGuard(name); ok {
		return guard
	}
	return nil
}

// extractIntField retrieves an int-compatible field from a struct via reflection
func extractIntField(payload any, fieldName string) (int64, bool) {
	v := reflect.ValueOf(payload)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}

	field := v.FieldByName(fieldName)
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

// extractBoolField retrieves a bool field from a struct via reflection
func extractBoolField(payload any, fieldName string) (bool, bool) {
	v := reflect.ValueOf(payload)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false, false
	}

	field := v.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, false
	}
	return field.Bool(), true
}

// extractStringField retrieves a string field from a struct via reflection
func extractStringField(payload any, fieldName string) (string, bool) {
	v := reflect.ValueOf(payload)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}

	field := v.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	return field.String(), true
}