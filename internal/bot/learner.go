package bot

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/lixenwraith/genetic"
	"github.com/lixenwraith/genetic/persistence"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// What the learner searches by. A step is a share of a weight's range: a run
// starts at learnStep and settles at learnStepFloor. Every learnRepeat-th
// evaluation plays an archive member again, and a member with learnConfirmed
// outcomes behind its score is one a run fields.
const (
	learnPool      = 32
	learnRate      = 0.1
	learnStep      = 0.1
	learnStepFloor = 0.03
	learnStepCeil  = 0.2
	learnStepTurn  = 1.5
	learnKeep      = 0.9
	learnRepeat    = 3
	learnConfirmed = 2

	// learnSaveEvery is the least wall time between two saves of a population in play
	learnSaveEvery = 30 * time.Second
)

// Learner evolves the weights of one graph's network, a round an evaluation: a
// steady-state archive of scored genomes, bred a hidden unit or an output at a
// time, its members played again in turn so a score is the mean of its rounds. It
// lives beside the driver, outside the world, and keeps its archive in a store.
type Learner struct {
	engine *genetic.StreamingEngine[[]float64, float64]
	store  persistence.Store
	name   string
	tag    string
	seed   []float64

	id     genetic.EvalID // the evaluation in play, zero while its genome plays unscored
	drawn  int            // evaluations begun
	seeded bool           // the hand-written genome has a score, or the archive came loaded
	stats  LearnStats
	saved  time.Time
	dirty  bool
	failed error // the last save's failure, which Close reports
}

// LearnStats is a learner's progress: its archive's size and best score, and the
// score and outcomes of the member it fields, zero while none is confirmed.
type LearnStats struct {
	Evaluations  uint64
	Size         int
	Best         float64
	Fielded      float64
	FieldedCount int
}

// NewLearner builds the learner of g's network, its stream drawn from seed, on the
// population store holds under name; a nil store keeps it for the run. A population
// of another layout, or none, starts over from the weights g wrote by hand.
func NewLearner(g *Graph, seed uint64, store persistence.Store, name string) (*Learner, error) {
	if g.net == nil {
		return nil, fmt.Errorf("bot %s: no [network] to learn", g.Name)
	}
	n := g.net
	bounds := make([]genetic.ParameterBounds, n.geneCount())
	for i := range bounds {
		bounds[i] = genetic.ParameterBounds{Min: -weightBound, Max: weightBound}
	}
	perturb := &genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}
	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize, cfg.ProposalCapacity, cfg.PendingCapacity = learnPool, 2, 8
	cfg.PerturbationRate, cfg.PerturbationStrength = learnRate, learnStep
	cfg.Seed = vmath.DeriveSeed(seed, "bot.learner")

	l := &Learner{store: store, name: name, tag: n.digest, seed: slices.Clone(n.genes), saved: time.Now()} // [wall] a save's spacing
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
	l.engine.SetSchedule(genetic.AdaptiveStrength[float64](learnRate, learnStep, learnStepFloor, learnStepCeil, learnStepTurn))

	if store != nil {
		dto, err := store.Load(name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("bot %s: population %s: %w", g.Name, name, err)
		}
		if err == nil && dto.Version == persistence.SchemaVersion && dto.Tag == l.tag {
			members := slices.DeleteFunc(dto.ToPool(), func(c genetic.Candidate[[]float64, float64]) bool {
				return len(c.Data) != len(l.seed)
			})
			l.engine.Inject(members, dto.Generation)
			l.seeded = len(members) > 0
		}
	}
	l.engine.Start()
	return l, nil
}

// turn closes the evaluation in play, scored when its round was played to its own
// end, and returns the genome the next round plays: learning, the next candidate,
// every learnRepeat-th an archive member again; otherwise the member it fields.
func (l *Learner) turn(score float64, played, learning bool) []float64 {
	if l.id != 0 {
		if played {
			l.engine.CompleteEvaluation(l.id, score)
			l.seeded, l.dirty = true, true
		} else {
			l.engine.AbandonEvaluation(l.id)
		}
		l.id = 0
	}
	fielded := l.field()
	if !learning {
		return fielded
	}
	l.drawn++
	var genes []float64
	switch {
	case !l.seeded:
		genes, l.id = slices.Clone(l.seed), l.engine.BeginEvaluation(l.seed)
	case l.drawn%learnRepeat == 0:
		genes, l.id = l.engine.Reevaluate()
	}
	if l.id == 0 {
		genes, l.id = l.engine.Propose()
	}
	if l.id == 0 {
		genes = fielded
	}
	if l.dirty && time.Since(l.saved) >= learnSaveEvery { // [wall]
		l.save()
	}
	return genes
}

// field is the genome a run that is not learning plays: the best member a second
// outcome has confirmed, since a member enters on one, or the hand-written one.
// It brings the stats up to the archive as it stands.
func (l *Learner) field() []float64 {
	st := l.engine.Stats()
	l.stats = LearnStats{Evaluations: st.Evaluations, Size: st.Size, Best: st.BestScore}
	for _, c := range l.engine.Snapshot().Members {
		if c.Count >= learnConfirmed {
			l.stats.Fielded, l.stats.FieldedCount = c.Score, c.Count
			return c.Data
		}
	}
	return slices.Clone(l.seed)
}

func (l *Learner) save() {
	if l.store == nil {
		return
	}
	dto := persistence.FromPool(l.engine.Snapshot())
	dto.Tag = l.tag
	l.failed = l.store.Save(l.name, dto)
	l.saved, l.dirty = time.Now(), false // [wall]
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
