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

// TestAProposedTransitionWaitsForItsApply: a proposing machine hands a passed
// transition over with the fields it captured and moves only when Apply brings it
// back; in between it parks the events the state it is entering reacts to and
// replays them there, while an internal transition parks nothing, so every event
// it counts is proposed; an apply whose owning state the region has left is refused
func TestAProposedTransitionWaitsForItsApply(t *testing.T) {
	m, trace, err := loadMachine(t, proposedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var proposed []*event.FSMTransitionPayload
	m.Propose = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar, then []event.FSMHop) {
		proposed = append(proposed, &event.FSMTransitionPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars, Then: then})
	}
	m.Steer(false, true, false)
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

const tickedDoc = `
[regions.r]
initial = "A"
[states.A]
on_update = [{ action = "IncrementVar", payload = { name = "beat" } }]
transitions = [
    { trigger = "Tick", target = "B", guard = "VarCompare", guard_args = { var = "notes", op = "gte", value = 2 } },
    { trigger = "Tick", internal = true, guard = "VarCompare", guard_args = { var = "beat", op = "gte", value = 1 }, actions = [
        { action = "SetVar", payload = { name = "beat", value = 0 } },
        { action = "IncrementVar", payload = { name = "notes" } },
    ] },
    { trigger = "EventSpeciesKilled", internal = true, actions = [{ action = "IncrementVar", payload = { name = "dead" } }] },
]
[states.B]
`

// TestAnInternalTickTransitionHoldsTheRegionsTicksUntilItLands: an internal Tick
// transition's guard, which its own actions turn false, passes until it lands, so a
// proposing machine hands it over once and takes no Tick transition meanwhile; an
// event still crosses, and only the Tick one's apply ends the hold.
func TestAnInternalTickTransitionHoldsTheRegionsTicksUntilItLands(t *testing.T) {
	m, trace, err := loadMachine(t, tickedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var proposed []*event.FSMTransitionPayload
	m.Propose = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar, then []event.FSMHop) {
		proposed = append(proposed, &event.FSMTransitionPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars, Then: then})
	}
	m.Steer(false, true, false)
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	for range 3 {
		m.Update(trace, time.Millisecond)
	}
	killed := event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{}}
	if !m.HandleEvent(trace, killed) || len(proposed) != 2 || proposed[0].Index != 1 || proposed[1].Index != 2 {
		t.Fatalf("after three ticks and a kill: %d proposals; want the note once, then the kill", len(proposed))
	}
	if err := m.Apply(trace, proposed[0]); err != nil {
		t.Fatalf("apply the note: %v", err)
	}
	m.Update(trace, time.Millisecond)
	if len(proposed) != 3 || proposed[2].Index != 1 {
		t.Fatalf("after the note landed: %d proposals; want the next note", len(proposed))
	}
	if err := m.Apply(trace, proposed[1]); err != nil {
		t.Fatalf("apply the kill: %v", err)
	}
	m.Update(trace, time.Millisecond)
	if len(proposed) != 3 || m.GetVar("dead") != 1 {
		t.Fatalf("after the kill landed: %d proposals, dead %d; want the second note still holding", len(proposed), m.GetVar("dead"))
	}
	if err := m.Apply(trace, proposed[2]); err != nil {
		t.Fatalf("apply the second note: %v", err)
	}
	m.Update(trace, time.Millisecond)
	if len(proposed) != 4 || proposed[3].Index != 0 || m.GetVar("notes") != 2 {
		t.Fatalf("after two notes: %d proposals, notes %d; want B's transition proposed", len(proposed), m.GetVar("notes"))
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
	m.Steer(true, false, false)
	made := event.GameEvent{Type: event.EventSpeciesCreated, Payload: &event.SpeciesCreatedPayload{Species: 4}}
	m.Update(trace, 20*time.Millisecond)
	if m.HandleEvent(trace, made) || m.GetRegionState("r") != "A" {
		t.Fatalf("following: state %q after an event and 20ms, want A untouched", m.GetRegionState("r"))
	}
	a, _ := m.GetStateID("A")
	if err := m.Apply(trace, &event.FSMTransitionPayload{Region: "r", Owner: int(a), Index: 2}); err != nil || m.GetRegionState("r") != "C" {
		t.Fatalf("apply: err %v, state %q, want C", err, m.GetRegionState("r"))
	}
	m.Steer(false, false, false)
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
	m.Steer(false, true, false)
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
	follower.Steer(true, false, false)
	if err := follower.Apply(trace2, p); err != nil || follower.GetRegionState("r") != "D" || !slices.Equal(*trace2, *trace) {
		t.Fatalf("a follower applying the chain: err %v, state %q, ran %v; want D by the same path", err, follower.GetRegionState("r"), *trace2)
	}
	d, _ := follower.GetStateID("D")
	bad := &event.FSMTransitionPayload{Region: "r", Owner: p.Owner, Index: p.Index, Then: []event.FSMHop{{Owner: int(b), Index: 0}, {Owner: int(d), Index: 0}}}
	fresh, trace3, _ := loadMachine(t, foldedDoc)
	_ = fresh.Init(trace3)
	fresh.Steer(true, false, false)
	if err := fresh.Apply(trace3, bad); err == nil {
		t.Fatal("a hop naming a guarded transition was applied")
	}
}

const askedDoc = `
[regions.r]
initial = "A"
[states.A]
transitions = [
    { trigger = "EventCharacterTyped", target = "B", guard = "PayloadIntCompare", guard_args = { field = "char", op = "eq", value = 120 }, capture_vars = { x = "col" } },
    { trigger = "EventEnergyGlyphConsumed", internal = true, actions = [{ action = "IncrementVar", payload = { name = "eaten" } }] },
    { trigger = "EventSpeciesCreated", target = "C" },
]
[states.B]
on_enter = [{ action = "EmitEvent", event = "EventStrobeRequest" }]
transitions = [
    { trigger = "EventModeChanged", target = "C" },
    { trigger = "EventSpeciesKilled", target = "C" },
    { trigger = "Tick", target = "C", guard = "StateTimeExceeds", guard_args = { ms = 600000 } },
]
[states.C]
`

// TestAFollowerAsksForWhatALocalEventTriggers: a guest's machine judges the guard of
// a transition a local event triggers, where the event is, and asks for it with what
// it captured, moving only by Apply; a shared event's is the authority's to judge.
// It waits as a proposing one does, and gives an unanswered ask up.
func TestAFollowerAsksForWhatALocalEventTriggers(t *testing.T) {
	m, trace, err := loadMachine(t, askedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var asked []*event.FSMTransitionRequestPayload
	m.Ask = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar) {
		asked = append(asked, &event.FSMTransitionRequestPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars})
	}
	m.AskUpdates = 3
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	m.Steer(true, false, true)
	typed := func(char rune) event.GameEvent {
		return event.GameEvent{Type: event.EventCharacterTyped, Payload: &event.CharacterTypedPayload{Char: char, X: 7}}
	}
	mode := event.GameEvent{Type: event.EventModeChanged, Payload: &event.ModeChangedPayload{}}
	a, _ := m.GetStateID("A")
	b, _ := m.GetStateID("B")

	if m.HandleEvent(trace, event.GameEvent{Type: event.EventSpeciesCreated, Payload: &event.SpeciesCreatedPayload{}}) ||
		m.HandleEvent(trace, typed('y')) || len(asked) != 0 {
		t.Fatalf("a shared event and a local one its guard refuses asked %d transition(s), want none", len(asked))
	}
	if !m.HandleEvent(trace, event.GameEvent{Type: event.EventEnergyGlyphConsumed, Payload: &event.EnergyGlyphConsumedPayload{}}) ||
		!m.HandleEvent(trace, typed('x')) || len(asked) != 2 || m.GetRegionState("r") != "A" || m.GetVar("eaten") != 0 {
		t.Fatalf("after an internal and a passing local event: %d asks, state %q, eaten %d; want both asked and nothing taken",
			len(asked), m.GetRegionState("r"), m.GetVar("eaten"))
	}
	if p := asked[1]; p.Owner != int(a) || p.Index != 0 || p.Trigger != event.EventCharacterTyped || len(p.Vars) != 1 || p.Vars[0] != (event.FSMVar{Name: "col", Value: 7}) {
		t.Fatalf("the ask is %+v, want A's first transition on EventCharacterTyped capturing col=7", *p)
	}
	if !m.HandleEvent(trace, mode) || m.HandleEvent(trace, typed('x')) || len(asked) != 2 {
		t.Fatalf("while asked: %d asks; want the mode change B reacts to parked and a second typed glyph dropped", len(asked))
	}
	landed := asked[1]
	if err := m.Apply(trace, &event.FSMTransitionPayload{Region: "r", Owner: landed.Owner, Index: landed.Index, Trigger: landed.Trigger, Vars: landed.Vars}); err != nil ||
		m.GetRegionState("r") != "B" || m.GetVar("col") != 7 || len(*trace) != 1 {
		t.Fatalf("apply: err %v, state %q, col %d, events %v; want B entered with col=7", err, m.GetRegionState("r"), m.GetVar("col"), *trace)
	}
	if len(asked) != 3 || asked[2].Owner != int(b) || asked[2].Index != 0 {
		t.Fatalf("%d asks after B was entered; want the parked mode change judged there and asked", len(asked))
	}
	for range m.AskUpdates {
		if m.HandleEvent(trace, mode); len(asked) != 3 {
			t.Fatalf("%d asks while the second waits, want it alone", len(asked))
		}
		m.Update(trace, time.Millisecond)
	}
	if !m.HandleEvent(trace, mode) || len(asked) != 4 || m.GetRegionState("r") != "B" {
		t.Fatalf("after %d updates unanswered: %d asks, state %q; want the region judging again in B", m.AskUpdates, len(asked), m.GetRegionState("r"))
	}
}

// TestAnAuthorityGrantsOnlyALocalEventsTransitionWhereItsRegionStands: an ask is
// taken as the machine's own proposal when it names a transition a local event
// triggers, of a state its running region stands in with none in flight, and only
// variables that transition captures; any other is refused and changes nothing.
func TestAnAuthorityGrantsOnlyALocalEventsTransitionWhereItsRegionStands(t *testing.T) {
	m, trace, err := loadMachine(t, askedDoc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var proposed []*event.FSMTransitionPayload
	m.Propose = func(region string, owner fsm.StateID, index int, trigger event.EventType, vars []event.FSMVar, then []event.FSMHop) {
		proposed = append(proposed, &event.FSMTransitionPayload{Region: region, Owner: int(owner), Index: index, Trigger: trigger, Vars: vars, Then: then})
	}
	m.Steer(false, true, false)
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	a, _ := m.GetStateID("A")
	b, _ := m.GetStateID("B")
	ask := func(owner fsm.StateID, index int, trigger event.EventType, vars ...event.FSMVar) *event.FSMTransitionRequestPayload {
		return &event.FSMTransitionRequestPayload{Region: "r", Owner: int(owner), Index: index, Trigger: trigger, Vars: vars}
	}
	typed := ask(a, 0, event.EventCharacterTyped, event.FSMVar{Name: "col", Value: 7})
	if err := m.Grant(trace, typed); err != nil || len(proposed) != 1 || m.GetRegionState("r") != "A" {
		t.Fatalf("grant: err %v, %d proposals, state %q; want A's transition proposed and A standing until it applies", err, len(proposed), m.GetRegionState("r"))
	}
	if p := proposed[0]; p.Owner != int(a) || p.Index != 0 || len(p.Vars) != 1 || p.Vars[0].Value != 7 {
		t.Fatalf("the proposal is %+v, want the transition asked for with col=7", *p)
	}
	if err := m.Grant(trace, typed); err == nil {
		t.Fatal("an ask was granted with a transition of its region in flight")
	}
	if err := m.Apply(trace, proposed[0]); err != nil || m.GetRegionState("r") != "B" {
		t.Fatalf("apply: err %v, state %q, want B", err, m.GetRegionState("r"))
	}

	unknown := ask(b, 0, event.EventModeChanged)
	unknown.Region = "none"
	for name, p := range map[string]*event.FSMTransitionRequestPayload{
		"a state the region left":        typed,
		"an index past the state's":      ask(b, 3, event.EventModeChanged),
		"a negative index":               ask(b, -1, event.EventModeChanged),
		"a state the graph lacks":        ask(-4, 0, event.EventModeChanged),
		"a region that is not running":   unknown,
		"a shared event's transition":    ask(b, 1, event.EventSpeciesKilled),
		"a Tick transition":              ask(b, 2, event.EventNone),
		"another trigger than its own":   ask(b, 0, event.EventCharacterTyped),
		"a variable it does not capture": ask(b, 0, event.EventModeChanged, event.FSMVar{Name: "eaten", Value: 9}),
	} {
		if err := m.Grant(trace, p); err == nil || len(proposed) != 1 || m.GetRegionState("r") != "B" || m.GetVar("eaten") != 0 {
			t.Errorf("%s: err %v, %d proposals, state %q, eaten %d; want it refused with nothing changed",
				name, err, len(proposed), m.GetRegionState("r"), m.GetVar("eaten"))
		}
	}
	m.PauseRegion("r")
	if err := m.Grant(trace, ask(b, 0, event.EventModeChanged)); err == nil {
		t.Error("an ask was granted in a paused region")
	}
	m.ResumeRegion("r")
	if err := m.Grant(trace, ask(b, 0, event.EventModeChanged)); err != nil || len(proposed) != 2 {
		t.Fatalf("B's local transition: err %v, %d proposals; want it granted", err, len(proposed))
	}
	for _, following := range []bool{true, false} {
		m.Steer(following, false, false)
		if err := m.Grant(trace, ask(b, 0, event.EventModeChanged)); err == nil {
			t.Fatalf("a machine that proposes nothing (following %t) granted an ask", following)
		}
	}
}
