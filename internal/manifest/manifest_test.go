package manifest

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// scratchWorld builds a world with resources but no services, enough to
// construct every system and read its declarations
func scratchWorld(t *testing.T) *engine.World {
	t.Helper()
	event.EnsureRegistry()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	return w
}

// TestActiveSystemsMatchRuntimeNames asserts the manifest key equals the name
// the system answers to. Config validation reads ActiveSystems while the
// runtime toggle matches System.Name(), so a divergence makes a valid config
// name do nothing and a working name fail validation.
func TestActiveSystemsMatchRuntimeNames(t *testing.T) {
	built := BuildSystems(scratchWorld(t))
	declared := ActiveSystems()

	if len(built) != len(declared) {
		t.Fatalf("BuildSystems returns %d systems, ActiveSystems %d", len(built), len(declared))
	}
	for i, sys := range built {
		if sys.Name() != declared[i] {
			t.Errorf("manifest key %q but System.Name() is %q", declared[i], sys.Name())
		}
	}
}
