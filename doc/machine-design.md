# Behavior machines

The design and the phased plan for per-entity state machines in species systems
and for the data-driven species they prepare. §1 to §8 are the design, verified
against the code at the plan's date; §9 is the plan, one phase per task, branch and
PR, each with the files it touches, the tests it adds and the state a person can
check by hand. Phases 0 to 6 have landed: §6 describes `pkg/machine` as built,
§5 the swarm's, the quasar's and the storm circles' graphs as they run, §7 the
dialect kernel and §8 the data path as built. As phases land, this document
becomes the subsystem's reference and §9 shrinks to what each phase moved.

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

- Swarm (`swarm.go`, before Phase 5) had a five-state enum with five countdown
  fields, one live per state. Stun reset it to Chase on the first stunned tick
  (`resetSwarmState`) and then froze it. The two wedge fixes (a failed lock
  re-arms `SwarmTransitionRetryInterval`, a failed charge falls to Decel) are
  pinned by `swarm_stall_test.go`, which Phase 5 kept.
- Quasar (`quasar.go`, before Phase 2) encoded three legal states in three
  bools. A failing `startZapping` returned before its commit, and that was
  unreachable only because `isCursorInZapRange` returns true on the same
  failures. `SplashTimerCancel` and the lightning despawn were each emitted from
  two sites.
- Storm circles (`storm.go`, before Phase 6) ran Idle/Cooldown/Active through
  three `switch circleType` tables. A missing cursor skipped the attack step,
  physics honoured stun (`updateCirclePhysics` reads the header's `StatusStun`)
  while attacks did not, the root's `pendingBlueSpawns` was a D-19 carrier
  (`stormSnapshot`, `SaveShared`) with relative timers, and the blue landing
  draw precedes validation (D-8, pinned by `storm_attack_test.go`, which Phase 6
  kept).
- Commit styles differ: swarm, storm circles and, since Phase 2, the quasar use
  `GetPtr`; the storm root uses a detached `GetComponent` plus `SetComponent`.
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
5. **Relative timers.** D-19 admits no absolute instant in a component, and the
   instance's relative `Elapsed` is what lets suspension freeze a timer.
6. **Moore outputs stay materialized.** Combat reads
   `QuasarComponent.IsShielded`, `StormCircleComponent.IsInvulnerable` and, since
   Phase 4, `BehaviorComponent.Shielded` for stun immunity (`applyStunEffect`).
   The host writes the flag-derived fields on every step, and the components
   keep them.
7. **Renderers.** `renderer/chargeline.go` and `teleportline.go` recomputed
   elapsed time as `parameter.X - Remaining` and read `Machine.Elapsed` and
   `Progress()` since Phase 5; `renderer/storm.go` read `AttackState` and
   `AttackProgress` and reads `Machine.State` and `Progress()` since Phase 6;
   `renderer/quasar.go` read `IsCharging || IsZapping` and
   reads `Machine.State` since Phase 2. `Instance.Elapsed`, `Duration` and
   `Progress()` serve all three.
8. **Capture schema, not fingerprint.** Replacing component fields changes the
   columnar capture (`snapshot.Schema`, 25 at the plan's date), so each phase
   that changes a store bumps it. The fingerprint moves only when
   `definition.go` changes, which Phase 4 did by adding a component and a
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

The swarm's, the quasar's and the storm's rows are their code before Phases 5, 2
and 6, which put the swarm, the quasar and the storm circles on the machines of
§5; the root's pending list went in Phase 6, its swarms riding their beams (S16).
Snake, drain, eye, gateway, fuse, pylon, tower, kraken and materialize have no
behavioral machine (§5).

## 4. Timer and state inconsistencies

Each item gives the finding, the proposal and the decision taken for the plan.

- **S1. Storm: the blue spawn queue outlived its storm.** Landed:
  `terminateStorm` dropped the queue. Decided: keep the drop, no flush. Since
  Phase 6 the pending swarm is its beam, and `terminateStorm` ends the storm's
  beams in flight.
- **S2. Storm: the blue one-shot rests on parameter arithmetic.** The
  materialize fires once only because its dedup entry (`hasPendingBlueSpawn`,
  1 s) outlives the attack's tail (0.5 s of 2.5 s); `AttackTargetX != 0` works
  as "no target" only because `SwarmHeaderOffsetX = 1`. Decided: a cue at 80 %
  of Active, which fires once per entry by construction (§6.3), plus a stored
  `LandingFound` from the entry hook. That drops the dedup scan and the
  sentinel. Landed in Phase 6.
- **S3. Storm: outputs lag the state by one tick.** `Mount.Armed` and
  `IsInvulnerable` are written before the state switch. Decided: write flags
  after the step (the machine default); the one-tick shift is accepted. Landed
  in Phase 6: the red's mount fires in the step that opens its burst.
- **S4. Storm: the no-cursor gate also skips outputs.** With no cursor an
  Active red circle keeps `Mount.Armed = true` and a stale `IsInvulnerable`.
  Decided: no-cursor is a suspend condition; timers freeze, flags are written
  every step, and `Armed` reads false while suspended. Landed in Phase 6.
- **S5. Storm: stun freezes physics but not attacks.** Decided: stun is the
  other suspend condition, so a stunned circle pauses its attack as swarm and
  quasar pause theirs. Stun lands only on a convex or attacking circle, so this
  changes play: a stunned burst or pulse stops until the stun ends. Landed in
  Phase 6, whose manual check watched a red burst held across ten stuns.
- **S6. Storm: three switch tables for one topology.** Decided: one
  `[StormCircleCount]` variant row {initial, attack, repeat, cue}, per the
  per-kind table rule. Idle disappears: spawn enters Cooldown with the initial
  duration, and Active's timeout re-enters it with the repeat duration. Landed
  in Phase 6 as `circleVariants`, whose rows also name Active's flags and hooks.
- **S7. Quasar: Charging → Zapping totality is non-local** (§2). Decided:
  Charging timeout → [Zapping (probe: position and cursor) | Homing], with an
  unconditional commit. Landed in Phase 2.
- **S8. Quasar: exit effects are duplicated.** Decided: Charging's exit hook
  cancels the splash; Zapping's exit hook despawns the lightning and drops the
  shield; termination calls `Graph.Stop`. Landed in Phase 2, the shield dropping
  with the Shielded flag rather than in the exit (§5).
- **S9. Quasar: the stun branch ignores Zapping.** It relies on combat refusing
  to stun a shielded quasar. Decided: a global stun edge from {Charging,
  Zapping} to Homing; its Zapping half never fires today, but correctness no
  longer depends on combat. Landed in Phase 2.
- **S10. Quasar: entry ticks are inconsistent.** Entering Charging skips homing
  for that tick, and entering Zapping deals no damage until the next tick, while
  leaving Zapping homes on the same tick. Decided: run-to-completion runs the
  resulting state's tick uniformly, adding one homing tick and one zap damage
  tick per cycle; accepted. Landed in Phase 2.
- **S11. Quasar: speed scaling starts at once.** `LastSpeedIncreaseAt` starts
  at the zero instant, so the first movement tick lifts `SpeedMultiplier` from
  1.0 to 1.1. Decided: a carried accumulator (`SpeedElapsed`) counting movement
  time from spawn, so the first increase lands after `QuasarSpeedIncreaseTicks`
  (20 ticks, 1 s) as the parameter name says. Landed in Phase 2.
- **S12. Swarm: transition ticks skip integration.** Charge timeout → Decel,
  Decel timeout → Chase and Chase → Lock all return without integrating, while
  the wall-hit path integrates first. Decided: timeouts fire before the tick, so
  the resulting state's tick runs in the same step and Decel's first drag tick
  lands one tick earlier; accepted. The wall hit stays a post-tick edge. Landed
  in Phase 5, where the charge's first steer and the chase's first homing tick
  move a tick earlier too.
- **S13. Swarm: five timers with one live.** `resetSwarmState` zeroes three of
  the four attack timers but not `TeleportRemaining`, harmless only because every
  entry re-arms its own timer. One instance timer removes the class. Landed in
  Phase 5.
- **S14. Swarm: stun asymmetry by state is intended.** Stun in Chase freezes
  the interval; stun anywhere else cancels the attack and restarts the full
  interval. This becomes a global edge from {Lock, Charge, Teleport, Decel} to
  Chase carrying `SwarmChargeInterval`, followed by suspension. Landed in
  Phase 5.
- **S15. Timer idioms diverge across species.** Countdown `Remaining` (swarm,
  quasar, storm, eye frames, the fuse and storm queues); lifetimes and one-shot
  deadlines on `TimerComponent` slots (the swarm's lifetime included); count-up
  with carry (gateway); count-up with reset (storm `InvulnerableElapsed`);
  absolute instants (quasar speed); absolute ticks (drain spawn schedule).
  Decided: phases move to `Instance`; the swarm pattern and the quasar speed
  move to a carried accumulator in their phases; eye frames, gateway and drain
  stay as they are. The quasar's phases and speed landed in Phase 2, the swarm's
  phases and pattern in Phase 5, the storm circles' phases in Phase 6, where the
  fuse's and the storm's queues became their beams (S16).
- **S16. Three hand-rolled spawn queues.** Fuse (`fusions`, a `LocalStateSaver`
  carried by a journal's opening), storm (`pendingBlueSpawns`, a
  `SharedStateSaver`) and drain (`pendingSpawns`) each deferred a spawn behind a
  materialize. Fuse and storm ran timers of their own beside the beam and
  swap-removed while iterating in reverse, so same-tick emission order depended
  on slice history. The storm's timer was the beam's length; the fuse's were
  0.5 s for a drain pair, whose spirits flew into no beam unless one was asked
  for, and 0.6 s for a quasar, which landed while its 1 s beam still receded.
  Drain completes on `EventMaterializeComplete`, not a timer. Decided first: one
  `machine.Queue[T]` (relative timers, firing in push order, any number of
  entries, JSON-encodable), built in Phase 1 for storm and fuse. Decided in
  Phase 6 instead, with the user: a beam names the species its completion lands,
  as a data species' beam has since Phase 4, so the beam is the pending spawn and
  its timer, its visual and the spawn end on one tick. Storm and fuse dropped
  their lists, the queue went with no user left, and drain keeps its
  event-completed queue. Landed in Phase 6.

## 5. Target graphs

**Swarm** (`C = *swarmCtx`), as built in `internal/system/swarm.go`:

- States, every one but Chase flagged Enraged:
  - Chase, 5 s: tick homes and integrates; timeout → [Lock], else
    `Retry(200 ms)`, the chase moving meanwhile.
  - Lock, 2 s: probe a base target; enter stores it and zeroes the velocity;
    timeout → [Charge (guard: line of sight, probe: a kinetic) | Teleport
    (probe: a position and a landing) | Decel].
  - Charge, 0.8 s: enter stores start and target and aims to arrive as the
    entry's `Duration` ends; tick steers over what is left of it, integrates and
    notes a wall hit; timeout → Decel; post-tick wall hit → Decel.
  - Teleport, 0.4 s: enter marks the beam's two ends and zeroes the velocity;
    timeout → Decel, the edge's action clearing the landing and relocating.
  - Decel, 0.1 s: enter counts `ChargesCompleted`; tick scales the velocity by
    `SwarmDecelDrag`, a per-tick factor, and integrates; timeout → Chase (5 s).
- Global edge: stunned, from {Lock, Charge, Teleport, Decel} → Chase with the
  full interval. Suspend: stunned.
- The pattern accumulator advances ahead of the step unless the swarm is
  stunned, the suspend's own condition.
- Hit points, the charge limit and the lifetime are checked ahead of the step;
  they, a cancel and a breach end the swarm through `despawnSwarm`, which calls
  `Stop`.
- After the step the host writes `Combat.IsEnraged` from the flags and, unless
  the step suspended, breaks the gold under the swarm and strikes what it
  touches; a stunned swarm still counts in `swarm.count`.
- The observer, a method value bound in `NewSwarmSystem`, counts `CauseRetry`
  and a Lock → Decel commit in `swarm.transition_stalls`: a lock with no target,
  and one that can neither charge nor teleport.
- Relocation is an edge action rather than an exit hook, because a stun during
  Teleport must not move the swarm.
- The context holds the entity, the component, combat and kinetic pointers, the
  sensed `stunned`, the capped dt in seconds, the lock probe's target cell, the
  teleport probe's two header cells and the charge's wall hit; the hooks are
  `SwarmSystem` methods. `Start` runs once the composite is built, before
  `EventSpeciesCreated`.

**Quasar** (`C = *quasarCtx`), as built in `internal/system/quasar.go`:

| State | Duration | Flags | Probe / enter / exit | Tick | Edges |
|---|---|---|---|---|---|
| Homing | — | — | — | home (speed accumulator inside) | pre-tick: not in range → Charging |
| Charging | 3 s | Enraged | enter: splash request for the entry's `Duration`; exit: splash cancel | home | timeout → [Zapping \| Homing] |
| Zapping | — | Enraged, Shielded | probe: position, cursor; enter: lightning; exit: lightning despawn | retarget, zap damage | — |

- Global edges, in priority order: stunned, from {Charging, Zapping} → Homing;
  in range, from {Charging, Zapping} → Homing. Suspend: stunned.
- From the flags, the host writes `Combat.IsEnraged`, `IsShielded` and
  `Shield.Active`, and refreshes the ward status while shielded. The shield is
  a flag, so it drops with the state in the step that leaves Zapping and no
  exit hook writes it.
- The host strikes the cursors the quasar touches after the step, unless the
  step suspended: a stunned quasar strikes nothing.
- A header move sweeps the new footprint (`processCollisionsAtNewPositions`)
  and leaves a destruction flash per glyph, drawn under the body; the glyph
  itself dies in the tick's closing settle. A glyph can stand under a member
  only through the one-tick lag between the header's move and the composite
  sync, which the next move's sweep covers.
- The component is read through `GetPtr`: the detached copy existed only to
  order commits that the step now makes unconditional.
- The context holds the entity, the component, the sensed `stunned` and
  `inRange` (true with no cursor, so a missing cursor never charges), dt and
  its capped seconds, and the probe's origin and target; the hooks are
  `QuasarSystem` methods, which reach the world through the system.
- `Start` runs at spawn once the composite is built, and `terminateQuasar` calls
  `Stop`, so a death ends the splash or the lightning through the same exit a
  transition does.

**Storm circle** (`C = *circleCtx`, one graph per colour), as built in
`internal/system/storm.go`:

- `circleVariants`, indexed by circle type, holds one row per colour: the
  spawn's cooldown, the attack, the cooldown after one, Active's flags beside
  Attacking, its enter and tick hooks, and its cue. `NewStormSystem` builds the
  three graphs from the rows once (`machine.Must`).
  - Green, 1 s, 1 s, 1 s: tick `pulse` strikes the cursors inside its area.
  - Red, 1 s, 2 s, 1 s, flagged Armed: tick `burst` counts; the armed turret
    mount fires after the storm system.
  - Blue, 5 s, 2.5 s, 5 s: enter `findLanding` draws the direction, then
    searches and stores `LandingFound` (D-8); tick `glow` counts; the cue at
    `StormBlueMaterializePermille` (800 ‰), `beamIn`, beams the swarm onto the
    landing if one was found.
- States:
  - Cooldown, the row's repeat by default: timeout → [Active (guard: convex)],
    else `Hold`, so an expired cooldown waits for the convex.
  - Active, the row's attack, flagged Attacking: timeout → Cooldown.
- `createCircleHeader` calls `Start` into Cooldown with the row's initial
  cooldown once the circle is built, so Idle is gone.
- Suspend: no cursor or stunned (the circle header's `StatusStun`).
- After every step, suspended or not, the host writes
  `IsInvulnerable = concave && !Attacking` and `Mount.Armed = Armed &&
  !suspended`. The physics Z clamp and the renderer read `Machine.State`, the
  green pulse and the blue glow `Machine.Progress()`, and the red's muzzle flash
  the armed mount, so a held burst shows none while a held pulse or glow keeps
  its progress.
- The blue's swarm rides its beam: the beam is Shared and names the swarm, and
  the storm lands it on the beam's `EventMaterializeComplete`, its header on the
  beam's footprint. `terminateStorm` ends the storm's beams in flight (S1).
- The context holds the entity, the component, the circle's cell and the sensed
  `hasCursor` and `stunned`; the hooks are `StormSystem` methods. No state has
  an exit hook and the graphs take no observer.

**No adoption.** Snake (shield is body liveness, spawning a counter), drain
(HP-threshold enrage, player-domain population queue), eye (frame cycle and
GA-driven motion), gateway (periodic emitter), fuse (its fusions ride their
beams, S16), pylon and tower (stationary), kraken, materialize (a progress
animation), storm root (liveness aggregation).

## 6. Runtime (`pkg/machine`, stdlib only)

### 6.1 Instance

```go
type Instance[S ~uint8] struct {
	State    S             `json:"state"`
	Elapsed  time.Duration `json:"elapsed"`
	Duration time.Duration `json:"duration"` // 0 = untimed
}
func (in Instance[S]) Progress() float64 // Elapsed/Duration clamped to [0,1]; 0 when untimed
func (in Instance[S]) Expired() bool     // timed and Elapsed >= Duration
```

`Duration` is stored because entries override it (storm initial or repeat,
`Retry`). There is no entry counter: cues make one-shots structural, and RNG
seeds from (tick, entity). The reads take a value, so a renderer calls them on a
component copy.

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
`PostTick(from, edge)` lists, ordered `Global(edge)` and one `Suspend(pred)`,
each returning the builder so a graph declares as one chain. `Build` validates
and returns an immutable `*Graph[S, C]`, with `Flags`, `Name` and `States` as its
reads; `Must` wraps it for code graphs, which panic in their constructor as a
broken manifest would, while a data graph's failure is a load error. Graphs are
limited to 64 states because `Mask` is a `uint64`. A nil observer is allowed.

### 6.3 Step

```go
func (g *Graph[S, C]) Step(in *Instance[S], c C, dt time.Duration) (flags uint64, suspended bool)
func (g *Graph[S, C]) Start(in *Instance[S], c C, s S, d time.Duration) // spawn: enter without exit or probe
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
success it runs `Exit(current)`, then `Do`, commits `{State, Elapsed: 0,
Duration}`, runs `Enter(target)` and reports `(from, to, cause)`.

Each hook sees its own entry: `Exit` and `Do` read the entry being left, and
`Enter` reads the entry being started, its `Duration` included, so an enter hook
sees a candidate's override. Quasar's Charging enter sizes the splash telegraph
from it and swarm's Charge enter derives its velocity from the charge time.
`Start` commits before `Enter` the same way. Probes run before `Exit`, so `Enter`
cannot fail and nothing is half-committed: exit runs only when a transition
commits, and fallible work stays out of the hooks that cannot fail.

If no candidate commits: `Retry(d)` re-arms the current state in place (no exit
or enter) and reports `CauseRetry` with `from == to`, and the new period runs the
state's cues again; `Hold` changes nothing, so an expired state tries its timeout
again next step. In a phase, the first edge whose source holds the state and
whose guard passes owns the phase: when its chain fails, its fallback applies and
later edges wait for the next step.

The resulting state's tick runs in the step that entered it (S10, S12 accepted).
A global edge acts at the start of the step, so the state it enters advances by
dt in that step; the later phases act at its end, so a state they enter ticks at
`Elapsed == 0`, which a tick that must skip its entry step tests.

### 6.4 Validation

`Build` rejects a graph when a target or start state is undefined; a candidate
chain can fail entirely (every candidate has a guard or probe) and declares no
fallback; a `Retry` duration is zero or less; a cue or timeout sits on an untimed
state; a cue point lies outside (0, 1000]; a global edge has an empty `From`; a
state declares two timeout edges. It also rejects a state outside 0..63 or
defined twice, a `From` on a non-global edge, a negative duration and a second
suspend predicate, and reports every fault at once, each naming its state or edge
(`timeout edge 1 from state "Chase": every candidate can fail and no fallback is
declared`). The validator turns the swarm comments ("every failed entry must land
in a state that moves") into structure: a wedge needs a skipped tick, and the
step never skips one.

### 6.5 Host contract

- **Context.** Each species has one context struct, held as a system field and
  refilled per entity: the entity, component pointers, observations sensed once
  before the step (stunned, in range, has cursor), probe results and tick
  outcomes, and the world unless the hooks are the system's own methods (the
  quasar's and the swarm's are, so their contexts hold no world). Passing a
  pointer to a field of the long-lived system allocates nothing per call.
- **Mutation.** The runtime mutates only the `Instance`; hooks reach the world
  through `C` or their system, and no store is touched by the machine. Hosts
  hold the instance through `GetPtr`.
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
- **No deferred queue.** `machine.Queue[T]`, values each behind a relative
  timer, was decided for S16 and built in Phase 1 for storm and fuse; Phase 6
  deleted it unused. A spawn deferred behind a beam rides the beam, which names
  the species its completion lands, so nothing waits beside it (S16).
- **Carried accumulator.** A `time.Duration` field advanced by dt and reduced
  by its period, carrying the remainder: the swarm pattern
  (`SwarmComponent.PatternElapsed`, advanced while not stunned) and the quasar
  speed (`QuasarComponent.SpeedElapsed`, advanced by movement ticks). Two
  fields, no type.

## 7. Shared dialect kernel (fsm, bot, behavior)

`internal/fsm/dialect` is what the scenario machine, bot graphs and behavior
programs share of their TOML. Names resolve once, at load; only `Op.Holds` runs
per evaluation.

1. **`Op`.** An args struct's `dialect.Op` field takes `eq`, `neq`, `gt`, `gte`,
   `lt` or `lte`; any other value fails the load naming its key, and an omitted
   op is `eq`. `Op.Holds[T cmp.Ordered](a, b T)` evaluates it, and `Ops()` lists
   the six for `-schema`.
2. **`Decode[A](raw)`, `DecodeInto(raw, v)`.** A strict typed decode of an
   argument table: `toml.Unmapped` names every key no field takes by its path
   (`unknown keys "inner.y", "vr"`), the `Op` fields are parsed by name, and
   `toml.Decode` refuses a mistyped value by its path (`within: cannot convert
   string to int`). The caller prefixes the place (`guard 'HasTarget': `,
   `action 'Intent': payload: `, `[bot]: `). `Fields[A]` lists A's keys with
   their kinds: string, int, float, bool, op, array, table or any.
3. **`Tree[G]`.** `And` and `Or` over `guards = [{ name, args }]` and `Not` over
   `guard = { name, args }`, nested freely, each node decoded strictly. It is
   generic over the guard type, so each runtime supplies its leaf builders and its
   own `All`, `Any` and `Not`: `fsm.GuardFunc[T]` sees region and payload, a
   behavior guard only `C`. A fault names each compound it is in
   (`guard 'And': guards[1]: guard 'VarCompareVar': op: ...`).
4. **`Entry`.** `{name, kind, keys, args}`; `Sort` orders a vocabulary by kind,
   then name, and `TreeEntries` are `And`, `Not` and `Or`.

`fsm.Machine` registers a name with the struct its arguments decode into, through
generic methods: `RegisterGuard[A]`; `RegisterAction[A, R]` for an action that
reads only `payload`, compiled to `R` at load (`fsm.Payload[A]` keeps the decoded
payload as it is); `RegisterKeyedAction[R]` for one that reads keys of its own
table (`EmitEvent`: `event`, `payload`, `payload_vars`; the region actions:
`region`, `initial_state`); and `RefuseAction`, a name whose use fails the load
and which the vocabulary leaves out. The loader refuses a key of an action's
table the action does not read (`action 'Text' takes no payload_vars`) and a key
no config struct takes under `[systems]`, a region or a state, its transitions
and actions included (`state 'A': unknown key "on_entr"`); a top-level key is its
embedder's to check, `resource.entryKeys` for a scenario entry and `bot` for a
graph. `Machine.Vocabulary` lists a machine's entries; `-schema`
(`SchemaVersion` 2) prints a scenario's and a bot graph's under `vocabularies`.

The runtimes are not shared (O9).

## 8. Data-driven behavior (O4)

### 8.1 Program shape

The program uses the fsm dialect's own keys, with the machine's additions
(`duration_ms`, `flags`, `probe`, `cues`, `suspend`, `[[global]]`, a candidate
list as `target`, `fallback`, the `Timeout` and `AfterTick` triggers) and a
`[prototype]` table. The sample as shipped; the
[FSM reference](fsm-reference.md#behavior-programs) lists every key and verb:

```toml
# wad/scenario/warden/behavior/warden.toml, listed by the scenario entry
initial = "Roam"
suspend = { guard = "Stunned" }

[prototype]
width = 4
height = 2
header = { x = 1, y = 0 }           # phantom head offset from the top-left
hp = 40
combat = "swarm"                    # the attack-matrix row it fights as
homing = "swarm"                    # profile.Homing, by name
mass = 2.0                          # soft collision and flocking, as the swarm's
drops = "swarm"                     # rolls the swarm's drop tiers
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
on_update = [{ action = "Freeze" }]
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

- `trigger` is `Timeout`, `Tick` (pre-tick) or `AfterTick` (post-tick). A
  `Timeout` takes no guard, as the machine takes timeouts unguarded; its
  candidates carry the guards.
- `target` is a name or a candidate list; a candidate may be
  `{ state = "Strike", guard = "LineOfSight", duration_ms = 600 }`. `fallback`
  is `{ retry_ms = n }` or `"hold"`.
- `flags` resolve against the vocabulary's flag table (`Enraged`, `Shielded`).
- State IDs follow sorted names, because map-decoded tables carry no order.
  Edge order is array order.
- A state's tick is its sustained verbs, then the host's `Integrate`; every
  entry ends with the host's `Entered`, which drops the cell the probe resolved,
  so a probe serves the entry it guarded and no later one.
- A verb out of place (an effect in `on_update`, a sustained verb on an enter,
  exit, cue or edge) or an unknown name fails the compile naming program, place
  and key, every fault joined; the builder's own checks (§6.4) then apply.

### 8.2 Vocabulary (`internal/behavior`)

The registry is generic (`behavior.Vocabulary[C]`). Its generic methods register a
name with the struct its args decode into, strictly and once at load:
`Predicate[A]`, `Seeded[A]` (a guard whose build also takes its site, numbering
the program's guards and probes in load order), `Probe[A]`, `Effect[A]`,
`Sustained[A]`, `Emit` (an effect reading `event` beside `payload`) and
`Flag(name, bit)`, with the host's `Integrate` and `Entered` hooks. `Entries`
lists them for `-schema`'s `vocabularies.behavior`. The game's verbs bind in
`internal/system/behavior.go` to `behaviorCtx`, refilled per instance; the target
is `resolveBaseTarget`'s, the nearest cursor.

- Predicates (read-only): `TargetDistance{op, cells}` (rows scaled by the cell
  aspect), `LineOfSight`, `HPFraction{op, value}`, `Stunned`, `HasTarget`,
  `HitWall` (this step's move met a wall or the map edge), `Chance{percent}`
  (mixed from tick, entity and site), `Param{index, op, value}` (the `Genotype`
  genes).
- Probes (read-only, results land in `C`): `ResolveTarget`
  (`resolveBaseTarget`), `FindLanding` (`FindFreeAreaSpiral` over the footprint).
- Effects (enter, exit, cue, edge): `StoreTarget`, `ZeroVelocity`, `Teleport`
  (clearing its landing with the destruction flash), `Telegraph{color}` and
  `CancelTelegraph` (the splash request pair), `Lightning{color}` (the cursor's
  owner takes the contact damage), `Materialize{spawn}` (`swarm` or `nugget`
  beams, which spawn nothing; a drain beam's completion spawns a drain),
  `SpawnSpecies{species, x, y}` (swarm or quasar), `EmitEvent{event, payload}`.
  Each verb fixes its push (presentation local, shared requests shared, cursor
  damage local behind `SimulatesLocally`); `EmitEvent` takes only Shared or
  Local classes.
- Sustained (tick): `Home`, `ChargeToStored`, `Drag{rate}` (linear drag per
  second, so it holds at any tick rate), `Freeze` (no move this tick), each
  wrapping the in-place helpers of `pkg/vmath/physics` the species use
  ([generic kinetic §8](generic-kinetic.md#8-feasible-pseudo-system-design)).
- Integrate (the tick's tail unless `Freeze` ran): `IntegrateWithBounceStats`
  over the footprint with `parameter.BehaviorRestitution` (0.5, the swarm's),
  recording `HitWall` and the bounce rows; the members follow, and moving sweeps
  nothing, as the swarm's does not.

Contact interactions (`strikeContacts` with the prototype's damage), lifecycle
and spawning stay in the generic system's scaffold, not in the vocabulary.

### 8.3 Delivery and identity

- **Programs are scenario files.** The entry lists them
  (`[behaviors] warden = { file = "behavior/warden.toml" }`) or holds one inline.
  The include reads, parses and expands each file against the entry's macros
  through the helper `loadAndResolve` uses for region files, records it in
  `visited` and replaces the `{ file }` table; anything else under `behaviors`
  fails naming it. `ScenarioFiles` therefore lists programs, so
  `resource.ReadScenario` (`internal/resource/scenario.go`) carries them, digests
  them and transfers them with no new mechanism; `validScenarioName` admits the
  subdirectory. `Machine.LoadScenarioFromMap` is unchanged, a top-level key being
  its embedder's, and `resource.entryKeys` admits `behaviors`.
  `Scenario.Behaviors()` resolves the entry and returns the programs by name.
- **Compiled once per run.** `App.readScenario` compiles the programs
  (`system.CompileBehaviors`, against `system.BehaviorVocabulary()`) right after
  `resource.LoadScenario`, so a bad program fails like a bad scenario, before any
  service starts. The prototypes go into `world.Resources.Behaviors` in
  `initWorld` before the systems are built, which lets loot, soft collision and
  `NewMetaSystem` size their data rows and the renderers read glyphs; the
  compiled graphs go to `system.NewBehaviorSystem(world, set)`, which the app
  adds beside `MetaSystem` (`manifest.ContextSystems`), because the generator's
  constructors take only a world.
- **`-check` compiles every program:** `resource.ValidateScenario` runs the same
  compile, so an unknown name or bad argument fails validation, not play, and a
  mid-run scenario change refuses a bad program before anything is torn down.
  `-check` prints `behaviors ok: <n> (<names>)`.

### 8.4 Determinism

Edges run in array order; no map is iterated at run time; `Chance` uses no
stream (§2, correction 4); verbs are native code under the existing float
discipline, so nothing is interpreted; programs are sorted by name before IDs
are assigned, and the digest covers the names.

### 8.5 Beyond the machine

A data species needs a prototype before behavior matters. The machine is a
minority of the work.

- **Prototype** (`component.BehaviorPrototype`, plain data in
  `Resources.Behaviors`). Footprint (at most 8×8) and header offset, hit points,
  the combat row it fights as (`swarm` or `quasar`, the unit composites whose
  damage lands on their header), homing profile (`profile.Homing`, by name),
  soft-collision mass, the code species whose drop tiers it rolls, contact
  damage, lifetime, glyph frames and colour, pattern period. It names existing
  rows of the per-kind tables that are code; a new attack profile, materialize
  style or drop table stays a code change.
- **Species IDs.** `component.SpeciesType` keeps its code enum ending at
  `SpeciesCount`; `SpeciesDataFirst = SpeciesCount` and
  `SpeciesMax = SpeciesCount + parameter.BehaviorProgramsMax` (8) bound the data
  range. Programs take `SpeciesDataFirst + i` in sorted-name order; a scenario
  with more than eight fails to compile. A name is `[a-z][a-z0-9_]*` of at most
  24 bytes, neither a code species' name nor `total` or `uncredited`, because it
  keys `kills.<name>` and `behavior.<name>.*`.
- **Species tables.** Every table indexed by `SpeciesType` is `[SpeciesMax]`, so
  a data ID never indexes out of range: `profile.Contact`, `profile.SoftKraken`,
  `profile.Drops`, the soft-collision matrices and caches, and `MetaSystem`'s
  kill counters and capture; the unread `SpeciesDimensionsLUT` is gone. The
  package-level rows for data species stay zero, and a data species' values come
  from its prototype at the reader: the behavior system passes the contact
  damage, loot rolls the named species' tiers under a pity of its own, the
  health bar measures the prototype, and soft collision builds data rows in
  `Init` for a prototype with mass (to and from swarm, quasar and every other
  data species; storm, pylon and kraken push it one way), so every code pair
  keeps its rule. The range checks in `meta.go`, `genetic.go` and `loot.go` read
  `>= SpeciesMax`.
- **Composite routing.** `component.Behavior` holds `BehaviorData` where the
  unused `BehaviorBoss` stood; the behavior system handles
  `EventCompositeIntegrityBreach` for it. `handleEmptyComposite` needs no case:
  its default destroys the head.
- **Events.** `EventBehaviorSpawnRequest{program, x, y}` (bus and barrier-bound,
  like the swarm's) and `EventBehaviorCancelRequest{program}` (shared, as every
  species' cancel; an empty program cancels every data species) are what a
  scenario emits; a program no entry defines is counted
  (`behavior.unknown_program_rejects`) and dropped. A spawn is immediate, as the
  swarm's and quasar's are, and `EventFuseBehaviorRequest{program, x, y}`
  (shared) beams one in: the fuse starts a swarm beam over the place the
  footprint fits, the beam's `Species` naming the data species, and sends the
  spawn when the beam completes. The pending spawn is that Shared entity, which a
  capture carries with the stores; a cancel ends its program's beams in flight.
  Genetics stays `EventGeneticRegisterSpecies` with the
  data species' ID; the genes land in the `Genotype` component as every species'
  do, sampled once the spawn has its place, and `Param` reads them.
- **Telemetry.** `behavior.*`, and per program `behavior.<program>.count` and
  the lifecycle rows (`newLifecycleTelemetry`), bind in `NewBehaviorSystem`,
  `kills.<program>` in `NewMetaSystem`, all before `Freeze`
  ([telemetry](telemetry-reference.md)).
- **Renderer.** `BehaviorRenderer` (`renderer/behavior.go`,
  `render.PriorityBehavior` after `PrioritySwarm`) draws each `Behavior`
  header's members from the prototype's frame at `PatternIndex`: hit flash, then
  enraged, then the prototype's colour. The hit-flash pulse is one function,
  `hitFlashColor`, where five renderers held copies.
- **Generic species system.** `internal/system/behavior.go` runs every data
  species: hit points, lifetime, the pattern cycle (held by a stun), `Step`, the
  flags written back (`Enraged` to `Combat.IsEnraged`; `Shielded` to a refreshed
  ward and `Behavior.Shielded`, which `applyStunEffect` reads as it reads the
  quasar's shield), then, unless suspended, gold breaking and contacts.
  `terminate` runs the state's exit (`Stop`) before the composite goes, so a
  telegraph never outlives its instance. It owns one manifest component,
  `Behavior` {program index, instance, pattern frame and accumulator, stored
  target, `Shielded`}; with the context system it is the fingerprint's change.
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
- A spawn deferred behind a beam rides the beam, which replaced the planned
  `machine.Queue[T]` when Phase 6 was taken up (S16); S1's drop stays.
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
`snapshot.Schema` 25 → 26 (Phase 2), 27 (the cursor clocks between Phases 2 and 3),
28 (Phase 4), 29 (Phase 5), 30 (Phase 6);
`event.JournalSchema` 39 → 40 (Phase 4, the beam's `Species` in the materialize
payloads), 41 (Phase 6: an opening without the fuse's record, and a beam's
`Species` naming any species);
`manifest.SchemaVersion` 1 → 2 (Phase 3), 3 (Phase 4). `manifest.Fingerprint`
moves on its own in Phase 4, and in Phase 6 with the storm's optional
`materialize`. Each phase updates `doc/term-index.md` entries it
turns from "Future design" into code and the documents it names.

A manual check runs the built binary: `make dev`, then `bin/vif` (the checkout's
`wad/` resolves beside `bin/`), `:n <scenario>` to switch scenario,
`:emit <EventName> { payload }` to raise an event, `:god` to survive,
`:log stat <ticks>` to print the status registry to the log. A headless script
is `bin/vif -script <file.toml> -seed <n>`; add `-watch` to see it.

Rules the first phases taught, binding on the rest:

- The implementing agent runs the phase's manual check itself before the PR is
  marked ready, in the terminal where it can and as a headless probe where it
  cannot, and the PR body says what was seen. Phase 2's tests, benchmarks and
  harness all passed while the plough-through flash a player sees was found at
  the keyboard.
- A mechanical edit across many files is proven by a count, before and after,
  taken with a search that reads the pattern as the file holds it (`grep -P`
  for `\t`); Phase 0's constructor rewrite shipped with none of its 55 edits
  because its pattern matched no file and its check used the same pattern.
- A test that builds systems outside the app initialises them through the
  engine's pass (`World.InitSystems` on the set, `ready()` on one), never by
  calling a constructor and using the result.
- A species host that clears its footprint names the effect it leaves:
  `sweep.emit(w, EventFlashSpawnOneRequest)` draws each dead glyph's rune for
  `DestructionFlashDuration` (500 ms); the particle layer sits under the species
  since Phase 2's review, so the body covers it. Swarm and storm move without
  sweeping and leave nothing; a program's `Teleport` or `ChargeToStored` verb
  that clears cells says which.

### Phase 0: the engine initialises every system (landed)

Constructors in `internal/system` no longer call `Init`; `App.initWorld` adds
`MetaSystem` beside the generated set and runs `World.InitSystems` once before
`Rand.NextSession`, so the streams and the journal anchor read as before;
`World.CheckSystemDependencies` is folded into `InitSystems`; standalone system
tests wrap a construction in `ready()`. This gives the generic system's `Init`
and the soft-collision matrix a per-run rebuild that is the same pass at
startup and on reset.

### Phase 1: `pkg/machine` (landed)

`pkg/machine` holds §6 as built: `machine.go` (the instance, the graph and its
step), `build.go` (the builder and its validation) and `queue.go` (deleted in
Phase 6, S16), listed in [package map](package-map.md) §5. Tests pin each
validation fault by its message, one step making all four transitions in phase
order, a cue landing on a tick boundary, and the queue's order, timing and
encoding. `FuzzStep` checks the step's invariants from inside its hooks over
1,000 steps a seed, guards, probe, suspension, dt and respawns drawn from a PCG;
sixty seconds of it (520,000 seeds) found nothing, and nineteen mutants of the
kernel each fail a test. §6 records what the kernel settled or changed: enter
runs after the commit rather than before it, a retry's period runs cues again,
an edge owns its phase, and the structural faults; the queue's one empty
encoding went with the queue. No game code changed.

### Phase 2: quasar on the machine (landed)

`QuasarComponent` holds `Machine machine.Instance[QuasarState]` (`QuasarHoming`,
`QuasarCharging`, `QuasarZapping`) and `SpeedElapsed` where `IsCharging`,
`IsZapping`, `ChargeRemaining` and `LastSpeedIncreaseAt` stood. `QuasarSystem`
builds §5's graph once (`newQuasarGraph`, `machine.Must`), steps each quasar
through `GetPtr` and writes the flags after the step, so S7 to S11 close as §4
decided; the renderer reads `Machine.State`, and `snapshot.Schema` is 26. Against
the steps planned: the context carries no world or combat pointer, the hooks being
`QuasarSystem` methods; the shield follows the Shielded flag, so no exit writes
it; a suspended step strikes nothing, as a stunned quasar struck nothing before;
Charging's exit cancels the splash on every path, a timeout's included, where the
timer system has removed the splash earlier in the tick and the cancel finds
nothing; the observer is `newQuasarGraph`'s argument, nil in the game. With the
speed counted in movement time no shared store carries an absolute instant, so
`TestSimulationEpochIsSessionIdentity`, whose one carrier was the quasar's speed
instant, is retired with its child role; it returns inverted once the cursor clocks
come off the epoch (below).

Tests (`internal/system/quasar_test.go`): a charge whose zap cannot aim goes home
in the step it expires and cancels its splash once (S7); a stun during a zap
despawns the lightning and drops the shield and the enrage (S9); the first speed
lift lands on the twentieth movement tick, and a stun pauses the count (S11); a
returning cursor cancels the splash once (S8). Four mutants (Charging without its
exit, the stun edge without Zapping, the accumulator fed twice its dt, the timeout
without its Homing candidate) each fail one. `BenchmarkQuasarUpdate` times
`QuasarSystem.Update` alone and `BenchmarkQuasars` (`internal/app`) the whole
tick, each with 1 to 256 quasars on a 200×60 map and a cursor crossing between
corners every 100 ticks, so the far quasars charge, zap and come home.

Readings before (Phase 1's main) and after on a four-core VM, benchstat over
alternating rounds (whole tick 6 × 2,000 ticks, `Update` 8 × 20,000). No time
moved beyond the noise (±6 to 47 %, every p ≥ 0.46):

```
benchmark                    before    after     B/op     allocs/op
HeadlessTicks/main-160x50    61.3 µs   56.4 µs   -1.2 %   14 = 14
HeadlessTicks/td-200x60      392 µs    391 µs    ~        19 = 19
Quasars n=1                  31.2 µs   29.0 µs   ~        15 = 15
Quasars n=16                 229 µs    228 µs    -0.5 %   69 = 69
Quasars n=64                 1.21 ms   1.22 ms   -0.3 %   262 → 261
Quasars n=256                11.2 ms   11.0 ms   -0.1 %   1124 → 1116
QuasarUpdate n=1             917 ns    878 ns    =        0 = 0
QuasarUpdate n=16            12.9 µs   13.0 µs   +0.5 %   19 = 19
QuasarUpdate n=64            49.9 µs   50.8 µs   +0.6 %   77 → 78
QuasarUpdate n=256           195 µs    196 µs    +1.1 %   209 → 212
```

In a CPU profile of `Update` alone at 256 quasars the kernel's own frames (`Step`,
`first`, `run`, `take`) hold 4.7 % of the samples, about 37 ns a quasar step; in
the whole tick the quasar system holds 12 to 18 % of the samples in both builds,
movement most of it. `Update`'s added allocations are the zap tick S10 adds (a
lightning update and a shield drain a zap) and the timeout's splash cancel, an
8-byte payload. The in-game profiler (`sys quasar`, three rounds of 2,000 ticks a
build) reads 183 to 217 µs a tick at 64 quasars and 1.62 to 1.76 ms at 256 in
both; the capture is 7,680 B against 7,690 at 64 quasars and 21,776 against 21,840
at 256, and `QuasarComponent` stays 56 bytes. Over those 2,000 ticks at 64
quasars the events agree but for the accepted shifts and the paths they move:
lightning updates 63,689 → 64,104 (one more per zap), splash cancels 10 → 425
(one per timed-out charge), shield drains 74,173 → 74,245 and area crossings
10,484 → 10,141. A heap profile of the whole tick at 256 quasars showed a cost
neither build changed: each splash countdown rebuilt every other countdown's box to
place itself, 89 % of the bytes, about 670 KiB a tick. The splash system has since
filled one box buffer a tick (`SplashSystem.boxes`), which took `BenchmarkQuasars`
at 256 quasars from 645 KB to 61 KB a tick and the splash system out of that
profile.

The harness's fight spawns three of the scenario's own quasars before the storm
is raised and more after, so its four shapes read the quasar too, and they read
alike before and after (pg pages and cells repaired, kf keyframes, lc late
commits, lcp late copies, the lead at the end with the highest in brackets):

```
shape       pg   cells  kf  lc  lcp  lead     manifests
3,3,97,0     0      0    4   0    5  11 (13)  279
2,2,3,100   17    218    4   1    0  8 (9)    278
3,3,251,0    7     39    1   3    6  9 (10)   299
4,3,41,50   30    295   15   0    1  13 (15)  115
```

A scratch variant raising one more quasar on both ends at one tick in place of
the storm read alike in every late count as well; on the first shape its fight
drifted by one scenario quasar, seven spawned against six.

Headless, `bin/vif -script <file> -seed 1 -log-stdout -log-stat 50` on both
builds: with `god` at tick 50 and the quasar at (40, 15) at tick 100 over 600
ticks, the quasar spawns inside its circle and never charges, its `quasar.*`
counters agree, and its contact strikes move with the later speed lift (shield
drains 323 → 303); there are no `lightning.*` counters. With the cursor sent to a
corner of a 120×40 map at tick 150, one charge and then a zap to the end, the
quasar's counts move by S10 and the timeout's cancel alone: physics steps
109 → 110, lightning updates 989 → 990, shield drains 1,045 → 1,046 and splash
cancels 0 → 1; the decay and dust counts, which meet its footprint, drift with
where it stops.

Manual check: `bin/vif -seed 1`, `:god`,
`:emit EventQuasarSpawnRequest { x = 40, y = 15 }`; the quasar homes while the
cursor is inside its circle; leaving it starts the cyan splash, three seconds
later the lightning and the shield; coming back drops both; a pulse stun during
the splash cancels it.

### Between Phases 2 and 3: the cursor clocks as ages (landed)

`ShieldComponent.DrainElapsed`, `HeatComponent.EmberElapsed` and
`WraithComponent.Waited` stand where `LastDrainTime`, `EmberDecayTime` and `Since`
did: each a carried age its owner advances by dt in its `Update` (the shield and
heat systems where the cursor is simulated, `CursorSystem` for every Wraith on every
instance) and resets where the stamp was written; the shield's restart of a stamp
ahead of its clock went with the stamps. No component holds a `time.Time`, so the
column codec's RFC 3339 form and the generator's exception for it are gone, and a
component that declares one fails generation; `snapshot.Schema` is 27. The marker
pulse, the one shared reader of the instant itself, takes its phase from
`TimeResource.GameElapsed`.

`TestSimulationEpochIsNotSessionIdentity` (`internal/app/script_test.go`) is the
retired control inverted. The cross-process gate's origin starts all three clocks
before its capture, the surface adds each cursor's clocks to the compared lines, and
a receiver whose `SimEpoch` is shifted 37 h 17 ms reproduces the origin's 500-tick
continuation exactly; on the stamps it diverged in the FSM's timeline, the drain's
phase and the Wraith's wait. The gate world's capture is 3,137 bytes against 3,165.

One timing moved. A stamp read its tick's own instant, so a reset settled in the
tick's pre or post settle started at that tick; an age also counts the tick its owner
then runs, so that reset's first period ends a tick early: an ember a solo run's
scenario lights decays first after four ticks, then every five. Resets outside those
settles fire on the ticks they did, which a scripted arm read on both builds: shield
activations (pushed from `EnergySystem.Update`), input and system events, and wire
crossings (Wraith turns, a session's scenario arms), which land before the tick's
time advance, where the stamp read the completed tick. Three of the harness's four
shapes read alike before and after in every count, and all four in their late
counts; on 2,2,3,100 a correction that shrank below one `SnapshotCorrectionChunkBytes`
chunk travels in one frame instead of two at tick 766, which moves the shape's
every-100th-frame drop, and its repairs after the storm differ from there (38 pages
and 556 cells against 17 and 218). [Runtime §5](runtime.md#5-simulation-tick-steps)
states the rule for every clock.

### Phase 3: the dialect kernel (landed)

`internal/fsm/dialect` holds §7 as built, in one file and its test where the plan
named four. Against the steps planned: `Tree` is a struct whose `Build` a runtime
calls per guard reference; `Decode` takes no place, its callers prefixing one; and
no `Catalog` type stands between the registrations and `-schema`:
`Machine.Vocabulary` reads the registries. `fsm/std` and `bot` register typed
structs through the generic methods of §7, which replace `RegisterGuardFactory`,
`RegisterActionArgs` and `ArgCompiler`; `ParseIntArg`, `CompareInt`, `std.Ops`,
both `decodePayload`s, bot's guard and payload key lists and the unread
`SystemControlArgs.Enabled` are gone. Beyond the plan, an action refuses a key of
its own table it does not read, an `EmitEvent` payload on an event that carries
none fails, and a bot graph's refused actions leave its vocabulary. The strict walk
is `toml.Unmapped` and `toml.UnknownKeys` (toml v0.4.0), which `paths.UnknownKeys`
now aliases. `resource.entryKeys` holds the keys an entry reads, nine until
Phase 4 added `behaviors`. `-schema` replaces `guards` and `actions` with
`vocabularies.scenario` and `vocabularies.bot`, and no longer prints the runtime
capture's banner ahead of its JSON in a race build or under `-dev`. Not yet checked
at load: the payload fields a `Payload*` guard, `capture_vars` or `payload_vars`
names ([todo](todo.md)).

Tests: `dialect_test.go`, one per rule (an op outside the six, each op and an
omitted one, unknown keys and a mistyped value named by path, a tree nesting
`And`, `Or` and `Not`, a broken tree naming where, the entries' order);
`TestABrokenGraphFailsAtLoadNamingTheFault` gains a mistyped value, an unknown op,
a misspelt state and transition key and an unread action key;
`TestAnEntryKeyNothingReadsIsRefused`. `std_test.go`, `machine_test.go` and the
`internal/app` tests that load inline scenarios pass unchanged.

Manual check: `bin/vif -check` and `-check -s wad/scenario/<blank, simon, td>`
print `scenario ok`. Copies of td fail `-check` naming the state and the fault:
`op = "ge"` (`state 'TdCalm' transitions: guard 'StatusIntCompare': op: "ge" is
not one of eq, neq, gt, gte, lt, lte`), `on_entr` (`state 'PlayerBoot': unknown key
"on_entr"`), `=>` inside an `And` (`guard 'And': guards[1]: guard 'VarCompareVar':
op: ...`), `render_fgg` in an `EmitEvent` payload (`payload for event
'EventMazeSpawnRequest': unknown key "visual.render_fgg"`) and a `payload` on
`TerminateRegion` (`action 'TerminateRegion' takes no payload`). `bin/vif -schema`
prints the same bytes on every run; the scenario vocabulary holds 20 guards
(`And`, `Or` and `Not` among them) and 19 actions, the bot's 26 and 19, none of
the four that write the world.

### Phase 4: programs, the `Behavior` component and the generic system (landed)

A species defined by files plays in a scenario and `-check` validates it, as §8
describes as built: `internal/behavior` holds the program document, the registry
and the compiler (`program.go`, `vocab.go`, `compile.go`),
`internal/fsm/file_loader.go` the include, `internal/system/behavior.go` the game
vocabulary and the generic system, `internal/render/renderer/behavior.go` the
renderer and `wad/scenario/warden` the sample. `snapshot.Schema` is 28 (the
`Behavior` store, the kill array at `SpeciesMax`), `manifest.SchemaVersion` 3
(`vocabularies.behavior`) and `event.JournalSchema` 40 (the beam's `Species`).
Against the steps planned:

- The prototype has no `spawn` key. A spawn queued behind its beam would be state
  outside any store, a `Snapshot: "state"` carrier, so the fuse beams a data
  species in instead, as it plays a fusion before the direct request: the beam
  is a Shared entity naming the species, its completion sends the spawn, and
  nothing outside the stores waits (§8.5, Events). The fuse's profile becomes
  `dual`, its fusions player-domain and the entrance shared.
- A spawn counts in `behavior.<program>.count` at once rather than at the next
  step, since a beam lands its instance after the step and a region reads the
  count.
- It gains `mass` and `drops`, which soft collision and loot read where §8.1 had
  no key for them.
- No `Params` slice: `Param` reads the `Genotype` component genetics already
  writes.
- Status keys are `behavior.<program>.*`, since a bare `<program>.*` shares every
  system's prefix namespace; the name rules keep them and `kills.<program>` apart
  from the code species' keys.
- `LoadScenarioFromMap` is unchanged, and the include is the program's own:
  `loadAndResolve` adds `states` and merges region files, which a program must
  not get, so the two share only the factored read, parse and expand.
- Cancel is shared, as every species' cancel; spawn is bus, as the swarm's, and so
  barrier-bound, since a crossed one allocates shared identity.
- `Shielded` refreshes the ward and refuses a stun, as the quasar's shield does.
- `Drag{rate}` is per second where `Drag{factor}` was per tick.
- A tick is the sustained verbs, then one integration; `Freeze` skips the move,
  and the sample's Wind freezes as the swarm's lock stands still. The
  `Vocabulary.Entered` hook ends every entry, so a probe's cell serves only the
  entry it guarded.
- `SpeciesDimensionsLUT` went rather than grew, nothing reading it;
  `BehaviorBoss` became `BehaviorData`. `combatEntityNames` moved to
  `component.CombatEntityNames` and the homing profiles are named in
  `profile.Homing`, where the compiler reads them. The context system's optional
  dependencies add `quasar`, for `SpawnSpecies`.
- The sample beams its warden in at (10, 5), past its reach of the centred
  cursor, so it opens with a wind-up, and beams another in after a kill (3 s,
  "Warden down") or a lifetime (1 s) rather than ending; its cursor keeps blank's
  heat 10, since heat 100 raised ten drains.

Mechanical edits, counted with `grep -P` over `internal/` before and after:
`[component.SpeciesCount]` tables 10 → 1 (`SpeciesNames`, the code species
alone) and `[component.SpeciesMax]` 0 → 8; `>= SpeciesCount` checks 3 → 0 and
`>= SpeciesMax` 0 → 3; `< SpeciesCount` loops 9 → 2 (meta's code kill keys, the
kraken's code rows) and `< SpeciesMax` 0 → 4; hit-flash copies 5 → 1.

Tests, one per rule. `internal/behavior`: IDs follow sorted names; a compiled
program steps as its states say (the Wind cue once at 800 ‰, the timeout's
candidates by the probe, `AfterTick` on `HitWall`, the global stun edge and
suspend); a compile error names program, place and key (unknown verbs, flags,
states, keys, rows, profiles and drops, glyph shapes, a header outside the
footprint, nine programs, each misplaced verb); the name rules; the vocabulary's
entries. `internal/resource`: a program is a scenario file (listed, digested,
expanded against the entry's macros, its entry loading into a machine); a
`behaviors` table names a file or is the program; a program that does not compile
fails the scenario; `-check` reports the programs. `internal/system`: a warden
winds up past its range, then charges the cell it stored; a stun sends a winding
warden back to Roam and ends its telegraph; a cancel ends it through its
composite; a request naming no program is counted and dropped; at zero hit points
it dies as species 10 and `kills.warden` counts it; combat refuses to stun a
shielded data species; the drops its prototype names; soft-collision rows only
with mass, every code pair unchanged; a spawn counts its instance at once; a
fused data species lands when its beam ends, on the beam's footprint; a cancel
ends its program's beams in flight; only a shared beam lands one. The store
round trip in `internal/snapshot` covers the beam's `Species` column. The
renderer draws the frame shown in the
state's colour, and the health bar spans the prototype.
`TestSnapshotDeclarationsMatchImplementations` holds the context systems to their
declarations, and `internal/app`'s soak and domain tests run the warden.

The harness (no program in the scenario), main against the branch, read alike in
every FSM transition line and in manifests, keyframes, late commits, late copies,
leads, pace steps and the storm's spawns; only bytes moved:

```
shape       pg        cells       kf  lc  lcp  lead     link B/s
3,3,97,0     0          0          4   0    5  11 (13)   5908 → 5908
2,2,3,100   38 → 30   556 → 374    4   1    0  8 (9)    10703 → 10757
3,3,251,0    7         39 → 41     1   3    6  9 (10)    5390 → 5391
4,3,41,50   30        295 → 301   15   0    1  13 (15)  12247 → 12329
```

Seven `behavior.*` keys join the compared status (`protected_player_rejects` is
excluded by its suffix), so a repaired status page holds one to three more cells;
the capture grows (the `Behavior` section, the kill array from 10 to 18), which
moves 2,2,3,100's every-100th-frame drop onto another frame, as the cursor clocks
did on that shape. The fuse entrance moved only the link rates, by a byte or two
a second, the storm's shared beams carrying the `Species` column.

Manual check, in tmux (`bin/vif`, `:n warden`, from the campaign too, and
`:energy 1000000000` to outlast it): a beam converges on the top-left corner for
a second and the warden appears on its footprint as it ends; it winds up enraged
and still under the cyan countdown, the beam marks the cursor's cell at 0.8 s, it
charges that cell and takes its own colour on arriving; a disruptor stun
(`:emit EventWeaponAddRequest { entity = 1, weapon = 2 }`) drops it to Roam and
holds it in place until it dies; `:emit EventBehaviorCancelRequest { program =
"warden" }` removes it, and another beams in a second later
(`behavior.warden.despawned` 1); under `:god` it dies fast, "Warden down" shows
and another beams in three seconds later. An idle cursor dies at the first
strike, its shield's energy drained, then its heat. A headless run beams the
warden in at tick 19 and lands it at 38, loses it to its lifetime at 738 and
lands another at 778.
`-check -s wad/scenario/warden` prints `behaviors ok: 1 (warden)`; copies
misspelling a sustained verb, a flag, a state and a key fail naming each
(`program 'warden': state 'Wind': on_update[0]: unknown sustained action
'Freez'`), and nine programs fail on the count where eight pass. `-schema` prints
the same bytes on every run, `vocabularies.behavior` holding 28 entries.

### Phase 5: swarm on the machine (landed)

`SwarmComponent` holds `Machine machine.Instance[SwarmState]` and `PatternElapsed`
where `State`, `PatternRemaining` and the five phase countdowns stood.
`SwarmSystem` builds §5's graph once in `NewSwarmSystem` (`machine.Must`), steps
each swarm through `GetPtr` after its lifecycle checks and writes
`Combat.IsEnraged` from the flags, so S12 to S14 close as §4 decided, and S15 for
the swarm; the charge line reads `Machine.Elapsed`, the teleport line
`Machine.Progress()`, and `snapshot.Schema` is 29. Against the steps planned:

- The context carries no dt: no hook reads it, the pattern accumulator advancing
  in `Update` ahead of the step.
- The teleport line takes `Progress()`, the clamped fraction it computed by hand.
- Every Lock → Decel commit counts in `swarm.transition_stalls`, so a lock whose
  teleport finds no landing now counts; the old code braked there uncounted.
- A lock with line of sight but no kinetic would try the teleport before braking,
  where the old entry braked; every swarm has its kinetic from spawn, so neither
  runs.
- The swarm's states have no exit hook, so `Stop` only reports to the observer.
- `stepSwarm` replaces the stall tests' three inline copies of the fixed tick.

Mechanical edits, counted with `grep -P` over `internal/` before and after:
`SwarmComponent` countdown fields 6 → 0; reads and writes of a swarm countdown
26 → 0; a swarm component's `.State` 13 → 0 and `.Machine.State` 0 → 5; the
renderers' `parameter.Swarm…Duration - …Remaining` 2 → 0; the stall tests'
nanosecond fixtures 2 → 0, `aboutToExpire` fixtures 0 → 2, and inline
`Time.Update` calls 3 → 1, the one in `stepSwarm`.

Tests (`internal/system/swarm_stall_test.go`): the five rules pass on
about-to-expire fixtures (`Elapsed = Duration - 1 ns`); a stun in any attack state
sends the swarm back to the chase with the whole interval, one in the chase
freezes what is left of it, and nothing moves while it holds, a teleport included
(S14); a charge that meets a wall, here the map's edge, decelerates in the step it
hits and keeps the bounce (S12). Seven mutants each fail one: no post-tick wall
edge, the relocation as Teleport's exit, the stun edge without Teleport or with
Chase, the chase timeout holding instead of retrying, the observer ignoring
retries, and the lock's timeout without its Decel candidate.

Per-tick traces of one swarm over 800 ticks (a scratch test: in the open,
chasing a cursor moved every 45 ticks, and behind a wall that forces a teleport)
change state on the same ticks in both builds, and `ChargesCompleted` agrees on
every tick. What moved is S12's and the flags': the branch integrates in the step
that enters Charge (its first steer, over the entry's whole duration), Decel and
Chase, where the old code returned, so a charge flies 16 ticks against 15 and
lands on the stored cell's centre in both, Decel brakes twice against once, and
the chase homes a tick earlier; `IsEnraged` turns on in the Lock's entry step and
off in the Chase's, a tick earlier at both ends. The chase's path drifts from
there, up to 9 cells, and meets again where each attack ends.

The harness's four shapes read alike before and after in every count; only the
link rates moved, by up to 9 B/s with the component's columns. A swarm's rows
show in one repair, at tick 1018 on 3,3,251,0, alike in both builds but for those
columns:

```
shape       pg   cells  kf  lc  lcp  lead     manifests  link B/s
3,3,97,0     0      0    4   0    5  11 (13)  279         5908 → 5907
2,2,3,100   30    374    4   1    0  8 (9)    277        10757 → 10766
3,3,251,0    7     41    1   3    6  9 (10)   299         5391 → 5392
4,3,41,50   30    301   15   0    1  13 (15)  115        12329 → 12329
```

Headless, `bin/vif -script <file> -seed 1 -log-stdout -log-stat 50` on both builds
with `god` at tick 1 and the swarm at (40, 15) at tick 100 over 900 ticks of a
120×40 map, the `swarm.*` counters agree but `physics_steps`, 729 → 774; with a
wall down x = 40 and the swarm at (20, 18), 703 → 696. That counter sums
sub-steps, which follow the speed, so S12's earlier steps move it both ways;
stalls, wall collisions and boundary reflections stay 0.

Manual check, in tmux (`bin/vif -seed 1`, `:god`, `a` to stop the auto-fire,
`:log on`, `:log stat 50`, `:emit EventSwarmSpawnRequest { x = 40, y = 15 }`),
read from pane captures every 50 ms: the swarm chases for 5 s and stops; the
charge line pulses twice, the first from 0.5 s into the lock and the brighter
second 0.75 s later; 2 s after stopping it charges about 30 cells in 0.8 s onto
the cursor's cell, stops, and the cursor's shield knocks it clear once it chases;
the next lock comes 5 s later. Behind a column of walls at x = 60 the lock ends
in the teleport beam through the wall for 0.4 s, and the swarm stands at the
landing by the cursor. With the loadout cut to one disruptor charge (`:god` arms
every weapon), one main fire as the first pulse left the swarm stunned it: the
line was gone in the next frame, the swarm stood for the stun's 2 s, made no
charge when its lock would have ended, chased from rest and locked again 5 s
after the stun ended. `swarm.transition_stalls` stayed 0 over five swarms, every
lock finding the cursor; the no-target path is the first stall test's.

### Phase 6: storm circles, and spawns that ride their beams (landed)

`StormCircleComponent` holds `Machine machine.Instance[StormCircleState]`
(`StormCircleCooldown`, `StormCircleActive`) and `LandingFound` where
`AttackState` with its Idle value, `CooldownRemaining`, `AttackRemaining` and
`AttackProgress` stood. `StormSystem` builds §5's three graphs once from
`circleVariants`, starts each circle as its header is built, steps it through
`GetPtr` and writes `IsInvulnerable` and `Mount.Armed` from the flags, so S2 to
S6 close as §4 decided, and S15 for the storm; the renderer reads `Machine.State`
and `Machine.Progress()`, and `snapshot.Schema` is 30.

How the fuse completes was this phase's open question. The user chose to have
every spawn deferred behind a beam ride it (S16), as a data species' already did:
the beam names the species its completion lands, so the spawn, the beam's timer
and its visual end on one tick. The blue's cue starts a Shared beam naming the
swarm, which `StormSystem` lands on the beam's completion; a fusion's
player-domain beam names the swarm or the quasar, which `FuseSystem` crosses as
the spawn request (D-3) on its completion. The storm's pending list and its D-19
record of it, the fuse's list and its `LocalStateSaver`, and `machine.Queue`
went; `event.JournalSchema` is 41, and `manifest.Fingerprint` moved with the
storm's optional `materialize`. Against the steps planned:

- No queue (steps 2, 3 and 6): the beams replaced it, `stormSnapshot` keeps the
  root alone, `FuseSystem.Update` is empty, and `pkg/machine` lost `queue.go`.
- The variant row names Active's flags and its enter and tick hooks beside
  {initial, attack, repeat, cue}: the colours differ in what Active does as well
  as in its timing.
- Every fusion plays a beam and lands as it ends, at 1 s. A drain pair's swarm
  landed at 0.5 s, its spirits flying into no beam unless one was asked for, and
  a quasar at 0.6 s while its beam still receded. `SwarmFuseAnimationDuration`
  and `SpiritSafetyBuffer` went with those timers.
- A quasar fusion is refused while a quasar's beam flies (`quasarBeaming`),
  where a pending entry refused it, and its landing no longer despawns every
  spirit, which could cut a drain pair's spirits short.
- A drain pair's fusion emitted its drains' death twice; `killDrains` alone
  emits it now.
- The blue glow fades from the beam's opening, `StormBlueMaterializePermille`.
  The red's muzzle flash draws while its mount is armed, where an
  `AttackProgress <= 0` test skipped the burst's entry tick: with S5 a stunned
  burst stays Active, and its flash would flicker on with nothing fired.
- `aboutToExpire` in `swarm_stall_test.go` is generic over the state type, so
  the storm tests share it.
- `TestOneSharedQuasarTriggerProducesOneSpawn` steps 28 ticks plus the barrier
  delay, for the 1 s beam and a lead, where the 0.6 s timer took 20.
- `storm.buf_pending_blue_spawns_hwm` and `fuse.buf_pending_hwm` went with their
  lists.

Mechanical edits, counted as occurrences with `grep -rPo` over `internal/` and
`pkg/`, tests included and generated files not, before and after: the circle's
countdown and progress fields (`.AttackState`, `.CooldownRemaining`,
`.AttackRemaining`, `.AttackProgress`) 29 → 0; `StormCircleAttack{Idle,Cooldown,
Active}` 16 → 0 and `StormCircle{Cooldown,Active}` 0 → 16; the three duration
getters 9 → 0 and the per-colour attack functions (`processCircleAttack`,
`process{Green,Red,Blue}Attack`, `initBlueAttack`) 15 → 0; the storm's list
(`pendingBlueSpawn(s)`, `hasPendingBlueSpawn`, `processPendingBlueSpawns`) 36 → 0
and the fuse's (`pendingFusion`, `s.fusions`, `hasQuasarFusion`) 32 → 0;
`machine.Queue` and `Queue[` 11 → 0; the three deleted parameters 8 → 0; reads of
a circle's machine (`circle.Machine`, `circleComp.Machine`) 0 → 22, and
`Machine.Progress()` 1 → 3.

Tests (`internal/system/storm_attack_test.go`): a stun holds an Active red's
attack time and disarms its turret, and the burst goes on where it stopped once
the stun ends (S5, S4); with no cursor a circle's machine holds while
`IsInvulnerable` follows its depth and its attack on every step (S4); an
expired cooldown holds in the concave and attacks at the convex; the blue's cue
beams once an attack and not at all without a landing (S2); the draw still
precedes the search (D-8); the blue's swarm lands as its beam ends, on its
footprint; a storm's end ends its beams and no other (S1). In
`internal/system/fuse_test.go`, a drain pair with spirits and without and a
quasar each land the tick their beam ends, on its footprint, and not before; a
second quasar fusion waits for the one in flight. In
`internal/render/renderer/storm_test.go`, a disarmed turret draws no muzzle
flash. Twenty-two mutants, thirteen over the landings and the fuse, eight over
the circle machine and one over the flash: twenty-one fail a test, and the
survivor, the spawn entering the repeat cooldown, is equivalent while every
colour's initial cooldown equals its repeat.

Readings, both builds from the same seeds:

- Per tick, headless, seed 1, the storm at tick 101: the red's first burst opens
  at tick 120 against 122 (Idle's tick gone, S6, and the attack's tick run in
  its entry step, §6.3); the green's first pulse at 126 against 127 and the
  blue's glow at 216 against 217, both waiting for the convex; the blue's swarm
  at 276 against 277.
- That shift moves a three-body orbit, so a seed plays as another seed would;
  each build repeats itself exactly, and the readings compare distributions.
  Headless over 40 seeds (`god` at tick 1, the storm at tick 100, 1,500 ticks of
  a 120×40 map, embedded content): boundary reflections 48.2 ± 17.1 → 45.7 ±
  18.2, protected rejects 4,285 → 4,379, green frames 580.4 → 581.2, red frames
  831.1 → 833.3, swarms 8.9 in both; every gap is under 1.4 standard errors.
- The harness's four shapes, one fight each: a late commit more on one shape and
  a late copy more on two, pages and cells repaired both ways. The sweep below
  reads all of it as the orbit's noise.

  ```
  shape       pg        cells       kf        lc      lcp     lead     manifests  link B/s
  3,3,97,0     0 → 4      0 → 65     4        0 → 1   5       11 (13)  279 → 278   5907 → 7016
  2,2,3,100   30 → 15   374 → 227    4 → 3    1       0       8 (9)    277 → 286  10766 → 8302
  3,3,251,0    7 → 1     41 → 8      1        3       6 → 7   9 (10)   299         5392 → 6626
  4,3,41,50   30 → 43   301 → 492   15 → 11   0       1 → 2   13 (15)  115 → 137  12329 → 4004
  ```

- A session sweep, 8 seeds over the four shapes, 1,200 ticks with the storm
  raised at tick 400, 32 fights a build: pages repaired 17.0 → 16.8, cells 204 →
  236, late commits 1.0 → 1.0, late copies 3.7 → 4.4, keyframes 5.0 → 5.0, host
  to guest 394.5 → 392.9 kB, guest to host 219.8 → 225.3 kB. Every gap is under
  0.8 standard errors but quasar kills, 5.2 → 4.3 (z = −3.2).
- That gap is the fusion's landing, not the storm: fuse to spawn takes 24.2
  ticks against 16.0 (median 20 against 12), a quasar lives 66.3 ticks against
  46.0 once the storm is raised (58.9 against 55.6 before), and the raised storm
  370.9 against 341.6. The branch without S5 reads 363.8, and main with its
  fusion timers at 1 s 369.2 and 4.28 kills. Fusion beams of 0.5 to 0.6 s would
  give the old pacing back, spawn and visual still matched.
- CPU: a scratch benchmark over 1,500 ticks of a live storm, six runs
  interleaved, takes 219 µs a tick on main and 222 µs on the branch, inside the
  ±7 % between runs; profiles put `StormSystem.Update` at 57 to 70 µs against
  43 to 67, most of it physics, and the step allocates nothing per tick.

Manual check, in tmux at 160×50 (`bin/vif -seed 1 -nw`, `:god`, `:log on`,
`:log stat 1`, `:emit EventStormSpawnRequest`), read from the per-tick status
log and from pane captures every 50 ms:

- The green pulses for 20 ticks with at least 1 s off; the red bursts for 40
  with at least 1 s of rest, longer where it waits for the convex, and
  `mount.fired` counts its burst ticks exactly; the blue glows for 50 (546–595,
  697–746, 851–900 and 1001–1050 of a storm raised at 431), and its swarms land
  at 606, 757, 911 and 1061, 60 ticks after each glow opens: the cue 40 ticks in,
  then the 20-tick beam.
- The first landing's captures show the beam's last cells at tick 605 and, at
  606, the swarm on the beam's footprint with no beam left. The glow fades from
  the beam's opening and ends 0.5 s before its swarm lands, as on main.
- With a disruptor alone (`:energy 100000000`, `EventWeaponRemoveRequest` for
  weapons 0 to 5, `EventWeaponAddRequest { entity = 1, weapon = 2 }`, `a` for
  auto-fire) beside a raised storm, pulses every 41 ticks stunned the circles
  for 40. The red's burst ran 30 ticks, then one tick in each of the ten
  one-tick gaps between stuns, and ended after exactly 40 burst ticks with
  `mount.fired` counting each: the stun held the burst and did not restart it
  (S5).
- In that run seven drain pairs each landed their swarm 20 ticks after
  `drain.fusions` counted them, and `:emit EventFuseQuasarRequest` at tick 4805
  landed its quasar at 4825; the captures show the beam's last cells at 4824 and
  the quasar with no beam at 4825.

### Phase 7: close-out

- `doc/ecs-and-events.md` §7 (species phase timers now live in
  `machine.Instance`), `doc/term-index.md` (every "Future design" entry of this
  document reads as code), `doc/README.md` (this document is the subsystem
  reference), `doc/telemetry-reference.md`, `doc/package-map.md`.
- This document: §2 and §4 shrink to what landed; §9 keeps the readings and the
  version moves.
- `doc/todo.md`: a deferred gap found in a phase goes there in the file's
  pattern.
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
