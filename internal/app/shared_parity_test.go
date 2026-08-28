package app

import (
	"strings"
	"testing"
)

// parityScript builds the option set two instances step in lockstep. Resizes and
// resets both re-derive map bounds from this instance's terminal, so both are
// excluded; motions are restricted to the map-relative set, since a screen- or
// page-relative motion resolves against a viewport the instances do not share.
func parityScript(seed uint64, steps int) ScriptOptions {
	opt := DefaultScript(seed, steps)
	opt.Resizes = false
	opt.Resets = false
	opt.MapMotionsOnly = true
	return opt
}

// TestSharedSnapshotParityAcrossTerminalSizes is Phase 4's exit criterion: two
// instances of one seed on different terminals agree on every shared record.
//
// Both are constructed at one size and diverge only after SetupLevel decouples the
// map from the viewport with crop off. Constructing them at different sizes instead
// would bake different map bounds into the FSM's entry actions, which run inside New,
// before any Tick.
func TestSharedSnapshotParityAcrossTerminalSizes(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := 400
	if testing.Short() {
		steps = 60
	}

	a := mustHeadless(t, seed, 120, 40)
	defer a.Close()
	b := mustHeadless(t, seed, 120, 40)
	defer b.Close()

	for _, x := range []*App{a, b} {
		tickUntilCursor(t, x)
		x.SetupLevel(100, 30, true, false)
		x.Tick(1)
	}
	assertSharedParity(t, a, b, -2)

	// Only b's terminal changes; the map is now the FSM's, not the terminal's
	b.Resize(180, 56)
	b.Tick(1)
	a.Tick(1)
	assertSharedParity(t, a, b, -1)

	opt := parityScript(seed, steps)
	da, db := NewScriptDriver(a, opt), NewScriptDriver(b, opt)
	for i := range steps {
		if !da.Step() {
			t.Fatalf("step %d quit instance a", i)
		}
		if !db.Step() {
			t.Fatalf("step %d quit instance b", i)
		}
		assertSharedParity(t, a, b, i)
	}
}

// assertSharedParity fails with the first divergent record and its neighbours
func assertSharedParity(t *testing.T, a, b *App, step int) {
	t.Helper()
	x, y := a.SnapshotShared(), b.SnapshotShared()
	idx, lx, ly, ok := FirstDiff(x, y)
	if !ok {
		return
	}
	t.Fatalf("step %d: shared snapshot diverged at line %d\n  a: %s\n  b: %s\n%s",
		step, idx, lx, ly, strings.Join(Diff(x, y, 8), "\n"))
}
