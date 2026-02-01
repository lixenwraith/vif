package genetic

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/parameter"
)

// EvalID identifies a pending evaluation
type EvalID uint64

// EvalOutcome contains deferred evaluation result
type EvalOutcome[F Numeric] struct {
	ID    EvalID
	Score F
}

// StreamingConfig extends EngineConfig with async parameters
type StreamingConfig struct {
	EngineConfig
	TickBudget        time.Duration
	OutcomeBufferSize int
	MinOutcomesPerGen int
}

func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		EngineConfig:      DefaultConfig(),
		TickBudget:        parameter.GATickBudget,
		OutcomeBufferSize: parameter.GAOutcomeBufferSize,
		MinOutcomesPerGen: parameter.GAMinOutcomesPerGen,
	}
}

// StreamingEngine provides non-blocking evolution
type StreamingEngine[S Solution, F Numeric] struct {
	initializer InitializerFunc[S]
	selector    Selector[S, F]
	combiner    Combiner[S, F]
	perturbator Perturbator[S]

	config      StreamingConfig
	rng         *rand.Rand
	currentPool *Pool[S, F]

	outcomesChan chan EvalOutcome[F]
	requestChan  chan struct{}
	bestChan     chan Candidate[S, F]

	pendingMu  sync.RWMutex
	pending    map[EvalID]*Candidate[S, F]
	nextEvalID atomic.Uint64

	stopChan chan struct{}
	stopOnce sync.Once
	running  atomic.Bool
}

func NewStreamingEngine[S Solution, F Numeric](
	initializer InitializerFunc[S],
	selector Selector[S, F],
	combiner Combiner[S, F],
	perturbator Perturbator[S],
	config StreamingConfig,
) *StreamingEngine[S, F] {
	var rng *rand.Rand
	if config.Seed == 0 {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	} else {
		rng = rand.New(rand.NewPCG(config.Seed, config.Seed))
	}

	return &StreamingEngine[S, F]{
		initializer:  initializer,
		selector:     selector,
		combiner:     combiner,
		perturbator:  perturbator,
		config:       config,
		rng:          rng,
		outcomesChan: make(chan EvalOutcome[F], config.OutcomeBufferSize),
		requestChan:  make(chan struct{}, 1),
		bestChan:     make(chan Candidate[S, F], 1),
		pending:      make(map[EvalID]*Candidate[S, F]),
		stopChan:     make(chan struct{}),
	}
}

func (e *StreamingEngine[S, F]) Start() {
	if !e.running.CompareAndSwap(false, true) {
		return
	}
	// Only initialize if pool doesn't exist (preserves population on restart/reset)
	if e.currentPool == nil {
		e.initializePool()
	}
	go e.evolutionLoop()
}

func (e *StreamingEngine[S, F]) Stop() {
	e.stopOnce.Do(func() {
		if e.running.CompareAndSwap(true, false) {
			close(e.stopChan)
		}
	})
}

func (e *StreamingEngine[S, F]) BeginEvaluation(solution S) EvalID {
	id := EvalID(e.nextEvalID.Add(1))

	candidate := &Candidate[S, F]{
		Data:     solution,
		Metadata: make(map[string]any),
	}

	e.pendingMu.Lock()
	e.pending[id] = candidate
	e.pendingMu.Unlock()

	return id
}

func (e *StreamingEngine[S, F]) CompleteEvaluation(id EvalID, score F) {
	select {
	case e.outcomesChan <- EvalOutcome[F]{ID: id, Score: score}:
	default:
	}
}

func (e *StreamingEngine[S, F]) GetBestImmediate() (Candidate[S, F], bool) {
	if e.currentPool == nil || len(e.currentPool.Members) == 0 {
		return Candidate[S, F]{}, false
	}

	best := e.currentPool.Members[0]
	for _, c := range e.currentPool.Members[1:] {
		if c.Score > best.Score {
			best = c
		}
	}
	return best, true
}

func (e *StreamingEngine[S, F]) SamplePopulation(n int) []S {
	if e.currentPool == nil {
		return nil
	}

	samples := make([]S, 0, n)
	poolSize := len(e.currentPool.Members)

	for i := 0; i < n && i < poolSize; i++ {
		idx := e.rng.IntN(poolSize)
		samples = append(samples, e.currentPool.Members[idx].Data)
	}

	return samples
}

func (e *StreamingEngine[S, F]) ReceiveBest() <-chan Candidate[S, F] {
	return e.bestChan
}

func (e *StreamingEngine[S, F]) initializePool() {
	candidates := make([]Candidate[S, F], e.config.PoolSize)

	for i := 0; i < e.config.PoolSize; i++ {
		candidates[i] = Candidate[S, F]{
			Data:     e.initializer(e.rng),
			Score:    F(0),
			Metadata: make(map[string]any),
		}
	}

	e.currentPool = &Pool[S, F]{
		Members:    candidates,
		Generation: 0,
	}
}

func (e *StreamingEngine[S, F]) evolutionLoop() {
	completedThisGen := 0

	for {
		select {
		case <-e.stopChan:
			return

		case outcome := <-e.outcomesChan:
			e.processOutcome(outcome)
			completedThisGen++

			if completedThisGen >= e.config.MinOutcomesPerGen {
				e.evolveWithBudget()
				completedThisGen = 0
			}

		case <-e.requestChan:
			if best, ok := e.GetBestImmediate(); ok {
				select {
				case e.bestChan <- best:
				default:
				}
			}
		}
	}
}

func (e *StreamingEngine[S, F]) processOutcome(outcome EvalOutcome[F]) {
	e.pendingMu.Lock()
	candidate, ok := e.pending[outcome.ID]
	if ok {
		candidate.Score = outcome.Score
		delete(e.pending, outcome.ID)
	}
	e.pendingMu.Unlock()

	if !ok {
		return
	}

	worstIdx := 0
	worstScore := e.currentPool.Members[0].Score
	for i, c := range e.currentPool.Members {
		if c.Score < worstScore {
			worstScore = c.Score
			worstIdx = i
		}
	}

	if candidate.Score > worstScore {
		e.currentPool.Members[worstIdx] = *candidate
	}
}

func (e *StreamingEngine[S, F]) evolveWithBudget() {
	deadline := time.Now().Add(e.config.TickBudget)
	maxOffspring := e.config.PoolSize / 4

	for i := 0; i < maxOffspring && time.Now().Before(deadline); i++ {
		parents := e.selector.Select(e.currentPool, 2, e.rng)
		offspring := e.combiner.Combine(parents, e.rng)

		for j := range offspring {
			if e.rng.Float64() < e.config.PerturbationRate {
				e.perturbator.Perturb(&offspring[j], e.config.PerturbationStrength, e.rng)
			}

			replaceIdx := e.config.EliteCount + e.rng.IntN(e.config.PoolSize-e.config.EliteCount)
			e.currentPool.Members[replaceIdx] = Candidate[S, F]{
				Data:     offspring[j],
				Score:    F(0),
				Metadata: make(map[string]any),
			}
		}
	}

	e.currentPool.Generation++
}

// Generation returns current evolution generation
func (e *StreamingEngine[S, F]) Generation() int {
	if e.currentPool == nil {
		return 0
	}
	return e.currentPool.Generation
}

// PoolStats returns current population statistics
func (e *StreamingEngine[S, F]) PoolStats() (best, worst, avg F, size int) {
	if e.currentPool == nil || len(e.currentPool.Members) == 0 {
		return
	}

	size = len(e.currentPool.Members)
	best = e.currentPool.Members[0].Score
	worst = e.currentPool.Members[0].Score
	var total F

	for _, c := range e.currentPool.Members {
		if c.Score > best {
			best = c.Score
		}
		if c.Score < worst {
			worst = c.Score
		}
		total += c.Score
	}

	avg = total / F(size)
	return
}

// PendingCount returns number of evaluations awaiting completion
func (e *StreamingEngine[S, F]) PendingCount() int {
	e.pendingMu.RLock()
	defer e.pendingMu.RUnlock()
	return len(e.pending)
}

// EvaluationsStarted returns total evaluations begun (not completed)
func (e *StreamingEngine[S, F]) EvaluationsStarted() uint64 {
	return e.nextEvalID.Load()
}