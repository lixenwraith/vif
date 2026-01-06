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
	regionInitials map[string]StateID // Region name -> initial state ID

	// Runtime State (per-region)
	regions map[string]*RegionState

	// Dependency Injection
	guardReg        map[string]GuardFunc[T]
	guardFactoryReg map[string]GuardFactoryFunc[T]
	actionReg       map[string]ActionFunc[T]

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
	TargetID StateID
	Event    event.EventType // 0 = Tick (auto-transition)
	Guard    GuardFunc[T]    // nil = Always true
}

// Action represents a side-effect
type Action[T any] struct {
	Func ActionFunc[T]
	Args any // Pre-compiled struct/payload
}

// GuardFunc returns true if the transition should occur
type GuardFunc[T any] func(ctx T, region *RegionState) bool

// ActionFunc executes a side effect
type ActionFunc[T any] func(ctx T, args any)

// GuardFactoryFunc creates a parameterized guard from JSON args
// Used for configurable guards like StateTimeExceeds with duration parameter
type GuardFactoryFunc[T any] func(m *Machine[T], args map[string]any) GuardFunc[T]

// EmitEventArgs holds pre-compiled event data for the EmitEvent action
// Type identifies the event; Payload is the decoded struct (or nil)
type EmitEventArgs struct {
	Type    event.EventType
	Payload any
}

// RegionControlArgs holds args for region control actions
type RegionControlArgs struct {
	RegionName   string
	InitialState string // For SpawnRegion
}