package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/resource"
)

// TestVanguardRaidersBeamInAtTheRightEdgeAndNeverStandInRock plays the shipped
// vanguard scenario with nobody steering, through the resets its crushes and
// collisions bring: raiders beam in at the right edge, at most three fly at once,
// and no tick ends with one standing in rock, which pushes them as it scrolls and
// which their teleports land beside.
func TestVanguardRaidersBeamInAtTheRightEdgeAndNeverStandInRock(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "vanguard"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vanguard scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 120, Height: 40, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	w := a.World()
	var created []event.SpeciesCreatedPayload
	a.SetDispatchTap(func(ev event.GameEvent) {
		if p, ok := ev.Payload.(*event.SpeciesCreatedPayload); ok && p.Species >= component.SpeciesDataFirst {
			created = append(created, *p)
		}
	})

	most := 0
	for tick := range 1200 {
		a.Tick(1)
		var stuck core.Entity
		live := 0
		w.RunSafe(func() {
			for _, header := range w.Components.Behavior.Entities() {
				live++
				h, _ := w.Components.Header.GetComponent(header)
				for _, m := range h.MemberEntries {
					if pos, ok := w.Positions.GetPosition(m.Entity); ok && w.Positions.HasBlockingWallAt(pos.X, pos.Y, component.WallBlockKinetic) {
						stuck = header
					}
				}
			}
		})
		if stuck != 0 {
			t.Fatalf("tick %d: raider %d stands in rock", tick, stuck)
		}
		most = max(most, live)
	}

	if len(created) < 3 {
		t.Fatalf("%d raiders beamed in over a minute, want several", len(created))
	}
	width := w.Resources.Config.MapWidth
	for _, p := range created {
		if p.X < width*3/4 {
			t.Fatalf("a raider beamed in at (%d,%d), want the right quarter of a %d-wide map", p.X, p.Y, width)
		}
	}
	if most > 3 {
		t.Fatalf("%d raiders flew at once, want at most three", most)
	}
}
