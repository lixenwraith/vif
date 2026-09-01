package manifest

import (
	"sort"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// TestSnapshotDeclarationsMatchImplementations is D-19 made mechanical, and it is
// the point of the whole declaration.
//
// The multiplayer plan's hidden-state survey found that a snapshot must carry
// everything deciding a future shared outcome, that much of it is not in a
// component store — RNG positions, a maze generator, EXP3 route weights, genetic
// populations, a derivation phase, per-system scratch — and, in its own words,
// that "no inventory exists". An inventory maintained by hand goes stale the
// first time a system grows a field. So the manifest declares the obligation and
// this test asserts the declaration against the code: a system declared "state"
// must implement engine.SharedStateSaver, and a system declared "none" must not.
//
// Both directions matter. The first catches a declaration whose implementation
// was never written. The second catches the more likely drift: a system that
// grew save/load methods without anyone updating the manifest, which would leave
// a real piece of shared state out of every capture while looking implemented.
func TestSnapshotDeclarationsMatchImplementations(t *testing.T) {
	w := scratchWorld(t)
	systems := BuildSystems(w)
	if len(systems) < 50 {
		t.Fatalf("built %d systems, expected the full set; the manifest has drifted", len(systems))
	}

	declared := SnapshotDeclarations()
	var missing, undeclared []string

	for _, sys := range systems {
		name := sys.Name()
		_, implements := sys.(engine.SharedStateSaver)
		switch SnapshotFor(name) {
		case engine.SnapshotState:
			if !implements {
				missing = append(missing, name)
			}
		default:
			if implements {
				undeclared = append(undeclared, name)
			}
		}
		delete(declared, name)
	}

	sort.Strings(missing)
	sort.Strings(undeclared)
	if len(missing) > 0 {
		t.Errorf(`systems declare Snapshot: "state" but do not implement `+
			"engine.SharedStateSaver: %v", missing)
	}
	if len(undeclared) > 0 {
		t.Errorf("systems implement engine.SharedStateSaver without declaring "+
			`Snapshot: "state" in the manifest, so their state is in no capture: %v`,
			undeclared)
	}
}

// TestSnapshotStateSystemsRoundTrip asserts each declared carrier actually
// carries something: SaveShared must produce bytes LoadShared accepts. A carrier
// that returns an error, or produces something it cannot read back, is a hole in
// the capture that only shows up when a join installs it.
func TestSnapshotStateSystemsRoundTrip(t *testing.T) {
	w := scratchWorld(t)
	carriers := 0
	for _, sys := range BuildSystems(w) {
		saver, ok := sys.(engine.SharedStateSaver)
		if !ok {
			continue
		}
		carriers++
		data, err := saver.SaveShared()
		if err != nil {
			t.Errorf("%s: SaveShared: %v", sys.Name(), err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s: SaveShared produced nothing; a declared carrier that "+
				"writes no bytes carries no state", sys.Name())
			continue
		}
		if err := saver.LoadShared(data); err != nil {
			t.Errorf("%s: LoadShared rejected its own SaveShared output: %v", sys.Name(), err)
		}
	}
	if carriers == 0 {
		t.Fatal("no snapshot carriers were built; the check passed vacuously")
	}
}

// TestSnapshotCarriersAreSharedOrDual keeps the player domain out of a capture.
// A snapshot describes the shared world; a player-profile system carrying state
// into one would be replicating a participant's own simulation, which D-2 says
// does not exist elsewhere and D-6 says is per-instance.
func TestSnapshotCarriersAreSharedOrDual(t *testing.T) {
	for name, profile := range SnapshotDeclarations() {
		if profile != engine.SnapshotState {
			continue
		}
		if d := ProfileFor(name).Domain; d == engine.SystemPlayer {
			t.Errorf("%s declares snapshot state but is a player-domain system; "+
				"a capture carries shared state only (D-1, D-6)", name)
		}
	}
}
