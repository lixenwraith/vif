package fsm

import (
	"fmt"
	"sort"
	"time"

	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/toml"
)

// LoadConfig parses a TOML byte slice and populates the Machine
// Validates all references (states, guards, actions, events)
// Clears existing graph data before loading
func (m *Machine[T]) LoadConfig(data []byte) error {
	// 1. Decode TOML into intermediate config
	var config RootConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal FSM config: %w", err)
	}

	// 2. Clear existing graph
	m.nodes = make(map[StateID]*Node[T])
	m.regions = make(map[string]*RegionState)
	m.regionInitials = make(map[string]StateID)
	m.activeStateID = StateNone
	m.activePath = m.activePath[:0]

	// // 3. First Pass: Create GameState IDs and Nodes
	// nameToID := make(map[string]StateID)
	// // Reserve StateRoot = 1
	// m.nodes[StateRoot] = m.AddState(StateRoot, "Root", StateNone)
	// nameToID["Root"] = StateRoot

	// 3. First Pass: Create GameState IDs and Nodes/Root node
	m.nodes[StateRoot] = m.AddState(StateRoot, "Root", StateNone)
	nameToID := make(map[string]StateID)
	nameToID["Root"] = StateRoot

	if _, ok := config.States["Root"]; !ok {
		config.States["Root"] = &StateConfig{}
	}

	// Generate IDs
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

	// 4. Second Pass: Build Nodes and resolve relationships
	for name, cfg := range config.States {
		id := nameToID[name]

		// Skip root creation (done manually above), but process its config
		var node *Node[T]
		if id == StateRoot {
			node = m.nodes[StateRoot]
		} else {
			pName := cfg.Parent
			if pName == "" {
				pName = "Root"
			}
			parentID, ok := nameToID[pName]
			if !ok {
				return fmt.Errorf("state '%s' references unknown parent '%s'", name, pName)
			}
			node = m.AddState(id, name, parentID)
		}

		// Compile Actions
		var err error
		if node.OnEnter, err = m.compileActions(cfg.OnEnter, nameToID); err != nil {
			return fmt.Errorf("state '%s' OnEnter: %w", name, err)
		}
		if node.OnUpdate, err = m.compileActions(cfg.OnUpdate, nameToID); err != nil {
			return fmt.Errorf("state '%s' OnUpdate: %w", name, err)
		}
		if node.OnExit, err = m.compileActions(cfg.OnExit, nameToID); err != nil {
			return fmt.Errorf("state '%s' OnExit: %w", name, err)
		}

		// Compile Transitions
		if err := m.compileTransitions(node, cfg.Transitions, nameToID); err != nil {
			return fmt.Errorf("state '%s' transitions: %w", name, err)
		}
	}

	// 5. Finalize: Compile Paths for LCA
	if err := m.CompilePaths(); err != nil {
		return err
	}

	// 6. Build state metadata for telemetry
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

	// // 7. Validate Initial GameState
	// initialID, ok := nameToID[config.InitialState]
	// if !ok {
	// 	return fmt.Errorf("initial state '%s' not found", config.InitialState)
	// }
	// m.InitialStateID = initialID

	// 7. Handle regions initial state
	if len(config.Regions) > 0 {
		// Multi-region mode
		for regionName, regionCfg := range config.Regions {
			initialID, ok := nameToID[regionCfg.Initial]
			if !ok {
				return fmt.Errorf("region '%s' references unknown initial state '%s'", regionName, regionCfg.Initial)
			}
			m.regionInitials[regionName] = initialID
		}
	} else if config.InitialState != "" {
		// Legacy single-region mode
		initialID, ok := nameToID[config.InitialState]
		if !ok {
			return fmt.Errorf("initial state '%s' not found", config.InitialState)
		}
		m.InitialStateID = initialID
		m.regionInitials["main"] = initialID
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

func (m *Machine[T]) compileActions(configs []ActionConfig, nameToID map[string]StateID) ([]Action[T], error) {
	actions := make([]Action[T], 0, len(configs))
	for _, cfg := range configs {
		fn, ok := m.actionReg[cfg.Action]
		if !ok {
			return nil, fmt.Errorf("unknown action function '%s'", cfg.Action)
		}

		var args any = nil

		switch cfg.Action {
		case "EmitEvent":
			if cfg.Event == "" {
				return nil, fmt.Errorf("EmitEvent action requires 'event' field")
			}
			et, ok := event.GetEventType(cfg.Event)
			if !ok {
				return nil, fmt.Errorf("unknown event type '%s'", cfg.Event)
			}
			payload := event.NewPayloadStruct(et)
			if payload != nil && cfg.Payload != nil {
				if err := toml.Decode(cfg.Payload, payload); err != nil {
					return nil, fmt.Errorf("failed to decode payload for event '%s': %w", cfg.Event, err)
				}
			}
			args = &EmitEventArgs{
				Type:    et,
				Payload: payload,
			}

		case "SpawnRegion", "TerminateRegion", "PauseRegion", "ResumeRegion":
			if cfg.Region == "" {
				return nil, fmt.Errorf("%s action requires 'region' field", cfg.Action)
			}
			rcArgs := &RegionControlArgs{
				RegionName: cfg.Region,
			}
			if cfg.Action == "SpawnRegion" {
				if cfg.InitialState == "" {
					return nil, fmt.Errorf("SpawnRegion action requires 'initial_state' field")
				}
				rcArgs.InitialState = cfg.InitialState
			}
			args = rcArgs
		}

		actions = append(actions, Action[T]{
			Func: fn,
			Args: args,
		})
	}
	return actions, nil
}

// func (m *Machine[T]) compileActions(configs []ActionConfig) ([]Action[T], error) {
// 	actions := make([]Action[T], 0, len(configs))
// 	for _, cfg := range configs {
// 		// Resolve Action Function
// 		fn, ok := m.actionReg[cfg.Action]
// 		if !ok {
// 			return nil, fmt.Errorf("unknown action function '%s'", cfg.Action)
// 		}
//
// 		var args any = nil
//
// 		// Special handling for "EmitEvent" action to compile payload
// 		if cfg.Action == "EmitEvent" {
// 			if cfg.Event == "" {
// 				return nil, fmt.Errorf("EmitEvent action requires 'event' field")
// 			}
//
// 			// Resolve Event Type
// 			et, ok := event.GetEventType(cfg.Event)
// 			if !ok {
// 				return nil, fmt.Errorf("unknown event type '%s'", cfg.Event)
// 			}
//
// 			// Create Payload Struct
// 			payload := event.NewPayloadStruct(et)
// 			if payload != nil && cfg.Payload != nil {
// 				// Decode map[string]any into struct
// 				if err := toml.Decode(cfg.Payload, payload); err != nil {
// 					return nil, fmt.Errorf("failed to decode payload for event '%s': %w", cfg.Event, err)
// 				}
// 			}
//
// 			args = &EmitEventArgs{
// 				Type:    et,
// 				Payload: payload,
// 			}
// 		}
//
// 		actions = append(actions, Action[T]{
// 			Func: fn,
// 			Args: args,
// 		})
// 	}
// 	return actions, nil
// }

func (m *Machine[T]) compileTransitions(node *Node[T], configs []TransitionConfig, nameToID map[string]StateID) error {
	for _, cfg := range configs {
		targetID, ok := nameToID[cfg.Target]
		if !ok {
			return fmt.Errorf("transition references unknown target '%s'", cfg.Target)
		}

		var eventType event.EventType = 0 // 0 = Tick
		if cfg.Trigger != "Tick" {
			et, ok := event.GetEventType(cfg.Trigger)
			if !ok {
				return fmt.Errorf("unknown event type '%s'", cfg.Trigger)
			}
			eventType = et
		}

		var guard GuardFunc[T]
		if cfg.Guard != "" {
			// Check factory first
			if factory, ok := m.guardFactoryReg[cfg.Guard]; ok {
				guard = factory(m, cfg.GuardArgs)
			} else if g, ok := m.guardReg[cfg.Guard]; ok {
				guard = g
			} else {
				return fmt.Errorf("unknown guard '%s'", cfg.Guard)
			}
		}

		node.Transitions = append(node.Transitions, Transition[T]{
			TargetID: targetID,
			Event:    eventType,
			Guard:    guard,
		})
	}
	return nil
}