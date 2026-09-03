# Multi-instance domain model — vi-fighter

This document describes the multiplayer domain model as implemented. Rules D-1
through D-24 define simulation ownership, transport, correction, admission, and
authority continuity. The operational overview and remaining roadmap are in
[Multiplayer architecture and remaining work](multi-player-enhancement.md).

Terminology is deliberately orthogonal:

- a **network peer** is a transport endpoint;
- the **game host** is the current Shared-domain authority and coordinator;
- a **game guest** predicts the Shared world and adopts corrections;
- a **relay** forwards and retains authoritative content without authoring it;
- **Shared** and **Player** describe simulation ownership, not network role;
- the **authority term** identifies the generation under which authoritative
  content was produced.

Participant 1 opened the session; it is not permanently synonymous with the
authority. A successful handoff increments the term and moves authorship without
changing participant IDs, roster slots, or entity domains. In this document,
`epoch` means one source's closed tick of crossing production.

## 1. Domains and identity

Every `World` has two domains:

- **Shared** is authoritative on the host and predicted on guests. It contains
  shared species, cursors, gold, walls, towers, gateways, map state, simulation
  time, and the shared FSM.
- **Player** belongs to the local participant. It contains corpus glyphs, drains,
  nuggets, weapons, projectiles, loot, and presentation effects. It is never
  reconstructed on another instance.

`core.Entity` is `[domain:8][id:56]`. Each domain has its own allocator counter;
zero is invalid in both. A cursor's roster slot lives in `CursorComponent` and is
not encoded in the domain tag.

A participant and a cursor are separate things. A participant holds an identity,
an authority term and a vote; a roster slot is what binds it to a cursor. The
coordinator of a dedicated host holds `parameter.NoPlayerSlot` and therefore no
cursor: it authors the Shared world and puts nobody on the map. Only the
coordinator may be cursorless — every other participant is in the session to drive
one — and the ceiling of `parameter.MaxPlayers` counts cursors rather than
participants.

## 2. Domain rules

### D-1 — Reads follow ownership

A player-domain system may read Shared state. A shared-domain system reads Shared
state only, except for the narrow geometry and owner-authored seams named by D-12
and D-13. Cross-domain access is declared and checked rather than inferred from a
filename or call stack.

### D-2 — One instance simulates each player

Only the instance that owns a cursor simulates that cursor's weapons, projectiles,
drains, nuggets, and player effects. `World.SimulatesLocally` and
`World.ResolveOwnedCursor` are the admission gates. Remote Player-domain state
does not exist locally.

The same rule admits owner-authored cursor writes: grants and per-tick ageing do
not update a cursor this instance does not simulate.

### D-3 — Cross the smallest shared outcome

When a player mechanic affects Shared state, it emits the smallest artifact that
fully determines the shared outcome. Ordinary crossings apply immediately on the
producer and on the receive schedule everywhere else. The host applies requests
in its own order; its next correction is canonical.

| Effect | Crossing artifact |
|---|---|
| Direct player hit on a shared target | one combat request per target |
| Missile, dust, or disruptor area effect | immutable centre/radius/attack geometry |
| Drain fusion | one spawn request from the causal participant |
| Shared gold member typed | header, member, and typist cursor |
| Dying drain donation | target and heal amount |
| Personal drain death affecting shared progression | owner cursor |
| Typing or nugget cursor advance | cursor and absolute destination cell |
| Owned shield striking shared species | target/member set and owner cursor |
| Cursor entering or leaving combined defeat state | cursor and state |

Effects on Player targets do not cross. Shared follow-up events derived from a
crossing do not cross again (D-5).

Arrival, departure, and full reset are `barrierBound`. They create or destroy
shared identity, so their producer also waits for the agreed apply tick.

### D-4 — Wire payloads name Shared entities only

A replicated payload may name Shared entities. Player emitters are reduced to
values such as cell, velocity, radius, amount, and owner cursor. Local payloads may
name Player entities.

The same restriction applies inside a capture: a component on a Shared entity may
not hold a Player-domain entity handle. Store-derived local indexes are used where
runtime effects need such handles.

### D-5 — Derived events are not transported

An event produced deterministically from a replicated event is re-derived on every
instance and must not be sent. For example, explosion geometry crosses;
per-target combat requests derived from that geometry do not.

### D-6 — Presentation and personal effects are Player-domain

Lightning, flash, fadeout, splash, motion markers, explosion smoke, materialise
beams, dust, decay, blossom, orbs, bullets, missiles, and loot are Player-domain.
They may depend on local view state and must not decide a Shared outcome.

Player-domain does not mean that only one participant sees an effect. A shared FSM
may raise a local effect on every instance. Effects intended for one participant
carry a cursor scope; entity zero means session-wide, while a non-zero cursor is
admitted only by the instance that simulates it.

### D-7 — Domain is explicit ambient context

`World.WithDomain`, `PushEventDomain`, `PushLocal`, and `PushCrossing` stamp the
producer or target domain. The declared system profile does not silently change
the ambient tag. Generic systems resolve the request or target domain at runtime.

### D-8 — RNG streams are domain-separated and restorable

`RandResource.Stream(domain, label)` derives from `(session root, domain, label)`.
A dual system chooses the stream from the target domain; a Player-only system uses
its Player stream. Simulation never seeds from wall time.

Every issued stream is inventoried by `RandResource`, and a capture stores its
generator position, not merely its seed. Restoring a seed restarts a sequence;
restoring a position continues it.

### D-9 — Entity identity is domain-local and deterministic

`CreateEntity(domain)` uses one counter per domain. Shared entity creation order
must be identical wherever the same Shared world is represented. Creation and
destruction telemetry is tracked per domain; aggregate values are sums.

### D-10 — Event class and event domain answer different questions

Each event type declares one class:

| Class | Meaning |
|---|---|
| `Shared` | emitted and consumed in Shared simulation; re-derived, not sent |
| `Bus` | may be a Player-originated request affecting Shared state |
| `Local` | never replicated |
| `Stamped` | class depends on the event's explicit domain tag |

`event.Replicated` answers whether two instances should hold the record.
`event.OnWire` answers whether another peer must receive it. The wire set is
narrower: sending a re-derived Shared event would apply it twice. A Bus type is on
the wire only when a Player producer explicitly used the crossing path.

### D-11 — The host is exact; guests converge

The host owns the canonical Shared event order, entity creation order, RNG
progress, and component values. A guest equals the most recently installed host
world plus its bounded local prediction suffix; it may differ between corrections
by design.

Shared entity identity and creation order stay exact. Component values are either
re-derived deterministically or owner-authored and transported, never both.

Correction containment has two boundaries:

- peer-authored and barrier-bound frames are classified against the capture tick;
- ordinary host-authored frames are classified against the capture's completed
  authority crossing sequence, because the host applied them locally before their
  remote `ApplyTick`.

### D-12 — Shared geometry may inspect both domains

A Shared system that claims cells—spawn footprint clearing, composite sweep, or
wall push-out—may enumerate both domains. Its Shared outcome depends only on the
cell set and protection masks; Player victims remain local effects. Emission is
split by domain so a Shared record never names a Player entity.

### D-13 — Owner-authored Shared values have one writer

A Shared cursor carries values written by exactly one instance and transported
rather than re-derived:

- energy, heat, boost, shield, weapon, and cursor combat values;
- `CursorComponent.Control` and `PeerID`;
- `CursorViewComponent`, `PingComponent`, and `PulseComponent` presentation.

Position is different: it is Shared and changes through
`EventCursorMoveRequest`. Protection is a deterministic creation constant.

A capture carries owner-authored values so a joiner can materialise remote
cursors. During an in-session correction, the receiver preserves the values for
cursors it authors and rebuilds control/roster binding from participant identity.
No shared-profile system computes a remote owner's values.

### D-14 — Map bounds are Shared authority

`MapWidth`, `MapHeight`, and `CropOnResize` are Shared simulation state. Session
admission installs them before the FSM boots. While `World.SessionShared()` is
true, a terminal resize changes only local viewport/camera state; it cannot crop
the map or announce a Shared cursor move.

The session-shared latch survives lobby wait, disconnect, and journal replay. A
script reading viewport-only values while a session is shared is warned because it
can choose a different FSM branch on different terminals.

### D-15 — Classification is declared once

System domain profiles, required/optional dependencies, and snapshot participation
are declared in `internal/manifest/definition.go` and generated into runtime
tables. Initialization dependency order is distinct from per-tick system priority.
A `dual` profile means the system resolves domain per request or target; it does
not grant unrestricted cross-domain writes.

### D-16 — Shared triggers elect one causal player

A Shared trigger must not fan out into every participant's Player mechanic when
those mechanics would each cross one logical Shared result. The trigger selects a
causal cursor deterministically before any Player state is read, and only that
cursor's owner may emit the crossing.

Personal causes remain distinct. Separate participant-owned drain collisions may
each produce their own swarm request.

### D-17 — Throttle phase is state

A cached Shared derivation with a recompute throttle carries both its inputs and
its phase. Dirty history changes the age of the value consumers read. Navigation
therefore snapshots recompute counters, last targets, and gateway route rebuild
budget, then re-derives fields and graphs during install from the captured inputs.

Local view changes do not mark Shared navigation dirty.

### D-18 — Local prediction is private

A value selected by this participant's input is reflected locally at once. The
local cursor prediction is a bounded FIFO of requested absolute cells not yet
announced by Shared simulation. Input, camera, and rendering read it; Shared
systems do not.

`World.PushCursorMove` advances prediction and emits the crossing atomically from
the producer's perspective. An expected applied cell pops the queue. An
authoritative move the prediction did not produce clears the queue and snaps.
Replay reconstructs prediction from recorded Player-stamped move requests.

### D-19 — Every future-affecting Shared value is restorable

A value that can change a future Shared outcome is one of:

1. a component on a Shared entity;
2. declared system state carried through `engine.SharedStateSaver`; or
3. state re-derived by the install itself from captured inputs.

Declared system carriers are:

| System | Private Shared state |
|---|---|
| `wall` | maze generator position |
| `adaptation` | route weights, sample pool, consumer head, fallback rotation |
| `genetic` | streaming checkpoints, archives, pending evaluations, IDs, scout state, fitness accumulators |
| `navigation` | recompute phase, targets, route rebuild budget, route endpoints |
| `gold` | sequence liveness, header, deadlines, per-slot contribution |

A capture also carries every RNG stream position, FSM runtime state, Shared
component stores, allocator counters, and the compared status surface. Durations
are relative to capture tick. Absolute component instants are sound because
`engine.SimEpoch` and tick interval are session identity.

Snapshot schema 3 carries the complete simulation checkpoint plus the completed
authority crossing sequence. An install preserves receiver-owned cursor values,
rebuilds roster/control binding, derives spatial/navigation caches, and restores
status only after carriers have loaded.

### D-20 — Shared FSM regions use replicated triggers

Every FSM region is Shared state. A transition trigger must therefore be present
on every instance; a `Local` event cannot steer a Shared region. Per-instance
effects are emitted by the local owning system or by a cursor-scoped action without
moving the region differently.

### D-21 — Simulation time is tick-derived

`engine.SimTime(tick, interval)` measured from `engine.SimEpoch` is the only
simulation instant. Wall time paces ticks and presentation but cannot select a
Shared transition or kinetics threshold. `DeltaTime` and
`time.game_elapsed_ms` are functions of tick interval and completed tick.

### D-22 — Admit before capturing

A running join becomes a transport peer before the authority capture is read. It
buffers traffic until the world it applies to exists, closing the interval in
which an event could otherwise be in neither the capture nor the wire stream.

The join uses a sufficiently recent cadence keyframe, waits through the receive
lead so pre-admission epochs are represented, then simulates only a bounded
transfer gap. Admission refuses a gap beyond the lead or a link that cannot carry
one keyframe inside the convergence floor.

After install, the barrier remembers both the capture tick and authority sequence.
Already-contained queued artifacts are discarded, and later arrivals are tested
against the same boundary.

### D-23 — The host world is the correction

The host publishes an authoritative `SharedCapture` on a cadence. JSON remains the
schema and integrity surface; a bounded versioned deflate envelope precedes
chunking. A guest resolves into a reusable staging world and commits between
ticks.

A correction exchange is selective:

1. the authority sends a deterministic manifest of section hashes;
2. an equal root produces a hash-only acknowledgement and no state body;
3. a mismatch descends only into differing sections and pages;
4. returned pages prove their own hash and must reconstruct the authority root;
5. any refusal leaves the live world untouched and falls back to a keyframe.

The manifest root excludes tick-local header values so worlds at different
prediction ticks can compare. A repair must match the manifest's authority term,
participant, and crossing sequence fence. Capture integrity may differ when an
equal canonical world has another dense-store order.

Correction magnitude is measured during commit. It is telemetry, not a verdict.
Runtime shared digests identify drift surfaces between corrections but do not
escalate to a terminal desynchronisation state.

The guest retains its own encoded ordinary crossings and replays the suffix whose
authoritative apply ticks are later than the installed baseline. A hole makes the
suffix unavailable and selects authority-only recovery.

### D-24 — Cadence adapts; the convergence floor does not

Each direct peer receives a cadence and keyframe interval selected from measured
round-trip time, jitter, delivered bytes, saturation, and relative correction
demand. Degradation is immediate; recovery is stepped. Keyframes stretch before
cadence slows because whole worlds are more expensive than manifests and ordinary
repairs.

No plan may leave more than `SnapshotFloorKeyframeTicks` between whole worlds. A
link that cannot meet the floor is refused at admission or reported as an
unrecoverable operating condition. Relevance changes when a peer receives a
correction, not which version of Shared state is authoritative.

## 3. Spatial and entity classification

`Cell` is 256 bytes: count, Shared count, metadata, and 31 entities. Shared
occupants are the prefix `Entities[:SharedCount]`; Player occupants follow.
`ReservedPlayerPerCell` preserves Shared capacity, and insertion rejects rather
than evicts when full.

Spatial queries take `ScopeShared`, `ScopePlayer`, or `ScopeBoth`. Shared species
target Shared entities; local weapons may target both. `PositionBatch.CommitShared`
is the Shared placement gate.

| Domain | Entities |
|---|---|
| **Shared** | cursor, quasar, swarm, storm, snake, eye, pylon, tower, gateway, wall, gold, marker, FSM, time |
| **Player** | glyph, nugget, dust, drain, decay, blossom, bullet, missile, orb, lightning, flash, fadeout, splash, motion marker, explosion presentation, loot |
| **Stamped** | cleaner, materialise, spirit; domain is resolved per request |

Gold is contested Shared state: typing contribution is tallied per roster slot and
ties resolve to the lowest slot. Nuggets and loot are personal and cannot be
claimed by a remote cursor. Loot owns a private single-goal route to its cursor;
the Shared navigation field answers nearest-target questions and cannot answer
"mine".

## 4. Transport and ordering

### 4.1 Receive lead

Ordinary local crossings are published immediately and encoded into the source's
next epoch for peers. Remote copies wait for an absolute `ApplyTick`. At tick open,
due frames sort by `(ApplyTick, Source, Seq)` and settle before `BeginTick`.

A correction may rewind world tick, but never the source's production epoch.
Peers use `ProducedTick` as a replay key; reusing one after rewind would make a new
batch look like a duplicate. Crossings produced during catch-up remain in the next
unsent epoch until world tick reaches it.

Snapshot containment is not a universal tick comparison. Authority-local ordinary
frames use `AuthorityCrossingSeq`; barrier-bound and other-source frames use the
capture tick. Both the already-scheduled queue and later arrivals apply the same
classification.

### 4.2 Mesh relay

Every node forwards a newly admitted source epoch to each neighbour except the
arrival edge. A per-source 64-epoch window admits out-of-order paths once and
suppresses duplicates. The hop limit is a backstop, not the termination rule.

Owner-state sync uses a per-slot sequence. Correction chunks carry the authority
term and capture tick unchanged. A relay serves selective pages only from retained
authority captures whose root it can prove; an unavailable retained tick is
reported rather than answered from another baseline.

### 4.3 Runtime parity

Instances exchange category digests for the same surface as `SnapshotShared`.
Mismatches increment drift telemetry and name position, kinetics, combat, context,
status, or combined surface. Guests are expected to differ provisionally, so a
digest is diagnostic rather than a failure state.

The player-visible distinction is:

- `COR n`: the last correction changed n entities/cells;
- `LAG n`: this instance is far enough behind that the receive lead is being
  missed;
- `LINK!`: no feasible cadence satisfies the keyframe floor;
- `HOST LOST:LOCAL`: this instance is an explicit local fork.

### 4.4 Membership and session control

Arrival and departure are coordinator-authored barrier crossings. A non-authority
roster artifact is refused. Full reset is likewise serialized by the coordinator;
it preserves the closed roster and rebuilds cursors in slot order.

Pause, speed, step, raw Shared mutation, and synchronous diagnostic save are
instance-local operator actions and are refused while peers are live. Inspection
modes remain available without stopping simulation.

### 4.5 Join and stream framing

The handshake establishes schema, tick interval, seed/session, configuration,
corpus identity, map latch, authority term, participant ID, and roster slot before
state is accepted. A slot of `parameter.NoPlayerSlot` is the coordinator declaring
it drives nothing. A mid-run join installs a current keyframe and catches up the
bounded transfer gap. Reconnect uses the same path.

`network.SocketPort` uses a fixed 12-byte frame header and complete short-read and
short-write handling. Heartbeats and deadlines close silent streams. Bounded input
and output queue refusal is counted; application events are not retransmitted.
Newer corrections and keyframes recover state.

At 20 ticks/s the event stream is small relative to authoritative state: an empty
epoch is 44 bytes, four cursor moves are about 567 bytes, and one owner-state sync
is about 703 bytes. At the storm high-water fixture, a plain capture is about
172 KiB, a compressed keyframe about 15.4 KiB, a compressed delta about 7.1 KiB,
and a converged manifest exchange about 1.5 KiB.

## 5. Telemetry and snapshot views

Three views share one status reading:

| View | Excludes | Purpose |
|---|---|---|
| `Snapshot` | nothing | local diagnostics |
| `SnapshotSimulation` | operator/session surface | replay versus source run |
| `SnapshotShared` | Player, owner-authored, view, network, and local scheduler state | cross-instance comparison |

The Shared filter is driven by system profiles and explicit key/field exclusions.
It keeps tick-derived elapsed time, Shared FSM state, Shared navigation phase, map
bounds, corpus identity, and canonical Shared entity digests. It drops terminal,
camera, network, pacing, Player aggregate, owner-authored cursor, and local effect
state.

Correction/order diagnostics include:

- `snapshot.correction_entries`, `snapshot.correction_entities`,
  `snapshot.correction_cells`;
- `snapshot.replay_records`, `snapshot.replay_skipped`,
  `snapshot.replay_suffix_unavailable`;
- `network.artifacts_pre_install` and
  `network.artifacts_authority_superseded`;
- `network.barrier_late`, `network.lag_ticks`, `network.stale`;
- `network.transport_lost_in`, `network.transport_lost_out`;
- `snapshot.cadence_*`, `network.link_*`, and authority-term counters.

## 6. Verification model

The boundaries are enforced mechanically:

- generated manifests classify systems, dependencies, snapshot carriers, and
  event types;
- static boundary checks compare declared profiles with component/RNG/event use;
- deterministic capture/install criteria continue a world after transfer;
- two-participant, TCP, and mesh criteria drive local prediction and convergence;
- selective repair criteria cover equal roots, sparse disagreement, corrupt
  proofs, supersession, relay retention, and keyframe fallback;
- correction ordering criteria cover local replay, queued authority frames, late
  authority frames, and captures racing undispatched input;
- link criteria cover cadence bounds, saturation, recovery, and floor refusal;
- succession criteria cover majority election, retention eligibility, term gates,
  roster continuity, and explicit fork fallback.

Run the generated and repository gates after focused changes:

```sh
go generate ./internal/event ./internal/manifest
go test ./...
go vet ./...
```

Manual host/guest acceptance should use different terminal sizes and rapid input
from both participants. After a correction, a remote cursor must not walk backward
through older absolute positions. Resize must not move the map, host reset must
preserve the roster, guest reset must be refused, disconnect must remove only the
departing cursor, and reconnect must take a current world.

## 7. Current limits and next work

| Area | Current limit |
|---|---|
| Trust | Transport, participant claims, votes, and handoff records are unauthenticated and plaintext. Structural checks prevent races, not hostility. |
| Guest replay | Suffix membership uses the agreed apply tick. If a guest frame misses the lead and a host capture overtakes it, one correction can omit the action. A per-source applied sequence fence is the exact follow-up. |
| Playout | The three-tick receive lead is fixed and not graph-diameter aware. |
| Topology | The protocol relays over a graph, but `-join` dials one address, so ordinary CLI sessions form a star. |
| Partition | Majority succession works; merging an explicit local fork does not. |
| Relay scheduling | A relayed participant inherits its neighbour's cadence and repair pricing. |
| Operator API | Interactive mutation is session-aware; programmatic map/FSM mutation still relies on caller discipline. |
| Tower ownership | Optional tower configurations still bind to slot zero rather than an explicit session-owned/cursor-owned rule. |
| Progression | `kills.drain` is a session total, so quasar progression is session-wide rather than per cursor. |
| Presentation | Small terminals clip the map; remote cursor presentation has no interpolation beyond receive scheduling. |
| Portability | `float64` determinism is a same-build guarantee, not arbitrary cross-platform bit-exact lockstep. |

Domain-boundary debt remains visible:

- remove the remaining ambient-Shared exemptions for Local-class events;
- route `event.EmitDeath` through the ordinary event boundary;
- remove Shared-only entity casts from gateway/adaptation code;
- split mixed combat telemetry so Shared results can be compared directly;
- close the programmatic operator surface;
- define tower and per-cursor progression ownership together.

Authentication is the next security-shaped change. Per-source applied crossing
fences are the next correction-ordering refinement if late-link guest rollback is
observed. Adaptive playout and presentation interpolation must remain outside
Shared simulation decisions.
