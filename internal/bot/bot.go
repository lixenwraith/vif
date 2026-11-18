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
	"github.com/lixenwraith/vif/internal/component"
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
}

// Graph is one parsed bot document: its settings, the networks it declares, in
// name order, and the FSM graph, which parsing compiles once so a broken reference
// fails at load rather than in play.
type Graph struct {
	Name     string
	Settings Settings
	nets     []*network
	doc      map[string]any
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
// so the loader resolves every name against it, and each network's while built
// of that machine's guards, for a Steer that names the network to ask.
func (g *Graph) machine() (*fsm.Machine[*mind], error) {
	m := fsm.NewMachine[*mind]()
	whiles := make([]fsm.GuardFunc[*mind], len(g.nets))
	register(m, g.nets, whiles)
	for i, n := range g.nets {
		if n.while == nil {
			continue
		}
		var err error
		if whiles[i], err = m.Guard(n.while.Name, n.while.Args); err != nil {
			return nil, fmt.Errorf("[network]: %s: while: %w", n.name, err)
		}
	}
	if err := m.LoadScenarioFromMap(g.doc); err != nil {
		return nil, err
	}
	return m, nil
}

// Vocabulary lists the actions and guards a graph resolves, for -schema
func Vocabulary() []dialect.Entry {
	m := fsm.NewMachine[*mind]()
	register(m, nil, nil)
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
// game its own play brought on. Games is how many games played at once it sums,
// zero for a driver's own.
type Stats struct {
	Games    int
	Ticks    uint64
	Injected uint64
	Dropped  uint64
	Fitness  int64
	Rounds   uint64
	Score    float64 // the rounds' scores summed: how far they travelled and what they earned
	State    string  // each region's active state, region:state, space separated

	// Learn is how far each of the graph's learners is, in its networks' order;
	// none for a driver that plays the graph's own weights
	Learn []LearnStats
}

// fitness is indexed by row: an event the bot's own step dispatched, the payload
// field that must name its cursor (empty for any), a bool payload field that must
// hold (empty for none), the weight and how it weighs. A correct character is the
// boost it earns and an error the boost it ends; a predicted death counts once
// confirmed.
var fitness = [...]struct {
	event  event.EventType
	owner  string
	when   string
	weight int64
	weigh  weigh
}{
	{event.EventBoostReward, "entity", "", 1, weighPlain},
	{event.EventBoostDeactivate, "entity", "", -1, weighPlain},
	{event.EventSpeciesKilled, "killer_entity", "", 5, weighKill},
	{event.EventSpeciesKillConfirmed, "killer_entity", "", 5, weighKill},
	{event.EventGoldCompleted, "entity", "", 25, weighPlain},
	{event.EventCursorDefeatState, "entity", "defeated", -100, weighEnd},
	{event.EventCursorCrushed, "entity", "", -100, weighEnd},
}

// weigh is how a fitness row weighs: as its weight says; as a kill, a data species'
// worth its program's hit points instead; or as a defeat, which ends a round and
// is so no part of the round's own score
type weigh uint8

const (
	weighPlain weigh = iota
	weighKill
	weighEnd
)

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

	// earned is fitness without its defeats, and travelled the columns the cave has
	// scrolled under the bot. The round in play began at roundTick with roundEarned
	// and roundTravelled, and downed says its end has been weighed; ended is how a
	// new game was asked for since, which any dispatch may write.
	earned         int64
	travelled      uint64
	roundTick      uint64
	roundEarned    int64
	roundTravelled uint64
	downed         bool
	ended          atomic.Uint32

	// Each network's learner, nil for none, has a genome play a round; while the
	// bot plays alone one of them at a time, explorer, plays a candidate, scored
	// from where its network first steered in the round (from). A round that
	// outlasts limit is one the driver asked the game to end (asked).
	learners []*Learner
	from     []roundMark
	explorer int
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

// roundMark is where in a round a network first steered: what the round had
// earned and how far it had travelled by then
type roundMark struct {
	steered   bool
	earned    int64
	travelled uint64
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
		learners: make([]*Learner, len(g.nets)), from: make([]roundMark, len(g.nets)), explorer: -1,
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

// observe weighs an event and keeps a copy of one the graph reacts to, as dispatch
// recycles a pooled payload; what an operator drives while the graph is suspended
// is not the bot's.
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
	if ev.Phase != event.PhasePredicted {
		for _, row := range fitness {
			if row.event == ev.Type && (row.owner == "" || d.mind.owns(ev.Payload, row.owner)) &&
				(row.when == "" || payloadTrue(ev.Payload, row.when)) {
				weight := row.weight
				switch row.weigh {
				case weighKill:
					weight = d.mind.killWorth(ev.Payload, weight)
				case weighEnd:
					// A round ends once, however often its last hits lift and drop
					// the defeat before the new game, and a crush is a defeat too
					if d.downed {
						continue
					}
					d.downed = true
				}
				d.fitness += weight
				if row.weigh != weighEnd {
					d.earned += weight
				}
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

// krakenPar is the hit points a second a gun kept on a Kraken takes, and bountyFloor
// the least of its bounty a slow kill keeps
const (
	krakenPar   = 4.0
	bountyFloor = 0.25
)

// killWorth is what a kill weighs. A data species' is the hit points its program
// gives it, as what it took to shoot down. A Kraken's is the ticks its hit points
// take at krakenPar, so a fight at that pace earns its own time, and the share of
// them that pace is of the one it died at, bountyFloor at least. Any other's is weight.
func (m *mind) killWorth(payload any, weight int64) int64 {
	p, ok := payload.(*event.SpeciesKilledPayload)
	if !ok {
		return weight
	}
	if i := int(p.Species - component.SpeciesDataFirst); p.Species >= component.SpeciesDataFirst && i < len(m.ctx.World.Resources.Behaviors) {
		return int64(m.ctx.World.Resources.Behaviors[i].HitPoints)
	}
	if k, ok := m.ctx.World.Components.Kraken.GetPtr(p.Entity); ok && p.Species == component.SpeciesKraken && k.Time > 0 {
		par := float64(k.MaxHitPoints) / krakenPar
		return int64(par / m.ctx.World.TickInterval().Seconds() * min(max(par/k.Time, bountyFloor), 1))
	}
	return weight
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
		// A game that plays no further holds none of the rest back
		defer func() {
			if !more || err != nil {
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
		for i, steered := range d.mind.steered {
			if steered && !d.from[i].steered {
				d.from[i] = roundMark{steered: true, earned: d.earned, travelled: d.travelled}
			}
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
	d.inst.Tick(1)
	d.stats.Ticks++
	d.ctx.World.RunSafe(func() { d.travelled += uint64(max(d.ctx.World.Resources.Cave.Steps, 0)) })
	if how := d.ended.Swap(roundOn); how != roundOn {
		d.closeRound(how == roundOver || d.asked)
		d.asked = false
	} else if over := time.Duration(d.stats.Ticks-d.roundTick) * tick; d.limit > 0 && over >= d.limit && !d.asked && d.explorer >= 0 && d.alone() {
		// A learning round the game will not end is closed as far as it came, by a new game
		d.asked = true
		d.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{}, event.OriginDebug)
	}
	if d.games != nil {
		d.games.reach(d.game, d.stats.Ticks+1)
	}
	return true, nil
}

// Learn has each driver play the learners' genomes on its networks, a round each,
// as one of the games that learn from them together. While alone reports the
// bots play by themselves a game's learners explore in turn: one plays a
// candidate and the rest the members they field, so a candidate's score follows
// from its own change. Otherwise, as in a session, each plays what it fields.
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
		d.explorer = slices.IndexFunc(d.learners, func(l *Learner) bool { return l != nil })
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

// deal hands each learner's network the genome its next round plays
func (d *Driver) deal() {
	alone := d.alone()
	for i, l := range d.learners {
		if l != nil {
			d.mind.nets[i].genes = l.take(d.game, alone && i == d.explorer)
		}
	}
}

// closeRound ends the round a new game closed. Its score is how far its cave
// scrolled, which time alone does not move, and what its events earned, a defeat
// aside since one ends every round. The explorer takes the part from where its
// network first steered, when the game or the run's limit ended the round, and its
// turn passes once its candidate is settled, or at once where the round never
// reached it.
func (d *Driver) closeRound(played bool) {
	var earned int64
	d.ctx.World.RunSafe(func() { earned, d.downed = d.earned, false })
	score := float64(d.travelled-d.roundTravelled) + float64(earned-d.roundEarned)
	d.roundTick, d.roundEarned, d.roundTravelled = d.stats.Ticks, earned, d.travelled
	d.stats.Rounds++
	d.stats.Score += score
	if d.explorer >= 0 && (d.games == nil || d.games.enter(d.game, d.stats.Ticks)) {
		from := d.from[d.explorer]
		part := float64(d.travelled-from.travelled) + float64(earned-from.earned)
		if !from.steered || d.learners[d.explorer].settle(d.game, part, played && d.alone()) {
			for d.explorer = (d.explorer + 1) % len(d.learners); d.learners[d.explorer] == nil; {
				d.explorer = (d.explorer + 1) % len(d.learners)
			}
		}
		d.deal()
		if d.games != nil {
			d.games.exit()
		}
	}
	clear(d.from)
	clear(d.mind.steered)
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
