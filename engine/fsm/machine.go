package fsm

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/event"
)

// NewMachine creates a new FSM instance
func NewMachine[T any]() *Machine[T] {
	m := &Machine[T]{
		nodes:          make(map[StateID]*Node[T]),
		regionInitials: make(map[string]StateID),
		regionConfigs:  make(map[string]*RegionConfig),
		regions:        make(map[string]*RegionState),
		variables:      make(map[string]int64),
		delayedActions: make(map[string][]DelayedAction[T]),

		guardReg:        make(map[string]GuardFunc[T]),
		guardFactoryReg: make(map[string]GuardFactoryFunc[T]),
		actionReg:       make(map[string]ActionFunc[T]),

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

// GetGuardFactory returns a registered guard factory by name
func (m *Machine[T]) GetGuardFactory(name string) (GuardFactoryFunc[T], bool) {
	fn, ok := m.guardFactoryReg[name]
	return fn, ok
}

// GetGuard returns a registered static guard by name
func (m *Machine[T]) GetGuard(name string) (GuardFunc[T], bool) {
	fn, ok := m.guardReg[name]
	return fn, ok
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

	// Clear variables on init
	m.variables = make(map[string]int64)
	m.delayedActions = make(map[string][]DelayedAction[T])

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
	m.delayedActions[regionName] = nil

	// Execute OnEnter for the entire chain from Root to Initial
	for _, id := range region.ActivePath {
		if n, exists := m.nodes[id]; exists {
			m.executeActions(ctx, region, n.OnEnter)
		}
	}

	return nil
}

// Update advances the FSM by delta time for all active regions
func (m *Machine[T]) Update(ctx T, dt time.Duration) {
	// Snapshot region names to prevent map mutation during iteration
	regionNames := make([]string, 0, len(m.regions))
	for name := range m.regions {
		regionNames = append(regionNames, name)
	}

	for _, name := range regionNames {
		region, ok := m.regions[name]
		if !ok {
			continue // Region terminated during this update
		}
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

	// Process delayed actions
	m.processDelayedActions(ctx, region)

	// Execute OnUpdate actions for current leaf state
	leaf := m.nodes[region.ActiveStateID]
	m.executeActions(ctx, region, leaf.OnUpdate)

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

// ExecuteAction runs a registered action by name
// Used for initialization hooks like ApplyGlobalSystemConfig
func (m *Machine[T]) ExecuteAction(ctx T, actionName string, args any) bool {
	fn, ok := m.actionReg[actionName]
	if !ok {
		return false
	}
	fn(ctx, args)
	return true
}

// executeActions runs a slice of actions, respecting guards and delays
func (m *Machine[T]) executeActions(ctx T, region *RegionState, actions []Action[T]) {
	for _, action := range actions {
		// Check guard
		if action.Guard != nil && !action.Guard(ctx, region) {
			continue
		}

		// Handle delay
		if action.DelayMs > 0 {
			delayed := DelayedAction[T]{
				ExecuteAt: region.TimeInState + time.Duration(action.DelayMs)*time.Millisecond,
				Action: Action[T]{
					Func:  action.Func,
					Args:  action.Args,
					Guard: nil, // Guard already evaluated
				},
			}
			m.delayedActions[region.Name] = append(m.delayedActions[region.Name], delayed)
			continue
		}

		// Execute immediately
		action.Func(ctx, action.Args)
	}
}

// processDelayedActions executes actions whose delay has elapsed
func (m *Machine[T]) processDelayedActions(ctx T, region *RegionState) {
	delayed := m.delayedActions[region.Name]
	if len(delayed) == 0 {
		return
	}

	remaining := delayed[:0]
	for _, da := range delayed {
		if region.TimeInState >= da.ExecuteAt {
			da.Action.Func(ctx, da.Action.Args)
		} else {
			remaining = append(remaining, da)
		}
	}
	m.delayedActions[region.Name] = remaining
}

// HandleEvent routes an external event through all active regions
// Returns true if the event triggered a transition or was consumed
func (m *Machine[T]) HandleEvent(ctx T, eventType event.EventType) bool {
	// Snapshot region names to prevent dispatch to regions spawned by this event
	regionNames := make([]string, 0, len(m.regions))
	for name := range m.regions {
		regionNames = append(regionNames, name)
	}

	handled := false

	for _, name := range regionNames {
		region, ok := m.regions[name]
		if !ok {
			continue // Region terminated during this event's handling
		}
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
			m.executeActions(ctx, region, node.OnExit)
		}
	}

	// Clear delayed actions on state exit
	m.delayedActions[region.Name] = nil

	// Enter Phase: walk DOWN from LCA (exclusive) to target leaf
	// Reset TimeInState before entering new state
	region.TimeInState = 0

	for i := lcaIndex + 1; i < len(targetPath); i++ {
		nodeID := targetPath[i]
		if node, exists := m.nodes[nodeID]; exists {
			m.executeActions(ctx, region, node.OnEnter)
		}
	}

	// Update region state
	region.ActiveStateID = targetID

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
					m.executeActions(ctx, region, node.OnExit)
				}
			}
		}
	}

	// Clear regions and variables
	m.regions = make(map[string]*RegionState)
	m.variables = make(map[string]int64)
	m.delayedActions = make(map[string][]DelayedAction[T])

	// Clear telemetry cache
	m.lastTelemetryRegion = ""
	m.lastTelemetryStateID = StateNone
	m.lastTelemetryTime = 0

	// Re-initialize using loaded config
	return m.Init(ctx)
}

// GetActiveRegionTelemetry returns telemetry data for display purposes
// Skips background regions; caches last foreground state for transition periods
// Returns state name, state ID, and time spent in current state
func (m *Machine[T]) GetActiveRegionTelemetry() (stateName string, stateID StateID, timeInState time.Duration) {
	var activeRegion *RegionState

	// Find first unpaused foreground region
	for name, region := range m.regions {
		if region.Paused {
			continue
		}
		if cfg := m.regionConfigs[name]; cfg != nil && cfg.Background {
			continue
		}
		activeRegion = region
		break
	}

	// Update cache if foreground region found
	if activeRegion != nil {
		m.lastTelemetryRegion = activeRegion.Name
		m.lastTelemetryStateID = activeRegion.ActiveStateID
		m.lastTelemetryTime = activeRegion.TimeInState

		if node, ok := m.nodes[activeRegion.ActiveStateID]; ok {
			return node.Name, activeRegion.ActiveStateID, activeRegion.TimeInState
		}
		return "", activeRegion.ActiveStateID, activeRegion.TimeInState
	}

	// No foreground region: return cached values
	if m.lastTelemetryStateID != StateNone {
		if node, ok := m.nodes[m.lastTelemetryStateID]; ok {
			return node.Name, m.lastTelemetryStateID, m.lastTelemetryTime
		}
	}

	return "", StateNone, 0
}

// === Variable Methods ===

// GetVar returns the value of a variable (0 if not set)
func (m *Machine[T]) GetVar(name string) int64 {
	return m.variables[name]
}

// SetVar sets a variable value
func (m *Machine[T]) SetVar(name string, value int64) {
	m.variables[name] = value
}

// IncrementVar adds delta to a variable and returns the new value
func (m *Machine[T]) IncrementVar(name string, delta int64) int64 {
	m.variables[name] += delta
	return m.variables[name]
}

// === System Configuration ===

// GetSystemsConfig returns the loaded systems configuration
func (m *Machine[T]) GetSystemsConfig() *SystemsConfig {
	return m.systemsConfig
}

// GetRegionConfig returns the configuration for a region
func (m *Machine[T]) GetRegionConfig(regionName string) *RegionConfig {
	return m.regionConfigs[regionName]
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
			m.executeActions(ctx, region, node.OnExit)
		}
	}

	delete(m.regions, regionName)
	delete(m.delayedActions, regionName)
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