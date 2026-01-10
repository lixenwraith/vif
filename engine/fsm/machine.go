package fsm

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/event"
)

// NewMachine creates a new FSM instance
func NewMachine[T any]() *Machine[T] {
	m := &Machine[T]{
		nodes:           make(map[StateID]*Node[T]),
		regionInitials:  make(map[string]StateID),
		regions:         make(map[string]*RegionState),
		guardReg:        make(map[string]GuardFunc[T]),
		guardFactoryReg: make(map[string]GuardFactoryFunc[T]),
		actionReg:       make(map[string]ActionFunc[T]),
		// Pre-allocate path slice to avoid resize during typical depth operations
		StateDurations: make(map[StateID]time.Duration),
		StateIndices:   make(map[StateID]int),
		StateCount:     0,
	}
	return m
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

// Init initializes all configured regions
func (m *Machine[T]) Init(ctx T) error {
	if len(m.regionInitials) == 0 {
		return fmt.Errorf("FSM has no defined regions to initialize")
	}

	// Multi-region initialization
	for regionName, regionInitial := range m.regionInitials {
		if err := m.initRegion(ctx, regionName, regionInitial); err != nil {
			return fmt.Errorf("region '%s': %w", regionName, err)
		}
	}

	return nil
}

// initRegion initializes a single region
func (m *Machine[T]) initRegion(ctx T, regionName string, initialID StateID) error {
	node, ok := m.nodes[initialID]
	if !ok {
		return fmt.Errorf("initial state ID %d not found", initialID)
	}

	region := &RegionState{
		Name:          regionName,
		ActiveStateID: initialID,
		TimeInState:   0,
		ActivePath:    make([]StateID, len(node.Path)),
		Paused:        false,
	}
	copy(region.ActivePath, node.Path)

	m.regions[regionName] = region

	// Execute OnEnter for the entire chain from Root to Initial
	for _, id := range region.ActivePath {
		if n, exists := m.nodes[id]; exists {
			for _, action := range n.OnEnter {
				action.Func(ctx, action.Args)
			}
		}
	}

	return nil
}

// Update advances the FSM by delta time for all active regions
func (m *Machine[T]) Update(ctx T, dt time.Duration) {
	for _, region := range m.regions {
		if region.Paused {
			continue
		}
		m.updateRegion(ctx, region, dt)
	}
}

// updateRegion advances a single region, handling automatic transitions (Event == 0) and per-tick actions
func (m *Machine[T]) updateRegion(ctx T, region *RegionState, dt time.Duration) {
	if region.ActiveStateID == StateNone {
		return
	}

	region.TimeInState += dt

	// Execute OnUpdate actions for current leaf state
	leaf := m.nodes[region.ActiveStateID]
	for _, action := range leaf.OnUpdate {
		action.Func(ctx, action.Args)
	}

	// Evaluate Tick Transitions (Event == 0), bubble up
	currID := region.ActiveStateID
	for currID != StateNone {
		node := m.nodes[currID]
		for _, trans := range node.Transitions {
			if trans.Event == 0 {
				if trans.Guard == nil || trans.Guard(ctx, region) {
					m.transitionRegion(ctx, region, trans.TargetID)
					return
				}
			}
		}
		currID = node.ParentID
	}
}

// HandleEvent routes an external event through all active regions
// Returns true if the event triggered a transition or was consumed
func (m *Machine[T]) HandleEvent(ctx T, eventType event.EventType) bool {
	handled := false

	for _, region := range m.regions {
		if region.Paused {
			continue
		}
		if m.handleEventInRegion(ctx, region, eventType) {
			handled = true
		}
	}

	return handled
}

// handleEventInRegion processes event in a single region
func (m *Machine[T]) handleEventInRegion(ctx T, region *RegionState, eventType event.EventType) bool {
	if region.ActiveStateID == StateNone {
		return false
	}

	// Bubble up: Leaf -> Parent -> Root
	currID := region.ActiveStateID
	for currID != StateNone {
		node := m.nodes[currID]
		for _, trans := range node.Transitions {
			if trans.Event == eventType {
				if trans.Guard == nil || trans.Guard(ctx, region) {
					m.transitionRegion(ctx, region, trans.TargetID)
					return true
				}
			}
		}
		currID = node.ParentID
	}

	return false
}

// transitionRegion performs state change within a specific region
func (m *Machine[T]) transitionRegion(ctx T, region *RegionState, targetID StateID) {
	if region.ActiveStateID == targetID {
		return
	}

	targetNode, ok := m.nodes[targetID]
	if !ok {
		panic(fmt.Sprintf("FSM: Attempted transition to unknown state ID %d in region '%s'", targetID, region.Name))
	}

	// Find LCA
	lcaIndex := -1
	currentPath := region.ActivePath
	targetPath := targetNode.Path

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

	// Exit Phase: walk UP from current leaf to LCA (exclusive)
	for i := len(currentPath) - 1; i > lcaIndex; i-- {
		nodeID := currentPath[i]
		if node, exists := m.nodes[nodeID]; exists {
			for _, action := range node.OnExit {
				action.Func(ctx, action.Args)
			}
		}
	}

	// Enter Phase: walk DOWN from LCA (exclusive) to target leaf
	for i := lcaIndex + 1; i < len(targetPath); i++ {
		nodeID := targetPath[i]
		if node, exists := m.nodes[nodeID]; exists {
			for _, action := range node.OnEnter {
				action.Func(ctx, action.Args)
			}
		}
	}

	// Update region state
	region.ActiveStateID = targetID
	region.TimeInState = 0

	if cap(region.ActivePath) >= len(targetPath) {
		region.ActivePath = region.ActivePath[:len(targetPath)]
		copy(region.ActivePath, targetPath)
	} else {
		region.ActivePath = make([]StateID, len(targetPath))
		copy(region.ActivePath, targetPath)
	}
}

// Reset returns FSM to initial state for all regions
func (m *Machine[T]) Reset(ctx T) error {
	// Exit all regions
	for _, region := range m.regions {
		if region.ActiveStateID != StateNone {
			for i := len(region.ActivePath) - 1; i >= 0; i-- {
				if node, ok := m.nodes[region.ActivePath[i]]; ok {
					for _, action := range node.OnExit {
						action.Func(ctx, action.Args)
					}
				}
			}
		}
	}

	// ClearAllComponent regions
	m.regions = make(map[string]*RegionState)

	// Re-initialize using loaded config
	return m.Init(ctx)
}

// === Region Lifecycle Methods ===

// SpawnRegion creates a new parallel region
func (m *Machine[T]) SpawnRegion(ctx T, regionName string, initialID StateID) error {
	if _, exists := m.regions[regionName]; exists {
		return fmt.Errorf("region '%s' already exists", regionName)
	}
	return m.initRegion(ctx, regionName, initialID)
}

// TerminateRegion destroys a region, executing exit actions
func (m *Machine[T]) TerminateRegion(ctx T, regionName string) error {
	region, ok := m.regions[regionName]
	if !ok {
		return fmt.Errorf("region '%s' not found", regionName)
	}

	// Execute OnExit for entire path
	for i := len(region.ActivePath) - 1; i >= 0; i-- {
		if node, exists := m.nodes[region.ActivePath[i]]; exists {
			for _, action := range node.OnExit {
				action.Func(ctx, action.Args)
			}
		}
	}

	delete(m.regions, regionName)
	return nil
}

// PauseRegion suspends a region's evaluation
func (m *Machine[T]) PauseRegion(regionName string) {
	if region, ok := m.regions[regionName]; ok {
		region.Paused = true
	}
}

// ResumeRegion resumes a region's evaluation
func (m *Machine[T]) ResumeRegion(regionName string) {
	if region, ok := m.regions[regionName]; ok {
		region.Paused = false
	}
}

// HasRegion checks if a region exists
func (m *Machine[T]) HasRegion(regionName string) bool {
	_, ok := m.regions[regionName]
	return ok
}

// GetRegionState returns current state name for a region
func (m *Machine[T]) GetRegionState(regionName string) string {
	if region, ok := m.regions[regionName]; ok {
		if node, ok := m.nodes[region.ActiveStateID]; ok {
			return node.Name
		}
	}
	return ""
}

// RegionTimeInState returns time spent in current state for a region
func (m *Machine[T]) RegionTimeInState(regionName string) time.Duration {
	if region, ok := m.regions[regionName]; ok {
		return region.TimeInState
	}
	return 0
}

// RegionStateID returns the active StateID for a named region
// Returns StateNone if region doesn't exist
func (m *Machine[T]) RegionStateID(regionName string) StateID {
	if region, ok := m.regions[regionName]; ok {
		return region.ActiveStateID
	}
	return StateNone
}