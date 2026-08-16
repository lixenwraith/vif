package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/manifest"
)

const bisectTicks = 400

// interleavedDivergence runs two scripted instances of one seed in lockstep
// with the named systems disabled, returning the first divergent tick (-1 for
// none) and the differing snapshot lines.
func interleavedDivergence(t *testing.T, seed uint64, ticks int, disabled []string) (int, []string) {
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

	sa, sb := newScript(a), newScript(b)
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

// TestBisectDivergingSystem disables one system at a time and reports which
// removals make the scripted run converge. Skips when the baseline is clean.
//
// A region's enabled_systems re-enables its declarations on spawn or resume,
// so a system named in the FSM config may not stay disabled for the whole run;
// a candidate that still diverges is inconclusive, not exonerated.
func TestBisectDivergingSystem(t *testing.T) {
	const seed = 0xA11CE

	base, diffs := interleavedDivergence(t, seed, bisectTicks, nil)
	if base < 0 {
		t.Skip("scripted run is deterministic; nothing to bisect")
	}
	t.Logf("baseline diverges at tick %d:\n%s", base, strings.Join(diffs, "\n"))

	var converged, delayed []string
	for _, name := range manifest.ActiveSystems() {
		tick, _ := interleavedDivergence(t, seed, bisectTicks, []string{name})
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
