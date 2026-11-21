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
// end; RoundSeconds is the game time after which such a run ends a round the
// game has not, none when zero. What a population kept is fielded either way.
type Settings struct {
	ActionsPerSecond int    `toml:"actions_per_second"`
	AutoFire         string `toml:"auto_fire"`
	Learn            bool   `toml:"learn"`
	RoundSeconds     int    `toml:"round_seconds"`
	// Inputs are the sensors every network of the graph reads ahead of its own
	Inputs []string `toml:"inputs"`
	// Score is what pays the bot: a source of the catalogue's and what one of it
	// pays, or an event by name with the payload field that names its owner. A
	// graph with none scores nothing and may not learn.
	Score map[string]any `toml:"score"`
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
	for _, n := range g.nets {
		if slices.Contains(n.units, "fire") && g.Settings.AutoFire != engine.AutoFireWords[engine.AutoFireOff] {
			return fmt.Errorf("[network.%s]: the output fire is for a graph whose [bot] auto_fire is off: the gun fires by itself otherwise", n.name)
		}
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

	// Learn is how far each of the graph's learners is, in its networks' order, or
	// for a driver that only fields, what each population it read holds; none for
	// one that plays the graph's own weights
	Learn []LearnStats
}

// Fielding says what each population behind the bot fields, a network each with
// its member's score and the rounds behind it, "" with none
func (st Stats) Fielding() string {
	told := make([]string, len(st.Learn))
	for i, l := range st.Learn {
		told[i] = fmt.Sprintf("%s %.0f over %d", l.Network, l.Fielded, l.FieldedRounds)
	}
	return strings.Join(told, ", ")
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

	// The round in play began at roundTick with roundScore paid, and paid is what
	// each of pays has paid in it; ended is how a new game was asked for since,
	// which any dispatch may write.
	roundTick  uint64
	roundScore float64
	paid       []Paid
	ended      atomic.Uint32

	// Each network's learner, nil for none, has a genome play a round: a candidate
	// while the round is the bot's own, scored by its network's part of the round
	// (parts), shared being what of score the parts have taken. alone is set on a
	// run's games (Learn), and own holds while it has reported so since the round
	// began. A round that outlasts limit is one the driver, alone, asked to end (asked).
	learners []*Learner
	kept     []LearnStats // what Field had it play, as its populations stand
	parts    []part
	shared   float64
	alone    func() bool
	own      bool
	limit    time.Duration
	asked    bool

	// game is the driver's place among a run's games, and games the order their
	// rounds close in, nil for a driver that plays by itself
	game  int
	games *games

	// Round, when set, is told each round as it closes: in its turn among a run's
	// games, and none that closes past the run's end
	Round func(Round)
	// Reset asks for the new game that closes a round at the graph's limit, false
	// where none may be asked for. Learn sets the driver's own request where a run
	// sets none: in a session only its authority starts a game, so a run sets what
	// reaches it.
	Reset func() bool
}

// Round is a round as it closed: its number among its driver's, what the graph's
// score paid in it and over how many ticks, how it ended and under which network,
// the last to steer, each network's part of it and what each source paid.
type Round struct {
	N     uint64
	Score float64
	Ticks uint64
	Ended Ending
	Under string
	Parts []Part
	Paid  []Paid
}

// Part is a network's of a round: what the score paid in the ticks that were its own
type Part struct {
	Network string
	Score   float64
}

// Paid is what one source of a graph's score paid in a round, and for how many
type Paid struct {
	Source string
	Count  int
	Score  float64
}

// Ending is how a round ended: by the game's own play, as a defeat does, at the
// limit its graph sets, or cut by anything else, an operator's new game or a
// session's, which scores nothing
type Ending uint8

const (
	EndedByGame Ending = iota
	EndedAtLimit
	EndedCut
)

func (e Ending) String() string {
	return [...]string{"ended by its game", "closed at its limit", "cut short"}[e]
}

// String is the round as a run that tells its rounds prints one, a line: its
// score, its ticks, how it ended and under which network, then each network's
// part where the graph has several, and what each source that paid did
func (r Round) String() string {
	line := fmt.Sprintf("Round %d scored %.0f in %d ticks, %s", r.N, r.Score, r.Ticks, r.Ended)
	if r.Under != "" {
		line += " under " + r.Under
	}
	var parts, paid []string
	if len(r.Parts) > 1 {
		for _, p := range r.Parts {
			parts = append(parts, fmt.Sprintf("%s %.0f", p.Network, p.Score))
		}
	}
	for _, p := range r.Paid {
		if p.Count != 0 || p.Score != 0 {
			paid = append(paid, fmt.Sprintf("%s %.0f over %d", p.Source, p.Score, p.Count))
		}
	}
	told := strings.Join(parts, ", ")
	if len(parts) > 0 && len(paid) > 0 {
		told += "; "
	}
	if told += strings.Join(paid, ", "); told != "" {
		line += ": " + told
	}
	return line
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
		learners: make([]*Learner, len(g.nets)), parts: make([]part, len(g.nets)), pays: g.pays, paid: make([]Paid, len(g.pays)),
		limit: time.Duration(g.Settings.RoundSeconds) * time.Second}
	for i, p := range g.pays {
		d.paid[i].Source = p.row.name
	}
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

// pay adds what the score pays for an event of the bot's own cursor, to the score
// and to its source's tally of the round. A death its world only predicted pays
// nothing yet: it is weighed as it is seen, while its entity stands to be read
// (mind.weigh), and paid by that at the event that proves it, the entity gone.
func (d *Driver) pay(ev event.GameEvent) {
	kind, predicted := ev.Type, ev.Phase == event.PhasePredicted
	kill, _ := ev.Payload.(*event.SpeciesKilledPayload)
	switch {
	case kind == event.EventSpeciesKillConfirmed:
		kind = event.EventSpeciesKilled
	case kill != nil && !predicted:
		delete(d.mind.weighed, kill.Entity) // nothing predicted this one
	}
	own := false
	for i, p := range d.pays {
		if p.row.each != nil || p.row.event != kind || !d.mind.owns(ev.Payload, p.row.owner) {
			continue
		}
		if own = true; predicted {
			continue
		}
		worth := 1.0
		if p.row.worth != nil {
			worth = p.row.worth(&d.mind, ev.Payload)
		}
		if worth != 0 {
			d.score += p.weight * worth
			d.paid[i].Count, d.paid[i].Score = d.paid[i].Count+1, d.paid[i].Score+p.weight*worth
		}
	}
	if kill != nil && own {
		if predicted {
			d.mind.weigh(kill)
		} else {
			delete(d.mind.weighed, kill.Entity)
		}
	}
}

// Step updates the graph at the completed tick, after the events the last step
// dispatched, then lets go of the keys whose holds are up, sends what the rate
// allows, advances input-driven work and ticks once. False means an intent quit
// the game, or the run's games have ended. Credit left idle is capped at one
// second's worth, so a quiet bot bursts no further.
func (d *Driver) Step() (more bool, err error) {
	// A round another cursor shared any tick of is no longer the bot's own
	if d.own && !d.alone() {
		d.own = false
	}
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
		ending := EndedCut
		switch {
		case d.asked:
			ending = EndedAtLimit
		case how == roundOver:
			ending = EndedByGame
		}
		d.closeRound(ending)
		d.asked = false
	} else if over := time.Duration(d.stats.Ticks-d.roundTick) * tick; d.limit > 0 && over >= d.limit && !d.asked && d.alone != nil && d.alone() {
		// A round the game will not end is closed as far as it came, by a new game the
		// bot asks for only while alone: one it shared is closed so, and scores nothing
		d.asked = d.Reset()
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
		for i, p := range d.pays {
			if p.row.each != nil {
				units := p.row.each(&d.mind)
				d.score += p.weight * units
				d.paid[i].Count, d.paid[i].Score = d.paid[i].Count+int(units), d.paid[i].Score+p.weight*units
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

// Field has the network k names play the member its population fields, where one
// is proved, and reports k as that network's progress. It is how a run that does
// not learn plays what its graph was taught, with no learner, and is called
// before the driver's first step.
func (d *Driver) Field(k Kept) {
	i := slices.IndexFunc(d.mind.nets, func(n *network) bool { return n.name == k.Network })
	if i < 0 {
		return
	}
	if n := d.mind.nets[i]; len(k.Fielded) == n.geneCount() {
		n.wear(k.Fielded)
	}
	d.kept = append(d.kept, LearnStats{Network: k.Network, Rounds: k.Rounds, Recent: k.Recent, Fielded: k.Score, FieldedRounds: k.Proved, Kept: true})
}

// Learn has drivers play as a run's games, the first the run's own, whose end is
// theirs: their rounds close in one order and, where alone reports the bot plays
// by itself from a round's start to its end, at the graph's limit. Each learner,
// of none or more, plays a candidate such a round on its network, scored by the
// ticks that are its network's; any other round plays what it fields.
func Learn(alone func() bool, learners []*Learner, drivers ...*Driver) {
	var order *games
	if len(drivers) > 1 {
		order = newGames(len(drivers))
	}
	for game, d := range drivers {
		d.alone, d.game, d.games = alone, game, order
		if d.Reset == nil {
			d.Reset = func() bool {
				d.ctx.PushEventOrigin(event.EventGameResetRequest, &event.GameResetPayload{}, event.OriginDebug)
				return true
			}
		}
		for _, l := range learners {
			if i := slices.IndexFunc(d.mind.nets, func(n *network) bool { return n.name == l.name }); i >= 0 {
				d.learners[i] = l
			}
		}
		d.deal()
	}
}

// End ends the run d is a game of at tick: no round a game closes past it
// reaches the learners or is told, and none of the games steps further than it
func (d *Driver) End(tick uint64) {
	if d.games != nil {
		d.games.end.Store(tick)
		d.games.wake()
	}
}

// deal opens a round, the bot's own where it is alone as it opens, and hands each
// learner's network the genome the round plays: in its own, the candidate it has
// in flight until a round settles it
func (d *Driver) deal() {
	d.own = d.alone()
	for i, l := range d.learners {
		if l != nil {
			d.mind.nets[i].wear(l.take(d.game, d.own))
		}
	}
}

// closeRound ends the round a new game closed. Its score is what the graph's
// score paid in it. A learner whose network steered is scored by its part, when
// the round was the bot's own and the game or its limit ended it; one whose
// network never did keeps its candidate for the round that reaches it.
func (d *Driver) closeRound(how Ending) {
	r := Round{N: d.stats.Rounds + 1, Ticks: d.stats.Ticks - d.roundTick, Ended: how}
	var total float64
	d.ctx.World.RunSafe(func() {
		total = d.score
		clear(d.mind.weighed) // a prediction is dropped with the game it was made in
		if d.Round != nil {
			r.Paid = slices.Clone(d.paid)
		}
		for i := range d.paid {
			d.paid[i].Count, d.paid[i].Score = 0, 0
		}
	})
	r.Score = total - d.roundScore
	d.roundTick, d.roundScore = d.stats.Ticks, total
	d.stats.Rounds++
	for i, n := range d.mind.nets {
		if r.Parts = append(r.Parts, Part{n.name, d.parts[i].score}); d.parts[i].holds {
			r.Under = strings.TrimPrefix(r.Under+"+"+n.name, "+")
		}
	}
	// A run's games close their rounds in turn, and one closed past the run's end is nobody's
	if d.games == nil || d.games.enter(d.game, d.stats.Ticks) {
		if d.alone != nil {
			scored := how != EndedCut && d.own
			for i, l := range d.learners {
				if l != nil && d.parts[i].steered {
					l.settle(d.game, d.parts[i].score, scored)
				}
			}
			d.deal()
		}
		if d.Round != nil {
			d.Round(r)
		}
		if d.games != nil {
			d.games.exit()
		}
	}
	clear(d.parts)
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
	st.Learn = slices.Clone(d.kept)
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
