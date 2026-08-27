package core

import (
	"fmt"
	"slices"
	"strings"
)

// UnknownDependencyError names an edge whose target is absent from the graph
type UnknownDependencyError struct {
	Name       string // the node declaring the edge
	Dependency string // the name it declares, which no node provides
}

func (e *UnknownDependencyError) Error() string {
	return fmt.Sprintf("%s depends on unregistered %s", e.Name, e.Dependency)
}

// CycleError names the nodes a cycle left unresolved, sorted
type CycleError struct {
	Unresolved []string
}

func (e *CycleError) Error() string {
	return "circular dependency between " + strings.Join(e.Unresolved, ", ")
}

// TopoSort orders names so every dependency precedes its dependents, using
// Kahn's algorithm. Callers pass a name to the names it depends on.
//
// The result is deterministic: names are sorted before the graph is walked and
// ready nodes are appended in that order, so an order that feeds RNG seeding or
// entity creation never varies between runs.
func TopoSort(deps map[string][]string) ([]string, error) {
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	slices.Sort(names)

	inDegree := make(map[string]int, len(deps))
	dependents := make(map[string][]string, len(deps)) // dependency -> names requiring it

	for _, name := range names {
		inDegree[name] = 0 // Initialize entry
		for _, dep := range deps[name] {
			if _, exists := deps[dep]; !exists {
				return nil, &UnknownDependencyError{Name: name, Dependency: dep}
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// Seed from sorted names so the walk starts identically every run
	queue := make([]string, 0, len(names))
	for _, name := range names {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]string, 0, len(names))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)

		// Sorting siblings keeps equal-depth nodes in a fixed order
		ready := dependents[name]
		slices.Sort(ready)

		for _, dependent := range ready {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(names) {
		return nil, &CycleError{Unresolved: unresolved(names, result)}
	}
	return result, nil
}

// unresolved returns the sorted names the walk never reached
func unresolved(names, resolved []string) []string {
	done := make(map[string]bool, len(resolved))
	for _, n := range resolved {
		done[n] = true
	}
	rest := make([]string, 0, len(names)-len(resolved))
	for _, n := range names { // names is already sorted
		if !done[n] {
			rest = append(rest, n)
		}
	}
	return rest
}
