package std

import (
	"fmt"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/fsm"
)

// EmitEventArgs holds pre-compiled event data for the EmitEvent action
// Type identifies the event; Payload is the decoded struct (or nil)
type EmitEventArgs struct {
	Type        event.EventType
	Payload     any
	PayloadVars map[string]string // Field name -> variable name
}

// RegionControlArgs holds args for region control actions
type RegionControlArgs struct {
	RegionName   string
	InitialState string      // For SpawnRegion
	InitialID    fsm.StateID // Resolved at load time
}

// VariableArgs holds args for variable manipulation actions
type VariableArgs struct {
	Name      string `toml:"name"`
	Value     int64  `toml:"value"`
	Delta     int64  `toml:"delta"`
	SourceVar string `toml:"source_var"` // If set, use this var's value instead of Value/Delta
	Min       *int64 `toml:"min"`        // Optional clamp lower bound
	Max       *int64 `toml:"max"`        // Optional clamp upper bound
}

// SystemControlArgs holds args for system enable/disable actions
type SystemControlArgs struct {
	SystemName string `toml:"system_name"`
	Enabled    bool   `toml:"enabled"`
}

// StatusIntArgs holds args for status registry int manipulation
type StatusIntArgs struct {
	Key   string `toml:"key"`
	Value int64  `toml:"value"`
}

// ConfigToVarArgs carries a resolved config accessor and its target variable
// was {Field, Name string} with a runtime switch; the accessor now
// binds at load, so an unknown field fails -check and evaluation is a direct call
type ConfigToVarArgs[T any] struct {
	Name     string
	Accessor func(ctx T) int64
}

// decodePayload is the compiler for actions whose args decode straight from TOML
func decodePayload[T any, A any](m *fsm.Machine[T], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
	args := new(A)
	if cfg.Payload != nil {
		if err := toml.Decode(cfg.Payload, args); err != nil {
			return nil, fmt.Errorf("decode payload: %w", err)
		}
	}
	return args, nil
}

// compileEmitEvent resolves the event type and decodes its payload at load
func compileEmitEvent[T any](m *fsm.Machine[T], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
	if cfg.Event == "" {
		return nil, fmt.Errorf("requires 'event' field")
	}
	et, ok := event.GetEventType(cfg.Event)
	if !ok {
		return nil, fmt.Errorf("unknown event type '%s'", cfg.Event)
	}
	payload := event.NewPayloadStruct(et)
	if payload != nil && cfg.Payload != nil {
		if err := toml.Decode(cfg.Payload, payload); err != nil {
			return nil, fmt.Errorf("decode payload for event '%s': %w", cfg.Event, err)
		}
	}
	return &EmitEventArgs{Type: et, Payload: payload, PayloadVars: cfg.PayloadVars}, nil
}

// compileRegionControl validates the region reference and resolves SpawnRegion's initial state
func compileRegionControl[T any](m *fsm.Machine[T], cfg fsm.ActionConfig, resolve fsm.StateResolver) (any, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("requires 'region' field")
	}
	if m.GetRegionConfig(cfg.Region) == nil {
		return nil, fmt.Errorf("references undeclared region '%s'", cfg.Region)
	}

	args := &RegionControlArgs{RegionName: cfg.Region}
	if cfg.Action == "SpawnRegion" {
		if cfg.InitialState == "" {
			return nil, fmt.Errorf("requires 'initial_state' field")
		}
		id, ok := resolve(cfg.InitialState)
		if !ok {
			return nil, fmt.Errorf("unknown initial state '%s'", cfg.InitialState)
		}
		args.InitialState = cfg.InitialState
		args.InitialID = id
	}
	return args, nil
}

// compileConfigToVar binds the accessor at load
// A host without config access yields a nil accessor and an inert action,
// matching the Host contract; field validation is skipped in that case
func compileConfigToVar[T any](h Host[T]) fsm.ArgCompiler[T] {
	return func(m *fsm.Machine[T], cfg fsm.ActionConfig, _ fsm.StateResolver) (any, error) {
		var raw struct {
			Field string `toml:"field"`
			Name  string `toml:"name"`
		}
		if cfg.Payload != nil {
			if err := toml.Decode(cfg.Payload, &raw); err != nil {
				return nil, fmt.Errorf("decode payload: %w", err)
			}
		}
		if raw.Field == "" || raw.Name == "" {
			return nil, fmt.Errorf("requires 'field' and 'name'")
		}
		if h.ConfigInt == nil {
			return &ConfigToVarArgs[T]{Name: raw.Name}, nil
		}
		accessor, ok := h.ConfigInt(raw.Field)
		if !ok {
			return nil, fmt.Errorf("unknown config field '%s'", raw.Field)
		}
		return &ConfigToVarArgs[T]{Name: raw.Name, Accessor: accessor}, nil
	}
}
