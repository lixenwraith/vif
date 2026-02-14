package fsm

// External configuration paths (Unix-only)
const (
	DefaultConfigDir  = "./config"
	DefaultConfigFile = "game.toml"
	DefaultConfigPath = DefaultConfigDir + "/" + DefaultConfigFile
)

// RootConfig represents the top-level config structure
type RootConfig struct {
	Systems *SystemsConfig          `toml:"systems,omitempty"` // Global system toggles
	Regions map[string]RegionConfig `toml:"regions"`           // Multi-region
	States  map[string]*StateConfig `toml:"states"`
}

// SystemsConfig defines global system enable/disable
type SystemsConfig struct {
	Disabled []string `toml:"disabled,omitempty"`
}

// RegionConfig defines a parallel region
type RegionConfig struct {
	Initial         string   `toml:"initial"`
	File            string   `toml:"file,omitempty"`             // External file path, relative to config dir
	EnabledSystems  []string `toml:"enabled_systems,omitempty"`  // Systems to enable when region spawns
	DisabledSystems []string `toml:"disabled_systems,omitempty"` // Systems to disable when region spawns
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
	Action       string            `toml:"action"`                  // Action function name (e.g. "EmitEvent")
	Event        string            `toml:"event,omitempty"`         // For EmitEvent: Event Name
	Payload      any               `toml:"payload,omitempty"`       // For EmitEvent: Event Payload (map[string]any from parser)
	PayloadVars  map[string]string `toml:"payload_vars,omitempty"`  // Field name -> variable name for runtime injection
	Region       string            `toml:"region,omitempty"`        // For region control actions
	InitialState string            `toml:"initial_state,omitempty"` // For SpawnRegion
	Guard        string            `toml:"guard,omitempty"`         // Conditional execution guard
	GuardArgs    map[string]any    `toml:"guard_args,omitempty"`    // Guard parameters
	DelayMs      int               `toml:"delay_ms,omitempty"`      // Delay before execution (ms)
}