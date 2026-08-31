# Multiplayer enhancement: local-first input and a restorable shared checkpoint

This is the outcome of a code review of the live multiplayer surface —
`internal/engine`, `internal/event`, `internal/journal`, `internal/network`,
`internal/service`, `internal/manifest`, and their consumers in `internal/system`,
`internal/mode` and `internal/app`. It states what the review measured, weighs the
recovery designs against what the code can carry, selects one, and plans it in
phases that each compile, ship, and can be checked from two terminals.

Domain rules remain authoritative in [domain-design.md](domain-design.md); the
divergence incident and the option scoring are in [desync.md](desync.md). This
document does not replace either. It adds a defect neither covers, and revises two
of desync.md's assumptions on measured evidence.

Every number below was measured during the review, on this commit, by driving the
real input path and the real two-participant harness. The probe source is
reproduced in §9 so the figures can be regenerated.

## 1. Conclusion first

Three problems are usually discussed as one. They are separable, they have
separable fixes, and conflating them is why the roadmap looks larger than it is.

| Problem | Measured symptom | Mechanism |
|---|---|---|
| **Responsiveness** | A session discards 80 % of fast cursor motion and scores 5 of 6 fast keystrokes as *typing errors* | Predict the owner's own cursor cell locally (**D-18**) — no wire change at all |
| **Delivery** | A lost, refused or late artifact forks the session silently | Session-ordered ledger with acknowledgement, gap detection, bounded retransmission |
| **Continuity** | Once forked nothing re-converges; mid-run join is *refused*; a departed host ends the run | A restorable shared checkpoint (**D-19**) plus a canonical artifact suffix |

The recommended architecture is **deterministic shared simulation, kept — with a
reliable ordered ledger, a restorable host-committed checkpoint, and a local-first
input path.** That agrees with desync.md's selection of checkpoint-plus-suffix over
the alternatives. It revises it in three ways:

1. **Responsiveness is phase one.** It is not polish. The measurements in §2.1
   show a session actively punishing a player for typing at speed, and it is the
   only item on the roadmap that needs no wire change, no codec and no authority.
2. **Shortening the playout lead does not fix it.** Measured at leads of 3, 2 and
   1 tick, the input loss is *identical*. The defect is a stale read, not a delay,
   so the adaptive-lead work — worth doing — must not be mistaken for the fix.
3. **Re-simulation is not the expensive part.** A full tick costs ~0.35 ms at six
   times the observed shared high-water. Catch-up, checkpoint validation and even
   bounded rollback are all affordable. The binding constraint is the save/load
   contract, which makes the checkpoint codec the *single* enabling investment.

## 2. What the review measured

### 2.1 A session discards most fast input

`World.PushCrossing` routes a D-3 artifact into `EventQueue.Push`, which offers it
to the wire sink before publishing (`internal/event/queue.go:41`). In a live
session `NetworkSystem.Cross` takes ownership and returns true, so **the event is
never published locally**: it is scheduled at `productionEpoch + delayTicks` and
published by `applyDue` at the opening of that tick. `EventCursorMoveRequest` is
`ClassBus` and `mode.OpJump` pushes it as a crossing, so every cursor move — `h`,
`w`, `f`, mouse, and the post-typing advance — takes the full lead.

Probe A, one `l` press, measuring when the producing instance's *own* store moves:

| | Applied after |
|---|---|
| Solo | Immediately, without a tick at all (`DispatchEventsImmediately`) |
| Session | **4 ticks — 200 ms** |

Latency alone would be tolerable. The compounding defect is that the input path
re-reads the authoritative store for every subsequent action:
`Router.handleMotion`, `handleCharMotion` and `handleInsertChar` all resolve from
`World.LocalCursor()`, and `TypingSystem.moveCursorRight` reads the store again to
request the advance. Inside the lead window every one of them sees a cell the
player has already left.

Probe B — five `l` presses with no tick between them, as a fast player produces
them:

| | Cursor moved |
|---|---|
| Solo | 5 of 5 cells |
| Session | **1 of 5 cells** |

Probe C — park on a run of typeable glyphs and type the exact six runes back to
back, which is the game's core loop:

| | Advanced | Typing errors scored |
|---|---|---|
| Solo | 6 of 6 cells | 0 |
| Session | **1 of 6 cells** | **5** |

That last row is the finding that reorders the roadmap. In a session, fast typing
does not merely fail to register: five of six correct keystrokes are resolved
against a cell whose glyph has already been consumed, so they are scored as
errors, which costs heat and energy and fires the error feedback path. The player
is punished for typing quickly, in a typing game.

Probe D — the same five presses at each negotiated lead:

| Lead | Cursor moved |
|---|---|
| 3 ticks | 1 of 5 cells |
| 2 ticks | 1 of 5 cells |
| 1 tick | 1 of 5 cells |

**The loss does not depend on the lead.** A one-tick lead is the shortest a session
can negotiate (`SessionOffer.Validate` rejects zero) and it loses exactly as much.
Any amount of deferral collapses every action issued between two ticks onto one
stale cell. Only a locally applied value fixes it.

Nothing in the current suite can see any of this: the parity harness drives one
tick per participant per step and asserts *agreement*, never *responsiveness*, and
both instances are equally late.

### 2.2 Detection exists; repair does not

The runtime digest is a good detector and, since per-record escalation, a good
diagnostic. It is not a protocol. Confirming desync.md §2, there is no repair on
any edge:

- **Delivery.** `SocketPort.push` drops an inbound notification when the poll
  buffer is full; `PeerManager.BroadcastExcept` counts a refused frame. Both are
  counted and logged once, neither retransmitted. `epochWindow.admit` suppresses
  duplicates but never *detects* a gap, although epochs are contiguous by
  construction — an idle tick still closes one — so the information is there and
  simply unused.
- **Lateness.** `applyDue` publishes an artifact whose apply tick has passed,
  increments `barrier_late`, and continues. Applying a crossing one tick late on
  one instance is precisely the divergence the barrier exists to prevent; the code
  chooses to apply it anyway.
- **Logic.** A determinism defect — the D-17 flow-field cache phase is the proven
  instance — loses no artifact, so no delivery mechanism can repair it. Only a
  state transfer can.
- **Membership.** `reportDisconnect` correctly names an unrecoverable host loss
  rather than waiting for a digest that can no longer arrive. There is no
  election, no state migration, no partition detector.

### 2.3 Mid-run join is refused, and the memory for it is paid anyway

This is sharper than domain-design §9.4's "`cmd/vif` does not yet complete its
running-host tick-phase handoff".

`App.initJournal` retains a complete record log for the life of any run started
with `-host` (`a.cfg.RetainSessionLog || a.cfg.HostAddress != ""`), growing
without bound. But `App.hostNetworkConfig` builds
`network.Coordinator{Assign, Release}` and **never sets `Log`**. So
`sendSessionLog` finds a nil accessor, returns `join: this session retains no
replayable log`, and `HostAcceptor` propagates it — the connection is closed.

A participant joining a `vif` host after tick zero is therefore **rejected**, while
the host pays unbounded memory for the log that would have served it. A dead-code
scan from the `cmd/vif` entry point confirms the whole transfer is unreachable in
the shipped binary: `event.EncodeSessionLog`, `event.DecodeSessionLogChunk`,
`LogRecord.record`, `PendingJoin.MidRun` and `PendingJoin.ReceiveSessionLog` are
all test-only. Phase 5 deletes this path, which is therefore strictly a win: it
removes the memory growth *and* code that no shipped path executes.

### 2.4 The checkpoint's real cost is the hidden-state surface

desync.md §6.2 lists what a keyframe must contain. The review's finding is that
the *list* is the risk, not the size, and that it is longer than that section
suggests. State that decides a future shared outcome lives in places a component
walk will not reach:

| Hidden state | Where | Serializable? |
|---|---|---|
| ~24 per-system RNG streams | private `*vmath.FastRand`, seeded in each `Init` | `State()` exists; **there is no `SetState`** |
| A second generator | `WallSystem.mazeRng`, a `math/rand.Rand` | not directly |
| **EXP3 route learning** | `AdaptationResource.Entries`: weights, pre-sampled `Pool`, consumer `Head`, `spin` — decides which route a spawned eye takes | yes, but it is a resource, not a store |
| **Genetic populations** | `GeneticResource.Registry`, a whole GA registry behind `sync.Mutex`/`atomic.Pointer` in `pkg/genetic` | needs an export contract it does not have |
| FSM runtime | `fsm.Machine` regions, `variables`, `delayedActions` | yes, and small |
| Throttled derivation phase | `FlowFieldCache.TicksSinceCompute`, `PendingUpdate`, `LastTargets` (D-17) | phase yes; the field itself should be recomputed, not shipped |
| Barrier | scheduled artifacts, `productionEpoch`, per-source epoch windows, `crossSeq` | yes |
| Allocator, scheduler | `nextEntityID`, per-domain counters, settle stamp, run/tick | yes |
| Per-system scratch | dirty sets, `NavigationSystem.routeRebuildTicks`, throttles, `GeneticSystem.tracking` | **no inventory exists** |

Two of these — the bandit and the GA — are `shared`-profile learned state that
neither existing document lists, and neither is covered by the digest, so a
divergence in either is silent until it moves an entity.

**A trap the round-trip test must be designed to catch.** Shared components store
*absolute* instants: `GenotypeComponent.SpawnTime`, `QuasarComponent.LastSpeedIncreaseAt`,
`ShieldComponent.LastDrainTime`, and `AdaptationEntry.DrainTime`. This is sound
today by a subtle argument — every reader takes a *difference* against
`Time.GameTime`, and the start gate freezes game time until every participant is
ready, so the origins align. It stops being sound the moment state is transferred:
a checkpoint captured on one process and installed on another imports instants
from a foreign clock origin, and every `now.Sub(stored)` is wrong by that
difference. None of it appears in `worldDigestScopedLocked`, so the error would be
invisible until an eye changed speed at the wrong moment. **The checkpoint must
store these as tick-relative values, and the round-trip test must run
cross-process with a deliberately offset clock origin** — a same-process
round-trip would pass while the real transfer fails.

This is why the plan makes checkpoint participation a **declared, statically
checked, per-system contract in `internal/manifest/definition.go`** — the
mechanism that already made domain profiles mechanical rather than reviewed
(D-15) — instead of one large serializer written by inspection.

### 2.5 Tick cost

Driven headless on `config/main` with the tower and storm regions forced, so the
shared population sits far above the incident trace's 500-entity high water:

| Shared positioned | Live total | Cost of one full tick |
|---:|---:|---:|
| 12 | ~400 | 13 µs |
| 2,487 | 3,157 | 123 µs |
| 2,866 | 3,854 | 239 µs |
| 2,984 | 4,046 | **353 µs** |

One `sharedDigestLocked(false)` costs 273 µs at that load, i.e. 45 µs/tick
amortised at the six-tick cadence.

Three consequences, and they are the evidence for §1's third revision:

1. **A tick costs 0.7 % of its 50 ms budget at six times the observed high
   water.** Simulation headroom is not the constraint anywhere in this plan.
2. **Catch-up runs at 3,000–75,000 ticks/second.** Replaying a ten-minute session
   costs seconds. Bounding the log is a memory decision, not a latency one.
3. **Rollback is CPU-affordable.** Re-simulating the whole lead costs ~1 ms, ~2 %
   of a tick. desync.md scored rollback tractability at 1 assuming many-tick
   re-simulation is expensive; it is not. What remains hard is the save/load
   contract and bounded side effects — and Phase 4 *is* that contract. Rollback
   therefore becomes an option this plan unlocks, though still not the first step,
   because Phase 1 removes the symptom rollback would exist to hide.

### 2.6 Findings fixed in this pass

Behaviour-neutral, and net **−161 lines**:

- **`World.LocalCursor()`** replaces 26 copies of
  `Positions.GetPosition(Resources.Player.Entity)` across `mode`, `app`, `engine`
  and four player-profile systems. It is a reduction now and the **single seam
  Phase 1 installs behind**: with one accessor, prediction is one implementation
  change rather than 26 call-site edits. Every current caller is `player`-profile
  or view, so the D-1 boundary the accessor must respect already holds.
- **`internal/mode/router.go` is 64 lines shorter.** Four handlers duplicated the
  same shape; `recordCommand`, `applyOperator`, `rememberFind` and `charCells`
  name the four repeated concepts. Three one-armed `switch intent.Operator` blocks
  and eight copies of the `SetLastCommand` tail collapse into one each.
- **The fabricated-identity admission path is gone.** `PeerManager.AddConnection`
  assigned a *connection-local* `nextID` to any stream accepted without a session
  handshake. The barrier's per-source epoch window and every roster lookup are
  keyed by canonical participant ID, so that path would have admitted a peer under
  an identity the session never issued — silent corruption rather than a refusal.
  Both call sites now fail loudly. `GetPeer` and `nextID` went with it.
- **The wall batch pool was write-only.** `ReleaseWallBatchRequest` was called
  from `WallSystem.HandleEvent`, but nothing ever called
  `AcquireWallBatchRequest`: all three producers use a composite literal. Payloads
  were being returned to a pool no allocation ever drew from. Removed.
- Dead on arrival, removed: `network.NewAckMessage`, `event.EmitBatch`,
  `event.HasPayload`, `Hub.Get`, `Hub.Names`, and the unreachable
  `RoleClient`/`RoleServer` aliases that gave `Transport.Start` two names for one
  branch.
- **`NetworkSystem` telemetry reset is table-driven.** Registration enrols each
  counter in the set `Init` clears, so a counter added to the constructor can no
  longer survive a reset holding the previous run's value.

Observed and deliberately left alone:

- `readCursorState`/`writeCursorState` are ~110 lines of symmetric field copying
  and could be collapsed. They should not be: they are the D-13 contract written
  out, and the domain model's claim that `NetworkSystem` is the *only* writer of a
  remote cursor's owner-authored cells is auditable precisely because the list is
  explicit. A table or reflection would trade a checkable invariant for lines.
- The reserved message codes (`MsgAck`, `MsgPeerList`, `MsgRoleAssign`,
  `MsgAuthRequest`/`Response`) are documented placeholders and Phase 3 claims
  `MsgAck`. Removing them would only churn the numbering.
- `Peer.OutSeq`/`InSeq` already put a per-link sequence and acknowledgement in
  every frame header, consumed by nothing. Phase 3 needs the *session* sequence
  instead — a per-link number cannot name a relayed artifact — but the header
  fields are free carriage for it.
- `event.EncodeSessionLog` marshals every chunk twice. It is unreachable from the
  binary (§2.3) and Phase 5 deletes it; not worth touching first.

## 3. Approaches, briefly

desync.md §5 scored nine options; that analysis stands and is not repeated. What
this review adds is where the measurements move a score.

| Direction | Fit | Verdict |
|---|---|---|
| **Keep determinism; add ledger + checkpoint + suffix** | Preserves the architecture, its tests, its 3–38 KB/s wire cost and its replay story. Repairs delivery loss and logic divergence alike, because a checkpoint does not care which caused the fork. | **Selected** |
| Authoritative state stream (full or delta) | Repairs trivially, but discards deterministic re-simulation — what nine of the seventeen domain rules exist to protect — and makes host uplink scale with clients. At the measured 2,900-entity load a naive full stream is >100 KB/tick. | Rejected as a first move; the fallback if determinism proves unmaintainable |
| Input prediction with world rollback | Now CPU-affordable (§2.5) and the only design that removes prediction error entirely. Needs the same save/load contract *plus* bounded side effects across ~50 systems, to solve a symptom Phase 1 removes for free. | Deferred to Phase 6, on evidence |
| Strict lockstep, replay-from-zero, restart, CRDT | Unchanged from desync.md: stalls, unbounded growth, no continuity, or wrong semantics for ordered combat. | Rejected |

The argument for keeping determinism is structural, not sentimental. The
shared/player split means **the entire player domain already runs locally with no
replication** — glyphs, weapons, projectiles, drains, nuggets, every effect. An
authoritative-stream rewrite would buy authority over the ~500 shared entities the
deterministic model already agrees on cheaply, at the cost of the mechanism that
makes the several thousand others free. Add authority only where the deterministic
model has no answer: as a tie-breaking checkpoint, not as a per-tick truth.

## 4. The selected design

### 4.1 Two new rules

**D-18 Predicted local state.** A value the local participant's own input
determines may be applied locally before its authoritative counterpart arrives,
provided that (a) the authoritative value is a pure function of the *same* local
producer's crossing, (b) only player-domain producers and the view read the
prediction, and (c) the prediction emits no event and enters no shared state,
digest or snapshot record outside `view`. When an authoritative value arrives that
the prediction did not produce, the prediction is discarded, not merged.

This is exactly the cursor cell's shape and no more. `EventCursorMoveRequest`
names an **absolute** target cell, so the authoritative result of the local
producer's own crossing is already known at production time: prediction here is
bookkeeping, not extrapolation, and cannot drift. The exceptions are the moves the
local producer did not make — the shared wall push-out, the gold jump, level setup
and reset. Each is a shared re-derivation landing identically on every instance,
and clause (c) resolves it by snapping.

The rule is statically checkable by the machinery `TestSystemDomainProfiles`
already uses for `ownerAuthoredStores`: a `shared`-profile system calling the
prediction accessor fails the build. Today's callers are `camera`, `dust`,
`motion_marker` and `splash` — all `player` — plus the input router and the view,
so the boundary holds before the first line of Phase 1 is written.

**D-19 Restorable shared state.** Every value that can change a future shared
outcome is either (a) a component in a shared entity's store, (b) declared by its
owning system in `internal/manifest/definition.go` as checkpoint state and
serialized through that declaration, or (c) provably re-derivable from (a) and (b)
at install time. Durations are stored relative to the checkpoint's tick, never as
absolute instants (§2.4). A system holding future-affecting private state without
declaring it fails the boundary suite.

`SnapshotShared` and its FNV digest remain a comparison surface and are explicitly
*not* the checkpoint format — desync.md says so; stating it as a rule stops a
future change from quietly conflating them.

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

Phase 1 is independent of everything to its right; Phases 2 and 3 are
independently useful whether or not Phase 4 ever ships.

### 4.3 Authority, restated

The coordinator gains one new power and no others: it **commits** a checkpoint
identity `(session, run, tick, session sequence, shared-state hash)` and serves
the bytes on request. During normal play it still holds no copy of shared state
more authoritative than anyone else's — agreement remains a property of
determinism. The checkpoint is the tie-breaker used only once determinism has
already failed; the ledger is what makes "the suffix after the checkpoint" a
well-defined object.

## 5. Phased plan

Every phase compiles, passes `make verify`, and ends with a two-terminal check a
person can run. No phase depends on a later one. The journal schema is 11 today;
a phase that changes it says so.

---

### Phase 1 — Local-first input (D-18)

**Goal.** A session's local cursor responds like a solo run: motions and typed
characters resolve against the cell the player has actually reached.

**Requirements.**

1. `Resources.Player` holds a predicted cell for the locally simulated cursor plus
   the ordered queue of crossings that produced it.
2. `World.LocalCursor()` returns the predicted cell. It is already the single read
   site (§2.6); no other change is needed at the 26 callers.
3. Every producer of a local `EventCursorMoveRequest` — `mode.OpJump`,
   `TypingSystem.moveCursorRight`, `NuggetSystem`'s jump — advances the prediction
   at production time through one helper, so prediction and crossing leave from
   the same statement and cannot disagree.
4. `CursorSystem.move` announcing `EventCursorMoved` for the local cursor
   reconciles: matching the oldest outstanding prediction pops it; anything else
   clears the queue and snaps.
5. Render, camera and player-domain effects keyed to the local cursor read the
   prediction. `SnapshotContext` reports it in the `view` record only.
6. A `shared`-profile system may not read it: extend the existing static check.

**Boundaries — must not.** Change any wire message, the barrier, an apply tick or
an event class. Predict anything but the local cursor's cell. Emit an event from
the prediction. Add a value to `SnapshotShared`. Predict for a cursor
`SimulatesLocally` rejects.

**Tests.** Promote the four probes in §9 to assertions: session figures must reach
the solo figures. The existing `TestTwoLiveParticipantsStayInLockstep*` must be
untouched and still green — this phase must not move the shared digest at all,
which is the strongest statement that it stayed inside the player domain.

**Manual acceptance.** Two terminals, `-host`/`-join`. Hold `w` and `l`: the local
cursor tracks the keys with no perceptible lag and no swallowed motion. Type a
corpus line at full speed: every character lands on its own cell and the error
counter stays at zero, as it does solo. The remote cursor still moves in its
six-tick sync steps — unchanged, and the visible proof that only local state was
predicted. Neither status bar shows `DESYNC`.

---

### Phase 2 — Adaptive playout lead

**Goal.** Stop paying 150 ms of lead on a 5 ms path and stop under-paying on a
200 ms one. This shrinks Phase 1's residual prediction gap and makes
`barrier_late` actionable rather than merely reported. It is *not* the
responsiveness fix — §2.1 probe D shows the lead length does not affect input
loss.

**Requirements.**

1. Measure per-link RTT and jitter from the existing heartbeat; derive the
   session's worst-case active path across the mesh.
2. The coordinator owns the lead and publishes a change as a crossing applying at
   an absolute tick, so every instance changes lead at one tick — the same shape
   as a roster change.
3. The lead travels in the journal anchor and the record stream, so a reproduction
   adopts the lead the run actually used at each point rather than a constant.
   This is the trap D-14's map latch fell into and it has the same answer.
4. `cmd/vif` exposes bounds (`-lead-min`, `-lead-max`), not a fixed value.
5. A path that cannot meet the minimum lead is refused at join time with a
   message, not admitted and left to diverge.

**Boundaries — must not.** Let two instances hold different leads at one tick.
Change the lead from anywhere but the coordinator. Derive it from the live peer
count — `SessionBarrier` exists because that was wrong. Let a reproduction read it
from a transport it does not hold.

**Manual acceptance.** A loopback session converges to the minimum lead and the
status bar reports it. Add 150 ms with `tc netem` and watch the lead rise,
`barrier_late` stay at zero and no `DESYNC` appear; remove it and watch it fall.

---

### Phase 3 — The ordered crossing ledger

**Goal.** Make delivery loss repairable and an unrepairable gap loud, so a missing
artifact stops the session instead of forking it.

**Requirements.**

1. Every crossing carries a session-global sequence. The producer assigns
   `(participant, producer sequence)`; the coordinator assigns the session
   sequence and republishes. Both travel with the artifact and survive relaying,
   as `ApplyTick` already does.
2. Receivers acknowledge the highest contiguous session sequence, in the `Ack`
   header field every frame already carries.
3. Each participant retains an unacknowledged window, bounded by ticks rather than
   count, and answers a gap request from it.
4. A detected gap is requested once with a deadline derived from the current lead.
   A gap unfilled by its apply tick enters a new `RECOVERING` state; **it is never
   applied late.** This replaces `applyDue`'s current behaviour of publishing a
   late artifact and counting it.
5. `RECOVERING` freezes shared simulation on that instance and appears in the
   status bar beside `DESYNC`/`DIVERGED`. Until Phase 5 it resolves only by the
   gap being filled or the session ending — honest, and already better than a
   silent fork.
6. Separate the four failure kinds that today share counters: queue refusal,
   disconnect, decode failure, simulation mismatch.

**Boundaries — must not.** Repair state; it repairs *delivery*. Change what an
artifact means or when it applies. Make the coordinator an authority over gameplay
outcomes — it orders and retains. Retain unboundedly.

**Manual acceptance.** Two terminals with `SendQueueSize` shrunk so refusal is
reachable: drive a busy shield/storm exchange, confirm `transport_lost_out` rises,
a retransmission fills the gap, and no `DESYNC` follows. Then block the
retransmission and confirm the receiver reaches `RECOVERING` at a named sequence
rather than `DIVERGED` seconds later.

---

### Phase 4 — Restorable shared checkpoint (D-19)

**Goal.** Produce a checkpoint that reconstructs the shared world exactly, and
prove it by construction. This is the large phase and everything after it depends
on it.

**Requirements.**

1. **A declared contract.** `SystemDef` gains a checkpoint declaration — `none`
   (holds no future-affecting private state) or `state` (implements
   `SaveShared`/`LoadShared`). The generator emits the table; the boundary suite
   fails a system whose declaration does not match what its file holds, the same
   construction that made D-15's profiles mechanical. This turns §2.4's unknown
   inventory into a build-time list.
2. **RNG continuation.** Add `FastRand.SetState`. Replace `WallSystem.mazeRng`'s
   `math/rand` with the project generator or declare and serialize it.
3. **The two learned resources get export contracts**: `AdaptationResource`
   (weights, pool, head, spin) and `GeneticResource` (per-species populations, via
   a new `pkg/genetic` export/import that does not leak the mutex).
4. **A versioned codec** with schema, build, config and corpus fingerprints and a
   transfer-integrity hash. References use `core.Entity`, never dense indices.
5. **Tick-relative durations.** No absolute `time.Time` crosses the boundary
   (§2.4).
6. **Derived, not shipped.** The flow field, spatial index and passability grid
   are recomputed at install; only D-17's *phase* is serialized. Most of the naive
   size estimate disappears here.
7. **Player domain never imports.** No player entity, no D-13 owner-authored cell
   except through an explicit ownership record, no view, audio, transport or
   effect state.
8. **Staged install.** Load into a second world, validate, swap at a tick
   boundary; a checkpoint failing validation is rejected before the swap.
9. **The round-trip test is the deliverable, not the codec.** Capture at tick T,
   load **in a separate process whose clock origin is deliberately offset**, then
   assert: identical `SnapshotShared`; identical next shared entity ID; identical
   next draw from every shared stream; and — the one that catches §2.4's hidden
   state — **identical shared digest after a further 500 ticks driven by an
   identical record stream**. Run it across the soak seeds and inside storm, gold,
   composite destruction and reset. A same-process round-trip is not sufficient
   and must not be the gate.

**Boundaries — must not.** Send a checkpoint on the wire yet, or change any live
behaviour. Load one into a running session. Serialize anything derivable. Treat
`SnapshotShared` as the format.

**Manual acceptance.** `:d checkpoint save` and a headless `-checkpoint <file>`
resume: a solo run saved at T and resumed in a fresh process reaches the same
shared digest as the uninterrupted run at T+500. Once mid-storm, once across a
`:new` reset. Record bytes, capture time, install time and allocation peak at the
storm high water — Phase 5's cadence is chosen from those numbers.

---

### Phase 5 — Checkpoint-plus-suffix recovery, join and reconnect

**Goal.** Turn a detected fork into a repair, bound join cost, and delete the
unreachable log path.

**Requirements.**

1. The coordinator captures on a measured cadence (start at every 100 ticks
   retaining three intervals, plus one after each large lifecycle transition) and
   commits the identity. Normal play transmits the identity, not the bytes.
2. **Recovery.** On confirmed divergence or an unresolved Phase 3 gap: freeze
   shared simulation, request the newest committed checkpoint this instance lacks,
   install into a staging world, replay the retained suffix from the ledger,
   verify the digest against the commit, swap at one tick. Failure to verify ends
   the session with a message rather than resuming on a guess.
3. **Guest-local merge**, as desync.md §6.3 specifies: capture the local bundle
   first; rebind to the rostered cursor; restore D-13 state only if the ownership
   epoch matches; restore durable personal mechanics whose shared references are
   live; discard disposable effects; resubmit only unacknowledged producer
   sequences. Phase 1's prediction queue is discarded and re-seeded from the
   installed authoritative cell — D-18 clause (c) already says how.
4. **Join and reconnect use the same path.** A mid-run joiner receives a
   checkpoint plus a suffix. This is what makes the joiner's residual gap a
   function of the cadence rather than of session length, and it closes the
   running-host handoff domain-design §9.4 leaves open.
5. **Delete the tick-zero retention.** `App.SessionLog`, `SessionLogChunks`,
   `event.EncodeSessionLog`/`DecodeSessionLogChunk`, `MsgSessionLog`,
   `PendingJoin.MidRun`/`ReceiveSessionLog` and `App.CatchUp`'s full-replay path
   all go, along with the `HostAddress != ""` clause in `initJournal`. Per §2.3
   none of it is reachable from the binary today, so this removes unbounded memory
   growth *and* dead code in one step.

**Boundaries — must not.** Elect a coordinator, migrate authority, or survive host
loss: that stays an explicit session end and a separate project with its own
prerequisites (replicated ledger, split-brain rule). Recover across a partition.
Resume without a verified digest. Accept a checkpoint whose fingerprints do not
match the running build.

**Manual acceptance.** (a) Two terminals in a storm; corrupt one shared component
on the guest via a debug command; it reaches `DESYNC`, recovers, both digests
agree, and the interruption is visible and bounded. (b) Kill the guest process and
rejoin mid-run against a *live, playing* host — which today is refused outright
(§2.3): the joiner arrives at the session's tick within a bounded time and both
terminals show both cursors. (c) Black-hole the link past the silent timeout,
restore it, confirm reconnect takes the same path. (d) Quit the host: the guest
still reports unrecoverable host loss — this phase deliberately does not change
that.

---

### Phase 6 — Optimise from evidence

Nothing here is committed; each ships only if Phase 5's measurements ask for it.

- **Deltas**, if checkpoint bandwidth is material. A delta names its base and ends
  with the resulting digest; one missing delta invalidates its descendants, so
  periodic full checkpoints stay mandatory. Prefer store generation counters over
  per-tick snapshot comparison.
- **Bounded rollback**, if Phase 2's minimum lead is still visible in play. §2.5
  says the CPU is there and Phase 4 supplies the save/load contract; what remains
  is bounding player-domain side effects across re-simulated ticks.
- **Multi-link topology** from the CLI (`-join` taking more than one address). The
  relay already works; this is the operator surface for it.
- **Authentication.** Populate `Config.TLS`, give `MsgAuthRequest`/`Response`
  meaning, bind artifact authorship to identity. A prerequisite for anything
  beyond trusted peers, and for any future host election.
- **Host migration and partition health**, as a separate project. Election without
  replicated checkpoint and ledger history produces an empty authority.

## 6. What each phase retires

| Risk | Retired by | Proof |
|---|---|---|
| A session discards most fast input and scores it as errors | Phase 1 | §9 probes reach their solo figures |
| Lead is wrong for the actual path | Phase 2 | `barrier_late` at zero under injected delay |
| A dropped or refused frame forks the session silently | Phase 3 | Forced refusal recovers; blocked retransmit reaches `RECOVERING`, not a fork |
| Hidden per-system and learned state is missing from any transfer | Phase 4 | Declared contract + cross-process, clock-offset, 500-tick digest equality |
| A logic divergence is permanent | Phase 5 | Deliberate corruption recovers to digest equality |
| Mid-run join is refused while its memory is paid | Phase 5 | A live host accepts a joiner; retention deleted |
| Host loss ends the game | Phase 6 / separate project | Out of scope, stated |

## 7. Instrumentation to add

desync.md §7's measurement plan stands. Three additions this review found missing,
each landing with the phase that needs it:

- **Input-to-apply latency** for locally produced crossings, p50/p95/max, in ticks
  and milliseconds. This is the number Phase 1 exists to move and no counter for
  it exists.
- **Prediction reconciliation**: predictions outstanding, snaps taken, cell
  distance per snap. A rising snap rate means a shared producer is moving the
  cursor more than expected — a gameplay signal as well as a health one.
- **Ledger health**: contiguous acknowledged sequence per peer, gaps opened and
  filled, retransmitted bytes, time in `RECOVERING`.

For every manual reproduction keep both logs *and* both journals, the exact
commit, both terminal sizes, the CLI arguments, and every pause or resize. A
one-sided log cannot name a first unequal value, which is what cost the
2026-08-30 investigation its byte-level proof.

## 8. Open questions

1. **Prediction scope.** Phase 1 predicts the cursor cell only. Should the local
   `EventCompositeMemberDestroyed` (gold typing) also predict its visual removal,
   or is a lead's delay on a *shared* glyph vanishing acceptable? The conservative
   answer is the one taken here; the aggressive one needs a rollback of that
   glyph's presentation when the crossing is refused.
2. **Checkpoint cadence and retention** are guesses until Phase 4 measures. 100
   ticks and three intervals is desync.md's starting point, not a constant.
3. **Session end on host loss** stays Phase 5 behaviour. Confirm that is
   acceptable for the intended deployment before sizing Phase 6.
4. **Mid-run join today.** §2.3 shows it is refused. Wiring `Coordinator.Log`
   would make it *attempt* to work, but the tick-phase handoff is incomplete, so a
   joiner could arrive subtly desynced. Leaving the clean refusal until Phase 5 is
   the safer call — confirm.
5. **Tower ownership in optional maps** (`config/main/tower.toml`, `config/td`)
   still binds every tower to slot zero. It does not block this plan, but it is a
   gameplay rule to settle before towers appear in a real session.

## 9. Reproducing the measurements

The probes were run as a temporary `internal/app` test file against the real input
path (`App.Inject` is what `cmd/vif`'s event loop calls) and the real
two-participant harness (`meshSession`). They are reproduced here rather than
committed, because as assertions they would encode the current defect; Phase 1
promotes them to tests asserting the solo figures.

```go
func motion(op input.MotionOp) *input.Intent {
    return &input.Intent{Type: input.IntentMotion, Motion: op, Count: 1}
}

// A: ticks between an input and the producing instance's own store moving.
// B: five presses with no tick between them -> cells actually moved.
// C: park on a glyph run, type its exact runes back to back -> cells advanced
//    and typing.errors delta.
// D: B repeated with Resources.Network.BarrierDelayTicks overridden to 3, 2, 1.
```

Solo runs use `mustHeadless` and `a.Tick(1)`; session runs use
`meshSession(t, seed, 2, [][2]int{{1, 2}})` and `tickAll`. For D, override the
negotiated lead after `AttachTransport` and re-run `activateNetworkSession`.

The tick-cost table in §2.5 comes from `towerConfig` with the tower and storm
regions forced via `a.Region(event.RegionSpawn, ...)`, timing `a.Tick(200)`
batches and counting `engine.ScopeShared`-selected entities in
`world.Positions.Entities()`.
