# Desynchronisation: present guarantees, diagnosis, and recovery roadmap

This document describes live multiplayer synchronisation as it exists, what the
2026-08-30 two-terminal trace establishes, and how to make a session recoverable
rather than merely deterministic. It is not a claim that recovery already exists.
The domain rules remain authoritative in [domain-design.md](domain-design.md);
transport details are in
[services-and-networking.md](services-and-networking.md).

## 1. Executive conclusion

The current strategy is useful but incomplete. Deterministic re-simulation reduces
bandwidth and makes defects observable, but it is not a continuity protocol. A
deterministic program can still diverge when an artifact is lost, arrives after its
apply tick, is applied twice, or when one instance executes an unmodelled local
input. Once divergence exists, hashes report it but cannot choose an authority or
restore state.

The best next architecture for vi-fighter is **host-authoritative shared-state
keyframes plus an ordered, retained crossing stream**:

1. keep deterministic shared simulation and the D-3 crossing boundary;
2. give every crossing a session-global order and reliable acknowledgement;
3. capture periodic host shared-world keyframes at committed ticks;
4. on divergence or reconnect, install a keyframe and replay the retained suffix;
5. preserve the guest's player-domain world and merge its owner-authored cursor
   state through an explicit contract.

Start with full shared keyframes. The observed shared population is small enough
that a correct measurable codec is preferable to an immediately complex delta
protocol. Add deltas only if measurements show keyframe bandwidth is material.
The host can provide ordering and the canonical checkpoint first because it
already allocates identity, publishes the anchor, and serialises roster/reset
changes. Session continuity later requires that committed checkpoints and ledger
history be replicated before electing a replacement; election alone preserves no
game state.

## 2. What is synchronised today

Each process owns one `World`. Shared entities are re-simulated on every instance;
player-domain entities exist only for their local participant. The wire carries:

- D-3 crossing artifacts scheduled at an absolute apply tick after a fixed
  three-tick playout lead;
- D-13 owner-authored cursor state copied to non-owning instances;
- roster departures;
- periodic hashes of `SnapshotShared()` for detection.

TCP framing prevents partial reads from becoming messages. The mesh epoch window
deduplicates relayed epochs, but acknowledgements are observational: there is no
retransmission, retained unacknowledged window, negative acknowledgement, or gap
repair. A bounded receive channel may drop a notification and a bounded send queue
may refuse a frame. Both are counted and logged, but neither is repaired.

The digest cadence is six ticks. Two consecutive mismatches raise `DESYNC`, five
raise `DIVERGED`, and later agreement clears the active divergence. Current builds
request per-record hashes after the first mismatch and expose differing records in
`network.sync_records`. This identifies a surface; it does not contain repair data.

Late join uses a retained session log from tick zero. It is not general live
resynchronisation: the CLI does not phase a mid-run joiner onto a running host, the
log grows without bound, and no agreed checkpoint accepts a partial suffix.

| Condition | Detected today | Repaired today |
|---|---|---|
| Shared gameplay logic differs | Digest, after sampling delay | No |
| Inbound poll queue drops | Counter and warning | No |
| Outbound send queue refuses | Counter and warning | No |
| Epoch gap/delayed artifact | Consequence or lateness telemetry | No retransmit |
| Peer exits | Disconnect and `NET:DOWN`; explicit current-branch status | Roster departure only |
| Host disappears from guest | Explicit unrecoverable-host status | No host election |
| Network partition | Loss on remaining edges | No authority or merge |

## 3. Analysis of `vif-log-260830-192013.jsonl`

The supplied file contains one process's log: it compares peer 2 but does not
contain a second process identity or a second statistics stream. Paired guest logs
or both journals are required to identify the first unequal event/value.

| First reported tick | First category | Outcome visible in trace |
|---:|---|---|
| 792 | kinetics | `DIVERGED` at 810; agreement restored at tick 822 |
| 1146 | kinetics | `DIVERGED` at 1164; agreement restored at tick 1200 |
| 1704 | positions | `DIVERGED` at 1722; persistent through exit at tick 2186 |

The trace reports `late=0`, `transport_lost_in=0`,
`transport_lost_out=0`, and no dropped event-queue records. A detected transport
overflow or late barrier application is therefore unlikely for this run. This
does not prove identical epochs because the build did not log a comparable global
crossing sequence.

The permanent mismatch is not triggered by the second storm. It begins at tick
1704; `StormSetup` starts at 1773 and `StormActive` at 1783. A storm can amplify an
existing difference, but this ordering rules it out as the origin. The earlier
kinetic mismatches recover, demonstrating why one bad digest is not proof of
permanent divergence.

### Relevant fixes already on `main`

The trace predates two changes:

- local participant binding no longer emits `EventCursorMoved`. That event dirtied
  the throttled navigation cache only on the participant being bound, shifted its
  recomputation phase, and eventually produced kinetic differences (D-17);
- digest escalation now exchanges hashes per snapshot record, so future logs name
  `network.sync_records` rather than stopping at `positions` or `kinetics`.

The local-view defect accounts for the observed progression from kinetics to
positions and for apparent recovery when affected movers die. A targeted
two-live-participant run on current `main` used the incident seed for 2,200
boundaries and stayed in sync; the existing
`TestLocalViewChangesLeaveTheFlowFieldPhaseAlone` pins the causal cache-phase
boundary. That is strong attribution, although a one-sided pre-fix log cannot
provide a byte-level proof of the first unequal value. A manual rerun should still
retain both logs and journals.

### Early dimensioning

The samples are cumulative except for live counts. "Not shared-positioned" is
`live total - shared positioned`; it includes player-domain entities and any
shared entity without a position, so it must not be treated as an exact domain
count.

| Tick | Live total | Shared positioned | Not shared-positioned | Created | Destroyed | Sent | Received | Digest mismatches |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 200 | 238 | 13 | 225 | 2,294 | 2,060 | 8 | 189 | 0 |
| 400 | 960 | 2 | 958 | 3,831 | 2,875 | 8 | 353 | 0 |
| 600 | 970 | 13 | 957 | 5,160 | 4,193 | 14 | 456 | 0 |
| 800 | 1,095 | 38 | 1,057 | 7,123 | 6,037 | 100 | 549 | 3 |
| 1,000 | 829 | 29 | 800 | 10,631 | 9,845 | 262 | 647 | 6 |
| 1,200 | 1,069 | 500 | 569 | 14,099 | 13,093 | 422 | 740 | 16 |
| 1,400 | 1,196 | 267 | 929 | 15,831 | 14,698 | 552 | 1,345 | 16 |
| 1,600 | 478 | 65 | 413 | 19,407 | 19,024 | 729 | 1,530 | 16 |
| 1,800 | 1,125 | 482 | 643 | 22,918 | 21,903 | 920 | 1,761 | 33 |
| 2,000 | 1,036 | 7 | 1,029 | 25,645 | 24,719 | 1,037 | 2,057 | 67 |

At tick 2,000 (100 seconds), the run averaged about 256 entity creations and 247
destructions per second. Local crossings averaged 10.4/s and received crossings
20.6/s. The busiest sampled ten-second windows reached 19.1 local and 60.5
received crossings/s. Storm setup raised shared positioned population from tens
to a sampled high-water of 500.

Existing complete-frame measurements are 44 bytes for an empty epoch, 567 bytes
for four cursor moves, 1,771 bytes for six resolved three-member shield hits, and
703 bytes for one owner-state sync. At 20 ticks/s and the six-tick state cadence,
the corresponding steady-state envelopes are about 3.2 KiB/s idle, 13.7 KiB/s at
four crossings/tick, and 37.8 KiB/s at the busy shield rate, per direction and
owned cursor. These rates favour retaining the artifact model.

The log does not contain component byte sizes. A deliberately rough uncompressed
full-keyframe budget at the observed 500-shared-positioned high-water is 40–70
KiB: roughly 8 KiB for identities/component masks, 8 KiB for positions, 20–30
KiB for storm/combat members, 8–12 KiB for memberships/variable structures, and
5–15 KiB for FSM/RNG/system/barrier state. This is an order-of-magnitude planning
estimate, not a measured codec. Continuously sending it would cost 40–70 KiB/s at
one second or 8–14 KiB/s at five seconds. Retaining it locally and transferring
only on join/recovery makes normal transfer cost the small commitment/ack, not
`keyframe_size / interval`.

## 4. Why determinism alone cannot provide continuity

Desync is not inevitable merely because Internet latency varies. The playout lead
absorbs variation inside its window, TCP orders bytes while connected, and correct
deterministic simulation may stay aligned indefinitely. The protocol is fragile
because it has no recovery when those assumptions are exceeded:

- one instance drops/refuses an artifact;
- an artifact arrives after its apply tick and later state already depends on it;
- local input, timer, geometry, iteration order, floating-point behavior, or cache
  phase leaks into shared state;
- a participant disconnects beyond retained live history;
- partitioned groups continue and create incompatible histories.

Journaling is an excellent reproduction mechanism, but it is not recovery by
itself. Recovery needs an authority, checkpoint identity, complete state format,
retained ordered suffix, and rules for intentionally omitted local state.

## 5. Industry patterns and fit

Scores are contextual, from 1 (poor) to 5 (strong). The weighted result uses
current-domain fit 25%, recovery correctness 25%, delivery tractability 15%,
bandwidth 15%, latency/play feel 10%, and session continuity 10%. It compares
directions for this codebase, not universal networking quality.

| Option | Domain fit | Recovery | Tractability | Bandwidth | Latency | Continuity | Weighted |
|---|---:|---:|---:|---:|---:|---:|---:|
| End session and restart | 5 | 1 | 5 | 5 | 3 | 1 | 3.40 |
| Replay complete log from tick zero | 5 | 3 | 4 | 4 | 1 | 2 | 3.50 |
| Strict delayed lockstep | 4 | 3 | 3 | 5 | 1 | 2 | 3.25 |
| **Host keyframe + canonical artifact suffix** | **5** | **5** | **3** | **4** | **3** | **4** | **4.25** |
| Periodic authoritative full-state stream | 3 | 5 | 3 | 2 | 4 | 4 | 3.55 |
| Authoritative delta-state stream | 3 | 5 | 1 | 5 | 4 | 4 | 3.70 |
| Input prediction and rollback | 2 | 4 | 1 | 4 | 5 | 3 | 3.05 |
| Dedicated authoritative server | 2 | 5 | 1 | 3 | 4 | 4 | 3.15 |
| CRDT/eventual merge | 1 | 1 | 1 | 4 | 5 | 2 | 1.95 |

Restart remains the safe fallback. Complete replay is useful for diagnosis but
its time, memory, and transfer grow with session length, and concurrent
interactive scheduling is outside the bit-exact replay claim. Strict lockstep
turns network variance into global stalls and still needs reconnect state.

Full state streaming has simple repair semantics but changes the current
peer-derived model and makes host uplink scale with clients. Delta streaming can
be efficient, but it adds acknowledged baselines, component removals, schema-aware
field encoding, and full fallback after a missing baseline. A dedicated server is
attractive for anti-cheat/public matchmaking but is a much larger ownership and
deployment change.

Rollback is excellent for hiding short input-prediction errors, but requires the
same complete save/load contract plus cheap frequent saves, bounded side effects,
and rapid many-tick simulation. Vi-fighter sends resolved crossings rather than a
compact per-frame input stream and has high-churn ECS/FSM encounters, so it is not
the first recovery step. CRDT-style merging is unsuitable for ordered collision,
combat, death, and contested progression.

Action games commonly combine authority, snapshots, sequence/ack histories,
interpolation, prediction, and selective rollback. Reliability preserves ordered
causality; keyframes bound recovery; prediction hides latency; rollback corrects
short speculation; authority resolves conflict.

## 6. Recommended protocol

### 6.1 Authority and ordering

The host is authoritative for shared state. Every shared crossing gets a monotonic
`SessionSeq` assigned by the host. Guests submit D-3 artifacts with producer ID and
producer sequence; the host validates, orders, and republishes them. A commit is:

`(session ID, run, tick, session sequence, shared-state hash)`.

Guests acknowledge the highest contiguous sequence. Gaps request retransmission
before the apply deadline; an unfillable/late gap enters recovery instead of being
applied opportunistically. This makes the host authoritative for order without
discarding the D-3 artifact boundary.

### 6.2 Shared keyframe

A keyframe contains only state required to continue shared simulation:

- run/tick/session sequence and RNG continuation state;
- next shared entity ID and shared create/destroy counters;
- every shared entity and simulation-relevant attached component;
- shared FSM state/timers and navigation cache phase/data, or canonical inputs that
  reproduce them at the same phase;
- every other hidden future-affecting system value, including pending storm
  spawns, scheduled clock actions, throttles, dirty sets, and local scratch that
  changes a later shared decision;
- scheduled barrier artifacts, next production epoch, source admission windows,
  per-source sequences, and the committed ledger position;
- D-14 context, status/progression read by systems, roster and ownership metadata;
- schema/build/corpus/config fingerprints, a strong transfer-integrity hash, and
  the final shared diagnostic digest.

It excludes player entities, terminal/view/audio/effects, transport telemetry, and
D-13 cursor values except through an explicit ownership record. References use
stable `core.Entity`, never dense indices. Install into a staging world, validate,
then swap atomically at a tick boundary.

`SnapshotShared` and its FNV digest are canonical comparison/diagnostic surfaces,
not a loadable checkpoint or a sufficiently strong transferred-object identity.

### 6.3 Guest-local merge

Before install, the guest captures a `LocalRecoveryBundle`: player-domain durable
state, owner-authored cursor state, local input sequence, and unacknowledged
submissions. After keyframe install:

1. bind the participant to its rostered shared cursor;
2. restore D-13 state only if its ownership epoch matches;
3. restore durable personal mechanics whose shared references remain live;
4. discard disposable D-6 effects and stale references;
5. resubmit only producer sequences the host has not acknowledged;
6. replay the authoritative suffix and verify the final digest before resuming.

Discarding/recreating presentation effects is safer than serialising every visual
transient in the first implementation.

### 6.4 Cadence and deltas

Choose cadence by bounds on catch-up CPU, retained bytes, encode/install pause,
recovery time, throughput, and worst shared population/dirty rate. A measurement
starting point is a full keyframe every 100 ticks (5 seconds at 20 Hz), retaining
at least three intervals, with extra keyframes after large lifecycle transitions.
This is an experiment, not a protocol constant. Normal operation commits its
identity; it does not need to transmit every keyframe. Transfer one when a joiner
or recovering participant lacks the chosen committed copy.

Add deltas only after full keyframes work. A delta names its base, carries canonical
entity create/destroy and component upsert/remove operations, and ends with the
resulting digest. One missing delta invalidates its descendants, so periodic full
keyframes remain mandatory. Store generation counters/dirty bits are preferable to
full snapshot comparison every tick.

## 7. Measurement plan

| Metric | Why |
|---|---|
| Shared entities/components by type | Sizes keyframes and dominant stores. |
| Changed/created/destroyed components per tick | Sizes deltas and churn. |
| Raw/compressed keyframe and delta bytes p50/p95/p99/max | Determines cadence/join cost. |
| Crossing bytes/tick p50/p95/p99/max | Sizes retained history and queues. |
| Capture/codec/install CPU and allocations p50/p95/p99/max | Protects the 50 ms tick budget. |
| Ack lag, gaps, retransmit bytes | Separates network loss from logic defects. |
| RTT, jitter, queue occupancy and deadline margin | Sizes the negotiated lead. |
| Catch-up ticks/second and duration | Bounds suffix length. |
| Recovery attempts/success/final digest | Makes continuity testable. |

Measure solo host, two-player LAN, injected delay/jitter/loss, and high-population
storm/gold/combat. Report uncompressed and fast compression results. Throughput
dominates keyframe transfer; latency matters at the artifact deadline, gap
retransmit, and catch-up pause. Negotiate lead from RTT/jitter or reject paths that
cannot meet the deadline.

## 8. Incremental roadmap

### Stage 0 — actionable failures

- Keep per-record digest breakdown and explicit disconnect status.
- Log session identity, participant/coordinator, lead, and build fingerprint.
- Capture both logs and journals for every manual reproduction.
- Add a rate-limited shared snapshot dump on divergence.

### Stage 1 — reliable ordered crossings

- Add host `SessionSeq`, contiguous acks, retained epochs, gap detection, bounded
  retransmission, and a `RECOVERING` state for unresolved gaps.
- Distinguish queue refusal, disconnect, decode failure, and simulation mismatch.

This prevents recoverable transport loss but cannot fix logic divergence.

### Stage 2 — full shared keyframes

- Define a versioned canonical codec/component manifest.
- Round-trip into a new world and assert identical shared snapshot, next RNG draw,
  and next shared entity ID.
- Use keyframe plus suffix for late join/manual resync.
- Implement the guest-local merge and staging swap.

### Stage 3 — automatic recovery and reconnect

- Freeze guest shared simulation on confirmed divergence, request a committed
  keyframe, catch up, verify, and resume.
- Reconnect with session/participant tokens and ownership epochs.
- End the session explicitly on host loss initially. Host migration requires
  replicated history, election, and split-brain prevention as a separate project.

### Stage 4 — optimise from evidence

- Add dirty tracking/deltas if measured bandwidth warrants.
- Adapt cadence and playout lead within negotiated bounds.
- Consider short rollback/prediction only for remaining responsiveness issues.

## 9. Required invariants and evidence

Protocol tests should assert that keyframe round-trip preserves the shared snapshot,
next RNG draw, and shared entity ID; local entities never import; local restoration
cannot mutate a cursor it does not own; duplicate/reordered frames apply once in
sequence; a gap retransmits or recovers but is never skipped; keyframe plus suffix
equals uninterrupted simulation; recovery during storm, composite destruction,
departure, and reset ends on the host digest; invalid keyframes are rejected before
swap; and host loss cannot present as `SYNCED`.

Manual play remains the quickest leak detector. For the next reproduction preserve
both logs, both journals, exact commit, terminal sizes, CLI arguments, and all
pause/resize actions. At first `DESYNC`, record both `network.sync_records` and dump
both shared snapshots. Attribute cause from the first unequal tick, not the boss
visible later.

### Manual acceptance for the current branch

1. Run host and guest idle through at least two storm cycles; require no digest
   mismatch, late artifact, or transport-loss counter.
2. Move/type/fire both cursors concurrently through storm, then issue `:new` on
   the host; require the same reset and shared digest.
3. Quit the host cleanly; require the guest host-loss message and
   `NET:DOWN/LOCK` without waiting for `DESYNC`.
4. Black-hole the connection without closing either process; require the same
   message after the configured silent timeout (30 seconds by default) and record
   the observed duration.
5. Once recovery exists, deliberately corrupt one shared component, install a
   committed keyframe plus suffix, and require digest equality before play resumes.
6. Repeat recovery at storm high-water and record bytes, capture/load time, replay
   rate, allocation peak, and total interruption.

## 10. Primary references for the patterns

- Ensemble's [Age of Empires lockstep account](https://www.gamedeveloper.com/programming/1500-archers-on-a-28-8-network-programming-in-age-of-empires-and-beyond)
  is the closest conceptual match: small commands plus deterministic simulation,
  with determinism treated as a strict engineering contract.
- Valve's [Source multiplayer networking](https://developer.valvesoftware.com/wiki/Source_Multiplayer_Networking)
  describes authoritative snapshots, deltas from acknowledged baselines, full
  fallback snapshots, interpolation, prediction, and lag compensation.
- The [GGPO developer guide](https://github.com/pond3r/ggpo/blob/master/doc/DeveloperGuide.md)
  states rollback's prerequisites: deterministic simulation, fully encapsulated
  serializable state, load/save, and frame advance without rendering.
- Unity Netcode's [client prediction documentation](https://docs.unity3d.com/Packages/com.unity.netcode%401.4/manual/intro-to-prediction.html)
  describes applying an authoritative snapshot and selectively rolling predicted
  entities back.
- Unity's [host migration documentation](https://docs.unity.com/en-us/mps-sdk/session-host-migration)
  separates capturing/applying network-synchronised data from choosing a new host.
  Its [host election documentation](https://docs.unity.com/en-us/mps-sdk/session-op-host)
  likewise notes that election alone does not migrate network state.

These are evidence for primitives, not templates to copy unchanged. Vi-fighter's
mixed shared/player domains and already-small artifact stream make a canonical
keyframe-plus-suffix protocol the least disruptive combination.
