package system

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
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
mass = 2.0
drops = "swarm"
contact = { energy = 1000, heat = 10 }
lifetime_ms = 35000
frames = [{ glyphs = ["▄▀▀▄", "▀▄▄▀"] }, { glyphs = ["▀▄▄▀", "▄▀▀▄"] }]
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
	return newProgramWorld(t, wardenProgram, x, y)
}

// newProgramWorld is newWardenWorld with program as the warden's
func newProgramWorld(t *testing.T, program string, x, y int) (*engine.GameContext, *BehaviorSystem, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	doc, err := toml.NewParser([]byte(program)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil)
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
	aimAtCursors(w, component.PositionComponent{X: x, Y: y})
}

// aimAtCursors sets the cursor group as navigation publishes it: one target per
// cursor, in roster order
func aimAtCursors(w *engine.World, cells ...component.PositionComponent) {
	state := engine.TargetGroupState{Type: component.TargetCursor, Count: len(cells), Valid: true}
	for i, c := range cells {
		state.Targets[i] = engine.TargetData{PosX: c.X, PosY: c.Y}
	}
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
	materializes := countEvents(stepWhile(t, w, s, b, wardenWind), event.EventMaterializeAreaRequest)
	if b.Machine.State != wardenStrike || combat.IsEnraged || !b.Shielded ||
		combat.Status[component.StatusWard].Remaining <= 0 {
		t.Fatalf("wind-up ended: state %d enraged %v shielded %v ward %v, want a warded strike",
			b.Machine.State, combat.IsEnraged, b.Shielded, combat.Status[component.StatusWard].Remaining)
	}
	if materializes != 1 {
		t.Fatalf("the wind-up's cue materialized %d times, want once", materializes)
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

// newEntranceWorld runs entrances over an 80x40 world whose one program is the
// warden, materializing as it comes on, its prototype taking any further lines
func newEntranceWorld(t *testing.T, lines ...string) (*materializeWorld, *BehaviorSystem) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	prototype := "[prototype]\nentrance = \"materialize\"\n"
	for _, line := range lines {
		prototype += line + "\n"
	}
	program := strings.Replace(wardenProgram, "[prototype]\n", prototype, 1)
	doc, err := toml.NewParser([]byte(program)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Resources.Behaviors = set.Prototypes()
	w.Resources.Target = &engine.TargetResource{}
	s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)
	return newMaterializeWorld(w, s), s
}

// enterWarden asks for a warden with its header at the cell
func enterWarden(e *materializeWorld, x, y int) {
	e.deliver(event.GameEvent{
		Type:    event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: x, Y: y},
	})
}

// TestAnEnteringDataSpeciesLandsAsItsMaterializeEnds: a request stands nothing; a
// Shared materialize names the species over the footprint, counted as an instance,
// and the body lands the tick it ends, on that footprint or, rock having come over
// it, on one clear of the rock
func TestAnEnteringDataSpeciesLandsAsItsMaterializeEnds(t *testing.T) {
	e, s := newEntranceWorld(t)
	w := e.w
	p := w.Resources.Behaviors[0]
	enterWarden(e, wardenX, wardenY)
	enterWarden(e, wardenX+20, wardenY)
	var footprints []component.MaterializeComponent
	for _, m := range w.Components.Materialize.Entities() {
		c, _ := w.Components.Materialize.GetComponent(m)
		if m.Domain() != core.DomainShared || c.Species != p.Species || c.AreaWidth != p.Width || c.AreaHeight != p.Height {
			t.Fatalf("materialize %+v, want a Shared one naming the warden over its %dx%d footprint", c, p.Width, p.Height)
		}
		footprints = append(footprints, c)
	}
	s.Update()
	if n := w.Resources.Status.Ints.Get("behavior.warden.count").Load(); len(footprints) != 2 || n != 2 || w.Components.Behavior.CountEntities() != 0 {
		t.Fatalf("%d materializes counted as %d wardens, want two arriving and none standing", len(footprints), n)
	}

	for range materializeTicks() - 1 {
		e.tick()
	}
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("%d wardens landed before their materializes ended", n)
	}
	rock := footprints[1]
	spawnWall(w, rock.TargetX, rock.TargetY)
	e.tick()
	headers := w.Components.Behavior.Entities()
	if len(headers) != 2 || w.Components.Materialize.CountEntities() != 0 {
		t.Fatalf("%d wardens landed as their materializes ended, want two", len(headers))
	}
	landed := map[[2]int]bool{}
	for _, header := range headers {
		pos, _ := w.Positions.GetPosition(header)
		left, top := pos.X-p.HeaderX, pos.Y-p.HeaderY
		if !w.Positions.IsAreaFree(left, top, p.Width, p.Height, component.WallBlockSpawn) {
			t.Fatalf("a warden landed at (%d, %d) on the rock that came over its footprint", left, top)
		}
		landed[[2]int{left, top}] = true
	}
	if clear := footprints[0]; !landed[[2]int{clear.TargetX, clear.TargetY}] {
		t.Fatalf("wardens landed at %v, none on the clear footprint at (%d, %d)", landed, clear.TargetX, clear.TargetY)
	}
}

// TestAnEntranceLastsItsPrototypesDuration: entrance_ms times the materialize in
// place of its style's second, and the body lands the tick it ends
func TestAnEntranceLastsItsPrototypesDuration(t *testing.T) {
	e, _ := newEntranceWorld(t, "entrance_ms = 250")
	w := e.w
	enterWarden(e, wardenX, wardenY)
	ticks := int(250 * time.Millisecond / parameter.GameUpdateInterval)
	for range ticks - 1 {
		e.tick()
	}
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("%d wardens landed before their 250 ms entrance ended", n)
	}
	e.tick()
	if n := w.Components.Behavior.CountEntities(); n != 1 || w.Components.Materialize.CountEntities() != 0 {
		t.Fatalf("%d wardens landed on the entrance's tick %d, want one as it ended", n, ticks)
	}
}

// TestAMaterializeCueTimesItsMaterialize: the verb's duration_ms rides its request,
// and a negative one fails the compile
func TestAMaterializeCueTimesItsMaterialize(t *testing.T) {
	const swarm = `payload = { spawn = "swarm" }`
	timed := func(ms string) string {
		return strings.Replace(wardenProgram, swarm, `payload = { spawn = "swarm", duration_ms = `+ms+` }`, 1)
	}
	ctx, s, header := newProgramWorld(t, timed("300"), wardenX+40, wardenY)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)
	stepBehaviors(w, s)
	var request *event.MaterializeAreaRequestPayload
	for _, ev := range stepWhile(t, w, s, b, wardenWind) {
		if p, ok := ev.Payload.(*event.MaterializeAreaRequestPayload); ok {
			request = p
		}
	}
	if request == nil || request.DurationMs != 300 {
		t.Fatalf("the wind-up's cue requested %+v, want a 300 ms materialize", request)
	}

	doc, err := toml.NewParser([]byte(timed("-1"))).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil); err == nil || !strings.Contains(err.Error(), "duration_ms -1 is negative") {
		t.Fatalf("a negative duration compiled: %v", err)
	}
}

// TestACancelEndsItsProgramsEntrancesInFlight: a materialize is the pending spawn,
// so the cancel that ends a program's instances ends it too, and nothing lands after
func TestACancelEndsItsProgramsEntrancesInFlight(t *testing.T) {
	e, _ := newEntranceWorld(t)
	w := e.w
	enterWarden(e, wardenX, wardenY)
	e.tick()
	e.deliver(event.GameEvent{Type: event.EventBehaviorCancelRequest, Payload: &event.BehaviorCancelRequestPayload{Program: "warden"}})
	if n := w.Components.Materialize.CountEntities(); n != 0 {
		t.Fatalf("%d materializes outlived the cancel", n)
	}
	for range materializeTicks() {
		e.tick()
	}
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("%d wardens landed after their cancel", n)
	}
}

// TestOnlyASharedMaterializeLandsADataSpecies: a player-domain materialize ran on
// one instance alone, so its completion names a species no other instance spawns
func TestOnlyASharedMaterializeLandsADataSpecies(t *testing.T) {
	e, _ := newEntranceWorld(t)
	w := e.w
	e.deliver(event.GameEvent{
		Type:   event.EventMaterializeComplete,
		Domain: core.DomainPlayer,
		Payload: &event.MaterializeCompletedPayload{
			X: wardenX, Y: wardenY, Type: component.SpawnTypeSwarm, Species: w.Resources.Behaviors[0].Species,
		},
	})
	if n := w.Components.Behavior.CountEntities(); n != 0 {
		t.Fatalf("a player materialize landed %d wardens", n)
	}
}

// TestASpawnCountsItsInstanceAtOnce: a spawn landing after the step, as an
// entrance's does, shows in its program's count before the next step, so a region
// reading the count never takes a landed instance for gone
func TestASpawnCountsItsInstanceAtOnce(t *testing.T) {
	ctx, _, _ := newWardenWorld(t, wardenX+4, wardenY)
	if n := ctx.World.Resources.Status.Ints.Get("behavior.warden.count").Load(); n != 1 {
		t.Fatalf("behavior.warden.count = %d before the next step, want the landed warden", n)
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

// TestADataSpeciesIsKnockedBackByItsOwnMass: a tracer pushes a warden of the
// swarm's mass as it pushes the swarm, and one of twice that mass half as far
func TestADataSpeciesIsKnockedBackByItsOwnMass(t *testing.T) {
	ctx, _, header := newWardenWorld(t, wardenX+4, wardenY)
	w := ctx.World
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	attack := profile.Attack(component.CombatAttackTracer, component.CombatEntityCursor, component.CombatEntitySwarm)
	push := func(mass float64) float64 {
		w.Resources.Behaviors[0].Mass = mass
		x, _ := combat.knockbackImpulse(header, 1, 0, 0, 0, attack, nil)
		return x
	}
	swarm, _ := physics.ApplyOffsetCollisionImpulse(1, 0, 0, 0, 0, attack.Collision.MassRatio, 0,
		attack.Collision.ImpulseMin, attack.Collision.ImpulseMin, nil)
	if got := push(profile.MassSwarm); got != swarm {
		t.Fatalf("a warden of the swarm's mass pushed %v, want the swarm's %v", got, swarm)
	}
	if got := push(2 * profile.MassSwarm); got != swarm/2 {
		t.Fatalf("a warden of twice the swarm's mass pushed %v, want half the swarm's %v", got, swarm)
	}
}

// TestAnEmittedPayloadNamesADataSpeciesByItsProgram: EmitEvent spells a species by
// a program's name, which its set numbers, and a name no species holds fails the
// compile
func TestAnEmittedPayloadNamesADataSpeciesByItsProgram(t *testing.T) {
	const enter = `on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]`
	emitting := func(species string) string {
		return strings.Replace(wardenProgram, enter, strings.TrimSuffix(enter, "]")+
			`, { action = "EmitEvent", event = "EventGeneticAbandonEval", payload = { species = "`+species+`" } }]`, 1)
	}
	ctx, s, _ := newProgramWorld(t, emitting("warden"), wardenX+40, wardenY)
	var abandoned *event.GeneticAbandonEvalPayload
	for _, ev := range stepBehaviors(ctx.World, s) {
		if p, ok := ev.Payload.(*event.GeneticAbandonEvalPayload); ok {
			abandoned = p
		}
	}
	if abandoned == nil || abandoned.Species != component.SpeciesDataFirst {
		t.Fatalf("entering Wind emitted %+v, want species %d", abandoned, component.SpeciesDataFirst)
	}

	doc, err := toml.NewParser([]byte(emitting("ghost"))).Parse()
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileBehaviors(map[string]map[string]any{"warden": doc}, nil)
	if err == nil || !strings.Contains(err.Error(), `species: "ghost" is not one of`) {
		t.Fatalf("a payload naming no species compiled: %v", err)
	}
}
