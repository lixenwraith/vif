package fsm

import (
	"time"

	"github.com/lixenwraith/vi-fighter/event"
)

// StateID is a unique identifier for a node
type StateID int

const (
	StateNone StateID = 0
	StateRoot StateID = 1
)

// RegionState holds runtime state for a single parallel region
type RegionState struct {
	Name          string
	ActiveStateID StateID
	TimeInState   time.Duration
	ActivePath    []StateID
	Paused        bool
}

// Machine is the generic Hierarchical Finite State Machine runtime with parallel region support
// T is the context type passed to actions and guards (e.g., *engine.World)
type Machine[T any] struct {
	// Graph Data (Immutable after load)
	nodes map[StateID]*Node[T]

	// Region Configuration (from config)
	regionInitials map[string]StateID       // Region name -> initial state ID
	regionConfigs  map[string]*RegionConfig // Region name -> config (for system toggles)

	// Runtime State (per-region)
	regions map[string]*RegionState

	// Deterministic iteration order, mirrors regions map
	regionOrder []string
	// Reusable snapshots; separate buffers so a nested dispatch cannot alias
	updateOrder []string
	eventOrder  []string

	// FSM Variables (runtime state)
	variables map[string]int64

	// Delayed Actions Queue (per-region)
	delayedActions map[string][]DelayedAction[T]

	// Telemetry cache (preserves last foreground state during transitions)
	lastTelemetryRegion  string
	lastTelemetryStateID StateID
	lastTelemetryTime    time.Duration

	// System Configuration
	systemsConfig *SystemsConfig

	// Dependency Injection
	guardReg        map[string]GuardFunc[T]
	guardFactoryReg map[string]GuardFactoryFunc[T]
	actionReg       map[string]ActionFunc[T]
	argCompilerReg  map[string]ArgCompiler[T]

	// State metadata (populated by loader)
	StateDurations map[StateID]time.Duration // Max duration per state (0 = instant/event-driven)
	StateIndices   map[StateID]int           // Deterministic index for color mapping
	StateCount     int                       // Total non-Root states for normalization
}

// Node represents a state in the hierarchy
type Node[T any] struct {
	ID       StateID
	Name     string
	ParentID StateID

	// Optimization: Pre-calculated path from Root to this node
	// Used for zero-allocation LCA (Lowest Common Ancestor) lookup
	Path []StateID

	// Lifecycle Actions
	OnEnter  []Action[T]
	OnUpdate []Action[T]
	OnExit   []Action[T]

	// Transitions sorted by evaluation priority
	Transitions []Transition[T]
}

// Transition defines a link between states
type Transition[T any] struct {
	TargetID    StateID
	Event       event.EventType   // 0 = Tick (auto-transition)
	Guard       GuardFunc[T]      // nil = Always true
	CaptureVars map[string]string // Payload field → FSM variable name
	Actions     []Action[T]       // Executed between exit and enter phases
	Internal    bool
}

// Action represents a side-effect
type Action[T any] struct {
	Func    ActionFunc[T]
	Args    any          // Pre-compiled struct/payload
	Guard   GuardFunc[T] // Conditional execution (nil = always)
	DelayMs int          // Delay before execution (0 = immediate)
}

type DelayedAction[T any] struct {
	Remaining time.Duration // Countdown decremented by dt (was TimeInState threshold)
	Owner     StateID       // Cleared when owner state exits
	Action    Action[T]
}

// GuardFunc returns true if the transition should occur
// payload is the event payload (nil for Tick transitions and action guards)
type GuardFunc[T any] func(ctx T, region *RegionState, payload any) bool

// ActionFunc executes a side effect
type ActionFunc[T any] func(ctx T, args any)

// GuardFactoryFunc creates a parameterized guard from JSON args
// Used for configurable guards like StateTimeExceeds with duration parameter
// Return errors (invalid args surface at load, not panic)
type GuardFactoryFunc[T any] func(m *Machine[T], args map[string]any) (GuardFunc[T], error)

// ArgCompiler builds the pre-compiled Args value for an action from its config
// Runs once at load; returning an error fails the load with a precise diagnostic
type ArgCompiler[T any] func(m *Machine[T], cfg ActionConfig, resolve StateResolver) (any, error)

// StateResolver maps a state name to its ID during load
// Valid only for the duration of the compile call
type StateResolver func(name string) (StateID, bool)
