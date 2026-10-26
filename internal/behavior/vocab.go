package behavior

import (
	"fmt"
	"maps"
	"slices"

	"github.com/lixenwraith/vif/internal/fsm/dialect"
)

// Vocabulary is what a program compiles against: its host's verbs over the context
// C each step fills, the flags its states raise, and the movement every tick ends
// with. Built once, it serves every program; Compile only reads it.
type Vocabulary[C any] struct {
	verbs     map[string]verb[C]
	flags     map[string]uint64
	integrate func(C)
}

// verb is one registered name: its kind, the keys of its table it reads beside
// action, the args -schema lists, and the build that decodes them once at load. A
// test's build takes its site, the compiler's count of its program's guards and
// probes in load order.
type verb[C any] struct {
	kind  string
	keys  []string
	args  []dialect.Field
	test  func(args map[string]any, site int) (func(C) bool, error)
	apply func(event string, payload any) (func(C), error)
}

func NewVocabulary[C any]() *Vocabulary[C] {
	return &Vocabulary[C]{verbs: make(map[string]verb[C]), flags: make(map[string]uint64)}
}

// Predicate registers a read-only guard whose guard_args decode strictly into A
func (v *Vocabulary[C]) Predicate[A any](name string, build func(*A) (func(C) bool, error)) {
	v.Seeded(name, func(a *A, _ int) (func(C) bool, error) { return build(a) })
}

// Seeded registers a guard whose build also takes its site, so two seeded draws
// of one program in one step stay apart while every peer draws alike
func (v *Vocabulary[C]) Seeded[A any](name string, build func(a *A, site int) (func(C) bool, error)) {
	v.add(name, verb[C]{kind: dialect.KindGuard, args: dialect.Fields[A](), test: decoded(build)})
}

// Probe registers a fallible entry: it may stash what it resolved in C, and a
// state it guards is not entered when it fails
func (v *Vocabulary[C]) Probe[A any](name string, build func(*A) (func(C) bool, error)) {
	test := decoded(func(a *A, _ int) (func(C) bool, error) { return build(a) })
	v.add(name, verb[C]{kind: dialect.KindProbe, args: dialect.Fields[A](), test: test})
}

// Effect registers a one-shot action for an enter, exit, cue or edge, its payload
// decoded strictly into A
func (v *Vocabulary[C]) Effect[A any](name string, build func(*A) (func(C), error)) {
	v.add(name, verb[C]{kind: dialect.KindAction, keys: payloadKey[A](), args: dialect.Fields[A](), apply: payload(build)})
}

// Sustained registers an action a state's update runs on every tick it holds
func (v *Vocabulary[C]) Sustained[A any](name string, build func(*A) (func(C), error)) {
	v.add(name, verb[C]{kind: dialect.KindSustained, keys: payloadKey[A](), args: dialect.Fields[A](), apply: payload(build)})
}

// Emit registers an effect that reads its table's event beside its payload, as
// the scenario dialect's EmitEvent does; build checks both
func (v *Vocabulary[C]) Emit(name string, build func(event string, payload any) (func(C), error)) {
	v.add(name, verb[C]{kind: dialect.KindAction, keys: []string{"event", "payload"}, apply: build})
}

func (v *Vocabulary[C]) Flag(name string, bit uint64) {
	if _, ok := v.flags[name]; ok {
		panic(fmt.Sprintf("behavior: flag %q registered twice", name))
	}
	v.flags[name] = bit
}

// Integrate sets the movement every state's tick ends with, after its sustained
// verbs, so a post-tick edge reads what this step's move did
func (v *Vocabulary[C]) Integrate(f func(C)) { v.integrate = f }

// Entries lists the vocabulary for -schema: And, Or and Not, every verb, every flag
func (v *Vocabulary[C]) Entries() []dialect.Entry {
	out := dialect.TreeEntries()
	for name, vb := range v.verbs {
		out = append(out, dialect.Entry{Name: name, Kind: vb.kind, Keys: vb.keys, Args: vb.args})
	}
	for name := range v.flags {
		out = append(out, dialect.Entry{Name: name, Kind: dialect.KindFlag})
	}
	dialect.Sort(out)
	return out
}

func (v *Vocabulary[C]) add(name string, vb verb[C]) {
	if _, ok := v.verbs[name]; ok {
		panic(fmt.Sprintf("behavior: verb %q registered twice", name))
	}
	v.verbs[name] = vb
}

// flagNames lists the registered flags for an error naming what a program may use
func (v *Vocabulary[C]) flagNames() []string { return slices.Sorted(maps.Keys(v.flags)) }

func decoded[A, R any](build func(*A, int) (R, error)) func(map[string]any, int) (R, error) {
	return func(args map[string]any, site int) (R, error) {
		a, err := dialect.Decode[A](args)
		if err != nil {
			var zero R
			return zero, err
		}
		return build(a, site)
	}
}

// payload adapts a build over A to an action table, which holds no event
func payload[A, R any](build func(*A) (R, error)) func(string, any) (R, error) {
	return func(_ string, raw any) (R, error) {
		a, err := dialect.Decode[A](raw)
		if err != nil {
			var zero R
			return zero, fmt.Errorf("payload: %w", err)
		}
		return build(a)
	}
}

// payloadKey is the table key an action over A reads: payload, or none when A
// takes no args
func payloadKey[A any]() []string {
	if dialect.Fields[A]() == nil {
		return nil
	}
	return []string{"payload"}
}
