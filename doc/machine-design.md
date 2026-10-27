# Behavior machines

Per-entity state machines for species systems, and the species a scenario defines
from data on the same runtime. §1 is the overview. §2 and §3 are the runtime and
the contract a host follows, the reference for writing or reading a graph. §4 gives
each species' graph as it runs. §5 and §6 are the data path, from the dialect the
scenario machine shares to a data species' place in the engine. §7 says why it is
deterministic and how it is identified, §8 how it is verified, and §9 which
alternatives were weighed and where the design comes from. D-n is the
[domain model](domain-design.md); the keys and verbs a program uses are in the
[FSM reference](fsm-reference.md#behavior-programs).

## 1. Overview

One runtime, two front-ends:

- **Runtime.** `pkg/machine`, standard library only. A `machine.Instance[S]`, a
  state byte with a relative `Elapsed` and the entry's `Duration`, lives inside the
  species component as plain integers, so capture, compare, digest and install
  treat it as any field. A `machine.Graph[S, C]`, immutable and shared by every
  instance of a species, steps it in fixed phases (§2).
- **Code graphs.** A species system declares its graph once, in its constructor,
  from typed Go: per-state hooks, flags, a default duration and cues; ordered
  edges with candidate chains; global edges with source masks; one suspend
  predicate. `Build` validates it, and a graph that fails is a broken program.
- **Behavior programs.** TOML in the scenario's dialect, compiled at load against
  the game vocabulary into the same `Graph` type (§6). Names resolve to closures
  over pre-decoded arguments at load; nothing is interpreted per tick.

The swarm, the quasar and the storm circles run on code graphs (§4), every data
species on its program's. They adopt the runtime for correctness, not speed or
line count: every fallible entry names where it falls, every way out of a state
runs that state's exit, a stun is one global edge and one suspend predicate, and
one relative timer per instance replaces a countdown per state. The other species
keep their code (§4.5).

The scenario HFSM (`internal/fsm`) stays the world-level director. It is
event-triggered, map-backed and sized for a handful of regions, so it is not the
per-entity runtime; the two share their dialect (§5), not their runtime.

Placement:

- `pkg/machine`: the instance, the graph and its step (`machine.go`), the builder
  and its validation (`build.go`). `make arch-check` reports a `pkg/` package
  importing `internal/`.
- `internal/system/swarm.go`, `quasar.go`, `storm.go`: each code graph, beside
  its host.
- `internal/fsm/dialect`: the dialect kernel, beside the scenario machine.
- `internal/behavior`: a program's document, the vocabulary registry and the
  compiler.
- `internal/system/behavior.go`: the game vocabulary and the generic species
  system, one file, so the domain audit (`internal/system/domain_test.go`) sees
  every store the verbs touch.

## 2. Runtime

### 2.1 Instance

```go
type Instance[S ~uint8] struct {
	State    S             `json:"state"`
	Elapsed  time.Duration `json:"elapsed"`
	Duration time.Duration `json:"duration"` // 0 = untimed
}
func (in Instance[S]) Progress() float64 // Elapsed/Duration clamped to [0, 1]; 0 when untimed
func (in Instance[S]) Expired() bool     // timed and Elapsed >= Duration
```

`Duration` is stored because an entry may override its state's default: a storm
circle's first cooldown, a retry. `Elapsed` is relative, so a suspension freezes
it and no component holds an absolute instant (§7). There is no entry counter:
cues make one-shots structural, and random draws seed from the tick and the
entity. The reads take a value, so a renderer calls them on a component copy.

### 2.2 Graph

```go
type Mask uint64                       // a bit per state; MaskOf(states...)
type Cause uint8                       // Start, Timeout, PreTick, PostTick, Global, Retry, Stop
type Observer[S ~uint8] func(from, to S, cause Cause)

type State[C any] struct {
	Name     string
	Duration time.Duration // default for entries that set none; 0 = untimed
	Flags    uint64
	Probe    func(C) bool // fallible entry, may stash results in C; nil = infallible
	Enter, Exit, Tick func(C)
	Cues     []Cue[C]     // {AtPermille uint16, Do func(C)}; timed states only
}
type Target[S ~uint8, C any] struct { State S; Guard func(C) bool; Duration time.Duration }
type Edge[S ~uint8, C any] struct {
	From     Mask           // global edges only
	Guard    func(C) bool   // nil = always
	Do       func(C)        // runs between exit and enter
	To       []Target[S, C] // candidates in order; Duration 0 takes the state's default
	Fallback Fallback       // Retry(d) or Hold; required when every candidate can fail
}
```

`machine.New[S, C](observe)` returns a builder that takes `State(id, st)`, one
`Timeout(from, edge)` per timed state, ordered `PreTick(from, edge)` and
`PostTick(from, edge)` lists, ordered `Global(edge)` and one `Suspend(pred)`, each
returning the builder, so a graph declares as one chain. `Build` validates the
declarations (§2.4) and returns the `*Graph[S, C]`, which nothing changes
afterwards; `Flags`, `Name` and `States` are its reads. `Must` panics on `Build`'s
error: a code graph that fails to build is a broken program, as a broken manifest
is, while a program's failure is a load error. A graph holds at most `StatesMax`
(64) states, a bit of `Mask` each, and its observer may be nil.

### 2.3 Step

```go
func (g *Graph[S, C]) Step(in *Instance[S], c C, dt time.Duration) (flags uint64, suspended bool)
func (g *Graph[S, C]) Start(in *Instance[S], c C, s S, d time.Duration) // a spawn: enter, no exit or probe
func (g *Graph[S, C]) Stop(in *Instance[S], c C)                         // a death: exit, no enter
```

A step runs these phases in order:

1. **Global edges**, in declaration order: the first whose `From` holds the
   current state and whose guard passes takes its transition.
2. **Suspend.** If the predicate holds, the step returns the current flags with
   `suspended` set: no advance, cue, timeout or tick.
3. **Advance** `Elapsed` by dt, firing every cue whose point falls in (old, new],
   computed in integer nanoseconds, so a cue on a tick boundary fires once.
4. **Timeout**, if the state is timed and has expired.
5. **Pre-tick edges**, in declaration order.
6. **Tick** of the current state.
7. **Post-tick edges**, which read what the tick left in `C`.
8. **Return** the current state's flags.

Global edges run before suspension, so a stun can cancel an attack and then freeze
the result. Each phase takes at most one transition, so a step makes at most four,
and the state a phase enters is the one the next phase runs.

A transition tries its candidates in order, each by its guard, then its state's
probe. On the first that passes it runs the current state's `Exit`, then the
edge's `Do`, commits `{State, Elapsed: 0, Duration}`, runs the target's `Enter`
and reports `(from, to, cause)`. `Exit` and `Do` therefore read the entry being
left and `Enter` the entry being started, its `Duration` included: the quasar's
charge sizes its telegraph from it, the swarm's charge its velocity. Probes run
before `Exit`, so `Enter` cannot fail and nothing is half-committed: an exit runs
only when a transition commits, and fallible work stays out of the hooks that
cannot fail.

When no candidate commits, the fallback applies. `Retry(d)` re-arms the current
state in place for d, with no exit or enter, reports `CauseRetry` with from == to,
and runs the state's cues again in the new period. `Hold` changes nothing, so an
expired state tries its timeout again next step. In a phase, the first edge whose
source holds the state and whose guard passes owns the phase: when its chain
fails, its fallback applies and later edges wait for the next step.

The state a transition enters ticks in the same step. A global edge acts at the
start of the step, so the state it enters also advances by dt; the later phases
act after the advance, so a state they enter ticks at `Elapsed == 0`, which a tick
that must skip its entry step tests.

`Start` enters a state for d, or its default when d is not positive, with no exit
and no probe; it panics on a state the graph does not define, which `Build`
cannot see. `Stop` runs the current state's exit with no enter. Both report to
the observer with from == to, as a retry does.

### 2.4 Validation

`Build` reports every fault at once, each naming its state or edge
(`timeout edge 1 from state "Chase": every candidate can fail and no fallback is
declared`). It rejects:

- a state outside 0..63 or defined twice, and a negative duration;
- a cue on an untimed state, or at a point outside (0, 1000];
- a second suspend predicate;
- a global edge whose `From` is empty or holds an undefined state, and a `From`
  on any other edge;
- an edge from an undefined state, a timeout on an untimed state, and a second
  timeout on one state;
- a candidate naming an undefined state, or with a negative duration;
- a chain that can fail entirely, every candidate having a guard or a probe, with
  no fallback;
- a `Retry` of zero or less.

With the fallback required and a step that never skips a tick, a wedge is
impossible by construction: an entry that keeps failing leaves the instance in a
state that still ticks and tries again.

### 2.5 Types

- **Typed IDs.** `S ~uint8` IDs index per-state slices, so a graph rejects a
  foreign enum at compile time.
- **Context type.** `C` is a pointer type, and hooks are func fields rather than
  constraint methods, so instantiations sharing a GC shape pay no dictionary
  dispatch.
- **Generic methods.** A method with type parameters of its own, on a generic
  receiver too, carries the dialect's comparison (`Op.Holds`) and the typed
  registrations of §5 and §6.2. A generic method cannot satisfy an interface
  method, so those types stay concrete.

## 3. Hosting a machine

A host is the species system that steps a graph. Its contract:

- **Context.** One context struct per species, a system field refilled per
  entity: the entity, component pointers, observations sensed once before the
  step (stunned, in range, has a cursor), probe results and tick outcomes. A code
  graph's hooks are the system's methods and reach the world through it, so the
  context holds no world; a program's verbs, compiled before the system exists,
  reach it through the context's `sys`. Passing a pointer to a field of the
  long-lived system allocates nothing per call.
- **Mutation.** The runtime mutates only the `Instance`, the hooks the world. The
  host holds the component through `GetPtr`, so a step's commits land in the
  store with no copy to write back.
- **Lifecycle.** Hit points, limits, lifetime, cancel and breach are checked ahead
  of the step. A death calls `Stop`, which runs the state's exit as a transition
  does, so a telegraph or a lightning never outlives its instance; the storm
  circles, whose states have no exit and whose graphs no observer, skip it.
  `Start` runs once the composite is built, so an enter hook reads complete
  components.
- **Moore outputs.** The step returns the flags of the state it ends in, and the
  host writes the component fields derived from them on every step, suspended or
  not: an output never lags its state by a tick or goes stale while held. Combat
  reads them as fields (`QuasarComponent.IsShielded`,
  `StormCircleComponent.IsInvulnerable`, `BehaviorComponent.Shielded` in
  `applyStunEffect`), so the components keep them.
- **Suspension.** A suspended step strikes nothing: contacts and gold breaking run
  only after a step that did not suspend. A held instance still counts in its
  species' count.
- **Timers.** A phase's timer is the instance's. A cycle that spans states is a
  carried accumulator, a `time.Duration` field advanced by dt and reduced by its
  period, carrying the remainder: the swarm's and a data species' pattern
  (`PatternElapsed`, held by a stun) and the quasar's speed (`SpeedElapsed`,
  advanced by movement ticks). Lifetimes and one-shot deadlines stay in
  `TimerComponent` slots ([ECS and events §7](ecs-and-events.md#timers)).
- **Observer.** Bound when the graph is built, so a host feeds status counters
  without the runtime importing `status`. Nothing reads `Graph.Flags` or
  `Graph.States` yet ([todo](todo.md#show-a-species-machine-state-in-a-debug-overlay)).
- **Footprint effects.** A host that clears the cells under a footprint names the
  effect each victim leaves. A move or a landing leaves
  `EventFlashSpawnOneRequest`, each dead glyph's rune drawn for
  `DestructionFlashDuration` in the particle layer under the species, so the body
  covers it: the quasar's and the storm circles' moves, a program's `Teleport`,
  and the gold every composite breaks (`breakGoldUnder`). A spawn clears its
  footprint with no effect, and the swarm and a data species move without
  sweeping.

## 4. Species graphs

Each graph lists its states with their default durations, flags and hooks, then
its edges, then what its host does around the step.

### 4.1 Swarm

`C = *swarmCtx`, built in `NewSwarmSystem`:

- States, every one but Chase flagged Enraged:
  - Chase, `SwarmChargeInterval` (5 s): tick homes and integrates; timeout →
    [Lock], else `Retry(SwarmTransitionRetryInterval)`, the chase moving
    meanwhile.
  - Lock, 2 s: probe a base target; enter stores it and zeroes the velocity;
    timeout → [Charge (guard: the body clears the way to the stored cell; probe:
    a kinetic) | Teleport (probe: a position and a landing) | Decel].
  - Charge, 0.8 s: enter stores start and target and aims to arrive as the
    entry's `Duration` ends; tick steers over what is left of it, integrates and
    notes a wall hit; timeout → Decel; post-tick, a wall hit → Decel in the step
    that meets it, so the bounce stands.
  - Teleport, 0.4 s: enter marks the landing and zeroes the velocity; the beam
    leaves from wherever the header stands as it runs, so a wall pushing the
    swarm carries the beam's source along; timeout → [Arrive | Decel].
  - Arrive, `SwarmArriveDuration` (0.1 s): probe the landing's footprint, else
    the nearest clear one (`FindFreeAreaSpiral`, `WallBlockSpawn`), so rock that
    came over the landing during the beam never holds the swarm; enter counts
    `ChargesCompleted`, clears the footprint in both domains (D-12) and stands
    the swarm on it at rest; timeout → Chase. The move is the arrival's entry
    rather than the beam's exit, so a stun that ends a teleport moves nothing.
  - Decel, 0.1 s: enter counts `ChargesCompleted`; tick scales the velocity by
    `SwarmDecelDrag`, a per-tick factor, and integrates; timeout → Chase.
- Global edge: stunned, from {Lock, Charge, Teleport, Arrive, Decel} → Chase
  with the full interval. Suspend: stunned. A stun in the chase therefore freezes what is
  left of the interval, and one anywhere else cancels the attack and restarts the
  whole interval.
- Around the step:
  - The pattern accumulator advances first unless the swarm is stunned, the
    suspend's own condition.
  - Hit points, the charge limit (`SwarmMaxCharges`) and the lifetime are
    checked ahead of the step; they, a cancel and a breach end the swarm through
    `despawnSwarm`, which calls `Stop`. No state has an exit, so `Stop` only
    reports.
  - After the step the host writes `Combat.IsEnraged` from the flags and, unless
    the step suspended, breaks the gold under the swarm and strikes what it
    touches.
  - The observer, `SwarmSystem.observe`, counts each `CauseRetry` and each Lock →
    Decel or Teleport → Decel commit in `swarm.transition_stalls`: a lock with
    no target, one that can neither charge nor teleport, and a teleport with no
    room anywhere to arrive.
- `swarmCtx` holds the entity, the component, combat and kinetic pointers, the
  sensed `stunned`, the capped dt in seconds, the lock probe's target cell, the
  landing a teleport or arrival probe found and the charge's wall hit. `Start` enters
  Chase with the full interval once the composite is built, before
  `EventSpeciesCreated`.
- The charge line reads `Machine.Elapsed`, the teleport line `Machine.Progress()`.

### 4.2 Quasar

`C = *quasarCtx`, built by `newQuasarGraph` in `NewQuasarSystem`:

- States:
  - Homing, untimed: tick homes; pre-tick, out of range → Charging.
  - Charging, `QuasarChargeDuration` (3 s), Enraged: enter requests the splash
    telegraph for the entry's `Duration`, exit cancels it; tick homes; timeout →
    [Zapping | Homing], so a charge whose zap cannot aim goes home in the step it
    expires.
  - Zapping, untimed, Enraged and Shielded: probe resolves the origin and the
    cursor; enter starts the lightning, exit despawns it; tick retargets and
    deals the zap's damage.
- Global edges, in priority order: stunned, from {Charging, Zapping} → Homing; in
  range, from {Charging, Zapping} → Homing. Suspend: stunned. Combat refuses to
  stun a shielded quasar, so the stun edge's Zapping half does not fire today,
  but the quasar's correctness does not rest on that refusal.
- Around the step:
  - The host writes `Combat.IsEnraged`, `IsShielded` and `Shield.Active` from the
    flags and refreshes the ward while shielded. The shield is a flag, so it drops
    in the step that leaves Zapping, and no exit writes it.
  - Unless the step suspended, it strikes the cursors the quasar touches.
  - Homing advances `SpeedElapsed` by movement time from spawn, so the first speed
    increase lands after `QuasarSpeedIncreaseTicks` of movement, and a stun pauses
    the count.
  - A header move sweeps the new footprint (`processCollisionsAtNewPositions`),
    and each glyph it kills dies in the tick's closing settle. A glyph can stand
    under a member only through the one-tick lag between the header's move and
    the composite sync, which the next move's sweep covers.
- `quasarCtx` holds the entity, the component, the sensed `stunned` and `inRange`
  (true with no cursor, so a missing cursor never charges), dt and its capped
  seconds, and the probe's origin and target. `Start` enters Homing at spawn once
  the composite is built, and `terminateQuasar` calls `Stop`, so a death ends the
  splash or the lightning through the exit a transition runs. Charging's exit
  cancels the splash on every path, a timeout's included, where the timer system
  has already removed the splash and the cancel finds nothing.
- The renderer reads `Machine.State`.

### 4.3 Storm circles

`C = *circleCtx`, one graph per colour:

- `circleVariants`, indexed by `StormCircleType`, holds a row per colour: the
  spawn's cooldown, the attack, the cooldown after one, Active's flags beside
  Attacking, its enter and tick hooks, and its cue. `NewStormSystem` builds the
  three graphs from the rows once (`newCircleGraph`); a colour is a row, never a
  switch.
  - Green, 1 s, 1 s, 1 s: tick `pulse` strikes the cursors inside its area.
  - Red, 1 s, 2 s, 1 s, flagged Armed: tick `burst` counts; the armed turret
    mount fires after the storm system.
  - Blue, 5 s, 2.5 s, 5 s: enter `findLanding` draws the direction, then searches,
    and stores `LandingFound` (D-8); tick `glow` counts; the cue `beamIn` at
    `StormBlueMaterializePermille`, the attack's 2 s mark, beams the swarm onto
    the landing if one was found. A cue fires once per entry, so the blue's one
    swarm an attack needs no dedup.
- States:
  - Cooldown, the row's repeat by default: timeout → [Active (guard: convex)],
    else `Hold`, so an expired cooldown waits for the convex.
  - Active, the row's attack, flagged Attacking: timeout → Cooldown.
- `createCircleHeader` starts each circle in Cooldown with the row's initial
  cooldown.
- Suspend: no cursor, or stunned (the circle header's `StatusStun`, which its
  physics reads too). A stun lands only on a convex or attacking circle, so a
  stunned burst or pulse stops where it is and goes on when the stun ends, as the
  swarm and the quasar hold theirs.
- After every step, suspended or not, the host writes
  `IsInvulnerable = concave && !Attacking` and `Mount.Armed = Armed &&
  !suspended`, so the red fires from the step that opens its burst and never while
  held. The physics Z clamp and the renderer read `Machine.State`, the green
  pulse and the blue glow `Machine.Progress()`, and the red's muzzle flash the
  armed mount: a held burst shows no flash, while a held pulse or glow keeps its
  progress.
- `circleCtx` holds the entity, the component, the circle's cell and the sensed
  `hasCursor` and `stunned`; the hooks are `StormSystem` methods, bound per row.

### 4.4 Spawns that ride their beams

A spawn deferred behind a materialize beam rides the beam:
`MaterializeComponent.Species` names the species the beam's completion lands, so
the spawn, the beam's timer and its visual end on one tick. The pending spawn is
the beam entity, which a capture carries with the stores, not a list beside its
system.

- **Storm.** The blue's cue starts a Shared beam naming the swarm, and
  `StormSystem` lands the swarm on the beam's `EventMaterializeComplete`, its
  header on the beam's footprint. `terminateStorm` ends the storm's beams in
  flight, and the storm's own capture (`stormSnapshot`) holds its root alone.
- **Fusions.** A drain pair's swarm and a quasar fusion fly player-domain beams
  naming the species; on the beam's completion `landFusion` crosses the spawn
  request (D-3). A quasar fusion is refused while one's beam flies
  (`quasarBeaming`), the fuse keeps no state of its own (`FuseSystem.Update` is
  empty), and `killDrains` alone reports the consumed drains' deaths.
- **Data species.** `EventFuseBehaviorRequest` starts a Shared beam naming the
  data species over the nearest place its footprint fits, searched over the whole
  map (`FindFreeAreaInside` with a negative radius: `FindFreeAreaSpiral` from the
  origin pulled in from the edges until the footprint fits, as a direct spawn,
  `FindLanding` and `Teleport` search), and on a shared completion
  `landBehavior` sends the direct spawn, its header on the beam's footprint. A
  cancel of the program ends its beams in flight (`cancelBehaviorBeams`).

The drain keeps its own queue (`pendingSpawns`), which completes on
`EventMaterializeComplete`, not on a timer of its own.

### 4.5 Species without a machine

Each keeps its code: none has the fallible entries, exit effects or preemption a
machine makes structural.

- Snake: its shield is body liveness, its spawning a counter.
- Drain: an enrage at a hit-point threshold, and the player-domain population
  queue above.
- Eye: a frame cycle and motion the genetics drive.
- Gateway: a periodic emitter, counting up with carry.
- Fuse: no state; its fusions ride their beams (§4.4).
- Pylon and tower: stationary.
- Kraken: an animation that is all simulation state, its cycle of idle, attack,
  spin and move chosen by draws and timed by countdowns of its own.
- Materialize: a progress animation on a timer slot.
- Storm root: liveness aggregated over its circles.

## 5. Dialect kernel

`internal/fsm/dialect` is what the scenario machine, bot graphs and behavior
programs share of their TOML: one dialect, three vocabularies. Names resolve once,
at load; only `Op.Holds` runs per evaluation.

- **`Op`.** An args struct's `dialect.Op` field takes `eq`, `neq`, `gt`, `gte`,
  `lt` or `lte`, and an omitted op is `eq`; any other value fails the load naming
  its key. `Op.Holds[T cmp.Ordered](a, b T)` evaluates it, and `Ops()` lists the
  six for `-schema`.
- **`Decode[A](raw)`, `DecodeInto(raw, v)`.** A strict typed decode of an argument
  table: `toml.Unmapped` names every key no field takes by its path
  (`unknown keys "inner.y", "vr"`), `Op` fields take their names, and `toml.Decode`
  refuses a mistyped value by its path (`within: cannot convert string to int`).
  The caller prefixes the place (`guard 'HasTarget': `,
  `action 'Intent': payload: `). `Fields[A]` lists A's keys with their kinds:
  string, int, float, bool, op, array, table or any.
- **`Tree[G]`.** `And` and `Or` over `guards = [{ name, args }]` and `Not` over
  `guard = { name, args }`, nested freely, each node decoded strictly. It is
  generic over the guard type, so each runtime supplies its leaf builders and its
  own `All`, `Any` and `Not`: an `fsm.GuardFunc[T]` sees region and payload, a
  behavior guard only `C`. A fault names each compound it is in
  (`guard 'And': guards[1]: guard 'VarCompareVar': op: ...`).
- **`Entry`.** `{name, kind, keys, args}`, the row `-schema` prints; `Sort` orders
  a vocabulary by kind, then name, and `TreeEntries` are `And`, `Not` and `Or`.
- **`Names`.** Per Go type, the names a document spells its values by: `Enum[E]`
  names a dense enum, `Mask[E]` a bit set's bits and named unions (a field takes
  one name or a list, their union), and `Variant[E, K]` E's values where the K
  field beside it holds a key, as a gateway's `sub_type` takes its `species`'
  kinds. `Names.DecodeInto` swaps each name for its value in the raw table, at any
  depth, before `toml.Decode` reads it, so a reordered enum retargets nothing a
  document reads; a name the type lacks fails by its path with the names it takes
  (`rooms[1].kind: "attic" is not one of hall, vault`), and so does a number.
  `Decode` is the names that hold only `Op`. `event.PayloadNames(programs)` holds
  every enum a payload holds, a scenario's programs among the species;
  `event.AuthoredPayload` decodes through it for a scenario's and a program's
  `EmitEvent`, `:emit` and a script's event, and `List` is what `-schema` prints
  under `names`.

`fsm.Machine` registers a name with the struct its arguments decode into, through
generic methods: `RegisterGuard[A]`; `RegisterAction[A, R]` for an action that
reads only `payload`, compiled to `R` at load (`fsm.Payload[A]` keeps the decoded
payload as it is); `RegisterKeyedAction[R]` for one that reads keys of its own
table (`EmitEvent`: `event`, `payload`, `payload_vars`, `pick`; the region actions:
`region`, `initial_state`); and `RefuseAction`, a name whose use fails the load
and which the vocabulary leaves out. The loader refuses a key of an action's
table the action does not read (`action 'Text' takes no payload_vars`) and a key
no config struct takes under `[systems]`, a region or a state, its transitions and
actions included (`state 'A': unknown key "on_entr"`). A top-level key is its
embedder's to check: `resource.entryKeys` for a scenario entry, `bot` for a graph.
`Machine.Vocabulary` lists a machine's entries, and `-schema` prints the
scenario's, a bot graph's and the behavior vocabulary under `vocabularies`. The
payload fields a guard, `capture_vars` or `payload_vars` names are not yet checked
at load ([todo](todo.md#check-payload-field-names-against-their-event-at-load)).

## 6. Data species

A behavior program defines a species: a `[prototype]` body and a state machine
over the game's verbs. A scenario lists up to `BehaviorProgramsMax` of them, each
compiled at load into a `machine.Graph` that `BehaviorSystem` steps for every
instance, over the scaffold every composite species shares. `wad/scenario/vanguard`
is the worked example: four raider types, each a program over one macro of states.

### 6.1 Program shape

A program uses the fsm dialect's keys with the machine's additions: `duration_ms`,
`flags`, `probe`, `cues`, `suspend`, `[[global]]`, a candidate list as `target`,
`fallback`, the `Timeout` and `AfterTick` triggers, and a `[prototype]` table. The
scarab's lock, as vanguard's `RaiderStates` macro expands it with the scarab's
timings:

```toml
[states.Lock]
duration_ms = 1200
flags = ["Enraged"]
probe = "ResolveTarget"
on_enter = [{ action = "StoreTarget" }, { action = "Telegraph", payload = { color = "cyan" } }]
on_update = [{ action = "Freeze" }]
on_exit = [{ action = "CancelTelegraph" }]
transitions = [{ trigger = "Timeout", target = [{ state = "Charge", guard = "LineOfSight", guard_args = { to = "stored" } }, "Beam", "Brake"] }]
```

How the keys map onto the runtime:

- `trigger` is `Timeout`, `Tick` (a pre-tick edge) or `AfterTick` (a post-tick
  one). A `Timeout` takes no guard, as the runtime takes timeouts unguarded; its
  candidates carry the guards.
- `target` is a name or a candidate list, a candidate
  `{ state, guard, guard_args, duration_ms }`; `fallback` is `"hold"` or
  `{ retry_ms = n }`; an edge's `actions` are its `Do`.
- `flags` resolve against the vocabulary's flag table, `suspend` against its
  guards, and the `[[global]]` tables, each with its `from`, are the global edges
  in array order.
- State IDs follow sorted names, because a decoded table carries no order; edge
  order is array order.
- `on_update` is the state's tick: its sustained verbs, then the host's
  `Integrate`. `on_enter` is its effects, then the host's `Entered`, which drops
  the cell the probe resolved, so a probe serves the entry it guarded and no later
  one.
- A verb out of place (an effect in `on_update`, a sustained verb on an enter,
  exit, cue or edge) or an unknown name fails the compile naming program, place
  and key, every fault joined, and the builder's checks (§2.4) then apply.

### 6.2 Vocabulary

The registry is generic, `behavior.Vocabulary[C]`, and its generic methods
register a name with the struct its args decode into, strictly and once at load:
`Predicate[A]`, `Seeded[A]` (a guard whose build also takes its site, the
program's guards and probes numbered in load order), `Probe[A]`, `Effect[A]`,
`Sustained[A]`, `Emit` (an effect reading `event` beside `payload`) and
`Flag(name, bit)`, with the host's `Integrate` and `Entered` hooks. `Entries`
lists them for `-schema`'s `vocabularies.behavior`. The game's verbs bind in
`BehaviorVocabulary` (`internal/system/behavior.go`) to `behaviorCtx`, refilled
per instance:

- Predicates read the instance and the world: `TargetDistance`, `LineOfSight`,
  `HPFraction`, `Stunned`, `HasTarget`, `HitWall`, `Chance`, `Param` (the
  `Genotype` genes), with the kernel's `And`, `Or` and `Not`.
- Probes read too, and leave their result in the context: `ResolveTarget`
  (`resolveBaseTarget`, the nearest cursor) and `FindLanding`
  (`FindFreeAreaSpiral` over the footprint). `LineOfSight` and `FindLanding` read
  the live target, or with `to = "stored"` the cell `StoreTarget` kept: the swarm
  charges and teleports to the cell its lock stored, and so do vanguard's raiders.
- Effects run on an enter, an exit, a cue or an edge: `StoreTarget`,
  `ZeroVelocity`, `Teleport`, `Telegraph` and `CancelTelegraph` (the splash
  request pair), `Lightning` (the cursor it strikes takes the contact damage),
  `Materialize` (a `swarm` or `nugget` beam that lands nothing), `SpawnSpecies` (a
  swarm or a quasar) and `EmitEvent`.
- Sustained verbs run in a tick: `Home`, `ChargeToStored`, `Drag` (linear drag
  per second, so it holds at any tick rate) and `Freeze` (no move this tick), each
  over the in-place helpers of `pkg/vmath/physics` the code species use
  ([generic kinetic §8](generic-kinetic.md#8-feasible-pseudo-system-design)).
- `Integrate` ends every tick unless `Freeze` ran: `IntegrateWithBounceStats` over
  the footprint with the prototype's restitution, recording `HitWall` and the
  bounce rows; the members follow, and moving sweeps nothing.

Each verb fixes its own event routing, so an author never names a domain:
presentation is local, a shared request shared, and cursor damage local behind
`SimulatesLocally`; `EmitEvent` takes only Shared or Local classes. Contacts
(`strikeContacts` with the prototype's damage), lifecycle and spawning stay in the
system's scaffold (§6.4), not in the vocabulary.

### 6.3 Delivery and compilation

- **Programs are scenario files.** The entry lists them
  (`[behaviors] scarab = { file = "behavior/scarab.toml" }`) or holds one inline.
  `includeBehaviors` (`internal/fsm/file_loader.go`) reads, parses and expands each
  file against the entry's macros through `readExpanded`, the step region files
  share, records it in `visited` and replaces the `{ file }` table; anything else
  under `behaviors` fails naming it. `ScenarioFiles` therefore lists programs, so
  `resource.ReadScenario` carries, digests and transfers them with no mechanism of
  their own. `validScenarioName` admits the subdirectory, `resource.entryKeys`
  admits `behaviors`, and `Scenario.Behaviors()` returns the programs by name.
  `Machine.LoadScenarioFromMap` takes no part: a top-level key is its embedder's.
- **Compiled once per run.** `App.readScenario` compiles the programs
  (`system.CompileBehaviors`, against `system.BehaviorVocabulary()`) right after
  `resource.LoadScenario`, so a bad program fails as a bad scenario does, before
  any service starts, and before the scenario machine loads, whose species names
  resolve against the prototypes. `initWorld` puts the prototypes in `world.Resources.Behaviors`
  before the systems are built, so loot, soft collision and `NewMetaSystem` size
  their data rows and the renderers read glyphs. The compiled graphs go to
  `system.NewBehaviorSystem(world, set)`, which the app adds beside `MetaSystem`
  (`manifest.ContextSystems`), because the generator's constructors take only a
  world.
- **`-check` compiles every program.** `resource.ValidateScenario` runs the same
  compile, then the machine load, so an unknown name or a bad argument fails
  validation, not play, and a
  mid-run scenario change refuses a bad program before anything is torn down.
  `-check` prints `behaviors ok: <n> (<names>)`.

### 6.4 Beyond the machine

A data species needs a body before its behavior matters, and the machine is the
smaller part of it.

- **Prototype** (`component.BehaviorPrototype`, plain data in
  `Resources.Behaviors`). Footprint (at most `BehaviorFootprintMax` cells each
  way) and header offset, hit points, the combat row it fights as (`swarm` or
  `quasar`, the unit composites whose damage lands on their header), its physics
  (a homing profile and a restitution, `profile.BehaviorHoming` and
  `parameter.BehaviorRestitution`, the swarm's chase, with each key of the program's
  `[prototype.physics]` replacing its value), soft-collision mass, the code species
  whose drop tiers it rolls, contact damage, lifetime, its look and the pattern
  period.
  The look (`component.Look`) is the frames cell by cell, each cell a glyph and
  entries of the prototype's fg and bg palettes with its attributes, plus the
  colour of a glyph without an entry and an optional console pair;
  `visual.CompileLook` builds it from the authored rows, the eye's types' art
  included. It names rows of per-kind tables that are code, so a new attack profile,
  beam style or drop table is a code change
  ([todo](todo.md#give-data-species-attack-beam-and-drop-rows-of-their-own)).
- **Species IDs.** `component.SpeciesType` keeps its code enum, ending at
  `SpeciesCount`; `SpeciesDataFirst = SpeciesCount` and
  `SpeciesMax = SpeciesCount + parameter.BehaviorProgramsMax` bound the data
  range. Programs take `SpeciesDataFirst + i` in sorted-name order, and a scenario
  with more fails to compile. A scenario's guard names a species
  (`PayloadSpeciesIn`): the FSM bridge resolves it at load against
  `SpeciesNames` and the compiled prototypes, so a new program moves no guard. A name is `[a-z][a-z0-9_]*` of at most
  `BehaviorNameMax` bytes, neither a code species' name nor `total` or
  `uncredited`, because it keys `kills.<name>` and `behavior.<name>.*`.
- **Species tables.** Every table indexed by `SpeciesType` is `[SpeciesMax]`, so a
  data ID never indexes out of range: `profile.Contact`, `profile.SoftKraken`,
  `profile.Drops`, the soft-collision matrices and caches, and `MetaSystem`'s kill
  counters and capture. A data species' package-level rows stay zero, and its
  values come from its prototype at the reader: the behavior system passes the
  contact damage, loot rolls the named species' tiers under a pity of its own, the
  health bar measures the prototype, and soft collision builds data rows in `Init`
  for a prototype with mass (to and from swarm, quasar and every other data
  species; storm, pylon and kraken push it one way), so every code pair keeps its
  rule. The range checks in `meta.go`, `genetic.go` and `loot.go` read
  `>= SpeciesMax`.
- **Composite routing.** A data species' header carries `component.BehaviorData`;
  the behavior system handles `EventCompositeIntegrityBreach` for it, and
  `handleEmptyComposite`'s default destroys the head.
- **Events.** A scenario emits `EventBehaviorSpawnRequest{program, x, y}` (bus,
  and barrier-bound like the swarm's, since a crossed one allocates shared
  identity) and `EventBehaviorCancelRequest{program}` (shared, as every species'
  cancel; an empty program cancels every data species). A program no entry
  defines is counted (`behavior.unknown_program_rejects`) and dropped. A spawn is
  immediate, as the swarm's and the quasar's are, its footprint searched over the
  whole map as a beam's is, since a scenario names a cell blind to the walls;
  `EventFuseBehaviorRequest` beams one in (§4.4). A scenario registers a data species with genetics through
  `EventGeneticRegisterSpecies` and its ID; the genes land in the `Genotype`
  component, sampled once the spawn has its place, where `Param` reads them.
- **Telemetry.** `behavior.*`, and per program `behavior.<program>.count` and the
  lifecycle rows (`newLifecycleTelemetry`), bind in `NewBehaviorSystem`,
  `kills.<program>` in `NewMetaSystem`, all before `Freeze`
  ([telemetry](telemetry-reference.md)). A spawn counts at once rather than at the
  next step, since a beam lands its instance after the step and a region reads
  the count.
- **Renderer.** `BehaviorRenderer` (`renderer/behavior.go`,
  `render.PriorityBehavior` after `PrioritySwarm`) draws each header's members
  from the look's frame at `PatternIndex`, each cell its background and its glyph
  in its palette entry, else the look's colour; the hit flash, then enraged, tints
  every glyph. A 256-colour host draws a look with a console pair in that pair and
  shows a hit by reversing the glyph. `lookPainter` draws the eye's cells too, so
  the two cannot drift. The hit-flash pulse is `hitFlashColor`, one function for
  every species renderer.
- **Generic species system.** `BehaviorSystem` runs every data species: hit
  points, lifetime, the pattern cycle (held by a stun), `Step`, the flags written
  back (`Enraged` to `Combat.IsEnraged`; `Shielded` to a refreshed ward and
  `BehaviorComponent.Shielded`, which `applyStunEffect` reads as it reads the
  quasar's shield; `Beam` to `BehaviorComponent.Beaming`, which
  `TeleportLineRenderer` reads to draw the swarm's line to the stored cell; and
  `ChargeLine` to `BehaviorComponent.ChargeLine`, which `ChargeLineRenderer` reads
  to pulse the swarm's charge line toward it), then, unless suspended, gold
  breaking and contacts. `terminate` runs the state's exit (`Stop`) before the
  composite goes. It owns one manifest component, `Behavior` {program index,
  instance, pattern frame and accumulator, stored target, `Shielded`, `Beaming`,
  `ChargeLine`}.
- **Engine split.** This is how the engine takes species it was not built with: a
  data species adds a program and a prototype, not a component, a system or a mask
  bit.

### 6.5 A program's look

A program draws itself from its prototype's look, plain data the renderers read, so
a new species needs no renderer of its own and no code. The keys are in the
[FSM reference](fsm-reference.md#behavior-programs); vanguard's four raiders
(`wad/scenario/vanguard/behavior/*.toml`) are the shipped examples. To define one:

- **Frames.** `frames` holds a table per frame whose `glyphs` are `height` strings
  of `width` cells; a space draws nothing and the map shows through. Two frames or
  more cycle every `pattern_ms`, held while a stun holds. Draw the silhouette in
  glyphs, not in colour: a host that has few colours still shows the shape.
- **Glyphs.** Each is ASCII or one every text console font carries
  (`consoleGlyphs` in `renderer/color256_test.go`): the single, double and mixed
  box drawing, `█░▒`, `■▲▶▼◀♦`, the arrows `←↑→↓` and `·×÷°±•‼≈≡≤≥`. A console
  draws any other as `#` or a replacement mark, and a look has no console frames
  to fall back on, so half and quadrant blocks (`▀▄▌▐▘`) are out. The desktop
  window draws from the font atlas (`internal/asset/font/mono.toml`, rendered by
  `tool/font-atlas`), which must hold every glyph too.
  `TestConsoleGlyphsAreInEveryConsoleFont` and
  `TestTheFontAtlasCoversEveryGlyphTheCodeDraws` read every shipped scenario's
  TOML, so a glyph outside either set fails CI; `-check` does not test the glyph
  sets, so a scenario kept outside the repository is its author's to check.
- **Colour.** `fg` and `bg` are palettes of up to 16 colours. A frame's `fg` and
  `bg` rows give each cell a palette entry as a hex digit, a space meaning the
  look's `color` or no background, and `attr` gives a cell bold or dim. Colour the
  parts (shell, eyes, legs) rather than each cell, as the raiders do.
- **Console pair.** `fg256` and `bg256` are the pair a 256-colour host draws the
  whole look in, every glyph in the one and every background in the other, a hit
  reversing the glyph; without them the host takes each palette entry's nearest
  colour, which a text console's sixteen can merge into the background. Pick the
  pair from the console's own entries, the glyph bright (8-15) over a background
  of 0-7, which is all a Linux console's backgrounds take; such an entry passes
  through unchanged ([rendering §6](rendering.md#6-finalization-and-color-modes)).
- **State.** The look does not follow the machine's state: the hit flash, then
  `Enraged`, tints every glyph, `ChargeLine` pulses the charge line and `Beam`
  draws the teleport line to the stored cell, and `Telegraph` raises the splash
  countdown beside the body. A look per state
  would be a feature of its own.
- **Checking it.** `-check` compiles the look with its program
  (`visual.CompileLook`: palette sizes, rows and cells against the footprint,
  palette digits and attributes). Then play it: `bin/vif -s <scenario>` in tmux,
  captured with colour (`capture-pane -e`) under a 256-colour `TERM` and under
  `TERM=linux`, and in the desktop window with `-g`.

## 7. Determinism and identity

A machine adds no source of divergence and no identity of its own.

- **Plain integers.** `gen-manifest` lays a nested `machine.Instance[S]` out field
  by field through `go/types`, three columns of its component's capture, and
  snapshot equality compares it as any field. A component type must not implement
  a marshaler (`internal/gen-manifest` refuses `MarshalJSON` and `MarshalText`).
- **Relative timers.** No component holds an absolute instant (D-19). An
  instance's `Elapsed` is relative, which is also what lets a suspension freeze it.
- **Random draws.** A conditional per-entity draw seeds from (tick, entity)
  through `vmath.MixChain` (D-8), as mount spread and soft collision do, so a
  guard's short-circuit or a predicted death moves no stream. `Chance` mixes
  `BehaviorSystem.chanceRoot` (derived from the shared domain root at `Init`,
  carried by `SaveLocal`) with the tick, the entity and its site; the blue circle
  draws its landing's direction before it searches.
- **Order.** Edges run in declaration or array order, no map is iterated at run
  time, and programs are sorted by name before IDs are assigned. Verbs are native
  code under the existing float discipline, so nothing is interpreted.
- **Event domains.** Authors never name a domain: `manifest/fsm_bridge.go` routes
  a scenario's emit by class (Local to `PushLocal`, every other to `PushEvent`), a
  species effect crosses by `PushCrossing` behind `SimulatesLocally`, and each
  behavior verb binds its own routing (§6.2).
- **Identity.** `manifest.Fingerprint` hashes `definition.go` (each component's
  fields, types and domain; each system's name, domain, snapshot, requires and
  optional), not code, so a code graph rides on nothing. A program is a scenario
  file: it enters `ScenarioDigest`, travels with the scenario to a joiner and
  keeps to its bounds (`ScenarioMaxFiles`, `ScenarioMaxBytes`), so programs need
  no identity field, and the digest covers the names that fix their IDs.
- **Versions.** A change to a machine's component changes the columnar capture and
  bumps `snapshot.Schema`; a change to a beam's payload bumps
  `event.JournalSchema`; a change to what `-schema` prints bumps
  `manifest.SchemaVersion`; the fingerprint moves only with `definition.go`.
- **Component mask.** The entity mask is a `uint64`, so the manifest holds at most
  64 components, and a 65th fails to compile (`1 << iota` overflows). Data species
  spend no bit: all of them share `Behavior`, the program index inside. Widening
  the mask to `[2]uint64` would touch `World.AddComponentMask`, its removal and
  reads in `internal/engine` and the generated `Bit` constants.

## 8. Verification

Each rule above has a test named for it, beside its code:

- `pkg/machine`: each validation fault by its message, a step running its phases
  in order, a cue on a tick boundary firing once. `FuzzStep` steps a graph with
  guards, probes, suspension, dt and respawns drawn from a PCG seeded by its input
  and checks the step's invariants from inside the hooks that see them;
  `go test ./pkg/machine -run '^$' -fuzz FuzzStep -fuzztime 60s` runs it.
- `internal/system`: `quasar_test.go`, `swarm_stall_test.go`,
  `storm_attack_test.go`, `fuse_test.go` and `behavior_test.go` hold §4's and
  §6.4's rules. `aboutToExpire`, generic over the state type, starts an entry one
  nanosecond before it expires.
- `internal/behavior`: IDs by sorted name, a compiled program stepping as its
  states say, a compile error naming program, place and key, the name rules, the
  vocabulary's entries. `internal/resource`: a program as a scenario file, and
  `-check`. `internal/render/renderer`: the muzzle flash and a data species' frame.

Cost: `BenchmarkQuasarUpdate` (`internal/system`) times `QuasarSystem.Update`
alone, and `BenchmarkQuasars` (`internal/app`) the whole tick, each over a range of
quasar counts with a cursor crossing between corners, so the far quasars charge,
zap and come home. Compare builds with `benchstat` over alternating rounds, and
read the runtime's share from a CPU profile of `Update` (`Step` and its callees).
A step allocates nothing of its own; a machine's allocations are its hooks'
events.

Sessions: a change to a machine's species changes a shared system's state, so it
runs the divergence harness over its four shapes (`3,3,97,0`, `2,2,3,100`,
`3,3,251,0`, `4,3,41,50`) before and after
([netcode design §3.5](netcode-design.md#35-the-divergence-harness)) and reads the
late counts, which are robust, over pages repaired, which are chaotic:

```
VIF_DIVERGENCE=1 VIF_DIVERGENCE_SHAPE=<shape> go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v
```

A timing change that moves the storm's three-body orbit plays a seed as another
seed would, so compare distributions over many seeds rather than one fight's
counts.

Manual checks: tests and the harness do not see what a player sees, so the
implementer of a change to a machine runs its check on the built binary, in a
terminal or as a headless probe, before the PR is marked ready, and the PR body
says what was seen. `make dev`, then `bin/vif` (the checkout's `wad/` resolves
beside `bin/`); `:god` to survive, `:n <scenario>` to switch scenario,
`:emit <EventName> { payload }` to raise an event, `:log on` and
`:log stat <ticks>` for the status registry in the log; headless,
`bin/vif -script <file.toml> -seed <n>`, with `-watch` to see it. In tmux, pane
captures, one a tick, read the frames. Per species:

- Quasar: `:emit EventQuasarSpawnRequest { x = 40, y = 15 }`. It homes while the
  cursor is inside its circle; leaving it starts the cyan splash, then the
  lightning and the shield; coming back drops both; a pulse stun during the splash
  cancels it.
- Swarm: `a` to stop the auto-fire, `:emit EventSwarmSpawnRequest { x = 40, y = 15 }`.
  It chases, stops, its charge line pulses twice, and it charges onto the
  cursor's cell; behind a column of walls the lock ends in the teleport beam
  through it. A stun as the line shows ends the attack: the swarm stands for the
  stun, then chases from rest.
- Storm: `:emit EventStormSpawnRequest` with `:log stat 1`. The per-tick status
  log counts each colour's attack frames and `mount.fired`, and captures show the
  blue's beam and the swarm on its footprint the tick the beam goes. A disruptor
  alone (`:energy 100000000`, `EventWeaponRemoveRequest` for each weapon, `rod`
  to `gun`, `EventWeaponAddRequest { entity = 1, weapon = "disruptor" }`, `a`)
  stuns the circles in a series, and a stunned burst holds and resumes.
- Fusions: `:emit EventFuseQuasarRequest` lands its quasar the tick its beam's
  last cells go.
- Data species: `:n vanguard`, with `:god` to outlast the raid. A raider spawns at
  the right edge every two seconds while fewer than three fly, chases, locks red
  under the cyan countdown, and charges the cell it saw where its body clears the
  way, else beams beside it; a disruptor stun drops it back to its chase;
  `:emit EventBehaviorCancelRequest { program = "scarab" }` removes the scarabs.
  `bin/vif -check -s wad/scenario/vanguard` prints
  `behaviors ok: 4 (construct, invader, scarab, storm_eye)`. A look shows best in
  tmux pane captures with colour, on a 256-colour `TERM` and on `TERM=linux`.

## 9. Alternatives and prior art

The shapes adopted, with their precedents, then the alternatives weighed and why
each was not taken.

- **A state table with native actions**, adopted. Doom's `info.c` gives each mobj a
  state table whose rows hold a sprite, tics, a native action and the next state;
  DeHackEd and later DECORATE made the table data while the actions stayed native.
  A code graph is that table with typed hooks, and a behavior program its data form
  over a fixed native vocabulary.
- **A shared graph with per-entity instances**, adopted. Overwatch's Statescript
  shares graph data and replicates per-entity instance state, and Unreal's
  StateTree is an immutable asset with per-entity instance data whose priority
  enter conditions select states, as ordered global edges and candidate chains do
  here.
- **Data composing a fixed native vocabulary**, adopted for data species.
  RimWorld's ThinkTrees, the StarCraft II data editor and Factorio's prototypes
  compose native verbs from data and need identical data on every peer, which
  `ScenarioDigest` gives a session.
- **Conventions on hand-rolled code**: per-species enums, bools and countdowns,
  their rules in comments. The invariants stay non-local (a fallible entry made
  safe by another function's checks, a stun branch relying on combat), and there
  is no data path.
- **State as a function value**, Quake's `think` and `nextthink`. A func value is
  neither comparable nor encodable, so it breaks capture and digest, and
  persisting one needs a name table, Quake's tax.
- **A bytecode VM with a blackboard.** It needs a verifier and every interpreted
  float operation under the conversion discipline, and a blackboard copied in goes
  stale mid-step; compare guards, `And`, `Or`, `Not` and typed actions are enough,
  as the `td` scenario shows.
- **Behavior-tree or StateTree selection as the runtime.** Species attacks are
  timed cycles, and ordered global edges already give priority preemption; a
  selector would be a front-end over this runtime, not a runtime of its own.
- **A tag component per state.** Each state would cost a manifest edit, a store, a
  snapshot section, a mask bit and a fingerprint change, so states could not come
  from data.
- **A central `MachineSystem`.** It would break each species' order of lifecycle,
  step, physics and interactions, and need peer references to bind species
  vocabularies.
- **Per-entity `internal/fsm` instances.** That runtime is event-triggered and
  map-backed, with regions, variables and delayed actions; the dialect is shared,
  not the runtime.
- **Utility AI or GOAP.** Opaque tuning and tie-breaks whose determinism needs
  auditing, for attack cycles that need neither.
- **Machines injected through the manifest.** Construction is uniform and most
  systems have no machine; a graph is immutable per-species data, not a service.
- **Intent arrays as the step's output.** They allocate and go stale; hooks act
  through the context.
- **Programs in the build fingerprint.** The fingerprint is compile-time manifest
  identity, while programs are session data that `ScenarioDigest` covers.
- **Data species IDs above `SpeciesCount` without resizing the tables.** A data ID
  would index past every `[SpeciesCount]` table; the tables are `[SpeciesMax]`
  instead (§6.4).
- **Widening the component mask for data species.** They share one component, so
  they spend no bit (§7).
