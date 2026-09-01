# Multiplayer enhancement plan: authoritative host, deterministic guests

**Status: this is the plan of record for multiplayer.** It supersedes the staged
recommendation in [desync.md](desync.md), which is retained as the diagnosis of the
2026-08-30 divergence and as the survey of the option space. Domain rules D-1..D-21
in [domain-design.md](domain-design.md) remain authoritative for the *existing*
code; §5 of this document states which of them the target architecture keeps,
changes, and adds to. D-18 landed with Phase 1; D-19, D-20 and D-21 landed with
Phase 2.

**Phases 1 and 2 are done. Phase 3 is next**, and §6's Phase 3 entry says what it
starts from. §9 records the defects the 2026-08-31 session surfaced and what each
turned out to be.

## 1. Why the current design is being replaced rather than repaired

The session model that exists today was assembled from compromises, and the
compromises are load-bearing. Restating the original requirements makes that
visible:

| Original requirement | What exists |
|---|---|
| A solo game can be toggled into a host, and others join **at any time** | Join is only possible at tick zero, through a lobby gate fixed before the run starts. The replay-from-tick-zero path that nominally provided mid-run join was never reachable from `cmd/vif`; it has been removed. |
| A **true multiplayer experience** | Fast input is discarded, not merely delayed: measured, a session drops 4 of 5 rapid cursor motions and scores 5 of 6 fast keystrokes as *typing errors*. |
| Resilience to **lag, jitter and bandwidth limits** | None. A deterministic lockstep barrier converts jitter into a permanently forked session, and there is no repair on any edge. |

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
| Bandwidth | Low snapshot rate, deltas, and an adaptive cadence |
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
The store column is unchanged and deliberately so: the crossing still applies at
its barrier tick on every instance, and what moved is the cell the producing
instance reads.

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
noted at the end of the section.

### 4.1 The hidden-state surface

A snapshot must carry everything that decides a future shared outcome. Much of it
is not in a component store:

| State | Where | Obstacle |
|---|---|---|
| ~24 per-system RNG streams | private `*vmath.FastRand`, seeded in each `Init` | `State()` exists; **there is no `SetState`** |
| A second generator | `WallSystem.mazeRng`, a `math/rand.Rand` | not restorable as written |
| **EXP3 route learning** | `AdaptationResource.Entries` — weights, pre-sampled `Pool`, consumer `Head`, `spin`; decides which route a spawned eye takes | a resource, not a store; not in the digest |
| **Genetic populations** | `GeneticResource.Registry` — a whole GA registry behind `sync.Mutex`/`atomic.Pointer` in `pkg/genetic` | needs an export contract it does not have |
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
learned resources have export contracts; the FSM runtime travels. The last row —
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
capture tick, because that stays correct if a capture is rebased; and the
cross-process gate is still owed, for the reason the section gives.

### 4.3 The digest is narrower than the state

`worldDigestScopedLocked` hashes positions, kinetics and non-cursor combat. It does
not hash genotype, quasar timers, adaptation weights, GA populations, or FSM
variables. It is a good cheap detector and must not be mistaken for a completeness
check on a snapshot.

## 5. Rule changes

The target keeps most of D-1..D-17. Three entries change meaning and two are added;
`domain-design.md` is updated as each phase lands, not in advance.

**D-11 is weakened, deliberately.** "Identical shared component values on every
instance at every tick" becomes "identical on the host; on a guest, equal to the
host as of the last applied snapshot, and converging". Bit-exact cross-instance
agreement stops being a runtime invariant and becomes a *test* invariant for the
host's own replay.

**D-3 keeps its shape but changes its destination.** A crossing artifact still
names the smallest thing that determines a shared outcome, but it is now a
*request to the authority* rather than a fact every instance applies at an agreed
tick.

**D-13 generalises.** Owner-authored state stops being a special exception to
re-derivation and becomes the ordinary case for one class of value: the owner
applies immediately, the host arbitrates, everyone else receives.

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
   a `pkg/genetic` export/import that does not leak the mutex).
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

### Phase 3 — Join anytime  ← next

**Goal.** Deliver the original requirement: a running solo game can be toggled into
a host, and a participant can arrive at any moment.

**Requirements.**

1. A running instance can begin hosting on request (`:host <addr>`, or the existing
   flag) without restarting the run.
2. A joiner receives the anchor, then a snapshot at a named tick, installs it,
   and enters the session at the following tick. No replay, no session log.
3. The roster arrival remains a crossing, so every participant creates the new
   cursor at one agreed tick.
4. Reconnect is the same path with the same code.
5. The host stays playable throughout; the snapshot is captured without stalling
   its tick beyond a bounded, measured pause.

**Boundaries — must not.** Reintroduce a retained record stream. Require the host
to pause. Elect a coordinator or survive host loss.

**Manual acceptance.** Start a solo run, play into a storm, toggle hosting, join
from a second terminal: the guest arrives inside the storm with the same world, and
both cursors work. Kill the guest and rejoin. Repeat with the join deliberately
delayed until several minutes in — join cost must not grow with session length.

**What Phase 3 starts from.** Phase 2 left five things on its doorstep, in the
order they are wanted:

1. **The staged install.** `App.InstallShared` validates everything before writing
   but writes into the live world. Phase 3 needs the second world and the swap at
   a tick boundary, because the instance being installed into is a running one.
2. **The cross-process gate.** §6's requirement 9 — capture in one process, load
   in another whose clock origin is deliberately offset, then 500 ticks under an
   identical record stream. D-21 makes the clock-offset half much less dangerous
   than it was (the simulation instant no longer carries a process origin at all),
   but it must still be the gate rather than an argument, and the record-stream
   half is only expressible once a capture travels.
3. **The gold spawn-tick defect.** A joiner's FSM reaches `MainSpawnGold` one tick
   before the host's, so the two carry deadline origins a tick apart for the life
   of the sequence. `gold.timer` is excluded from the compared surface only
   because of this; re-admitting the key is the check that closes it. Reproduce
   with `TestJoinerOnAnotherTerminalSharesTheMapFromTickZero` and the deny entry
   removed from `internal/app/snapshot.go`.
4. **The storm high-water sizing.** Capture bytes, capture time, install time and
   allocation peak at the storm high water. Phase 4's cadence is chosen from those
   numbers and Phase 2 only has the quiet-world ones.
5. **A failing case for the navigation phase.** The route-rebuild phase is carried
   but no sabotage of it fails the gate, so its coverage is asserted rather than
   demonstrated.

---

### Phase 4 — Authority and correction

**Goal.** The host becomes the authority and guests become predictors. This is
where divergence stops being a failure mode and becomes a routine, corrected
condition.

**Requirements.**

1. Guests apply local input immediately (Phase 1 already does this for the cursor;
   extend to the rest of the player domain's shared-facing actions) and submit the
   artifact to the host.
2. The host orders, validates and applies; its result is the truth.
3. Periodic authoritative snapshots at an adaptive cadence, full first, deltas once
   full snapshots are correct and measured.
4. Guests apply corrections into the staging world and swap; the correction
   magnitude is telemetry, not an error.
5. The playout barrier is removed from the local path. Any remaining receive-side
   delay is an interpolation buffer for remote action, justified separately.
6. `DESYNC`/`DIVERGED` are retired as failure states and replaced by a correction
   magnitude and a staleness indicator.

**Boundaries — must not.** Let a guest's derivation override the host. Make the
host's uplink scale with entity count at tick rate — the whole point is the low
snapshot cadence prediction buys. Break solo replay.

**Manual acceptance.** Two terminals through injected delay, jitter and loss
(`tc netem`): play stays responsive, remote entities stay smooth, no session ever
enters an unrecoverable state, and the correction magnitude stays bounded. Kill the
link entirely and restore it: the guest resumes from the next snapshot.

---

### Phase 5 — Adaptive cadence and bandwidth resilience

**Goal.** Make the system degrade gracefully rather than fail at the edge of the
link's capacity — the explicit priority.

**Requirements.**

1. Measure RTT, jitter, throughput and correction magnitude; drive snapshot rate
   and delta/full choice from them.
2. Relevance and priority: entities that matter to a given participant update more
   often.
3. A floor that still guarantees convergence, and a refusal path for links that
   cannot meet it.
4. Report the operating point in the status bar, so a player can see the link is
   constrained rather than guessing that the game is broken.

**Boundaries — must not.** Let adaptation silently reach a rate at which
convergence is not guaranteed.

**Manual acceptance.** Shape the link down in stages and confirm play degrades
smoothly — snapshot rate falls, prediction carries more, correction magnitude rises
but stays bounded, and nothing forks or disconnects.

---

### Phase 6 — From evidence

Not committed. Bounded rollback where prediction error is still visible; host
migration and partition health as a separate project with its own prerequisites;
authentication (`Config.TLS`, `MsgAuthRequest`/`Response`) before anything beyond
trusted peers; multi-link topology from the CLI.

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
  deterministic replay are untouched** and remain valuable for debugging.
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

## 8. Open questions

1. **Prediction scope in Phase 1.** Cursor cell only, or also the visual removal of
   a typed *shared* gold member? The conservative answer is taken here.
2. **Snapshot cadence** is a guess until it is measured at the storm high water.
   Phase 2 measured a quiet world — ~4 KB per capture with three swarms and a gold
   sequence — which supports the 2–5 Hz hypothesis but does not test it. The
   figure that decides it is still owed (§6, Phase 3's starting list).
3. **Host loss** ends the session. Confirm that is acceptable before sizing Phase 6.
4. **Does the host keep re-deriving, or become the only simulator?** This plan
   keeps guests simulating (§2.1). If measurement later shows correction magnitude
   is large enough to be distracting, the fallback is thinner guests and a higher
   snapshot rate — a tuning change, not a rewrite.
5. **Tower ownership in optional maps** still binds every tower to slot zero. Not a
   blocker; a gameplay rule to settle before towers appear in a real session.

## 9. What the 2026-08-31 session showed

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

Everything below is a two-terminal check. Phases 1 and 2 changed the clock, the
monitor region and the swarm state machine, and none of those is on the wire — so
what is being verified is that nothing regressed *and* that the two reported
defects are gone.

### 10.1 How to run it, and what to send back

```bash
# terminal 1 — host, logging on, journal on
./bin/vif -host :7777 -players 2 -lv info -ls afs -lt 200 -j

# terminal 2 — guest
./bin/vif -join 127.0.0.1:7777 -lv info -ls afs -lt 200 -j
```

`-ls afs` keeps `app`, `fsm` and `stat`: the divergence reports, the region
transitions and the periodic counters. That is the mask both 2026-08-31 findings
were diagnosed from, and it keeps the file small enough to attach. Add `+e` only
if a specific event needs chasing — it was 5,634 of the last log's 6,801 lines and
carried nothing.

**Send back the log from *both* terminals.** The last session had only one side,
which was enough to see that the two disagreed but not to see which one was
wrong. With both, `part` and `records` on one side can be read against the other's
records at the same tick.

Run for **at least 2,000 ticks (about 100 s)**, since both reported divergences
appeared after tick 700.

### 10.2 What to check

| # | Check | Pass |
|---|---|---|
| 1 | Play both cursors normally for 2,000+ ticks | No `shared state divergence` (WARN) and no `shared state diverged` (ERROR) in either log. The status bar never shows `DESYNC` or `DIVERGED`. |
| 2 | **Heat burst, both sides.** Drive one participant's heat to a burst, then the other's | The cleaner sweep still appears for the participant who burst, and only for them. `fsm.monitor` never enters a heat state — the region should stay `MonitorActive`. No divergence follows either burst. |
| 3 | **Quasar speed.** Play until a quasar spawns and survives ~30 s | No `part=kinetics` divergence. This is the tick-744 defect; a quasar living through several speed steps is what exercised it. |
| 4 | **Swarm in a shield.** Both in god mode, let a swarm reach a shield and sit in it | The swarm is ejected rather than parking. In the `stat` records, `swarm.physics_steps` keeps rising while `shield.shield_hit` does; the two moving together is the fix, `shield_hit` rising while `physics_steps` is flat is the bug. `swarm.transition_stalls` should stay 0; a non-zero value is not a failure but is worth reporting. |
| 5 | **Gold across a join.** Let a gold sequence run to its timeout | Both instances resolve it on the same tick. (`gold.timer` is still excluded from the compared surface, so a divergence here will not be reported automatically — read the two `stat` records for `gold` at the same tick and compare.) |
| 6 | **Reset.** `:new` on the coordinator | Both reset together, no divergence follows, and the swarm/gold counters restart from zero on both. |
| 7 | **Solo, unchanged.** One terminal, no `-host`/`-join`, several minutes | Nothing regressed: the clock change touched every run, not only sessions. Speed control (`:speed 1/2`, `:speed 2`) and pause still behave. |

### 10.3 What to look at first in the returned logs

```bash
# every divergence report, both files
jq -c 'select(.level=="WARN" or .level=="ERROR")' <log>

# the ~20 ticks before the first one — the cause is usually not adjacent
jq -c 'select(.tick>=<T-30> and .tick<=<T+5> and .sub!="event")' <log>

# the counters that named the swarm stall
jq -c 'select(.sub=="stat" and (.fields.msg=="swarm" or .fields.msg=="shield"))' <log>
```

Both 2026-08-31 findings were 10—30 ticks *downstream* of their cause, so the
second command is the one that matters: read the window before the report, not
the report.

### 10.4 Phase 3 wants one more thing

Phase 3 is join-anytime, and its sizing depends on numbers Phase 2 could not take
(§6, Phase 3's starting list, item 4). If a storm can be reached in one of these
runs, note the `stat` record for `entity` and `spatial` at the storm high water
and send it with the log — that is what Phase 4's snapshot cadence gets chosen
from.

