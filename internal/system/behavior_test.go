package system

import (
	"math"
	"slices"
	"strconv"
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
	"github.com/lixenwraith/vif/pkg/vmath"
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

// newWardenWorld spawns one warden at (20, 10) of an 80x40 terminal's world whose
// target group holds one target at (x, y). The spawn's events are consumed.
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
	materializes := countEvents(stepWhile(t, w, s, b, wardenWind), event.EventMaterializeRequest)
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

// TestALandingProbedAtItsPostFliesTheBodyBackToItsSpawnCell: to = "post" names the
// cell the header spawned on, so a sortie that stored its target's cell stores its
// post next
func TestALandingProbedAtItsPostFliesTheBodyBackToItsSpawnCell(t *testing.T) {
	head, _, _ := strings.Cut(wardenProgram, "[states.Roam]")
	program := strings.Replace(head, `initial = "Roam"`, `initial = "Sortie"`, 1) + `
[states.Rest]
on_update = [{ action = "Freeze" }]

[states.Sortie]
duration_ms = 500
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [{ trigger = "Timeout", target = "Return", fallback = "hold" }]

[states.Return]
duration_ms = 500
probe = "FindLanding"
probe_args = { to = "post" }
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [{ trigger = "Timeout", target = "Rest", actions = [{ action = "ZeroVelocity" }] }]
`
	const back, sortie component.BehaviorState = 1, 2
	ctx, s, header := newProgramWorld(t, program, wardenX+30, wardenY+15)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)

	stepWhile(t, w, s, b, sortie)
	out, _ := w.Positions.GetPosition(header)
	if b.Machine.State != back || out.X < wardenX+25 || out.Y < wardenY+10 {
		t.Fatalf("after the sortie: state %d at (%d,%d), want returning from by the target", b.Machine.State, out.X, out.Y)
	}
	if b.TargetX != wardenX || b.TargetY != wardenY {
		t.Fatalf("the return stored (%d,%d), want the spawn cell (%d,%d)", b.TargetX, b.TargetY, wardenX, wardenY)
	}
	stepWhile(t, w, s, b, back)
	if end, _ := w.Positions.GetPosition(header); end.X != wardenX || end.Y != wardenY {
		t.Fatalf("after the return the body stands at (%d,%d), want its spawn cell (%d,%d)", end.X, end.Y, wardenX, wardenY)
	}
}

// wardenHead is the warden's program down to its states, which a test writes
func wardenHead(initial string) string {
	head, _, _ := strings.Cut(wardenProgram, "[states.Roam]")
	return strings.Replace(head, `initial = "Roam"`, `initial = "`+initial+`"`, 1)
}

// TestAProgramSteersBackToThePostItWasPushedOff: Home steers to the cell its to
// names, and TargetDistance measures to it, so a body a wall pushed returns
func TestAProgramSteersBackToThePostItWasPushedOff(t *testing.T) {
	program := wardenHead("Hold") + `
[states.Hold]
on_update = [{ action = "Home", payload = { to = "post" } }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { to = "post", op = "gt", cells = 2 }, target = "Back" }]

[states.Back]
on_update = [{ action = "Home", payload = { to = "post" } }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { to = "post", op = "lt", cells = 1 }, target = "Hold" }]
`
	const back, hold component.BehaviorState = 0, 1
	ctx, s, header := newProgramWorld(t, program, wardenX+30, wardenY)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)
	for range 5 {
		stepBehaviors(w, s)
	}
	if pos, _ := w.Positions.GetPosition(header); b.Machine.State != hold || pos.X != wardenX || pos.Y != wardenY {
		t.Fatalf("a warden on its post stands at (%d,%d) in state %d, want its post held", pos.X, pos.Y, b.Machine.State)
	}

	kinetic, _ := w.Components.Kinetic.GetPtr(header)
	kinetic.PreciseX += 6
	s.place(header, wardenX+6, wardenY)
	if stepBehaviors(w, s); b.Machine.State != back {
		t.Fatalf("six cells off its post the warden is in state %d, want Back", b.Machine.State)
	}
	for range int(5 * time.Second / parameter.GameUpdateInterval) {
		stepBehaviors(w, s)
	}
	if pos, _ := w.Positions.GetPosition(header); b.Machine.State != hold || pos.X != wardenX || pos.Y != wardenY {
		t.Fatalf("five seconds on the warden stands at (%d,%d) in state %d, want its post (%d,%d) held",
			pos.X, pos.Y, b.Machine.State, wardenX, wardenY)
	}
}

// TestAWaypointSpansTheFootprintsPlacesMirroredFromTheLeft: x and y run from the
// footprint flush against the left or top edge to the right or bottom, and mirror
// reflects x for a body posted left of the middle, so one route flies either side
func TestAWaypointSpansTheFootprintsPlacesMirroredFromTheLeft(t *testing.T) {
	program := wardenHead("Mark") + `
[states.Mark]
on_enter = [{ action = "Waypoint", payload = { x = 1.0, y = 1.0, mirror = true } }]
on_update = [{ action = "Freeze" }]
`
	ctx, s, left := newProgramWorld(t, program, wardenX, wardenY)
	w := ctx.World
	s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: 60, Y: wardenY}})
	right := w.Components.Behavior.Entities()[1]
	// The warden is four cells by two, its header the first row's second
	width, height := w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
	for header, wantX := range map[core.Entity]int{left: 1, right: width - 3} {
		if b, _ := w.Components.Behavior.GetComponent(header); b.TargetX != wantX || b.TargetY != height-2 {
			t.Errorf("posted at x %d the waypoint stored (%d,%d), want (%d,%d)", b.PostX, b.TargetX, b.TargetY, wantX, height-2)
		}
	}

	doc, err := toml.NewParser([]byte(strings.Replace(program, "x = 1.0", "x = 1.5", 1))).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil); err == nil || !strings.Contains(err.Error(), "not fractions of the map") {
		t.Fatalf("a waypoint past the map compiled: %v", err)
	}
}

// TestAnOrbitCarriesItsBodyRoundItsCellTheWayItNames: from off the circle the body
// settles onto it and laps it in lap_ms, rows read by the cell aspect, clockwise on
// screen or the other way
func TestAnOrbitCarriesItsBodyRoundItsCellTheWayItNames(t *testing.T) {
	for _, clockwise := range []bool{true, false} {
		program := wardenHead("Circle") + `
[states.Circle]
on_enter = [{ action = "Waypoint", payload = { x = 0.5, y = 0.5 } }]
on_update = [{ action = "Orbit", payload = { to = "stored", radius = 10.0, lap_ms = 4000, clockwise = ` + strconv.FormatBool(clockwise) + ` } }]
`
		ctx, s, header := newProgramWorld(t, program, wardenX, wardenY)
		w := ctx.World
		b, _ := w.Components.Behavior.GetPtr(header)
		k, _ := w.Components.Kinetic.GetPtr(header)
		cx, cy := vmath.Point{X: b.TargetX, Y: b.TargetY}.Center()
		polar := func() (float64, float64) {
			dx, dy := k.PreciseX-cx, w.Resources.Config.CellAspect.Circular(k.PreciseY-cy)
			return math.Atan2(dy, dx), vmath.Magnitude(dx, dy)
		}
		for range int(2 * time.Second / parameter.GameUpdateInterval) {
			stepBehaviors(w, s)
		}
		last, _ := polar()
		swept := 0.0
		for range int(4 * time.Second / parameter.GameUpdateInterval) {
			stepBehaviors(w, s)
			angle, d := polar()
			if math.Abs(d-10) > 1 {
				t.Fatalf("clockwise %v: the body runs %.2f cells from the centre, want 10 within one", clockwise, d)
			}
			swept += math.Remainder(angle-last, 2*math.Pi)
			last = angle
		}
		// Rows grow downward, so a clockwise lap sweeps the angle up
		if !clockwise {
			swept = -swept
		}
		if swept < 1.8*math.Pi || swept > 2.2*math.Pi {
			t.Fatalf("clockwise %v: a lap swept %.2f radians, want one turn its way", clockwise, swept)
		}
	}
}

// TestAnArmedProgramFiresItsMountOnlyInItsArmedStates: a program flagging a state
// Armed holds its mount's fire in every other, and one naming none fires throughout
func TestAnArmedProgramFiresItsMountOnlyInItsArmedStates(t *testing.T) {
	for _, flags := range []string{`["Armed"]`, `[]`} {
		program := wardenHead("Fire") + `
[states.Fire]
duration_ms = 500
flags = ` + flags + `
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Timeout", target = "Hold" }]

[states.Hold]
duration_ms = 500
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Timeout", target = "Fire" }]
`
		const fire = component.BehaviorState(0)
		ctx, s, header := newProgramWorld(t, program, wardenX+4, wardenY+10)
		w := ctx.World
		ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
			Payload: &event.CursorSpawnRequestPayload{X: wardenX + 4, Y: wardenY + 10, Control: uint8(component.ControlLocal)}})
		mounts, timers := ready(NewMountSystem(w)).(*MountSystem), ready(NewTimerSystem(w))
		mounts.HandleEvent(event.GameEvent{Type: event.EventMountRequest,
			Payload: &event.MountRequestPayload{Host: header, Weapon: component.WeaponTurret, IntervalMs: 100}})
		w.Resources.Event.Queue.Consume()
		b, _ := w.Components.Behavior.GetPtr(header)

		var shots [2]int // by state
		for range int(3 * time.Second / parameter.GameUpdateInterval) {
			timers.Update()
			stepBehaviors(w, s)
			mounts.Update()
			shots[b.Machine.State] += countEvents(w.Resources.Event.Queue.Consume(), event.EventBulletSpawnRequest)
		}
		armed := flags != `[]`
		if shots[fire] == 0 || armed != (shots[1-fire] == 0) {
			t.Fatalf("flags %s: %d shots in Fire and %d in Hold, want Hold firing only when no state is armed", flags, shots[fire], shots[1-fire])
		}
	}
}

// TestAChanceByMeanWaitRollsEachStepsShare: mean_ms fires once a mean wait whatever
// the tick, where a percent is a roll a step
func TestAChanceByMeanWaitRollsEachStepsShare(t *testing.T) {
	program := wardenHead("Wait") + `
[states.Wait]
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Tick", guard = "Chance", guard_args = { mean_ms = 1000 }, target = "Go" }]

[states.Go]
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Tick", target = "Wait" }]
`
	const went component.BehaviorState = 0
	fired := func(tick time.Duration) int {
		ctx, s, header := newProgramWorld(t, program, wardenX+30, wardenY)
		w := ctx.World
		b, _ := w.Components.Behavior.GetPtr(header)
		n := 0
		for range int(400 * time.Second / tick) {
			w.Resources.Game.State.IncrementGameTicks()
			w.Resources.Time.Update(engine.SimTime(0, tick), engine.SimTime(0, tick), tick)
			s.Update()
			if b.Machine.State == went {
				n++
			}
		}
		w.Resources.Event.Queue.Consume()
		return n
	}
	// 400 s of a one-second mean wait, each firing costing its tick in Go
	for _, tick := range []time.Duration{50 * time.Millisecond, 10 * time.Millisecond} {
		if n := fired(tick); n < 330 || n > 440 {
			t.Errorf("at a %v tick a one-second mean wait fired %d times in 400 s, want about 380 to 400", tick, n)
		}
	}

	for args, want := range map[string]string{
		`{ mean_ms = 1000, percent = 5 }`: "not both",
		`{ mean_ms = -5 }`:                "mean_ms -5 is not positive",
		`{}`:                              "percent must be 1..100",
	} {
		doc, err := toml.NewParser([]byte(strings.Replace(program, `{ mean_ms = 1000 }`, args, 1))).Parse()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Chance %s compiled: %v, want %q", args, err, want)
		}
	}
}

// TestAnInstanceItsOwnLeaveEndsIsNoKill: a body that reaches the edge it flies at
// ends itself through its state's exit, counted as left, and nothing reports a kill
func TestAnInstanceItsOwnLeaveEndsIsNoKill(t *testing.T) {
	program := wardenHead("Fly") + `
[states.Fly]
duration_ms = 400
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [
    { trigger = "Timeout", target = "Fly" },
    { trigger = "AfterTick", guard = "AtEdge", guard_args = { edge = "bottom" }, target = "Gone" },
]

[states.Gone]
duration_ms = 400
on_enter = [{ action = "Telegraph", payload = { color = "white" } }, { action = "Leave", payload = { effect = "fadeout" } }]
on_exit = [{ action = "CancelTelegraph" }]
transitions = [{ trigger = "Timeout", target = "Fly" }]
`
	// The warden is two rows high with its header on the first
	ctx, s, header := newProgramWorld(t, program, wardenX, 34)
	w := ctx.World
	var events []event.GameEvent
	var destroy *event.CompositeDestroyRequestPayload
	for range 20 {
		events = append(events, stepBehaviors(w, s)...)
		if n := len(events); n > 0 && events[n-1].Type == event.EventCompositeDestroyRequest {
			destroy, _ = events[n-1].Payload.(*event.CompositeDestroyRequestPayload)
			break
		}
	}
	if destroy == nil || destroy.HeaderEntity != header || destroy.Effect != component.DeathFadeout {
		t.Fatalf("at the bottom edge the warden asked for %+v, want its composite fading out", destroy)
	}
	if kills, cancels := countEvents(events, event.EventSpeciesKilled), countEvents(events, event.EventSplashTimerCancel); kills != 0 || cancels != 1 {
		t.Fatalf("leaving raised %d kills and %d telegraph cancels, want none and its state's exit", kills, cancels)
	}
	reg := w.Resources.Status.Ints
	if left, count := reg.Get("behavior.warden.left").Load(), reg.Get("behavior.warden.count").Load(); left != 1 || count != 0 {
		t.Fatalf("behavior.warden.left = %d and count = %d, want 1 and 0", left, count)
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
		if m.Domain() != core.DomainShared || c.Species != p.Species || c.Footprint != p.Footprint {
			t.Fatalf("materialize %+v, want a Shared one naming the warden over its footprint %+v", c, p.Footprint)
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
	spawnWall(w, rock.X, rock.Y)
	e.tick()
	headers := w.Components.Behavior.Entities()
	if len(headers) != 2 || w.Components.Materialize.CountEntities() != 0 {
		t.Fatalf("%d wardens landed as their materializes ended, want two", len(headers))
	}
	landed := map[[2]int]bool{}
	for _, header := range headers {
		pos, _ := w.Positions.GetPosition(header)
		if !w.Positions.IsAreaFree(p.Footprint.At(pos.X, pos.Y), component.WallBlockSpawn) {
			t.Fatalf("a warden landed at (%d, %d) on the rock that came over its footprint", pos.X, pos.Y)
		}
		landed[[2]int{pos.X, pos.Y}] = true
	}
	if clear := footprints[0]; !landed[[2]int{clear.X, clear.Y}] {
		t.Fatalf("wardens landed at %v, none on the clear footprint at (%d, %d)", landed, clear.X, clear.Y)
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
	var request *event.MaterializeRequestPayload
	for _, ev := range stepWhile(t, w, s, b, wardenWind) {
		if p, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
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

// TestADataSpeciesMaterializesInItsOwnStyle: a program's style is its own, its
// entrance playing it for the style's time, and a Materialize names it by the program
func TestADataSpeciesMaterializesInItsOwnStyle(t *testing.T) {
	e, _ := newEntranceWorld(t, "materialize = { fill_ms = 100, hold_ms = 50, recede_ms = 50 }")
	enterWarden(e, wardenX, wardenY)
	entrances := e.w.Components.Materialize.Entities()
	if len(entrances) != 1 {
		t.Fatalf("%d materializes, want the entrance's", len(entrances))
	}
	m, _ := e.w.Components.Materialize.GetComponent(entrances[0])
	timer, _ := e.w.Components.Timer.GetPtr(entrances[0])
	if d := timer.Slots[component.TimerDeadline].Duration; m.Type != component.SpawnTypeDataFirst || d != 200*time.Millisecond {
		t.Fatalf("the entrance plays style %d for %v, want the warden's own %d for 200ms", m.Type, d, component.SpawnTypeDataFirst)
	}

	own := strings.Replace(wardenProgram, `payload = { spawn = "swarm" }`, `payload = { spawn = "warden" }`, 1)
	ctx, s, header := newProgramWorld(t, own, wardenX+40, wardenY)
	w := ctx.World
	b, _ := w.Components.Behavior.GetPtr(header)
	stepBehaviors(w, s)
	var request *event.MaterializeRequestPayload
	for _, ev := range stepWhile(t, w, s, b, wardenWind) {
		if p, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
			request = p
		}
	}
	if request == nil || request.Type != component.SpawnTypeDataFirst {
		t.Fatalf("the cue naming the warden requested %+v, want its style %d", request, component.SpawnTypeDataFirst)
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

// TestALandRequestStandsTheDataSpeciesItNames: a personal carrier's landing
// crosses, so a player-stamped request stands the body as a Shared entrance's does,
// its header on the request's cell
func TestALandRequestStandsTheDataSpeciesItNames(t *testing.T) {
	e, _ := newEntranceWorld(t)
	w := e.w
	e.deliver(event.GameEvent{
		Type:    event.EventSpeciesLandRequest,
		Domain:  core.DomainPlayer,
		Payload: &event.SpeciesLandRequestPayload{Species: w.Resources.Behaviors[0].Species, X: wardenX, Y: wardenY},
	})
	headers := w.Components.Behavior.Entities()
	if len(headers) != 1 {
		t.Fatalf("%d wardens stood, want one", len(headers))
	}
	if pos, _ := w.Positions.GetPosition(headers[0]); pos.X != wardenX || pos.Y != wardenY {
		t.Fatalf("the warden stands at (%d, %d), want (%d, %d)", pos.X, pos.Y, wardenX, wardenY)
	}
}

// TestAnEdgeRequestStandsOnThatEdgeBetweenItsInsets: a request naming an edge
// stands on that edge, and along it counts from the inset and wraps over the cells
// the insets leave, an inset past the middle keeping the middle
func TestAnEdgeRequestStandsOnThatEdgeBetweenItsInsets(t *testing.T) {
	ctx, s, first := newWardenWorld(t, wardenX+4, wardenY)
	w := ctx.World
	width, height := w.Positions.Width, w.Positions.Height
	seen := map[core.Entity]bool{first: true}
	// The warden is 4x2 with its header on the top row, one column in
	for _, c := range []struct {
		req  event.BehaviorSpawnRequestPayload
		x, y int
	}{
		{event.BehaviorSpawnRequestPayload{Edge: event.MapEdgeRight, Inset: 2, Y: height - 4 + 3}, width - 3, 5},
		{event.BehaviorSpawnRequestPayload{Edge: event.MapEdgeLeft, Inset: 2, Y: -1}, 1, height - 3},
		{event.BehaviorSpawnRequestPayload{Edge: event.MapEdgeBottom, X: 30}, 30, height - 2},
		{event.BehaviorSpawnRequestPayload{Edge: event.MapEdgeTop, Inset: width}, (width - 1) / 2, 0},
	} {
		c.req.Program = "warden"
		s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest, Payload: &c.req})
		var stood []core.Entity
		for _, header := range w.Components.Behavior.Entities() {
			if !seen[header] {
				seen[header] = true
				stood = append(stood, header)
			}
		}
		if len(stood) != 1 {
			t.Fatalf("%+v stood %d wardens, want one", c.req, len(stood))
		}
		if pos, _ := w.Positions.GetPosition(stood[0]); pos.X != c.x || pos.Y != c.y {
			t.Fatalf("%+v on a %dx%d map stands the warden at (%d, %d), want (%d, %d)",
				c.req, width, height, pos.X, pos.Y, c.x, c.y)
		}
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

// TestADataSpeciesTakesEachAttackByItsOwnRow: combat resolves a hit on a data
// species by the row its prototype builds, a family it names taking its keys and
// the rest its combat row's profile
func TestADataSpeciesTakesEachAttackByItsOwnRow(t *testing.T) {
	program := strings.Replace(wardenProgram, "mass = 2.0", "mass = 2.0\nattacks = { pulse = { damage = 0, stun = false } }", 1)
	ctx, _, header := newProgramWorld(t, program, wardenX+4, wardenY)
	w := ctx.World
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	c, _ := w.Components.Combat.GetPtr(header)
	if pulse := combat.attack(component.CombatAttackPulse, component.CombatEntityCursor, header, c); pulse == nil ||
		pulse.DamageValue != 0 || pulse.EffectMask&component.CombatEffectStun != 0 {
		t.Fatalf("a pulse lands %+v, want no damage and no stun", pulse)
	}
	swarm := profile.Attack(component.CombatAttackTracer, component.CombatEntityCursor, component.CombatEntitySwarm)
	if tracer := combat.attack(component.CombatAttackTracer, component.CombatEntityCursor, header, c); tracer == nil || *tracer != *swarm {
		t.Fatalf("a tracer lands %+v, want the swarm row's %+v", tracer, swarm)
	}
}

// TestASpawnSpeciesLandsAnySharedSpecies: the verb asks the species it names, a
// program's among them, to land at the header plus its offset, and a species that is
// not Shared fails the compile
func TestASpawnSpeciesLandsAnySharedSpecies(t *testing.T) {
	const enter = `on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]`
	spawning := func(species string) string {
		return strings.Replace(wardenProgram, enter, strings.TrimSuffix(enter, "]")+
			`, { action = "SpawnSpecies", payload = { species = "`+species+`", x = 2, y = 1 } }]`, 1)
	}
	ctx, s, header := newProgramWorld(t, spawning("warden"), wardenX+40, wardenY)
	var landing *event.SpeciesLandRequestPayload
	for _, ev := range stepBehaviors(ctx.World, s) {
		if p, ok := ev.Payload.(*event.SpeciesLandRequestPayload); ok {
			landing = p
		}
	}
	pos, _ := ctx.World.Positions.GetPosition(header)
	want := event.SpeciesLandRequestPayload{Species: component.SpeciesDataFirst, X: pos.X + 2, Y: pos.Y + 1}
	if landing == nil || *landing != want {
		t.Fatalf("entering Wind requested %+v, want %+v", landing, want)
	}

	for _, species := range []string{"drain", "none"} {
		doc, err := toml.NewParser([]byte(spawning(species))).Parse()
		if err != nil {
			t.Fatal(err)
		}
		_, err = CompileBehaviors(map[string]map[string]any{"warden": doc}, nil)
		if err == nil || !strings.Contains(err.Error(), "is not a Shared species") {
			t.Fatalf("a SpawnSpecies naming %s compiled: %v", species, err)
		}
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

// TestASpawnMountsItsPrototypesWeapon: [prototype.mount] is the mount request each
// spawn makes for its own header, and each host's interval is drawn inside its spread
func TestASpawnMountsItsPrototypesWeapon(t *testing.T) {
	const mount = "[prototype.mount]\nweapon = \"turret\"\ninterval_ms = 3000\nspread_ms = 600\nrange = 1000\nenergy = 300\n\n"
	armed := func(mount string) string {
		return strings.Replace(wardenProgram, "[states.Roam]", mount+"[states.Roam]", 1)
	}
	ctx, s, _ := newProgramWorld(t, armed(mount), wardenX+4, wardenY)
	w := ctx.World
	mounts := ready(NewMountSystem(w)).(*MountSystem)
	intervals := map[time.Duration]bool{}
	for i := range 4 {
		s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest,
			Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: 10 + 12*i, Y: 25}})
		var host core.Entity
		for _, ev := range slices.Clone(w.Resources.Event.Queue.Consume()) {
			if p, ok := ev.Payload.(*event.MountRequestPayload); ok {
				host = p.Host
				mounts.HandleEvent(ev)
			}
		}
		m, ok := w.Components.Mount.GetComponent(host)
		if !ok || !w.Components.Behavior.HasEntity(host) || m.Weapon != component.WeaponTurret || m.Damage.EnergyDrain != 300 {
			t.Fatalf("spawn %d mounted %+v on %d, want its header's turret", i, m, host)
		}
		if m.Interval < 2400*time.Millisecond || m.Interval > 3600*time.Millisecond {
			t.Fatalf("spawn %d fires every %v, want within 600 ms of 3 s", i, m.Interval)
		}
		intervals[m.Interval] = true
	}
	if len(intervals) < 3 {
		t.Fatalf("four hosts drew %d intervals, want each its own", len(intervals))
	}
	bare, plain, _ := newProgramWorld(t, armed("[prototype.mount]\n\n"), wardenX+4, wardenY)
	plain.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest,
		Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: 40, Y: 25}})
	if n := countEvents(bare.World.Resources.Event.Queue.Consume(), event.EventMountRequest); n != 0 {
		t.Fatalf("an empty mount table asked for %d mounts, want a bare body", n)
	}

	for table, want := range map[string]string{
		"[prototype.mount]\nweapon = \"turret\"\nhost = 3\n\n":                           "host is the body",
		"[prototype.mount]\nrange = 10\n\n":                                              "weapon is required",
		"[prototype.mount]\nweapon = \"cannon\"\n\n":                                     `weapon: "cannon" is not one of`,
		"[prototype.mount]\nweapon = \"turret\"\ninterval_ms = 500\nspread_ms = 500\n\n": "spread_ms 500 is not under its interval",
	} {
		doc, err := toml.NewParser([]byte(armed(table))).Parse()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q compiled: %v, want %q", table, err, want)
		}
	}
}

// TestAProgramReadsWhatItsScenarioDecides: a Status guard reads a row every instance
// holds alike, a Var guard a variable of the scenario machine's, and each state's
// row counts the instances standing in it
func TestAProgramReadsWhatItsScenarioDecides(t *testing.T) {
	program := wardenHead("Hold") + `
[states.Hold]
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Tick", target = "Dive", guard = "And", guard_args = { guards = [
    { name = "Status", args = { key = "behavior.warden.count", op = "lte", value = 2 } },
    { name = "Var", args = { name = "wave", op = "gte", value = 3 } },
] } }]

[states.Dive]
on_update = [{ action = "Freeze" }]
`
	ctx, s, _ := newProgramWorld(t, program, wardenX+30, wardenY)
	w := ctx.World
	wave := int64(0)
	w.Resources.Scenario.Bind(func(name string) int64 {
		if name != "wave" {
			t.Errorf("the guard read variable %q", name)
		}
		return wave
	}, nil)
	spawn := func(x int) {
		s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest,
			Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: x, Y: 25}})
	}
	spawn(10)
	spawn(30)
	reg := w.Resources.Status.Ints
	standing := func() [2]int64 {
		stepBehaviors(w, s)
		return [2]int64{reg.Get("behavior.warden.state.Dive").Load(), reg.Get("behavior.warden.state.Hold").Load()}
	}
	wave = 3
	if got := standing(); got != [2]int64{0, 3} {
		t.Fatalf("three wardens at wave 3 stand %v in Dive and Hold, want all holding past a count of 2", got)
	}
	for _, header := range w.Components.Behavior.Entities()[:1] {
		w.Components.Behavior.RemoveEntity(header)
	}
	wave = 2
	if got := standing(); got != [2]int64{0, 2} {
		t.Fatalf("two wardens at wave 2 stand %v, want both holding under wave 3", got)
	}
	wave = 3
	if got := standing(); got != [2]int64{2, 0} {
		t.Fatalf("two wardens at wave 3 stand %v, want both diving", got)
	}

	local := strings.Replace(program, "behavior.warden.count", "kills.warden", 1)
	doc, err := toml.NewParser([]byte(local)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, nil); err == nil || !strings.Contains(err.Error(), `key "kills.warden" is one instance's own`) {
		t.Fatalf("a guard on a row instances do not share compiled: %v", err)
	}
}

// TestASignalIsRaisedOnAsManyAsItCountsOfThoseItsEdgeTakes: a request raises its
// signal on count of the instances whose state takes it, the same ones whatever
// order the store holds them in, and a payload names program and signal at load
func TestASignalIsRaisedOnAsManyAsItCountsOfThoseItsEdgeTakes(t *testing.T) {
	program := strings.Replace(wardenHead("Hold"), `initial = "Hold"`, "initial = \"Hold\"\nsignals = [\"dive\"]", 1) + `
[states.Hold]
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Tick", guard = "Signalled", guard_args = { name = "dive" }, target = "Dive" }]

[states.Dive]
duration_ms = 400
on_update = [{ action = "Freeze" }]
transitions = [{ trigger = "Timeout", target = "Hold" }]
`
	const dive component.BehaviorState = 0
	diving := func(reorder bool) []core.Entity {
		ctx, s, _ := newProgramWorld(t, program, wardenX+30, wardenY)
		w := ctx.World
		for i := range 5 {
			s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSpawnRequest,
				Payload: &event.BehaviorSpawnRequestPayload{Program: "warden", X: 8 + 12*i, Y: 25}})
		}
		w.Resources.Event.Queue.Consume()
		headers := slices.Clone(w.Components.Behavior.Entities())
		slices.Sort(headers)
		if reorder { // another store order, as an instance a correction rebuilt may hold
			for _, header := range headers[1:4] {
				b, _ := w.Components.Behavior.GetComponent(header)
				w.Components.Behavior.RemoveEntity(header)
				w.Components.Behavior.SetComponent(header, b)
			}
		}
		busy, _ := w.Components.Behavior.GetPtr(headers[0])
		busy.Machine.State, busy.Machine.Duration = dive, time.Hour

		names := event.PayloadNames(s.set.Declared())
		payload, err := event.AuthoredPayload(names, event.EventBehaviorSignalRequest,
			map[string]any{"program": "warden", "signal": "dive", "count": int64(2)})
		if err != nil {
			t.Fatal(err)
		}
		s.HandleEvent(event.GameEvent{Type: event.EventBehaviorSignalRequest, Payload: payload})
		if busy.Signals != 0 {
			t.Fatal("a warden already diving, whose state takes no dive, was signalled")
		}
		stepBehaviors(w, s)
		var out []core.Entity
		for _, header := range headers[1:] {
			if b, _ := w.Components.Behavior.GetComponent(header); b.Machine.State == dive {
				if b.Signals != 0 {
					t.Fatalf("warden %d dived still holding its signal", header)
				}
				out = append(out, header)
			}
		}
		for _, bad := range []map[string]any{{"program": "warden", "signal": "retreat"}, {"program": "swarm", "signal": "dive"}} {
			if _, err := event.AuthoredPayload(names, event.EventBehaviorSignalRequest, bad); err == nil {
				t.Fatalf("a request for %v decoded", bad)
			}
		}
		return out
	}
	first, second := diving(false), diving(true)
	if len(first) != 2 || !slices.Equal(first, second) {
		t.Fatalf("a signal for two of five holding wardens sent %v, and %v in another store order, want the same two", first, second)
	}
}

// TestALaidMapStandsItsFormationFromItsCell: a lay request names a map of the
// scenario's at load, and each instance its legend names stands where its row and
// column put it from the request's cell
func TestALaidMapStandsItsFormationFromItsCell(t *testing.T) {
	doc, err := toml.NewParser([]byte(wardenProgram)).Parse()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := toml.NewParser([]byte("[maps.rank]\nlegend = { w = { program = \"warden\" } }\nrows = [\"w      w\", \"        \", \"    w   \"]\n")).Parse()
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileBehaviors(map[string]map[string]any{"warden": doc}, entry)
	if err != nil {
		t.Fatal(err)
	}
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	w.Resources.Behaviors, w.Resources.Maps = set.Prototypes(), set.Maps()
	w.Resources.Target = &engine.TargetResource{}
	s := ready(NewBehaviorSystem(w, set)).(*BehaviorSystem)

	names := event.PayloadNames(set.Declared())
	payload, err := event.AuthoredPayload(names, event.EventMapLayRequest, map[string]any{"map": "rank", "x": int64(20), "y": int64(10)})
	if err != nil {
		t.Fatal(err)
	}
	s.HandleEvent(event.GameEvent{Type: event.EventMapLayRequest, Payload: payload})
	var stood []component.PositionComponent
	for _, header := range w.Components.Behavior.Entities() {
		pos, _ := w.Positions.GetPosition(header)
		stood = append(stood, pos)
	}
	want := []component.PositionComponent{{X: 20, Y: 10}, {X: 27, Y: 10}, {X: 24, Y: 12}}
	if !slices.Equal(stood, want) {
		t.Fatalf("the rank stands at %v, want %v", stood, want)
	}
	if _, err := event.AuthoredPayload(names, event.EventMapLayRequest, map[string]any{"map": "wave"}); err == nil ||
		!strings.Contains(err.Error(), `map: "wave" is not one of rank`) {
		t.Fatalf("a request for a map the scenario lacks decoded: %v", err)
	}
}
