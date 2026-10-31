# genetic

Evolutionary optimization for simulations and games, in Go. The core package and
every subpackage use only the standard library.

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
| `genetic/persistence` | `genetic` | Population save/load, atomic writes, JSON by default |

`genetic` never imports its own subpackages. Using `StreamingEngine` directly
requires nothing else. `sandbox/genetic` is a runnable example: two streams of
lander controllers, a population each, released in step from opposite corners
toward one pad, where two landers of different streams that meet are both
destroyed. Each population's best answer depends on the other's, so the example
shows what a game needs from the engine: flights that end out of order, a score
that is the mean of several flights against the other stream, archive members
re-flown so their scores follow the other population, and a moved pad that
re-scores every archive. It has a terminal view and a `-headless N` mode that
prints a convergence table.

---

## Core types

```go
type Solution any // Any candidate encoding
type Numeric interface {
    ~int | ~int8 | ~int16 | ~int32 | ~int64 |
        ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
        ~float32 | ~float64
}

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

`Solution` is unconstrained: `[]float64` is the common case, but bit patterns,
permutations, trees and strings work as long as the operators match.

```go
type InitializerFunc[S Solution]            func(rng *rand.Rand) S
type EvaluatorFunc[S Solution, F Numeric]   func(solution S) F
type TerminationFunc[S Solution, F Numeric] func(pool *Pool[S, F], iteration int) bool
type DiversityFunc[S Solution, F Numeric]   func(members []Candidate[S, F]) float64
type EqualFunc[S Solution]                  func(a, b S) bool
```

---

## StreamingEngine

### Model

The engine maintains three disjoint structures:

```
  archive       fixed-capacity, score-sorted, scored candidates only
  proposals     FIFO ring of unevaluated offspring awaiting a caller
  pending       open-addressed table of in-flight evaluations, keyed by EvalID
```

Only the archive participates in selection and statistics. Unevaluated offspring
never pollute either. Because the archive admits a candidate only if it beats the
current tail, **elitism is structural** — there is no elite count to configure and
no way for a good candidate to be displaced by a worse one.

Structural elitism has one failure mode: an unmutated crossover of two similar
parents is often an exact copy of one of them, and a copy of a good member beats
the tail, so an archive left alone fills with clones of its best member and
crossover stops contributing. `SetEqual` closes it:

```go
e.SetEqual(slices.Equal[[]float64])
```

With it a genotype is archived once, at its latest score: a repeat outcome
moves the member to where its new score sorts, up or down, instead of adding a
second copy. The check is one comparison per archive member on each outcome.
That is what lets an archive follow a changing objective: a host that re-flies
archived genotypes now and then, as the lander example does, replaces scores
set when the world was otherwise, where a best-score rule would keep every
lucky outcome for good. `registry` installs it for every species.

Generations advance on *outcome count*, not on batch boundaries:
`MinOutcomesPerGen` completed evaluations increment `Generation` and trigger a
proposal refill. Between refills, an empty queue is topped up one offspring batch
at a time, so `ProposalCapacity` sets how stale a proposal can be: a deep queue
bred at a refill approximates a generational GA, a shallow one keeps every
proposal bred from the current archive.

### Lifecycle

```go
e := genetic.NewStreamingEngine[[]float64, float64](
    initializer, selector, combiner, perturbator, cloner, cfg)
e.Start()                       // Enables Propose, primes the ring
defer e.Stop()                  // Retains the archive; Start resumes
```

`Start`/`Stop` are idempotent and may be interleaved any number of times.
`Stop` does not discard state — `Propose` simply returns a zero `EvalID`.
`Reset` drops pending evaluations, queued proposals and the partial generation's
outcome count; the archive and generation stay.

### Evaluation protocol

```go
genes, id := e.Propose()        // id == 0: stopped, or nothing could be bred
if id != 0 {
    score := evaluateSomewhere(genes)   // May take arbitrarily long
    e.CompleteEvaluation(id, score)     // ... or:
    e.AbandonEvaluation(id)             // discard without scoring
}
```

For genotypes the engine did not generate — probes, seeds, hand-authored
candidates:

```go
id := e.BeginEvaluation(myGenes)  // Copied under a Cloner; kept as is when nil
```

`EvalID` is monotonic and `0` is never issued, so it doubles as a validity flag.

**Completion is optional.** An evaluation occupies one of the eight slots from
its id's home slot in the pending table, and a new one evicts the oldest id in
its window only when all eight hold newer ones. So `PendingCapacity` bounds the
evaluations in flight, an evaluation left alone in a sparse table is never lost,
and under load the oldest goes first. An evicted genotype is recycled,
`PoolStats.Evicted` increments, and a late `CompleteEvaluation` for it is
ignored, which makes it safe for the host to drop evaluations it can no longer
score. Size `PendingCapacity` at about four times the evaluations the host keeps
in flight, so a window rarely fills; a climbing `Evicted` says it did.

Asynchronous completion biases the early archive: evaluations that finish fast
are admitted first and become the first parents. The initializer is used only
until the archive holds two members, so when a short lifetime correlates with a
poor outcome, expect the first generations to breed from poor stock before the
slow, good outcomes arrive. Waiting for a full archive before breeding was
measured on the lander example and changed nothing, so the engine does not.

An elitist archive admits an outcome, not a genotype's expectation, so under a
noisy objective it selects for luck: a controller that crashes two flights in
five is archived on the three it did not. When the noise is the point, as it is
whenever another population is part of the objective, score each genotype by
the mean of several evaluations and report that once; the lander example flies
each genotype four times before completing its evaluation.

### Ownership

The `Cloner` argument decides who owns what:

| `cloner` | `Propose` returns | Caller may mutate |
|---|---|---|
| non-nil | a caller-owned copy | yes |
| `nil` | engine memory | **no** — read-only |

`SliceCloner[S, T]` covers slice genotypes and reuses `dst` capacity. Pass `nil`
only when the caller provably reads the genotype and discards it before the next
`Propose`. `Best`, `Snapshot` and `Checkpoint` clone under the same rule. With a
cloner, buffers the archive drops and evaluations the host abandons are reused
for later offspring, so a steady host allocates nothing per proposal.

### Diversity

`SetDiversity` installs a `DiversityFunc` that is evaluated once per generation
under the engine mutex and published as `PoolStats.Diversity`. `GeneSpread`
is the built-in for bounded real genotypes: the mean per-gene standard deviation
as a fraction of each gene's range, 0 for identical members and about 0.29 for
a uniformly random archive.

```go
e.SetDiversity(genetic.GeneSpread[float64](bounds))
```

### Concurrency

Every mutating method takes one mutex; there are no background goroutines and no
channels. `Propose`, `CompleteEvaluation`, `BeginEvaluation`, `AbandonEvaluation`,
`Reset`, `Best`, `Snapshot`, `Inject`, `Checkpoint`, and `Restore` are safe from
any goroutine.

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
does not. Pending evaluations survive `Inject` and complete into the new archive;
abandon them first if their scores belong to the old objective. Re-scoring a
`Snapshot` and injecting it is how a host changes the objective without
discarding what was learned; the lander example does it when the pad moves, then
re-flies every member live, since a score computed alone knows nothing of the
other stream.

```go
state, err := e.Checkpoint()         // Complete, caller-owned continuation point
err = other.Restore(state)           // Exact next proposal and EvalID
```

`StreamingState` carries the archive, generation and last diversity, plus the
normalized configuration, PCG position, FIFO proposal ring, pending evaluations
with the slots they occupy, partial-generation outcome count, next ID, eviction
counter and running state. `Restore` validates the complete value before changing
the engine, refusing a mismatch with `ErrInvalidStreamingState`, and
`ValidateState` runs that check alone, to preflight several engines before
restoring any. The destination must use the same normalized configuration and
equivalent initializer/operators. The state is plain generic data and
standard-library `encoding/json` can encode it when the solution type can be
encoded. `StreamingStateVersion` names the layout; a checkpoint of another
version is refused.

---

## Engine (generational)

```go
e := genetic.NewEngine[[]float64, float64](
    evaluator, initializer, selector, combiner, perturbator, cfg)
e.SetTerminator(func(p *genetic.Pool[[]float64, float64], iter int) bool {
    return p.Stats.BestScore > 0.99
})
pool, err := e.Run(ctx)
best, err := e.Best()
history := e.History()   // PoolStats per generation
```

Each generation carries the sorted head over as elites (`EliteCount`), fills the
rest by selection → recombination → mutation, scores the new members, then
re-sorts. Solution *generation* is serial because `*rand.Rand` is not
goroutine-safe; *scoring* runs on a worker set of `Parallelism` goroutines over
one atomic index, so **the evaluator must be goroutine-safe when
`Parallelism > 1`**. The result does not depend on `Parallelism`. `Run` honours
context cancellation between generations. The batch engine takes no `Cloner`:
`Best` and the returned `Pool` alias engine memory, and the buffers of the
members a generation replaces are written over by the next generation's
offspring, so a terminator or a caller between runs copies what it keeps. A
steady generation allocates nothing. Its `SetDiversity` takes the same
`DiversityFunc`, evaluated once per generation into `PoolStats.Diversity`.

---

## Operators

All four interfaces are allocation-free in steady state. `Selector` and
`Combiner` write into caller-provided destination slices; `Perturbator` mutates
in place.

```go
type Selector[S Solution, F Numeric] interface {
    Select(members []Candidate[S, F], dst []Candidate[S, F], version uint64, rng *rand.Rand)
}
type Combiner[S Solution, F Numeric] interface {
    Combine(parents []Candidate[S, F], dst []S, rng *rand.Rand) int
}
type Perturbator[S Solution] interface {
    Perturb(solution *S, rate, strength float64, rng *rand.Rand)
}
type Cloner[S Solution] interface {
    Clone(dst, src S) S
}
```

`version` changes whenever `members` does, so a selector may keep work derived
from them between calls; `RouletteSelector` rebuilds its cumulative table only
on a new version. `Combine` returns the number of `dst` entries written and must
reuse `dst[i]` capacity when shape-compatible.

| Implementation | Notes |
|---|---|
| `TournamentSelector` | k uniform draws, best wins. `TournamentSize < 2` clamps to 2 |
| `RouletteSelector` | Score-proportional, min-shifted so negative fitness works. `Floor` keeps the worst member selectable (default `1e-9`). Stateful — not goroutine-safe |
| `UniformCombiner` | Per-element mix at `MixProbability`; emits up to 2 offspring |
| `NPointCombiner` | `Points` crossover sites, alternating segments. Keeps a cut-point scratch buffer |
| `BoundedPerturbator` | Gaussian noise sigma = `strength × (Max-Min)` per gene, returned to `ParameterBounds` by `Boundary`: clamp (default), reflect, or wrap. Genes past `len(Bounds)` are left alone, a NaN result (from a non-finite range) leaves the gene untouched, and zero `strength` is a no-op |
| `GaussianPerturbator` | Unbounded noise for numeric slices |
| `SliceCloner` | Capacity-reusing copy for `~[]T` |
| `GeneSpread` | `DiversityFunc` for bounded `[]float64`: mean normalized per-gene standard deviation |

`Perturb`'s two knobs are independent: `rate` is the per-*element* probability,
`strength` the magnitude. Both are applied by the engine from config; a
`PerturbationRate` of 0.2 with `PerturbationStrength` 0.15 mutates one gene in
five with a sigma of 15% of its range. Size `rate` to the genome: with four
genes, 0.2 leaves 41% of offspring unmutated, so short genomes want a rate near
`1/len` or above, and a genome whose value depends on an exact arrangement
wants a rate near `1/len` or below with a `MixProbability` near 1, so that most
children differ from a parent by one element. `BoundaryClamp` piles mutated
mass onto the bounds; prefer `BoundaryReflect` when a gene is decoded into bins.

`ParameterBounds.Bin(v, n)` decodes a gene into one of `n` equal bins, half-open
at the top so `Max` lands in bin `n-1`; `BinCenter(i, n)` is its inverse.

A genome that is not a real vector brings its own perturbator and, if wanted,
diversity metric, and reuses the rest: `[]bool` patterns run under
`TournamentSelector`, `UniformCombiner[[]bool, bool, float64]` and
`SliceCloner[[]bool, bool]` with a ten-line perturbator that flips each bit
with probability `rate` and ignores `strength`.

---

## Configuration

`EngineConfig` is shared; `StreamingConfig` embeds it. Take a defaults value and
override fields — never build one from zero. `Normalize` replaces zero sizes
and budgets with defaults and raises `Parallelism` and `MinOutcomesPerGen` to 1,
but keeps a zero `PerturbationRate` or `PerturbationStrength`, so a zero config
never mutates.

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
| `Parallelism` | 4 | Batch engine evaluator workers |
| `Seed` | 0 | PCG seed; every value including 0 is reproducible |
| `RefillMode` | `RefillDeterministic` | Full deterministic refill; opt into `RefillTimeBudget` only when scheduling-dependent streams are acceptable |
| `TickBudget` | 500µs | Wall-clock cap for `RefillTimeBudget`; ignored by deterministic refills |
| `ProposalCapacity` | 32 | Depth of the unevaluated offspring ring; rounded up to a power of two |
| `PendingCapacity` | 512 | Pending-table slots, rounded up to a power of two; bounds the evaluations in flight, and an evaluation is evicted only when the eight slots from its id's home slot all hold newer ones |
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
cfg.PerturbationStrength = 0.1
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
`TournamentSize` / `MixProbability` take the package defaults, an unset `Name`
becomes `species_<id>`, and a nil `EngineConfig` takes `DefaultStreamingConfig`;
mutation rate and strength are the engine configuration's. Every species engine
runs tournament selection, uniform crossover, bounded Gaussian mutation under
`Boundary`, and duplicate rejection. `Start` is idempotent — it loads and starts
only species not already running, so it is safe to call after each late
registration. A missing persistence file is not an error; other I/O failures are
returned. `Deregister` stops and drops one species, `Registered` lists the held
ids, and `Reset` clears every species' pending evaluations and queued proposals
while keeping its archive.

Hot-path methods (`Sample`, `SampleScout`, `Stats`, `ReportFitness`) resolve the
species through an `atomic.Pointer` array, with no map and no registry lock.
`Register`, `Deregister`, `Registered`, `Start`, `Export` and `Import` take a
mutex.

`Export`/`Import` are the registry-level exact-continuation contract. Each
`SpeciesState` contains the engine checkpoint plus the scout PCG position and
stratification counter. `Import` refuses a state whose species set, IDs or names
differ from the registered ones, and validates every species before restoring
any. This is distinct from `SaveAll`, whose files intentionally retain only
learned archives.

### Sampling

```go
genes, evalID := reg.Sample(1)       // Archive-derived proposal
probe, probeID := reg.SampleScout(1) // Stratified probe
reg.ReportFitness(1, evalID, score)  // Bypasses the aggregator
reg.AbandonFitness(1, probeID)       // Subject never materialized
```

`Sample` degrades gracefully: when the engine is stopped it returns the
per-gene bound *midpoints* with `evalID == 0`, so a host can always spawn
something. Test `evalID != 0` to distinguish a real proposal from the fallback.
An unregistered species returns `nil, 0`.

`SampleScout` synthesizes a genotype outside the archive: `gene[0]` walks
`ProbeBins` bin centers round-robin, remaining genes are uniform within bounds.
This guarantees continuous coverage of the `gene[0]` phenotype axis regardless of
how far the archive has converged — the intended use is keeping every discrete
variant under evaluation when `gene[0]` is decoded into a category. Returns
`nil, 0` when the species is stopped. `ProbeBins == 0` disables stratification.

Decode a categorical `gene[0]` with `Bounds[0].Bin(gene[0], ProbeBins)`, whose
bin centers are exactly the scout probes, and register it with
`Boundary: genetic.BoundaryReflect`: clamped mutations pile onto both bounds and
over-weight the end bins.

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

`Stats(id)` returns the species engine's `genetic.PoolStats[float64]`, or the
zero value for an unregistered id. Lock-free; poll freely. `Evicted` climbing
steadily means `PendingCapacity` is undersized or the host is dropping
evaluations.

---

## fitness

```go
type Aggregator interface {
    Calculate(metrics tracking.MetricBundle, ctx Context) float64
}
```

`WeightedAggregator` is a weighted sum of optionally normalized metrics:

```go
const threat = "threat"   // A context key is the host's own

agg := &fitness.WeightedAggregator{
    Weights: map[string]float64{
        tracking.MetricTicksAlive: 0.4,
        "avg_speed":               0.6,
    },
    Normalizers: map[string]fitness.NormalizeFunc{
        tracking.MetricTicksAlive: fitness.NormalizeCap(600),
    },
    WeightAdjuster: func(key string, w float64, ctx fitness.Context) float64 {
        if t, ok := ctx.Get(threat); ok && t > 0.7 && key == "avg_speed" {
            return w * 2
        }
        return w
    },
}
```

Metrics absent from the bundle are skipped, not treated as zero. `WeightAdjuster`
is called for each weighted key present in the bundle, only when `ctx` is
non-nil, and rescales in place — no map is rebuilt. Terms are summed in key
order, so a score is bit-identical across runs; map iteration order would
otherwise perturb rounding and break seeded replay.

Normalizers: `NormalizeLinear(min, max)` clamped to [0,1] · `NormalizeInverse(scale)`
= `1/(1+raw/scale)`, monotone decreasing, for costs like distance or time ·
`NormalizeCap(max)` = `raw/max` clamped to [0,1]. A degenerate range
(`max <= min`, `max <= 0`) makes Linear and Cap return a constant 0, and
Inverse substitutes a scale of 1 for `scale <= 0`.

`Context` is a `Get(key) (float64, bool)` lookup; `MapContext` is the trivial
implementation. The package defines no keys: what a host measures is its own.

---

## tracking

`MetricBundle` is `map[string]float64`. `StandardCollector` accumulates one
entity's samples and emits derived series on `Finalize`:

| Emitted key | From |
|---|---|
| `ticks_alive` | `Collect` and `Tick` call count |
| `avg_<k>`, `min_<k>`, `max_<k>` | every key seen |
| `time_<k>` | seconds where value > 0.5 (boolean-metric convention) |

`Collect` records a whole bundle for one tick; `Add`, `Flag` and `Tick` record a
single sample, active time, or a tick without a sample. Derived names are cached
per key and survive `Reset`, so a pooled collector allocates strings only on
first sighting. `Finalize` overlays the death bundle last, so death-condition
keys win.

`CompositeCollector` reads `member_count` from each bundle and adds
`peak_member_count`, `final_member_count`, and (when the peak is positive)
`member_retention` for multi-part entities. The package names only the keys its
collectors read or write (`MetricTicksAlive`, `MetricMemberCount`,
`MetricPeakMemberCount`, `MetricFinalMemberCount`, `MetricMemberRetention`);
every other key is the host's.

`CollectorPool` is a LIFO free list — deterministic reuse, no `sync.Pool`
scavenging, resets on acquire. Both collectors satisfy:

```go
type Collector interface {
    Collect(metrics MetricBundle, dt time.Duration)
    Finalize(deathCondition MetricBundle) MetricBundle
    Reset()
}
```

Collectors are not goroutine-safe. The registry locks only its map of active
collectors and calls `Collect` and `Finalize` outside it, so feed one
evaluation from one goroutine.

---

## persistence

```go
store := persistence.NewManager("./state", nil)  // nil codec selects JSON
reg := registry.NewRegistry(store)
// ... register, Start, evolve ...
err := reg.SaveAll()
```

One file per species named after `SpeciesConfig.Name`. Writes go through a temp
file + `fsync` + `rename`, so a crash mid-save leaves the previous file intact.
`PopulationDTO.Version` is stamped from `SchemaVersion` on every save.

File persistence is archive persistence, not an in-flight checkpoint: proposal
queues, pending evaluations, IDs and RNG positions are not written. Use
`Registry.Export`/`Import` when the next sample must continue exactly.

Supply a `Codec` to change format; the DTOs carry `toml` tags beside their
`json` tags, so a TOML codec is a few lines over any TOML package:

```go
type Codec interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
    Ext() string   // Including the leading dot
}

type tomlCodec struct{}

func (tomlCodec) Marshal(v any) ([]byte, error)   { return toml.Marshal(v) }
func (tomlCodec) Unmarshal(b []byte, v any) error { return toml.Unmarshal(b, v) }
func (tomlCodec) Ext() string                     { return ".toml" }
```

Implement `Store` directly for non-file backends — the registry only needs
`Save`/`Load`.

---

## Worked example

Maximize `f(x, y) = -(x-3)² - (y+1)²` with asynchronous scoring. `sandbox/genetic`
is the full-size version of this loop, with evaluations that take hundreds of
ticks and finish out of order.

```go
package main

import (
    "fmt"
    "math/rand/v2"
    "slices"

    "github.com/lixenwraith/vif/pkg/genetic"
)

func main() {
    bounds := []genetic.ParameterBounds{{Min: -10, Max: 10}, {Min: -10, Max: 10}}

    cfg := genetic.DefaultStreamingConfig()
    cfg.PoolSize = 24
    cfg.MinOutcomesPerGen = 4
    cfg.PerturbationRate = 0.6
    cfg.PerturbationStrength = 0.1
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
        &genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect},
        genetic.SliceCloner[[]float64, float64]{},
        cfg,
    )
    e.SetEqual(slices.Equal[[]float64])
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
- Outside `Inject` and `Restore`, a candidate leaves the archive only by being
  displaced by a strictly better one, or, under `SetEqual`, by a new outcome for
  its own genotype.
- With `SetEqual`, no two archive members share a genotype, and a member's score
  is its genotype's latest.
- Unevaluated genotypes never appear in the archive, statistics, or selection.
- `EvalID` is monotonic; `0` is never issued.
- `pending` never exceeds `PendingCapacity`, regardless of host behaviour, and
  an evaluation is evicted only when the eight slots from its home slot all
  hold newer ones.
- With a non-nil `Cloner`, no solution handed to a caller aliases engine memory.
- `Stats()` never blocks and never observes a torn snapshot.
- A successful `Restore(Checkpoint())` reproduces the next proposal, ID, every
  later state transition and every later eviction under the same operation
  sequence.

## Gotchas

- Build config from `DefaultConfig()` / `DefaultStreamingConfig()`. A zero
  `StreamingConfig` gets default sizes, `Parallelism` and `MinOutcomesPerGen`
  of 1, and zero mutation rate and strength.
- `EliteCount` does nothing in the streaming engine.
- Without `SetEqual` the streaming archive converges to copies of one genotype;
  `Diversity` reads 0 and `AverageScore` equals `BestScore` long before the
  search is done.
- Under a noisy objective the archive holds lucky outcomes: complete each
  evaluation with the mean of several, and re-evaluate archived genotypes when
  the objective can move.
- `PendingCapacity` and `ProposalCapacity` round up to powers of two. Size the
  pending table at several times the evaluations in flight; a window of eight
  consecutive live ids is what evicts.
- `Snapshot`, `Checkpoint`, and `Best` clone when a `Cloner` is configured;
  `Stats` does not touch solutions at all. Prefer `Stats` for telemetry.
- `RefillTimeBudget` intentionally weakens seeded reproducibility; deterministic
  refill is the default.
- The batch `Engine`'s evaluator runs concurrently when `Parallelism > 1`, and
  its pool is rewritten in place each generation: copy what a terminator keeps.
- `RouletteSelector` and `NPointCombiner` carry scratch buffers; do not share
  one instance across engines.
