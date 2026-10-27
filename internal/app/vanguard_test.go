package app

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/pattern"
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

// TestVanguardAShotDownRaiderDropsAUThatRaisesTheGun plays the shipped scenario
// and ends two raiders as they beam in: one that dies by no player's hand drops
// nothing, and one the player shoots down drops a U where it fell, which flies to
// the ship and raises its gun a level of vanguard's path; a new game starts the
// path over.
func TestVanguardAShotDownRaiderDropsAUThatRaisesTheGun(t *testing.T) {
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
	var kills []event.SpeciesKilledPayload
	var drops []event.LootSpawnRequestPayload
	resets := 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.SpeciesKilledPayload:
			if ev.Type == event.EventSpeciesKilled && p.Species >= component.SpeciesDataFirst {
				kills = append(kills, *p)
			}
		case *event.LootSpawnRequestPayload:
			drops = append(drops, *p)
		}
		if ev.Type == event.EventGameResetRequest {
			resets++
		}
	})
	// gun is the player's gun: its charges and the levels of its path
	gun := func() (charges, levels int) {
		w.RunSafe(func() {
			if weapon, ok := w.Components.Weapon.GetComponent(w.Resources.Player.Entity); ok {
				charges, levels = weapon.Charges[component.WeaponGun], weapon.GunLevels
			}
		})
		return charges, levels
	}
	// end waits for a raider and takes its hit points, the player's shot or nobody's
	end := func(byPlayer bool) {
		t.Helper()
		for range 900 {
			a.Tick(1)
			ended := false
			w.RunSafe(func() {
				for _, header := range w.Components.Behavior.Entities() {
					if combat, ok := w.Components.Combat.GetPtr(header); ok {
						combat.HitPoints, combat.LastDamagedBy = 0, 0
						if byPlayer {
							combat.LastDamagedBy = w.Resources.Player.Entity
						}
						ended = true
						return
					}
				}
			})
			if ended {
				a.Tick(1)
				return
			}
		}
		t.Fatal("no raider beamed in")
	}

	for range 60 {
		if _, levels := gun(); levels > 0 {
			break
		}
		a.Tick(1)
	}
	if charges, levels := gun(); charges != 1 || levels != 9 {
		t.Fatalf("the ship's gun holds %d charges on %d levels, want the first of vanguard's nine", charges, levels)
	}

	end(false)
	if len(kills) != 1 || len(drops) != 0 {
		t.Fatalf("a raider nobody shot: %d kills and drops %+v, want one kill and nothing dropped", len(kills), drops)
	}

	end(true)
	if len(kills) != 2 || len(drops) != 1 {
		t.Fatalf("a raider the player shot: %d kills and drops %+v, want two kills and one drop", len(kills), drops)
	}
	if drop, kill := drops[0], kills[1]; drop.Type != component.LootUpgrade || drop.X != kill.X || drop.Y != kill.Y {
		t.Fatalf("dropped %+v for a kill at (%d,%d), want a U there", drop, kill.X, kill.Y)
	}
	for tick := range 600 {
		if charges, _ := gun(); charges == 2 {
			break
		}
		if resets > 0 {
			t.Fatalf("tick %d: the game started over before the U reached the ship", tick)
		}
		a.Tick(1)
	}
	if charges, _ := gun(); charges != 2 {
		t.Fatalf("the U left the gun at %d charges, want 2", charges)
	}

	// A new game flies a new ship, back at the path's first level
	if !a.Reset(false) {
		t.Fatal("the reset was refused")
	}
	for range 60 {
		if _, levels := gun(); levels > 0 {
			break
		}
		a.Tick(1)
	}
	if charges, levels := gun(); charges != 1 || levels != 9 {
		t.Fatalf("after a reset the gun holds %d charges on %d levels, want the first of nine", charges, levels)
	}
}

// TestVanguardsCaveWearsOneOfItsStoneArts boots the shipped scenario: its rock draws
// one of the four stone arts the cave start lists, from the cave's seed, and a new
// game's cave draws one again
func TestVanguardsCaveWearsOneOfItsStoneArts(t *testing.T) {
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
	for game := range 2 {
		a.Tick(5)
		var look engine.RockLook
		w.RunSafe(func() {
			if rock := w.Positions.Rock(); rock != nil {
				look = rock.Look
			}
		})
		art := pattern.ArtPreset(look.Art).String()
		if !look.Stone || look.Seed == 0 || !slices.Contains([]string{"strata", "cracked", "moss", "crystal"}, art) {
			t.Fatalf("game %d: the cave's rock draws %+v (%s), want one of vanguard's stone arts", game, look, art)
		}
		if !a.Reset(false) {
			t.Fatal("the reset was refused")
		}
	}
}
