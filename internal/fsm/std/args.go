package std

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
)

// EmitEventArgs holds pre-compiled event data for the EmitEvent action
// Type identifies the event; Payload is the decoded struct (or nil). With a pick,
// Picks holds the payload decoded once per entry in place of Payload.
type EmitEventArgs struct {
	Type        event.EventType
	Payload     any
	PayloadVars map[string]string // Field name -> variable name
	Picks       []any             // The payload with each entry of the pick in its field
	PickVar     string            // The variable whose value, modulo len(Picks), names the entry
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

// RandomVarArgs draws name in [min, max] as a pure function of the host seed and
// two variables, so a sequence is recomputed from its seed and index, not stored
type RandomVarArgs struct {
	Name     string `toml:"name"`
	Min      int64  `toml:"min"`
	Max      int64  `toml:"max"`
	SeedVar  string `toml:"seed_var"`
	IndexVar string `toml:"index_var"`
}

// SystemControlArgs holds args for system enable/disable actions
type SystemControlArgs struct {
	SystemName string `toml:"system_name"`
}

// StatusToVarArgs copies a status int into a variable; the registry is read-only
// to scripts, so a scenario marks where a count starts instead of zeroing it
type StatusToVarArgs struct {
	Key  string `toml:"key"`
	Name string `toml:"name"`
}

// ConfigToVarArgs carries a config accessor bound at load and its target variable,
// so an unknown field fails -check and evaluation is a direct call
type ConfigToVarArgs[T any] struct {
	Name     string
	Accessor func(ctx T) int64
}

// configToVarPayload is what ConfigToVar's payload names
type configToVarPayload struct {
	Field string `toml:"field"`
	Name  string `toml:"name"`
}

// The guard args, one struct per set of keys a guard reads; a bool value left out
// means true
type (
	msArgs struct {
		Ms int64 `toml:"ms"`
	}
	regionArgs struct {
		Region string `toml:"region"`
	}
	varEqualsArgs struct {
		Var   string `toml:"var"`
		Value int64  `toml:"value"`
	}
	varCompareArgs struct {
		Var   string     `toml:"var"`
		Op    dialect.Op `toml:"op"`
		Value int64      `toml:"value"`
	}
	varCompareVarArgs struct {
		VarA string     `toml:"var_a"`
		Op   dialect.Op `toml:"op"`
		VarB string     `toml:"var_b"`
	}
	statusBoolArgs struct {
		Key   string `toml:"key"`
		Value *bool  `toml:"value"`
	}
	statusIntArgs struct {
		Key   string     `toml:"key"`
		Op    dialect.Op `toml:"op"`
		Value int64      `toml:"value"`
		Since string     `toml:"since"`
	}
	fieldCompareArgs struct {
		Field string     `toml:"field"`
		Op    dialect.Op `toml:"op"`
		Value int64      `toml:"value"`
	}
	fieldCompareVarArgs struct {
		Field string     `toml:"field"`
		Op    dialect.Op `toml:"op"`
		Var   string     `toml:"var"`
	}
	fieldBoolArgs struct {
		Field string `toml:"field"`
		Value *bool  `toml:"value"`
	}
	fieldStringArgs struct {
		Field string `toml:"field"`
		Value string `toml:"value"`
	}
	fieldSpeciesArgs struct {
		Field string   `toml:"field"`
		Names []string `toml:"names"`
	}
)

// The keys of their own tables the keyed actions read
var (
	emitKeys   = []string{"event", "payload", "payload_vars", "pick"}
	regionKeys = []string{"region"}
	spawnKeys  = []string{"region", "initial_state"}
)

// compileEmitEvent resolves the event type and decodes its payload at load, its
// enum fields as names spell them
func compileEmitEvent(names *dialect.Names) func(fsm.ActionConfig, fsm.StateResolver) (*EmitEventArgs, error) {
	return func(cfg fsm.ActionConfig, _ fsm.StateResolver) (*EmitEventArgs, error) {
		if cfg.Event == "" {
			return nil, errors.New("requires 'event' field")
		}
		et, ok := event.GetEventType(cfg.Event)
		if !ok {
			return nil, fmt.Errorf("unknown event type '%s'", cfg.Event)
		}
		if cfg.Pick == nil {
			payload, err := event.AuthoredPayload(names, et, cfg.Payload)
			if err != nil {
				return nil, err
			}
			return &EmitEventArgs{Type: et, Payload: payload, PayloadVars: cfg.PayloadVars}, nil
		}
		// The payload decodes only with an entry in its field, which may be the key
		// a sibling's names hang on, as a sub-type's on its species
		picks, err := compilePick(names, et, cfg)
		if err != nil {
			return nil, fmt.Errorf("pick: %w", err)
		}
		return &EmitEventArgs{Type: et, PayloadVars: cfg.PayloadVars, Picks: picks, PickVar: cfg.Pick.Var}, nil
	}
}

// compilePick decodes the payload once per entry of the pick's list, the entry
// written at its field, so each entry is checked as that field's value is: its
// type, and its names where the field is an enum
func compilePick(names *dialect.Names, et event.EventType, cfg fsm.ActionConfig) ([]any, error) {
	p := cfg.Pick
	switch {
	case p.Field == "" || p.Var == "":
		return nil, errors.New("requires 'field' and 'var'")
	case len(p.From) == 0:
		return nil, errors.New("requires 'from', a list of at least one entry")
	}
	if _, ok := cfg.PayloadVars[p.Field]; ok {
		return nil, fmt.Errorf("payload_vars sets '%s' as well", p.Field)
	}
	path := strings.Split(p.Field, ".")
	picks := make([]any, len(p.From))
	for i, entry := range p.From {
		raw, err := withField(cfg.Payload, path, entry)
		if err != nil {
			return nil, fmt.Errorf("field '%s': %w", p.Field, err)
		}
		if picks[i], err = event.AuthoredPayload(names, et, raw); err != nil {
			return nil, fmt.Errorf("from[%d]: %w", i, err)
		}
	}
	return picks, nil
}

// withField is raw, a payload as its document gives it, with value at path. The
// tables and lists along the path are copied, a table made where raw has none, so
// raw is left as it was.
func withField(raw any, path []string, value any) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	switch v := raw.(type) {
	case nil:
		next, err := withField(nil, path[1:], value)
		return map[string]any{path[0]: next}, err
	case map[string]any:
		next, err := withField(v[path[0]], path[1:], value)
		out := maps.Clone(v)
		out[path[0]] = next
		return out, err
	case []any:
		i, err := strconv.Atoi(path[0])
		if err != nil || i < 0 || i >= len(v) {
			return nil, fmt.Errorf("'%s' is not an entry of a list of %d", path[0], len(v))
		}
		next, err := withField(v[i], path[1:], value)
		out := slices.Clone(v)
		out[i] = next
		return out, err
	}
	return nil, fmt.Errorf("'%s' is under %T, not a table", path[0], raw)
}

// compileRandomVar refuses a draw that would repeat or has no range: without an index
// every draw is the same value
func compileRandomVar(a *RandomVarArgs) (*RandomVarArgs, error) {
	if a.Name == "" || a.IndexVar == "" {
		return nil, errors.New("requires 'name' and 'index_var'")
	}
	if a.Max < a.Min {
		return nil, fmt.Errorf("'max' %d is below 'min' %d", a.Max, a.Min)
	}
	return a, nil
}

// compileRegionControl validates the region reference and resolves SpawnRegion's initial state
func compileRegionControl[T any](m *fsm.Machine[T]) func(fsm.ActionConfig, fsm.StateResolver) (*RegionControlArgs, error) {
	return func(cfg fsm.ActionConfig, resolve fsm.StateResolver) (*RegionControlArgs, error) {
		if cfg.Region == "" {
			return nil, errors.New("requires 'region' field")
		}
		if m.GetRegionConfig(cfg.Region) == nil {
			return nil, fmt.Errorf("references undeclared region '%s'", cfg.Region)
		}
		args := &RegionControlArgs{RegionName: cfg.Region}
		if cfg.Action == "SpawnRegion" {
			if cfg.InitialState == "" {
				return nil, errors.New("requires 'initial_state' field")
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
}

// compileConfigToVar binds the accessor at load
// A host without config access yields a nil accessor and an inert action,
// matching the Host contract; field validation is skipped in that case
func compileConfigToVar[T any](h Host[T]) func(*configToVarPayload) (*ConfigToVarArgs[T], error) {
	return func(p *configToVarPayload) (*ConfigToVarArgs[T], error) {
		if p.Field == "" || p.Name == "" {
			return nil, errors.New("requires 'field' and 'name'")
		}
		if h.ConfigInt == nil {
			return &ConfigToVarArgs[T]{Name: p.Name}, nil
		}
		accessor, ok := h.ConfigInt(p.Field)
		if !ok {
			return nil, fmt.Errorf("unknown config field '%s'", p.Field)
		}
		return &ConfigToVarArgs[T]{Name: p.Name, Accessor: accessor}, nil
	}
}
