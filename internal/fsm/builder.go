package fsm

import (
	"fmt"
	"maps"
	"slices"
)

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

func (m *Machine[T]) AddTransition(sourceID StateID, t Transition[T]) {
	if node, ok := m.nodes[sourceID]; ok {
		node.Transitions = append(node.Transitions, t)
		node.triggers.set(t.Event)
	}
}

// CompilePaths gives every node its root-to-leaf path, which is what makes LCA
// lookups O(1) at runtime; it runs after the last node is added and before Init.
// The walk is in ID order, which the loader assigns by name, so a cycle is
// reported from the same state on every load.
func (m *Machine[T]) CompilePaths() error {
	m.reacts.clear()
	for _, id := range slices.Sorted(maps.Keys(m.nodes)) {
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
