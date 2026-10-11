// Package bot drives one participant from an internal/fsm graph instead of a
// terminal. The graph reads its own instance's world and queues intents, which a
// Driver releases through that instance's mode router at the graph's rate, so a
// bot is bound by everything that binds a person. See doc/todo-bots.md.
package bot

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
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

// ParseGraph parses and validates one bot document.
func ParseGraph(name string, data []byte) (*Graph, error) {
	doc, err := toml.NewParser(data).Parse()
	if err != nil {
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
	if raw, ok := g.doc["bot"]; ok {
		table, ok := raw.(map[string]any)
		if !ok {
			return errors.New("[bot] must be a table")
		}
		if key := unknownKey(table, "actions_per_second"); key != "" {
			return fmt.Errorf("unknown [bot] setting %q", key)
		}
		if err := toml.Decode(table, &g.Settings); err != nil {
			return fmt.Errorf("[bot]: %w", err)
		}
		if g.Settings.ActionsPerSecond <= 0 {
			return fmt.Errorf("actions_per_second must be positive, got %d", g.Settings.ActionsPerSecond)
		}
	}
	regions, _ := g.doc["regions"].(map[string]any)
	for name, raw := range regions {
		if region, ok := raw.(map[string]any); ok && region["file"] != nil {
			return fmt.Errorf("region %q names a file; a bot graph is one document", name)
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
// authored script and the fuzz driver make.
type Instance interface {
	Tick(int)
	Inject(...*input.Intent) bool
	InputTick() bool
}

// Stats reports one bot's progress.
type Stats struct {
	Ticks    uint64
	Injected uint64
	Dropped  uint64
	State    string // each region's active state, region:state, space separated
}

// Driver plays one graph on one instance, a tick at a time.
type Driver struct {
	inst    Instance
	ctx     *engine.GameContext
	machine *fsm.Machine[*mind]
	regions []string
	mind    mind
	queue   intentQueue
	rate    int64 // intents per second
	credit  int64 // intents owed, in thousandths
	started bool
	stats   Stats
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
	return d, nil
}

// Step updates the graph at the completed tick, releases what the rate allows,
// advances input-driven work and ticks once. False means an intent quit the game.
// Credit left idle is capped at one second's worth, so a quiet bot bursts no further.
func (d *Driver) Step() (bool, error) {
	d.credit = min(d.credit+d.rate*parameter.GameUpdateInterval.Milliseconds(), d.rate*1000)
	var err error
	d.ctx.World.RunSafe(func() {
		d.mind.fresh = [targetCount]bool{}
		d.mind.room = int(d.credit/1000) - d.queue.n
		if !d.started {
			d.started = true
			err = d.machine.Init(&d.mind)
			return
		}
		d.machine.Update(&d.mind, parameter.GameUpdateInterval)
	})
	if err != nil {
		return false, err
	}
	if !d.release() || !d.inst.InputTick() {
		return false, nil
	}
	d.inst.Tick(1)
	d.stats.Ticks++
	return true, nil
}

// release injects queued intents in order, as many as the rate has earned.
func (d *Driver) release() bool {
	for d.credit >= 1000 {
		intent, ok := d.queue.pop()
		if !ok {
			break
		}
		d.credit -= 1000
		if !d.inst.Inject(&intent) {
			return false
		}
		d.stats.Injected++
	}
	return true
}

// Stats returns the bot's progress and the state each region is in.
func (d *Driver) Stats() Stats {
	st := d.stats
	st.Dropped = d.queue.dropped
	states := make([]string, 0, len(d.regions))
	d.ctx.World.RunSafe(func() {
		for _, name := range d.regions {
			if r := d.machine.RegionTelemetry(name); r.Active {
				states = append(states, name+":"+r.State)
			}
		}
	})
	st.State = strings.Join(states, " ")
	return st
}

// intentQueue is a bounded FIFO of intents waiting on the rate.
type intentQueue struct {
	buf     [queueMax]input.Intent
	head, n int
	dropped uint64
}

func (q *intentQueue) push(intent input.Intent) {
	if q.n == queueMax {
		q.dropped++
		return
	}
	q.buf[(q.head+q.n)%queueMax] = intent
	q.n++
}

func (q *intentQueue) pop() (input.Intent, bool) {
	if q.n == 0 {
		return input.Intent{}, false
	}
	intent := q.buf[q.head]
	q.head = (q.head + 1) % queueMax
	q.n--
	return intent, true
}
