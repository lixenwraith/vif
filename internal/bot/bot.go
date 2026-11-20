// Package bot drives one participant from an internal/fsm graph instead of a
// terminal. The graph reads its own instance's world and queues intents, which a
// Driver sends through that instance's mode router at the graph's rate, so a bot
// is bound by everything that binds a person. See doc/bots.md.
package bot

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/toml/macro"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/dialect"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/pkg/vmath"
)

const (
	// defaultActionsPerSecond is a graph's intent rate when its [bot] table names none
	defaultActionsPerSecond = 12

	// queueMax bounds the intents waiting on the rate; one that finds it full is dropped
	queueMax = 256
)

// Settings is a graph's [bot] table. AutoFire is the auto-fire its run starts each
// game on, off, main or on as [play] spells them, the shipped one when empty.
// Learn has a solo run evolve the weights of the graph's networks as its rounds
// end, and any run field the best its stores hold; RoundSeconds is the game time
// after which such a run ends a round the game has not, none when zero.
type Settings struct {
	ActionsPerSecond int    `toml:"actions_per_second"`
	AutoFire         string `toml:"auto_fire"`
	Learn            bool   `toml:"learn"`
	RoundSeconds     int    `toml:"round_seconds"`
	// Inputs are the sensors every network of the graph reads ahead of its own
	Inputs []string `toml:"inputs"`
	// Score is what pays the bot: a source of the catalogue's and what one of it
	// pays. A graph with none scores nothing and may not learn.
	Score map[string]float64 `toml:"score"`
}

// Graph is one parsed bot document: its settings, the networks it declares, in
// name order, and the FSM graph, which parsing compiles once so a broken reference
// fails at load rather than in play.
type Graph struct {
	Name     string
	Settings Settings
	nets     []*network
	text     []byte // the document as written, which Document writes weights into
	doc      map[string]any
	pays     []paying // what its score pays, and scoredBy how the score is spelt
	scoredBy string
}

// Networks names the graph's networks, in the order a list of them takes
func (g *Graph) Networks() []string {
	names := make([]string, len(g.nets))
	for i, n := range g.nets {
		names[i] = n.name
	}
	return names
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
	g := &Graph{Name: name, Settings: Settings{ActionsPerSecond: defaultActionsPerSecond}, text: data, doc: doc}
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
	if key := unknownKey(g.doc, "bot", "network", "regions", "states"); key != "" {
		return fmt.Errorf("unknown table %q", key)
	}
	if err := dialect.DecodeInto(g.doc["bot"], &g.Settings); err != nil {
		return fmt.Errorf("[bot]: %w", err)
	}
	if g.Settings.ActionsPerSecond <= 0 {
		return fmt.Errorf("actions_per_second must be positive, got %d", g.Settings.ActionsPerSecond)
	}
	if word := g.Settings.AutoFire; word != "" && !slices.Contains(engine.AutoFireWords[:], word) {
		return fmt.Errorf("auto_fire %q is not off, main or on", word)
	}
	if g.Settings.RoundSeconds < 0 {
		return fmt.Errorf("round_seconds must not be negative, got %d", g.Settings.RoundSeconds)
	}
	var err error
	if g.nets, err = parseNetworks(g.doc["network"], g.Settings.Inputs); err != nil {
		return fmt.Errorf("[network]: %w", err)
	}
	if g.Settings.Learn && len(g.nets) == 0 {
		return errors.New("learn needs a [network.<name>] whose weights to learn")
	}
	if g.pays, g.scoredBy, err = scoring(g.Settings.Score); err != nil {
		return fmt.Errorf("[bot] score: %w", err)
	}
	if g.Settings.Learn && len(g.pays) == 0 {
		return errors.New("learn needs a [bot] score: what a round pays its networks for")
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

// machine compiles a fresh machine over the graph, the vocabulary registered
// first so the loader resolves every name against it
func (g *Graph) machine() (*fsm.Machine[*mind], error) {
	m := fsm.NewMachine[*mind]()
	register(m, g.nets)
	if err := m.LoadScenarioFromMap(g.doc); err != nil {
		return nil, err
	}
	return m, nil
}

// Vocabulary lists the actions and guards a graph resolves, for -schema
func Vocabulary() []dialect.Entry {
	m := fsm.NewMachine[*mind]()
	register(m, nil)
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

// Stats reports one bot's progress. A round is a game, from its start to the new
// game that ended it, whoever asked for that. Games is how many games played at
// once it sums, zero for a driver's own.
type Stats struct {
	Games    int
	Ticks    uint64
	Injected uint64
	Dropped  uint64
	Rounds   uint64
	Score    float64 // what the graph's score has paid, the round in play included
	State    string  // each region's active state, region:state, space separated

	// Learn is how far each of the graph's learners is, in its networks' order;
	// none for a driver that plays the graph's own weights
	Learn []LearnStats
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
	// reacts to, in order, written inside dispatch. score is what the graph's score
	// (pays) has paid, written there and as a tick is counted. Both are read under
	// the world lock. observing is set only while the driver itself plays.
	seen      []event.GameEvent
	pays      []paying
	score     float64
	observing atomic.Bool
	// owed is what each death the bot's world predicted pays once it is proved
	owed map[core.Entity]float64

	// The round in play began at roundTick with roundScore paid; ended is how a new
	// game was asked for since, which any dispatch may write.
	roundTick  uint64
	roundScore float64
	ended      atomic.Uint32

	// Each network's learner, nil for none, has a genome play a round: a candidate
	// while the bot plays alone, scored by its network's part of the round
	// (parts), shared being what of score the parts have taken. A round that
	// outlasts limit is one the driver asked the game to end (asked).
	learners []*Learner
	parts    []part
	shared   float64
	learns   bool
	alone    func() bool
	limit    time.Duration
	asked    bool

	// game is the driver's place among those that learn together, and games the
	// order their rounds close in, nil for a driver that learns by itself
	game  int
	games *games

	// Round, when set, is told each round as it closes: its number and its score
	Round func(n uint64, score float64)
}

// part is a network's of the round in play: whether it steered in it, whether
// the ticks are now its own, as the last to steer, and what those that were paid
type part struct {
	steered, holds bool
	score          float64
}

// How the round in play was asked to end: by the game itself, as a defeat does, or
// by anything else, an operator's new game or a session's
const (
	roundOn uint32 = iota
	roundOver
	roundCut
)

// NewDriver builds a driver for one instance. The graph's draws come from its own
// stream, named for the participant so two bots in one session draw apart.
func NewDriver(inst Instance, ctx *engine.GameContext, g *Graph, seed uint64, participant uint32) (*Driver, error) {
	m, err := g.machine()
	if err != nil {
		return nil, fmt.Errorf("bot %s: %w", g.Name, err)
	}
	d := &Driver{inst: inst, ctx: ctx, machine: m, regions: m.DeclaredRegions(), rate: int64(g.Settings.ActionsPerSecond),
		learners: make([]*Learner, len(g.nets)), parts: make([]part, len(g.nets)), pays: g.pays,
		limit: time.Duration(g.Settings.RoundSeconds) * time.Second}
	d.mind = mind{ctx: ctx, queue: &d.queue, steered: make([]bool, len(g.nets)),
		rng: vmath.NewSeededRand(seed, "bot."+strconv.FormatUint(uint64(participant), 10))}
	for _, n := range g.nets {
		d.mind.nets = append(d.mind.nets, n.playing())
	}
	inst.SetDispatchTap(d.observe)
	inst.ReportReleases()
	return d, nil
}

// observe pays for an event and keeps a copy of one the graph reacts to, as
// dispatch recycles a pooled payload; what an operator drives while the graph is
// suspended is not the bot's.
func (d *Driver) observe(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		how := roundOver
		if ev.Origin != event.OriginSystem {
			how = roundCut
		}
		d.ended.Store(how)
	}
	if !d.observing.Load() {
		return
	}
	d.pay(ev)
	if d.machine.Reacts(ev.Type) {
		ev.Payload = event.ClonePayload(ev.Payload)
		d.seen = append(d.seen, ev)
	}
}

// pay adds what the score pays for an event of the bot's own cursor. A death its
// world only predicted is weighed as it is seen, while its entity stands to be
// read, and that held for the event that proves it, which comes once the entity
// is gone.
func (d *Driver) pay(ev event.GameEvent) {
	kind := ev.Type
	if kind == event.EventSpeciesKillConfirmed {
		kind = event.EventSpeciesKilled
	}
	sum, own := 0.0, false
	for _, p := range d.pays {
		if p.row.each != nil || p.row.event != kind || !d.mind.owns(ev.Payload, p.row.owner) {
			continue
		}
		worth := 1.0
		if p.row.worth != nil {
			worth = p.row.worth(&d.mind, ev.Payload)
		}
		sum, own = sum+p.weight*worth, true
	}
	if !own {
		return
	}
	if kill, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
		if held, ok := d.owed[kill.Entity]; ok && ev.Type == event.EventSpeciesKillConfirmed {
			delete(d.owed, kill.Entity)
			sum = held
		} else if ev.Phase == event.PhasePredicted {
			if d.owed == nil {
				d.owed = make(map[core.Entity]float64)
			}
			d.owed[kill.Entity] = sum
		}
	}
	if ev.Phase != event.PhasePredicted {
		d.score += sum
	}
}

// Step updates the graph at the completed tick, after the events the last step
// dispatched, then lets go of the keys whose holds are up, sends what the rate
// allows, advances input-driven work and ticks once. False means an intent quit
// the game, or the games it learns with have ended. Credit left idle is capped
// at one second's worth, so a quiet bot bursts no further.
func (d *Driver) Step() (more bool, err error) {
	if g := d.games; g != nil {
		if d.stats.Ticks >= g.end.Load() {
			return false, nil
		}
		// A game that plays no further holds none of the rest back, and the run's
		// own ends the learning at its tick before a game waiting on it can pass it
		defer func() {
			if !more || err != nil {
				if d.game == 0 {
					d.End(d.stats.Ticks)
				}
				g.reach(d.game, gone)
			}
		}()
	}
	tick := d.ctx.World.TickInterval()
	d.credit = min(d.credit+d.rate*tick.Milliseconds(), d.rate*1000)
	d.ctx.World.RunSafe(func() {
		d.mind.fresh = [targetCount]bool{}
		d.mind.room, d.mind.tick = int(d.credit/1000)-d.queue.n, d.stats.Ticks
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
		// The ticks are the networks' that steered last, until another steers
		if slices.Contains(d.mind.steered, true) {
			for i, steered := range d.mind.steered {
				d.parts[i].holds = steered
				d.parts[i].steered = d.parts[i].steered || steered
			}
			clear(d.mind.steered)
		}
	})
	if err != nil {
		return false, err
	}
	d.observing.Store(true)
	defer d.observing.Store(false)
	if !d.expire() || !d.inject() || !d.inst.InputTick() {
		return false, nil
	}
	// A new game asked for ahead of the tick, by the step's own input or between
	// steps, plays it, so the round closes before the tick is counted
	early := d.ended.Load() != roundOn
	d.inst.Tick(1)
	d.stats.Ticks++
	if !early {
		d.count()
	}
	if how := d.ended.Swap(roundOn); how != roundOn {
		d.closeRound(how == roundOver || d.asked)
		d.asked = false
	} else if over := time.Duration(d.stats.Ticks-d.roundTick) * tick; d.limit > 0 && over >= d.limit && !d.asked && d.learns && d.alone() {
		// A learning round the game will not end is closed as far as it came, by a new game
		d.asked = true
		d.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{}, event.OriginDebug)
	}
	if early {
		d.count()
	}
	if d.games != nil {
		d.games.reach(d.game, d.stats.Ticks+1)
	}
	return true, nil
}

// count pays for what the score counts a tick, and takes that and what the step's
// events paid into the part of each network whose ticks these are
func (d *Driver) count() {
	d.ctx.World.RunSafe(func() {
		for _, p := range d.pays {
			if p.row.each != nil {
				d.score += p.weight * p.row.each(&d.mind)
			}
		}
		gain := d.score - d.shared
		d.shared = d.score
		for i := range d.parts {
			if d.parts[i].holds {
				d.parts[i].score += gain
			}
		}
	})
}

// Learn has each driver play the learners' genomes on its networks, a round each,
// as one of the games that learn from them together, the first the run's own,
// whose end is theirs. While alone reports the bots play by themselves every
// learner plays a candidate, scored by the ticks of the round that are its
// network's. Otherwise, as in a session, each plays what it fields.
func Learn(alone func() bool, learners []*Learner, drivers ...*Driver) {
	var order *games
	if len(drivers) > 1 {
		order = newGames(len(drivers))
	}
	for game, d := range drivers {
		d.alone, d.game, d.games = alone, game, order
		for _, l := range learners {
			if i := slices.IndexFunc(d.mind.nets, func(n *network) bool { return n.name == l.name }); i >= 0 {
				d.learners[i] = l
			}
		}
		d.learns = slices.ContainsFunc(d.learners, func(l *Learner) bool { return l != nil })
		d.deal()
	}
}

// End closes the learning d takes part in at tick: no round a game closes past it
// reaches the learners, and none of the games steps further than it
func (d *Driver) End(tick uint64) {
	if d.games != nil {
		d.games.end.Store(tick)
		d.games.wake()
	}
}

// deal hands each learner's network the genome its next round plays, which alone
// is the candidate it has in flight until a round settles it
func (d *Driver) deal() {
	alone := d.alone()
	for i, l := range d.learners {
		if l != nil {
			d.mind.nets[i].wear(l.take(d.game, alone))
		}
	}
}

// closeRound ends the round a new game closed. Its score is what the graph's
// score paid in it. A learner whose network steered is scored by its part, when
// the game or the run's limit ended the round; one whose network never did keeps
// its candidate for the round that reaches it.
func (d *Driver) closeRound(played bool) {
	var total float64
	d.ctx.World.RunSafe(func() {
		total = d.score
		clear(d.owed) // a prediction is dropped with the game it was made in
	})
	score := total - d.roundScore
	d.roundTick, d.roundScore = d.stats.Ticks, total
	d.stats.Rounds++
	if d.learns && (d.games == nil || d.games.enter(d.game, d.stats.Ticks)) {
		scored := played && d.alone()
		for i, l := range d.learners {
			if l != nil && d.parts[i].steered {
				l.settle(d.game, d.parts[i].score, scored)
			}
		}
		d.deal()
		if d.games != nil {
			d.games.exit()
		}
	}
	clear(d.parts)
	if d.Round != nil {
		d.Round(d.stats.Rounds, score)
	}
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
	for _, l := range d.learners {
		if l != nil {
			st.Learn = append(st.Learn, l.Stats())
		}
	}
	states := make([]string, 0, len(d.regions))
	d.ctx.World.RunSafe(func() {
		st.Score = d.score
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
