package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// wardenProgram homes on its target, winds up under a telegraph past twelve cells,
// then charges the cell it stored; a stun drops it back to Roam from either phase
const wardenProgram = `
initial = "Roam"
suspend = { guard = "Stunned" }

[prototype]
width = 4
height = 2
header = { x = 1, y = 0 }
hp = 40
combat = "swarm"
homing = "swarm"
mass = 2.0
drops = "swarm"
contact = { energy = 1000, heat = 10 }
lifetime_ms = 35000
glyphs = [["▄▀▀▄", "▀▄▄▀"], ["▀▄▄▀", "▄▀▀▄"]]
pattern_ms = 500
color = { r = 200, g = 120, b = 40 }

[states.Roam]
on_update = [{ action = "Home" }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { op = "gt", cells = 12 }, target = "Wind" }]

[states.Wind]
duration_ms = 1500
flags = ["Enraged"]
on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]
on_update = [{ action = "Freeze" }]
on_exit = [{ action = "CancelTelegraph" }]
cues = [{ at = 800, action = "Materialize", payload = { spawn = "swarm" } }]
transitions = [{ trigger = "Timeout", target = ["Strike", "Roam"] }]

[states.Strike]
duration_ms = 800
flags = ["Shielded"]
probe = "ResolveTarget"
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [{ trigger = "Timeout", target = "Roam" }, { trigger = "AfterTick", guard = "HitWall", target = "Roam" }]

[[global]]
from = ["Wind", "Strike"]
guard = "Stunned"
target = "Roam"
`

// The warden's states take their IDs in sorted-name order
const (
	wardenRoam component.BehaviorState = iota
	wardenStrike
	wardenWind
)

const wardenX, wardenY = 20, 10

// newWardenWorld spawns one warden at (20, 10) of an 80x40 world whose target group
// holds one target at (x, y). The spawn's events are consumed.
func newWardenWorld(t *testing.T, x, y int) (*engine.GameContext, *BehaviorSystem, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	doc, err := toml.NewParser([]byte(wardenProgram)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc})
	if err != nil {
		t.Fatal(err)
	}
	w.Resources.Behaviors = set.Prototypes()
	if w.Resources.Target == nil {
		w.Resources.Target = &engine.TargetResource{}
	}
	aimAt(w, x, y)

	s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
	s.HandleEvent(event.GameEvent{
		Type:    event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: wardenX, Y: wardenY},
	})
	headers := w.Components.Behavior.Entities()
	if len(headers) != 1 {
		t.Fatalf("%d wardens spawned, want one", len(headers))
	}
	w.Resources.Event.Queue.Consume()
	return ctx, s, headers[0]
}

// aimAt makes (x, y) the target group's one target
func aimAt(w *engine.World, x, y int) {
	state := engine.TargetGroupState{Type: component.TargetCursor, Count: 1, Valid: true}
	state.Targets[0] = engine.TargetData{PosX: x, PosY: y}
	w.Resources.Target.SetGroup(0, state)
}

// stepBehaviors runs one tick of the behavior system and returns the events it raised
func stepBehaviors(w *engine.World, s *BehaviorSystem) []event.GameEvent {
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	s.Update()
	return w.Resources.Event.Queue.Consume()
}

// stepWhile steps until the warden leaves state, failing past a bound well over
// any of its durations
func stepWhile(t *testing.T, w *engine.World, s *BehaviorSystem, b *component.BehaviorComponent, state component.BehaviorState) []event.GameEvent {
	t.Helper()
	var events []event.GameEvent
	for range int(2 * time.Second / parameter.GameUpdateInterval) {
		events = append(events, stepBehaviors(w, s)...)
		if b.Machine.State != state {
			return events
		}
	}
	t.Fatalf("warden never left state %d", state)
	return nil
}

// TestAWardenWindsUpPastItsRangeThenChargesTheCellItStored: the flags of the state
// it stands in become the combat outputs on every step, the telegraph counts down
// the wind-up's own duration, and the cue fires once
func TestAWardenWindsUpPastItsRangeThenChargesTheCellItStored(t *testing.T) {
	ctx, s, header := newWardenWorld(t, wardenX+40, wardenY)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)
	combat, _ := w.Components.Combat.GetPtr(header)

	events := stepBehaviors(w, s)
	if b.Machine.State != wardenWind || !combat.IsEnraged || b.Shielded {
		t.Fatalf("past twelve cells: state %d enraged %v shielded %v, want an enraged wind-up",
			b.Machine.State, combat.IsEnraged, b.Shielded)
	}
	var splash *event.SplashTimerRequestPayload
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.SplashTimerRequestPayload); ok {
			splash = p
		}
	}
	if splash == nil || splash.AnchorEntity != header || splash.Duration != 1500*time.Millisecond {
		t.Fatalf("telegraph %+v, want the header's counting down 1.5 s", splash)
	}

	start, _ := w.Positions.GetPosition(header)
	beams := countEvents(stepWhile(t, w, s, b, wardenWind), event.EventMaterializeAreaRequest)
	if b.Machine.State != wardenStrike || combat.IsEnraged || !b.Shielded ||
		combat.Status[component.StatusWard].Remaining <= 0 {
		t.Fatalf("wind-up ended: state %d enraged %v shielded %v ward %v, want a warded strike",
			b.Machine.State, combat.IsEnraged, b.Shielded, combat.Status[component.StatusWard].Remaining)
	}
	if beams != 1 {
		t.Fatalf("the wind-up's cue beamed %d times, want once", beams)
	}
	if b.TargetX != wardenX+40 || b.TargetY != wardenY {
		t.Fatalf("strike stored (%d,%d), want the target's cell", b.TargetX, b.TargetY)
	}

	stepWhile(t, w, s, b, wardenStrike)
	end, _ := w.Positions.GetPosition(header)
	if b.Machine.State != wardenRoam || !(end.X > start.X+20) || end.Y != start.Y {
		t.Fatalf("after the strike: state %d at (%d,%d) from (%d,%d), want roaming near the target",
			b.Machine.State, end.X, end.Y, start.X, start.Y)
	}
}

// TestAStunSendsAWindingWardenBackToRoamAndEndsItsTelegraph: the global edge takes
// it out through the wind-up's exit, and the suspend then holds it where it stands
func TestAStunSendsAWindingWardenBackToRoamAndEndsItsTelegraph(t *testing.T) {
	ctx, s, header := newWardenWorld(t, wardenX+40, wardenY)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)
	combat, _ := w.Components.Combat.GetPtr(header)
	stepBehaviors(w, s)
	if b.Machine.State != wardenWind {
		t.Fatalf("state %d, want the wind-up", b.Machine.State)
	}

	combat.Status[component.StatusStun] = component.Status{Remaining: parameter.PulseStunDuration}
	if n := countEvents(stepBehaviors(w, s), event.EventSplashTimerCancel); n != 1 || b.Machine.State != wardenRoam || combat.IsEnraged {
		t.Fatalf("stunned: state %d enraged %v, %d telegraph cancels, want Roam and one", b.Machine.State, combat.IsEnraged, n)
	}
	held, _ := w.Positions.GetPosition(header)
	stepBehaviors(w, s)
	if pos, _ := w.Positions.GetPosition(header); b.Machine.State != wardenRoam || pos != held {
		t.Fatalf("a stunned warden moved to %+v or wound up again (state %d)", pos, b.Machine.State)
	}
}

// TestACancelRequestEndsTheWardenThroughItsComposite: the state's exit runs first,
// so a telegraph never outlives its warden
func TestACancelRequestEndsTheWardenThroughItsComposite(t *testing.T) {
	ctx, s, header := newWardenWorld(t, wardenX+40, wardenY)
	w := ctx.World
	stepBehaviors(w, s)

	s.HandleEvent(event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{Program: "warden"}})
	events := w.Resources.Event.Queue.Consume()
	if n := countEvents(events, event.EventSplashTimerCancel); n != 1 {
		t.Fatalf("cancel ended %d telegraphs, want the wind-up's", n)
	}
	composite := ready(NewCompositeSystem(w)).(*CompositeSystem)
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.CompositeDestroyRequestPayload); ok && p.HeaderEntity == header {
			composite.HandleEvent(ev)
		}
	}
	if w.Components.Behavior.HasEntity(header) || w.Components.Header.HasEntity(header) {
		t.Fatal("the warden outlived its cancel")
	}
	if n := w.Resources.Status.Ints.Get("behavior.warden.despawned").Load(); n != 1 {
		t.Fatalf("behavior.warden.despawned = %d, want 1", n)
	}
}

// TestARequestNamingNoProgramIsCountedAndDropped: no program is guessed at, and
// nothing spawns or ends
func TestARequestNamingNoProgramIsCountedAndDropped(t *testing.T) {
	ctx, s, header := newWardenWorld(t, wardenX+4, wardenY)
	w := ctx.World
	s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest, Payload: &event.BehaviorSpawnRequestPayload{Program: "wraith", X: 40, Y: 20}})
	s.HandleEvent(event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{Program: "wraith"}})
	if n := w.Resources.Status.Ints.Get("behavior.unknown_program_rejects").Load(); n != 2 {
		t.Fatalf("behavior.unknown_program_rejects = %d, want 2", n)
	}
	if headers := w.Components.Behavior.Entities(); len(headers) != 1 || headers[0] != header || len(w.Resources.Event.Queue.Consume()) != 0 {
		t.Fatalf("an unknown program changed the wardens: %v", headers)
	}
}

// TestAWardenAtZeroHitPointsDiesAsItsDataSpecies: the kill names the first data
// species, which meta counts under the program's name
func TestAWardenAtZeroHitPointsDiesAsItsDataSpecies(t *testing.T) {
	ctx, s, header := newWardenWorld(t, wardenX+4, wardenY)
	w := ctx.World
	meta := ready(NewMetaSystem(ctx)).(*MetaSystem)
	combat, _ := w.Components.Combat.GetPtr(header)
	combat.HitPoints = 0

	var killed *event.SpeciesKilledPayload
	for _, ev := range stepBehaviors(w, s) {
		if p, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			killed = p
			meta.HandleEvent(ev)
		}
	}
	if killed == nil || killed.Entity != header || killed.Species != component.SpeciesDataFirst {
		t.Fatalf("kill %+v, want the header as species %d", killed, component.SpeciesDataFirst)
	}
	if n := w.Resources.Status.Ints.Get("kills.warden").Load(); n != 1 {
		t.Fatalf("kills.warden = %d, want 1", n)
	}
}

// TestCombatRefusesToStunAShieldedDataSpecies: a state flagged Shielded refuses the
// stun as the quasar's shield does
func TestCombatRefusesToStunAShieldedDataSpecies(t *testing.T) {
	ctx, _, header := newWardenWorld(t, wardenX+4, wardenY)
	w := ctx.World
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	b, _ := w.Components.Behavior.GetPtr(header)
	c, _ := w.Components.Combat.GetPtr(header)

	b.Shielded = true
	if combat.applyStunEffect(header, c) {
		t.Fatal("a shielded warden was stunned")
	}
	b.Shielded = false
	if !combat.applyStunEffect(header, c) {
		t.Fatal("an unshielded warden refused the stun")
	}
}
