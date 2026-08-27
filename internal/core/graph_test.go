package core

import (
	"errors"
	"strings"
	"testing"
)

// TestTopoSortOrdersDependenciesFirst asserts every dependency precedes its dependents
func TestTopoSortOrdersDependenciesFirst(t *testing.T) {
	deps := map[string][]string{
		"render":   {"terminal", "content"},
		"audio":    {"content"},
		"content":  nil,
		"network":  nil,
		"terminal": nil,
	}

	got, err := TopoSort(deps)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(got) != len(deps) {
		t.Fatalf("got %d names, want %d", len(got), len(deps))
	}

	seen := make(map[string]bool, len(got))
	for _, name := range got {
		for _, dep := range deps[name] {
			if !seen[dep] {
				t.Errorf("%s ordered before its dependency %s: %v", name, dep, got)
			}
		}
		seen[name] = true
	}
}

// TestTopoSortIsDeterministic asserts one graph always resolves to one order,
// independent of map iteration
func TestTopoSortIsDeterministic(t *testing.T) {
	deps := map[string][]string{
		"a": nil, "b": nil,
		"c": {"a"}, "d": {"a"}, "e": {"b"}, "f": {"b"},
		"g": {"c", "e"},
	}
	want := "a b c d e f g"

	for range 64 {
		got, err := TopoSort(deps)
		if err != nil {
			t.Fatalf("TopoSort: %v", err)
		}
		if joined := strings.Join(got, " "); joined != want {
			t.Fatalf("got %s, want %s", joined, want)
		}
	}
}

// TestTopoSortEmpty asserts an empty graph is legal
func TestTopoSortEmpty(t *testing.T) {
	got, err := TopoSort(nil)
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// TestTopoSortUnknownDependency asserts the error names both endpoints
func TestTopoSortUnknownDependency(t *testing.T) {
	_, err := TopoSort(map[string][]string{"a": {"ghost"}})

	var unknown *UnknownDependencyError
	if !errors.As(err, &unknown) {
		t.Fatalf("got %v, want UnknownDependencyError", err)
	}
	if unknown.Name != "a" || unknown.Dependency != "ghost" {
		t.Errorf("got %+v, want {a ghost}", unknown)
	}
}

// TestTopoSortCycle asserts a cycle is reported with the nodes it holds
func TestTopoSortCycle(t *testing.T) {
	deps := map[string][]string{
		"root": nil,
		"a":    {"c"},
		"b":    {"a"},
		"c":    {"b", "root"},
	}

	_, err := TopoSort(deps)

	var cycle *CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("got %v, want CycleError", err)
	}
	if got := strings.Join(cycle.Unresolved, " "); got != "a b c" {
		t.Errorf("unresolved %s, want a b c", got)
	}
}

// TestTopoSortSelfCycle asserts a node depending on itself is a cycle, not a hang
func TestTopoSortSelfCycle(t *testing.T) {
	_, err := TopoSort(map[string][]string{"a": {"a"}})

	var cycle *CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("got %v, want CycleError", err)
	}
}
