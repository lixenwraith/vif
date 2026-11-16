package behavior

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
)

// Set is a scenario's compiled programs in sorted-name order, program i species
// SpeciesDataFirst+i, and the loot it declares, which their kills roll. Its slices
// are shared and never change after Compile.
type Set[C any] struct {
	names    []string
	protos   []component.BehaviorPrototype
	graphs   []*machine.Graph[component.BehaviorState, C]
	initials []component.BehaviorState
	fits     []func(C)
	signals  [][]string // each program's signals, a bit each in declared order
	hears    [][]uint8  // by program and state, the signals an edge out of it takes
	loot     component.LootSet
	maps     []component.MapLayout
}

func (s *Set[C]) Len() int { return len(s.names) }

func (s *Set[C]) Names() []string { return s.names }

// Lookup finds a program's index by name
func (s *Set[C]) Lookup(name string) (int, bool) { return slices.BinarySearch(s.names, name) }

// Prototypes lists each program's species body by index
func (s *Set[C]) Prototypes() []component.BehaviorPrototype { return s.protos }

func (s *Set[C]) Graph(i int) *machine.Graph[component.BehaviorState, C] { return s.graphs[i] }

// Initial is the state a spawn of program i starts in
func (s *Set[C]) Initial(i int) component.BehaviorState { return s.initials[i] }

// Fit is what a spawn of program i applies to its instance before it starts, its
// prototype's fittings; nil when it has none
func (s *Set[C]) Fit(i int) func(C) { return s.fits[i] }

// Signals is the signals program i declares, signal n its bit n
func (s *Set[C]) Signals(i int) []string { return s.signals[i] }

// Hears is the signals an instance of program i in state takes on an edge out of it
func (s *Set[C]) Hears(i int, state component.BehaviorState) uint8 {
	if int(state) >= len(s.hears[i]) {
		return 0
	}
	return s.hears[i][state]
}

// Loot is the loot kinds and drop tables the scenario declares
func (s *Set[C]) Loot() *component.LootSet { return &s.loot }

// Maps is the maps the scenario declares, in sorted-name order
func (s *Set[C]) Maps() []component.MapLayout { return s.maps }

// Declared is what the scenario adds to the code's names: its programs with their
// signals, and its kinds
func (s *Set[C]) Declared() component.Declared {
	d := component.Declared{Programs: s.names, Loot: s.loot.Names(), Signals: make(map[string][]string)}
	for _, m := range s.maps {
		d.Maps = append(d.Maps, m.Name)
	}
	for i, name := range s.names {
		if len(s.signals[i]) > 0 {
			d.Signals[name] = s.signals[i]
		}
	}
	return d
}

// Compile builds the loot and the maps a scenario's entry declares, its [loot],
// [drops] and [maps] as entry holds them, any other key unread, so the resolved
// entry itself serves; then every program against v, each a machine graph whose
// states take IDs in sorted-name order. Every fault is reported, each naming its
// program, loot or map and place; no programs is an empty set.
func Compile[C any](v *Vocabulary[C], programs map[string]map[string]any, entry map[string]any) (*Set[C], error) {
	names := slices.Sorted(maps.Keys(programs))
	if len(names) > parameter.BehaviorProgramsMax {
		return nil, fmt.Errorf("%d behavior programs, at most %d", len(names), parameter.BehaviorProgramsMax)
	}
	set := &Set[C]{names: names}
	var errs []error
	set.loot, errs = compileLoot(entry, names)
	layouts, faults := compileMaps(entry, names)
	set.maps, errs = layouts, append(errs, faults...)
	// A payload may name any program's signal, so each is read ahead of the compile
	for _, name := range names {
		var signals []string
		list, _ := programs[name]["signals"].([]any)
		for _, item := range list {
			if s, ok := item.(string); ok {
				signals = append(signals, s)
			}
		}
		set.signals = append(set.signals, signals)
	}
	payloads := dialect.NewNames()
	if v.names != nil {
		payloads = v.names(set.Declared())
	}
	for i, name := range names {
		if err := checkName(name); err != nil {
			errs = append(errs, err)
			continue
		}
		c := compiler[C]{v: v, program: name, payloads: payloads, loot: &set.loot}
		raw, fit := c.fittings(programs[name])
		proto, graph, initial := c.build(raw, component.SpeciesDataFirst+component.SpeciesType(i))
		errs = append(errs, c.errs...)
		set.protos = append(set.protos, proto)
		set.graphs = append(set.graphs, graph)
		set.initials = append(set.initials, initial)
		set.fits = append(set.fits, fit)
		set.hears = append(set.hears, c.hears)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return set, nil
}

// compiler builds one program, collecting every fault with its place
type compiler[C any] struct {
	v        *Vocabulary[C]
	program  string
	payloads *dialect.Names
	loot     *component.LootSet // the tables a prototype's drops may name
	ids      map[string]component.BehaviorState
	sites    int
	errs     []error

	signals []string // the program's, a bit each
	hears   []uint8  // by state, the signals an edge out of it takes
	onEdge  bool     // an edge is compiling, which may take a signal
	heard   uint8    // the signals its guards read
}

// fail records err at where, a path below the program; each error a join holds
// is its own line, so every line names its place
func (c *compiler[C]) fail(where string, err error) {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range j.Unwrap() {
			c.fail(where, e)
		}
		return
	}
	if where != "" {
		err = fmt.Errorf("%s: %w", where, err)
	}
	c.errs = append(c.errs, fmt.Errorf("program '%s': %w", c.program, err))
}

// fittings builds the [prototype] tables the host registered, in key order, and
// returns the program without them, for the strict decode, and what a spawn applies
func (c *compiler[C]) fittings(raw map[string]any) (map[string]any, func(C)) {
	proto, _ := raw["prototype"].(map[string]any)
	var fs []func(C)
	cloned := false
	for _, key := range slices.Sorted(maps.Keys(c.v.fittings)) {
		table, ok := proto[key]
		if !ok {
			continue
		}
		// An empty table fits nothing, which is how a macro's default leaves a body bare
		if t, isTable := table.(map[string]any); !isTable || len(t) > 0 {
			f, err := c.v.fittings[key](c.payloads, table)
			if err != nil {
				c.fail("prototype: "+key, err)
			}
			fs = append(fs, f)
		}
		if !cloned { // the document is its caller's, so a copy loses the key
			raw, proto, cloned = maps.Clone(raw), maps.Clone(proto), true
			raw["prototype"] = proto
		}
		delete(proto, key)
	}
	return raw, sequence(fs)
}

func (c *compiler[C]) build(raw map[string]any, species component.SpeciesType) (proto component.BehaviorPrototype, graph *machine.Graph[component.BehaviorState, C], initial component.BehaviorState) {
	doc, err := dialect.Decode[programDoc](raw)
	if err != nil {
		c.fail("", err)
		return
	}
	if doc.Prototype == nil {
		c.fail("", errors.New("[prototype] is required"))
	} else if proto, err = doc.Prototype.prototype(c.program, species, c.loot); err != nil {
		c.fail("prototype", err)
	}

	names := slices.Sorted(maps.Keys(doc.States))
	switch {
	case len(names) == 0:
		c.fail("", errors.New("[states] defines no state"))
		return
	case len(names) > machine.StatesMax:
		c.fail("", fmt.Errorf("%d states, at most %d", len(names), machine.StatesMax))
		return
	}
	c.ids = make(map[string]component.BehaviorState, len(names))
	for i, name := range names {
		c.ids[name] = component.BehaviorState(i)
	}
	c.signals, c.hears, proto.Signals = doc.Signals, make([]uint8, len(names)), doc.Signals
	if len(c.signals) > parameter.BehaviorSignalsMax {
		c.fail("signals", fmt.Errorf("%d signals, at most %d", len(c.signals), parameter.BehaviorSignalsMax))
		return
	}
	for i, name := range c.signals {
		switch {
		case !wellNamed(name):
			c.fail("signals", fmt.Errorf("%.32q is not [a-z][a-z0-9_]* of at most %d bytes", name, parameter.BehaviorNameMax))
		case slices.Index(c.signals, name) != i:
			c.fail("signals", fmt.Errorf("%q is declared twice", name))
		}
	}
	initial, ok := c.ids[doc.Initial]
	if !ok {
		c.fail("initial", c.unknownState(doc.Initial))
	}

	b := machine.New[component.BehaviorState, C](nil)
	for _, name := range names {
		c.state(b, name, doc.States[name])
	}
	for i, g := range doc.Global {
		where := fmt.Sprintf("global[%d]", i)
		e, heard := c.edge(where, g.Guard, g.GuardArgs, g.Target, g.Actions, g.Fallback)
		if len(g.From) == 0 {
			c.fail(where, errors.New("from lists no state"))
		}
		for _, name := range g.From {
			if id, ok := c.ids[name]; ok {
				e.From |= machine.MaskOf(id)
				c.hears[id] |= heard
			} else {
				c.fail(where+": from", c.unknownState(name))
			}
		}
		b.Global(e)
	}
	if s := doc.Suspend; s != nil {
		if s.Guard == "" {
			c.fail("suspend", errors.New("guard is required"))
		}
		b.Suspend(c.guard("suspend", s.Guard, s.GuardArgs))
	}

	if len(c.errs) > 0 {
		return
	}
	if graph, err = b.Build(); err != nil {
		c.fail("", err)
	}
	return
}

// state declares one state and the edges leaving it
func (c *compiler[C]) state(b *machine.Builder[component.BehaviorState, C], name string, d *stateDoc) {
	where := fmt.Sprintf("state '%s'", name)
	id := c.ids[name]
	st := machine.State[C]{
		Name:     name,
		Duration: time.Duration(d.DurationMs) * time.Millisecond,
		Enter:    sequence([]func(C){c.effects(where+": on_enter", d.OnEnter), c.v.entered}),
		Exit:     c.effects(where+": on_exit", d.OnExit),
		Tick:     c.tick(where+": on_update", d.OnUpdate),
	}
	for _, f := range d.Flags {
		if bit, ok := c.v.flags[f]; ok {
			st.Flags |= bit
		} else {
			c.fail(where+": flags", fmt.Errorf("unknown flag '%s', not one of %s", f, strings.Join(c.v.flagNames(), ", ")))
		}
	}
	switch {
	case d.Probe != "":
		st.Probe = c.probe(where+": probe", d.Probe, d.ProbeArgs)
	case d.ProbeArgs != nil:
		c.fail(where, errors.New("probe_args without probe"))
	}
	for i, q := range d.Cues {
		do := c.effect(fmt.Sprintf("%s: cues[%d]", where, i), actionDoc{Action: q.Action, Event: q.Event, Payload: q.Payload})
		st.Cues = append(st.Cues, machine.Cue[C]{AtPermille: q.At, Do: do})
	}
	b.State(id, st)

	for i, t := range d.Transitions {
		at := fmt.Sprintf("%s: transitions[%d]", where, i)
		e, heard := c.edge(at, t.Guard, t.GuardArgs, t.Target, t.Actions, t.Fallback)
		c.hears[id] |= heard
		switch t.Trigger {
		case "Timeout":
			if t.Guard != "" {
				c.fail(at, errors.New("a Timeout takes no guard, an expired entry always tries its targets; guard the targets"))
			}
			b.Timeout(id, e)
		case "Tick":
			b.PreTick(id, e)
		case "AfterTick":
			b.PostTick(id, e)
		default:
			c.fail(at, fmt.Errorf("trigger '%s' is not Timeout, Tick or AfterTick", t.Trigger))
		}
	}
}

// edge compiles what transitions and global edges share, and the signals its
// guards read, which it lowers as it commits, ahead of its actions
func (c *compiler[C]) edge(where, guard string, guardArgs map[string]any, target any, actions []actionDoc, fallback any) (machine.Edge[component.BehaviorState, C], uint8) {
	c.onEdge, c.heard = true, 0
	e := machine.Edge[component.BehaviorState, C]{
		Guard:    c.guard(where, guard, guardArgs),
		Do:       c.effects(where+": actions", actions),
		To:       c.targets(where+": target", target),
		Fallback: c.fallback(where+": fallback", fallback),
	}
	c.onEdge = false
	if heard, word := c.heard, c.v.signals; heard != 0 {
		e.Do = sequence([]func(C){func(ctx C) { *word(ctx) &^= heard }, e.Do})
	}
	return e, c.heard
}

// signalled builds the guard that passes while a signal of the program's is raised,
// and notes it for the edge compiling
func (c *compiler[C]) signalled(args map[string]any) (func(C) bool, error) {
	a, err := dialect.Decode[signalArgs](args)
	if err != nil {
		return nil, err
	}
	i := slices.Index(c.signals, a.Name)
	switch {
	case i < 0 && len(c.signals) == 0:
		return nil, fmt.Errorf("signal %q: the program declares no signals", a.Name)
	case i < 0:
		return nil, fmt.Errorf("signal %q is not one of %s", a.Name, strings.Join(c.signals, ", "))
	case !c.onEdge:
		return nil, errors.New("only an edge takes a signal")
	}
	bit, word := uint8(1)<<i, c.v.signals
	c.heard |= bit
	return func(ctx C) bool { return *word(ctx)&bit != 0 }, nil
}

// targets reads a state name, or a list of names and candidate tables
func (c *compiler[C]) targets(where string, raw any) []machine.Target[component.BehaviorState, C] {
	list, ok := raw.([]any)
	switch {
	case raw == nil:
		c.fail(where, errors.New("required"))
		return nil
	case !ok:
		list = []any{raw}
	case len(list) == 0:
		c.fail(where, errors.New("lists no state"))
	}
	out := make([]machine.Target[component.BehaviorState, C], 0, len(list))
	for i, item := range list {
		at := where
		if ok {
			at = fmt.Sprintf("%s[%d]", where, i)
		}
		var d candidateDoc
		switch v := item.(type) {
		case string:
			d.State = v
		case map[string]any:
			if err := dialect.DecodeInto(v, &d); err != nil {
				c.fail(at, err)
				continue
			}
		default:
			c.fail(at, fmt.Errorf("want a state name or { state, guard, guard_args, duration_ms }, got %T", item))
			continue
		}
		id, known := c.ids[d.State]
		if !known {
			c.fail(at, c.unknownState(d.State))
			continue
		}
		out = append(out, machine.Target[component.BehaviorState, C]{
			State:    id,
			Guard:    c.guard(at, d.Guard, d.GuardArgs),
			Duration: time.Duration(d.DurationMs) * time.Millisecond,
		})
	}
	return out
}

func (c *compiler[C]) fallback(where string, raw any) machine.Fallback {
	switch f := raw.(type) {
	case nil:
		return machine.Fallback{}
	case string:
		if f == "hold" {
			return machine.Hold
		}
		c.fail(where, fmt.Errorf(`%q is not "hold" or { retry_ms = n }`, f))
	case map[string]any:
		r, err := dialect.Decode[retryDoc](f)
		if err != nil {
			c.fail(where, err)
			return machine.Fallback{}
		}
		return machine.Retry(time.Duration(r.RetryMs) * time.Millisecond)
	default:
		c.fail(where, fmt.Errorf(`want "hold" or { retry_ms = n }, got %T`, raw))
	}
	return machine.Fallback{}
}

// guard builds a guard tree over the vocabulary's guards; no name is no guard
func (c *compiler[C]) guard(where, name string, args map[string]any) func(C) bool {
	if name == "" {
		if args != nil {
			c.fail(where, errors.New("guard_args without guard"))
		}
		return nil
	}
	g, err := dialect.Tree[func(C) bool]{
		Leaf: func(name string) (func(map[string]any) (func(C) bool, error), bool) {
			if name == signalled && c.v.signals != nil {
				return c.signalled, true
			}
			vb, ok := c.v.verbs[name]
			if !ok {
				return nil, false
			}
			return func(args map[string]any) (func(C) bool, error) {
				if vb.kind != dialect.KindGuard {
					return nil, fmt.Errorf("%s verb, not a guard", vb.kind)
				}
				return vb.test(args, c.site())
			}, true
		},
		All: allOf[C],
		Any: anyOf[C],
		Not: not[C],
	}.Build(name, args)
	if err != nil {
		c.fail(where, err)
	}
	return g
}

func (c *compiler[C]) probe(where, name string, args map[string]any) func(C) bool {
	vb, ok := c.v.verbs[name]
	switch {
	case !ok:
		c.fail(where, fmt.Errorf("unknown probe '%s'", name))
	case vb.kind != dialect.KindProbe:
		c.fail(where, fmt.Errorf("'%s' is a %s verb, not a probe", name, vb.kind))
	default:
		p, err := vb.test(args, c.site())
		if err != nil {
			c.fail(where, fmt.Errorf("probe '%s': %w", name, err))
		}
		return p
	}
	return nil
}

// site numbers the program's guards and probes in load order
func (c *compiler[C]) site() int {
	c.sites++
	return c.sites - 1
}

// effects compiles one-shot actions into one hook running them in order
func (c *compiler[C]) effects(where string, docs []actionDoc) func(C) {
	fs := make([]func(C), 0, len(docs))
	for i, d := range docs {
		fs = append(fs, c.effect(fmt.Sprintf("%s[%d]", where, i), d))
	}
	return sequence(fs)
}

// tick compiles a state's update: its sustained verbs, then the host's movement
func (c *compiler[C]) tick(where string, docs []actionDoc) func(C) {
	fs := make([]func(C), 0, len(docs)+1)
	for i, d := range docs {
		fs = append(fs, c.action(fmt.Sprintf("%s[%d]", where, i), d, dialect.KindSustained))
	}
	return sequence(append(fs, c.v.integrate))
}

func (c *compiler[C]) effect(where string, d actionDoc) func(C) {
	return c.action(where, d, dialect.KindAction)
}

// action compiles one action table into the verb of kind it names, refusing a key
// of its table the verb does not read
func (c *compiler[C]) action(where string, d actionDoc, kind string) func(C) {
	vb, ok := c.v.verbs[d.Action]
	switch {
	case d.Action == "":
		c.fail(where, errors.New("action is required"))
	case !ok && kind == dialect.KindSustained:
		c.fail(where, fmt.Errorf("unknown sustained action '%s'", d.Action))
	case !ok:
		c.fail(where, fmt.Errorf("unknown effect '%s'", d.Action))
	case vb.kind == dialect.KindAction && kind == dialect.KindSustained:
		c.fail(where, fmt.Errorf("action '%s' is an effect, which on_update would run every tick; use on_enter, a cue or an edge", d.Action))
	case vb.kind == dialect.KindSustained && kind == dialect.KindAction:
		c.fail(where, fmt.Errorf("action '%s' is sustained, which only on_update runs", d.Action))
	case vb.kind != kind:
		c.fail(where, fmt.Errorf("'%s' is a %s verb, not an action", d.Action, vb.kind))
	case d.Event != "" && !slices.Contains(vb.keys, "event"):
		c.fail(where, fmt.Errorf("action '%s' takes no event", d.Action))
	case d.Payload != nil && !slices.Contains(vb.keys, "payload"):
		c.fail(where, fmt.Errorf("action '%s' takes no payload", d.Action))
	default:
		f, err := vb.apply(c.payloads, d.Event, d.Payload)
		if err != nil {
			c.fail(where, fmt.Errorf("action '%s': %w", d.Action, err))
		}
		return f
	}
	return nil
}

func (c *compiler[C]) unknownState(name string) error {
	if name == "" {
		return errors.New("names no state")
	}
	return fmt.Errorf("unknown state '%s'", name)
}

// sequence runs hooks in order, skipping none; nil when there is nothing to run
func sequence[C any](fs []func(C)) func(C) {
	fs = slices.DeleteFunc(fs, func(f func(C)) bool { return f == nil })
	switch len(fs) {
	case 0:
		return nil
	case 1:
		return fs[0]
	}
	return func(c C) {
		for _, f := range fs {
			f(c)
		}
	}
}

// allOf, anyOf and not are And, Or and Not over guards; And and Or of no guards
// both pass, as the scenario dialect's do
func allOf[C any](gs []func(C) bool) func(C) bool {
	return func(c C) bool {
		for _, g := range gs {
			if !g(c) {
				return false
			}
		}
		return true
	}
}

func anyOf[C any](gs []func(C) bool) func(C) bool {
	return func(c C) bool {
		for _, g := range gs {
			if g(c) {
				return true
			}
		}
		return len(gs) == 0
	}
}

func not[C any](g func(C) bool) func(C) bool { return func(c C) bool { return !g(c) } }
