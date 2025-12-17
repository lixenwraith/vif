package fsm

import "fmt"

// AddState adds a node to the machine manually
// Useful for constructing the graph programmatically or during JSON load
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
	}
}

// CompilePaths calculates the Path slice for every node in the graph
// Must be called after all nodes are added and before Init
// This ensures O(1) LCA lookups during runtime
func (m *Machine[T]) CompilePaths() error {
	for id, node := range m.nodes {
		path := make([]StateID, 0, 4)
		curr := node

		// Walk up to root
		for curr != nil {
			path = append(path, curr.ID)
			if curr.ParentID == StateNone {
				break
			}
			var ok bool
			curr, ok = m.nodes[curr.ParentID]
			if !ok {
				return fmt.Errorf("node %d references missing parent %d", id, curr.ParentID)
			}
		}

		// Reverse to get [Root, ..., Leaf]
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}

		node.Path = path
	}
	return nil
}