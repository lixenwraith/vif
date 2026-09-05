package fsm

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/event"
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

	triggers triggerMask // union over ActivePath; rebuilt on every commit
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
	compiledActions map[uint32]Action[T]
	nextActionID    uint32

	// State metadata (populated by loader)
	StateDurations map[StateID]time.Duration // Max duration per state (0 = instant/event-driven)
	StateIndices   map[StateID]int           // Deterministic index for color mapping
	StateCount     int                       // Total non-Root states for normalization

	// active is the union of unpaused regions' trigger sets; HandleEvent
	// rejects an unmatched event with one probe and no graph walk
	active triggerMask

	// OnTransition observes committed state changes. Fires before the exit
	// phase, so a transition record precedes the records its actions emit.
	// trigger is EventNone for tick transitions. nil disables.
	OnTransition func(region string, from, to StateID, trigger event.EventType, internal bool)

	// OnRegion observes region lifecycle: init, spawn, terminate, pause,
	// resume. nil disables.
	OnRegion func(op, region string, state StateID)
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

	triggers triggerMask // event types this node's transitions react to
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
	ID            uint32 // Deterministic compiled-config identity; zero only for programmatic actions
	Func          ActionFunc[T]
	Args          any          // Pre-compiled struct/payload
	Guard         GuardFunc[T] // Conditional execution (nil = always)
	DelayMs       int          // Delay before execution (0 = immediate)
	Reconcile     bool         // Replay when an imported position crosses this lifecycle boundary
	ReconcileVars []string     // Payload variables whose changed value also crosses the boundary
}

type DelayedAction[T any] struct {
	Remaining time.Duration // Countdown decremented by dt (was TimeInState threshold)
	Owner     StateID       // Cleared when owner state exits
	Action    Action[T]     // Carries the compiled ID used by capture restore
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

// triggerMask is the set of event types a node, region, or machine reacts to.
// EventNone is never recorded: tick transitions are evaluated by Update, not
// by HandleEvent, so bit 0 stays clear and rejects the sentinel for free.
type triggerMask [(event.EventTypeCount + 63) / 64]uint64

func (t *triggerMask) set(e event.EventType) {
	if e > 0 && int(e) < event.EventTypeCount {
		t[e>>6] |= 1 << uint(e&63)
	}
}

func (t *triggerMask) or(o *triggerMask) {
	for i := range t {
		t[i] |= o[i]
	}
}

func (t *triggerMask) clear() {
	for i := range t {
		t[i] = 0
	}
}

// has reports whether e can match a transition in this set.
func (t *triggerMask) has(e event.EventType) bool {
	if e <= 0 || int(e) >= event.EventTypeCount {
		return false
	}
	return t[e>>6]&(1<<uint(e&63)) != 0
}

// RegionTelemetry is a per-region runtime snapshot for status publication.
// Active is false when the region is not currently instantiated.
type RegionTelemetry struct {
	State       string
	StateID     StateID
	TimeInState time.Duration
	MaxDuration time.Duration
	Index       int
	Paused      bool
	Active      bool
}
