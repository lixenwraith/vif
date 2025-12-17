package fsm

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lixenwraith/vi-fighter/events"
)

// LoadJSON parses a JSON byte slice and populates the Machine
// Validates all references (states, guards, actions, events)
// Clears existing graph data before loading
func (m *Machine[T]) LoadJSON(data []byte) error {
	// 1. Decode JSON into intermediate config
	var config RootConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal FSM config: %w", err)
	}

	// 2. Clear existing graph
	m.nodes = make(map[StateID]*Node[T])
	m.activeStateID = StateNone
	m.activePath = m.activePath[:0]

	// 3. First Pass: Create State IDs and Nodes
	nameToID := make(map[string]StateID)
	// Reserve StateRoot = 1
	m.nodes[StateRoot] = m.AddState(StateRoot, "Root", StateNone)
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
		if node.OnEnter, err = m.compileActions(cfg.OnEnter); err != nil {
			return fmt.Errorf("state '%s' OnEnter: %w", name, err)
		}
		if node.OnUpdate, err = m.compileActions(cfg.OnUpdate); err != nil {
			return fmt.Errorf("state '%s' OnUpdate: %w", name, err)
		}
		if node.OnExit, err = m.compileActions(cfg.OnExit); err != nil {
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

	// 6. Validate Initial State
	initialID, ok := nameToID[config.InitialState]
	if !ok {
		return fmt.Errorf("initial state '%s' not found", config.InitialState)
	}
	m.InitialStateID = initialID

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

func (m *Machine[T]) compileActions(configs []ActionConfig) ([]Action[T], error) {
	actions := make([]Action[T], 0, len(configs))
	for _, cfg := range configs {
		// Resolve Action Function
		fn, ok := m.actionReg[cfg.Action]
		if !ok {
			return nil, fmt.Errorf("unknown action function '%s'", cfg.Action)
		}

		var args any = nil

		// Special handling for "EmitEvent" action to compile payload
		if cfg.Action == "EmitEvent" {
			if cfg.Event == "" {
				return nil, fmt.Errorf("EmitEvent action requires 'event' field")
			}

			// Resolve Event Type
			et, ok := events.GetEventType(cfg.Event)
			if !ok {
				return nil, fmt.Errorf("unknown event type '%s'", cfg.Event)
			}

			// Create Payload Struct
			payload := events.NewPayloadStruct(et)
			if payload != nil && len(cfg.Payload) > 0 {
				// Unmarshal JSON into struct
				if err := json.Unmarshal(cfg.Payload, payload); err != nil {
					return nil, fmt.Errorf("failed to unmarshal payload for event '%s': %w", cfg.Event, err)
				}
			}

			// For EmitEvent, Args is a tuple-like struct or we rely on the specific Action implementation
			// We'll wrap it in a generic EmitArgs struct if needed, or pass the payload directly if the action expects it
			// However, `ActionFunc` signature is `func(ctx, args)`.
			// We need to pass both EventType and Payload.
			// Let's use a standard wrapper struct for EmitEvent.
			args = &EmitEventArgs{
				Type:    et,
				Payload: payload,
			}
		} else {
			// Generic args handling (if needed later)
			// For now, only EmitEvent is fully supported with reflection
		}

		actions = append(actions, Action[T]{
			Func: fn,
			Args: args,
		})
	}
	return actions, nil
}

func (m *Machine[T]) compileTransitions(node *Node[T], configs []TransitionConfig, nameToID map[string]StateID) error {
	for _, cfg := range configs {
		// Resolve Trigger
		et, ok := events.GetEventType(cfg.Trigger)
		if !ok {
			return fmt.Errorf("unknown trigger event '%s'", cfg.Trigger)
		}

		// Resolve Target
		targetID, ok := nameToID[cfg.Target]
		if !ok {
			return fmt.Errorf("unknown target state '%s'", cfg.Target)
		}

		// Resolve Guard (factory first, then static)
		var guard GuardFunc[T]
		if cfg.Guard != "" {
			if factory, ok := m.guardFactoryReg[cfg.Guard]; ok {
				// Parameterized guard via factory
				guard = factory(m, cfg.GuardArgs)
			} else if staticGuard, ok := m.guardReg[cfg.Guard]; ok {
				// Static guard
				guard = staticGuard
			} else {
				return fmt.Errorf("unknown guard function '%s'", cfg.Guard)
			}
		}

		node.Transitions = append(node.Transitions, Transition[T]{
			TargetID: targetID,
			Event:    et,
			Guard:    guard,
		})
	}
	return nil
}

// EmitEventArgs wraps arguments for the EmitEvent action
type EmitEventArgs struct {
	Type    events.EventType
	Payload any
}