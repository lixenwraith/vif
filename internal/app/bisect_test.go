package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/manifest"
)

const (
	bisectTicks      = 400
	bisectTowerTicks = 500 // tower entry is at tick 100; leaves 400 in-region
)

// interleavedDivergence runs two instances of one seed in lockstep under the
// given arm, with the named systems disabled. Returns the first divergent tick
// (-1 for none) and the differing snapshot lines.
func interleavedDivergence(t *testing.T, mkScript func(*App) *script, seed uint64, ticks int, disabled []string) (int, []string) {
	t.Helper()

	a, err := NewHeadless(testConfig(seed))
	if err != nil {
		t.Fatalf("NewHeadless: %v", err)
	}
	defer a.Close()

	b, err := NewHeadless(testConfig(seed))
	if err != nil {
		t.Fatalf("NewHeadless: %v", err)
	}
	defer b.Close()

	for _, name := range disabled {
		setSystemEnabled(a, name, false)
		setSystemEnabled(b, name, false)
	}

	sa, sb := mkScript(a), mkScript(b)
	for n := 1; n <= ticks; n++ {
		sa.step(t, n)
		sb.step(t, n)
		if d := diffSnapshots(a.Snapshot(), b.Snapshot()); len(d) > 0 {
			dumpDivergence(t, a, b, 12)
			return n, d
		}
	}
	return -1, nil
}

// TestBisectDivergingSystem bisects the combat arm
func TestBisectDivergingSystem(t *testing.T) {
	bisectArm(t, "combat", newScript, 0xA11CE, bisectTicks)
}

// TestBisectDivergingSystemTower bisects the tower arm, the only one covering
// route graphs, the bandit and the GA
func TestBisectDivergingSystemTower(t *testing.T) {
	bisectArm(t, "tower", newTowerScript, 0xA11CE, bisectTowerTicks)
}

// bisectArm disables one system at a time and reports which removals make the
// arm converge. Skips when the baseline is clean.
//
// A region's enabled_systems re-enables its declarations on spawn or resume,
// so a system named in the FSM config may not stay disabled for the whole run;
// a candidate that still diverges is inconclusive, not exonerated.
func bisectArm(t *testing.T, arm string, mkScript func(*App) *script, seed uint64, ticks int) {
	t.Helper()

	base, diffs := interleavedDivergence(t, mkScript, seed, ticks, nil)
	if base < 0 {
		t.Skipf("%s arm is deterministic; nothing to bisect", arm)
	}
	t.Logf("%s baseline diverges at tick %d:\n%s", arm, base, strings.Join(diffs, "\n"))

	var converged, delayed []string
	for _, name := range manifest.ActiveSystems() {
		tick, _ := interleavedDivergence(t, mkScript, seed, ticks, []string{name})
		switch {
		case tick < 0:
			converged = append(converged, name)
		case tick > base:
			delayed = append(delayed, name+"@"+itoa(tick))
		}
	}

	t.Logf("converged when disabled: %v", converged)
	t.Logf("delayed when disabled:   %v", delayed)

	if len(converged) == 0 && len(delayed) == 0 {
		t.Log("no single system accounts for the divergence; try pairs or check shared helpers")
	}
}
