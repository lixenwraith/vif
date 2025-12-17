package fsm

import (
	"time"

	"github.com/lixenwraith/vi-fighter/events"
)

// StateID is a unique identifier for a node
type StateID int

const (
	StateNone StateID = 0
	StateRoot StateID = 1
)

// Machine is the generic Hierarchical Finite State Machine runtime
// T is the context type passed to actions and guards (e.g., *engine.World)
type Machine[T any] struct {
	// Graph Data (Immutable after load)
	nodes map[StateID]*Node[T]

	// Configuration
	InitialStateID StateID // Stored during load for reset/init

	// Runtime State
	activeStateID StateID       // The current leaf node
	timeInState   time.Duration // Time elapsed in current state
	activePath    []StateID     // Stack of active states (Root -> Child -> Leaf)

	// Dependency Injection
	guardReg        map[string]GuardFunc[T]
	guardFactoryReg map[string]GuardFactoryFunc[T]
	actionReg       map[string]ActionFunc[T]
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
	Event    events.EventType // 0 = Tick (auto-transition)
	Guard    GuardFunc[T]     // nil = Always true
}

// Action represents a side-effect
type Action[T any] struct {
	Func ActionFunc[T]
	Args any // Pre-compiled struct/payload
}

// GuardFunc returns true if the transition should occur
type GuardFunc[T any] func(ctx T) bool

// ActionFunc executes a side effect
type ActionFunc[T any] func(ctx T, args any)

// GuardFactoryFunc creates a parameterized guard from JSON args
// Used for configurable guards like StateTimeExceeds with duration parameter
type GuardFactoryFunc[T any] func(m *Machine[T], args map[string]any) GuardFunc[T]