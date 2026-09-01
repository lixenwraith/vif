package genetic

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// EvalID identifies an in-flight evaluation; 0 is never issued
type EvalID uint64

// StreamingEngine implements a steady-state (mu+lambda) GA with caller-driven,
// asynchronous evaluation. Proposals are drawn by the caller, scored out of band,
// and returned via CompleteEvaluation. All state transitions run on the caller's
// goroutine under one mutex; there are no background workers.
//
// The archive holds only scored candidates, sorted by score descending, and is
// therefore elitist by construction. Unevaluated offspring live in a separate
// bounded proposal queue and never affect statistics or selection.
type StreamingEngine[S Solution, F Numeric] struct {
	initializer InitializerFunc[S]
	selector    Selector[S, F]
	combiner    Combiner[S, F]
	perturbator Perturbator[S]
	cloner      Cloner[S]
	diversity   DiversityFunc[S, F]

	cfg StreamingConfig

	mu        sync.Mutex
	rngSource *rand.PCG
	rng       *rand.Rand
	archive   []Candidate[S, F]
	gen       int
	outcomes  int
	lastDiv   float64
	aggBest   F
	aggWorst  F
	aggAvg    F
	aggDirty  bool

	proposals ring[S]
	free      []S
	parents   []Candidate[S, F]
	offspring []S
	pending   pendingTable[S]

	nextID  uint64
	evicted uint64

	stats   atomic.Pointer[PoolStats[F]]
	started atomic.Bool
}

// NewStreamingEngine builds an engine. cloner may be nil, in which case solutions
// handed to callers alias engine memory and must be treated as read-only.
func NewStreamingEngine[S Solution, F Numeric](
	initializer InitializerFunc[S],
	selector Selector[S, F],
	combiner Combiner[S, F],
	perturbator Perturbator[S],
	cloner Cloner[S],
	config StreamingConfig,
) *StreamingEngine[S, F] {
	cfg := config.Normalize()
	rngSource := rand.NewPCG(cfg.Seed, cfg.Seed^defaultSeed)

	e := &StreamingEngine[S, F]{
		initializer: initializer,
		selector:    selector,
		combiner:    combiner,
		perturbator: perturbator,
		cloner:      cloner,
		cfg:         cfg,
		rngSource:   rngSource,
		rng:         rand.New(rngSource),
		archive:     make([]Candidate[S, F], 0, cfg.PoolSize),
		parents:     make([]Candidate[S, F], 2),
		offspring:   make([]S, 2),
		free:        make([]S, 0, cfg.PoolSize),
	}
	e.proposals.init(cfg.ProposalCapacity)
	e.pending.init(cfg.PendingCapacity)
	e.publishLocked()
	return e
}

// SetDiversity installs an optional diversity metric, recomputed once per generation
func (e *StreamingEngine[S, F]) SetDiversity(fn DiversityFunc[S, F]) {
	e.mu.Lock()
	e.diversity = fn
	e.mu.Unlock()
}

// Start enables proposal issuance and primes the queue. Idempotent and restartable
func (e *StreamingEngine[S, F]) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	e.mu.Lock()
	e.fillLocked()
	e.publishLocked()
	e.mu.Unlock()
}

// Stop halts proposal issuance; the archive is retained
func (e *StreamingEngine[S, F]) Stop() { e.started.Store(false) }

func (e *StreamingEngine[S, F]) Running() bool { return e.started.Load() }

// Propose returns the next unevaluated genotype and its evaluation id.
// The returned value is owned by the caller. A zero id means the engine is stopped
func (e *StreamingEngine[S, F]) Propose() (S, EvalID) {
	var zero S
	if !e.started.Load() {
		return zero, 0
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	sol, ok := e.proposals.pop()
	if !ok {
		e.produceLocked()
		if sol, ok = e.proposals.pop(); !ok {
			return zero, 0
		}
	}

	// Caller receives a recycled buffer; the engine retains the ring slice
	out := sol
	if e.cloner != nil {
		out = e.cloner.Clone(e.takeFreeLocked(), sol)
	}
	id := e.beginLocked(sol)
	e.publishLocked()
	return out, id
}

// BeginEvaluation registers an externally built genotype. The engine keeps a copy
func (e *StreamingEngine[S, F]) BeginEvaluation(sol S) EvalID {
	e.mu.Lock()
	defer e.mu.Unlock()

	kept := sol
	if e.cloner != nil {
		kept = e.cloner.Clone(e.takeFreeLocked(), sol)
	}
	id := e.beginLocked(kept)
	e.publishLocked()
	return id
}

// CompleteEvaluation admits a scored genotype into the archive
func (e *StreamingEngine[S, F]) CompleteEvaluation(id EvalID, score F) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sol, ok := e.pending.take(id)
	if !ok {
		return
	}
	e.insertLocked(sol, score)

	e.outcomes++
	if e.outcomes >= e.cfg.MinOutcomesPerGen {
		e.outcomes = 0
		e.gen++
		if e.diversity != nil {
			e.lastDiv = e.diversity(e.archive)
		}
		e.fillLocked()
	}
	e.publishLocked()
}

// AbandonEvaluation discards an in-flight evaluation without scoring it
func (e *StreamingEngine[S, F]) AbandonEvaluation(id EvalID) {
	e.mu.Lock()
	if sol, ok := e.pending.take(id); ok {
		e.recycleLocked(sol)
	}
	e.publishLocked()
	e.mu.Unlock()
}

// Reset drops in-flight evaluations and queued proposals; archive and generation persist
func (e *StreamingEngine[S, F]) Reset() {
	e.mu.Lock()
	e.pending.clear()
	e.proposals.clear()
	e.free = e.free[:0]
	e.outcomes = 0
	if e.started.Load() {
		e.fillLocked()
	}
	e.publishLocked()
	e.mu.Unlock()
}

// Stats returns the latest published snapshot without locking
func (e *StreamingEngine[S, F]) Stats() PoolStats[F] { return *e.stats.Load() }

func (e *StreamingEngine[S, F]) Generation() int   { return e.stats.Load().Generation }
func (e *StreamingEngine[S, F]) PendingCount() int { return e.stats.Load().Pending }
func (e *StreamingEngine[S, F]) EvaluationsStarted() uint64 {
	return e.stats.Load().Evaluations
}

// Best returns a copy of the top archive member
func (e *StreamingEngine[S, F]) Best() (Candidate[S, F], bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.archive) == 0 {
		return Candidate[S, F]{}, false
	}
	c := e.archive[0]
	if e.cloner != nil {
		var zero S
		c.Data = e.cloner.Clone(zero, c.Data)
	}
	return c, true
}

// Snapshot returns a deep copy of the archive for persistence
func (e *StreamingEngine[S, F]) Snapshot() *Pool[S, F] {
	e.mu.Lock()
	defer e.mu.Unlock()

	p := &Pool[S, F]{
		Members:    make([]Candidate[S, F], len(e.archive)),
		Generation: e.gen,
		Stats:      *e.stats.Load(),
	}
	for i, c := range e.archive {
		if e.cloner != nil {
			var zero S
			c.Data = e.cloner.Clone(zero, c.Data)
		}
		p.Members[i] = c
	}
	return p
}

// Inject replaces the archive with persisted candidates and takes ownership of
// them. Queued proposals derived from the old archive are discarded
func (e *StreamingEngine[S, F]) Inject(candidates []Candidate[S, F], generation int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.archive {
		e.recycleLocked(e.archive[i].Data)
	}
	e.archive = e.archive[:0]
	e.aggDirty = true

	for _, c := range candidates {
		e.insertLocked(c.Data, c.Score)
	}
	e.gen = generation

	e.proposals.clear()
	if e.started.Load() {
		e.fillLocked()
	}
	e.publishLocked()
}

// --- internals (mu held) ---

func (e *StreamingEngine[S, F]) beginLocked(sol S) EvalID {
	e.nextID++
	id := EvalID(e.nextID)
	if old, hit := e.pending.put(id, sol); hit {
		e.evicted++
		e.recycleLocked(old)
	}
	return id
}

// insertLocked places a scored candidate into the descending archive,
// evicting the tail when full
func (e *StreamingEngine[S, F]) insertLocked(sol S, score F) {
	n := len(e.archive)
	if n == cap(e.archive) {
		if score <= e.archive[n-1].Score {
			e.recycleLocked(sol)
			return
		}
		e.recycleLocked(e.archive[n-1].Data)
		n--
		e.archive = e.archive[:n]
	}

	idx := n
	for idx > 0 && e.archive[idx-1].Score < score {
		idx--
	}
	e.archive = e.archive[:n+1]
	copy(e.archive[idx+1:], e.archive[idx:n])
	e.archive[idx] = Candidate[S, F]{Data: sol, Score: score}
	e.aggDirty = true
}

// fillLocked tops up the proposal queue within the tick budget
func (e *StreamingEngine[S, F]) fillLocked() {
	if e.proposals.free() == 0 {
		return
	}
	if e.cfg.RefillMode == RefillDeterministic {
		for e.proposals.free() > 0 && e.produceLocked() {
		}
		return
	}
	deadline := time.Now().Add(e.cfg.TickBudget)
	for i := 0; e.proposals.free() > 0; i++ {
		if !e.produceLocked() {
			return
		}
		if i&3 == 3 && time.Now().After(deadline) {
			return
		}
	}
}

// produceLocked queues one offspring batch, or a random genotype while the
// archive is too small for selection. Reports whether anything was queued
func (e *StreamingEngine[S, F]) produceLocked() bool {
	if len(e.archive) < 2 {
		return e.proposals.push(e.initializer(e.rng))
	}

	e.selector.Select(e.archive, e.parents, e.rng)
	n := e.combiner.Combine(e.parents, e.offspring, e.rng)

	pushed := false
	for j := range n {
		e.perturbator.Perturb(&e.offspring[j],
			e.cfg.PerturbationRate, e.cfg.PerturbationStrength, e.rng)
		if !e.proposals.push(e.offspring[j]) {
			return pushed
		}
		pushed = true
		e.offspring[j] = e.takeFreeLocked()
	}
	return pushed
}

func (e *StreamingEngine[S, F]) publishLocked() {
	if e.aggDirty {
		e.aggDirty = false
		e.aggBest, e.aggWorst, e.aggAvg = 0, 0, 0
		if n := len(e.archive); n > 0 {
			var total F
			for i := range e.archive {
				total += e.archive[i].Score
			}
			e.aggBest, e.aggWorst, e.aggAvg = e.archive[0].Score, e.archive[n-1].Score, total/F(n)
		}
	}
	e.stats.Store(&PoolStats[F]{
		BestScore:    e.aggBest,
		WorstScore:   e.aggWorst,
		AverageScore: e.aggAvg,
		Diversity:    e.lastDiv,
		Size:         len(e.archive),
		Generation:   e.gen,
		Pending:      e.pending.live,
		Evaluations:  e.nextID,
		Evicted:      e.evicted,
	})
}

func (e *StreamingEngine[S, F]) recycleLocked(sol S) {
	if e.cloner == nil || len(e.free) == cap(e.free) {
		return
	}
	e.free = append(e.free, sol)
}

func (e *StreamingEngine[S, F]) takeFreeLocked() S {
	var zero S
	n := len(e.free)
	if n == 0 {
		return zero
	}
	v := e.free[n-1]
	e.free[n-1] = zero
	e.free = e.free[:n-1]
	return v
}
