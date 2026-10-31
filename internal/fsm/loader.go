package fsm

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/toml/macro"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
)

// LoadScenario parses a one-document scenario, expands the macros it defines and
// populates the Machine, replacing any graph it held
func (m *Machine[T]) LoadScenario(data []byte) error {
	p := toml.NewParser(data)
	parsed, err := p.Parse()
	if err != nil {
		return fmt.Errorf("failed to parse scenario: %w", err)
	}
	if parsed, err = macro.ExpandDocument(parsed); err != nil {
		return fmt.Errorf("failed to expand scenario: %w", err)
	}
	return m.LoadScenarioFromMap(parsed)
}

// LoadScenarioFromMap builds the Machine from a pre-parsed scenario document
// Used by file loader after merging external includes
func (m *Machine[T]) LoadScenarioFromMap(doc map[string]any) error {
	// 1. Decode the document into the intermediate config structs
	if err := unknownKeys(doc); err != nil {
		return err
	}
	var config ScenarioDoc
	if err := toml.Decode(doc, &config); err != nil {
		return fmt.Errorf("failed to decode scenario: %w", err)
	}

	// Enforce regions existence
	if len(config.Regions) == 0 {
		return fmt.Errorf("at least one region must be defined in [regions]")
	}
	if c := config.Systems; c != nil && c.OnlySystems != nil && len(c.DisabledSystems) > 0 {
		return fmt.Errorf("[systems]: only_systems already disables every system it does not name; drop disabled_systems")
	}
	for _, name := range slices.Sorted(maps.Keys(config.Regions)) {
		if r := config.Regions[name]; r.OnlySystems != nil && len(r.DisabledSystems) > 0 {
			return fmt.Errorf("region %q: only_systems already disables every system it does not name; drop disabled_systems", name)
		}
	}

	// 2. Clear existing graph
	m.nodes = make(map[StateID]*Node[T])
	m.regions = make(map[string]*RegionState)
	m.regionOrder = m.regionOrder[:0]
	m.regionInitials = make(map[string]StateID)
	m.regionConfigs = make(map[string]*RegionConfig)
	m.variables = make(map[string]int64)
	m.delayedActions = make(map[string][]DelayedAction[T])
	m.compiledActions = make(map[uint32]Action[T])
	m.nextActionID = 0
	m.active.clear()

	// 3. Store systems config
	m.systemsConfig = config.Systems

	// 4. First Pass: Create State IDs and Nodes/Root node
	m.nodes[StateRoot] = m.AddState(StateRoot, "Root", StateNone)
	nameToID := make(map[string]StateID)
	nameToID["Root"] = StateRoot

	if config.States == nil {
		config.States = make(map[string]*StateConfig)
	}
	if _, ok := config.States["Root"]; !ok {
		config.States["Root"] = &StateConfig{}
	}

	// Generate IDs, Root = 1
	nextID := 2

	// Sort keys for deterministic ID generation
	stateNames := make([]string, 0, len(config.States))
	for name := range config.States {
		if name != "Root" {
			stateNames = append(stateNames, name)
		}
	}
	sort.Strings(stateNames)

	for _, name := range stateNames {
		nameToID[name] = StateID(nextID)
		nextID++
	}

	// Store region configs before action compilation (reference validation)
	for regionName, regionCfg := range config.Regions {
		cfgCopy := regionCfg
		m.regionConfigs[regionName] = &cfgCopy
	}

	// 5. Second Pass: build nodes; accumulate validation errors per state
	var errs []error
	orderedNames := append([]string{"Root"}, stateNames...) // deterministic diagnostics
	for _, name := range orderedNames {
		cfg := config.States[name]
		id := nameToID[name]

		var node *Node[T]
		if id == StateRoot {
			node = m.nodes[StateRoot]
			if len(cfg.OnEnter) > 0 || len(cfg.OnUpdate) > 0 || len(cfg.OnExit) > 0 {
				errs = append(errs, fmt.Errorf(
					"state 'Root': on_enter/on_update/on_exit are not permitted; "+
						"Root is shared by every region and its actions run once per region. "+
						"Put one-shot setup in a dedicated region's initial state"))
			}
		} else {
			pName := cfg.Parent
			if pName == "" {
				pName = "Root"
			}
			parentID, ok := nameToID[pName]
			if !ok {
				errs = append(errs, fmt.Errorf("state '%s': unknown parent '%s'", name, pName))
				continue
			}
			node = m.AddState(id, name, parentID)
		}

		var err error
		if node.OnEnter, err = m.compileActions(cfg.OnEnter, nameToID, true); err != nil {
			errs = append(errs, fmt.Errorf("state '%s' on_enter: %w", name, err))
		}
		if node.OnUpdate, err = m.compileActions(cfg.OnUpdate, nameToID, false); err != nil {
			errs = append(errs, fmt.Errorf("state '%s' on_update: %w", name, err))
		}
		if node.OnExit, err = m.compileActions(cfg.OnExit, nameToID, true); err != nil {
			errs = append(errs, fmt.Errorf("state '%s' on_exit: %w", name, err))
		}
		if err = m.compileTransitions(node, cfg.Transitions, nameToID); err != nil {
			errs = append(errs, fmt.Errorf("state '%s' transitions: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// 6. Finalize: Compile Paths for LCA
	if err := m.CompilePaths(); err != nil {
		return err
	}

	// 7. Build state metadata for telemetry
	m.StateDurations = make(map[StateID]time.Duration)
	m.StateIndices = make(map[StateID]int)
	m.StateCount = len(stateNames) // Excludes Root

	for idx, name := range stateNames {
		id := nameToID[name]
		m.StateIndices[id] = idx

		// Extract duration from StateTimeExceeds guards
		if cfg, ok := config.States[name]; ok {
			for _, trans := range cfg.Transitions {
				if trans.Guard == "StateTimeExceeds" && trans.GuardArgs != nil {
					if ms, ok := trans.GuardArgs["ms"]; ok {
						switch v := ms.(type) {
						case float64:
							m.StateDurations[id] = time.Duration(v) * time.Millisecond
						case int64:
							m.StateDurations[id] = time.Duration(v) * time.Millisecond
						case int:
							m.StateDurations[id] = time.Duration(v) * time.Millisecond
						}
					}
				}
			}
		}
	}

	// 8. Handle regions initial state and store configs
	for regionName, regionCfg := range config.Regions {
		// Skip regions without initial (states-only, spawned dynamically)
		if regionCfg.Initial == "" {
			continue
		}
		initialID, ok := nameToID[regionCfg.Initial]
		if !ok {
			return fmt.Errorf("region '%s' references unknown initial state '%s'", regionName, regionCfg.Initial)
		}
		m.regionInitials[regionName] = initialID
	}

	return nil
}

// GetStateID resolves a state name to ID
func (m *Machine[T]) GetStateID(name string) (StateID, bool) {
	for id, node := range m.nodes {
		if node.Name == name {
			return id, true
		}
	}
	return StateNone, false
}

// StateName resolves a state id to its configured name; "" when unknown.
func (m *Machine[T]) StateName(id StateID) string {
	if n, ok := m.nodes[id]; ok {
		return n.Name
	}
	return ""
}

// DeclaredRegions returns the configured region names in sorted order.
// Regions without an initial state are included: they are spawn targets.
func (m *Machine[T]) DeclaredRegions() []string {
	names := make([]string, 0, len(m.regionConfigs))
	for n := range m.regionConfigs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RegionTelemetry reports one region's runtime state for status publication.
func (m *Machine[T]) RegionTelemetry(name string) RegionTelemetry {
	r, ok := m.regions[name]
	if !ok {
		return RegionTelemetry{}
	}
	return RegionTelemetry{
		State:       m.StateName(r.ActiveStateID),
		StateID:     r.ActiveStateID,
		TimeInState: r.TimeInState,
		MaxDuration: m.StateDurations[r.ActiveStateID],
		Index:       m.StateIndices[r.ActiveStateID],
		Paused:      r.Paused,
		Active:      true,
	}
}

func (m *Machine[T]) compileActions(configs []ActionConfig, nameToID map[string]StateID, lifecycle bool) ([]Action[T], error) {
	resolve := func(name string) (StateID, bool) {
		id, ok := nameToID[name]
		return id, ok
	}

	actions := make([]Action[T], 0, len(configs))
	for _, cfg := range configs {
		if cfg.Reconcile {
			if !lifecycle {
				return nil, fmt.Errorf("reconcile is valid only on state on_enter/on_exit actions")
			}
			if cfg.Action != "EmitEvent" {
				return nil, fmt.Errorf("reconcile requires action 'EmitEvent', got %q", cfg.Action)
			}
			et, ok := event.GetEventType(cfg.Event)
			if !ok {
				return nil, fmt.Errorf("reconcile names unknown event type %q", cfg.Event)
			}
			if class := event.ClassOf(et); class != event.ClassLocal {
				return nil, fmt.Errorf("reconcile event %q is %s, want local", cfg.Event, class)
			}
			if cfg.DelayMs != 0 {
				return nil, fmt.Errorf("reconcile event %q cannot be delayed", cfg.Event)
			}
			if cfg.Guard != "" {
				return nil, fmt.Errorf("reconcile event %q cannot have an action guard", cfg.Event)
			}
		}

		entry, ok := m.actionReg[cfg.Action]
		if !ok {
			return nil, fmt.Errorf("unknown action function '%s'", cfg.Action)
		}
		if entry.refuse != "" {
			return nil, fmt.Errorf("action '%s': %s", cfg.Action, entry.refuse)
		}
		if key := unreadKey(cfg, entry.keys); key != "" {
			return nil, fmt.Errorf("action '%s' takes no %s", cfg.Action, key)
		}
		args, err := entry.compile(cfg, resolve)
		if err != nil {
			return nil, fmt.Errorf("action '%s': %w", cfg.Action, err)
		}

		var guard GuardFunc[T]
		if cfg.Guard != "" {
			if guard, err = m.guard(cfg.Guard, cfg.GuardArgs); err != nil {
				return nil, fmt.Errorf("action '%s': %w", cfg.Action, err)
			}
		}

		// The variables a reconciled payload reads, a pick's among them, in name order
		var reconcileVars []string
		if cfg.Reconcile {
			for _, name := range cfg.PayloadVars {
				if name != "" {
					reconcileVars = append(reconcileVars, name)
				}
			}
			if cfg.Pick != nil {
				reconcileVars = append(reconcileVars, cfg.Pick.Var)
			}
			slices.Sort(reconcileVars)
			reconcileVars = slices.Compact(reconcileVars)
		}

		m.nextActionID++
		action := Action[T]{
			ID:            m.nextActionID,
			Func:          entry.run,
			Args:          args,
			Guard:         guard,
			DelayMs:       cfg.DelayMs,
			Reconcile:     cfg.Reconcile,
			ReconcileVars: reconcileVars,
		}
		actions = append(actions, action)
		m.compiledActions[action.ID] = action
	}
	return actions, nil
}

func (m *Machine[T]) compileTransitions(node *Node[T], configs []TransitionConfig, nameToID map[string]StateID) error {
	for _, cfg := range configs {
		var targetID StateID
		if cfg.Internal {
			// Internal transitions have no target
			if cfg.Target != "" {
				return fmt.Errorf("internal transition cannot have target '%s'", cfg.Target)
			}
		} else {
			id, ok := nameToID[cfg.Target]
			if !ok {
				return fmt.Errorf("transition references unknown target '%s'", cfg.Target)
			}
			targetID = id
		}

		var eventType event.EventType
		if cfg.Trigger != "Tick" {
			et, ok := event.GetEventType(cfg.Trigger)
			if !ok {
				return fmt.Errorf("unknown event type '%s'", cfg.Trigger)
			}
			eventType = et
		}

		var guard GuardFunc[T]
		if cfg.Guard != "" {
			var err error
			if guard, err = m.guard(cfg.Guard, cfg.GuardArgs); err != nil {
				return err
			}
		}

		// Transition actions
		actions, err := m.compileActions(cfg.Actions, nameToID, false)
		if err != nil {
			return fmt.Errorf("transition actions: %w", err)
		}

		node.Transitions = append(node.Transitions, Transition[T]{
			TargetID:    targetID,
			Event:       eventType,
			Guard:       guard,
			CaptureVars: cfg.CaptureVars,
			Actions:     actions,
			Internal:    cfg.Internal,
		})
	}
	return nil
}

// guard makes the guard name and args spell from the registered guards and the
// dialect tree's And, Or and Not over them
func (m *Machine[T]) guard(name string, args map[string]any) (GuardFunc[T], error) {
	return dialect.Tree[GuardFunc[T]]{
		Leaf: func(name string) (func(map[string]any) (GuardFunc[T], error), bool) {
			g, ok := m.guardReg[name]
			return g.build, ok
		},
		All: allOf[T],
		Any: anyOf[T],
		Not: negate[T],
	}.Build(name, args)
}

// allOf, anyOf and negate are And, Or and Not over guards; And and Or of no guards
// both pass
func allOf[T any](gs []GuardFunc[T]) GuardFunc[T] {
	return func(ctx T, region *RegionState, payload any) bool {
		for _, g := range gs {
			if !g(ctx, region, payload) {
				return false
			}
		}
		return true
	}
}

func anyOf[T any](gs []GuardFunc[T]) GuardFunc[T] {
	return func(ctx T, region *RegionState, payload any) bool {
		for _, g := range gs {
			if g(ctx, region, payload) {
				return true
			}
		}
		return len(gs) == 0
	}
}

func negate[T any](g GuardFunc[T]) GuardFunc[T] {
	return func(ctx T, region *RegionState, payload any) bool { return !g(ctx, region, payload) }
}

// unreadKey names a key of its own table cfg sets that an action reading only keys
// would ignore
func unreadKey(cfg ActionConfig, keys []string) string {
	for _, k := range [...]struct {
		key string
		set bool
	}{
		{"event", cfg.Event != ""},
		{"payload", cfg.Payload != nil},
		{"payload_vars", cfg.PayloadVars != nil},
		{"pick", cfg.Pick != nil},
		{"region", cfg.Region != ""},
		{"initial_state", cfg.InitialState != ""},
	} {
		if k.set && !slices.Contains(keys, k.key) {
			return k.key
		}
	}
	return ""
}

// unknownKeys refuses a key no config field takes under [systems], a region or a
// state, naming where it sits; a top-level key is its embedder's to check, as a
// scenario's [map] is and a bot graph's [bot]
func unknownKeys(doc map[string]any) error {
	var errs []error
	for _, k := range toml.Unmapped(doc, (*ScenarioDoc)(nil)) {
		switch {
		case len(k) == 1: // the embedder's
		case len(k) > 2 && k[0] == "states":
			errs = append(errs, fmt.Errorf("state '%s': %w", k[1], toml.UnknownKeys{k[2:]}))
		case len(k) > 2 && k[0] == "regions":
			errs = append(errs, fmt.Errorf("region '%s': %w", k[1], toml.UnknownKeys{k[2:]}))
		default:
			errs = append(errs, toml.UnknownKeys{k})
		}
	}
	return errors.Join(errs...)
}
