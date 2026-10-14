package manifest

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
)

// buildWorld returns a world carrying every manifest system
func buildWorld(t *testing.T) *engine.World {
	t.Helper()
	w := scratchWorld(t)
	for _, sys := range BuildSystems(w) {
		w.AddSystem(sys, ProfileFor(sys.Name()))
	}
	return w
}

// TestSystemGraphResolves asserts every declared dependency names a registered
// system and the graph is acyclic
func TestSystemGraphResolves(t *testing.T) {
	if err := buildWorld(t).CheckSystemDependencies(); err != nil {
		t.Fatalf("system graph: %v", err)
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
	for _, profile := range SystemProfiles() {
		for _, dep := range profile.Requires {
			if dep.Name == profile.Name {
				t.Errorf("%s declares itself a dependency", profile.Name)
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
