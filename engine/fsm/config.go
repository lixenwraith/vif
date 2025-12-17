package fsm

import "encoding/json"

// RootConfig represents the top-level JSON structure
type RootConfig struct {
	InitialState string                  `json:"initial"`
	States       map[string]*StateConfig `json:"states"`
}

// StateConfig represents a single state definition in JSON
type StateConfig struct {
	Parent      string             `json:"parent,omitempty"`
	OnEnter     []ActionConfig     `json:"on_enter,omitempty"`
	OnUpdate    []ActionConfig     `json:"on_update,omitempty"`
	OnExit      []ActionConfig     `json:"on_exit,omitempty"`
	Transitions []TransitionConfig `json:"transitions,omitempty"`
}

// TransitionConfig represents a transition definition
type TransitionConfig struct {
	Trigger   string         `json:"trigger"`              // Event Name or "Tick"
	Target    string         `json:"target"`               // Target State Name
	Guard     string         `json:"guard,omitempty"`      // Guard function name
	GuardArgs map[string]any `json:"guard_args,omitempty"` // parameters for factory guards
}

// ActionConfig represents an action definition
// Payload is kept as RawMessage to be unmarshaled later using reflection
type ActionConfig struct {
	Action  string          `json:"action"`            // Action function name (e.g. "EmitEvent")
	Event   string          `json:"event,omitempty"`   // For EmitEvent: Event Name
	Payload json.RawMessage `json:"payload,omitempty"` // For EmitEvent: Event Payload object
}