package machine

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

type st uint8

const (
	sA st = iota
	sB
	sC
	sD
	sE
)

var (
	stNames    = [...]string{"A", "B", "C", "D", "E"}
	causeNames = [...]string{"start", "timeout", "pre-tick", "post-tick", "global", "retry", "stop"}
)

// trace logs what a graph ran, naming the state the instance held at each hook
type trace struct {
	in  *Instance[st]
	log []string
}

type (
	state  = State[*trace]
	edge   = Edge[st, *trace]
	target = Target[st, *trace]
)

func (tr *trace) at(what string) func(*trace) {
	return func(tr *trace) { tr.log = append(tr.log, what+" "+stNames[tr.in.State]) }
}

func TestBuildNamesEachFault(t *testing.T) {
	never := func(*trace) bool { return false }
	untimed := state{Name: "A"}
	timed := state{Name: "A", Duration: time.Second}
	probed := state{Name: "B", Probe: never}
	toA := []target{{State: sA}}
	cue := func(at uint16) []Cue[*trace] { return []Cue[*trace]{{AtPermille: at}} }

	cases := []struct {
		build func(b *Builder[st, *trace])
		want  string
	}{
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).PreTick(sA, edge{To: []target{{State: 9}}}) },
			`pre-tick edge 1 from state "A": candidate 1: state 9 undefined`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).PostTick(9, edge{To: toA}) },
			`post-tick edge from state 9: state undefined`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).Global(edge{From: MaskOf(sA, 9), To: toA}) },
			`global edge 1: From holds state 9, which is undefined`},
		{func(b *Builder[st, *trace]) {
			b.State(sA, timed).State(sB, probed).Timeout(sA, edge{To: []target{{State: sB}, {State: sA, Guard: never}}})
		}, `timeout edge 1 from state "A": every candidate can fail and no fallback is declared`},
		{func(b *Builder[st, *trace]) {
			b.State(sA, timed).Timeout(sA, edge{To: []target{{State: sA, Guard: never}}, Fallback: Retry(0)})
		}, `timeout edge 1 from state "A": retry duration 0s is not positive`},
		{func(b *Builder[st, *trace]) { b.State(sA, state{Name: "A", Cues: cue(500)}) },
			`state "A": cue 1 on an untimed state`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).Timeout(sA, edge{To: toA}) },
			`timeout edge 1 from state "A": state is untimed`},
		{func(b *Builder[st, *trace]) { b.State(sA, state{Name: "A", Duration: time.Second, Cues: cue(0)}) },
			`state "A": cue 1 at 0‰ outside (0, 1000]`},
		{func(b *Builder[st, *trace]) { b.State(sA, state{Name: "A", Duration: time.Second, Cues: cue(1001)}) },
			`state "A": cue 1 at 1001‰ outside (0, 1000]`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).Global(edge{To: toA}) },
			`global edge 1: From is empty`},
		{func(b *Builder[st, *trace]) { b.State(sA, timed).Timeout(sA, edge{To: toA}).Timeout(sA, edge{To: toA}) },
			`timeout edge 2 from state "A": a state takes one timeout edge`},
		{func(b *Builder[st, *trace]) { b.State(64, untimed) },
			`state 64: outside the 64 states a graph holds`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).State(sA, untimed) },
			`state "A": defined twice`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).PreTick(sA, edge{From: MaskOf(sA), To: toA}) },
			`pre-tick edge 1 from state "A": From is for global edges only`},
		{func(b *Builder[st, *trace]) { b.State(sA, state{Name: "A", Duration: -time.Second}) },
			`state "A": duration -1s is negative`},
		{func(b *Builder[st, *trace]) {
			b.State(sA, untimed).PreTick(sA, edge{To: []target{{State: sA, Duration: -1}}})
		}, `pre-tick edge 1 from state "A": candidate 1: duration -1ns is negative`},
		{func(b *Builder[st, *trace]) { b.State(sA, untimed).Suspend(never).Suspend(never) },
			`suspend declared 2 times`},
	}
	for _, tc := range cases {
		b := New[st, *trace](nil)
		tc.build(b)
		if g, err := b.Build(); g != nil || err == nil || err.Error() != "machine: "+tc.want {
			t.Errorf("Build() = %v, want the one fault %q", err, tc.want)
		}
	}
}

// TestAStepRunsItsPhasesInOrder makes the four transitions one step can: a global
// edge into B, whose cue and whole duration then elapse, B's timeout, C's pre-tick
// edge, and D's post-tick edge after D's tick. Hooks log the state the instance
// holds, so exit and the edge action read the old entry and enter the new one.
func TestAStepRunsItsPhasesInOrder(t *testing.T) {
	tr := &trace{in: &Instance[st]{}}
	hooks := func(name string, d time.Duration, flags uint64) state {
		return state{Name: name, Duration: d, Flags: flags, Enter: tr.at("enter"), Exit: tr.at("exit"), Tick: tr.at("tick")}
	}
	withCue := hooks("B", 50*time.Millisecond, 2)
	withCue.Cues = []Cue[*trace]{{AtPermille: 500, Do: tr.at("cue")}}
	g := Must(New[st, *trace](func(from, to st, cause Cause) {
		tr.log = append(tr.log, causeNames[cause]+" "+stNames[from]+">"+stNames[to])
	}).
		State(sA, hooks("A", 0, 1)).
		State(sB, withCue).
		State(sC, hooks("C", time.Second, 4)).
		State(sD, hooks("D", 0, 8)).
		State(sE, hooks("E", 0, 16)).
		Global(edge{From: MaskOf(sA), Do: tr.at("do"), To: []target{{State: sB}}}).
		Timeout(sB, edge{Do: tr.at("do"), To: []target{{State: sC}}}).
		PreTick(sC, edge{To: []target{{State: sD}}}).
		PostTick(sD, edge{To: []target{{State: sE}}}).
		Suspend(func(tr *trace) bool { tr.log = append(tr.log, "suspend?"); return false }).
		Build())

	g.Start(tr.in, tr, sA, 0)
	tr.log = nil
	flags, suspended := g.Step(tr.in, tr, 50*time.Millisecond)

	want := []string{
		"exit A", "do A", "enter B", "global A>B",
		"suspend?",
		"cue B",
		"exit B", "do B", "enter C", "timeout B>C",
		"exit C", "enter D", "pre-tick C>D",
		"tick D",
		"exit D", "enter E", "post-tick D>E",
	}
	if !slices.Equal(tr.log, want) || flags != 16 || suspended || *tr.in != (Instance[st]{State: sE}) {
		t.Fatalf("step ran %q, returned %b %v, left %+v; want %q, 10000 false, E", tr.log, flags, suspended, *tr.in, want)
	}
}

// TestACueOnATickBoundaryFiresOnce steps a 100 ms state with cues at its end and
// its half, declared in that order, in 25 ms ticks: a cue fires on the tick its
// point lands on exactly and never on the next, and none fires once it expired.
func TestACueOnATickBoundaryFiresOnce(t *testing.T) {
	var fired []string
	step := 0
	cue := func(name string) func(*trace) {
		return func(*trace) { fired = append(fired, fmt.Sprintf("%s@%d", name, step)) }
	}
	g := Must(New[st, *trace](nil).State(sA, state{Name: "A", Duration: 100 * time.Millisecond, Cues: []Cue[*trace]{
		{AtPermille: 1000, Do: cue("end")},
		{AtPermille: 500, Do: cue("half")},
	}}).Build())

	var in Instance[st]
	g.Start(&in, nil, sA, 0)
	for step = 1; step <= 6; step++ {
		g.Step(&in, nil, 25*time.Millisecond)
	}
	if want := []string{"half@2", "end@4"}; !slices.Equal(fired, want) {
		t.Fatalf("cues fired %q, want %q", fired, want)
	}
}
