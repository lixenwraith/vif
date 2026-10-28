package app

import (
	"fmt"
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
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/pattern"
	"github.com/lixenwraith/vif/internal/resource"
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
// and ends two raiders as they arrive: one that dies by no player's hand drops
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

// TestVanguardAKillInAPairDropsEachPlayerOneU ends a raider by the host player's
// shot on a mesh pair. The raid's kill edge, an internal transition on the kill
// event, moves on the authority's crossing (D-20) and runs once on each instance,
// so each requests one U, and each player's ship takes the U its own instance
// simulates and goes up a level, where an edge proposed again on the lead would
// drop once more.
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

	var kills, drops, resets [2]int
	for i, a := range apps {
		a.SetDispatchTap(func(ev event.GameEvent) {
			switch p := ev.Payload.(type) {
			case *event.SpeciesKilledPayload:
				if ev.Type == event.EventSpeciesKilled && p.Species >= component.SpeciesDataFirst {
					kills[i]++
				}
			case *event.LootSpawnRequestPayload:
				if p.Type == component.LootUpgrade {
					drops[i]++
				}
			}
			if ev.Type == event.EventGameResetRequest {
				resets[i]++
			}
		})
	}
	step := func() {
		host.Tick(1)
		guest.Tick(1)
		_ = host.corrections.PublishDue()
		guest.ApplyPendingCorrections()
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

	// A raider both instances fly, ended by the host player's shot in both worlds
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
		if n := charges(a); n != 1 {
			t.Fatalf("instance %d: the ship's gun holds %d charges before the kill, want vanguard's first level", i, n)
		}
	}
	var shooter core.Entity
	host.World().RunSafe(func() { shooter = host.World().Resources.Player.Entity })
	for _, a := range apps {
		w := a.World()
		w.RunSafe(func() {
			if combat, ok := w.Components.Combat.GetPtr(raider); ok {
				combat.HitPoints, combat.LastDamagedBy = 0, shooter
			}
		})
	}

	for tick := 0; charges(host) < 2 || charges(guest) < 2; tick++ {
		if tick == 600 {
			t.Fatalf("the guns hold %d and %d charges 600 ticks after the kill, want 2 each", charges(host), charges(guest))
		}
		if resets != [2]int{} {
			t.Fatalf("tick %d: the game started over before a U reached each ship", tick)
		}
		step()
	}
	if h, g := charges(host), charges(guest); h != 2 || g != 2 {
		t.Fatalf("the guns hold %d and %d charges, want one level up each", h, g)
	}
	for range 30 {
		step()
	}
	if kills != [2]int{1, 1} || drops != [2]int{1, 1} {
		t.Fatalf("kills %v and U requests %v on host and guest, want one of each on each", kills, drops)
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

// TestVanguardsRaidClimbsARankEveryThreeKillsAndStartsOverAfterItsBoss: raiders
// the player shoots down as they arrive come from the first rank's two types until
// three fall, then the next rank's, to the fourth; its third kill brings the boss's
// green strobe and the first rank again
func TestVanguardsRaidClimbsARankEveryThreeKillsAndStartsOverAfterItsBoss(t *testing.T) {
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

	a := newVanguard(t)
	w := a.World()
	var spawned []string
	greens, resets := 0, 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		switch p := ev.Payload.(type) {
		case *event.SpeciesCreatedPayload:
			if p.Species >= component.SpeciesDataFirst {
				spawned = append(spawned, w.Resources.Behaviors[p.Species-component.SpeciesDataFirst].Name)
			}
		case *event.StrobeRequestPayload:
			if p.Color == (color.RGB{G: 255}) {
				greens++
			}
		}
		if ev.Type == event.EventGameResetRequest {
			resets++
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

	const perRank = 3
	for kill := range 4*perRank + 1 {
		shoot()
		if resets != 0 {
			t.Skip("the game started over mid-climb")
		}
		want := kill / perRank % 4
		if got := spawned[kill]; rankOf[got] != want {
			t.Fatalf("raider %d is a %s of rank %d, want rank %d after %d kills (%v)", kill, got, rankOf[got]+1, want+1, kill, spawned)
		}
		if bosses := kill / (4 * perRank); greens != bosses {
			t.Fatalf("after %d kills the boss strobed %d times, want %d", kill+1, greens, bosses)
		}
	}
}
