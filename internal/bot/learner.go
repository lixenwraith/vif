package bot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/lixenwraith/genetic"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// What the learner searches by. A step is a share of a weight's range: a new
// population starts at learnStep and settles at learnStepFloor. Every
// learnRepeat-th evaluation plays an archive member again, a member with
// learnProven rounds behind its score is one a run fields, and learnRecent rounds
// are what the recent score looks back over.
const (
	learnPool      = 32
	learnRate      = 0.03
	learnStep      = 0.1
	learnStepFloor = 0.03
	learnStepCeil  = 0.2
	learnStepTurn  = 1.5
	learnKeep      = 0.9
	learnRepeat    = 3
	learnProven    = 8
	learnRecent    = 200

	// learnSaveEvery is the least wall time between two saves of a population in play
	learnSaveEvery = 30 * time.Second

	// populationVersion names the layout of the document a population is kept as
	populationVersion = 1
)

// Store keeps one population's document: Load returns what was kept, nothing for
// none yet, and Store replaces it whole.
type Store interface {
	Load() ([]byte, error)
	Store([]byte) error
}

// population is the document a learner keeps: its archive, best first, and what
// reads it without the code. Rounds is every round scored since the population
// began, Recent their mean lately, Step the mutation step in force, and Fielded
// the member a run that is not learning plays, -1 while none is proven.
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

// Learner evolves the weights of one graph's network, a round an evaluation: a
// steady-state archive of scored genomes, bred a hidden unit or an output at a
// time, its members played again in turn so a score is the mean of its rounds. It
// lives beside the driver, outside the world, and keeps its archive in a store.
type Learner struct {
	engine *genetic.StreamingEngine[[]float64, float64]
	store  Store
	layout string
	seed   []float64

	genes  []float64      // the genome in play
	id     genetic.EvalID // its evaluation, zero while it plays unscored
	fresh  bool           // it is a new candidate's first round, which only qualifies it
	drawn  int            // evaluations begun
	seeded bool           // the hand-written genome has played this run
	scored int            // rounds scored this run
	rounds int            // and since the population began
	recent float64        // their mean, the latest learnRecent weighing most
	step   float64        // the mutation step in force
	born   map[uint64]int // the round each archive member entered at, by its genes
	stats  LearnStats
	saved  time.Time
	dirty  bool
	failed error // the last save's failure, which Close reports

	// Saved, when set, is told the progress each time the population is kept
	Saved func(LearnStats)
}

// LearnStats is a learner's progress: the rounds it scored this run and since its
// population began, their recent mean, which no luck picked, the step in force,
// its archive's size and best score, and the score and rounds of the member it
// fields, zero while none is proven.
type LearnStats struct {
	Scored        int
	Rounds        int
	Recent        float64
	Step          float64
	Size          int
	Best          float64
	Fielded       float64
	FieldedRounds int
}

// NewLearner builds the learner of g's network, its stream drawn from seed, on the
// population store keeps; a nil store keeps it for the run. A population of
// another layout is started over, and the weights g wrote by hand play the first
// round of every run, so an edit to them meets what the store kept.
func NewLearner(g *Graph, seed uint64, store Store) (*Learner, error) {
	if g.net == nil {
		return nil, fmt.Errorf("bot %s: no [network] to learn", g.Name)
	}
	n := g.net
	l := &Learner{store: store, layout: n.digest, seed: slices.Clone(n.genes), step: learnStep,
		born: make(map[uint64]int), saved: time.Now()} // [wall] a save's spacing
	var kept population
	if store != nil {
		data, err := store.Load()
		if err == nil && len(data) > 0 {
			err = json.Unmarshal(data, &kept)
		}
		if err != nil {
			return nil, fmt.Errorf("bot %s: population: %w", g.Name, err)
		}
	}
	var members []genetic.Candidate[[]float64, float64]
	if kept.Version == populationVersion && kept.Layout == l.layout {
		l.rounds, l.recent = kept.Rounds, kept.Recent
		l.step = min(max(kept.Step, learnStepFloor), learnStepCeil)
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
	cfg.PoolSize, cfg.ProposalCapacity, cfg.PendingCapacity = learnPool, 2, 8
	cfg.PerturbationRate, cfg.PerturbationStrength = learnRate, l.step
	cfg.Seed = vmath.DeriveSeed(seed, "bot.learner")
	l.engine = genetic.NewStreamingEngine[[]float64, float64](
		// Until two members are scored, a child is the hand-written genome with every
		// weight a settled step off
		func(rng *rand.Rand) []float64 {
			child := slices.Clone(l.seed)
			perturb.Perturb(&child, 1, learnStepFloor, rng)
			return child
		},
		&genetic.TournamentSelector[[]float64, float64]{TournamentSize: genetic.DefaultTournamentSize},
		&genetic.BlockCombiner[[]float64, float64, float64]{Blocks: n.blocks(), MixProbability: learnKeep},
		perturb, genetic.SliceCloner[[]float64, float64]{}, cfg)
	l.engine.SetEqual(slices.Equal[[]float64])
	l.engine.SetMerge(genetic.MeanScore[float64])
	adapt := genetic.AdaptiveStrength[float64](learnRate, l.step, learnStepFloor, learnStepCeil, learnStepTurn)
	l.engine.SetSchedule(func(generation int, stats genetic.PoolStats[float64]) (float64, float64) {
		rate, step := adapt(generation, stats)
		l.step = step
		return rate, step
	})
	l.engine.Inject(members, 0)
	l.engine.Start()
	return l, nil
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

// turn closes the round a genome played, scored when it was played to its own
// end, and returns the next round's. Learning, a candidate whose first round would
// enter a full archive plays again and is scored by that round, the first being
// picked for its luck; else the next candidate plays, every learnRepeat-th an
// archive member again. Not learning, the member it fields plays.
func (l *Learner) turn(score float64, played, learning bool) []float64 {
	if l.id != 0 && played {
		l.scored++
		l.rounds++
		l.recent += (score - l.recent) / float64(min(l.rounds, learnRecent))
		if st := l.engine.Stats(); l.fresh && st.Size == learnPool && score > st.WorstScore {
			l.fresh, l.stats.Scored, l.stats.Rounds, l.stats.Recent = false, l.scored, l.rounds, l.recent
			return l.genes
		}
		l.engine.CompleteEvaluation(l.id, score)
		l.seeded, l.dirty = true, true
	} else if l.id != 0 {
		l.engine.AbandonEvaluation(l.id)
	}
	l.id = 0
	fielded := l.field(l.engine.Snapshot().Members)
	if !learning {
		return fielded
	}
	l.drawn++
	l.genes, l.fresh = nil, true
	switch {
	case !l.seeded:
		l.genes, l.id = slices.Clone(l.seed), l.engine.BeginEvaluation(l.seed)
	case l.drawn%learnRepeat == 0:
		l.genes, l.id = l.engine.Reevaluate()
		l.fresh = false
	}
	if l.id == 0 {
		l.genes, l.id = l.engine.Propose()
		l.fresh = true
	}
	if l.id == 0 {
		l.genes = fielded
	}
	if l.dirty && time.Since(l.saved) >= learnSaveEvery { // [wall]
		l.save()
	}
	return l.genes
}

// field is the genome a run that is not learning plays, of the archive's members
// best first: the best its rounds have proved, since one enters on a round or two
// and the top of an archive is its luckiest, or the hand-written one. It brings
// the stats, and the round each member entered at, up to the archive as it stands.
func (l *Learner) field(members []genetic.Candidate[[]float64, float64]) []float64 {
	l.stats = LearnStats{Scored: l.scored, Rounds: l.rounds, Recent: l.recent, Step: l.step, Size: len(members)}
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
			l.stats.Best = c.Score
		}
		if l.stats.FieldedRounds == 0 && c.Count >= learnProven {
			l.stats.Fielded, l.stats.FieldedRounds, fielded = c.Score, c.Count, c.Data
		}
	}
	l.born = born
	return fielded
}

// save keeps the population as it stands
func (l *Learner) save() {
	if l.store == nil {
		return
	}
	doc := population{Version: populationVersion, Layout: l.layout, Rounds: l.rounds, Recent: l.recent, Step: l.step, Fielded: -1}
	for i, c := range l.engine.Snapshot().Members {
		if doc.Fielded < 0 && c.Count >= learnProven {
			doc.Fielded = i
		}
		doc.Members = append(doc.Members, member{Score: c.Score, Rounds: c.Count, Born: l.born[genomeKey(c.Data)], Genes: c.Data})
	}
	data, err := json.Marshal(doc)
	if err == nil {
		err = l.store.Store(data)
	}
	l.failed = err
	l.saved, l.dirty = time.Now(), false // [wall]
	if l.Saved != nil && err == nil {
		l.Saved(l.stats)
	}
}

// Close saves what the run learned and reports the last save that failed
func (l *Learner) Close() error {
	if l.dirty {
		l.save()
	}
	return l.failed
}

// Stats is the learner's progress as its last round left it
func (l *Learner) Stats() LearnStats { return l.stats }
