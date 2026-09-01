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

// DelayedSnapshot is one pending delayed action, identified by its region, its
// owning state and its position in that region's queue. The action itself is
// compiled configuration and is re-resolved on install; only the countdown and
// the owner travel.
type DelayedSnapshot struct {
	Region    string        `json:"region"`
	Index     int           `json:"index"`
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
		for i, da := range m.delayedActions[name] {
			out.Delayed = append(out.Delayed, DelayedSnapshot{
				Region:    name,
				Index:     i,
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

// Import installs a captured runtime position onto this machine.
//
// Regions are placed directly rather than transitioned into: the capture
// describes a machine that has already entered these states, so re-running their
// entry actions would emit every event that entry produced a second time. What
// the caller receives is a machine standing where the sender's stood.
//
// A region or state the configuration does not define is an error. The two sides
// are meant to be running the same build; a silent skip would leave one region on
// this instance's own trajectory while the rest adopted the capture's.
func (m *Machine[T]) Import(ctx T, state MachineState) error {
	// Resolve everything before writing: a half-imported machine is worse than one
	// that refused, because it looks like it worked.
	type placement struct {
		region  *RegionState
		stateID StateID
		node    *Node[T]
		snap    RegionSnapshot
	}
	placements := make([]placement, 0, len(state.Regions))
	for _, rs := range state.Regions {
		stateID, ok := m.GetStateID(rs.ActiveState)
		if !ok {
			return fmt.Errorf("fsm: capture names state %q, which this configuration does not define", rs.ActiveState)
		}
		node, ok := m.nodes[stateID]
		if !ok {
			return fmt.Errorf("fsm: state %q resolves to no node", rs.ActiveState)
		}
		region := m.regions[rs.Name]
		if region == nil {
			if _, declared := m.regionConfigs[rs.Name]; !declared {
				return fmt.Errorf("fsm: capture names region %q, which this configuration does not declare", rs.Name)
			}
			region = &RegionState{Name: rs.Name}
		}
		placements = append(placements, placement{region: region, stateID: stateID, node: node, snap: rs})
	}

	for _, p := range placements {
		if _, existed := m.regions[p.snap.Name]; !existed {
			m.regionOrder = append(m.regionOrder, p.snap.Name)
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
	present := make(map[string]bool, len(state.Regions))
	for _, rs := range state.Regions {
		present[rs.Name] = true
	}
	for _, name := range append([]string(nil), m.regionOrder...) {
		if !present[name] {
			delete(m.regions, name)
			delete(m.delayedActions, name)
		}
	}
	m.regionOrder = m.regionOrder[:0]
	for _, rs := range state.Regions {
		m.regionOrder = append(m.regionOrder, rs.Name)
	}

	m.variables = make(map[string]int64, len(state.Variables))
	for _, v := range state.Variables {
		m.variables[v.Name] = v.Value
	}

	// Delayed actions keep their compiled Action from this build's configuration
	// and adopt the capture's countdown. The queue is rebuilt in capture order.
	for name := range m.delayedActions {
		m.delayedActions[name] = nil
	}
	for _, d := range state.Delayed {
		owner, ok := m.GetStateID(d.Owner)
		if !ok {
			return fmt.Errorf("fsm: delayed action names owner state %q, which this configuration does not define", d.Owner)
		}
		node, ok := m.nodes[owner]
		if !ok || d.Index < 0 || d.Index >= len(node.OnEnter) {
			// The action list a delayed entry indexes into is configuration; an
			// index outside it means the two builds disagree about the state.
			return fmt.Errorf("fsm: delayed action %d of state %q is outside this build's action list", d.Index, d.Owner)
		}
		m.delayedActions[d.Region] = append(m.delayedActions[d.Region], DelayedAction[T]{
			Remaining: d.Remaining,
			Owner:     owner,
			Action:    node.OnEnter[d.Index],
		})
	}

	m.refreshActive()
	return nil
}
