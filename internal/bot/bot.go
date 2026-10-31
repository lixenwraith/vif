// Package bot drives one participant from an internal/fsm graph instead of a
// terminal. The graph reads its own instance's world and queues intents, which a
// Driver sends through that instance's mode router at the graph's rate, so a bot
// is bound by everything that binds a person. See doc/bots.md.
package bot

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/toml/macro"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/fsm/std"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/pkg/vmath"
)

const (
	// DefaultActionsPerSecond is a graph's intent rate when its [bot] table names none
	DefaultActionsPerSecond = 12

	// queueMax bounds the intents waiting on the rate; one that finds it full is dropped
	queueMax = 256
)

// Settings is a graph's [bot] table.
type Settings struct {
	ActionsPerSecond int `toml:"actions_per_second"`
}

// Graph is one parsed bot document: its settings and the FSM graph, which parsing
// compiles once so a broken reference fails at load rather than in play.
type Graph struct {
	Name     string
	Settings Settings
	doc      map[string]any
}

// ParseGraph parses and validates one bot document. A startup graph parses before
// any App has built the event catalogue its triggers name.
func ParseGraph(name string, data []byte) (*Graph, error) {
	event.EnsureRegistry()
	doc, err := toml.NewParser(data).Parse()
	if err != nil {
		return nil, fmt.Errorf("bot %s: %w", name, err)
	}
	if doc, err = macro.ExpandDocument(doc); err != nil {
		return nil, fmt.Errorf("bot %s: %w", name, err)
	}
	g := &Graph{Name: name, Settings: Settings{ActionsPerSecond: DefaultActionsPerSecond}, doc: doc}
	if err := g.validate(); err != nil {
		return nil, fmt.Errorf("bot %s: %w", name, err)
	}
	if _, err := g.machine(); err != nil {
		return nil, fmt.Errorf("bot %s: %w", name, err)
	}
	return g, nil
}

// validate checks what the FSM loader does not: the document's own tables and the
// one-file rule, since a bot graph resolves by name with no directory around it.
func (g *Graph) validate() error {
	if key := unknownKey(g.doc, "bot", "regions", "states"); key != "" {
		return fmt.Errorf("unknown table %q", key)
	}
	if err := dialect.DecodeInto(g.doc["bot"], &g.Settings); err != nil {
		return fmt.Errorf("[bot]: %w", err)
	}
	if g.Settings.ActionsPerSecond <= 0 {
		return fmt.Errorf("actions_per_second must be positive, got %d", g.Settings.ActionsPerSecond)
	}
	regions, _ := g.doc["regions"].(map[string]any)
	for name, raw := range regions {
		region, _ := raw.(map[string]any)
		if region["file"] != nil {
			return fmt.Errorf("region %q names a file; a bot graph is one document", name)
		}
		// System toggles would write the world, and background only shapes game telemetry
		if key := unknownKey(region, "initial"); key != "" {
			return fmt.Errorf("region %q: %q does nothing in a bot graph", name, key)
		}
	}
	return nil
}

// machine compiles a fresh machine over the graph, the vocabulary registered first
// so the loader resolves every name against it.
func (g *Graph) machine() (*fsm.Machine[*mind], error) {
	m := fsm.NewMachine[*mind]()
	register(m)
	if err := m.LoadScenarioFromMap(g.doc); err != nil {
		return nil, err
	}
	return m, nil
}

// Vocabulary lists the actions and guards a graph resolves, for -schema
func Vocabulary() []dialect.Entry {
	m := fsm.NewMachine[*mind]()
	register(m)
	return m.Vocabulary()
}

// unknownKey returns the first key, in sorted order, that allowed does not name.
func unknownKey(table map[string]any, allowed ...string) string {
	keys := make([]string, 0, len(table))
	for key := range table {
		if !slices.Contains(allowed, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// Instance is the surface of the participant a driver plays: the same calls an
// authored script and the fuzz driver make, the tap its dispatch reports through,
// and the switch that has each key it presses hold until the driver lets go of it.
type Instance interface {
	Tick(int)
	Inject(...*input.Intent) bool
	InputTick() bool
	SetDispatchTap(func(event.GameEvent))
	ReportReleases()
}

// Stats reports one bot's progress.
type Stats struct {
	Ticks    uint64
	Injected uint64
	Dropped  uint64
	Fitness  int64
	State    string // each region's active state, region:state, space separated
}

// fitness is indexed by row: an event the bot's own step dispatched, the payload
// field that must name its cursor (empty for any), a bool payload field that must
// hold (empty for none), and the weight. A correct character is the boost it earns
// and an error the boost it ends; a predicted death counts once confirmed.
var fitness = [...]struct {
	event  event.EventType
	owner  string
	when   string
	weight int64
}{
	{event.EventBoostReward, "entity", "", 1},
	{event.EventBoostDeactivate, "entity", "", -1},
	{event.EventSpeciesKilled, "killer_entity", "", 5},
	{event.EventSpeciesKillConfirmed, "killer_entity", "", 5},
	{event.EventGoldCompleted, "entity", "", 25},
	{event.EventCursorDefeatState, "entity", "defeated", -100},
}

// Driver plays one graph on one instance, a tick at a time.
type Driver struct {
	inst    Instance
	ctx     *engine.GameContext
	machine *fsm.Machine[*mind]
	regions []string
	mind    mind
	queue   intentQueue
	held    []keyHold // the keys the bot holds down, in the order it pressed them
	rate    int64     // intents per second
	credit  int64     // intents owed, in thousandths
	started bool
	stats   Stats

	// seen is what the instance dispatched during the last step that the graph
	// reacts to, in order, and fitness what all it dispatched was worth; both are
	// written inside dispatch and read under the world lock. observing is set only
	// while the driver itself plays.
	seen      []event.GameEvent
	fitness   int64
	observing atomic.Bool
}

// NewDriver builds a driver for one instance. The graph's draws come from its own
// stream, named for the participant so two bots in one session draw apart.
func NewDriver(inst Instance, ctx *engine.GameContext, g *Graph, seed uint64, participant uint32) (*Driver, error) {
	m, err := g.machine()
	if err != nil {
		return nil, fmt.Errorf("bot %s: %w", g.Name, err)
	}
	d := &Driver{inst: inst, ctx: ctx, machine: m, regions: m.DeclaredRegions(),
		rate: int64(g.Settings.ActionsPerSecond)}
	d.mind = mind{ctx: ctx, queue: &d.queue,
		rng: vmath.NewSeededRand(seed, "bot."+strconv.FormatUint(uint64(participant), 10))}
	inst.SetDispatchTap(d.observe)
	inst.ReportReleases()
	return d, nil
}

// observe weighs an event and keeps a copy of one the graph reacts to, as dispatch
// recycles a pooled payload; what an operator drives while the graph is suspended
// is not the bot's.
func (d *Driver) observe(ev event.GameEvent) {
	if !d.observing.Load() {
		return
	}
	if ev.Phase != event.PhasePredicted {
		for _, row := range fitness {
			if row.event == ev.Type && (row.owner == "" || d.mind.owns(ev.Payload, row.owner)) &&
				(row.when == "" || payloadTrue(ev.Payload, row.when)) {
				d.fitness += row.weight
			}
		}
	}
	if d.machine.Reacts(ev.Type) {
		ev.Payload = event.ClonePayload(ev.Payload)
		d.seen = append(d.seen, ev)
	}
}

func payloadTrue(payload any, field string) bool {
	v, ok := std.ExtractBoolField(payload, field)
	return ok && v
}

// Step updates the graph at the completed tick, after the events the last step
// dispatched, then lets go of the keys whose holds are up, sends what the rate
// allows, advances input-driven work and ticks once. False means an intent quit the
// game. Credit left idle is capped at one second's worth, so a quiet bot bursts no
// further.
func (d *Driver) Step() (bool, error) {
	tick := d.ctx.World.TickInterval()
	d.credit = min(d.credit+d.rate*tick.Milliseconds(), d.rate*1000)
	var err error
	d.ctx.World.RunSafe(func() {
		d.mind.fresh = [targetCount]bool{}
		d.mind.room = int(d.credit/1000) - d.queue.n
		if !d.started {
			d.started = true
			err = d.machine.Init(&d.mind)
			return
		}
		for _, ev := range d.seen {
			d.machine.HandleEvent(&d.mind, ev)
		}
		clear(d.seen)
		d.seen = d.seen[:0]
		d.machine.Update(&d.mind, tick)
	})
	if err != nil {
		return false, err
	}
	d.observing.Store(true)
	defer d.observing.Store(false)
	if !d.expire() || !d.inject() || !d.inst.InputTick() {
		return false, nil
	}
	d.inst.Tick(1)
	d.stats.Ticks++
	return true, nil
}

// inject sends queued intents in order, as many as the rate has earned.
func (d *Driver) inject() bool {
	for d.credit >= 1000 {
		q, ok := d.queue.pop()
		if !ok {
			break
		}
		d.credit -= 1000
		if !d.send(q) {
			return false
		}
		d.stats.Injected++
	}
	return true
}

// send injects one queued intent. The bot lets go of every key it presses: a tap at
// once, a hold when its ticks are up or at its key's release, and a key it holds
// before pressing it again, as a keyboard cannot press a key that is down. Those
// releases cost no rate; a release the graph queues does.
func (d *Driver) send(q queued) bool {
	switch q.intent.Type {
	case input.IntentKeyRelease:
		return d.letGo(q.intent.Char)
	case input.IntentKeyPress:
		key := q.intent.Char
		if !d.letGo(key) || !d.inst.Inject(&q.intent) {
			return false
		}
		due := uint64(math.MaxUint64)
		if q.hold != holdOpen {
			due = d.stats.Ticks + uint64(q.hold)
		}
		d.held = append(d.held, keyHold{key: key, due: due})
		if q.hold == 0 {
			return d.letGo(key)
		}
		return true
	}
	return d.inst.Inject(&q.intent)
}

// keyHold is a key the bot holds down and the step it lets go at
type keyHold struct {
	key rune
	due uint64
}

// expire lets go of each key whose hold is up, in the order the bot pressed them.
func (d *Driver) expire() bool {
	for i := 0; i < len(d.held); {
		if d.held[i].due > d.stats.Ticks {
			i++
			continue
		}
		if !d.unhold(i) {
			return false
		}
	}
	return true
}

// letGo lets go of key if the bot holds it.
func (d *Driver) letGo(key rune) bool {
	if i := slices.IndexFunc(d.held, func(h keyHold) bool { return h.key == key }); i >= 0 {
		return d.unhold(i)
	}
	return true
}

// unhold injects the release of the i-th held key. A hold the router has already
// ended, as play stopped, finds nothing to release there.
func (d *Driver) unhold(i int) bool {
	key := d.held[i].key
	d.held = slices.Delete(d.held, i, i+1)
	return d.inst.Inject(&input.Intent{Type: input.IntentKeyRelease, Char: key, Count: 1})
}

// Stats returns the bot's progress and the state each region is in.
func (d *Driver) Stats() Stats {
	st := d.stats
	st.Dropped = d.queue.dropped
	states := make([]string, 0, len(d.regions))
	d.ctx.World.RunSafe(func() {
		st.Fitness = d.fitness
		for _, name := range d.regions {
			if r := d.machine.RegionTelemetry(name); r.Active {
				states = append(states, name+":"+r.State)
			}
		}
	})
	st.State = strings.Join(states, " ")
	return st
}

// queued is an intent waiting on the rate, and for a press, how long its key stays
// down: ticks, holdOpen until the graph releases it, or zero for a tap
type queued struct {
	intent input.Intent
	hold   int
}

// holdOpen holds a pressed key until the graph releases it
const holdOpen = -1

// intentQueue is a bounded FIFO of intents waiting on the rate.
type intentQueue struct {
	buf     [queueMax]queued
	head, n int
	dropped uint64
}

func (q *intentQueue) push(item queued) {
	if q.n == queueMax {
		q.dropped++
		return
	}
	q.buf[(q.head+q.n)%queueMax] = item
	q.n++
}

func (q *intentQueue) pop() (queued, bool) {
	if q.n == 0 {
		return queued{}, false
	}
	item := q.buf[q.head]
	q.head = (q.head + 1) % queueMax
	q.n--
	return item, true
}
