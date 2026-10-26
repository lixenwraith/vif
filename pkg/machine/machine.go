// Package machine steps per-entity state machines. An Instance (a state, its
// elapsed time and the duration of its entry) lives in a component as plain
// integers; a Graph, built once from typed hooks and immutable after Build, steps
// it in fixed phases. Standard library only, so a graph reaches its world only
// through the context its host passes.
package machine

import (
	"fmt"
	"time"
)

// Instance is one entity's place in a graph. Elapsed is relative, so a
// suspension freezes it, and Duration is stored because an entry may override
// its state's default.
type Instance[S ~uint8] struct {
	State    S             `json:"state"`
	Elapsed  time.Duration `json:"elapsed"`
	Duration time.Duration `json:"duration"` // 0 = untimed
}

// Progress is Elapsed over Duration clamped to [0, 1], and 0 while untimed
func (in Instance[S]) Progress() float64 {
	if in.Duration <= 0 {
		return 0
	}
	return min(max(float64(in.Elapsed)/float64(in.Duration), 0), 1)
}

// Expired reports a timed entry whose duration has run out
func (in Instance[S]) Expired() bool {
	return in.Duration > 0 && in.Elapsed >= in.Duration
}

// Mask holds one bit per state, which is why a graph has at most StatesMax
type Mask uint64

// StatesMax is how many states a graph holds, one bit of Mask each
const StatesMax = 64

func MaskOf[S ~uint8](states ...S) Mask {
	var m Mask
	for _, s := range states {
		m |= Mask(1) << s
	}
	return m
}

// Cause says why an instance changed
type Cause uint8

const (
	CauseStart Cause = iota
	CauseTimeout
	CausePreTick
	CausePostTick
	CauseGlobal
	CauseRetry
	CauseStop
)

// Observer hears every change once it is committed; Start, Stop and a retry
// report from == to
type Observer[S ~uint8] func(from, to S, cause Cause)

// State declares one state. A nil hook does nothing.
type State[C any] struct {
	Name     string
	Duration time.Duration // default for entries that set none; 0 = untimed
	Flags    uint64
	Probe    func(C) bool // fallible entry, may stash results in C; nil = infallible
	Enter    func(C)
	Exit     func(C)
	Tick     func(C)
	Cues     []Cue[C] // timed states only
}

// Cue runs Do once per entry, in the step that brings the entry's elapsed time to
// AtPermille of its duration or past it
type Cue[C any] struct {
	AtPermille uint16
	Do         func(C)
}

// Target is one candidate of an edge
type Target[S ~uint8, C any] struct {
	State    S
	Guard    func(C) bool  // nil = always
	Duration time.Duration // 0 = the state's default
}

// Edge is a transition: its guard selects it, and its candidates are tried in
// order, each by its guard and then its state's probe
type Edge[S ~uint8, C any] struct {
	From     Mask         // global edges only
	Guard    func(C) bool // nil = always
	Do       func(C)      // runs between exit and enter
	To       []Target[S, C]
	Fallback Fallback // required when every candidate can fail
}

// Fallback is what an edge does when none of its candidates commits
type Fallback struct {
	kind  uint8
	retry time.Duration
}

const (
	fallbackNone uint8 = iota
	fallbackRetry
	fallbackHold
)

// Retry re-arms the current state for d in place: no exit or enter, and the new
// period runs the state's cues again
func Retry(d time.Duration) Fallback {
	return Fallback{kind: fallbackRetry, retry: d}
}

// Hold leaves the state as it is, so an expired one tries its timeout again on
// the next step
var Hold = Fallback{kind: fallbackHold}

// Graph is one species' machine, shared by all its instances and never changed
// after Build
type Graph[S ~uint8, C any] struct {
	states  []node[S, C] // by state; a gap is undefined and zero
	defined Mask
	global  []Edge[S, C]
	suspend func(C) bool
	observe Observer[S]
}

// node is a state with the edges leaving it; pre and post carry their source as
// From, so every edge list is selected by one test
type node[S ~uint8, C any] struct {
	State[C]
	timeout   *Edge[S, C]
	pre, post []Edge[S, C]
}

// Step advances in by dt: global edges, suspension, advance and cues, timeout,
// pre-tick edges, tick, post-tick edges. Each phase takes at most one transition,
// and the state a phase enters is the one the next phase runs. It returns the
// flags of the state it ends in and whether it suspended after the global edges.
func (g *Graph[S, C]) Step(in *Instance[S], c C, dt time.Duration) (flags uint64, suspended bool) {
	g.first(in, c, g.global, CauseGlobal)
	if g.suspend != nil && g.suspend(c) {
		return g.states[in.State].Flags, true
	}

	old := in.Elapsed
	in.Elapsed += dt
	for _, q := range g.states[in.State].Cues {
		at := permille(in.Duration, q.AtPermille)
		if at > in.Elapsed {
			break
		}
		if at > old {
			run(q.Do, c)
		}
	}

	if e := g.states[in.State].timeout; e != nil && in.Expired() {
		g.take(in, c, e, CauseTimeout)
	}
	g.first(in, c, g.states[in.State].pre, CausePreTick)
	run(g.states[in.State].Tick, c)
	g.first(in, c, g.states[in.State].post, CausePostTick)
	return g.states[in.State].Flags, false
}

// Start enters s for d, or its default when d is not positive, with no exit and
// no probe: a spawn. It panics on a state the graph does not define.
func (g *Graph[S, C]) Start(in *Instance[S], c C, s S, d time.Duration) {
	if !g.has(s) {
		panic(fmt.Sprintf("machine: start in undefined state %d", s))
	}
	if d <= 0 {
		d = g.states[s].Duration
	}
	*in = Instance[S]{State: s, Duration: d}
	run(g.states[s].Enter, c)
	g.report(s, s, CauseStart)
}

// Stop runs the current state's exit with no enter: a death
func (g *Graph[S, C]) Stop(in *Instance[S], c C) {
	run(g.states[in.State].Exit, c)
	g.report(in.State, in.State, CauseStop)
}

func (g *Graph[S, C]) Flags(s S) uint64 { return g.states[s].Flags }

func (g *Graph[S, C]) Name(s S) string { return g.states[s].Name }

// States lists the defined states in order, for an overlay
func (g *Graph[S, C]) States() []S {
	out := make([]S, 0, len(g.states))
	for s := range g.states {
		if g.has(S(s)) {
			out = append(out, S(s))
		}
	}
	return out
}

// first takes the first edge whose source holds the current state and whose
// guard passes; that edge owns the phase whether or not a candidate commits
func (g *Graph[S, C]) first(in *Instance[S], c C, edges []Edge[S, C], cause Cause) {
	for i := range edges {
		if e := &edges[i]; e.From&(Mask(1)<<in.State) != 0 && pass(e.Guard, c) {
			g.take(in, c, e, cause)
			return
		}
	}
}

// take commits the first candidate whose guard and probe pass: exit, the edge's
// action, the commit, then enter, so an enter hook reads the entry it starts.
// With none, a retry re-arms the state in place and a hold changes nothing.
func (g *Graph[S, C]) take(in *Instance[S], c C, e *Edge[S, C], cause Cause) {
	from := in.State
	for _, t := range e.To {
		to := &g.states[t.State]
		if !pass(t.Guard, c) || !pass(to.Probe, c) {
			continue
		}
		run(g.states[from].Exit, c)
		run(e.Do, c)
		*in = Instance[S]{State: t.State, Duration: t.Duration}
		run(to.Enter, c)
		g.report(from, t.State, cause)
		return
	}
	if e.Fallback.kind == fallbackRetry {
		in.Elapsed, in.Duration = 0, e.Fallback.retry
		g.report(from, from, CauseRetry)
	}
}

func (g *Graph[S, C]) has(s S) bool { return g.defined&(Mask(1)<<s) != 0 }

func (g *Graph[S, C]) report(from, to S, cause Cause) {
	if g.observe != nil {
		g.observe(from, to, cause)
	}
}

func pass[C any](f func(C) bool, c C) bool { return f == nil || f(c) }

func run[C any](f func(C), c C) {
	if f != nil {
		f(c)
	}
}

// permille is p thousandths of d, exact in integer nanoseconds for any d
func permille(d time.Duration, p uint16) time.Duration {
	q := time.Duration(p)
	return d/1000*q + d%1000*q/1000
}
