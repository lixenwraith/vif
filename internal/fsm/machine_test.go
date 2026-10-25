package fsm_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
)

func loadMachine(t *testing.T, doc string) (*fsm.Machine[*eventTrace], *eventTrace, error) {
	t.Helper()
	event.EnsureRegistry()
	trace := new(eventTrace)
	m := fsm.NewMachine[*eventTrace]()
	std.Register(m, std.Host[*eventTrace]{
		Emit: func(dst *eventTrace, et event.EventType, _ any) { *dst = append(*dst, et) },
	})
	return m, trace, m.LoadScenario([]byte(doc))
}

func TestLoadRefusesParentCycle(t *testing.T) {
	for name, doc := range map[string]string{
		"pair": "[regions.r]\ninitial = \"A\"\n[states.A]\nparent = \"B\"\n[states.B]\nparent = \"A\"\n",
		"self": "[regions.r]\ninitial = \"A\"\n[states.A]\nparent = \"A\"\n",
	} {
		_, _, err := loadMachine(t, doc)
		if err == nil || !strings.Contains(err.Error(), "'A'") {
			t.Fatalf("%s: load error = %v, want a parent cycle naming A", name, err)
		}
	}
}

func TestLoadFailsWithoutStates(t *testing.T) {
	_, _, err := loadMachine(t, "[regions.r]\ninitial = \"A\"\n")
	if err == nil || !strings.Contains(err.Error(), "'A'") {
		t.Fatalf("load error = %v, want unknown initial state A", err)
	}
}

func TestOnExitTerminatesOwnRegion(t *testing.T) {
	const doc = `
[regions.r]
initial = "A"
[states.A]
on_exit = [{ action = "TerminateRegion", region = "r" }]
transitions = [{ trigger = "Tick", target = "B" }]
[states.B]
on_enter = [{ action = "EmitEvent", event = "EventStrobeRequest" }]
`
	m, trace, err := loadMachine(t, doc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	m.Update(trace, time.Millisecond)
	if m.HasRegion("r") || len(*trace) != 0 {
		t.Fatalf("after exit: region r = %v, events = %v; want gone and no enter", m.HasRegion("r"), *trace)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	if err := m.Reset(trace); err != nil || m.GetRegionState("r") != "A" {
		t.Fatalf("reset: err = %v, state = %q, want A", err, m.GetRegionState("r"))
	}
}

func TestLoadRefusesOnlySystemsBesideDisabled(t *testing.T) {
	for where, doc := range map[string]string{
		"[systems]":    "[systems]\nonly_systems = [\"a\"]\ndisabled_systems = [\"b\"]\n[regions.r]\ninitial = \"A\"\n[states.A]\n",
		"region \"r\"": "[regions.r]\ninitial = \"A\"\nonly_systems = [\"a\"]\ndisabled_systems = [\"b\"]\n[states.A]\n",
	} {
		if _, _, err := loadMachine(t, doc); err == nil || !strings.Contains(err.Error(), where) {
			t.Errorf("%s: load error = %v, want only_systems beside disabled_systems refused", where, err)
		}
	}
}

// TestRunningRegionsDecideTheSystems: the union of the running regions'
// only_systems lists stands in for the baseline, their enables add, their
// disables take away, and a paused region decides nothing
func TestRunningRegionsDecideTheSystems(t *testing.T) {
	const doc = `
[systems]
disabled_systems = ["gold"]
[regions.main]
initial = "A"
enabled_systems = ["glyph"]
[regions.puzzle]
only_systems = ["typing"]
[regions.calm]
only_systems = ["timer"]
[regions.quiet]
disabled_systems = ["typing"]
[states.A]
`
	m, trace, err := loadMachine(t, doc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	a, _ := m.GetStateID("A")
	expect := func(when string, wanted, decided map[string]bool) {
		t.Helper()
		for name, want := range wanted {
			if got := m.SystemWanted(name); got != want {
				t.Errorf("%s: %s wanted = %v, want %v", when, name, got, want)
			}
		}
		for name, want := range decided {
			if got := m.SystemDecided(name); got != want {
				t.Errorf("%s: %s decided = %v, want %v", when, name, got, want)
			}
		}
	}

	expect("baseline", map[string]bool{"gold": false, "particle": true, "glyph": true},
		map[string]bool{"glyph": true, "particle": false, "gold": false})
	if err := m.SpawnRegion(trace, "puzzle", a); err != nil {
		t.Fatal(err)
	}
	expect("narrowed", map[string]bool{"typing": true, "particle": false, "glyph": true, "timer": false},
		map[string]bool{"particle": true})
	if err := m.SpawnRegion(trace, "calm", a); err != nil {
		t.Fatal(err)
	}
	expect("two narrowed", map[string]bool{"typing": true, "timer": true, "particle": false}, nil)
	m.PauseRegion("puzzle")
	expect("one paused", map[string]bool{"typing": false, "timer": true}, nil)
	if err := m.SpawnRegion(trace, "quiet", a); err != nil {
		t.Fatal(err)
	}
	m.ResumeRegion("puzzle")
	expect("disabled beside", map[string]bool{"typing": false, "timer": true}, map[string]bool{"typing": true})
	for _, r := range []string{"calm", "puzzle", "quiet"} {
		if err := m.TerminateRegion(trace, r); err != nil {
			t.Fatal(err)
		}
	}
	expect("handed back", map[string]bool{"gold": false, "particle": true, "typing": true, "timer": true},
		map[string]bool{"particle": false, "typing": false})
}

const proposedDoc = `
[regions.r]
initial = "A"
[states.A]
transitions = [
    { trigger = "EventSpeciesCreated", target = "B", guard = "PayloadIntCompare", guard_args = { field = "species", op = "eq", value = 4 }, capture_vars = { species = "made" } },
    { trigger = "EventSpeciesKilled", internal = true, actions = [{ action = "IncrementVar", payload = { name = "dead", delta = 1 } }] },
    { trigger = "Tick", target = "C", guard = "StateTimeExceeds", guard_args = { ms = 10 } },
]
[states.B]
on_enter = [{ action = "EmitEvent", event = "EventStrobeRequest" }]
transitions = [{ trigger = "EventSpeciesKilled", target = "C" }]
[states.C]
`

// TestAProposedTransitionWaitsForItsApply: a proposing machine hands a
// passed transition over with the fields it captured and moves only when Apply
// brings it back; in between it parks the events the state it is entering
// reacts to and replays them there, while an internal transition parks nothing,
// so every event it counts is proposed; an apply whose owning state the region
// has left is refused.
func TestAProposedTransitionWaitsForItsApply(t *testing.T) {
	m, trace, err := loadMachine(t, proposedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var proposed []*event.FSMTransitionPayload
	m.Propose = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar, then []event.FSMHop) {
		proposed = append(proposed, &event.FSMTransitionPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars, Then: then})
	}
	m.Steer(false, true)
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	killed := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{}}
	made := event.GameEvent{Type: event.EventSpeciesCreated, Payload: &event.SpeciesCreatedPayload{Species: 4}}
	if !m.HandleEvent(trace, killed) || !m.HandleEvent(trace, killed) || len(proposed) != 2 || m.GetVar("dead") != 0 {
		t.Fatalf("after two internal events: %d proposals, dead %d; want both proposed and nothing counted", len(proposed), m.GetVar("dead"))
	}
	if !m.HandleEvent(trace, made) || len(proposed) != 3 || m.GetRegionState("r") != "A" || len(*trace) != 0 {
		t.Fatalf("after the event: proposed %d, state %q, events %v; want a third proposal, A and no enter", len(proposed), m.GetRegionState("r"), *trace)
	}
	a, _ := m.GetStateID("A")
	p := proposed[2]
	if p.Owner != int(a) || p.Index != 0 || p.Trigger != event.EventSpeciesCreated || len(p.Vars) != 1 || p.Vars[0] != (event.FSMVar{Name: "made", Value: 4}) {
		t.Fatalf("proposal = %+v, want A's first transition on EventSpeciesCreated capturing made=4", *p)
	}
	m.Update(trace, 20*time.Millisecond)
	if !m.HandleEvent(trace, killed) || len(proposed) != 3 || m.GetRegionState("r") != "A" {
		t.Fatalf("while proposed: %d proposals, state %q; want the region parked in A holding the kill B reacts to", len(proposed), m.GetRegionState("r"))
	}
	for _, q := range proposed[:2] {
		if err := m.Apply(trace, q); err != nil {
			t.Fatalf("apply internal: %v", err)
		}
	}
	if m.GetVar("dead") != 2 || m.GetRegionState("r") != "A" || len(proposed) != 3 {
		t.Fatalf("after the internal applies: dead %d, state %q, %d proposals; want 2 counted in A, still parked", m.GetVar("dead"), m.GetRegionState("r"), len(proposed))
	}
	if err := m.Apply(trace, p); err != nil || m.GetRegionState("r") != "B" || m.GetVar("made") != 4 || len(*trace) != 1 {
		t.Fatalf("apply: err %v, state %q, made %d, events %v; want B entered with made=4", err, m.GetRegionState("r"), m.GetVar("made"), *trace)
	}
	if len(proposed) != 4 || proposed[3].Trigger != event.EventSpeciesKilled {
		t.Fatalf("%d proposals after entering B; want the parked kill replayed there and proposed", len(proposed))
	}
	if err := m.Apply(trace, proposed[3]); err != nil || m.GetRegionState("r") != "C" {
		t.Fatalf("apply the replayed kill: err %v, state %q, want C", err, m.GetRegionState("r"))
	}
	if err := m.Apply(trace, p); err == nil {
		t.Fatal("a transition of a state the region has left was applied")
	}
}

// TestAFollowingMachineMovesByApplyAlone: a guest's machine evaluates no guard,
// so neither an event nor time moves it, and Apply alone does.
func TestAFollowingMachineMovesByApplyAlone(t *testing.T) {
	m, trace, err := loadMachine(t, proposedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	m.Steer(true, false)
	made := event.GameEvent{Type: event.EventSpeciesCreated, Payload: &event.SpeciesCreatedPayload{Species: 4}}
	m.Update(trace, 20*time.Millisecond)
	if m.HandleEvent(trace, made) || m.GetRegionState("r") != "A" {
		t.Fatalf("following: state %q after an event and 20ms, want A untouched", m.GetRegionState("r"))
	}
	a, _ := m.GetStateID("A")
	if err := m.Apply(trace, &event.FSMTransitionPayload{Region: "r", Owner: int(a), Index: 2}); err != nil || m.GetRegionState("r") != "C" {
		t.Fatalf("apply: err %v, state %q, want C", err, m.GetRegionState("r"))
	}
	m.Steer(false, false)
	m.Update(trace, 20*time.Millisecond)
	if m.GetRegionState("r") != "C" {
		t.Fatalf("state %q after following ended, want C: C has no transition", m.GetRegionState("r"))
	}
}

const foldedDoc = `
[regions.r]
initial = "A"
[states.A]
transitions = [{ trigger = "EventSpeciesCreated", target = "B" }]
[states.B]
on_enter = [{ action = "EmitEvent", event = "EventStrobeRequest" }]
transitions = [{ trigger = "Tick", target = "C" }]
[states.C]
on_enter = [{ action = "EmitEvent", event = "EventDrainPause" }]
transitions = [{ trigger = "Tick", target = "D" }]
[states.D]
on_enter = [{ action = "EmitEvent", event = "EventDrainResume" }]
transitions = [{ trigger = "Tick", target = "E", guard = "StateTimeExceeds", guard_args = { ms = 10 } }]
[states.E]
`

// TestAProposedTransitionFoldsTheUnguardedHopsAfterIt: the authority carries the
// unguarded Tick hops after a transition's target in the crossing it proposes, up to
// the first guarded one, and Apply takes the whole chain at once, each state's
// on_enter in order, on the authority's machine and a following one alike; a hop
// naming a guarded transition is refused.
func TestAProposedTransitionFoldsTheUnguardedHopsAfterIt(t *testing.T) {
	m, trace, err := loadMachine(t, foldedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var proposed []*event.FSMTransitionPayload
	m.Propose = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar, then []event.FSMHop) {
		proposed = append(proposed, &event.FSMTransitionPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars, Then: then})
	}
	m.Steer(false, true)
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	made := event.GameEvent{Type: event.EventSpeciesCreated, Payload: &event.SpeciesCreatedPayload{Species: 4}}
	if !m.HandleEvent(trace, made) || len(proposed) != 1 {
		t.Fatalf("after the event: %d proposals, want one", len(proposed))
	}
	b, _ := m.GetStateID("B")
	c, _ := m.GetStateID("C")
	p := proposed[0]
	if want := []event.FSMHop{{Owner: int(b), Index: 0}, {Owner: int(c), Index: 0}}; !slices.Equal(p.Then, want) {
		t.Fatalf("the crossing carries hops %v, want B's and C's unguarded Ticks, %v", p.Then, want)
	}
	m.Update(trace, 20*time.Millisecond)
	if m.GetRegionState("r") != "A" || len(*trace) != 0 {
		t.Fatalf("while proposed the region moved to %q with %v", m.GetRegionState("r"), *trace)
	}
	if err := m.Apply(trace, p); err != nil || m.GetRegionState("r") != "D" {
		t.Fatalf("apply: err %v, state %q, want D", err, m.GetRegionState("r"))
	}
	if want := []event.EventType{event.EventStrobeRequest, event.EventDrainPause, event.EventDrainResume}; !slices.Equal(*trace, want) {
		t.Fatalf("the chain ran %v, want each state's on_enter in order %v", *trace, want)
	}
	m.Update(trace, 20*time.Millisecond)
	if len(proposed) != 2 || proposed[1].Then != nil || m.GetRegionState("r") != "D" {
		t.Fatalf("after D's guard passed: %d proposals, last hops %v, state %q; want D's guarded Tick proposed alone", len(proposed), proposed[len(proposed)-1].Then, m.GetRegionState("r"))
	}

	follower, trace2, err := loadMachine(t, foldedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := follower.Init(trace2); err != nil {
		t.Fatalf("init: %v", err)
	}
	follower.Steer(true, false)
	if err := follower.Apply(trace2, p); err != nil || follower.GetRegionState("r") != "D" || !slices.Equal(*trace2, *trace) {
		t.Fatalf("a follower applying the chain: err %v, state %q, ran %v; want D by the same path", err, follower.GetRegionState("r"), *trace2)
	}
	d, _ := follower.GetStateID("D")
	bad := &event.FSMTransitionPayload{Region: "r", Owner: p.Owner, Index: p.Index, Then: []event.FSMHop{{Owner: int(b), Index: 0}, {Owner: int(d), Index: 0}}}
	fresh, trace3, _ := loadMachine(t, foldedDoc)
	_ = fresh.Init(trace3)
	fresh.Steer(true, false)
	if err := fresh.Apply(trace3, bad); err == nil {
		t.Fatal("a hop naming a guarded transition was applied")
	}
}
