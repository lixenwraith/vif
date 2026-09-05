package fsm

import (
	"fmt"
	"sort"
	"time"
)

// MachineState is the FSM runtime's transfer contract (D-19).
//
// The state graph is immutable configuration and travels with the build, so a
// capture carries only what a run has reached: which state each region is in, how
// long it has been there, the variables guards read, and the delayed actions
// still pending. The multiplayer plan's hidden-state survey called this
// "straightforward, and small", and it is — but omitting it makes an installed
// world enter its next timed transition on a different tick than the run it
// reproduces, which is a divergence no component store shows.
//
// States and regions are named by string, never by StateID. IDs are assigned by
// sorting the configuration's state names at load, so they are stable for one
// configuration and meaningless across a changed one; a name that no longer
// resolves is an error the receiver can report rather than a silent landing in
// whichever state now holds that number.
type MachineState struct {
	Regions   []RegionSnapshot   `json:"regions"`
	Variables []VariableSnapshot `json:"variables"`
	Delayed   []DelayedSnapshot  `json:"delayed"`
}

// RegionSnapshot is one region's runtime position.
type RegionSnapshot struct {
	Name        string        `json:"name"`
	ActiveState string        `json:"active_state"`
	TimeInState time.Duration `json:"time_in_state"`
	Paused      bool          `json:"paused"`
}

// VariableSnapshot is one FSM variable. Emitted as a sorted list rather than a
// map so a capture is byte-comparable.
type VariableSnapshot struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// DelayedSnapshot is one pending delayed action, identified by its region, owning
// state, and deterministic compiled action ID. The action implementation and
// arguments are immutable configuration and are re-resolved on install; only its
// identity, countdown, and cancellation owner travel.
type DelayedSnapshot struct {
	Region    string        `json:"region"`
	ActionID  uint32        `json:"action"`
	Owner     string        `json:"owner"`
	Remaining time.Duration `json:"remaining"`
}

// Export reads the runtime position of every region, the variables, and the
// pending delayed actions, in a canonical order.
func (m *Machine[T]) Export() MachineState {
	var out MachineState

	names := append([]string(nil), m.regionOrder...)
	sort.Strings(names)
	for _, name := range names {
		region, ok := m.regions[name]
		if !ok {
			continue
		}
		out.Regions = append(out.Regions, RegionSnapshot{
			Name:        name,
			ActiveState: m.StateName(region.ActiveStateID),
			TimeInState: region.TimeInState,
			Paused:      region.Paused,
		})
		for _, da := range m.delayedActions[name] {
			out.Delayed = append(out.Delayed, DelayedSnapshot{
				Region:    name,
				ActionID:  da.Action.ID,
				Owner:     m.StateName(da.Owner),
				Remaining: da.Remaining,
			})
		}
	}

	vars := make([]string, 0, len(m.variables))
	for k := range m.variables {
		vars = append(vars, k)
	}
	sort.Strings(vars)
	for _, k := range vars {
		out.Variables = append(out.Variables, VariableSnapshot{Name: k, Value: m.variables[k]})
	}
	return out
}

// Import installs a captured runtime position without producing side effects.
// This is the validation/staging form: it proves the position can be resolved by
// this build while leaving instance-local systems untouched.
func (m *Machine[T]) Import(ctx T, state MachineState) error {
	_, err := m.importState(ctx, state, false)
	return err
}

// ImportReconciled installs a captured runtime position and replays only the
// lifecycle actions explicitly marked reconcile=true when the import crosses
// their state boundary. Those actions are restricted at load time to immediate,
// unguarded ClassLocal EmitEvent actions. One-shot local rewards and presentation
// bursts therefore remain untouched, while persistent local state follows the
// Shared FSM position that owns it.
//
// Exit actions run against the old variables before the position changes. Entry
// actions run against the imported variables afterwards. Every old exit precedes
// every new entry, so an overlapping handoff such as quasar -> storm releases the
// old hold before taking the new one.
func (m *Machine[T]) ImportReconciled(ctx T, state MachineState) (int, error) {
	return m.importState(ctx, state, true)
}

// importState resolves the complete capture before either mutating the machine or
// emitting a reconciliation event. A half-imported machine is worse than one that
// refused, because it looks like it worked.
func (m *Machine[T]) importState(ctx T, state MachineState, reconcile bool) (int, error) {
	type placement struct {
		region  *RegionState
		stateID StateID
		node    *Node[T]
		snap    RegionSnapshot
		lca     int
	}

	placements := make([]placement, 0, len(state.Regions))
	placementByName := make(map[string]int, len(state.Regions))
	present := make(map[string]bool, len(state.Regions))
	for regionIndex := range len(state.Regions) {
		rs := state.Regions[regionIndex]
		if present[rs.Name] {
			return 0, fmt.Errorf("fsm: capture names region %q more than once", rs.Name)
		}
		present[rs.Name] = true

		stateID, ok := m.GetStateID(rs.ActiveState)
		if !ok {
			return 0, fmt.Errorf("fsm: capture names state %q, which this configuration does not define", rs.ActiveState)
		}
		node, ok := m.nodes[stateID]
		if !ok {
			return 0, fmt.Errorf("fsm: state %q resolves to no node", rs.ActiveState)
		}
		region := m.regions[rs.Name]
		lca := -1
		if region == nil {
			if _, declared := m.regionConfigs[rs.Name]; !declared {
				return 0, fmt.Errorf("fsm: capture names region %q, which this configuration does not declare", rs.Name)
			}
			region = &RegionState{Name: rs.Name}
		} else {
			lca = commonPathIndex(region.ActivePath, node.Path)
		}
		placementByName[rs.Name] = len(placements)
		placements = append(placements, placement{
			region: region, stateID: stateID, node: node, snap: rs, lca: lca,
		})
	}

	variables := make(map[string]int64, len(state.Variables))
	for _, v := range state.Variables {
		if _, duplicate := variables[v.Name]; duplicate {
			return 0, fmt.Errorf("fsm: capture names variable %q more than once", v.Name)
		}
		variables[v.Name] = v.Value
	}
	if reconcile {
		// A scoped hold can stay in the same state while the imported variable
		// naming its owner changes. Treat that as crossing the owning node: release
		// with the old variables, then acquire with the imported ones.
		for i := range placements {
			p := &placements[i]
			for pathIndex := 0; pathIndex <= p.lca; pathIndex++ {
				node := m.nodes[p.node.Path[pathIndex]]
				if node != nil && (reconcileVariablesChanged(node.OnEnter, m.variables, variables) ||
					reconcileVariablesChanged(node.OnExit, m.variables, variables)) {
					p.lca = pathIndex - 1
					break
				}
			}
		}
	}

	// Delayed actions keep their compiled Action from this build's configuration
	// and adopt the capture's countdown. Resolve them before writing so an invalid
	// owner cannot fail after region placement or local reconciliation.
	delayed := make(map[string][]DelayedAction[T], len(state.Delayed))
	for _, d := range state.Delayed {
		if !present[d.Region] {
			return 0, fmt.Errorf("fsm: delayed action names inactive region %q", d.Region)
		}
		owner, ok := m.GetStateID(d.Owner)
		if !ok {
			return 0, fmt.Errorf("fsm: delayed action names owner state %q, which this configuration does not define", d.Owner)
		}
		if _, ok := m.nodes[owner]; !ok {
			return 0, fmt.Errorf("fsm: delayed action owner state %q resolves to no node", d.Owner)
		}
		action, ok := m.compiledActions[d.ActionID]
		if !ok || d.ActionID == 0 {
			return 0, fmt.Errorf("fsm: delayed action %d is not present in this build", d.ActionID)
		}
		// A scheduled action already passed its guard and delay once. Restoring the
		// compiled function/arguments must not guard it again or schedule it anew.
		action.Guard = nil
		action.DelayMs = 0
		action.Reconcile = false
		action.ReconcileVars = nil
		delayed[d.Region] = append(delayed[d.Region], DelayedAction[T]{
			Remaining: d.Remaining,
			Owner:     owner,
			Action:    action,
		})
	}

	reconciled := 0
	if reconcile {
		// Exit every path the imported position no longer holds, in the old
		// machine's deterministic region order and leaf-to-root order.
		for _, name := range m.regionOrder {
			region := m.regions[name]
			if region == nil {
				continue
			}
			lca := -1
			if i, ok := placementByName[name]; ok {
				lca = placements[i].lca
			}
			for i := len(region.ActivePath) - 1; i > lca; i-- {
				if node := m.nodes[region.ActivePath[i]]; node != nil {
					reconciled += m.executeReconcileActions(ctx, region, node.OnExit)
				}
			}
		}
	}

	for i := range placements {
		p := &placements[i]
		if _, existed := m.regions[p.snap.Name]; !existed {
			m.regions[p.snap.Name] = p.region
		}
		p.region.ActiveStateID = p.stateID
		p.region.TimeInState = p.snap.TimeInState
		p.region.Paused = p.snap.Paused
		p.region.ActivePath = append(p.region.ActivePath[:0], p.node.Path...)
		m.refreshRegionMask(p.region)
	}

	// Regions the capture does not name are not running on the sender, so they
	// must not be running here either.
	for _, name := range append([]string(nil), m.regionOrder...) {
		if !present[name] {
			delete(m.regions, name)
		}
	}
	m.regionOrder = m.regionOrder[:0]
	for regionIndex := range len(state.Regions) {
		rs := state.Regions[regionIndex]
		m.regionOrder = append(m.regionOrder, rs.Name)
	}
	m.variables = variables
	m.delayedActions = delayed

	if reconcile {
		// Enter newly held paths in capture order and root-to-leaf order. The
		// imported variables are already installed for payload_vars resolution.
		for i := range placements {
			p := &placements[i]
			for pathIndex := p.lca + 1; pathIndex < len(p.node.Path); pathIndex++ {
				if node := m.nodes[p.node.Path[pathIndex]]; node != nil {
					reconciled += m.executeReconcileActions(ctx, p.region, node.OnEnter)
				}
			}
		}
	}

	m.refreshActive()
	return reconciled, nil
}

// commonPathIndex returns the final shared index of two root-to-leaf paths, or
// -1 when they share nothing.
func commonPathIndex(a, b []StateID) int {
	lca := -1
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			break
		}
		lca = i
	}
	return lca
}

func (m *Machine[T]) executeReconcileActions(ctx T, region *RegionState, actions []Action[T]) int {
	executed := 0
	for _, action := range actions {
		if !action.Reconcile {
			continue
		}
		// Load validation excludes guards and delays. Keep the guard check so a
		// directly built machine preserves Action's ordinary contract too.
		if action.Guard != nil && !action.Guard(ctx, region, nil) {
			continue
		}
		action.Func(ctx, action.Args)
		executed++
	}
	return executed
}

func reconcileVariablesChanged[T any](actions []Action[T], old, next map[string]int64) bool {
	for _, action := range actions {
		if !action.Reconcile {
			continue
		}
		for _, name := range action.ReconcileVars {
			if old[name] != next[name] {
				return true
			}
		}
	}
	return false
}
