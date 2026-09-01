package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/journal"
)

// parityScript builds the option set two instances step in lockstep. Resizes and
// resets both re-derive map bounds from this instance's terminal, so both are
// excluded; motions are restricted to the map-relative set, since a screen- or
// page-relative motion resolves against a viewport the instances do not share.
func parityScript(seed uint64, steps int) journal.FuzzOptions {
	opt := journal.DefaultFuzz(seed, steps)
	opt.Resizes = false
	opt.Resets = false
	opt.MapMotionsOnly = true
	return opt
}

// TestSharedSnapshotParityAcrossTerminalSizes is the D-11 criterion: two
// instances of one seed on different terminals agree on every shared record.
//
// Both are constructed at one size and diverge only after SetupLevel decouples the
// map from the viewport with crop off. Constructing them at different sizes instead
// would bake different map bounds into the FSM's entry actions, which run inside New,
// before any Tick.
func TestSharedSnapshotParityAcrossTerminalSizes(t *testing.T) {
	const seed = 0x5EEDBEEF
	steps := soakScale(60, 200, 400)

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
	da, db := journal.NewFuzzDriver(a, opt), journal.NewFuzzDriver(b, opt)
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
	t.Fatalf("step %d: shared snapshot diverged at line %d\n  a: %s\n  b: %s\n%s\n%s",
		step, idx, lx, ly, strings.Join(Diff(x, y, 8), "\n"), strings.Join(diffSharedWorld(a, b, 8), "\n"))
}

// diffSharedWorld names the entities behind a world-digest mismatch.
func diffSharedWorld(a, b *App, maxDiff int) []string {
	type state struct {
		position  component.PositionComponent
		kinetic   component.KineticComponent
		combat    component.CombatComponent
		hasPos    bool
		hasKin    bool
		hasCombat bool
	}
	read := func(x *App) map[core.Entity]state {
		out := make(map[core.Entity]state)
		x.World().RunSafe(func() {
			w := x.World()
			visit := func(e core.Entity) {
				if e.Domain() != core.DomainShared {
					return
				}
				s := out[e]
				s.position, s.hasPos = w.Positions.GetPosition(e)
				s.kinetic, s.hasKin = w.Components.Kinetic.GetComponent(e)
				s.combat, s.hasCombat = w.Components.Combat.GetComponent(e)
				if s.hasPos || s.hasKin || s.hasCombat {
					out[e] = s
				}
			}
			for _, e := range w.Positions.Entities() {
				visit(e)
			}
			for _, e := range w.Components.Kinetic.Entities() {
				visit(e)
			}
			for _, e := range w.Components.Combat.Entities() {
				visit(e)
			}
		})
		return out
	}

	x, y := read(a), read(b)
	entities := make([]core.Entity, 0, len(x)+len(y))
	for e := range x {
		entities = append(entities, e)
	}
	for e := range y {
		if _, ok := x[e]; !ok {
			entities = append(entities, e)
		}
	}
	slices.Sort(entities)

	out := make([]string, 0, maxDiff)
	for _, e := range entities {
		sx, okx := x[e]
		sy, oky := y[e]
		if okx == oky && sx == sy {
			continue
		}
		out = append(out, fmt.Sprintf("  entity %d: a=(%+v,%t) b=(%+v,%t)", e, sx, okx, sy, oky))
		if len(out) == maxDiff {
			break
		}
	}
	if len(out) == 0 {
		return []string{"  compared shared world state agrees"}
	}
	return out
}
