package fsm

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/events"
)

// NewMachine creates a new FSM instance
func NewMachine[T any]() *Machine[T] {
	return &Machine[T]{
		nodes:           make(map[StateID]*Node[T]),
		guardReg:        make(map[string]GuardFunc[T]),
		guardFactoryReg: make(map[string]GuardFactoryFunc[T]),
		actionReg:       make(map[string]ActionFunc[T]),
		// Pre-allocate path slice to avoid resize during typical depth operations
		activePath: make([]StateID, 0, 8),
	}
}

// RegisterGuard adds a predicate function to the registry
func (m *Machine[T]) RegisterGuard(name string, fn GuardFunc[T]) {
	m.guardReg[name] = fn
}

// RegisterGuardFactory adds a parameterized guard factory to the registry
func (m *Machine[T]) RegisterGuardFactory(name string, factory GuardFactoryFunc[T]) {
	m.guardFactoryReg[name] = factory
}

// RegisterAction adds a side-effect function to the registry
func (m *Machine[T]) RegisterAction(name string, fn ActionFunc[T]) {
	m.actionReg[name] = fn
}

// Init sets the initial state and runs its entry actions
// Must be called after loading nodes
func (m *Machine[T]) Init(ctx T, initialID StateID) error {
	node, ok := m.nodes[initialID]
	if !ok {
		return fmt.Errorf("initial state ID %d not found", initialID)
	}

	// Set state directly without full transition logic (no exit)
	m.activeStateID = initialID
	m.timeInState = 0
	m.activePath = make([]StateID, len(node.Path))
	copy(m.activePath, node.Path)

	// Execute OnEnter for the entire chain from Root to Initial
	// Since we are starting fresh, we enter everything
	for _, id := range m.activePath {
		if n, exists := m.nodes[id]; exists {
			for _, action := range n.OnEnter {
				action.Func(ctx, action.Args)
			}
		}
	}

	return nil
}

// Update advances the FSM by delta time
// Handles automatic transitions (Event == 0) and per-tick actions
func (m *Machine[T]) Update(ctx T, dt time.Duration) {
	if m.activeStateID == StateNone {
		return
	}

	m.timeInState += dt

	// 1. Execute OnUpdate actions for current active state (Leaf)
	// Design Choice: Do we update parents? Typically only the active leaf updates logic.
	// We will execute only the active leaf updates to keep performance high.
	leaf := m.nodes[m.activeStateID]
	for _, action := range leaf.OnUpdate {
		action.Func(ctx, action.Args)
	}

	// 2. Evaluate Tick Transitions (Event == 0)
	// Bubble up: Leaf -> Parent -> Root
	currID := m.activeStateID
	for currID != StateNone {
		node := m.nodes[currID]
		for _, trans := range node.Transitions {
			// 0 denotes a Tick/Auto transition
			if trans.Event == 0 {
				if trans.Guard == nil || trans.Guard(ctx) {
					m.TransitionTo(ctx, trans.TargetID)
					return // State changed, stop processing this tick
				}
			}
		}
		currID = node.ParentID
	}
}

// HandleEvent routes an external event through the state hierarchy
// Returns true if the event triggered a transition or was consumed
func (m *Machine[T]) HandleEvent(ctx T, eventType events.EventType) bool {
	if m.activeStateID == StateNone {
		return false
	}

	// Bubble up: Leaf -> Parent -> Root
	currID := m.activeStateID
	for currID != StateNone {
		node := m.nodes[currID]
		for _, trans := range node.Transitions {
			if trans.Event == eventType {
				if trans.Guard == nil || trans.Guard(ctx) {
					m.TransitionTo(ctx, trans.TargetID)
					return true
				}
			}
		}
		currID = node.ParentID
	}

	return false
}

// TransitionTo performs a state change from current to target
// Executes OnExit (up to LCA) and OnEnter (down from LCA)
func (m *Machine[T]) TransitionTo(ctx T, targetID StateID) {
	if m.activeStateID == targetID {
		return
	}

	targetNode, ok := m.nodes[targetID]
	if !ok {
		// In production, log error. For now, silent fail or panic?
		// Panic is safer to catch config errors early.
		panic(fmt.Sprintf("FSM: Attempted transition to unknown state ID %d", targetID))
	}

	// 1. Find LCA (Lowest Common Ancestor)
	// We rely on the pre-calculated node.Path: [Root, ..., Parent, Node]
	lcaIndex := -1
	currentPath := m.activePath
	targetPath := targetNode.Path

	// Compare paths to find divergence point
	minLen := len(currentPath)
	if len(targetPath) < minLen {
		minLen = len(targetPath)
	}

	for i := 0; i < minLen; i++ {
		if currentPath[i] == targetPath[i] {
			lcaIndex = i
		} else {
			break
		}
	}

	// 2. Exit Phase
	// Walk UP from Current Leaf to LCA (exclusive of LCA)
	// currentPath indices: [len-1 ... lcaIndex+1]
	for i := len(currentPath) - 1; i > lcaIndex; i-- {
		nodeID := currentPath[i]
		if node, exists := m.nodes[nodeID]; exists {
			for _, action := range node.OnExit {
				action.Func(ctx, action.Args)
			}
		}
	}

	// 3. Enter Phase
	// Walk DOWN from LCA (exclusive) to Target Leaf
	// targetPath indices: [lcaIndex+1 ... len-1]
	for i := lcaIndex + 1; i < len(targetPath); i++ {
		nodeID := targetPath[i]
		if node, exists := m.nodes[nodeID]; exists {
			for _, action := range node.OnEnter {
				action.Func(ctx, action.Args)
			}
		}
	}

	// 4. Update State
	m.activeStateID = targetID
	m.timeInState = 0

	// Update active path (copy target path)
	// Re-use capacity if possible
	if cap(m.activePath) >= len(targetPath) {
		m.activePath = m.activePath[:len(targetPath)]
		copy(m.activePath, targetPath)
	} else {
		m.activePath = make([]StateID, len(targetPath))
		copy(m.activePath, targetPath)
	}
}

// CurrentStateID returns the ID of the current leaf state
func (m *Machine[T]) CurrentStateID() StateID {
	return m.activeStateID
}

// CurrentStateName returns the name of the current leaf state
func (m *Machine[T]) CurrentStateName() string {
	if n, ok := m.nodes[m.activeStateID]; ok {
		return n.Name
	}
	return ""
}

// TimeInState returns the duration spent in the current active state
func (m *Machine[T]) TimeInState() time.Duration {
	return m.timeInState
}