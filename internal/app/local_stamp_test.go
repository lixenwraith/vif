package app

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// unstampedLocal pins the Local-class types some producer still pushes in the
// ambient domain. Phase 5 stamped the owner-authored grants and D-6 effects and
// Phase 6 stamped internal/mode, but app, engine, fsm and the shared species
// systems still push these unstamped. The set must only shrink: an entry that
// stops appearing fails, and a type not listed here fails on first sight.
// Not a transport gate — the class keeps a Local type off the wire whatever its
// tag — but a per-instance effect journaled as shared is a record two instances
// legitimately differ on while claiming they should not.
// TODO: empty this, then delete it and the exemption with it.
var unstampedLocal = map[string]bool{
	"EventCombatAttackAreaRequest":  true,
	"EventDecaySpawnOne":            true,
	"EventDrainPause":               true,
	"EventDrainResume":              true,
	"EventDustAllRequest":           true,
	"EventEnergySetRequest":         true,
	"EventFuseQuasarRequest":        true,
	"EventGamePauseChanged":         true,
	"EventGamePauseRequest":         true,
	"EventGameSpeedChanged":         true,
	"EventGrayoutEnd":               true,
	"EventGrayoutStart":             true,
	"EventHeatSetRequest":           true,
	"EventLightningSpawnRequest":    true,
	"EventMetaStatusMessageRequest": true,
	"EventMissileSpawnRequest":      true,
	"EventModeChanged":              true,
	"EventScreenResize":             true,
	"EventStrobeRequest":            true,
}

// TestLocalEventsCarryThePlayerDomain asserts that a Local-class record is tagged
// player. The class already keeps it out of the transported set, so this is about
// the record being honest: a per-instance effect journaled as shared is a record
// two instances will legitimately differ on while claiming they should not.
//
// core.DomainShared is the zero value and the ambient domain defaults to it, so
// every type reported here is a push site that never stamped.
func TestLocalEventsCarryThePlayerDomain(t *testing.T) {
	if testing.Short() {
		t.Skip("soak")
	}

	const seed, steps = 0x10CA1, 1500

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()

	unstamped := make(map[string]int)
	a.SetDispatchTap(func(ev event.GameEvent) {
		if event.ClassOf(ev.Type) == event.ClassLocal && ev.Domain == core.DomainShared {
			unstamped[event.GetEventName(ev.Type)]++
		}
	})

	if _, err := RunScript(a, DefaultScript(seed, steps)); err != nil {
		t.Fatalf("soak: %v", err)
	}

	var bad []string
	for name, n := range unstamped {
		if !unstampedLocal[name] {
			bad = append(bad, fmt.Sprintf(
				"%s: %d records tagged shared; stamp the push site or add it to unstampedLocal", name, n))
		}
	}
	for name := range unstampedLocal {
		if unstamped[name] == 0 {
			bad = append(bad, name+": listed in unstampedLocal but every push now stamps; drop the entry")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("local-class stamping drifted:\n  %s", strings.Join(bad, "\n  "))
	}
	t.Logf("%d local-class types still push unstamped", len(unstamped))
}
