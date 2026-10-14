package fsm

import "slices"

// ScenarioDoc is a scenario's top-level document: global toggles, the regions it
// declares, and every state they resolve against
type ScenarioDoc struct {
	Description string                  `toml:"description,omitempty"`
	Systems     *SystemsConfig          `toml:"systems,omitempty"` // Global system toggles
	Regions     map[string]RegionConfig `toml:"regions"`           // Multi-region
	States      map[string]*StateConfig `toml:"states"`
}

// SystemsConfig is the baseline system set regions toggle on top of: everything
// but DisabledSystems, or only OnlySystems when that list is present
type SystemsConfig struct {
	DisabledSystems []string `toml:"disabled_systems,omitempty"`
	OnlySystems     []string `toml:"only_systems,omitempty"`
}

// Enabled reports whether the baseline leaves name on
func (c *SystemsConfig) Enabled(name string) bool {
	switch {
	case c == nil:
		return true
	case c.OnlySystems != nil:
		return slices.Contains(c.OnlySystems, name)
	}
	return !slices.Contains(c.DisabledSystems, name)
}

// RegionConfig defines a parallel region
type RegionConfig struct {
	Initial         string   `toml:"initial"`
	File            string   `toml:"file,omitempty"`             // External file path, relative to the entry's directory
	Background      bool     `toml:"background,omitempty"`       // Kept out of the foreground fsm.state summary, not fsm.<region>.*
	EnabledSystems  []string `toml:"enabled_systems,omitempty"`  // Enabled on spawn and resume, reapplied while the region is active
	DisabledSystems []string `toml:"disabled_systems,omitempty"` // Disabled on spawn and resume, reapplied while the region is active
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
	Trigger     string            `toml:"trigger"`                // Event Name or "Tick"
	Target      string            `toml:"target,omitempty"`       // Target GameState Name, optional for internal
	Guard       string            `toml:"guard,omitempty"`        // Guard function name
	GuardArgs   map[string]any    `toml:"guard_args,omitempty"`   // Parameters for factory guards
	CaptureVars map[string]string `toml:"capture_vars,omitempty"` // Payload field → FSM variable (extracted on match)
	Actions     []ActionConfig    `toml:"actions,omitempty"`      // Transition effect actions
	Internal    bool              `toml:"internal,omitempty"`     // Consume without state change
}

// ActionConfig represents an action definition
type ActionConfig struct {
	Action       string            `toml:"action"`                  // Action function name (e.g. "EmitEvent")
	Event        string            `toml:"event,omitempty"`         // For EmitEvent: Event Name
	Payload      any               `toml:"payload,omitempty"`       // For EmitEvent: Event Payload (map[string]any from parser)
	PayloadVars  map[string]string `toml:"payload_vars,omitempty"`  // Field name -> variable name for runtime injection
	Region       string            `toml:"region,omitempty"`        // For region control actions
	InitialState string            `toml:"initial_state,omitempty"` // For SpawnRegion
	Guard        string            `toml:"guard,omitempty"`         // Conditional execution guard
	GuardArgs    map[string]any    `toml:"guard_args,omitempty"`    // Guard parameters
	DelayMs      int               `toml:"delay_ms,omitempty"`      // Delay before execution (ms)
	Reconcile    bool              `toml:"reconcile,omitempty"`     // Replay an idempotent local lifecycle action across an imported state boundary
}
