package fsm

import (
	"fmt"
	"slices"
)

// AddState adds a node to the machine manually
// Useful for constructing the graph programmatically or during scenario load
func (m *Machine[T]) AddState(id StateID, name string, parentID StateID) *Node[T] {
	node := &Node[T]{
		ID:          id,
		Name:        name,
		ParentID:    parentID,
		Transitions: make([]Transition[T], 0),
		OnEnter:     make([]Action[T], 0),
		OnUpdate:    make([]Action[T], 0),
		OnExit:      make([]Action[T], 0),
	}
	m.nodes[id] = node
	return node
}

// AddTransition adds a transition to a specific node
func (m *Machine[T]) AddTransition(sourceID StateID, t Transition[T]) {
	if node, ok := m.nodes[sourceID]; ok {
		node.Transitions = append(node.Transitions, t)
		node.triggers.set(t.Event)
	}
}

// CompilePaths calculates the Path slice for every node in the graph
// Must be called after all nodes are added and before Init
// This ensures O(1) LCA lookups during runtime
func (m *Machine[T]) CompilePaths() error {
	m.reacts.clear()
	// In ID order, the loader's name order, so a cycle is reported by the same
	// state every load
	ids := make([]StateID, 0, len(m.nodes))
	for id := range m.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		node := m.nodes[id]
		path := make([]StateID, 0, 4)
		curr := node

		// Walk up to root; a path longer than the graph has looped
		for curr != nil {
			if len(path) == len(m.nodes) {
				return fmt.Errorf("state '%s': parent cycle", node.Name)
			}
			path = append(path, curr.ID)
			if curr.ParentID == StateNone {
				break
			}
			var ok bool
			parentID := curr.ParentID
			curr, ok = m.nodes[parentID]
			if !ok {
				return fmt.Errorf("node %d references missing parent %d", id, parentID)
			}
		}

		// Reverse to get [Root, ..., Leaf]
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}

		node.Path = path

		// Trigger mask: the event types this node alone reacts to. Regions
		// union these along the active path.
		node.triggers.clear()
		for i := range node.Transitions {
			node.triggers.set(node.Transitions[i].Event)
		}
		m.reacts.or(&node.triggers)
	}
	return nil
}
