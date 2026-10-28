package app

import (
	"maps"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
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

// TestSimonsNoteCrossesOnceInASession: simon's Listen lights a note by an internal
// Tick transition whose own actions turn its guard false. The authority proposes it
// once and takes no other Tick transition in the region until it lands, so at level
// 1 each of Listen's transitions crosses once on both instances, one note as solo.
func TestSimonsNoteCrossesOnceInASession(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "simon"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("simon scenario not present")
	}
	apps := meshSessionOf(t, Config{Seed: fixtureSeed, Width: 120, Height: 40, Resources: opts}, 2, [][2]int{{1, 2}})
	stateOf := func(a *App) string { return a.World().Resources.Status.Strings.Get("fsm.simon.state").Load() }
	// crossed counts each instance's crossings out of Listen by owner and index
	crossed := [2]map[[2]int]int{{}, {}}
	for i, a := range apps {
		a.SetDispatchTap(func(ev event.GameEvent) {
			if p, ok := ev.Payload.(*event.FSMTransitionPayload); ok && p.Region == "simon" && stateOf(a) == "Listen" {
				crossed[i][[2]int{p.Owner, p.Index}]++
			}
		})
	}
	tickAll(apps)
	press, err := input.IntentFor(input.PressAction, 1, "1")
	if err != nil {
		t.Fatal(err)
	}
	apps[0].Inject(&press) // a press starts the first game
	for tick := 0; tick < 600 && (stateOf(apps[0]) != "Turn" || stateOf(apps[1]) != "Turn"); tick++ {
		tickAll(apps)
	}
	if stateOf(apps[0]) != "Turn" || stateOf(apps[1]) != "Turn" {
		t.Fatalf("simon stands in %s and %s, want both at level 1's Turn", stateOf(apps[0]), stateOf(apps[1]))
	}
	for i, c := range crossed {
		for at, n := range c {
			if n != 1 {
				t.Errorf("participant %d crossed transition %d of state %d %d times in Listen, want once", i+1, at[1], at[0], n)
			}
		}
	}
	if len(crossed[0]) != 2 || !maps.Equal(crossed[0], crossed[1]) {
		t.Fatalf("Listen crossed %v and %v, want its note and its Turn on both instances", crossed[0], crossed[1])
	}
}
