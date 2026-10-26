# Behavior machines

The design and the phased plan for per-entity state machines in species systems
and for the data-driven species they prepare. §1 to §8 are the design, verified
against the code at the plan's date; §9 is the plan, one phase per task, branch and
PR, each with the files it touches, the tests it adds and the state a person can
check by hand. Phase 0 has landed. As phases land, this document becomes the
subsystem's reference and §9 shrinks to what each phase moved.

## 1. Decision

One runtime, two front-ends (the accepted O1 + O2 + O4):

- **Kernel (O1).** `machine.Instance[S]`: a state byte plus relative `Elapsed`
  and per-entry `Duration`, stored inside the species component. Plain
  integers, so capture, compare, digest and install are unchanged in kind.
- **Code graph (O2).** `machine.Graph[S, C]`, built once in the system
  constructor from typed Go: per-state probe/enter/exit/tick hooks, a flag mask,
  a default duration and cues; ordered edges with candidate chains; global
  preemption edges with source masks; one suspend predicate. The builder
  validates the graph at construction.
- **Data front-end (O4).** Behavior programs written in the scenario's TOML
  dialect and compiled against a game vocabulary into the same `Graph` type.
  Names resolve to closures over pre-decoded arguments at load; nothing is
  interpreted per tick.

Swarm and Quasar adopt it for correctness, not speed or line count. Storm
circles adopt it with a variant table. Every other species keeps its code (§5).
The scenario HFSM (`internal/fsm`) stays the world-level director: it is
event-triggered, map-backed and sized for a handful of regions, so it is not the
per-entity runtime, but the new front-end shares its dialect (§7).

Placement: `pkg/machine` is standard library only (`make arch-check` reports a
`pkg/` package importing `internal/`); the dialect kernel is
`internal/fsm/dialect`, beside its first user; the program compiler and its
schema are `internal/behavior`; the game vocabulary and the generic species
system are one file, `internal/system/behavior.go`, so the domain audit
(`internal/system/domain_test.go`) sees every store the verbs touch.

## 2. Verification against the code

**Confirmed.**

- Swarm (`internal/system/swarm.go`) has a five-state enum with five countdown
  fields, one live per state. Stun resets it to Chase on the first stunned tick
  (`resetSwarmState`) and then freezes it. The two wedge fixes (a failed lock
  re-arms `SwarmTransitionRetryInterval`, a failed charge falls to Decel) are
  pinned by `swarm_stall_test.go`.
- Quasar (`quasar.go`) encodes three legal states in three bools. A failing
  `startZapping` returns before its commit, and that is unreachable only because
  `isCursorInZapRange` returns true on the same failures. `SplashTimerCancel`
  and the lightning despawn are each emitted from two sites.
- Storm circles (`storm.go`) run Idle/Cooldown/Active through three
  `switch circleType` tables. A missing cursor skips the attack step, physics
  honours stun (`updateCirclePhysics` reads the header's `StatusStun`) while
  attacks do not, the root's `pendingBlueSpawns` is a D-19 carrier
  (`stormSnapshot`, `SaveShared`) with relative timers, and the blue landing
  draw precedes validation (D-8, pinned by `storm_attack_test.go`).
- Commit styles differ: swarm and storm circles use `GetPtr`; quasar and the
  storm root use a detached `GetComponent` plus `SetComponent`.
- Telemetry binds in constructors, and the status registry closes at
  `Scheduler.Prepare` (`Status.Freeze`); a key first asked for after that is a
  detached cell. The tick is 50 ms (`parameter.GameUpdateInterval`) and
  `MaxSimulationDeltaSeconds` (0.1 s) never binds.
- Generic methods compile on the module's toolchain (go 1.27.2): a method with
  its own type parameters on a generic receiver, and `Op.Holds[T cmp.Ordered]`.
  A generic method cannot satisfy an interface method.
- Startup now initialises every system through `World.InitSystems` (Phase 0),
  so a per-run table a system fills in `Init` is rebuilt for every game.

**Corrected.**

1. **Fingerprint.** `manifest.Fingerprint` hashes `definition.go` (component
   field, type and domain; system name, domain, snapshot, requires, optional),
   not code. Code-defined graphs ride on nothing. Data programs are scenario
   files: they enter `ScenarioDigest`, travel with the scenario to joiners and
   obey its bounds (`ScenarioMaxFiles` 64, `ScenarioMaxBytes` 1 MiB). No new
   identity field is needed (§8.3).
2. **One dialect, not two.** `internal/bot` is an `fsm.Machine[*mind]` that
   calls `std.Register` and adds its own vocabulary. There is one dialect with
   two vocabularies, and the behavior front-end becomes the third (§7).
3. **Event domain.** `manifest/fsm_bridge.go` already routes FSM emits by class
   (Local to `PushLocal`, everything else to `PushEvent`), and species effects
   use `PushCrossing` gated by `SimulatesLocally`. Authors never name a domain:
   each verb binds its own routing.
4. **RNG.** D-8 prescribes seeding per-entity conditional draws from (tick,
   entity) through `vmath.MixChain`, as mount spread and soft collision do. A
   `Chance` predicate draws that way, so guard short-circuiting cannot move any
   stream.
5. **Relative timers.** D-19 states that absolute instants are sound. The
   instance uses a relative `Elapsed` anyway: suspension has to freeze a timer.
6. **Moore outputs stay materialized.** Combat reads
   `QuasarComponent.IsShielded` and `StormCircleComponent.IsInvulnerable` for
   stun immunity (`applyStunEffect`). The host writes the flag-derived fields on
   every step, and the components keep them.
7. **Renderers.** `renderer/chargeline.go` and `teleportline.go` recompute
   elapsed time as `parameter.X - Remaining`; `renderer/storm.go` reads
   `AttackState` and `AttackProgress`; `renderer/quasar.go` reads
   `IsCharging || IsZapping`. `Instance.Elapsed`, `Duration` and `Progress()`
   serve all three.
8. **Capture schema, not fingerprint.** Replacing component fields changes the
   columnar capture (`snapshot.Schema`, 25 at the plan's date), so each phase
   that changes a store bumps it. The fingerprint moves only when
   `definition.go` changes, which Phase 4 does by adding a component and a
   system. `gen-manifest` lays a component out field by field through
   `go/types`, so a nested `machine.Instance[S]` becomes three columns with no
   generator change; a component type must not implement a marshaler.
9. **Component mask.** The entity mask is a `uint64` and the manifest declares
   54 components, so 10 bits remain; a 65th fails to compile (`1 << iota`
   overflows). The data path spends one bit, on `Behavior`, and data species
   none: every file-defined species shares that component with its program
   index inside. The mask stays 64 wide; widening it to `[2]uint64` would touch
   `World.AddComponentMask`, its removal and reads (eight sites in
   `internal/engine`) and the generated `Bit` constants, and nothing in this
   plan needs it.

## 3. Current machines

| System | Encoding | Timers | Preemption | Fallible entries | Moore outputs | Commit |
|---|---|---|---|---|---|---|
| Swarm | enum ×5 | 5 countdowns, pattern cycle | stun → Chase, then freeze | Lock, Charge, Teleport | `IsEnraged`; `ChargesCompleted++` on Decel entry | `GetPtr` |
| Quasar | 3 bools | `ChargeRemaining`; speed scaling on an instant | stun cancels Charging; in range cancels both | Zapping | `IsEnraged`, `IsShielded`, `Shield.Active`, ward status | detached |
| Storm circle | enum ×3 | cooldown/attack countdowns; `InvulnerableElapsed` | none; no cursor skips the step | none (Active waits for convex) | `IsInvulnerable`, `Mount.Armed`, `AttackProgress`, Z clamp | `GetPtr` |
| Storm root | liveness aggregation | `pendingBlueSpawns` | — | — | — | detached |

Snake, drain, eye, gateway, fuse, pylon, tower, kraken and materialize have no
behavioral machine (§5).

## 4. Timer and state inconsistencies

Each item gives the finding, the proposal and the decision taken for the plan.

- **S1. Storm: the blue spawn queue outlived its storm.** Landed:
  `terminateStorm` drops the queue. Decided: keep the drop, no flush.
- **S2. Storm: the blue one-shot rests on parameter arithmetic.** The
  materialize fires once only because its dedup entry (`hasPendingBlueSpawn`,
  1 s) outlives the attack's tail (0.5 s of 2.5 s); `AttackTargetX != 0` works
  as "no target" only because `SwarmHeaderOffsetX = 1`. Decided: a cue at 80 %
  of Active, which fires once per entry by construction (§6.3), plus a stored
  `LandingFound` from the entry hook. That drops the dedup scan and the
  sentinel.
- **S3. Storm: outputs lag the state by one tick.** `Mount.Armed` and
  `IsInvulnerable` are written before the state switch. Decided: write flags
  after the step (the machine default); the one-tick shift is accepted.
- **S4. Storm: the no-cursor gate also skips outputs.** With no cursor an
  Active red circle keeps `Mount.Armed = true` and a stale `IsInvulnerable`.
  Decided: no-cursor is a suspend condition; timers freeze, flags are written
  every step, and `Armed` reads false while suspended.
- **S5. Storm: stun freezes physics but not attacks.** Decided: stun is the
  other suspend condition, so a stunned circle pauses its attack as swarm and
  quasar pause theirs. Stun lands only on a convex or attacking circle, so this
  changes play: a stunned burst or pulse stops until the stun ends.
- **S6. Storm: three switch tables for one topology.** Decided: one
  `[StormCircleCount]` variant row {initial, attack, repeat, cue}, per the
  per-kind table rule. Idle disappears: spawn enters Cooldown with the initial
  duration, and Active's timeout re-enters it with the repeat duration.
- **S7. Quasar: Charging → Zapping totality is non-local** (§2). Decided:
  Charging timeout → [Zapping (probe: position and cursor) | Homing], with an
  unconditional commit.
- **S8. Quasar: exit effects are duplicated.** Decided: Charging's exit hook
  cancels the splash; Zapping's exit hook despawns the lightning and drops the
  shield; termination calls `Graph.Stop`.
- **S9. Quasar: the stun branch ignores Zapping.** It relies on combat refusing
  to stun a shielded quasar. Decided: a global stun edge from {Charging,
  Zapping} to Homing; its Zapping half never fires today, but correctness no
  longer depends on combat.
- **S10. Quasar: entry ticks are inconsistent.** Entering Charging skips homing
  for that tick, and entering Zapping deals no damage until the next tick, while
  leaving Zapping homes on the same tick. Decided: run-to-completion runs the
  resulting state's tick uniformly, adding one homing tick and one zap damage
  tick per cycle; accepted.
- **S11. Quasar: speed scaling starts at once.** `LastSpeedIncreaseAt` starts
  at the zero instant, so the first movement tick lifts `SpeedMultiplier` from
  1.0 to 1.1. Decided: a carried accumulator (`SpeedElapsed`) counting movement
  time from spawn, so the first increase lands after `QuasarSpeedIncreaseTicks`
  (20 ticks, 1 s) as the parameter name says.
- **S12. Swarm: transition ticks skip integration.** Charge timeout → Decel,
  Decel timeout → Chase and Chase → Lock all return without integrating, while
  the wall-hit path integrates first. Decided: timeouts fire before the tick, so
  the resulting state's tick runs in the same step and Decel's first drag tick
  lands one tick earlier; accepted. The wall hit stays a post-tick edge.
- **S13. Swarm: five timers with one live.** `resetSwarmState` zeroes three of
  the four attack timers but not `TeleportRemaining`, harmless only because every
  entry re-arms its own timer. One instance timer removes the class.
- **S14. Swarm: stun asymmetry by state is intended.** Stun in Chase freezes
  the interval; stun anywhere else cancels the attack and restarts the full
  interval. This becomes a global edge from {Lock, Charge, Teleport, Decel} to
  Chase carrying `SwarmChargeInterval`, followed by suspension.
- **S15. Timer idioms diverge across species.** Countdown `Remaining` (swarm,
  quasar, storm, eye frames, the fuse and storm queues); lifetimes and one-shot
  deadlines on `TimerComponent` slots (the swarm's lifetime included); count-up
  with carry (gateway); count-up with reset (storm `InvulnerableElapsed`);
  absolute instants (quasar speed); absolute ticks (drain spawn schedule).
  Decided: phases move to `Instance`; the swarm pattern and the quasar speed
  move to a carried accumulator in their phases; eye frames, gateway and drain
  stay as they are.
- **S16. Three hand-rolled spawn queues.** Fuse (`fusions`, a `LocalStateSaver`
  carried by a journal's opening), storm (`pendingBlueSpawns`, a
  `SharedStateSaver`) and drain (`pendingSpawns`) each defer a spawn behind a
  materialize. Fuse and storm run a timer the length of the materialize and
  swap-remove while iterating in reverse, so same-tick emission order depends on
  slice history. Drain completes on `EventMaterializeComplete`, not a timer.
  Decided: one `machine.Queue[T]` (relative timers, firing in push order, any
  number of entries, JSON-encodable), adopted by storm and fuse in Phase 6; drain
  keeps its event-completed queue. "Two users" was the rule for admitting a
  sibling primitive, not a capacity.

## 5. Target graphs

**Swarm** (`C = *swarmCtx`):

| State | Duration | Flags | Probe / enter | Tick | Edges |
|---|---|---|---|---|---|
| Chase | 5 s interval | — | — | home, integrate | timeout → [Lock], else `Retry(200 ms)` |
| Lock | 2 s | Enraged | probe: base target; enter: store target, zero velocity | — | timeout → [Charge (guard: LOS, probe: kinetic) \| Teleport (probe: landing) \| Decel] |
| Charge | 0.8 s | Enraged | enter: start, target, velocity | steer, integrate; records wall hit | timeout → Decel; post-tick wall hit → Decel |
| Teleport | 0.4 s | Enraged | enter: from/to, zero velocity | — | timeout → Decel, edge action: clear area, relocate |
| Decel | 0.1 s | Enraged | enter: `ChargesCompleted++` | drag, integrate | timeout → Chase (5 s) |

- Global edge: stunned, from {Lock, Charge, Teleport, Decel} → Chase with the
  full interval. Suspend: stunned.
- The pattern accumulator advances only when the step was not suspended.
- HP, the charge limit, cancel and breach stay as lifecycle checks ahead of the
  step and call `Stop`.
- `swarm.transition_stalls` counts `CauseRetry` and a Lock → Decel commit
  through the observer, which keeps today's meaning of the key.
- Relocation is an edge action rather than an exit hook, because a stun during
  Teleport must not move the swarm.

**Quasar** (`C = *quasarCtx`):

| State | Duration | Flags | Probe / enter / exit | Tick | Edges |
|---|---|---|---|---|---|
| Homing | — | — | — | home (speed accumulator inside) | pre-tick: not in range → Charging |
| Charging | 3 s | Enraged | enter: splash request; exit: splash cancel | home | timeout → [Zapping \| Homing] |
| Zapping | — | Enraged, Shielded | probe: position, cursor; enter: lightning; exit: despawn, shield off | retarget, zap damage | — |

- Global edges, in priority order: stunned, from {Charging, Zapping} → Homing;
  in range, from {Charging, Zapping} → Homing. Suspend: stunned.
- From the flags, the host writes `Combat.IsEnraged`, `IsShielded` and
  `Shield.Active`, and refreshes the ward status while shielded.
- The component moves to `GetPtr`: the detached copy existed only to order
  commits that the step now makes unconditional.

**Storm circle** (`C = *circleCtx`, one graph per variant row):

| State | Duration | Flags | Hooks | Edges |
|---|---|---|---|---|
| Cooldown | row.initial at spawn, row.repeat after Active | — | — | timeout → [Active (guard: convex)], else `Hold` |
| Active | row.attack | Attacking (+ Armed on red) | enter: blue landing search (one draw, then validation); tick: variant pulse | timeout → Cooldown (row.repeat) |

- Blue cue at 800 ‰: if `LandingFound`, materialize and push the spawn on the
  queue.
- Suspend: no cursor or stunned (the header's `StatusStun`).
- The host derives `IsInvulnerable = concave && !Attacking` and
  `Mount.Armed = Armed && !suspended`; the physics Z clamp reads Attacking.
- `AttackProgress` gives way to `Instance.Progress()`, read only while Active.

**No adoption.** Snake (shield is body liveness, spawning a counter), drain
(HP-threshold enrage, player-domain population queue), eye (frame cycle and
GA-driven motion), gateway (periodic emitter), fuse (a queue, S16), pylon and
tower (stationary), kraken, materialize (a progress animation), storm root
(liveness aggregation).

## 6. Runtime (`pkg/machine`, stdlib only)

### 6.1 Instance

```go
type Instance[S ~uint8] struct {
	State    S             `json:"state"`
	Elapsed  time.Duration `json:"elapsed"`
	Duration time.Duration `json:"duration"` // 0 = untimed
}
func (in *Instance[S]) Progress() float64 // Elapsed/Duration clamped to [0,1]; 0 when untimed
func (in *Instance[S]) Expired() bool     // timed and Elapsed >= Duration
```

`Duration` is stored because entries override it (storm initial or repeat,
`Retry`). There is no entry counter: cues make one-shots structural, and RNG
seeds from (tick, entity).

### 6.2 Graph

```go
type Mask uint64                                 // bit per state; MaskOf(states...)
type Cause uint8                                 // Start, Timeout, PreTick, PostTick, Global, Retry, Stop
type Observer[S ~uint8] func(from, to S, cause Cause)

type State[C any] struct {
	Name     string
	Duration time.Duration // default for entries that set none
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
	To       []Target[S, C] // candidates tried in order; Duration 0 takes the state's default
	Fallback Fallback       // Retry(d) or Hold; required when every candidate can fail
}
```

The builder, `machine.New[S, C](observe)`, takes `State(id, st)`, one
`Timeout(from, edge)` per state, ordered `PreTick(from, edge)` and
`PostTick(from, edge)` lists, ordered `Global(edge)` and one `Suspend(pred)`.
`Build` validates and returns an immutable `*Graph[S, C]`; `Must` wraps it for
code graphs, which panic in their constructor as a broken manifest would, while a
data graph's failure is a load error. Graphs are limited to 64 states because
`Mask` is a `uint64`. A nil observer is allowed.

### 6.3 Step

```go
func (g *Graph[S, C]) Step(in *Instance[S], c C, dt time.Duration) (flags uint64, suspended bool)
func (g *Graph[S, C]) Start(in *Instance[S], c C, s S, d time.Duration) // spawn: enter without exit
func (g *Graph[S, C]) Stop(in *Instance[S], c C)                         // death: exit without enter
```

A step runs these phases in order:

1. **Global edges**, in declaration order. The first whose `From` holds the
   current state and whose guard passes takes its transition.
2. **Suspend.** If the predicate holds, return the current flags with
   `suspended` set. There is no advance, cue or tick.
3. **Advance** `Elapsed` by dt. Fire every cue whose point falls in
   (old, new], computed in integer nanoseconds.
4. **Timeout**, if the state is timed and has expired.
5. **Pre-tick edges**, in declaration order.
6. **Tick** of the current state.
7. **Post-tick edges.** These read outcomes the tick left in `C`.
8. **Return** the current state's flags.

Global edges run before suspension so that stun can cancel an attack and then
freeze the result. Each phase takes at most one transition, so a step makes at
most four.

A transition tries its candidates in order, guard then probe. On the first
success it runs `Exit(current)`, then `Do`, then `Enter(target)`, commits
`{State, Elapsed: 0, Duration}` and reports `(from, to, cause)`. Exit runs only
when a transition commits, and probes keep fallible work out of hooks that cannot
fail. If no candidate commits: `Retry(d)` re-arms the current state in place
(no exit or enter) and reports `CauseRetry` with `from == to`; `Hold` leaves the
state expired, so the edge is evaluated again next step.

The resulting state's tick runs in the step that entered it (S10, S12 accepted).
A tick that must skip its entry step tests `Elapsed == 0`.

### 6.4 Validation

`Build` rejects a graph when a target or start state is undefined; a candidate
chain can fail entirely (every candidate has a guard or probe) and declares no
fallback; a `Retry` duration is zero or less; a cue or timeout sits on an untimed
state; a cue point lies outside (0, 1000]; a global edge has an empty `From`; a
state declares two timeout edges. The validator turns the swarm comments ("every
failed entry must land in a state that moves") into structure: a wedge needs a
skipped tick, and the step never skips one.

### 6.5 Host contract

- **Context.** Each species has one context struct, held as a system field and
  refilled per entity: world, entity, component pointers, observations sensed
  once before the step (stunned, in range, has cursor), probe results and tick
  outcomes. Passing a pointer to a field of the long-lived system allocates
  nothing per call.
- **Mutation.** The runtime mutates only the `Instance`; hooks reach the world
  through `C`, and no store is touched by the machine. Hosts hold the instance
  through `GetPtr`.
- **Lifecycle.** HP, limits, cancel and breach stay ahead of the step and call
  `Stop`, so death paths run exit effects such as the lightning despawn.
- **Flags.** Flags map to component fields on every step, suspended or not (S4).
- **Observer.** Bound at construction; feeds status counters without the package
  importing `status`. Nothing in this plan builds an overlay over `States()`;
  the accessor exists for one.

### 6.6 Generics and sibling primitives

- **Typed IDs.** `S ~uint8` IDs index per-state slices, so typed graphs reject
  a foreign enum at compile time.
- **Context type.** `C` is a pointer type, and hooks are func fields rather than
  constraint methods, so instantiations that share a GC shape pay no dictionary
  dispatch.
- **Generic methods** tidy the dialect kernel's registration and comparisons
  (§7); because one cannot satisfy an interface method, both stay concrete
  types.
- **Deferred queue** (decided, S16):

  ```go
  type Item[T any] struct { Remaining time.Duration `json:"remaining"`; Value T `json:"value"` }
  type Queue[T any] struct { Items []Item[T] `json:"items"` }
  func (q *Queue[T]) Push(after time.Duration, v T)
  func (q *Queue[T]) Tick(dt time.Duration, fire func(T)) // expired items fire in push order, then compact
  func (q *Queue[T]) Len() int
  func (q *Queue[T]) Clear()
  func (q *Queue[T]) Any(match func(T) bool) bool
  ```

  Any system may hold one; storm and fuse adopt it in Phase 6.
- **Carried accumulator.** A `time.Duration` field advanced by dt and reduced
  by its period, carrying the remainder: the swarm pattern (Phase 5) and the
  quasar speed (Phase 2). Two fields, no type.

## 7. Shared dialect kernel (fsm, bot, behavior)

**Today:**

- Guard factories take `map[string]any` (`fsm.GuardFactoryFunc`).
- `std.ParseIntArg` (`fsm/std/guard.go`) returns 0 for a missing or mistyped
  key; `std.CompareInt` switches on the op string at every evaluation and
  treats an unknown op as `eq`; `std.Ops()` lists the six names for `-schema`.
- `std.decodePayload` (`fsm/std/args.go`) decodes action payloads with
  `toml.Decode`, which drops unknown keys, as does the `ScenarioDoc` decode in
  `fsm/loader.go`; `bot/mind.go` hand-lists allowed keys (`decodePayload`,
  `unknownKey`) and reads ints through `ParseIntArg`.
- `manifest.Schema` (`SchemaVersion` 1) lists guard and action names, the ops,
  the config fields and every event's payload fields, but no guard or action
  arguments.

**Kernel**, `internal/fsm/dialect`, one implementation used by `fsm/std`, `bot`
and the behavior compiler:

1. **`Op`.** `ParseOp` accepts the six names and rejects any other; an omitted
   op stays `eq`, which the reference documents. `Op.Holds[T cmp.Ordered](a, b T)`
   evaluates it. This replaces the per-tick string switch and the silent `eq`.
2. **`Decode[A any](raw any, where string) (*A, error)`.** A strict typed
   decode: keys are checked against A's `toml` tags (cached per type), then
   `toml.Decode` runs; an unknown key names its table. It replaces
   `ParseIntArg`, bot's allowed-key lists and std's lenient decode. `Fields[A]`
   feeds the schema, so an editor learns every guard's and action's arguments.
3. **Guard trees.** The `{ name, args }` / `guards = [...]` shape of
   `And`/`Or`/`Not` is parsed once, generically over the guard type:
   `Tree[G any](raw any, leaf func(string, map[string]any) (G, error), all, anyOf func([]G) G, not func(G) G) (G, error)`.
   Each runtime keeps its own guard signature; `fsm.GuardFunc[T]` sees region
   and payload, a behavior guard only `C`.
4. **`Catalog`.** Name → {kind, fields} in deterministic order, the source of
   `-schema` output for every vocabulary.

The runtimes are not shared (O9).

## 8. Data-driven behavior (O4)

### 8.1 Program shape

The program uses the fsm dialect's own keys, with the machine's additions
(`duration_ms`, `flags`, `cues`, `suspend`, `[[global]]`, a candidate list as
`target`, `fallback`, the `Timeout` and `AfterTick` triggers) and a `[prototype]`
table:

```toml
# behavior/warden.toml, listed by the scenario entry
initial = "Roam"
suspend = { guard = "Stunned" }

[prototype]
width = 4
height = 2
header = { x = 1, y = 0 }           # phantom head offset from the top-left
hp = 40
combat = "swarm"                    # CombatEntityType row the attack matrix already holds
spawn = "swarm"                     # materialize style
homing = "swarm"                    # physics.HomingProfile
contact = { energy = 1000, heat = 10 }
lifetime_ms = 35000
glyphs = [["▄▀▀▄", "▀▄▄▀"], ["▀▄▄▀", "▄▀▀▄"]]   # frames of height rows
pattern_ms = 500
color = { r = 200, g = 120, b = 40 }

[states.Roam]
on_update = [{ action = "Home" }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { op = "gt", cells = 12 }, target = "Wind" }]

[states.Wind]
duration_ms = 1500
flags = ["Enraged"]
on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]
on_exit = [{ action = "CancelTelegraph" }]
cues = [{ at = 800, action = "Materialize", payload = { spawn = "swarm" } }]
transitions = [{ trigger = "Timeout", target = ["Strike", "Roam"] }]

[states.Strike]
duration_ms = 800
flags = ["Enraged"]
probe = "ResolveTarget"
on_enter = [{ action = "StoreTarget" }]
on_update = [{ action = "ChargeToStored" }]
transitions = [{ trigger = "Timeout", target = "Roam" }, { trigger = "AfterTick", guard = "HitWall", target = "Roam" }]

[[global]]
from = ["Wind", "Strike"]
guard = "Stunned"
target = "Roam"
```

How the keys map onto the runtime:

- `trigger` is `Timeout`, `Tick` (pre-tick) or `AfterTick` (post-tick).
- `target` is a name or a candidate list; a candidate may be
  `{ state = "Strike", guard = "LineOfSight", duration_ms = 600 }`. `fallback`
  is `{ retry_ms = n }` or `"hold"`.
- `flags` resolve against the vocabulary's flag table (`Enraged`, `Shielded`).
- State IDs follow sorted names, because map-decoded tables carry no order.
  Edge order is array order.

### 8.2 Vocabulary (`internal/behavior`)

The registry is generic (`behavior.Vocabulary[C]`); the game's verbs bind to the
generic system's context in `internal/system/behavior.go`.

- Predicates (read-only): `TargetDistance{op, cells}`, `LineOfSight`,
  `HPFraction{op, value}`, `Stunned`, `HasTarget`, `HitWall`,
  `Chance{percent}` (seeded from tick, entity and site), `Param{index, op, value}`.
- Probes (read-only, results land in `C`): `ResolveTarget`
  (`resolveBaseTarget`), `FindLanding` (`FindFreeAreaSpiral` over the footprint).
- Effects (enter, exit, cue, edge): `StoreTarget`, `ZeroVelocity`, `Teleport`,
  `Telegraph{color}`/`CancelTelegraph` (the splash request pair), `Lightning`,
  `Materialize{spawn}`, `SpawnSpecies{species, x, y}`, `EmitEvent{event, payload}`.
  Each verb fixes its push (presentation local, shared requests shared, cursor
  damage local behind `SimulatesLocally`); `EmitEvent` takes only Shared or
  Local classes.
- Sustained (tick): `Home`, `ChargeToStored`, `Drag{factor}`, `Freeze`, each
  wrapping the in-place helpers of `pkg/vmath/physics` the species use
  ([generic kinetic §8](generic-kinetic.md#8-feasible-pseudo-system-design)).

Contact interactions (`strikeContacts` with the prototype's damage), lifecycle
and spawning stay in the generic system's scaffold, not in the vocabulary.

### 8.3 Delivery and identity

- **Programs are scenario files.** The entry lists them
  (`[behaviors] warden = { file = "behavior/warden.toml" }`).
  `fsm.loadAndResolve` follows that table as it follows `regions.*.file` and
  `macro_files`: it reads the file, expands it against the entry's macros,
  records it in `visited` and replaces the table with the expanded document.
  `ScenarioFiles` therefore lists programs, so `resource.ReadScenario`
  (`internal/resource/scenario.go`) carries them, digests them and transfers
  them with no new mechanism; `validScenarioName` admits the subdirectory.
  `Machine.LoadScenarioFromMap` deletes `behaviors` before decoding
  `ScenarioDoc`; `fsm.ResolveScenario` keeps it, which is how a reader reaches
  it: `Scenario.Behaviors()` resolves the entry and returns the programs by
  name.
- **Compiled once per run.** `App.readScenario` compiles the programs against
  `system.BehaviorVocabulary()` right after `resource.LoadScenario`, so a bad
  program fails like a bad scenario, before any service starts. The prototypes
  go into `world.Resources.Behaviors` in `initWorld` before the systems are
  built, which lets `NewMetaSystem` name its kill counters and the renderer
  read glyphs; the compiled graphs go to `system.NewBehaviorSystem(world, set)`,
  which the app adds beside `MetaSystem` (`manifest.ContextSystems`), because
  the generator's constructors take only a world.
- **`-check` compiles every program:** `resource.ValidateScenario` runs the same
  compile, so an unknown name or bad argument fails validation, not play.

### 8.4 Determinism

Edges run in array order; no map is iterated at run time; `Chance` uses no
stream (§2, correction 4); verbs are native code under the existing float
discipline, so nothing is interpreted; programs are sorted by name before IDs
are assigned, and the digest covers the names.

### 8.5 Beyond the machine

A data species needs a prototype before behavior matters. The machine is a
minority of the work.

- **Prototype.** Footprint and header offset, hit points, the combat entity row
  it fights as, materialize style, homing profile, contact damage, lifetime,
  glyph frames and colour, pattern period. It names existing rows of the
  per-kind tables that are code (`CombatEntityType`, `SpawnType`,
  `physics.HomingProfile`); a new attack profile or materialize style stays a
  code change.
- **Species IDs** (decided). `component.SpeciesType` keeps its code enum ending
  at `SpeciesCount`; `SpeciesDataFirst = SpeciesCount` and
  `SpeciesMax = SpeciesCount + 8` bound the data range. Programs take
  `SpeciesDataFirst + i` in sorted-name order; a scenario with more than eight
  fails to compile. The six tables indexed by `SpeciesType` grow to
  `[SpeciesMax]` so a data ID never indexes out of range: `profile.Contact`,
  `profile.SoftKraken`, `profile.Drops`, `component.SpeciesDimensionsLUT`
  (unread today), the soft-collision matrices and caches, and `MetaSystem`'s
  kill counters and snapshot. The package-level profile rows for data species
  stay zero; a data species' values come from its prototype at the reader: the
  generic system passes the prototype's contact damage, loot reads
  `Resources.Behaviors` for a data species' drops, and soft collision builds the
  data rows of its matrix in `Init` from the prototype's mass. The range checks
  `>= SpeciesCount` in `meta.go`, `genetic.go` and `loot.go` become
  `>= SpeciesMax`.
- **Composite routing.** `component.Behavior` gains `BehaviorData`; the generic
  system handles `EventCompositeIntegrityBreach` for it. `handleEmptyComposite`
  needs no case: its default destroys the head.
- **Events.** `EventBehaviorSpawnRequest{program, x, y}` and
  `EventBehaviorCancelRequest{program}` (bus class, like the swarm's) are what a
  scenario emits; genetics stays `EventGeneticRegisterSpecies` with the data
  species' ID, and `SpeciesCreatedPayload.Genes` lands in the component's
  `Params`.
- **Telemetry.** `newLifecycleTelemetry(reg, program)` keys `<program>.*` bind
  in `NewBehaviorSystem`, `kills.<program>` in `NewMetaSystem`, both before
  `Freeze`.
- **Renderer.** One `BehaviorRenderer` (`renderer/behavior.go`,
  `render.PriorityBehavior` after `PrioritySwarm`) draws each `Behavior`
  header's members from the prototype's glyph frames, with the swarm renderer's
  enraged and hit-flash colouring.
- **Generic species system.** `internal/system/behavior.go` runs every data
  species: lifecycle checks, `Step`, flag mapping, contacts, sweep; it owns one
  manifest component, `Behavior` {program index, instance, pattern
  accumulator, locked target, `Params`}. That is the one fingerprint change.
- **Engine split.** This is also how the engine takes species it was not built
  with: a data species adds a program and a prototype, not a component.

## 9. Phased plan

### 9.0 Decisions

Taken with the user for this plan; each phase below assumes them.

- Order: kernel, quasar, dialect kernel, data path, swarm, storm.
- Startup initialisation moves into the engine (Phase 0, landed).
- Data species IDs: `SpeciesDataFirst`/`SpeciesMax`, eight reserved rows.
- Placement as §1: `pkg/machine`, `internal/fsm/dialect`, `internal/behavior`,
  `internal/system/behavior.go`.
- Storm suspends on no cursor or stun, `Armed` off while suspended (S4, S5).
- The one-tick shifts are accepted (S3, S10, S12).
- Quasar speed counts from spawn (S11).
- One `machine.Queue[T]`, adopted by storm and fuse (S16); S1's drop stays.
- The component mask stays `uint64`; `Behavior` is its 55th bit.

Conventions for every phase: one task, one branch, one PR; the gates of
`AGENTS.md` (`go build ./...`, `gofmt -l`, `go test` on the touched packages,
`make generate` when `definition.go`, an event or a component's fields change,
`make arch-check` when `pkg/` changes, the tagged builds when a constrained file
changes); a change to a shared species system's state runs the divergence
harness over its four shapes before and after, reading the late counts:

```
VIF_DIVERGENCE=1 VIF_DIVERGENCE_SHAPE=3,3,97,0  go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v
VIF_DIVERGENCE=1 VIF_DIVERGENCE_SHAPE=2,2,3,100 go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v
VIF_DIVERGENCE=1 VIF_DIVERGENCE_SHAPE=3,3,251,0 go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v
VIF_DIVERGENCE=1 VIF_DIVERGENCE_SHAPE=4,3,41,50 go test ./internal/app -run TestADivergenceOverAShapedLinkNamesItsRows -v
```

Version moves, one per phase that changes a store's layout or adds one:
`snapshot.Schema` 25 → 26 (Phase 2), 27 (Phase 4), 28 (Phase 5), 29 (Phase 6);
`event.JournalSchema` 39 → 40 (Phase 6, the fuse queue's `SaveLocal` form);
`manifest.SchemaVersion` 1 → 2 (Phase 3), 3 (Phase 4). `manifest.Fingerprint`
moves on its own in Phase 4. Each phase updates `doc/term-index.md` entries it
turns from "Future design" into code and the documents it names.

A manual check runs the built binary: `make dev`, then `bin/vif` (the checkout's
`wad/` resolves beside `bin/`), `:n <scenario>` to switch scenario,
`:emit <EventName> { payload }` to raise an event, `:god` to survive,
`:log stat <ticks>` to print the status registry to the log. A headless script
is `bin/vif -script <file.toml> -seed <n>`; add `-watch` to see it.

### Phase 0: the engine initialises every system (landed)

Constructors in `internal/system` no longer call `Init`; `App.initWorld` adds
`MetaSystem` beside the generated set and runs `World.InitSystems` once before
`Rand.NextSession`, so the streams and the journal anchor read as before;
`World.CheckSystemDependencies` is folded into `InitSystems`; standalone system
tests wrap a construction in `ready()`. This gives the generic system's `Init`
and the soft-collision matrix a per-run rebuild that is the same pass at
startup and on reset.

### Phase 1: `pkg/machine`

Goal: the kernel of §6, standard library only, proven by tests before any
species touches it.

Steps:

1. `pkg/machine/machine.go`: `Instance`, `Mask`, `MaskOf`, `Cause`,
   `Observer`, `State`, `Cue`, `Target`, `Edge`, `Fallback` (`Retry`, `Hold`),
   `Graph` with `Step`, `Start`, `Stop`, `Flags`, `Name`, `States`. Package doc
   of five lines.
2. `pkg/machine/build.go`: `Builder`, `New`, `State`, `Timeout`, `PreTick`,
   `PostTick`, `Global`, `Suspend`, `Build`, `Must`, and the validation errors
   of §6.4, each naming the state or edge.
3. `pkg/machine/queue.go`: `Queue[T]`, `Item[T]` (§6.6).
4. `doc/package-map.md` §5: a `pkg/machine` row (standard library only, the
   per-entity machine kernel and the deferred queue).

Tests (`machine_test.go`, `queue_test.go`, `fuzz_test.go`), one per rule:

- `Build` rejects each §6.4 condition, naming it.
- A step makes at most four transitions; exit and enter are paired; the flags
  returned are the current state's; `Retry` re-arms without exit or enter and
  reports `CauseRetry`; `Hold` re-evaluates next step; each cue fires once per
  entry, including a cue whose point falls exactly on a tick boundary; global
  edges run before suspension; a suspended step neither advances nor ticks;
  `Start` enters without exit and `Stop` exits without enter.
- `FuzzStep` drives a four-state graph with guard outcomes and dt from a seeded
  `math/rand/v2` PCG over 1,000 steps and asserts the invariants above; a few
  seeds go in the corpus so `go test` runs them.
- `Queue`: items fire in push order whatever their push times, an item fires
  on the first `Tick` that spends its remaining time and never before, `Any` and
  `Clear`, and a JSON round trip.

Gates: `go test ./pkg/machine`, `make arch-check`. Manually testable state: the
tests and `go test -fuzz=FuzzStep -fuzztime=30s ./pkg/machine`; no game change.

### Phase 2: quasar on the machine

Goal: the smallest live species proves the kernel; S7 to S11 close; the
component moves to `GetPtr`.

Steps:

1. `internal/component/quasar.go`: `QuasarState` (`QuasarHoming`,
   `QuasarCharging`, `QuasarZapping`); `QuasarComponent` becomes
   `{Machine machine.Instance[QuasarState]; SpeedMultiplier float64;
   SpeedElapsed time.Duration; IsShielded bool; ZapRadius float64}`.
   `LastSpeedIncreaseAt`, `IsZapping`, `IsCharging` and `ChargeRemaining` go.
2. `internal/system/quasar.go`: a `quasarCtx` field (world, entity, component
   and combat pointers, `stunned`, `inRange`, `dtSec`, the probe's cursor and
   target); the graph of §5 built in `NewQuasarSystem` with `machine.Must`;
   `Update` reads the component through `GetPtr`, runs the lifecycle check
   (`HitPoints <= 0` → `Stop`, `terminateQuasar`), fills the context, calls
   `Step`, then writes `combat.IsEnraged`, `IsShielded`, `Shield.Active` and
   the ward from the flags and runs `strikeContacts`. `startCharging`,
   `startZapping`, `stopZapping` and the detached commits become the hooks.
   `updateKineticMovement` advances `SpeedElapsed` by dt and lifts the
   multiplier each `QuasarSpeedIncreaseTicks * GameUpdateInterval`, carrying
   the remainder. The observer is nil.
3. `internal/render/renderer/quasar.go`: `armed` reads
   `quasar.Machine.State != component.QuasarHoming`.
4. `snapshot.Schema` 26; `make generate`.

Tests (`internal/system/quasar_test.go`, fixture in the style of
`newStalledSwarm`):

- a charge whose zap probe cannot resolve returns to Homing on the same step
  (S7);
- a stun while zapping despawns the lightning and drops the shield (S9);
- the first speed increase lands after twenty movement ticks (S11);
- a cursor re-entering range cancels the splash exactly once (S8).

Gates: `go test ./internal/system ./internal/render/... ./internal/snapshot`;
the four shapes before and after (the fight raises a storm, so the quasar's
shift shows only in a run that spawns one; record the readings in §3 of
[netcode](netcode-design.md) style under this phase).

Manually testable state: `bin/vif -seed 1`, `:god`,
`:emit EventQuasarSpawnRequest { x = 40, y = 15 }`; the quasar homes while the
cursor is inside its circle; leaving it starts the cyan splash, three seconds
later the lightning and the shield; coming back drops both; a pulse stun during
the splash cancels it. A script (`-script`) with that event at tick 100 and 600
ticks runs headless on the old and the new build with one seed and
`-log-stdout -log-stat 50`; the `quasar.*` and `lightning.*` counters of the
last status snapshot agree, and the ticks they move on differ by the accepted
one-tick shifts.

### Phase 3: the dialect kernel

Goal: §7 for the two existing vocabularies, closing the todo item "Refuse
unknown keys and `op` values at load" and giving `-schema` argument fields,
before the third vocabulary is written against it.

Steps:

1. `internal/fsm/dialect/op.go`, `args.go`, `tree.go`, `catalog.go` and
   `dialect_test.go`; package doc of five lines.
2. `internal/fsm/std/guard.go`, `payload.go`, `args.go`, `std.go`: typed
   argument structs for every factory guard (`StateTimeExceeds{ms}`,
   `VarEquals{var, value}`, `VarCompare{var, op, value}`, `VarCompareVar{var_a,
   op, var_b}`, `RegionExists{region}`, the payload, status and config guards),
   decoded through `dialect.Decode`; compound guards through `dialect.Tree`;
   actions through `dialect.Decode`; `ParseIntArg` and `CompareInt` deleted;
   `Ops()` from the kernel; every registration enters the `Catalog`.
3. `internal/bot/mind.go`: `decodePayload`, `unknownKey` on guard args and the
   `ParseIntArg` reads replaced by typed `dialect.Decode`; the catalog gains the
   bot's names.
4. `internal/fsm/loader.go`: state, transition and action tables are checked
   against their config structs' tags before decoding, so `on_entr` fails the
   load naming the state; `internal/resource/scenario.go` `validate` refuses a
   top-level key outside {description, systems, regions, states, macros,
   macro_files, map, min_players, max_players, behaviors}.
5. `internal/manifest/schema.go`: guards and actions become
   `[]{name, kind, args: [{name, type}]}` from the catalogs; `SchemaVersion` 2.
6. `doc/fsm-reference.md` (ops are refused, omitted is `eq`; unknown keys fail),
   `doc/bots.md` if it names the lenient decode, `doc/todo.md` (drop the item,
   and the editor item's prerequisite reads as met).

Tests: `dialect_test.go`, one per rule (unknown op refused, omitted op is eq,
unknown key named with its table path, mistyped value refused, each tree shape,
catalog order deterministic); `std_test.go` and `bot_test.go` keep passing;
`TestABrokenGraphFailsAtLoadNamingTheFault` gains the unknown-key case.

Gates: `go test ./internal/fsm/... ./internal/bot ./internal/manifest
./internal/resource`. Manually testable state: `bin/vif -check` and
`bin/vif -check -s wad/scenario/td` print `scenario ok`; a copy of a scenario
with `op = "ge"` or a misspelt `on_entr` fails `-check` naming the state and
the key; `bin/vif -schema` lists each guard's and action's fields.

### Phase 4: programs, the `Behavior` component and the generic system

Goal: a species defined by files plays in a scenario; `-check` validates it.
Four batches, one PR, in this order.

**4a. The compiler and the vocabulary registry** (`internal/behavior`).

1. `program.go`: `ProgramDoc` (initial, suspend, prototype, states, global) and
   `PrototypeDoc` (§8.1), decoded through `dialect.Decode`; `Prototype` as
   plain data (`component.BehaviorPrototype`, in `internal/component/behavior.go`
   beside the component, so engine and renderer read it without importing
   this package).
2. `vocab.go`: `Vocabulary[C]` with `Predicate[A]`, `Probe[A]`, `Effect[A]`,
   `Sustained[A]` registrations (generic methods, each entering a
   `dialect.Catalog`) and `Flag(name, bit)`.
3. `compile.go`: `Compile[C](v *Vocabulary[C], programs map[string]map[string]any)
   (*Set[C], error)`: sorted names, IDs from `SpeciesDataFirst`, each program to
   a `machine.Graph[component.BehaviorState, C]` through the builder, errors
   naming program, state and key. More than eight programs is an error.
4. Tests: the §8.1 program compiles against a test vocabulary; an unknown
   verb, flag, state or key fails naming it; IDs follow sorted names.

**4b. Scenario include and `-check`.**

1. `internal/fsm/file_loader.go`: `loadAndResolve` follows
   `behaviors.<name>.file` as §8.3 says; `Machine.LoadScenarioFromMap` deletes
   `behaviors` before decoding.
2. `internal/resource/scenario.go`: `Scenario.Behaviors()`.
3. `internal/resource/check.go`: `ValidateScenario` compiles the programs
   against `system.BehaviorVocabulary()`; `checkScenario` prints
   `behaviors ok: <n>`.
4. Tests: a scenario listing a program has it in `ScenarioFiles` and its digest
   moves when the program changes; a program with an unknown verb fails
   `ValidateScenario`.

**4c. The component, the system, the renderer and the species range.**

1. `internal/component/behavior.go`: `BehaviorState uint8`;
   `BehaviorComponent{Program uint8; Machine machine.Instance[BehaviorState];
   PatternIndex int; PatternElapsed time.Duration; TargetX, TargetY int;
   Params []float64}`; `BehaviorPrototype`. `internal/component/species.go`:
   `SpeciesDataFirst`, `SpeciesMax`. `internal/component/header.go`:
   `BehaviorData`.
2. The six tables to `[SpeciesMax]` and the two range checks to `SpeciesMax`
   (§8.5); `profile.Contact` is not read for a data species, loot reads
   `Resources.Behaviors` for drops, soft collision adds the data rows in `Init`.
3. `internal/engine/resource.go`: `Resource.Behaviors []component.BehaviorPrototype`.
4. `internal/manifest/definition.go`: `{"Behavior", "BehaviorComponent",
   "shared"}` in `Components`; `{Name: "behavior", Constructor:
   "NewBehaviorSystem", Domain: "shared", Requires: composite, timer; Optional:
   navigation, combat, lightning, materialize, splash, swarm}` in
   `ContextSystems`; `{"behavior", "NewBehaviorRenderer", "PriorityBehavior"}`
   in `Renderers`. `parameter.PriorityBehavior` after `PriorityKraken`;
   `render.PriorityBehavior` after `PrioritySwarm`. `make generate`.
5. `internal/event/type.go`, `payload.go`: `EventBehaviorSpawnRequest`
   (`BehaviorSpawnRequestPayload{Program string; X, Y int}`) and
   `EventBehaviorCancelRequest` (`{Program string}`), bus class;
   `make generate`.
6. `internal/system/behavior.go`: `BehaviorVocabulary()` (the §8.2 verbs over
   `*behaviorCtx`), `NewBehaviorSystem(world, set)`, `Init` (per-program
   telemetry reset, the RNG stream for `Chance` sites is not drawn: `Chance`
   mixes tick, entity and site), `HandleEvent` (spawn, cancel, breach,
   `SpeciesKilled`, system command), `Update` (lifecycle, `Step`, flags,
   contacts, sweep), spawn building the composite from the prototype as
   `createSwarmComposite` does, `terminate` calling `Stop`.
7. `internal/app/app.go`: `readScenario` compiles (`a.behaviors`), `initWorld`
   sets `Resources.Behaviors` before the systems and adds the system beside
   `MetaSystem`. `internal/system/meta.go`: kill keys for the prototypes.
8. `internal/render/renderer/behavior.go`: `BehaviorRenderer`.
9. `snapshot.Schema` 27; `manifest.SchemaVersion` 3 with a `behavior` catalog
   section in `-schema`.
10. Tests: `domain_test.go` passes with the new file (shared: no player stream,
    no owner-authored writes); `snapshot_declaration_test.go` passes with the
    new system (no private state); a spawned data species steps its graph and
    writes `IsEnraged` from its flags; a cancel request despawns through the
    composite; `-check` of a scenario naming nine programs fails.

**4d. The sample.** `wad/scenario/warden/scenario.toml` with a region that
spawns one warden on entry (`EmitEvent EventBehaviorSpawnRequest { program =
"warden", x = 40, y = 15 }`) and ends on its `EventSpeciesKilled` (species
`10`), and `wad/scenario/warden/behavior/warden.toml` as §8.1. `doc/fsm-reference.md`
gains the program keys; `doc/telemetry-reference.md` the `<program>.*` and
`kills.<program>` rows; `doc/filesystem-layout.md` the `behavior/` directory.

Gates: `go test ./internal/behavior ./internal/system ./internal/manifest
./internal/resource ./internal/fsm/... ./internal/render/... ./internal/app`;
`make generate` is clean; `bin/vif -check -s wad/scenario/warden`. Manually
testable state: `bin/vif`, `:n warden`: the warden roams toward the cursor,
telegraphs with the cyan splash past twelve cells, strikes, and a pulse stun
returns it to roaming; `:emit EventBehaviorCancelRequest { program = "warden" }`
removes it; `-schema` lists the behavior vocabulary with its fields.

### Phase 5: swarm on the machine

Goal: S12 to S14 close; five timers become one instance; stalls count through
the observer.

Steps:

1. `internal/component/swarm.go`: `SwarmComponent{Machine
   machine.Instance[SwarmState]; PatternIndex int; PatternElapsed time.Duration;
   LockedTargetX, LockedTargetY int; ChargeStartX, ChargeStartY, ChargeTargetX,
   ChargeTargetY float64; TeleportStartX, TeleportStartY, TeleportTargetX,
   TeleportTargetY int; ChargesCompleted int}`; the six countdowns go.
2. `internal/system/swarm.go`: `swarmCtx`, the graph of §5 built in
   `NewSwarmSystem`, the observer counting `CauseRetry` and a Lock → Decel
   commit into `statStalls`; `Update` keeps the lifecycle order (HP, charges,
   lifetime, then the step), advances the pattern accumulator when the step was
   not suspended, and runs `breakGoldUnder` and `strikeContacts` after it. The
   `update*State`, `enter*State` and `resetSwarmState` functions become hooks
   and probes; `createSwarmComposite` calls `Start` with `SwarmChargeInterval`.
3. `internal/render/renderer/chargeline.go`, `teleportline.go`: elapsed is
   `swarmComp.Machine.Elapsed`.
4. `swarm_stall_test.go`: the fixtures set `Machine` to an about-to-expire
   state (`Elapsed = Duration - time.Nanosecond`) in place of the nanosecond
   countdowns; the four rules keep passing unchanged.
5. `snapshot.Schema` 28; `make generate`.

Tests: the existing four plus one for the stun edge re-arming the full interval
from every attack state (S14) and one for the wall hit landing in Decel on the
step it happened (S12).

Gates: `go test ./internal/system ./internal/render/...`; the four shapes
before and after (blue circles spawn swarms, so the fight exercises this).
Manually testable state: `bin/vif -seed 1`, `:god`,
`:emit EventSwarmSpawnRequest { x = 40, y = 15 }`: chase for five seconds, the
charge line pulses twice over two seconds, the charge or a teleport beam, the
brief stop, chase again; a stun during the lock sends it back to chase at once;
`:log stat 50` shows `swarm.transition_stalls` only when the lock finds no
target.

### Phase 6: storm circles and the deferred queue

Goal: S2 to S6 close; storm and fuse share `machine.Queue`.

Steps:

1. `internal/component/storm.go`: `StormCircleState` (`StormCircleCooldown`,
   `StormCircleActive`); `StormCircleComponent` keeps `Pos3D`, `Vel3D`, `Index`,
   `IsInvulnerable`, `InvulnerableElapsed`, `AttackTargetX`/`Y` (JSON names
   kept), gains `Machine machine.Instance[StormCircleState]` and
   `LandingFound bool`, loses `AttackState`, `CooldownRemaining`,
   `AttackRemaining`, `AttackProgress` and the Idle value.
2. `internal/system/storm.go`: `circleVariants [StormCircleCount]{initial,
   attack, repeat time.Duration; cue uint16}` replaces `getInitialCooldown`,
   `getAttackDuration` and `getRepeatCooldown`; three graphs built in
   `NewStormSystem` from the rows with shared hooks over `circleCtx`; the blue
   enter hook draws, then searches, and sets `LandingFound`; the 800 ‰ cue
   materializes and pushes on `s.pending machine.Queue[pendingBlueSpawn]`;
   `updateCircleAttacks` senses cursor and stun, steps, then writes
   `Mount.Armed` and `IsInvulnerable` from the flags; `processPendingBlueSpawns`
   becomes `s.pending.Tick(dt, fire)`; `hasPendingBlueSpawn` and the sentinel
   go; `stormSnapshot.Pending` is the queue; `createCircleHeader` calls `Start`
   with the row's initial duration.
3. `internal/system/fuse.go`: `fusions` becomes `machine.Queue[pendingFusion]`;
   `Update` is `Tick`; `hasQuasarFusion` is `Any`; `SaveLocal`/`LoadLocal`
   carry the queue.
4. `internal/render/renderer/storm.go`: `AttackState == Active` reads the
   machine; the three `AttackProgress` reads become `Machine.Progress()` gated
   on Active (the muzzle flash's `<= 0` skip becomes a state test).
5. `storm_attack_test.go`: the red fixture sets `Machine` to Cooldown about to
   expire; `TestStormDrawsBeforeItReadsLivePositions` keeps passing.
6. `snapshot.Schema` 29; `event.JournalSchema` 40; `make generate`.

Tests: a stunned Active circle keeps its remaining attack time and `Armed`
reads false (S5, S4); a circle with no cursor keeps `IsInvulnerable` current
(S4); the blue cue fires once per Active entry with no landing and with one
(S2); a storm's termination drops its queue (S1); the fuse queue fires two
same-tick fusions in push order.

Gates: `go test ./internal/system ./internal/render/... ./internal/app`; the
four shapes before and after. Manually testable state: `bin/vif`, `:n td`
(or wait for the storm region of the default scenario), `:god`: the green
circle pulses one second on, one off; the red turret fires for two seconds and
rests one; the blue circle glows for two and a half seconds, the materialize
opens at two and a swarm follows; a pulse stun on a convex circle pauses its
attack and the red turret stops firing until the stun ends.

### Phase 7: close-out

- `doc/ecs-and-events.md` §7 (species phase timers now live in
  `machine.Instance`), `doc/term-index.md` (every "Future design" entry of this
  document reads as code), `doc/README.md` (this document is the subsystem
  reference), `doc/telemetry-reference.md`, `doc/package-map.md`.
- This document: §2 and §4 shrink to what landed; §9 keeps the readings and the
  version moves.
- `doc/todo.md`: the editor's prerequisite is met; a deferred gap found in a
  phase goes there in the file's pattern.
- The user runs the full `go test ./...` sweep.

## 10. Rejected approaches

- **O0, status quo with conventions.** Fixes neither the non-local invariants
  (S7, S9) nor the data path.
- **O3, state as a function.** Func values are neither comparable nor
  encodable, so they break capture and digest; persisting them needs a name
  table.
- **O5, bytecode VM with blackboard.** Needs a verifier, every interpreted float
  operation under the conversion discipline, and a copied-in blackboard goes
  stale mid-step. The `td` scenario shows compare guards, `And`/`Or` and typed
  actions suffice.
- **O6, BT or StateTree selection as a runtime.** Bosses are timed cycles;
  ordered global edges already give priority preemption.
- **O7, a tag component per state.** Each state would cost a manifest edit, a
  store, a snapshot section, a mask bit and a fingerprint change.
- **O8, a central `MachineSystem`.** Breaks per-entity ordering (lifecycle →
  step → physics → interactions).
- **O9, per-entity `internal/fsm` instances.** Event-triggered and map-backed,
  with regions, variables and delayed actions. Reuse its dialect, not its
  runtime.
- **O10, utility AI or GOAP.** Opaque tuning; overkill for attack cycles.
- **Manifest DI of machines, intent arrays as step output, data programs in the
  build fingerprint, a species-ID range above `SpeciesCount` without resizing
  the tables, widening the component mask for data species.** Each rejected for
  the reasons in §2 and §8.5.

## 11. Prior art

Doom `info.c` (a state table of sprite, tics, native action and next state,
made data by DeHackEd and DECORATE while actions stayed native: the O1 + O4
shape); Overwatch Statescript (shared graph data, replicated per-entity
instance state); Unreal StateTree (an immutable asset with per-entity instance
data and priority enter conditions); RimWorld ThinkTrees, the StarCraft II data
editor and Factorio prototypes (data composing a fixed native vocabulary under
lockstep, which `ScenarioDigest` provides here); Quake `think`/`nextthink`
(function fields persisted by name, the tax O3 would impose).
