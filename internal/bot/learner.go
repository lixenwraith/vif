package bot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/genetic"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// What the learner searches by. A population whose graph writes no weight plays
// learnPool genomes drawn learnSpread of a weight's bound about zero first. A step
// is learnStep of a weight's range. Every learnRepeat-th candidate is a member
// again, learnProven rounds prove one, learnProbation slots stay with the
// unproved, and recent looks back learnRecent.
const (
	learnPool      = 32
	learnSpread    = 0.03
	learnRate      = 0.03
	learnStep      = 0.03
	learnKeep      = 0.9
	learnRepeat    = 3
	learnProven    = 8
	learnProbation = 24
	learnRecent    = 200

	// learnSaveEvery is the least wall time between two saves of a population in play
	learnSaveEvery = 30 * time.Second

	// populationVersion names the document a population is kept as and how its
	// rounds were scored: one kept under another starts over
	populationVersion = 3
)

// Store keeps one population's document: Load returns what was kept, nothing for
// none yet, and Store replaces it whole.
type Store interface {
	Load() ([]byte, error)
	Store([]byte) error
}

// population is the document a learner keeps: its archive in its order, the
// proved first, and what reads it without the code. Rounds is every round scored
// since the population began, Recent their mean lately, Step the mutation step,
// and Fielded the member a run that is not learning plays, -1 with none.
type population struct {
	Version int      `json:"version"`
	Layout  string   `json:"layout"`
	Rounds  int      `json:"rounds"`
	Recent  float64  `json:"recent"`
	Step    float64  `json:"step"`
	Fielded int      `json:"fielded"`
	Members []member `json:"members"`
}

// member is one genome of a population: the mean score of the rounds it has
// played, how many they are, and the population's round it entered at.
type member struct {
	Score  float64   `json:"score"`
	Rounds int       `json:"rounds"`
	Born   int       `json:"born"`
	Genes  []float64 `json:"genes"`
}

// Learner evolves the weights of one network of a graph, a round an evaluation: a
// steady-state archive of scored genomes ranked by what their rounds prove (rank),
// bred a hidden unit or an output at a time, its members played again in turn so
// a score is the mean of its rounds. It lives beside the drivers of the games it
// learns from, outside their worlds, and keeps its archive in a store.
type Learner struct {
	engine *genetic.StreamingEngine[[]float64, float64]
	store  Store
	name   string // the network it learns
	layout string
	seed   []float64

	slots   []slot         // what each game has in play
	rng     *rand.Rand     // what a population's first genomes are drawn from
	drawn   int            // candidates taken
	seeded  bool           // the hand-written genome has played this run, or holds no weight to try
	written bool           // the graph writes weights, which a population then starts from
	scored  int            // rounds scored this run
	rounds  int            // and since the population began
	recent  float64        // their mean, the latest learnRecent weighing most
	born    map[uint64]int // the round each archive member entered at, by its genes
	saved   time.Time
	dirty   bool
	failed  error // what keeps the population from its store, which Close reports
	untold  error // what Lost has yet to hear

	// stats is the progress as the last round left it, which any goroutine reads
	// while the games play
	stats atomic.Pointer[LearnStats]

	// Saved, when set, is told the progress each time the population is kept
	Saved func(LearnStats)
	// Lost, when set, is told once what keeps the population from its store: a file
	// that cannot be read or parsed, as the first round is taken, and a save that
	// fails, not those failing after it
	Lost func(error)
}

// slot is the genome one game has in play: its evaluation, zero while it plays
// unscored, whether this is a new candidate's first round, which only qualifies
// it, and for an archive member played again the score and rounds it had
type slot struct {
	genes []float64
	id    genetic.EvalID
	fresh bool
	mean  float64
	count int
}

// LearnStats is a learner's progress: its network, the rounds it scored this run
// and since its population began, their recent mean, which no luck picked, the
// step, its archive's size and its first member's score, and the score and
// rounds of the member it fields, zero while none is proven.
type LearnStats struct {
	Network       string
	Scored        int
	Rounds        int
	Recent        float64
	Step          float64
	Size          int
	Best          float64
	Fielded       float64
	FieldedRounds int
}

// NewLearner builds the learner of g's network name for games games at once, its
// stream drawn from seed, on the population store keeps; a nil store keeps it for
// the run. A population of another layout or one that cannot be parsed is started
// over, one that cannot be read is left alone and the run keeps none, and any
// weights g wrote by hand are the first candidate of every run.
func NewLearner(g *Graph, name string, seed uint64, games int, store Store) (*Learner, error) {
	i := slices.IndexFunc(g.nets, func(n *network) bool { return n.name == name })
	if i < 0 {
		return nil, fmt.Errorf("bot %s: no [network.%s] to learn", g.Name, name)
	}
	n := g.nets[i]
	l := &Learner{store: store, name: name, layout: n.digest, seed: slices.Clone(n.genes),
		slots: make([]slot, games), born: make(map[uint64]int), saved: time.Now()} // [wall] a save's spacing
	l.rng = rand.New(rand.NewPCG(vmath.DeriveSeed(seed, "bot.learner."+name), vmath.DeriveSeed(seed, "bot.learner.draw")))
	l.written = slices.ContainsFunc(l.seed, func(w float64) bool { return w != 0 })
	l.seeded = !l.written
	l.stats.Store(&LearnStats{Network: name})
	var kept population
	if store != nil {
		data, err := store.Load()
		if err != nil {
			// The file may be a good one, so nothing this run learns replaces it
			l.store, l.failed = nil, fmt.Errorf("%s population could not be read and is left as it is: %w", name, err)
			l.untold = l.failed
		} else if err := json.Unmarshal(data, &kept); len(data) > 0 && err != nil {
			kept, l.untold = population{}, fmt.Errorf("%s population could not be parsed and starts over: %w", name, err)
		}
	}
	var members []genetic.Candidate[[]float64, float64]
	if kept.Version == populationVersion && kept.Layout == l.layout {
		l.rounds, l.recent = max(kept.Rounds, 0), kept.Recent
		for _, m := range kept.Members {
			if len(m.Genes) == len(l.seed) && m.Rounds > 0 {
				l.born[genomeKey(m.Genes)] = m.Born
				members = append(members, genetic.Candidate[[]float64, float64]{Data: m.Genes, Score: m.Score, Count: m.Rounds})
			}
		}
	}

	bounds := make([]genetic.ParameterBounds, n.geneCount())
	for i := range bounds {
		bounds[i] = genetic.ParameterBounds{Min: -weightBound, Max: weightBound}
	}
	perturb := &genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}
	cfg := genetic.DefaultStreamingConfig()
	// Four slots a game, as the engine asks; one in flight may still be evicted (settle)
	cfg.PoolSize, cfg.ProposalCapacity, cfg.PendingCapacity = learnPool, 2, max(8, 4*games)
	cfg.PerturbationRate, cfg.PerturbationStrength = learnRate, learnStep
	cfg.Seed = vmath.DeriveSeed(seed, "bot.learner."+name)
	l.engine = genetic.NewStreamingEngine[[]float64, float64](
		// The engine draws its own only while it holds under two members, which take fills first
		func(*rand.Rand) []float64 { return l.draw() },
		&genetic.TournamentSelector[[]float64, float64]{TournamentSize: genetic.DefaultTournamentSize},
		&genetic.BlockCombiner[[]float64, float64, float64]{Blocks: n.blocks(), MixProbability: learnKeep},
		perturb, genetic.SliceCloner[[]float64, float64]{}, cfg)
	l.engine.SetEqual(slices.Equal[[]float64])
	l.engine.SetMerge(genetic.MeanScore[float64])
	l.engine.SetRank(l.rank)
	l.engine.Inject(members, 0)
	l.engine.Start()
	return l, nil
}

// draw is a genome to start from: every weight drawn at random where the graph
// writes none, and where it writes some, those with a child's share stepped, as
// a written start is lost among copies drawn about its every weight.
func (l *Learner) draw() []float64 {
	genes := slices.Clone(l.seed)
	one := -1
	if l.written {
		one = l.rng.IntN(len(genes)) // a start handed back unchanged would be no candidate
	}
	for i := range genes {
		if l.written && i != one && l.rng.Float64() >= learnRate {
			continue
		}
		genes[i] = min(max(genes[i]+l.rng.NormFloat64()*learnSpread*weightBound, -weightBound), weightBound)
	}
	return genes
}

// genomeKey names a genome by its genes' bits, which is how an archive member is
// known from one snapshot to the next
func genomeKey(genes []float64) uint64 {
	h := fnv.New64a()
	var word [8]byte
	for _, g := range genes {
		binary.LittleEndian.PutUint64(word[:], math.Float64bits(g))
		h.Write(word[:])
	}
	return h.Sum64()
}

// settle closes the evaluation game has in flight with the round its candidate
// played, scored when it was played to its own end, and reports whether it is
// over. A candidate whose first round would enter a full archive is held for
// another and scored by that round alone, the first being picked for its luck.
func (l *Learner) settle(game int, score float64, played bool) bool {
	s := &l.slots[game]
	if s.id != 0 && played {
		l.scored++
		l.rounds++
		l.recent += (score - l.recent) / float64(min(l.rounds, learnRecent))
		l.dirty = true
		if s.fresh && l.enters(score) {
			s.fresh = false
			held := l.Stats()
			held.Scored, held.Rounds, held.Recent = l.scored, l.rounds, l.recent
			l.stats.Store(&held)
			return false
		}
		// A member that left the archive with this round in flight comes back, if at
		// all, on the mean of its rounds, not on this one as a new genome would
		if s.count > 0 && !l.holds(s.genes) {
			score = genetic.MeanScore(s.mean, score, s.count)
		}
		// The engine evicts an evaluation in flight once eight newer fill its window,
		// as nine games can, so the round is scored under one begun as it closes
		l.engine.AbandonEvaluation(s.id)
		l.engine.CompleteEvaluation(l.engine.BeginEvaluation(s.genes), score)
		l.seeded = true
	} else if s.id != 0 {
		l.engine.AbandonEvaluation(s.id)
	}
	s.id = 0
	return true
}

// take is the genome game's next round plays. Exploring, it is the candidate the
// game has in flight until that is settled, then the next: a hand-written genome
// until a round has scored it, one drawn at random while the population is new,
// and every learnRepeat-th a member again, to prove a guess or test the title.
// Else, the fielded.
func (l *Learner) take(game int, explore bool) []float64 {
	l.tell()
	s := &l.slots[game]
	if s.id != 0 && explore {
		return s.genes
	}
	l.settle(game, 0, false)
	members := l.engine.Snapshot().Members
	*s = slot{genes: l.field(members), fresh: explore}
	if explore {
		l.drawn++
		var genes []float64
		switch {
		case !l.seeded:
			genes, s.id = slices.Clone(l.seed), l.engine.BeginEvaluation(l.seed)
		case !l.written && l.rounds+l.flying() < learnPool:
			genes = l.draw()
			s.id = l.engine.BeginEvaluation(genes)
		case l.drawn%learnRepeat == 0:
			// By turns the member short of its proof the archive trusts most, and the less
			// played of the fielded and its rival, so a title is tested while it is held
			again := -1
			for i, c := range members {
				if c.Count+l.playing(c.Data) < learnProven && (again < 0 || trusted(c, l.recent) > trusted(members[again], l.recent)) {
					again = i
				}
			}
			if rival := l.rival(members); rival >= 0 && (again < 0 || l.drawn/learnRepeat%2 == 1) {
				again = rival
			}
			if again >= 0 {
				genes, s.id = members[again].Data, l.engine.BeginEvaluation(members[again].Data)
			}
		}
		if s.id == 0 {
			genes, s.id = l.engine.Propose()
		}
		if s.id != 0 {
			s.genes = genes
			// One the archive holds is that member again, whatever drew it, and none to qualify
			if i := slices.IndexFunc(members, func(c genetic.Candidate[[]float64, float64]) bool { return slices.Equal(c.Data, genes) }); i >= 0 {
				s.fresh, s.mean, s.count = false, members[i].Score, members[i].Count
			}
		}
	}
	if l.dirty && time.Since(l.saved) >= learnSaveEvery { // [wall]
		l.save()
	}
	return s.genes
}

// holds reports whether genes is an archive member as the archive stands
func (l *Learner) holds(genes []float64) bool {
	return slices.ContainsFunc(l.engine.Snapshot().Members, func(c genetic.Candidate[[]float64, float64]) bool {
		return slices.Equal(c.Data, genes)
	})
}

// flying counts the games with a candidate in flight
func (l *Learner) flying() (n int) {
	for i := range l.slots {
		if l.slots[i].id != 0 {
			n++
		}
	}
	return n
}

// playing counts the games with genes in flight, each a round toward its proof
func (l *Learner) playing(genes []float64) (n int) {
	for i := range l.slots {
		if l.slots[i].id != 0 && slices.Equal(l.slots[i].genes, genes) {
			n++
		}
	}
	return n
}

// field is the genome a round plays where no candidate does: the member fielded
// (fieldedOf), which stands first of the archive, or with none proved the
// hand-written one. It brings the stats, and the round each member entered at,
// up to the archive as it stands.
func (l *Learner) field(members []genetic.Candidate[[]float64, float64]) []float64 {
	st := LearnStats{Network: l.name, Scored: l.scored, Rounds: l.rounds, Recent: l.recent, Step: learnStep, Size: len(members)}
	born := make(map[uint64]int, len(members))
	fielded := slices.Clone(l.seed)
	for i, c := range members {
		key := genomeKey(c.Data)
		at, known := l.born[key]
		if !known {
			at = l.rounds
		}
		born[key] = at
		if i == 0 {
			st.Best = c.Score
		}
	}
	if f, _ := fieldedOf(members, l.recent); f >= 0 {
		st.Fielded, st.FieldedRounds, fielded = members[f].Score, members[f].Count, members[f].Data
	}
	l.born = born
	l.stats.Store(&st)
	return fielded
}

// trusted is a member's mean as its rounds are trusted for: weighed with
// learnProven rounds at the population's recent mean, so a few lucky rounds do
// not outrank many good ones.
func trusted(c genetic.Candidate[[]float64, float64], recent float64) float64 {
	return (c.Score*float64(c.Count) + recent*learnProven) / float64(c.Count+learnProven)
}

// rank keys the archive for the engine by what its members' rounds prove: each
// by its trusted mean, the proved above those on probation. So a newcomer
// displaces only the last on probation, and where a full archive holds more
// proved than probation leaves room for, the last of them leaves instead: the
// one just proved, where it stands above no other.
func (l *Learner) rank(members []genetic.Candidate[[]float64, float64], keys []float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, c := range members {
		keys[i] = trusted(c, l.recent)
		lo, hi = min(lo, keys[i]), max(hi, keys[i])
	}
	proved, last := 0, -1
	for i, c := range members {
		if c.Count < learnProven {
			continue
		}
		keys[i] += hi - lo + 1
		if proved++; last < 0 || keys[i] <= keys[last] {
			last = i
		}
	}
	if len(members) > learnPool && proved > learnPool-learnProbation {
		keys[last] = lo - 1
	}
}

// enters reports whether a first round of score would have its genome enter a
// full archive, which is where a member of that one round ranks above the last
func (l *Learner) enters(score float64) bool {
	members := l.engine.Snapshot().Members
	return len(members) == learnPool &&
		trusted(genetic.Candidate[[]float64, float64]{Score: score, Count: 1}, l.recent) > trusted(members[learnPool-1], l.recent)
}

// fieldedOf is the member a run fields, the most trusted of those learnProven
// rounds have proved, and the next most trusted, its rival; -1 for none.
func fieldedOf(members []genetic.Candidate[[]float64, float64], recent float64) (fielded, rival int) {
	fielded, rival = -1, -1
	for i, c := range members {
		switch {
		case c.Count < learnProven:
		case fielded < 0 || trusted(c, recent) > trusted(members[fielded], recent):
			fielded, rival = i, fielded
		case rival < 0 || trusted(c, recent) > trusted(members[rival], recent):
			rival = i
		}
	}
	return fielded, rival
}

// rival is the member a round tests the title on: of the fielded and its rival
// the one with fewer rounds, those in flight counted, and -1 while none is proved
func (l *Learner) rival(members []genetic.Candidate[[]float64, float64]) int {
	fielded, rival := fieldedOf(members, l.recent)
	rounds := func(i int) int { return members[i].Count + l.playing(members[i].Data) }
	if rival >= 0 && rounds(rival) < rounds(fielded) {
		return rival
	}
	return fielded
}

// save keeps the population as it stands
func (l *Learner) save() {
	if l.store == nil {
		return
	}
	doc := population{Version: populationVersion, Layout: l.layout, Rounds: l.rounds, Recent: l.recent, Step: learnStep, Fielded: -1}
	members := l.engine.Snapshot().Members
	l.field(members)
	doc.Fielded, _ = fieldedOf(members, l.recent)
	for _, c := range members {
		doc.Members = append(doc.Members, member{Score: c.Score, Rounds: c.Count, Born: l.born[genomeKey(c.Data)], Genes: c.Data})
	}
	data, err := json.Marshal(doc)
	if err == nil {
		err = l.ahead()
	}
	if err == nil {
		err = l.store.Store(data)
	}
	l.saved = time.Now() // [wall]
	if err != nil {
		err = fmt.Errorf("%s population could not be saved: %w", l.name, err)
		if l.failed == nil {
			l.untold = err
		}
		l.failed = err
		l.tell()
		return
	}
	l.failed, l.dirty = nil, false
	if l.Saved != nil {
		l.Saved(l.Stats())
	}
}

// ahead is the failure of a save onto a population of this layout that holds
// more rounds: another run's, further on, which keeps the file
func (l *Learner) ahead() error {
	var kept struct {
		Version, Rounds int
		Layout          string
	}
	if data, err := l.store.Load(); err != nil || json.Unmarshal(data, &kept) != nil {
		return nil
	}
	if kept.Version == populationVersion && kept.Layout == l.layout && kept.Rounds > l.rounds {
		return fmt.Errorf("another run has it %d rounds further on", kept.Rounds-l.rounds)
	}
	return nil
}

// tell has Lost hear what it has yet to
func (l *Learner) tell() {
	if l.untold != nil && l.Lost != nil {
		l.Lost(l.untold)
		l.untold = nil
	}
}

// Close saves what the run learned and reports what kept it from its store
func (l *Learner) Close() error {
	if l.dirty {
		l.save()
	}
	if l.dirty {
		return l.failed
	}
	return nil
}

// Stats is the learner's progress as its last round left it
func (l *Learner) Stats() LearnStats { return *l.stats.Load() }

// games is the order the rounds of the drivers that learn together close in, a
// game each: by the tick a round closed at, then by its game, as if the games
// took turns a tick at a time. So one seed and one count of games search alike
// however their cores run them.
type games struct {
	mu    sync.Mutex
	moved *sync.Cond
	// next is the first tick each game's next round can close at, one past those it
	// has played and gone once it has left; wanted is the earliest tick a game
	// waits its turn at, gone with none waiting; end is the last tick a round
	// closes into the learners at, gone until the run ends
	next   []atomic.Uint64
	wanted atomic.Uint64
	end    atomic.Uint64
}

// gone is where a game that has left stands: past every tick
const gone = math.MaxUint64

func newGames(n int) *games {
	g := &games{next: make([]atomic.Uint64, n)}
	g.moved = sync.NewCond(&g.mu)
	g.wanted.Store(gone)
	g.end.Store(gone)
	for i := range g.next {
		g.next[i].Store(1)
	}
	return g
}

// reach says game's next round can close no earlier than tick, and wakes the
// game waiting its turn where that may be what it waits on
func (g *games) reach(game int, tick uint64) {
	before := g.next[game].Swap(tick)
	if w := g.wanted.Load(); before <= w && tick >= w {
		g.wake()
	}
}

func (g *games) wake() {
	g.mu.Lock()
	g.moved.Broadcast()
	g.mu.Unlock()
}

// enter waits for the turn of the round game closed at tick, which comes once no
// other game can close one before it, and holds the learners until exit; false
// for a round that closed past the run's end. The games still waiting then name
// the earliest tick among them again.
func (g *games) enter(game int, tick uint64) bool {
	g.mu.Lock()
	for tick <= g.end.Load() {
		if tick < g.wanted.Load() {
			g.wanted.Store(tick)
		}
		if g.due(game, tick) {
			break
		}
		g.moved.Wait()
	}
	g.wanted.Store(gone)
	g.moved.Broadcast()
	if tick > g.end.Load() {
		g.mu.Unlock()
		return false
	}
	return true
}

func (g *games) exit() { g.mu.Unlock() }

// due reports whether every other game's next round closes after game's at tick
func (g *games) due(game int, tick uint64) bool {
	for other := range g.next {
		if at := g.next[other].Load(); other != game && (at < tick || at == tick && other < game) {
			return false
		}
	}
	return true
}
