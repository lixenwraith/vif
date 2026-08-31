# Multiplayer enhancement: local-first input and a restorable shared checkpoint

This document is the outcome of a code review of the live multiplayer surface —
`internal/engine`, `internal/event`, `internal/journal`, `internal/network`,
`internal/service`, `internal/manifest`, and the consumers in `internal/system`,
`internal/mode` and `internal/app`. It states the defects the review found, weighs
the recovery designs against what the code can actually carry, selects one, and
plans it in phases that each compile, ship, and can be checked from two terminals.

The domain rules remain authoritative in [domain-design.md](domain-design.md).
The divergence incident, the option scoring, and the sizing estimates that this
plan builds on are in [desync.md](desync.md). This document does not replace
either; it revises two of desync.md's conclusions on measured evidence and adds
the responsiveness problem, which neither existing document addresses.

## 1. Conclusion first

Three problems are usually discussed as one and are in fact separable, with
separable fixes. Conflating them is why the roadmap looks larger than it is.

| Problem | Symptom | Mechanism that fixes it |
|---|---|---|
| **Responsiveness** | Local cursor and typing lag by 150–216 ms in a session, and fast input is silently swallowed | Predict the owner's own cursor cell locally (**D-18**); it needs no protocol change at all |
| **Delivery** | A lost, refused or late artifact silently forks the session | Session-ordered ledger with acknowledgement, gap detection and bounded retransmission |
| **Continuity** | Once forked, nothing re-converges; join costs the whole session; a departed host ends it | A restorable shared checkpoint (**D-19**) plus a canonical artifact suffix |

The recommended architecture is therefore **deterministic shared simulation, kept
— with a reliable ordered ledger, a restorable host-committed checkpoint, and a
local-first input path**. This agrees with desync.md's selection of
checkpoint-plus-suffix over the alternatives, and revises it in two ways the
measurements below require:

1. **Responsiveness is phase one, not phase four.** It is the defect a player
   feels every keystroke, and it is the only item on the list that needs no wire
   change, no codec, and no authority.
2. **Re-simulation is not the expensive part.** A tick costs ~0.3 ms at a shared
   population six times the observed high-water. Catch-up, checkpoint validation,
   and even bounded rollback are all affordable; the binding constraint is the
   save/load contract, not CPU. That makes the checkpoint codec the *single*
   enabling investment, and it makes rollback a later option rather than the
   implausible one desync.md scored.

## 2. What the review found

### 2.1 The playout barrier sits on the local input path

This is the responsiveness defect, and it is worse than lag.

`World.PushCrossing` routes a D-3 artifact into `EventQueue.Push`, which offers it
to the wire sink before publishing it (`internal/event/queue.go:41`). In a live
session `NetworkSystem.Cross` takes ownership and returns true, so **the event is
never published locally**; it is scheduled at `productionEpoch + delayTicks` and
published by `applyDue` at the opening of that tick
(`internal/system/network.go`). `EventCursorMoveRequest` is `ClassBus`, and
`mode.OpJump` pushes it as a crossing, so *every* cursor move — `h`, `w`, `f`,
mouse, the post-typing advance — takes the full lead.

| Path | Keystroke to applied position |
|---|---|
| Solo | Immediate settle (`DispatchEventsImmediately`) plus one render frame: **≤16 ms** |
| Session | 3 ticks of lead, plus the remainder of the current tick, plus one render frame: **150–216 ms** |

Latency alone would be tolerable. The compounding defect is that **the input path
re-reads the authoritative position for every subsequent action**:

- `Router.handleMotion` and `handleCharMotion` resolve the motion from the
  cursor's store position (`internal/mode/router.go`), so two `w` presses inside
  the lead window both compute from the same start cell and the second lands where
  the first already did — the keystroke is not delayed, it is *lost*.
- `Router.handleInsertChar` stamps the same stale cell into
  `CharacterTypedPayload`, and `TypingSystem.moveCursorRight` reads the store
  again to request the advance. In a game whose core loop is typing, a session
  caps useful typing at roughly one character per 200 ms and silently retypes the
  same cell above that rate.

Nothing in the current test suite can see this: the parity harness drives one tick
per participant per step and asserts *agreement*, never *responsiveness*, and both
instances are equally late.

### 2.2 Detection exists; repair does not

The runtime digest (`app/digest.go`, `NetworkSystem.compareStateDigest`) is a good
detector and, since per-record escalation, a good diagnostic. It is not a
protocol. Confirming desync.md §2, the review found no repair path on any edge:

- **Delivery.** `SocketPort.push` drops an inbound notification when the poll
  buffer is full; `PeerManager.BroadcastExcept` counts a refused frame. Both are
  counted (`transport_lost_in`/`out`) and logged once. Neither is retransmitted,
  and no receiver can tell that it is missing an epoch — `epochWindow.admit`
  suppresses duplicates but detects no gaps, because a source's epochs are not
  contiguous by construction (an idle tick still closes an epoch, so gaps *are*
  detectable, and the window simply does not look).
- **Lateness.** `applyDue` publishes an artifact whose `applyTick` has already
  passed, increments `barrier_late`, and continues. Applying a crossing one tick
  late on one instance is exactly the divergence the barrier exists to prevent;
  the code chooses to apply it anyway.
- **Logic.** A determinism defect — the D-17 flow-field cache phase is the proven
  instance — produces no artifact loss at all, so no delivery mechanism can repair
  it. Only a state transfer can.
- **Membership.** `reportDisconnect` correctly names an unrecoverable host loss
  rather than waiting for a digest that can no longer arrive, but there is no
  election, no state migration, and no partition detector.

### 2.3 The checkpoint's real cost is the hidden-state surface, not the bytes

desync.md §6.2 lists what a keyframe must contain. The review's finding is that
the *list* is the risk, not the size. The state that decides a future shared
outcome is spread across the process in places a component walk will not find:

| Hidden state | Where | Serializable? |
|---|---|---|
| ~24 per-system RNG streams | each system's private `*vmath.FastRand`, seeded in `Init` via `World.Rand` | `FastRand.State()` exists; **there is no `SetState`** |
| A second generator | `WallSystem.mazeRng` is a `math/rand.Rand` | not directly |
| FSM runtime | `fsm.Machine.regions`, `variables`, `delayedActions` | yes, and small |
| Throttled derivation phase | `navigation.FlowFieldCache.TicksSinceCompute`, `PendingUpdate`, `LastTargets` (D-17) | phase yes; the field itself is derivable and should be recomputed, not shipped |
| Barrier | scheduled artifacts, `productionEpoch`, per-source epoch windows, `crossSeq` | yes |
| Allocator and counters | `World.nextEntityID`, per-domain created/destroyed | yes |
| Scheduler | settle-group stamp, tick counter, run counter | yes |
| Per-system scratch | dirty sets, rebuild budgets (`NavigationSystem.routeRebuildTicks`), throttles | **per system; no inventory exists** |

The last row is the one that decides the project's success. A one-shot
serialization written by inspection will miss members, and the failure mode is a
checkpoint that loads, validates against `SnapshotShared`, and diverges 200 ticks
later. The plan below therefore makes checkpoint participation a **declared,
statically-checked, per-system contract in `internal/manifest/definition.go`** —
the same mechanism that already made domain profiles mechanical rather than
reviewed (D-15) — instead of one large serializer.

### 2.4 Measurements taken during this review

Driven headless on `config/main` with the tower and storm regions forced, so the
shared population sits far above the incident trace's 500-entity high-water:

| Shared positioned | Live total | Cost of one full tick |
|---:|---:|---:|
| 12 | ~400 | 13 µs |
| 2,487 | 3,157 | 123 µs |
| 2,866 | 3,854 | 239 µs |
| 2,984 | 4,046 | **353 µs** |

One `sharedDigestLocked(false)` costs 273 µs at that load, i.e. 45 µs/tick
amortised at the six-tick cadence.

Three consequences follow, and they are the evidence for §1's revisions:

1. **A tick costs 0.7 % of its 50 ms budget at six times the observed
   high-water.** Simulation headroom is not the constraint anywhere in this plan.
2. **Catch-up runs at 3,000–75,000 ticks/second.** Replaying a ten-minute session
   (12,000 ticks) costs seconds, not minutes — which is why today's unbounded
   log *works*, and also why bounding it is a memory decision rather than a
   latency one.
3. **Rollback is CPU-affordable.** Re-simulating the full 3-tick lead costs
   ~1 ms, ~2 % of a tick. desync.md scored rollback's tractability at 1 on the
   assumption that many-tick re-simulation is expensive; it is not. What remains
   genuinely hard about rollback is the save/load contract and bounded side
   effects — and the checkpoint of Phase 4 *is* that contract. Rollback therefore
   becomes an option this plan unlocks rather than one it forecloses; it is still
   not the first step, because prediction (Phase 1) removes the symptom rollback
   would exist to hide.

### 2.5 Smaller findings, and what this pass changed

Fixed in this review, behaviour-neutral and size-reducing:

- **`World.LocalCursor()`** replaces 26 copies of
  `Positions.GetPosition(Resources.Player.Entity)` across `mode`, `app`, `engine`
  and four player-profile systems. This is a reduction now and the **single seam
  Phase 1 installs behind**: with one accessor, prediction is one implementation
  change rather than 26 call-site edits. Every current caller is `player`-profile
  or view, so the D-1 boundary the accessor must respect already holds.
- **`network.NewAckMessage` removed** — dead since it was written.
- **`RoleClient`/`RoleServer` removed.** They were unreachable aliases of
  `RolePeer`/`RoleHost`; nothing ever constructed them, and `Transport.Start`
  carried both names for one branch.
- **`NetworkSystem` telemetry reset is table-driven.** Registration now enrols
  each counter in the set `Init` clears, so the 26-name reset block became three
  loops and a counter added to the constructor can no longer survive a reset
  carrying the previous run's value.

Observed and left alone, with reasons:

- The reserved message codes (`MsgAck`, `MsgPeerList`, `MsgRoleAssign`,
  `MsgAuthRequest`/`Response`) are documented placeholders and Phase 3 claims
  `MsgAck`. Removing them now would only churn the numbering.
- `Peer.OutSeq`/`InSeq` already carry a per-link sequence and acknowledgement in
  every frame header, consumed by nothing. Phase 3 should use the *session*
  sequence rather than these — a per-link number cannot name an artifact that
  arrived relayed — but the header fields are free carriage for it.
- `event.EncodeSessionLog` marshals every chunk twice, once to size it and once to
  stamp `Total`/`Final`. It is join-path-only and Phase 5 deletes it; not worth
  touching first.

## 3. Approaches, briefly

desync.md §5 scored nine options. That analysis stands and is not repeated. What
this review adds is where the measurements move a score, and why the two designs
that look most "modern" are the wrong first move here.

| Direction | Fit for vi-fighter | Verdict |
|---|---|---|
| **Keep determinism; add ledger + checkpoint + suffix** | Preserves the entire existing architecture, its tests, its ~3–38 KB/s wire cost, and its replay story. Repairs both delivery loss and logic divergence, because a checkpoint does not care which caused the fork. | **Selected** |
| Authoritative state stream (full or delta) from the host | Repairs trivially, but discards deterministic re-simulation — the property nine of the seventeen domain rules exist to protect — and makes host uplink scale with clients. At the *measured* 2,900-entity load a naive full stream is >100 KB/tick. Delta encoding fixes the bytes and adds acknowledged baselines, component removal, and schema-aware field encoding. | Rejected as a first move; remains the fallback if determinism proves unmaintainable |
| Input prediction with world rollback | Now CPU-affordable (§2.4), and it is the only design that removes the residual prediction error entirely. But it needs the same save/load contract as the checkpoint *plus* bounded side effects across ~50 systems, and it solves a symptom that Phase 1 removes for free. | Deferred to Phase 6, on evidence |
| Strict lockstep, replay-from-zero, restart, CRDT | Unchanged from desync.md: stalls, unbounded growth, no continuity, or wrong semantics for ordered combat. | Rejected |

The decisive argument for keeping determinism is not sentiment. This codebase's
shared/player split means **the whole player domain already runs locally with no
replication at all** — glyphs, weapons, projectiles, drains, nuggets, every
effect. An authoritative-stream rewrite would buy authority over the ~500 shared
entities that the deterministic model already agrees on cheaply, at the cost of
the mechanism that makes the other several thousand free. The correct move is to
add authority *only where the deterministic model has no answer*: as a
tie-breaking checkpoint, not as a per-tick truth.

## 4. The selected design

### 4.1 Two new rules

The design adds two rules to the D-series, stated in the same form the existing
ones use so they can be enforced the same way.

**D-18 Predicted local state.** A value the local participant's own input
determines may be applied locally before its authoritative counterpart arrives,
provided that (a) the authoritative value is a pure function of the *same* local
producer's crossing, (b) only player-domain producers and the view read the
prediction, and (c) the prediction emits no event and enters no shared state,
digest or snapshot record outside `view`. When an authoritative value arrives that
the prediction did not produce, the prediction is discarded, not merged.

This is exactly the cursor cell's shape and no more. `EventCursorMoveRequest`
names an **absolute** target cell, so the authoritative result of the local
producer's own crossing is already known at production time — prediction here is
bookkeeping, not extrapolation, and cannot drift. The exceptions are the moves the
local producer did *not* make: the shared wall push-out (`system/wall.go`), the
gold jump (`system/gold.go`), level setup and reset. Each of those is a shared
re-derivation that lands on every instance identically, and clause (c) resolves
it by snapping.

The rule is statically checkable by the machinery `TestSystemDomainProfiles`
already uses for `ownerAuthoredStores`: a `shared`-profile system that calls the
prediction accessor fails the build. Today's callers are `camera`, `dust`,
`motion_marker`, `splash` (all `player`), plus the input router and the view —
so the boundary holds before the first line of Phase 1 is written.

**D-19 Restorable shared state.** Every value that can change a future shared
outcome is either (a) a component in a shared entity's store, (b) declared by its
owning system in `internal/manifest/definition.go` as checkpoint state and
serialized through that declaration, or (c) provably re-derivable from (a) and
(b) at install time. A system's declaration is part of its manifest entry, and a
system that holds future-affecting private state without declaring it fails the
boundary suite.

`SnapshotShared` and its FNV digest remain a comparison surface and are explicitly
*not* the checkpoint format — the same statement desync.md makes, restated here as
a rule so a future change cannot quietly conflate them.

### 4.2 Where each mechanism sits

```
  input ──► predicted cell (D-18) ──► player domain + view          [Phase 1]
      │                                    (instant, local)
      └──► crossing ──► ledger ──► barrier ──► shared simulation    [Phase 3]
                          │            ▲          (deterministic)
                          │            └── adaptive lead            [Phase 2]
                          ▼
                   retained suffix ──┐
                                     ├──► recovery / join / reconnect [Phase 5]
        host checkpoint (D-19) ──────┘                                [Phase 4]
```

The ordering matters: **Phase 1 is independent of everything to its right**, and
Phases 2 and 3 are independently useful whether or not Phase 4 ever ships.

### 4.3 Authority, restated

The coordinator gains exactly one new power and no others: it **commits** a
checkpoint identity `(session, run, tick, session sequence, shared-state hash)`
and serves the bytes on request. It still holds no copy of shared state more
authoritative than anyone else's during normal play — agreement remains a property
of determinism. The checkpoint is the tie-breaker used only when determinism has
already failed, and the ledger is what makes "the suffix after the checkpoint"
a well-defined object.

## 5. Phased plan

Every phase compiles, passes `make verify`, and ends with a two-terminal manual
check that a person can run. No phase depends on a later one. Where a phase
changes the journal schema it says so; the current schema is 11.

---

### Phase 1 — Local-first input (D-18)

**Goal.** A session's local cursor responds like a solo run: motions and typed
characters resolve against the cell the player has actually reached.

**Requirements.**

1. `Resources.Player` (or a dedicated local-view record) holds a predicted cell
   for the locally simulated cursor, plus the ordered set of crossings that
   produced it.
2. `World.LocalCursor()` returns the predicted cell. It is already the single
   read site (§2.5); no other change is needed at the 26 callers.
3. Every producer of a local `EventCursorMoveRequest` — `mode.OpJump`,
   `TypingSystem.moveCursorRight`, `NuggetSystem`'s jump — advances the prediction
   at production time, through one helper, so the prediction and the crossing are
   emitted from the same statement and cannot disagree.
4. `CursorSystem.move` announcing `EventCursorMoved` for the local cursor
   reconciles: if the applied cell matches the oldest outstanding prediction, drop
   that entry; otherwise clear the prediction queue and snap to the authoritative
   cell.
5. Render, camera and every player-domain effect keyed to the local cursor read
   the prediction. `SnapshotContext` reports it in the `view` record only.
6. A `shared`-profile system may not read it: extend the existing static check.

**Boundaries — this phase must not.** Change any wire message, the barrier, the
apply tick, or the class of any event. Predict anything other than the local
cursor's cell. Emit an event from the prediction. Add a value to `SnapshotShared`.
Predict on behalf of a cursor `SimulatesLocally` rejects.

**Tests.** A unit test that N rapid motions inside one lead window produce N
distinct predicted cells and N crossings, and that the authoritative positions
arrive in the same order; a test that a shared wall push-out snaps the prediction;
the existing `TestTwoLiveParticipantsStayInLockstep*` unchanged and still green —
this phase must not move the shared digest at all, which is the strongest
statement that it stayed inside the player domain.

**Manual acceptance.** Two terminals, `-host`/`-join`. Hold `w` and `l`: the local
cursor tracks the keys with no perceptible lag and no swallowed motion. Type a
line of a corpus paragraph at full speed: every character lands on its own cell,
as it does solo. The other terminal's remote cursor still moves in its six-tick
sync steps — unchanged, and the visible proof that only local state was predicted.
Neither status bar shows `DESYNC`.

---

### Phase 2 — Adaptive playout lead

**Goal.** Stop paying 150 ms of lead on a link whose worst path is 5 ms, and stop
under-paying on one whose path is 200 ms. This shrinks the residual gap between
Phase 1's prediction and the authoritative position, and it is what makes
`barrier_late` actionable instead of merely reported.

**Requirements.**

1. Measure per-link RTT and jitter from the existing heartbeat, and derive the
   session's worst-case active path across the mesh.
2. The coordinator owns the lead. It publishes a change as a crossing that applies
   at an absolute tick, so every instance changes lead at one tick — the same
   shape as a roster change.
3. The lead travels in the journal anchor and in the record stream, so a
   reproduction adopts the lead the run actually used at each point rather than a
   constant. This is the same trap D-14's map latch fell into, and it has the same
   answer.
4. `cmd/vif` exposes bounds (`-lead-min`, `-lead-max`), not a fixed value.
5. A path that cannot meet the minimum lead is refused at join time with a
   message, rather than admitted and left to diverge.

**Boundaries — this phase must not.** Let two instances hold different leads at
the same tick. Change the lead from anywhere but the coordinator. Derive the lead
from the live peer count (`SessionBarrier` exists precisely because that was
wrong). Let a reproduction read the lead from a transport it does not hold.

**Manual acceptance.** Loopback session converges to the minimum lead and the
status bar reports it; add 150 ms of artificial delay (`tc netem` on the loopback,
or an injected delay in the mesh harness) and watch the lead rise, `barrier_late`
stay at zero, and no `DESYNC` appear. Remove the delay and watch it fall.

---

### Phase 3 — The ordered crossing ledger

**Goal.** Make delivery loss repairable and make an unrepairable gap loud, so a
missing artifact stops the session instead of forking it.

**Requirements.**

1. Every crossing carries a session-global sequence. The producer assigns its own
   `(participant, producer sequence)`; the coordinator assigns the session
   sequence and republishes. Both travel with the artifact and survive relaying,
   exactly as `ApplyTick` does today.
2. Receivers acknowledge the highest contiguous session sequence. The `Ack` field
   already present in every frame header carries it.
3. Each participant retains an unacknowledged window and answers a gap request
   from it. The window is bounded by ticks, not by count.
4. A detected gap is requested once, with a deadline derived from the current lead.
   A gap that cannot be filled before its apply tick enters a new `RECOVERING`
   state; **it is never applied late**. `applyDue`'s current behaviour of
   publishing a late artifact and counting it is replaced by this.
5. `RECOVERING` freezes shared simulation on that instance and is visible in the
   status bar beside `DESYNC`/`DIVERGED`. Until Phase 5 it resolves only by the
   gap being filled or by the session ending; that is honest and is already better
   than a silent fork.
6. Distinguish the four failure kinds that today all reach the same counters:
   queue refusal, disconnect, decode failure, simulation mismatch.

**Boundaries — this phase must not.** Repair state; it repairs *delivery*. Change
what an artifact means or when it applies. Make the coordinator an authority over
gameplay outcomes — it orders and retains, it does not decide. Retain
unboundedly.

**Manual acceptance.** Two terminals with a deliberately shrunk send queue
(`SendQueueSize`) so refusal is reachable: drive a busy shield/storm exchange,
confirm `transport_lost_out` rises, a retransmission fills the gap, and no
`DESYNC` follows. Then block the retransmission and confirm the receiver reaches
`RECOVERING` at a named sequence rather than `DIVERGED` several seconds later.

---

### Phase 4 — Restorable shared checkpoint (D-19)

**Goal.** Produce a checkpoint that reconstructs the shared world exactly, and
prove it by construction rather than by review. This is the large phase and the
one everything after it depends on.

**Requirements.**

1. **A declared contract.** `SystemDef` gains a checkpoint declaration:
   `none` (holds no future-affecting private state), or `state` (implements
   `SaveShared([]byte) []byte` / `LoadShared([]byte) error`). The generator emits
   the table; the boundary suite fails a system whose declaration does not match
   what its file actually holds — the same construction that made D-15's profiles
   mechanical. This converts §2.3's unknown inventory into a build-time list.
2. **RNG continuation.** Add `FastRand.SetState`. Replace `WallSystem.mazeRng`'s
   `math/rand` with the project generator, or declare and serialize it. A system's
   RNG state is part of its `SaveShared` and nothing else.
3. **A versioned codec** with schema, build, config and corpus fingerprints, and a
   transfer-integrity hash. References use `core.Entity`, never dense indices.
4. **Derived-not-shipped.** The flow field, spatial index and passability grid are
   recomputed at install; only their *phase* (D-17's `TicksSinceCompute`,
   `PendingUpdate`, `LastTargets`) is serialized. This is where most of the naive
   size estimate disappears.
5. **Player domain never imports.** A checkpoint carries no player entity, no
   D-13 owner-authored cell except through an explicit ownership record, and no
   view, audio, transport or effect state.
6. **Staged install.** Load into a second world, validate, swap at a tick
   boundary. A checkpoint that fails validation is rejected before the swap.
7. **The round-trip test is the deliverable, not the codec.** Capture at tick T,
   load into a fresh process, then assert: identical `SnapshotShared`; identical
   next shared entity ID; identical next draw from every shared stream; and —
   the one that catches §2.3's hidden state — **identical shared digest after a
   further 500 ticks driven by an identical record stream**. Run it across the
   soak seeds and inside storm, gold, composite-destruction and reset.

**Boundaries — this phase must not.** Send a checkpoint on the wire yet, or change
any live behaviour. Load a checkpoint into a running session. Serialize anything
derivable. Treat `SnapshotShared` as the format.

**Manual acceptance.** `:d checkpoint save` and a headless `-checkpoint <file>`
resume: a solo run saved at tick T and resumed in a fresh process reaches the same
shared digest as the uninterrupted run at T+500. Do it once mid-storm and once
across a `:new` reset. Record capture bytes, capture time, install time and
allocation peak at the storm high-water — those are the numbers Phase 5's cadence
is chosen from.

---

### Phase 5 — Checkpoint-plus-suffix recovery, join and reconnect

**Goal.** Turn a detected fork into a repair, bound join cost, and delete the
unbounded log.

**Requirements.**

1. The coordinator captures a checkpoint on a measured cadence (start from every
   100 ticks, retaining three intervals, plus one after each large lifecycle
   transition) and commits its identity. Normal play transmits the identity, not
   the bytes.
2. **Recovery.** On confirmed divergence or an unresolved Phase 3 gap, the
   instance freezes shared simulation, requests the newest committed checkpoint it
   lacks, installs it into a staging world, replays the retained suffix from the
   ledger, verifies the digest against the commit, and swaps at one tick. Failure
   to verify ends the session with a message rather than resuming on a guess.
3. **Guest-local merge**, as desync.md §6.3 specifies: capture the local bundle
   first; rebind the participant to its rostered cursor; restore D-13 state only
   if the ownership epoch matches; restore durable personal mechanics whose shared
   references are still live; discard disposable effects; resubmit only
   unacknowledged producer sequences. Phase 1's prediction queue is discarded and
   re-seeded from the installed authoritative cell — clause (c) of D-18 already
   says how.
4. **Join and reconnect use the same path.** A mid-run joiner receives a
   checkpoint plus a suffix rather than the session from tick zero. This completes
   the running-host handoff that domain-design §9.4 leaves open, and it is what
   finally makes the joiner's residual gap a function of the cadence rather than
   of session length.
5. **Delete the tick-zero retention.** `App.SessionLog`, `SessionLogChunks`,
   `event.EncodeSessionLog`/`DecodeSessionLogChunk`, `MsgSessionLog`,
   `PendingJoin.ReceiveSessionLog` and `App.CatchUp`'s full-replay path all go.
   This is the plan's largest code reduction and removes the unbounded memory
   growth named in domain-design §9.4.

**Boundaries — this phase must not.** Elect a coordinator, migrate authority, or
attempt to survive host loss — that stays an explicit session end, and remains a
separate project with its own prerequisites (replicated ledger, split-brain rule).
Recover across a partition. Resume without a verified digest. Repair a checkpoint
whose fingerprints do not match the running build.

**Manual acceptance.** (a) Two terminals in a storm; corrupt one shared component
on the guest through a debug command; the guest reaches `DESYNC`, recovers, and
both digests agree, with the interruption visible and bounded. (b) Kill the guest's
process and rejoin mid-run against a *live, playing* host: the joiner arrives at
the session's tick within a bounded time and both terminals show both cursors.
(c) Black-hole the link past the silent timeout, restore it, and confirm reconnect
takes the same path. (d) Quit the host: the guest still reports unrecoverable host
loss — this phase deliberately does not change that.

---

### Phase 6 — Optimise from evidence

Nothing here is committed. Each item ships only if Phase 5's measurements say it
is needed.

- **Deltas**, if checkpoint bandwidth is material. A delta names its base and ends
  with the resulting digest; one missing delta invalidates its descendants, so
  periodic full checkpoints stay mandatory. Prefer store generation counters over
  per-tick snapshot comparison.
- **Bounded rollback**, if Phase 2's minimum lead is still visible in play. §2.4
  says the CPU is there and Phase 4 supplies the save/load contract; what remains
  is bounding player-domain side effects across re-simulated ticks, which is a
  real but now well-scoped piece of work.
- **Multi-link topology** from the CLI (`-join` accepting more than one address).
  The relay already works; this is the operator surface for it.
- **Authentication.** Populate `Config.TLS`, give `MsgAuthRequest`/`Response`
  meaning, and bind artifact authorship to identity. A prerequisite for anything
  beyond trusted peers, and for any future host election.
- **Host migration and partition health**, as a separate project. Election without
  replicated checkpoint and ledger history produces an empty authority.

## 6. What each phase retires

| Risk | Retired by | How it is proven |
|---|---|---|
| Multiplayer feels sluggish and eats input | Phase 1 | Rapid-motion unit test; two-terminal typing at full speed |
| Lead is wrong for the actual path | Phase 2 | `barrier_late` at zero under injected delay |
| A dropped or refused frame forks the session silently | Phase 3 | Forced queue refusal recovers; blocked retransmit reaches `RECOVERING`, not a fork |
| Hidden per-system state is missing from any state transfer | Phase 4 | Declared contract + 500-tick post-install digest equality |
| A logic divergence is permanent | Phase 5 | Deliberate corruption recovers to digest equality |
| Join cost and memory grow with session length | Phase 5 | Join time and retained bytes bounded by cadence |
| Host loss ends the game | Phase 6 / separate project | Out of scope, stated explicitly |

## 7. Instrumentation to add alongside

The measurement plan in desync.md §7 stands. Three additions this review found
missing, each of which should land with the phase that needs it:

- **Input-to-apply latency** for locally produced crossings, p50/p95/max, in ticks
  and milliseconds. This is the number Phase 1 exists to move and there is no
  counter for it today.
- **Prediction reconciliation**: predictions outstanding, snaps taken, and the
  cell distance of each snap. A rising snap rate means a shared producer is moving
  the cursor more than expected, which is a gameplay signal as well as a health one.
- **Ledger health**: contiguous acknowledged sequence per peer, gaps opened,
  gaps filled, retransmitted bytes, and time spent in `RECOVERING`.

For every manual reproduction, keep both logs *and* both journals, the exact
commit, both terminal sizes, the CLI arguments, and every pause or resize. A
one-sided log cannot name a first unequal value, which is what cost the 2026-08-30
investigation its byte-level proof.

## 8. Open questions for the maintainer

1. **Prediction scope.** Phase 1 predicts the cursor cell only. Should the local
   `EventCompositeMemberDestroyed` (gold typing) also predict its visual removal,
   or is a 50–150 ms delay on a *shared* glyph vanishing acceptable? The
   conservative answer is the one this plan takes; the aggressive one needs a
   rollback of that glyph's presentation when the crossing is refused.
2. **Checkpoint cadence and retention** are guesses until Phase 4 measures. 100
   ticks and three intervals is a starting point from desync.md, not a constant.
3. **Session end on host loss** stays the Phase 5 behaviour. Confirm that is
   acceptable for the intended deployment before Phase 6 sizing.
4. **Tower ownership in optional maps** (`config/main/tower.toml`, `config/td`)
   still binds every tower to slot zero. It does not block this plan, but it is a
   gameplay rule that should be settled before towers appear in a real session.
