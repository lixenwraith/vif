# genetic

Generic evolutionary optimization for Go. No dependencies outside the standard
library except `github.com/lixenwraith/toml` in the optional `persistence`
subpackage.

Two engines share one operator set:

- **`StreamingEngine`** — steady-state (mu+lambda), caller-driven, asynchronous
  evaluation. Built for simulations where fitness is only known long after a
  candidate is handed out, and where the number of evaluations in flight is
  dictated by the host, not the optimizer.
- **`Engine`** — classic generational GA with a synchronous evaluator function.
  Use when you can score a candidate on demand.

---

## Package layout

| Package | Depends on | Purpose |
|---|---|---|
| `genetic` | stdlib | Engines, operators, config, core types |
| `genetic/registry` | `genetic`, `fitness`, `tracking`, `persistence` | Multi-population manager keyed by a `uint8` id |
| `genetic/fitness` | `tracking` | Scalarizing multi-objective metrics into one score |
| `genetic/tracking` | stdlib | Per-entity metric accumulation over a lifetime |
| `genetic/persistence` | `genetic`, `toml` | Population save/load, atomic writes |

`genetic` never imports its own subpackages. Using `StreamingEngine` directly
requires nothing else.

---

## Core types

```go
type Solution any                      // Any candidate encoding
type Numeric interface { ~int | ... | ~float64 }

type Candidate[S Solution, F Numeric] struct {
    Data  S
    Score F
}

type Pool[S Solution, F Numeric] struct {
    Members    []Candidate[S, F]  // Sorted by Score descending
    Generation int
    Stats      PoolStats[F]
}

type PoolStats[F Numeric] struct {
    BestScore, WorstScore, AverageScore F
    Diversity                           float64
    Size, Generation, Pending           int
    Evaluations, Evicted                uint64
}
```

`Solution` is unconstrained: `[]float64` is the common case, but permutations,
trees, and strings work as long as the operators match.

---

## StreamingEngine

### Model

The engine maintains three disjoint structures:

```
  archive       fixed-capacity, score-sorted, scored candidates only
  proposals     FIFO ring of unevaluated offspring awaiting a caller
  pending       slot table of in-flight evaluations, keyed by EvalID
```

Only the archive participates in selection and statistics. Unevaluated offspring
never pollute either. Because the archive admits a candidate only if it beats the
current tail, **elitism is structural** — there is no elite count to configure and
no way for a good candidate to be displaced by a worse one.

Generations advance on *outcome count*, not on batch boundaries:
`MinOutcomesPerGen` completed evaluations increment `Generation` and trigger a
proposal refill.

### Lifecycle

```go
e := genetic.NewStreamingEngine[[]float64, float64](
    initializer, selector, combiner, perturbator, cloner, cfg)
e.Start()                       // Enables Propose, primes the ring
defer e.Stop()                  // Retains the archive; Start resumes
```

`Start`/`Stop` are idempotent and may be interleaved any number of times.
`Stop` does not discard state — `Propose` simply returns a zero `EvalID`.

### Evaluation protocol

```go
genes, id := e.Propose()        // id == 0 means the engine is stopped
if id != 0 {
    score := evaluateSomewhere(genes)   // May take arbitrarily long
    e.CompleteEvaluation(id, score)     // ... or:
    e.AbandonEvaluation(id)             // discard without scoring
}
```

For genotypes the engine did not generate — probes, seeds, hand-authored
candidates:

```go
id := e.BeginEvaluation(myGenes)  // Engine keeps a copy; caller retains myGenes
```

`EvalID` is monotonic and `0` is never issued, so it doubles as a validity flag.

**Completion is optional.** An id that is never completed or abandoned is
eventually evicted when a later id lands in its slot; the genotype is recycled
and `PoolStats.Evicted` increments. This bounds memory without a sweep and makes
it safe for the host to drop evaluations it can no longer score. Sizing rule:
`PendingCapacity` must exceed the maximum number of concurrently live
evaluations, or good candidates will be evicted before they finish.

### Ownership

The `Cloner` argument decides who owns what:

| `cloner` | `Propose` returns | Caller may mutate |
|---|---|---|
| non-nil | a caller-owned copy | yes |
| `nil` | engine memory | **no** — read-only |

`SliceCloner[S, T]` covers slice genotypes and reuses `dst` capacity. Pass `nil`
only when the caller provably reads the genotype and discards it before the next
`Propose`. `Best` and `Snapshot` clone under the same rule.

### Concurrency

Every mutating method takes one mutex; there are no background goroutines and no
channels. `Propose`, `CompleteEvaluation`, `BeginEvaluation`, `AbandonEvaluation`,
`Reset`, `Best`, `Snapshot`, and `Inject` are safe from any goroutine.

`Stats()` is lock-free: it dereferences an `atomic.Pointer[PoolStats]` republished
after each state transition. It is safe to poll every frame. `Generation()`,
`PendingCount()`, and `EvaluationsStarted()` read the same snapshot.

Operators are invoked under the mutex and need not be goroutine-safe. Stateful
selectors such as `RouletteSelector` rely on this.

### Determinism

Identical `Seed` values and operation sequences yield identical proposal streams.
`RefillDeterministic` is the default: it fills the bounded proposal queue by work
count and never consults a clock. `Seed == 0` is a valid, reproducible seed; the
package never chooses a wall-clock seed on a caller's behalf.

`RefillTimeBudget` is an explicit throughput/latency tradeoff. It stops a refill
after `TickBudget`, so machine load may change how many proposals were derived
from the current archive and therefore the later stream. Use it only when that
variation is acceptable.

The determinism contract assumes the initializer and operators derive their
semantic output only from their arguments and the supplied `*rand.Rand`. Built-in
operators satisfy that contract; caller-owned hidden state is outside an engine
checkpoint.

### Archive persistence and exact continuation

```go
p := e.Snapshot()                    // Deep copy of the archive + stats
e.Inject(p.Members, p.Generation)    // Replaces the archive; engine takes ownership
```

`Snapshot`/`Inject` are deliberately archive-only persistence hooks. `Inject`
discards queued proposals and starts a new stream position from the receiving
engine; use them when retaining learned candidates matters but exact continuation
does not.

```go
state, err := e.Checkpoint()         // Complete, caller-owned continuation point
err = other.Restore(state)           // Exact next proposal and EvalID
```

`StreamingState` also carries the normalized configuration, PCG position, FIFO
proposal ring, pending evaluations, partial-generation outcome count, next ID,
eviction counter and running state. `Restore` validates the complete value before
changing the engine. The destination must use the same normalized configuration
and equivalent initializer/operators. The state is plain generic data and
standard-library `encoding/json` can encode it when the solution type can be
encoded.

---

## Engine (generational)

```go
e := genetic.NewEngine[[]float64, float64](
    evaluator, initializer, selector, combiner, perturbator, cfg)
e.SetTerminator(func(p *Pool[[]float64, float64], iter int) bool {
    return p.Stats.BestScore > 0.99
})
pool, err := e.Run(ctx)
best, err := e.Best()
history := e.History()   // PoolStats per generation
```

Each `step` carries the sorted head over as elites (`EliteCount`), fills the rest
by selection → recombination → mutation, scores the new members, then re-sorts.
Solution *generation* is serial because `*rand.Rand` is not goroutine-safe;
*scoring* fans out to `Parallelism` goroutines, so **the evaluator must be
goroutine-safe when `Parallelism > 1`**. `Run` honours context cancellation
between generations.

---

## Operators

All four interfaces are allocation-free in steady state. `Selector` and
`Combiner` write into caller-provided destination slices; `Perturbator` mutates
in place.

```go
type Selector[S, F] interface {
    Select(members, dst []Candidate[S, F], rng *rand.Rand)
}
type Combiner[S, F] interface {
    Combine(parents []Candidate[S, F], dst []S, rng *rand.Rand) int
}
type Perturbator[S] interface {
    Perturb(solution *S, rate, strength float64, rng *rand.Rand)
}
type Cloner[S] interface {
    Clone(dst, src S) S
}
```

`Combine` returns the number of `dst` entries written and must reuse `dst[i]`
capacity when shape-compatible.

| Implementation | Notes |
|---|---|
| `TournamentSelector` | k uniform draws, best wins. `TournamentSize < 2` clamps to 2 |
| `RouletteSelector` | Score-proportional, min-shifted so negative fitness works. `Floor` keeps the worst member selectable (default `1e-9`). Stateful — not goroutine-safe |
| `UniformCombiner` | Per-element mix at `MixProbability`; emits up to 2 offspring |
| `NPointCombiner` | `Points` crossover sites, alternating segments |
| `BoundedPerturbator` | Gaussian noise sigma = `strength × (Max-Min)` per gene, clamped to `ParameterBounds`. Genes past `len(Bounds)` are left alone. Non-finite results are rejected, never written |
| `GaussianPerturbator` | Unbounded noise for numeric slices |
| `SliceCloner` | Capacity-reusing copy for `~[]T` |

`Perturb`'s two knobs are independent: `rate` is the per-*element* probability,
`strength` the magnitude. Both are applied by the engine from config; a
`PerturbationRate` of 0.2 with `PerturbationStrength` 0.15 mutates one gene in
five by 15% of its range.

---

## Configuration

`EngineConfig` is shared; `StreamingConfig` embeds it. Take a defaults value and
override fields — never build one from zero, since `Normalize` treats several
zeros as "unset" and substitutes the default rather than the literal.

```go
cfg := genetic.DefaultStreamingConfig()
cfg.PoolSize = 64
cfg.Seed = 0xC0FFEE
```

| Field | Default | Meaning |
|---|---|---|
| `PoolSize` | 32 | Archive capacity (scored candidates retained) |
| `EliteCount` | 4 | Batch engine only; the streaming archive is elitist by construction and ignores it |
| `PerturbationRate` | 0.2 | Per-element mutation probability, clamped to [0,1] |
| `PerturbationStrength` | 0.15 | Mutation sigma as a fraction of element range, clamped to [0,1] |
| `MaxIterations` | 1000 | Batch engine generation cap |
| `Parallelism` | 4 | Batch engine evaluator concurrency |
| `Seed` | 0 | PCG seed; every value including 0 is reproducible |
| `RefillMode` | `RefillDeterministic` | Full deterministic refill; opt into `RefillTimeBudget` only when scheduling-dependent streams are acceptable |
| `TickBudget` | 500µs | Wall-clock cap for `RefillTimeBudget`; ignored by deterministic refills |
| `ProposalCapacity` | 32 | Depth of the unevaluated offspring ring; rounded up to a power of two |
| `PendingCapacity` | 512 | In-flight evaluation slots; rounded up to a power of two. Older entries are evicted on collision |
| `MinOutcomesPerGen` | 5 | Completed evaluations that advance `Generation` |
| `DefaultTournamentSize` | 3 | Consumed by `registry`, not by config |
| `DefaultMixProbability` | 0.5 | Consumed by `registry`, not by config |

`Normalize()` is applied inside both constructors; calling it yourself is
harmless and idempotent.

---

## registry

Manages up to 256 independent populations addressed by `SpeciesID uint8`, each
with its own engine, bounds, and optional metric pipeline.

```go
reg := registry.NewRegistry(store)   // store may be nil to disable persistence

cfg := genetic.DefaultStreamingConfig()
reg.Register(registry.SpeciesConfig{
    ID:        1,
    Name:      "walker",
    GeneCount: 3,
    Bounds: []genetic.ParameterBounds{
        {Min: 0, Max: 1}, {Min: 0, Max: 10}, {Min: -1, Max: 1},
    },
    ProbeBins:    7,
    EngineConfig: &cfg,
}, aggregator)

reg.Start()                          // Loads persisted state, starts new species
defer reg.Stop()
```

`Register` validates `GeneCount == len(Bounds)` and rejects duplicate ids. Unset
`TournamentSize` / `MixProbability` / `Name` are filled from package defaults.
`Start` is idempotent — it loads and starts only species not already running, so
it is safe to call after each late registration. A missing persistence file is
not an error; other I/O failures are returned.

Hot-path methods (`Sample`, `SampleScout`, `Stats`, `ReportFitness`) resolve the
species through an `atomic.Pointer` array — no map, no lock. `Register`/`Start`
take a mutex.

`Export`/`Import` are the registry-level exact-continuation contract. Each
`SpeciesState` contains the engine checkpoint plus the scout PCG position and
stratification counter; species name and ID must match exactly. This is distinct
from `SaveAll`, whose files intentionally retain only learned archives.

### Sampling

```go
genes, evalID := reg.Sample(1)       // Archive-derived proposal
genes, evalID := reg.SampleScout(1)  // Stratified probe
reg.ReportFitness(1, evalID, score)  // Bypasses the aggregator
reg.AbandonFitness(1, evalID)        // Subject never materialized
```

`Sample` degrades gracefully: when the engine is stopped it returns the
per-gene bound *midpoints* with `evalID == 0`, so a host can always spawn
something. Test `evalID != 0` to distinguish a real proposal from the fallback.

`SampleScout` synthesizes a genotype outside the archive: `gene[0]` walks
`ProbeBins` bin centers round-robin, remaining genes are uniform within bounds.
This guarantees continuous coverage of the `gene[0]` phenotype axis regardless of
how far the archive has converged — the intended use is keeping every discrete
variant under evaluation when `gene[0]` is decoded into a category. Returns
`nil, 0` when the species is stopped. `ProbeBins == 0` disables stratification.

If `gene[0]` is decoded as `int(gene[0] * N)`, register its `Max` strictly below
the top of the decode range; clamped mutations land exactly on `Max` and would
otherwise over-weight the last bin.

### Metric pipeline (optional)

Only needed when fitness is a function of behaviour observed over time. Hosts
that compute a scalar directly should call `ReportFitness` and ignore this.

```go
reg.BeginTracking(id, evalID)                         // Acquires a pooled collector
reg.CollectMetrics(id, evalID, bundle, dt)            // Per tick
reg.CompleteTracking(id, evalID, deathBundle, ctx)    // Finalize → aggregate → score
```

`CompleteTracking` with a nil `Aggregator` **abandons** the evaluation rather than
scoring it zero — a missing aggregator is a configuration error, not a fitness
signal of zero.

### Stats

```go
type Stats struct {
    Generation                            int
    BestFitness, WorstFitness, AvgFitness float64
    Diversity                             float64
    PoolSize, PendingCount                int
    TotalEvals, Evicted                   uint64
}
```

Lock-free; poll freely. `Evicted` climbing steadily means `PendingCapacity` is
undersized or the host is dropping evaluations.

---

## fitness

```go
type Aggregator interface {
    Calculate(metrics tracking.MetricBundle, ctx Context) float64
}
```

`WeightedAggregator` is a weighted sum of optionally normalized metrics:

```go
agg := &fitness.WeightedAggregator{
    Weights: map[string]float64{
        tracking.MetricTicksAlive: 0.4,
        "avg_speed":               0.6,
    },
    Normalizers: map[string]fitness.NormalizeFunc{
        tracking.MetricTicksAlive: fitness.NormalizeCap(600),
    },
    WeightAdjuster: func(key string, w float64, ctx fitness.Context) float64 {
        if t, ok := ctx.Get(fitness.ContextThreatLevel); ok && t > 0.7 && key == "avg_speed" {
            return w * 2
        }
        return w
    },
}
```

Metrics absent from the bundle are skipped, not treated as zero. `WeightAdjuster`
is called per key per evaluation and rescales in place — no map is rebuilt.

Normalizers: `NormalizeLinear(min, max)` clamped to [0,1] · `NormalizeInverse(scale)`
= `1/(1+raw/scale)`, monotone decreasing, for costs like distance or time ·
`NormalizeCap(max)` = `min(raw/max, 1)`. All three handle degenerate arguments by
returning a constant 0 rather than dividing by zero.

`Context` is a `Get(key) (float64, bool)` lookup; `MapContext` is the trivial
implementation.

---

## tracking

`MetricBundle` is `map[string]float64`. `StandardCollector` accumulates one
entity's samples and emits derived series on `Finalize`:

| Emitted key | From |
|---|---|
| `ticks_alive` | `Collect` call count |
| `avg_<k>`, `min_<k>`, `max_<k>` | every key seen |
| `time_<k>` | seconds where value > 0.5 (boolean-metric convention) |

Derived names are cached per key and survive `Reset`, so a pooled collector
allocates strings only on first sighting. `Finalize` overlays the death bundle
last, so death-condition keys win.

`CompositeCollector` adds `peak_member_count`, `final_member_count`, and
`member_retention` for multi-part entities.

`CollectorPool` is a LIFO free list — deterministic reuse, no `sync.Pool`
scavenging, resets on acquire. Both collectors satisfy:

```go
type Collector interface {
    Collect(metrics MetricBundle, dt time.Duration)
    Finalize(deathCondition MetricBundle) MetricBundle
    Reset()
}
```

Collectors are not goroutine-safe; the registry serializes access.

---

## persistence

```go
store := persistence.NewManager("./state", nil)  // nil codec selects TOML
reg := registry.NewRegistry(store)
...
reg.SaveAll()
```

One file per species named after `SpeciesConfig.Name`. Writes go through a temp
file + `fsync` + `rename`, so a crash mid-save leaves the previous file intact.
`PopulationDTO.Version` is stamped from `SchemaVersion` on every save.

File persistence is archive persistence, not an in-flight checkpoint: proposal
queues, pending evaluations, IDs and RNG positions are not written. Use
`Registry.Export`/`Import` when the next sample must continue exactly.

Supply a `Codec` to change format:

```go
type Codec interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
    Ext() string   // Including the leading dot
}
```

`TOMLCodec` (default) and `JSONCodec` ship with the package. Implement `Store`
directly for non-file backends — the registry only needs `Save`/`Load`.

---

## Worked example

Maximize `f(x, y) = -(x-3)² - (y+1)²` with asynchronous scoring.

```go
package main

import (
    "fmt"
    "math/rand/v2"

    "github.com/lixenwraith/vi-fighter/pkg/genetic"
)

func main() {
    bounds := []genetic.ParameterBounds{{Min: -10, Max: 10}, {Min: -10, Max: 10}}

    cfg := genetic.DefaultStreamingConfig()
    cfg.PoolSize = 24
    cfg.MinOutcomesPerGen = 4
    cfg.PerturbationRate = 0.6
    cfg.Seed = 0xC0FFEE

    e := genetic.NewStreamingEngine[[]float64, float64](
        func(rng *rand.Rand) []float64 {
            g := make([]float64, len(bounds))
            for i, b := range bounds {
                g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
            }
            return g
        },
        &genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
        &genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
        &genetic.BoundedPerturbator{Bounds: bounds},
        genetic.SliceCloner[[]float64, float64]{},
        cfg,
    )
    e.Start()
    defer e.Stop()

    for range 2000 {
        g, id := e.Propose()
        if id == 0 {
            break
        }
        dx, dy := g[0]-3, g[1]+1
        e.CompleteEvaluation(id, -(dx*dx + dy*dy))
    }

    best, _ := e.Best()
    s := e.Stats()
    fmt.Printf("gen=%d evals=%d best=%.6f at (%.4f, %.4f)\n",
        s.Generation, s.Evaluations, best.Score, best.Data[0], best.Data[1])
}
```

Deferring `CompleteEvaluation` — holding ids across loop iterations, completing
out of order, abandoning some — changes nothing about correctness. That is the
whole point of the streaming model.

---

## Invariants

- The archive is sorted by `Score` descending and never exceeds `PoolSize`.
- A candidate leaves the archive only by being displaced by a strictly better one.
- Unevaluated genotypes never appear in the archive, statistics, or selection.
- `EvalID` is monotonic; `0` is never issued.
- `pending` never exceeds `PendingCapacity`, regardless of host behaviour.
- With a non-nil `Cloner`, no solution handed to a caller aliases engine memory.
- `Stats()` never blocks and never observes a torn snapshot.
- A successful `Restore(Checkpoint())` reproduces the next proposal, ID and every
  later state transition under the same operation sequence.

## Gotchas

- Build config from `DefaultConfig()` / `DefaultStreamingConfig()`. A zero
  `StreamingConfig` normalizes every field to its default, silently discarding
  intentional zeros.
- `EliteCount` does nothing in the streaming engine.
- `PendingCapacity` and `ProposalCapacity` round up to powers of two.
- `Snapshot`, `Checkpoint`, and `Best` clone when a `Cloner` is configured;
  `Stats` does not touch solutions at all. Prefer `Stats` for telemetry.
- `RefillTimeBudget` intentionally weakens seeded reproducibility; deterministic
  refill is the default.
- The batch `Engine`'s evaluator runs concurrently when `Parallelism > 1`.
- `RouletteSelector` carries a scratch buffer; do not share one instance across
  engines.
