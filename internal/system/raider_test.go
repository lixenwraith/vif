package system

import (
	"os"
	"testing"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
)

// newRaiderWorld spawns vanguard's raider, compiled from its shipped program, at
// (20, 10) of an 80x40 world whose one target stands at (50, 10), behind a column
// of rock that blocks the line between them. It returns the raider's states by name.
func newRaiderWorld(t *testing.T) (*engine.World, *BehaviorSystem, core.Entity, map[string]component.BehaviorState) {
	t.Helper()
	src, err := os.ReadFile("../../wad/scenario/vanguard/behavior/raider.toml")
	if err != nil {
		t.Skipf("vanguard's raider program not present: %v", err)
	}
	doc, err := toml.NewParser(src).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"raider": doc})
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
	rockRows(w, 30, 34, 40)

	s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
	s.HandleEvent(event.GameEvent{
		Type:    event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "raider", X: 20, Y: 10},
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

// TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding: a lock that cannot
// see its target beams to the nearest place by it, the beam's line drawn from the
// header while the state holds; rock that comes over that place meanwhile moves the
// landing beside it, and the brake and the chase follow
func TestARaiderBeamsAroundRockAndLandsBesideWhatTookItsLanding(t *testing.T) {
	w, s, header, states := newRaiderWorld(t)
	b, _ := w.Components.Behavior.GetPtr(header)
	b.Machine = aboutToExpire(states["Lock"], 2*parameter.GameUpdateInterval)
	start, _ := w.Positions.GetPosition(header)

	stepBehaviors(w, s)
	if b.Machine.State != states["Beam"] || !b.Beaming {
		t.Fatalf("a blocked lock went to state %d beaming %t, want the beam", b.Machine.State, b.Beaming)
	}
	if b.TargetX != 50 || b.TargetY != 10 {
		t.Fatalf("the beam aims at (%d,%d), want the open target cell (50,10)", b.TargetX, b.TargetY)
	}

	// Rock comes over the landing, as a cave's does while a beam runs
	rockRows(w, 46, 56, 14)
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
