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
)

// newRaiderWorld spawns vanguard's invader, compiled from its shipped program as
// the scenario expands it, at (20, 10) of an 80x40 world whose one target stands at
// (50, 10), behind rock open only along row 10: the header's line runs through, the
// invader's 4x2 body with its header at (1, 0) does not. Every raider type flies
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
	doc, _ := merged["behaviors"].(map[string]any)["invader"].(map[string]any)
	set, err := CompileBehaviors(map[string]map[string]any{"invader": doc})
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
	s.HandleEvent(event.GameEvent{
		Type:    event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "invader", X: 20, Y: 10},
	})
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

// footprintInRock reports whether any cell of a 4x2 footprint with its header at
// (1, 0) holds rock
func footprintInRock(w *engine.World, pos component.PositionComponent) bool {
	return w.Positions.HasBlockingWallInArea(pos.X-1, pos.Y, 4, 2, component.WallBlockKinetic)
}

// TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding: a lock whose body
// cannot fly to the cell it stored beams to the nearest place by its target, the
// beam's line drawn from the header while the state holds; rock that comes over that
// place meanwhile moves the landing beside it, and the brake and the chase follow
func TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding(t *testing.T) {
	w, s, header, states := newRaiderWorld(t)
	b, _ := w.Components.Behavior.GetPtr(header)
	b.Machine = aboutToExpire(states["Lock"], 2*parameter.GameUpdateInterval)
	b.TargetX, b.TargetY = 50, 10
	start, _ := w.Positions.GetPosition(header)

	stepBehaviors(w, s)
	if b.Machine.State != states["Beam"] || !b.Beaming {
		t.Fatalf("a blocked lock went to state %d beaming %t, want the beam", b.Machine.State, b.Beaming)
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
	if b.Machine.State != states["Brake"] || b.Beaming {
		t.Fatalf("after the beam: state %d beaming %t, want the brake", b.Machine.State, b.Beaming)
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
// the lock draws the swarm's charge line toward it
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
			if !b.ChargeLine {
				t.Fatal("the lock draws no charge line")
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
	return set, docs
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
				s.HandleEvent(event.GameEvent{
					Type:    event.EventBehaviorSpawnRequest,
					Payload: &event.BehaviorSpawnRequestPayload{Program: set.Names()[i], X: tc.x, Y: tc.y},
				})
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
	if got := set.Names(); !slices.Equal(got, []string{"construct", "invader", "scarab", "storm_eye"}) {
		t.Fatalf("programs %v, want the four raider types", got)
	}
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
			s.HandleEvent(event.GameEvent{
				Type:    event.EventBehaviorSpawnRequest,
				Payload: &event.BehaviorSpawnRequestPayload{Program: p.Name, X: 20, Y: 20},
			})
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
