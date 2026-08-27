package manifest

import (
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// buildWorld returns a world carrying every manifest system
func buildWorld(t *testing.T) *engine.World {
	t.Helper()
	w := scratchWorld(t)
	for _, sys := range BuildSystems(w) {
		w.AddSystem(sys)
	}
	return w
}

// TestSystemGraphResolves asserts every declared dependency names a registered
// system and the graph is acyclic
func TestSystemGraphResolves(t *testing.T) {
	w := buildWorld(t)

	order, err := w.SystemInitOrder()
	if err != nil {
		t.Fatalf("system graph: %v", err)
	}
	if len(order) != len(ActiveSystems()) {
		t.Fatalf("resolved %d systems, registered %d", len(order), len(ActiveSystems()))
	}
}

// TestSystemInitOrderIsDeterministic asserts one system set resolves to one
// order; a shared RNG stream derived during init must not vary between runs
func TestSystemInitOrderIsDeterministic(t *testing.T) {
	var first string
	for range 16 {
		order, err := buildWorld(t).SystemInitOrder()
		if err != nil {
			t.Fatalf("system graph: %v", err)
		}
		got := strings.Join(order, " ")
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("order varies between runs:\n %s\n %s", first, got)
		}
	}
}

// TestSystemInitOrderPrecedesDependents asserts the resolved order places every
// dependency, required or optional, ahead of the system declaring it
func TestSystemInitOrderPrecedesDependents(t *testing.T) {
	w := buildWorld(t)

	order, err := w.SystemInitOrder()
	if err != nil {
		t.Fatalf("system graph: %v", err)
	}

	position := make(map[string]int, len(order))
	for i, name := range order {
		position[name] = i
	}
	for _, sys := range w.Systems() {
		for _, dep := range sys.Requires() {
			if position[dep.Name] >= position[sys.Name()] {
				t.Errorf("%s initializes at %d, after its dependency %s at %d",
					sys.Name(), position[sys.Name()], dep.Name, position[dep.Name])
			}
		}
	}
}

// TestSystemsRequiringIsSorted asserts the dependent lookup is deterministic,
// since its output reaches diagnostics and refusal messages
func TestSystemsRequiringIsSorted(t *testing.T) {
	w := buildWorld(t)

	for _, name := range ActiveSystems() {
		for _, strength := range []engine.DependencyStrength{engine.DepRequired, engine.DepOptional} {
			got := w.SystemsRequiring(name, strength)
			if !slices.IsSorted(got) {
				t.Errorf("SystemsRequiring(%q, %v) is unsorted: %v", name, strength, got)
			}
		}
	}
}

// TestNoSystemRequiresItself asserts a self-edge is caught as a declaration
// error rather than resolved into a cycle report
func TestNoSystemRequiresItself(t *testing.T) {
	for _, sys := range buildWorld(t).Systems() {
		for _, dep := range sys.Requires() {
			if dep.Name == sys.Name() {
				t.Errorf("%s declares itself a dependency", sys.Name())
			}
		}
	}
}

// TestAllowSystemDisableRefusesRequired asserts the runtime guard refuses a
// disable a dependent cannot survive, and permits one it can
func TestAllowSystemDisableRefusesRequired(t *testing.T) {
	w := buildWorld(t)

	if w.AllowSystemDisable("composite") {
		t.Error("disabling composite was allowed; wall and every composite species require it")
	}
	if !w.AllowSystemDisable("glyph") {
		t.Error("disabling glyph was refused; every dependent declares it optional")
	}
}

// TestEverySystemDependencyIsRegistered asserts no declaration names a system
// the manifest does not build, which would fail startup rather than validation
func TestEverySystemDependencyIsRegistered(t *testing.T) {
	registered := make(map[string]bool, len(ActiveSystems()))
	for _, n := range ActiveSystems() {
		registered[n] = true
	}
	for _, p := range SystemProfiles() {
		for _, dep := range p.Requires {
			if !registered[dep.Name] {
				t.Errorf("%s declares %s, which the manifest does not build", p.Name, dep.Name)
			}
		}
	}
}
