# Behavior machines

Working design for per-entity state machines in species systems and for the
data-driven species they prepare. The next iteration turns §9 into a phased
plan; this document then becomes the subsystem's reference as phases land.

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

## 2. Verification of the prior review

**Confirmed against the code.**

- Swarm has a five-state enum with five countdown fields, one live per state.
  Stun resets it to Chase on the first stunned tick and then freezes it. The two
  wedge fixes (a failed lock re-arms `SwarmTransitionRetryInterval`, a failed
  charge falls to Decel) are pinned by `swarm_stall_test.go`.
- Quasar encodes three legal states in three bools. A failing `startZapping`
  returns before its commit, and that is unreachable only because
  `isCursorInZapRange` returns true on the same failures. `SplashTimerCancel`
  and the lightning despawn are each emitted from two sites.
- Storm circles run Idle/Cooldown/Active through three `switch circleType`
  tables. A missing cursor freezes their timers, physics honours stun while
  attacks do not, the root's `pendingBlueSpawns` is a D-19 carrier with relative
  timers, and spawn Z-jitter is drawn before validation (D-8).
- Commit styles differ: swarm and storm circles use `GetPtr`; quasar and the
  storm root use a detached `GetComponent` plus `SetComponent`.
- Telemetry binds in constructors, and the status registry closes at `Freeze`.
  The tick is 50 ms and boss counts are O(1–10).
- Go 1.27 adds generic methods (gated at `go1_27` in `types2`). A generic method
  cannot satisfy an interface method, and interface methods still take no type
  parameters.
- A stdlib-only runtime belongs under `pkg/` (precedent `pkg/linkpace`), and
  `make arch-check` reports a `pkg/` package importing `internal/`.

**Corrected.**

1. **Fingerprint.** `manifest.Fingerprint` hashes the manifest (component
   field/type/domain, systems, snapshot carriers), not code. Code-defined graphs
   ride on nothing, like any other system code. Data programs belong to the
   session half of `network.PeerIdentity`: shipped as scenario files, they enter
   `ScenarioDigest`, travel with the scenario to joiners and obey its size
   bounds. No new identity field is needed (§8.3).
2. **One dialect, not two.** `internal/bot` is an `fsm.Machine[*mind]` that
   calls `std.Register` and adds its own vocabulary. There is one dialect with
   two vocabularies, and the behavior front-end becomes the third (§7).
3. **Event domain.** The catalog class already routes FSM emits
   (`manifest/fsm_bridge.go`: Local goes to `PushLocal`, everything else to
   `PushEvent`). Species effects also use `PushCrossing` gated by
   `SimulatesLocally`. Authors therefore never name a domain: each verb binds its
   own routing, and a generic `EmitEvent` verb accepts only classes whose routing
   is mechanical.
4. **RNG.** D-8 already prescribes the remedy for per-entity conditional draws:
   seed from (tick, entity) through `vmath.Mix64`, as mount spread and soft
   collision do. A `Chance` predicate draws that way, so guard short-circuiting
   cannot move any stream. A "fixed draws per step" rule is not needed.
5. **Relative timers.** D-19 states that absolute instants are sound, because
   `SimEpoch` and the tick interval are session identity. The instance uses a
   relative `Elapsed` anyway: suspension has to freeze a timer, and an instant
   cannot express that without being rewritten.
6. **dt cap.** The scheduler stamps `DeltaTime = tickInterval` on every tick, so
   `min(dt, MaxSimulationDeltaSeconds)` (0.1 s) never binds. Every duration is a
   tick multiple.
7. **Go features.** Inferring type arguments when a generic function is assigned
   to a function-typed variable is Go 1.21, not 1.27. The code-pointer argument
   is unnecessary: func values are neither comparable nor encodable, which on its
   own rules out O3.
8. **Moore outputs stay materialized.** Combat reads `QuasarComponent.IsShielded`
   and `StormCircleComponent.IsInvulnerable` for stun immunity
   (`applyStunEffect`). Stun itself clears `IsEnraged` and zeroes velocity. The
   host writes the flag-derived fields on every step, and the components keep
   them.
9. **Renderers.** `chargeline.go` and `teleportline.go` recompute elapsed time as
   `parameter.X - Remaining`. The storm renderer reads `AttackState` and
   `AttackProgress`, and the quasar renderer reads `IsCharging || IsZapping`.
   `Elapsed`, `Duration` and `Progress()` serve all three without the renderers
   knowing any parameter.
10. **Capture schema, not fingerprint.** Replacing component fields changes the
    capture JSON, so it needs a `snapshot.Schema` bump. The fingerprint stays
    the same because store names and types do not change.

## 3. Current machines

| System | Encoding | Timers | Preemption | Fallible entries | Moore outputs | Commit |
|---|---|---|---|---|---|---|
| Swarm | enum ×5 | 5 countdowns, pattern cycle | stun → Chase, then freeze | Lock, Charge, Teleport | `IsEnraged`; `ChargesCompleted++` on Decel entry | `GetPtr` |
| Quasar | 3 bools | `ChargeRemaining`; speed scaling on an instant | stun cancels Charging; in range cancels both | Zapping | `IsEnraged`, `IsShielded`, `Shield.Active`, sealed immunity | detached |
| Storm circle | enum ×3 | cooldown/attack countdowns; `InvulnerableElapsed` | none; no cursor freezes | none (Active waits for convex) | `IsInvulnerable`, `Mount.Armed`, `AttackProgress`, Z clamp | `GetPtr` |
| Storm root | liveness aggregation | `pendingBlueSpawns` | — | — | — | detached |

Snake, drain, eye, gateway, fuse, pylon, tower and materialize have no
behavioral machine (§5).

## 4. Timer and state inconsistencies

Each item gives the finding, then the proposal. Items marked **Decision** need
a call before the plan is written.

- **S1. Storm: the blue spawn queue outlived its storm (fixed in this change).**
  `Update` returns while `rootEntity == 0`, and `terminateStorm` left
  `pendingBlueSpawns` intact. A spawn queued in a storm's last second therefore
  froze and fired into the next storm at a stale cell. `terminateStorm` now drops
  the queue, which matches the existing "not processed after death" gate.
  **Decision:** keep the drop, or flush instead (spawn the owed swarm after
  death). Flushing would mean processing the queue outside the root gate.
- **S2. Storm: the blue one-shot rests on parameter arithmetic.** The
  materialize fires once only because its dedup entry (1 s,
  `MaterializeAnimationDuration`) outlives the attack's tail (0.5 s of 2.5 s); a
  longer tail would materialize twice. `AttackTargetX != 0` works as "no target"
  only because `SwarmHeaderOffsetX = 1`. Proposal: a cue at 80 % of Active,
  which fires once per entry by construction (§6.3), plus a stored "landing
  found" result from the entry hook. That drops the dedup scan and the sentinel.
- **S3. Storm: outputs lag the state by one tick.** `Mount.Armed` and
  `IsInvulnerable` are written before the state switch. The red turret arms one
  tick after Active begins and stays armed one tick into Cooldown; a concave
  circle stays vulnerable one tick after its attack. Burst length is unchanged.
  Proposal: write flags after the step, which is the machine default.
  **Decision:** accept the one-tick shift.
- **S4. Storm: the no-cursor gate also skips outputs.** With no cursor, an
  Active red circle keeps `Mount.Armed = true` and a stale `IsInvulnerable`.
  Proposal: make no-cursor the suspend predicate, so timers freeze while the host
  still writes flags every step. **Decision:** whether suspension masks
  offensive flags (proposed: Armed is off while suspended).
- **S5. Storm: stun freezes physics but not attacks.** A stunned, frozen circle
  keeps pulsing green, firing red and spawning blue, while swarm and quasar
  suspend under stun. **Decision:** (a) suspend attack timers under stun like the
  other bosses (proposed), (b) preempt Active to Cooldown with the repeat
  duration, or (c) keep. Stun lands only while a circle is convex or attacking,
  so (a) and (b) both change play.
- **S6. Storm: three switch tables for one topology.** `getInitialCooldown`,
  `getAttackDuration` and `getRepeatCooldown` all switch on circle type, and
  green uses `StormGreenRepeatInterval` for both attack and repeat. Proposal:
  one `[StormCircleCount]` variant row {initial, attack, repeat, cue}, following
  the per-kind table rule. Idle disappears: spawn enters Cooldown with the
  initial duration, and Active's timeout re-enters it with the repeat duration.
- **S7. Quasar: Charging → Zapping totality is non-local** (§2). Proposal:
  Charging timeout → [Zapping (probe: position and cursor) | Homing], with an
  unconditional commit.
- **S8. Quasar: exit effects are duplicated.** Proposal: Charging's exit hook
  cancels the splash; Zapping's exit hook despawns the lightning and drops the
  shield; termination calls `Graph.Stop`.
- **S9. Quasar: the stun branch ignores Zapping.** It relies on combat refusing
  to stun a shielded quasar, which is a cross-system invariant. Proposal: a
  global stun edge from {Charging, Zapping} to Homing. Its Zapping half never
  fires today, but correctness no longer depends on combat.
- **S10. Quasar: entry ticks are inconsistent.** Entering Charging skips homing
  for that tick, and entering Zapping deals no damage until the next tick, while
  leaving Zapping homes on the same tick. Run-to-completion runs the resulting
  state's tick uniformly, which adds one homing tick and one zap damage tick
  per cycle. **Decision:** accept, or keep parity per §6.3.
- **S11. Quasar: speed scaling starts at once.** `LastSpeedIncreaseAt` starts at
  the zero instant, so the first movement tick lifts `SpeedMultiplier` from 1.0
  to 1.1. Scaling advances only on movement ticks and never catches up after a
  zap. **Decision:** stamp the spawn time (or use a carried accumulator, §6.6),
  or keep it as tuned.
- **S12. Swarm: transition ticks skip integration.** Charge timeout → Decel,
  Decel timeout → Chase, and Chase → Lock all return without integrating, while
  the wall-hit path integrates first. Under the machine, timeouts fire before the
  tick, so the resulting state's tick runs in the same step: Decel's first drag
  tick lands one tick earlier. The wall hit stays a post-tick edge.
  **Decision:** accept.
- **S13. Swarm: five timers with one live.** `resetSwarmState` zeroes three of
  the four attack timers but not `TeleportRemaining`. That is harmless only
  because every entry re-arms its own timer. One instance timer removes the
  whole class.
- **S14. Swarm: stun asymmetry by state is intended.** Stun in Chase freezes the
  interval; stun anywhere else cancels the attack and restarts the full
  interval. This becomes a global edge from {Lock, Charge, Teleport, Decel} to
  Chase, followed by suspension.
- **S15. Timer idioms diverge across species.**
  - Countdown `Remaining`: swarm, quasar, storm, eye frames, the fuse and storm
    queues, and `TimerComponent`.
  - Count-up with carry: gateway.
  - Count-up with reset: storm `InvulnerableElapsed`.
  - Absolute instants: quasar speed scaling, drain energy drain.
  - Absolute ticks: drain spawn schedule and backoff.

  Periodic cycles drop the remainder on reset (swarm pattern, eye frames), while
  gateway carries it. With a fixed dt and tick-multiple durations, nothing
  drifts today. Proposal: phases move to `Instance`, and cycles move to a carried
  accumulator as they are touched. Drain's absolute ticks stay: they are
  player-domain and never captured.
- **S16. Three hand-rolled spawn queues.** Fuse (`fusions`), storm
  (`pendingBlueSpawns`) and drain (`pendingSpawns`) each defer a spawn behind a
  materialize. Drain completes on `EventMaterializeComplete`. Fuse and storm run
  their own timer of the same length; fuse does this deliberately, because its
  beam is local presentation and its spawn is a shared crossing. Fuse and storm
  swap-remove while iterating in reverse, so same-tick emission order depends on
  slice history (deterministic, but fragile). The storm's clock agrees with the
  materialize's float progress only because 20 × 0.05 rounds up past 1.0.
  **Decision:** whether fuse and storm share a small deferred queue (relative
  timers, stable order) in `pkg/machine`. It is not a machine.

## 5. Target graphs

**Swarm** (`C = *swarmCtx`):

| State | Duration | Flags | Probe / enter | Tick | Edges |
|---|---|---|---|---|---|
| Chase | 5 s interval | — | — | home, integrate | timeout → [Lock], else `Retry(200 ms)` |
| Lock | 2 s | Enraged | probe: base target; enter: store target, zero velocity | — | timeout → [Charge (guard: LOS) \| Teleport \| Decel] |
| Charge | 0.8 s | Enraged | probe: kinetic; enter: start, target, velocity | steer, integrate; records wall hit | timeout → Decel; post-tick wall hit → Decel |
| Teleport | 0.4 s | Enraged | probe: free landing; enter: from/to, zero velocity | — | timeout → Decel, edge action: clear area, relocate |
| Decel | 0.1 s | Enraged | enter: `ChargesCompleted++` | drag, integrate | timeout → Chase |

- Global edge: stunned, from {Lock, Charge, Teleport, Decel} → Chase. Suspend:
  stunned.
- The pattern cycle advances only when the step was not suspended.
- HP, the charge limit, cancel and breach stay as lifecycle checks ahead of the
  step.
- `transition_stalls` counts `Retry` through the observer.
- Relocation is an edge action rather than an exit hook, because a stun during
  Teleport must not move the swarm.

**Quasar** (`C = *quasarCtx`):

| State | Duration | Flags | Probe / enter / exit | Tick | Edges |
|---|---|---|---|---|---|
| Homing | — | — | — | home (speed scaling inside) | pre-tick: not in range → Charging |
| Charging | 3 s | Enraged | enter: splash request; exit: splash cancel | home | timeout → [Zapping \| Homing] |
| Zapping | — | Enraged, Shielded | probe: position, cursor; enter: lightning; exit: despawn | retarget, zap damage | — |

- Global edges, in priority order: stunned, from {Charging, Zapping} → Homing;
  in range, from {Charging, Zapping} → Homing.
- Suspend: stunned.
- From the flags, the host writes `IsEnraged`, `IsShielded` and `Shield.Active`,
  and seals immunity.
- The component moves to `GetPtr`: the detached copy existed only to order
  commits that the step now makes unconditional.

**Storm circle** (`C = *circleCtx`, one graph per variant row):

| State | Duration | Flags | Hooks | Edges |
|---|---|---|---|---|
| Cooldown | row.initial at spawn, row.repeat after Active | — | — | timeout → [Active (guard: convex)], else `Hold` |
| Active | row.attack | Attacking (+ Armed on red) | enter: blue landing search (one shared draw); tick: variant pulse | timeout → Cooldown (row.repeat) |

- Blue cue at 80 %: if a landing was found, materialize and queue the spawn.
- Suspend: no cursor, plus stun if S5 is decided as (a).
- The host derives `IsInvulnerable = concave && !Attacking` and `Mount.Armed`
  from the flags, and the physics Z clamp reads Attacking.
- `AttackProgress` gives way to `Instance.Progress()`.

**No adoption.**

- Snake: its shield is body liveness, and spawning is a counter.
- Drain: enrage is an HP-threshold level flag, and population control is a
  player-domain queue.
- Eye: a frame cycle and GA-driven motion.
- Gateway: a periodic emitter with rate acceleration.
- Fuse: a spawn queue (S16).
- Pylon and tower: stationary, with no timers.
- Materialize: a progress animation.
- Storm root: liveness aggregation.

## 6. Runtime (`pkg/machine`, stdlib only)

### 6.1 Instance

```go
type Instance[S ~uint8] struct {
	State    S             `json:"state"`
	Elapsed  time.Duration `json:"elapsed"`
	Duration time.Duration `json:"duration"` // 0 = untimed
}
func (in *Instance[S]) Progress() float64 // Elapsed/Duration clamped to [0,1]; 0 when untimed
func (in *Instance[S]) Expired() bool
```

`Duration` is stored because entries override it (storm initial or repeat,
`Retry`). There is no entry counter: cues make one-shots structural, and RNG
seeds from (tick, entity).

### 6.2 Graph

```go
type State[C any] struct {
	Name     string
	Duration time.Duration // default for entries that set none
	Flags    uint64
	Probe    func(C) bool // fallible entry, may stash results in C; nil = infallible
	Enter, Exit, Tick func(C)
	Cues     []Cue[C]     // {AtPermille uint16, Do func(C)}; timed states only
}
type Edge[S ~uint8, C any] struct {
	From     Mask           // global edges only; bit per state
	Guard    func(C) bool   // nil = always
	Do       func(C)        // runs between exit and enter
	To       []Target[S, C] // candidates {State, Guard, Duration}, tried in order
	Fallback Fallback       // Retry(d) or Hold; required when every candidate can fail
}
```

A builder (`machine.New[S, C](observer)`) takes states, timeout, pre-tick,
post-tick and global edges, and a suspend predicate. `Build` validates the
graph and returns an immutable `*Graph[S, C]`. A code graph that fails `Build`
panics in its constructor, as a broken manifest would; a data graph's failure
is a load error. Graphs are limited to 64 states, because `Mask` is a
`uint64`.

### 6.3 Step

```go
func (g *Graph[S, C]) Step(in *Instance[S], c C, dt time.Duration) (flags uint64, suspended bool)
func (g *Graph[S, C]) Start(in *Instance[S], c C, s S, d time.Duration) // spawn: enter without exit
func (g *Graph[S, C]) Stop(in *Instance[S], c C)                         // death: exit without enter
```

A step runs these phases in order:

1. **Global edges**, in declaration order (priority). The first whose `From`
   holds the current state and whose guard passes takes its transition.
2. **Suspend.** If the predicate holds, return the current flags with
   `suspended` set. There is no advance, cue or tick.
3. **Advance** `Elapsed` by dt. Fire every cue whose point falls in
   (old, new], computed in integer nanoseconds.
4. **Timeout**, if the state is timed and has expired.
5. **Pre-tick edges**, in declaration order.
6. **Tick** of the current state.
7. **Post-tick edges.** These read outcomes the tick left in `C`, such as the
   swarm's wall hit.
8. **Return** the current state's flags.

Global edges run before suspension so that stun can cancel an attack and then
freeze the result. Each phase takes at most one transition, so a step makes at
most four.

A transition tries its candidates in order. For each one, it checks the guard
and then the probe. On the first success it runs `Exit(current)`, then `Do`,
then `Enter(target)`, commits `{State, Elapsed: 0, Duration}`, and reports
`(from, to, cause)` to the observer. Exit therefore runs only when a transition
commits, and probes keep fallible work out of hooks that cannot fail.

If no candidate commits, the fallback applies:

- `Retry(d)` re-arms the current state in place, with no exit or enter, and
  reports a stall.
- `Hold` leaves the state expired, so the edge is evaluated again on the next
  step.

The resulting state's tick runs in the step that entered it. Where the current
code skips that tick (S10, S12), the plan decides per edge. A tick that must
skip its entry step tests `Elapsed == 0`.

### 6.4 Validation

`Build` rejects a graph when any of the following holds:

- a target or start state is undefined;
- a candidate chain can fail entirely (every candidate has a guard or probe) and
  declares no fallback;
- a `Retry` has a duration of zero or less;
- a cue or timeout sits on an untimed state;
- a cue point lies outside (0, 1000];
- a global edge has an empty `From`.

The validator turns the swarm comments ("every failed entry must land in a state
that moves") into structure. A wedge also needs a skipped tick, and the step
never skips one.

### 6.5 Host contract

- **Context.** Each species has one context struct, held as a system field and
  refilled per entity: world, entity, component pointers, observations sensed
  once before the step (stunned, in range, has cursor), probe results and tick
  outcomes. Passing a pointer to a field of the long-lived system allocates
  nothing per call.
- **Mutation.** The runtime mutates only the `Instance`; hooks reach the world
  through `C`, and no store is touched by the machine. `GetPtr` hosts pass the
  live instance, and detached hosts commit after the step.
- **Lifecycle.** HP, limits, cancel and breach stay ahead of the step and call
  `Stop`, so death paths run exit effects such as the lightning despawn.
- **Flags.** Flags map to component fields on every step, suspended or not
  (which fixes S4).
- **Observer.** `func(from, to S, cause Cause)` is bound at construction. It
  feeds status counters, the debug overlay and journal annotations without the
  package importing `status`.
- **Introspection.** `States()` and `Edges()` return `iter.Seq` values for the
  flow/graph overlay and the schema dump.

### 6.6 Generics and sibling primitives

- **Typed IDs.** `S ~uint8` IDs index per-state slices, so typed graphs reject
  a foreign enum at compile time.
- **Context type.** `C` is a pointer type, and hooks are func fields rather than
  constraint methods. Instantiations that share a GC shape therefore pay no
  dictionary dispatch.
- **Go 1.27 generic methods.** They tidy registration
  (`func (v *Vocabulary[C]) Guard[A any](name string, build func(A) (func(C) bool, error))`)
  and comparisons (`Op.Holds[T cmp.Ordered]`). Because a generic method cannot
  satisfy an interface method, both stay concrete types.
- **Sibling primitives** (pending the S11, S15 and S16 decisions, each added
  only with two users):
  - a carried accumulator for cycles (swarm pattern, eye frames, quasar speed);
  - a deferred queue for fuse and storm.

## 7. Shared dialect kernel (fsm, bot, behavior)

**Today:**

- Guard factories take `map[string]any`.
- `std.ParseIntArg` returns 0 for a missing or mistyped key.
- `std.CompareInt(current, op string, value)` switches on the op string at
  every evaluation and treats an unknown op as `eq`.
- `std.decodePayload` decodes non-strictly, while bot's `decodePayload`
  hand-lists the allowed keys.
- The `-schema` dump lists guard and action names plus `std.Ops()`, but no
  argument fields.

**Kernel**, one implementation used by `fsm/std`, `bot` and the behavior
compiler:

1. **`Op`.** Parsed once at load; `ParseOp` rejects unknown names, and
   `Op.Holds[T cmp.Ordered](a, b T)` evaluates it. This replaces the per-tick
   string switch and the silent `eq`.
2. **`Args[A]`.** A strict typed decode: keys are checked against A's `toml`
   tags (cached per type), then `toml.Decode` runs. It replaces `ParseIntArg`,
   bot's allowed-key lists and std's lenient decode. The same field list feeds
   the schema, so the map editor learns every guard's and action's arguments.
3. **Guard trees.** The `{ name, args }` / `guards = [...]` shape of
   `And`/`Or`/`Not` is parsed once, generically over the guard type:
   `Tree[G any](raw any, leaf func(string, map[string]any) (G, error), all, anyOf func([]G) G, not func(G) G)`.
   Each runtime keeps its own guard signature; `fsm.GuardFunc[T]` sees region
   and payload, a behavior guard only `C`.
4. **`Catalog`.** Name → {kind, args type} in deterministic order, which is the
   source of `-schema` output for every vocabulary.

The runtimes are not shared (O9 below).

**Placement (TBD):**

- `internal/fsm/dialect` sits beside its first user, and every consumer is
  internal. This is the default.
- `pkg/dialect` is a stdlib-plus-TOML leaf like `pkg/genetic/persistence`. It
  makes sense if the behavior spec compiler moves to `pkg/machine/spec` with
  only the game vocabulary left in `internal/behavior`.

## 8. Data-driven behavior (O4)

### 8.1 Program shape

The program uses the fsm dialect's own keys:

```toml
# behavior/warden.toml, listed by the scenario entry
initial = "Roam"
suspend = { guard = "Stunned" }

[states.Roam]
on_update = [{ action = "Home", payload = { profile = "quasar" } }]
transitions = [{ trigger = "Tick", guard = "TargetDistance", guard_args = { op = "gt", cells = 12 }, target = "Wind" }]

[states.Wind]
duration_ms = 1500
flags = ["Enraged"]
on_enter = [{ action = "Telegraph", payload = { color = "cyan" } }]
on_exit = [{ action = "CancelTelegraph" }]
cues = [{ at = 800, action = "Materialize", payload = { spawn = "swarm" } }]
transitions = [{ trigger = "Timeout", target = ["Strike", "Roam"] }]

[[global]]
from = ["Wind", "Strike"]
guard = "Stunned"
target = "Roam"
```

How the keys map onto the runtime:

- `trigger` is one of `Timeout`, `Tick` (pre-tick) or `AfterTick` (post-tick).
- `target` is a name or a candidate list. `fallback` is `{ retry_ms = n }` or
  `"hold"`.
- `flags` resolve against the vocabulary's flag table.
- State IDs follow sorted names, because map-decoded tables carry no order.
  Edge order is array order.

### 8.2 Vocabulary (`internal/behavior`)

| Tier | Examples | Domain binding |
|---|---|---|
| Predicates | `TargetDistance{op, cells}`, `LineOfSight`, `HPFraction{op, value}`, `Stunned`, `HasTarget`, `Chance{percent}` | read-only; `Chance` seeds from (tick, entity, site) |
| Probes | `ResolveTarget`, `FindLanding{w, h}` | read-only; results land in `C` |
| Effects (enter, exit, cue, edge) | `StoreTarget`, `ZeroVelocity`, `Teleport`, `Telegraph`, `Lightning`, `Materialize`, `SpawnSpecies`, `EmitEvent` | each verb fixes its push (presentation local, shared requests shared, cursor damage local behind `SimulatesLocally`); `EmitEvent` takes only Shared or Local classes |
| Sustained (tick) | `Home{profile}`, `ChargeToStored`, `Drag{factor}`, `Freeze` | wraps the in-place kinetic helpers of [generic-kinetic.md §8](generic-kinetic.md) |

Contact interactions (shield drain, heat), lifecycle and spawning stay in the
species scaffold, not in the vocabulary.

### 8.3 Delivery and identity

- **Programs are scenario files.** The entry lists them (for example
  `[behaviors] warden = { file = "behavior/warden.toml" }`). `fsm.ScenarioFiles`
  follows that table as it follows region files and strips it before the
  director decodes. `ScenarioDigest`, the size bounds and the transfer to joiners
  then cover programs with no new mechanism.
- **Compiled once per run.** The scenario resolves before services and systems
  are built, and the compiled programs are delivered as a world resource. A
  scenario change already rebuilds the run.
- **`-check` compiles every program,** so an unknown name or bad argument fails
  validation, not play.

### 8.4 Determinism

Determinism rests on four rules:

- edges run in array order;
- no map is iterated at run time;
- `Chance` uses no stream (§2, correction 4);
- verbs are native code under the existing float discipline, so nothing is
  interpreted.

### 8.5 Beyond the machine

A data species needs a prototype before behavior matters. The machine is a
minority of the work.

- **Prototype.** It covers footprint and offsets, protection masks, combat
  stats, homing profile, navigation dimensions, interaction profile, lifetime
  and cycle limit, spawn validation and renderer binding.
- **Species IDs.** `component.SpeciesType` is a code enum, so data species take
  a fixed range above `SpeciesCount`, assigned in scenario order and covered by
  the digest.
- **Telemetry.** It is bound at construction from the compiled programs, before
  `Freeze`.
- **Generic species system.** It runs every data species and owns one manifest
  component, `Behavior` {program ID, instance, parameters}. That is a single
  fingerprint change.
- **Genetics.** Programs may declare bounded numeric parameters as genes, as
  `td_main` already registers species with gene bounds.

## 9. Work outline for the phased plan

1. **`pkg/machine`.** Build the kernel, builder, validator and step. Property
   tests over random guard outcomes and dt traces check that:
   - exit and enter are paired;
   - flags match the current state;
   - a step makes at most four transitions;
   - `Retry` re-arms;
   - each cue fires once per entry.
2. **Quasar** (S7–S11). It is the smallest graph and fixes the non-local
   invariant. The component moves to `GetPtr`, the renderer reads the state, and
   the capture schema is bumped.
3. **Swarm** (S12–S14). Five timers become one instance. `chargeline` and
   `teleportline` read `Elapsed`, and stalls count through the observer.
4. **Storm circles** (S2–S6), after the S4 and S5 decisions. The variant row,
   cues and post-step flags land here.
5. **Dialect kernel** (§7). `fsm/std` and bot guards move to `Op`, `Args` and
   guard trees, and the schema gains argument fields.
6. **Behavior vocabulary.** Add the vocabulary, the program compiler, the
   scenario include and `-check`.
7. **Generic species system.** Add the system, the `Behavior` component and the
   prototype schema.

**Verification.** Scripted runs (`vif -script`) on the old and new code compare
transition sequences and spawn/death events. Exact digests differ by the
accepted one-tick shifts. `swarm_stall_test` and `storm_attack_test` keep
passing.

## 10. Rejected approaches

- **O0, status quo with conventions.** It fixes neither non-local invariants
  (S7, S9) nor the data path.
- **O3, state as a function.** Func values are neither comparable nor
  encodable, so they break capture and digest. Persisting them needs a name
  table, Quake's progs tax.
- **O5, bytecode VM with blackboard.** It needs a verifier, every interpreted
  float operation must follow the conversion discipline, and a copied-in
  blackboard goes stale mid-step. The `td` scenario shows that compare guards,
  `And`/`Or` and typed actions suffice.
- **O6, BT or StateTree selection as a runtime.** Bosses are timed cycles.
  Ordered global edges already give priority preemption, and a selector can
  become a front-end feature later.
- **O7, a tag component per state.** Each state would cost a manifest edit, a
  store, a snapshot section and a fingerprint change, so states could not come
  from data.
- **O8, a central `MachineSystem`.** It breaks per-entity ordering (lifecycle →
  step → physics → interactions) and needs peer references to bind species
  vocabularies.
- **O9, per-entity `internal/fsm` instances.** That runtime is event-triggered
  and map-backed, with regions, variables and delayed actions. Reuse its
  dialect, not its runtime.
- **O10, utility AI or GOAP.** Tuning is opaque and tie-break determinism needs
  auditing; it is overkill for attack cycles.
- **Manifest DI of machines.** Construction is uniform and most systems have no
  machine. Graphs are immutable per-species data, not services.
- **Intent arrays as step output.** They allocate and go stale. Hooks act
  through the context directly.
- **Data programs in the build fingerprint.** The fingerprint is compile-time
  manifest identity, while programs are session data covered by
  `ScenarioDigest`.

## 11. Prior art

- **Doom `info.c`.** Each mobj indexes a state table whose rows hold a sprite,
  tics, a native action and the next state. DeHackEd and DECORATE made that table
  data while actions stayed native, which is the O1 + O4 shape.
- **Overwatch Statescript.** Graph data is shared, and instance state is per
  entity and replicated: the same graph/instance split as here.
- **Unreal StateTree.** An immutable asset carries per-entity instance data, and
  priority enter conditions select states (the later selector option).
- **RimWorld ThinkTrees, the StarCraft II data editor and Factorio prototypes.**
  Data composes a fixed native vocabulary, and lockstep needs identical data on
  every peer, which `ScenarioDigest` provides here.
- **Quake `think`/`nextthink`.** Function fields are persisted by name, which is
  the tax O3 would impose.
