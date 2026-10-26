package machine

import (
	"errors"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
)

// Builder collects a graph's declarations; Build validates them all at once
type Builder[S ~uint8, C any] struct {
	observe  Observer[S]
	states   []stateDecl[S, C]
	edges    []edgeDecl[S, C]
	suspend  func(C) bool
	suspends int
}

type stateDecl[S ~uint8, C any] struct {
	id S
	st State[C]
}

type edgeDecl[S ~uint8, C any] struct {
	kind Cause // CauseTimeout, CausePreTick, CausePostTick or CauseGlobal
	from S
	e    Edge[S, C]
}

var edgeKinds = [...]string{CauseTimeout: "timeout", CausePreTick: "pre-tick", CausePostTick: "post-tick"}

// New starts a graph that reports every change to observe, which may be nil
func New[S ~uint8, C any](observe Observer[S]) *Builder[S, C] {
	return &Builder[S, C]{observe: observe}
}

func (b *Builder[S, C]) State(id S, st State[C]) *Builder[S, C] {
	b.states = append(b.states, stateDecl[S, C]{id: id, st: st})
	return b
}

// Timeout declares the edge a timed state takes once its entry expires
func (b *Builder[S, C]) Timeout(from S, e Edge[S, C]) *Builder[S, C] {
	return b.edge(CauseTimeout, from, e)
}

// PreTick adds an edge from tried before the state's tick, in declaration order
func (b *Builder[S, C]) PreTick(from S, e Edge[S, C]) *Builder[S, C] {
	return b.edge(CausePreTick, from, e)
}

// PostTick adds an edge from tried after the state's tick, reading what the tick
// left in the context
func (b *Builder[S, C]) PostTick(from S, e Edge[S, C]) *Builder[S, C] {
	return b.edge(CausePostTick, from, e)
}

// Global adds an edge tried first in every step whose state e.From holds, in
// declaration order
func (b *Builder[S, C]) Global(e Edge[S, C]) *Builder[S, C] {
	return b.edge(CauseGlobal, 0, e)
}

// Suspend declares the condition under which a step stops after its global
// edges, with no advance, cue, timeout or tick
func (b *Builder[S, C]) Suspend(pred func(C) bool) *Builder[S, C] {
	b.suspend = pred
	b.suspends++
	return b
}

func (b *Builder[S, C]) edge(kind Cause, from S, e Edge[S, C]) *Builder[S, C] {
	b.edges = append(b.edges, edgeDecl[S, C]{kind: kind, from: from, e: e})
	return b
}

// Build validates the declarations and returns the graph, or an error naming
// every fault with its state or edge
func (b *Builder[S, C]) Build() (*Graph[S, C], error) {
	g := &Graph[S, C]{suspend: b.suspend, observe: b.observe}
	var errs []error
	fail := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("machine: "+format, a...))
	}

	for _, d := range b.states {
		g.define(d.id, d.st, fail)
	}
	if b.suspends > 1 {
		fail("suspend declared %d times", b.suspends)
	}

	var declared [CausePostTick + 1][StatesMax]int
	globals := 0
	for _, d := range b.edges {
		e := d.e
		var where string
		if d.kind == CauseGlobal {
			globals++
			where = fmt.Sprintf("global edge %d", globals)
			switch undefined := e.From &^ g.defined; {
			case e.From == 0:
				fail("%s: From is empty", where)
			case undefined != 0:
				fail("%s: From holds state %d, which is undefined", where, bits.TrailingZeros64(uint64(undefined)))
			}
		} else {
			if !g.has(d.from) {
				fail("%s edge from state %d: state undefined", edgeKinds[d.kind], d.from)
				continue
			}
			declared[d.kind][d.from]++
			where = fmt.Sprintf("%s edge %d from %s", edgeKinds[d.kind], declared[d.kind][d.from], g.label(d.from))
			if e.From != 0 {
				fail("%s: From is for global edges only", where)
			}
			e.From = MaskOf(d.from)
		}
		g.resolve(&e, where, fail)

		switch d.kind {
		case CauseTimeout:
			if g.states[d.from].Duration <= 0 {
				fail("%s: state is untimed", where)
			}
			if declared[d.kind][d.from] > 1 {
				fail("%s: a state takes one timeout edge", where)
			}
			g.states[d.from].timeout = &e
		case CausePreTick:
			g.states[d.from].pre = append(g.states[d.from].pre, e)
		case CausePostTick:
			g.states[d.from].post = append(g.states[d.from].post, e)
		case CauseGlobal:
			g.global = append(g.global, e)
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return g, nil
}

// Must returns g, panicking on err: a code graph that fails to build is a
// broken program, as a broken manifest is
func Must[S ~uint8, C any](g *Graph[S, C], err error) *Graph[S, C] {
	if err != nil {
		panic(err)
	}
	return g
}

// define validates one state and stores it, cues in time order
func (g *Graph[S, C]) define(id S, st State[C], fail func(string, ...any)) {
	switch {
	case id >= StatesMax:
		fail("state %d: outside the %d states a graph holds", id, StatesMax)
		return
	case g.has(id):
		fail("state %q: defined twice", st.Name)
		return
	}
	if st.Duration < 0 {
		fail("state %q: duration %v is negative", st.Name, st.Duration)
	}
	for i, q := range st.Cues {
		if st.Duration <= 0 {
			fail("state %q: cue %d on an untimed state", st.Name, i+1)
		}
		if q.AtPermille == 0 || q.AtPermille > 1000 {
			fail("state %q: cue %d at %d‰ outside (0, 1000]", st.Name, i+1, q.AtPermille)
		}
	}
	st.Cues = slices.Clone(st.Cues)
	slices.SortStableFunc(st.Cues, func(a, b Cue[C]) int { return int(a.AtPermille) - int(b.AtPermille) })

	if n := int(id) + 1; n > len(g.states) {
		g.states = append(g.states, make([]node[S, C], n-len(g.states))...)
	}
	g.states[id].State = st
	g.defined |= Mask(1) << id
}

// resolve validates an edge's candidates and fallback on a private copy, filling
// each candidate's default duration. A chain whose every candidate has a guard
// or a probe can fail entirely, and must say what happens then.
func (g *Graph[S, C]) resolve(e *Edge[S, C], where string, fail func(string, ...any)) {
	e.To = slices.Clone(e.To)
	canFail := true
	for i := range e.To {
		t := &e.To[i]
		if !g.has(t.State) {
			fail("%s: candidate %d: state %d undefined", where, i+1, t.State)
			canFail = false
			continue
		}
		if t.Duration < 0 {
			fail("%s: candidate %d: duration %v is negative", where, i+1, t.Duration)
		}
		if t.Duration == 0 {
			t.Duration = g.states[t.State].Duration
		}
		if t.Guard == nil && g.states[t.State].Probe == nil {
			canFail = false
		}
	}
	switch e.Fallback.kind {
	case fallbackNone:
		if canFail {
			fail("%s: every candidate can fail and no fallback is declared", where)
		}
	case fallbackRetry:
		if e.Fallback.retry <= 0 {
			fail("%s: retry duration %v is not positive", where, e.Fallback.retry)
		}
	}
}

func (g *Graph[S, C]) label(s S) string { return "state " + strconv.Quote(g.states[s].Name) }
