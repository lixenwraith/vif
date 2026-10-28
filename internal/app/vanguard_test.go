package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/color"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestVanguardRaidersMaterializeAtTheRightEdgeAndNeverStandInRock plays the shipped
// vanguard scenario with nobody steering, through the resets its crushes and
// collisions bring: raiders of several types materialize at the right edge, each
// landing as its materialize ends, no drain ever standing, at most three fly at
// once, and no tick ends with one standing in rock, which pushes them as it scrolls
// and which their landings and teleports keep clear of.
func TestVanguardRaidersMaterializeAtTheRightEdgeAndNeverStandInRock(t *testing.T) {
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
	materialized := 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.SpeciesCreatedPayload:
			if p.Species >= component.SpeciesDataFirst {
				created = append(created, *p)
			}
		case *event.MaterializeCompletedPayload:
			if ev.Domain == core.DomainShared && p.Species >= component.SpeciesDataFirst {
				materialized++
			}
		}
	})

	most := 0
	for tick := range 1200 {
		a.Tick(1)
		var stuck core.Entity
		live, drains := 0, 0
		w.RunSafe(func() {
			drains = w.Components.Drain.CountEntities()
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
		if drains != 0 {
			t.Fatalf("tick %d: %d drains stand, want none", tick, drains)
		}
		most = max(most, live)
	}

	if len(created) < 3 {
		t.Fatalf("%d raiders spawned over a minute, want several", len(created))
	}
	if materialized != len(created) {
		t.Fatalf("%d raiders stood as %d materializes ended, want each landing as its own ends", len(created), materialized)
	}
	types := map[component.SpeciesType]bool{}
	for _, p := range created {
		types[p.Species] = true
	}
	if len(types) < 2 {
		t.Fatalf("%d raiders of %d types, want the raid to pick among its types", len(created), len(types))
	}
	width := w.Resources.Config.MapWidth
	for _, p := range created {
		if p.X < width*3/4 {
			t.Fatalf("a raider spawned at (%d,%d), want the right quarter of a %d-wide map", p.X, p.Y, width)
		}
	}
	if most > 3 {
		t.Fatalf("%d raiders flew at once, want at most three", most)
	}
}

// TestVanguardAShotDownRaiderDropsAUThatRaisesTheGun plays the shipped scenario
// and ends raiders as they arrive: one that dies by no player's hand drops nothing,
// and within two the player shoots down, the U's even odds certain after a miss,
// one drops a U where it fell, which flies to the ship and raises its gun a level
// of vanguard's path; a new game starts the path over.
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
	resets := 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		if p, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && ev.Type == event.EventSpeciesKilled && p.Species >= component.SpeciesDataFirst {
			kills = append(kills, *p)
		}
		if ev.Type == event.EventGameResetRequest {
			resets++
		}
	})
	// drops are where the U's on the map stand
	drops := func() (cells []component.PositionComponent) {
		w.RunSafe(func() {
			for _, e := range vanguardUs(w) {
				if pos, ok := w.Positions.GetPosition(e); ok {
					cells = append(cells, pos)
				}
			}
		})
		return cells
	}
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
		t.Fatal("no raider spawned")
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
	if got := drops(); len(kills) != 1 || len(got) != 0 {
		t.Fatalf("a raider nobody shot: %d kills and U's at %+v, want one kill and nothing dropped", len(kills), got)
	}

	var got []component.PositionComponent
	for len(got) == 0 && len(kills) < 3 {
		end(true)
		got = drops()
	}
	if len(got) != 1 {
		t.Fatalf("%d raiders the player shot left U's at %+v, want one within two", len(kills)-1, got)
	}
	// Heat or energy dropping beside it scatters the drops a cell from the kill
	if drop, kill := got[0], kills[len(kills)-1]; vmath.IntAbs(drop.X-kill.X) > 2 || vmath.IntAbs(drop.Y-kill.Y) > 2 {
		t.Fatalf("a U at (%d,%d) for a kill at (%d,%d), want it where the raider fell", drop.X, drop.Y, kill.X, kill.Y)
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

// vanguardUs are the drops of vanguard's U on w's map; the caller holds its lock
func vanguardUs(w *engine.World) []core.Entity {
	i := slices.Index(w.Resources.Loot.Names(), "upgrade")
	if i < 0 {
		return nil
	}
	var out []core.Entity
	for _, e := range w.Components.Loot.Entities() {
		if l, ok := w.Components.Loot.GetPtr(e); ok && l.Type == component.LootCount+component.LootType(i) {
			out = append(out, e)
		}
	}
	return out
}

// TestVanguardAKillInAPairDropsEachPlayerOneU ends raiders by the host player's
// shot on a mesh pair until each instance has dropped its player a U, two kills at
// most. Each instance rolls the raider table once a kill for the player it
// simulates, the guest when its ledger confirms the death, and each player's ship
// takes its own U's and goes up a level a U. The rank counts each kill by an
// internal transition on it, which each instance crosses once.
func TestVanguardAKillInAPairDropsEachPlayerOneU(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "vanguard"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vanguard scenario not present")
	}
	host, err := NewHeadless(Config{Seed: fixtureSeed, Width: 120, Height: 40, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	an := host.JoinAnchor()
	guest, err := NewHeadless(Config{
		Seed: fixtureSeed, Width: 84, Height: 26, Resources: opts,
		MapWidth: an.Anchor.MapWidth, MapHeight: an.Anchor.MapHeight,
		CropOnResize: an.Anchor.CropOnResize, LockMap: an.Anchor.SessionShared,
	})
	if err != nil {
		t.Fatalf("headless joiner: %v", err)
	}
	t.Cleanup(func() { host.Close(); guest.Close() })
	if err := guest.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	host.membership.adoptMapLatch(an.Anchor)
	apps := []*App{host, guest}
	for _, a := range apps {
		tickUntilCursor(t, a)
		if !a.SetupLevel(100, 30, true, false) {
			t.Fatal("level setup refused before the session was live")
		}
		a.Tick(1)
	}
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	host.AttachTransport(mesh.Node(1))
	guest.AttachTransport(mesh.Node(2))

	// rolls are the deaths the loot system rolls on: one an instance proves, or
	// one its ledger confirms
	var kills, rolls, resets [2]int
	for i, a := range apps {
		a.SetDispatchTap(func(ev event.GameEvent) {
			if p, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && p.Species >= component.SpeciesDataFirst {
				if ev.Type == event.EventSpeciesKilled {
					kills[i]++
				}
				if ev.Type == event.EventSpeciesKillConfirmed || ev.Type == event.EventSpeciesKilled && ev.Phase != event.PhasePredicted {
					rolls[i]++
				}
			}
			if ev.Type == event.EventGameResetRequest {
				resets[i]++
			}
		})
	}
	// drops are the U's each instance has dropped, each once however long it flies
	drops := [2]map[core.Entity]bool{{}, {}}
	step := func() {
		host.Tick(1)
		guest.Tick(1)
		_ = host.corrections.PublishDue()
		guest.ApplyPendingCorrections()
		for i, a := range apps {
			w := a.World()
			w.RunSafe(func() {
				for _, e := range vanguardUs(w) {
					drops[i][e] = true
				}
			})
		}
	}
	// rankKills is the kills the raid's rank has counted on an instance
	rankKills := func(a *App) int64 {
		for _, v := range a.scheduler.ExportFSM().Variables {
			if v.Name == "raid_kills" {
				return v.Value
			}
		}
		return 0
	}
	// charges is the gun of the ship an instance flies
	charges := func(a *App) (n int) {
		w := a.World()
		w.RunSafe(func() {
			if weapon, ok := w.Components.Weapon.GetComponent(w.Resources.Player.Entity); ok && weapon.GunLevels > 0 {
				n = weapon.Charges[component.WeaponGun]
			}
		})
		return n
	}

	var shooter core.Entity
	host.World().RunSafe(func() { shooter = host.World().Resources.Player.Entity })
	// Raiders both instances fly, each ended by the host player's shot in both
	// worlds, until each instance has dropped a U
	n := 0
	for ; len(drops[0]) == 0 || len(drops[1]) == 0; n++ {
		if n == 2 {
			t.Fatalf("two kills dropped %d and %d U's on host and guest, want one on each at least", len(drops[0]), len(drops[1]))
		}
		var raider core.Entity
		for range 900 {
			step()
			host.World().RunSafe(func() {
				for _, e := range host.World().Components.Behavior.Entities() {
					raider = e
					return
				}
			})
			onGuest := false
			guest.World().RunSafe(func() { onGuest = raider != 0 && guest.World().Components.Behavior.HasEntity(raider) })
			if onGuest {
				break
			}
			raider = 0
		}
		if raider == 0 {
			t.Fatal("no raider flew on both instances")
		}
		for i, a := range apps {
			if c := charges(a); n == 0 && c != 1 {
				t.Fatalf("instance %d: the ship's gun holds %d charges before the kill, want vanguard's first level", i, c)
			}
		}
		for _, a := range apps {
			w := a.World()
			w.RunSafe(func() {
				if combat, ok := w.Components.Combat.GetPtr(raider); ok {
					combat.HitPoints, combat.LastDamagedBy = 0, shooter
				}
			})
		}
		// The guest's ledger confirms the death a few ticks on
		for range 30 {
			step()
		}
	}

	for tick := 0; charges(host) < 1+len(drops[0]) || charges(guest) < 1+len(drops[1]); tick++ {
		if tick == 600 {
			t.Fatalf("the guns hold %d and %d charges 600 ticks after the kills, want a level up a U", charges(host), charges(guest))
		}
		if resets != [2]int{} {
			t.Fatalf("tick %d: the game started over before the U's reached each ship", tick)
		}
		step()
	}
	for range 30 {
		step()
	}
	if h, g := charges(host), charges(guest); h != 1+len(drops[0]) || g != 1+len(drops[1]) {
		t.Fatalf("the guns hold %d and %d charges for %d and %d U's, want a level up each", h, g, len(drops[0]), len(drops[1]))
	}
	if kills != [2]int{n, n} || rolls != [2]int{n, n} {
		t.Fatalf("kills %v and rolls %v on host and guest, want %d of each on each", kills, rolls, n)
	}
	if h, g := rankKills(host), rankKills(guest); h != int64(n) || g != int64(n) {
		t.Fatalf("the rank counted %d and %d kills on host and guest, want the %d on each", h, g, n)
	}
}

// TestVanguardABotSeatFliesTheShipsGunPath seats a bot on a vanguard run. Its
// instance installs the game, the aim with it, before its cursor arrives, and that
// cursor's gun climbs vanguard's path from its first ticks as the player's does,
// not from the next game, whose start aims it afresh.
func TestVanguardABotSeatFliesTheShipsGunPath(t *testing.T) {
	// Not parallel: a paced run against wall-clock deadlines.
	opts := resource.Options{Dir: soakRoot, Scenario: "vanguard"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vanguard scenario not present")
	}
	holder, _ := driveBot(t, Config{Bots: []string{"roam"}, Resources: opts}, "patrol", nil)
	waitForCursors(t, holder, 2)
	// gun reads the gun on the cursor a run flies: whether it holds one, and its path
	gun := func(a *App) (armed bool, levels int) {
		w := a.World()
		w.RunSafe(func() {
			weapon, ok := w.Components.Weapon.GetComponent(w.Resources.Player.Entity)
			armed, levels = ok, weapon.GunLevels
		})
		return armed, levels
	}
	const settle = 5 // ticks the seat's first reading may take
	var seat *App
	var armedAt uint64
	for deadline := time.Now().Add(socketWait); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if seat == nil {
			holder.membership.seatsMu.Lock()
			if len(holder.membership.seats) == 1 {
				seat = holder.membership.seats[0].app.Load()
			}
			holder.membership.seatsMu.Unlock()
			continue
		}
		tick := seat.Position().Tick
		armed, levels := gun(seat)
		switch {
		case !armed:
		case levels == 9:
			if _, own := gun(holder); own != 9 {
				t.Fatalf("the player's gun climbs %d levels, want vanguard's nine", own)
			}
			return
		case armedAt == 0:
			armedAt = tick
		case tick > armedAt+settle:
			t.Fatalf("the seat's gun climbs %d levels %d ticks after its cursor armed, want vanguard's nine", levels, tick-armedAt)
		}
	}
	t.Fatalf("no seat's cursor armed within %s (seat %v)", socketWait, seat != nil)
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

// TestVanguardsCaveOpensOnOpenMap boots the shipped scenario: its cave starts with a
// mouth of open map as wide as the view, no rock at all, and its rock comes in past
// the right edge as it scrolls
func TestVanguardsCaveOpensOnOpenMap(t *testing.T) {
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
	// rock is the cells of rock in view, the leftmost column holding any, and the
	// cave's scroll
	rock := func() (cells, left int, origin int64, ok bool) {
		w.RunSafe(func() {
			r := w.Positions.Rock()
			if ok = r != nil; ok {
				left = -1
				for x, col := range r.Columns() {
					if n := col.Ceil + w.Resources.Config.MapHeight - col.Floor; n > 0 {
						cells += n
						if left < 0 {
							left = x
						}
					}
				}
				origin = r.Origin
			}
		})
		return cells, left, origin, ok
	}
	for range 5 {
		if _, _, _, ok := rock(); ok {
			break
		}
		a.Tick(1)
	}
	if cells, _, origin, ok := rock(); !ok || cells > 0 {
		t.Fatalf("the cave stands %v with %d rock cells in view at column %d, want none", ok, cells, origin)
	}
	for range 60 {
		if cells, _, _, _ := rock(); cells > 0 {
			break
		}
		a.Tick(1)
	}
	if cells, left, origin, _ := rock(); cells == 0 || left < w.Resources.Config.MapWidth*3/4 {
		t.Fatalf("at column %d the rock in view is %d cells from column %d, want it coming in at the right edge", origin, cells, left)
	}
}

// TestVanguardsCaveStepsOnAnEvenCadence boots the shipped scenario: its cave steps a
// column on whole ticks, the same number apart every time, since a period between
// whole ticks falls alternately short and long and reads as dropped frames
func TestVanguardsCaveStepsOnAnEvenCadence(t *testing.T) {
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
	origin := func() (o int64) {
		w.RunSafe(func() {
			if rock := w.Positions.Rock(); rock != nil {
				o = rock.Origin
			}
		})
		return o
	}
	a.Tick(5)
	// since counts from the first step seen, so the first gap is whole
	var gaps []int
	last, since := origin(), -1
	for range 100 {
		a.Tick(1)
		if since >= 0 {
			since++
		}
		if o := origin(); o != last {
			if o != last+1 {
				t.Fatalf("the cave stepped from column %d to %d in a tick, want one column", last, o)
			}
			if since > 0 {
				gaps = append(gaps, since)
			}
			last, since = o, 0
		}
	}
	if len(gaps) < 10 || slices.Min(gaps) != slices.Max(gaps) {
		t.Fatalf("the cave stepped %d ticks apart, want an even cadence", gaps)
	}
}

// TestVanguardsCaveOpensTheRowsATallMapAdds boots the shipped scenario on a map a
// phone held upright gives: each wall keeps at most the 29 rows it keeps on a map of
// 40, so the rows past those open, and at least 32 of 90 stay open in every column
func TestVanguardsCaveOpensTheRowsATallMapAdds(t *testing.T) {
	t.Parallel()
	opts := resource.Options{Dir: soakRoot, Scenario: "vanguard"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vanguard scenario not present")
	}
	const width, height, deepest = 80, 90, 29
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: width, Height: height, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()

	w := a.World()
	seen := 0
	for tick := range 400 {
		a.Tick(1)
		w.RunSafe(func() {
			rock := w.Positions.Rock()
			if rock == nil {
				return
			}
			for x, col := range rock.Columns() {
				if col.Ceil > deepest || height-col.Floor > deepest {
					t.Fatalf("tick %d: column %d is rock to row %d and from row %d, want walls of at most %d rows of %d",
						tick, x, col.Ceil, col.Floor, deepest, height)
				}
				seen++
			}
		})
	}
	if seen == 0 {
		t.Fatal("no cave stood")
	}
}

// TestVanguardsDecayFallsDownOneColumnOfTheCaveThroughItsRock fires the ship on
// negative energy, which sheds decay: each decay keeps one column of the cave, its map
// column plus the rock's origin, never rises, and falls on from the open into the
// floor, which lets it pass. A new game lays a new cave and starts the count again.
func TestVanguardsDecayFallsDownOneColumnOfTheCaveThroughItsRock(t *testing.T) {
	t.Parallel()
	a := newVanguard(t)
	w := a.World()
	w.RunSafe(func() {
		player := w.Resources.Player.Entity
		w.PushLocal(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Entity: player, Value: parameter.HeatMax})
		w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: player, Value: -parameter.GodEnergyAmount, Weapons: true})
	})
	type place struct {
		column    int64
		row       int
		wasInOpen bool
	}
	seen := map[core.Entity]place{}
	origin, passed := int64(-1), 0
	for tick := range 200 {
		if tick%2 == 0 {
			a.Inject(&input.Intent{Type: input.IntentFireMain})
		}
		a.Tick(1)
		w.RunSafe(func() {
			rock := w.Positions.Rock()
			if rock == nil {
				return
			}
			if rock.Origin < origin {
				clear(seen)
			}
			origin = rock.Origin
			for _, e := range w.Components.Particle.Entities() {
				if p, _ := w.Components.Particle.GetComponent(e); p.Behavior != component.ParticleDecay {
					continue
				}
				pos, _ := w.Positions.GetPosition(e)
				col, _ := rock.Column(pos.X)
				was, ok := seen[e]
				at := place{column: int64(pos.X) + rock.Origin, row: pos.Y, wasInOpen: was.wasInOpen || !col.Rock(pos.Y)}
				if ok && (at.column != was.column || at.row < was.row) {
					t.Fatalf("tick %d: decay went from cave column %d row %d to column %d row %d, want its column, falling",
						tick, was.column, was.row, at.column, at.row)
				}
				if was.wasInOpen && pos.Y >= col.Floor {
					passed++
				}
				seen[e] = at
			}
		})
	}
	if passed == 0 {
		t.Fatalf("of %d decay none fell from the open into the floor", len(seen))
	}
}

// newVanguard is a headless 120x40 vanguard run past its boot, the ship armed
func newVanguard(t *testing.T) *App {
	t.Helper()
	opts := resource.Options{Dir: soakRoot, Scenario: "vanguard"}
	if _, err := resource.ScenarioPath(opts); err != nil {
		t.Skip("vanguard scenario not present")
	}
	a, err := NewHeadless(Config{Mode: ModeHeadless, Seed: fixtureSeed, Width: 120, Height: 40, Resources: opts})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	t.Cleanup(a.Close)
	w := a.World()
	for range 60 {
		armed := false
		w.RunSafe(func() {
			weapon, ok := w.Components.Weapon.GetComponent(w.Resources.Player.Entity)
			armed = ok && weapon.GunLevels > 0
		})
		if armed {
			return a
		}
		a.Tick(1)
	}
	t.Fatal("the ship never armed")
	return nil
}

// TestVanguardsLostGameFreezesUnderARedStrobeThenStartsOver: a crush and a defeat
// by heat and energy alike halt the rock and the drift under a red strobe, and the
// game starts over once the strobe has run, opening on what ended the last
func TestVanguardsLostGameFreezesUnderARedStrobeThenStartsOver(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		lose          func(w *engine.World)
	}{
		{"crushed", "Crushed Against The Edge!", func(w *engine.World) {
			w.PushEvent(event.EventCursorCrushed, &event.CursorCrushedPayload{Entity: w.Resources.Player.Entity})
		}},
		{"died", "You Died!", func(w *engine.World) {
			w.PushEvent(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: w.Resources.Player.Entity})
			w.PushEvent(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Entity: w.Resources.Player.Entity})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newVanguard(t)
			w := a.World()
			var seen []event.EventType
			var red bool
			var messages []string
			a.SetDispatchTap(func(ev event.GameEvent) {
				seen = append(seen, ev.Type)
				switch p := ev.Payload.(type) {
				case *event.StrobeRequestPayload:
					red = red || p.Color == (color.RGB{R: 255})
				case *event.MetaStatusMessagePayload:
					messages = append(messages, p.Message)
				}
			})
			origin := func() (o int64) {
				w.RunSafe(func() { o = w.Positions.Rock().Origin })
				return o
			}
			w.RunSafe(func() { tc.lose(w) })
			a.Tick(2)
			frozen := origin()
			if !slices.Contains(seen, event.EventCaveStop) || !slices.Contains(seen, event.EventDriftCancel) || !red {
				t.Fatalf("lost: cave stop %t, drift cancel %t, red strobe %t, want all three", slices.Contains(seen, event.EventCaveStop), slices.Contains(seen, event.EventDriftCancel), red)
			}
			ticks := 2
			for !slices.Contains(seen, event.EventGameResetRequest) {
				if ticks > 60 {
					t.Fatal("the game never started over")
				}
				if o := origin(); o != frozen {
					t.Fatalf("tick %d: the rock scrolled from column %d to %d under the strobe", ticks, frozen, o)
				}
				a.Tick(1)
				ticks++
			}
			if held := time.Duration(ticks) * parameter.GameUpdateInterval; held < 250*time.Millisecond {
				t.Fatalf("the game started over %v after it was lost, want after the strobe's 250ms", held)
			}
			if !slices.Contains(messages, tc.message) {
				t.Fatalf("the new game opened on %q, want %q", messages, tc.message)
			}
		})
	}
}

// TestVanguardsRaidClimbsToTheKrakenAndStartsOverInTheNextCave: raiders the player
// shoots down as they arrive come from the first rank's two types until three fall,
// then the next rank's, to the fourth, whose third kill strobes green and ends the
// cave. No raider comes while the cave opens to no rock, the rock and the drift at
// twice their pace, and its scroll halts, and the Kraken rises at the right edge
// with vanguard's hit points. The player's kill of it
// drops a missile charge, heat and two energy, doubles species damage and lays the
// next cave in another stone art, the raider table's streaks carried into it,
// scrolling a fifth faster once its pace has eased in, where the climb starts over
// at the first rank of that cave's types.
func TestVanguardsRaidClimbsToTheKrakenAndStartsOverInTheNextCave(t *testing.T) {
	t.Parallel()
	merged, err := fsm.ResolveScenario(os.DirFS(soakRoot+"/scenario/vanguard"), "scenario.toml")
	if err != nil {
		t.Skipf("vanguard scenario not present: %v", err)
	}
	// rankOf is each type's rank, read from the guard counting its kills
	rankOf := map[string]int{}
	states := merged["states"].(map[string]any)
	for r := range 4 {
		kill := states[fmt.Sprintf("RaidRank%d", r+1)].(map[string]any)["transitions"].([]any)[0].(map[string]any)
		guard := kill["guard_args"].(map[string]any)["guards"].([]any)[0].(map[string]any)
		for _, name := range guard["args"].(map[string]any)["names"].([]any) {
			rankOf[name.(string)] = r
		}
	}
	// arts and types are the picks' lists: eight types to an art, in its order
	var arts, types []any
	for _, action := range states["CaveStart"].(map[string]any)["on_enter"].([]any) {
		if action.(map[string]any)["event"] == "EventCaveStart" {
			arts = action.(map[string]any)["pick"].(map[string]any)["from"].([]any)
		}
	}
	spawn := states["RaidSpawn"].(map[string]any)["transitions"].([]any)[0].(map[string]any)["actions"].([]any)[0]
	types = spawn.(map[string]any)["pick"].(map[string]any)["from"].([]any)

	a := newVanguard(t)
	w := a.World()
	var spawned []string
	var krakens []event.SpeciesCreatedPayload
	var opens []event.CaveOpenPayload
	var messages []string
	greens, resets, opened, entered := 0, 0, 0, 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.SpeciesCreatedPayload:
			if p.Species >= component.SpeciesDataFirst {
				spawned = append(spawned, w.Resources.Behaviors[p.Species-component.SpeciesDataFirst].Name)
			} else if p.Species == component.SpeciesKraken {
				krakens = append(krakens, *p)
			}
		case *event.StrobeRequestPayload:
			if p.Color == (color.RGB{G: 255}) {
				greens++
			}
		case *event.CaveOpenPayload:
			opens = append(opens, *p)
		case *event.MetaStatusMessagePayload:
			messages = append(messages, p.Message)
		}
		switch ev.Type {
		case event.EventGameResetRequest:
			resets++
		case event.EventCaveOpened:
			opened++
		case event.EventCaveEntered:
			entered++
		}
	})
	// shoot waits for a raider and has the player shoot it down
	shoot := func() {
		t.Helper()
		for range 900 {
			a.Tick(1)
			shot := false
			w.RunSafe(func() {
				for _, header := range w.Components.Behavior.Entities() {
					if combat, ok := w.Components.Combat.GetPtr(header); ok {
						combat.HitPoints, combat.LastDamagedBy = 0, w.Resources.Player.Entity
						shot = true
						return
					}
				}
			})
			if shot {
				a.Tick(1)
				return
			}
		}
		t.Fatal("no raider spawned")
	}
	// rock is the cave's stone art and how far it has scrolled
	rock := func() (art string, origin int64) {
		w.RunSafe(func() {
			if r := w.Positions.Rock(); r != nil {
				art, origin = pattern.ArtPreset(r.Look.Art).String(), r.Origin
			}
		})
		return art, origin
	}
	inArt := func(name, art string) bool {
		i := slices.Index(arts, any(art))
		return i >= 0 && slices.Contains(types[i*8:i*8+8], any(name))
	}
	// second waits out an ease and reads the next second: the columns the rock
	// scrolled and the drift's pace
	second := func() (cols int64, drift float64) {
		a.Tick(25)
		_, from := rock()
		a.Tick(20)
		_, to := rock()
		w.RunSafe(func() { drift = w.Resources.Status.Floats.Get("environment.drift_speed").Get() })
		return to - from, drift
	}
	// streaks are the raider table's pity rows as the loot system carries them
	streaks := func() (rows []string) {
		w.RunSafe(func() {
			for _, sys := range w.Systems() {
				if s, ok := sys.(engine.LocalStateSaver); ok && sys.Name() == "loot" {
					data, err := s.SaveLocal()
					var all []struct {
						Table  string `json:"table"`
						Misses []int  `json:"misses"`
					}
					if err != nil || json.Unmarshal(data, &all) != nil {
						t.Errorf("the loot system's state: %v", err)
					}
					for _, r := range all {
						if r.Table == "raider" {
							rows = append(rows, fmt.Sprint(r.Misses))
						}
					}
				}
			}
		})
		return rows
	}

	// Two tiers: each climb ends in the Kraken, and each cave after one is of
	// another stone, a fifth faster in whole columns a second
	const perRank = 3
	art, _ := rock()
	pace := int64(20)
	for cycle, speed := range []int64{24, 29} {
		for kill := range 4 * perRank {
			shoot()
			if resets != 0 {
				t.Skip("the game started over mid-climb")
			}
			n := cycle*4*perRank + kill
			if got, want := spawned[n], kill/perRank; rankOf[got] != want || !inArt(got, art) {
				t.Fatalf("raider %d is a %s of rank %d, want one of %s's rank %d (%v)", n, got, rankOf[got]+1, art, want+1, spawned)
			}
		}
		if a.Tick(2); greens != cycle+1 {
			t.Fatalf("tier %d: the tier's end strobed %d times, want %d", cycle, greens, cycle+1)
		}
		// The approach, once eased up: the rock and the drift at twice their pace
		if cols, drift := second(); cols < 2*pace-1 || cols > 2*pace+1 || drift != 160 {
			t.Fatalf("tier %d: the approach scrolled %d columns in a second, the drift at %v; want %d and 160", cycle, cols, drift, 2*pace)
		}

		// The boss: the cave opens to no rock and halts, and the Kraken rises
		for range 1200 {
			if a.Tick(1); len(krakens) > cycle || resets > 0 {
				break
			}
		}
		if resets != 0 {
			t.Skip("the game started over before the Kraken")
		}
		if len(opens) != cycle+1 || opens[cycle] != (event.CaveOpenPayload{}) || opened != cycle+1 || len(krakens) != cycle+1 {
			t.Fatalf("tier %d: cave opens %v, %d announced, %d Krakens; want an opening to no rock, then a Kraken, a tier", cycle, opens, opened, len(krakens))
		}
		if len(spawned) != (cycle+1)*4*perRank {
			t.Fatalf("tier %d: %d raiders came while the cave opened, want none", cycle, len(spawned)-(cycle+1)*4*perRank)
		}
		var hp int
		w.RunSafe(func() {
			for _, e := range w.Components.Kraken.Entities() {
				combat, _ := w.Components.Combat.GetComponent(e)
				hp = combat.HitPoints
			}
		})
		cfg := w.Resources.Config
		edge := cfg.MapWidth - 1 - 2*int(parameter.KrakenBodyRadius)
		if k := krakens[cycle]; hp != 120 || k.X != edge || k.Y != cfg.MapHeight/2 {
			t.Fatalf("tier %d: the Kraken rose at (%d,%d) with %d hit points, want 120 at the right edge's (%d,%d)", cycle, k.X, k.Y, hp, edge, cfg.MapHeight/2)
		}
		_, halted := rock()
		if a.Tick(10); len(spawned) != (cycle+1)*4*perRank {
			t.Fatalf("tier %d: a raider came while the Kraken stood", cycle)
		}
		if _, origin := rock(); origin != halted {
			t.Fatalf("tier %d: the cave scrolled from column %d to %d with the Kraken up, want it halted", cycle, halted, origin)
		}

		// The player shoots it down, the raiders' drops still flying left out
		carried := streaks()
		before := map[core.Entity]bool{}
		w.RunSafe(func() {
			for _, e := range w.Components.Loot.Entities() {
				before[e] = true
			}
			for _, e := range w.Components.Kraken.Entities() {
				combat, _ := w.Components.Combat.GetPtr(e)
				combat.HitPoints, combat.LastDamagedBy = 0, w.Resources.Player.Entity
			}
		})
		a.Tick(2)
		drops := map[component.LootType]int{}
		var multiplier int64
		w.RunSafe(func() {
			for _, e := range w.Components.Loot.Entities() {
				if loot, _ := w.Components.Loot.GetComponent(e); loot.Owner == w.Resources.Player.Entity && !before[e] {
					drops[loot.Type]++
				}
			}
			multiplier = w.Resources.Status.Ints.Get("energy.damage_multiplier").Load()
		})
		if want := map[component.LootType]int{component.LootLauncher: 1, component.LootHeat: 1, component.LootEnergy: 2}; !maps.Equal(drops, want) {
			t.Fatalf("tier %d: the Kraken dropped %v, want %v", cycle, drops, want)
		}
		if multiplier != 2<<cycle || !slices.Contains(messages, "Species Damage Up") {
			t.Fatalf("tier %d: species damage x%d after the Kraken, messages %q; want x%d and its notice", cycle, multiplier, messages, 2<<cycle)
		}

		// The next cave: another stone art, the rock and the drift rushing in at twice
		// its pace while its mouth passes, then easing to its own, the raid held till then
		entries := entered
		next, start := rock()
		if next == art || !slices.Contains(arts, any(next)) || start > 1 {
			t.Fatalf("tier %d: the next cave wears %q from column %d after %q, want another of %v from its start", cycle, next, start, art, arts)
		}
		if got := streaks(); len(carried) == 0 || !slices.Equal(got, carried) {
			t.Fatalf("tier %d: the raider table's streaks went from %v to %v over the Kraken, want them carried on", cycle, carried, got)
		}
		if cols, drift := second(); cols < 2*speed-1 || cols > 2*speed+1 || drift != 160 {
			t.Fatalf("tier %d: the next cave rushed in %d columns in a second, the drift at %v; want %d and 160", cycle, cols, drift, 2*speed)
		}
		for range 600 {
			if a.Tick(1); entered > entries || resets > 0 {
				break
			}
		}
		if entered != entries+1 || len(spawned) != (cycle+1)*4*perRank {
			t.Fatalf("tier %d: the next cave filled the map %d times with %d raiders come since the Kraken, want once and none", cycle, entered-entries, len(spawned)-(cycle+1)*4*perRank)
		}
		if cols, drift := second(); cols < speed-1 || cols > speed+1 || drift != 80 {
			t.Fatalf("tier %d: the next cave scrolled %d columns in a second, the drift at %v; want %d and 80", cycle, cols, drift, speed)
		}
		art, pace = next, speed
	}
	shoot()
	if got := spawned[8*perRank]; rankOf[got] != 0 || !inArt(got, art) {
		t.Fatalf("the third cave's first raider is a %s of rank %d, want one of %s's first rank", got, rankOf[got]+1, art)
	}
}
