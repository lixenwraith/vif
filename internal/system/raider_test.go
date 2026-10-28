package system

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// newRaiderWorld spawns vanguard's scarab, compiled from its shipped program as
// the scenario expands it, at (20, 10) of an 80x40 world whose one target stands at
// (50, 10), behind rock open only along row 10: the header's line runs through, the
// scarab's 4x2 body with its header at (1, 0) does not. Every raider type flies
// the one cycle. It returns the raider's states by name.
func newRaiderWorld(t *testing.T) (*engine.World, *BehaviorSystem, core.Entity, map[string]component.BehaviorState) {
	t.Helper()
	const dir = "../../wad/scenario/vanguard"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("vanguard scenario not present: %v", err)
	}
	merged, err := fsm.ResolveScenario(os.DirFS(dir), "scenario.toml")
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := merged["behaviors"].(map[string]any)["scarab"].(map[string]any)
	set, err := CompileBehaviors(map[string]map[string]any{"scarab": doc})
	if err != nil {
		t.Fatal(err)
	}
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	w.Resources.Behaviors = set.Prototypes()
	if w.Resources.Target == nil {
		w.Resources.Target = &engine.TargetResource{}
	}
	aimAt(w, 50, 10)
	rockRows(w, 30, 34, 10, 11)

	s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
	standRaider(s, "scarab", 20, 10)
	headers := w.Components.Behavior.Entities()
	if len(headers) != 1 {
		t.Fatalf("%d raiders spawned, want one", len(headers))
	}
	w.Resources.Event.Queue.Consume()
	states := map[string]component.BehaviorState{}
	g := set.Graph(0)
	for _, st := range g.States() {
		states[g.Name(st)] = st
	}
	return w, s, headers[0], states
}

// standRaider asks for a raider with its header at the cell and lands it at once,
// completing the materialize it comes on through, so a test of its machine starts
// on the body the request stood
func standRaider(s *BehaviorSystem, program string, x, y int) {
	s.HandleEvent(event.GameEvent{
		Type:    event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: program, X: x, Y: y},
	})
	for _, ev := range s.world.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.MaterializeAreaRequestPayload); ok {
			s.HandleEvent(event.GameEvent{Type: event.EventMaterializeComplete, Domain: core.DomainShared, Payload: &event.MaterializeCompletedPayload{
				X: p.X, Y: p.Y, Type: p.Type, Species: p.Species,
			}})
		}
	}
}

// footprintInRock reports whether any cell of a 4x2 footprint with its header at
// (1, 0) holds rock
func footprintInRock(w *engine.World, pos component.PositionComponent) bool {
	return w.Positions.HasBlockingWallInArea(pos.X-1, pos.Y, 4, 2, component.WallBlockKinetic)
}

// TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding: a lock whose body
// cannot fly to the cell it stored beams to the nearest place by its target, the
// beam's line and landing marker drawn while the state holds; rock that comes over
// that place meanwhile moves the landing beside it, and the brake and the chase follow
func TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding(t *testing.T) {
	w, s, header, states := newRaiderWorld(t)
	b, _ := w.Components.Behavior.GetPtr(header)
	b.Machine = aboutToExpire(states["Lock"], 2*parameter.GameUpdateInterval)
	b.TargetX, b.TargetY = 50, 10
	start, _ := w.Positions.GetPosition(header)

	stepBehaviors(w, s)
	if b.Machine.State != states["Beam"] || !b.Beaming || !b.Landing {
		t.Fatalf("a blocked lock went to state %d beaming %t marked %t, want the beam marked",
			b.Machine.State, b.Beaming, b.Landing)
	}
	if b.TargetX != 50 || b.TargetY != 10 {
		t.Fatalf("the beam aims at (%d,%d), want the open target cell (50,10)", b.TargetX, b.TargetY)
	}

	// Rock comes over the landing, as a cave's does while a beam runs
	rockRows(w, 46, 56, 14, 40)
	for range 2 * parameter.SwarmTeleportDuration / parameter.GameUpdateInterval {
		stepBehaviors(w, s)
		if b.Machine.State != states["Beam"] {
			break
		}
	}
	pos, _ := w.Positions.GetPosition(header)
	if b.Machine.State != states["Brake"] || b.Beaming || b.Landing {
		t.Fatalf("after the beam: state %d beaming %t marked %t, want the brake unmarked",
			b.Machine.State, b.Beaming, b.Landing)
	}
	if pos == start || footprintInRock(w, pos) {
		t.Fatalf("landed at %+v from %+v, want moved and clear of rock", pos, start)
	}
	for range 4 {
		stepBehaviors(w, s)
	}
	if b.Machine.State != states["Chase"] {
		t.Fatalf("after the brake: state %d, want the chase", b.Machine.State)
	}
}

// TestARaiderLocksTheCursorNearestItInRosterOrder: with several cursors the lock
// stores the nearest one's cell, and on a tie the lower roster slot's, as the swarm
// does, so every instance of a session that holds the same Shared cursors aims alike;
// the lock draws the swarm's charge line toward it and marks where it lands
func TestARaiderLocksTheCursorNearestItInRosterOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cursors []component.PositionComponent
		want    component.PositionComponent
	}{
		{"nearest", []component.PositionComponent{{X: 20, Y: 20}, {X: 29, Y: 10}}, component.PositionComponent{X: 29, Y: 10}},
		{"tie", []component.PositionComponent{{X: 20, Y: 20}, {X: 30, Y: 10}}, component.PositionComponent{X: 20, Y: 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, s, header, states := newRaiderWorld(t)
			b, _ := w.Components.Behavior.GetPtr(header)
			b.Machine = aboutToExpire(states["Chase"], 2*parameter.GameUpdateInterval)
			aimAtCursors(w, tc.cursors...)

			stepBehaviors(w, s)
			if b.Machine.State != states["Lock"] || b.TargetX != tc.want.X || b.TargetY != tc.want.Y {
				t.Fatalf("state %d stored (%d,%d), want the lock on (%d,%d)",
					b.Machine.State, b.TargetX, b.TargetY, tc.want.X, tc.want.Y)
			}
			if !b.ChargeLine || !b.Landing {
				t.Fatalf("the lock draws charge line %t and landing marker %t, want both", b.ChargeLine, b.Landing)
			}
		})
	}
}

// TestARaidersBeamLandsByTheCellItsLockStored: a lock that cannot fly to its cell
// beams there, as the swarm's teleport does, however the cursors moved meanwhile:
// the locked player stepping away and another coming nearer move nothing
func TestARaidersBeamLandsByTheCellItsLockStored(t *testing.T) {
	w, s, header, states := newRaiderWorld(t)
	b, _ := w.Components.Behavior.GetPtr(header)
	b.Machine = aboutToExpire(states["Lock"], 2*parameter.GameUpdateInterval)
	b.TargetX, b.TargetY = 50, 10
	aimAtCursors(w, component.PositionComponent{X: 60, Y: 30}, component.PositionComponent{X: 24, Y: 20})

	stepBehaviors(w, s)
	if b.Machine.State != states["Beam"] {
		t.Fatalf("a blocked lock went to state %d, want the beam", b.Machine.State)
	}
	for range 2 * parameter.SwarmTeleportDuration / parameter.GameUpdateInterval {
		stepBehaviors(w, s)
		if b.Machine.State != states["Beam"] {
			break
		}
	}
	if pos, _ := w.Positions.GetPosition(header); pos.X != 50 || pos.Y != 10 {
		t.Fatalf("landed at (%d,%d), want the stored cell (50,10)", pos.X, pos.Y)
	}
}

// TestAStunDuringARaidersBeamMovesNothing: the landing is the beam's timeout, so a
// stun that ends the beam leaves the raider where it stood, back in the chase
func TestAStunDuringARaidersBeamMovesNothing(t *testing.T) {
	w, s, header, states := newRaiderWorld(t)
	b, _ := w.Components.Behavior.GetPtr(header)
	b.Machine = machine.Instance[component.BehaviorState]{
		State: states["Beam"], Elapsed: parameter.SwarmTeleportDuration / 2, Duration: parameter.SwarmTeleportDuration,
	}
	b.TargetX, b.TargetY = 50, 10
	combat, _ := w.Components.Combat.GetPtr(header)
	combat.Status[component.StatusStun] = component.Status{Remaining: parameter.PulseStunDuration}
	start, _ := w.Positions.GetPosition(header)

	for range 3 {
		stepBehaviors(w, s)
	}
	if pos, _ := w.Positions.GetPosition(header); pos != start || b.Machine.State != states["Chase"] || b.Beaming {
		t.Fatalf("stunned mid-beam: at %+v in state %d beaming %t, want still at %+v in the chase",
			pos, b.Machine.State, b.Beaming, start)
	}
}

// vanguardBehaviors compiles every program of the shipped vanguard scenario, as the
// scenario expands them, and returns the expanded documents beside them
func vanguardBehaviors(t *testing.T) (*BehaviorSet, map[string]map[string]any) {
	set, docs, _ := vanguardScenario(t)
	return set, docs
}

// vanguardScenario is vanguardBehaviors with the expanded scenario beside them
func vanguardScenario(t *testing.T) (*BehaviorSet, map[string]map[string]any, map[string]any) {
	t.Helper()
	const dir = "../../wad/scenario/vanguard"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("vanguard scenario not present: %v", err)
	}
	merged, err := fsm.ResolveScenario(os.DirFS(dir), "scenario.toml")
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]map[string]any{}
	for name, doc := range merged["behaviors"].(map[string]any) {
		docs[name] = doc.(map[string]any)
	}
	set, err := CompileBehaviors(docs)
	if err != nil {
		t.Fatal(err)
	}
	return set, docs, merged
}

// TestARaiderRequestedAtAnEdgeStandsFlushAgainstIt: a spawn names a cell blind to
// the body, so a request on any edge of the map, or past it, stands each raider
// type against that edge on the row or column it named, whatever its size and
// header offset; a scenario scrolling from any side spawns at its edge alike
func TestARaiderRequestedAtAnEdgeStandsFlushAgainstIt(t *testing.T) {
	set, _ := vanguardBehaviors(t)
	newWorld := func() *engine.World {
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
		w.Resources.Behaviors = set.Prototypes()
		if w.Resources.Target == nil {
			w.Resources.Target = &engine.TargetResource{}
		}
		return w
	}
	cfg := newWorld().Resources.Config
	width, height, row, col := cfg.MapWidth, cfg.MapHeight, cfg.MapHeight/2, cfg.MapWidth/2
	for i, p := range set.Prototypes() {
		right, bottom := width-p.Width+p.HeaderX, height-p.Height+p.HeaderY
		for _, tc := range []struct {
			edge         string
			x, y         int
			wantX, wantY int
		}{
			{"right", width - 1, row, right, row},
			{"past the right", width + 9, row, right, row},
			{"left", 0, row, p.HeaderX, row},
			{"top", col, -3, col, p.HeaderY},
			{"bottom", col, height - 1, col, bottom},
			{"corner", width + 2, height + 2, right, bottom},
		} {
			t.Run(p.Name+"/"+tc.edge, func(t *testing.T) {
				w := newWorld()
				s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
				standRaider(s, set.Names()[i], tc.x, tc.y)
				headers := w.Components.Behavior.Entities()
				if len(headers) != 1 {
					t.Fatalf("%d spawned, want one", len(headers))
				}
				if pos, _ := w.Positions.GetPosition(headers[0]); pos.X != tc.wantX || pos.Y != tc.wantY {
					t.Fatalf("a %dx%d body asked for (%d,%d) stands at (%d,%d), want (%d,%d)",
						p.Width, p.Height, tc.x, tc.y, pos.X, pos.Y, tc.wantX, tc.wantY)
				}
			})
		}
	}
}

// TestEveryVanguardRaiderTypeFliesTheCycle: each raider type, compiled as the
// scenario expands it, chases, locks, charges along a clear line, brakes and chases
// again, closing on its target, and wears a look of its own footprint
func TestEveryVanguardRaiderTypeFliesTheCycle(t *testing.T) {
	set, docs := vanguardBehaviors(t)
	for i, p := range set.Prototypes() {
		t.Run(p.Name, func(t *testing.T) {
			for f, frame := range p.Look.Frames {
				if len(frame) != p.Width*p.Height {
					t.Fatalf("frame %d holds %d cells, want %d×%d", f, len(frame), p.Width, p.Height)
				}
			}
			if len(p.Look.Frames) < 2 || !p.Look.Console {
				t.Fatalf("%d frames, console pair %t: want an animation and a console pair", len(p.Look.Frames), p.Look.Console)
			}

			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
			w.Resources.Behaviors = set.Prototypes()
			if w.Resources.Target == nil {
				w.Resources.Target = &engine.TargetResource{}
			}
			aimAt(w, 60, 20)
			s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
			standRaider(s, p.Name, 20, 20)
			headers := w.Components.Behavior.Entities()
			if len(headers) != 1 {
				t.Fatalf("%d spawned, want one", len(headers))
			}
			b, _ := w.Components.Behavior.GetPtr(headers[0])
			if int(b.Program) != i {
				t.Fatalf("spawned program %d, want %d", b.Program, i)
			}

			g := set.Graph(i)
			path, ticks := []string{g.Name(b.Machine.State)}, []int{0}
			for range int(15 * time.Second / parameter.GameUpdateInterval) {
				stepBehaviors(w, s)
				if name := g.Name(b.Machine.State); name != path[len(path)-1] {
					path, ticks = append(path, name), append(ticks, 0)
				}
				if len(path) == 5 {
					break
				}
				ticks[len(ticks)-1]++
			}
			if want := []string{"Chase", "Lock", "Charge", "Brake", "Chase"}; !slices.Equal(path, want) {
				t.Fatalf("flew %v, want %v", path, want)
			}
			// The lock holds the type's own lock_ms, the macro's parameter
			lock := docs[p.Name]["states"].(map[string]any)["Lock"].(map[string]any)["duration_ms"].(int64)
			if held := time.Duration(ticks[1]) * parameter.GameUpdateInterval; held != time.Duration(lock)*time.Millisecond {
				t.Fatalf("locked for %v, want its lock_ms %d", held, lock)
			}
			if pos, _ := w.Positions.GetPosition(headers[0]); pos.X <= 20 {
				t.Fatalf("stands at %v after its charge, want closer to its target at (60, 20)", pos)
			}
		})
	}
}

// TestEachVanguardCaveRaidsWithTheSameRanks: the raid's pick lists eight raider
// types to each stone art, in the order CaveStart's pick lists the arts, and every
// program once. Each cave's eight wear one palette and console colour, and fly the
// same ranks in the same order, its hit points rising, so caves differ in looks
// alone. The U drop names every type.
func TestEachVanguardCaveRaidsWithTheSameRanks(t *testing.T) {
	set, _, merged := vanguardScenario(t)
	path := func(v any, keys ...any) any {
		for _, k := range keys {
			switch k := k.(type) {
			case string:
				v = v.(map[string]any)[k]
			case int:
				v = v.([]any)[k]
			}
		}
		return v
	}
	list := func(v any) []string {
		var out []string
		for _, s := range v.([]any) {
			out = append(out, s.(string))
		}
		return out
	}
	states := merged["states"]
	arts := list(path(states, "CaveStart", "on_enter", 1, "pick", "from"))
	raiders := list(path(states, "RaidSpawn", "transitions", 0, "actions", 0, "pick", "from"))
	drops := list(path(states, "Raid", "transitions", 0, "guard_args", "guards", 0, "args", "names"))

	const perCave = 8
	if len(raiders) != perCave*len(arts) {
		t.Fatalf("the raid picks among %d types, want %d to each of %d stone arts", len(raiders), perCave, len(arts))
	}
	if got := slices.Sorted(slices.Values(raiders)); !slices.Equal(got, set.Names()) {
		t.Fatalf("the raid picks %v, want every program once: %v", got, set.Names())
	}
	if got := slices.Sorted(slices.Values(drops)); !slices.Equal(got, set.Names()) {
		t.Fatalf("the U drop names %v, want every program: %v", got, set.Names())
	}

	byName := map[string]component.BehaviorPrototype{}
	for _, p := range set.Prototypes() {
		byName[p.Name] = p
	}
	// stats is what a rank decides: what it takes to kill, what it costs to touch,
	// how it flies
	type stats struct {
		hp      int
		mass    float64
		contact component.CursorDamage
		homing  physics.HomingProfile
	}
	ranks := func(cave []string) []stats {
		var out []stats
		for _, name := range cave {
			p := byName[name]
			out = append(out, stats{p.HitPoints, p.Mass, p.Contact, p.Homing})
		}
		return out
	}
	first := ranks(raiders[:perCave])
	for i := 1; i < perCave; i++ {
		if first[i].hp < first[i-1].hp {
			t.Fatalf("%s has %d hit points after %s's %d, want the ranks rising", raiders[i], first[i].hp, raiders[i-1], first[i-1].hp)
		}
	}
	if first[0].hp == first[perCave-1].hp {
		t.Fatalf("every raider of a cave has %d hit points, want ranks", first[0].hp)
	}
	for c, art := range arts {
		cave := raiders[c*perCave : (c+1)*perCave]
		if got := ranks(cave); !slices.Equal(got, first) {
			t.Errorf("%s's raiders %v fly %+v, want the strata's ranks %+v", art, cave, got, first)
		}
		look := byName[cave[0]].Look
		for _, name := range cave[1:] {
			if l := byName[name].Look; !slices.Equal(l.Fg, look.Fg) || l.Fg256 != look.Fg256 {
				t.Errorf("%s wears %v (console %d), want %s's palette %v (console %d) as the rest of %s's",
					name, l.Fg, l.Fg256, cave[0], look.Fg, look.Fg256, art)
			}
		}
	}
}
