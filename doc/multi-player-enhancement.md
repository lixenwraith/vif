# Multiplayer enhancement plan: authoritative host, deterministic guests

**Status: this is the plan of record for multiplayer.** It supersedes the staged
recommendation in [desync.md](desync.md), which is retained as the diagnosis of the
2026-08-30 divergence and as the survey of the option space. Domain rules D-1..D-23
in [domain-design.md](domain-design.md) remain authoritative for the *existing*
code; §5 of this document states which of them the target architecture keeps,
changes, and adds to. D-18 landed with Phase 1; D-19, D-20 and D-21 landed with
Phase 2; D-22 landed with Phase 3; D-23 landed with Phase 4, which also weakened
D-11 and changed D-3's destination; D-24 landed with Phase 5.

**Phases 1 through 5 are done.** What is left is Phase 6, and §6's entry for it
says what each of its items still needs. §9 records the defects each session
surfaced and what each turned out to be.

## 1. Why the current design is being replaced rather than repaired

The session model that exists today was assembled from compromises, and the
compromises are load-bearing. Restating the original requirements makes that
visible:

| Original requirement | What existed when this was written | Where it stands |
|---|---|---|
| A solo game can be toggled into a host, and others join **at any time** | Join is only possible at tick zero, through a lobby gate fixed before the run starts. The replay-from-tick-zero path that nominally provided mid-run join was never reachable from `cmd/vif`; it has been removed. | **Done** (Phase 3). `:host <addr>` opens a running instance and a participant installs the world at whatever tick it has reached. |
| A **true multiplayer experience** | Fast input is discarded, not merely delayed: measured, a session drops 4 of 5 rapid cursor motions and scores 5 of 6 fast keystrokes as *typing errors*. | **Done** (Phase 1). Local input applies immediately; §3 carries the after figures. |
| Resilience to **lag, jitter and bandwidth limits** | None. A deterministic lockstep barrier converts jitter into a permanently forked session, and there is no repair on any edge. | **Done** (Phases 4 and 5). A guest predicts and is corrected on a cadence, a divergence is a magnitude rather than a failure state, and jitter costs freshness rather than the session. Phase 5 made the cadence a function of the link the protocol now measures, bounded by a convergence floor that is refused at admission and reported mid-session rather than adapted past. |

The root cause of all three is one decision: **shared state is agreed by having
every instance re-derive it, so nothing may be applied until everyone can apply
it.** That forces input through a playout barrier (which is the responsiveness
defect), forces agreement to be all-or-nothing (which is why jitter forks the
session and why nothing re-converges), and leaves no object that describes the
world (which is why joining means replaying the entire session).

Determinism is not the problem and is not being discarded. Using determinism *as
the replication mechanism* is the problem. The target keeps the deterministic
simulation and demotes it from "the source of truth" to "a very good predictor".

## 2. The target architecture

**Authoritative host, deterministic guests, snapshot-corrected.**

- The **host is the sole authority** for shared state. Its simulation is the game.
- **Guests keep running the same deterministic shared simulation**, but as a
  *predictor* seeded from the host's most recent authoritative snapshot, not as an
  independent source of truth.
- The host sends **periodic authoritative snapshots** (full, then deltas) plus the
  ordered stream of inputs/crossings. Guests apply corrections when they arrive.
- **Local input applies immediately.** A guest never waits for anyone to move its
  own cursor, fire its own weapon, or type its own glyph.
- **Joining is sending a snapshot.** Cost is a function of world size, not of
  session length, so a participant may arrive at any moment — and a solo run can
  become a host at any moment.

### 2.1 Why this shape, for this codebase

Because both sides run identical deterministic code, guest prediction is
extremely accurate: divergence between a guest's extrapolation and the host's
truth accumulates slowly and only where an unmodelled input intervened. That has a
direct consequence the naive authoritative model does not get:

> **The snapshot rate can be low.** A classic authoritative server must stream
> state at or near tick rate because its clients cannot simulate. Here the guest
> *can*, so snapshots are corrections rather than the picture itself — 2–5 Hz
> rather than 20 Hz, with the artifact stream filling the gaps.

That is the bandwidth-resilience answer, and it is only available because the
determinism work already exists. It also degrades gracefully in exactly the way
the requirement asks: under bandwidth pressure the snapshot rate drops and
prediction carries more of the load; under jitter a late snapshot simply means the
guest extrapolates a little longer; under loss the next snapshot is self-sufficient
or names its baseline. None of these fork the session, because a guest's own
derivation was never authoritative to begin with.

The existing domain split is already the right foundation. The player domain —
glyphs, weapons, projectiles, drains, nuggets, every effect, 26 of the 53 declared
systems — already runs locally with no replication at all. That half needs no
change. What changes is the 19 shared systems: they keep running on every
instance, but on a guest their output is provisional.

### 2.2 What each mechanism is for

| Concern | Mechanism |
|---|---|
| Local responsiveness | Local-first input: a guest applies its own player-domain action and its own cursor placement immediately, and tells the host afterwards |
| Remote cursors and shared entities looking smooth | Guest extrapolation between snapshots, which is just the existing simulation |
| Correctness | Host authority: where guest and host disagree, the host wins, always, with no negotiation |
| Jitter | Extrapolation absorbs it; a late snapshot is not an error |
| Bandwidth | Low snapshot rate, deltas, and a cadence chosen per peer from the link's own measured round trip, delivery rate and backlog |
| Join anytime / reconnect / solo-to-host | One mechanism: send the current snapshot |
| Loss | The next snapshot supersedes; the ordered stream carries acknowledgement and gap repair for the artifacts that must not be missed |

### 2.3 What is deliberately *not* in the target

- **No lockstep barrier on input.** The playout lead is removed from the local
  path entirely. It may survive as a receive-side interpolation delay for *remote*
  actions, which is a different thing with a different justification.
- **No bit-exact cross-instance agreement requirement.** Guests are allowed to
  drift between corrections. This is the single biggest conceptual change and it is
  what buys the resilience.
- **No host election or partition survival** in this plan. Host loss ends the
  session, explicitly and with a message. Election without state migration
  produces an empty authority; it is a separate project.
- **No discarding of the journal.** Deterministic replay stays, for solo runs and
  for debugging, and the host's own journal remains a faithful record. It simply
  stops being the multiplayer transport.

## 3. Measured evidence

All figures were measured on this repository by driving the real input path
(`App.Inject`, which is what `cmd/vif`'s event loop calls) and the real
two-participant harness (`meshSession`).

**Responsiveness.** `EventCursorMoveRequest` is `ClassBus`; `mode.OpJump` pushes it
as a crossing; in a live session `NetworkSystem.Cross` takes ownership and the
event is *never published locally* until its apply tick.

| Probe | Solo | Session, before Phase 1 |
|---|---|---|
| One `l` press reaches the producing instance's own store | immediately, without a tick | **after 4 ticks (200 ms)** |
| Five `l` presses issued between two ticks | 5 of 5 cells | **1 of 5 cells** |
| Six correct keystrokes typed back to back over a glyph run | 6 cells, **0 typing errors** | 1 cell, **5 typing errors** |

Phase 1 has since landed and every session figure above now equals its solo
neighbour; the three probes are assertions in `internal/app/local_input_test.go`.
The store column was unchanged by Phase 1 and deliberately so: the crossing still
applied at its barrier tick on every instance, and what moved was the cell the
producing instance reads. **Phase 4 moved the store column too** — the playout lead
came off the local path entirely, so a producer's own crossing reaches its own
shared store in the tick that produced it, and only the peers still wait out the
lead.

The third row is the one that matters most: fast typing is not merely dropped, it
is *scored against the player*, because five of six correct keystrokes resolve
against a cell whose glyph has already been consumed. In a typing game.

**The lead is not the cause.** Repeating the five-press probe at negotiated leads
of 3, 2 and 1 ticks loses identically (1 of 5 cells every time). Any deferral
collapses everything issued between two ticks onto one stale cell, because the
input path re-reads the authoritative store for each action. **Shortening the lead
cannot fix responsiveness; only applying locally can.**

**Simulation cost is not a constraint.** Driven on `config/main` with tower and
storm forced:

| Shared positioned entities | Live total | One full tick |
|---:|---:|---:|
| 12 | ~400 | 13 µs |
| 2,487 | 3,157 | 123 µs |
| 2,984 | 4,046 | **353 µs** |

At six times the observed incident high-water, a tick costs 0.7 % of its 50 ms
budget. Guest extrapolation, snapshot validation and catch-up are all affordable;
`sharedDigestLocked` costs 273 µs at that load.

**Where the sizing lands.** At the incident trace's 500-entity shared high water, a
snapshot carrying identity, position, kinetics and combat is on the order of tens
of kilobytes uncompressed. At 2–5 Hz that is single-digit to low-tens KB/s, in the
same envelope as today's 3–38 KB/s artifact stream — which is the point of using
determinism to keep the rate low.

## 4. The obstacles this plan has to clear

These are the findings that determine the phase order. They are stated here rather
than discovered mid-implementation.

*Sections 4.1 and 4.2 are resolved; 4.3 stands.* They are kept as written because
they are the reasoning the phase order came from, and because §9 shows §4.2 was
right about the hazard and wrong about when it bites. What each turned into is
noted at the end of the section. §4.2's cross-process gate landed with Phase 3.

### 4.1 The hidden-state surface

A snapshot must carry everything that decides a future shared outcome. Much of it
is not in a component store:

| State | Where | Obstacle |
|---|---|---|
| ~24 per-system RNG streams | private `*vmath.FastRand`, seeded in each `Init` | `State()` exists; **there is no `SetState`** |
| A second generator | `WallSystem.mazeRng`, a `math/rand.Rand` | not restorable as written |
| **EXP3 route learning** | `AdaptationResource.Entries` — weights, pre-sampled `Pool`, consumer `Head`, `spin`; decides which route a spawned eye takes | a resource, not a store; not in the digest |
| **Genetic evolution** | `GeneticResource.Registry` — PCG positions, scored archives, pre-produced proposals, pending evaluations/IDs and scout phase behind package locks | complete checkpoint contract; archive persistence alone is insufficient |
| FSM runtime | `fsm.Machine` regions, `variables`, `delayedActions` | straightforward, and small |
| Throttled derivation phase | `FlowFieldCache.TicksSinceCompute`, `PendingUpdate`, `LastTargets` (D-17) | phase must travel; the field itself should be recomputed |
| Allocator, scheduler | `nextEntityID`, per-domain counters, settle stamp, run/tick | straightforward |
| Per-system scratch | dirty sets, `NavigationSystem.routeRebuildTicks`, `GeneticSystem.tracking`, throttles | **no inventory exists** |

The two learned resources are `shared`-profile state that neither existing
document lists, and neither is covered by the digest — so a divergence in either
is silent until it moves an entity.

**Resolved by Phase 2.** The inventory is a build-time list now: `SystemDef`
declares the obligation, the generator emits the table, and the boundary suite
fails a system whose declaration does not match its code (D-19). `FastRand.SetState`
exists; `RandResource` enumerates the streams because they all come from one
factory; `WallSystem.mazeRng` carries the PCG source's own binary form; both
learned resources have export contracts; the FSM runtime travels. The genetic
contract was initially archive-only. The Phase 4 cleanup strengthened it to the
complete streaming and scout continuation point after the gateway gate proved an
archive cannot determine the next genotype. The last row —
"per-system scratch: no inventory exists" — turned out to be the interesting one:
the 500-tick gate found the genetic telemetry throttle, the gold deadlines and the
D-17 recompute phase by failing, one at a time. What the survey could not list, a
gate could.

### 4.2 Absolute timestamps will not survive a transfer

Shared components store *absolute* instants: `GenotypeComponent.SpawnTime`,
`QuasarComponent.LastSpeedIncreaseAt`, `ShieldComponent.LastDrainTime`, and
`AdaptationEntry.DrainTime`. This is sound today only by a subtle argument — every
reader takes a *difference* against `Time.GameTime`, and the start gate freezes
game time until all participants are ready, so origins align.

It stops being sound the moment state is transferred between processes: a snapshot
installed on a machine with a different clock origin makes every `now.Sub(stored)`
wrong by that difference, and none of it appears in `worldDigestScopedLocked`, so
the error is invisible until an eye changes speed at the wrong moment.

**Consequence for the plan:** snapshots must carry tick-relative durations, and the
round-trip test must run **cross-process with a deliberately offset clock origin**.
A same-process round-trip would pass while the real transfer fails, so it must not
be the gate.

**Resolved by Phase 2, and the scoping here was wrong.** This section says the
absolute instants are "sound today by a subtle argument" and stop being sound "the
moment state is transferred between processes". The 2026-08-31 log shows they were
never sound: each instance's game time came from its own wall clock, so a quasar's
speed step landed on different *ticks* in a live session with no transfer
involved (§9.1). D-21 makes the instant a function of the tick, which fixes the
live defect and largely dissolves the transfer hazard — an instant no longer
carries a process origin at all. Captures still write durations relative to the
capture tick, because that stays correct if a capture is rebased.

**The gate arrived in Phase 3, and the residue has a name.**
`TestCaptureContinuesInAnotherProcess` runs the 500-tick continuation with the two
halves in different processes, started at different wall instants and pacing their
ticks differently. It passes, and the reason it passes is worth stating rather than
enjoying: a shared *component* still carries absolute instants and a capture
carries them as they stand, which is sound only because `engine.SimEpoch` is a
build constant, so tick N is the same instant in every process of the same build.
`TestSimulationEpochIsSessionIdentity` breaks that deliberately and the
continuation diverges — so what this section called "a different clock origin" is
now a *build* property rather than a process one, and it belongs to session
identity beside the seed.

### 4.3 The digest is narrower than the state

`worldDigestScopedLocked` hashes positions, kinetics and non-cursor combat. It does
not hash genotype, quasar timers, adaptation weights, GA populations, or FSM
variables. It is a good cheap detector and must not be mistaken for a completeness
check on a snapshot.

## 5. Rule changes

The target keeps most of D-1..D-17. Three entries change meaning and two are added;
`domain-design.md` is updated as each phase lands, not in advance.

**D-11 is weakened, deliberately (landed with Phase 4).** "Identical shared
component values on every instance at every tick" becomes "identical on the host; on
a guest, equal to the host as of the last applied snapshot, and converging".
Bit-exact cross-instance agreement stops being a runtime invariant and becomes a
*test* invariant for the host's own replay. What did *not* weaken is shared entity
identity and creation order, because a capture references entities by id; the three
artifacts that create or destroy one still apply at an agreed tick everywhere. The
rule as built is in `domain-design.md`.

**D-3 keeps its shape but changes its destination (landed with Phase 4).** A
crossing artifact still names the smallest thing that determines a shared outcome,
but it is now a *request to the authority* rather than a fact every instance applies
at an agreed tick. The producer applies it in the tick it produced it for; the
playout lead survives only on the receive side, where it is an interpolation buffer.

**D-13 generalises (landed with Phase 4).** Owner-authored state stops being a
special exception to re-derivation and becomes the ordinary case for one class of
value: the owner applies immediately, the host arbitrates, everyone else receives.
What still distinguishes the D-13 components is that they are *transported* rather
than arbitrated — no instance but their owner ever computes them.

**D-18 Predicted local state (new, landed with Phase 1).** A value the local
participant's own input determines is applied locally at once. Only player-domain
producers and the view read the prediction; it emits no event and enters no
snapshot record outside `view`. An authoritative value the prediction did not
produce replaces it — the prediction is discarded, never merged. The rule as built,
with the accessors and the checks that pin it, is in `domain-design.md`.

**D-19 Restorable shared state (new, landed with Phase 2).** Every value that can
change a future shared outcome is either a component in a shared entity's store,
or declared by its owning system in `internal/manifest/definition.go` as snapshot
state and serialized through that declaration, or provably re-derivable from those
at install time. Durations are stored relative to the snapshot's tick, never as
absolute instants (§4.2). A system holding future-affecting private state without
declaring it fails the boundary suite — the same construction that made D-15's
domain profiles mechanical rather than reviewed. The rule as built, its five
declared carriers and the reason the *status surface* is in a capture for a
different reason from everything else, are in `domain-design.md`.

**D-20 Replicated triggers for shared regions (new, landed with Phase 2).** An FSM
region is shared state, so only an event every instance holds may move it. A
`ClassLocal` trigger advances the region on the one instance whose participant
produced it and nowhere else, and nothing re-derives a missing local event. Not
anticipated by this plan: it was found in the 2026-08-31 log (§9).

**D-21 Tick-derived simulation instant (new, landed with Phase 2).** The instant a
tick reads is a function of the tick number and a constant epoch, never of the
pacing clock. §4.2 predicted this would matter *for transfers*; the log showed it
already mattered *live* (§9), which is why it landed as a rule rather than as a
snapshot convention.

**D-23 The host's world is the correction (new, landed with Phase 4).** The host
publishes its world on a cadence — a whole capture, or a delta against the last
whole one — and a guest applies what arrives into a staging world and swaps it in
between two ticks. Nothing acknowledges a correction and nothing retransmits one: a
keyframe supersedes everything before it, so loss costs freshness and never
correctness. A delta is exact or it is refused, and what proves it is the capture's
own integrity hash rather than a comparison. The magnitude is measured rather than
asserted, and it is what replaced `DESYNC`. The rule as built, its four properties
and the one thing the host validates beyond structure are in `domain-design.md`.

**D-24 The cadence is a function of the link; the convergence floor is not (new,
landed with Phase 5).** The host publishes to each peer on a cadence and a
keyframe interval chosen from that link's measured round trip, its variation, the
rate bytes are arriving at, and how much that participant has at stake in the next
correction. Both are bounded, their product may never exceed
`SnapshotFloorKeyframeTicks`, and a link that cannot carry a whole world per floor
window is refused at admission and reported mid-session rather than adapted past.
The round trip lives entirely in the transport, so no timing value has to enter the
world for the cadence to exist; relevance moves the *schedule* and never the
content, because a scoped correction has nothing left for D-23's integrity hash to
be about. The rule as built, its four parts and the three places the floor binds
are in `domain-design.md`.

**D-22 An arrival is admitted before the world is read for it (new, landed with
Phase 3).** A participant joining a running session becomes a peer — receiving this
instance's crossings — before the capture it will install is taken, and holds that
traffic until the world it applies to exists. Artifacts a capture already contains
are refused rather than applied twice.

The obvious order is the other one and it loses data silently: read at tick T,
transfer, then admit, and every artifact produced in between reaches nobody. What
the correct order costs is a gap in *time* — a world read at T is installed when the
session is at T+k — and the joiner closes that by simulating the k ticks before its
own clock starts, refusing the join if what is left exceeds the playout lead. k is
a function of world size and link speed, never of session length, which is the
property that makes join-anytime possible. The rule as built is in
`domain-design.md`.

## 6. Phases

Each phase compiles, passes `make verify`, and ends with a check a person can run
from two terminals. Phase 1 is independent of everything after it and is the one a
player will feel.

---

### Phase 1 — Local-first input  ✅ landed

**Goal.** A session's local cursor and typing respond exactly as a solo run does.
This is worth shipping on its own, before any protocol work, and it is the phase
the next session should start with.

**Requirements.**

1. A predicted cell for the locally simulated cursor, plus the ordered queue of
   crossings that produced it, held in `Resources.Player`.
2. `World.LocalCursor()` returns the predicted cell. It is already the single read
   site — 26 call sites were consolidated behind it for exactly this — so no
   consumer changes.
3. Every producer of a local `EventCursorMoveRequest` (`mode.OpJump`,
   `TypingSystem.moveCursorRight`, `NuggetSystem`'s jump) advances the prediction
   at production time, through one helper, so prediction and crossing leave from
   the same statement.
4. `CursorSystem.move` announcing `EventCursorMoved` for the local cursor
   reconciles: matching the oldest outstanding prediction pops it; anything else
   clears the queue and snaps.
5. Render, camera and player-domain effects keyed to the local cursor read the
   prediction. `SnapshotContext` reports it in the `view` record only.
6. A `shared`-profile system may not read it; extend the existing static check.

**Boundaries — must not.** Change any wire message, the barrier, an apply tick, or
an event class. Predict anything but the local cursor's cell. Emit an event from
the prediction. Add a value to `SnapshotShared`. Predict for a cursor
`SimulatesLocally` rejects.

**Tests.** Promote the §3 probes to assertions: the session figures must reach the
solo figures. `TestTwoLiveParticipantsStayInLockstep*` must be untouched and still
green — this phase must not move the shared digest at all, which is the proof it
stayed inside the player domain.

**Manual acceptance.** Two terminals. Hold `w` and `l`: the local cursor tracks the
keys with no perceptible lag and no swallowed motion. Type a corpus line at full
speed: every character lands on its own cell and `typing.errors` stays at zero.
The remote cursor still moves in its six-tick sync steps — unchanged, and the
visible proof that only local state was predicted.

**Landed.** D-18 is in `domain-design.md` with the rule as built. The prediction is
a bounded queue in `Resources.Player`; `World.PushCursorMove` is its only producer
and pushes the crossing in the same statement, which is why the D-3 table resolves
that helper by name (`crossingHelpers`, pinned by
`TestCrossingHelpersPushWhatTheyDeclare`). `World.LocalCursor` and its per-cursor
form `World.CursorCell` are the read seam; `PingAbsoluteBoundsOf` follows them,
because every motion measures its step from those bounds and bounds a lead behind
the cursor accelerate a run of keypresses away from the player. A shared-profile
system reading any of them fails `TestSystemDomainProfiles`. Two things the
requirements did not anticipate: the shield, ember and ping rasterizers read their
owner's cell, so they were detaching from the cursor they are drawn around; and the
prediction is private state no record carries, so `World.PushRecord` rebuilds it
from the replayed crossing — without that, a replay resolves every player-domain
effect keyed to the local cursor against a cell the run never showed.
`TestTwoLiveParticipantsStayInLockstep*` is untouched and green.

---

### Phase 2 — The snapshot (D-19)  ✅ landed

**Goal.** Produce an object that reconstructs the shared world exactly, and prove
it by construction. Everything after this depends on it; nothing before it does.

**Requirements.**

1. **A declared contract.** `SystemDef` gains a snapshot declaration — `none` or
   `state` (implements `SaveShared`/`LoadShared`). The generator emits the table;
   the boundary suite fails a system whose declaration does not match what its file
   holds. This turns §4.1's unknown inventory into a build-time list.
2. **RNG continuation.** Add `FastRand.SetState`; replace or declare
   `WallSystem.mazeRng`.
3. **Export contracts for the two learned resources**: `AdaptationResource`
   (weights, pool, head, spin) and `GeneticResource` (per-species populations, via
   a `pkg/genetic` export/import that does not leak the mutex; since strengthened
   to include the full streaming position rather than only the archive).
4. **A versioned codec** with schema, build, config and corpus fingerprints and an
   integrity hash. References use `core.Entity`, never dense indices.
5. **Tick-relative durations only** (§4.2).
6. **Derived, not shipped**: flow field, spatial index and passability grid are
   recomputed at install; only D-17's phase travels.
7. **Player domain never imports.** No player entity, no view, audio, transport or
   effect state.
8. **Staged install**: load into a second world, validate, swap at a tick boundary.
9. **The gate is a cross-process, clock-offset round trip.** Capture at tick T,
   load in a separate process whose clock origin is deliberately offset, then
   assert identical `SnapshotShared`, identical next shared entity ID, identical
   next draw from every shared stream, and **identical shared digest after a
   further 500 ticks driven by an identical record stream**. Across the soak seeds,
   and inside storm, gold, composite destruction and reset. A same-process
   round-trip is not sufficient and must not be the gate.

**Boundaries — must not.** Send a snapshot on the wire yet, or change any live
behaviour. Load one into a running session. Serialize anything derivable. Treat
`SnapshotShared` as the format (§4.3).

**Manual acceptance.** A solo run saved at tick T and resumed in a fresh process
reaches the same shared digest as the uninterrupted run at T+500 — once mid-storm,
once across a `:new` reset. Record bytes, capture time, install time and allocation
peak at the storm high water; Phase 4's cadence is chosen from those numbers.

**Landed.** D-19, D-20 and D-21 are in `domain-design.md` with the rules as built.

*The declaration.* `SystemDef.Snapshot` is `""` or `"state"`; the generator emits
`systemSnapshots` beside `systemProfiles`, and
`TestSnapshotDeclarationsMatchImplementations` fails in both directions. The
second direction is the one that matters: a system growing save/load methods
without a manifest entry would look implemented while its state reached no
capture. Five carriers are declared — `wall`, `adaptation`, `genetic`,
`navigation`, `gold` — and the table in D-19 says what each holds.

*The capture.* `app.SharedCapture` is the object. Its world half is generated from
the manifest, so a component added to the declaration list appears in a capture
without anyone remembering to add it: 52 stores, filtered to shared entities,
referenced by `core.Entity` and never by a dense index. Around it: the allocator's
next ID and lifetime counters, every RNG stream's position, the FSM's runtime
position, each declared carrier's bytes, and the compared status surface. The
header carries the same identity set `anchorIdentity` compares, so a capture and a
replay answer "are these the same simulation" the same way; integrity is a hash
over the body with its own field zeroed. Both are checked and every carrier
resolved before anything is written.

*What the gate found.* Requirement 9's 500-tick continuation is what made this
real, and every item below was discovered by that loop failing, one at a time,
with none of them in a component store: the FSM's time in state; the gold
sequence's liveness, header and both deadlines; the genetic telemetry throttle and
its running per-type average; the shared lifetime counters and the whole shared
status surface; and the spatial gauges, republished at install because the index
is rebuilt there. §4.1's "no inventory exists" is answered by construction now,
and the loop is how the inventory was actually assembled.

*Two limits, and they are the honest ones.* The gate stops player-domain
production in both runs first. A capture carries no player state by design (D-2,
D-6), so two instances holding one shared world still hold different drains, and a
drain defeated on one advances the shared escalation FSM there and nowhere else.
That is a crossing, not a capture defect, and delivering crossings is Phase 4. And
the gate is in-process: **§6's cross-process, clock-offset, record-stream-driven
form is not yet built**, and it is the first thing Phase 3 should stand up, since
Phase 3 is what puts a capture on a wire. Sabotage checks confirm the in-process
gate is not vacuous — dropping the RNG stream restore diverges it at 300 ticks —
but the navigation route-rebuild phase is carried without a failing case that
exercises it, which is a known soft spot.

*Sizing, measured.* ~4 KB per capture at three swarms and a gold sequence; 24 RNG
streams, 5 system records, 2 FSM regions. The storm high-water figure §6 asks for
still needs taking, and Phase 4's cadence should be chosen from that rather than
from this.

*Not done in Phase 2, deliberately.* No capture goes on the wire and no live
behaviour changed (the boundaries hold). Staged install into a second world is
**not** built: `InstallShared` validates fully before writing, and resolves every
carrier first, but it writes into the live world rather than swapping at a tick
boundary. Phase 3 needs the swap and should build it there, where a joining
instance is the thing being installed into.

---

### Phase 3 preparation — deterministic run machinery  ✅ landed

**Goal.** Make the cross-process gate and repeatable two-terminal diagnosis a
supported runtime path before snapshot transport changes the session protocol.
No multiplayer authority or join behavior changes in this preparation.

`internal/journal` now owns the deterministic external-run layer rather than
only parsing its output: recording attachment/lifecycle, in-memory capture,
rotated-file loading, replay ordering and payload decode, the seeded soak fuzzer,
and a versioned authored tick-script driver. Each driver depends on a narrow
target interface and never imports `internal/app`; App retains construction,
anchor/config verification, terminal playback, and session startup.

`cmd/vif -script <file>` constructs a caller-driven `ModeHeadless` App. Schema 1
scripts declare a hard tick budget, optional geometry, and ordered actions at a
completed `(run, tick)`: a canonical semantic intent, text, an ex command, or a
registered event with journal-compatible TOML payload. Event domains are derived
from D-10 where unambiguous; a Stamped event must state its domain. Same-position
actions preserve file order and settle separately.

The existing `-host`/`-join` gate is available to this headless path. Two
processes may run different scripts, and their manual clocks are wall-paced at
the 50 ms game interval after the ready gate so one cannot race ahead of the
socket peer. `script/phase3-host.toml` and `script/phase3-guest.toml` are the
checked-in 2,000-tick pair: separate heat bursts, local motion on each side, one
crossed quasar, a swarm request inside the host shield, and a shared storm
request injected on both.
With `-j`, the result is an ordinary journal of the resulting non-system events,
not a second script format.

**Boundaries kept.** A script does not inspect or assert world state, carry a
capture, compare processes, stage an install, or permit a mid-run join. Networked
headless execution is intentionally real-time; single-process scripts and the
seeded fuzzer still run flat out. The cross-process capture continuation remains
a Phase 3 gate, but it now has a reproducible process/tick/input driver instead of
depending on terminal choreography.

---

### Phase 3 — Join anytime  ✅ landed

**Goal.** Deliver the original requirement: a running solo game can be toggled into
a host, and a participant can arrive at any moment.

**What was built.**

`:host <addr>` opens a run that is already playing. The port is created, started
and attached; the world latches as shared (D-14) and the crossing barrier takes
ownership of this instance's artifacts from that tick. `:session` reports the role,
address, participant identity, peer count and tick. A run started with `-host`
still uses the tick-zero lobby, and both paths now hand a joiner the same thing.

**The capture travels.** `MsgStateSnapshot` takes the code the retired
replay-from-tick-zero join reserved. It is the one message whose size is a function
of the world rather than of the format, so it is the one that is chunked, under a
header naming the tick, the piece, the count and the whole body's length —
`SnapshotAssembly` refuses a skipped predecessor, a frame from another capture, a
truncated frame and an empty body, each of which would otherwise install a world
that looks installed and is not.

**The install is staged.** `App.StageShared` resolves the whole capture into a
*second world* — this build's system set, its FSM, its RNG stream inventory — and
`Commit` then writes the same bytes into the live world. `World.RunSafe` holds the
update mutex and a tick runs entirely inside one acquisition of it, so the swap is
between two ticks by construction rather than by a scheduler handshake. What
survives the staging pass is what the live pass cannot fail on: identical code,
identical input, no dependence on the state being written over.

**A joiner receives the world instead of reproducing it.** `JoinSessionAt` adopts
the map latch, settles the FSM boot queue (which is what declares the cursor
template a late arrival is armed from), stages and commits, and then applies only
the D-13 control assignment. Its own cursor is *not* in the capture — it arrives as
the `EventParticipantJoined` crossing, at one agreed tick, on every instance.

**The ordering is the design, and it is D-22.** A joiner is admitted as a peer
*before* the world is read for it, so the crossings the host produces during the
transfer reach it rather than falling into the gap between the capture and the
admission. It holds that traffic while it reads its gate and its capture, hands it
to the port once the transport owns the stream, and the barrier drops the artifacts
the capture already contains. Reading first and admitting second loses every
artifact produced in between, silently, which is the failure this plan exists to
stop having.

**The gap that is left is time, and it is closed rather than tolerated.** A world
read at tick T is installed some milliseconds later, by which point the session is
at T+k. Left open, k is permanent and every crossing the new participant produces
arrives k ticks late. `resumeJoinedSession` simulates those k ticks before the
joiner's own clock starts, learning the target from the epochs the session closes —
every tick closes one, empty or not — and refuses the join if what remains exceeds
the playout lead. k is a function of world size and link speed, never of session
length.

**Reconnect is that path a second time.** Nothing about it is reconnect-specific:
the departure crossing returns the identity to the pool, and the next dial gets the
same acceptor, the same allocation, a capture at whatever tick the host has now
reached, the same install and the same arrival crossing.

**What the boundaries cost.** The host is never paused: the only stall is the
capture's own read under the world lock, and that is measured (§8, question 2). No
record stream is retained — the frames a joiner holds are the transfer window's
epochs and nothing older. No coordinator is elected and host loss still ends the
session.

**The five items Phase 2 left, and what happened to each.**

1. **The staged install** — built, as above. Building it is what exposed two
   things an install must *re-derive rather than adopt*: the slot→entity roster,
   which mirrored the cursor store and named destroyed entities after an install;
   and a cursor's control assignment, which travels inside a shared component, so
   a receiver that adopted it would start simulating the sender's cursor and stop
   simulating its own. Both are D-13 and both fail two join tests when removed.
2. **The capture carrier and cross-process gate** — both built.
   `TestCaptureContinuesInAnotherProcess` runs Phase 2's 500-tick gate with the two
   halves in *different processes*: the capture is bytes on a disk, the two start
   at different wall instants, and the receiver paces its five hundred ticks in
   bursts while the sender ran them in one go. Beside it,
   `TestSimulationEpochIsSessionIdentity` breaks the reason it works — a receiver
   on a different `SimEpoch` installs the same bytes and diverges — which is the
   honest statement of what §4.2's hazard became.
3. **The gold spawn-tick defect** — closed. A joiner no longer reproduces the
   session, so nothing is a tick early; the gold carrier writes both deadlines
   relative to the capture's tick and the origin is the host's everywhere.
   `gold.timer` is back in the compared surface and
   `TestSnapshotJoinCarriesTheGoldDeadline` is what holds it.
4. **The storm high-water sizing** — measured. §8, question 2 carries the numbers.
5. **A failing case for the navigation phase** — built, and it found two defects
   first. See below.

**What Phase 3 found that was not on the list.**

- *The navigation carrier preserved a phase the next tick destroyed.* The install
  left the flow field underived, so the first tick took `FlowFieldCache.Update`'s
  `!Field.Valid` branch: derived from that tick's targets rather than the ones the
  restored phase belonged to, and zeroed `TicksSinceCompute` on the way. It also
  left the composite passability grid computed from the walls the install had just
  replaced. Both are re-derived by `LoadShared` now, the field from `LastTargets`,
  which is also what makes it the field the sender held. D-19's "re-derivable at
  install time" is now "re-derivable **by** the install", which is a different and
  stronger claim.
- *Phase 2's own 500-tick gate was weaker than it read.* It spawned three swarms
  and then advanced to the next status-cadence boundary, which can be nearly a
  whole cadence away; the escalation FSM swept in the meantime and the capture the
  comment claims carries species carried none. The species are spawned after the
  advance now and asserted alive — the capture grew from four kilobytes to
  seventeen, which is the measure of what the gate was not exercising.
- *`route_rebuild_ticks` is still uncovered.* It paces one gateway route graph
  rebuild per interval and the shipped scenario builds no gateways, so a sabotaged
  value changes nothing observable. A scenario with gateways is what would close
  it; it is on Phase 4's list.

**Boundaries kept.** No retained record stream; no host pause beyond the measured
capture read; no coordinator election and no survival of host loss.

**Manual acceptance.** §10.

---

### Phase 4 — Authority and correction  ✅ landed

**Goal.** The host becomes the authority and guests become predictors. This is
where divergence stops being a failure mode and becomes a routine, corrected
condition.

**What was built.**

*The host publishes.* A run that is hosting takes a capture every
`SnapshotCorrectionTicks` — 5 Hz — on a goroutine of its own rather than on the tick
loop: the read is one acquisition of the world lock (1.6 ms at the storm high water,
3.2 % of a tick) and the encode, the diff and the chunking hold no lock at all.
Every `SnapshotKeyframeCorrections`th correction is a whole capture; the rest are
deltas against it.

*The delta is generated, and it is exact.* `SharedWorldDelta` is emitted from the
manifest beside the capture, so a component added to the declaration reaches a
correction without anyone remembering to add it. Applying one to the baseline it
names reproduces the sender's capture **byte for byte** — entity order included —
and what says so is the capture's own integrity hash rather than a value comparison:
a delta that rebuilt an equivalent world in a different store order passes every
value check and fails that hash, which is why the delta carries order at all. A
receiver holding a different baseline refuses the delta and waits for the next
keyframe, which is a bounded wait by construction.

*The measurement is the answer to §8's question 2.* At the storm high water a
keyframe is 175,908 bytes and a delta is 29,488 — 16.8 % — so the cadence's uplink
with one keyframe in ten is **215 KiB/s at 5 Hz and 86 KiB/s at 2 Hz**, against 859
and 344 for full snapshots. The 2–5 Hz hypothesis holds at the load this game
actually reaches, and it holds *because* of the deltas rather than in spite of the
storm.

*A guest predicts.* The playout lead came off the local path: a producer applies its
own crossing in the tick it produced it for and sends it, and the peers keep the
lead as an interpolation buffer for remote action. Three artifacts are exempt and
the exemption is D-11's — an arrival, a departure and a reset create or destroy a
shared entity, and identity is what a capture references by, so those still apply at
one agreed tick on their producer too.

*A correction is a clock as much as a world.* D-21 makes every stored deadline a
function of the tick, so installing a capture means adopting the tick it describes;
a guest that had extrapolated past it is re-based back by the transfer's latency.
That is bounded, measured, and reported: `network.lag_ticks` every tick, with
`network.stale` past the playout lead. Re-simulating the gap forward — rollback and
*replay* — is Phase 6's bounded-rollback entry, and until then the guest's own
un-arbitrated artifacts are erased by a correction and restored by the one after it.

*`DESYNC` and `DIVERGED` are gone.* Not renamed: removed, with `network.sync_state`,
`network.sync_part`, `network.sync_records` and `network.diverged`. A guest is
expected to differ from the host between corrections, so an escalation had nothing
left to be right about. The digest survives as a gauge — `network.digest_mismatches`
with `network.drift_part` — and the status bar shows `COR n`, the size of the last
correction, and `LAG n`, the staleness. The per-record breakdown is no longer
requested on the wire, because a guest disagrees by design and asking for detail
would mean sending a map of per-record hashes for the whole session.

**The six things Phase 3 left, and what happened to each.**

1. **The staging world** — built once and re-used. `Commit` stopped being a second
   full write: it reconciles the live world onto the capture rather than clearing
   and re-inserting it, so what it writes is the size of the correction. Measured on
   one receiver at the storm high water, the first install (a join) costs 11.6 ms
   staging and 6.9 ms committing; the second (a correction) costs 3.0 and 2.9.
   `TestStagingWorldIsBuiltOnceAndReused` is what says re-use leaves what a fresh
   world would, and `TestReconcileMatchesAFullInstall` that the reconciled world and
   a fully re-installed one agree — then and sixty ticks later.
2. **Deltas** — built, measured above.
3. **The gap a guest can be behind** — measured every tick now, as
   `network.lag_ticks`/`network.stale`, and turned into requirement 6's staleness
   indicator. `network.join_lag_ticks` still reports what the join itself closed.
4. **A guest predicts rather than re-derives**, and the code says so: D-11 is
   weakened in `domain-design.md`, the lockstep criteria became convergence criteria
   (`TestTwoLiveParticipantsConvergeOnCorrections`, `TestGuestConvergesOnEveryCorrection`),
   and each of them asserts that the two really did disagree in between — a
   criterion a guest could pass by never predicting anything proves nothing.
5. **The route-rebuild phase has a failing case.** The tower region is the only
   scenario any shipped config has that attaches route-graph gateways, so that is
   what the case runs; a zeroed budget rebuilds on different ticks than the sender
   within 22, and the unmodified capture rebuilds on the sender's for 200. Building
   it found two things — see below.
6. **A join no longer reads the world for itself.** It takes the cadence's most
   recent keyframe when one is fresh enough, and only reads when none is, so two
   joins arriving together share one acquisition of the world lock. The gate still
   runs on the accepting goroutine, which the plan already called bounded and
   deliberate at `MaxPlayers`.

**What Phase 4 found that was not on the list.**

- *A window D-22 did not close.* An epoch produced **before** a joiner was admitted,
  and flushed to the peers the host had at that moment, reaches the joiner not at
  all — and a capture taken at the admission tick does not contain it either,
  because its apply tick is still a playout lead ahead and the barrier's floor does
  not drop it. A join asks for a world a lead further on now, which closes it by
  construction and costs three ticks.
- *The install did not re-derive gateway route graphs.* D-19 says derived state is
  re-derived **by** the install; the flow fields and the passability grid were, and
  the route graphs were not — so a receiver kept the graphs its own run had built,
  aimed at cells the sender's were not and present for gateways the sender has none
  for, while the installed `NavigationComponent`s named route indices into them.
  `LoadShared` clears the resource and rebuilds every graph the capture named now,
  from the source and target cells it named. It is exactly the defect Phase 3 found
  in the passability grid, one layer up, and only a scenario with gateways could
  show it.
- *A correction chunk reaching a participant mid-handshake.* The host broadcasts to
  every peer it has, and D-22 makes a joiner one before the world is read for it, so
  correction chunks arrive interleaved with the gate. They are swallowed there:
  there is nothing to keep, because the participant is about to install a whole
  world.
- *The genetic carrier restored an archive and not a position.* Phase 4 recorded
  rather than hid it: `Registry.Export` carried members and generation, while the
  streaming engine also owned its PCG position, pre-produced offspring, pending
  evaluations, partial-generation phase and next ID. The registry's scout PCG and
  counter, plus `GeneticSystem`'s live fitness accumulators, were adjacent state the
  original report did not list. The Phase 4 cleanup below closes the complete set;
  the gateway gate now leaves spawning enabled and compares every resulting
  genotype tick by tick.

**Boundaries kept.** A guest's derivation never overrides the host: a correction is
installed whole and the guest's own drift is discarded, never merged. The host's
uplink does not scale with entity count at tick rate — it scales with the *delta* at
5 Hz, which is the point. Solo replay is untouched: `PushRecord` bypasses the wire
sink, and the local path publishes at production tick in a run and in its replay
alike, which is one fewer difference between the two than before.

**Not done in Phase 4, deliberately.** The cadence is a constant rather than a
function of the link — that was Phase 5's whole subject, and Phase 5 has since
landed it. There is no bounded
rollback, so a guest's own un-arbitrated artifact is erased by the correction that
predates it and restored by the one after: a bounded flicker of about one cadence on
shared entities, never on the local cursor, which D-18 predicts privately and no
capture touches. Nothing authenticates a peer, so the host's validation is
structural (the coordinator is the only producer of a roster change) rather than
adversarial; authentication is Phase 6's.

**Manual acceptance.** §10.

---

### Phase 4 cleanup — exact genetic continuation  ✅ landed

**Goal.** Make an installed world issue the same next genotype as the authority,
instead of restarting from the same scored archive at a different stream position.

`pkg/genetic` now separates two contracts. `Snapshot`/`Inject` remain
archive-only persistence. `StreamingEngine.Checkpoint`/`Restore` carry the complete
continuation point: the `math/rand/v2` PCG binary state, scored archive, FIFO
proposal ring, pending evaluations, partial-generation outcome count, next ID,
eviction count, running state and normalized configuration. The state is generic
plain data, the root package still imports only the standard library, and restore
validates the whole value before changing an engine.

Seeded output no longer depends on how much work fit inside a wall-clock window.
Deterministic full refill is the default; the old time-budgeted behavior survives
as explicit `RefillTimeBudget` opt-in for callers willing to trade reproducibility
for a wall-time cap. Seed zero is a valid deterministic seed, not an implicit clock
request.

The registry composes that contract with the scout PCG position and bin counter,
and requires the complete registered species set on import. The game carrier also
carries live per-entity fitness accumulators and pending deaths, which affect the
scores later returned to those restored pending evaluations. Snapshot schema 2
marks the stronger carrier.

**Gate.** Package tests JSON-round-trip a checkpoint with queued and pending work,
restore it over a deliberately different engine, then compare 250 exact proposal
and ID transitions. The registry gate interleaves ordinary and scout samples for
another 180 transitions. `TestGeneticContinuationSurvivesAnInstall` runs the tower
gateway world with spawning enabled for 200 ticks after an install and compares the
captured genotype store on every tick; the old archive-only carrier failed there
within ten.

---

### Phase 5 — Adaptive cadence and bandwidth resilience  ✅ landed

**Goal.** Make the system degrade gracefully rather than fail at the edge of the
link's capacity — the explicit priority.

**What was built.**

*The protocol makes a round trip now, and it never touches the game.* This is the
number §10.4 said was missing by construction: everything the session measured
was one-directional, so a cadence had a constant and nothing else to be chosen
from. `MsgLinkProbe` leaves every 200 ms per peer and `MsgLinkEcho` answers it
**inside the receiving port**, before the frame could reach a tick — so what it
measures is the wire rather than how often an instance runs a tick, and the world
never has to hold a timing value for the cadence to exist. The world publishes one
opaque 25-byte `LinkReport` (its tick, its staleness, its last correction
magnitude, its cursor cell) and reads back one estimate it may schedule transport
from. Both directions are copies. `TestLinkMeasurementNeverEntersTheComparedSurface`
is the criterion, over a shaped link.

*Three numbers come out of it, and the third is the one that is easy to get
wrong.* Round-trip time and its variation come from the probe's own timestamp,
returned untouched, so neither end has to agree with the other about what time it
is. The delivery rate comes from two consecutive echoes' byte counts. And the
**backlog** — what this end has queued against what the far end says arrived — is
what separates a fast link from an idle sender: a rate measured while nothing was
queued is a lower bound on capacity and not a measurement of it, so the estimator
reports `Saturated` beside it and a controller given an unsaturated rate keeps its
nominal point. Without that distinction every quiet moment reads as a narrow link
and the session throttles itself for having nothing to say.

*The controller is a leaf package.* `pkg/linkpace` is the estimator and the
bounded controller, standard library only, no `internal` in sight — which is what
makes "network timing may not enter the simulation" structural rather than
remembered: the package cannot see a world, an event or a component. It takes
measurements, measured correction sizes and a demand, and returns a cadence and a
keyframe interval inside a declared envelope. Under pressure the keyframe interval
stretches before the cadence slows, because a keyframe costs six times a delta on
this world and stretching it spends recovery time the floor already bounds rather
than freshness a player sees. Degradation is immediate and recovery is stepped.

*Per-peer, on one timeline.* Each direct link gets its own controller, because two
participants on one host can be a LAN cable and a phone. What is **not** per peer
is the correction: it is still computed once against one baseline and is still
exact. The session's base cadence is the fastest peer's and its keyframe period is
the *longest* any peer planned, capped by the floor — longest because a keyframe
is the expensive frame and every peer has to hold the one a delta names. A peer
receives a delta when its own cadence says it is due, and a keyframe always.

*Relevance and priority move the schedule, never the content.* A participant whose
share of what the next correction moves stands above the session's mean is served
first and published to faster; one with nothing near it and a prediction the last
correction did not have to move settles at the quiet cadence, which is what pays
for the first. Scoping a correction's *content* is not what was built, and the
reason is D-23: a delta is proved by reconstructing the sender's capture and
re-checking its integrity hash, so a correction carrying a subset reconstructs a
capture nobody holds and has no proof left to offer — and it would hand a receiver
a world assembled from two ticks. That is Phase 6's, with its own integrity
contract, if the bytes turn out to be worth it.

*The floor is the part that is not adaptive.* `SnapshotFloorKeyframeTicks` is 60 —
three seconds, 1.5× the nominal keyframe period. Cadence times keyframe interval
may never exceed it, and the controller's search space is bounded by it *before*
capacity is consulted, so no plan it can return violates it. A link that cannot
carry the cheapest schedule that honours it is refused at admission, measured from
the join's own transfer, which is the only rate available before a probe has
completed a round trip. Mid-session it is reported from both ends and they are
different claims: the host says its link cannot carry a whole world per window,
and the guest says one did not arrive.

*The operating point is on screen.* `snapshot.cadence` carries the cadence, the
keyframe interval and period, the planned uplink, the measured budget and what the
floor costs; `network.link` carries the round trip in milliseconds and
microseconds, the jitter, the rate, the probe loss and the saturation flag.
`:session` prints the set. The status bar draws two conditions differently on
purpose: `LNK 120±14ms 8x7 32K` is the design working, and `LINK!` is a link that
cannot keep the guarantee.

**Measured, on the checked-in `script/phase5-*.toml` pair over a real socket.**
3,000 host ticks with a mid-run join at 400, a storm from 900 and a swarm at
1,800:

| | value |
|---|---|
| corrections published / keyframes | 560 / 69 |
| uplink over the session | 46.5 MB, ≈358 KiB/s at the storm high water |
| host stall per capture | 1.51 ms — 3.0 % of a 50 ms tick |
| cadence in force | 4 ticks, with excursions to 2 when drift rose |
| keyframe period | 40 ticks, against a 60-tick floor |
| correction magnitude on the guest | 36–530 entities, no upward trend |
| shared placements a correction visibly moved | 1–7 |
| corrections refused / superseded | 0 / 0 |
| longest the guest waited for a whole world | 15 ticks |
| `WARN`/`ERROR` | one on the host — its own shutdown — none on the guest |

**What Phase 5 found that was not on the list.**

- *An absolute correction magnitude is a threshold about the world, not about the
  participant.* The first live run pinned the cadence at its 10 Hz minimum for the
  whole storm and spent 1.2 MB/s doing it. The cause is in the table above: a
  correction at the storm high water moves ~500 entities over ~492 shared
  placements, so a fixed magnitude fires permanently on a condition that is simply
  what a storm looks like. What says the cadence is falling behind is the **rise**
  above that participant's own recent level, and relevance is a comparison against
  the session's mean for the same reason. Both are percentages now.
- *"Constrained" meant "not nominal", which put a warning on the status bar of the
  participant being served best.* A plan the demand pulled *faster* is the opposite
  condition. It means "worse than nominal" now.
- *A probe interleaving with the join handshake failed the join.* D-22 admits a
  participant before the world is read for it, so the stream is a peer — and
  therefore probed — while the gate is still reading raw frames off it. The gate
  answers a probe now rather than choking on it, which also keeps the transfer from
  scoring as loss on the link it is the best measurement of.
- *The byte counters a backlog is derived from have no shared origin.* This end
  starts counting when it accepted the stream, the far end when its port took the
  stream over — for a mid-run join, a whole capture apart. Measuring the absolute
  difference left a standing backlog the size of the installed world, and the link
  read as permanently saturated for the rest of the session. The meter measures
  growth from a re-based origin.

**Boundaries kept.** Local input is untouched; host authority is untouched; the
relay, mid-run join, reusable staging world and exact genetic continuation are
untouched. No network timing reaches shared simulation state, an RNG stream, a
replay or a game decision — `pkg/linkpace` cannot see any of them, the probe is
answered in the transport, and the compared surface is asserted clean over a
shaped link. Adaptation never reaches a rate at which convergence is not
guaranteed: the floor bounds the search, and a link below it is refused or
reported.

**Not done in Phase 5, deliberately.** Relevance does not scope a correction's
content, for D-23's reason above. Per-peer cadence is a property of a *direct*
link — a participant reached by relay rides its neighbour's schedule, because the
flood forwards what that neighbour was sent, and in the shipped star topology
every participant is direct. The playout lead is still a constant: it decides the
tick an artifact applies at, so making it adaptive is a protocol change rather
than a scheduling one. And the status bar reports the *worst* link, which is right
for the host and misleading for a well-connected guest in a session with a badly
connected one; `:session` names the link.

**Manual acceptance.** §10, and `script/phase5-linkshape.sh` for the staged form.

---

### Phase 6 — From evidence  ← next

Not committed, and every entry now has a reason it is next rather than a guess.

- **Bounded rollback *and replay*.** Installing a correction adopts the
  authority's earlier capture tick, so a guest is re-based backwards by the
  transfer's latency and its own outstanding artifacts are erased by the
  correction that predates them and restored by the one after. Phase 5 measured
  what that costs — 1 to 7 shared placements visibly moved per correction on the
  shipped scenario — and made the cadence that bounds it adaptive, which is as far
  as scheduling can take it. Removing the flicker needs the replay: re-simulating
  the gap forward with this participant's own retained artifacts. The
  deterministic capture contract is the state prerequisite and exists; a retained,
  canonical input suffix is the one that does not.
- **A content-scoped correction.** Phase 5 deliberately kept relevance to the
  schedule, because a correction carrying a subset of the world has nothing left
  for D-23's integrity hash to be about and would hand a receiver a world
  assembled from two ticks. Doing it properly needs an integrity contract over the
  subset and a partial reconcile that does not adopt the authority's tick. Whether
  it is worth it is a bandwidth question the Phase 5 telemetry can now answer:
  `snapshot.cadence_uplink_bps` against `cadence_budget_bps` says how often the
  schedule is actually the constraint.
- **Authentication** (`Config.TLS`, `MsgAuthRequest`/`Response`) before anything
  beyond trusted peers. Phase 5 added two message types a peer can send freely —
  a probe and an echo — and the echo carries a report a host schedules from. A
  hostile peer can therefore ask to be published to more often. It cannot make the
  session believe anything about the world, and the floor and the bounds cap what
  it can extract, but it is one more reason the trust boundary is where the next
  work is.
- **Host migration and partition health**, as a separate project with its own
  prerequisites: transferring the newest authority, the membership and the
  in-flight admission state before electing a replacement.
- **Multi-link topology from the CLI.** `-join` dials one address, so the links
  are a star. The relay makes any graph work, and Phase 5's per-peer cadence is a
  property of a *direct* link — a relayed participant rides its neighbour's
  schedule — so a real graph is also what would make that limitation visible.
- **An adaptive playout lead.** Phase 5 left `NetworkBarrierDelayTicks` a constant
  on purpose: the lead decides the tick an artifact applies at, so changing it
  mid-session changes an agreed apply tick on every instance. It is a protocol
  change rather than a scheduling one, and the round trip Phase 5 added is the
  measurement it would be driven from.

## 7. Cleanup already performed

This document's branch removed what the target architecture will not use, so the
implementation starts clean:

- **The replay-based mid-run join is gone** — `App.CatchUp`, `SessionLog`,
  `SessionLogChunks`, `event.EncodeSessionLog`/`DecodeSessionLogChunk`,
  `LogRecord`, `MsgSessionLog`, `PendingJoin.MidRun`/`ReceiveSessionLog`, the
  handshake's log leg, `NetworkSystem.DiscardArtifactsThrough`, `event.MultiSink`,
  the `RetainSessionLog` config flag and the `HostAddress` retention clause, plus
  their tests. It was unreachable from `cmd/vif`, cost unbounded memory, and is
  replaced by Phase 3. **The journal, `Capture`, `ReplayDriver` and solo
  deterministic replay remain intact** (now organized under `internal/journal`)
  and remain valuable for debugging.
- **`World.LocalCursor()`** consolidates 26 copies of the local-cursor read across
  `mode`, `app`, `engine` and four player-profile systems. It is the seam Phase 1
  installs behind.
- **`internal/mode/router.go` is 64 lines shorter**: `recordCommand`,
  `applyOperator`, `rememberFind` and `charCells` name four repeated concepts.
- **The fabricated-identity admission path is gone.** `PeerManager.AddConnection`
  assigned a connection-local `nextID` to any stream accepted without a session
  handshake; participant IDs key the barrier's epoch window and every roster
  lookup, so it would have admitted a peer under an identity the session never
  issued. Both call sites now fail loudly.
- **The wall batch pool was write-only** — released into, never acquired from.
  Removed.
- Dead on arrival: `network.NewAckMessage`, `event.EmitBatch`, `event.HasPayload`,
  `Hub.Get`, `Hub.Names`, the `RoleClient`/`RoleServer` aliases.
- `NetworkSystem` telemetry reset is table-driven, so a counter added to the
  constructor cannot survive a reset holding the previous run's value.
- `pkg/genetic` no longer conflates learned-archive persistence with exact stream
  continuation. The latter is a versioned, standard-library-only checkpoint, and
  the game carrier consumes it rather than reaching through package locks.

## 8. Open questions

1. **Prediction scope in Phase 1.** Cursor cell only, or also the visual removal of
   a typed *shared* gold member? The conservative answer is taken here.
2. **Snapshot cadence — measured, and the answer changes the design rather than
   just filling in a number.** `TestSnapshotCostAtTheStormHighWater` reports it.
   At rest this world holds 12 shared entities and a capture is 11 KiB; at the
   storm high water it holds 492 and a capture is 176 KiB — sixteen times.

   | | quiet | storm high water |
   |---|---|---|
   | shared placements | 12 | 492 |
   | capture bytes | 10,891 | 175,910 |
   | read under the world lock | 1.3 ms | 1.2 ms |
   | encode (outside the lock) | 0.04 ms | 0.43 ms |
   | joiner stage / commit | 9.6 / 6.1 ms | 12.4 / 4.1 ms |
   | allocated per capture | 531 KiB | 1,032 KiB |

   The host stall is the reassuring half: 1.2 ms is 2.4% of a 50 ms tick, so the
   read is affordable at any cadence this plan contemplates. The link is the other
   half: *full* snapshots at the high water are 859 KiB/s at 5 Hz and 344 KiB/s at
   2 Hz. The 2–5 Hz hypothesis holds comfortably at rest (54 KiB/s at 5 Hz) and
   does not hold at the high water without the deltas Phase 4's requirement 3
   already plans for. That is the finding: the cadence question was never only a
   rate, and the storm is what says so.

   **Answered, with the deltas built.** `TestCorrectionCostAtTheStormHighWater` is
   the second half of the measurement, taken on the same world one cadence apart:

   | | storm high water |
   |---|---|
   | keyframe bytes | 175,908 |
   | delta bytes | 29,488 (16.8%) |
   | component cells the delta moves | 529, over 492 shared placements |
   | diff / apply (outside the lock) | 0.71 / 0.90 ms |
   | first install into a receiver (a join) | stage 11.6 ms / commit 6.9 ms |
   | second install into the same one (a correction) | stage 3.0 ms / commit 2.9 ms |

   With one keyframe every ten corrections the uplink is **215 KiB/s at 5 Hz and
   86 KiB/s at 2 Hz**. So the hypothesis holds at the load this game actually
   reaches, and the second pair of install rows is the other half of what Phase 3
   left on the doorstep: the staging world is built once, and the commit reconciles
   rather than replaces, which is why a correction costs a third of what the join
   before it did.

   **And the answer is a range now rather than a number**, which is Phase 5's
   whole subject. 5 Hz is the *nominal* point; what a peer receives is chosen from
   its own link inside 10 Hz and 1 Hz, and the keyframe interval inside 1 and 30 —
   with the product bounded by a 60-tick convergence floor. Measured over the
   Phase 5 socket pair the session sat at the nominal 4 ticks with excursions to 2,
   for 46.5 MB over 560 corrections at the storm high water: ≈358 KiB/s, and the
   host stall stayed at 1.51 ms, 3.0 % of a tick. What the earlier figures did not
   say and these do is what happens when the link *cannot* carry that, which is
   §6's Phase 5 entry.
3. **Host loss** ends the session. Confirm that is acceptable before sizing Phase 6.
   Phase 5 adds one number to the same decision: the guest now measures the
   authority's absence directly (`snapshot.cadence_keyframe_age_ticks`) and says so
   when it passes the floor, so a session that has lost its host is distinguishable
   from one whose link has merely narrowed — which is exactly the distinction a
   migration would have to make before electing anything.
4. **Does the host keep re-deriving, or become the only simulator?** This plan
   keeps guests simulating (§2.1), and Phase 4 kept it. The magnitude is published
   now (`snapshot.correction_entities`), so the question has an instrument rather
   than an argument: if it turns out large enough to be distracting, the fallback is
   thinner guests and a higher snapshot rate — a tuning change, not a rewrite.

   **Phase 5 read the instrument and the answer is "keep simulating", with one
   caveat worth recording.** On the shipped scenario a correction moved 36–530
   entities with no upward trend, and the *visible* part of that —
   `correction_entities` against `correction_cells` — was 1 to 7 shared placements.
   So the prediction is close where a player looks and loose in the bookkeeping,
   which is the shape this design wants. The caveat is that the magnitude in a
   storm is essentially the whole shared population every cadence, which made an
   absolute threshold on it useless as a control signal (§6, Phase 5's findings).
   A magnitude is a good gauge and a bad thermostat.
5. **Tower ownership in optional maps** still binds every tower to slot zero. Not a
   blocker; a gameplay rule to settle before towers appear in a real session.
6. **A hostile peer can ask to be scheduled, though not to be believed.** The
   Phase 5 echo carries a report the host schedules from — a participant's
   staleness, its correction magnitude and its cursor cell — and nothing
   authenticates it. What that buys is a faster cadence for the peer that lies,
   bounded by `SnapshotCadenceMinTicks` and by whatever its own link actually
   carries; it buys no influence over the world, because relevance moves the
   schedule and never the content. It is recorded here rather than fixed because
   the fix is authentication, which the plan already puts before anything beyond
   trusted peers.

7. **How many participants may join a run that started solo.** `:host <addr>`
   opens a lobby sized by `-players`, which a solo run never set, so it admits one.
   Giving the command its own count is a one-line change and nobody has needed it
   yet; it is recorded here so the limit is a decision rather than an oversight.

## 9. What each session showed

### 9.B The Phase 5 script pair — the cadence, chosen rather than fixed

`script/phase5-host.toml` and `script/phase5-guest.toml`, run as §10.1 describes:
3,000 host ticks, a mid-run join at 400, a storm from 900 and a swarm at 1,800, so
the cadence has something to actually decide. Recorded because two of the numbers
in it changed the implementation rather than confirming it.

- **The cadence settled at its nominal 4 ticks with excursions to 2**, keyframe
  interval 10, keyframe period 40 ticks against a 60-tick floor. On loopback the
  link is never the constraint, so what is being read here is the *demand* half of
  the controller working correctly: the excursions coincide with the storm's
  arrival and the swarm's, and it returns.
- **560 corrections, 69 keyframes, 46.5 MB** — ≈358 KiB/s at the storm high water,
  against §8's 215 KiB/s prediction for a quieter world at the same nominal
  cadence. The capture read stayed at **1.51 ms**, 3.0 % of a tick, which is the
  reassuring half again.
- **The magnitude is bounded and does not trend**: 36 to 530 entities over the
  run, with `correction_cells` — shared placements a correction visibly moved —
  between 1 and 7. `corrections_refused` and `corrections_superseded` were both
  **0** for the whole session.
- **The guest never waited more than 15 ticks for a whole authoritative world**,
  against a floor of 60. `cadence_floor_breached` stayed false on both halves.
- **No `WARN` or `ERROR` on the guest at all**, and one on the host, which is its
  own shutdown.

*The first run of this pair did not look like that, and what it found is the entry
worth keeping.* The cadence sat pinned at its 10 Hz minimum for the entire storm
and spent 1.2 MB/s there. The cause is in the numbers above: a correction at the
storm high water moves ~500 entities over ~492 shared placements, so an absolute
threshold on the correction magnitude fires permanently — the "urgent" rule was
reading *the world is busy* as *the cadence is falling behind*. It is a rise above
the peer's own recent level now, and relevance is a comparison against the
session's mean for the same reason. A magnitude is a good gauge and a bad
thermostat, and only a run at the load the game actually reaches says so.

The same run found the join gate choking on a probe — D-22 makes a joiner a peer
before its world is read, so it is probed while still reading raw frames off the
stream — and, once that was fixed, a standing backlog the size of the installed
world, because the two byte counters a backlog is derived from start at different
moments on the two ends. Both are in §6's Phase 5 findings.

### 9.A The Phase 4 script pair — the correction, in two processes

`script/phase4-host.toml` and `script/phase4-guest.toml`, run as §10.1 describes:
the host starts solo, opens hosting at tick 400, and the guest dials in and runs
1,600 ticks against it. Recorded because it is the first evidence that the cadence
works outside a harness, and because one number in it is the phase's whole claim.

- **The guest applied 371 corrections and refused none.** `corrections_refused` and
  `corrections_superseded` both stayed 0, so every delta found the keyframe it named
  and nothing arrived faster than the guest could take it.
- **The magnitude is small and bounded.** Sampled at three points across the run,
  `correction_entries` read 10, 9 and 6 over `correction_entities` of 5, 5 and 4,
  with `correction_cells` at 0 — the prediction was never more than a handful of
  component cells out and no shared placement was ever visibly moved by a
  correction. Bounded rather than zero is the right answer: zero would mean the
  guest was not predicting anything.
- **No `WARN` or `ERROR` on the guest at all**, and one on the host, which is its
  own shutdown. `network.lag_ticks` read 0 and 1; `stale` never set.
- **`digest_mismatches` reached 17 over ~1,600 ticks**, naming `positions` and then
  `kinetics`. That is the drift *between* corrections, which is the condition this
  phase created deliberately, and it is exactly what the counter is now for.
- **`gold.timer` matched exactly, tick for tick** — 1,100,000,000 at tick 2000 on
  both files. That is the key Phase 2 had to exclude and Phase 3 admitted; it now
  survives a session in which the two instances are not in lockstep at all.
- **The `fsm` sequences agree where the two logs overlap.** Not tick for tick any
  more, and §10.3 says why: each instance applies its own artifacts a playout lead
  before the other sees them.
- **The host's uplink was 4.2 MB over 402 corrections**, about 10.5 KiB each, in a
  world whose whole capture is 9.7 KiB. That is the honest shape of the delta in a
  *quiet* world: it compresses the world half, which is a small share of a small
  capture, so most of what remains is the fixed sections. The delta earns its keep
  where the world is large — 16.8 % at the storm high water — which is where it is
  needed and where §8's table says the constraint actually is.

### 9.0 The 2026-09-01 run — the D-20 and D-21 fixes hold

Two participants, ~2,930 ticks each, host and guest logs both returned. Nothing to
fix; recorded because a clean run is evidence and because the checks §10 asked for
were answered by it. The check numbers below are §10.2's as it stood for Phase 3;
Phase 4 renumbered that table, and the tick-for-tick `fsm` comparison this entry
relies on is now a sequence comparison for the reason §10.3 gives.

- **No divergence at all.** Zero `WARN` or `ERROR` records on the host; one on the
  guest, and it is the host's own shutdown (`network peer disconnected` at tick
  2924). No `DESYNC`, no `DIVERGED`.
- **All 106 `fsm` records are byte-identical between the two files, tick for
  tick** — every region transition, every spawn, every timeout, in both directions.
  That covers checks 2, 3 and 5 in one comparison: `monitor` reached `MonitorActive`
  at tick 1 and never left it, so the `EventHeatBurst` region move D-20 was written
  for did not recur; a quasar lived from roughly tick 400 to 1000, several speed
  steps, with no `part=kinetics` report, which is the tick-744 defect exercised and
  absent.
- **`gold.timer` matched exactly at all fourteen sampled ticks**, including
  mid-sequence values. That is the key Phase 2 excluded, agreeing in a live session
  before Phase 3 admitted it to the compared surface — which is what said the defect
  was in the reproduction path rather than in the deadline.
- **`swarm.transition_stalls` stayed 0 throughout**, and `physics_steps` rose
  whenever a swarm was alive. The one window where `shield_hit` rose while
  `physics_steps` was flat (ticks 2000–2200, +214 and +0) had `swarm.count = 0` at
  both samples: the shield was striking something else. Not the 9.3 stall.
- Check 4's actual reproduction — a swarm parked in a shield under god mode — did
  not occur in this run. `swarm_stall_test.go` is what holds it.

### The 2026-08-31 session

Two participants, both in god mode, ~1,940 ticks. The log carried three defects
and none of them was the one the plan expected next. All three are fixed; the
first two are why D-20 and D-21 exist.

### 9.1 Kinetics divergence at tick 744 — the wall-derived clock

Reported as `part=kinetics, records=world`, first sample at tick 744 and
`DIVERGED` at 762, with no proximate cause anywhere near it. The cause was 30
ticks earlier: a quasar spawned at tick 712.

Game time came from each process's `PausableClock`, which projects `time.Now()`.
`QuasarSystem` steps `SpeedMultiplier` when `now.Sub(LastSpeedIncreaseAt)` passes
one second, so a sub-millisecond difference between two schedulers puts that step
on tick N for one instance and N+1 for the other. The multiplier compounds, so the
two velocity streams never re-converged.

§4.2 of this plan had already identified absolute instants as a hazard, but
scoped it to *transfers* — "it stops being sound the moment state is transferred
between processes". The log shows the argument was too generous: the origins do
not align during a live session either, because the start gate freezes game time
until everyone is ready and then each instance's wall clock runs on its own. Every
reader §4.2 lists was already exposed.

The fix is D-21. It also removed the reason `time.game_elapsed_ms` was excluded
from the compared surface, and that exclusion is exactly why nothing caught this:
the clock itself was never compared.

### 9.2 Status divergence at tick 1914 — a shared region on a local event

Reported as `part=status, records=reg|stat|fsm.monitor`. The cause is eleven ticks
earlier and visible in the same log: at tick 1903 the monitor region transitioned
`MonitorActive → MonitorHeatBurst via EventHeatBurst`, and back at 1904.

`EventHeatBurst` is `ClassLocal`, pushed by `HeatSystem` for the cursor that
overheated. Only the bursting participant's region moved. The state name recovered
a tick later, which is why the transition looks harmless in the log — what stayed
apart was the region's *elapsed* time, measured from a re-entry only one instance
made, and `fsm.monitor` is in the compared surface.

The fix is D-20 plus moving the sweep into `HeatSystem`, where a per-instance
effect belongs (D-6). Its hits on shared targets already cross as combat events
under D-3, so nothing about the gameplay changed.

### 9.3 A swarm parked inside a shield — a wedged state machine

Not a divergence; a gameplay stall, and the telemetry named it precisely.
Between ticks 800 and 1000 `shield.shield_hit` rose by 64 while
`swarm.physics_steps` did not move at all, and `combat.effects.kinetic` reached
404 over the run. The shield was striking the swarm every tick and combat was
applying knockback impulses to its velocity, but nothing was integrating that
velocity into a position.

`updateChaseState` decremented `ChargeIntervalRemaining`, called `enterLockState`
and returned. `enterLockState` refuses when no target resolves — and refused
without re-arming the interval, so an expired interval took the same branch on
every following tick and returned before applying homing or integrating. The
shield deals no damage by design; its whole job is ejection, and with god mode it
never dropped, so nothing resolved the stall.

The observation in the report — "as if immunity to damage and kinetic were both
being reset" — was close. `IsEnraged`, which `updateLockState` re-asserts every
tick, is one of the two gates that reject a knockback, and it is latched rather
than reset. But the load-bearing failure was the frozen integrator, and the fix
makes every swarm state total: an entry that refuses is a delay, never a wedge,
and `swarm.transition_stalls` counts refusals so the condition is visible in a log
rather than only as a species that stopped moving.

**What this suggests for the other species.** `QuasarSystem` refreshes
`RemainingDamageImmunity` to its full duration on every tick a quasar is shielded,
which makes a shielded quasar permanently invulnerable. That reads as deliberate
and was left alone, but it is the same shape as the swarm's latched `IsEnraged`,
and a shielded quasar that cannot leave its shielded state would be the same
stall. Worth a look when the quasar is next touched.

## 10. Manual verification for the next session

Everything below is a two-terminal check. Phase 4 made the host the authority and
the guest a predictor; Phase 5 made the cadence a function of the link. So what is
being verified is that the correction closes what the prediction opened, that the
cadence moves with the link and says so, that the convergence floor holds under
every shape, and that nothing the earlier phases fixed came back. The join is still
worth watching once by hand.

### 10.1 The join, which is the phase

`:host <addr>` on a run that is already playing, then dial it. This is the headline
and it wants doing by hand at least once.

```bash
# terminal 1 — start solo, play for a minute, then open the session
./bin/vif -lv info -ls afs -lt 200 -j
#   ... play into a storm, then type:  :host :7777
#   the status bar answers "Hosting on 127.0.0.1:7777"

# terminal 2 — arrive whenever
./bin/vif -join 127.0.0.1:7777 -lv info -ls afs -lt 200 -j
```

The repeatable headless form is the checked-in Phase 4 pair. The host half starts
**solo**, runs flat out to tick 400, opens hosting there, and is wall-paced from
that point — so the operator has the rest of the run to start the guest:

```bash
# terminal 1 — host script; opens hosting at tick 400
./bin/vif -script script/phase4-host.toml \
  -l=log/phase4-host -lv info -ls afs -lt 100 -j

# terminal 2 — once "hosting opened mid-run" appears in the host log
./bin/vif -join 127.0.0.1:7777 -script script/phase4-guest.toml \
  -l=log/phase4-guest -lv info -ls afs -lt 100 -j
```

The **Phase 5 pair** is the same shape run long enough to shape the link
underneath it, and `script/phase5-linkshape.sh` drives the whole thing — both
halves, the four `tc netem` stages and the extraction — as one command:

```bash
# as root: this shapes ALL loopback traffic on the machine for the length of the run
sudo script/phase5-linkshape.sh
```

By hand it is the same two terminals as above with `phase5-host.toml` and
`phase5-guest.toml`, plus a third for the stages:

```bash
sudo tc qdisc add dev lo root netem delay 80ms
sudo tc qdisc change dev lo root netem delay 80ms 40ms distribution normal
sudo tc qdisc change dev lo root netem delay 40ms loss 3%
sudo tc qdisc change dev lo root netem rate 512kbit delay 40ms
sudo tc qdisc del dev lo root
```

The Phase 3 pair (`script/phase3-host.toml`, `script/phase3-guest.toml`, both with
`-host`/`-join` and 2,000 wall-paced ticks) remains the tick-zero regression and
still exercises the heat bursts, the crossed quasar, the shared storm and the swarm
in the shield.

`-ls afs` keeps `app`, `fsm` and `stat`: the join and correction records, the region
transitions and the periodic counters. Add `+e` only if a specific event needs
chasing — it was 5,634 of the 2026-08-31 log's 6,801 lines and carried nothing.
**Send back the log from both terminals**; one side shows the correction it applied
and never what the host thought it was sending.

### 10.2 What to check

| # | Check | Pass |
|---|---|---|
| 1 | **The join itself.** Play solo into a storm, `:host :7777`, dial from terminal 2 | The guest arrives inside the storm holding the same world. Its `app` log carries `capture staged`, `capture installed`, `join installed the session world` and `join caught up`; the host's carries `session capture` and `mid-run participant admitted`. Both cursors work. |
| 2 | **The arrival is one crossing.** Read `cursor spawn` on both logs after the join | The same `entity` and `slot` on both instances. A different entity id on each side is still the most serious thing this check can find: D-11 weakened its *values* clause and not its identity one. |
| 3 | **Local input is immediate.** Hold `l`, then type a corpus line at full speed | The cursor tracks the keys with no perceptible lag on either side, `typing.errors` stays at zero, and the shared cell moves with the local one rather than a playout lead behind it. The *remote* cursor still steps in its sync cadence. |
| 4 | **The correction, which is the phase.** Read the `snapshot.correction` stat group on the guest over several minutes | `corrections_applied` rises steadily. `correction_entities` is small and **does not trend upward** — a bounded magnitude is convergence, a growing one is not. `corrections_refused` may be non-zero briefly after the join and must not keep rising; `corrections_superseded` rising means this machine cannot keep up with the cadence. |
| 5 | **The cadence's cost.** Read `snapshot` and `snapshot.cadence` on the host | `corrections_sent`, `keyframes` and `correction_bytes_sent` — the last divided by elapsed seconds is the actual uplink. `capture_us` is the host's stall and should stay a low single-digit percentage of the 50 ms tick even in a storm. `cadence_ticks` and `cadence_keyframe_period_ticks` are what the controller chose; `cadence_uplink_bps` is what it priced that at. **Send these back**: §8's figures are in-process on one machine with no link at all. |
| 6 | **Staleness says something true.** Watch `network.lag_ticks` and the status bar | Zero or one on a healthy loopback link and no `LAG` item. Under `tc netem` delay it rises and the item appears; remove the delay and it clears. A `LAG` that never clears on a quiet local link is a defect, not a slow network. |
| 7 | **Nothing enters an unrecoverable state.** Kill the link entirely (`tc qdisc` drop-all, or unplug), leave it down for ten seconds, restore it | The guest keeps playing on its prediction, `LAG` appears, `cadence_keyframe_age_ticks` climbs past the floor and `LINK!` appears with it, and the *next keyframe* restores everything — within one keyframe period at most, which the floor bounds at 60 ticks. No `DESYNC`, no `DIVERGED`, nothing to restart. This is the check the phase exists for. |
| 8 | **Jitter and loss.** `tc netem delay 80ms 40ms loss 2%` on the guest | Play stays responsive because local input never waits. Remote entities stay smooth between corrections. `correction_entities` rises and stays bounded; `corrections_refused` rises with loss and is harmless. `network.link_rtt_ms` reads about 80 and `link_jitter_ms` follows the shape. |
| 8a | **The cadence follows the link.** Walk the four stages of §10.1, reading `snapshot.cadence` on the host at each | The cadence and keyframe interval move with the shape and come back when it clears — stretching the interval first and slowing the cadence only when that is not enough. `cadence_constrained` turns on under the bandwidth stage and off after. `cadence_keyframe_period_ticks` **never exceeds 60**: a larger value is a defect in the controller, not a slow link. |
| 8b | **The floor is refused rather than crossed.** Shape hard enough that a whole capture cannot cross in three seconds (`rate 64kbit` at the storm high water), then dial a *new* participant | The join is refused with "link cannot sustain the convergence floor", naming what the floor costs and what the link carries. An existing participant is not dropped — it reports `LINK!` and keeps predicting. Silence here is the one outcome the phase forbids. |
| 9 | **Join cost is a function of the world, not the session.** Repeat the join after several minutes | `snapshot.bytes` and `stage_us` track what is on the map, not how long the host has been running. `stage_us` on the *second* install of a session is far below the first: the staging world is built once. |
| 10 | **Reconnect.** Kill terminal 2, watch the host despawn the guest cursor, then dial again | The second arrival installs at a *later* `install_tick` than the first and lands the same way. The host's roster returns to one cursor in between. |
| 11 | **Gold, quasar and heat across a join** | Both instances report the same `gold.timer` at the same tick; a quasar living ~30 s across the join produces no kinetics disagreement; the cleaner sweep appears only for the participant that burst and `fsm.monitor` stays `MonitorActive`. |
| 12 | **Swarm in a shield.** Both in god mode, park a swarm in a shield | The swarm is ejected. `swarm.physics_steps` keeps rising while `shield.shield_hit` does; `swarm.transition_stalls` stays 0. |
| 13 | **Reset.** `:new` on the coordinator | Both reset together, the counters restart from zero on both, and corrections resume against the new run. |
| 14 | **Solo, unchanged.** One terminal, no `-host`/`-join`, several minutes | Nothing regressed. `:session` says "Solo run"; `:speed`, `:step` and pause still behave, and no `snapshot` counter moves. |
| 15 | **The operating point is legible.** `:session` on the host during check 8a | One line carrying the cadence, the keyframe interval and period, the round trip and its variation, the link rate, the planned uplink, what the floor costs, and whether the link is nominal, constrained, or below the floor. A player who cannot tell a small link from a broken game from that line is the defect this check is for. |

### 10.3 What to look at first in the returned logs

```bash
# anything the session called a problem, both files
jq -c 'select(.level=="WARN" or .level=="ERROR")' <log>

# the join itself, both files
jq -c 'select(.sub=="app" and (.fields.msg|test("capture|join|hosting|admitted")))' <log>

# every correction the guest installed, with what it had to move
jq -c 'select(.sub=="app" and .fields.msg=="capture installed")
       | {tick, t:.fields.tick, cells:.fields.correction_entries,
          entities:.fields.correction_entities, shift:.fields.correction_cells}' <guest>

# the cadence's cost and this instance's staleness, over the run
jq -c 'select(.sub=="stat" and (.fields.msg|test("^snapshot|^network")))' <log>

# the operating point the link put the session at, over the run
jq -r 'select(.sub=="stat" and .fields.msg=="snapshot.cadence")
       | "\(.tick) cadence=\(.fields.ticks) kf=\(.fields.keyframe_interval) period=\(.fields.keyframe_period_ticks) uplink=\(.fields.uplink_bps) budget=\(.fields.budget_bps) floor=\(.fields.floor_bps) constrained=\(.fields.constrained) breached=\(.fields.floor_breached)"' <host>

# the link the cadence was chosen from
jq -r 'select(.sub=="stat" and .fields.msg=="network.link")
       | "\(.tick) rtt=\(.fields.link_rtt_ms)ms/\(.fields.link_rtt_us)us jitter=\(.fields.link_jitter_ms)ms rate=\(.fields.link_bps) loss=\(.fields.link_loss_pct)% saturated=\(.fields.link_saturated)"' <log>

# fsm transitions, host against guest
jq -r 'select(.sub=="fsm")|"\(.fields.region) \(.fields.from)->\(.fields.to // .fields.state)"' <host> > h
jq -r 'select(.sub=="fsm")|"\(.fields.region) \(.fields.from)->\(.fields.to // .fields.state)"' <guest> > g
diff h g
```

The correction line is the one to read first, and what it says is a *trend* rather
than a value: a magnitude that stays in the same range is a guest predicting well
and being corrected, and one that climbs is a guest falling behind faster than the
cadence repairs it.

The `fsm` diff is the cheapest whole-session check there is, and Phase 4 changed how
to read it. It used to be tick-for-tick: on the 2026-09-01 run all 106 records
matched exactly. A guest applies its own artifacts a playout lead earlier now, so
the *ticks* may differ by up to that lead — the **sequence** must not. Dropping the
tick from the key, as above, is what makes it a comparison of what happened rather
than of when. One `nav.entities` difference is expected and excluded: it counts both
domains, so a participant's own player-domain population moves it.

### 10.4 What the next session wants from these runs

Phase 5's own question is answered: the round trip exists, the cadence follows it,
and §9.B carries the numbers from the socket pair. What is still missing is the
same set **over a link that is not loopback and not `netem` on one machine** —
two hosts, a real path between them — because every figure this plan has was taken
where the link is either free or artificial. Specifically:

- `snapshot.cadence` and `network.link` together, at rest and under load. The
  interesting column is `cadence_budget_bps`: it is only non-zero when the link was
  actually the limit, and nothing on a loopback ever fills it in.
- How much shaping it takes before `snapshot.correction_entities` stops being
  bounded, what `network.lag_ticks` reads when that happens, and what the uplink was
  at the time.
- Whether `cadence_floor_breached` ever fires on a link a person would call usable.
  The floor is priced at one whole world per three seconds — 59 to 66 KiB/s on the
  measured storm world — and a link below that on a real path is the case the
  refusal exists for.

For Phase 6 the run wants one thing more: `correction_cells` over a session with a
real path, because that is how visible the rollback flicker actually is, and it is
the number that says whether bounded replay is worth building.
