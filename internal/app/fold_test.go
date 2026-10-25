package app

import (
	"maps"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/resource"
)

// TestTheTowersSetupChainCrossesOnce: the tower's setup hands each pylon on by an
// unguarded Tick once its gateway is attached; the authority folds that hop into
// the crossing that attached it, so no instance stands in an attach state at a tick
// boundary and both reach TowerActive at one tick.
func TestTheTowersSetupChainCrossesOnce(t *testing.T) {
	t.Parallel()
	apps := meshSessionOf(t, Config{Seed: 0x70E4, Width: 160, Height: 50, Resources: resource.Options{Embedded: true}},
		2, [][2]int{{1, 2}})
	localCursors(t, apps)
	// A tick first, so the host's machine proposes and the guest's follows before
	// the region spawns; its entry is each instance's own, as a reset's is.
	tickAll(apps)
	for _, a := range apps {
		a.World().RunSafe(func() {
			a.World().PushEventDomain(event.EventFSMRegionRequest,
				&event.FSMRegionPayload{Op: event.RegionSpawn, Region: "tower", State: "TowerSetup"}, core.DomainShared)
		})
		a.Settle()
	}
	stateOf := func(a *App) string {
		var s string
		a.World().RunSafe(func() { s = a.World().Resources.Status.Strings.Get("fsm.tower.state").Load() })
		return s
	}
	active, seen := map[int]uint64{}, map[string]bool{}
	for tick := 0; tick < 900 && len(active) < len(apps); tick++ {
		tickAll(apps)
		for i, a := range apps {
			s := stateOf(a)
			seen[s] = true
			if _, done := active[i]; !done && s == "TowerActive" {
				active[i] = a.Position().Tick
			}
		}
	}
	if len(active) < len(apps) {
		t.Fatalf("the tower became active on %v alone; states seen %v", active, slices.Sorted(maps.Keys(seen)))
	}
	if active[0] != active[1] {
		t.Fatalf("TowerActive at ticks %d and %d, want one tick", active[0], active[1])
	}
	for _, s := range []string{"TowerAttachSnakeGW", "TowerAttachGW1", "TowerAttachGW2", "TowerAttachGW3", "TowerAttachGW4"} {
		if seen[s] {
			t.Fatalf("an instance stood in %s at a tick boundary; the hop after it is folded into its crossing", s)
		}
	}
}
