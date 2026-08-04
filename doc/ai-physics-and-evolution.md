# Navigation, Physics, Adaptation, and Evolution

Vi-Fighter's moving actors share reusable math and algorithm packages, while
systems retain game-specific policy. This document covers four related layers:
grid navigation, continuous movement/collision, route adaptation, and genetic
parameter evolution.

## 1. Package relationships

```mermaid
flowchart TD
    Systems["species and navigation systems"] --> Navigation["pkg/navigation"]
    Systems --> Physics["pkg/physics"]
    Systems --> Genetic["pkg/genetic registry"]
    Navigation --> VMath["pkg/vmath"]
    Physics --> VMath
    Adapt["AdaptationSystem"] --> Navigation
```

`pkg/navigation` and `pkg/physics` accept data/callbacks instead of reaching
directly into the ECS where practical. `NavigationSystem` adapts wall/target
resources to those APIs. `GeneticSystem` adapts entity lifetime events to the
generic evolutionary registry. `AdaptationSystem` is separate from genetics:
it learns route selection, not actor parameters.

## 2. Flow fields

A `FlowField` runs weighted Dijkstra outward from one or more target cells and
stores, for every reachable cell, the direction that descends toward a target.

| Step | Weighted cost |
|---|---|
| Horizontal | 10 |
| Vertical | 20 |
| Diagonal | 22 |

The asymmetric costs compensate for a terminal cell's approximate 2:1 visual
aspect ratio. Eight-way movement is allowed, but a diagonal cannot cut the
corner when either adjacent cardinal cell is blocked.

```mermaid
flowchart TD
    Targets["one or more targets"] --> Seed["seed passable target cells"]
    Seed --> Dijkstra["weighted reverse Dijkstra"]
    Dijkstra --> Distance["per-cell distance"]
    Dijkstra --> Direction["per-cell downhill direction"]
```

If a requested target cell is blocked, the field searches outward up to eight
cells and seeds the first passable ring as virtual targets with axis-weighted
gap costs. This lets a large actor navigate toward an occupied anchor without
pretending its footprint can stand in the blocked cell.

Generation stamps avoid clearing full distance/direction arrays on each
recompute. A reusable binary min-heap reduces allocation. `DirNone` identifies
blocked/unreachable cells and `DirTarget` a seed/arrival cell.

## 3. Flow-field cache and target groups

Computing a field over a large map every 50 ms is too expensive, so
`FlowFieldCache` latches dirty state and throttles recomputation. It recomputes
when:

- target count changes;
- a target moves at least the configured Manhattan dirty distance;
- wall/level logic explicitly marks it dirty and the tick threshold is met;
- the existing field is invalid.

Target groups allow multiple actors/structures to define one navigation goal.
Group 0 is the cursor; other groups can contain up to eight targets. A field
seeded from a group naturally selects the nearest reachable target at each
cell.

`NavigationComponent` records whether an actor follows a normal group flow
field or a route graph, along with graph/route IDs and movement state. The
system updates component actors but delegates special formation movement to
their species systems.

## 4. Composite passability

Point passability is insufficient for a 5-by-3 eye or other composite.
`CompositePassability` precomputes whether the entire footprint fits at every
candidate header coordinate, using header-to-top-left offsets.

```mermaid
flowchart LR
    Walls["wall callback"] --> Footprint["test every footprint cell"]
    Footprint --> Grid["valid header-position grid"]
    Grid --> Field["flow field or route graph"]
```

It supports full rebuilds and region-of-interest rebuilds after local changes.
Its `IsBlocked` callback can be handed directly to flow-field and route-graph
algorithms. As a result, a path guarantees footprint clearance at sampled
header cells, rather than requiring the species to correct point paths after
the fact.

## 5. Route graphs

A route graph generates several meaningfully distinct, near-optimal corridors
between a source and target:

1. run a normal weighted Dijkstra to establish the optimal distance;
2. accept the path if within the configured tolerance;
3. dilate its corridor and penalize those cells;
4. rerun penalized Dijkstra to push the next candidate elsewhere;
5. reject paths whose corridor overlap is above the configured percentage;
6. stop at the route limit, attempt limit, no path, or excessive true cost.

```mermaid
flowchart TD
    Shortest["shortest path"] --> Accept["accept distinct in-tolerance route"]
    Accept --> Penalize["penalize dilated corridor"]
    Penalize --> Next["find next penalized shortest path"]
    Next --> Accept
```

Each accepted route stores decimated waypoints, true weighted distance, a
normalized inverse-distance initial weight, and a flow field constrained to its
dilated corridor. The graph also records the footprint/header offsets used to
compute it.

Graph IDs live in `RouteGraphResource`. Gateways attach the graph ID and a
sampled route ID to spawned enemies. `:graph` and `:flow` expose the structures
through the debug renderer.

## 6. Online route adaptation

`AdaptationSystem` maintains an independent route population for each
`(routeGraphID, enemy subtype)`. A newly computed graph seeds subtype 0 from
inverse-distance route weights; subtype-specific populations clone that
baseline lazily.

For each graph-routed enemy it caches route metadata before destruction. On
death, fitness is:

```text
progress = clamp(1 - remainingCorridorDistance / routeDistance, 0, 1)
efficiency = shortestRouteDistance / chosenRouteDistance
fitness = progress * efficiency
```

If collision knocked an actor just outside its corridor, a radius-five ring
search recovers the nearest valid corridor distance with an aspect-weighted gap
penalty. An actor disappearing without a normal death contributes zero.

The code historically calls its update `EXP3`, but the current implementation
is more accurately multiplicative-weights/Hedge over the mean observed fitness
per arm: rewards are not importance-weighted as canonical EXP3 requires. It:

- multiplies an observed route weight by `exp(learningRate * meanFitness)`;
- normalizes all weights;
- applies a 0.5% floor to avoid numeric extinction;
- samples 90% proportionally by CDF and 10% as uniform scouts;
- shuffles a reusable route pool consumed by spawns.

This distinction matters when reasoning about statistical guarantees. The
current policy is a pragmatic online route selector, not a textbook adversarial
bandit implementation.

Gateway despawn marks its adaptation entry as draining. After a timeout, the
system removes the entry, route graph, and buffered outcomes once in-flight
actors have had time to finish.

## 7. Genetic library

`pkg/genetic` offers two engines over generic solution and numeric fitness
types:

| Engine | Model | Suitable use |
|---|---|---|
| `StreamingEngine` | Caller-driven steady-state archive with asynchronous evaluation IDs. | Long-lived simulation actors whose score arrives on death. |
| `Engine` | Conventional synchronous generational evolution with optional parallel evaluators. | Offline/on-demand objective functions. |

The streaming engine separates:

```mermaid
flowchart LR
    Archive["scored elite archive"] --> Proposals["unevaluated proposal ring"]
    Proposals --> Pending["in-flight EvalID slots"]
    Pending -->|"complete"| Archive
    Pending -->|"abandon or evict"| Recycle["recycle storage"]
```

Only scored candidates enter the fixed-capacity, descending archive, so elitism
is structural. Outcome count advances generations. Pending/proposal rings are
bounded; an evaluation can be abandoned explicitly or evicted when capacity is
overrun. The registry manages up to 256 independent species through atomic
slots and provides lock-free sampling/stat snapshots after registration.

Operators include tournament/roulette selection, uniform/N-point crossover,
bounded or Gaussian perturbation, and capacity-reusing slice cloning. The
optional fitness/tracking packages scalarize multi-metric lifetimes, and the
optional persistence package provides atomic store/codecs without coupling the
core engine to disk.

## 8. Game genetic adapter

The live game constructs `registry.NewRegistry(nil)`, so population state is
in memory only. Species register gene count, bounds, perturbation, probe bins,
and composite policy through events. Spawn systems request either an evolved
sample or a stratified scout and attach `EvalID`/genes to the entity.

The current game integration actively evaluates eyes. `GeneticSystem` tracks:

- closest squared distance to any target in the eye's target group;
- whether a self-destruct attack dealt its configured damage;
- subtype, species, route group, and evaluation ID;
- termination through normal death or loss of navigation state.

At completion it converts closest approach plus weighted successful damage into
fitness and reports it to the eye population. Runs with no positional signal
are abandoned rather than scored as zero.

On `:new`/game reset, in-flight evaluations and proposals are dropped but the
scored archive is retained, so learning survives a level reset. Because the
game registry has no persistence store, it does not survive process exit. The
generic persistence package is available but not wired into the application.

## 9. Physics primitives

`pkg/physics` operates primarily on `core.Kinetic`:

| Area | Facilities |
|---|---|
| Integration | acceleration/velocity/position integration and grid conversion |
| Bounds/walls | reflection, clamping, restitution, axis-separated substeps (up to 20) |
| Steering | speed caps, homing, arrival slowdown, drag, dead-zone snap |
| Collision | profile-based impulses, additive/override modes, mass ratio, variance, immunity |
| Shapes | point/ellipse/circle and entity-profile collision tests |
| Orbital | orbit constraints/forces and actor-specific field motion |
| 3D | fixed and float vector integration/constraint helpers projected into the terminal plane |

`IntegrateWithBounce` limits a substep to roughly 0.45 cells to reduce wall
tunneling, moves one axis at a time, restores the prior axis position on a wall,
and scales reflected velocity by restitution. Projectiles with stricter swept
collision needs use grid traversal in their owning system rather than relying
only on final-cell overlap.

Homing separates desired cruise speed, acceleration, overspeed drag, arrival
radius, near-target drag/acceleration, and a snap dead zone. Profiles under
`internal/parameter` choose these values for loot, missiles, species, and
effects.

## 10. Numeric model and determinism

`pkg/vmath` defines Q32.32 representation (`int64`, 32 fractional bits), grid
center conversions, lookup-table trig/decay, vectors, ellipses/arcs, grid
traversal, and a fast seeded RNG. Many kinetic components store precise position
and velocity in this representation.

The representation must not be described as a blanket integer-deterministic
engine. Performance-critical operations such as fixed `Div`, `MulDiv`, `Sqrt`,
normalization, and exact magnitude convert through hardware floating point, and
parallel float-native vector/geometry APIs are used heavily. Some random sources
also use process/time seeds, and real-time scheduling changes event timing.

The correct guarantee is local: a seeded algorithm may be reproducible given
the same input/order and no wall-clock budget, but the whole game is not a
cross-platform lockstep simulation. Any future multiplayer/replay design must
define its own authoritative state and determinism boundary.

## 11. Maze generation

`pkg/maze` generates odd-sized stochastic mazes using a recursive backtracker,
then optionally:

- reserves explicitly positioned or randomly placed rooms;
- removes outer borders;
- adds smart braiding/cycles while respecting topology constraints;
- connects rooms to passages and reports their entry points;
- forces start/end cells open;
- solves a BFS path from start to end.

Seed zero selects current time; a nonzero seed makes the generator's random
choices reproducible for the same configuration. Wall-system events translate
the resulting boolean grid and solution metadata into ECS walls/level state.

## 12. Extension guidance

- Use a shared flow field for many actors with the same footprint/targets; do
  not compute one per entity.
- Build composite passability before routing a large footprint.
- Choose route adaptation for path policy and genetics for continuous actor
  parameters; do not mix their fitness meanings.
- Cache entity metadata needed after death because component stores are wiped
  before deferred learning updates may run.
- Treat Q32.32 overflow, float conversion, zero denominators, and cell rounding
  as explicit boundary cases.
- Add debug/status observability for a new learner before trying to tune it.
- If persistence is enabled, version the gene schema and do not rely on
  process-local IDs.

## 13. Source map

| Concern | Primary source |
|---|---|
| Flow fields/cache | `pkg/navigation/flowfield.go`, `cache.go` |
| Composite navigation | `pkg/navigation/composite.go` |
| Route generation | `pkg/navigation/routegraph.go` |
| ECS navigation adapter | `internal/system/navigation.go` |
| Route learning | `internal/system/adaptation.go`, `internal/engine/resource.go` |
| Genetic engines | `pkg/genetic/*.go`, `pkg/genetic/README.md` |
| Registry/tracking/persistence | `pkg/genetic/{registry,tracking,fitness,persistence}` |
| Game genetic adapter | `internal/system/genetic.go`, `internal/system/eye.go` |
| Physics | `pkg/vmath/physics/*.go` |
| Numeric/geometry | `pkg/vmath/*.go` |
| Mazes | `pkg/maze/generator.go` |
