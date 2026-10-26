package machine

import (
	"cmp"
	"math/rand/v2"
	"testing"
	"time"
)

// The fuzzed graph, suspending one step in ten:
// A untimed: pre 1/4 → B; post 1/8 → D, hold; global 1/16 → D, hold.
// B 100 ms, cues at 250 and 1000‰: timeout → [C 1/2 | D], retry 30 ms.
// C 70 ms, cue at 500‰: timeout → [A 1/3], hold; post 1/6 → D, retry 20 ms.
// D 50 ms, cue at 1000‰, probe 2/3: timeout → [A 1/2 | B]; B C D global 1/16 → A.
var (
	fuzzDuration = [4]time.Duration{0, 100 * time.Millisecond, 70 * time.Millisecond, 50 * time.Millisecond}
	fuzzRetry    = [4]time.Duration{sB: 30 * time.Millisecond, sC: 20 * time.Millisecond}
	fuzzTimeout  = [4]bool{sB: true, sC: true, sD: true}
	fuzzCues     = [4][]uint16{sB: {250, 1000}, sC: {500}, sD: {1000}}
	fuzzSteps    = []time.Duration{0, 5, 10, 20, 25, 35, 50, 75}
)

type (
	fuzzEdge   = Edge[st, *fuzzRun]
	fuzzTarget = Target[st, *fuzzRun]
)

// FuzzStep steps the graph above 1,000 times, with guards, probe, suspension, dt
// and the odd respawn drawn from a PCG seeded by the input, and checks the
// step's invariants from inside the hooks that see them.
func FuzzStep(f *testing.F) {
	f.Add(uint64(1), uint64(2))
	f.Add(uint64(3), uint64(5))
	f.Add(uint64(8), uint64(13))
	f.Fuzz(func(t *testing.T, seed1, seed2 uint64) {
		newFuzzRun(t, seed1, seed2).run(1000)
	})
}

type fuzzRun struct {
	t    *testing.T
	rng  *rand.Rand
	g    *Graph[st, *fuzzRun]
	in   Instance[st]
	dt   time.Duration
	step int

	live     bool // between Start and Stop
	starting bool
	stopping bool
	exiting  bool // an exit waits for its enter
	changed  bool // an exit or enter ran since the last report
	probed   bool // the last guard or probe asked was D's probe, and it passed
	cur      st   // the state last entered

	inStep       bool
	globals      int  // global guards asked this step
	suspends     int  // suspend predicates asked this step
	frozen       bool // the predicate held: nothing may run until the step returns
	afterGlobal  Instance[st]
	reports      int
	ticks        int
	timeoutTried bool
	fired        [2]bool // the current entry's cues
}

func newFuzzRun(t *testing.T, seed1, seed2 uint64) *fuzzRun {
	r := &fuzzRun{t: t, rng: rand.New(rand.NewPCG(seed1, seed2))}
	b := New[st, *fuzzRun](r.report)
	for s := sA; s <= sD; s++ {
		decl := State[*fuzzRun]{
			Name:     stNames[s],
			Duration: fuzzDuration[s],
			Flags:    1 << s,
			Enter:    func(r *fuzzRun) { r.enter(s) },
			Exit:     func(r *fuzzRun) { r.exit(s) },
			Tick:     func(r *fuzzRun) { r.tick(s) },
		}
		for i, at := range fuzzCues[s] {
			decl.Cues = append(decl.Cues, Cue[*fuzzRun]{AtPermille: at, Do: func(r *fuzzRun) { r.cue(s, i) }})
		}
		if s == sD {
			decl.Probe = (*fuzzRun).probe
		}
		b.State(s, decl)
	}
	do := (*fuzzRun).do
	b.PreTick(sA, fuzzEdge{Guard: odds(4, 'm'), To: []fuzzTarget{{State: sB}}}).
		PostTick(sA, fuzzEdge{Guard: odds(8, 'm'), To: []fuzzTarget{{State: sD}}, Fallback: Hold}).
		Timeout(sB, fuzzEdge{Do: do, To: []fuzzTarget{{State: sC, Guard: odds(2, 't')}, {State: sD}}, Fallback: Retry(fuzzRetry[sB])}).
		Timeout(sC, fuzzEdge{To: []fuzzTarget{{State: sA, Guard: odds(3, 't')}}, Fallback: Hold}).
		PostTick(sC, fuzzEdge{Guard: odds(6, 'm'), To: []fuzzTarget{{State: sD}}, Fallback: Retry(fuzzRetry[sC])}).
		Timeout(sD, fuzzEdge{To: []fuzzTarget{{State: sA, Guard: odds(2, 't')}, {State: sB}}}).
		Global(fuzzEdge{From: MaskOf(sB, sC, sD), Guard: odds(16, 'g'), Do: do, To: []fuzzTarget{{State: sA}}}).
		Global(fuzzEdge{From: MaskOf(sA), Guard: odds(16, 'g'), To: []fuzzTarget{{State: sD}}, Fallback: Hold}).
		Suspend((*fuzzRun).suspend)
	r.g = Must(b.Build())
	r.start()
	return r
}

func (r *fuzzRun) run(steps int) {
	for r.step = 1; r.step <= steps; r.step++ {
		if r.rng.IntN(64) == 0 {
			r.stopping = true
			r.g.Stop(&r.in, r)
			r.stopping = false
			r.start()
		}
		r.dt = fuzzSteps[r.rng.IntN(len(fuzzSteps))] * time.Millisecond
		r.inStep, r.globals, r.suspends, r.reports, r.ticks, r.timeoutTried = true, 0, 0, 0, 0, false
		flags, suspended := r.g.Step(&r.in, r, r.dt)
		r.inStep = false
		r.check(flags, suspended)
		r.frozen = false
	}
}

// start respawns in a random state, for its default duration or a drawn one
func (r *fuzzRun) start() {
	s := st(r.rng.IntN(4))
	var d time.Duration
	if r.rng.IntN(2) == 0 {
		d = time.Duration(1+r.rng.IntN(100)) * time.Millisecond
	}
	r.starting = true
	r.g.Start(&r.in, r, s, d)
	r.starting = false
	if want := (Instance[st]{State: s, Duration: cmp.Or(d, fuzzDuration[s])}); r.in != want {
		r.fail("Start left %+v, want %+v", r.in, want)
	}
}

// check holds what a whole step promises: the flags of the state it ends in, a
// suspended step that moved nothing after its global edges, progress full just
// when the entry expired, an unsuspended step that ticked once, an expired entry
// that tried its timeout, and exactly the cues whose points it passed fired once.
func (r *fuzzRun) check(flags uint64, suspended bool) {
	in, before := r.in, r.afterGlobal
	if flags != 1<<in.State {
		r.fail("flags %b in state %s", flags, stNames[in.State])
	}
	if suspended != r.frozen || r.suspends != 1 {
		r.fail("suspended %v after %d predicates, the last holding %v", suspended, r.suspends, r.frozen)
	}
	if suspended && in != before {
		r.fail("a suspended step moved %+v to %+v", before, in)
	}
	if p := in.Progress(); p < 0 || p > 1 || in.Expired() != (in.Duration > 0 && p == 1) {
		r.fail("progress %v of %+v disagrees with its expiry", p, in)
	}
	if !suspended && r.ticks != 1 {
		r.fail("an unsuspended step ran %d ticks", r.ticks)
	}
	if !suspended && fuzzTimeout[before.State] && before.Elapsed+r.dt >= before.Duration && !r.timeoutTried {
		r.fail("%+v expired over %v without trying its timeout", before, r.dt)
	}
	for i := range r.fired {
		passed := i < len(fuzzCues[in.State]) && in.Duration*time.Duration(fuzzCues[in.State][i])/1000 <= in.Elapsed
		if r.fired[i] != passed {
			r.fail("cue %d of %+v fired %v, its point passed %v", i, in, r.fired[i], passed)
		}
	}
}

// odds is a guard passing one time in n. Its phase is where it may be asked:
// 'g' a global edge's, before the predicate and once a step; 't' a timeout's
// first candidate, only once the entry expired; 'm' any after the predicate.
func odds(n int, phase byte) func(*fuzzRun) bool {
	return func(r *fuzzRun) bool {
		switch phase {
		case 'g':
			if r.globals++; r.suspends != 0 || r.globals > 1 {
				r.fail("global guard %d asked after %d predicates", r.globals, r.suspends)
			}
		case 't':
			r.later()
			if !r.in.Expired() {
				r.fail("timeout tried on %+v, which has not expired", r.in)
			}
			r.timeoutTried = true
		default:
			r.later()
		}
		r.probed = false
		return r.rng.IntN(n) == 0
	}
}

func (r *fuzzRun) suspend() bool {
	r.unfrozen()
	if r.suspends++; r.suspends != 1 || r.globals != 1 {
		r.fail("predicate %d asked after %d global guards", r.suspends, r.globals)
	}
	r.afterGlobal = r.in
	r.frozen = r.rng.IntN(10) == 0
	r.probed = false
	return r.frozen
}

func (r *fuzzRun) probe() bool {
	r.unfrozen()
	r.probed = r.rng.IntN(3) != 0
	return r.probed
}

func (r *fuzzRun) exit(s st) {
	r.unfrozen()
	if !r.live || r.exiting || r.cur != s || r.in.State != s {
		r.fail("exit %s with %s current, live %v, exiting %v", stNames[s], stNames[r.in.State], r.live, r.exiting)
	}
	r.changed = true
	if r.stopping {
		r.live = false
		return
	}
	r.exiting = true
}

func (r *fuzzRun) do() {
	r.unfrozen()
	if !r.exiting {
		r.fail("an edge action ran outside an exit and its enter")
	}
}

// enter checks a spawn enters without an exit, a transition after one and, into
// D, right after D's probe passed; either way the entry is already committed
func (r *fuzzRun) enter(s st) {
	r.unfrozen()
	switch {
	case r.starting && (r.live || r.exiting):
		r.fail("Start entered %s over a live instance", stNames[s])
	case r.starting:
		r.live = true
	case !r.exiting:
		r.fail("enter %s with no exit", stNames[s])
	case s == sD && !r.probed:
		r.fail("entered D without its probe passing")
	}
	if r.in.State != s || r.in.Elapsed != 0 {
		r.fail("enter %s reads %+v", stNames[s], r.in)
	}
	r.exiting, r.changed, r.cur = false, true, s
}

func (r *fuzzRun) tick(s st) {
	r.later()
	if r.ticks++; r.in.State != s || r.ticks > 1 {
		r.fail("tick %d of %s with %s current", r.ticks, stNames[s], stNames[r.in.State])
	}
}

// cue checks a cue fires in the step its point falls in, once per entry
func (r *fuzzRun) cue(s st, i int) {
	r.later()
	at := r.in.Duration * time.Duration(fuzzCues[s][i]) / 1000
	if r.in.State != s || r.fired[i] || at <= r.in.Elapsed-r.dt || at > r.in.Elapsed {
		r.fail("cue %d of %s at %v fired again or outside the step to %+v", i, stNames[s], at, r.in)
	}
	r.fired[i] = true
}

// report checks every change: at most four a step, a spawn and a death with
// their one hook, a retry re-arming the state with no exit or enter, and a
// transition committed after its exit and enter
func (r *fuzzRun) report(from, to st, cause Cause) {
	r.unfrozen()
	in := r.in
	var ok bool
	switch cause {
	case CauseStart:
		ok = r.starting && from == to && r.cur == to && r.changed
	case CauseStop:
		ok = r.stopping && !r.live && from == to && r.changed
	case CauseRetry:
		ok = from == to && in.State == to && !r.changed && in.Elapsed == 0 && in.Duration == fuzzRetry[to] && fuzzRetry[to] > 0
	default:
		ok = r.changed && !r.exiting && r.cur == to && in == Instance[st]{State: to, Duration: fuzzDuration[to]}
	}
	if !ok {
		r.fail("%s %s>%s reported over %+v, changed %v", causeNames[cause], stNames[from], stNames[to], in, r.changed)
	}
	r.changed, r.probed = false, false
	if cause != CauseStop {
		r.fired = [2]bool{}
	}
	if r.inStep {
		if r.reports++; r.reports > 4 {
			r.fail("%d changes in one step", r.reports)
		}
	}
}

// later guards a hook of the phases after a predicate that let the step go on
func (r *fuzzRun) later() {
	if !r.inStep || r.suspends != 1 || r.frozen {
		r.fail("a later phase ran outside a step that went on")
	}
}

func (r *fuzzRun) unfrozen() {
	if r.frozen {
		r.fail("a hook ran after the suspend predicate held")
	}
}

func (r *fuzzRun) fail(format string, a ...any) {
	r.t.Helper()
	r.t.Fatalf("step %d: "+format, append([]any{r.step}, a...)...)
}
