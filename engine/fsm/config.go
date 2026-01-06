package fsm

// RootConfig represents the top-level config structure
type RootConfig struct {
	InitialState string                  `toml:"initial"` // Legacy single-region
	Regions      map[string]RegionConfig `toml:"regions"` // Multi-region
	States       map[string]*StateConfig `toml:"states"`
}

// RegionConfig defines a parallel region
type RegionConfig struct {
	Initial string `toml:"initial"`
}

// StateConfig represents a single state definition
type StateConfig struct {
	Parent      string             `toml:"parent,omitempty"`
	OnEnter     []ActionConfig     `toml:"on_enter,omitempty"`
	OnUpdate    []ActionConfig     `toml:"on_update,omitempty"`
	OnExit      []ActionConfig     `toml:"on_exit,omitempty"`
	Transitions []TransitionConfig `toml:"transitions,omitempty"`
}

// TransitionConfig represents a transition definition
type TransitionConfig struct {
	Trigger   string         `toml:"trigger"`              // Event Name or "Tick"
	Target    string         `toml:"target"`               // Target GameState Name
	Guard     string         `toml:"guard,omitempty"`      // Guard function name
	GuardArgs map[string]any `toml:"guard_args,omitempty"` // Parameters for factory guards
}

// ActionConfig represents an action definition
type ActionConfig struct {
	Action       string `toml:"action"`                  // Action function name (e.g. "EmitEvent")
	Event        string `toml:"event,omitempty"`         // For EmitEvent: Event Name
	Payload      any    `toml:"payload,omitempty"`       // For EmitEvent: Event Payload (map[string]any from parser)
	Region       string `toml:"region,omitempty"`        // For region control actions
	InitialState string `toml:"initial_state,omitempty"` // For SpawnRegion
}